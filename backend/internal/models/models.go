package models

import (
	"database/sql"
	"time"
)

type User struct {
	ID        int64          `db:"id" json:"id"`
	Email     string         `db:"email" json:"email"`
	Name      string         `db:"name" json:"name"`
	Role      string         `db:"role" json:"role"` // "admin" or "user"
	EntraID   sql.NullString `db:"entra_id" json:"-"`
	CreatedAt time.Time      `db:"created_at" json:"created_at"`
	UpdatedAt time.Time      `db:"updated_at" json:"updated_at"`
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
	ID          int64     `db:"id" json:"id"`
	Name        string    `db:"name" json:"name"`
	Slug        string    `db:"slug" json:"slug"`
	Description string    `db:"description" json:"description"`
	Image       string    `db:"image" json:"image"`
	Profiles    string    `db:"profiles" json:"profiles"`   // JSON array
	Resources   string    `db:"resources" json:"resources"` // JSON object
	CloudInit   string    `db:"cloud_init" json:"cloud_init"`
	IsActive    bool      `db:"is_active" json:"is_active"`
	CreatedAt   time.Time `db:"created_at" json:"created_at"`
	UpdatedAt   time.Time `db:"updated_at" json:"updated_at"`
}

type Volume struct {
	ID        int64     `db:"id" json:"id"`
	Name      string    `db:"name" json:"name"`
	ServerID  int64     `db:"server_id" json:"server_id"`
	Pool      string    `db:"pool" json:"pool"`
	SizeGB    int       `db:"size_gb" json:"size_gb"`
	CreatedAt time.Time `db:"created_at" json:"created_at"`
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
	LastActiveAt sql.NullTime   `db:"last_active_at" json:"last_active_at"`
	CreatedAt    time.Time      `db:"created_at" json:"created_at"`
	UpdatedAt    time.Time      `db:"updated_at" json:"updated_at"`
}
