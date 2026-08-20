import { computed, ref } from "vue";
import { defineStore } from "pinia";

/**
 * The server-wide on/off switches.
 *
 * One store rather than per-screen state because the same switch appears on
 * several screens — SSH access is on the site's SSH page, the server security
 * page and the dashboard's protection checklist — and three copies of the same
 * boolean is three chances for the panel to contradict itself about whether
 * root login is enabled.
 *
 * Keys and defaults are the design's; the real panel will hydrate them from
 * `GET /api/v1/security/settings`.
 */
export type FlagKey =
  | "fw" | "waf" | "ssh" | "rootLogin" | "passwordLogin" | "fail2ban"
  | "autoOs" | "autoPhp" | "autoWp" | "twofa" | "ipBlock" | "realtime"
  | "autoBlock" | "autoQuarantine" | "notifyEmail" | "notifySlack" | "schedScan";

const DEFAULTS: Record<FlagKey, boolean> = {
  fw: true, waf: true, ssh: true, rootLogin: false, passwordLogin: false, fail2ban: true,
  autoOs: true, autoPhp: true, autoWp: true, twofa: true, ipBlock: true, realtime: false,
  autoBlock: true, autoQuarantine: true, notifyEmail: true, notifySlack: false, schedScan: false,
};

export const useFlagsStore = defineStore("flags", () => {
  const flags = ref<Record<FlagKey, boolean>>({ ...DEFAULTS });

  function isOn(key: FlagKey) {
    return flags.value[key] === true;
  }

  function set(key: FlagKey, value: boolean) {
    flags.value[key] = value;
  }

  function toggle(key: FlagKey) {
    flags.value[key] = !flags.value[key];
  }

  /** "On"/"Off", as the design labels its switches. */
  function label(key: FlagKey) {
    return isOn(key) ? "On" : "Off";
  }

  const protectionScore = computed(() => {
    const checks = [flags.value.fw, flags.value.waf, !flags.value.rootLogin, flags.value.twofa];
    return Math.round((checks.filter(Boolean).length / checks.length) * 100);
  });

  return { flags, isOn, set, toggle, label, protectionScore };
});
