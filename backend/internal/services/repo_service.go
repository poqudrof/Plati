package services

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/jmoiron/sqlx"
	"golang.org/x/crypto/ssh"

	"github.com/homaserver/plati/internal/database/queries"
	"github.com/homaserver/plati/internal/models"
)

const settingKeyServerGitPrivateKey = "server_git_private_key"
const settingKeyServerGitManagedKeyID = "server_git_managed_key_id"

type RepoService struct {
	db      *sqlx.DB
	userSvc *UserService
	reposDir string
}

func NewRepoService(db *sqlx.DB, userSvc *UserService, reposDir string) *RepoService {
	return &RepoService{db: db, userSvc: userSvc, reposDir: reposDir}
}

// GenerateServerKey generates a new ed25519 keypair, stores the encrypted private key in settings,
// and returns the public key in authorized_keys format.
func (s *RepoService) GenerateServerKey() (string, error) {
	pubKey, privKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return "", fmt.Errorf("generate key: %w", err)
	}

	pemBlock, err := ssh.MarshalPrivateKey(privKey, "")
	if err != nil {
		return "", fmt.Errorf("marshal private key: %w", err)
	}
	privateKeyPEM := string(pem.EncodeToMemory(pemBlock))

	encrypted, err := s.userSvc.Encrypt(privateKeyPEM)
	if err != nil {
		return "", fmt.Errorf("encrypt private key: %w", err)
	}
	if err := queries.SetSetting(s.db, settingKeyServerGitPrivateKey, encrypted); err != nil {
		return "", fmt.Errorf("store private key: %w", err)
	}

	sshPub, err := ssh.NewPublicKey(pubKey)
	if err != nil {
		return "", fmt.Errorf("derive public key: %w", err)
	}
	publicKey := strings.TrimSpace(string(ssh.MarshalAuthorizedKey(sshPub)))
	return publicKey, nil
}

// SetServerManagedKey designates an existing managed SSH key as the server git key.
// Returns the key's public key string.
func (s *RepoService) SetServerManagedKey(managedKeyID int64) (string, error) {
	key, err := queries.GetManagedSSHKey(s.db, managedKeyID)
	if err != nil {
		return "", fmt.Errorf("managed key not found: %w", err)
	}
	if err := queries.SetSetting(s.db, settingKeyServerGitManagedKeyID, strconv.FormatInt(managedKeyID, 10)); err != nil {
		return "", fmt.Errorf("store managed key id: %w", err)
	}
	return strings.TrimSpace(key.PublicKey), nil
}

// GetServerManagedKeyID returns the ID of the managed key currently used as the server git key, or 0 if none.
func (s *RepoService) GetServerManagedKeyID() int64 {
	val, err := queries.GetSetting(s.db, settingKeyServerGitManagedKeyID)
	if err != nil || val == "" {
		return 0
	}
	id, _ := strconv.ParseInt(val, 10, 64)
	return id
}

// GetServerPublicKey returns the server's public key in authorized_keys format.
func (s *RepoService) GetServerPublicKey() (string, error) {
	// If a managed key is configured, return its stored public key directly.
	if id := s.GetServerManagedKeyID(); id > 0 {
		key, err := queries.GetManagedSSHKey(s.db, id)
		if err != nil {
			return "", fmt.Errorf("load managed key: %w", err)
		}
		return strings.TrimSpace(key.PublicKey), nil
	}
	privPEM, err := s.getServerPrivateKey()
	if err != nil {
		// No key configured yet — not an error condition.
		return "", nil
	}
	key, err := ssh.ParsePrivateKey([]byte(privPEM))
	if err != nil {
		return "", fmt.Errorf("parse private key: %w", err)
	}
	return strings.TrimSpace(string(ssh.MarshalAuthorizedKey(key.PublicKey()))), nil
}

func (s *RepoService) getServerPrivateKey() (string, error) {
	if id := s.GetServerManagedKeyID(); id > 0 {
		key, err := queries.GetManagedSSHKey(s.db, id)
		if err != nil {
			return "", fmt.Errorf("load managed key %d: %w", id, err)
		}
		privPEM, err := s.userSvc.Decrypt(key.EncryptedPrivateKey)
		if err != nil {
			return "", fmt.Errorf("decrypt managed key: %w", err)
		}
		return privPEM, nil
	}
	encrypted, err := queries.GetSetting(s.db, settingKeyServerGitPrivateKey)
	if err != nil {
		return "", fmt.Errorf("no server git key configured: %w", err)
	}
	if encrypted == "" {
		return "", fmt.Errorf("no server git key configured")
	}
	privPEM, err := s.userSvc.Decrypt(encrypted)
	if err != nil {
		return "", fmt.Errorf("decrypt server git key: %w", err)
	}
	return privPEM, nil
}

func (s *RepoService) ListRepos() ([]models.GitRepo, error) {
	return queries.ListGitRepos(s.db)
}

// AddRepo registers a repo by SSH URL, derives its name, and starts cloning in the background.
func (s *RepoService) AddRepo(sshURL string) (*models.GitRepo, error) {
	name, err := nameFromSSHURL(sshURL)
	if err != nil {
		return nil, fmt.Errorf("invalid SSH URL: %w", err)
	}
	localPath := filepath.Join(s.reposDir, name)

	id, err := queries.CreateGitRepo(s.db, name, sshURL, localPath)
	if err != nil {
		return nil, fmt.Errorf("create repo record: %w", err)
	}

	repo, err := queries.GetGitRepo(s.db, id)
	if err != nil {
		return nil, err
	}

	go s.cloneRepo(id)

	return repo, nil
}

// SyncRepo runs a git pull for the given repo and returns the command and its output.
func (s *RepoService) SyncRepo(id int64) (command string, output string, err error) {
	repo, err := queries.GetGitRepo(s.db, id)
	if err != nil {
		return "", "", fmt.Errorf("repo not found: %w", err)
	}
	return s.gitPullSync(repo.ID)
}

// RenameRepo updates the display name of a repo.
func (s *RepoService) RenameRepo(id int64, name string) error {
	if name == "" {
		return fmt.Errorf("name is required")
	}
	return queries.RenameGitRepo(s.db, id, name)
}

// DeleteRepo removes the repo from the DB and deletes its local directory.
func (s *RepoService) DeleteRepo(id int64) error {
	repo, err := queries.GetGitRepo(s.db, id)
	if err != nil {
		return fmt.Errorf("repo not found: %w", err)
	}
	if err := queries.DeleteGitRepo(s.db, id); err != nil {
		return fmt.Errorf("delete repo record: %w", err)
	}
	if repo.LocalPath != "" {
		if err := os.RemoveAll(repo.LocalPath); err != nil {
			log.Printf("warning: remove repo dir %s: %v", repo.LocalPath, err)
		}
		if err := os.Remove(knownHostsPath(repo.LocalPath)); err != nil && !os.IsNotExist(err) {
			log.Printf("warning: remove known_hosts for %s: %v", repo.LocalPath, err)
		}
	}
	return nil
}

func (s *RepoService) cloneRepo(id int64) {
	repo, err := queries.GetGitRepo(s.db, id)
	if err != nil {
		log.Printf("cloneRepo: get repo %d: %v", id, err)
		return
	}

	queries.UpdateGitRepoStatus(s.db, id, "cloning", "", nil)

	privPEM, err := s.getServerPrivateKey()
	if err != nil {
		log.Printf("cloneRepo: get server key: %v", err)
		queries.UpdateGitRepoStatus(s.db, id, "error", err.Error(), nil)
		return
	}

	keyFile, err := writeTempKey(privPEM)
	if err != nil {
		log.Printf("cloneRepo: write temp key: %v", err)
		queries.UpdateGitRepoStatus(s.db, id, "error", err.Error(), nil)
		return
	}
	defer os.Remove(keyFile)

	khPath, err := ensureKnownHosts(repo.SSHURL, repo.LocalPath)
	if err != nil {
		log.Printf("cloneRepo: known_hosts for %s: %v", repo.Name, err)
		queries.UpdateGitRepoStatus(s.db, id, "error", err.Error(), nil)
		return
	}

	cmd := exec.Command("git", "clone", repo.SSHURL, repo.LocalPath)
	cmd.Env = gitSSHEnv(keyFile, khPath)

	out, err := cmd.CombinedOutput()
	if err != nil {
		msg := fmt.Sprintf("clone failed: %v\n%s", err, string(out))
		log.Printf("cloneRepo %s: %s", repo.Name, msg)
		queries.UpdateGitRepoStatus(s.db, id, "error", msg, nil)
		return
	}

	now := time.Now()
	queries.UpdateGitRepoStatus(s.db, id, "ready", "", &now)
	log.Printf("cloneRepo %s: done", repo.Name)
}

// gitPullSync runs git pull synchronously and returns the command string and output.
func (s *RepoService) gitPullSync(id int64) (command string, output string, err error) {
	repo, err := queries.GetGitRepo(s.db, id)
	if err != nil {
		return "", "", fmt.Errorf("get repo %d: %w", id, err)
	}

	privPEM, err := s.getServerPrivateKey()
	if err != nil {
		queries.UpdateGitRepoStatus(s.db, id, "error", err.Error(), nil)
		return "", "", fmt.Errorf("get server key: %w", err)
	}

	keyFile, err := writeTempKey(privPEM)
	if err != nil {
		queries.UpdateGitRepoStatus(s.db, id, "error", err.Error(), nil)
		return "", "", fmt.Errorf("write temp key: %w", err)
	}
	defer os.Remove(keyFile)

	khPath, err := ensureKnownHosts(repo.SSHURL, repo.LocalPath)
	if err != nil {
		queries.UpdateGitRepoStatus(s.db, id, "error", err.Error(), nil)
		return "", "", fmt.Errorf("known_hosts: %w", err)
	}

	cmdStr := fmt.Sprintf("git -C %s pull --ff-only", repo.LocalPath)
	cmd := exec.Command("git", "-C", repo.LocalPath, "pull", "--ff-only")
	cmd.Env = gitSSHEnv(keyFile, khPath)

	out, cmdErr := cmd.CombinedOutput()
	outStr := strings.TrimSpace(string(out))

	if cmdErr != nil {
		msg := fmt.Sprintf("pull failed: %v\n%s", cmdErr, outStr)
		log.Printf("gitPullSync %s: %s", repo.Name, msg)
		queries.UpdateGitRepoStatus(s.db, id, "error", msg, nil)
		return cmdStr, outStr, fmt.Errorf("pull failed: %w", cmdErr)
	}

	now := time.Now()
	queries.UpdateGitRepoStatus(s.db, id, "ready", "", &now)
	log.Printf("gitPullSync %s: done", repo.Name)
	return cmdStr, outStr, nil
}

func (s *RepoService) gitPull(id int64) {
	repo, err := queries.GetGitRepo(s.db, id)
	if err != nil {
		log.Printf("gitPull: get repo %d: %v", id, err)
		return
	}

	privPEM, err := s.getServerPrivateKey()
	if err != nil {
		log.Printf("gitPull: get server key: %v", err)
		queries.UpdateGitRepoStatus(s.db, id, "error", err.Error(), nil)
		return
	}

	keyFile, err := writeTempKey(privPEM)
	if err != nil {
		log.Printf("gitPull: write temp key: %v", err)
		queries.UpdateGitRepoStatus(s.db, id, "error", err.Error(), nil)
		return
	}
	defer os.Remove(keyFile)

	khPath, err := ensureKnownHosts(repo.SSHURL, repo.LocalPath)
	if err != nil {
		log.Printf("gitPull: known_hosts for %s: %v", repo.Name, err)
		queries.UpdateGitRepoStatus(s.db, id, "error", err.Error(), nil)
		return
	}

	cmd := exec.Command("git", "-C", repo.LocalPath, "pull", "--ff-only")
	cmd.Env = gitSSHEnv(keyFile, khPath)

	out, err := cmd.CombinedOutput()
	if err != nil {
		msg := fmt.Sprintf("pull failed: %v\n%s", err, string(out))
		log.Printf("gitPull %s: %s", repo.Name, msg)
		queries.UpdateGitRepoStatus(s.db, id, "error", msg, nil)
		return
	}

	now := time.Now()
	queries.UpdateGitRepoStatus(s.db, id, "ready", "", &now)
	log.Printf("gitPull %s: done", repo.Name)
}

// nameFromSSHURL derives a short name from a git SSH URL.
// "git@github.com:org/repo.git" → "repo"
// Returns an error if the URL contains path-traversal sequences or if the
// derived name is empty or contains unsafe characters.
func nameFromSSHURL(url string) (string, error) {
	// Reject any URL that contains ".." path-traversal sequences before
	// extracting the final path component. This prevents URLs like
	// "git@github.com:org/../../../etc/evil" from resolving to a safe-looking name.
	for _, seg := range strings.FieldsFunc(url, func(r rune) bool { return r == '/' || r == ':' }) {
		if seg == ".." {
			return "", fmt.Errorf("SSH URL %q contains path traversal", url)
		}
	}

	url = strings.TrimSuffix(url, ".git")
	name := url
	for i := len(url) - 1; i >= 0; i-- {
		if url[i] == '/' || url[i] == ':' {
			name = url[i+1:]
			break
		}
	}
	if name == "" || name == "." || name == ".." {
		return "", fmt.Errorf("cannot derive safe repo name from URL %q", url)
	}
	for _, c := range name {
		if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '-' || c == '_' || c == '.') {
			return "", fmt.Errorf("repo name %q contains unsafe character %q", name, c)
		}
	}
	return name, nil
}

func writeTempKey(privPEM string) (string, error) {
	f, err := os.CreateTemp("", "plati-git-key-*")
	if err != nil {
		return "", err
	}
	if _, err := f.WriteString(privPEM); err != nil {
		f.Close()
		os.Remove(f.Name())
		return "", err
	}
	if err := f.Chmod(0600); err != nil {
		f.Close()
		os.Remove(f.Name())
		return "", err
	}
	f.Close()
	return f.Name(), nil
}

// hostFromSSHURL extracts the hostname from a git SSH URL.
// "git@github.com:org/repo.git" → "github.com"
func hostFromSSHURL(sshURL string) string {
	if idx := strings.Index(sshURL, "@"); idx >= 0 {
		rest := sshURL[idx+1:]
		if idx2 := strings.Index(rest, ":"); idx2 >= 0 {
			return rest[:idx2]
		}
	}
	return ""
}

// knownHostsPath returns the path of the known_hosts file stored alongside a repo directory.
func knownHostsPath(localPath string) string {
	return localPath + ".known_hosts"
}

// sshKeyscan fetches the host keys for host and returns known_hosts-format content.
func sshKeyscan(host string) (string, error) {
	if host == "" {
		return "", fmt.Errorf("cannot determine host from SSH URL")
	}
	out, err := exec.Command("ssh-keyscan", "-H", host).Output()
	if err != nil {
		return "", fmt.Errorf("ssh-keyscan %s: %w", host, err)
	}
	content := strings.TrimSpace(string(out))
	if content == "" {
		return "", fmt.Errorf("ssh-keyscan %s: empty output", host)
	}
	return content, nil
}

// ensureKnownHosts returns the path to a valid known_hosts file for sshURL.
// If the file already exists it is reused; otherwise ssh-keyscan populates it.
func ensureKnownHosts(sshURL, localPath string) (string, error) {
	khPath := knownHostsPath(localPath)
	if _, err := os.Stat(khPath); err == nil {
		return khPath, nil
	}
	host := hostFromSSHURL(sshURL)
	content, err := sshKeyscan(host)
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(khPath, []byte(content+"\n"), 0600); err != nil {
		return "", fmt.Errorf("write known_hosts: %w", err)
	}
	return khPath, nil
}

// gitSSHEnv returns the GIT_SSH_COMMAND environment using strict host-key checking
// against the provided known_hosts file.
func gitSSHEnv(keyFile, knownHostsFile string) []string {
	sshCmd := fmt.Sprintf("ssh -i %s -o StrictHostKeyChecking=yes -o UserKnownHostsFile=%s", keyFile, knownHostsFile)
	return append(os.Environ(), "GIT_SSH_COMMAND="+sshCmd)
}
