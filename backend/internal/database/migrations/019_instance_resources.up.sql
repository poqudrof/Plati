-- Per-instance resource overrides, set by an administrator. The empty string means
-- "inherit the template's value", mirroring how sleep_timeout_minutes = 0 means "use the
-- platform default" (migration 018).
--
-- These are stored as text, not numbers: limits.cpu legitimately accepts a count ("2"),
-- a pinned set ("0-3", "0,2") or a percentage ("50%"), and limits.memory carries its unit.
ALTER TABLE instances ADD COLUMN limits_cpu TEXT NOT NULL DEFAULT '';
ALTER TABLE instances ADD COLUMN limits_memory TEXT NOT NULL DEFAULT '';
