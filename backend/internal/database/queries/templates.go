package queries

import (
	"github.com/jmoiron/sqlx"
	"github.com/homaserver/plati/internal/models"
)

func ListTemplates(db *sqlx.DB, activeOnly bool) ([]models.Template, error) {
	templates := []models.Template{}
	// Exclude large command fields (post_create_commands, first_init_commands, rebuild_commands)
	// which can contain base64-encoded binaries from mixins (100MB+). Use GetTemplate for full data.
	query := `SELECT id, name, slug, description, image, profiles, resources,
		terminal_user, persistence_mode, persistence_dirs, includes, repos,
		health_checks, tailscale_serve, is_active,
		created_at, updated_at FROM templates`
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
		`INSERT INTO templates (name, slug, description, image, profiles, resources, terminal_user, post_create_commands, persistence_mode, persistence_dirs, first_init_commands, rebuild_commands, includes, repos, health_checks, tailscale_serve, is_active)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		t.Name, t.Slug, t.Description, t.Image, t.Profiles, t.Resources, t.TerminalUser, t.PostCreateCommands,
		t.PersistenceMode, t.PersistenceDirs, t.FirstInitCommands, t.RebuildCommands, t.Includes, t.Repos,
		t.HealthChecks, t.TailscaleServe, t.IsActive,
	)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func UpdateTemplate(db *sqlx.DB, t *models.Template) error {
	_, err := db.Exec(
		`UPDATE templates SET name = ?, slug = ?, description = ?, image = ?, profiles = ?, resources = ?, terminal_user = ?, post_create_commands = ?, persistence_mode = ?, persistence_dirs = ?, first_init_commands = ?, rebuild_commands = ?, includes = ?, repos = ?, health_checks = ?, tailscale_serve = ?, is_active = ?, updated_at = CURRENT_TIMESTAMP
		 WHERE id = ?`,
		t.Name, t.Slug, t.Description, t.Image, t.Profiles, t.Resources, t.TerminalUser, t.PostCreateCommands,
		t.PersistenceMode, t.PersistenceDirs, t.FirstInitCommands, t.RebuildCommands, t.Includes, t.Repos,
		t.HealthChecks, t.TailscaleServe, t.IsActive, t.ID,
	)
	return err
}

func DeleteTemplate(db *sqlx.DB, id int64) error {
	_, err := db.Exec("DELETE FROM templates WHERE id = ?", id)
	return err
}

func GetLatestDebugInstanceForTemplate(db *sqlx.DB, templateID, userID int64) (*models.Instance, error) {
	var inst models.Instance
	err := db.Get(&inst,
		`SELECT * FROM instances WHERE template_id = ? AND user_id = ? AND name LIKE 'debug-%' ORDER BY created_at DESC LIMIT 1`,
		templateID, userID,
	)
	if err != nil {
		return nil, err
	}
	return &inst, nil
}
