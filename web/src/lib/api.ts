// Typed API client for the HeroPanel control-plane API.
//
// All responses use the standard envelope: { data, meta } on success and
// { error } on failure (see docs/04). Cookies carry the session, so every
// request includes credentials.

export interface ApiError {
  code: string;
  message: string;
  request_id?: string;
  fields?: { field: string; code: string; message: string }[];
}

export class ApiRequestError extends Error {
  status: number;
  code: string;
  fields?: ApiError["fields"];
  constructor(status: number, err: ApiError) {
    super(err.message || "Request failed");
    this.name = "ApiRequestError";
    this.status = status;
    this.code = err.code;
    this.fields = err.fields;
  }
}

type Method = "GET" | "POST" | "PATCH" | "PUT" | "DELETE";

// Reads the double-submit CSRF token the server set as a readable cookie.
function csrfToken(): string | undefined {
  const m = document.cookie.match(/(?:^|;\s*)hp_csrf=([^;]+)/);
  return m ? decodeURIComponent(m[1]) : undefined;
}

async function request<T>(method: Method, path: string, body?: unknown): Promise<T> {
  const headers: Record<string, string> = {};
  if (body) headers["Content-Type"] = "application/json";
  // Echo the CSRF token on mutations (no-op unless the server enforces it).
  if (method !== "GET") {
    const t = csrfToken();
    if (t) headers["X-CSRF-Token"] = t;
  }

  const res = await fetch(`/api/v1${path}`, {
    method,
    credentials: "include",
    headers: Object.keys(headers).length ? headers : undefined,
    body: body ? JSON.stringify(body) : undefined,
  });

  // 204/empty bodies.
  const text = await res.text();
  const json = text ? JSON.parse(text) : {};

  if (!res.ok) {
    const err: ApiError = json?.error ?? { code: "unknown", message: res.statusText };
    throw new ApiRequestError(res.status, err);
  }
  return (json?.data ?? null) as T;
}

export const api = {
  get: <T>(path: string) => request<T>("GET", path),
  post: <T>(path: string, body?: unknown) => request<T>("POST", path, body),
  patch: <T>(path: string, body?: unknown) => request<T>("PATCH", path, body),
  put: <T>(path: string, body?: unknown) => request<T>("PUT", path, body),
  del: <T>(path: string) => request<T>("DELETE", path),
};

// ── Shared API types ────────────────────────────────────────────────────────

export interface Principal {
  user_id: number;
  user_uid: string;
  email: string;
  username: string;
  display_name: string;
  permissions: string[];
}

export interface AuthStatus {
  needs_bootstrap: boolean;
  authenticated: boolean;
}

export interface SystemInfo {
  product: string;
  version: string;
  go: string;
  os: string;
  arch: string;
  cpus: number;
  started_at: string;
  uptime_seconds: number;
}

export interface UserSummary {
  uid: string;
  email: string;
  username: string;
  display_name: string;
  status: string;
}

export interface Site {
  uid: string;
  name: string;
  primary_domain: string;
  type: string;
  deploy_mode: string;
  status: string;
  webserver: string;
  document_root: string;
  system_user: string;
  created_at: string;
}

export type JobStatus = "queued" | "running" | "succeeded" | "failed" | "cancelled";

export interface Job {
  id: string;
  type: string;
  status: JobStatus;
  progress: number;
  result?: unknown;
  error?: string;
  ws_channel?: string;
  created_at: string;
}

export interface FPM {
  pm: string;
  pm_max_children: number;
  pm_start_servers: number;
  pm_min_spare_servers: number;
  pm_max_spare_servers: number;
  pm_max_requests: number;
  pm_idle_timeout_sec: number;
}

export interface OPcache {
  enabled: boolean;
  jit: string;
}

export interface PHPInfo {
  version: string;
  socket_path: string;
  memory_limit_mb: number;
  fpm: FPM;
  ini: Record<string, string>;
  opcache: OPcache;
  allowed_ini: string[];
}

export interface PHPExtensions {
  version: string;
  available: string[];
  enabled: string[];
  scope_note: string;
}

export interface SiteLog {
  kind: string;
  lines: number;
  content: string;
  exists: boolean;
}

export interface SiteLimits {
  cpu_quota_pct: number;
  mem_limit_bytes: number;
  pids_max: number;
}

export interface Domain {
  uid: string;
  fqdn: string;
  kind: string;
  redirect_to?: string;
  redirect_code?: number;
  force_https?: boolean;
}

export interface Runtime {
  runtime: string;
  command: string;
  port: number;
  env: Record<string, string>;
  health_path?: string;
  status: string;
}

export interface RuntimeHealth {
  configured: boolean;
  healthy: boolean;
  status_code?: number;
  latency_ms?: number;
  error?: string;
}

export interface GitSource {
  uid: string;
  repo_url: string;
  branch: string;
  build_command: string;
  web_root: string;
  auth_kind: string;
  auth_username?: string;
  public_key?: string; // deploy key to register on the repo (ssh_key only)
  host_key?: string; // pinned SSH host key(s); strict checking when set (ssh_key only)
  auto_composer: boolean;
  webhook_url?: string;
}

export interface Deployment {
  uid: string;
  commit_sha: string;
  status: string;
  trigger: string;
  log?: string;
  created_at: string;
  finished_at?: string;
}

export interface Database {
  uid: string;
  name: string;
  size_bytes?: number;
  created_at: string;
}

export interface DatabaseUser {
  uid: string;
  username: string;
  host: string;
  created_at: string;
}

export interface AdminerSSO {
  url: string;
  driver: string;
  server: string;
  database: string;
  username: string;
  password: string;
  expires_at: string;
}

export interface DNSZone {
  uid: string;
  name: string;
  primary_ns: string;
  admin_email: string;
  serial: number;
  ttl: number;
  status: string;
  created_at: string;
}

export interface DNSRecord {
  uid: string;
  name: string;
  type: string;
  content: string;
  ttl: number;
  priority: number;
}

export interface Certificate {
  uid: string;
  provider: string;
  common_name: string;
  sans: string[];
  status: string;
  issued_at: string;
  expires_at: string;
  auto_renew: boolean;
}

export interface AuditEntry {
  uid: string;
  actor_user_id: number;
  actor_ip: string;
  actor_kind: string;
  action: string;
  resource_type: string;
  resource_id: string;
  outcome: string;
  detail: string;
  created_at: string;
}

export interface ModuleInfo {
  slug: string;
  state: string;
  capabilities: string[];
}

export function can(principal: Principal | null | undefined, permission: string): boolean {
  if (!principal) return false;
  return principal.permissions.some((p) => p === "*" || p === permission);
}
