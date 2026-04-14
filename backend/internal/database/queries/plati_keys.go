package queries

import (
	"github.com/homaserver/plati/internal/models"
	"github.com/jmoiron/sqlx"
)

// ── User SSH Keys ────────────────────────────────────────────────────────────

// ListUserSSHKeys returns all Plati-generated keys for a user (public key only, safe for API).
func ListUserSSHKeys(db *sqlx.DB, userID int64) ([]models.UserSSHKey, error) {
	keys := []models.UserSSHKey{}
	err := db.Select(&keys,
		"SELECT id, user_id, name, public_key, created_at FROM user_ssh_keys WHERE user_id = ? ORDER BY created_at DESC",
		userID)
	return keys, err
}

// ListUserSSHKeysForInjection returns all keys including the encrypted private key, for instance creation only.
func ListUserSSHKeysForInjection(db *sqlx.DB, userID int64) ([]models.UserSSHKey, error) {
	keys := []models.UserSSHKey{}
	err := db.Select(&keys,
		"SELECT * FROM user_ssh_keys WHERE user_id = ? ORDER BY created_at DESC",
		userID)
	return keys, err
}

func CreateUserSSHKey(db *sqlx.DB, userID int64, name, publicKey, encryptedPrivateKey string) (int64, error) {
	res, err := db.Exec(
		"INSERT INTO user_ssh_keys (user_id, name, public_key, encrypted_private_key) VALUES (?, ?, ?, ?)",
		userID, name, publicKey, encryptedPrivateKey,
	)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func DeleteUserSSHKey(db *sqlx.DB, id, userID int64) error {
	_, err := db.Exec("DELETE FROM user_ssh_keys WHERE id = ? AND user_id = ?", id, userID)
	return err
}

// ── Managed SSH Keys ─────────────────────────────────────────────────────────

// ListManagedSSHKeys returns all admin-managed keys (public key only, safe for API).
func ListManagedSSHKeys(db *sqlx.DB) ([]models.ManagedSSHKey, error) {
	keys := []models.ManagedSSHKey{}
	err := db.Select(&keys,
		"SELECT id, name, public_key, created_at FROM managed_ssh_keys ORDER BY created_at DESC")
	return keys, err
}

// GetManagedSSHKey returns a single managed key including encrypted private key.
func GetManagedSSHKey(db *sqlx.DB, id int64) (*models.ManagedSSHKey, error) {
	key := &models.ManagedSSHKey{}
	err := db.Get(key, "SELECT * FROM managed_ssh_keys WHERE id = ?", id)
	return key, err
}

func CreateManagedSSHKey(db *sqlx.DB, name, publicKey, encryptedPrivateKey string) (int64, error) {
	res, err := db.Exec(
		"INSERT INTO managed_ssh_keys (name, public_key, encrypted_private_key) VALUES (?, ?, ?)",
		name, publicKey, encryptedPrivateKey,
	)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func DeleteManagedSSHKey(db *sqlx.DB, id int64) error {
	_, err := db.Exec("DELETE FROM managed_ssh_keys WHERE id = ?", id)
	return err
}

// ── Managed Key Assignments ───────────────────────────────────────────────────

func GetManagedKeyUsers(db *sqlx.DB, keyID int64) ([]int64, error) {
	var ids []int64
	err := db.Select(&ids, "SELECT user_id FROM managed_key_assignments WHERE managed_key_id = ? ORDER BY user_id", keyID)
	return ids, err
}

// SetManagedKeyUsers replaces the full user assignment for a managed key.
func SetManagedKeyUsers(db *sqlx.DB, keyID int64, userIDs []int64) error {
	tx, err := db.Beginx()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err = tx.Exec("DELETE FROM managed_key_assignments WHERE managed_key_id = ?", keyID); err != nil {
		return err
	}
	for _, uid := range userIDs {
		if _, err = tx.Exec("INSERT INTO managed_key_assignments (managed_key_id, user_id) VALUES (?, ?)", keyID, uid); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// GetManagedKeysForUser returns managed keys available to a user for injection.
// A key is available if it is explicitly assigned to the user OR if it has no
// assignments at all (treated as a global/shared key available to everyone).
func GetManagedKeysForUser(db *sqlx.DB, userID int64) ([]models.ManagedSSHKey, error) {
	keys := []models.ManagedSSHKey{}
	err := db.Select(&keys, `
		SELECT DISTINCT mk.* FROM managed_ssh_keys mk
		LEFT JOIN managed_key_assignments mka ON mka.managed_key_id = mk.id
		WHERE mka.user_id = ?
		   OR mk.id NOT IN (SELECT managed_key_id FROM managed_key_assignments)
	`, userID)
	return keys, err
}
