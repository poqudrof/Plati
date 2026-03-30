package queries

import (
	"github.com/jmoiron/sqlx"
	"github.com/homaserver/plati/internal/models"
)

func CreateVolume(db *sqlx.DB, name string, serverID int64, pool string, sizeGB int) (int64, error) {
	res, err := db.Exec(
		"INSERT INTO volumes (name, server_id, pool, size_gb) VALUES (?, ?, ?, ?)",
		name, serverID, pool, sizeGB,
	)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func GetVolume(db *sqlx.DB, id int64) (*models.Volume, error) {
	var v models.Volume
	err := db.Get(&v, "SELECT * FROM volumes WHERE id = ?", id)
	if err != nil {
		return nil, err
	}
	return &v, nil
}

func DeleteVolume(db *sqlx.DB, id int64) error {
	_, err := db.Exec("DELETE FROM volumes WHERE id = ?", id)
	return err
}
