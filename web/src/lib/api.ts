// Typed API client for the NexPanel control-plane API.
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
  const m = document.cookie.match(/(?:^|;\s*)np_csrf=([^;]+)/);
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
    ? "Could not reach the NexPanel API. Start npd (it listens on :8443 by default) — that is where the dev server proxies /api."
    : "Could not reach the NexPanel API. Check that npd is running and reachable at this address.";
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
//
// Only the shapes the app actually reads are declared. A type per endpoint,
// written ahead of the screen that uses it, is a second API surface to keep in
// sync with the real one — and the one that rots silently, because nothing
// fails when it drifts. Each module adds its own as it is wired.

export interface Principal {
  user_id: number;
  user_uid: string;
  email: string;
  username: string;
  display_name: string;
  permissions: string[];
  /** Present only while impersonating: the real administrator behind the
   *  session. The UI shows a banner and the "stop" control when set. */
  impersonator_user_id?: number;
  impersonator_uid?: string;
  impersonator_email?: string;
}

export interface AuthStatus {
  needs_bootstrap: boolean;
  authenticated: boolean;
  /** False when npd has no datastore configured, so no one can sign in. Older
   *  servers omit it; treat undefined as "configured". */
  configured?: boolean;
  /** False on a fresh install until the first-run setup wizard is completed.
   *  Older servers omit it; treat undefined as "complete" so the wizard never
   *  blocks a panel that does not know about it. */
  setup_complete?: boolean;
}

/**
 * What POST /auth/login answers.
 *
 * It is one of two shapes, not a union of optional fields: either the Principal
 * (the session is established) or `{mfa_required, mfa_token}` (it is not, and
 * the token must be exchanged at /auth/mfa for a second factor). `mfa_required`
 * is the discriminator.
 */
export type LoginResult = Principal | { mfa_required: true; mfa_token: string };

export function needsMFA(r: LoginResult): r is { mfa_required: true; mfa_token: string } {
  return "mfa_required" in r && r.mfa_required === true;
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

/** One selectable (or merely announced) component in the setup wizard. */
export interface SetupOption {
  id: string;
  label: string;
  note?: string;
  supported: boolean;
}

export interface SetupSelection {
  webserver: string;
  db_engine: string;
  manage_dns: boolean;
  create_mail: boolean;
  license_key?: string;
  panel_domain?: string;
  panel_ipv4?: string;
}

export interface SetupState extends SetupSelection {
  completed: boolean;
  completed_at?: string;
}

export interface SetupInfo {
  state: SetupState | null;
  webservers: SetupOption[];
  db_engines: SetupOption[];
  /** Installed on every host, not offered as a choice. */
  baseline: SetupOption[];
}

export interface Site {
  uid: string;
  name: string;
  primary_domain: string;
  /** How the vhost is built: "static", "php" or "proxy". */
  type: string;
  /**
   * What the site runs: "static", "php", "node", "python" or "app".
   *
   * Not derivable from `type` — three stacks share the "proxy" answer — which
   * is why npd computes it. Read this, never `type`, when the question is
   * "what kind of site is this".
   */
  stack: string;
  deploy_mode: string;
  status: string;
  webserver: string;
  document_root: string;
  system_user: string;
  waf_enabled?: boolean;
  created_at: string;
  /** A point-in-time snapshot from creation: "verified" (a free/parked domain,
   *  or a subdomain of one) or "unverified". Empty when the registry was not
   *  available at creation time. */
  dns_status?: "verified" | "unverified" | "";
}

/** What POST /databases/{uid}/phpmyadmin answers: a URL, and no credentials. */
export interface PMAHandoff {
  url: string;
  expires_at: string;
}

/**
 * Opens a database in phpMyAdmin.
 *
 * The window is opened *before* the request, not after. A browser only treats a
 * window as user-initiated while it is still handling the click, so opening it
 * from the promise's callback gets it blocked by the popup blocker — and the
 * operator sees nothing happen at all. So a blank tab is claimed first and
 * pointed at the hand-off URL once npd answers; if the request fails, that tab
 * is closed again rather than left sitting on about:blank.
 *
 * The hand-off carries a one-time ticket and no credentials: phpMyAdmin's own
 * sign-on script redeems it against the panel over loopback, so the database
 * password never reaches the browser.
 */
export async function openPhpMyAdmin(databaseUid: string): Promise<void> {
  const tab = window.open("", "_blank", "noopener");
  try {
    const handoff = await api.post<PMAHandoff>(`/databases/${databaseUid}/phpmyadmin`);
    if (tab) tab.location.replace(handoff.url);
    else window.location.assign(handoff.url); // popups blocked: use this tab
  } catch (e) {
    tab?.close();
    throw e;
  }
}

/**
 * True when the principal holds a permission.
 *
 * "*" is the administrator wildcard npd issues, and it has to be honoured here
 * or an admin sees a panel with every control hidden — the API would allow
 * every one of them.
 */
export function can(principal: Principal | null | undefined, permission: string): boolean {
  if (!principal) return false;
  return principal.permissions.some((p) => p === "*" || p === permission);
}
