package queries

import (
	"github.com/jmoiron/sqlx"
	"github.com/homaserver/plati/internal/models"
)

func ListSSHKeys(db *sqlx.DB, userID int64) ([]models.SSHKey, error) {
	keys := []models.SSHKey{}
	err := db.Select(&keys, "SELECT * FROM ssh_keys WHERE user_id = ? ORDER BY created_at DESC", userID)
	return keys, err
}

// AdminSSHKey is a public key carrying its owner's identity, so it can be presented as
// "<someone>'s key" rather than a bare name. Named for its first use — offering a server
// administrator's key — it also carries the whole-platform listing below, hence UserRole:
// only the query filtered on 'admin' can assume what it holds.
type AdminSSHKey struct {
	models.SSHKey
	UserName  string `db:"user_name"  json:"user_name"`
	UserEmail string `db:"user_email" json:"user_email"`
	UserRole  string `db:"user_role"  json:"user_role"`
}

// ListAdminSSHKeys returns every public key registered by a server administrator,
// so an instance owner can grant one of them SSH access to their instance.
func ListAdminSSHKeys(db *sqlx.DB) ([]AdminSSHKey, error) {
	keys := []AdminSSHKey{}
	err := db.Select(&keys, `SELECT k.*, u.name AS user_name, u.email AS user_email, u.role AS user_role
		FROM ssh_keys k JOIN users u ON u.id = k.user_id
		WHERE u.role = 'admin'
		ORDER BY u.name, k.created_at DESC`)
	return keys, err
}

// ListAllSSHKeysWithOwner returns every registered public key, whoever owns it, with
// its owner's identity and role. Admin-only material: it is what lets an administrator
// grant any account SSH access to any instance from the SSH Access tab, which is why
// the role travels with the key — the caller still has to mark the administrators.
//
// Admins are listed last, so the owner of a key is read in the order the UI shows it:
// the instance's owner, then everyone else, administrators at the end.
func ListAllSSHKeysWithOwner(db *sqlx.DB) ([]AdminSSHKey, error) {
	keys := []AdminSSHKey{}
	err := db.Select(&keys, `SELECT k.*, u.name AS user_name, u.email AS user_email, u.role AS user_role
		FROM ssh_keys k JOIN users u ON u.id = k.user_id
		ORDER BY (u.role = 'admin'), u.name, k.created_at DESC`)
	return keys, err
}

func CreateSSHKey(db *sqlx.DB, userID int64, name, publicKey string) (int64, error) {
	res, err := db.Exec(
		"INSERT INTO ssh_keys (user_id, name, public_key) VALUES (?, ?, ?)",
		userID, name, publicKey,
	)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func DeleteSSHKey(db *sqlx.DB, id, userID int64) error {
	_, err := db.Exec("DELETE FROM ssh_keys WHERE id = ? AND user_id = ?", id, userID)
	return err
}
