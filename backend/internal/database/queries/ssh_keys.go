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

// AdminSSHKey is a public key belonging to a user with the admin role, carrying the
// owner's identity so it can be presented as "<admin>'s key" rather than a bare name.
type AdminSSHKey struct {
	models.SSHKey
	UserName  string `db:"user_name"  json:"user_name"`
	UserEmail string `db:"user_email" json:"user_email"`
}

// ListAdminSSHKeys returns every public key registered by a server administrator,
// so an instance owner can grant one of them SSH access to their instance.
func ListAdminSSHKeys(db *sqlx.DB) ([]AdminSSHKey, error) {
	keys := []AdminSSHKey{}
	err := db.Select(&keys, `SELECT k.*, u.name AS user_name, u.email AS user_email
		FROM ssh_keys k JOIN users u ON u.id = k.user_id
		WHERE u.role = 'admin'
		ORDER BY u.name, k.created_at DESC`)
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
