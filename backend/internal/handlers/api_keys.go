package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/homaserver/plati/internal/auth"
	"github.com/homaserver/plati/internal/models"
	"github.com/homaserver/plati/internal/services"
)

type APIKeyHandler struct {
	svc *services.APIKeyService
}

func NewAPIKeyHandler(svc *services.APIKeyService) *APIKeyHandler {
	return &APIKeyHandler{svc: svc}
}

func regenerateResponse(plaintext string, key *models.APIKey) map[string]interface{} {
	return map[string]interface{}{
		"id":         key.ID,
		"name":       key.Name,
		"key_prefix": key.KeyPrefix,
		"key":        plaintext, // shown only once
	}
}

// ListMine returns the caller's own API keys.
func (h *APIKeyHandler) ListMine(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	keys, err := h.svc.List(user.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, keys)
}

// RegenerateMine rotates one of the caller's own API keys.
func (h *APIKeyHandler) RegenerateMine(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	id, err := parseID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	plaintext, key, err := h.svc.Regenerate(id, user.ID)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, regenerateResponse(plaintext, key))
}

// AdminList returns a given user's API keys.
func (h *APIKeyHandler) AdminList(w http.ResponseWriter, r *http.Request) {
	userID, err := parseID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	keys, err := h.svc.List(userID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, keys)
}

// AdminCreate creates a new API key for a given user.
func (h *APIKeyHandler) AdminCreate(w http.ResponseWriter, r *http.Request) {
	userID, err := parseID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}

	var req struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}

	plaintext, key, err := h.svc.Create(userID, req.Name)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, regenerateResponse(plaintext, key))
}

// AdminRegenerate rotates a given user's API key.
func (h *APIKeyHandler) AdminRegenerate(w http.ResponseWriter, r *http.Request) {
	userID, err := parseID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	keyID, err := parseID(r, "key_id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid key id")
		return
	}
	plaintext, key, err := h.svc.Regenerate(keyID, userID)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, regenerateResponse(plaintext, key))
}
