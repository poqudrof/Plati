package auth

import (
	"context"
	"net/http"
	"strings"
)

type contextKey string

const UserContextKey contextKey = "user"

type ContextUser struct {
	ID    int64
	Email string
	Role  string
}

// APIKeyAuthenticator resolves a plaintext API key to its owning user.
type APIKeyAuthenticator interface {
	Authenticate(plaintext string) (*ContextUser, error)
}

func AuthMiddleware(jwtSecret string, apiKeys APIKeyAuthenticator) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if authHeader := r.Header.Get("Authorization"); strings.HasPrefix(authHeader, "Bearer ") {
				key := strings.TrimPrefix(authHeader, "Bearer ")
				user, err := apiKeys.Authenticate(key)
				if err != nil {
					http.Error(w, `{"error":"invalid api key"}`, http.StatusUnauthorized)
					return
				}
				ctx := context.WithValue(r.Context(), UserContextKey, user)
				next.ServeHTTP(w, r.WithContext(ctx))
				return
			}

			cookie, err := r.Cookie("plati_token")
			if err != nil {
				http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
				return
			}

			claims, err := ValidateToken(cookie.Value, jwtSecret)
			if err != nil {
				http.Error(w, `{"error":"invalid token"}`, http.StatusUnauthorized)
				return
			}

			ctx := context.WithValue(r.Context(), UserContextKey, &ContextUser{
				ID:    claims.UserID,
				Email: claims.Email,
				Role:  claims.Role,
			})
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func AdminMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user := UserFromContext(r.Context())
		if user == nil || user.Role != "admin" {
			http.Error(w, `{"error":"forbidden"}`, http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func UserFromContext(ctx context.Context) *ContextUser {
	u, _ := ctx.Value(UserContextKey).(*ContextUser)
	return u
}
