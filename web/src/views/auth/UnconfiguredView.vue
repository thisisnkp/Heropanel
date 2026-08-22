<script setup lang="ts">
/**
 * What the panel shows when npd has no datastore.
 *
 * Nobody can sign in and no correct password will change that, so showing a
 * login form would be a lie. npd answers `configured: false` on /auth/status
 * precisely so this case can be told apart from "signed out", and the fix is a
 * config change on the host rather than anything anyone can do in a browser.
 */
import { useSessionStore } from "@/stores/session";
import AuthShell from "./AuthShell.vue";

const session = useSessionStore();
</script>

<template>
  <AuthShell
    title="This panel has no database"
    subtitle="npd is running, but it has nowhere to keep accounts, sites or settings."
  >
    <p class="unconf__text">
      Set <code class="nx-mono">database.dsn</code> in the config file — or the
      <code class="nx-mono">NP_DATABASE_DSN</code> environment variable — and restart npd. Until
      then there is nothing to sign in to.
    </p>

    <NxButton variant="default" @click="session.load()">Check again</NxButton>
  </AuthShell>
</template>

<style scoped>
.unconf__text { margin: 0; font-size: var(--nx-text-base); color: var(--nx-text-2); line-height: 1.55; }
.unconf__text code { font-size: var(--nx-text-sm); }
</style>
