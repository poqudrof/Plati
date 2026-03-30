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
