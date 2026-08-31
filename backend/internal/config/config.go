package config

import (
	"os"

	"github.com/spf13/viper"
	"gopkg.in/yaml.v3"
)

type Config struct {
	Server              ServerConfig   `mapstructure:"server" yaml:"server"`
	Auth                AuthConfig     `mapstructure:"auth" yaml:"auth"`
	Database            DatabaseConfig `mapstructure:"database" yaml:"database"`
	Servers             []IncusServer  `mapstructure:"servers" yaml:"servers"`
	SecretEncryptionKey string         `mapstructure:"secret_encryption_key" yaml:"secret_encryption_key"`
	SleepTimeout        string         `mapstructure:"sleep_timeout" yaml:"sleep_timeout"`
	TemplatesDir        string         `mapstructure:"templates_dir" yaml:"templates_dir"`
	ReposDir            string         `mapstructure:"repos_dir" yaml:"repos_dir"`
	// ReposHostDir, if set, is the path to the repos directory as seen by the Incus host.
	// Use this when the server runs inside Docker: set repos_dir to the Docker-internal path
	// (for cloning) and repos_host_dir to the host path (for Incus disk device mounting).
	// Defaults to repos_dir when empty.
	ReposHostDir        string         `mapstructure:"repos_host_dir" yaml:"repos_host_dir,omitempty"`
}

type ServerConfig struct {
	Host        string `mapstructure:"host" yaml:"host"`
	Port        int    `mapstructure:"port" yaml:"port"`
	FrontendURL string `mapstructure:"frontend_url" yaml:"frontend_url"`
}

type AuthConfig struct {
	EntraClientID     string `mapstructure:"entra_client_id" yaml:"entra_client_id"`
	EntraClientSecret string `mapstructure:"entra_client_secret" yaml:"entra_client_secret"`
	EntraTenantID     string `mapstructure:"entra_tenant_id" yaml:"entra_tenant_id"`
	EntraRedirectURI  string `mapstructure:"entra_redirect_uri" yaml:"entra_redirect_uri"`
	AdminPasswordHash string `mapstructure:"admin_password_hash" yaml:"admin_password_hash"`
	JWTSecret         string `mapstructure:"jwt_secret" yaml:"jwt_secret"`
	JWTLifetimeHours  int    `mapstructure:"jwt_lifetime_hours" yaml:"jwt_lifetime_hours"`
}

type DatabaseConfig struct {
	Path string `mapstructure:"path" yaml:"path"`
}

type IncusServer struct {
	Name          string `mapstructure:"name" yaml:"name"`
	Endpoint      string `mapstructure:"endpoint" yaml:"endpoint"`
	TLSClientCert string `mapstructure:"tls_client_cert" yaml:"tls_client_cert"`
	TLSClientKey  string `mapstructure:"tls_client_key" yaml:"tls_client_key"`
	MaxInstances  int    `mapstructure:"max_instances" yaml:"max_instances"`
}

// Write serializes the config to YAML and writes it to the given path.
func Write(cfg *Config, path string) error {
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0600)
}

func Load(path string) (*Config, error) {
	v := viper.New()
	v.SetConfigFile(path)
	v.SetDefault("server.host", "0.0.0.0")
	v.SetDefault("server.port", 8080)
	v.SetDefault("auth.jwt_lifetime_hours", 24)
	v.SetDefault("database.path", "plati.db")
	v.SetDefault("sleep_timeout", "4h")
	v.SetDefault("templates_dir", "templates")
	v.SetDefault("repos_dir", "repos")

	if err := v.ReadInConfig(); err != nil {
		return nil, err
	}

	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}
