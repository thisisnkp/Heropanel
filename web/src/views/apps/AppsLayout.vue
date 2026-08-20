<script setup lang="ts">
// Apps is two sections; the section is in the URL so it can be linked to.
import type { NxTab } from "@/components/ui/NxTabs.vue";

const tabs: NxTab[] = [
  { to: "apps-installed", label: "Installed apps", icon: "apps" },
  { to: "apps-install", label: "Install new app", icon: "add-circle" },
];
</script>

<template>
  <div>
    <NxPageHeader title="Apps" subtitle="One-click applications running on this server." />
    <NxTabs :tabs="tabs" />
    <RouterView />
  </div>
</template>
