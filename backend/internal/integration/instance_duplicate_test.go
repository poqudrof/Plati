// Copying an instance, end to end.
//
// The promise the feature makes is: the copy is the same workspace under a new name —
// same template, same image, same profiles, same resource limits, same auto-stop policy,
// and the same files in every persistent volume — but it belongs to whoever it was given
// to. What must NOT come across is the source owner's identity: their SSH keys, their
// decrypted secrets and their tailnet hostname. The new owner installs their own key and
// their own Tailscale key afterwards and works in it, which is what the last tests here
// walk through.
package integration_test

import (
	"bytes"
	"crypto/ed25519"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"

	"github.com/homaserver/plati/internal/models"
	"github.com/homaserver/plati/internal/services"
)

// ── Fixtures ─────────────────────────────────────────────────────────────────

// persistDir is one entry of a template's persistence.directories block.
type persistDir struct {
	path string
	size string
}

// seedPersistentTemplate imports a template with an explicit persistence block, so a
// copy has more than one volume to carry over. seedTemplate declares none and falls back
// to the implicit single home volume, which hides any per-path mix-up.
func seedPersistentTemplate(t *testing.T, h *harness, slug string, dirs ...persistDir) int64 {
	t.Helper()

	directories := make([]map[string]string, len(dirs))
	for i, d := range dirs {
		directories[i] = map[string]string{"path": d.path, "size": d.size, "pool": "default"}
	}
	return importTemplate(t, h, map[string]any{
		"name":          slug,
		"slug":          slug,
		"description":   "duplicate test",
		"image":         "images:ubuntu/24.04/cloud",
		"profiles":      []string{"default"},
		"resources":     map[string]string{"cpu": "2", "memory": "4GB", "disk": "20GB"},
		"terminal_user": "ubuntu",
		"incus_config":  map[string]string{"security.nesting": "true"},
		"persistence":   map[string]any{"mode": "normal", "directories": directories},
	})
}

// seedEphemeralTemplate imports a template with no persistent storage at all.
func seedEphemeralTemplate(t *testing.T, h *harness, slug string) int64 {
	t.Helper()
	return importTemplate(t, h, map[string]any{
		"name":          slug,
		"slug":          slug,
		"description":   "duplicate test (ephemeral)",
		"image":         "images:ubuntu/24.04/cloud",
		"profiles":      []string{"default"},
		"resources":     map[string]string{"cpu": "2", "memory": "4GB", "disk": "20GB"},
		"terminal_user": "ubuntu",
		"persistence":   map[string]any{"mode": "ephemeral"},
	})
}

// importTemplate posts a template through the admin import route. The endpoint parses
// YAML, and JSON is valid YAML, so the map marshals straight into it.
func importTemplate(t *testing.T, h *harness, body map[string]any) int64 {
	t.Helper()
	resp := h.do("POST", "/api/v1/admin/templates/import", body)
	if resp.StatusCode != http.StatusCreated {
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		t.Fatalf("import template: %d — %s", resp.StatusCode, b)
	}
	var tmpl models.Template
	mustJSON(t, resp, &tmpl)
	return tmpl.ID
}

// session is one account's own cookie jar. Logging a second user in through h.client
// would replace the admin session every fixture is built with.
type session struct {
	t      *testing.T
	h      *harness
	client *http.Client
	user   models.User
}

// newUserSession creates a regular account and logs it in.
func newUserSession(t *testing.T, h *harness, email, name, password string) *session {
	t.Helper()

	resp := h.do("POST", "/api/v1/admin/users", map[string]any{
		"email": email, "name": name, "password": password, "role": "user",
	})
	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		t.Fatalf("create user %s: %d — %s", email, resp.StatusCode, b)
	}
	var user models.User
	mustJSON(t, resp, &user)

	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar}
	body, _ := json.Marshal(map[string]string{"email": email, "password": password})
	login, err := client.Post(h.srv.URL+"/auth/login", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("login %s: %v", email, err)
	}
	login.Body.Close()
	if login.StatusCode != http.StatusOK {
		t.Fatalf("login %s: %d", email, login.StatusCode)
	}
	return &session{t: t, h: h, client: client, user: user}
}

func (s *session) do(method, path string, body any) *http.Response {
	s.t.Helper()
	var r io.Reader
	if body != nil {
		data, _ := json.Marshal(body)
		r = bytes.NewReader(data)
	}
	req, _ := http.NewRequest(method, s.h.srv.URL+path, r)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := s.client.Do(req)
	if err != nil {
		s.t.Fatalf("%s %s: %v", method, path, err)
	}
	return resp
}

// createInstance creates an instance and waits for the async creation to finish, so the
// volumes exist before anything tries to copy them.
func createInstance(t *testing.T, h *harness, do func(string, string, any) *http.Response, name string, templateID int64) models.InstanceJSON {
	t.Helper()
	resp := do("POST", "/api/v1/instances", map[string]any{"name": name, "template_id": templateID})
	if resp.StatusCode != http.StatusCreated {
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		t.Fatalf("create %q: %d — %s", name, resp.StatusCode, b)
	}
	var inst models.InstanceJSON
	mustJSON(t, resp, &inst)
	waitForInstanceReady(t, h, inst.ID)
	return inst
}

// duplicate copies an instance through the owner's own route.
func duplicate(t *testing.T, do func(string, string, any) *http.Response, srcID int64) models.InstanceJSON {
	t.Helper()
	resp := do("POST", fmt.Sprintf("/api/v1/instances/%d/duplicate", srcID), nil)
	if resp.StatusCode != http.StatusCreated {
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		t.Fatalf("duplicate %d: %d — %s", srcID, resp.StatusCode, b)
	}
	var dst models.InstanceJSON
	mustJSON(t, resp, &dst)
	return dst
}

// tarStreams makes every volume read hand back a payload naming the instance and the
// directory it came from, so a copy that writes the right bytes to the wrong path — or
// pipes nothing at all into tar — is visible.
func tarStreams(h *harness) {
	h.mock.streamCommandFn = func(name string, command []string) (io.ReadCloser, error) {
		return io.NopCloser(strings.NewReader("TAR(" + name + ":" + tarDir(command) + ")")), nil
	}
}

// tarDir returns the argument of tar's -C flag.
func tarDir(command []string) string {
	for i, arg := range command {
		if arg == "-C" && i+1 < len(command) {
			return command[i+1]
		}
	}
	return ""
}

// newPublicKey returns a fresh, genuinely valid authorized_keys line. The API validates
// keys with ssh.ParseAuthorizedKey, so a made-up string is rejected.
func newPublicKey(t *testing.T, comment string) string {
	t.Helper()
	pub, _, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	sshPub, err := ssh.NewPublicKey(pub)
	if err != nil {
		t.Fatalf("wrap key: %v", err)
	}
	return strings.TrimSpace(string(ssh.MarshalAuthorizedKey(sshPub))) + " " + comment
}

// volumesOf returns the instance's attached volumes, keyed by mount path.
func volumesOf(t *testing.T, do func(string, string, any) *http.Response, instanceID int64) map[string]models.VolumeDetail {
	t.Helper()
	resp := do("GET", fmt.Sprintf("/api/v1/instances/%d/volumes", instanceID), nil)
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		t.Fatalf("volumes of %d: %d — %s", instanceID, resp.StatusCode, b)
	}
	var info services.InstanceStorageInfo
	mustJSON(t, resp, &info)
	out := make(map[string]models.VolumeDetail, len(info.Volumes))
	for _, v := range info.Volumes {
		out[v.MountPath] = v
	}
	return out
}

// ── The copy is a new, independent instance ──────────────────────────────────

func TestDuplicate_CopyIsAnIndependentInstanceWithANewName(t *testing.T) {
	h := newHarness(t)
	defer h.teardown()
	h.do("POST", "/auth/login", map[string]string{"password": testPassword}).Body.Close()
	tarStreams(h)

	templateID := seedPersistentTemplate(t, h, "two-volumes",
		persistDir{"/home/ubuntu", "20GB"}, persistDir{"/data", "10GB"})
	src := createInstance(t, h, h.do, "src-box", templateID)

	dst := duplicate(t, h.do, src.ID)

	if dst.ID == src.ID {
		t.Fatal("duplicate returned the source instance itself")
	}
	if dst.Name != "src-box-copy" {
		t.Errorf("copy name = %q, want %q", dst.Name, "src-box-copy")
	}
	if dst.IncusName == src.IncusName {
		t.Fatalf("copy reuses the source container name %q", dst.IncusName)
	}
	if want := fmt.Sprintf("plati-%d-src-box-copy", dst.UserID); dst.IncusName != want {
		t.Errorf("copy incus_name = %q, want %q", dst.IncusName, want)
	}
	if dst.TemplateID != src.TemplateID {
		t.Errorf("copy template = %d, want %d", dst.TemplateID, src.TemplateID)
	}
	if dst.ServerID != src.ServerID {
		t.Errorf("copy server = %d, want %d", dst.ServerID, src.ServerID)
	}
	if dst.Status != "running" {
		t.Errorf("copy status = %q, want running", dst.Status)
	}

	// Both containers exist side by side: the copy is not a rename of the source.
	if _, ok := h.mock.instances[dst.IncusName]; !ok {
		t.Errorf("no container %q was created for the copy", dst.IncusName)
	}
	if _, ok := h.mock.instances[src.IncusName]; !ok {
		t.Errorf("the source container %q disappeared", src.IncusName)
	}

	// Same storage layout, different volumes. Sharing a volume with the source would
	// mean two containers writing the same disk.
	srcVols := volumesOf(t, h.do, src.ID)
	dstVols := volumesOf(t, h.do, dst.ID)
	if len(dstVols) != len(srcVols) {
		t.Fatalf("copy has %d volume(s), source has %d", len(dstVols), len(srcVols))
	}
	for path, sv := range srcVols {
		dv, ok := dstVols[path]
		if !ok {
			t.Errorf("copy has no volume mounted at %s", path)
			continue
		}
		if dv.VolumeID == sv.VolumeID || dv.VolumeName == sv.VolumeName {
			t.Errorf("copy shares the source volume at %s (%q)", path, sv.VolumeName)
		}
		if dv.DeviceName != sv.DeviceName {
			t.Errorf("device at %s = %q, want %q", path, dv.DeviceName, sv.DeviceName)
		}
		if dv.SizeGB != sv.SizeGB {
			t.Errorf("size at %s = %dGB, want %dGB", path, dv.SizeGB, sv.SizeGB)
		}
	}
}

// ── The filesystem comes across ──────────────────────────────────────────────

func TestDuplicate_CopiesTheFilesystemOfEveryVolume(t *testing.T) {
	h := newHarness(t)
	defer h.teardown()
	h.do("POST", "/auth/login", map[string]string{"password": testPassword}).Body.Close()
	tarStreams(h)

	templateID := seedPersistentTemplate(t, h, "two-volumes",
		persistDir{"/home/ubuntu", "20GB"}, persistDir{"/data", "10GB"})
	src := createInstance(t, h, h.do, "full-box", templateID)

	dst := duplicate(t, h.do, src.ID)

	// Every mount path of the source must have been read from the source and written
	// into the copy at the same path, with the bytes actually piped through.
	for _, path := range []string{"/home/ubuntu", "/data"} {
		want := "TAR(" + src.IncusName + ":" + path + ")"
		found := false
		for _, call := range h.mock.execCallsFor(dst.IncusName) {
			if tarDir(call.command) != path || !strings.Contains(strings.Join(call.command, " "), "-xf") {
				continue
			}
			found = true
			if call.stdin != want {
				t.Errorf("%s of the copy received %q, want %q", path, call.stdin, want)
			}
		}
		if !found {
			t.Errorf("nothing was extracted into %s of the copy; exec calls: %v",
				path, h.mock.execCallsFor(dst.IncusName))
		}
	}

	// And nothing was written back into the source.
	for _, call := range h.mock.execCallsFor(src.IncusName) {
		if strings.Contains(strings.Join(call.command, " "), "-xf") {
			t.Errorf("the duplicate wrote into the source: %v", call.command)
		}
	}
}

// ── System settings: the copy must boot the same ─────────────────────────────

func TestDuplicate_CopyBootsFromTheSameImageProfilesAndConfig(t *testing.T) {
	h := newHarness(t)
	defer h.teardown()
	h.do("POST", "/auth/login", map[string]string{"password": testPassword}).Body.Close()
	tarStreams(h)

	templateID := seedPersistentTemplate(t, h, "configured", persistDir{"/home/ubuntu", "20GB"})
	src := createInstance(t, h, h.do, "boot-box", templateID)

	dst := duplicate(t, h.do, src.ID)

	srcC := h.mock.instances[src.IncusName]
	dstC := h.mock.instances[dst.IncusName]
	if srcC == nil || dstC == nil {
		t.Fatalf("missing container: src=%v dst=%v", srcC != nil, dstC != nil)
	}

	if h.mock.instanceImages[dst.IncusName] != h.mock.instanceImages[src.IncusName] {
		t.Errorf("copy image = %q, want %q",
			h.mock.instanceImages[dst.IncusName], h.mock.instanceImages[src.IncusName])
	}
	if strings.Join(dstC.Profiles, ",") != strings.Join(srcC.Profiles, ",") {
		t.Errorf("copy profiles = %v, want %v", dstC.Profiles, srcC.Profiles)
	}

	// Every config key must match, apart from the tailnet hostname — which is derived
	// from the name and therefore has to differ, or both machines would claim the same
	// name on the tailnet.
	const hostnameKey = "environment.PLATI_TAILSCALE_HOSTNAME"
	for k, v := range srcC.Config {
		if k == hostnameKey {
			continue
		}
		if got := dstC.Config[k]; got != v {
			t.Errorf("copy config %s = %q, want %q", k, got, v)
		}
	}
	for k := range dstC.Config {
		if _, ok := srcC.Config[k]; !ok {
			t.Errorf("copy has an extra config key %s = %q", k, dstC.Config[k])
		}
	}
	if got := dstC.Config[hostnameKey]; got != "boot-box-copy" {
		t.Errorf("copy tailnet hostname = %q, want %q", got, "boot-box-copy")
	}
}

func TestDuplicate_CopiesResourceLimits(t *testing.T) {
	h := newHarness(t)
	defer h.teardown()
	h.do("POST", "/auth/login", map[string]string{"password": testPassword}).Body.Close()
	tarStreams(h)

	templateID := seedPersistentTemplate(t, h, "limited", persistDir{"/home/ubuntu", "20GB"})
	src := createInstance(t, h, h.do, "big-box", templateID)

	// An admin raises this one instance above its template's 2 CPU / 4GB.
	resp := h.do("PUT", fmt.Sprintf("/api/v1/admin/instances/%d/resources", src.ID),
		map[string]any{"limits_cpu": "8", "limits_memory": "16GB"})
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		t.Fatalf("set resources: %d — %s", resp.StatusCode, b)
	}
	resp.Body.Close()

	dst := duplicate(t, h.do, src.ID)

	// The override is stored on the copy's row, so a later rebuild keeps it.
	if dst.LimitsCPU != "8" || dst.LimitsMemory != "16GB" {
		t.Errorf("copy override = %q/%q, want 8/16GB", dst.LimitsCPU, dst.LimitsMemory)
	}

	var got services.InstanceResources
	mustJSON(t, h.do("GET", fmt.Sprintf("/api/v1/instances/%d/resources", dst.ID), nil), &got)
	if got.EffectiveCPU != "8" || got.EffectiveMemory != "16GB" {
		t.Errorf("copy effective limits = %s/%s, want 8/16GB", got.EffectiveCPU, got.EffectiveMemory)
	}

	// And the container really came up with them: a copy that boots on the template's
	// 2 CPU is not the same workspace.
	c := h.mock.instances[dst.IncusName]
	if c == nil {
		t.Fatalf("no container %q", dst.IncusName)
	}
	if c.Config["limits.cpu"] != "8" || c.Config["limits.memory"] != "16GB" {
		t.Errorf("copy container limits = %q/%q, want 8/16GB",
			c.Config["limits.cpu"], c.Config["limits.memory"])
	}
}

func TestDuplicate_CopiesAutoStopPolicy(t *testing.T) {
	h := newHarness(t)
	defer h.teardown()
	h.do("POST", "/auth/login", map[string]string{"password": testPassword}).Body.Close()
	tarStreams(h)

	templateID := seedPersistentTemplate(t, h, "sleepy", persistDir{"/home/ubuntu", "20GB"})
	src := createInstance(t, h, h.do, "long-runner", templateID)

	resp := h.do("PUT", fmt.Sprintf("/api/v1/instances/%d/sleep", src.ID),
		map[string]any{"disabled": true, "timeout_minutes": 90})
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		t.Fatalf("set sleep policy: %d — %s", resp.StatusCode, b)
	}
	resp.Body.Close()

	dst := duplicate(t, h.do, src.ID)

	if !dst.SleepDisabled {
		t.Error("the copy is back in the auto-stop sweep: it will be stopped under the owner")
	}
	if dst.SleepTimeoutMinutes != 90 {
		t.Errorf("copy timeout = %d minutes, want 90", dst.SleepTimeoutMinutes)
	}

	var settings services.SleepSettings
	mustJSON(t, h.do("GET", fmt.Sprintf("/api/v1/instances/%d/sleep", dst.ID), nil), &settings)
	if !settings.Disabled || settings.TimeoutMinutes != 90 {
		t.Errorf("copy sleep settings = disabled:%v timeout:%d, want true/90",
			settings.Disabled, settings.TimeoutMinutes)
	}
}

// ── The copy carries the new owner's identity, not the source owner's ────────

func TestDuplicate_CopyCarriesTheTargetOwnersKeysAndSecrets(t *testing.T) {
	h := newHarness(t)
	defer h.teardown()
	h.do("POST", "/auth/login", map[string]string{"password": testPassword}).Body.Close()
	tarStreams(h)

	alice := newUserSession(t, h, "alice@plati.local", "Alice", "alice-password")
	bob := newUserSession(t, h, "bob@plati.local", "Bob", "bob-password")

	aliceKey := newPublicKey(t, "alice@laptop")
	bobKey := newPublicKey(t, "bob@laptop")
	for _, c := range []struct {
		s   *session
		key string
	}{{alice, aliceKey}, {bob, bobKey}} {
		resp := c.s.do("POST", "/api/v1/ssh-keys", map[string]any{"name": "laptop", "public_key": c.key})
		if resp.StatusCode != http.StatusCreated {
			b, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			t.Fatalf("add public key: %d — %s", resp.StatusCode, b)
		}
		resp.Body.Close()
	}
	for _, c := range []struct {
		s     *session
		name  string
		value string
	}{{alice, "ALICE_TOKEN", "alice-only"}, {bob, "BOB_TOKEN", "bob-only"}} {
		resp := c.s.do("POST", "/api/v1/secrets", map[string]any{"name": c.name, "value": c.value})
		if resp.StatusCode != http.StatusCreated {
			b, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			t.Fatalf("add secret: %d — %s", resp.StatusCode, b)
		}
		resp.Body.Close()
	}

	templateID := seedPersistentTemplate(t, h, "shared", persistDir{"/home/ubuntu", "20GB"})
	src := createInstance(t, h, alice.do, "alice-box", templateID)

	// The admin hands a copy of Alice's workspace to Bob.
	resp := h.do("POST", fmt.Sprintf("/api/v1/admin/instances/%d/duplicate", src.ID),
		map[string]any{"user_id": bob.user.ID})
	if resp.StatusCode != http.StatusCreated {
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		t.Fatalf("admin duplicate: %d — %s", resp.StatusCode, b)
	}
	var dst models.InstanceJSON
	mustJSON(t, resp, &dst)

	if dst.UserID != bob.user.ID {
		t.Fatalf("copy owner = %d, want Bob (%d)", dst.UserID, bob.user.ID)
	}

	c := h.mock.instances[dst.IncusName]
	if c == nil {
		t.Fatalf("no container %q", dst.IncusName)
	}
	if got := c.Config["environment.BOB_TOKEN"]; got != "bob-only" {
		t.Errorf("copy is missing Bob's secret: BOB_TOKEN = %q", got)
	}
	if got, ok := c.Config["environment.ALICE_TOKEN"]; ok {
		t.Errorf("the copy leaked Alice's secret to Bob: ALICE_TOKEN = %q", got)
	}
	if got := c.Config["environment.PLATI_TAILSCALE_HOSTNAME"]; got != "alice-box-copy" {
		t.Errorf("copy tailnet hostname = %q, want alice-box-copy", got)
	}

	// Same for the keys written into the container: Bob must be able to log in, Alice
	// must not keep access to a machine that is no longer hers.
	authorized, ok := h.mock.pushedFor(dst.IncusName, "/root/.ssh/authorized_keys")
	if !ok {
		t.Fatal("no authorized_keys was written into the copy")
	}
	if !strings.Contains(string(authorized), bobKey) {
		t.Errorf("Bob's key is not in the copy's authorized_keys:\n%s", authorized)
	}
	if strings.Contains(string(authorized), aliceKey) {
		t.Errorf("Alice's key was copied into Bob's instance:\n%s", authorized)
	}
}

// The user route has no target picker, so an admin using it on someone else's instance
// gives that user a second copy rather than taking one for themselves.
func TestDuplicate_AdminOnTheUserRouteKeepsTheCopyWithTheOwner(t *testing.T) {
	h := newHarness(t)
	defer h.teardown()
	h.do("POST", "/auth/login", map[string]string{"password": testPassword}).Body.Close()
	tarStreams(h)

	alice := newUserSession(t, h, "alice@plati.local", "Alice", "alice-password")
	templateID := seedPersistentTemplate(t, h, "shared", persistDir{"/home/ubuntu", "20GB"})
	src := createInstance(t, h, alice.do, "alice-box", templateID)

	dst := duplicate(t, h.do, src.ID) // h.do is the admin session

	if dst.UserID != alice.user.ID {
		t.Errorf("copy owner = %d, want Alice (%d)", dst.UserID, alice.user.ID)
	}
}

// ── Working in the copy afterwards ───────────────────────────────────────────

func TestDuplicate_OwnerInstallsTheirSSHKeyIntoTheCopy(t *testing.T) {
	h := newHarness(t)
	defer h.teardown()
	h.do("POST", "/auth/login", map[string]string{"password": testPassword}).Body.Close()
	tarStreams(h)

	alice := newUserSession(t, h, "alice@plati.local", "Alice", "alice-password")
	templateID := seedPersistentTemplate(t, h, "workable", persistDir{"/home/ubuntu", "20GB"})
	src := createInstance(t, h, alice.do, "alice-box", templateID)
	dst := duplicate(t, alice.do, src.ID)

	// A key registered after the copy was made — the usual case: you copy a workspace,
	// then add the machine you are actually sitting at.
	newKey := newPublicKey(t, "alice@desktop")
	resp := alice.do("POST", "/api/v1/ssh-keys", map[string]any{"name": "desktop", "public_key": newKey})
	if resp.StatusCode != http.StatusCreated {
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		t.Fatalf("add public key: %d — %s", resp.StatusCode, b)
	}
	var key models.SSHKey
	mustJSON(t, resp, &key)

	// The copy offers it, not yet installed.
	var offered []services.InstanceAuthorizedKey
	mustJSON(t, alice.do("GET", fmt.Sprintf("/api/v1/instances/%d/authorized-keys", dst.ID), nil), &offered)
	found := false
	for _, k := range offered {
		if k.ID == key.ID {
			found = true
		}
	}
	if !found {
		t.Fatalf("the copy does not offer the owner's new key: %+v", offered)
	}

	resp = alice.do("POST", fmt.Sprintf("/api/v1/instances/%d/authorized-keys", dst.ID),
		map[string]any{"key_id": key.ID})
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		t.Fatalf("install key into the copy: %d — %s", resp.StatusCode, b)
	}
	resp.Body.Close()

	// The install runs inside the copy, and nowhere else.
	if !h.mock.hasRunCall(dst.IncusName, "add_key /root/.ssh") {
		t.Errorf("no authorized_keys install ran in the copy; calls: %v", h.mock.runCallsFor(dst.IncusName))
	}
	if h.mock.hasRunCall(src.IncusName, "add_key /root/.ssh") {
		t.Error("the key was installed into the source instead of the copy")
	}
}

func TestDuplicate_OwnerUsesTheirOwnTailscaleKeyInTheCopy(t *testing.T) {
	h := newHarness(t)
	defer h.teardown()
	h.do("POST", "/auth/login", map[string]string{"password": testPassword}).Body.Close()
	tarStreams(h)

	// The platform key is what a copy gets by default.
	resp := h.do("PUT", "/api/v1/admin/settings/tailscale-key", map[string]any{"value": "tskey-platform"})
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		t.Fatalf("set platform tailscale key: %d — %s", resp.StatusCode, b)
	}
	resp.Body.Close()

	alice := newUserSession(t, h, "alice@plati.local", "Alice", "alice-password")
	templateID := seedPersistentTemplate(t, h, "workable", persistDir{"/home/ubuntu", "20GB"})
	src := createInstance(t, h, alice.do, "alice-box", templateID)
	dst := duplicate(t, alice.do, src.ID)

	if got := h.mock.instances[dst.IncusName].Config["environment.TAILSCALE_AUTH_KEY"]; got != "tskey-platform" {
		t.Errorf("copy TAILSCALE_AUTH_KEY = %q, want the platform key", got)
	}

	// Alice switches the copy to her own key and rebuilds it.
	for _, step := range []struct {
		method, path string
		body         any
	}{
		{"POST", "/api/v1/secrets", map[string]any{"name": "TAILSCALE_AUTH_KEY", "value": "tskey-alice"}},
		{"PUT", "/api/v1/preferences", map[string]any{"ssh_key_mode": "plati", "tailscale_mode": "personal"}},
		{"POST", fmt.Sprintf("/api/v1/instances/%d/rebuild", dst.ID), nil},
	} {
		r := alice.do(step.method, step.path, step.body)
		if r.StatusCode != http.StatusOK && r.StatusCode != http.StatusCreated {
			b, _ := io.ReadAll(r.Body)
			r.Body.Close()
			t.Fatalf("%s %s: %d — %s", step.method, step.path, r.StatusCode, b)
		}
		r.Body.Close()
	}

	c := h.mock.instances[dst.IncusName]
	if c == nil {
		t.Fatalf("no container %q after rebuild", dst.IncusName)
	}
	if got := c.Config["environment.TAILSCALE_AUTH_KEY"]; got != "tskey-alice" {
		t.Errorf("after the rebuild TAILSCALE_AUTH_KEY = %q, want tskey-alice", got)
	}
	// The copy keeps its own tailnet name, so it does not fight the source for it.
	if got := c.Config["environment.PLATI_TAILSCALE_HOSTNAME"]; got != "alice-box-copy" {
		t.Errorf("copy tailnet hostname = %q, want alice-box-copy", got)
	}
	if srcC := h.mock.instances[src.IncusName]; srcC != nil {
		if got := srcC.Config["environment.TAILSCALE_AUTH_KEY"]; got != "tskey-platform" {
			t.Errorf("rebuilding the copy changed the source: TAILSCALE_AUTH_KEY = %q", got)
		}
	}
}

// ── Edge cases ───────────────────────────────────────────────────────────────

func TestDuplicate_EphemeralTemplateHasNoFilesystemToCopy(t *testing.T) {
	h := newHarness(t)
	defer h.teardown()
	h.do("POST", "/auth/login", map[string]string{"password": testPassword}).Body.Close()
	tarStreams(h)

	templateID := seedEphemeralTemplate(t, h, "throwaway")
	src := createInstance(t, h, h.do, "temp-box", templateID)

	dst := duplicate(t, h.do, src.ID)

	if len(volumesOf(t, h.do, dst.ID)) != 0 {
		t.Error("an ephemeral copy was given persistent volumes")
	}
	for _, call := range h.mock.execCallsFor(dst.IncusName) {
		if strings.Contains(strings.Join(call.command, " "), "tar") {
			t.Errorf("an ephemeral copy tried to restore a filesystem: %v", call.command)
		}
	}
	if _, ok := h.mock.instances[dst.IncusName]; !ok {
		t.Errorf("no container %q was created for the ephemeral copy", dst.IncusName)
	}
}

func TestDuplicate_SecondCopyOfTheSameInstanceGetsItsOwnName(t *testing.T) {
	h := newHarness(t)
	defer h.teardown()
	h.do("POST", "/auth/login", map[string]string{"password": testPassword}).Body.Close()
	tarStreams(h)

	templateID := seedPersistentTemplate(t, h, "twice", persistDir{"/home/ubuntu", "20GB"})
	src := createInstance(t, h, h.do, "src-box", templateID)

	first := duplicate(t, h.do, src.ID)
	second := duplicate(t, h.do, src.ID)

	if second.Name == first.Name {
		t.Fatalf("both copies are named %q", first.Name)
	}
	if second.Name != "src-box-copy-2" {
		t.Errorf("second copy name = %q, want src-box-copy-2", second.Name)
	}
	if second.IncusName == first.IncusName {
		t.Errorf("both copies use the container name %q", first.IncusName)
	}
}

func TestDuplicate_RefusesAnInstanceTheCallerCannotReach(t *testing.T) {
	h := newHarness(t)
	defer h.teardown()
	h.do("POST", "/auth/login", map[string]string{"password": testPassword}).Body.Close()
	tarStreams(h)

	alice := newUserSession(t, h, "alice@plati.local", "Alice", "alice-password")
	mallory := newUserSession(t, h, "mallory@plati.local", "Mallory", "mallory-password")

	templateID := seedPersistentTemplate(t, h, "private", persistDir{"/home/ubuntu", "20GB"})
	src := createInstance(t, h, alice.do, "alice-box", templateID)

	resp := mallory.do("POST", fmt.Sprintf("/api/v1/instances/%d/duplicate", src.ID), nil)
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode == http.StatusCreated {
		t.Fatalf("a stranger copied Alice's instance: %s", body)
	}

	// Nothing was created for them, and the admin route to another user's account is
	// still the only way to hand a copy over.
	var mine []models.InstanceJSON
	mustJSON(t, mallory.do("GET", "/api/v1/instances", nil), &mine)
	if len(mine) != 0 {
		t.Errorf("the refused copy still landed in Mallory's account: %+v", mine)
	}
}

func TestDuplicate_AdminCopyToAnUnknownUserCreatesNothing(t *testing.T) {
	h := newHarness(t)
	defer h.teardown()
	h.do("POST", "/auth/login", map[string]string{"password": testPassword}).Body.Close()
	tarStreams(h)

	templateID := seedPersistentTemplate(t, h, "private", persistDir{"/home/ubuntu", "20GB"})
	src := createInstance(t, h, h.do, "src-box", templateID)

	before := len(h.mock.instances)
	resp := h.do("POST", fmt.Sprintf("/api/v1/admin/instances/%d/duplicate", src.ID),
		map[string]any{"user_id": 9999})
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode == http.StatusCreated {
		t.Fatalf("a copy was assigned to a user that does not exist: %s", body)
	}
	if got := len(h.mock.instances); got != before {
		t.Errorf("%d container(s) created for a target user that does not exist", got-before)
	}
}

func TestDuplicate_DeletingTheCopyLeavesTheSourceIntact(t *testing.T) {
	h := newHarness(t)
	defer h.teardown()
	h.do("POST", "/auth/login", map[string]string{"password": testPassword}).Body.Close()
	tarStreams(h)

	templateID := seedPersistentTemplate(t, h, "two-volumes",
		persistDir{"/home/ubuntu", "20GB"}, persistDir{"/data", "10GB"})
	src := createInstance(t, h, h.do, "src-box", templateID)
	srcVols := volumesOf(t, h.do, src.ID)

	dst := duplicate(t, h.do, src.ID)

	resp := h.do("DELETE", fmt.Sprintf("/api/v1/instances/%d", dst.ID), nil)
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		t.Fatalf("delete the copy: %d — %s", resp.StatusCode, b)
	}
	resp.Body.Close()

	if _, ok := h.mock.instances[src.IncusName]; !ok {
		t.Fatal("deleting the copy took the source container with it")
	}
	stillThere := volumesOf(t, h.do, src.ID)
	if len(stillThere) != len(srcVols) {
		t.Fatalf("the source has %d volume(s) left, had %d", len(stillThere), len(srcVols))
	}
	for path, v := range srcVols {
		if _, ok := h.mock.volumes[v.VolumeName]; !ok {
			t.Errorf("the source volume at %s (%q) was deleted with the copy", path, v.VolumeName)
		}
	}
}
