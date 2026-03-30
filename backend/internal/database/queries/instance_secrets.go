package queries

import (
	"github.com/jmoiron/sqlx"
	"github.com/homaserver/plati/internal/models"
)

func ListInstanceSecrets(db *sqlx.DB, instanceID int64) ([]models.InstanceSecret, error) {
	secrets := []models.InstanceSecret{}
	err := db.Select(&secrets, "SELECT id, instance_id, name, created_at, updated_at FROM instance_secrets WHERE instance_id = ? ORDER BY name", instanceID)
	return secrets, err
}

func ListInstanceSecretsForInjection(db *sqlx.DB, instanceID int64) ([]models.InstanceSecret, error) {
	secrets := []models.InstanceSecret{}
	err := db.Select(&secrets, "SELECT * FROM instance_secrets WHERE instance_id = ? ORDER BY name", instanceID)
	return secrets, err
}

func CreateInstanceSecret(db *sqlx.DB, instanceID int64, name, encryptedValue string) (int64, error) {
	res, err := db.Exec(
		"INSERT INTO instance_secrets (instance_id, name, encrypted_value) VALUES (?, ?, ?)",
		instanceID, name, encryptedValue,
	)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func UpdateInstanceSecret(db *sqlx.DB, id, instanceID int64, encryptedValue string) error {
	_, err := db.Exec(
		"UPDATE instance_secrets SET encrypted_value = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ? AND instance_id = ?",
		encryptedValue, id, instanceID,
	)
	return err
}

func DeleteInstanceSecret(db *sqlx.DB, id, instanceID int64) error {
	_, err := db.Exec("DELETE FROM instance_secrets WHERE id = ? AND instance_id = ?", id, instanceID)
	return err
}
