package integration_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
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
