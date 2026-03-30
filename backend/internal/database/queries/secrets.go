package queries

import (
	"github.com/jmoiron/sqlx"
	"github.com/homaserver/plati/internal/models"
)

func ListSecrets(db *sqlx.DB, userID int64) ([]models.Secret, error) {
	secrets := []models.Secret{}
	err := db.Select(&secrets, "SELECT id, user_id, name, created_at, updated_at FROM secrets WHERE user_id = ? ORDER BY name", userID)
	return secrets, err
}

func GetSecret(db *sqlx.DB, id, userID int64) (*models.Secret, error) {
	var s models.Secret
	err := db.Get(&s, "SELECT * FROM secrets WHERE id = ? AND user_id = ?", id, userID)
	if err != nil {
		return nil, err
	}
	return &s, nil
}

func CreateSecret(db *sqlx.DB, userID int64, name, encryptedValue string) (int64, error) {
	res, err := db.Exec(
		"INSERT INTO secrets (user_id, name, encrypted_value) VALUES (?, ?, ?)",
		userID, name, encryptedValue,
	)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func UpdateSecret(db *sqlx.DB, id, userID int64, encryptedValue string) error {
	_, err := db.Exec(
		"UPDATE secrets SET encrypted_value = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ? AND user_id = ?",
		encryptedValue, id, userID,
	)
	return err
}

func DeleteSecret(db *sqlx.DB, id, userID int64) error {
	_, err := db.Exec("DELETE FROM secrets WHERE id = ? AND user_id = ?", id, userID)
	return err
}

// ListSecretsForInjection returns all secrets including encrypted values for injection into instances.
func ListSecretsForInjection(db *sqlx.DB, userID int64) ([]models.Secret, error) {
	secrets := []models.Secret{}
	err := db.Select(&secrets, "SELECT * FROM secrets WHERE user_id = ? ORDER BY name", userID)
	return secrets, err
}
