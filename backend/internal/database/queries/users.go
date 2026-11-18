package queries

import (
	"github.com/jmoiron/sqlx"
	"github.com/homaserver/plati/internal/models"
)

func GetUserByID(db *sqlx.DB, id int64) (*models.User, error) {
	var u models.User
	err := db.Get(&u, "SELECT * FROM users WHERE id = ?", id)
	if err != nil {
		return nil, err
	}
	return &u, nil
}

func GetUserByEmail(db *sqlx.DB, email string) (*models.User, error) {
	var u models.User
	err := db.Get(&u, "SELECT * FROM users WHERE email = ?", email)
	if err != nil {
		return nil, err
	}
	return &u, nil
}

func GetUserByEntraID(db *sqlx.DB, entraID string) (*models.User, error) {
	var u models.User
	err := db.Get(&u, "SELECT * FROM users WHERE entra_id = ?", entraID)
	if err != nil {
		return nil, err
	}
	return &u, nil
}

func ListUsers(db *sqlx.DB) ([]models.User, error) {
	users := []models.User{}
	err := db.Select(&users, "SELECT * FROM users ORDER BY created_at DESC")
	return users, err
}

func CreateUser(db *sqlx.DB, email, name, role string, entraID *string) (int64, error) {
	res, err := db.Exec(
		"INSERT INTO users (email, name, role, entra_id) VALUES (?, ?, ?, ?)",
		email, name, role, entraID,
	)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func CreateUserWithPassword(db *sqlx.DB, email, name, role, passwordHash string) (int64, error) {
	res, err := db.Exec(
		"INSERT INTO users (email, name, role, password_hash) VALUES (?, ?, ?, ?)",
		email, name, role, passwordHash,
	)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// CreateSSHOnlyUser inserts an account that cannot log in: no password hash, and
// ssh_only set so Entra and API keys refuse it too.
func CreateSSHOnlyUser(db *sqlx.DB, email, name string) (int64, error) {
	res, err := db.Exec(
		"INSERT INTO users (email, name, role, ssh_only) VALUES (?, ?, 'user', 1)",
		email, name,
	)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// SetUserSSHOnly switches an account to or from SSH-only. Turning it on also drops the
// password hash and the admin role, bumps session_version and deletes the account's API
// keys, so turning it back off grants nothing by itself: every open session and every
// existing key stays revoked, and the admin has to set a password or mint a new key.
func SetUserSSHOnly(db *sqlx.DB, id int64, sshOnly bool) error {
	if !sshOnly {
		_, err := db.Exec("UPDATE users SET ssh_only = 0, updated_at = CURRENT_TIMESTAMP WHERE id = ?", id)
		return err
	}
	tx, err := db.Beginx()
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck // no-op once committed
	if _, err := tx.Exec(
		`UPDATE users SET ssh_only = 1, password_hash = NULL, role = 'user',
		 session_version = session_version + 1, updated_at = CURRENT_TIMESTAMP WHERE id = ?`,
		id,
	); err != nil {
		return err
	}
	if _, err := tx.Exec("DELETE FROM api_keys WHERE user_id = ?", id); err != nil {
		return err
	}
	return tx.Commit()
}

// IsSessionRevoked reports whether a JWT issued under sessionVersion may no longer be
// used: the account is SSH-only, or was switched to SSH-only since the token was issued.
// A missing user reads as false: whether a deleted account's token still works is not
// this check's call.
func IsSessionRevoked(db *sqlx.DB, id, sessionVersion int64) bool {
	var row struct {
		SSHOnly bool  `db:"ssh_only"`
		Version int64 `db:"session_version"`
	}
	if err := db.Get(&row, "SELECT ssh_only, session_version FROM users WHERE id = ?", id); err != nil {
		return false
	}
	return row.SSHOnly || row.Version != sessionVersion
}

func UpdateUser(db *sqlx.DB, id int64, name, role string) error {
	_, err := db.Exec(
		"UPDATE users SET name = ?, role = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?",
		name, role, id,
	)
	return err
}

// SetUserPassword replaces the user's bcrypt hash. Passing an empty hash would
// lock the account out of password login, so callers must hash first.
func SetUserPassword(db *sqlx.DB, id int64, hash string) error {
	_, err := db.Exec(
		"UPDATE users SET password_hash = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?",
		hash, id,
	)
	return err
}

func DeleteUser(db *sqlx.DB, id int64) error {
	_, err := db.Exec("DELETE FROM users WHERE id = ?", id)
	return err
}

func UpsertEntraUser(db *sqlx.DB, email, name, entraID string) (*models.User, error) {
	_, err := db.Exec(`
		INSERT INTO users (email, name, entra_id) VALUES (?, ?, ?)
		ON CONFLICT(entra_id) DO UPDATE SET name = excluded.name, updated_at = CURRENT_TIMESTAMP
	`, email, name, entraID)
	if err != nil {
		return nil, err
	}
	return GetUserByEntraID(db, entraID)
}
