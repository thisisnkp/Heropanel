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

  let res: Response;
  try {
    res = await fetch(`/api/v1${path}`, {
      method,
      credentials: "include",
      headers: Object.keys(headers).length ? headers : undefined,
      body: body ? JSON.stringify(body) : undefined,
    });
  } catch {
    // fetch only rejects when the request never completed: the server is down,
    // the port is wrong, or (in `vite dev`) the proxy target is unreachable.
    // Surfacing this as a typed error matters — otherwise every caller reports
    // its own generic "failed", which tells the operator nothing.
    throw new ApiRequestError(0, {
      code: "network_error",
      message: unreachableMessage(),
    });
  }

  // 204/empty bodies.
  const text = await res.text();
  let json: { data?: unknown; error?: ApiError } = {};
  if (text) {
    try {
      json = JSON.parse(text) as typeof json;
    } catch {
      // The API always answers with a JSON envelope. Anything else means we did
      // not actually reach it — most often a dev proxy returning its own HTML
      // error page, or a reverse proxy in front of the panel.
      throw new ApiRequestError(res.status, {
        code: "bad_response",
        message: `The server returned a non-JSON response (HTTP ${res.status}). ${unreachableMessage()}`,
      });
    }
  }

  if (!res.ok) {
    const err: ApiError = json?.error ?? { code: "unknown", message: res.statusText };
    throw new ApiRequestError(res.status, err);
  }
  return (json?.data ?? null) as T;
}

// unreachableMessage explains a connection-level failure, including the dev-mode
// case, since that is where the port mismatch actually bites.
function unreachableMessage(): string {
  const dev = location.port === "5173";
  return dev
    ? "Could not reach the HeroPanel API. Start hpd (it listens on :8443 by default) — that is where the dev server proxies /api."
    : "Could not reach the HeroPanel API. Check that hpd is running and reachable at this address.";
}

export const api = {
  get: <T>(path: string) => request<T>("GET", path),
  post: <T>(path: string, body?: unknown) => request<T>("POST", path, body),
  patch: <T>(path: string, body?: unknown) => request<T>("PATCH", path, body),
  put: <T>(path: string, body?: unknown) => request<T>("PUT", path, body),
  del: <T>(path: string) => request<T>("DELETE", path),
};

// rawFetch is for endpoints whose body is *not* the JSON envelope: file
// download (a stream of bytes) and file upload/save (raw file bytes). It still
// carries the session cookie and the CSRF token, and on failure it parses the
// standard { error } envelope so callers get the same ApiRequestError. On
// success it returns the Response for the caller to read as blob/text.
export async function rawFetch(
  method: Method,
  path: string,
  body?: BodyInit,
  contentType?: string,
): Promise<Response> {
  const headers: Record<string, string> = {};
  if (contentType) headers["Content-Type"] = contentType;
  if (method !== "GET") {
    const t = csrfToken();
    if (t) headers["X-CSRF-Token"] = t;
  }
  let res: Response;
  try {
    res = await fetch(`/api/v1${path}`, {
      method,
      credentials: "include",
      headers: Object.keys(headers).length ? headers : undefined,
      body,
    });
  } catch {
    throw new ApiRequestError(0, { code: "network_error", message: unreachableMessage() });
  }
  if (!res.ok) {
    let err: ApiError = { code: "unknown", message: res.statusText };
    try {
      const j = await res.clone().json();
      if (j?.error) err = j.error;
    } catch {
      /* non-JSON error body: keep the statusText fallback */
    }
    throw new ApiRequestError(res.status, err);
  }
  return res;
}

// uploadWithProgress sends a body and reports how much of it has gone out.
//
// It uses XMLHttpRequest rather than fetch, which is the whole reason it exists:
// fetch has **no upload progress event** at all. A 200 MB upload through
// rawFetch shows a spinner and nothing else — no percentage, no way to tell a
// slow connection from a stalled one, no cancel. (The streaming-request-body
// alternative needs HTTP/2 plus `duplex: "half"` and is not broadly supported.)
//
// It carries the session cookie and CSRF token exactly as rawFetch does, and
// parses the same `{ error }` envelope, so callers get the usual
// ApiRequestError.
export function uploadWithProgress(
  path: string,
  body: Blob,
  opts: {
    contentType?: string;
    /** Called with bytes sent so far, repeatedly, as the body goes out. */
    onProgress?: (loaded: number, total: number) => void;
    signal?: AbortSignal;
  } = {},
): Promise<void> {
  return new Promise((resolve, reject) => {
    const xhr = new XMLHttpRequest();
    xhr.open("PUT", `/api/v1${path}`);
    xhr.withCredentials = true;
    xhr.setRequestHeader("Content-Type", opts.contentType || "application/octet-stream");
    const t = csrfToken();
    if (t) xhr.setRequestHeader("X-CSRF-Token", t);

    xhr.upload.onprogress = (e) => {
      // lengthComputable is false for a body of unknown size; a File always has
      // one, so this is only a guard against exotic bodies.
      if (e.lengthComputable) opts.onProgress?.(e.loaded, e.total);
    };

    xhr.onload = () => {
      if (xhr.status >= 200 && xhr.status < 300) {
        resolve();
        return;
      }
      let err: ApiError = { code: "unknown", message: xhr.statusText || "Upload failed" };
      try {
        const j = JSON.parse(xhr.responseText) as { error?: ApiError };
        if (j?.error) err = j.error;
      } catch {
        /* non-JSON error body: keep the statusText fallback */
      }
      reject(new ApiRequestError(xhr.status, err));
    };

    xhr.onerror = () =>
      reject(new ApiRequestError(0, { code: "network_error", message: unreachableMessage() }));
    // A cancelled upload is not a failure to report as one — the operator asked
    // for it — so it gets its own code for callers to recognise and stay quiet.
    xhr.onabort = () =>
      reject(new ApiRequestError(0, { code: "upload_cancelled", message: "Upload cancelled." }));

    if (opts.signal) {
      if (opts.signal.aborted) {
        xhr.abort();
        return;
      }
      opts.signal.addEventListener("abort", () => xhr.abort(), { once: true });
    }
    xhr.send(body);
  });
}

/** True when an error is a user-initiated cancellation rather than a failure. */
export function isCancelled(e: unknown): boolean {
  return e instanceof ApiRequestError && e.code === "upload_cancelled";
}

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
  /** False when hpd has no datastore configured, so no one can sign in. Older
   *  servers omit it; treat undefined as "configured". */
  configured?: boolean;
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

export interface FileEntry {
  name: string;
  kind: "file" | "dir" | "symlink" | "other";
  size: number;
  mode: string; // octal, e.g. "644"
  mtime: number; // epoch seconds
}

export interface FileListing {
  path: string;
  entries: FileEntry[];
}

export interface TerminalRecording {
  uid: string;
  /** The site the session ran on. Empty when that site has since been deleted. */
  site_uid: string;
  site_name: string;
  actor_user_id: number;
  actor_email: string;
  actor_ip: string;
  system_user: string;
  size_bytes: number;
  duration_ms: number;
  /** The recording hit its size cap or a write error, so it is incomplete. */
  truncated: boolean;
  started_at: string;
  ended_at?: string;
  expires_at: string;
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
