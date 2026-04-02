package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/homaserver/plati/internal/services"
)

type AdminSettingsHandler struct {
	adminSvc *services.AdminSettingsService
}

func NewAdminSettingsHandler(adminSvc *services.AdminSettingsService) *AdminSettingsHandler {
	return &AdminSettingsHandler{adminSvc: adminSvc}
}

// GetTailscaleKey returns whether the platform Tailscale key is configured (never returns plaintext).
func (h *AdminSettingsHandler) GetTailscaleKey(w http.ResponseWriter, r *http.Request) {
	key, err := h.adminSvc.GetPlatformTailscaleKey()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]bool{"configured": key != ""})
}

// SetTailscaleKey sets the platform Tailscale auth key.
func (h *AdminSettingsHandler) SetTailscaleKey(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Value string `json:"value"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if req.Value == "" {
		http.Error(w, "value is required", http.StatusBadRequest)
		return
	}
	if err := h.adminSvc.SetPlatformTailscaleKey(req.Value); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]bool{"configured": true})
}

// DeleteTailscaleKey removes the platform Tailscale auth key.
func (h *AdminSettingsHandler) DeleteTailscaleKey(w http.ResponseWriter, r *http.Request) {
	if err := h.adminSvc.DeletePlatformTailscaleKey(); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "deleted"})
}
