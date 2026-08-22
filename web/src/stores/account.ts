import { computed } from "vue";
import { defineStore } from "pinia";
import { useSessionStore } from "@/stores/session";

/**
 * The signed-in account as the chrome needs it: a name to show, initials for the
 * avatar, and the plan that decides what the panel offers.
 *
 * Separate from the session store, which answers "may this request proceed".
 * That one mirrors npd's RBAC and is a security question; this one is display,
 * and conflating them means an avatar that cannot render until permissions load.
 * It reads *from* the session rather than holding its own copy, so there is one
 * answer to "who is signed in" and it cannot go stale.
 *
 * Initials are derived rather than stored — a stored pair goes stale the moment
 * someone changes their name, and there is no reason for two fields to disagree.
 */
export const useAccountStore = defineStore("account", () => {
  const session = useSessionStore();

  // Display name, then username, then the local part of the email: npd fills in
  // whichever it has, and an avatar showing "?" because the optional field was
  // empty is worse than one showing a username.
  const name = computed(() => {
    const p = session.principal;
    if (!p) return "";
    return p.display_name || p.username || p.email.split("@")[0] || "";
  });

  const email = computed(() => session.principal?.email ?? "");

  /**
   * The licence tier. Hard-coded because there is no licensing endpoint yet —
   * and named here, once, so that when one lands there is a single place to
   * change rather than a string in the sidebar.
   */
  const plan = computed(() => "Business");

  const initials = computed(() =>
    name.value
      .split(/[\s._-]+/)
      .filter(Boolean)
      .slice(0, 2)
      .map((part) => part[0]?.toUpperCase() ?? "")
      .join(""),
  );

  return { name, email, plan, initials };
});
