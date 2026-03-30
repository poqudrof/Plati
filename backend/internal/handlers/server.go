package handlers

import (
	"net/http"

	"github.com/homaserver/plati/internal/incus"
	"github.com/homaserver/plati/internal/services"
)

type ServerHandler struct {
	svc  *services.ServerService
	pool *incus.Pool
}

func NewServerHandler(svc *services.ServerService, pool *incus.Pool) *ServerHandler {
	return &ServerHandler{svc: svc, pool: pool}
}

func (h *ServerHandler) List(w http.ResponseWriter, r *http.Request) {
	servers, err := h.svc.List()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list servers")
		return
	}
	writeJSON(w, http.StatusOK, servers)
}

func (h *ServerHandler) ListImages(w http.ResponseWriter, r *http.Request) {
	serverName := r.URL.Query().Get("server")
	if serverName == "" {
		// Use first server
		names := h.pool.ListServers()
		if len(names) == 0 {
			writeError(w, http.StatusNotFound, "no servers configured")
			return
		}
		serverName = names[0]
	}

	client, err := h.pool.GetClient(serverName)
	if err != nil {
		writeError(w, http.StatusNotFound, "server not found")
		return
	}

	images, err := client.ListImages()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list images")
		return
	}

	writeJSON(w, http.StatusOK, incus.SummarizeImages(images))
}
