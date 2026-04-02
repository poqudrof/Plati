package services

import (
	"fmt"

	"github.com/jmoiron/sqlx"

	"github.com/homaserver/plati/internal/database/queries"
	"github.com/homaserver/plati/internal/models"
)

type PreferencesService struct {
	db *sqlx.DB
}

func NewPreferencesService(db *sqlx.DB) *PreferencesService {
	return &PreferencesService{db: db}
}

func (s *PreferencesService) Get(userID int64) (*models.UserPreferences, error) {
	return queries.GetUserPreferences(s.db, userID)
}

func (s *PreferencesService) Update(userID int64, sshKeyMode, tailscaleMode string) error {
	if sshKeyMode != "plati" && sshKeyMode != "personal" {
		return fmt.Errorf("invalid ssh_key_mode: %q", sshKeyMode)
	}
	if tailscaleMode != "plati" && tailscaleMode != "personal" {
		return fmt.Errorf("invalid tailscale_mode: %q", tailscaleMode)
	}
	return queries.UpsertUserPreferences(s.db, &models.UserPreferences{
		UserID:        userID,
		SSHKeyMode:    sshKeyMode,
		TailscaleMode: tailscaleMode,
	})
}
