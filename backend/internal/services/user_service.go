package services

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"encoding/pem"
	"fmt"
	"io"
	"strings"

	"github.com/jmoiron/sqlx"
	"golang.org/x/crypto/ssh"

	"github.com/homaserver/plati/internal/database/queries"
	"github.com/homaserver/plati/internal/models"
)

// generateSSHKeypair creates a new ed25519 keypair.
// Returns (privateKeyPEM, publicKeyAuthorizedKeys).
func generateSSHKeypair() (string, string, error) {
	pubKey, privKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return "", "", err
	}
	pemBlock, err := ssh.MarshalPrivateKey(privKey, "")
	if err != nil {
		return "", "", err
	}
	privateKeyPEM := string(pem.EncodeToMemory(pemBlock))

	sshPubKey, err := ssh.NewPublicKey(pubKey)
	if err != nil {
		return "", "", err
	}
	publicKey := strings.TrimSpace(string(ssh.MarshalAuthorizedKey(sshPubKey)))
	return privateKeyPEM, publicKey, nil
}

type UserService struct {
	db            *sqlx.DB
	encryptionKey []byte
}

func NewUserService(db *sqlx.DB, encryptionKeyHex string) (*UserService, error) {
	key, err := hex.DecodeString(encryptionKeyHex)
	if err != nil || len(key) != 32 {
		// Use a zero key if not configured (dev mode)
		key = make([]byte, 32)
	}
	return &UserService{db: db, encryptionKey: key}, nil
}

func (s *UserService) GetUser(id int64) (*models.User, error) {
	return queries.GetUserByID(s.db, id)
}

func (s *UserService) ListUsers() ([]models.User, error) {
	return queries.ListUsers(s.db)
}

func (s *UserService) UpdateUser(id int64, name, role string) error {
	return queries.UpdateUser(s.db, id, name, role)
}

func (s *UserService) DeleteUser(id int64) error {
	return queries.DeleteUser(s.db, id)
}

// Plati-generated SSH Keys

// GenerateUserSSHKey creates a new ed25519 keypair for the user.
// Returns the full key (including the private key PEM, shown once to the user).
func (s *UserService) GenerateUserSSHKey(userID int64, name string) (*models.UserSSHKey, string, error) {
	privateKeyPEM, publicKey, err := generateSSHKeypair()
	if err != nil {
		return nil, "", fmt.Errorf("generate keypair: %w", err)
	}
	encryptedPrivKey, err := s.Encrypt(privateKeyPEM)
	if err != nil {
		return nil, "", fmt.Errorf("encrypt private key: %w", err)
	}
	id, err := queries.CreateUserSSHKey(s.db, userID, name, publicKey, encryptedPrivKey)
	if err != nil {
		return nil, "", fmt.Errorf("save key: %w", err)
	}
	key := &models.UserSSHKey{
		ID:        id,
		UserID:    userID,
		Name:      name,
		PublicKey: publicKey,
	}
	return key, privateKeyPEM, nil
}

func (s *UserService) ListUserSSHKeys(userID int64) ([]models.UserSSHKey, error) {
	return queries.ListUserSSHKeys(s.db, userID)
}

func (s *UserService) DeleteUserSSHKey(id, userID int64) error {
	return queries.DeleteUserSSHKey(s.db, id, userID)
}

// Secrets (encrypted)
func (s *UserService) ListSecrets(userID int64) ([]models.Secret, error) {
	return queries.ListSecrets(s.db, userID)
}

func (s *UserService) CreateSecret(userID int64, name, value string) (int64, error) {
	encrypted, err := s.Encrypt(value)
	if err != nil {
		return 0, fmt.Errorf("encrypt secret: %w", err)
	}
	return queries.CreateSecret(s.db, userID, name, encrypted)
}

func (s *UserService) UpdateSecret(id, userID int64, value string) error {
	encrypted, err := s.Encrypt(value)
	if err != nil {
		return fmt.Errorf("encrypt secret: %w", err)
	}
	return queries.UpdateSecret(s.db, id, userID, encrypted)
}

func (s *UserService) DeleteSecret(id, userID int64) error {
	return queries.DeleteSecret(s.db, id, userID)
}

func (s *UserService) Encrypt(plaintext string) (string, error) {
	block, err := aes.NewCipher(s.encryptionKey)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	ciphertext := gcm.Seal(nonce, nonce, []byte(plaintext), nil)
	return hex.EncodeToString(ciphertext), nil
}

func (s *UserService) Decrypt(ciphertextHex string) (string, error) {
	ciphertext, err := hex.DecodeString(ciphertextHex)
	if err != nil {
		return "", err
	}
	block, err := aes.NewCipher(s.encryptionKey)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonceSize := gcm.NonceSize()
	if len(ciphertext) < nonceSize {
		return "", fmt.Errorf("ciphertext too short")
	}
	nonce, ciphertext := ciphertext[:nonceSize], ciphertext[nonceSize:]
	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", err
	}
	return string(plaintext), nil
}
