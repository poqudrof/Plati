-- SSH-only users: people who need SSH access to instances (contractors, students, a
-- partner team) but must never log into Plati. They are ordinary rows in users, so their
-- public keys live in ssh_keys and show up in the SSH Access tab like anyone else's;
-- every auth path (password, Entra, API key, an already-issued JWT) refuses them.
ALTER TABLE users ADD COLUMN ssh_only INTEGER NOT NULL DEFAULT 0;
