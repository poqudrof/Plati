CREATE TABLE git_repos (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    name          TEXT NOT NULL UNIQUE,
    ssh_url       TEXT NOT NULL UNIQUE,
    local_path    TEXT NOT NULL,
    clone_status  TEXT NOT NULL DEFAULT 'pending',
    error_message TEXT,
    last_synced_at DATETIME,
    created_at    DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at    DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
