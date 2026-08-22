import { computed, ref } from "vue";
import { defineStore } from "pinia";
import {
  api,
  can as hasPermission,
  needsMFA,
  type AuthStatus,
  type LoginResult,
  type Principal,
} from "@/lib/api";

/**
 * Who is signed in, what they may do, and what the panel needs before it can
 * show anything at all.
 *
 * The three first-run states are kept apart because they need different screens
 * and are not orderable as one "readiness" number:
 *
 *   configured   — npd has a datastore. False means nobody can sign in and no
 *                  amount of correct credentials will change that; the only
 *                  useful screen explains the missing DSN.
 *   bootstrapped — an administrator exists. False means the login form would
 *                  reject everyone, so the first-run screen creates one instead.
 *   setupComplete— the infrastructure wizard has been answered.
 *
 * `can()` mirrors npd's RBAC names (`site.write`, `system.read`, …) so a control
 * hidden here is one the API would refuse anyway. It is a courtesy, never the
 * enforcement — every check exists again on the server.
 */
export const useSessionStore = defineStore("session", () => {
  const principal = ref<Principal | null>(null);
  const status = ref<AuthStatus | null>(null);

  /** True until the first /auth/status answers. The shell shows nothing yet. */
  const loading = ref(true);
  /** Set when the panel could not be reached at all, rather than said no. */
  const unreachable = ref<string | null>(null);

  /** Held between /auth/login and /auth/mfa. Never persisted. */
  const mfaToken = ref<string | null>(null);

  const isAuthenticated = computed(() => principal.value !== null);
  const configured = computed(() => status.value?.configured !== false);
  const needsBootstrap = computed(() => status.value?.needs_bootstrap === true);
  const setupComplete = computed(() => status.value?.setup_complete !== false);

  function can(permission: string) {
    return hasPermission(principal.value, permission);
  }

  /**
   * Asks the panel where it stands, then who we are.
   *
   * /auth/status is public and always answers; /auth/me needs a session, so it
   * is only called when status says there is one. A 401 from it is not an error
   * to surface — it means the cookie expired between the two calls — so it just
   * leaves the session empty and the guard sends the browser to the login form.
   */
  async function load() {
    loading.value = true;
    try {
      status.value = await api.get<AuthStatus>("/auth/status");
      unreachable.value = null;
    } catch (e) {
      // A transport failure is not "signed out". Saying so would replace the
      // real problem — npd is down — with a login form that cannot ever work.
      unreachable.value = e instanceof Error ? e.message : "The panel is unreachable.";
      status.value = null;
      principal.value = null;
      loading.value = false;
      return;
    }
    if (status.value.authenticated) {
      try {
        principal.value = await api.get<Principal>("/auth/me");
      } catch {
        principal.value = null;
      }
    } else {
      principal.value = null;
    }
    loading.value = false;
  }

  /**
   * Signs in. Resolves "mfa" when a second factor is owed, in which case
   * `mfaToken` is held for completeMFA and no session exists yet.
   */
  async function login(email: string, password: string): Promise<"ok" | "mfa"> {
    const res = await api.post<LoginResult>("/auth/login", { email, password });
    if (needsMFA(res)) {
      mfaToken.value = res.mfa_token;
      return "mfa";
    }
    mfaToken.value = null;
    principal.value = res;
    await refreshStatus();
    return "ok";
  }

  async function completeMFA(code: string) {
    if (!mfaToken.value) throw new Error("There is no login waiting for a code.");
    principal.value = await api.post<Principal>("/auth/mfa", {
      mfa_token: mfaToken.value,
      code,
    });
    mfaToken.value = null;
    await refreshStatus();
  }

  /** Creates the first administrator. Only possible while needsBootstrap. */
  async function bootstrap(email: string, username: string, password: string) {
    await api.post<Principal>("/auth/bootstrap", { email, username, password });
    // Bootstrap creates the account; it does not sign anyone in. Logging in
    // here rather than sending the operator to a form they just filled twice.
    await login(email, password);
  }

  async function logout() {
    try {
      await api.post("/auth/logout");
    } finally {
      // Whatever the server said, this browser is done with the session. A
      // failed logout that leaves the UI signed in is the worse outcome.
      principal.value = null;
      mfaToken.value = null;
      await refreshStatus();
    }
  }

  /** Re-reads the first-run flags without touching `loading`. */
  async function refreshStatus() {
    try {
      status.value = await api.get<AuthStatus>("/auth/status");
    } catch {
      /* keep the last known state; load() owns the unreachable case */
    }
  }

  /** Replaces the principal wholesale — used by tests. */
  function set(next: Principal | null) {
    principal.value = next;
    loading.value = false;
  }

  return {
    principal,
    status,
    loading,
    unreachable,
    mfaToken,
    isAuthenticated,
    configured,
    needsBootstrap,
    setupComplete,
    can,
    load,
    login,
    completeMFA,
    bootstrap,
    logout,
    refreshStatus,
    set,
  };
});
