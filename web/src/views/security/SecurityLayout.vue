<script setup lang="ts">
// Server-wide security. Site-scoped scanning lives under the site drawer, so
// nothing here is per-site and the two never have to be told apart at a glance.
import type { NxTab } from "@/components/ui/NxTabs.vue";

const tabs: NxTab[] = [
  { to: "security-overview", label: "Overview", icon: "shield" },
  { to: "security-firewall", label: "Firewall", icon: "local-fire-department" },
  { to: "security-waf", label: "WAF", icon: "verified-user" },
  { to: "security-malware", label: "Malware scanner", icon: "bug-report" },
  { to: "security-ssh", label: "SSH security", icon: "terminal" },
  { to: "security-updates", label: "Updates", icon: "system-update-alt" },
  { to: "security-login", label: "Login protection", icon: "lock" },
  { to: "security-logs", label: "Logs", icon: "receipt-long" },
  { to: "security-settings", label: "Settings", icon: "tune" },
];
</script>

<template>
  <div>
    <NxPageHeader title="Security" subtitle="Server-wide protection, and what it is currently blocking." />
    <NxTabs :tabs="tabs" />
    <RouterView />
  </div>
</template>
