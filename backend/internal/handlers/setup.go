package handlers

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http"

	"github.com/jmoiron/sqlx"

	"github.com/homaserver/plati/internal/auth"
	"github.com/homaserver/plati/internal/config"
	"github.com/homaserver/plati/internal/database/queries"
)

type SetupHandler struct {
	db         *sqlx.DB
	cfg        *config.Config
	configPath string
}

func NewSetupHandler(db *sqlx.DB, cfg *config.Config, configPath string) *SetupHandler {
	return &SetupHandler{db: db, cfg: cfg, configPath: configPath}
}

func (h *SetupHandler) Status(w http.ResponseWriter, r *http.Request) {
	val, err := queries.GetSetting(h.db, "setup_completed")
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]bool{"setup_completed": false})
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"setup_completed": val == "true"})
}

type setupRequest struct {
	AdminPassword string       `json:"admin_password"`
	Entra         *setupEntra  `json:"entra,omitempty"`
	Server        *setupServer `json:"server,omitempty"`
}

type setupEntra struct {
	ClientID     string `json:"client_id"`
	ClientSecret string `json:"client_secret"`
	TenantID     string `json:"tenant_id"`
}

type setupServer struct {
	Name          string `json:"name"`
	Endpoint      string `json:"endpoint"`
	TLSClientCert string `json:"tls_client_cert"`
	TLSClientKey  string `json:"tls_client_key"`
	MaxInstances  int    `json:"max_instances"`
}

func (h *SetupHandler) Complete(w http.ResponseWriter, r *http.Request) {
	// Reject if already set up
	val, _ := queries.GetSetting(h.db, "setup_completed")
	if val == "true" {
		writeError(w, http.StatusBadRequest, "setup already completed")
		return
	}

	var req setupRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request")
		return
	}

	if len(req.AdminPassword) < 8 {
		writeError(w, http.StatusBadRequest, "password must be at least 8 characters")
		return
	}

	// Validate Entra fields: all-or-nothing
	if req.Entra != nil {
		if req.Entra.ClientID == "" || req.Entra.ClientSecret == "" || req.Entra.TenantID == "" {
			writeError(w, http.StatusBadRequest, "all Entra ID fields are required when configuring Entra")
			return
		}
	}

	// Validate server fields
	if req.Server != nil {
		if req.Server.Name == "" || req.Server.Endpoint == "" {
			writeError(w, http.StatusBadRequest, "server name and endpoint are required when adding a server")
			return
		}
	}

	// Hash admin password
	hash, err := auth.HashPassword(req.AdminPassword)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to hash password")
		return
	}

	// Generate fresh secrets
	jwtSecret := randomHex(32)
	encryptionKey := randomHex(32)

	// Build new config (copy current, override fields)
	newCfg := *h.cfg
	newCfg.Auth.AdminPasswordHash = hash
	newCfg.Auth.JWTSecret = jwtSecret
	newCfg.SecretEncryptionKey = encryptionKey

	if req.Entra != nil {
		newCfg.Auth.EntraClientID = req.Entra.ClientID
		newCfg.Auth.EntraClientSecret = req.Entra.ClientSecret
		newCfg.Auth.EntraTenantID = req.Entra.TenantID
	} else {
		newCfg.Auth.EntraClientID = ""
		newCfg.Auth.EntraClientSecret = ""
		newCfg.Auth.EntraTenantID = ""
	}

	if req.Server != nil {
		maxInst := req.Server.MaxInstances
		if maxInst == 0 {
			maxInst = 50
		}
		tlsCert := req.Server.TLSClientCert
		tlsKey := req.Server.TLSClientKey
		// Preserve existing TLS certs if none were submitted
		if tlsCert == "" || tlsKey == "" {
			for _, existing := range h.cfg.Servers {
				if existing.Name == req.Server.Name {
					if tlsCert == "" {
						tlsCert = existing.TLSClientCert
					}
					if tlsKey == "" {
						tlsKey = existing.TLSClientKey
					}
					break
				}
			}
		}
		newCfg.Servers = []config.IncusServer{{
			Name:          req.Server.Name,
			Endpoint:      req.Server.Endpoint,
			TLSClientCert: tlsCert,
			TLSClientKey:  tlsKey,
			MaxInstances:  maxInst,
		}}
	} else {
		newCfg.Servers = []config.IncusServer{}
	}

	// Write config to disk
	if err := config.Write(&newCfg, h.configPath); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to write config: "+err.Error())
		return
	}

	// Update password hash in memory so admin can log in immediately
	h.cfg.Auth.AdminPasswordHash = hash

	// Mark setup as complete
	if err := queries.SetSetting(h.db, "setup_completed", "true"); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save setup status")
		return
	}

	needsRestart := (req.Entra != nil) || (req.Server != nil)
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"message":       "setup complete",
		"needs_restart": needsRestart,
	})
}

func randomHex(n int) string {
	b := make([]byte, n)
	rand.Read(b)
	return hex.EncodeToString(b)
}
