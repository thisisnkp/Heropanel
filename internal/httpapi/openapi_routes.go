package httpapi

// apiDocs is the documentation metadata for every mounted route, keyed by
// "<METHOD> <chi-pattern>". buildOpenAPI walks the real routing tree and looks
// each route up here; a route with no entry is reported by UndocumentedRoutes
// and fails the drift test. Request/response *shapes* live in openapi_schemas.go
// and are referenced here so a shape is described once.

// okResp is the shape returned by endpoints that only acknowledge success
// (deletes, toggles). The envelope is still emitted; the data payload is an
// implementation detail not worth pinning, so it is left unspecified with a
// description instead of a false schema.

var apiDocs = map[string]opMeta{
	// ── system / infra ────────────────────────────────────────────────────────
	"GET /healthz": {
		Summary: "Liveness probe", Tags: []string{"System"}, NoAuth: true,
		RespSchema: ref("Health"), RespDesc: "The process is up.",
	},
	"GET /readyz": {
		Summary: "Readiness probe", Tags: []string{"System"}, NoAuth: true,
		RespSchema: ref("Health"), RespDesc: "Dependencies (datastore, broker) are reachable.",
	},
	"GET /api/v1/system/info": {
		Summary: "Instance information", Tags: []string{"System"}, NoAuth: true,
		RespSchema: ref("SystemInfo"),
	},
	"GET /api/v1/openapi.json": {
		Summary: "This OpenAPI document", Tags: []string{"System"}, NoAuth: true, NoEnvelope: true,
		RespSchema: map[string]any{"type": "object", "description": "OpenAPI 3.1 document."},
	},
	"POST /hooks/git/{uid}": {
		Summary: "Git push webhook", Tags: []string{"Git"}, NoAuth: true,
		ReqDesc:  "Provider webhook payload; authorized by the per-source secret, not a session.",
		RespDesc: "Deploy triggered (or ignored for a non-matching ref).",
	},

	// ── auth ──────────────────────────────────────────────────────────────────
	"GET /api/v1/auth/status": {
		Summary: "Bootstrap / auth status", Tags: []string{"Auth"}, NoAuth: true,
		RespSchema: ref("AuthStatus"),
	},
	"POST /api/v1/auth/bootstrap": {
		Summary: "Create the first admin", Tags: []string{"Auth"}, NoAuth: true,
		ReqSchema: object(map[string]any{
			"email":    prop("string", ""),
			"username": prop("string", ""),
			"password": prop("string", ""),
		}, "email", "username", "password"),
		RespSchema: ref("Principal"), RespStatus: 201, RespDesc: "Admin created; only allowed on a fresh install.",
	},
	"POST /api/v1/auth/login": {
		Summary: "Log in", Tags: []string{"Auth"}, NoAuth: true,
		ReqSchema: object(map[string]any{
			"email":    prop("string", ""),
			"password": prop("string", ""),
		}, "email", "password"),
		RespSchema: ref("LoginResult"), RespDesc: "Session established, or MFA required.",
	},
	"POST /api/v1/auth/mfa": {
		Summary: "Complete an MFA login", Tags: []string{"Auth"}, NoAuth: true,
		ReqSchema: object(map[string]any{
			"mfa_token": prop("string", "From the login response."),
			"code":      prop("string", "Current TOTP code."),
		}, "mfa_token", "code"),
		RespSchema: ref("LoginResult"),
	},
	"POST /api/v1/auth/logout": {
		Summary: "Log out", Tags: []string{"Auth"}, RespDesc: "Session cleared.",
	},
	"GET /api/v1/auth/me": {
		Summary: "Current principal", Tags: []string{"Auth"}, RespSchema: ref("Principal"),
	},
	"POST /api/v1/auth/mfa/setup": {
		Summary: "Begin MFA enrolment", Tags: []string{"Auth"}, RespSchema: ref("MFASetup"),
	},
	"POST /api/v1/auth/mfa/enable": {
		Summary: "Enable MFA", Tags: []string{"Auth"},
		ReqSchema: object(map[string]any{"code": prop("string", "TOTP code proving possession.")}, "code"),
		RespDesc:  "MFA enabled.",
	},
	"POST /api/v1/auth/mfa/disable": {
		Summary: "Disable MFA", Tags: []string{"Auth"},
		ReqSchema: object(map[string]any{"code": prop("string", "Current TOTP code.")}, "code"),
		RespDesc:  "MFA disabled.",
	},

	// ── account / api keys ────────────────────────────────────────────────────
	"GET /api/v1/account/api-keys": {
		Summary: "List API keys", Tags: []string{"Account"}, RespSchema: arrayOf(ref("APIKey")),
	},
	"POST /api/v1/account/api-keys": {
		Summary: "Mint an API key", Tags: []string{"Account"},
		ReqSchema: object(map[string]any{
			"name":   prop("string", ""),
			"scopes": arrayOf(prop("string", "Permission scopes to grant the key.")),
		}, "name"),
		RespSchema: ref("APIKey"), RespStatus: 201, RespDesc: "The secret is returned once, here only.",
	},
	"DELETE /api/v1/account/api-keys/{uid}": {
		Summary: "Revoke an API key", Tags: []string{"Account"}, RespDesc: "Key revoked.",
	},

	// ── users ─────────────────────────────────────────────────────────────────
	"GET /api/v1/users": {
		Summary: "List users", Tags: []string{"Users"}, Permission: "user.read",
		RespSchema: arrayOf(ref("Principal")),
	},

	// ── modules / capabilities ────────────────────────────────────────────────
	"GET /api/v1/capabilities": {
		Summary: "Capability set", Tags: []string{"Modules"}, RespSchema: ref("Capabilities"),
	},
	"GET /api/v1/modules": {
		Summary: "Installed modules", Tags: []string{"Modules"}, RespSchema: ref("Modules"),
	},

	// ── audit ─────────────────────────────────────────────────────────────────
	"GET /api/v1/audit": {
		Summary: "List audit entries", Tags: []string{"Audit"}, Permission: "audit.read",
		RespSchema: arrayOf(ref("AuditEntry")),
	},
	"GET /api/v1/audit/verify": {
		Summary: "Verify the hash chain", Tags: []string{"Audit"}, Permission: "audit.read",
		RespSchema: ref("AuditVerify"),
		RespDesc:   "200 even when the chain is broken; check the intact field.",
	},

	// ── php ───────────────────────────────────────────────────────────────────
	"GET /api/v1/php/extensions": {
		Summary: "List PHP extensions", Tags: []string{"PHP"}, Permission: "system.read",
		RespSchema: ref("PHPExtensions"),
	},
	"POST /api/v1/php/extensions": {
		Summary: "Toggle a PHP extension", Tags: []string{"PHP"}, Permission: "system.write",
		ReqDesc: "Server-scope: restarts FPM for every site on this PHP version.",
		ReqSchema: object(map[string]any{
			"version":   prop("string", ""),
			"extension": prop("string", ""),
			"enabled":   prop("boolean", ""),
		}, "version", "extension", "enabled"),
		RespSchema: ref("PHPExtensions"),
	},

	// ── databases ─────────────────────────────────────────────────────────────
	"GET /api/v1/databases": {
		Summary: "List databases", Tags: []string{"Databases"}, Permission: "database.read",
		RespSchema: arrayOf(ref("Database")),
	},
	"POST /api/v1/databases": {
		Summary: "Create a database", Tags: []string{"Databases"}, Permission: "database.write",
		ReqSchema:  object(map[string]any{"name": prop("string", "")}, "name"),
		RespSchema: ref("Database"), RespStatus: 201,
	},
	"DELETE /api/v1/databases/{uid}": {
		Summary: "Drop a database", Tags: []string{"Databases"}, Permission: "database.write",
		RespDesc: "Database dropped.",
	},
	"POST /api/v1/databases/{uid}/grant": {
		Summary: "Grant a user access", Tags: []string{"Databases"}, Permission: "database.write",
		ReqSchema: object(map[string]any{
			"user_uid":   prop("string", ""),
			"privileges": arrayOf(prop("string", "")),
		}, "user_uid"),
		RespDesc: "Grant applied.",
	},
	"POST /api/v1/databases/{uid}/revoke": {
		Summary: "Revoke a user's access", Tags: []string{"Databases"}, Permission: "database.write",
		ReqSchema: object(map[string]any{
			"user_uid":   prop("string", ""),
			"privileges": arrayOf(prop("string", "")),
		}, "user_uid"),
		RespDesc: "Grant revoked.",
	},
	"GET /api/v1/databases/{uid}/size": {
		Summary: "Database size", Tags: []string{"Databases"}, Permission: "database.read",
		RespSchema: ref("DatabaseSize"),
	},
	"GET /api/v1/databases/{uid}/export": {
		Summary: "Export (dump) a database", Tags: []string{"Databases"}, Permission: "database.write",
		RespDesc: "Streams a SQL dump (application/sql). Write-gated: a full copy leaves the server.",
	},
	"POST /api/v1/databases/{uid}/import": {
		Summary: "Import into a database", Tags: []string{"Databases"}, Permission: "database.write",
		ReqDesc:  "A SQL dump body to load into the database.",
		RespDesc: "Import complete.",
	},
	"POST /api/v1/databases/{uid}/adminer-sso": {
		Summary: "Adminer single sign-on", Tags: []string{"Databases"}, Permission: "database.write",
		RespSchema: ref("AdminerSSO"),
		RespDesc:   "Mints a throwaway credential to POST into Adminer.",
	},
	"GET /api/v1/database-users": {
		Summary: "List database users", Tags: []string{"Databases"}, Permission: "database.read",
		RespSchema: arrayOf(ref("DatabaseUser")),
	},
	"POST /api/v1/database-users": {
		Summary: "Create a database user", Tags: []string{"Databases"}, Permission: "database.write",
		ReqSchema: object(map[string]any{
			"username": prop("string", ""),
			"host":     prop("string", "Host the user may connect from."),
			"password": prop("string", ""),
		}, "username", "password"),
		RespSchema: ref("DatabaseUser"), RespStatus: 201,
	},
	"DELETE /api/v1/database-users/{uid}": {
		Summary: "Drop a database user", Tags: []string{"Databases"}, Permission: "database.write",
		RespDesc: "User dropped.",
	},

	// ── ssl ───────────────────────────────────────────────────────────────────
	"GET /api/v1/ssl/certificates": {
		Summary: "List certificates", Tags: []string{"SSL"}, Permission: "ssl.read",
		RespSchema: arrayOf(ref("Certificate")),
	},
	"POST /api/v1/ssl/self-signed": {
		Summary: "Issue a self-signed certificate", Tags: []string{"SSL"}, Permission: "ssl.write",
		ReqSchema:  object(map[string]any{"domain": prop("string", "")}, "domain"),
		RespSchema: ref("Certificate"), RespStatus: 201,
	},
	"POST /api/v1/ssl/upload": {
		Summary: "Upload a custom certificate", Tags: []string{"SSL"}, Permission: "ssl.write",
		ReqSchema: object(map[string]any{
			"cert_pem": prop("string", "PEM certificate chain."),
			"key_pem":  prop("string", "PEM private key."),
		}, "cert_pem", "key_pem"),
		RespSchema: ref("Certificate"), RespStatus: 201,
	},
	"POST /api/v1/ssl/issue": {
		Summary: "Issue via Let's Encrypt", Tags: []string{"SSL"}, Permission: "ssl.write",
		ReqSchema: object(map[string]any{
			"domain":  prop("string", ""),
			"webroot": prop("string", "For HTTP-01."),
			"method":  map[string]any{"type": "string", "enum": []any{"http-01", "dns-01"}},
		}, "domain"),
		RespSchema: ref("Certificate"), RespStatus: 201, RespDesc: "Certificate issued (supports wildcards via dns-01).",
	},

	// ── sites ─────────────────────────────────────────────────────────────────
	"GET /api/v1/sites": {
		Summary: "List sites", Tags: []string{"Sites"}, Permission: "site.read",
		RespSchema: arrayOf(ref("Site")),
	},
	"POST /api/v1/sites": {
		Summary: "Create a site", Tags: []string{"Sites"}, Permission: "site.write",
		ReqSchema: object(map[string]any{
			"name":           prop("string", ""),
			"primary_domain": prop("string", ""),
			"type":           map[string]any{"type": "string", "enum": []any{"php", "static", "proxy"}},
			"deploy_mode":    map[string]any{"type": "string", "enum": []any{"managed", "git"}},
		}, "name", "primary_domain", "type"),
		RespSchema: ref("Site"), RespStatus: 201,
	},
	"GET /api/v1/sites/{uid}": {
		Summary: "Get a site", Tags: []string{"Sites"}, Permission: "site.read", RespSchema: ref("Site"),
	},
	"DELETE /api/v1/sites/{uid}": {
		Summary: "Delete a site", Tags: []string{"Sites"}, Permission: "site.write", RespDesc: "Site deleted.",
	},
	"GET /api/v1/sites/{uid}/php": {
		Summary: "Get a site's PHP settings", Tags: []string{"Sites", "PHP"}, Permission: "site.read",
		RespSchema: ref("PHPInfo"),
	},
	"PUT /api/v1/sites/{uid}/php": {
		Summary: "Update a site's PHP settings", Tags: []string{"Sites", "PHP"}, Permission: "site.write",
		ReqSchema: object(map[string]any{
			"version":         prop("string", ""),
			"memory_limit_mb": prop("integer", ""),
			"fpm":             ref("FPM"),
			"ini":             map[string]any{"type": "object", "additionalProperties": map[string]any{"type": "string"}, "description": "Allowlisted php.ini overrides."},
			"opcache":         ref("OPcache"),
		}),
		RespSchema: ref("PHPInfo"),
	},
	"GET /api/v1/sites/{uid}/limits": {
		Summary: "Get resource limits", Tags: []string{"Sites"}, Permission: "site.read",
		RespSchema: ref("SiteLimits"),
	},
	"PUT /api/v1/sites/{uid}/limits": {
		Summary: "Set resource limits", Tags: []string{"Sites"}, Permission: "site.write",
		ReqSchema: ref("SiteLimits"), RespSchema: ref("SiteLimits"),
	},
	"GET /api/v1/sites/{uid}/logs": {
		Summary: "Tail a site log", Tags: []string{"Sites"}, Permission: "site.read",
		RespSchema: ref("SiteLog"),
		RespDesc:   "Query: kind=access|error, lines=N.",
	},
	"POST /api/v1/sites/{uid}/suspend": {
		Summary: "Suspend a site", Tags: []string{"Sites"}, Permission: "site.write",
		RespSchema: ref("Site"), RespDesc: "Site walled off behind a 503 while keeping its domain mapping.",
	},
	"POST /api/v1/sites/{uid}/resume": {
		Summary: "Resume a site", Tags: []string{"Sites"}, Permission: "site.write", RespSchema: ref("Site"),
	},
	"POST /api/v1/sites/{uid}/clone": {
		Summary: "Clone a site", Tags: []string{"Sites"}, Permission: "site.write",
		ReqSchema: object(map[string]any{
			"name":           prop("string", ""),
			"primary_domain": prop("string", ""),
		}, "name", "primary_domain"),
		RespSchema: ref("Site"), RespStatus: 202, RespDesc: "Clone started; copies the document root (not DB/git/runtime).",
	},

	// ── domains ───────────────────────────────────────────────────────────────
	"GET /api/v1/sites/{uid}/domains": {
		Summary: "List a site's domains", Tags: []string{"Domains"}, Permission: "site.read",
		RespSchema: arrayOf(ref("Domain")),
	},
	"POST /api/v1/sites/{uid}/domains": {
		Summary: "Add a domain", Tags: []string{"Domains"}, Permission: "site.write",
		ReqSchema: object(map[string]any{
			"fqdn":          prop("string", ""),
			"kind":          map[string]any{"type": "string", "enum": []any{"alias", "redirect"}},
			"redirect_to":   prop("string", ""),
			"redirect_code": prop("integer", ""),
		}, "fqdn", "kind"),
		RespSchema: ref("Domain"), RespStatus: 201,
	},
	"DELETE /api/v1/sites/{uid}/domains/{did}": {
		Summary: "Remove a domain", Tags: []string{"Domains"}, Permission: "site.write", RespDesc: "Domain removed.",
	},
	"PUT /api/v1/sites/{uid}/force-https": {
		Summary: "Toggle force-HTTPS", Tags: []string{"Domains"}, Permission: "site.write",
		ReqSchema: object(map[string]any{"enabled": prop("boolean", "")}, "enabled"),
		RespDesc:  "Setting applied.",
	},

	// ── runtime ───────────────────────────────────────────────────────────────
	"GET /api/v1/sites/{uid}/runtime": {
		Summary: "Get the runtime config", Tags: []string{"Runtime"}, Permission: "site.read",
		RespSchema: ref("Runtime"), RespDesc: "404 until a runtime is configured.",
	},
	"GET /api/v1/sites/{uid}/runtime/health": {
		Summary: "Probe the app's health", Tags: []string{"Runtime"}, Permission: "site.read",
		RespSchema: ref("RuntimeHealth"),
	},
	"PUT /api/v1/sites/{uid}/runtime": {
		Summary: "Configure the runtime", Tags: []string{"Runtime"}, Permission: "site.write",
		ReqSchema: object(map[string]any{
			"runtime":     map[string]any{"type": "string", "enum": []any{"node", "python", "go", "generic"}},
			"command":     prop("string", ""),
			"port":        prop("integer", ""),
			"env":         map[string]any{"type": "object", "additionalProperties": map[string]any{"type": "string"}},
			"health_path": prop("string", ""),
		}, "runtime", "command", "port"),
		RespSchema: ref("Runtime"),
	},
	"POST /api/v1/sites/{uid}/runtime/start": {
		Summary: "Start the app", Tags: []string{"Runtime"}, Permission: "site.write", RespSchema: ref("Runtime"),
	},
	"POST /api/v1/sites/{uid}/runtime/stop": {
		Summary: "Stop the app", Tags: []string{"Runtime"}, Permission: "site.write", RespSchema: ref("Runtime"),
	},
	"POST /api/v1/sites/{uid}/runtime/restart": {
		Summary: "Restart the app", Tags: []string{"Runtime"}, Permission: "site.write", RespSchema: ref("Runtime"),
	},

	// ── scheduler ─────────────────────────────────────────────────────────────
	"GET /api/v1/sites/{uid}/cron": {
		Summary: "List scheduled jobs", Tags: []string{"Scheduler"}, Permission: "site.read",
		RespDesc: "The site's cron jobs. Each is a real systemd timer triggering a oneshot service that runs as the site's user, in its home, inside its cgroup slice.",
	},
	"POST /api/v1/sites/{uid}/cron": {
		Summary: "Schedule a job", Tags: []string{"Scheduler"}, Permission: "site.write",
		ReqSchema: object(map[string]any{
			"name":     prop("string", "Job name."),
			"command":  prop("string", "The command, one line. Runs as the site user in the site home — never root."),
			"schedule": prop("string", "systemd OnCalendar expression, e.g. \"daily\", \"*-*-* 02:00:00\", \"Mon *-*-* 00:00:00\"."),
		}, "name", "command", "schedule"),
		RespDesc: "Creates the job and enables its timer. Overlap policy comes free from systemd: a oneshot still running when its timer fires again is not started a second time, so a slow job never stacks. Persistent=true also runs a job missed during downtime once.",
	},
	"PUT /api/v1/sites/{uid}/cron/{jid}": {
		Summary: "Enable or disable a job", Tags: []string{"Scheduler"}, Permission: "site.write",
		ReqSchema: object(map[string]any{"enabled": prop("boolean", "Whether the timer is active.")}, "enabled"),
		RespDesc:  "Disabling removes the systemd timer (the definition is kept); enabling re-applies it.",
	},
	"POST /api/v1/sites/{uid}/cron/{jid}/run": {
		Summary: "Run a job now", Tags: []string{"Scheduler"}, Permission: "site.write",
		RespDesc: "Triggers the job immediately, so it can be tested without waiting for the timer.",
	},
	"GET /api/v1/sites/{uid}/cron/{jid}/logs": {
		Summary: "A job's output", Tags: []string{"Scheduler"}, Permission: "site.read",
		RespDesc: "A bounded tail of the job's captured stdout+stderr (the launcher appends to a log in the site's logs directory, so this works without the journal). Force-audited.",
	},
	"DELETE /api/v1/sites/{uid}/cron/{jid}": {
		Summary: "Delete a job", Tags: []string{"Scheduler"}, Permission: "site.write",
		RespDesc: "Disables the timer and removes the job.",
	},

	// ── backups ───────────────────────────────────────────────────────────────
	"GET /api/v1/sites/{uid}/backups": {
		Summary: "List a site's backups", Tags: []string{"Backups"}, Permission: "site.read",
		RespDesc: "The site's backups (newest first), its schedule/retention policy, and which targets are configured. Every archive is zstd-compressed and sealed with chunked AES-256-GCM before it touches any target — a stolen disk or bucket yields ciphertext.",
	},
	"POST /api/v1/sites/{uid}/backups": {
		Summary: "Run a backup now", Tags: []string{"Backups"}, Permission: "site.write",
		ReqSchema: object(map[string]any{
			"level":  prop("string", "full | incr; empty picks automatically (full for a fresh chain, incremental after)."),
			"target": prop("string", "local | s3 (default local)."),
		}),
		RespDesc: "Archives the site (GNU tar --listed-incremental, zstd), seals it, and stores it on the target. Requires a data key (HP_SECRET_KEY) — encrypted-at-rest is not optional. 503 without one.",
	},
	"PUT /api/v1/sites/{uid}/backups/config": {
		Summary: "Set the backup policy", Tags: []string{"Backups"}, Permission: "site.write",
		ReqSchema: object(map[string]any{
			"enabled":        prop("boolean", "Whether the scheduler backs this site up."),
			"interval_hours": prop("integer", "How often (1–720)."),
			"target":         prop("string", "local | s3."),
			"keep_chains":    prop("integer", "How many full+incremental chains to retain (1–30)."),
			"db_uid":         prop("string", "Optional: a panel-managed database whose FULL dump rides along with every backup as a second sealed object (SQL dumps do not do incrementals). Empty = files only."),
		}, "enabled", "interval_hours", "target", "keep_chains"),
		RespDesc: "The scheduler sweeps hourly and backs up any enabled site whose newest backup is older than its interval; a new full chain retires the oldest beyond keep_chains.",
	},
	"POST /api/v1/sites/{uid}/backups/{bid}/restore": {
		Summary: "Restore into a new site", Tags: []string{"Backups"}, Permission: "site.write",
		ReqSchema: object(map[string]any{
			"name":           prop("string", "Name for the restored site."),
			"primary_domain": prop("string", "Domain for the restored site."),
			"db_name":        prop("string", "Optional: create a NEW database with this name and import the backup's database dump into it — the original database is never touched, for the same reason the tree goes into a new site."),
		}, "name", "primary_domain"),
		RespDesc: "Provisions a fresh site and replays the backup's chain into it (the full, then each incremental in order — deletions included). The original keeps serving untouched, so a mistaken restore destroys nothing; a backup that fails authentication is refused and nothing is written. Returns the new site (plus the new database when one was restored).",
	},
	"DELETE /api/v1/sites/{uid}/backups/{bid}": {
		Summary: "Delete a backup", Tags: []string{"Backups"}, Permission: "site.write",
		RespDesc: "Removes the backup **and every later backup in its chain** — they depend on it, and saying so beats a chain that breaks silently at restore time. The database dump object, when the backup carried one, goes with it. Returns the removed uids.",
	},
	"GET /api/v1/system/backups": {
		Summary: "List panel self-backups", Tags: []string{"Backups"}, Permission: "system.read",
		RespDesc: "Sealed snapshots of the panel's own database (newest first) plus the active policy. Every snapshot is full and stands alone. Restore is deliberately out-of-band: `hpd decrypt` on the sealed object plus the documented manual steps — a panel that needs its database back cannot be trusted to serve that request.",
	},
	"POST /api/v1/system/backups": {
		Summary: "Snapshot the panel now", Tags: []string{"Backups"}, Permission: "system.write",
		RespDesc: "Snapshots the panel's database (SQLite VACUUM INTO, or mysqldump via the broker), seals it, and stores it on the policy's target. Requires a data key (HP_SECRET_KEY).",
	},
	"DELETE /api/v1/system/backups/{uid}": {
		Summary: "Delete a panel self-backup", Tags: []string{"Backups"}, Permission: "system.write",
		RespDesc: "Removes one snapshot, row and stored object. No chains here — each snapshot stands alone.",
	},

	// ── git ───────────────────────────────────────────────────────────────────
	"GET /api/v1/sites/{uid}/git": {
		Summary: "Get the Git source", Tags: []string{"Git"}, Permission: "git.read",
		RespSchema: ref("GitSource"), RespDesc: "404 until a source is set.",
	},
	"PUT /api/v1/sites/{uid}/git": {
		Summary: "Set the Git source", Tags: []string{"Git"}, Permission: "git.write",
		ReqSchema: object(map[string]any{
			"repo_url":      prop("string", ""),
			"branch":        prop("string", ""),
			"build_command": prop("string", ""),
			"web_root":      prop("string", ""),
			"auth_kind":     map[string]any{"type": "string", "enum": []any{"none", "token", "ssh_key"}},
			"auth_username": prop("string", ""),
			"token":         prop("string", "Stored sealed; write-only."),
			"rotate_key":    prop("boolean", "Regenerate the ssh deploy key."),
			"host_key":      prop("string", "Pin the repo host's SSH host key(s) for strict checking (ssh_key auth)."),
			"auto_composer": prop("boolean", ""),
		}, "repo_url"),
		RespSchema: ref("GitSource"),
	},
	"GET /api/v1/sites/{uid}/git/deployments": {
		Summary: "List deployments", Tags: []string{"Git"}, Permission: "git.read",
		RespSchema: arrayOf(ref("Deployment")),
	},
	"POST /api/v1/sites/{uid}/git/deploy": {
		Summary: "Deploy the current branch", Tags: []string{"Git"}, Permission: "git.write",
		RespSchema: ref("DeployResult"), RespStatus: 202, RespDesc: "Pull, build, and atomic release swap (async job).",
	},
	"POST /api/v1/sites/{uid}/git/rollback/{dep}": {
		Summary: "Roll back to a deployment", Tags: []string{"Git"}, Permission: "git.write",
		RespSchema: ref("DeployResult"), RespStatus: 202,
	},

	// ── files ─────────────────────────────────────────────────────────────────
	"GET /api/v1/sites/{uid}/files": {
		Summary: "List a directory", Tags: []string{"Files"}, Permission: "file.read",
		RespSchema: ref("FileListing"),
		RespDesc:   "Query: path=<site-relative dir> (empty lists the site root). Baremetal sites only.",
	},
	"GET /api/v1/sites/{uid}/files/content": {
		Summary: "Download a file", Tags: []string{"Files"}, Permission: "file.read",
		RespDesc: "Streams the raw file bytes (application/octet-stream). Query: path=<site-relative file>. Force-audited.",
	},
	"GET /api/v1/sites/{uid}/files/archive": {
		Summary: "Download a directory as a .zip", Tags: []string{"Files"}, Permission: "file.read",
		RespDesc: "Streams a zip of the directory (application/zip). Query: path=<site-relative directory>; empty means the site root. " +
			"The archive is built server-side and deleted once the response completes, so nothing is left in the site's tree.",
	},
	"PUT /api/v1/sites/{uid}/files/content": {
		Summary: "Write (save/upload) a file", Tags: []string{"Files"}, Permission: "file.write",
		ReqDesc:  "The raw file bytes as the request body; truncates then writes. Query: path=<site-relative file>.",
		RespDesc: "File written; returns the byte count.",
	},
	"DELETE /api/v1/sites/{uid}/files": {
		Summary: "Delete a file or directory", Tags: []string{"Files"}, Permission: "file.write",
		RespDesc: "Query: path=<site-relative path>. Refuses the site root.",
	},
	"POST /api/v1/sites/{uid}/files/mkdir": {
		Summary: "Create a directory", Tags: []string{"Files"}, Permission: "file.write",
		ReqSchema:  object(map[string]any{"path": prop("string", "Site-relative directory to create (with parents).")}, "path"),
		RespStatus: 201, RespDesc: "Directory created.",
	},
	"POST /api/v1/sites/{uid}/files/rename": {
		Summary: "Move or rename a path", Tags: []string{"Files"}, Permission: "file.write",
		ReqSchema: object(map[string]any{
			"from": prop("string", "Site-relative source path."),
			"to":   prop("string", "Site-relative destination path."),
		}, "from", "to"),
		RespDesc: "Path moved.",
	},
	"GET /api/v1/docker/info": {
		Summary: "Docker daemon status", Tags: []string{"Docker"}, Permission: "docker.read",
		RespDesc: "Whether a usable daemon is present and its version. An absent daemon is reported as available:false with the daemon's own reason — not an error, because \"no Docker on this host\" is a state the UI renders.",
	},
	"GET /api/v1/docker/containers": {
		Summary: "List containers", Tags: []string{"Docker"}, Permission: "docker.read",
		RespDesc: "Every container on the host, running or stopped, each flagged `managed` — whether HeroPanel created it. Unmanaged containers are listed (an admin must be able to see what is consuming the host) but cannot be modified. Query: site.",
	},
	"GET /api/v1/docker/containers/{id}": {
		Summary: "Inspect a container", Tags: []string{"Docker"}, Permission: "docker.read",
		RespDesc: "Docker's own inspect payload, passed through unmodelled.",
	},
	"GET /api/v1/docker/containers/{id}/logs": {
		Summary: "Read a container's logs", Tags: []string{"Docker"}, Permission: "docker.read",
		RespDesc: "A bounded tail (max 2000 lines), stdout and stderr separately. Force-audited: container logs routinely carry connection strings and customer data. Query: tail, timestamps.",
	},
	"GET /api/v1/docker/containers/{id}/stats": {
		Summary: "Sample a container's resource usage", Tags: []string{"Docker"}, Permission: "docker.read",
		RespDesc: "One sample, not a stream — the client polls, so a wedged container can never hold a request open.",
	},
	"GET /api/v1/docker/containers/{id}/logs/stream": {
		Summary: "Follow a container's logs live", Tags: []string{"Docker"}, Permission: "docker.read",
		RespDesc: "WebSocket upgrade. Binary frames carry log output as the container writes it; a JSON text frame carries the exit when the follow ends. " +
			"The one-way, live twin of the polled logs read — same `docker.read` grant, same force-audit, because logs carry secrets. Not ownership-gated: reading any container's logs is allowed. Query: tail, timestamps.",
	},
	"POST /api/v1/docker/containers": {
		Summary: "Create a container", Tags: []string{"Docker"}, Permission: "docker.write",
		ReqSchema: object(map[string]any{
			"name":      prop("string", "Container name."),
			"image":     prop("string", "Image reference."),
			"site":      prop("string", "Optional site uid to attribute the container to."),
			"env":       prop("object", "Environment variables. Sent to docker through stdin as an env-file, never as arguments — argv is world-readable via /proc, and this is where a generated password lives."),
			"ports":     prop("array", "Published ports as {host, container, proto}. Always bound to 127.0.0.1: docker's firewall rules are evaluated before the host's, so publishing on all interfaces would expose the container even on a host whose firewall denies the port."),
			"volumes":   prop("array", "Mounts as {volume, path, read_only}. **Named volumes only** — a host path is not rejected but unrepresentable, which is what prevents `-v /:/host` and mounting the docker socket."),
			"restart":   prop("string", "no | on-failure | unless-stopped (default) | always."),
			"network":   prop("string", "Optional network name."),
			"memory_mb": prop("integer", "Memory limit in MB (16 … 1048576)."),
			"command":   prop("array", "Optional command. Everything after the image operand is the container's own argv, so a leading dash here is the program's flag, not docker's."),
		}, "name", "image"),
		RespDesc: "The container is created with HeroPanel's managed label and `no-new-privileges`. `--privileged`, `--cap-add`, `--device`, `--userns` and host namespaces have no corresponding field and cannot be produced.",
	},
	"POST /api/v1/docker/compose": {
		Summary: "Bring a compose stack up", Tags: []string{"Docker"}, Permission: "docker.write",
		ReqSchema: object(map[string]any{
			"project": prop("string", "Compose project name."),
			"site":    prop("string", "Optional site uid to attribute the stack to."),
			"file":    prop("string", "The compose file itself. Passed to docker on stdin, never a path."),
		}, "project", "file"),
		RespDesc: "The compose file is user-authored YAML and can request anything docker compose understands, so this is the module's explicit escape hatch: the panel labels and scopes the stack but does not harden arbitrary compose the way it hardens a container it builds. Every container the stack creates carries the managed label, so tear-down and the ownership boundary apply to it.",
	},
	"GET /api/v1/docker/compose/{project}": {
		Summary: "List a stack's services", Tags: []string{"Docker"}, Permission: "docker.read",
		RespDesc: "The services in a compose project and their state.",
	},
	"GET /api/v1/docker/compose/{project}/logs": {
		Summary: "Read a stack's logs", Tags: []string{"Docker"}, Permission: "docker.read",
		RespDesc: "A bounded tail of the whole stack's output. Force-audited. Query: tail.",
	},
	"DELETE /api/v1/docker/compose/{project}": {
		Summary: "Tear a stack down", Tags: []string{"Docker"}, Permission: "docker.write",
		RespDesc: "Removes the stack's containers and networks but never its volumes. Refused with 403 for a stack HeroPanel did not create.",
	},
	"GET /api/v1/apps/templates": {
		Summary: "The one-click app catalog", Tags: []string{"Apps"}, Permission: "docker.read",
		RespDesc: "Every deployable template, each with a memory-feasibility verdict against the host's available memory, so an app the host cannot run is marked before it is chosen.",
	},
	"POST /api/v1/apps": {
		Summary: "Deploy an app", Tags: []string{"Apps"}, Permission: "docker.write",
		ReqSchema: object(map[string]any{
			"slug":   prop("string", "Template slug, e.g. ghost."),
			"name":   prop("string", "The app/stack name."),
			"site":   prop("string", "Optional site uid to attribute it to."),
			"values": prop("object", "Operator-supplied field values. Secret fields are ignored here and generated instead."),
		}, "slug", "name"),
		RespDesc: "Generates any secret fields (never taken from input), checks memory feasibility, renders the template's compose file and brings it up. Returns the generated secrets **once** — they are not stored in a form the panel can return later, and are deliberately not written to the audit log.",
	},
	"GET /api/v1/apps/{project}": {
		Summary: "An app's status", Tags: []string{"Apps"}, Permission: "docker.read",
		RespDesc: "The app's running services.",
	},
	"GET /api/v1/apps/{project}/logs": {
		Summary: "An app's logs", Tags: []string{"Apps"}, Permission: "docker.read",
		RespDesc: "A bounded tail of the app's combined logs. Force-audited: an app's logs carry its secrets and its users' data.",
	},
	"DELETE /api/v1/apps/{project}": {
		Summary: "Remove an app", Tags: []string{"Apps"}, Permission: "docker.write",
		RespDesc: "Tears the app's stack down. Its volumes survive, so a redeploy reattaches the data.",
	},
	"GET /api/v1/apps/{project}/exposure": {
		Summary: "An app's exposure", Tags: []string{"Apps"}, Permission: "docker.read",
		RespDesc: "Whether the app is fronted by a domain, and if so which — plus the proxy site's uid and status. `{exposed:false}` when it is only reachable on loopback.",
	},
	"POST /api/v1/apps/{project}/expose": {
		Summary: "Expose an app on a domain", Tags: []string{"Apps"}, Permission: "site.write",
		ReqSchema: object(map[string]any{
			"domain": prop("string", "The domain to serve the app on, e.g. blog.example.com."),
		}, "domain"),
		RespDesc: "Creates a proxy **site** whose vhost reverse-proxies to the app's live loopback port, resolved at render time so it follows a redeploy. `site.write`, not docker.write: it stands up a real site, with the domain/TLS/suspend controls every site has. The app keeps running on loopback; this is its front door. Refused with 409 if the app is already exposed, 404 if it is not deployed.",
	},
	"DELETE /api/v1/apps/{project}/expose": {
		Summary: "Unexpose an app", Tags: []string{"Apps"}, Permission: "site.write",
		RespDesc: "Deletes the proxy site fronting the app, dropping its vhost. The app itself is left running on loopback — this takes down the front door, not the app.",
	},
	"GET /api/v1/docker/containers/{id}/exec": {
		Summary: "Open a shell inside a container", Tags: []string{"Docker"}, Permission: "docker.write",
		RespDesc: "WebSocket upgrade. Binary frames carry terminal bytes in both directions; JSON text frames carry resize/exit. " +
			"`docker.write` rather than `docker.read`: a shell inside a container can stop the process, read its secrets and edit its data. " +
			"Refused with 403 unless the container carries HeroPanel's managed label — a shell in someone else's container would bypass every other refusal in this module. " +
			"Query: shell (/bin/sh, /bin/bash, /bin/ash), cols, rows.",
	},
	"GET /api/v1/docker/volumes": {
		Summary: "List volumes", Tags: []string{"Docker"}, Permission: "docker.read",
		RespDesc: "Every volume on the host, each flagged `managed`.",
	},
	"GET /api/v1/docker/volumes/{name}": {
		Summary: "Inspect a volume", Tags: []string{"Docker"}, Permission: "docker.read",
		RespDesc: "The volume's full record plus the containers that mount it — including unmanaged ones, so the destructive remove is an informed decision. Read-only, so not ownership-guarded.",
	},
	"POST /api/v1/docker/volumes": {
		Summary: "Create a volume", Tags: []string{"Docker"}, Permission: "docker.write",
		ReqSchema: object(map[string]any{
			"name": prop("string", "Volume name."),
			"site": prop("string", "Optional site uid to attribute it to."),
		}, "name"),
		RespDesc: "Creates a named volume carrying HeroPanel's managed label.",
	},
	"DELETE /api/v1/docker/volumes/{name}": {
		Summary: "Remove a volume", Tags: []string{"Docker"}, Permission: "docker.write",
		RespDesc: "Deletes the volume **and its contents**. Refused with 403 for volumes HeroPanel did not create — this is the one operation in the module that destroys data, and an unmanaged volume usually belongs to a database.",
	},
	"GET /api/v1/docker/networks": {
		Summary: "List networks", Tags: []string{"Docker"}, Permission: "docker.read",
		RespDesc: "Every network on the host, each flagged `managed`.",
	},
	"GET /api/v1/docker/networks/{name}": {
		Summary: "Inspect a network", Tags: []string{"Docker"}, Permission: "docker.read",
		RespDesc: "The network's full record, including the containers connected to it (docker's inspect payload carries them). Read-only, so not ownership-guarded.",
	},
	"POST /api/v1/docker/networks": {
		Summary: "Create a network", Tags: []string{"Docker"}, Permission: "docker.write",
		ReqSchema: object(map[string]any{
			"name": prop("string", "Network name."),
			"site": prop("string", "Optional site uid to attribute it to."),
		}, "name"),
		RespDesc: "Always a bridge network: a container on the host network shares the host's stack outright, discarding the isolation that made containerising it worthwhile.",
	},
	"DELETE /api/v1/docker/networks/{name}": {
		Summary: "Remove a network", Tags: []string{"Docker"}, Permission: "docker.write",
		RespDesc: "Managed networks only.",
	},
	"GET /api/v1/docker/stats": {
		Summary: "Sample every container's resource usage", Tags: []string{"Docker"}, Permission: "docker.read",
		RespDesc: "One sample for all containers. Same measurement as the per-container route, so the two views cannot disagree.",
	},
	"POST /api/v1/docker/containers/{id}/start": {
		Summary: "Start a container", Tags: []string{"Docker"}, Permission: "docker.write",
		RespDesc: "Refused with 403 unless the container carries HeroPanel's managed label, enforced in the broker.",
	},
	"POST /api/v1/docker/containers/{id}/stop": {
		Summary: "Stop a container", Tags: []string{"Docker"}, Permission: "docker.write",
		RespDesc: "Sends SIGTERM with a 30s grace before SIGKILL. Managed containers only.",
	},
	"POST /api/v1/docker/containers/{id}/restart": {
		Summary: "Restart a container", Tags: []string{"Docker"}, Permission: "docker.write",
		RespDesc: "Managed containers only.",
	},
	"DELETE /api/v1/docker/containers/{id}": {
		Summary: "Remove a container", Tags: []string{"Docker"}, Permission: "docker.write",
		RespDesc: "Managed containers only. Never removes the container's volumes: deleting a container is routine, deleting its data is a separate and explicit act. Query: force.",
	},
	"GET /api/v1/monitor/node": {
		Summary: "Sample node health", Tags: []string{"Monitor"}, Permission: "monitor.read",
		RespDesc: "One snapshot of the host: CPU %, load, memory/swap, uptime and per-filesystem disk usage. A one-shot read for the initial paint — the live dashboard is pushed over the `monitor:node` WebSocket channel (subscription-gated, so an unwatched panel samples nothing) rather than polling this.",
	},
	"GET /api/v1/monitor/sites": {
		Summary: "Sample per-site usage", Tags: []string{"Monitor"}, Permission: "monitor.read",
		RespDesc: "Each site's live memory, CPU % and task count, read from its cgroup v2 accounting (the slice every site runs in, with accounting on since it was created). A site whose slice has no cgroup yet reports `present:false`. Live view: the `monitor:sites` channel.",
	},
	"GET /api/v1/monitor/services": {
		Summary: "Service health", Tags: []string{"Monitor"}, Permission: "monitor.read",
		RespDesc: "Whether the services the host depends on (web server, database, cache) are active, read through the broker's service.status capability. Live view: the `monitor:services` channel.",
	},
	"GET /api/v1/monitor/history": {
		Summary: "Node metrics history", Tags: []string{"Monitor"}, Permission: "monitor.read",
		RespDesc: "Node CPU / memory / load / disk over a bounded range (query range=1h|6h|24h|7d|30d, default 24h). A raw sample a minute is kept ~48h and folded into hourly averages kept ~30d, so the service returns raw within the window and hourly beyond it. Mounted only when a datastore is present.",
	},
	"GET /api/v1/monitor/alerts/rules": {
		Summary: "List alert rules", Tags: []string{"Monitor"}, Permission: "monitor.read",
		RespDesc: "The configured threshold rules. Notification targets are **never** returned — they are write-only, and a Telegram token is sealed at rest.",
	},
	"POST /api/v1/monitor/alerts/rules": {
		Summary: "Create an alert rule", Tags: []string{"Monitor"}, Permission: "monitor.write",
		ReqSchema: object(map[string]any{
			"name":          prop("string", "Rule name."),
			"metric":        prop("string", "cpu | mem | swap | load1 | disk_root."),
			"op":            prop("string", "gt | lt (default gt)."),
			"threshold":     prop("number", "The value to compare against."),
			"for_sec":       prop("integer", "Seconds the breach must persist before firing (0 = immediate)."),
			"notify_kind":   prop("string", "log | webhook | telegram."),
			"notify_target": prop("object", "Webhook URL, or Telegram bot token + chat id. Sealed at rest; requires a data key for the non-log kinds."),
		}, "name", "metric", "threshold"),
		RespDesc: "Creates a rule. It fires only after the breach has persisted for for_sec, so a one-tick spike pages nobody. webhook/telegram targets are sealed with the panel's data key (refused with 503 when none is configured).",
	},
	"PUT /api/v1/monitor/alerts/rules/{uid}": {
		Summary: "Enable or disable a rule", Tags: []string{"Monitor"}, Permission: "monitor.write",
		ReqSchema: object(map[string]any{"enabled": prop("boolean", "Whether the rule is active.")}, "enabled"),
		RespDesc:  "Toggles a rule without deleting it.",
	},
	"DELETE /api/v1/monitor/alerts/rules/{uid}": {
		Summary: "Delete an alert rule", Tags: []string{"Monitor"}, Permission: "monitor.write",
		RespDesc: "Removes a rule. Its past events are kept as history.",
	},
	"GET /api/v1/monitor/alerts/events": {
		Summary: "Recent alert events", Tags: []string{"Monitor"}, Permission: "monitor.read",
		RespDesc: "Recent firings and resolutions, newest first. Query: limit (default 100, max 500).",
	},
	"GET /api/v1/docker/images": {
		Summary: "List images", Tags: []string{"Docker"}, Permission: "docker.read",
		RespDesc: "Images present on the host.",
	},
	"POST /api/v1/docker/images/pull": {
		Summary: "Pull an image", Tags: []string{"Docker"}, Permission: "docker.write",
		ReqSchema: object(map[string]any{
			"image": prop("string", "Image reference, e.g. ghost:5-alpine."),
		}, "image"),
		RespDesc: "Fetches the image. A write, not a read: it places someone else's code on the host and consumes disk. Can take minutes.",
	},
	"POST /api/v1/docker/images/prune": {
		Summary: "Prune unused images", Tags: []string{"Docker"}, Permission: "docker.write",
		RespDesc: "Reclaims disk from images no container needs. Dangling (untagged) layers only by default; query all=true also removes every image no container uses. Docker's own in-use check still protects anything running or stopped.",
	},
	"DELETE /api/v1/docker/images/{ref}": {
		Summary: "Remove an image", Tags: []string{"Docker"}, Permission: "docker.write",
		RespDesc: "Deletes an image by id or reference. Images carry no managed label, so ownership is not checked here — instead docker's refusal to remove an image a container still uses is passed straight through. Query: force (detaches extra tags only; does not override the in-use check).",
	},
	"GET /api/v1/sites/{uid}/terminal/recordings": {
		Summary: "List a site's recorded terminal sessions", Tags: []string{"Terminal"},
		Permission: "terminal.recordings.read",
		RespDesc:   "Recordings for this site, newest first. Query: limit, offset.",
	},
	"GET /api/v1/terminal/recordings": {
		Summary: "List recorded terminal sessions", Tags: []string{"Terminal"},
		Permission: "terminal.recordings.read",
		RespDesc:   "All recordings, newest first. Query: limit, offset.",
	},
	"GET /api/v1/terminal/recordings/{rid}": {
		Summary: "Get a recording's metadata", Tags: []string{"Terminal"},
		Permission: "terminal.recordings.read",
		RespDesc:   "Who opened the session, as which Linux user, how long it ran, and whether the recording is complete.",
	},
	"GET /api/v1/terminal/recordings/{rid}/cast": {
		Summary: "Download a recording", Tags: []string{"Terminal"},
		Permission: "terminal.recordings.read",
		RespDesc: "Streams the session as asciicast v2 (application/x-asciicast). Force-audited — this is the route that hands over the transcript. " +
			"Input typed while the terminal had echo disabled (a password prompt) was redacted before it was ever written.",
	},
	"DELETE /api/v1/terminal/recordings/{rid}": {
		Summary: "Delete a recording", Tags: []string{"Terminal"},
		Permission: "terminal.recordings.delete",
		RespDesc:   "Removes the row and its file. Separate from the read permission on purpose: destroying an audit artifact is grantable to fewer people than viewing one.",
	},
	"POST /api/v1/sites/{uid}/files/copy": {
		Summary: "Copy a path", Tags: []string{"Files"}, Permission: "file.write",
		ReqSchema: object(map[string]any{
			"from":        prop("string", "Site-relative source path."),
			"to":          prop("string", "Site-relative destination path."),
			"on_conflict": prop("string", "\"fail\" (default) or \"rename\" to land beside an existing entry."),
		}, "from", "to"),
		RespDesc: "Path copied; the response echoes the destination actually used.",
	},
	"POST /api/v1/sites/{uid}/files/move": {
		Summary: "Move a path", Tags: []string{"Files"}, Permission: "file.write",
		ReqSchema: object(map[string]any{
			"from":        prop("string", "Site-relative source path."),
			"to":          prop("string", "Site-relative destination path."),
			"on_conflict": prop("string", "\"fail\" (default) or \"rename\" to land beside an existing entry."),
		}, "from", "to"),
		RespDesc: "Path moved; the response echoes the destination actually used.",
	},
	"POST /api/v1/sites/{uid}/files/chmod": {
		Summary: "Change a path's mode", Tags: []string{"Files"}, Permission: "file.write",
		ReqSchema: object(map[string]any{
			"path": prop("string", "Site-relative path."),
			"mode": prop("string", "Octal mode, 3–4 digits, e.g. \"644\"."),
		}, "path", "mode"),
		RespDesc: "Mode changed.",
	},
	"POST /api/v1/sites/{uid}/files/extract": {
		Summary: "Extract an archive", Tags: []string{"Files"}, Permission: "file.write",
		ReqSchema: object(map[string]any{
			"archive": prop("string", "Site-relative .zip or .tar[.gz|.bz2|.xz] archive."),
			"dest":    prop("string", "Site-relative destination directory (created if absent)."),
		}, "archive", "dest"),
		RespDesc: "Archive extracted.",
	},

	"POST /api/v1/sites/{uid}/files/compress": {
		Summary: "Create an archive", Tags: []string{"Files"}, Permission: "file.write",
		ReqSchema: object(map[string]any{
			"sources": arrayOf(prop("string", "Site-relative entries, all from the same folder.")),
			"archive": prop("string", "Site-relative destination, e.g. assets/backup.zip."),
			"format":  map[string]any{"type": "string", "enum": []any{"zip", "tar.gz"}, "description": "Defaults to the archive's suffix."},
		}, "sources", "archive"),
		RespStatus: 201, RespDesc: "Archive created.",
	},
	"POST /api/v1/sites/{uid}/files/chown": {
		Summary: "Repair ownership", Tags: []string{"Files"}, Permission: "file.write",
		ReqSchema: object(map[string]any{"path": prop("string", "Site-relative path; applied recursively.")}, "path"),
		RespDesc:  "Ownership reset to the site's Linux user. The target account cannot be chosen — it is always the site's own user.",
	},
	"GET /api/v1/sites/{uid}/files/search": {
		Summary: "Search the file tree", Tags: []string{"Files"}, Permission: "file.read",
		RespSchema: ref("FileSearch"),
		RespDesc:   "Query: q=<term>, path=<site-relative subtree>, mode=name|content. Results are capped; `truncated` says whether they were.",
	},

	// ── terminal ──────────────────────────────────────────────────────────────
	"GET /api/v1/sites/{uid}/terminal": {
		Summary: "Open an interactive terminal (WebSocket)", Tags: []string{"Terminal"}, Permission: "terminal.use",
		RespStatus: 101,
		RespDesc: "Upgrades to a WebSocket carrying a PTY session run as the site's Linux user. " +
			"Query: cwd (site-relative, clamped), cols, rows. Terminal bytes travel as binary frames in both " +
			"directions; JSON text frames carry control messages ({type:\"resize\",cols,rows} in, " +
			"{type:\"exit\",exit_code} / {type:\"error\",message} out). Force-audited: the session records the " +
			"Linux account it ran as.",
	},

	// ── jobs ──────────────────────────────────────────────────────────────────
	"GET /api/v1/jobs": {
		Summary: "List jobs", Tags: []string{"Jobs"}, RespSchema: arrayOf(ref("Job")),
	},
	"GET /api/v1/jobs/{id}": {
		Summary: "Get a job", Tags: []string{"Jobs"}, RespSchema: ref("Job"),
	},

	// ── dns ───────────────────────────────────────────────────────────────────
	"GET /api/v1/dns/zones": {
		Summary: "List zones", Tags: []string{"DNS"}, Permission: "dns.read", RespSchema: arrayOf(ref("DNSZone")),
	},
	"POST /api/v1/dns/zones": {
		Summary: "Create a zone", Tags: []string{"DNS"}, Permission: "dns.write",
		ReqSchema: object(map[string]any{
			"name":        prop("string", ""),
			"primary_ns":  prop("string", ""),
			"admin_email": prop("string", ""),
			"ns_ip":       prop("string", "Glue A record for the primary NS."),
		}, "name"),
		RespSchema: ref("DNSZone"), RespStatus: 201,
	},
	"GET /api/v1/dns/zones/{uid}": {
		Summary: "Get a zone", Tags: []string{"DNS"}, Permission: "dns.read", RespSchema: ref("DNSZone"),
	},
	"DELETE /api/v1/dns/zones/{uid}": {
		Summary: "Delete a zone", Tags: []string{"DNS"}, Permission: "dns.write", RespDesc: "Zone deleted.",
	},
	"GET /api/v1/dns/zones/{uid}/records": {
		Summary: "List records", Tags: []string{"DNS"}, Permission: "dns.read", RespSchema: arrayOf(ref("DNSRecord")),
	},
	"POST /api/v1/dns/zones/{uid}/records": {
		Summary: "Add a record", Tags: []string{"DNS"}, Permission: "dns.write",
		ReqSchema: object(map[string]any{
			"name":     prop("string", ""),
			"type":     map[string]any{"type": "string", "enum": []any{"A", "AAAA", "CNAME", "MX", "TXT", "NS", "SRV", "CAA"}},
			"content":  prop("string", ""),
			"ttl":      prop("integer", ""),
			"priority": prop("integer", ""),
		}, "name", "type", "content"),
		RespSchema: ref("DNSRecord"), RespStatus: 201,
	},
	"DELETE /api/v1/dns/records/{uid}": {
		Summary: "Delete a record", Tags: []string{"DNS"}, Permission: "dns.write", RespDesc: "Record deleted.",
	},
}
