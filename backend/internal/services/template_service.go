package services

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/jmoiron/sqlx"
	"gopkg.in/yaml.v3"

	"github.com/homaserver/plati/internal/database/queries"
	"github.com/homaserver/plati/internal/incus"
	"github.com/homaserver/plati/internal/models"
)

// MixinFileInfo describes a file pushed by a mixin.
type MixinFileInfo struct {
	Src  string `json:"src"`
	Dest string `json:"dest"`
	Mode string `json:"mode"`
}

// MixinInfo is returned by ListMixins so the frontend knows what mixins are available.
type MixinInfo struct {
	Name     string          `json:"name"`
	Commands []string        `json:"commands"`
	Files    []MixinFileInfo `json:"files"`
}

type TemplateService struct {
	db           *sqlx.DB
	mixins       map[string]MixinYAML
	mu           sync.RWMutex
	templatesDir string
}

func NewTemplateService(db *sqlx.DB, templatesDir string) *TemplateService {
	return &TemplateService{db: db, mixins: make(map[string]MixinYAML), templatesDir: templatesDir}
}

type MixinFile struct {
	Src  string `yaml:"src"`
	Dest string `yaml:"dest"`
	Mode string `yaml:"mode,omitempty"`
}

// resolvedMixinFile holds the absolute host path for a mixin file after LoadMixinsFromDir.
type resolvedMixinFile struct {
	SrcPath string // absolute path on host
	Dest    string // destination path inside container
	Mode    int    // unix file mode (e.g. 0755)
}

type MixinYAML struct {
	Name               string              `yaml:"name"`
	Files              []MixinFile         `yaml:"files,omitempty"`
	PostCreateCommands []string            `yaml:"post_create_commands,omitempty"`
	resolvedFiles      []resolvedMixinFile // populated at load time, not from YAML
}

type PersistenceDirYAML struct {
	Path string `yaml:"path"`
	Size string `yaml:"size"`
	Pool string `yaml:"pool,omitempty"`
}

type PersistenceYAML struct {
	Mode        string               `yaml:"mode"`        // "normal" | "ephemeral"
	Directories []PersistenceDirYAML `yaml:"directories"`
}

type RepoRefYAML struct {
	Name string `yaml:"name"`
	Dest string `yaml:"dest"`
}

type HealthCheckYAML struct {
	Port           int    `yaml:"port" json:"port"`
	Path           string `yaml:"path" json:"path"`
	ExpectedStatus int    `yaml:"expected_status" json:"expected_status"`
	Timeout        int    `yaml:"timeout" json:"timeout"` // seconds to wait
	Description    string `yaml:"description" json:"description"`
}

type TailscaleServeYAML struct {
	Port   int  `yaml:"port" json:"port"`
	Funnel bool `yaml:"funnel" json:"funnel"`
}

type TemplateYAML struct {
	Name               string               `yaml:"name"`
	Slug               string               `yaml:"slug"`
	Description        string               `yaml:"description"`
	Image              string               `yaml:"image"`
	Profiles           []string             `yaml:"profiles"`
	Resources          map[string]any       `yaml:"resources"`
	TerminalUser       string               `yaml:"terminal_user,omitempty"`
	Includes           []string             `yaml:"includes,omitempty"`
	PostCreateCommands []string             `yaml:"post_create_commands,omitempty"` // deprecated
	FirstInitCommands  []string             `yaml:"first_init_commands,omitempty"`
	RebuildCommands    []string             `yaml:"rebuild_commands,omitempty"`
	Persistence        *PersistenceYAML     `yaml:"persistence,omitempty"`
	Repos              []RepoRefYAML        `yaml:"repos,omitempty"`
	HealthChecks       []HealthCheckYAML    `yaml:"health_checks,omitempty"`
	TailscaleServe     *TailscaleServeYAML  `yaml:"tailscale_serve,omitempty"`
}

func (s *TemplateService) List(activeOnly bool) ([]models.Template, error) {
	return queries.ListTemplates(s.db, activeOnly)
}

func (s *TemplateService) Get(id int64) (*models.Template, error) {
	return queries.GetTemplate(s.db, id)
}

func (s *TemplateService) Create(t *models.Template) (int64, error) {
	return queries.CreateTemplate(s.db, t)
}

func (s *TemplateService) Update(t *models.Template) error {
	return queries.UpdateTemplate(s.db, t)
}

func (s *TemplateService) Delete(id int64) error {
	return queries.DeleteTemplate(s.db, id)
}

// ListMixins returns all loaded mixins sorted by name.
func (s *TemplateService) ListMixins() []MixinInfo {
	s.mu.RLock()
	defer s.mu.RUnlock()
	names := make([]string, 0, len(s.mixins))
	for name := range s.mixins {
		names = append(names, name)
	}
	sort.Strings(names)
	result := make([]MixinInfo, 0, len(names))
	for _, name := range names {
		m := s.mixins[name]
		result = append(result, MixinInfo{Name: name, Commands: m.PostCreateCommands, Files: mixinFilesInfo(m)})
	}
	return result
}

// GetMixin returns info for a single mixin by stem name.
func (s *TemplateService) GetMixin(name string) (MixinInfo, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	m, ok := s.mixins[name]
	if !ok {
		return MixinInfo{}, false
	}
	return MixinInfo{Name: name, Commands: m.PostCreateCommands, Files: mixinFilesInfo(m)}, true
}

func mixinFilesInfo(m MixinYAML) []MixinFileInfo {
	files := make([]MixinFileInfo, len(m.Files))
	for i, f := range m.Files {
		mode := f.Mode
		if mode == "" {
			mode = "0644"
		}
		files[i] = MixinFileInfo{Src: f.Src, Dest: f.Dest, Mode: mode}
	}
	return files
}

// SaveToDisk writes the template YAML to the templates directory on disk.
func (s *TemplateService) SaveToDisk(id int64) error {
	if s.templatesDir == "" {
		return fmt.Errorf("templates directory not configured")
	}
	t, err := queries.GetTemplate(s.db, id)
	if err != nil {
		return fmt.Errorf("get template: %w", err)
	}
	yamlBytes, err := s.ExportYAML(id)
	if err != nil {
		return fmt.Errorf("export yaml: %w", err)
	}
	return os.WriteFile(filepath.Join(s.templatesDir, t.Slug+".yaml"), yamlBytes, 0644)
}

// ReloadFromDisk reads the template's YAML file from disk and updates the DB record.
func (s *TemplateService) ReloadFromDisk(id int64) (*models.Template, error) {
	if s.templatesDir == "" {
		return nil, fmt.Errorf("templates directory not configured")
	}
	t, err := queries.GetTemplate(s.db, id)
	if err != nil {
		return nil, fmt.Errorf("get template: %w", err)
	}
	data, err := os.ReadFile(filepath.Join(s.templatesDir, t.Slug+".yaml"))
	if err != nil {
		return nil, fmt.Errorf("read yaml file: %w", err)
	}
	return s.UpdateFromYAML(id, data)
}

// GetMixinFileSteps returns PushFile setup steps for all files declared in the given mixin names.
// These steps transfer large binaries (tarballs, executables) into the instance using the
// Incus file API — no shell embedding, no bind-mounts.
func (s *TemplateService) GetMixinFileSteps(names []string) []incus.SetupStep {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var steps []incus.SetupStep
	for _, name := range names {
		m, ok := s.mixins[name]
		if !ok {
			continue
		}
		for _, rf := range m.resolvedFiles {
			steps = append(steps, incus.SetupStep{
				Label:          fmt.Sprintf("Push %s → %s", filepath.Base(rf.SrcPath), rf.Dest),
				FileDest:       rf.Dest,
				FileSourcePath: rf.SrcPath,
				FileMode:       rf.Mode,
			})
		}
	}
	return steps
}

// DuplicateTemplate clones template id with a new name and slug.
func (s *TemplateService) DuplicateTemplate(id int64, newName, newSlug string) (*models.Template, error) {
	src, err := queries.GetTemplate(s.db, id)
	if err != nil {
		return nil, fmt.Errorf("get template: %w", err)
	}
	clone := *src
	clone.ID = 0
	clone.Name = newName
	clone.Slug = newSlug
	newID, err := queries.CreateTemplate(s.db, &clone)
	if err != nil {
		return nil, fmt.Errorf("create template: %w", err)
	}
	clone.ID = newID
	return &clone, nil
}

// UpdateFromYAML parses YAML, sets the template ID, and persists to DB.
func (s *TemplateService) UpdateFromYAML(id int64, data []byte) (*models.Template, error) {
	t, err := s.templateFromYAML(data)
	if err != nil {
		return nil, err
	}
	t.ID = id
	if err := queries.UpdateTemplate(s.db, t); err != nil {
		return nil, fmt.Errorf("update template: %w", err)
	}
	return t, nil
}

func (s *TemplateService) LoadMixinsFromDir(dir string) error {
	files, _ := filepath.Glob(filepath.Join(dir, "mixins", "*.yaml"))
	newMixins := make(map[string]MixinYAML)
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			log.Printf("warning: read mixin %s: %v", f, err)
			continue
		}
		var m MixinYAML
		if err := yaml.Unmarshal(data, &m); err != nil {
			log.Printf("warning: parse mixin %s: %v", f, err)
			continue
		}
		// Resolve file entries to absolute host paths.
		// Files are pushed into the instance via PushFile at creation time.
		for _, mf := range m.Files {
			srcPath := filepath.Join(dir, "mixins", mf.Src)
			if _, err := os.Stat(srcPath); err != nil {
				log.Printf("warning: mixin file %s not found: %v", srcPath, err)
				continue
			}
			mode := 0644
			if mf.Mode != "" {
				if parsed, err := strconv.ParseInt(mf.Mode, 8, 32); err == nil {
					mode = int(parsed)
				}
			}
			m.resolvedFiles = append(m.resolvedFiles, resolvedMixinFile{
				SrcPath: srcPath,
				Dest:    mf.Dest,
				Mode:    mode,
			})
		}

		stem := strings.TrimSuffix(filepath.Base(f), ".yaml")
		newMixins[stem] = m
		log.Printf("loaded mixin: %s (%d commands)", stem, len(m.PostCreateCommands))
	}
	s.mu.Lock()
	s.mixins = newMixins
	s.mu.Unlock()
	return nil
}

func (s *TemplateService) templateFromYAML(data []byte) (*models.Template, error) {
	var ty TemplateYAML
	if err := yaml.Unmarshal(data, &ty); err != nil {
		return nil, fmt.Errorf("invalid YAML: %w", err)
	}

	s.mu.RLock()
	mixins := s.mixins
	s.mu.RUnlock()

	var resolvedCmds []string
	for _, name := range ty.Includes {
		m, ok := mixins[name]
		if !ok {
			log.Printf("warning: mixin %q not found, skipping", name)
			continue
		}
		resolvedCmds = append(resolvedCmds, m.PostCreateCommands...)
	}
	resolvedCmds = append(resolvedCmds, ty.PostCreateCommands...)

	profiles, _ := json.Marshal(ty.Profiles)
	resources, _ := json.Marshal(ty.Resources)
	postCmds, _ := json.Marshal(resolvedCmds)
	if string(postCmds) == "null" {
		postCmds = []byte("[]")
	}

	// Resolve first_init / rebuild commands.
	firstInitCmds := ty.FirstInitCommands
	if len(firstInitCmds) == 0 && len(resolvedCmds) > 0 {
		// Backward compat: post_create_commands treated as first_init_commands.
		firstInitCmds = resolvedCmds
	}
	firstInitJSON, _ := json.Marshal(firstInitCmds)
	if string(firstInitJSON) == "null" {
		firstInitJSON = []byte("[]")
	}
	rebuildJSON, _ := json.Marshal(ty.RebuildCommands)
	if string(rebuildJSON) == "null" {
		rebuildJSON = []byte("[]")
	}

	// Resolve persistence config.
	persistenceMode := "normal"
	persistenceDirsJSON := []byte("[]")
	if ty.Persistence != nil {
		if ty.Persistence.Mode == "ephemeral" {
			persistenceMode = "ephemeral"
		}
		dirsJSON, _ := json.Marshal(ty.Persistence.Directories)
		if string(dirsJSON) != "null" {
			persistenceDirsJSON = dirsJSON
		}
	} else {
		// Backward compat: derive single workspace dir from resources.disk.
		diskSize := "20GB"
		if ty.Resources != nil {
			if d, ok := ty.Resources["disk"]; ok {
				diskSize = fmt.Sprintf("%v", d)
			}
		}
		defaultDirs := []PersistenceDirYAML{{Path: "/workspace", Size: diskSize, Pool: "default"}}
		persistenceDirsJSON, _ = json.Marshal(defaultDirs)
	}

	includesJSON, _ := json.Marshal(ty.Includes)
	if string(includesJSON) == "null" {
		includesJSON = []byte("[]")
	}

	reposJSON, _ := json.Marshal(ty.Repos)
	if string(reposJSON) == "null" {
		reposJSON = []byte("[]")
	}

	healthChecksJSON, _ := json.Marshal(ty.HealthChecks)
	if string(healthChecksJSON) == "null" {
		healthChecksJSON = []byte("[]")
	}

	tailscaleServeJSON := ""
	if ty.TailscaleServe != nil && ty.TailscaleServe.Port > 0 {
		tsj, _ := json.Marshal(ty.TailscaleServe)
		tailscaleServeJSON = string(tsj)

		// Generate tailscale serve commands appended to first_init_commands.
		waitCmd := `for i in $(seq 1 30); do tailscale status >/dev/null 2>&1 && break; sleep 2; done`
		serveCmd := fmt.Sprintf("tailscale serve --bg https+insecure://localhost:%d", ty.TailscaleServe.Port)
		if ty.TailscaleServe.Funnel {
			serveCmd = fmt.Sprintf("tailscale funnel --bg https+insecure://localhost:%d", ty.TailscaleServe.Port)
		}
		firstInitCmds = append(firstInitCmds, waitCmd, serveCmd)
		firstInitJSON, _ = json.Marshal(firstInitCmds)
	}

	return &models.Template{
		Name:               ty.Name,
		Slug:               ty.Slug,
		Description:        ty.Description,
		Image:              ty.Image,
		Profiles:           string(profiles),
		Resources:          string(resources),
		TerminalUser:       ty.TerminalUser,
		PostCreateCommands: string(postCmds),
		PersistenceMode:    persistenceMode,
		PersistenceDirs:    string(persistenceDirsJSON),
		FirstInitCommands:  string(firstInitJSON),
		RebuildCommands:    string(rebuildJSON),
		Includes:           string(includesJSON),
		Repos:              string(reposJSON),
		HealthChecks:       string(healthChecksJSON),
		TailscaleServe:     tailscaleServeJSON,
		IsActive:           true,
	}, nil
}

func (s *TemplateService) ImportYAML(data []byte) (*models.Template, error) {
	t, err := s.templateFromYAML(data)
	if err != nil {
		return nil, err
	}
	id, err := queries.CreateTemplate(s.db, t)
	if err != nil {
		return nil, err
	}
	t.ID = id
	return t, nil
}

func (s *TemplateService) ExportYAML(id int64) ([]byte, error) {
	t, err := queries.GetTemplate(s.db, id)
	if err != nil {
		return nil, err
	}

	var profiles []string
	json.Unmarshal([]byte(t.Profiles), &profiles)
	var resources map[string]any
	json.Unmarshal([]byte(t.Resources), &resources)
	var postCmds []string
	json.Unmarshal([]byte(t.PostCreateCommands), &postCmds)
	var firstInitCmds []string
	json.Unmarshal([]byte(t.FirstInitCommands), &firstInitCmds)
	var rebuildCmds []string
	json.Unmarshal([]byte(t.RebuildCommands), &rebuildCmds)
	var persistenceDirs []PersistenceDirYAML
	json.Unmarshal([]byte(t.PersistenceDirs), &persistenceDirs)
	var includes []string
	json.Unmarshal([]byte(t.Includes), &includes)
	var repos []RepoRefYAML
	json.Unmarshal([]byte(t.Repos), &repos)
	var healthChecks []HealthCheckYAML
	json.Unmarshal([]byte(t.HealthChecks), &healthChecks)
	var tailscaleServe *TailscaleServeYAML
	if t.TailscaleServe != "" {
		json.Unmarshal([]byte(t.TailscaleServe), &tailscaleServe)
	}

	ty := TemplateYAML{
		Name:              t.Name,
		Slug:              t.Slug,
		Description:       t.Description,
		Image:             t.Image,
		Profiles:          profiles,
		Resources:         resources,
		TerminalUser:      t.TerminalUser,
		Includes:          includes,
		FirstInitCommands: firstInitCmds,
		RebuildCommands:   rebuildCmds,
		Repos:             repos,
		HealthChecks:      healthChecks,
		TailscaleServe:    tailscaleServe,
		Persistence: &PersistenceYAML{
			Mode:        t.PersistenceMode,
			Directories: persistenceDirs,
		},
	}

	return yaml.Marshal(ty)
}

// SyncFromDir syncs the templates table with YAML files on disk.
// Templates present on disk are upserted; DB templates with no matching YAML are deleted.
func (s *TemplateService) SyncFromDir(dir string) error {
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		log.Printf("warning: templates dir does not exist: %s (skipping sync)", dir)
		return nil
	}

	if err := s.LoadMixinsFromDir(dir); err != nil {
		log.Printf("warning: load mixins: %v", err)
	}

	files, err := filepath.Glob(filepath.Join(dir, "*.yaml"))
	if err != nil {
		return err
	}

	yamlSlugs := make(map[string]bool)

	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			log.Printf("warning: read %s: %v", f, err)
			continue
		}
		t, err := s.templateFromYAML(data)
		if err != nil {
			log.Printf("warning: parse %s: %v", f, err)
			continue
		}
		yamlSlugs[t.Slug] = true

		existing, err := queries.GetTemplateBySlug(s.db, t.Slug)
		if err == nil {
			t.ID = existing.ID
			if err := queries.UpdateTemplate(s.db, t); err != nil {
				log.Printf("warning: update template %s: %v", t.Slug, err)
			} else {
				log.Printf("synced template: %s (updated)", t.Slug)
			}
		} else {
			id, err := queries.CreateTemplate(s.db, t)
			if err != nil {
				log.Printf("warning: create template %s: %v", t.Slug, err)
			} else {
				log.Printf("synced template: %s (created, id=%d)", t.Slug, id)
			}
		}
	}

	// Delete DB templates that no longer have a YAML file on disk.
	allTemplates, err := queries.ListTemplates(s.db, false)
	if err != nil {
		return fmt.Errorf("list templates for cleanup: %w", err)
	}
	for _, t := range allTemplates {
		if !yamlSlugs[t.Slug] {
			if err := queries.DeleteTemplate(s.db, t.ID); err != nil {
				log.Printf("warning: delete stale template %s: %v", t.Slug, err)
			} else {
				log.Printf("synced template: %s (deleted, no YAML on disk)", t.Slug)
			}
		}
	}

	return nil
}

// WatchDir watches dir for YAML changes and re-runs SyncFromDir on any write event.
// It debounces rapid successive changes with a 500ms delay.
// The goroutine exits when ctx is cancelled.
func (s *TemplateService) WatchDir(ctx context.Context, dir string) error {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return fmt.Errorf("create watcher: %w", err)
	}

	for _, d := range []string{dir, filepath.Join(dir, "mixins")} {
		if _, err := os.Stat(d); err == nil {
			if err := watcher.Add(d); err != nil {
				log.Printf("warning: watch %s: %v", d, err)
			}
		}
	}

	go func() {
		defer watcher.Close()
		var debounce *time.Timer
		for {
			select {
			case <-ctx.Done():
				if debounce != nil {
					debounce.Stop()
				}
				return
			case event, ok := <-watcher.Events:
				if !ok {
					return
				}
				if !strings.HasSuffix(event.Name, ".yaml") {
					continue
				}
				if event.Op&(fsnotify.Write|fsnotify.Create|fsnotify.Remove|fsnotify.Rename) == 0 {
					continue
				}
				if debounce != nil {
					debounce.Stop()
				}
				debounce = time.AfterFunc(500*time.Millisecond, func() {
					log.Printf("templates changed (%s), reloading...", filepath.Base(event.Name))
					if err := s.SyncFromDir(dir); err != nil {
						log.Printf("warning: reload templates: %v", err)
					}
				})
			case err, ok := <-watcher.Errors:
				if !ok {
					return
				}
				log.Printf("warning: template watcher: %v", err)
			}
		}
	}()

	return nil
}
