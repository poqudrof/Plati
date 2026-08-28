package queries

import (
	"github.com/jmoiron/sqlx"

	"github.com/homaserver/plati/internal/models"
)

func CreateAPIKey(db *sqlx.DB, userID int64, name, prefix, hash string) (int64, error) {
	res, err := db.Exec(
		"INSERT INTO api_keys (user_id, name, key_prefix, key_hash) VALUES (?, ?, ?, ?)",
		userID, name, prefix, hash,
	)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func ListAPIKeysByUser(db *sqlx.DB, userID int64) ([]models.APIKey, error) {
	keys := []models.APIKey{}
	err := db.Select(&keys,
		"SELECT id, user_id, name, key_prefix, key_hash, last_used_at, revoked_at, created_at FROM api_keys WHERE user_id = ? ORDER BY created_at DESC",
		userID)
	return keys, err
}

// GetAPIKeyByHash returns the active (non-revoked) key matching hash, or nil if none.
func GetAPIKeyByHash(db *sqlx.DB, hash string) (*models.APIKey, error) {
	var k models.APIKey
	err := db.Get(&k,
		"SELECT * FROM api_keys WHERE key_hash = ? AND revoked_at IS NULL",
		hash)
	if err != nil {
		return nil, err
	}
	return &k, nil
}

func TouchAPIKeyLastUsed(db *sqlx.DB, id int64) error {
	_, err := db.Exec("UPDATE api_keys SET last_used_at = CURRENT_TIMESTAMP WHERE id = ?", id)
	return err
}

// RegenerateAPIKey rotates the secret for an existing key in place, preserving its id/name.
// Returns the number of rows affected (0 means no such key owned by userID).
func RegenerateAPIKey(db *sqlx.DB, id, userID int64, prefix, hash string) (int64, error) {
	res, err := db.Exec(
		"UPDATE api_keys SET key_prefix = ?, key_hash = ?, last_used_at = NULL WHERE id = ? AND user_id = ?",
		prefix, hash, id, userID)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

func GetAPIKeyByID(db *sqlx.DB, id int64) (*models.APIKey, error) {
	var k models.APIKey
	err := db.Get(&k, "SELECT * FROM api_keys WHERE id = ?", id)
	if err != nil {
		return nil, err
	}
	return &k, nil
}
