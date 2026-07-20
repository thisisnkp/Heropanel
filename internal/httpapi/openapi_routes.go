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
