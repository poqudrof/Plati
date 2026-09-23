// Who may install which public key from the SSH Access tab.
//
// A regular user is offered the instance owner's keys plus every server administrator's,
// so they can ask for help. An admin is offered every account's, which is what lets them
// give a colleague access to a workspace — before or after handing it over. The listing
// is the permission: AddAuthorizedKey resolves the key against the same list.
package integration_test

import (
	"fmt"
	"io"
	"net/http"
	"testing"

	"github.com/homaserver/plati/internal/models"
	"github.com/homaserver/plati/internal/services"
)

// registerPublicKey adds one of userID's own public keys through the admin route and
// returns the stored row, id included.
func registerPublicKey(t *testing.T, h *harness, userID int64, name string) models.SSHKey {
	t.Helper()
	resp := h.do("POST", fmt.Sprintf("/api/v1/admin/users/%d/public-keys", userID),
		map[string]string{"name": name, "public_key": newPublicKey(t, name)})
	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		t.Fatalf("add public key for %d: %d — %s", userID, resp.StatusCode, b)
	}
	var key models.SSHKey
	mustJSON(t, resp, &key)
	return key
}

// offeredKeys lists the keys the given session may install on the instance.
func offeredKeys(t *testing.T, do func(string, string, any) *http.Response, instanceID int64) []services.InstanceAuthorizedKey {
	t.Helper()
	resp := do("GET", fmt.Sprintf("/api/v1/instances/%d/authorized-keys", instanceID), nil)
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		t.Fatalf("list authorized keys of %d: %d — %s", instanceID, resp.StatusCode, b)
	}
	var keys []services.InstanceAuthorizedKey
	mustJSON(t, resp, &keys)
	return keys
}

func keyWithID(keys []services.InstanceAuthorizedKey, id int64) *services.InstanceAuthorizedKey {
	for i := range keys {
		if keys[i].ID == id {
			return &keys[i]
		}
	}
	return nil
}

func TestSSHAccess_AdminInstallsAnyAccountsKeyAndAUserCannot(t *testing.T) {
	h := newHarness(t)
	defer h.teardown()
	h.do("POST", "/auth/login", map[string]string{"password": testPassword}).Body.Close()
	tarStreams(h)

	templateID := seedTemplate(t, h)
	alice := newUserSession(t, h, "alice@plati.local", "Alice", "hunter2hunter2")
	bob := newUserSession(t, h, "bob@plati.local", "Bob", "hunter2hunter2")

	aliceKey := registerPublicKey(t, h, alice.user.ID, "alice-laptop")
	bobKey := registerPublicKey(t, h, bob.user.ID, "bob-laptop")

	inst := createInstance(t, h, alice.do, "alice-box", templateID)

	// The owner sees her own key, and not Bob's — he is neither her nor an admin.
	mine := offeredKeys(t, alice.do, inst.ID)
	if own := keyWithID(mine, aliceKey.ID); own == nil || !own.IsOwner {
		t.Fatalf("the owner's own key is missing or not flagged: %+v", mine)
	}
	if keyWithID(mine, bobKey.ID) != nil {
		t.Errorf("another user's key is offered to a non-admin: %+v", mine)
	}

	// And she cannot install it by guessing its id.
	resp := alice.do("POST", fmt.Sprintf("/api/v1/instances/%d/authorized-keys", inst.ID),
		map[string]any{"key_id": bobKey.ID})
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("owner installing another user's key: %d, want 400", resp.StatusCode)
	}
	if h.mock.hasRunCall(inst.IncusName, "add_key /root/.ssh") {
		t.Fatal("a key was written to authorized_keys by the rejected call")
	}

	// The admin is offered Bob's key on Alice's instance, with his identity on it.
	asAdmin := offeredKeys(t, h.do, inst.ID)
	bobsEntry := keyWithID(asAdmin, bobKey.ID)
	if bobsEntry == nil {
		t.Fatalf("admin is not offered the third account's key: %+v", asAdmin)
	}
	if bobsEntry.IsOwner || bobsEntry.IsAdmin {
		t.Errorf("Bob's key is flagged as the owner's or an admin's: %+v", bobsEntry)
	}
	if bobsEntry.OwnerName != "Bob" {
		t.Errorf("owner_name = %q, want %q", bobsEntry.OwnerName, "Bob")
	}

	// …and installs it, which is the whole point: Bob can now SSH into the workspace.
	resp = h.do("POST", fmt.Sprintf("/api/v1/instances/%d/authorized-keys", inst.ID),
		map[string]any{"key_id": bobKey.ID})
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		t.Fatalf("admin installing another user's key: %d — %s", resp.StatusCode, b)
	}
	resp.Body.Close()
	if !h.mock.hasRunCall(inst.IncusName, "add_key /root/.ssh") {
		t.Errorf("no install ran in the instance; calls: %v", h.mock.runCallsFor(inst.IncusName))
	}
	t.Logf("SSH access OK: admin installed %s's key on %s's workspace", bobsEntry.OwnerName, "Alice")
}
