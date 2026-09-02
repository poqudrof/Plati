package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/homaserver/plati/internal/auth"
	"github.com/homaserver/plati/internal/services"
)

// SleepHandler exposes the per-instance auto-stop policy — the "Status" tab on
// the instance page.
type SleepHandler struct {
	svc *services.SleepService
}

func NewSleepHandler(svc *services.SleepService) *SleepHandler {
	return &SleepHandler{svc: svc}
}

func (h *SleepHandler) Get(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	id, err := parseID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	set, err := h.svc.GetSettings(id, user.ID)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, set)
}

// updateSleepRequest uses pointers so a caller can change one field without
// having to restate the other.
type updateSleepRequest struct {
	Disabled       *bool `json:"disabled"`
	TimeoutMinutes *int  `json:"timeout_minutes"`
}

func (h *SleepHandler) Update(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	id, err := parseID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	var req updateSleepRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request")
		return
	}

	current, err := h.svc.GetSettings(id, user.ID)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	disabled := current.Disabled
	if req.Disabled != nil {
		disabled = *req.Disabled
	}
	timeout := current.TimeoutMinutes
	if req.TimeoutMinutes != nil {
		timeout = *req.TimeoutMinutes
	}

	set, err := h.svc.UpdateSettings(id, user.ID, disabled, timeout)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, set)
}

// Reset pushes the auto-stop deadline back by a full timeout.
func (h *SleepHandler) Reset(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	id, err := parseID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	set, err := h.svc.ResetTimer(id, user.ID)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, set)
}
