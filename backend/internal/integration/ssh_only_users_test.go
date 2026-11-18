package integration_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"testing"

	"github.com/homaserver/plati/internal/models"
)

// TestSSHOnlyUsers covers accounts that only hold SSH keys: an admin creates one without
// a password and adds its key, the key is offered on the SSH Access tab like anyone
// else's, and no way of reaching the app — password, API key, a session opened before
// the switch — lets the account in.
func TestSSHOnlyUsers(t *testing.T) {
	h := newHarness(t)
	defer h.teardown()
	h.do("POST", "/auth/login", map[string]string{"password": testPassword}).Body.Close()

	const pubKey = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIHkQ7mVtYb2sLpR9cX1wZ0nD5jT8uKaGvE3fShNxQlMr bob@laptop"

	// A malformed key is refused before the account exists.
	resp := h.do("POST", "/api/v1/admin/users", map[string]any{
		"email": "bob@contractor.example", "name": "Bob", "ssh_only": true, "public_key": "not-a-key",
	})
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("malformed key at creation: expected 400, got %d", resp.StatusCode)
	}
	resp.Body.Close()

	// No password needed; the key comes in the same call.
	resp = h.do("POST", "/api/v1/admin/users", map[string]any{
		"email": "bob@contractor.example", "name": "Bob", "ssh_only": true,
		"key_name": "laptop", "public_key": pubKey,
	})
	if resp.StatusCode != http.StatusCreated {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("create SSH-only user: %d — %s", resp.StatusCode, b)
	}
	var bob models.User
	mustJSON(t, resp, &bob)
	if !bob.SSHOnly || bob.Role != "user" {
		t.Fatalf("expected an SSH-only plain user, got %+v", bob)
	}

	resp = h.do("GET", fmt.Sprintf("/api/v1/admin/users/%d/public-keys", bob.ID), nil)
	var bobKeys []models.SSHKey
	mustJSON(t, resp, &bobKeys)
	if len(bobKeys) != 1 || bobKeys[0].Name != "laptop" || bobKeys[0].PublicKey != pubKey {
		t.Fatalf("expected the key given at creation, got %+v", bobKeys)
	}

	// An SSH-only account cannot be given a password or the admin role.
	for _, body := range []map[string]any{
		{"name": "Bob", "role": "user", "password": "longenough1"},
		{"name": "Bob", "role": "admin"},
	} {
		resp = h.do("PUT", fmt.Sprintf("/api/v1/admin/users/%d", bob.ID), body)
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("edit %v: expected 400, got %d", body, resp.StatusCode)
		}
		resp.Body.Close()
	}

	// An API key minted for it is refused.
	resp = h.do("POST", fmt.Sprintf("/api/v1/admin/users/%d/api-keys", bob.ID), map[string]string{"name": "ci"})
	var minted struct {
		Key string `json:"key"`
	}
	mustJSON(t, resp, &minted)
	req, _ := http.NewRequest("GET", h.srv.URL+"/api/v1/instances", nil)
	req.Header.Set("Authorization", "Bearer "+minted.Key)
	apiResp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	apiResp.Body.Close()
	if apiResp.StatusCode != http.StatusUnauthorized {
		t.Errorf("API key of an SSH-only user: expected 401, got %d", apiResp.StatusCode)
	}

	// The key is offered to an admin on any instance's SSH Access tab.
	templateID := seedTemplate(t, h)
	resp = h.do("POST", "/api/v1/instances", map[string]any{"name": "sshonly", "template_id": templateID})
	var inst models.InstanceJSON
	mustJSON(t, resp, &inst)
	resp = h.do("GET", fmt.Sprintf("/api/v1/instances/%d/authorized-keys", inst.ID), nil)
	var keys []map[string]any
	mustJSON(t, resp, &keys)
	found := false
	for _, k := range keys {
		if k["owner_name"] == "Bob" && k["public_key"] == pubKey {
			found = true
		}
	}
	if !found {
		t.Errorf("the SSH-only user's key is not offered on the SSH Access tab: %v", keys)
	}

	// A regular user switched to SSH-only loses the session they already had.
	carol := newUserSession(t, h, "carol@example.com", "Carol", "carolpass123")
	if r := carol.do("GET", "/api/v1/instances", nil); r.StatusCode != http.StatusOK {
		t.Fatalf("carol before the switch: %d", r.StatusCode)
	} else {
		r.Body.Close()
	}
	resp = h.do("PUT", fmt.Sprintf("/api/v1/admin/users/%d", carol.user.ID), map[string]any{
		"name": "Carol", "role": "user", "ssh_only": true,
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("switch carol to SSH-only: %d", resp.StatusCode)
	}
	resp.Body.Close()
	if r := carol.do("GET", "/api/v1/instances", nil); r.StatusCode != http.StatusUnauthorized {
		t.Errorf("carol's open session after the switch: expected 401, got %d", r.StatusCode)
	} else {
		r.Body.Close()
	}

	// ...and cannot log in again: the switch dropped her password.
	body, _ := json.Marshal(map[string]string{"email": "carol@example.com", "password": "carolpass123"})
	login, err := http.Post(h.srv.URL+"/auth/login", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	login.Body.Close()
	if login.StatusCode != http.StatusUnauthorized {
		t.Errorf("password login of an SSH-only user: expected 401, got %d", login.StatusCode)
	}
}

// TestSSHOnlyUsers_SwitchBackDoesNotReviveSessions: a session cut by a switch to
// SSH-only stays cut when the account is switched back. Checking ssh_only alone used to
// revive it — with the role its token was issued under, so a former admin got admin
// access back while the database said "user". Found by TestSSHOnlyUsers_Generative.
func TestSSHOnlyUsers_SwitchBackDoesNotReviveSessions(t *testing.T) {
	h := newHarness(t)
	defer h.teardown()
	h.do("POST", "/auth/login", map[string]string{"password": testPassword}).Body.Close()

	dave := newUserSession(t, h, "dave@example.com", "Dave", "davepass123")
	edit := func(body map[string]any) {
		t.Helper()
		resp := h.do("PUT", fmt.Sprintf("/api/v1/admin/users/%d", dave.user.ID), body)
		if resp.StatusCode != http.StatusOK {
			b, _ := io.ReadAll(resp.Body)
			t.Fatalf("edit dave %v: %d — %s", body, resp.StatusCode, b)
		}
		resp.Body.Close()
	}
	status := func(s *session, path string) int {
		t.Helper()
		r := s.do("GET", path, nil)
		r.Body.Close()
		return r.StatusCode
	}

	// Promote, then log in again so the session's token says "admin".
	edit(map[string]any{"name": "Dave", "role": "admin"})
	admin := newUserSessionLogin(t, h, dave.user, "davepass123")
	if got := status(admin, "/api/v1/admin/users"); got != http.StatusOK {
		t.Fatalf("dave as admin before the switch: %d", got)
	}

	edit(map[string]any{"name": "Dave", "role": "user", "ssh_only": true})
	// Back to a Plati account with a new password: nothing of the old sessions may return.
	edit(map[string]any{"name": "Dave", "role": "user", "ssh_only": false, "password": "newdavepass1"})

	for _, path := range []string{"/api/v1/instances", "/api/v1/admin/users"} {
		if got := status(admin, path); got != http.StatusUnauthorized {
			t.Errorf("pre-switch admin session on %s: expected 401, got %d", path, got)
		}
		if got := status(dave, path); got != http.StatusUnauthorized {
			t.Errorf("pre-switch user session on %s: expected 401, got %d", path, got)
		}
	}

	// A fresh login works, with the role the account has now.
	fresh := newUserSessionLogin(t, h, dave.user, "newdavepass1")
	if got := status(fresh, "/api/v1/instances"); got != http.StatusOK {
		t.Errorf("fresh session after the switch back: expected 200, got %d", got)
	}
	if got := status(fresh, "/api/v1/admin/users"); got != http.StatusForbidden {
		t.Errorf("fresh session is a plain user now: expected 403 on admin routes, got %d", got)
	}
}

// TestSSHOnlyUsers_SwitchRevokesAPIKeys: switching an account to SSH-only deletes its API
// keys, so switching it back does not bring them back — like its sessions. A key minted
// after the switch back works.
func TestSSHOnlyUsers_SwitchRevokesAPIKeys(t *testing.T) {
	h := newHarness(t)
	defer h.teardown()
	h.do("POST", "/auth/login", map[string]string{"password": testPassword}).Body.Close()

	erin := newUserSession(t, h, "erin@example.com", "Erin", "erinpass123")
	keysPath := fmt.Sprintf("/api/v1/admin/users/%d/api-keys", erin.user.ID)
	mint := func() string {
		t.Helper()
		var minted struct {
			Key string `json:"key"`
		}
		mustJSON(t, h.do("POST", keysPath, map[string]string{"name": "ci"}), &minted)
		return minted.Key
	}
	status := func(key string) int {
		t.Helper()
		req, _ := http.NewRequest("GET", h.srv.URL+"/api/v1/instances", nil)
		req.Header.Set("Authorization", "Bearer "+key)
		r, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		r.Body.Close()
		return r.StatusCode
	}
	edit := func(body map[string]any) {
		t.Helper()
		resp := h.do("PUT", fmt.Sprintf("/api/v1/admin/users/%d", erin.user.ID), body)
		if resp.StatusCode != http.StatusOK {
			b, _ := io.ReadAll(resp.Body)
			t.Fatalf("edit erin %v: %d — %s", body, resp.StatusCode, b)
		}
		resp.Body.Close()
	}
	listed := func() int {
		t.Helper()
		var keys []models.APIKey
		mustJSON(t, h.do("GET", keysPath, nil), &keys)
		return len(keys)
	}

	old := mint()
	if got := status(old); got != http.StatusOK {
		t.Fatalf("API key before the switch: expected 200, got %d", got)
	}

	edit(map[string]any{"name": "Erin", "role": "user", "ssh_only": true})
	if n := listed(); n != 0 {
		t.Errorf("after the switch to SSH-only: expected no API keys listed, got %d", n)
	}
	edit(map[string]any{"name": "Erin", "role": "user", "ssh_only": false})

	if got := status(old); got != http.StatusUnauthorized {
		t.Errorf("pre-switch API key after the switch back: expected 401, got %d", got)
	}
	fresh := mint()
	if got := status(fresh); got != http.StatusOK {
		t.Errorf("API key minted after the switch back: expected 200, got %d", got)
	}
	if n := listed(); n != 1 {
		t.Errorf("after the switch back: expected the fresh key only, got %d listed", n)
	}
}

// newUserSessionLogin logs an existing account in on a fresh cookie jar.
func newUserSessionLogin(t *testing.T, h *harness, user models.User, password string) *session {
	t.Helper()
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar}
	body, _ := json.Marshal(map[string]string{"email": user.Email, "password": password})
	login, err := client.Post(h.srv.URL+"/auth/login", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	login.Body.Close()
	if login.StatusCode != http.StatusOK {
		t.Fatalf("login %s: %d", user.Email, login.StatusCode)
	}
	return &session{t: t, h: h, client: client, user: user}
}
