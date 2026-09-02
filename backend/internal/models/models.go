package models

import (
	"database/sql"
	"time"
)

type GitRepo struct {
	ID           int64      `db:"id"             json:"id"`
	Name         string     `db:"name"           json:"name"`
	SSHURL       string     `db:"ssh_url"        json:"ssh_url"`
	LocalPath    string     `db:"local_path"     json:"local_path"`
	CloneStatus  string     `db:"clone_status"   json:"clone_status"`
	ErrorMessage *string    `db:"error_message"  json:"error_message,omitempty"`
	LastSyncedAt *time.Time `db:"last_synced_at" json:"last_synced_at,omitempty"`
	CreatedAt    time.Time  `db:"created_at"     json:"created_at"`
	UpdatedAt    time.Time  `db:"updated_at"     json:"updated_at"`
}

type TemplateRepoRef struct {
	Name string `json:"name"` // matches git_repos.name
	Dest string `json:"dest"` // path inside instance e.g. "/home/ubuntu/AI-state-art-public"
}

type User struct {
	ID           int64          `db:"id" json:"id"`
	Email        string         `db:"email" json:"email"`
	Name         string         `db:"name" json:"name"`
	Role         string         `db:"role" json:"role"` // "admin" or "user"
	EntraID      sql.NullString `db:"entra_id" json:"-"`
	PasswordHash sql.NullString `db:"password_hash" json:"-"`
	CreatedAt    time.Time      `db:"created_at" json:"created_at"`
	UpdatedAt    time.Time      `db:"updated_at" json:"updated_at"`
}

type APIKey struct {
	ID         int64      `db:"id" json:"id"`
	UserID     int64      `db:"user_id" json:"user_id"`
	Name       string     `db:"name" json:"name"`
	KeyPrefix  string     `db:"key_prefix" json:"key_prefix"`
	KeyHash    string     `db:"key_hash" json:"-"`
	LastUsedAt *time.Time `db:"last_used_at" json:"last_used_at,omitempty"`
	RevokedAt  *time.Time `db:"revoked_at" json:"revoked_at,omitempty"`
	CreatedAt  time.Time  `db:"created_at" json:"created_at"`
}

type SSHKey struct {
	ID        int64     `db:"id" json:"id"`
	UserID    int64     `db:"user_id" json:"user_id"`
	Name      string    `db:"name" json:"name"`
	PublicKey string    `db:"public_key" json:"public_key"`
	CreatedAt time.Time `db:"created_at" json:"created_at"`
}

// UserSSHKey is a Plati-generated keypair owned by a user.
// The private key is stored encrypted; it is injected into instances at creation time.
type UserSSHKey struct {
	ID                  int64     `db:"id" json:"id"`
	UserID              int64     `db:"user_id" json:"user_id"`
	Name                string    `db:"name" json:"name"`
	PublicKey           string    `db:"public_key" json:"public_key"`
	EncryptedPrivateKey string    `db:"encrypted_private_key" json:"-"`
	CreatedAt           time.Time `db:"created_at" json:"created_at"`
}

// ManagedSSHKey is an admin-created keypair assigned to managed users.
// The admin adds the public key to GitHub; the private key is injected into assigned users' instances.
type ManagedSSHKey struct {
	ID                  int64     `db:"id" json:"id"`
	Name                string    `db:"name" json:"name"`
	PublicKey           string    `db:"public_key" json:"public_key"`
	EncryptedPrivateKey string    `db:"encrypted_private_key" json:"-"`
	CreatedAt           time.Time `db:"created_at" json:"created_at"`
}

type Secret struct {
	ID             int64     `db:"id" json:"id"`
	UserID         int64     `db:"user_id" json:"user_id"`
	Name           string    `db:"name" json:"name"`
	EncryptedValue string    `db:"encrypted_value" json:"-"`
	CreatedAt      time.Time `db:"created_at" json:"created_at"`
	UpdatedAt      time.Time `db:"updated_at" json:"updated_at"`
}

type InstanceSecret struct {
	ID             int64     `db:"id" json:"id"`
	InstanceID     int64     `db:"instance_id" json:"instance_id"`
	Name           string    `db:"name" json:"name"`
	EncryptedValue string    `db:"encrypted_value" json:"-"`
	CreatedAt      time.Time `db:"created_at" json:"created_at"`
	UpdatedAt      time.Time `db:"updated_at" json:"updated_at"`
}

type Server struct {
	ID           int64     `db:"id" json:"id"`
	Name         string    `db:"name" json:"name"`
	Endpoint     string    `db:"endpoint" json:"endpoint"`
	TLSCertPath  string    `db:"tls_cert_path" json:"-"`
	TLSKeyPath   string    `db:"tls_key_path" json:"-"`
	MaxInstances int       `db:"max_instances" json:"max_instances"`
	IsOnline     bool      `db:"is_online" json:"is_online"`
	CreatedAt    time.Time `db:"created_at" json:"created_at"`
	UpdatedAt    time.Time `db:"updated_at" json:"updated_at"`
}

type Template struct {
	ID                 int64     `db:"id" json:"id"`
	Name               string    `db:"name" json:"name"`
	Slug               string    `db:"slug" json:"slug"`
	Description        string    `db:"description" json:"description"`
	Image              string    `db:"image" json:"image"`
	Profiles           string    `db:"profiles" json:"profiles"`                         // JSON array
	Resources          string    `db:"resources" json:"resources"`                       // JSON object
	TerminalUser       string    `db:"terminal_user" json:"terminal_user"`               // non-root login user; empty = root only
	PostCreateCommands string    `db:"post_create_commands" json:"post_create_commands"` // JSON array (deprecated)
	PersistenceMode    string    `db:"persistence_mode"    json:"persistence_mode"`      // "normal" | "ephemeral"
	PersistenceDirs    string    `db:"persistence_dirs"    json:"persistence_dirs"`      // JSON array of {path,size,pool}
	FirstInitCommands  string    `db:"first_init_commands" json:"first_init_commands"`   // JSON array
	RebuildCommands    string    `db:"rebuild_commands"    json:"rebuild_commands"`      // JSON array
	Includes           string    `db:"includes"            json:"includes"`              // JSON array of mixin names
	Repos              string    `db:"repos"               json:"repos"`                 // JSON array of {name,dest} repo refs
	HealthChecks       string    `db:"health_checks"       json:"health_checks"`         // JSON array of {port,path,expected_status,timeout,description}
	TailscaleServe     string    `db:"tailscale_serve"     json:"tailscale_serve"`       // JSON object {port,funnel} or empty
	IncusConfig        string    `db:"incus_config"        json:"incus_config"`          // JSON object of Incus config keys (e.g. security.nesting)
	IsActive           bool      `db:"is_active" json:"is_active"`
	CreatedAt          time.Time `db:"created_at" json:"created_at"`
	UpdatedAt          time.Time `db:"updated_at" json:"updated_at"`
}

type UserPreferences struct {
	UserID        int64     `db:"user_id"        json:"user_id"`
	SSHKeyMode    string    `db:"ssh_key_mode"   json:"ssh_key_mode"`
	TailscaleMode string    `db:"tailscale_mode" json:"tailscale_mode"`
	UpdatedAt     time.Time `db:"updated_at"     json:"updated_at"`
}

type InstanceVolume struct {
	ID         int64  `db:"id"          json:"id"`
	InstanceID int64  `db:"instance_id" json:"instance_id"`
	VolumeID   int64  `db:"volume_id"   json:"volume_id"`
	MountPath  string `db:"mount_path"  json:"mount_path"`
	DeviceName string `db:"device_name" json:"device_name"`
}

type Volume struct {
	ID        int64     `db:"id" json:"id"`
	Name      string    `db:"name" json:"name"`
	ServerID  int64     `db:"server_id" json:"server_id"`
	Pool      string    `db:"pool" json:"pool"`
	SizeGB    int       `db:"size_gb" json:"size_gb"`
	CreatedAt time.Time `db:"created_at" json:"created_at"`
}

// VolumeDetail is an instance_volume row enriched with volume metadata.
type VolumeDetail struct {
	VolumeID   int64     `db:"volume_id"    json:"volume_id"`
	MountPath  string    `db:"mount_path"   json:"mount_path"`
	DeviceName string    `db:"device_name"  json:"device_name"`
	VolumeName string    `db:"volume_name"  json:"volume_name"`
	Pool       string    `db:"pool"         json:"pool"`
	SizeGB     int       `db:"size_gb"      json:"size_gb"`
	CreatedAt  time.Time `db:"created_at"   json:"created_at"`
}

// DiskInfo is a volume enriched with instance, user, and server context.
type DiskInfo struct {
	VolumeID     int64     `db:"volume_id"      json:"volume_id"`
	VolumeName   string    `db:"volume_name"     json:"volume_name"`
	Pool         string    `db:"pool"            json:"pool"`
	SizeGB       int       `db:"size_gb"         json:"size_gb"`
	MountPath    string    `db:"mount_path"      json:"mount_path"`
	DeviceName   string    `db:"device_name"     json:"device_name"`
	InstanceID   int64     `db:"instance_id"     json:"instance_id"`
	InstanceName string    `db:"instance_name"   json:"instance_name"`
	IncusName    string    `db:"incus_name"      json:"incus_name"`
	Status       string    `db:"status"          json:"status"`
	UserID       int64     `db:"user_id"         json:"user_id"`
	UserEmail    string    `db:"user_email"      json:"user_email"`
	UserName     string    `db:"user_name"       json:"user_name"`
	ServerID     int64     `db:"server_id"       json:"server_id"`
	ServerName   string    `db:"server_name"     json:"server_name"`
	CreatedAt    time.Time `db:"created_at"      json:"created_at"`
}

type Instance struct {
	ID           int64          `db:"id" json:"id"`
	Name         string         `db:"name" json:"name"`
	UserID       int64          `db:"user_id" json:"user_id"`
	TemplateID   int64          `db:"template_id" json:"template_id"`
	ServerID     int64          `db:"server_id" json:"server_id"`
	VolumeID     sql.NullInt64  `db:"volume_id" json:"volume_id"`
	IncusName    string         `db:"incus_name" json:"incus_name"`
	Status       string         `db:"status" json:"status"` // creating, running, stopped, error
	IPAddress    sql.NullString `db:"ip_address" json:"ip_address"`
	CreationLog  string         `db:"creation_log" json:"creation_log"`
	LastActiveAt sql.NullTime   `db:"last_active_at" json:"last_active_at"`

	// Auto-stop policy. SleepDisabled takes the instance out of the sleep
	// worker's sweep; SleepTimeoutMinutes of 0 means "use the platform default".
	SleepDisabled       bool `db:"sleep_disabled" json:"sleep_disabled"`
	SleepTimeoutMinutes int  `db:"sleep_timeout_minutes" json:"sleep_timeout_minutes"`

	// Per-instance resource overrides set by an administrator. The empty string means
	// "inherit the template's value". Kept as strings because limits.cpu accepts a
	// count, a pinned set ("0-3") or a percentage, and limits.memory carries its unit.
	LimitsCPU    string    `db:"limits_cpu" json:"limits_cpu"`
	LimitsMemory string    `db:"limits_memory" json:"limits_memory"`
	CreatedAt    time.Time `db:"created_at" json:"created_at"`
	UpdatedAt    time.Time `db:"updated_at" json:"updated_at"`
}

// AdminInstance is an Instance enriched with the owner and template names.
// Admin views list machines across every user, where a bare user_id is useless.
type AdminInstance struct {
	Instance
	UserEmail    string `db:"user_email" json:"user_email"`
	UserName     string `db:"user_name" json:"user_name"`
	TemplateName string `db:"template_name" json:"template_name"`
}
