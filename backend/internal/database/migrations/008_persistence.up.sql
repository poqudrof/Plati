CREATE TABLE instance_volumes (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    instance_id INTEGER NOT NULL REFERENCES instances(id) ON DELETE CASCADE,
    volume_id   INTEGER NOT NULL REFERENCES volumes(id) ON DELETE CASCADE,
    mount_path  TEXT NOT NULL,
    device_name TEXT NOT NULL,
    UNIQUE(instance_id, mount_path)
);

ALTER TABLE templates ADD COLUMN persistence_mode TEXT NOT NULL DEFAULT 'normal'
    CHECK (persistence_mode IN ('ephemeral', 'normal'));
ALTER TABLE templates ADD COLUMN persistence_dirs TEXT NOT NULL DEFAULT '[]';
ALTER TABLE templates ADD COLUMN first_init_commands TEXT NOT NULL DEFAULT '[]';
ALTER TABLE templates ADD COLUMN rebuild_commands TEXT NOT NULL DEFAULT '[]';
