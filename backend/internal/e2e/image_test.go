//go:build e2e

// Package e2e contains image-level tests that provision real Incus containers
// and verify the resulting environment.
//
// These tests talk directly to a live Incus server — no Plati API layer is
// involved.  They are intentionally skipped when the required env vars are
// absent so that the normal `go test ./...` run (unit + integration) is
// unaffected.
//
// # Required env vars
//
//	INCUS_ENDPOINT   Incus API URL              (default: https://127.0.0.1:8443)
//	INCUS_TLS_CERT   path to TLS client cert    (default: <repo>/config/plati-client.crt)
//	INCUS_TLS_KEY    path to TLS client key     (default: <repo>/config/plati-client.key)
//
// # Optional env vars
//
//	TAILSCALE_AUTH_KEY  auth key for the tailscale join check
//	IMAGE_TIMEOUT_SEC   readiness wait budget in seconds (default: 300)
//
// # Running
//
//	./tests.sh --image              # via the top-level runner
//	cd backend && go test -v -timeout 30m ./internal/e2e/...
package e2e_test

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/homaserver/plati/internal/incus"
)

// ── helpers ───────────────────────────────────────────────────────────────────

const (
	defaultEndpoint   = "https://127.0.0.1:8443"
	defaultTimeoutSec = 300
)

// templateYAML mirrors services.TemplateYAML to avoid a service-layer import.
type templateYAML struct {
	Name     string   `yaml:"name"`
	Image    string   `yaml:"image"`
	Profiles []string `yaml:"profiles"`
}

// repoRoot returns the absolute path to the repository root by walking up from
// this file (backend/internal/e2e/image_test.go → ../../.. = repo root).
func repoRoot() string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(file), "..", "..", "..")
}

// loadTemplate reads and parses a YAML template from config/templates/<slug>.yaml.
func loadTemplate(t *testing.T, slug string) templateYAML {
	t.Helper()
	path := filepath.Join(repoRoot(), "config", "templates", slug+".yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read template %s: %v", path, err)
	}
	var tmpl templateYAML
	if err := yaml.Unmarshal(data, &tmpl); err != nil {
		t.Fatalf("parse template %s: %v", path, err)
	}
	return tmpl
}

// newIncusClient creates a real Incus client from env vars.
// The test is skipped when the cert/key files cannot be located.
func newIncusClient(t *testing.T) *incus.Client {
	t.Helper()

	endpoint := envOr("INCUS_ENDPOINT", defaultEndpoint)
	cert := envOr("INCUS_TLS_CERT", filepath.Join(repoRoot(), "config", "plati-client.crt"))
	key := envOr("INCUS_TLS_KEY", filepath.Join(repoRoot(), "config", "plati-client.key"))

	if _, err := os.Stat(cert); err != nil {
		t.Skipf("Incus TLS cert not found (%s) — set INCUS_TLS_CERT to run image tests", cert)
	}
	if _, err := os.Stat(key); err != nil {
		t.Skipf("Incus TLS key not found (%s) — set INCUS_TLS_KEY to run image tests", key)
	}

	client, err := incus.NewClient("e2e", endpoint, cert, key)
	if err != nil {
		t.Skipf("cannot connect to Incus at %s: %v", endpoint, err)
	}
	return client
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func readyTimeout() int {
	if v := os.Getenv("IMAGE_TIMEOUT_SEC"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return n
		}
	}
	return defaultTimeoutSec
}

// containerName returns a unique container name that is safe to use as an Incus
// instance name (alphanumeric + hyphens, max 63 chars).
func containerName(slug string) string {
	return fmt.Sprintf("plati-e2e-%s-%d", slug, time.Now().Unix())
}

// provision creates and starts a container, registering a cleanup that stops
// and deletes it even on test failure.
func provision(t *testing.T, client *incus.Client, name string, tmpl templateYAML, envVars map[string]string) {
	t.Helper()

	cfg := map[string]string{}
	for k, v := range envVars {
		cfg["environment."+k] = v
	}

	t.Logf("creating container %s (image: %s, profiles: %v) …", name, tmpl.Image, tmpl.Profiles)
	if err := client.CreateInstance(name, tmpl.Image, tmpl.Profiles, cfg, nil); err != nil {
		// Skip rather than fail when a required Incus profile doesn't exist.
		// The tailscale profile, for example, must be created with
		// scripts/setup-tailscale-profile.sh before that template can be tested.
		if strings.Contains(err.Error(), "profile") && strings.Contains(err.Error(), "doesn't exist") {
			t.Skipf("required Incus profile not configured (run scripts/setup-tailscale-profile.sh if needed): %v", err)
		}
		t.Fatalf("create container: %v", err)
	}

	t.Cleanup(func() {
		t.Logf("cleanup: stopping and deleting %s", name)
		_ = client.StopInstance(name)
		if err := client.DeleteInstance(name); err != nil {
			t.Logf("cleanup: delete %s: %v", name, err)
		}
	})

	if err := client.StartInstance(name); err != nil {
		t.Fatalf("start container: %v", err)
	}
	t.Logf("container %s started", name)
}

// waitReady waits until the container is responsive by polling a simple command.
// On /cloud images this effectively waits for cloud-init to finish creating users.
func waitReady(t *testing.T, client *incus.Client, name string) {
	t.Helper()
	timeout := readyTimeout()
	deadline := time.Now().Add(time.Duration(timeout) * time.Second)
	t.Logf("waiting for %s to be ready (budget: %ds) …", name, timeout)

	for time.Now().Before(deadline) {
		_, err := client.RunCommand(name, []string{"true"})
		if err == nil {
			t.Logf("container %s is ready", name)
			return
		}
		time.Sleep(5 * time.Second)
	}
	t.Fatalf("container %s not ready after %d seconds", name, timeout)
}

// run executes a command inside the container and returns trimmed stdout.
// The test is failed immediately if the command exits non-zero.
func run(t *testing.T, client *incus.Client, name string, cmd ...string) string {
	t.Helper()
	out, err := client.RunCommand(name, cmd)
	out = strings.TrimSpace(out)
	if err != nil {
		t.Fatalf("exec %v in %s: %v\noutput: %s", cmd, name, err, out)
	}
	return out
}

// runContains runs a command and asserts its output contains want.
func runContains(t *testing.T, client *incus.Client, name, want string, cmd ...string) string {
	t.Helper()
	out := run(t, client, name, cmd...)
	if !strings.Contains(out, want) {
		t.Errorf("exec %v: expected output to contain %q\ngot: %s", cmd, want, out)
	}
	return out
}

// ── Tailscale ─────────────────────────────────────────────────────────────────

func TestImage_Tailscale(t *testing.T) {
	client := newIncusClient(t)
	tmpl := loadTemplate(t, "tailscale")
	name := containerName("tailscale")

	envVars := map[string]string{}
	if key := os.Getenv("TAILSCALE_AUTH_KEY"); key != "" {
		envVars["TAILSCALE_AUTH_KEY"] = key
	}

	provision(t, client, name, tmpl, envVars)
	waitReady(t, client, name)

	// tailscaled service must be active (systemctl is-active exits 0 when active).
	run(t, client, name, "systemctl", "is-active", "tailscaled")
	t.Logf("tailscaled: active")

	// If an auth key was provided, verify the node has joined the network.
	if _, hasKey := envVars["TAILSCALE_AUTH_KEY"]; hasKey {
		out := run(t, client, name, "tailscale", "status")
		if strings.Contains(out, "Logged out") {
			t.Errorf("expected tailscale to be joined, got: %s", out)
		}
		t.Logf("tailscale status: %s", firstLine(out))
	} else {
		t.Logf("TAILSCALE_AUTH_KEY not set — skipping join check")
	}
}

// ── SSHX ──────────────────────────────────────────────────────────────────────

func TestImage_SSHX(t *testing.T) {
	client := newIncusClient(t)
	tmpl := loadTemplate(t, "sshx")
	name := containerName("sshx")

	provision(t, client, name, tmpl, nil)
	waitReady(t, client, name)

	// sshx systemd service must be active.
	run(t, client, name, "systemctl", "is-active", "sshx")
	t.Logf("sshx service: active")

	// sshx prints its collaborative URL to the journal shortly after starting.
	// Retry for up to 60 s in case the service just started.
	urlCmd := `journalctl -u sshx.service -n 200 --no-pager -o cat 2>/dev/null | grep -oE 'https://sshx\.io/s/[^[:space:]]+'`
	deadline := time.Now().Add(60 * time.Second)
	var sshxURL string
	for time.Now().Before(deadline) {
		out, _ := client.RunCommand(name, []string{"/bin/sh", "-c", urlCmd})
		if url := strings.TrimSpace(out); url != "" {
			sshxURL = url
			break
		}
		time.Sleep(3 * time.Second)
	}
	if sshxURL == "" {
		t.Error("sshx URL not found in journal after 60 s — service may not have printed it yet")
	} else {
		t.Logf("sshx URL: %s", sshxURL)
	}
}

// ── Node.js development ───────────────────────────────────────────────────────

func TestImage_NodeDev(t *testing.T) {
	client := newIncusClient(t)
	tmpl := loadTemplate(t, "node-dev")
	name := containerName("node-dev")

	provision(t, client, name, tmpl, nil)
	waitReady(t, client, name)

	ver := runContains(t, client, name, "v", "node", "--version")
	t.Logf("node: %s", ver)

	npmVer := run(t, client, name, "npm", "--version")
	t.Logf("npm: %s", npmVer)

	pnpmVer := run(t, client, name, "pnpm", "--version")
	t.Logf("pnpm: %s", pnpmVer)
}

// ── Python development ────────────────────────────────────────────────────────

func TestImage_PythonDev(t *testing.T) {
	client := newIncusClient(t)
	tmpl := loadTemplate(t, "python-dev")
	name := containerName("python-dev")

	provision(t, client, name, tmpl, nil)
	waitReady(t, client, name)

	ver := runContains(t, client, name, "Python 3", "python", "--version")
	t.Logf("python: %s", ver)

	poetryVer := run(t, client, name, "poetry", "--version")
	t.Logf("poetry: %s", poetryVer)
}

// ── Site CA ───────────────────────────────────────────────────────────────────

func TestImage_SiteCA(t *testing.T) {
	client := newIncusClient(t)
	tmpl := loadTemplate(t, "site-ca")
	name := containerName("site-ca")

	provision(t, client, name, tmpl, nil)
	waitReady(t, client, name)

	// Node.js is installed.
	nodeVer := runContains(t, client, name, "v", "node", "--version")
	t.Logf("node: %s", nodeVer)

	// Repository was checked out.
	run(t, client, name, "test", "-d", "/srv/site-ca")
	t.Logf("/srv/site-ca: present")

	// npm ci completed (node_modules exists).
	run(t, client, name, "test", "-d", "/srv/site-ca/node_modules")
	t.Logf("/srv/site-ca/node_modules: present")
}

// ── proxy helpers ─────────────────────────────────────────────────────────────

// startBridgeProxy starts a minimal HTTP/HTTPS proxy on the incusbr0 gateway
// address (10.70.69.1:3128) so that containers can reach the internet even when
// the host's nftables FORWARD chain blocks direct container egress.
// Returns the proxy URL ("http://10.70.69.1:3128"), or "" if the address is
// unavailable (e.g. not running on a machine with incusbr0).
func startBridgeProxy(t *testing.T) string {
	t.Helper()
	addr := "10.70.69.1:3128"
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		t.Logf("cannot bind proxy on %s (%v) — containers must have direct internet access", addr, err)
		return ""
	}
	srv := &http.Server{Handler: http.HandlerFunc(bridgeProxyHandler)}
	t.Cleanup(func() { _ = srv.Close() })
	go srv.Serve(ln) //nolint:errcheck
	t.Logf("bridge proxy listening on %s", addr)
	return "http://" + addr
}

func bridgeProxyHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodConnect {
		// HTTPS tunnel via CONNECT.
		dst, err := net.DialTimeout("tcp", r.Host, 30*time.Second)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		hijacker, ok := w.(http.Hijacker)
		if !ok {
			dst.Close()
			http.Error(w, "hijacking not supported", http.StatusInternalServerError)
			return
		}
		src, _, err := hijacker.Hijack()
		if err != nil {
			dst.Close()
			return
		}
		src.Write([]byte("HTTP/1.1 200 Connection established\r\n\r\n")) //nolint:errcheck
		done := make(chan struct{})
		go func() {
			defer close(done)
			io.Copy(dst, src) //nolint:errcheck
			dst.Close()
		}()
		io.Copy(src, dst) //nolint:errcheck
		src.Close()
		<-done
		return
	}
	// Plain HTTP: forward as-is.
	outReq := r.Clone(r.Context())
	outReq.RequestURI = ""
	resp, err := http.DefaultTransport.RoundTrip(outReq)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()
	for k, vv := range resp.Header {
		for _, v := range vv {
			w.Header().Add(k, v)
		}
	}
	w.WriteHeader(resp.StatusCode)
	io.Copy(w, resp.Body) //nolint:errcheck
}

// ── Docker-in-Docker ──────────────────────────────────────────────────────────

// injectProxyViaExec configures apt, curl, and Docker proxy settings via exec
// commands after the container has started.
func injectProxyViaExec(t *testing.T, client *incus.Client, name, proxyURL string) {
	t.Helper()
	cmds := []struct{ desc, cmd string }{
		{"apt proxy", fmt.Sprintf(`printf 'Acquire::http::Proxy "%s";\nAcquire::https::Proxy "%s";\n' > /etc/apt/apt.conf.d/99proxy`, proxyURL, proxyURL)},
		{"curl proxy", fmt.Sprintf(`printf 'proxy = %s\nnoproxy = localhost,127.0.0.1\n' > /root/.curlrc`, proxyURL)},
		{"docker proxy dir", "mkdir -p /etc/systemd/system/docker.service.d"},
		{"docker proxy", fmt.Sprintf(`printf '[Service]\nEnvironment="HTTP_PROXY=%s"\nEnvironment="HTTPS_PROXY=%s"\nEnvironment="NO_PROXY=localhost,127.0.0.1"\n' > /etc/systemd/system/docker.service.d/proxy.conf`, proxyURL, proxyURL)},
	}
	for _, c := range cmds {
		if _, err := client.RunCommand(name, []string{"/bin/sh", "-c", c.cmd}); err != nil {
			t.Logf("proxy setup (%s): %v", c.desc, err)
		}
	}
}

func TestImage_DockerDev(t *testing.T) {
	client := newIncusClient(t)
	tmpl := loadTemplate(t, "docker-dev")
	name := containerName("docker-dev")

	provision(t, client, name, tmpl, nil)
	waitReady(t, client, name)

	// Start a local proxy so the container can reach the internet even when
	// the host's nftables FORWARD chain blocks direct container egress.
	if proxyURL := startBridgeProxy(t); proxyURL != "" {
		injectProxyViaExec(t, client, name, proxyURL)
	}

	// Docker daemon must be active.
	run(t, client, name, "systemctl", "is-active", "docker")
	t.Logf("docker: active")

	// Docker can run a container and serve HTTP.
	// Use --network host so nginx binds directly to the Incus container's
	// loopback, bypassing Docker's iptables DNAT port-mapping which does not
	// work reliably inside nested containers.
	run(t, client, name, "docker", "run", "-d", "--network", "host", "--name", "plati-test-http", "nginx:alpine")
	t.Logf("nginx container started")

	// Give nginx a moment to bind the port.
	time.Sleep(3 * time.Second)

	// --noproxy '*' bypasses /root/.curlrc proxy so we connect to localhost
	// directly inside the container, not via the bridge proxy on the host.
	out := runContains(t, client, name, "Welcome to nginx", "curl", "--noproxy", "*", "-s", "http://localhost:80")
	t.Logf("HTTP response: %s", firstLine(out))

	// Clean up the inner Docker container.
	run(t, client, name, "docker", "rm", "-f", "plati-test-http")
}

// ── Simple Web Server ────────────────────────────────────────────────────────

func TestImage_SimpleWebServer(t *testing.T) {
	client := newIncusClient(t)
	tmpl := loadTemplate(t, "simple-webserver")
	name := containerName("simple-webserver")

	envVars := map[string]string{}
	if key := os.Getenv("TAILSCALE_AUTH_KEY"); key != "" {
		envVars["TAILSCALE_AUTH_KEY"] = key
		envVars["PLATI_TAILSCALE_HOSTNAME"] = name
	}

	provision(t, client, name, tmpl, envVars)
	waitReady(t, client, name)

	// webserver.service must be active.
	run(t, client, name, "systemctl", "is-active", "webserver")
	t.Logf("webserver: active")

	// Health endpoint must respond with 200 and JSON body.
	// Retry for up to 30s in case the service is still starting.
	deadline := time.Now().Add(30 * time.Second)
	var healthBody string
	for time.Now().Before(deadline) {
		out, err := client.RunCommand(name, []string{"/bin/sh", "-c", "curl -s http://localhost:3000/health"})
		if err == nil && strings.Contains(out, `"status"`) {
			healthBody = strings.TrimSpace(out)
			break
		}
		time.Sleep(2 * time.Second)
	}
	if healthBody == "" {
		t.Fatal("health endpoint did not respond within 30s")
	}
	if !strings.Contains(healthBody, `"ok"`) {
		t.Errorf("expected health response to contain \"ok\", got: %s", healthBody)
	}
	t.Logf("health: %s", healthBody)

	// Root path should return HTML.
	rootBody := runContains(t, client, name, "Hello from Plati", "/bin/sh", "-c", "curl -s http://localhost:3000/")
	t.Logf("root: %s", firstLine(rootBody))

	// If Tailscale auth key was provided, verify tailscale is up and serve works.
	if _, hasKey := envVars["TAILSCALE_AUTH_KEY"]; hasKey {
		// Wait for tailscale to join.
		tsDeadline := time.Now().Add(60 * time.Second)
		var tsStatus string
		for time.Now().Before(tsDeadline) {
			out, err := client.RunCommand(name, []string{"tailscale", "status"})
			if err == nil && !strings.Contains(out, "Logged out") && !strings.Contains(out, "failed") {
				tsStatus = firstLine(strings.TrimSpace(out))
				break
			}
			time.Sleep(3 * time.Second)
		}
		if tsStatus == "" {
			t.Error("tailscale did not join tailnet within 60s")
		} else {
			t.Logf("tailscale: %s", tsStatus)
		}

		// Check if tailscale serve was auto-configured.
		serveOut, _ := client.RunCommand(name, []string{"/bin/sh", "-c", "tailscale serve status 2>&1"})
		if strings.Contains(serveOut, "3000") || strings.Contains(serveOut, "proxy") {
			t.Logf("tailscale serve: auto-configured (%s)", firstLine(strings.TrimSpace(serveOut)))
		} else {
			// Start tailscale serve manually.
			run(t, client, name, "/bin/sh", "-c", "tailscale serve --bg https+insecure://localhost:3000")
			t.Logf("tailscale serve: started manually")
		}

		// Get DNS name and verify HTTPS.
		dnsOut, err := client.RunCommand(name, []string{"/bin/sh", "-c",
			`tailscale status --json 2>/dev/null | grep -o '"DNSName":"[^"]*"' | head -1 | cut -d'"' -f4`})
		dnsName := strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(dnsOut), "."))
		if err == nil && dnsName != "" {
			t.Logf("tailscale DNS: %s", dnsName)

			// Verify HTTPS is reachable from inside the container itself.
			httpsDeadline := time.Now().Add(30 * time.Second)
			var httpsBody string
			for time.Now().Before(httpsDeadline) {
				out, err := client.RunCommand(name, []string{"/bin/sh", "-c",
					fmt.Sprintf("curl -s --max-time 5 https://%s/health 2>/dev/null", dnsName)})
				if err == nil && strings.Contains(out, `"ok"`) {
					httpsBody = strings.TrimSpace(out)
					break
				}
				time.Sleep(3 * time.Second)
			}
			if httpsBody != "" {
				t.Logf("HTTPS health: %s", httpsBody)
			} else {
				t.Logf("HTTPS self-check did not respond (may need MagicDNS within container)")
			}
		}

		// Cleanup: turn off serve.
		client.RunCommand(name, []string{"/bin/sh", "-c", "tailscale serve off"})
	}
}

// ── helpers ───────────────────────────────────────────────────────────────────

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}
