package services

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/jmoiron/sqlx"

	"github.com/homaserver/plati/internal/auth"
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
	reposHostDir string // path as seen by the Incus host (may differ from reposDir when running in Docker)
}

func NewInstanceService(db *sqlx.DB, pool *incus.Pool, userSvc *UserService, prefSvc *PreferencesService, adminSvc *AdminSettingsService, keysDir string, reposDir string, reposHostDir string, repoSvc *RepoService, templateSvc *TemplateService) *InstanceService {
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
		reposHostDir: reposHostDir,
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
// e.g. "/home/ubuntu" → "home-ubuntu", "/data" → "data"
func deviceNameFromPath(path string) string {
	path = strings.TrimPrefix(path, "/")
	path = strings.ReplaceAll(path, "/", "-")
	if path == "" {
		return "vol"
	}
	return path
}

// shellQuote wraps a value in single quotes for safe interpolation into a /bin/sh script.
func shellQuote(v string) string {
	return "'" + strings.ReplaceAll(v, "'", `'\''`) + "'"
}

// defaultPersistenceDirs is what a template without a `persistence` block gets: a single
// volume on the login user's home, sized from resources.disk. Persisting the home is the
// convention every template follows — it is where repos, dotfiles and tool caches live.
func defaultPersistenceDirs(terminalUser string, resources map[string]string) []persistenceDir {
	diskSize := 20
	if sizeStr, ok := resources["disk"]; ok {
		fmt.Sscanf(sizeStr, "%dGB", &diskSize)
	}
	return []persistenceDir{{
		Path: defaultPersistencePath(terminalUser),
		Size: fmt.Sprintf("%dGB", diskSize),
		Pool: "default",
	}}
}

// defaultPersistencePath is the home of the login user, or /root when the template declares
// no terminal_user (root is then the only account).
func defaultPersistencePath(terminalUser string) string {
	if terminalUser == "" {
		return "/root"
	}
	return "/home/" + terminalUser
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

	if err := client.AttachHostPath(incusName, "plati-repos", s.reposHostDir, "/plati-repos"); err != nil {
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

// templateIncusConfig decodes the Incus config keys a template requires (merged from
// its mixins and its own incus_config block). A malformed value is ignored rather than
// failing instance creation.
func templateIncusConfig(t *models.Template) map[string]string {
	if t.IncusConfig == "" || t.IncusConfig == "{}" {
		return nil
	}
	var cfg map[string]string
	if err := json.Unmarshal([]byte(t.IncusConfig), &cfg); err != nil {
		log.Printf("warning: template %s: invalid incus_config: %v", t.Slug, err)
		return nil
	}
	return cfg
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
	config := incus.BuildInstanceConfig(resources, templateIncusConfig(tmpl))

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

	// If no persistence dirs configured, fall back to a single volume on the login user's home.
	if len(persistDirs) == 0 && tmpl.PersistenceMode != "ephemeral" {
		persistDirs = defaultPersistenceDirs(tmpl.TerminalUser, resources)
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
		config := incus.BuildInstanceConfig(resources, templateIncusConfig(tmpl))
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
			persistDirs = defaultPersistenceDirs(tmpl.TerminalUser, resources)
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

func (s *InstanceService) Start(id int64, actor auth.Actor) error {
	inst, err := queries.GetInstanceForActor(s.db, id, actor)
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

func (s *InstanceService) Stop(id int64, actor auth.Actor) error {
	inst, err := queries.GetInstanceForActor(s.db, id, actor)
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

func (s *InstanceService) Rebuild(id int64, actor auth.Actor) error {
	inst, err := queries.GetInstanceForActor(s.db, id, actor)
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

	// Resolve user preferences. Everything below is read from inst.UserID, not from
	// the caller: an admin may rebuild someone else's workspace, and injecting the
	// admin's SSH keys would lock the owner out of their own machine while injecting
	// the admin's decrypted secrets would leak them into another user's container.
	// ReapplySetup already works this way.
	prefs, _ := s.prefSvc.Get(inst.UserID)
	publicKeys, privateKeys := s.collectSSHKeysForMode(inst.UserID, prefs.SSHKeyMode)

	// Stop instance
	_ = client.StopInstance(inst.IncusName)

	// Detach all volumes using join table
	instanceVols, _ := queries.ListInstanceVolumes(s.db, inst.ID)
	for _, iv := range instanceVols {
		_ = client.DetachVolume(inst.IncusName, iv.DeviceName)
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

	secretsEnv := s.buildSecretsEnv(inst.UserID, id, prefs.TailscaleMode)
	secretsEnv["PLATI_TAILSCALE_HOSTNAME"] = sanitizeName(inst.Name)
	config := incus.BuildInstanceConfig(resources, templateIncusConfig(tmpl))
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

func (s *InstanceService) Delete(id int64, actor auth.Actor) error {
	inst, err := queries.GetInstanceForActor(s.db, id, actor)
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

	return nil
}

// Instance secrets management

func (s *InstanceService) ListInstanceSecrets(instanceID int64, actor auth.Actor) ([]models.InstanceSecret, error) {
	inst, err := queries.GetInstanceForActor(s.db, instanceID, actor)
	if err != nil {
		return nil, fmt.Errorf("instance not found: %w", err)
	}
	return queries.ListInstanceSecrets(s.db, inst.ID)
}

func (s *InstanceService) CreateInstanceSecret(instanceID int64, actor auth.Actor, name, value string) (int64, error) {
	inst, err := queries.GetInstanceForActor(s.db, instanceID, actor)
	if err != nil {
		return 0, fmt.Errorf("instance not found: %w", err)
	}
	encrypted, err := s.userSvc.Encrypt(value)
	if err != nil {
		return 0, fmt.Errorf("encrypt secret: %w", err)
	}
	return queries.CreateInstanceSecret(s.db, inst.ID, name, encrypted)
}

func (s *InstanceService) UpdateInstanceSecret(id, instanceID int64, actor auth.Actor, value string) error {
	inst, err := queries.GetInstanceForActor(s.db, instanceID, actor)
	if err != nil {
		return fmt.Errorf("instance not found: %w", err)
	}
	encrypted, err := s.userSvc.Encrypt(value)
	if err != nil {
		return fmt.Errorf("encrypt secret: %w", err)
	}
	return queries.UpdateInstanceSecret(s.db, id, inst.ID, encrypted)
}

func (s *InstanceService) DeleteInstanceSecret(id, instanceID int64, actor auth.Actor) error {
	inst, err := queries.GetInstanceForActor(s.db, instanceID, actor)
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
func (s *InstanceService) GetStorageInfo(id int64, actor auth.Actor) (*InstanceStorageInfo, error) {
	inst, err := queries.GetInstanceForActor(s.db, id, actor)
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
func (s *InstanceService) GetStats(id int64, actor auth.Actor) (*InstanceStats, error) {
	inst, err := queries.GetInstanceForActor(s.db, id, actor)
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

	// Stats describe the persistent volume, so ask about the path it is actually mounted on
	// rather than a fixed one — every template picks its own (usually the login user's home).
	statsPath := "/home"
	if vols, err := queries.ListInstanceVolumes(s.db, inst.ID); err == nil && len(vols) > 0 {
		statsPath = vols[0].MountPath
	}

	// Single shell script: detect git repo, count modified files, measure disk usage.
	script := fmt.Sprintf(`
P=%s
HAS_GIT=no
GIT_MOD=0
if git -C "$P" rev-parse --is-inside-work-tree >/dev/null 2>&1; then
  HAS_GIT=yes
  GIT_MOD=$(git -C "$P" status --porcelain 2>/dev/null | wc -l | tr -d ' ')
fi
DISK=$(du -sh "$P" 2>/dev/null | awk '{print $1}')
[ -z "$DISK" ] && DISK=unknown
printf "has_git=%%s\ngit_modified=%%s\ndisk_used=%%s\n" "$HAS_GIT" "$GIT_MOD" "$DISK"
`, shellQuote(statsPath))
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
func (s *InstanceService) GetSshxURL(id int64, actor auth.Actor) (*SshxURLResult, error) {
	inst, err := queries.GetInstanceForActor(s.db, id, actor)
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
func (s *InstanceService) TailscaleServe(id int64, actor auth.Actor, port int) (*TailscaleServeResult, error) {
	inst, err := queries.GetInstanceForActor(s.db, id, actor)
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
func (s *InstanceService) TailscaleServeStatus(id int64, actor auth.Actor) (*TailscaleServeResult, error) {
	inst, err := queries.GetInstanceForActor(s.db, id, actor)
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
func (s *InstanceService) TailscaleServeOff(id int64, actor auth.Actor) error {
	inst, err := queries.GetInstanceForActor(s.db, id, actor)
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
	// Installed reports whether the tailscale CLI is present at all, and DaemonActive
	// whether tailscaled is running. Together with Connected they tell apart the three
	// ways Tailscale can be unusable: never installed, installed but dead, logged out.
	Installed    bool `json:"installed"`
	DaemonActive bool `json:"daemon_active"`
	// LoginOutput carries what `tailscale up` actually said when the machine is
	// still logged out after an install. Empty when connected.
	LoginOutput string `json:"login_output,omitempty"`
}

// tskeyPattern matches a Tailscale auth key, so one never reaches the UI or the
// logs through a command's output.
var tskeyPattern = regexp.MustCompile(`tskey-[A-Za-z0-9-]+`)

func redactTailscaleKeys(s string) string {
	return tskeyPattern.ReplaceAllString(s, "tskey-<redacted>")
}

// GetTailscaleStatus returns the Tailscale machine's Magic DNS name and connection status.
func (s *InstanceService) GetTailscaleStatus(id int64, actor auth.Actor) (*TailscaleStatusResult, error) {
	inst, err := queries.GetInstanceForActor(s.db, id, actor)
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
	script := `CLI=0; command -v tailscale >/dev/null 2>&1 && CLI=1
	DAEMON=0; pgrep -x tailscaled >/dev/null 2>&1 && DAEMON=1
	JSON=$(tailscale status --json 2>/dev/null | tr -d ' \n'); DNS=$(echo "$JSON" | grep -o '"DNSName":"[^"]*"' | head -1 | cut -d'"' -f4); HOST=$(echo "$JSON" | grep -o '"HostName":"[^"]*"' | head -1 | cut -d'"' -f4); DNS=$(echo "$DNS" | sed 's/\.$//' ); echo "dns=$DNS"; echo "host=$HOST"; echo "cli=$CLI"; echo "daemon=$DAEMON"`
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
		case "cli":
			result.Installed = strings.TrimSpace(v) == "1"
		case "daemon":
			result.DaemonActive = strings.TrimSpace(v) == "1"
		}
	}
	return result, nil
}

// InstallTailscale (re)applies the tailscale mixin to a running instance: it refreshes
// the secrets env file so the current auth key is available, pushes the mixin's files and
// runs its commands. Used from the instance page when Tailscale is missing, its daemon is
// down, or the machine is logged out — typically because no auth key existed at create
// time. It returns the status observed afterwards.
func (s *InstanceService) InstallTailscale(id int64, actor auth.Actor) (*TailscaleStatusResult, error) {
	inst, err := queries.GetInstanceForActor(s.db, id, actor)
	if err != nil {
		return nil, fmt.Errorf("instance not found: %w", err)
	}
	if inst.Status != "running" {
		return nil, fmt.Errorf("instance not running")
	}
	client, err := s.clientForInstance(inst)
	if err != nil {
		return nil, err
	}

	prefs, _ := s.prefSvc.Get(inst.UserID)
	secretsEnv := s.buildSecretsEnv(inst.UserID, inst.ID, prefs.TailscaleMode)
	secretsEnv["PLATI_TAILSCALE_HOSTNAME"] = sanitizeName(inst.Name)
	if secretsEnv["TAILSCALE_AUTH_KEY"] == "" {
		return nil, fmt.Errorf("no Tailscale auth key available: set one in your secrets, or ask an admin to configure the platform key")
	}

	terminalUser := ""
	if tmpl, err := queries.GetTemplate(s.db, inst.TemplateID); err == nil {
		terminalUser = tmpl.TerminalUser
	}

	// Secrets first: the mixin's last command reads TAILSCALE_AUTH_KEY from the env file.
	steps := incus.BuildSecretsEnvSteps(secretsEnv)
	steps = append(steps, s.templateSvc.MixinSetupSteps([]string{"tailscale"}, terminalUser)...)
	if len(steps) == 0 {
		return nil, fmt.Errorf("tailscale mixin not loaded on this server")
	}
	s.runSetupSteps(client, inst.IncusName, steps, nil)

	status, err := s.GetTailscaleStatus(id, actor)
	if err != nil || status.Connected {
		return status, err
	}

	// The mixin's login step ends in "|| true" so a bad key never fails a create,
	// which also means a rejected login leaves no trace anywhere. Still logged out
	// means we owe the caller a reason: run the login once more with its output
	// captured. The key is read from the env file, never put on the command line.
	out, runErr := client.RunCommand(inst.IncusName, []string{"/bin/sh", "-c",
		`. /etc/profile.d/plati-env.sh; tailscale up --auth-key="$TAILSCALE_AUTH_KEY" --hostname="${PLATI_TAILSCALE_HOSTNAME:-$(hostname)}" 2>&1; echo "exit=$?"`})
	if runErr != nil {
		out += "\n" + runErr.Error()
	}
	out = redactTailscaleKeys(strings.TrimSpace(out))
	log.Printf("tailscale login %s: %s", inst.IncusName, out)

	// The retry may itself have logged the machine in.
	status, err = s.GetTailscaleStatus(id, actor)
	if err != nil {
		return nil, err
	}
	if !status.Connected {
		status.LoginOutput = out
	}
	return status, nil
}

// RenameOptions selects how far a rename propagates. Both are opt-in: one stops
// the instance, the other changes the machine's identity inside the OS.
type RenameOptions struct {
	// Container renames the Incus container to plati-{user_id}-{hostname}.
	// Incus requires a stopped instance, so a running one is restarted.
	Container bool `json:"rename_container"`
	// SystemHostname renames the machine inside Ubuntu. Needs the instance
	// running — there is no exec into a stopped container.
	SystemHostname bool `json:"rename_system_hostname"`
}

// tailnetHostnameScript refreshes the tailnet hostname on a running instance:
// the env file, so a later rebuild keeps it, and a live `tailscale set`.
// {{H}} is the sanitized hostname ([a-z0-9-]), so interpolation is safe.
const tailnetHostnameScript = `F=/etc/profile.d/plati-env.sh; if [ -f "$F" ]; then sed -i '/^export PLATI_TAILSCALE_HOSTNAME=/d' "$F"; echo 'export PLATI_TAILSCALE_HOSTNAME="{{H}}"' >> "$F"; fi; tailscale set --hostname={{H}} 2>&1 || true`

// systemHostnameScript renames the machine inside Ubuntu. /etc/hostname is what
// survives a restart; the /etc/hosts line is what keeps sudo from warning
// "unable to resolve host"; hostnamectl (or plain hostname, when hostnamed is
// not reachable) applies it to the running system without a reboot.
const systemHostnameScript = `
echo '{{H}}' > /etc/hostname
if grep -q '^127\.0\.1\.1' /etc/hosts 2>/dev/null; then sed -i 's/^127\.0\.1\.1.*/127.0.1.1 {{H}}/' /etc/hosts; else echo '127.0.1.1 {{H}}' >> /etc/hosts; fi
hostnamectl set-hostname '{{H}}' 2>/dev/null || hostname '{{H}}' 2>/dev/null || true`

// RenameResult reports a rename together with what it managed to apply, so the
// UI can say whether the Incus container followed the display name.
type RenameResult struct {
	*models.Instance
	// ContainerRenamed is true when incus_name changed too; ContainerRenameError
	// carries why it did not, when the caller asked for it.
	ContainerRenamed     bool   `json:"container_renamed"`
	ContainerRenameError string `json:"container_rename_error,omitempty"`
	// Restarted is true when the instance was stopped and started again to let
	// Incus rename it.
	Restarted bool `json:"restarted"`
	// SystemHostname is the hostname reported from inside the instance after the
	// rename, when one was asked for. Empty when it was not, or unreachable.
	SystemHostname string `json:"system_hostname,omitempty"`
}

// MarshalJSON emits the instance's fields plus the rename outcome. Instance
// defines MarshalJSON and embedding promotes it, so without this override the
// three fields above would silently vanish from the response.
func (r RenameResult) MarshalJSON() ([]byte, error) {
	base, err := json.Marshal(r.Instance)
	if err != nil {
		return nil, err
	}
	fields := map[string]any{}
	if err := json.Unmarshal(base, &fields); err != nil {
		return nil, err
	}
	fields["container_renamed"] = r.ContainerRenamed
	fields["restarted"] = r.Restarted
	if r.ContainerRenameError != "" {
		fields["container_rename_error"] = r.ContainerRenameError
	}
	if r.SystemHostname != "" {
		fields["system_hostname"] = r.SystemHostname
	}
	return json.Marshal(fields)
}

// renameContainer renames the Incus container to match the new display name.
// Incus refuses to rename a running instance, so a running one is stopped and
// started again — the caller must have asked for this explicitly.
//
// The stored volumes keep their original names: they are attached by device
// name and renaming them would mean detach/rename/reattach, which risks the
// data for a cosmetic gain.
func (s *InstanceService) renameContainer(inst *models.Instance, client incus.IncusClient, hostname string) (restarted bool, err error) {
	newIncusName := fmt.Sprintf("plati-%d-%s", inst.UserID, hostname)
	if newIncusName == inst.IncusName {
		return false, nil
	}

	taken, err := queries.InstanceIncusNameTaken(s.db, inst.ServerID, inst.ID, newIncusName)
	if err != nil {
		return false, fmt.Errorf("check container name: %w", err)
	}
	if taken {
		return false, fmt.Errorf("container name %q is already used on this server", newIncusName)
	}

	wasRunning := inst.Status == "running"
	if wasRunning {
		if err := client.StopInstance(inst.IncusName); err != nil {
			return false, fmt.Errorf("stop before rename: %w", err)
		}
		queries.UpdateInstanceStatus(s.db, inst.ID, "stopped")
	}

	renameErr := client.RenameInstance(inst.IncusName, newIncusName)
	if renameErr == nil {
		if err := queries.UpdateInstanceIncusName(s.db, inst.ID, inst.UserID, newIncusName); err != nil {
			// The container moved but the row did not: every later call would
			// address a container that no longer exists, so put it back.
			if backErr := client.RenameInstance(newIncusName, inst.IncusName); backErr != nil {
				log.Printf("rename instance %d: DB update failed (%v) AND rollback failed (%v) — incus_name is now stale", inst.ID, err, backErr)
			}
			renameErr = fmt.Errorf("record new container name: %w", err)
		} else {
			inst.IncusName = newIncusName
		}
	}

	if wasRunning {
		// Start again whether or not the rename worked — the instance was
		// running when the user asked, and it should still be running after.
		if startErr := client.StartInstance(inst.IncusName); startErr != nil {
			log.Printf("rename instance %d: restart %s: %v", inst.ID, inst.IncusName, startErr)
		} else {
			restarted = true
			queries.UpdateInstanceStatus(s.db, inst.ID, "running")
			inst.Status = "running"
			if ip, ipErr := incus.GetInstanceIP(client, inst.IncusName); ipErr == nil {
				queries.UpdateInstanceIP(s.db, inst.ID, ip)
			}
		}
	}

	return restarted, renameErr
}

// Rename changes an instance's display name and propagates it to the tailnet.
//
// The name that matters on the network is carried by two things:
//
//   - environment.PLATI_TAILSCALE_HOSTNAME in the Incus config, so a later
//     Rebuild brings the instance back up under the new hostname;
//   - a live `tailscale set --hostname` when the instance is running, so the
//     tailnet picks up the change immediately.
//
// inst.IncusName does not follow unless renameContainer is set: it is the
// identity every later call addresses the container by, and Incus requires a
// stopped instance to change it. See renameContainer for that path.
//
// The DB rename is committed first; Incus and Tailscale propagation is
// best-effort so a stopped or unreachable instance still gets renamed.
func (s *InstanceService) Rename(id int64, actor auth.Actor, newName string, opts RenameOptions) (*RenameResult, error) {
	inst, err := queries.GetInstanceForActor(s.db, id, actor)
	if err != nil {
		return nil, fmt.Errorf("instance not found: %w", err)
	}

	newName = strings.TrimSpace(newName)
	if newName == "" {
		return nil, fmt.Errorf("name is required")
	}
	hostname := sanitizeName(newName)
	if hostname == "" {
		return nil, fmt.Errorf("invalid name %q: leaves no usable characters for a tailnet hostname", newName)
	}
	if newName == inst.Name && !opts.Container && !opts.SystemHostname {
		return &RenameResult{Instance: inst}, nil
	}

	// Reject a hostname already taken by another of the OWNER's instances: the tailnet
	// namespace is theirs, not the caller's, so an admin renaming someone else's
	// workspace must be checked against that user's machines.
	// Tailscale would silently suffix a collision (-1, -2, ...) and the two names would
	// no longer match what Plati displays.
	siblings, err := queries.ListInstancesByUser(s.db, inst.UserID)
	if err != nil {
		return nil, fmt.Errorf("list instances: %w", err)
	}
	for _, sib := range siblings {
		if sib.ID != id && sanitizeName(sib.Name) == hostname {
			return nil, fmt.Errorf("tailnet hostname %q is already used by instance %q", hostname, sib.Name)
		}
	}

	if err := queries.UpdateInstanceName(s.db, id, inst.UserID, newName); err != nil {
		return nil, fmt.Errorf("update instance name: %w", err)
	}
	inst.Name = newName
	result := &RenameResult{Instance: inst}

	server, err := queries.GetServer(s.db, inst.ServerID)
	if err != nil {
		log.Printf("rename instance %d: server not found: %v", id, err)
		return result, nil
	}
	client, err := s.pool.GetClient(server.Name)
	if err != nil {
		log.Printf("rename instance %d: get incus client: %v", id, err)
		return result, nil
	}

	// Rename the container first: everything below addresses it by name.
	if opts.Container {
		restarted, err := s.renameContainer(inst, client, hostname)
		result.Restarted = restarted
		if err != nil {
			log.Printf("rename instance %d: rename container: %v", id, err)
			result.ContainerRenameError = err.Error()
		} else {
			result.ContainerRenamed = true
		}
	}

	if err := client.UpdateInstanceConfig(inst.IncusName, map[string]string{
		"environment.PLATI_TAILSCALE_HOSTNAME": hostname,
	}); err != nil {
		log.Printf("rename instance %d: update PLATI_TAILSCALE_HOSTNAME on %s: %v", id, inst.IncusName, err)
	}

	if inst.Status == "running" {
		script := strings.ReplaceAll(tailnetHostnameScript, "{{H}}", hostname)
		if opts.SystemHostname {
			script += strings.ReplaceAll(systemHostnameScript, "{{H}}", hostname)
			script += "\necho \"system_hostname=$(hostname)\""
		}

		// A container that was just restarted for the rename is up but not yet
		// accepting exec, so one attempt is not enough. Without the restart the
		// first try succeeds and the loop costs nothing.
		attempts := 1
		if result.Restarted {
			attempts = 5
		}
		for attempt := 1; ; attempt++ {
			out, err := client.RunCommand(inst.IncusName, []string{"/bin/sh", "-c", script})
			if err == nil {
				for _, line := range strings.Split(out, "\n") {
					if v, ok := strings.CutPrefix(strings.TrimSpace(line), "system_hostname="); ok {
						result.SystemHostname = v
					}
				}
				break
			}
			if attempt >= attempts {
				log.Printf("rename instance %d: apply hostname on %s: %v (%s)", id, inst.IncusName, err, strings.TrimSpace(out))
				break
			}
			time.Sleep(2 * time.Second)
		}
	}

	return result, nil
}

// Duplicate copies an instance and gives the copy to its owner. For a regular user that
// is themselves, since they can only reach their own; for an admin it means "give this
// user another copy", which is the only sensible reading — the dashboard has an explicit
// target picker (DuplicateForUser) for copying into a different account.
func (s *InstanceService) Duplicate(id int64, actor auth.Actor) (*models.Instance, error) {
	orig, err := queries.GetInstanceForActor(s.db, id, actor)
	if err != nil {
		return nil, fmt.Errorf("instance not found: %w", err)
	}
	return s.duplicateInto(orig, orig.UserID)
}

// DuplicateForUser copies any instance, whoever owns it, and assigns the copy
// to targetUserID. Admin-only path — ownership of the source is not checked.
// A zero targetUserID keeps the copy with the source's owner.
func (s *InstanceService) DuplicateForUser(id, targetUserID int64) (*models.Instance, error) {
	orig, err := queries.GetInstance(s.db, id)
	if err != nil {
		return nil, fmt.Errorf("instance not found: %w", err)
	}
	if targetUserID == 0 {
		targetUserID = orig.UserID
	}
	if _, err := queries.GetUserByID(s.db, targetUserID); err != nil {
		return nil, fmt.Errorf("target user not found: %w", err)
	}
	return s.duplicateInto(orig, targetUserID)
}

// duplicateInto creates a new instance using the same template as the source,
// owned by targetUserID, then deep-copies volume data from source to destination.
func (s *InstanceService) duplicateInto(orig *models.Instance, targetUserID int64) (*models.Instance, error) {
	// Create a fresh instance with empty volumes.
	dst, err := s.Create(CreateInstanceRequest{
		Name:       s.freeInstanceName(targetUserID, orig.Name+"-copy"),
		TemplateID: orig.TemplateID,
		UserID:     targetUserID,
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

// freeInstanceName returns base, or base-2, base-3… — the first name the target
// user does not already use. The Incus name is derived from the owner and the
// display name, and (incus_name, server_id) is unique, so duplicating twice
// into the same account would otherwise collide.
func (s *InstanceService) freeInstanceName(userID int64, base string) string {
	existing, err := queries.ListInstancesByUser(s.db, userID)
	if err != nil {
		return base
	}
	taken := make(map[string]bool, len(existing))
	for _, inst := range existing {
		taken[sanitizeName(inst.Name)] = true
	}
	name := base
	for i := 2; taken[sanitizeName(name)]; i++ {
		name = fmt.Sprintf("%s-%d", base, i)
	}
	return name
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
	s.runSetupSteps(client, incusName, incus.BuildSetupSteps(cfg), logFn)
}

// runSetupSteps executes setup steps in order, reporting progress through logFn.
// A failing step is logged and the run continues, as during instance creation.
func (s *InstanceService) runSetupSteps(client incus.IncusClient, incusName string, steps []incus.SetupStep, logFn func(string)) {
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

// ProfileCheck reports whether a single Incus profile exists on a given server.
type ProfileCheck struct {
	Profile string `json:"profile"`
	Server  string `json:"server"`
	Exists  bool   `json:"exists"`
}

// CheckTemplateProfiles returns, for every server, whether each profile required
// by the template is present on that Incus server.
func (s *InstanceService) CheckTemplateProfiles(templateID int64) ([]ProfileCheck, error) {
	tmpl, err := queries.GetTemplate(s.db, templateID)
	if err != nil {
		return nil, fmt.Errorf("template not found: %w", err)
	}

	var profiles []string
	json.Unmarshal([]byte(tmpl.Profiles), &profiles)

	servers, err := queries.ListServers(s.db)
	if err != nil {
		return nil, err
	}

	var results []ProfileCheck
	for _, srv := range servers {
		client, err := s.pool.GetClient(srv.Name)
		if err != nil {
			continue
		}
		existing, err := client.GetProfileNames()
		if err != nil {
			continue
		}
		existingSet := make(map[string]bool, len(existing))
		for _, p := range existing {
			existingSet[p] = true
		}
		for _, p := range profiles {
			results = append(results, ProfileCheck{
				Profile: p,
				Server:  srv.Name,
				Exists:  existingSet[p],
			})
		}
	}
	return results, nil
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
