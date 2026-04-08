package queries

import (
	"github.com/jmoiron/sqlx"

	"github.com/homaserver/plati/internal/models"
)

// ListInstanceVolumes returns all volume attachments for an instance.
func ListInstanceVolumes(db *sqlx.DB, instanceID int64) ([]models.InstanceVolume, error) {
	var vols []models.InstanceVolume
	err := db.Select(&vols, "SELECT * FROM instance_volumes WHERE instance_id = ?", instanceID)
	return vols, err
}

// CreateInstanceVolume records a volume attachment in the join table.
func CreateInstanceVolume(db *sqlx.DB, instanceID, volumeID int64, mountPath, deviceName string) error {
	_, err := db.Exec(
		`INSERT INTO instance_volumes (instance_id, volume_id, mount_path, device_name) VALUES (?, ?, ?, ?)`,
		instanceID, volumeID, mountPath, deviceName,
	)
	return err
}

// DeleteInstanceVolumes removes all volume attachment records for an instance.
func DeleteInstanceVolumes(db *sqlx.DB, instanceID int64) error {
	_, err := db.Exec("DELETE FROM instance_volumes WHERE instance_id = ?", instanceID)
	return err
}

const diskInfoQuery = `
	SELECT v.id AS volume_id, v.name AS volume_name, v.pool, v.size_gb,
	       iv.mount_path, iv.device_name,
	       i.id AS instance_id, i.name AS instance_name, i.incus_name, i.status,
	       u.id AS user_id, u.email AS user_email, u.name AS user_name,
	       s.id AS server_id, s.name AS server_name,
	       v.created_at
	FROM instance_volumes iv
	JOIN volumes v   ON v.id  = iv.volume_id
	JOIN instances i ON i.id  = iv.instance_id
	JOIN users u     ON u.id  = i.user_id
	JOIN servers s   ON s.id  = v.server_id
`

// ListAllDisks returns every volume with its instance, user, and server info (admin view).
func ListAllDisks(db *sqlx.DB) ([]models.DiskInfo, error) {
	var disks []models.DiskInfo
	err := db.Select(&disks, diskInfoQuery+` ORDER BY u.email, i.name, iv.mount_path`)
	return disks, err
}

// ListUserDisks returns volumes belonging to a specific user's instances.
func ListUserDisks(db *sqlx.DB, userID int64) ([]models.DiskInfo, error) {
	var disks []models.DiskInfo
	err := db.Select(&disks, diskInfoQuery+` WHERE i.user_id = ? ORDER BY i.name, iv.mount_path`, userID)
	return disks, err
}

// ListInstanceVolumesWithDetails returns volume attachments enriched with volume metadata.
func ListInstanceVolumesWithDetails(db *sqlx.DB, instanceID int64) ([]models.VolumeDetail, error) {
	var details []models.VolumeDetail
	err := db.Select(&details, `
		SELECT iv.volume_id, iv.mount_path, iv.device_name,
		       v.name AS volume_name, v.pool, v.size_gb, v.created_at
		FROM instance_volumes iv
		JOIN volumes v ON v.id = iv.volume_id
		WHERE iv.instance_id = ?
		ORDER BY iv.mount_path
	`, instanceID)
	return details, err
}
