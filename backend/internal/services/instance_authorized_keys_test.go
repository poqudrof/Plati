package services

import (
	"strings"
	"testing"

	"github.com/jmoiron/sqlx"

	"github.com/homaserver/plati/internal/auth"
	"github.com/homaserver/plati/internal/database"
	"github.com/homaserver/plati/internal/database/queries"
	"github.com/homaserver/plati/internal/models"
)

const testPubKey = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIP0BwK0dGxOw5Pt+VbFN6/pT7dS+ZAlnzQEGB3zLbHfa laptop"
const testPubKey2 = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIBqRcQ2sMlk4vFhZ8mE1sT5wYpN0uKjD9aXcVbGhLmQr admin-laptop"
const testPubKey3 = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIHkQ7mVtYb2sLpR9cX1wZ0nD5jT8uKaGvE3fShNxQlMr other-laptop"

// newTestDB returns a migrated in-memory database.
func newTestDB(t *testing.T) *sqlx.DB {
	t.Helper()
	db, err := database.New(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	if err := database.Migrate(db); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func mustCreateUser(t *testing.T, db *sqlx.DB, email, name, role string) int64 {
	t.Helper()
	id, err := queries.CreateUser(db, email, name, role, nil)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func mustCreateSSHKey(t *testing.T, db *sqlx.DB, userID int64, name, publicKey string) int64 {
	t.Helper()
	id, err := queries.CreateSSHKey(db, userID, name, publicKey)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func TestKeyBodyDropsComment(t *testing.T) {
	if got := keyBody(testPubKey); got != "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIP0BwK0dGxOw5Pt+VbFN6/pT7dS+ZAlnzQEGB3zLbHfa" {
		t.Errorf("unexpected key body: %q", got)
	}
}

func TestAuthorizedKeyScriptAddsRootAndTerminalUser(t *testing.T) {
	script := authorizedKeyScript(testPubKey, "dev")
	if !strings.Contains(script, "add_key /root/.ssh") {
		t.Error("script does not write root's authorized_keys")
	}
	if !strings.Contains(script, "id dev >/dev/null") || !strings.Contains(script, `chown -R dev:dev`) {
		t.Errorf("script does not handle the terminal user:\n%s", script)
	}
	// The key itself must never appear unquoted in the script (it is base64-encoded).
	if strings.Contains(script, testPubKey) {
		t.Error("public key was interpolated raw into the shell script")
	}
}

func TestAuthorizedKeyScriptRejectsUnsafeTerminalUser(t *testing.T) {
	script := authorizedKeyScript(testPubKey, "dev; rm -rf /")
	if strings.Contains(script, "rm -rf") {
		t.Errorf("unsafe terminal user was interpolated:\n%s", script)
	}
	if !strings.Contains(script, "add_key /root/.ssh") {
		t.Error("root step should still be present")
	}
}

func TestNormalizeSSHPublicKey(t *testing.T) {
	got, err := NormalizeSSHPublicKey("  " + testPubKey + "\n")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != testPubKey {
		t.Errorf("got %q, want %q", got, testPubKey)
	}

	if _, err := NormalizeSSHPublicKey("not a key"); err == nil {
		t.Error("expected an error for a malformed key")
	}
}

// Installing a key must not leave the terminal user's home root-owned, which happens
// when ~/.ssh is created under a freshly mounted (root:root) volume.
func TestAuthorizedKeyScriptFixesRootOwnedHome(t *testing.T) {
	script := authorizedKeyScript(testPubKey, "dev")
	if !strings.Contains(script, `-maxdepth 0 -user root`) {
		t.Errorf("script does not check whether the home is root-owned:\n%s", script)
	}
	if !strings.Contains(script, `chown dev:dev "$home"`) {
		t.Errorf("script does not take ownership of the home dir:\n%s", script)
	}
	if strings.Index(script, `chown dev:dev "$home"`) > strings.Index(script, `add_key "$home/.ssh"`) {
		t.Error("home ownership must be fixed before creating ~/.ssh")
	}
}

func TestAuthorizedKeyScriptSkipsHomeBlockForRoot(t *testing.T) {
	script := authorizedKeyScript(testPubKey, "root")
	if !strings.Contains(script, "add_key /root/.ssh") {
		t.Error("root step should be present")
	}
	if strings.Contains(script, "getent passwd root") {
		t.Errorf("root terminal user should not get a second home block:\n%s", script)
	}
}

// The SSH Access tab offers server administrators' keys alongside the owner's, so an
// instance owner can grant an admin access without a rebuild. Admin keys come last, and
// an admin looking at their own instance must not see their keys twice.
func TestInstallableKeysIncludesAdminsWithoutDuplicates(t *testing.T) {
	db := newTestDB(t)

	ownerID := mustCreateUser(t, db, "owner@example.com", "Owner", "user")
	adminID := mustCreateUser(t, db, "admin@example.com", "Admin", "admin")
	ownerKeyID := mustCreateSSHKey(t, db, ownerID, "laptop", testPubKey)
	adminKeyID := mustCreateSSHKey(t, db, adminID, "admin-laptop", testPubKey2)
	// A third account's key: invisible to the owner, offered to an admin.
	otherID := mustCreateUser(t, db, "other@example.com", "Other", "user")
	otherKeyID := mustCreateSSHKey(t, db, otherID, "other-laptop", testPubKey3)

	svc := &InstanceService{db: db}
	owner := auth.Actor{UserID: ownerID}
	keys, err := svc.installableKeys(&models.Instance{UserID: ownerID}, owner)
	if err != nil {
		t.Fatal(err)
	}
	if len(keys) != 2 {
		t.Fatalf("expected owner key + admin key, got %d: %+v", len(keys), keys)
	}
	if keys[0].ID != ownerKeyID || keys[0].IsAdmin || !keys[0].IsOwner {
		t.Errorf("owner key must come first, flagged as the owner's: %+v", keys[0])
	}
	if keys[1].ID != adminKeyID || !keys[1].IsAdmin || keys[1].OwnerName != "Admin" {
		t.Errorf("admin key should be flagged with its owner: %+v", keys[1])
	}
	for _, k := range keys {
		if k.ID == otherKeyID {
			t.Error("another user's key was offered to a non-admin caller")
		}
	}

	// An admin's own instance lists each of their keys once.
	adminActor := auth.Actor{UserID: adminID, Admin: true}
	keys, err = svc.installableKeys(&models.Instance{UserID: adminID}, adminActor)
	if err != nil {
		t.Fatal(err)
	}
	if len(keys) != 3 {
		t.Fatalf("admin should see every key, got %d: %+v", len(keys), keys)
	}
	if keys[0].ID != adminKeyID || !keys[0].IsOwner {
		t.Errorf("the instance owner's own key must come first: %+v", keys[0])
	}
	for _, k := range keys[1:] {
		if k.ID == adminKeyID {
			t.Error("the admin's key is listed twice on their own instance")
		}
	}
}

// An admin installs anybody's key, which is how they hand access to a colleague; a
// regular user only ever reaches the owner's and the administrators'.
func TestInstallableKeysGiveAnAdminEveryAccountsKeys(t *testing.T) {
	db := newTestDB(t)

	ownerID := mustCreateUser(t, db, "owner@example.com", "Owner", "user")
	adminID := mustCreateUser(t, db, "admin@example.com", "Admin", "admin")
	otherID := mustCreateUser(t, db, "other@example.com", "Other", "user")
	mustCreateSSHKey(t, db, ownerID, "laptop", testPubKey)
	mustCreateSSHKey(t, db, adminID, "admin-laptop", testPubKey2)
	otherKeyID := mustCreateSSHKey(t, db, otherID, "other-laptop", testPubKey3)

	svc := &InstanceService{db: db}
	keys, err := svc.installableKeys(&models.Instance{UserID: ownerID}, auth.Actor{UserID: adminID, Admin: true})
	if err != nil {
		t.Fatal(err)
	}

	var other *InstanceAuthorizedKey
	for i := range keys {
		if keys[i].ID == otherKeyID {
			other = &keys[i]
		}
	}
	if other == nil {
		t.Fatalf("the third account's key is not offered to an admin: %+v", keys)
	}
	if other.IsAdmin || other.IsOwner {
		t.Errorf("a plain user's key must be flagged as neither the owner's nor an admin's: %+v", other)
	}
	if other.OwnerName != "Other" || other.OwnerEmail != "other@example.com" {
		t.Errorf("the key does not carry its owner's identity: %+v", other)
	}
	// Administrators still come last, so the list reads owner → users → admins.
	if !keys[len(keys)-1].IsAdmin {
		t.Errorf("administrators should be listed last: %+v", keys)
	}
}
