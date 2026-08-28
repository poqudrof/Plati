package services

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"

	"github.com/jmoiron/sqlx"

	"github.com/homaserver/plati/internal/auth"
	"github.com/homaserver/plati/internal/database/queries"
	"github.com/homaserver/plati/internal/models"
)

type APIKeyService struct {
	db *sqlx.DB
}

func NewAPIKeyService(db *sqlx.DB) *APIKeyService {
	return &APIKeyService{db: db}
}

func hashAPIKey(key string) string {
	sum := sha256.Sum256([]byte(key))
	return hex.EncodeToString(sum[:])
}

// Create generates a new API key for the user. The plaintext key is returned only
// once here; only its hash is persisted.
func (s *APIKeyService) Create(userID int64, name string) (plaintext string, key *models.APIKey, err error) {
	raw := make([]byte, 24)
	if _, err = rand.Read(raw); err != nil {
		return "", nil, fmt.Errorf("generate key: %w", err)
	}
	secret := hex.EncodeToString(raw)
	plaintext = "plati_" + secret
	prefix := plaintext[:12]
	hash := hashAPIKey(plaintext)

	id, err := queries.CreateAPIKey(s.db, userID, name, prefix, hash)
	if err != nil {
		return "", nil, err
	}
	key = &models.APIKey{ID: id, UserID: userID, Name: name, KeyPrefix: prefix}
	return plaintext, key, nil
}

func (s *APIKeyService) List(userID int64) ([]models.APIKey, error) {
	return queries.ListAPIKeysByUser(s.db, userID)
}

// Regenerate rotates the secret of an existing key owned by userID, keeping its id/name.
// The new plaintext is returned only here; only its hash is persisted.
func (s *APIKeyService) Regenerate(id, userID int64) (plaintext string, key *models.APIKey, err error) {
	raw := make([]byte, 24)
	if _, err = rand.Read(raw); err != nil {
		return "", nil, fmt.Errorf("generate key: %w", err)
	}
	secret := hex.EncodeToString(raw)
	plaintext = "plati_" + secret
	prefix := plaintext[:12]
	hash := hashAPIKey(plaintext)

	rows, err := queries.RegenerateAPIKey(s.db, id, userID, prefix, hash)
	if err != nil {
		return "", nil, err
	}
	if rows == 0 {
		return "", nil, fmt.Errorf("api key not found")
	}
	key, err = queries.GetAPIKeyByID(s.db, id)
	if err != nil {
		return "", nil, err
	}
	return plaintext, key, nil
}

// Authenticate looks up the user owning a valid, non-revoked API key.
func (s *APIKeyService) Authenticate(plaintext string) (*models.User, error) {
	hash := hashAPIKey(plaintext)
	key, err := queries.GetAPIKeyByHash(s.db, hash)
	if err != nil {
		return nil, err
	}
	user, err := queries.GetUserByID(s.db, key.UserID)
	if err != nil {
		return nil, err
	}
	_ = queries.TouchAPIKeyLastUsed(s.db, key.ID)
	return user, nil
}

// AsAuthenticator adapts the service to auth.APIKeyAuthenticator, used by AuthMiddleware.
func (s *APIKeyService) AsAuthenticator() auth.APIKeyAuthenticator {
	return apiKeyAuthenticator{s}
}

type apiKeyAuthenticator struct {
	svc *APIKeyService
}

func (a apiKeyAuthenticator) Authenticate(plaintext string) (*auth.ContextUser, error) {
	user, err := a.svc.Authenticate(plaintext)
	if err != nil {
		return nil, err
	}
	return &auth.ContextUser{ID: user.ID, Email: user.Email, Role: user.Role}, nil
}
