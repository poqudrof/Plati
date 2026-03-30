package router

import (
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/cors"

	"github.com/homaserver/plati/internal/auth"
	"github.com/homaserver/plati/internal/handlers"
	mw "github.com/homaserver/plati/internal/middleware"
)

type Deps struct {
	AuthHandler       *handlers.AuthHandler
	UserHandler       *handlers.UserHandler
	TemplateHandler   *handlers.TemplateHandler
	InstanceHandler   *handlers.InstanceHandler
	ServerHandler     *handlers.ServerHandler
	AdminHandler      *handlers.AdminHandler
	ManagedKeyHandler *handlers.ManagedKeyHandler
	HealthHandler     *handlers.HealthHandler
	SetupHandler      *handlers.SetupHandler
	TerminalHandler   *handlers.TerminalHandler
	JWTSecret         string
	FrontendURL       string
}

func New(deps Deps) *chi.Mux {
	r := chi.NewRouter()

	// Global middleware
	r.Use(mw.Recovery)
	r.Use(mw.Logger)
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   []string{deps.FrontendURL},
		AllowedMethods:   []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Content-Type", "Authorization"},
		AllowCredentials: true,
		MaxAge:           300,
	}))

	// Public routes
	r.Get("/health", deps.HealthHandler.Health)
	r.Get("/api/setup/status", deps.SetupHandler.Status)
	r.Post("/api/setup/complete", deps.SetupHandler.Complete)
	r.Post("/auth/login", deps.AuthHandler.LoginAdmin)
	r.Get("/auth/entra", deps.AuthHandler.LoginEntra)
	r.Get("/auth/callback", deps.AuthHandler.CallbackEntra)
	r.Post("/auth/logout", deps.AuthHandler.Logout)

	// Authenticated routes
	r.Group(func(r chi.Router) {
		r.Use(auth.AuthMiddleware(deps.JWTSecret))

		r.Get("/auth/me", deps.AuthHandler.Me)

		r.Route("/api/v1", func(r chi.Router) {
			// User profile
			r.Get("/profile", deps.UserHandler.GetProfile)

			// SSH keys (Plati-generated)
			r.Get("/ssh-keys", deps.UserHandler.ListSSHKeys)
			r.Post("/ssh-keys/generate", deps.UserHandler.GenerateSSHKey)
			r.Delete("/ssh-keys/{id}", deps.UserHandler.DeleteSSHKey)

			// Secrets
			r.Get("/secrets", deps.UserHandler.ListSecrets)
			r.Post("/secrets", deps.UserHandler.CreateSecret)
			r.Put("/secrets/{id}", deps.UserHandler.UpdateSecret)
			r.Delete("/secrets/{id}", deps.UserHandler.DeleteSecret)

			// Templates (read-only for users)
			r.Get("/templates", deps.TemplateHandler.List)
			r.Get("/templates/{id}", deps.TemplateHandler.Get)
			r.Get("/templates/{id}/export", deps.TemplateHandler.Export)

			// Instances
			r.Get("/instances", deps.InstanceHandler.List)
			r.Post("/instances", deps.InstanceHandler.Create)
			r.Get("/instances/{id}", deps.InstanceHandler.Get)
			r.Post("/instances/{id}/start", deps.InstanceHandler.Start)
			r.Post("/instances/{id}/stop", deps.InstanceHandler.Stop)
			r.Post("/instances/{id}/rebuild", deps.InstanceHandler.Rebuild)
			r.Delete("/instances/{id}", deps.InstanceHandler.Delete)
			r.Get("/instances/{id}/terminal", deps.TerminalHandler.Connect)

			// Instance secrets
			r.Get("/instances/{id}/secrets", deps.InstanceHandler.ListSecrets)
			r.Post("/instances/{id}/secrets", deps.InstanceHandler.CreateSecret)
			r.Put("/instances/{id}/secrets/{secret_id}", deps.InstanceHandler.UpdateSecret)
			r.Delete("/instances/{id}/secrets/{secret_id}", deps.InstanceHandler.DeleteSecret)

			// Admin routes
			r.Route("/admin", func(r chi.Router) {
				r.Use(auth.AdminMiddleware)

				// Templates management
				r.Post("/templates", deps.TemplateHandler.Create)
				r.Put("/templates/{id}", deps.TemplateHandler.Update)
				r.Delete("/templates/{id}", deps.TemplateHandler.Delete)
				r.Post("/templates/import", deps.TemplateHandler.Import)

				// Servers
				r.Get("/servers", deps.ServerHandler.List)

				// Images
				r.Get("/images", deps.ServerHandler.ListImages)

				// Users
				r.Get("/users", deps.AdminHandler.ListUsers)
				r.Put("/users/{id}", deps.AdminHandler.UpdateUser)
				r.Delete("/users/{id}", deps.AdminHandler.DeleteUser)

				// Managed SSH Keys
				r.Get("/managed-keys", deps.ManagedKeyHandler.List)
				r.Post("/managed-keys", deps.ManagedKeyHandler.Generate)
				r.Delete("/managed-keys/{id}", deps.ManagedKeyHandler.Delete)
				r.Get("/managed-keys/{id}/users", deps.ManagedKeyHandler.GetUsers)
				r.Put("/managed-keys/{id}/users", deps.ManagedKeyHandler.SetUsers)

				// Instances (admin)
				r.Get("/instances", deps.AdminHandler.ListAllInstances)
				r.Post("/instances", deps.AdminHandler.CreateInstanceForUser)
				r.Get("/instances/{id}/incus-info", deps.InstanceHandler.IncusDetail)
			})
		})
	})

	return r
}
