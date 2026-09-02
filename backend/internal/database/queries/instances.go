package queries

import (
	"github.com/homaserver/plati/internal/models"
	"github.com/jmoiron/sqlx"
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

// ListAllInstancesWithOwner lists every instance together with its owner and
// template names, for the admin dashboard.
func ListAllInstancesWithOwner(db *sqlx.DB) ([]models.AdminInstance, error) {
	instances := []models.AdminInstance{}
	err := db.Select(&instances, `
		SELECT i.*,
		       u.email AS user_email,
		       COALESCE(u.name, '') AS user_name,
		       COALESCE(t.name, '') AS template_name
		FROM instances i
		JOIN users u ON u.id = i.user_id
		LEFT JOIN templates t ON t.id = i.template_id
		ORDER BY i.created_at DESC`)
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

// ListSleepableInstances returns the running instances whose auto-stop deadline
// has passed. Each row carries its own policy: sleep_disabled takes it out of
// the sweep entirely, and sleep_timeout_minutes overrides defaultMinutes (the
// platform-wide sleep_timeout) when non-zero.
func ListSleepableInstances(db *sqlx.DB, defaultMinutes int) ([]models.Instance, error) {
	instances := []models.Instance{}
	err := db.Select(&instances, `
		SELECT * FROM instances
		WHERE status = 'running'
		AND sleep_disabled = 0
		AND (last_active_at IS NULL OR last_active_at < datetime('now',
			'-' || (CASE WHEN sleep_timeout_minutes > 0
			             THEN sleep_timeout_minutes
			             ELSE ? END) || ' minutes'))
	`, defaultMinutes)
	return instances, err
}

// UpdateInstanceSleep stores the auto-stop policy of an instance owned by
// userID. timeoutMinutes of 0 means "fall back to the platform default".
func UpdateInstanceSleep(db *sqlx.DB, id, userID int64, disabled bool, timeoutMinutes int) error {
	_, err := db.Exec(
		`UPDATE instances SET sleep_disabled = ?, sleep_timeout_minutes = ?,
		 updated_at = CURRENT_TIMESTAMP WHERE id = ? AND user_id = ?`,
		disabled, timeoutMinutes, id, userID,
	)
	return err
}

// UpdateInstanceName changes the display name of an instance owned by userID.
// The incus_name is left untouched — renaming the container is a separate,
// opt-in step (see UpdateInstanceIncusName), since it requires stopping it.
func UpdateInstanceName(db *sqlx.DB, id, userID int64, name string) error {
	_, err := db.Exec(
		"UPDATE instances SET name = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ? AND user_id = ?",
		name, id, userID,
	)
	return err
}

// UpdateInstanceIncusName records a container rename that already succeeded in
// Incus. Call it only after the Incus operation returns, never before: the two
// names drifting apart leaves the instance unreachable.
func UpdateInstanceIncusName(db *sqlx.DB, id, userID int64, incusName string) error {
	_, err := db.Exec(
		"UPDATE instances SET incus_name = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ? AND user_id = ?",
		incusName, id, userID,
	)
	return err
}

// InstanceIncusNameTaken reports whether another instance on the same server
// already uses incusName — the (incus_name, server_id) unique constraint.
func InstanceIncusNameTaken(db *sqlx.DB, serverID, excludeID int64, incusName string) (bool, error) {
	var n int
	err := db.Get(&n,
		"SELECT COUNT(*) FROM instances WHERE server_id = ? AND incus_name = ? AND id != ?",
		serverID, incusName, excludeID,
	)
	return n > 0, err
}
