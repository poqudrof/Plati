package handlers

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/jmoiron/sqlx"

	"github.com/homaserver/plati/internal/auth"
	"github.com/homaserver/plati/internal/database/queries"
	"github.com/homaserver/plati/internal/services"
)

type AdminHandler struct {
	db          *sqlx.DB
	userSvc     *services.UserService
	instanceSvc *services.InstanceService
	templateSvc *services.TemplateService
}

func NewAdminHandler(db *sqlx.DB, userSvc *services.UserService, instanceSvc *services.InstanceService, templateSvc *services.TemplateService) *AdminHandler {
	return &AdminHandler{db: db, userSvc: userSvc, instanceSvc: instanceSvc, templateSvc: templateSvc}
}

func (h *AdminHandler) CreateUser(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Email    string `json:"email"`
		Name     string `json:"name"`
		Password string `json:"password"`
		Role     string `json:"role"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request")
		return
	}
	if req.Email == "" || req.Password == "" {
		writeError(w, http.StatusBadRequest, "email and password are required")
		return
	}
	if req.Role != "admin" && req.Role != "user" {
		req.Role = "user"
	}
	user, err := h.userSvc.CreateUser(req.Email, req.Name, req.Role, req.Password)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create user")
		return
	}
	writeJSON(w, http.StatusCreated, user)
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
		// Optional: an empty password leaves the current one untouched.
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request")
		return
	}
	if req.Password != "" && len(req.Password) < services.MinPasswordLength {
		writeError(w, http.StatusBadRequest,
			fmt.Sprintf("password must be at least %d characters", services.MinPasswordLength))
		return
	}
	if err := h.userSvc.UpdateUser(id, req.Name, req.Role); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update user")
		return
	}
	if req.Password != "" {
		if err := h.userSvc.SetPassword(id, req.Password); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to set password")
			return
		}
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

// User SSH key management (admin acts on behalf of a user)

func (h *AdminHandler) ListUserKeys(w http.ResponseWriter, r *http.Request) {
	userID, err := parseID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	keys, err := h.userSvc.ListUserSSHKeys(userID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list keys")
		return
	}
	writeJSON(w, http.StatusOK, keys)
}

// GenerateUserKey generates a keypair for a user.
// The private key is stored encrypted on the server and never returned to the admin.
func (h *AdminHandler) GenerateUserKey(w http.ResponseWriter, r *http.Request) {
	userID, err := parseID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	var req struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Name == "" {
		writeError(w, http.StatusBadRequest, "name required")
		return
	}
	key, _, err := h.userSvc.GenerateUserSSHKey(userID, req.Name)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to generate key")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"id":         key.ID,
		"name":       key.Name,
		"public_key": key.PublicKey,
		"created_at": key.CreatedAt,
	})
}

func (h *AdminHandler) DeleteUserKey(w http.ResponseWriter, r *http.Request) {
	userID, err := parseID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	keyID, err := parseID(r, "key_id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid key_id")
		return
	}
	if err := h.userSvc.DeleteUserSSHKey(keyID, userID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete key")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"message": "deleted"})
}

// User public keys (admin adds a user-provided public key so the user can SSH into
// their own instances). These land in ~/.ssh/authorized_keys on create/rebuild.

func (h *AdminHandler) ListUserPublicKeys(w http.ResponseWriter, r *http.Request) {
	userID, err := parseID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	keys, err := h.userSvc.ListSSHKeys(userID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list public keys")
		return
	}
	writeJSON(w, http.StatusOK, keys)
}

func (h *AdminHandler) AddUserPublicKey(w http.ResponseWriter, r *http.Request) {
	userID, err := parseID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	var req struct {
		Name      string `json:"name"`
		PublicKey string `json:"public_key"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Name == "" || req.PublicKey == "" {
		writeError(w, http.StatusBadRequest, "name and public_key required")
		return
	}
	if _, err := h.userSvc.GetUser(userID); err != nil {
		writeError(w, http.StatusNotFound, "user not found")
		return
	}
	key, err := h.userSvc.CreateSSHKey(userID, req.Name, req.PublicKey)
	if err != nil {
		if errors.Is(err, services.ErrInvalidPublicKey) {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to add public key")
		return
	}
	writeJSON(w, http.StatusCreated, key)
}

func (h *AdminHandler) DeleteUserPublicKey(w http.ResponseWriter, r *http.Request) {
	userID, err := parseID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	keyID, err := parseID(r, "key_id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid key_id")
		return
	}
	if err := h.userSvc.DeleteSSHKey(keyID, userID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete public key")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"message": "deleted"})
}

func (h *AdminHandler) ListAllInstances(w http.ResponseWriter, r *http.Request) {
	instances, err := queries.ListAllInstancesWithOwner(h.db)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list instances")
		return
	}
	writeJSON(w, http.StatusOK, instances)
}

// DuplicateInstance copies any user's instance and assigns the copy to the user
// given in the body. An omitted user_id keeps the copy with the source's owner.
func (h *AdminHandler) DuplicateInstance(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	var req struct {
		UserID int64 `json:"user_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && err != io.EOF {
		writeError(w, http.StatusBadRequest, "invalid request")
		return
	}

	inst, err := h.instanceSvc.DuplicateForUser(id, req.UserID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, inst)
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

	inst, err := h.instanceSvc.CreateAsync(req.CreateInstanceRequest)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, inst)
}

// DebugCreateInstance creates a temporary debug instance from a template (admin only).
// If a previous debug instance exists for this template, it is deleted first.
func (h *AdminHandler) DebugCreateInstance(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	id, err := parseID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid template id")
		return
	}

	if _, err := queries.GetTemplate(h.db, id); err != nil {
		writeError(w, http.StatusNotFound, "template not found")
		return
	}

	// Delete previous debug instance for this template (best-effort).
	if prev, err := queries.GetLatestDebugInstanceForTemplate(h.db, id, user.ID); err == nil {
		_ = h.instanceSvc.Delete(prev.ID, user.ID)
	}

	req := services.CreateInstanceRequest{
		Name:              fmt.Sprintf("debug-%d-%d", id, time.Now().Unix()),
		TemplateID:        id,
		UserID:            user.ID,
		SSHKeyModeOverride: "plati", // always inject the Plati private key for debug instances
	}

	inst, err := h.instanceSvc.CreateAsync(req)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, inst)
}

// GetDebugInstance returns the most recent debug instance for a template, or 404.
func (h *AdminHandler) GetDebugInstance(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	id, err := parseID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid template id")
		return
	}
	inst, err := queries.GetLatestDebugInstanceForTemplate(h.db, id, user.ID)
	if err != nil {
		writeError(w, http.StatusNotFound, "no debug instance found")
		return
	}
	writeJSON(w, http.StatusOK, inst)
}

// CheckTemplateProfiles checks whether every Incus profile required by the template
// exists on all registered servers.
func (h *AdminHandler) CheckTemplateProfiles(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid template id")
		return
	}
	results, err := h.instanceSvc.CheckTemplateProfiles(id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, results)
}

// ExecCommand runs a shell command inside a running instance (admin only).
func (h *AdminHandler) ExecCommand(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid instance id")
		return
	}
	var req struct {
		Command string `json:"command"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Command == "" {
		writeError(w, http.StatusBadRequest, "command is required")
		return
	}
	result, err := h.instanceSvc.ExecCommand(id, req.Command)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, result)
}

// ReapplySetup re-runs SSH key + secrets setup on a running instance (admin only).
func (h *AdminHandler) ReapplySetup(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid instance id")
		return
	}
	result, err := h.instanceSvc.ReapplySetup(id)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (h *AdminHandler) ListAllDisks(w http.ResponseWriter, r *http.Request) {
	disks, err := queries.ListAllDisks(h.db)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list disks")
		return
	}
	writeJSON(w, http.StatusOK, disks)
}
