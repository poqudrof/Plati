package queries

import (
	"github.com/jmoiron/sqlx"
	"github.com/homaserver/plati/internal/models"
)

func ListServers(db *sqlx.DB) ([]models.Server, error) {
	servers := []models.Server{}
	err := db.Select(&servers, "SELECT * FROM servers ORDER BY name")
	return servers, err
}

func GetServer(db *sqlx.DB, id int64) (*models.Server, error) {
	var s models.Server
	err := db.Get(&s, "SELECT * FROM servers WHERE id = ?", id)
	if err != nil {
		return nil, err
	}
	return &s, nil
}

func CreateServer(db *sqlx.DB, name, endpoint, certPath, keyPath string, maxInstances int) (int64, error) {
	res, err := db.Exec(
		"INSERT INTO servers (name, endpoint, tls_cert_path, tls_key_path, max_instances) VALUES (?, ?, ?, ?, ?)",
		name, endpoint, certPath, keyPath, maxInstances,
	)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func UpdateServerStatus(db *sqlx.DB, id int64, isOnline bool) error {
	_, err := db.Exec(
		"UPDATE servers SET is_online = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?",
		isOnline, id,
	)
	return err
}

func CountInstancesOnServer(db *sqlx.DB, serverID int64) (int, error) {
	var count int
	err := db.Get(&count, "SELECT COUNT(*) FROM instances WHERE server_id = ? AND status != 'error'", serverID)
	return count, err
}
