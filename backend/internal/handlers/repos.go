package handlers

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/homaserver/plati/internal/services"
)

type RepoHandler struct {
	repoSvc *services.RepoService
}

func NewRepoHandler(repoSvc *services.RepoService) *RepoHandler {
	return &RepoHandler{repoSvc: repoSvc}
}

func (h *RepoHandler) List(w http.ResponseWriter, r *http.Request) {
	repos, err := h.repoSvc.ListRepos()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(repos)
}

func (h *RepoHandler) Add(w http.ResponseWriter, r *http.Request) {
	var req struct {
		SSHURL string `json:"ssh_url"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if req.SSHURL == "" {
		http.Error(w, "ssh_url is required", http.StatusBadRequest)
		return
	}
	repo, err := h.repoSvc.AddRepo(req.SSHURL)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(repo)
}

func (h *RepoHandler) Rename(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}
	var req struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Name == "" {
		http.Error(w, "name is required", http.StatusBadRequest)
		return
	}
	if err := h.repoSvc.RenameRepo(id, req.Name); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "renamed"})
}

func (h *RepoHandler) Delete(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}
	if err := h.repoSvc.DeleteRepo(id); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "deleted"})
}

func (h *RepoHandler) Sync(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}
	command, output, syncErr := h.repoSvc.SyncRepo(id)
	status := "success"
	if syncErr != nil {
		status = "error"
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"status":  status,
		"command": command,
		"output":  output,
	})
}

func (h *RepoHandler) GetServerKey(w http.ResponseWriter, r *http.Request) {
	pubKey, err := h.repoSvc.GetServerPublicKey()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	managedKeyID := h.repoSvc.GetServerManagedKeyID()
	w.Header().Set("Content-Type", "application/json")
	resp := map[string]interface{}{"public_key": pubKey}
	if managedKeyID > 0 {
		resp["managed_key_id"] = managedKeyID
	}
	json.NewEncoder(w).Encode(resp)
}

func (h *RepoHandler) GenerateServerKey(w http.ResponseWriter, r *http.Request) {
	pubKey, err := h.repoSvc.GenerateServerKey()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"public_key": pubKey})
}

func (h *RepoHandler) SetServerManagedKey(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ManagedKeyID int64 `json:"managed_key_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.ManagedKeyID == 0 {
		http.Error(w, "managed_key_id required", http.StatusBadRequest)
		return
	}
	pubKey, err := h.repoSvc.SetServerManagedKey(req.ManagedKeyID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"public_key":     pubKey,
		"managed_key_id": req.ManagedKeyID,
	})
}
