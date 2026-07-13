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

export interface PHPInfo {
  version: string;
  socket_path: string;
  pm: string;
  pm_max_children: number;
  memory_limit_mb: number;
}

export function can(principal: Principal | null | undefined, permission: string): boolean {
  if (!principal) return false;
  return principal.permissions.some((p) => p === "*" || p === permission);
}
