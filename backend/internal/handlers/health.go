package handlers

import (
	"net/http"

	"github.com/jmoiron/sqlx"
)

type HealthHandler struct {
	db *sqlx.DB
}

func NewHealthHandler(db *sqlx.DB) *HealthHandler {
	return &HealthHandler{db: db}
}

func (h *HealthHandler) Health(w http.ResponseWriter, r *http.Request) {
	status := "ok"
	dbStatus := "ok"
	if err := h.db.Ping(); err != nil {
		dbStatus = "error"
		status = "degraded"
	}

	writeJSON(w, http.StatusOK, map[string]string{
		"status":   status,
		"database": dbStatus,
	})
}
