package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/homaserver/plati/internal/auth"
	"github.com/homaserver/plati/internal/services"
)

type PreferencesHandler struct {
	prefSvc *services.PreferencesService
}

func NewPreferencesHandler(prefSvc *services.PreferencesService) *PreferencesHandler {
	return &PreferencesHandler{prefSvc: prefSvc}
}

func (h *PreferencesHandler) Get(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	prefs, err := h.prefSvc.Get(user.ID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(prefs)
}

func (h *PreferencesHandler) Update(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())

	var req struct {
		SSHKeyMode    string `json:"ssh_key_mode"`
		TailscaleMode string `json:"tailscale_mode"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	if err := h.prefSvc.Update(user.ID, req.SSHKeyMode, req.TailscaleMode); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	prefs, _ := h.prefSvc.Get(user.ID)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(prefs)
}
