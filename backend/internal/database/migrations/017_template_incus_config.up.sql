-- Incus instance config keys a template needs (e.g. security.nesting for Docker).
-- JSON object of string→string, merged over the keys Plati derives from resources.
ALTER TABLE templates ADD COLUMN incus_config TEXT NOT NULL DEFAULT '{}';
