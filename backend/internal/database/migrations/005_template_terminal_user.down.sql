-- SQLite does not support DROP COLUMN directly before 3.35; recreate table without the column.
CREATE TABLE templates_backup AS SELECT id, name, slug, description, image, profiles, resources, cloud_init, is_active, created_at, updated_at FROM templates;
DROP TABLE templates;
ALTER TABLE templates_backup RENAME TO templates;
