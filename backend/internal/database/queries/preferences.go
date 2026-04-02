package queries

import (
	"database/sql"
	"time"

	"github.com/jmoiron/sqlx"

	"github.com/homaserver/plati/internal/models"
)

// GetUserPreferences returns a user's preferences, or defaults if no row exists.
func GetUserPreferences(db *sqlx.DB, userID int64) (*models.UserPreferences, error) {
	var p models.UserPreferences
	err := db.Get(&p, "SELECT * FROM user_preferences WHERE user_id = ?", userID)
	if err == sql.ErrNoRows {
		return &models.UserPreferences{
			UserID:        userID,
			SSHKeyMode:    "plati",
			TailscaleMode: "plati",
			UpdatedAt:     time.Now(),
		}, nil
	}
	if err != nil {
		return nil, err
	}
	return &p, nil
}

// UpsertUserPreferences creates or updates a user's preferences.
func UpsertUserPreferences(db *sqlx.DB, prefs *models.UserPreferences) error {
	_, err := db.Exec(`
		INSERT INTO user_preferences (user_id, ssh_key_mode, tailscale_mode, updated_at)
		VALUES (?, ?, ?, CURRENT_TIMESTAMP)
		ON CONFLICT(user_id) DO UPDATE SET
			ssh_key_mode   = excluded.ssh_key_mode,
			tailscale_mode = excluded.tailscale_mode,
			updated_at     = CURRENT_TIMESTAMP
	`, prefs.UserID, prefs.SSHKeyMode, prefs.TailscaleMode)
	return err
}
