<script setup lang="ts">
// Apps is three sections; the section is in the URL so it can be linked to.
// Each section writes its own heading, as the security tabs do.
import type { NxTab } from "@/components/ui/NxTabs.vue";

const tabs: NxTab[] = [
  { to: "apps-installed", label: "Installed apps", icon: "apps" },
  { to: "apps-install", label: "Install new app", icon: "add-circle" },
  { to: "apps-licenses", label: "Licenses", icon: "card-membership" },
];
</script>

<template>
  <div>
    <NxTabs :tabs="tabs" />
    <RouterView />
  </div>
</template>
