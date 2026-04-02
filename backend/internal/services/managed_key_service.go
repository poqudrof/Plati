package services

import (
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/jmoiron/sqlx"

	"github.com/homaserver/plati/internal/database/queries"
	"github.com/homaserver/plati/internal/models"
)

type ManagedKeyService struct {
	db      *sqlx.DB
	userSvc *UserService
	keysDir string
}

func NewManagedKeyService(db *sqlx.DB, userSvc *UserService, keysDir string) *ManagedKeyService {
	return &ManagedKeyService{db: db, userSvc: userSvc, keysDir: keysDir}
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
	if s.keysDir != "" {
		dir := filepath.Join(s.keysDir, fmt.Sprintf("managed_%d", id))
		if err := os.MkdirAll(dir, 0700); err != nil {
			log.Printf("warning: create managed key dir %s: %v", dir, err)
		} else {
			os.WriteFile(filepath.Join(dir, "id_rsa"), []byte(privateKeyPEM), 0600)
			os.WriteFile(filepath.Join(dir, "id_rsa.pub"), []byte(publicKey), 0644)
		}
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
	if err := queries.DeleteManagedSSHKey(s.db, id); err != nil {
		return err
	}
	if s.keysDir != "" {
		os.RemoveAll(filepath.Join(s.keysDir, fmt.Sprintf("managed_%d", id)))
	}
	return nil
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
