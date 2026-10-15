<script setup lang="ts">
// /shops/:tenantId has no content of its own: open the orders tab.
const route = useRoute()
await navigateTo(`/shops/${String(route.params.tenantId)}/orders`, { replace: true })
</script>

<template>
  <div />
</template>
