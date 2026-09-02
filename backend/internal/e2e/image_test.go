//go:build e2e

// Package e2e contains image-level tests that provision real Incus containers
// and verify the resulting environment.
//
// These tests talk directly to a live Incus server — no Plati API layer is
// involved. They are intentionally skipped when the required env vars are
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
//	cd backend && go test -v -timeout 30m -tags e2e ./internal/e2e/...
package e2e_test

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/homaserver/plati/internal/incus"
)

// ── constants ──────────────────────────────────────────────────────────────────

const (
	defaultEndpoint   = "https://127.0.0.1:8443"
	defaultTimeoutSec = 300
)

// ── YAML structs ───────────────────────────────────────────────────────────────

// templateYAML mirrors the minimal fields needed to provision a container.
type templateYAML struct {
	Name     string   `yaml:"name"`
	Image    string   `yaml:"image"`
	Profiles []string `yaml:"profiles"`
}

// fullTemplateYAML reads all fields from a template YAML file.
// Used by tests that also run first_init_commands or inspect repos/persistence.
type fullTemplateYAML struct {
	Name              string   `yaml:"name"`
	Slug              string   `yaml:"slug"`
	Image             string   `yaml:"image"`
	Profiles          []string `yaml:"profiles"`
	TerminalUser      string   `yaml:"terminal_user"`
	Includes          []string `yaml:"includes"`
	FirstInitCommands []string `yaml:"first_init_commands"`
	RebuildCommands   []string `yaml:"rebuild_commands"`
	Repos             []struct {
		Name string `yaml:"name"`
		Dest string `yaml:"dest"`
	} `yaml:"repos"`
}

// mixinYAML is the parsed form of a mixin YAML file in config/templates/mixins/.
type mixinYAML struct {
	Name               string   `yaml:"name"`
	PostCreateCommands []string `yaml:"post_create_commands"`
}

// ── file helpers ───────────────────────────────────────────────────────────────

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

// loadFullTemplate reads and parses the complete YAML for a template slug.
func loadFullTemplate(t *testing.T, slug string) fullTemplateYAML {
	t.Helper()
	path := filepath.Join(repoRoot(), "config", "templates", slug+".yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read template %s: %v", path, err)
	}
	var tmpl fullTemplateYAML
	if err := yaml.Unmarshal(data, &tmpl); err != nil {
		t.Fatalf("parse template %s: %v", path, err)
	}
	return tmpl
}

// repoTarget returns the name and destination path of the template's first repo —
// the path Plati copies it to, and the one the template's own rebuild_commands name.
func repoTarget(tmpl fullTemplateYAML) (name, dest string) {
	name = "AI-state-art-public"
	if len(tmpl.Repos) > 0 && tmpl.Repos[0].Name != "" {
		name = tmpl.Repos[0].Name
	}
	dest = "/home/ubuntu/" + name
	if len(tmpl.Repos) > 0 && tmpl.Repos[0].Dest != "" {
		dest = tmpl.Repos[0].Dest
	}
	return name, dest
}

// loadMixin reads and parses a mixin YAML from config/templates/mixins/<name>.yaml.
func loadMixin(t *testing.T, name string) mixinYAML {
	t.Helper()
	path := filepath.Join(repoRoot(), "config", "templates", "mixins", name+".yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read mixin %s: %v", path, err)
	}
	var m mixinYAML
	if err := yaml.Unmarshal(data, &m); err != nil {
		t.Fatalf("parse mixin %s: %v", path, err)
	}
	return m
}

// ── Incus client helpers ───────────────────────────────────────────────────────

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

// containerName returns a unique container name safe for Incus (alphanumeric + hyphens, max 63 chars).
func containerName(slug string) string {
	return fmt.Sprintf("plati-e2e-%s-%d", slug, time.Now().Unix())
}

// provision creates and starts a container, registering a cleanup that stops and deletes it.
func provision(t *testing.T, client *incus.Client, name string, tmpl templateYAML, envVars map[string]string) {
	t.Helper()

	cfg := map[string]string{}
	for k, v := range envVars {
		cfg["environment."+k] = v
	}

	t.Logf("creating container %s (image: %s, profiles: %v) …", name, tmpl.Image, tmpl.Profiles)
	if err := client.CreateInstance(name, tmpl.Image, tmpl.Profiles, cfg, nil); err != nil {
		if strings.Contains(err.Error(), "profile") && strings.Contains(err.Error(), "doesn't exist") {
			t.Skipf("required Incus profile not configured (run scripts/setup-*-profile.sh if needed): %v", err)
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

// waitReady polls until the container is responsive.
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
// Fails the test immediately on non-zero exit.
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

// runCmds runs a slice of shell commands in the container, logging each one.
// If failOnError is false, failures are logged but the test continues.
func runCmds(t *testing.T, client *incus.Client, name string, cmds []string, failOnError bool) {
	t.Helper()
	for _, cmd := range cmds {
		label := cmd
		if len(label) > 80 {
			label = label[:77] + "..."
		}
		t.Logf("exec: %s", label)
		out, err := client.RunCommand(name, []string{"/bin/sh", "-c", cmd})
		if err != nil {
			msg := fmt.Sprintf("exec %q: %v\noutput: %s", label, err, strings.TrimSpace(out))
			if failOnError {
				t.Fatal(msg)
			} else {
				t.Logf("WARN: %s", msg)
			}
		}
	}
}

// waitForService polls until a systemd service is active or the deadline is reached.
func waitForService(t *testing.T, client *incus.Client, name, service string, timeout time.Duration) bool {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		out, err := client.RunCommand(name, []string{"systemctl", "is-active", service})
		if err == nil && strings.TrimSpace(out) == "active" {
			return true
		}
		time.Sleep(5 * time.Second)
	}
	return false
}

// ── proxy helpers ─────────────────────────────────────────────────────────────

// startBridgeProxy starts a minimal HTTP/HTTPS proxy on the incusbr0 gateway
// address (10.70.69.1:3128) so containers can reach the internet when the
// host's nftables FORWARD chain blocks direct container egress.
// Returns the proxy URL ("http://10.70.69.1:3128"), or "" if unavailable.
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

// injectProxyViaExec configures apt, curl, and Docker proxy settings via exec.
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

// ── helpers ───────────────────────────────────────────────────────────────────

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

// ── Simple Web Server ─────────────────────────────────────────────────────────

// TestImage_SimpleWebServer verifies the simple-webserver template:
//   - webserver.service is active
//   - /health returns {"status":"ok"}
//   - root path returns HTML
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

	// The simple-webserver template runs first_init_commands via cloud-init equivalent
	// (exec steps in Plati). Here we manually run them so the e2e test is self-contained.
	fullTmpl := loadFullTemplate(t, "simple-webserver")
	if len(fullTmpl.FirstInitCommands) > 0 {
		// Set up proxy before package installation.
		if proxyURL := startBridgeProxy(t); proxyURL != "" {
			injectProxyViaExec(t, client, name, proxyURL)
		}
		t.Log("Running first_init_commands...")
		runCmds(t, client, name, fullTmpl.FirstInitCommands, false)
	}

	// webserver.service must be active.
	if !waitForService(t, client, name, "webserver", 120*time.Second) {
		t.Fatal("webserver service did not become active within 120s")
	}
	t.Logf("webserver: active")

	// Health endpoint must respond with 200 and JSON body.
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
}

// ── Docker-in-Docker ──────────────────────────────────────────────────────────

// TestImage_DockerInDocker verifies Docker-in-Docker capability using the
// ubuntu template's image and docker Incus profile.
//
// This test installs Docker via the docker mixin's post_create_commands, then
// verifies the daemon is active and can run a container serving HTTP.
//
// Requires the Incus 'docker' profile (run scripts/setup-docker-profile.sh).
func TestImage_DockerInDocker(t *testing.T) {
	client := newIncusClient(t)
	// Use the ubuntu template (image + profiles including docker).
	tmpl := loadTemplate(t, "ubuntu")
	name := containerName("dind")

	provision(t, client, name, tmpl, nil)
	waitReady(t, client, name)

	// Set up internet proxy before apt-get.
	proxyURL := startBridgeProxy(t)
	if proxyURL != "" {
		injectProxyViaExec(t, client, name, proxyURL)
	}

	// Run the docker mixin installation commands.
	dockerMixin := loadMixin(t, "docker")
	t.Log("Installing Docker via docker mixin commands...")
	runCmds(t, client, name, dockerMixin.PostCreateCommands, false)

	// Reload systemd and restart docker with the proxy config (if proxy was set up).
	if proxyURL != "" {
		_, _ = client.RunCommand(name, []string{"systemctl", "daemon-reload"})
		_, _ = client.RunCommand(name, []string{"systemctl", "restart", "docker"})
	}

	// Docker daemon must be active.
	if !waitForService(t, client, name, "docker", 60*time.Second) {
		t.Fatal("docker service did not become active within 60s")
	}
	t.Logf("docker: active")

	// Verify docker info.
	info := run(t, client, name, "docker", "info", "--format", "{{.ServerVersion}}")
	t.Logf("docker server version: %s", info)

	// Pull and run nginx (uses host network to bypass Docker's iptables DNAT inside Incus).
	run(t, client, name, "docker", "run", "-d",
		"--network", "host",
		"--name", "plati-e2e-nginx",
		"nginx:alpine",
	)
	t.Logf("nginx container started")
	time.Sleep(3 * time.Second)

	// Curl nginx directly on localhost (--noproxy bypasses the bridge proxy).
	out := runContains(t, client, name, "Welcome to nginx",
		"curl", "--noproxy", "*", "-s", "http://localhost:80")
	t.Logf("nginx HTTP response: %s", firstLine(out))

	// Clean up inner container.
	run(t, client, name, "docker", "rm", "-f", "plati-e2e-nginx")
	t.Logf("inner container cleaned up")

	// Verify docker ps is now empty.
	out = run(t, client, name, "docker", "ps", "-q")
	if out != "" {
		t.Errorf("expected no running containers after cleanup, got: %s", out)
	}
}

// ── QCM PoC Formation ─────────────────────────────────────────────────────────

// TestImage_QcmPocFormation verifies the qcm-poc-formation template:
//
//   - Container provisions from ubuntu/24.04/cloud
//   - git is available (pre-installed in Ubuntu cloud images)
//   - The repo destination directory can be created and used
//   - Git repository operations work (init, commit, clone, pull)
//   - The template's rebuild_command pattern (git pull --ff-only || true) succeeds
//   - SSH key injection is functional (authorized_keys written)
//
// The test simulates the Plati repo-copy flow locally: a bare repo is initialized
// inside the container and cloned to the template's own repo destination, so that
// the template's rebuild_commands (which name that path) succeed unmodified.
func TestImage_QcmPocFormation(t *testing.T) {
	client := newIncusClient(t)
	tmpl := loadTemplate(t, "qcm-poc-formation")
	fullTmpl := loadFullTemplate(t, "qcm-poc-formation")
	name := containerName("qcm")

	provision(t, client, name, tmpl, nil)
	waitReady(t, client, name)

	// ── 1. git is pre-installed ─────────────────────────────────────────────
	gitVer := runContains(t, client, name, "git version", "git", "--version")
	t.Logf("git: %s", gitVer)

	// ── 2. Repo destination setup ───────────────────────────────────────────
	// In production Plati attaches a named volume at the template's persistence
	// path and the sentinel check decides first-init vs rebuild. Here we take the
	// destination straight from the template and create its parent manually.
	repoName, repoDest := repoTarget(fullTmpl)
	repoBare := "/tmp/" + repoName + ".git"
	run(t, client, name, "mkdir", "-p", path.Dir(repoDest))
	t.Logf("%s: created", path.Dir(repoDest))

	// ── 3. SSH key injection (simulates Plati phase-2 setup) ────────────────
	// Write a throwaway test public key so authorized_keys is non-empty.
	const testPubKey = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAID/plati-e2e-test-key plati-e2e"
	run(t, client, name, "/bin/sh", "-c",
		fmt.Sprintf("mkdir -p /root/.ssh && chmod 700 /root/.ssh && echo '%s' > /root/.ssh/authorized_keys && chmod 600 /root/.ssh/authorized_keys", testPubKey))
	out := run(t, client, name, "cat", "/root/.ssh/authorized_keys")
	if !strings.Contains(out, "plati-e2e") {
		t.Errorf("authorized_keys not written correctly: %s", out)
	}
	t.Logf("SSH key injection: OK")

	// ── 4. Create a local bare repo (simulates the cached repo on the server) ─
	setupScript := strings.Join([]string{
		"git config --global user.email 'test@plati.dev'",
		"git config --global user.name 'Plati E2E Test'",
		"git init --bare " + repoBare,
		// Bootstrap: create a working clone, add a file, push to the bare repo.
		"git init /tmp/bootstrap-src",
		"git -C /tmp/bootstrap-src config user.email 'test@plati.dev'",
		"git -C /tmp/bootstrap-src config user.name 'Plati E2E Test'",
		"echo 'QCM PoC Formation' > /tmp/bootstrap-src/README.md",
		"git -C /tmp/bootstrap-src add README.md",
		"git -C /tmp/bootstrap-src commit -m 'Initial commit'",
		"git -C /tmp/bootstrap-src remote add origin " + repoBare,
		"git -C /tmp/bootstrap-src push -u origin HEAD:main",
	}, " && ")
	run(t, client, name, "/bin/sh", "-c", setupScript)
	t.Logf("bare repo initialized at %s", repoBare)

	// ── 5. Simulate Plati repo copy: cp → repo dest ─────────────────────────
	// In production: cp -rp /plati-repos/<name> <dest>
	// Here we clone the bare repo (same net effect — git-tracked directory at the dest path).
	run(t, client, name, "/bin/sh", "-c",
		fmt.Sprintf("git clone %s %s", repoBare, repoDest))
	run(t, client, name, "test", "-d", repoDest)
	t.Logf("%s: present after clone", repoDest)

	// Verify the README landed.
	readmeOut := run(t, client, name, "cat", repoDest+"/README.md")
	if !strings.Contains(readmeOut, "QCM") {
		t.Errorf("README.md content unexpected: %s", readmeOut)
	}
	t.Logf("README.md: %s", readmeOut)

	// ── 6. Simulate git remote set-url (Plati sets origin after copy) ────────
	run(t, client, name, "/bin/sh", "-c",
		fmt.Sprintf("git -C %s remote set-url origin %s", repoDest, repoBare))
	originURL := run(t, client, name, "/bin/sh", "-c",
		fmt.Sprintf("git -C %s remote get-url origin", repoDest))
	t.Logf("git remote origin: %s", originURL)

	// ── 7. Add a new commit to origin, then test git pull (rebuild_command) ──
	updateScript := strings.Join([]string{
		"echo 'Update v2' >> /tmp/bootstrap-src/README.md",
		"git -C /tmp/bootstrap-src add README.md",
		"git -C /tmp/bootstrap-src commit -m 'Update README'",
		"git -C /tmp/bootstrap-src push origin HEAD:main",
	}, " && ")
	run(t, client, name, "/bin/sh", "-c", updateScript)
	t.Logf("origin updated with a new commit")

	// Template rebuild_commands run verbatim: they name repoDest, which now exists.
	for _, rebuildCmd := range fullTmpl.RebuildCommands {
		t.Logf("rebuild_command: %s", rebuildCmd)
		out, err := client.RunCommand(name, []string{"/bin/sh", "-c", rebuildCmd})
		out = strings.TrimSpace(out)
		if err != nil {
			t.Errorf("rebuild_command failed: %v\noutput: %s", err, out)
		} else {
			t.Logf("rebuild_command output: %s", out)
		}
	}

	// Verify the update was pulled.
	readmeAfterPull := run(t, client, name, "cat", repoDest+"/README.md")
	if !strings.Contains(readmeAfterPull, "Update v2") {
		t.Errorf("git pull did not fetch the new commit; README: %s", readmeAfterPull)
	}
	t.Logf("git pull verified: Update v2 present")

	// ── 8. Validate template metadata ────────────────────────────────────────
	if fullTmpl.Slug != "qcm-poc-formation" {
		t.Errorf("unexpected slug: %s", fullTmpl.Slug)
	}
	if len(fullTmpl.Repos) == 0 {
		t.Error("template has no repos configured — expected at least one")
	} else {
		t.Logf("template repos: %+v", fullTmpl.Repos)
	}
}

// ── Site IA Gen ───────────────────────────────────────────────────────────────

// TestImage_SiteIAGen verifies the site-ia-gen template:
//
//   - Container provisions from ubuntu/24.04/cloud
//   - git is available
//   - The repo destination directory works for git operations
//   - rebuild_commands (git pull) run successfully after initial clone
//
// Uses the same bare-repo pattern as TestImage_QcmPocFormation since both
// templates share the same image, persistence model, and repo-copy approach.
func TestImage_SiteIAGen(t *testing.T) {
	client := newIncusClient(t)
	tmpl := loadTemplate(t, "site-ia-gen")
	fullTmpl := loadFullTemplate(t, "site-ia-gen")
	name := containerName("site-ia-gen")

	provision(t, client, name, tmpl, nil)
	waitReady(t, client, name)

	// ── 1. git is available ─────────────────────────────────────────────────
	gitVer := runContains(t, client, name, "git version", "git", "--version")
	t.Logf("git: %s", gitVer)

	// ── 2. Repo destination setup ───────────────────────────────────────────
	repoName, repoWorkPath := repoTarget(fullTmpl)
	run(t, client, name, "mkdir", "-p", path.Dir(repoWorkPath))

	// ── 3. Bootstrap bare repo + clone (simulates Plati repo copy) ──────────
	repoBarePath := "/tmp/" + repoName + ".git"

	setupScript := strings.Join([]string{
		"git config --global user.email 'test@plati.dev'",
		"git config --global user.name 'Plati E2E Test'",
		"git init --bare " + repoBarePath,
		"git init /tmp/site-bootstrap",
		"git -C /tmp/site-bootstrap config user.email 'test@plati.dev'",
		"git -C /tmp/site-bootstrap config user.name 'Plati E2E Test'",
		"echo '# AI State of the Art' > /tmp/site-bootstrap/README.md",
		"git -C /tmp/site-bootstrap add README.md",
		"git -C /tmp/site-bootstrap commit -m 'Initial commit'",
		"git -C /tmp/site-bootstrap remote add origin " + repoBarePath,
		"git -C /tmp/site-bootstrap push -u origin HEAD:main",
		"git clone " + repoBarePath + " " + repoWorkPath,
	}, " && ")
	run(t, client, name, "/bin/sh", "-c", setupScript)
	run(t, client, name, "test", "-d", repoWorkPath)
	t.Logf("%s: cloned to %s", repoName, repoWorkPath)

	// Set the origin URL (as Plati would after cp).
	run(t, client, name, "/bin/sh", "-c",
		fmt.Sprintf("git -C %s remote set-url origin %s", repoWorkPath, repoBarePath))
	t.Logf("git remote origin set to %s", repoBarePath)

	// ── 4. Push an update and run rebuild_commands ──────────────────────────
	updateScript := strings.Join([]string{
		"echo 'v2 content' >> /tmp/site-bootstrap/README.md",
		"git -C /tmp/site-bootstrap add README.md",
		"git -C /tmp/site-bootstrap commit -m 'Add v2 content'",
		"git -C /tmp/site-bootstrap push origin HEAD:main",
	}, " && ")
	run(t, client, name, "/bin/sh", "-c", updateScript)

	rebuildCmds := fullTmpl.RebuildCommands
	if len(rebuildCmds) == 0 {
		t.Log("no rebuild_commands in template — skipping git pull step")
	}
	for _, cmd := range rebuildCmds {
		// No rewriting needed: the clone above went to the very path the template names.
		t.Logf("rebuild_command: %s", cmd)
		out, err := client.RunCommand(name, []string{"/bin/sh", "-c", cmd})
		if err != nil {
			t.Errorf("rebuild_command failed: %v\noutput: %s", err, strings.TrimSpace(out))
		} else {
			t.Logf("output: %s", strings.TrimSpace(out))
		}
	}

	// Verify pull got v2 content.
	readme := run(t, client, name, "cat", repoWorkPath+"/README.md")
	if !strings.Contains(readme, "v2 content") {
		t.Errorf("git pull did not fetch v2 content; README: %s", readme)
	}
	t.Logf("git pull verified: v2 content present")

	// ── 5. Validate template metadata ────────────────────────────────────────
	if fullTmpl.Slug != "site-ia-gen" {
		t.Errorf("unexpected slug: %s", fullTmpl.Slug)
	}
	if len(fullTmpl.Repos) == 0 {
		t.Error("template has no repos configured — expected at least one")
	} else {
		t.Logf("template repos: %+v", fullTmpl.Repos)
	}
}
