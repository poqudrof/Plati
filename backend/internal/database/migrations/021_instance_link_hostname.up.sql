-- Whether the automatic tailnet hostname link (https://<name>.ts.net/, listed when the
-- instance serves 443) is pinned to the dashboard card. On by default: it is only ever
-- listed when something actually answers there. Custom links carry their own "pinned"
-- flag inside custom_links (migration 020); an entry without it counts as pinned.
ALTER TABLE instances ADD COLUMN link_hostname INTEGER NOT NULL DEFAULT 1;
