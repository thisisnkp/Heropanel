-- SSH host-key pinning for git sources (SQLite).
--
-- When set, host_key holds one or more known_hosts lines (ssh-keyscan format,
-- e.g. "github.com ssh-ed25519 AAAA...") for the repo's SSH host. The broker
-- writes them into a per-deploy known_hosts and switches SSH to strict host-key
-- checking, so even the *first* clone is verified against the pinned key rather
-- than trust-on-first-use. Empty keeps the prior accept-new (TOFU) behaviour.
-- The host key is a public value and may be returned by the API.
ALTER TABLE git_sources ADD COLUMN host_key TEXT NOT NULL DEFAULT '';
