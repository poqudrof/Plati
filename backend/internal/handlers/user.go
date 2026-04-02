package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/homaserver/plati/internal/auth"
	"github.com/homaserver/plati/internal/services"
)

type UserHandler struct {
	svc *services.UserService
}

func NewUserHandler(svc *services.UserService) *UserHandler {
	return &UserHandler{svc: svc}
}

func (h *UserHandler) GetProfile(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	u, err := h.svc.GetUser(user.ID)
	if err != nil {
		writeError(w, http.StatusNotFound, "user not found")
		return
	}
	writeJSON(w, http.StatusOK, u)
}

// SSH Keys (user-provided public keys)

func (h *UserHandler) ListSSHKeys(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	keys, err := h.svc.ListSSHKeys(user.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list keys")
		return
	}
	writeJSON(w, http.StatusOK, keys)
}

func (h *UserHandler) CreateSSHKey(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	var req struct {
		Name      string `json:"name"`
		PublicKey string `json:"public_key"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Name == "" || req.PublicKey == "" {
		writeError(w, http.StatusBadRequest, "name and public_key required")
		return
	}
	key, err := h.svc.CreateSSHKey(user.ID, req.Name, req.PublicKey)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to add key")
		return
	}
	writeJSON(w, http.StatusCreated, key)
}

func (h *UserHandler) DeleteSSHKey(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	id, err := parseID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	if err := h.svc.DeleteSSHKey(id, user.ID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete key")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"message": "deleted"})
}

// User Keys (Plati-generated keypairs)

func (h *UserHandler) ListUserKeys(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	keys, err := h.svc.ListUserSSHKeys(user.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list keys")
		return
	}
	writeJSON(w, http.StatusOK, keys)
}

// GenerateUserKey generates a new Plati keypair for the user.
// The private key PEM is returned only in this response — it is not accessible later.
func (h *UserHandler) GenerateUserKey(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	var req struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Name == "" {
		writeError(w, http.StatusBadRequest, "name required")
		return
	}

	key, privateKeyPEM, err := h.svc.GenerateUserSSHKey(user.ID, req.Name)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to generate key")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"id":          key.ID,
		"name":        key.Name,
		"public_key":  key.PublicKey,
		"private_key": privateKeyPEM,
	})
}

func (h *UserHandler) DeleteUserKey(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	id, err := parseID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	if err := h.svc.DeleteUserSSHKey(id, user.ID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete key")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"message": "deleted"})
}

// Secrets
func (h *UserHandler) ListSecrets(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	secrets, err := h.svc.ListSecrets(user.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list secrets")
		return
	}
	writeJSON(w, http.StatusOK, secrets)
}

func (h *UserHandler) CreateSecret(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	var req struct {
		Name  string `json:"name"`
		Value string `json:"value"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request")
		return
	}
	if req.Name == "" || req.Value == "" {
		writeError(w, http.StatusBadRequest, "name and value required")
		return
	}

	id, err := h.svc.CreateSecret(user.ID, req.Name, req.Value)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create secret")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]int64{"id": id})
}

func (h *UserHandler) UpdateSecret(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	id, err := parseID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	var req struct {
		Value string `json:"value"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request")
		return
	}
	if err := h.svc.UpdateSecret(id, user.ID, req.Value); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update secret")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"message": "updated"})
}

func (h *UserHandler) DeleteSecret(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	id, err := parseID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	if err := h.svc.DeleteSecret(id, user.ID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete secret")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"message": "deleted"})
}
