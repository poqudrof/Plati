package services

import (
	"fmt"

	"github.com/jmoiron/sqlx"

	"github.com/homaserver/plati/internal/database/queries"
	"github.com/homaserver/plati/internal/models"
)

type ManagedKeyService struct {
	db      *sqlx.DB
	userSvc *UserService
}

func NewManagedKeyService(db *sqlx.DB, userSvc *UserService) *ManagedKeyService {
	return &ManagedKeyService{db: db, userSvc: userSvc}
}

// GenerateManagedKey creates a new admin-managed keypair.
// Returns the key metadata and the private key PEM (shown once to the admin).
func (s *ManagedKeyService) GenerateManagedKey(name string) (*models.ManagedSSHKey, string, error) {
	privateKeyPEM, publicKey, err := generateSSHKeypair()
	if err != nil {
		return nil, "", fmt.Errorf("generate keypair: %w", err)
	}
	encryptedPrivKey, err := s.userSvc.Encrypt(privateKeyPEM)
	if err != nil {
		return nil, "", fmt.Errorf("encrypt private key: %w", err)
	}
	id, err := queries.CreateManagedSSHKey(s.db, name, publicKey, encryptedPrivKey)
	if err != nil {
		return nil, "", fmt.Errorf("save managed key: %w", err)
	}
	key := &models.ManagedSSHKey{
		ID:        id,
		Name:      name,
		PublicKey: publicKey,
	}
	return key, privateKeyPEM, nil
}

func (s *ManagedKeyService) ListManagedKeys() ([]models.ManagedSSHKey, error) {
	return queries.ListManagedSSHKeys(s.db)
}

func (s *ManagedKeyService) DeleteManagedKey(id int64) error {
	return queries.DeleteManagedSSHKey(s.db, id)
}

func (s *ManagedKeyService) GetManagedKeyUsers(keyID int64) ([]int64, error) {
	return queries.GetManagedKeyUsers(s.db, keyID)
}

func (s *ManagedKeyService) SetManagedKeyUsers(keyID int64, userIDs []int64) error {
	// Verify key exists
	if _, err := queries.GetManagedSSHKey(s.db, keyID); err != nil {
		return fmt.Errorf("managed key not found: %w", err)
	}
	return queries.SetManagedKeyUsers(s.db, keyID, userIDs)
}
