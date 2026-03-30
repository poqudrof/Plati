package queries

import (
	"github.com/jmoiron/sqlx"
	"github.com/homaserver/plati/internal/models"
)

func ListTemplates(db *sqlx.DB, activeOnly bool) ([]models.Template, error) {
	templates := []models.Template{}
	query := "SELECT * FROM templates"
	if activeOnly {
		query += " WHERE is_active = 1"
	}
	query += " ORDER BY name"
	err := db.Select(&templates, query)
	return templates, err
}

func GetTemplate(db *sqlx.DB, id int64) (*models.Template, error) {
	var t models.Template
	err := db.Get(&t, "SELECT * FROM templates WHERE id = ?", id)
	if err != nil {
		return nil, err
	}
	return &t, nil
}

func GetTemplateBySlug(db *sqlx.DB, slug string) (*models.Template, error) {
	var t models.Template
	err := db.Get(&t, "SELECT * FROM templates WHERE slug = ?", slug)
	if err != nil {
		return nil, err
	}
	return &t, nil
}

func CreateTemplate(db *sqlx.DB, t *models.Template) (int64, error) {
	res, err := db.Exec(
		`INSERT INTO templates (name, slug, description, image, profiles, resources, cloud_init, is_active)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		t.Name, t.Slug, t.Description, t.Image, t.Profiles, t.Resources, t.CloudInit, t.IsActive,
	)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func UpdateTemplate(db *sqlx.DB, t *models.Template) error {
	_, err := db.Exec(
		`UPDATE templates SET name = ?, slug = ?, description = ?, image = ?, profiles = ?, resources = ?, cloud_init = ?, is_active = ?, updated_at = CURRENT_TIMESTAMP
		 WHERE id = ?`,
		t.Name, t.Slug, t.Description, t.Image, t.Profiles, t.Resources, t.CloudInit, t.IsActive, t.ID,
	)
	return err
}

func DeleteTemplate(db *sqlx.DB, id int64) error {
	_, err := db.Exec("DELETE FROM templates WHERE id = ?", id)
	return err
}
