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
