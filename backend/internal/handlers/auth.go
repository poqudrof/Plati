package handlers

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http"

	"github.com/jmoiron/sqlx"
	"golang.org/x/crypto/bcrypt"

	"github.com/homaserver/plati/internal/auth"
	"github.com/homaserver/plati/internal/config"
	"github.com/homaserver/plati/internal/database/queries"
	"github.com/homaserver/plati/internal/models"
)

type AuthHandler struct {
	db        *sqlx.DB
	cfg       *config.Config
	entraAuth *auth.EntraAuth // nil if Entra not configured
}

func NewAuthHandler(db *sqlx.DB, cfg *config.Config, entraAuth *auth.EntraAuth) *AuthHandler {
	return &AuthHandler{db: db, cfg: cfg, entraAuth: entraAuth}
}

func (h *AuthHandler) LoginAdmin(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request")
		return
	}

	// User login: email + password against per-user password_hash
	if req.Email != "" {
		user, err := queries.GetUserByEmail(h.db, req.Email)
		if err != nil || !user.PasswordHash.Valid {
			writeError(w, http.StatusUnauthorized, "invalid credentials")
			return
		}
		if bcrypt.CompareHashAndPassword([]byte(user.PasswordHash.String), []byte(req.Password)) != nil {
			writeError(w, http.StatusUnauthorized, "invalid credentials")
			return
		}
		h.setTokenCookie(w, user)
		writeJSON(w, http.StatusOK, map[string]string{"message": "ok"})
		return
	}

	// Admin login: global password from config
	if !auth.CheckPassword(req.Password, h.cfg.Auth.AdminPasswordHash) {
		writeError(w, http.StatusUnauthorized, "invalid password")
		return
	}
	user, err := queries.GetUserByEmail(h.db, "admin@plati.local")
	if err != nil {
		writeError(w, http.StatusInternalServerError, "admin user not found")
		return
	}
	h.setTokenCookie(w, user)
	writeJSON(w, http.StatusOK, map[string]string{"message": "ok"})
}

func (h *AuthHandler) LoginEntra(w http.ResponseWriter, r *http.Request) {
	if h.entraAuth == nil {
		writeError(w, http.StatusNotImplemented, "Entra ID not configured")
		return
	}

	state := generateState()
	http.SetCookie(w, &http.Cookie{
		Name:     "oauth_state",
		Value:    state,
		Path:     "/",
		MaxAge:   300,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})

	http.Redirect(w, r, h.entraAuth.AuthURL(state), http.StatusFound)
}

func (h *AuthHandler) CallbackEntra(w http.ResponseWriter, r *http.Request) {
	if h.entraAuth == nil {
		writeError(w, http.StatusNotImplemented, "Entra ID not configured")
		return
	}

	// Verify state
	stateCookie, err := r.Cookie("oauth_state")
	if err != nil || stateCookie.Value != r.URL.Query().Get("state") {
		writeError(w, http.StatusBadRequest, "invalid state")
		return
	}

	code := r.URL.Query().Get("code")
	if code == "" {
		writeError(w, http.StatusBadRequest, "missing code")
		return
	}

	entraUser, err := h.entraAuth.Exchange(r.Context(), code)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "authentication failed")
		return
	}

	// Upsert user
	user, err := queries.UpsertEntraUser(h.db, entraUser.Email, entraUser.Name, entraUser.Sub)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create user")
		return
	}

	h.setTokenCookie(w, user)

	// Redirect to frontend
	http.Redirect(w, r, h.cfg.Server.FrontendURL+"/dashboard", http.StatusFound)
}

func (h *AuthHandler) Logout(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name:     "plati_token",
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
	writeJSON(w, http.StatusOK, map[string]string{"message": "logged out"})
}

func (h *AuthHandler) Me(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	if user == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	fullUser, err := queries.GetUserByID(h.db, user.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "user not found")
		return
	}

	writeJSON(w, http.StatusOK, fullUser)
}

func (h *AuthHandler) setTokenCookie(w http.ResponseWriter, user *models.User) {
	token, err := auth.GenerateToken(user.ID, user.Email, user.Role, h.cfg.Auth.JWTSecret, h.cfg.Auth.JWTLifetimeHours)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to generate token")
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:     "plati_token",
		Value:    token,
		Path:     "/",
		MaxAge:   h.cfg.Auth.JWTLifetimeHours * 3600,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
}

func generateState() string {
	b := make([]byte, 16)
	rand.Read(b)
	return hex.EncodeToString(b)
}
