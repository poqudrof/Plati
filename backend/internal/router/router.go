package router

import (
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/cors"

	"github.com/homaserver/plati/internal/auth"
	"github.com/homaserver/plati/internal/handlers"
	mw "github.com/homaserver/plati/internal/middleware"
)

type Deps struct {
	AuthHandler          *handlers.AuthHandler
	UserHandler          *handlers.UserHandler
	TemplateHandler      *handlers.TemplateHandler
	InstanceHandler      *handlers.InstanceHandler
	ServerHandler        *handlers.ServerHandler
	AdminHandler         *handlers.AdminHandler
	ManagedKeyHandler    *handlers.ManagedKeyHandler
	HealthHandler        *handlers.HealthHandler
	SetupHandler         *handlers.SetupHandler
	TerminalHandler      *handlers.TerminalHandler
	PreferencesHandler   *handlers.PreferencesHandler
	AdminSettingsHandler *handlers.AdminSettingsHandler
	RepoHandler          *handlers.RepoHandler
	StorageHandler       *handlers.StorageHandler
	APIKeyHandler        *handlers.APIKeyHandler
	APIKeyAuth           auth.APIKeyAuthenticator
	JWTSecret            string
	FrontendURL          string
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
		r.Use(auth.AuthMiddleware(deps.JWTSecret, deps.APIKeyAuth))

		r.Get("/auth/me", deps.AuthHandler.Me)

		r.Route("/api/v1", func(r chi.Router) {
			// User profile
			r.Get("/profile", deps.UserHandler.GetProfile)

			// User preferences
			r.Get("/preferences", deps.PreferencesHandler.Get)
			r.Put("/preferences", deps.PreferencesHandler.Update)

			// SSH keys (user-provided public keys)
			r.Get("/ssh-keys", deps.UserHandler.ListSSHKeys)
			r.Post("/ssh-keys", deps.UserHandler.CreateSSHKey)
			r.Delete("/ssh-keys/{id}", deps.UserHandler.DeleteSSHKey)

			// User keys (Plati-generated keypairs)
			r.Get("/user-keys", deps.UserHandler.ListUserKeys)
			r.Post("/user-keys/generate", deps.UserHandler.GenerateUserKey)
			r.Delete("/user-keys/{id}", deps.UserHandler.DeleteUserKey)

			// Secrets
			r.Get("/secrets", deps.UserHandler.ListSecrets)
			r.Post("/secrets", deps.UserHandler.CreateSecret)
			r.Put("/secrets/{id}", deps.UserHandler.UpdateSecret)
			r.Delete("/secrets/{id}", deps.UserHandler.DeleteSecret)

			// API keys (view + regenerate own; only admins can create — see /admin/users/{id}/api-keys)
			r.Get("/api-keys", deps.APIKeyHandler.ListMine)
			r.Post("/api-keys/{id}/regenerate", deps.APIKeyHandler.RegenerateMine)

			// Disks (user's own volumes)
			r.Get("/disks", deps.InstanceHandler.ListDisks)

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
			r.Get("/instances/{id}/volumes", deps.InstanceHandler.Volumes)
			r.Get("/instances/{id}/stats", deps.InstanceHandler.Stats)
			r.Get("/instances/{id}/sshx-url", deps.InstanceHandler.SshxURL)
			r.Post("/instances/{id}/duplicate", deps.InstanceHandler.Duplicate)
			r.Post("/instances/{id}/tailscale-serve", deps.InstanceHandler.TailscaleServe)
			r.Get("/instances/{id}/tailscale-serve", deps.InstanceHandler.TailscaleServeStatus)
			r.Delete("/instances/{id}/tailscale-serve", deps.InstanceHandler.TailscaleServeOff)
			r.Get("/instances/{id}/tailscale-status", deps.InstanceHandler.TailscaleStatus)
			r.Get("/instances/{id}/terminal", deps.TerminalHandler.Connect)
			r.Get("/instances/{id}/creation-stream", deps.TerminalHandler.CreationStream)

			// Instance secrets
			r.Get("/instances/{id}/secrets", deps.InstanceHandler.ListSecrets)
			r.Post("/instances/{id}/secrets", deps.InstanceHandler.CreateSecret)
			r.Put("/instances/{id}/secrets/{secret_id}", deps.InstanceHandler.UpdateSecret)
			r.Delete("/instances/{id}/secrets/{secret_id}", deps.InstanceHandler.DeleteSecret)

			// Instance storage (volumes listed via /instances/{id}/volumes above)
			r.Get("/instances/{id}/storage/browse", deps.StorageHandler.ListDirectory)
			r.Get("/instances/{id}/storage/download", deps.StorageHandler.DownloadFile)
			r.Get("/instances/{id}/storage/download-dir", deps.StorageHandler.DownloadDirectory)
			r.Get("/instances/{id}/storage/volumes/{vol_id}/snapshots", deps.StorageHandler.ListSnapshots)
			r.Post("/instances/{id}/storage/volumes/{vol_id}/snapshots", deps.StorageHandler.CreateSnapshot)
			r.Delete("/instances/{id}/storage/volumes/{vol_id}/snapshots/{name}", deps.StorageHandler.DeleteSnapshot)
			r.Post("/instances/{id}/storage/volumes/{vol_id}/snapshots/{name}/restore", deps.StorageHandler.RestoreSnapshot)

			// Admin routes
			r.Route("/admin", func(r chi.Router) {
				r.Use(auth.AdminMiddleware)

				// Templates management
				r.Post("/templates", deps.TemplateHandler.Create)
				r.Put("/templates/{id}", deps.TemplateHandler.Update)
				r.Delete("/templates/{id}", deps.TemplateHandler.Delete)
				r.Post("/templates/import", deps.TemplateHandler.Import)
				r.Get("/templates/mixins", deps.TemplateHandler.ListMixins)
				r.Post("/templates/{id}/debug", deps.AdminHandler.DebugCreateInstance)
				r.Post("/templates/{id}/duplicate", deps.TemplateHandler.Duplicate)
				r.Get("/templates/{id}/export-yaml", deps.TemplateHandler.ExportYAMLAsJSON)
				r.Post("/templates/{id}/update-from-yaml", deps.TemplateHandler.UpdateFromYAML)
				r.Post("/templates/{id}/save-to-disk", deps.TemplateHandler.SaveToDisk)
				r.Post("/templates/{id}/reload-from-disk", deps.TemplateHandler.ReloadFromDisk)
				r.Get("/templates/{id}/debug/instance", deps.AdminHandler.GetDebugInstance)
				r.Get("/templates/{id}/profiles/check", deps.AdminHandler.CheckTemplateProfiles)

				// Servers
				r.Get("/servers", deps.ServerHandler.List)

				// Images
				r.Get("/images", deps.ServerHandler.ListImages)

				// Users
				r.Get("/users", deps.AdminHandler.ListUsers)
				r.Post("/users", deps.AdminHandler.CreateUser)
				r.Put("/users/{id}", deps.AdminHandler.UpdateUser)
				r.Delete("/users/{id}", deps.AdminHandler.DeleteUser)

				// User SSH key management (admin on behalf of user)
				r.Get("/users/{id}/keys", deps.AdminHandler.ListUserKeys)
				r.Post("/users/{id}/keys/generate", deps.AdminHandler.GenerateUserKey)
				r.Delete("/users/{id}/keys/{key_id}", deps.AdminHandler.DeleteUserKey)

				// User API key management (admin creates/regenerates on behalf of user)
				r.Get("/users/{id}/api-keys", deps.APIKeyHandler.AdminList)
				r.Post("/users/{id}/api-keys", deps.APIKeyHandler.AdminCreate)
				r.Post("/users/{id}/api-keys/{key_id}/regenerate", deps.APIKeyHandler.AdminRegenerate)

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
				r.Put("/instances/{id}/incus-config", deps.InstanceHandler.UpdateIncusConfig)
				r.Get("/instances/{id}/debug-logs", deps.TerminalHandler.DebugLogs)
				r.Post("/instances/{id}/exec", deps.AdminHandler.ExecCommand)
				r.Post("/instances/{id}/reapply-setup", deps.AdminHandler.ReapplySetup)

				// Admin settings
				r.Get("/settings/tailscale-key", deps.AdminSettingsHandler.GetTailscaleKey)
				r.Put("/settings/tailscale-key", deps.AdminSettingsHandler.SetTailscaleKey)
				r.Delete("/settings/tailscale-key", deps.AdminSettingsHandler.DeleteTailscaleKey)

				// Disks (all volumes, admin view)
				r.Get("/disks", deps.AdminHandler.ListAllDisks)

				// Git repos
				r.Get("/repos", deps.RepoHandler.List)
				r.Post("/repos", deps.RepoHandler.Add)
				r.Put("/repos/{id}", deps.RepoHandler.Rename)
				r.Delete("/repos/{id}", deps.RepoHandler.Delete)
				r.Post("/repos/{id}/sync", deps.RepoHandler.Sync)
				r.Get("/repos/server-key", deps.RepoHandler.GetServerKey)
				r.Post("/repos/server-key/generate", deps.RepoHandler.GenerateServerKey)
				r.Put("/repos/server-key/managed", deps.RepoHandler.SetServerManagedKey)
			})
		})
	})

	return r
}
