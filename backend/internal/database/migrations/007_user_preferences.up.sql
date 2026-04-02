CREATE TABLE user_preferences (
    user_id        INTEGER PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    ssh_key_mode   TEXT NOT NULL DEFAULT 'plati'
                   CHECK (ssh_key_mode IN ('plati', 'personal')),
    tailscale_mode TEXT NOT NULL DEFAULT 'plati'
                   CHECK (tailscale_mode IN ('plati', 'personal')),
    updated_at     DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
