package queries

import (
	"github.com/jmoiron/sqlx"
	"github.com/homaserver/plati/internal/models"
)

func ListInstancesByUser(db *sqlx.DB, userID int64) ([]models.Instance, error) {
	instances := []models.Instance{}
	err := db.Select(&instances, "SELECT * FROM instances WHERE user_id = ? ORDER BY created_at DESC", userID)
	return instances, err
}

func ListAllInstances(db *sqlx.DB) ([]models.Instance, error) {
	instances := []models.Instance{}
	err := db.Select(&instances, "SELECT * FROM instances ORDER BY created_at DESC")
	return instances, err
}

func GetInstance(db *sqlx.DB, id int64) (*models.Instance, error) {
	var inst models.Instance
	err := db.Get(&inst, "SELECT * FROM instances WHERE id = ?", id)
	if err != nil {
		return nil, err
	}
	return &inst, nil
}

func GetInstanceByUser(db *sqlx.DB, id, userID int64) (*models.Instance, error) {
	var inst models.Instance
	err := db.Get(&inst, "SELECT * FROM instances WHERE id = ? AND user_id = ?", id, userID)
	if err != nil {
		return nil, err
	}
	return &inst, nil
}

func CreateInstance(db *sqlx.DB, inst *models.Instance) (int64, error) {
	res, err := db.Exec(
		`INSERT INTO instances (name, user_id, template_id, server_id, volume_id, incus_name, status)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		inst.Name, inst.UserID, inst.TemplateID, inst.ServerID, inst.VolumeID, inst.IncusName, inst.Status,
	)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func UpdateInstanceStatus(db *sqlx.DB, id int64, status string) error {
	_, err := db.Exec(
		"UPDATE instances SET status = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?",
		status, id,
	)
	return err
}

func UpdateInstanceIP(db *sqlx.DB, id int64, ip string) error {
	_, err := db.Exec(
		"UPDATE instances SET ip_address = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?",
		ip, id,
	)
	return err
}

func UpdateInstanceCreationLog(db *sqlx.DB, id int64, log string) error {
	_, err := db.Exec(
		"UPDATE instances SET creation_log = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?",
		log, id,
	)
	return err
}

func UpdateInstanceLastActive(db *sqlx.DB, id int64) error {
	_, err := db.Exec(
		"UPDATE instances SET last_active_at = CURRENT_TIMESTAMP, updated_at = CURRENT_TIMESTAMP WHERE id = ?",
		id,
	)
	return err
}

func DeleteInstance(db *sqlx.DB, id int64) error {
	_, err := db.Exec("DELETE FROM instances WHERE id = ?", id)
	return err
}

func ListSleepableInstances(db *sqlx.DB, olderThan string) ([]models.Instance, error) {
	instances := []models.Instance{}
	err := db.Select(&instances, `
		SELECT * FROM instances
		WHERE status = 'running'
		AND (last_active_at IS NULL OR last_active_at < datetime('now', ?))
	`, olderThan)
	return instances, err
}
