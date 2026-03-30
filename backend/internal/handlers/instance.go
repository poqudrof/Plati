package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/jmoiron/sqlx"

	"github.com/homaserver/plati/internal/auth"
	"github.com/homaserver/plati/internal/database/queries"
	"github.com/homaserver/plati/internal/services"
)

type InstanceHandler struct {
	svc *services.InstanceService
	db  *sqlx.DB
}

func NewInstanceHandler(svc *services.InstanceService, db *sqlx.DB) *InstanceHandler {
	return &InstanceHandler{svc: svc, db: db}
}

func (h *InstanceHandler) List(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	instances, err := queries.ListInstancesByUser(h.db, user.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list instances")
		return
	}
	writeJSON(w, http.StatusOK, instances)
}

func (h *InstanceHandler) Get(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	id, err := parseID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	inst, err := queries.GetInstanceByUser(h.db, id, user.ID)
	if err != nil {
		writeError(w, http.StatusNotFound, "instance not found")
		return
	}
	writeJSON(w, http.StatusOK, inst)
}

func (h *InstanceHandler) Create(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	var req services.CreateInstanceRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request")
		return
	}
	req.UserID = user.ID

	inst, err := h.svc.Create(req)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, inst)
}

func (h *InstanceHandler) Start(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	id, err := parseID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	if err := h.svc.Start(id, user.ID); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"message": "started"})
}

func (h *InstanceHandler) Stop(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	id, err := parseID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	if err := h.svc.Stop(id, user.ID); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"message": "stopped"})
}

func (h *InstanceHandler) Rebuild(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	id, err := parseID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	if err := h.svc.Rebuild(id, user.ID); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"message": "rebuilt"})
}

func (h *InstanceHandler) Delete(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	id, err := parseID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	if err := h.svc.Delete(id, user.ID); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"message": "deleted"})
}

func (h *InstanceHandler) IncusDetail(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	detail, err := h.svc.GetIncusDetail(id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, detail)
}

func (h *InstanceHandler) ListSecrets(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	instanceID, err := parseID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	secrets, err := h.svc.ListInstanceSecrets(instanceID, user.ID)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, secrets)
}

func (h *InstanceHandler) CreateSecret(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	instanceID, err := parseID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	var body struct {
		Name  string `json:"name"`
		Value string `json:"value"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Name == "" || body.Value == "" {
		writeError(w, http.StatusBadRequest, "name and value required")
		return
	}
	id, err := h.svc.CreateInstanceSecret(instanceID, user.ID, body.Name, body.Value)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, map[string]int64{"id": id})
}

func (h *InstanceHandler) UpdateSecret(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	instanceID, err := parseID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	secretID, err := parseID(r, "secret_id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid secret id")
		return
	}
	var body struct {
		Value string `json:"value"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Value == "" {
		writeError(w, http.StatusBadRequest, "value required")
		return
	}
	if err := h.svc.UpdateInstanceSecret(secretID, instanceID, user.ID, body.Value); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *InstanceHandler) DeleteSecret(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	instanceID, err := parseID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	secretID, err := parseID(r, "secret_id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid secret id")
		return
	}
	if err := h.svc.DeleteInstanceSecret(secretID, instanceID, user.ID); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
