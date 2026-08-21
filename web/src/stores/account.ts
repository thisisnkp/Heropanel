import { computed, ref } from "vue";
import { defineStore } from "pinia";

/**
 * The signed-in account as the chrome needs it: a name to show, initials for the
 * avatar, and the plan that decides what the panel offers.
 *
 * Separate from the session store, which answers "may this request proceed".
 * That one mirrors npd's RBAC and is a security question; this one is display,
 * and conflating them means an avatar that cannot render until permissions load.
 *
 * Initials are derived rather than stored — a stored pair goes stale the moment
 * someone changes their name, and there is no reason for two fields to disagree.
 */
export const useAccountStore = defineStore("account", () => {
  const name = ref("Aarav Rao");
  const email = ref("aarav@novaretail.in");
  const plan = ref("Business");

  const initials = computed(() =>
    name.value
      .split(/\s+/)
      .filter(Boolean)
      .slice(0, 2)
      .map((part) => part[0]?.toUpperCase() ?? "")
      .join(""),
  );

  return { name, email, plan, initials };
});
