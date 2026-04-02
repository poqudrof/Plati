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

func UpdateUser(db *sqlx.DB, id int64, name, role string) error {
	_, err := db.Exec(
		"UPDATE users SET name = ?, role = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?",
		name, role, id,
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
