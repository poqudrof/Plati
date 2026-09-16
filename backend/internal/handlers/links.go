package handlers

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/homaserver/plati/internal/services"
)

// Links returns every link of an instance, resolved and flagged pinned or not: the
// tailnet hostname, OpenVSCode and SSHX with their current URLs, then the custom ones.
// The dashboard card lists the pinned ones; the Links tab manages them all.
func (h *InstanceHandler) Links(w http.ResponseWriter, r *http.Request) {
	actor, ok := requireActor(w, r)
	if !ok {
		return
	}
	id, err := parseID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	res, err := h.svc.GetLinks(id, actor)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, res)
}

// UpdateLinks applies a partial update: omitted fields keep their value, and "custom",
// when present, replaces the whole list.
func (h *InstanceHandler) UpdateLinks(w http.ResponseWriter, r *http.Request) {
	actor, ok := requireActor(w, r)
	if !ok {
		return
	}
	id, err := parseID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	var req services.UpdateLinksRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request")
		return
	}
	res, err := h.svc.UpdateLinks(id, actor, req)
	if err != nil {
		status := http.StatusBadRequest
		if strings.HasPrefix(err.Error(), "instance not found") {
			status = http.StatusNotFound
		}
		writeError(w, status, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, res)
}
