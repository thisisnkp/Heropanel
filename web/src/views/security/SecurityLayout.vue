<script setup lang="ts">
// Server-wide security. Site-scoped scanning lives under the site drawer, so
// nothing here is per-site and the two never have to be told apart at a glance.
//
// The layout is a pass-through: the nine sections are listed in the Security
// context sidebar (see components/nav/SecuritySidebar.vue), not in a tab strip
// here. Nine tabs either wrap or scroll sideways at any realistic width, and
// duplicating them in both places is two lists to keep in step. Each section
// writes its own heading, so the page has one <h1> naming where you are.
</script>

<template>
  <RouterView />
</template>
