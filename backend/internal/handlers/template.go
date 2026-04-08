package handlers

import (
	"encoding/json"
	"io"
	"net/http"

	"github.com/homaserver/plati/internal/models"
	"github.com/homaserver/plati/internal/services"
)

type TemplateHandler struct {
	svc *services.TemplateService
}

func NewTemplateHandler(svc *services.TemplateService) *TemplateHandler {
	return &TemplateHandler{svc: svc}
}

func (h *TemplateHandler) List(w http.ResponseWriter, r *http.Request) {
	// Non-admin users only see active templates
	activeOnly := r.URL.Query().Get("all") != "true"
	templates, err := h.svc.List(activeOnly)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list templates")
		return
	}
	writeJSON(w, http.StatusOK, templates)
}

func (h *TemplateHandler) Get(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	tmpl, err := h.svc.Get(id)
	if err != nil {
		writeError(w, http.StatusNotFound, "template not found")
		return
	}
	writeJSON(w, http.StatusOK, tmpl)
}

func (h *TemplateHandler) Create(w http.ResponseWriter, r *http.Request) {
	var tmpl models.Template
	if err := json.NewDecoder(r.Body).Decode(&tmpl); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request")
		return
	}
	tmpl.IsActive = true
	id, err := h.svc.Create(&tmpl)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create template")
		return
	}
	tmpl.ID = id
	writeJSON(w, http.StatusCreated, tmpl)
}

func (h *TemplateHandler) Update(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	var tmpl models.Template
	if err := json.NewDecoder(r.Body).Decode(&tmpl); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request")
		return
	}
	tmpl.ID = id
	if err := h.svc.Update(&tmpl); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update template")
		return
	}
	writeJSON(w, http.StatusOK, tmpl)
}

func (h *TemplateHandler) Delete(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	if err := h.svc.Delete(id); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete template")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"message": "deleted"})
}

func (h *TemplateHandler) Export(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	data, err := h.svc.ExportYAML(id)
	if err != nil {
		writeError(w, http.StatusNotFound, "template not found")
		return
	}
	w.Header().Set("Content-Type", "application/yaml")
	w.Header().Set("Content-Disposition", "attachment; filename=template.yaml")
	w.Write(data)
}

func (h *TemplateHandler) Import(w http.ResponseWriter, r *http.Request) {
	data, err := io.ReadAll(io.LimitReader(r.Body, 1<<20)) // 1MB limit
	if err != nil {
		writeError(w, http.StatusBadRequest, "failed to read body")
		return
	}
	tmpl, err := h.svc.ImportYAML(data)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, tmpl)
}

// ListMixins returns all available mixins loaded from disk.
func (h *TemplateHandler) ListMixins(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, h.svc.ListMixins())
}

// Duplicate clones a template with a new name and slug.
func (h *TemplateHandler) Duplicate(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	var req struct {
		Name string `json:"name"`
		Slug string `json:"slug"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Name == "" || req.Slug == "" {
		writeError(w, http.StatusBadRequest, "name and slug are required")
		return
	}
	tmpl, err := h.svc.DuplicateTemplate(id, req.Name, req.Slug)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, tmpl)
}

// ExportYAMLAsJSON returns the template YAML as a JSON-wrapped string.
func (h *TemplateHandler) ExportYAMLAsJSON(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	data, err := h.svc.ExportYAML(id)
	if err != nil {
		writeError(w, http.StatusNotFound, "template not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"yaml": string(data)})
}

// SaveToDisk writes the template YAML back to the templates directory on disk.
func (h *TemplateHandler) SaveToDisk(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	if err := h.svc.SaveToDisk(id); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"message": "saved"})
}

// UpdateFromYAML parses a YAML string from the request body and updates the template.
func (h *TemplateHandler) UpdateFromYAML(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	var req struct {
		YAML string `json:"yaml"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.YAML == "" {
		writeError(w, http.StatusBadRequest, "yaml field is required")
		return
	}
	tmpl, err := h.svc.UpdateFromYAML(id, []byte(req.YAML))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, tmpl)
}
