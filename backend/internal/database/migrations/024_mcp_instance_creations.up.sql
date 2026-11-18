-- One row per instance created through the MCP endpoint (/mcp). Two limits read it:
--   * how many of them the user still owns (the row joins a live instance), and
--   * how many were created in the last hour, deleted ones included — which is why
--     instance_id is not a foreign key: the row must outlive the instance.
CREATE TABLE IF NOT EXISTS mcp_instance_creations (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id     INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    instance_id INTEGER NOT NULL,
    created_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_mcp_instance_creations_user ON mcp_instance_creations(user_id, created_at);
