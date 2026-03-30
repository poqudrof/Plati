package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/jmoiron/sqlx"

	"github.com/homaserver/plati/internal/database/queries"
	"github.com/homaserver/plati/internal/services"
)

type AdminHandler struct {
	db          *sqlx.DB
	userSvc     *services.UserService
	instanceSvc *services.InstanceService
}

func NewAdminHandler(db *sqlx.DB, userSvc *services.UserService, instanceSvc *services.InstanceService) *AdminHandler {
	return &AdminHandler{db: db, userSvc: userSvc, instanceSvc: instanceSvc}
}

func (h *AdminHandler) ListUsers(w http.ResponseWriter, r *http.Request) {
	users, err := h.userSvc.ListUsers()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list users")
		return
	}
	writeJSON(w, http.StatusOK, users)
}

func (h *AdminHandler) UpdateUser(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	var req struct {
		Name string `json:"name"`
		Role string `json:"role"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request")
		return
	}
	if err := h.userSvc.UpdateUser(id, req.Name, req.Role); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update user")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"message": "updated"})
}

func (h *AdminHandler) DeleteUser(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	if err := h.userSvc.DeleteUser(id); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete user")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"message": "deleted"})
}

func (h *AdminHandler) ListAllInstances(w http.ResponseWriter, r *http.Request) {
	instances, err := queries.ListAllInstances(h.db)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list instances")
		return
	}
	writeJSON(w, http.StatusOK, instances)
}

func (h *AdminHandler) CreateInstanceForUser(w http.ResponseWriter, r *http.Request) {
	var req struct {
		services.CreateInstanceRequest
		UserID int64 `json:"user_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request")
		return
	}
	req.CreateInstanceRequest.UserID = req.UserID

	inst, err := h.instanceSvc.Create(req.CreateInstanceRequest)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, inst)
}
