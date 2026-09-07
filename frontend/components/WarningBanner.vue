<template>
  <div v-if="isInMemory" class="warning-banner">
    ⚠ <strong>In-memory mode</strong> — data will be lost on restart.
    Run with <code>--db ./research.db</code> to persist data.
  </div>
</template>

<script setup lang="ts">
// The same one request the rest of the app makes. This used to call useApi on
// /api/health directly, which is a second composable with its own request — so
// the page fetched the same document twice, while useServerInfo's own comment
// claimed it did not. That mattered less when it was only the version string;
// one of those two calls now blocks first paint.
const { info } = useServerInfo()
const isInMemory = computed(() => info.value?.in_memory ?? false)
</script>
