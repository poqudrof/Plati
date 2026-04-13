-- SQLite does not support DROP COLUMN in older versions; recreate table without password_hash
CREATE TABLE users_backup AS SELECT id, email, name, role, entra_id, created_at, updated_at FROM users;
DROP TABLE users;
ALTER TABLE users_backup RENAME TO users;
