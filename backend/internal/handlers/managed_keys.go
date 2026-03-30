package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/homaserver/plati/internal/services"
)

type ManagedKeyHandler struct {
	svc *services.ManagedKeyService
}

func NewManagedKeyHandler(svc *services.ManagedKeyService) *ManagedKeyHandler {
	return &ManagedKeyHandler{svc: svc}
}

func (h *ManagedKeyHandler) List(w http.ResponseWriter, r *http.Request) {
	keys, err := h.svc.ListManagedKeys()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list managed keys")
		return
	}
	writeJSON(w, http.StatusOK, keys)
}

// Generate creates a new admin-managed keypair.
// The private key PEM is returned only in this response.
func (h *ManagedKeyHandler) Generate(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Name == "" {
		writeError(w, http.StatusBadRequest, "name required")
		return
	}

	key, privateKeyPEM, err := h.svc.GenerateManagedKey(req.Name)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to generate managed key")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"id":          key.ID,
		"name":        key.Name,
		"public_key":  key.PublicKey,
		"private_key": privateKeyPEM,
	})
}

func (h *ManagedKeyHandler) Delete(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	if err := h.svc.DeleteManagedKey(id); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete managed key")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"message": "deleted"})
}

func (h *ManagedKeyHandler) GetUsers(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	userIDs, err := h.svc.GetManagedKeyUsers(id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to get key users")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"user_ids": userIDs})
}

func (h *ManagedKeyHandler) SetUsers(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	var req struct {
		UserIDs []int64 `json:"user_ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request")
		return
	}
	if err := h.svc.SetManagedKeyUsers(id, req.UserIDs); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to set key users")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"message": "updated"})
}
