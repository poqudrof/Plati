package services

import (
	"encoding/json"
	"fmt"
	"log"
	"strings"

	"github.com/jmoiron/sqlx"

	"github.com/homaserver/plati/internal/database/queries"
	"github.com/homaserver/plati/internal/incus"
	"github.com/homaserver/plati/internal/models"
)

type InstanceService struct {
	db      *sqlx.DB
	pool    *incus.Pool
	userSvc *UserService
}

func NewInstanceService(db *sqlx.DB, pool *incus.Pool, userSvc *UserService) *InstanceService {
	return &InstanceService{db: db, pool: pool, userSvc: userSvc}
}

type CreateInstanceRequest struct {
	Name       string `json:"name"`
	TemplateID int64  `json:"template_id"`
	UserID     int64  `json:"-"`
}

// buildSecretsEnv decrypts global user secrets and (optionally) instance-specific secrets,
// returning a merged map of env var name → plaintext value.
// Instance secrets override global ones with the same name.
func (s *InstanceService) buildSecretsEnv(userID, instanceID int64) map[string]string {
	env := map[string]string{}

	globalSecrets, err := queries.ListSecretsForInjection(s.db, userID)
	if err != nil {
		log.Printf("warning: list global secrets for injection: %v", err)
	}
	for _, sec := range globalSecrets {
		val, err := s.userSvc.Decrypt(sec.EncryptedValue)
		if err != nil {
			log.Printf("warning: decrypt global secret %s: %v", sec.Name, err)
			continue
		}
		env[sec.Name] = val
	}

	if instanceID > 0 {
		instanceSecrets, err := queries.ListInstanceSecretsForInjection(s.db, instanceID)
		if err != nil {
			log.Printf("warning: list instance secrets for injection: %v", err)
		}
		for _, sec := range instanceSecrets {
			val, err := s.userSvc.Decrypt(sec.EncryptedValue)
			if err != nil {
				log.Printf("warning: decrypt instance secret %s: %v", sec.Name, err)
				continue
			}
			env[sec.Name] = val
		}
	}

	return env
}

func (s *InstanceService) Create(req CreateInstanceRequest) (*models.Instance, error) {
	// Get template
	tmpl, err := queries.GetTemplate(s.db, req.TemplateID)
	if err != nil {
		return nil, fmt.Errorf("template not found: %w", err)
	}

	// Select server with capacity
	server, err := s.selectServer()
	if err != nil {
		return nil, fmt.Errorf("no available server: %w", err)
	}

	client, err := s.pool.GetClient(server.Name)
	if err != nil {
		return nil, fmt.Errorf("get incus client: %w", err)
	}

	// Parse template resources
	var resources map[string]string
	if err := json.Unmarshal([]byte(tmpl.Resources), &resources); err != nil {
		resources = map[string]string{}
	}

	// Parse profiles
	var profiles []string
	if err := json.Unmarshal([]byte(tmpl.Profiles), &profiles); err != nil {
		profiles = []string{"default"}
	}

	// Collect SSH keys for this user
	publicKeys, privateKeys := s.collectSSHKeys(req.UserID)

	// Generate unique Incus name
	incusName := fmt.Sprintf("plati-%d-%s", req.UserID, sanitizeName(req.Name))

	// Build config
	config := incus.BuildInstanceConfig(tmpl.CloudInit, publicKeys, privateKeys, resources)

	// Inject global secrets as environment variables (instanceID=0 for new instance)
	for k, v := range s.buildSecretsEnv(req.UserID, 0) {
		config["environment."+k] = v
	}

	// Create workspace volume
	diskSize := 20
	if sizeStr, ok := resources["disk"]; ok {
		fmt.Sscanf(sizeStr, "%dGB", &diskSize)
	}
	volName := incusName + "-workspace"

	volID, err := queries.CreateVolume(s.db, volName, server.ID, "default", diskSize)
	if err != nil {
		return nil, fmt.Errorf("create volume record: %w", err)
	}

	// Create instance in Incus
	if err := client.CreateInstance(incusName, tmpl.Image, profiles, config, nil); err != nil {
		queries.DeleteVolume(s.db, volID)
		return nil, fmt.Errorf("create incus instance: %w", err)
	}

	// Create volume in Incus and attach
	if err := incus.CreateAndAttachVolume(client, "default", volName, incusName, "/workspace", diskSize); err != nil {
		log.Printf("warning: volume attach failed: %v", err)
	}

	// Start instance
	if err := client.StartInstance(incusName); err != nil {
		return nil, fmt.Errorf("start instance: %w", err)
	}

	// Get IP
	ip, _ := incus.GetInstanceIP(client, incusName)

	// Save to DB
	inst := &models.Instance{
		Name:       req.Name,
		UserID:     req.UserID,
		TemplateID: req.TemplateID,
		ServerID:   server.ID,
		IncusName:  incusName,
		Status:     "running",
	}
	if volID > 0 {
		inst.VolumeID.Valid = true
		inst.VolumeID.Int64 = volID
	}

	instID, err := queries.CreateInstance(s.db, inst)
	if err != nil {
		return nil, fmt.Errorf("save instance: %w", err)
	}
	inst.ID = instID
	queries.UpdateInstanceLastActive(s.db, instID)

	if ip != "" {
		inst.IPAddress.Valid = true
		inst.IPAddress.String = ip
		queries.UpdateInstanceIP(s.db, instID, ip)
	}

	return inst, nil
}

func (s *InstanceService) Start(id, userID int64) error {
	inst, err := queries.GetInstanceByUser(s.db, id, userID)
	if err != nil {
		return fmt.Errorf("instance not found: %w", err)
	}

	server, err := queries.GetServer(s.db, inst.ServerID)
	if err != nil {
		return err
	}

	client, err := s.pool.GetClient(server.Name)
	if err != nil {
		return err
	}

	if err := client.StartInstance(inst.IncusName); err != nil {
		return fmt.Errorf("start: %w", err)
	}

	queries.UpdateInstanceStatus(s.db, id, "running")
	queries.UpdateInstanceLastActive(s.db, id)

	// Update IP
	if ip, err := incus.GetInstanceIP(client, inst.IncusName); err == nil {
		queries.UpdateInstanceIP(s.db, id, ip)
	}

	return nil
}

func (s *InstanceService) Stop(id, userID int64) error {
	inst, err := queries.GetInstanceByUser(s.db, id, userID)
	if err != nil {
		return fmt.Errorf("instance not found: %w", err)
	}

	server, err := queries.GetServer(s.db, inst.ServerID)
	if err != nil {
		return err
	}

	client, err := s.pool.GetClient(server.Name)
	if err != nil {
		return err
	}

	if err := client.StopInstance(inst.IncusName); err != nil {
		return fmt.Errorf("stop: %w", err)
	}

	queries.UpdateInstanceStatus(s.db, id, "stopped")
	return nil
}

func (s *InstanceService) Rebuild(id, userID int64) error {
	inst, err := queries.GetInstanceByUser(s.db, id, userID)
	if err != nil {
		return fmt.Errorf("instance not found: %w", err)
	}

	server, err := queries.GetServer(s.db, inst.ServerID)
	if err != nil {
		return err
	}

	client, err := s.pool.GetClient(server.Name)
	if err != nil {
		return err
	}

	tmpl, err := queries.GetTemplate(s.db, inst.TemplateID)
	if err != nil {
		return err
	}

	// Stop instance
	_ = client.StopInstance(inst.IncusName)

	// Detach volume
	_ = client.DetachVolume(inst.IncusName, "workspace")

	// Delete instance
	if err := client.DeleteInstance(inst.IncusName); err != nil {
		return fmt.Errorf("delete for rebuild: %w", err)
	}

	// Parse template data
	var resources map[string]string
	json.Unmarshal([]byte(tmpl.Resources), &resources)
	var profiles []string
	if err := json.Unmarshal([]byte(tmpl.Profiles), &profiles); err != nil {
		profiles = []string{"default"}
	}
	publicKeys, privateKeys := s.collectSSHKeys(userID)
	config := incus.BuildInstanceConfig(tmpl.CloudInit, publicKeys, privateKeys, resources)

	// Inject global + instance-specific secrets
	for k, v := range s.buildSecretsEnv(userID, id) {
		config["environment."+k] = v
	}

	// Recreate instance
	if err := client.CreateInstance(inst.IncusName, tmpl.Image, profiles, config, nil); err != nil {
		queries.UpdateInstanceStatus(s.db, id, "error")
		return fmt.Errorf("recreate instance: %w", err)
	}

	// Reattach volume
	if inst.VolumeID.Valid {
		vol, err := queries.GetVolume(s.db, inst.VolumeID.Int64)
		if err == nil {
			_ = client.AttachVolume("default", vol.Name, inst.IncusName, "workspace", "/workspace")
		}
	}

	// Start
	if err := client.StartInstance(inst.IncusName); err != nil {
		queries.UpdateInstanceStatus(s.db, id, "error")
		return fmt.Errorf("start after rebuild: %w", err)
	}

	queries.UpdateInstanceStatus(s.db, id, "running")
	if ip, err := incus.GetInstanceIP(client, inst.IncusName); err == nil {
		queries.UpdateInstanceIP(s.db, id, ip)
	}

	return nil
}

func (s *InstanceService) Delete(id, userID int64) error {
	inst, err := queries.GetInstanceByUser(s.db, id, userID)
	if err != nil {
		return fmt.Errorf("instance not found: %w", err)
	}

	server, err := queries.GetServer(s.db, inst.ServerID)
	if err != nil {
		return err
	}

	client, err := s.pool.GetClient(server.Name)
	if err != nil {
		return err
	}

	// Stop if running
	_ = client.StopInstance(inst.IncusName)

	// Delete instance from DB first (removes FK reference to volume)
	if err := queries.DeleteInstance(s.db, id); err != nil {
		return fmt.Errorf("delete instance from DB: %w", err)
	}

	// Delete volume (now safe since instance FK is gone)
	if inst.VolumeID.Valid {
		vol, err := queries.GetVolume(s.db, inst.VolumeID.Int64)
		if err == nil {
			_ = client.DetachVolume(inst.IncusName, "workspace")
			_ = client.DeleteVolume("default", vol.Name)
			if err := queries.DeleteVolume(s.db, vol.ID); err != nil {
				log.Printf("warning: failed to delete volume %d from DB: %v", vol.ID, err)
			}
		}
	}

	// Delete instance in Incus
	if err := client.DeleteInstance(inst.IncusName); err != nil {
		log.Printf("warning: failed to delete incus instance %s: %v", inst.IncusName, err)
	}

	return nil
}

// Instance secrets management

func (s *InstanceService) ListInstanceSecrets(instanceID, userID int64) ([]models.InstanceSecret, error) {
	inst, err := queries.GetInstanceByUser(s.db, instanceID, userID)
	if err != nil {
		return nil, fmt.Errorf("instance not found: %w", err)
	}
	return queries.ListInstanceSecrets(s.db, inst.ID)
}

func (s *InstanceService) CreateInstanceSecret(instanceID, userID int64, name, value string) (int64, error) {
	inst, err := queries.GetInstanceByUser(s.db, instanceID, userID)
	if err != nil {
		return 0, fmt.Errorf("instance not found: %w", err)
	}
	encrypted, err := s.userSvc.Encrypt(value)
	if err != nil {
		return 0, fmt.Errorf("encrypt secret: %w", err)
	}
	return queries.CreateInstanceSecret(s.db, inst.ID, name, encrypted)
}

func (s *InstanceService) UpdateInstanceSecret(id, instanceID, userID int64, value string) error {
	inst, err := queries.GetInstanceByUser(s.db, instanceID, userID)
	if err != nil {
		return fmt.Errorf("instance not found: %w", err)
	}
	encrypted, err := s.userSvc.Encrypt(value)
	if err != nil {
		return fmt.Errorf("encrypt secret: %w", err)
	}
	return queries.UpdateInstanceSecret(s.db, id, inst.ID, encrypted)
}

func (s *InstanceService) DeleteInstanceSecret(id, instanceID, userID int64) error {
	inst, err := queries.GetInstanceByUser(s.db, instanceID, userID)
	if err != nil {
		return fmt.Errorf("instance not found: %w", err)
	}
	return queries.DeleteInstanceSecret(s.db, id, inst.ID)
}

// GetIncusDetail returns live Incus state for an instance (admin use).
func (s *InstanceService) GetIncusDetail(instanceID int64) (*incus.IncusDetail, error) {
	inst, err := queries.GetInstance(s.db, instanceID)
	if err != nil {
		return nil, fmt.Errorf("instance not found: %w", err)
	}
	server, err := queries.GetServer(s.db, inst.ServerID)
	if err != nil {
		return nil, fmt.Errorf("server not found: %w", err)
	}
	client, err := s.pool.GetClient(server.Name)
	if err != nil {
		return nil, fmt.Errorf("get incus client: %w", err)
	}
	return incus.GetIncusDetail(client, inst.IncusName)
}

// collectSSHKeys gathers all SSH keys for a user:
//   - their Plati-generated user key (if any)
//   - any managed keys assigned to them by an admin
//
// Returns public keys (for ssh_authorized_keys) and decrypted private key PEMs
// (for write_files injection into the instance).
func (s *InstanceService) collectSSHKeys(userID int64) (publicKeys []string, privateKeys []string) {
	userKeys, _ := queries.ListUserSSHKeysForInjection(s.db, userID)
	for _, k := range userKeys {
		publicKeys = append(publicKeys, k.PublicKey)
		if priv, err := s.userSvc.Decrypt(k.EncryptedPrivateKey); err == nil {
			privateKeys = append(privateKeys, priv)
		}
	}

	managedKeys, _ := queries.GetManagedKeysForUser(s.db, userID)
	for _, k := range managedKeys {
		publicKeys = append(publicKeys, k.PublicKey)
		if priv, err := s.userSvc.Decrypt(k.EncryptedPrivateKey); err == nil {
			privateKeys = append(privateKeys, priv)
		}
	}
	return
}

func (s *InstanceService) selectServer() (*models.Server, error) {
	servers, err := queries.ListServers(s.db)
	if err != nil {
		return nil, err
	}

	for _, srv := range servers {
		if !srv.IsOnline {
			continue
		}
		count, err := queries.CountInstancesOnServer(s.db, srv.ID)
		if err != nil {
			continue
		}
		if count < srv.MaxInstances {
			return &srv, nil
		}
	}

	return nil, fmt.Errorf("no server with available capacity")
}

func sanitizeName(name string) string {
	name = strings.ToLower(name)
	name = strings.ReplaceAll(name, " ", "-")
	// Keep only alphanumeric and hyphens
	var result strings.Builder
	for _, c := range name {
		if (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '-' {
			result.WriteRune(c)
		}
	}
	s := result.String()
	if len(s) > 40 {
		s = s[:40]
	}
	return s
}
