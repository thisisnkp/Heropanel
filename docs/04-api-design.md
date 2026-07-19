# 04 — API Design

Three surfaces: **REST** (operator/UI/automation), **WebSocket** (realtime), and **internal gRPC** (broker + modules). REST + WS are public; gRPC is loopback/Unix-socket only.

## 1. Principles
- **Resource-oriented, versioned:** base path `/api/v1`. Breaking changes → `/api/v2`; additive changes are backward-compatible.
- **JSON only** over HTTPS; `Content-Type: application/json`, UTF-8.
- **ULIDs, never DB ids** in URLs and bodies (`sit_01J...`, `dom_01J...` — prefix-typed ULIDs for readability).
- **Consistent envelope**, consistent errors, consistent pagination.
- **Idempotency** on unsafe creates via `Idempotency-Key` header.
- **Async by default** for anything that touches the OS: return `202` + a Job; stream progress over WS.
- **OpenAPI 3.1** is served at `/api/v1/openapi.json` (unauthenticated — a client needs it to learn how to authenticate). It is **not** a hand-maintained file: [`internal/httpapi/openapi.go`](../internal/httpapi/openapi.go) walks the *live* Chi routing tree, so every path and method in the document is one that is actually mounted, and enriches each operation from the metadata table in [`openapi_routes.go`](../internal/httpapi/openapi_routes.go) (summary, tags, the `requirePermission` scope as `x-required-permission`, request/response schemas). A route that is mounted but undocumented fails the drift test (`TestOpenAPINoUndocumentedRoutes`), which is what keeps the spec honest as the surface grows. The generated document is committed at [`docs/openapi.json`](openapi.json) and regenerated with `HP_UPDATE_OPENAPI=1 go test ./internal/httpapi -run Golden`.
  - A dependency-free viewer is served at `/api/docs` ([`docs_assets.go`](../internal/httpapi/docs_assets.go)): it fetches the spec client-side and renders a grouped, filterable, collapsible reference. It ships as same-origin CSS/JS (not inlined) so the strict `default-src 'self'` CSP needs no exception.

## 2. Response Envelope

Success:
```json
{
  "data": { "id": "sit_01J9...", "name": "acme", "...": "..." },
  "meta": { "request_id": "req_01J9...", "ts": "2026-07-10T12:00:00Z" }
}
```
Collection:
```json
{
  "data": [ { "...": "..." } ],
  "meta": {
    "request_id": "req_...",
    "pagination": { "cursor_next": "eyJ...", "cursor_prev": null, "limit": 50, "total_estimate": 1240 }
  }
}
```
Async accepted (`202`):
```json
{ "data": { "job": { "id": "job_01J9...", "type": "site.create", "status": "queued", "progress": 0, "ws_channel": "job:job_01J9..." } } }
```

## 3. Error Contract

Single shape, stable machine `code`, human `message`, optional field errors:
```json
{
  "error": {
    "code": "validation_failed",
    "message": "The request contains invalid fields.",
    "request_id": "req_01J9...",
    "fields": [ { "field": "primary_domain", "code": "invalid_fqdn", "message": "Not a valid domain." } ]
  }
}
```

| HTTP | `code` examples | Meaning |
|------|-----------------|---------|
| 400 | `bad_request`, `validation_failed` | Malformed / invalid input |
| 401 | `unauthenticated`, `token_expired`, `mfa_required` | No/def. identity |
| 403 | `forbidden`, `scope_insufficient` | Authn OK, not allowed |
| 404 | `not_found` | Resource absent / not visible to caller |
| 409 | `conflict`, `already_exists` | State collision |
| 422 | `unprocessable` | Semantically invalid (e.g. domain not delegated) |
| 429 | `rate_limited` | Throttled (`Retry-After` header) |
| 500 | `internal_error` | Bug/unexpected (correlation id logged) |
| 502/503 | `upstream_unavailable`, `module_unavailable` | Broker/module/engine down |

Domain error → HTTP mapping happens **once**, centrally. Raw OS/stderr is never returned.

## 4. Auth
- **Browser:** login → server-set `HttpOnly; Secure; SameSite=Strict` session cookie **+** short-lived access JWT for API/WS; refresh rotates. CSRF: double-submit token for cookie-auth mutations.
- **MFA:** if enabled, login returns `401 mfa_required` + `mfa_token`; client posts TOTP/WebAuthn assertion to `/auth/mfa`.
- **Programmatic:** `Authorization: Bearer hp_live_<key>`; API keys are scoped (see below) and rate-limited independently.
- **Every mutation is authorized against RBAC scopes** and audited.

## 5. Rate Limiting
- Per-IP (unauth) and per-identity (user/api-key) token buckets in Redis.
- Sensitive endpoints (`/auth/*`, cert issuance, backups) have stricter buckets.
- Responses include `RateLimit-Limit`, `RateLimit-Remaining`, `RateLimit-Reset`.

## 6. Pagination, Filtering, Sorting
- **Cursor-based** (opaque, stable) as default; `?limit=` (max 200). Offset paging available only on small admin tables.
- Filtering: `?filter[status]=active&filter[type]=wordpress`.
- Sorting: `?sort=-created_at,name`.
- Sparse fields: `?fields=id,name,status`.
- Search: `?q=` where supported (routes to global search index for cross-resource).

## 7. Resource Map (v1)

> Every path below is under `/api/v1`. `{id}` are prefixed ULIDs. Mutations that touch the OS return `202 + job`.

### Auth & Identity
```
POST   /auth/login                 POST /auth/mfa            POST /auth/logout
POST   /auth/refresh               GET  /auth/me             PATCH /auth/me
POST   /auth/password              POST /auth/webauthn/register/(begin|finish)
GET    /auth/sessions              DELETE /auth/sessions/{id}
GET    /account/api-keys           POST /account/api-keys    DELETE /account/api-keys/{id}
```

### Users, Roles (admin/reseller)
```
GET/POST        /users              GET/PATCH/DELETE /users/{id}
POST            /users/{id}/suspend  POST /users/{id}/impersonate  (admin, heavily audited)
GET/POST        /roles              GET/PATCH/DELETE /roles/{id}
GET             /permissions
```

### Sites (core)
```
GET/POST                 /sites
GET/PATCH/DELETE         /sites/{id}
POST                     /sites/{id}/(suspend|resume|clone)
GET/POST                 /sites/{id}/domains        DELETE /sites/{id}/domains/{did}
GET/PUT                  /sites/{id}/php            # php_pool config (version, ini, ext, fpm)
GET                      /sites/{id}/php/versions   POST /sites/{id}/php/extensions
GET/PUT                  /sites/{id}/runtime        # node/python/go runtime def
POST                     /sites/{id}/runtime/(start|stop|restart)
GET                      /sites/{id}/logs?stream=access|error   (SSE/WS for tail)
GET/PUT                  /sites/{id}/webserver-config            (raw vhost, validated)
GET                      /sites/{id}/metrics
```

### File Manager (baremetal sites only — enforced server-side)
```
GET    /sites/{id}/files?path=/           # list
GET    /sites/{id}/files/content?path=…   PUT /sites/{id}/files/content
POST   /sites/{id}/files/(mkdir|move|copy|delete|chmod|chown)
POST   /sites/{id}/files/upload           GET /sites/{id}/files/download?path=…
POST   /sites/{id}/files/(compress|extract)
POST   /sites/{id}/files/search           GET /sites/{id}/files/diff?a=…&b=…
```
> Returns `403 file_manager_unavailable` for `deploy_mode in (git,docker)`.

### Databases
```
GET/POST         /databases          GET/DELETE /databases/{id}
POST             /databases/{id}/(import|export)     GET /databases/{id}/size
GET/POST         /database-users     GET/PATCH/DELETE /database-users/{id}
POST             /database-users/{id}/grants
GET              /db-admin/(phpmyadmin|adminer)/session   # signed one-time SSO handoff
```

### DNS
```
GET/POST                 /dns/zones     GET/PATCH/DELETE /dns/zones/{id}
GET/POST                 /dns/zones/{id}/records  PATCH/DELETE …/records/{rid}
POST                     /dns/zones/{id}/(dnssec/enable|dnssec/disable|resync)
GET                      /dns/zones/{id}/export     POST /dns/zones/{id}/import
```

### SSL
```
GET/POST     /ssl/certificates     GET/DELETE /ssl/certificates/{id}
POST         /ssl/certificates/{id}/renew
POST         /ssl/issue            # {domains[], challenge, provider} → 202 job
POST         /ssl/upload           # custom cert/key
```

### Mail
```
GET/POST  /mail/domains  …/{id}   POST /mail/domains/{id}/dkim/(generate|rotate)
GET/POST  /mail/domains/{id}/accounts   …/{aid}
GET/POST  /mail/domains/{id}/aliases    GET /mail/queue    POST /mail/queue/flush
```

### Git deployments
```
GET/POST   /git/sources   …/{id}    POST /git/sources/{id}/test
GET/POST   /sites/{id}/git          # attach/config deployment
POST       /sites/{id}/git/deploy   # manual → 202
GET        /sites/{id}/git/runs     GET /git/runs/{id}   POST /git/runs/{id}/rollback
POST       /webhooks/git/{path}     # public, HMAC-verified (no session)
```

### Docker (module)
```
GET/POST   /docker/stacks   …/{id}   POST …/{id}/(up|down|restart|pull)
GET        /docker/containers  …/{id}/(logs|stats|start|stop|restart|exec)
GET        /docker/(images|volumes|networks)
GET        /app-templates            POST /app-templates/{slug}/deploy   # one-click → 202
```

### Backups
```
GET/POST   /backup/targets  …/{id}   POST …/{id}/test
GET/POST   /backup/policies …/{id}   POST …/{id}/run-now
GET        /backup/runs …/{id}       POST /backup/runs/{id}/restore   # → wizard job
```

### Cron
```
GET/POST   /cron/jobs  …/{id}   POST …/{id}/(enable|disable|run-now)   GET …/{id}/runs
```

### Security
```
GET/POST   /security/firewall/rules  …/{id}
GET        /security/fail2ban/jails   POST /security/fail2ban/unban
GET/POST   /security/ip-lists         GET/POST /security/scans   POST /security/scans/{id}/action
GET        /security/audit-log
GET/PATCH  /security/ssh              # hardening toggles
```

### Monitoring
```
GET   /monitor/overview               # node cpu/ram/disk/io/net/temp snapshot
GET   /monitor/metrics?scope=&id=&metric=&window=
GET/POST /monitor/alerts  …/{id}      GET /monitor/services   # systemd health
```

### System / Modules / Platform
```
GET   /system/info                    # os, arch, virt, kernel, versions
GET   /system/services  POST /system/services/{name}/(start|stop|restart)
GET/POST /modules  GET /modules/{slug}  POST /modules/{slug}/(install|enable|disable|restart|update)
GET   /system/updates  POST /system/updates/apply   POST /system/updates/rollback
GET   /jobs  GET /jobs/{id}  POST /jobs/{id}/cancel   GET /jobs/{id}/log
GET   /notifications  POST /notifications/{id}/read
GET   /search?q=                      # global cross-resource search
GET   /settings  PATCH /settings
```

## 8. Async Job Pattern (canonical)
1. `POST /sites` → validate → create DB row `provisioning` → enqueue `site.create` job → `202 { job }`.
2. Client subscribes to `job:{id}` over WS (channel returned in the response).
3. Worker executes idempotent steps, each emitting `{type:"job.progress", progress, step, message}`.
4. On completion: `{type:"job.completed"|"job.failed"}`; client refetches the resource.
5. `GET /jobs/{id}` gives the same terminal state for pollers that can't use WS.

## 9. WebSocket API
- Endpoint: `wss://host/api/v1/ws` (auth via cookie+JWT or `?token=` for api-key clients).
- Client → server: `{ "op": "subscribe", "channels": ["job:job_…", "site:sit_…", "metrics:node"] }` / `unsubscribe` / `ping`.
- Server → client: `{ "channel", "type", "resource", "data", "ts", "seq" }`.
- **Authorization per channel**: hub verifies the identity may read the resource before subscribing; unauthorized → `{type:"error", code:"forbidden"}`.
- Channel families: `job:*`, `site:*`, `deploy:*`, `container:*`, `metrics:{node|site|container}`, `notifications:{user}`, `security:events`, `system:updates`, `settings`.
- Metrics channels are **subscription-gated sampling**: the monitor module only samples/pushes while ≥1 client is subscribed → no idle polling.

## 10. Internal gRPC (not public)
Two contracts (in `pkg/proto/`):
- **Broker API** (`hpd` → `hp-broker`): typed privileged operations (see [05](05-security-architecture.md)). Bidi streaming for long ops (e.g. cert issuance) that emit progress.
- **Module API** (`hpd` ↔ `hp-mod-*`): lifecycle (`Handshake`, `Health`, `Configure`, `Shutdown`) + a capability-specific service per module. Server-streaming for logs/stats. Defined in [06](06-plugin-architecture.md).

## 11. Versioning, Deprecation, Compatibility
- Additive fields never break clients; removals/renames require a new major path.
- Deprecations announced via `Deprecation` + `Sunset` headers and changelog.
- OpenAPI diff is a CI gate: breaking changes fail unless the major version bumped.

## 12. API Security Headers (all responses)
`Strict-Transport-Security`, `X-Content-Type-Options: nosniff`, `X-Frame-Options: DENY`, `Content-Security-Policy` (strict, nonce-based for the SPA), `Referrer-Policy: no-referrer`, `Permissions-Policy` minimal.

---
Next: [05 — Security Architecture](05-security-architecture.md)
