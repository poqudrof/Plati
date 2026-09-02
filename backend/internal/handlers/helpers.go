package handlers

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/homaserver/plati/internal/auth"
)

func writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func parseID(r *http.Request, param string) (int64, error) {
	return strconv.ParseInt(chi.URLParam(r, param), 10, 64)
}

// requireActor resolves the caller and writes 401 when the request carries no
// authenticated user. Every instance-scoped handler goes through it, which is also what
// removes the unguarded user.ID dereferences those handlers used to do — they relied on
// AuthMiddleware having run and would panic into the recovery middleware otherwise.
func requireActor(w http.ResponseWriter, r *http.Request) (auth.Actor, bool) {
	actor, ok := auth.ActorFrom(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return auth.Actor{}, false
	}
	return actor, true
}
