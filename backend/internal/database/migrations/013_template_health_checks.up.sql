ALTER TABLE templates ADD COLUMN health_checks TEXT NOT NULL DEFAULT '[]';
ALTER TABLE templates ADD COLUMN tailscale_serve TEXT NOT NULL DEFAULT '';
