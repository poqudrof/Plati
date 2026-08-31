// Command plati-config écrit un config/plati.yaml d'amorçage au démarrage.
//
// Le backend ne peut pas s'auto-configurer entièrement : config.Load fait un
// log.Fatalf si le fichier est absent, donc il ne démarre pas — et sans lui,
// l'assistant /setup n'est pas servable. Ce générateur comble uniquement ce
// trou : il pose les valeurs d'infrastructure (adresses, chemins, serveur
// Incus), et laisse VIDES admin_password_hash, jwt_secret et
// secret_encryption_key, que POST /api/setup/complete remplit au premier accès.
//
// Idempotent : si le fichier existe déjà, il n'est pas touché. C'est ce qui
// protège les secrets écrits par l'assistant — en particulier
// secret_encryption_key, dont la perte rendrait indéchiffrables tous les
// secrets utilisateurs stockés en base.
package main

import (
	"flag"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"syscall"

	"github.com/homaserver/plati/internal/config"
)

func main() {
	path := flag.String("config", "config/plati.yaml", "chemin du fichier de config à générer")
	flag.Parse()

	if _, err := os.Stat(*path); err == nil {
		log.Printf("%s existe déjà — laissé intact", *path)
		return
	} else if !os.IsNotExist(err) {
		log.Fatalf("stat %s: %v", *path, err)
	}

	// Utilisé pour CORS et pour la redirection après callback Entra : une valeur
	// vide passerait inaperçue jusqu'à la première connexion, donc on refuse.
	frontendURL := os.Getenv("PLATI_FRONTEND_URL")
	if frontendURL == "" {
		log.Fatal("PLATI_FRONTEND_URL est vide — renseigner .env.plati (ex. https://plati.exemple.ts.net)")
	}

	cfg := &config.Config{
		Server: config.ServerConfig{
			Host:        env("PLATI_SERVER_HOST", "0.0.0.0"),
			Port:        envInt("PLATI_SERVER_PORT", 8080),
			FrontendURL: frontendURL,
		},
		Auth: config.AuthConfig{
			// Renseignés par l'assistant /setup, pas ici.
			AdminPasswordHash: "",
			JWTSecret:         "",
			JWTLifetimeHours:  envInt("PLATI_JWT_LIFETIME_HOURS", 24),
			EntraClientID:     os.Getenv("PLATI_ENTRA_CLIENT_ID"),
			EntraClientSecret: os.Getenv("PLATI_ENTRA_CLIENT_SECRET"),
			EntraTenantID:     os.Getenv("PLATI_ENTRA_TENANT_ID"),
			EntraRedirectURI:  os.Getenv("PLATI_ENTRA_REDIRECT_URI"),
		},
		Database: config.DatabaseConfig{
			Path: env("PLATI_DB_PATH", "/workspace/data/plati.db"),
		},
		Servers: []config.IncusServer{{
			Name:     env("PLATI_INCUS_NAME", "local"),
			Endpoint: env("PLATI_INCUS_ENDPOINT", "https://host.docker.internal:8443"),
			// Chemins relatifs : main.go les résout depuis le répertoire du
			// fichier de config, donc depuis ./config.
			TLSClientCert: env("PLATI_INCUS_CLIENT_CERT", "plati-client.crt"),
			TLSClientKey:  env("PLATI_INCUS_CLIENT_KEY", "plati-client.key"),
			MaxInstances:  envInt("PLATI_INCUS_MAX_INSTANCES", 50),
		}},
		SecretEncryptionKey: "", // renseigné par l'assistant /setup
		SleepTimeout:        env("PLATI_SLEEP_TIMEOUT", "4h"),
		TemplatesDir:        env("PLATI_TEMPLATES_DIR", "templates"),
		ReposDir:            env("PLATI_REPOS_DIR", "/workspace/data/repos"),
	}

	if err := config.Write(cfg, *path); err != nil {
		log.Fatalf("écriture de %s: %v", *path, err)
	}
	if err := chownToDirOwner(*path); err != nil {
		log.Printf("attention: chown %s: %v", *path, err)
	}
	log.Printf("%s généré — ouvrir %s pour terminer la configuration", *path, frontendURL)
}

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func envInt(key string, def int) int {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		log.Fatalf("%s=%q n'est pas un entier: %v", key, v, err)
	}
	return n
}

// chownToDirOwner rend le fichier à l'utilisateur propriétaire de son
// répertoire. config.Write crée en 0600 ; comme le conteneur tourne en root et
// que ./config est un bind mount du dépôt, le fichier serait sinon illisible
// sans sudo côté hôte. Les réécritures ultérieures par l'assistant conservent
// ce propriétaire (os.WriteFile ne rechown pas un fichier existant).
func chownToDirOwner(path string) error {
	if os.Geteuid() != 0 {
		return nil
	}
	info, err := os.Stat(filepath.Dir(path))
	if err != nil {
		return err
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok || st.Uid == 0 {
		return nil
	}
	return os.Chown(path, int(st.Uid), int(st.Gid))
}
