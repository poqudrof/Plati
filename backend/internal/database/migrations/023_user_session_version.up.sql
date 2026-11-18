-- Sessions cut by a switch to SSH-only must stay cut. Checking ssh_only alone revived
-- them the moment the account was switched back — with the role the token was issued
-- with, so a former admin got an admin session back. Each switch to SSH-only bumps
-- session_version; a JWT carries the version it was issued under and is refused once
-- they differ. Tokens issued before this column existed carry 0, the default.
ALTER TABLE users ADD COLUMN session_version INTEGER NOT NULL DEFAULT 0;
