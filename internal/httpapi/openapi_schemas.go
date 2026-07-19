package httpapi

// Component schemas and tag groups for the OpenAPI document (see openapi.go).
// These mirror the JSON the handlers emit and the TypeScript interfaces in
// web/src/lib/api.ts; they are the "annotations" half of the DoD line, kept in
// one place so the shapes are reviewable as a unit.

var openapiTags = []any{
	map[string]any{"name": "Auth", "description": "Bootstrap, login, MFA, session, and the current principal."},
	map[string]any{"name": "Account", "description": "Per-user API keys for programmatic access."},
	map[string]any{"name": "Users", "description": "User directory (read)."},
	map[string]any{"name": "Sites", "description": "Site lifecycle: create, inspect, suspend/resume, clone, delete."},
	map[string]any{"name": "PHP", "description": "Per-site PHP settings and server-scope extension toggles."},
	map[string]any{"name": "Domains", "description": "Aliases, redirects, and force-HTTPS for a site."},
	map[string]any{"name": "Runtime", "description": "Supervised app process for proxy sites (Node/Python/Go/generic)."},
	map[string]any{"name": "Git", "description": "Git source, deployments, rollback, and the push webhook."},
	map[string]any{"name": "Databases", "description": "Databases, database users, grants, dumps, and Adminer hand-off."},
	map[string]any{"name": "DNS", "description": "Authoritative zones and records."},
	map[string]any{"name": "SSL", "description": "Certificates: Let's Encrypt, self-signed, and custom upload."},
	map[string]any{"name": "Audit", "description": "Hash-chained audit log and chain verification."},
	map[string]any{"name": "Modules", "description": "Installed modules and the capability set the UI gates on."},
	map[string]any{"name": "Jobs", "description": "Asynchronous job status."},
	map[string]any{"name": "System", "description": "Instance info, health, and the OpenAPI document."},
}

var openapiSchemas = map[string]any{
	// ── envelopes ────────────────────────────────────────────────────────────
	"Meta": object(map[string]any{
		"request_id": prop("string", "Correlates the response with server logs."),
		"ts":         map[string]any{"type": "string", "format": "date-time"},
	}, "request_id", "ts"),
	"ErrorEnvelope": object(map[string]any{
		"error": ref("Error"),
	}, "error"),
	"Error": object(map[string]any{
		"code":       prop("string", "Stable machine-readable error code."),
		"message":    prop("string", "Human-readable message."),
		"request_id": prop("string", ""),
		"fields": arrayOf(object(map[string]any{
			"field":   prop("string", ""),
			"message": prop("string", ""),
		})),
	}, "code", "message"),

	// ── auth ─────────────────────────────────────────────────────────────────
	"Principal": object(map[string]any{
		"uid":         prop("string", ""),
		"email":       prop("string", ""),
		"username":    prop("string", ""),
		"kind":        map[string]any{"type": "string", "enum": []any{"user", "apikey"}},
		"roles":       arrayOf(prop("string", "")),
		"permissions": arrayOf(prop("string", "")),
	}),
	"AuthStatus": object(map[string]any{
		"needs_bootstrap": prop("boolean", "True on a fresh install with no admin yet."),
		"authenticated":   prop("boolean", ""),
	}),
	"LoginResult": object(map[string]any{
		"authenticated": prop("boolean", ""),
		"mfa_required":  prop("boolean", "When true, complete with POST /auth/mfa."),
		"mfa_token":     prop("string", "Short-lived token for the MFA step."),
	}),
	"MFASetup": object(map[string]any{
		"secret":         prop("string", "TOTP shared secret."),
		"otpauth_url":    prop("string", "otpauth:// URI for QR rendering."),
		"recovery_codes": arrayOf(prop("string", "")),
	}),
	"APIKey": object(map[string]any{
		"uid":        prop("string", ""),
		"name":       prop("string", ""),
		"scopes":     arrayOf(prop("string", "")),
		"created_at": map[string]any{"type": "string", "format": "date-time"},
		"last_used":  map[string]any{"type": "string", "format": "date-time"},
		"secret":     prop("string", "The plaintext key — returned only once, at creation."),
	}),

	// ── sites ────────────────────────────────────────────────────────────────
	"Site": object(map[string]any{
		"uid":            prop("string", ""),
		"name":           prop("string", ""),
		"primary_domain": prop("string", ""),
		"type":           map[string]any{"type": "string", "enum": []any{"php", "static", "proxy"}},
		"deploy_mode":    map[string]any{"type": "string", "enum": []any{"managed", "git"}},
		"status":         map[string]any{"type": "string", "enum": []any{"active", "suspended", "provisioning", "error"}},
		"system_user":    prop("string", "The dedicated non-root user the site runs as."),
		"php_version":    prop("string", ""),
		"created_at":     map[string]any{"type": "string", "format": "date-time"},
	}),
	"SiteLimits": object(map[string]any{
		"cpu_quota_pct":   prop("integer", "CPU quota as a percentage of one core (cgroup)."),
		"mem_limit_bytes": prop("integer", "Memory hard limit in bytes (cgroup)."),
		"pids_max":        prop("integer", "Maximum process count (cgroup)."),
	}),
	"SiteLog": object(map[string]any{
		"kind":   map[string]any{"type": "string", "enum": []any{"access", "error"}},
		"exists": prop("boolean", "False when the log file has not been created yet."),
		"lines":  arrayOf(prop("string", "")),
	}),

	// ── php ──────────────────────────────────────────────────────────────────
	"PHPInfo": object(map[string]any{
		"version":         prop("string", ""),
		"memory_limit_mb": prop("integer", ""),
		"fpm":             ref("FPM"),
		"opcache":         ref("OPcache"),
		"ini":             map[string]any{"type": "object", "additionalProperties": map[string]any{"type": "string"}, "description": "Applied php.ini overrides (allowlisted keys only)."},
		"allowed_ini":     arrayOf(prop("string", "The php.ini keys the editor permits.")),
	}),
	"FPM": object(map[string]any{
		"pm":                     map[string]any{"type": "string", "enum": []any{"dynamic", "static", "ondemand"}},
		"max_children":           prop("integer", ""),
		"start_servers":          prop("integer", ""),
		"min_spare_servers":      prop("integer", ""),
		"max_spare_servers":      prop("integer", ""),
		"max_requests":           prop("integer", ""),
		"process_idle_timeout_s": prop("integer", ""),
	}),
	"OPcache": object(map[string]any{
		"enabled":               prop("boolean", ""),
		"memory_mb":             prop("integer", ""),
		"max_accelerated_files": prop("integer", ""),
		"jit":                   prop("string", "JIT mode, e.g. tracing/off."),
	}),
	"PHPExtensions": object(map[string]any{
		"version":    prop("string", ""),
		"scope_note": prop("string", "Warns that toggling restarts FPM for every site on this version."),
		"extensions": arrayOf(object(map[string]any{
			"name":    prop("string", ""),
			"enabled": prop("boolean", ""),
		})),
	}),

	// ── domains ──────────────────────────────────────────────────────────────
	"Domain": object(map[string]any{
		"uid":           prop("string", ""),
		"fqdn":          prop("string", ""),
		"kind":          map[string]any{"type": "string", "enum": []any{"primary", "alias", "redirect"}},
		"redirect_to":   prop("string", ""),
		"redirect_code": prop("integer", ""),
	}),

	// ── runtime ──────────────────────────────────────────────────────────────
	"Runtime": object(map[string]any{
		"runtime":     map[string]any{"type": "string", "enum": []any{"node", "python", "go", "generic"}},
		"command":     prop("string", "Start command, run as the site user in current/."),
		"port":        prop("integer", "Local port the app listens on."),
		"env":         map[string]any{"type": "object", "additionalProperties": map[string]any{"type": "string"}},
		"health_path": prop("string", "Optional; makes 'running' mean the app answers."),
		"status":      map[string]any{"type": "string", "enum": []any{"running", "stopped", "errored"}},
	}),
	"RuntimeHealth": object(map[string]any{
		"configured":  prop("boolean", ""),
		"healthy":     prop("boolean", ""),
		"status_code": prop("integer", ""),
		"error":       prop("string", ""),
	}),

	// ── git ──────────────────────────────────────────────────────────────────
	"GitSource": object(map[string]any{
		"repo_url":      prop("string", ""),
		"branch":        prop("string", ""),
		"build_command": prop("string", ""),
		"web_root":      prop("string", ""),
		"auth_kind":     map[string]any{"type": "string", "enum": []any{"none", "token", "ssh_key"}},
		"auth_username": prop("string", ""),
		"auto_composer": prop("boolean", ""),
		"public_key":    prop("string", "The generated ed25519 deploy key's public half (ssh_key auth)."),
		"host_key":      prop("string", "Pinned SSH host key(s) (known_hosts format); when set, clones use strict host-key checking. ssh_key auth."),
		"webhook_url":   prop("string", "Push webhook URL to register on the repository."),
	}),
	"Deployment": object(map[string]any{
		"uid":        prop("string", ""),
		"status":     map[string]any{"type": "string", "enum": []any{"pending", "running", "succeeded", "failed"}},
		"trigger":    map[string]any{"type": "string", "enum": []any{"manual", "webhook"}},
		"commit_sha": prop("string", ""),
		"created_at": map[string]any{"type": "string", "format": "date-time"},
	}),
	"DeployResult": object(map[string]any{
		"uid":    prop("string", ""),
		"status": prop("string", ""),
		"job":    ref("Job"),
	}),

	// ── databases ────────────────────────────────────────────────────────────
	"Database": object(map[string]any{
		"uid":        prop("string", ""),
		"name":       prop("string", ""),
		"charset":    prop("string", ""),
		"created_at": map[string]any{"type": "string", "format": "date-time"},
	}),
	"DatabaseUser": object(map[string]any{
		"uid":      prop("string", ""),
		"username": prop("string", ""),
		"host":     prop("string", ""),
	}),
	"AdminerSSO": object(map[string]any{
		"url":      prop("string", "Adminer endpoint to POST the throwaway credential to."),
		"server":   prop("string", ""),
		"username": prop("string", "Throwaway account, auto-expired."),
		"password": prop("string", ""),
		"db":       prop("string", ""),
	}),
	"DatabaseSize": object(map[string]any{
		"bytes": prop("integer", ""),
	}),

	// ── dns ──────────────────────────────────────────────────────────────────
	"DNSZone": object(map[string]any{
		"uid":         prop("string", ""),
		"name":        prop("string", ""),
		"primary_ns":  prop("string", ""),
		"admin_email": prop("string", ""),
		"serial":      prop("integer", "SOA serial, bumped on every change."),
		"ttl":         prop("integer", ""),
		"status":      map[string]any{"type": "string", "enum": []any{"active", "pending", "error"}},
	}),
	"DNSRecord": object(map[string]any{
		"uid":      prop("string", ""),
		"name":     prop("string", ""),
		"type":     map[string]any{"type": "string", "enum": []any{"A", "AAAA", "CNAME", "MX", "TXT", "NS", "SRV", "CAA"}},
		"content":  prop("string", ""),
		"ttl":      prop("integer", ""),
		"priority": prop("integer", "MX/SRV priority."),
	}),

	// ── ssl ──────────────────────────────────────────────────────────────────
	"Certificate": object(map[string]any{
		"uid":        prop("string", ""),
		"domain":     prop("string", ""),
		"issuer":     map[string]any{"type": "string", "enum": []any{"letsencrypt", "self_signed", "custom"}},
		"status":     map[string]any{"type": "string", "enum": []any{"valid", "expiring", "expired", "pending", "error"}},
		"not_after":  map[string]any{"type": "string", "format": "date-time"},
		"auto_renew": prop("boolean", ""),
	}),

	// ── audit ────────────────────────────────────────────────────────────────
	"AuditEntry": object(map[string]any{
		"id":         prop("integer", ""),
		"created_at": map[string]any{"type": "string", "format": "date-time"},
		"actor_kind": map[string]any{"type": "string", "enum": []any{"user", "apikey", "anonymous", "system"}},
		"actor":      prop("string", ""),
		"action":     prop("string", "Route pattern of the audited request."),
		"resource":   prop("string", ""),
		"outcome":    map[string]any{"type": "string", "enum": []any{"success", "failure", "denied"}},
		"detail":     map[string]any{"type": "object", "description": "Canonical JSON detail; part of the hash chain."},
		"row_hash":   prop("string", "SHA-256 of prev_hash || canonical(row)."),
	}),
	"AuditVerify": object(map[string]any{
		"intact":       prop("boolean", "False if the hash chain has been broken."),
		"checked":      prop("integer", "Number of entries verified."),
		"broken_at_id": prop("integer", "First tampered entry, when intact is false."),
	}),

	// ── modules ──────────────────────────────────────────────────────────────
	"Capabilities": object(map[string]any{
		"capabilities": arrayOf(prop("string", "Flat capability set, e.g. site.manage.")),
	}),
	"Modules": object(map[string]any{
		"modules": arrayOf(ref("ModuleInfo")),
	}),
	"ModuleInfo": object(map[string]any{
		"slug":         prop("string", ""),
		"name":         prop("string", ""),
		"state":        map[string]any{"type": "string", "enum": []any{"running", "stopped", "errored"}},
		"capabilities": arrayOf(prop("string", "")),
	}),

	// ── jobs / system ────────────────────────────────────────────────────────
	"Job": object(map[string]any{
		"id":       prop("string", ""),
		"kind":     prop("string", ""),
		"status":   map[string]any{"type": "string", "enum": []any{"queued", "running", "succeeded", "failed"}},
		"progress": prop("integer", "0–100."),
		"message":  prop("string", ""),
		"error":    prop("string", ""),
	}),
	"SystemInfo": object(map[string]any{
		"version":  prop("string", ""),
		"uptime_s": prop("integer", ""),
	}),
	"Health": object(map[string]any{
		"status": prop("string", "\"ok\" when serving."),
	}),
}
