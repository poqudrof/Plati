package services

import (
	"database/sql"
	"errors"
	"fmt"

	"github.com/jmoiron/sqlx"

	"github.com/homaserver/plati/internal/database/queries"
)

const settingKeyPlatformTailscaleKey = "platform_tailscale_auth_key"

type AdminSettingsService struct {
	db      *sqlx.DB
	userSvc *UserService
}

func NewAdminSettingsService(db *sqlx.DB, userSvc *UserService) *AdminSettingsService {
	return &AdminSettingsService{db: db, userSvc: userSvc}
}

// GetPlatformTailscaleKey returns the decrypted platform Tailscale auth key, or "" if unset.
func (s *AdminSettingsService) GetPlatformTailscaleKey() (string, error) {
	encrypted, err := queries.GetSetting(s.db, settingKeyPlatformTailscaleKey)
	if errors.Is(err, sql.ErrNoRows) || encrypted == "" {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("get setting: %w", err)
	}
	plaintext, err := s.userSvc.Decrypt(encrypted)
	if err != nil {
		return "", fmt.Errorf("decrypt platform tailscale key: %w", err)
	}
	return plaintext, nil
}

// SetPlatformTailscaleKey encrypts and stores the platform Tailscale auth key.
func (s *AdminSettingsService) SetPlatformTailscaleKey(plaintext string) error {
	encrypted, err := s.userSvc.Encrypt(plaintext)
	if err != nil {
		return fmt.Errorf("encrypt platform tailscale key: %w", err)
	}
	return queries.SetSetting(s.db, settingKeyPlatformTailscaleKey, encrypted)
}

// DeletePlatformTailscaleKey removes the platform Tailscale auth key.
func (s *AdminSettingsService) DeletePlatformTailscaleKey() error {
	return queries.DeleteSetting(s.db, settingKeyPlatformTailscaleKey)
}
