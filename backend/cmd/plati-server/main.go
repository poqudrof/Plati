package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/homaserver/plati/internal/auth"
	"github.com/homaserver/plati/internal/config"
	"github.com/homaserver/plati/internal/database"
	"github.com/homaserver/plati/internal/handlers"
	"github.com/homaserver/plati/internal/incus"
	"github.com/homaserver/plati/internal/models"
	"github.com/homaserver/plati/internal/router"
	"github.com/homaserver/plati/internal/services"
)

func main() {
	configPath := flag.String("config", "config/plati.yaml", "path to config file")
	migrateOnly := flag.Bool("migrate", false, "run migrations and exit")
	flag.Parse()

	start := time.Now()
	log.Println("starting plati...")

	// Load config
	cfg, err := config.Load(*configPath)
	if err != nil {
		log.Fatalf("load config: %v", err)
	}
	log.Printf("[%.1fs] config loaded", time.Since(start).Seconds())

	// Open database
	db, err := database.New(cfg.Database.Path)
	if err != nil {
		log.Fatalf("open database: %v", err)
	}
	defer db.Close()
	log.Printf("[%.1fs] database opened", time.Since(start).Seconds())

	// Run migrations
	if err := database.Migrate(db); err != nil {
		log.Fatalf("migrate: %v", err)
	}
	log.Printf("[%.1fs] migrations applied", time.Since(start).Seconds())
	if *migrateOnly {
		log.Println("migrations complete")
		return
	}

	// Setup Incus pool
	pool := incus.NewPool()
	var serverModels []models.Server
	for _, srv := range cfg.Servers {
		log.Printf("[%.1fs] connecting to Incus server %s (%s)...", time.Since(start).Seconds(), srv.Name, srv.Endpoint)
		if err := pool.AddServer(srv.Name, srv.Endpoint, srv.TLSClientCert, srv.TLSClientKey); err != nil {
			log.Printf("[%.1fs] warning: failed to connect to Incus server %s: %v", time.Since(start).Seconds(), srv.Name, err)
		} else {
			log.Printf("[%.1fs] connected to Incus server %s", time.Since(start).Seconds(), srv.Name)
		}
		serverModels = append(serverModels, models.Server{
			Name:         srv.Name,
			Endpoint:     srv.Endpoint,
			TLSCertPath:  srv.TLSClientCert,
			TLSKeyPath:   srv.TLSClientKey,
			MaxInstances: srv.MaxInstances,
		})
	}

	// Setup Entra auth (optional)
	var entraAuth *auth.EntraAuth
	if cfg.Auth.EntraClientID != "" {
		redirectURL := fmt.Sprintf("http://%s:%d/auth/callback", cfg.Server.Host, cfg.Server.Port)
		if cfg.Server.Host == "0.0.0.0" {
			redirectURL = fmt.Sprintf("http://localhost:%d/auth/callback", cfg.Server.Port)
		}
		log.Printf("[%.1fs] fetching Entra OIDC configuration...", time.Since(start).Seconds())
		entraAuth, err = auth.NewEntraAuth(context.Background(), cfg.Auth.EntraClientID, cfg.Auth.EntraClientSecret, cfg.Auth.EntraTenantID, redirectURL)
		if err != nil {
			log.Printf("[%.1fs] warning: Entra auth init failed: %v", time.Since(start).Seconds(), err)
		} else {
			log.Printf("[%.1fs] Entra OIDC ready", time.Since(start).Seconds())
		}
	}

	// Keys directory: {config_dir}/ssh_keys/
	keysDir := filepath.Join(filepath.Dir(*configPath), "ssh_keys")

	// Init services
	userSvc, err := services.NewUserService(db, cfg.SecretEncryptionKey, keysDir)
	if err != nil {
		log.Fatalf("init user service: %v", err)
	}
	templateSvc := services.NewTemplateService(db)
	prefSvc := services.NewPreferencesService(db)
	adminSvc := services.NewAdminSettingsService(db, userSvc)
	instanceSvc := services.NewInstanceService(db, pool, userSvc, prefSvc, adminSvc, keysDir)
	serverSvc := services.NewServerService(db, pool)
	managedKeySvc := services.NewManagedKeyService(db, userSvc, keysDir)

	// Sync servers from config to DB
	if err := serverSvc.SyncServers(serverModels); err != nil {
		log.Printf("warning: sync servers: %v", err)
	}

	// Sync templates from YAML files on disk (resolve relative to config file location)
	templatesDir := cfg.TemplatesDir
	if templatesDir != "" && !filepath.IsAbs(templatesDir) {
		templatesDir = filepath.Join(filepath.Dir(*configPath), templatesDir)
	}
	if templatesDir != "" {
		if err := templateSvc.SyncFromDir(templatesDir); err != nil {
			log.Printf("warning: sync templates: %v", err)
		}
	}

	// Start sleep worker
	sleepTimeout, _ := time.ParseDuration(cfg.SleepTimeout)
	if sleepTimeout == 0 {
		sleepTimeout = 4 * time.Hour
	}
	sleepSvc := services.NewSleepService(db, pool, sleepTimeout)
	sleepSvc.Start()

	// Init handlers
	authHandler := handlers.NewAuthHandler(db, cfg, entraAuth)
	userHandler := handlers.NewUserHandler(userSvc)
	templateHandler := handlers.NewTemplateHandler(templateSvc)
	instanceHandler := handlers.NewInstanceHandler(instanceSvc, db)
	serverHandler := handlers.NewServerHandler(serverSvc, pool)
	adminHandler := handlers.NewAdminHandler(db, userSvc, instanceSvc, templateSvc)
	managedKeyHandler := handlers.NewManagedKeyHandler(managedKeySvc)
	healthHandler := handlers.NewHealthHandler(db)
	setupHandler := handlers.NewSetupHandler(db, cfg, *configPath)
	terminalHandler := handlers.NewTerminalHandler(db, pool, instanceSvc.CreationLogs())
	prefsHandler := handlers.NewPreferencesHandler(prefSvc)
	adminSettingsHandler := handlers.NewAdminSettingsHandler(adminSvc)

	// Build router
	r := router.New(router.Deps{
		AuthHandler:          authHandler,
		UserHandler:          userHandler,
		TemplateHandler:      templateHandler,
		InstanceHandler:      instanceHandler,
		ServerHandler:        serverHandler,
		AdminHandler:         adminHandler,
		ManagedKeyHandler:    managedKeyHandler,
		HealthHandler:        healthHandler,
		SetupHandler:         setupHandler,
		TerminalHandler:      terminalHandler,
		PreferencesHandler:   prefsHandler,
		AdminSettingsHandler: adminSettingsHandler,
		JWTSecret:            cfg.Auth.JWTSecret,
		FrontendURL:          cfg.Server.FrontendURL,
	})

	// Start server
	addr := fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port)
	srv := &http.Server{
		Addr:         addr,
		Handler:      r,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	go func() {
		log.Printf("[%.1fs] listening on %s", time.Since(start).Seconds(), addr)
		if err := srv.ListenAndServe(); err != http.ErrServerClosed {
			log.Fatalf("server error: %v", err)
		}
	}()

	// Graceful shutdown
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Println("shutting down...")
	sleepSvc.Stop()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	srv.Shutdown(ctx)
}
