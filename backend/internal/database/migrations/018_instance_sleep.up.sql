-- Per-instance auto-stop (sleep) policy.
--
-- sleep_disabled=1 takes the instance out of the sleep worker's sweep entirely.
-- sleep_timeout_minutes=0 means "use the platform default" (config sleep_timeout),
-- so existing rows keep the behaviour they had before this migration.
ALTER TABLE instances ADD COLUMN sleep_disabled INTEGER NOT NULL DEFAULT 0;
ALTER TABLE instances ADD COLUMN sleep_timeout_minutes INTEGER NOT NULL DEFAULT 0;
