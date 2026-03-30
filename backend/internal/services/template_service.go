package services

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/jmoiron/sqlx"
	"gopkg.in/yaml.v3"

	"github.com/homaserver/plati/internal/database/queries"
	"github.com/homaserver/plati/internal/models"
)

type TemplateService struct {
	db *sqlx.DB
}

func NewTemplateService(db *sqlx.DB) *TemplateService {
	return &TemplateService{db: db}
}

type TemplateYAML struct {
	Name        string         `yaml:"name"`
	Slug        string         `yaml:"slug"`
	Description string         `yaml:"description"`
	Image       string         `yaml:"image"`
	Profiles    []string       `yaml:"profiles"`
	Resources   map[string]any `yaml:"resources"`
	CloudInit   string         `yaml:"cloud_init"`
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

func (s *TemplateService) ImportYAML(data []byte) (*models.Template, error) {
	var ty TemplateYAML
	if err := yaml.Unmarshal(data, &ty); err != nil {
		return nil, fmt.Errorf("invalid YAML: %w", err)
	}

	profiles, _ := json.Marshal(ty.Profiles)
	resources, _ := json.Marshal(ty.Resources)

	t := &models.Template{
		Name:        ty.Name,
		Slug:        ty.Slug,
		Description: ty.Description,
		Image:       ty.Image,
		Profiles:    string(profiles),
		Resources:   string(resources),
		CloudInit:   ty.CloudInit,
		IsActive:    true,
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

	ty := TemplateYAML{
		Name:        t.Name,
		Slug:        t.Slug,
		Description: t.Description,
		Image:       t.Image,
		Profiles:    profiles,
		Resources:   resources,
		CloudInit:   t.CloudInit,
	}

	return yaml.Marshal(ty)
}

func (s *TemplateService) ImportFromDir(dir string) error {
	files, err := filepath.Glob(filepath.Join(dir, "*.yaml"))
	if err != nil {
		return err
	}

	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			return fmt.Errorf("read %s: %w", f, err)
		}
		if _, err := s.ImportYAML(data); err != nil {
			// Skip duplicates
			continue
		}
	}
	return nil
}
