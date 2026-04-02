package services

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/jmoiron/sqlx"
	"gopkg.in/yaml.v3"

	"github.com/homaserver/plati/internal/database/queries"
	"github.com/homaserver/plati/internal/models"
)

// MixinInfo is returned by ListMixins so the frontend knows what mixins are available.
type MixinInfo struct {
	Name     string   `json:"name"`
	Commands []string `json:"commands"`
}

type TemplateService struct {
	db     *sqlx.DB
	mixins map[string]MixinYAML
}

func NewTemplateService(db *sqlx.DB) *TemplateService {
	return &TemplateService{db: db, mixins: make(map[string]MixinYAML)}
}

type MixinFile struct {
	Src  string `yaml:"src"`
	Dest string `yaml:"dest"`
	Mode string `yaml:"mode,omitempty"`
}

type MixinYAML struct {
	Name               string      `yaml:"name"`
	Files              []MixinFile `yaml:"files,omitempty"`
	PostCreateCommands []string    `yaml:"post_create_commands,omitempty"`
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

type TemplateYAML struct {
	Name               string           `yaml:"name"`
	Slug               string           `yaml:"slug"`
	Description        string           `yaml:"description"`
	Image              string           `yaml:"image"`
	Profiles           []string         `yaml:"profiles"`
	Resources          map[string]any   `yaml:"resources"`
	CloudInit          string           `yaml:"cloud_init"`
	TerminalUser       string           `yaml:"terminal_user,omitempty"`
	Includes           []string         `yaml:"includes,omitempty"`
	PostCreateCommands []string         `yaml:"post_create_commands,omitempty"` // deprecated
	FirstInitCommands  []string         `yaml:"first_init_commands,omitempty"`
	RebuildCommands    []string         `yaml:"rebuild_commands,omitempty"`
	Persistence        *PersistenceYAML `yaml:"persistence,omitempty"`
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
	names := make([]string, 0, len(s.mixins))
	for name := range s.mixins {
		names = append(names, name)
	}
	sort.Strings(names)
	result := make([]MixinInfo, 0, len(names))
	for _, name := range names {
		m := s.mixins[name]
		result = append(result, MixinInfo{Name: name, Commands: m.PostCreateCommands})
	}
	return result
}

// GetMixin returns info for a single mixin by stem name.
func (s *TemplateService) GetMixin(name string) (MixinInfo, bool) {
	m, ok := s.mixins[name]
	if !ok {
		return MixinInfo{}, false
	}
	return MixinInfo{Name: name, Commands: m.PostCreateCommands}, true
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
		// Expand file entries into base64-write commands, prepended before post_create_commands.
		if len(m.Files) > 0 {
			var fileCmds []string
			for _, mf := range m.Files {
				filePath := filepath.Join(dir, "mixins", mf.Src)
				content, err := os.ReadFile(filePath)
				if err != nil {
					log.Printf("warning: mixin file %s: %v", filePath, err)
					continue
				}
				encoded := base64.StdEncoding.EncodeToString(content)
				fileCmds = append(fileCmds, fmt.Sprintf("printf '%%s' '%s' | base64 -d > %s", encoded, mf.Dest))
				if mf.Mode != "" {
					fileCmds = append(fileCmds, fmt.Sprintf("chmod %s %s", mf.Mode, mf.Dest))
				}
			}
			m.PostCreateCommands = append(fileCmds, m.PostCreateCommands...)
		}

		stem := strings.TrimSuffix(filepath.Base(f), ".yaml")
		s.mixins[stem] = m
		log.Printf("loaded mixin: %s (%d commands)", stem, len(m.PostCreateCommands))
	}
	return nil
}

func (s *TemplateService) templateFromYAML(data []byte) (*models.Template, error) {
	var ty TemplateYAML
	if err := yaml.Unmarshal(data, &ty); err != nil {
		return nil, fmt.Errorf("invalid YAML: %w", err)
	}

	var resolvedCmds []string
	for _, name := range ty.Includes {
		m, ok := s.mixins[name]
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

	return &models.Template{
		Name:               ty.Name,
		Slug:               ty.Slug,
		Description:        ty.Description,
		Image:              ty.Image,
		Profiles:           string(profiles),
		Resources:          string(resources),
		CloudInit:          ty.CloudInit,
		TerminalUser:       ty.TerminalUser,
		PostCreateCommands: string(postCmds),
		PersistenceMode:    persistenceMode,
		PersistenceDirs:    string(persistenceDirsJSON),
		FirstInitCommands:  string(firstInitJSON),
		RebuildCommands:    string(rebuildJSON),
		Includes:           string(includesJSON),
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

	ty := TemplateYAML{
		Name:              t.Name,
		Slug:              t.Slug,
		Description:       t.Description,
		Image:             t.Image,
		Profiles:          profiles,
		Resources:         resources,
		CloudInit:         t.CloudInit,
		TerminalUser:      t.TerminalUser,
		Includes:          includes,
		FirstInitCommands: firstInitCmds,
		RebuildCommands:   rebuildCmds,
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
