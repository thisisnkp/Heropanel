import { ref } from "vue";
import { defineStore } from "pinia";
import type { LogSource } from "@/data/securitySpec";

/**
 * The security screens' own selections — WAF level, profile, which log you are
 * reading, and whether a scan is running.
 *
 * Separate from the flag store because these are not booleans and not shared
 * with the site screens; keeping them here means switching between the nine
 * security tabs does not reset what you had selected on each.
 */
export const useSecurityStore = defineStore("security", () => {
  const wafLevel = ref("Advanced");
  const profile = ref("Standard");
  const logSource = ref<LogSource>("Authentication");
  const scanning = ref(false);

  function scan() {
    if (scanning.value) return;
    scanning.value = true;
    window.setTimeout(() => (scanning.value = false), 2600);
  }

  return { wafLevel, profile, logSource, scanning, scan };
});
