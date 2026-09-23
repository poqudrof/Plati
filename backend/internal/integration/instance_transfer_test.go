// Handing a workspace over to another account, end to end.
//
// Unlike a copy, a transfer moves the instance itself: the same container, the same
// volumes, the same data — under a new owner. The promise is that afterwards the new
// owner reaches it and the previous one does not, in Plati and over SSH, and that the
// names derived from the owner (incus_name, the tailnet hostname) follow.
package integration_test

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/homaserver/plati/internal/models"
)

// transferResponse is the admin transfer route's JSON — the instance plus the
// hand-over report.
type transferResponse struct {
	models.InstanceJSON
	PreviousUserID       int64    `json:"previous_user_id"`
	PreviousName         string   `json:"previous_name"`
	NewOwnerEmail        string   `json:"new_owner_email"`
	ContainerRenamed     bool     `json:"container_renamed"`
	ContainerRenameError string   `json:"container_rename_error"`
	Restarted            bool     `json:"restarted"`
	KeysInstalled        int      `json:"keys_installed"`
	KeysRevoked          int      `json:"keys_revoked"`
	Warnings             []string `json:"warnings"`
}

// transferInstance hands instanceID over through the admin route, failing on anything
// but 200. body carries user_id and the optional keep_* flags.
func transferInstance(t *testing.T, h *harness, instanceID int64, body map[string]any) transferResponse {
	t.Helper()
	resp := h.do("POST", fmt.Sprintf("/api/v1/admin/instances/%d/transfer", instanceID), body)
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		t.Fatalf("transfer %d: %d — %s", instanceID, resp.StatusCode, b)
	}
	var out transferResponse
	mustJSON(t, resp, &out)
	return out
}

// addPublicKey registers one of the user's own public keys (an authorized_keys entry).
func addPublicKey(t *testing.T, h *harness, userID int64, name string) string {
	t.Helper()
	key := newPublicKey(t, name)
	resp := h.do("POST", fmt.Sprintf("/api/v1/admin/users/%d/public-keys", userID),
		map[string]string{"name": name, "public_key": key})
	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		t.Fatalf("add public key for %d: %d — %s", userID, resp.StatusCode, b)
	}
	resp.Body.Close()
	return key
}

// ownerOf reads the instance's owner straight from the row: UpdateInstance* queries
// filter on user_id and return 200 having touched nothing, so only the DB tells the
// truth about a hand-over.
func ownerOf(t *testing.T, h *harness, instanceID int64) int64 {
	t.Helper()
	var userID int64
	if err := h.db.Get(&userID, "SELECT user_id FROM instances WHERE id = ?", instanceID); err != nil {
		t.Fatalf("read owner back: %v", err)
	}
	return userID
}

func TestTransfer_MovesTheWorkspaceToAnotherAccount(t *testing.T) {
	h := newHarness(t)
	defer h.teardown()
	h.do("POST", "/auth/login", map[string]string{"password": testPassword}).Body.Close()
	tarStreams(h)

	templateID := seedTemplate(t, h)
	alice := newUserSession(t, h, "alice@plati.local", "Alice", "hunter2hunter2")
	bob := newUserSession(t, h, "bob@plati.local", "Bob", "hunter2hunter2")
	addPublicKey(t, h, alice.user.ID, "alice-laptop")
	addPublicKey(t, h, bob.user.ID, "bob-laptop")

	inst := createInstance(t, h, alice.do, "shared-box", templateID)
	origIncusName := inst.IncusName

	out := transferInstance(t, h, inst.ID, map[string]any{"user_id": bob.user.ID})

	if out.UserID != bob.user.ID {
		t.Errorf("owner in response = %d, want %d", out.UserID, bob.user.ID)
	}
	if got := ownerOf(t, h, inst.ID); got != bob.user.ID {
		t.Fatalf("owner in DB = %d, want %d — the row did not move", got, bob.user.ID)
	}
	if out.PreviousUserID != alice.user.ID {
		t.Errorf("previous_user_id = %d, want %d", out.PreviousUserID, alice.user.ID)
	}
	if out.NewOwnerEmail != bob.user.Email {
		t.Errorf("new_owner_email = %q, want %q", out.NewOwnerEmail, bob.user.Email)
	}
	// The name was free in Bob's account, so it is kept.
	if out.Name != "shared-box" || out.PreviousName != "" {
		t.Errorf("name = %q (previous %q), want it unchanged", out.Name, out.PreviousName)
	}

	// The container carries the new owner's id, and Incus actually moved it.
	want := fmt.Sprintf("plati-%d-shared-box", bob.user.ID)
	if !out.ContainerRenamed {
		t.Fatalf("container_renamed = false (%s)", out.ContainerRenameError)
	}
	if out.IncusName != want {
		t.Errorf("incus_name = %q, want %q", out.IncusName, want)
	}
	if _, ok := h.mock.instances[want]; !ok {
		t.Errorf("mock has no container %q", want)
	}
	if _, ok := h.mock.instances[origIncusName]; ok {
		t.Errorf("mock still has the previous owner's container %q", origIncusName)
	}
	var dbIncusName, dbStatus string
	h.db.Get(&dbIncusName, "SELECT incus_name FROM instances WHERE id = ?", inst.ID)
	h.db.Get(&dbStatus, "SELECT status FROM instances WHERE id = ?", inst.ID)
	if dbIncusName != want {
		t.Errorf("incus_name in DB = %q, want %q", dbIncusName, want)
	}
	if !out.Restarted || dbStatus != "running" {
		t.Errorf("restarted = %v, status = %q — a running instance must come back up", out.Restarted, dbStatus)
	}

	// Access followed the row: Bob reaches it, Alice no longer does.
	if resp := bob.do("GET", fmt.Sprintf("/api/v1/instances/%d", inst.ID), nil); resp.StatusCode != http.StatusOK {
		t.Errorf("new owner GET instance: %d, want 200", resp.StatusCode)
	}
	if resp := alice.do("GET", fmt.Sprintf("/api/v1/instances/%d", inst.ID), nil); resp.StatusCode == http.StatusOK {
		t.Error("previous owner still reaches the instance through the API")
	}

	// SSH followed too: Bob's key was installed, Alice's dropped.
	if out.KeysInstalled != 1 {
		t.Errorf("keys_installed = %d, want 1 (the new owner's key)", out.KeysInstalled)
	}
	if out.KeysRevoked != 1 {
		t.Errorf("keys_revoked = %d, want 1 (the previous owner's key)", out.KeysRevoked)
	}
	if !h.mock.hasRunCall(want, "add_key /root/.ssh") {
		t.Error("the new owner's key was never written to authorized_keys")
	}
	if !h.mock.hasRunCall(want, "drop_key") {
		t.Error("the previous owner's key was never removed from authorized_keys")
	}

	// A transfer cannot re-provision the container, so it says so.
	if !hasWarning(out.Warnings, "rebuild") {
		t.Errorf("warnings = %v, want one about rebuilding for the new owner's secrets", out.Warnings)
	}
	t.Logf("Transfer OK: %s → %s (%s)", origIncusName, want, out.NewOwnerEmail)
}

func TestTransfer_RenamesWhenTheTargetAlreadyUsesTheName(t *testing.T) {
	h := newHarness(t)
	defer h.teardown()
	h.do("POST", "/auth/login", map[string]string{"password": testPassword}).Body.Close()
	tarStreams(h)

	templateID := seedTemplate(t, h)
	alice := newUserSession(t, h, "alice@plati.local", "Alice", "hunter2hunter2")
	bob := newUserSession(t, h, "bob@plati.local", "Bob", "hunter2hunter2")

	// Both accounts have a "box": the transfer has to find a free name, or the
	// (incus_name, server_id) unique constraint — and Bob's tailnet — would collide.
	bobsOwn := createInstance(t, h, bob.do, "box", templateID)
	inst := createInstance(t, h, alice.do, "box", templateID)

	out := transferInstance(t, h, inst.ID, map[string]any{"user_id": bob.user.ID})

	if out.Name != "box-2" {
		t.Errorf("name = %q, want %q", out.Name, "box-2")
	}
	if out.PreviousName != "box" {
		t.Errorf("previous_name = %q, want %q", out.PreviousName, "box")
	}
	if want := fmt.Sprintf("plati-%d-box-2", bob.user.ID); out.IncusName != want {
		t.Errorf("incus_name = %q, want %q", out.IncusName, want)
	}
	// Bob's own instance is untouched.
	var bobsName string
	h.db.Get(&bobsName, "SELECT name FROM instances WHERE id = ?", bobsOwn.ID)
	if bobsName != "box" {
		t.Errorf("the target's own instance was renamed to %q", bobsName)
	}
	// The tailnet hostname follows the new name, so a rebuild comes back as box-2.
	if got := h.mock.instances[out.IncusName].Config["environment.PLATI_TAILSCALE_HOSTNAME"]; got != "box-2" {
		t.Errorf("PLATI_TAILSCALE_HOSTNAME = %q, want %q", got, "box-2")
	}
	t.Logf("Transfer with a taken name OK: box → %s", out.Name)
}

func TestTransfer_KeepFlagsLeaveTheContainerAndTheKeysAlone(t *testing.T) {
	h := newHarness(t)
	defer h.teardown()
	h.do("POST", "/auth/login", map[string]string{"password": testPassword}).Body.Close()
	tarStreams(h)

	templateID := seedTemplate(t, h)
	alice := newUserSession(t, h, "alice@plati.local", "Alice", "hunter2hunter2")
	bob := newUserSession(t, h, "bob@plati.local", "Bob", "hunter2hunter2")
	addPublicKey(t, h, alice.user.ID, "alice-laptop")

	inst := createInstance(t, h, alice.do, "keep-box", templateID)

	out := transferInstance(t, h, inst.ID, map[string]any{
		"user_id":             bob.user.ID,
		"keep_container_name": true,
		"keep_previous_keys":  true,
	})

	if out.IncusName != inst.IncusName {
		t.Errorf("incus_name = %q, want it left at %q", out.IncusName, inst.IncusName)
	}
	if out.ContainerRenamed || out.Restarted {
		t.Error("the container was renamed or restarted despite keep_container_name")
	}
	if out.KeysRevoked != 0 {
		t.Errorf("keys_revoked = %d, want 0 with keep_previous_keys", out.KeysRevoked)
	}
	if h.mock.hasRunCall(inst.IncusName, "drop_key") {
		t.Error("the previous owner's keys were removed despite keep_previous_keys")
	}
	// Keeping the container name strands the workspace name in the previous
	// owner's namespace, which the caller is told about.
	if !hasWarning(out.Warnings, "still named") {
		t.Errorf("warnings = %v, want one about the container name", out.Warnings)
	}
	if got := ownerOf(t, h, inst.ID); got != bob.user.ID {
		t.Errorf("owner in DB = %d, want %d", got, bob.user.ID)
	}
	t.Logf("Transfer with both keep flags OK: %s stays %s", out.Name, out.IncusName)
}

func TestTransfer_RejectsAnUnknownTarget(t *testing.T) {
	h := newHarness(t)
	defer h.teardown()
	h.do("POST", "/auth/login", map[string]string{"password": testPassword}).Body.Close()
	tarStreams(h)

	templateID := seedTemplate(t, h)
	inst := createInstance(t, h, h.do, "admin-box", templateID)

	for _, body := range []map[string]any{
		{"user_id": 424242},
		{}, // no target at all
	} {
		resp := h.do("POST", fmt.Sprintf("/api/v1/admin/instances/%d/transfer", inst.ID), body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("transfer with %v: status = %d, want 400", body, resp.StatusCode)
		}
	}
	if got := ownerOf(t, h, inst.ID); got == 0 {
		t.Error("the instance lost its owner on a rejected transfer")
	}
}

// hasWarning reports whether any warning mentions substr.
func hasWarning(warnings []string, substr string) bool {
	for _, w := range warnings {
		if strings.Contains(w, substr) {
			return true
		}
	}
	return false
}
