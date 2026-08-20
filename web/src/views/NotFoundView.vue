<script setup lang="ts">
// Deliberately offers a way out rather than only stating the problem.
</script>

<template>
  <NxEmptyState
    icon="error"
    title="Page not found"
    description="That address does not match anything in the panel."
  >
    <NxButton variant="primary" @click="$router.push({ name: 'home' })">Go to Home</NxButton>
  </NxEmptyState>
</template>
