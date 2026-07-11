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

async function request<T>(method: Method, path: string, body?: unknown): Promise<T> {
  const res = await fetch(`/api/v1${path}`, {
    method,
    credentials: "include",
    headers: body ? { "Content-Type": "application/json" } : undefined,
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

export function can(principal: Principal | null | undefined, permission: string): boolean {
  if (!principal) return false;
  return principal.permissions.some((p) => p === "*" || p === permission);
}
