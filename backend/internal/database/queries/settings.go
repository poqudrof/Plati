package queries

import "github.com/jmoiron/sqlx"

func GetSetting(db *sqlx.DB, key string) (string, error) {
	var value string
	err := db.Get(&value, "SELECT value FROM settings WHERE key = ?", key)
	return value, err
}

func SetSetting(db *sqlx.DB, key, value string) error {
	_, err := db.Exec(
		`INSERT INTO settings (key, value) VALUES (?, ?)
		 ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = CURRENT_TIMESTAMP`,
		key, value,
	)
	return err
}
