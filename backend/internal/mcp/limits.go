package mcp

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/homaserver/plati/internal/auth"
	"github.com/homaserver/plati/internal/database/queries"
)

// Limits caps what one user can create through MCP. They apply to admins too: an
// agent looping on create_instance should hit a wall, whoever owns the key.
type Limits struct {
	// MaxInstances is how many MCP-created instances a user may own at once; 0 turns
	// creation through MCP off. Instances created in the web UI do not count.
	MaxInstances int `json:"max_instances"`
	// MaxCreationsPerHour counts creations in a sliding hour, deleted instances
	// included, so delete-and-recreate cannot get around it.
	MaxCreationsPerHour int `json:"max_creations_per_hour"`
}

var DefaultLimits = Limits{MaxInstances: 3, MaxCreationsPerHour: 5}

const (
	settingMaxInstances = "mcp_max_instances"
	settingMaxPerHour   = "mcp_max_creations_per_hour"

	maxInstancesCeiling = 50
	maxPerHourCeiling   = 100
)

func (s *Server) limits() Limits {
	l := DefaultLimits
	if v, err := queries.GetSetting(s.db, settingMaxInstances); err == nil {
		if n, err := strconv.Atoi(v); err == nil {
			l.MaxInstances = n
		}
	}
	if v, err := queries.GetSetting(s.db, settingMaxPerHour); err == nil {
		if n, err := strconv.Atoi(v); err == nil {
			l.MaxCreationsPerHour = n
		}
	}
	return l
}

// Quota is the caller's view of the limits, returned by get_mcp_quota and attached to
// every refusal so the model knows what it can do next.
type Quota struct {
	Limits
	Instances            int        `json:"instances"`
	CreationsLastHour    int        `json:"creations_last_hour"`
	NextCreationPossible *time.Time `json:"next_creation_possible_at,omitempty"`
}

func (s *Server) quota(userID int64) (Quota, error) {
	q := Quota{Limits: s.limits()}
	var err error
	if q.Instances, err = queries.CountMCPInstances(s.db, userID); err != nil {
		return q, err
	}
	var freeAt time.Time
	if q.CreationsLastHour, freeAt, err = queries.MCPCreationsLastHour(s.db, userID); err != nil {
		return q, err
	}
	if q.CreationsLastHour >= q.MaxCreationsPerHour && !freeAt.IsZero() {
		q.NextCreationPossible = &freeAt
	}
	return q, nil
}

// checkCreate returns nil when the user may create one more instance through MCP.
func (q Quota) checkCreate() error {
	switch {
	case q.MaxInstances <= 0:
		return fmt.Errorf("creating instances through MCP is disabled by the Plati administrator")
	case q.Instances >= q.MaxInstances:
		return fmt.Errorf("quota reached: you already own %d instances created through MCP (limit %d). "+
			"Delete one in the Plati web UI, or ask an administrator to raise the limit", q.Instances, q.MaxInstances)
	case q.MaxCreationsPerHour <= 0:
		return fmt.Errorf("creating instances through MCP is disabled by the Plati administrator")
	case q.CreationsLastHour >= q.MaxCreationsPerHour:
		when := "later"
		if q.NextCreationPossible != nil {
			when = "after " + q.NextCreationPossible.UTC().Format("15:04 UTC")
		}
		return fmt.Errorf("rate limit reached: %d instances created through MCP in the last hour (limit %d). Try again %s",
			q.CreationsLastHour, q.MaxCreationsPerHour, when)
	}
	return nil
}

// GetLimits serves GET /api/v1/admin/settings/mcp-limits.
func (s *Server) GetLimits(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.limits())
}

// PutLimits serves PUT /api/v1/admin/settings/mcp-limits. Both fields are required.
func (s *Server) PutLimits(w http.ResponseWriter, r *http.Request) {
	var req struct {
		MaxInstances        *int `json:"max_instances"`
		MaxCreationsPerHour *int `json:"max_creations_per_hour"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.MaxInstances == nil || req.MaxCreationsPerHour == nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "max_instances and max_creations_per_hour are required"})
		return
	}
	if *req.MaxInstances < 0 || *req.MaxInstances > maxInstancesCeiling {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": fmt.Sprintf("max_instances must be between 0 and %d", maxInstancesCeiling)})
		return
	}
	if *req.MaxCreationsPerHour < 0 || *req.MaxCreationsPerHour > maxPerHourCeiling {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": fmt.Sprintf("max_creations_per_hour must be between 0 and %d", maxPerHourCeiling)})
		return
	}
	if err := queries.SetSetting(s.db, settingMaxInstances, strconv.Itoa(*req.MaxInstances)); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to save limits"})
		return
	}
	if err := queries.SetSetting(s.db, settingMaxPerHour, strconv.Itoa(*req.MaxCreationsPerHour)); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to save limits"})
		return
	}
	writeJSON(w, http.StatusOK, s.limits())
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// GetQuota serves GET /api/v1/mcp/quota: the caller's own limits and usage, for the
// settings page (the same answer as the get_mcp_quota tool).
func (s *Server) GetQuota(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	if user == nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	q, err := s.quota(user.ID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to read quota"})
		return
	}
	writeJSON(w, http.StatusOK, q)
}

// GetInfo serves GET /api/v1/mcp/info for the web UI's /agents page: the caller's quota
// and the tool list straight from the server, so the page never drifts from it.
func (s *Server) GetInfo(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	if user == nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	q, err := s.quota(user.ID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to read quota"})
		return
	}
	type toolInfo struct {
		Name        string `json:"name"`
		Title       string `json:"title"`
		Description string `json:"description"`
		ReadOnly    bool   `json:"read_only"`
	}
	list := make([]toolInfo, 0, len(tools))
	for _, t := range tools {
		list = append(list, toolInfo{Name: t.name, Title: t.title, Description: t.description, ReadOnly: t.readOnly})
	}
	writeJSON(w, http.StatusOK, map[string]any{"quota": q, "tools": list, "protocol_version": supportedVersions[0]})
}
