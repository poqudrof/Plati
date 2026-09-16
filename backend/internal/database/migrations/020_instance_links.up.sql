-- Quick links shown on the instance's dashboard card.
--
-- link_openvscode / link_sshx are the user's ticks for the two built-in tools; their URLs
-- are not stored, because they are only known at read time (tailnet name or IP for
-- OpenVSCode, the session URL sshx prints to its journal on every start).
-- custom_links is a JSON array of {"label","url"} entered by hand.
ALTER TABLE instances ADD COLUMN link_openvscode INTEGER NOT NULL DEFAULT 0;
ALTER TABLE instances ADD COLUMN link_sshx INTEGER NOT NULL DEFAULT 0;
ALTER TABLE instances ADD COLUMN custom_links TEXT NOT NULL DEFAULT '[]';
