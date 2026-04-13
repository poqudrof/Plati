-- Drop the cloud_init column from templates.
-- Use the safe copy-table approach for SQLite compatibility.
CREATE TABLE templates_new (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL,
    slug TEXT NOT NULL UNIQUE,
    description TEXT NOT NULL DEFAULT '',
    image TEXT NOT NULL,
    profiles TEXT NOT NULL DEFAULT '["default"]',
    resources TEXT NOT NULL DEFAULT '{}',
    terminal_user TEXT NOT NULL DEFAULT '',
    post_create_commands TEXT NOT NULL DEFAULT '[]',
    persistence_mode TEXT NOT NULL DEFAULT 'normal',
    persistence_dirs TEXT NOT NULL DEFAULT '[]',
    first_init_commands TEXT NOT NULL DEFAULT '[]',
    rebuild_commands TEXT NOT NULL DEFAULT '[]',
    includes TEXT NOT NULL DEFAULT '[]',
    repos TEXT NOT NULL DEFAULT '[]',
    health_checks TEXT NOT NULL DEFAULT '[]',
    tailscale_serve TEXT NOT NULL DEFAULT '',
    is_active BOOLEAN NOT NULL DEFAULT 1,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

INSERT INTO templates_new (id, name, slug, description, image, profiles, resources,
    terminal_user, post_create_commands, persistence_mode, persistence_dirs,
    first_init_commands, rebuild_commands, includes, repos,
    health_checks, tailscale_serve, is_active, created_at, updated_at)
SELECT id, name, slug, description, image, profiles, resources,
    terminal_user, post_create_commands, persistence_mode, persistence_dirs,
    first_init_commands, rebuild_commands, includes, repos,
    health_checks, tailscale_serve, is_active, created_at, updated_at
FROM templates;

DROP TABLE templates;
ALTER TABLE templates_new RENAME TO templates;
