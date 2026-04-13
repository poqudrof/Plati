package services

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/jmoiron/sqlx"

	"github.com/homaserver/plati/internal/database/queries"
	"github.com/homaserver/plati/internal/incus"
	"github.com/homaserver/plati/internal/models"
)

type InstanceService struct {
	db           *sqlx.DB
	pool         *incus.Pool
	userSvc      *UserService
	prefSvc      *PreferencesService
	adminSvc     *AdminSettingsService
	repoSvc      *RepoService
	templateSvc  *TemplateService
	creationLogs *CreationLogManager
	keysDir      string
	reposDir     string
}

func NewInstanceService(db *sqlx.DB, pool *incus.Pool, userSvc *UserService, prefSvc *PreferencesService, adminSvc *AdminSettingsService, keysDir string, reposDir string, repoSvc *RepoService, templateSvc *TemplateService) *InstanceService {
	return &InstanceService{
		db:           db,
		pool:         pool,
		userSvc:      userSvc,
		prefSvc:      prefSvc,
		adminSvc:     adminSvc,
		repoSvc:      repoSvc,
		templateSvc:  templateSvc,
		creationLogs: NewCreationLogManager(),
		keysDir:      keysDir,
		reposDir:     reposDir,
	}
}

// CreationLogs returns the shared log manager so handlers can stream creation output.
func (s *InstanceService) CreationLogs() *CreationLogManager { return s.creationLogs }

type CreateInstanceRequest struct {
	Name                 string `json:"name"`
	TemplateID           int64  `json:"template_id"`
	UserID               int64  `json:"-"`
	SSHKeyModeOverride   string `json:"ssh_key_mode,omitempty"`
	TailscaleModeOverride string `json:"tailscale_mode,omitempty"`
}

// persistenceDir is the parsed form of one element from template.persistence_dirs.
type persistenceDir struct {
	Path string `json:"path"`
	Size string `json:"size"`
	Pool string `json:"pool"`
}

// deviceNameFromPath derives an Incus device name from a mount path.
// e.g. "/workspace" → "workspace", "/home/ubuntu" → "home-ubuntu"
func deviceNameFromPath(path string) string {
	path = strings.TrimPrefix(path, "/")
	path = strings.ReplaceAll(path, "/", "-")
	if path == "" {
		return "vol"
	}
	return path
}

// buildSecretsEnv decrypts global user secrets and (optionally) instance-specific secrets,
// returning a merged map of env var name → plaintext value.
// Instance secrets override global ones with the same name.
// If tailscaleMode == "plati" and TAILSCALE_AUTH_KEY is not already present, injects the
// platform key from admin settings (if configured).
func (s *InstanceService) buildSecretsEnv(userID, instanceID int64, tailscaleMode string) map[string]string {
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

	// Inject platform Tailscale key if user hasn't provided one and mode is "plati".
	if tailscaleMode == "plati" {
		if _, ok := env["TAILSCALE_AUTH_KEY"]; !ok && s.adminSvc != nil {
			if platformKey, err := s.adminSvc.GetPlatformTailscaleKey(); err == nil && platformKey != "" {
				env["TAILSCALE_AUTH_KEY"] = platformKey
			}
		}
	}

	return env
}

// readPrivKey reads a private key from the host filesystem if keysDir is set,
// falling back to decrypting from the DB.
func (s *InstanceService) readPrivKey(subdir, encryptedValue string) string {
	if s.keysDir != "" {
		if pem, err := os.ReadFile(filepath.Join(s.keysDir, subdir, "id_rsa")); err == nil {
			return string(pem)
		}
	}
	pem, _ := s.userSvc.Decrypt(encryptedValue)
	return pem
}

// collectSSHKeysForMode collects public and private keys for a user.
// In "plati" mode, the admin-managed key(s) assigned to the user are injected so
// the instance can do git/SSH operations automatically via the org key.
// In "personal" mode, the user's own personal keypair (from user_ssh_keys) is injected.
func (s *InstanceService) collectSSHKeysForMode(userID int64, mode string) (publicKeys []string, privateKeys []string) {
	// User-provided public keys (always included in both modes).
	sshKeys, _ := queries.ListSSHKeys(s.db, userID)
	for _, k := range sshKeys {
		publicKeys = append(publicKeys, k.PublicKey)
	}

	// User's personal keypair — injected in "personal" mode.
	userKeys, _ := queries.ListUserSSHKeysForInjection(s.db, userID)
	for _, k := range userKeys {
		publicKeys = append(publicKeys, k.PublicKey)
		if mode == "personal" {
			if priv := s.readPrivKey(fmt.Sprintf("user_%d", k.UserID), k.EncryptedPrivateKey); priv != "" {
				privateKeys = append(privateKeys, priv)
			}
		}
	}

	// Admin-managed shared keypairs — injected in "plati" mode.
	managedKeys, _ := queries.GetManagedKeysForUser(s.db, userID)
	for _, k := range managedKeys {
		publicKeys = append(publicKeys, k.PublicKey)
		if mode == "plati" {
			if priv := s.readPrivKey(fmt.Sprintf("managed_%d", k.ID), k.EncryptedPrivateKey); priv != "" {
				privateKeys = append(privateKeys, priv)
			}
		}
	}
	return
}

// effectiveMode returns the override if non-empty, otherwise the stored preference.
func effectiveMode(override, pref string) string {
	if override != "" {
		return override
	}
	return pref
}

// mixinFileStepsForTemplate returns PushFile setup steps for all mixin files declared
// in the template's Includes field. Falls back to nil if templateSvc is unset.
func (s *InstanceService) mixinFileStepsForTemplate(includesJSON string) []incus.SetupStep {
	if s.templateSvc == nil || includesJSON == "" || includesJSON == "[]" {
		return nil
	}
	var names []string
	if err := json.Unmarshal([]byte(includesJSON), &names); err != nil || len(names) == 0 {
		return nil
	}
	return s.templateSvc.GetMixinFileSteps(names)
}

// attachReposDirAndBuildCmds binds /plati-repos into the instance (if the template has repos
// configured and reposDir is set), and returns repo copy commands to prepend to firstInitCmds.
func (s *InstanceService) attachReposDirAndBuildCmds(client incus.IncusClient, incusName string, reposJSON string) []string {
	if s.reposDir == "" || reposJSON == "" || reposJSON == "[]" {
		return nil
	}
	var repoRefs []models.TemplateRepoRef
	if err := json.Unmarshal([]byte(reposJSON), &repoRefs); err != nil || len(repoRefs) == 0 {
		return nil
	}

	if err := client.AttachHostPath(incusName, "plati-repos", s.reposDir, "/plati-repos"); err != nil {
		log.Printf("warning: attach repos dir to %s: %v", incusName, err)
	}

	var cmds []string
	for _, ref := range repoRefs {
		repo, err := queries.GetGitRepoByName(s.db, ref.Name)
		if err != nil || repo.CloneStatus != "ready" {
			// Fall back to filesystem: if the directory exists in reposDir, still copy it.
			dirPath := filepath.Join(s.reposDir, ref.Name)
			if info, statErr := os.Stat(dirPath); statErr == nil && info.IsDir() {
				log.Printf("repo %q not in DB, falling back to filesystem copy", ref.Name)
				cmds = append(cmds, fmt.Sprintf("cp -rp /plati-repos/%s %s", ref.Name, ref.Dest))
			} else {
				log.Printf("warning: repo %q not ready (status=%v), skipping copy command", ref.Name, err)
			}
			continue
		}
		cmds = append(cmds,
			fmt.Sprintf("cp -rp /plati-repos/%s %s", repo.Name, ref.Dest),
			fmt.Sprintf("cd %s && git remote set-url origin %s", ref.Dest, repo.SSHURL),
		)
	}
	return cmds
}

func (s *InstanceService) Create(req CreateInstanceRequest) (*models.Instance, error) {
	// Get template
	tmpl, err := queries.GetTemplate(s.db, req.TemplateID)
	if err != nil {
		return nil, fmt.Errorf("template not found: %w", err)
	}

	// Resolve user preferences
	prefs, _ := s.prefSvc.Get(req.UserID)
	sshMode := effectiveMode(req.SSHKeyModeOverride, prefs.SSHKeyMode)
	tsMode := effectiveMode(req.TailscaleModeOverride, prefs.TailscaleMode)

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
	publicKeys, privateKeys := s.collectSSHKeysForMode(req.UserID, sshMode)

	// Generate unique Incus name
	incusName := fmt.Sprintf("plati-%d-%s", req.UserID, sanitizeName(req.Name))

	// Build config
	config := incus.BuildInstanceConfig(resources)

	// Inject global secrets as environment variables (instanceID=0 for new instance)
	secretsEnv := s.buildSecretsEnv(req.UserID, 0, tsMode)
	secretsEnv["PLATI_TAILSCALE_HOSTNAME"] = sanitizeName(req.Name)
	for k, v := range secretsEnv {
		config["environment."+k] = v
	}

	// Create instance in Incus first (no volume yet)
	if err := client.CreateInstance(incusName, tmpl.Image, profiles, config, nil); err != nil {
		return nil, fmt.Errorf("create incus instance: %w", err)
	}

	// Parse persistence dirs from template
	var persistDirs []persistenceDir
	if tmpl.PersistenceDirs != "" && tmpl.PersistenceDirs != "[]" {
		json.Unmarshal([]byte(tmpl.PersistenceDirs), &persistDirs)
	}

	// If no persistence dirs configured, fall back to legacy single-volume from resources.disk
	if len(persistDirs) == 0 && tmpl.PersistenceMode != "ephemeral" {
		diskSize := 20
		if sizeStr, ok := resources["disk"]; ok {
			fmt.Sscanf(sizeStr, "%dGB", &diskSize)
		}
		persistDirs = []persistenceDir{{Path: "/workspace", Size: fmt.Sprintf("%dGB", diskSize), Pool: "default"}}
	}

	// Attach volumes based on persistence mode
	var firstVolID int64
	if tmpl.PersistenceMode != "ephemeral" {
		for _, dir := range persistDirs {
			pool := dir.Pool
			if pool == "" {
				pool = "default"
			}
			sizeGB := 20
			fmt.Sscanf(dir.Size, "%dGB", &sizeGB)
			if sizeGB == 0 {
				fmt.Sscanf(dir.Size, "%dGiB", &sizeGB)
			}

			devName := deviceNameFromPath(dir.Path)
			volName := incusName + "-" + devName

			volID, err := queries.CreateVolume(s.db, volName, server.ID, pool, sizeGB)
			if err != nil {
				log.Printf("warning: create volume record %s: %v", volName, err)
				continue
			}
			if firstVolID == 0 {
				firstVolID = volID
			}

			if err := incus.CreateAndAttachVolume(client, pool, volName, incusName, devName, dir.Path, sizeGB); err != nil {
				log.Printf("warning: volume attach failed for %s: %v", dir.Path, err)
				queries.DeleteVolume(s.db, volID)
				continue
			}
		}
	}

	// Start instance
	if err := client.StartInstance(incusName); err != nil {
		return nil, fmt.Errorf("start instance: %w", err)
	}

	// Get IP
	ip, _ := incus.GetInstanceIP(client, incusName)

	// Save to DB first so we have an instanceID for the join table
	inst := &models.Instance{
		Name:       req.Name,
		UserID:     req.UserID,
		TemplateID: req.TemplateID,
		ServerID:   server.ID,
		IncusName:  incusName,
		Status:     "running",
	}
	if firstVolID > 0 {
		inst.VolumeID.Valid = true
		inst.VolumeID.Int64 = firstVolID
	}

	instID, err := queries.CreateInstance(s.db, inst)
	if err != nil {
		return nil, fmt.Errorf("save instance: %w", err)
	}
	inst.ID = instID
	queries.UpdateInstanceLastActive(s.db, instID)

	// Record all volumes in join table
	if tmpl.PersistenceMode != "ephemeral" {
		for _, dir := range persistDirs {
			devName := deviceNameFromPath(dir.Path)
			volName := incusName + "-" + devName
			// Find the volume ID we just created
			if vol, err := queries.GetVolumeByName(s.db, volName); err == nil {
				queries.CreateInstanceVolume(s.db, instID, vol.ID, dir.Path, devName)
			}
		}
	}

	if ip != "" {
		inst.IPAddress.Valid = true
		inst.IPAddress.String = ip
		queries.UpdateInstanceIP(s.db, instID, ip)
	}

	// Determine first-init sentinel path (first persistence dir + /.plati-initialized)
	var sentinelPath string
	if len(persistDirs) > 0 && tmpl.PersistenceMode != "ephemeral" {
		sentinelPath = persistDirs[0].Path + "/.plati-initialized"
	}

	// Phase 2: exec-based setup
	var firstInitCmds, rebuildCmds []string
	json.Unmarshal([]byte(tmpl.FirstInitCommands), &firstInitCmds)
	json.Unmarshal([]byte(tmpl.RebuildCommands), &rebuildCmds)
	var postCmds []string
	json.Unmarshal([]byte(tmpl.PostCreateCommands), &postCmds)

	s.runPhase2Setup(client, incusName, publicKeys, privateKeys, secretsEnv, tmpl.TerminalUser, incus.SetupConfig{
		PublicKeys:     publicKeys,
		PrivateKeyPEMs: privateKeys,
		Secrets:        secretsEnv,
		TerminalUser:   tmpl.TerminalUser,
		IsFirstInit:    true,
		FirstInitCmds:  firstInitCmds,
		RebuildCmds:    rebuildCmds,
		PostCreateCmds: postCmds,
		SentinelPath:   sentinelPath,
		MixinFileSteps: s.mixinFileStepsForTemplate(tmpl.Includes),
	}, nil)

	return inst, nil
}

// CreateAsync creates the DB record immediately with status "creating", then
// performs all Incus operations in a background goroutine. Progress lines are
// streamed via CreationLogs() and the final log is persisted to instances.creation_log.
// Use this from HTTP handlers so the request returns instantly.
// The synchronous Create method is kept for internal callers (e.g. Duplicate).
func (s *InstanceService) CreateAsync(req CreateInstanceRequest) (*models.Instance, error) {
	// ── Fast synchronous validation ──

	tmpl, err := queries.GetTemplate(s.db, req.TemplateID)
	if err != nil {
		return nil, fmt.Errorf("template not found: %w", err)
	}

	prefs, _ := s.prefSvc.Get(req.UserID)
	sshMode := effectiveMode(req.SSHKeyModeOverride, prefs.SSHKeyMode)
	tsMode := effectiveMode(req.TailscaleModeOverride, prefs.TailscaleMode)

	server, err := s.selectServer()
	if err != nil {
		return nil, fmt.Errorf("no available server: %w", err)
	}

	client, err := s.pool.GetClient(server.Name)
	if err != nil {
		return nil, fmt.Errorf("get incus client: %w", err)
	}

	var resources map[string]string
	if err := json.Unmarshal([]byte(tmpl.Resources), &resources); err != nil {
		resources = map[string]string{}
	}
	var profiles []string
	if err := json.Unmarshal([]byte(tmpl.Profiles), &profiles); err != nil {
		profiles = []string{"default"}
	}

	publicKeys, privateKeys := s.collectSSHKeysForMode(req.UserID, sshMode)
	incusName := fmt.Sprintf("plati-%d-%s", req.UserID, sanitizeName(req.Name))

	// ── Create DB record immediately ──

	inst := &models.Instance{
		Name:       req.Name,
		UserID:     req.UserID,
		TemplateID: req.TemplateID,
		ServerID:   server.ID,
		IncusName:  incusName,
		Status:     "creating",
	}
	instID, err := queries.CreateInstance(s.db, inst)
	if err != nil {
		return nil, fmt.Errorf("save instance: %w", err)
	}
	inst.ID = instID

	// ── Background goroutine for slow Incus work ──

	go func() {
		var logLines []string
		logFn := func(line string) {
			logLines = append(logLines, line)
			s.creationLogs.Log(instID, line)
		}
		finalize := func(status string) {
			queries.UpdateInstanceStatus(s.db, instID, status)
			queries.UpdateInstanceCreationLog(s.db, instID, strings.Join(logLines, "\n"))
			s.creationLogs.Complete(instID)
		}

		defer func() {
			if r := recover(); r != nil {
				log.Printf("panic in CreateAsync goroutine for instance %d: %v", instID, r)
				logFn(fmt.Sprintf("[PANIC] %v", r))
				finalize("error")
			}
		}()

		secretsEnv := s.buildSecretsEnv(req.UserID, 0, tsMode)
		secretsEnv["PLATI_TAILSCALE_HOSTNAME"] = sanitizeName(req.Name)
		config := incus.BuildInstanceConfig(resources)
		for k, v := range secretsEnv {
			config["environment."+k] = v
		}

		logFn("Creating Incus instance...")
		if err := client.CreateInstance(incusName, tmpl.Image, profiles, config, nil); err != nil {
			log.Printf("CreateAsync: create incus instance %s: %v", incusName, err)
			logFn(fmt.Sprintf("Error: create instance failed: %v", err))
			finalize("error")
			return
		}
		logFn("Instance created.")

		var persistDirs []persistenceDir
		if tmpl.PersistenceDirs != "" && tmpl.PersistenceDirs != "[]" {
			json.Unmarshal([]byte(tmpl.PersistenceDirs), &persistDirs)
		}
		if len(persistDirs) == 0 && tmpl.PersistenceMode != "ephemeral" {
			diskSize := 20
			if sizeStr, ok := resources["disk"]; ok {
				fmt.Sscanf(sizeStr, "%dGB", &diskSize)
			}
			persistDirs = []persistenceDir{{Path: "/workspace", Size: fmt.Sprintf("%dGB", diskSize), Pool: "default"}}
		}

		if tmpl.PersistenceMode != "ephemeral" && len(persistDirs) > 0 {
			logFn(fmt.Sprintf("Creating %d volume(s)...", len(persistDirs)))
			for _, dir := range persistDirs {
				pool := dir.Pool
				if pool == "" {
					pool = "default"
				}
				sizeGB := 20
				fmt.Sscanf(dir.Size, "%dGB", &sizeGB)
				if sizeGB == 0 {
					fmt.Sscanf(dir.Size, "%dGiB", &sizeGB)
				}
				devName := deviceNameFromPath(dir.Path)
				volName := incusName + "-" + devName

				volID, err := queries.CreateVolume(s.db, volName, server.ID, pool, sizeGB)
				if err != nil {
					log.Printf("warning: create volume record %s: %v", volName, err)
					logFn(fmt.Sprintf("Warning: create volume %s: %v", volName, err))
					continue
				}

				logFn(fmt.Sprintf("Attaching volume → %s (%dGB)...", dir.Path, sizeGB))
				if err := incus.CreateAndAttachVolume(client, pool, volName, incusName, devName, dir.Path, sizeGB); err != nil {
					log.Printf("warning: volume attach failed for %s: %v", dir.Path, err)
					logFn(fmt.Sprintf("Warning: attach %s: %v", dir.Path, err))
					queries.DeleteVolume(s.db, volID)
				}
			}
		}

		// Attach repos dir and build copy commands.
		repoCopyCmds := s.attachReposDirAndBuildCmds(client, incusName, tmpl.Repos)

		logFn("Starting instance...")
		if err := client.StartInstance(incusName); err != nil {
			log.Printf("CreateAsync: start instance %s: %v", incusName, err)
			logFn(fmt.Sprintf("Error: start failed: %v", err))
			finalize("error")
			return
		}
		logFn("Instance started.")

		logFn("Waiting for IP address...")
		if ip, err := incus.GetInstanceIP(client, incusName); err == nil && ip != "" {
			queries.UpdateInstanceIP(s.db, instID, ip)
			logFn(fmt.Sprintf("IP assigned: %s", ip))
		}

		if tmpl.PersistenceMode != "ephemeral" {
			for _, dir := range persistDirs {
				devName := deviceNameFromPath(dir.Path)
				volName := incusName + "-" + devName
				if vol, err := queries.GetVolumeByName(s.db, volName); err == nil {
					queries.CreateInstanceVolume(s.db, instID, vol.ID, dir.Path, devName)
				}
			}
		}
		queries.UpdateInstanceLastActive(s.db, instID)

		var sentinelPath string
		if len(persistDirs) > 0 && tmpl.PersistenceMode != "ephemeral" {
			sentinelPath = persistDirs[0].Path + "/.plati-initialized"
		}

		logFn("Running setup commands...")
		var firstInitCmds, rebuildCmds, postCmds []string
		json.Unmarshal([]byte(tmpl.FirstInitCommands), &firstInitCmds)
		json.Unmarshal([]byte(tmpl.RebuildCommands), &rebuildCmds)
		json.Unmarshal([]byte(tmpl.PostCreateCommands), &postCmds)
		firstInitCmds = append(repoCopyCmds, firstInitCmds...)

		s.runPhase2Setup(client, incusName, publicKeys, privateKeys, secretsEnv, tmpl.TerminalUser, incus.SetupConfig{
			PublicKeys:     publicKeys,
			PrivateKeyPEMs: privateKeys,
			Secrets:        secretsEnv,
			TerminalUser:   tmpl.TerminalUser,
			IsFirstInit:    true,
			FirstInitCmds:  firstInitCmds,
			RebuildCmds:    rebuildCmds,
			PostCreateCmds: postCmds,
			SentinelPath:   sentinelPath,
			MixinFileSteps: s.mixinFileStepsForTemplate(tmpl.Includes),
		}, logFn)

		logFn("Setup complete. Instance is ready.")
		finalize("running")
		log.Printf("CreateAsync: instance %d (%s) ready", instID, incusName)
	}()

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

	// Resolve user preferences
	prefs, _ := s.prefSvc.Get(userID)
	publicKeys, privateKeys := s.collectSSHKeysForMode(userID, prefs.SSHKeyMode)

	// Stop instance
	_ = client.StopInstance(inst.IncusName)

	// Detach all volumes using join table
	instanceVols, _ := queries.ListInstanceVolumes(s.db, inst.ID)
	for _, iv := range instanceVols {
		_ = client.DetachVolume(inst.IncusName, iv.DeviceName)
	}
	// Backward compat: also detach legacy workspace device if not in join table
	if len(instanceVols) == 0 && inst.VolumeID.Valid {
		_ = client.DetachVolume(inst.IncusName, "workspace")
	}

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

	secretsEnv := s.buildSecretsEnv(userID, id, prefs.TailscaleMode)
	secretsEnv["PLATI_TAILSCALE_HOSTNAME"] = sanitizeName(inst.Name)
	config := incus.BuildInstanceConfig(resources)
	for k, v := range secretsEnv {
		config["environment."+k] = v
	}

	// Recreate instance
	if err := client.CreateInstance(inst.IncusName, tmpl.Image, profiles, config, nil); err != nil {
		queries.UpdateInstanceStatus(s.db, id, "error")
		return fmt.Errorf("recreate instance: %w", err)
	}

	// Reattach volumes from join table
	for _, iv := range instanceVols {
		vol, err := queries.GetVolume(s.db, iv.VolumeID)
		if err != nil {
			log.Printf("warning: get volume %d: %v", iv.VolumeID, err)
			continue
		}
		_ = client.AttachVolume(vol.Pool, vol.Name, inst.IncusName, iv.DeviceName, iv.MountPath)
	}
	// Backward compat: reattach legacy workspace volume
	if len(instanceVols) == 0 && inst.VolumeID.Valid {
		vol, err := queries.GetVolume(s.db, inst.VolumeID.Int64)
		if err == nil {
			_ = client.AttachVolume("default", vol.Name, inst.IncusName, "workspace", "/workspace")
		}
	}

	// Reattach repos dir bind mount.
	repoCopyCmds := s.attachReposDirAndBuildCmds(client, inst.IncusName, tmpl.Repos)

	// Start
	if err := client.StartInstance(inst.IncusName); err != nil {
		queries.UpdateInstanceStatus(s.db, id, "error")
		return fmt.Errorf("start after rebuild: %w", err)
	}

	// Determine first-init vs rebuild by checking sentinel
	var persistDirs []persistenceDir
	json.Unmarshal([]byte(tmpl.PersistenceDirs), &persistDirs)
	var sentinelPath string
	isFirstInit := true
	if len(persistDirs) > 0 && tmpl.PersistenceMode != "ephemeral" {
		sentinelPath = persistDirs[0].Path + "/.plati-initialized"
		// Check sentinel existence
		out, _ := client.RunCommand(inst.IncusName, []string{"/bin/sh", "-c",
			fmt.Sprintf("test -f %s && echo yes || echo no", sentinelPath)})
		isFirstInit = strings.TrimSpace(out) != "yes"
	}
	if tmpl.PersistenceMode == "ephemeral" {
		isFirstInit = true
		sentinelPath = ""
	}

	var firstInitCmds, rebuildCmds, postCmds []string
	json.Unmarshal([]byte(tmpl.FirstInitCommands), &firstInitCmds)
	json.Unmarshal([]byte(tmpl.RebuildCommands), &rebuildCmds)
	json.Unmarshal([]byte(tmpl.PostCreateCommands), &postCmds)
	if isFirstInit {
		firstInitCmds = append(repoCopyCmds, firstInitCmds...)
	}

	s.runPhase2Setup(client, inst.IncusName, publicKeys, privateKeys, secretsEnv, tmpl.TerminalUser, incus.SetupConfig{
		PublicKeys:     publicKeys,
		PrivateKeyPEMs: privateKeys,
		Secrets:        secretsEnv,
		TerminalUser:   tmpl.TerminalUser,
		IsFirstInit:    isFirstInit,
		FirstInitCmds:  firstInitCmds,
		RebuildCmds:    rebuildCmds,
		PostCreateCmds: postCmds,
		SentinelPath:   sentinelPath,
		MixinFileSteps: s.mixinFileStepsForTemplate(tmpl.Includes),
	}, nil)

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

	// Gather volumes before deleting instance from DB
	instanceVols, _ := queries.ListInstanceVolumes(s.db, inst.ID)

	// Delete instance from DB first (removes FK references)
	if err := queries.DeleteInstance(s.db, id); err != nil {
		return fmt.Errorf("delete instance from DB: %w", err)
	}

	// Delete instance in Incus
	if err := client.DeleteInstance(inst.IncusName); err != nil {
		log.Printf("warning: failed to delete incus instance %s: %v", inst.IncusName, err)
	}

	// Delete volumes from join table
	for _, iv := range instanceVols {
		vol, err := queries.GetVolume(s.db, iv.VolumeID)
		if err != nil {
			continue
		}
		_ = client.DeleteVolume(vol.Pool, vol.Name)
		if err := queries.DeleteVolume(s.db, vol.ID); err != nil {
			log.Printf("warning: failed to delete volume %d from DB: %v", vol.ID, err)
		}
	}

	// Backward compat: delete legacy single workspace volume
	if len(instanceVols) == 0 && inst.VolumeID.Valid {
		vol, err := queries.GetVolume(s.db, inst.VolumeID.Int64)
		if err == nil {
			_ = client.DeleteVolume("default", vol.Name)
			queries.DeleteVolume(s.db, vol.ID)
		}
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

// SshxURLResult holds the collaborative terminal URL for an sshx instance.
type SshxURLResult struct {
	URL string `json:"url"`
}

// InstanceStorageInfo describes the storage layout of an instance.
type InstanceStorageInfo struct {
	PersistenceMode string               `json:"persistence_mode"`
	Volumes         []models.VolumeDetail `json:"volumes"`
}

// GetStorageInfo returns persistence mode and attached volumes for an instance.
func (s *InstanceService) GetStorageInfo(id, userID int64) (*InstanceStorageInfo, error) {
	inst, err := queries.GetInstanceByUser(s.db, id, userID)
	if err != nil {
		return nil, fmt.Errorf("instance not found: %w", err)
	}

	tmpl, err := queries.GetTemplate(s.db, inst.TemplateID)
	if err != nil {
		return nil, fmt.Errorf("template not found: %w", err)
	}

	volumes, err := queries.ListInstanceVolumesWithDetails(s.db, inst.ID)
	if err != nil {
		return nil, fmt.Errorf("failed to load volumes: %w", err)
	}

	// Legacy fallback: instances created before multi-volume support may only have volume_id set.
	if len(volumes) == 0 && inst.VolumeID.Valid {
		if v, err := queries.GetVolume(s.db, inst.VolumeID.Int64); err == nil {
			volumes = []models.VolumeDetail{{
				VolumeID: v.ID, MountPath: "/workspace", DeviceName: "workspace",
				VolumeName: v.Name, Pool: v.Pool, SizeGB: v.SizeGB, CreatedAt: v.CreatedAt,
			}}
		}
	}

	return &InstanceStorageInfo{
		PersistenceMode: tmpl.PersistenceMode,
		Volumes:         volumes,
	}, nil
}

// InstanceStats holds workspace metrics fetched by running commands inside the instance.
type InstanceStats struct {
	HasGit      bool   `json:"has_git"`
	GitModified int    `json:"git_modified"`
	DiskUsed    string `json:"disk_used"`
}

// GetStats runs lightweight commands inside a running instance to collect workspace metrics.
// Returns an empty stats struct (no error) for stopped instances.
func (s *InstanceService) GetStats(id, userID int64) (*InstanceStats, error) {
	inst, err := queries.GetInstanceByUser(s.db, id, userID)
	if err != nil {
		return nil, fmt.Errorf("instance not found: %w", err)
	}

	if inst.Status != "running" {
		return &InstanceStats{}, nil
	}

	server, err := queries.GetServer(s.db, inst.ServerID)
	if err != nil {
		return nil, fmt.Errorf("server not found: %w", err)
	}

	client, err := s.pool.GetClient(server.Name)
	if err != nil {
		return nil, fmt.Errorf("get incus client: %w", err)
	}

	// Single shell script: detect git repo, count modified files, measure disk usage.
	script := `
HAS_GIT=no
GIT_MOD=0
if git -C /workspace rev-parse --is-inside-work-tree >/dev/null 2>&1; then
  HAS_GIT=yes
  GIT_MOD=$(git -C /workspace status --porcelain 2>/dev/null | wc -l | tr -d ' ')
fi
DISK=$(du -sh /workspace 2>/dev/null | awk '{print $1}')
[ -z "$DISK" ] && DISK=$(du -sh /home 2>/dev/null | awk '{print $1}')
[ -z "$DISK" ] && DISK=unknown
printf "has_git=%s\ngit_modified=%s\ndisk_used=%s\n" "$HAS_GIT" "$GIT_MOD" "$DISK"
`
	out, err := client.RunCommand(inst.IncusName, []string{"/bin/sh", "-c", script})
	if err != nil {
		// Instance may not be fully ready; return empty rather than error
		log.Printf("stats exec %s: %v", inst.IncusName, err)
		return &InstanceStats{}, nil
	}

	stats := &InstanceStats{}
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		switch k {
		case "has_git":
			stats.HasGit = v == "yes"
		case "git_modified":
			n, _ := strconv.Atoi(strings.TrimSpace(v))
			stats.GitModified = n
		case "disk_used":
			stats.DiskUsed = strings.TrimSpace(v)
		}
	}
	return stats, nil
}

// GetSshxURL retrieves the collaborative terminal URL from the sshx service journal.
// Returns an empty URL (no error) when sshx hasn't printed its URL yet.
func (s *InstanceService) GetSshxURL(id, userID int64) (*SshxURLResult, error) {
	inst, err := queries.GetInstanceByUser(s.db, id, userID)
	if err != nil {
		return nil, fmt.Errorf("instance not found: %w", err)
	}
	if inst.Status != "running" {
		return nil, fmt.Errorf("instance not running")
	}
	server, err := queries.GetServer(s.db, inst.ServerID)
	if err != nil {
		return nil, fmt.Errorf("server not found: %w", err)
	}
	client, err := s.pool.GetClient(server.Name)
	if err != nil {
		return nil, fmt.Errorf("get incus client: %w", err)
	}
	cmd := `journalctl -u sshx.service -n 100 --no-pager -o cat 2>/dev/null | sed 's/\x1b\[[0-9;]*[a-zA-Z]//g' | grep -oE 'https://sshx\.io/s/[^[:space:]]+'`
	out, err := client.RunCommand(inst.IncusName, []string{"/bin/sh", "-c", cmd})
	if err != nil {
		log.Printf("sshx-url exec %s: %v", inst.IncusName, err)
		return &SshxURLResult{}, nil
	}
	url := strings.TrimSpace(out)
	if lines := strings.Split(url, "\n"); len(lines) > 1 {
		url = strings.TrimSpace(lines[len(lines)-1])
	}
	return &SshxURLResult{URL: url}, nil
}

// TailscaleServeResult holds the outcome of a Tailscale Serve operation.
type TailscaleServeResult struct {
	Status string `json:"status"` // "active", "off", or "error"
	URL    string `json:"url"`    // The public Tailscale Serve URL, if any
	Port   int    `json:"port"`   // The port being served
}

// TailscaleServe enables Tailscale Serve on the given port inside the instance.
func (s *InstanceService) TailscaleServe(id, userID int64, port int) (*TailscaleServeResult, error) {
	inst, err := queries.GetInstanceByUser(s.db, id, userID)
	if err != nil {
		return nil, fmt.Errorf("instance not found: %w", err)
	}
	if inst.Status != "running" {
		return nil, fmt.Errorf("instance not running")
	}
	server, err := queries.GetServer(s.db, inst.ServerID)
	if err != nil {
		return nil, fmt.Errorf("server not found: %w", err)
	}
	client, err := s.pool.GetClient(server.Name)
	if err != nil {
		return nil, fmt.Errorf("get incus client: %w", err)
	}

	script := fmt.Sprintf(`tailscale serve --bg https+insecure://localhost:%d 2>&1; DNS=$(tailscale status --json 2>/dev/null | grep -o '"DNSName":"[^"]*"' | head -1 | cut -d'"' -f4); echo "dns=$DNS"; echo "port=%d"`, port, port)
	out, err := client.RunCommand(inst.IncusName, []string{"/bin/sh", "-c", script})
	if err != nil {
		log.Printf("tailscale-serve exec %s: %v", inst.IncusName, err)
		return &TailscaleServeResult{Status: "error"}, nil
	}

	result := &TailscaleServeResult{Status: "active", Port: port}
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		switch k {
		case "dns":
			dns := strings.TrimSpace(v)
			dns = strings.TrimSuffix(dns, ".")
			if dns != "" {
				result.URL = fmt.Sprintf("https://%s", dns)
			}
		case "port":
			n, _ := strconv.Atoi(strings.TrimSpace(v))
			if n > 0 {
				result.Port = n
			}
		}
	}
	return result, nil
}

// TailscaleServeStatus checks the current Tailscale Serve status inside the instance.
func (s *InstanceService) TailscaleServeStatus(id, userID int64) (*TailscaleServeResult, error) {
	inst, err := queries.GetInstanceByUser(s.db, id, userID)
	if err != nil {
		return nil, fmt.Errorf("instance not found: %w", err)
	}
	if inst.Status != "running" {
		return nil, fmt.Errorf("instance not running")
	}
	server, err := queries.GetServer(s.db, inst.ServerID)
	if err != nil {
		return nil, fmt.Errorf("server not found: %w", err)
	}
	client, err := s.pool.GetClient(server.Name)
	if err != nil {
		return nil, fmt.Errorf("get incus client: %w", err)
	}

	script := `STATUS=$(tailscale serve status 2>&1); if echo "$STATUS" | grep -q "No serve config"; then echo "status=off"; else DNS=$(tailscale status --json 2>/dev/null | grep -o '"DNSName":"[^"]*"' | head -1 | cut -d'"' -f4); PORT=$(echo "$STATUS" | grep -oE ':[0-9]+' | head -1 | tr -d ':'); echo "status=active"; echo "dns=$DNS"; echo "port=$PORT"; fi`
	out, err := client.RunCommand(inst.IncusName, []string{"/bin/sh", "-c", script})
	if err != nil {
		log.Printf("tailscale-serve-status exec %s: %v", inst.IncusName, err)
		return &TailscaleServeResult{Status: "off"}, nil
	}

	result := &TailscaleServeResult{}
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		switch k {
		case "status":
			result.Status = strings.TrimSpace(v)
		case "dns":
			dns := strings.TrimSpace(v)
			dns = strings.TrimSuffix(dns, ".")
			if dns != "" {
				result.URL = fmt.Sprintf("https://%s", dns)
			}
		case "port":
			n, _ := strconv.Atoi(strings.TrimSpace(v))
			if n > 0 {
				result.Port = n
			}
		}
	}
	if result.Status == "" {
		result.Status = "off"
	}
	return result, nil
}

// TailscaleServeOff disables Tailscale Serve inside the instance.
func (s *InstanceService) TailscaleServeOff(id, userID int64) error {
	inst, err := queries.GetInstanceByUser(s.db, id, userID)
	if err != nil {
		return fmt.Errorf("instance not found: %w", err)
	}
	if inst.Status != "running" {
		return fmt.Errorf("instance not running")
	}
	server, err := queries.GetServer(s.db, inst.ServerID)
	if err != nil {
		return fmt.Errorf("server not found: %w", err)
	}
	client, err := s.pool.GetClient(server.Name)
	if err != nil {
		return fmt.Errorf("get incus client: %w", err)
	}

	_, err = client.RunCommand(inst.IncusName, []string{"/bin/sh", "-c", "tailscale serve off"})
	if err != nil {
		return fmt.Errorf("tailscale serve off: %w", err)
	}
	return nil
}

// TailscaleStatusResult holds the Tailscale machine status for an instance.
type TailscaleStatusResult struct {
	Connected   bool   `json:"connected"`
	DNSName     string `json:"dns_name"`
	MachineName string `json:"machine_name"`
}

// GetTailscaleStatus returns the Tailscale machine's Magic DNS name and connection status.
func (s *InstanceService) GetTailscaleStatus(id, userID int64) (*TailscaleStatusResult, error) {
	inst, err := queries.GetInstanceByUser(s.db, id, userID)
	if err != nil {
		return nil, fmt.Errorf("instance not found: %w", err)
	}
	if inst.Status != "running" {
		return nil, fmt.Errorf("instance not running")
	}
	server, err := queries.GetServer(s.db, inst.ServerID)
	if err != nil {
		return nil, fmt.Errorf("server not found: %w", err)
	}
	client, err := s.pool.GetClient(server.Name)
	if err != nil {
		return nil, fmt.Errorf("get incus client: %w", err)
	}

	// Collapse whitespace so the grep works regardless of whether tailscale
	// emits compact JSON ("DNSName":"...") or spaced JSON ("DNSName": "...").
	script := `JSON=$(tailscale status --json 2>/dev/null | tr -d ' \n'); DNS=$(echo "$JSON" | grep -o '"DNSName":"[^"]*"' | head -1 | cut -d'"' -f4); HOST=$(echo "$JSON" | grep -o '"HostName":"[^"]*"' | head -1 | cut -d'"' -f4); DNS=$(echo "$DNS" | sed 's/\.$//' ); echo "dns=$DNS"; echo "host=$HOST"`
	out, err := client.RunCommand(inst.IncusName, []string{"/bin/sh", "-c", script})
	if err != nil {
		log.Printf("tailscale-status exec %s: %v", inst.IncusName, err)
		return &TailscaleStatusResult{}, nil
	}

	result := &TailscaleStatusResult{}
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		switch k {
		case "dns":
			dns := strings.TrimSpace(v)
			dns = strings.TrimSuffix(dns, ".")
			if dns != "" {
				result.DNSName = dns
				result.Connected = true
			}
		case "host":
			result.MachineName = strings.TrimSpace(v)
		}
	}
	return result, nil
}

// Duplicate creates a new instance using the same template as the source,
// then deep-copies volume data from source to destination.
func (s *InstanceService) Duplicate(id, userID int64) (*models.Instance, error) {
	orig, err := queries.GetInstanceByUser(s.db, id, userID)
	if err != nil {
		return nil, fmt.Errorf("instance not found: %w", err)
	}

	// Create a fresh instance with empty volumes.
	dst, err := s.Create(CreateInstanceRequest{
		Name:       orig.Name + "-copy",
		TemplateID: orig.TemplateID,
		UserID:     userID,
	})
	if err != nil {
		return nil, fmt.Errorf("create duplicate: %w", err)
	}

	// Stream data from each source volume to the destination.
	srcVols, _ := queries.ListInstanceVolumes(s.db, orig.ID)
	dstVols, _ := queries.ListInstanceVolumes(s.db, dst.ID)

	// Build a path → dst InstanceVolume map.
	dstByPath := make(map[string]models.InstanceVolume, len(dstVols))
	for _, dv := range dstVols {
		dstByPath[dv.MountPath] = dv
	}

	server, err := queries.GetServer(s.db, orig.ServerID)
	if err != nil {
		return dst, nil // volume copy best-effort
	}
	client, err := s.pool.GetClient(server.Name)
	if err != nil {
		return dst, nil
	}

	for _, sv := range srcVols {
		dv, ok := dstByPath[sv.MountPath]
		if !ok {
			continue
		}

		// Stream: tar from src | tar into dst
		r, err := client.StreamCommand(orig.IncusName, []string{"tar", "-C", sv.MountPath, "-cf", "-", "."})
		if err != nil {
			log.Printf("duplicate: stream src %s: %v", sv.MountPath, err)
			continue
		}

		if err := client.ExecInstance(dst.IncusName, []string{"tar", "-C", dv.MountPath, "-xf", "-"},
			nil, io.NopCloser(r), nil, nil); err != nil {
			log.Printf("duplicate: extract dst %s: %v", dv.MountPath, err)
		}
		r.Close()
	}

	return dst, nil
}

// IncusConfigUpdate holds the editable Incus config fields for an instance.
type IncusConfigUpdate struct {
	LimitsCPU          string `json:"limits_cpu"`
	LimitsMemory       string `json:"limits_memory"`
	LimitsMemorySwap   string `json:"limits_memory_swap"`
	LimitsProcesses    string `json:"limits_processes"`
	BootAutostart      string `json:"boot_autostart"`
	BootAutostartDelay string `json:"boot_autostart_delay"`
	SecurityNesting    string `json:"security_nesting"`
	SecurityPrivileged string `json:"security_privileged"`
}

// UpdateIncusConfig applies the given config fields directly to the running Incus instance.
func (s *InstanceService) UpdateIncusConfig(instanceID int64, req IncusConfigUpdate) error {
	inst, err := queries.GetInstance(s.db, instanceID)
	if err != nil {
		return fmt.Errorf("instance not found: %w", err)
	}
	server, err := queries.GetServer(s.db, inst.ServerID)
	if err != nil {
		return fmt.Errorf("server not found: %w", err)
	}
	client, err := s.pool.GetClient(server.Name)
	if err != nil {
		return fmt.Errorf("get incus client: %w", err)
	}

	config := map[string]string{
		"limits.cpu":           req.LimitsCPU,
		"limits.memory":        req.LimitsMemory,
		"limits.memory.swap":   req.LimitsMemorySwap,
		"limits.processes":     req.LimitsProcesses,
		"boot.autostart":       req.BootAutostart,
		"boot.autostart.delay": req.BootAutostartDelay,
		"security.nesting":     req.SecurityNesting,
		"security.privileged":  req.SecurityPrivileged,
	}
	return client.UpdateInstanceConfig(inst.IncusName, config)
}

// GetIncusDetail returns live Incus state for an instance (admin use).
// Returns nil, nil when the instance is still being created in Incus.
func (s *InstanceService) GetIncusDetail(instanceID int64) (*incus.IncusDetail, error) {
	inst, err := queries.GetInstance(s.db, instanceID)
	if err != nil {
		return nil, fmt.Errorf("instance not found: %w", err)
	}
	if inst.Status == "creating" {
		return nil, nil
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

// runPhase2Setup runs SSH key injection, secrets env file, and lifecycle commands
// via incus exec after the instance has started. Errors are logged but non-fatal.
// logFn, if non-nil, receives progress lines: STEP:N/M:label before each step,
// OUT:line for each output line, and WARN:msg on failure.
func (s *InstanceService) runPhase2Setup(client incus.IncusClient, incusName string, publicKeys, privateKeys []string, secrets map[string]string, terminalUser string, cfg incus.SetupConfig, logFn func(string)) {
	steps := incus.BuildSetupSteps(cfg)
	total := len(steps)

	for i, step := range steps {
		if logFn != nil {
			logFn(fmt.Sprintf("STEP:%d/%d:%s", i+1, total, step.Label))
		}
		var out string
		var err error
		if len(step.FileContent) > 0 {
			err = client.PushFile(incusName, step.FileDest, step.FileContent, step.FileUID, step.FileGID, step.FileMode)
		} else if step.FileSourcePath != "" {
			var content []byte
			content, err = os.ReadFile(step.FileSourcePath)
			if err != nil {
				err = fmt.Errorf("read mixin file %s: %w", step.FileSourcePath, err)
			} else {
				err = client.PushFile(incusName, step.FileDest, content, step.FileUID, step.FileGID, step.FileMode)
			}
		} else {
			out, err = client.RunCommand(incusName, step.Cmd)
		}
		if logFn != nil {
			trimmed := strings.TrimSpace(out)
			if trimmed != "" {
				for _, line := range strings.Split(trimmed, "\n") {
					logFn("OUT:" + strings.TrimRight(line, "\r"))
				}
			}
		}
		if err != nil {
			log.Printf("phase2 step %d in %s: %v (out: %s)", i, incusName, err, strings.TrimSpace(out))
			if logFn != nil {
				logFn(fmt.Sprintf("WARN:step %d failed: %v", i+1, err))
			}
		}
	}
}

// ExecCommandResult holds the output of a shell command run inside an instance.
type ExecCommandResult struct {
	Output string `json:"output"`
	Error  string `json:"error,omitempty"`
}

// ExecCommand runs an arbitrary shell command inside a running instance (admin use).
func (s *InstanceService) ExecCommand(instanceID int64, command string) (*ExecCommandResult, error) {
	inst, err := queries.GetInstance(s.db, instanceID)
	if err != nil {
		return nil, fmt.Errorf("instance not found: %w", err)
	}
	if inst.Status != "running" {
		return nil, fmt.Errorf("instance is not running")
	}
	server, err := queries.GetServer(s.db, inst.ServerID)
	if err != nil {
		return nil, fmt.Errorf("server not found: %w", err)
	}
	client, err := s.pool.GetClient(server.Name)
	if err != nil {
		return nil, fmt.Errorf("get incus client: %w", err)
	}
	out, execErr := client.RunCommand(inst.IncusName, []string{"/bin/sh", "-c", command})
	result := &ExecCommandResult{Output: out}
	if execErr != nil {
		result.Error = execErr.Error()
	}
	return result, nil
}

// ReapplySetup re-runs the SSH key + secrets setup steps on a running instance without
// running any lifecycle commands (first_init or rebuild). Admin use only.
// It returns the captured step-by-step output so callers can display it.
func (s *InstanceService) ReapplySetup(instanceID int64) (*ExecCommandResult, error) {
	inst, err := queries.GetInstance(s.db, instanceID)
	if err != nil {
		return nil, fmt.Errorf("instance not found: %w", err)
	}
	if inst.Status != "running" {
		return nil, fmt.Errorf("instance is not running")
	}
	server, err := queries.GetServer(s.db, inst.ServerID)
	if err != nil {
		return nil, fmt.Errorf("server not found: %w", err)
	}
	client, err := s.pool.GetClient(server.Name)
	if err != nil {
		return nil, fmt.Errorf("get incus client: %w", err)
	}
	tmpl, err := queries.GetTemplate(s.db, inst.TemplateID)
	if err != nil {
		return nil, fmt.Errorf("template not found: %w", err)
	}

	prefs, _ := s.prefSvc.Get(inst.UserID)
	// Always inject the Plati private key during debug reapply so admins can
	// verify key injection regardless of their personal ssh_key_mode setting.
	publicKeys, privateKeys := s.collectSSHKeysForMode(inst.UserID, "plati")
	secretsEnv := s.buildSecretsEnv(inst.UserID, inst.ID, prefs.TailscaleMode)

	var lines []string
	logFn := func(line string) { lines = append(lines, line) }

	s.runPhase2Setup(client, inst.IncusName, publicKeys, privateKeys, secretsEnv, tmpl.TerminalUser, incus.SetupConfig{
		PublicKeys:     publicKeys,
		PrivateKeyPEMs: privateKeys,
		Secrets:        secretsEnv,
		TerminalUser:   tmpl.TerminalUser,
		IsFirstInit:    false,
	}, logFn)
	return &ExecCommandResult{Output: strings.Join(lines, "\n")}, nil
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
