<template>
  <span class="readonly-badge" :title="explanation">
    Read-only
    <span class="sr-only">. {{ explanation }}</span>
  </span>
</template>

<script setup lang="ts">
/**
 * The one thing a reader is told, instead of twelve disabled buttons.
 *
 * Edit controls are removed from the page rather than dimmed — a disabled
 * button still says "you could have done this" and still needs a tooltip
 * nobody reads. But absence is undetectable by a screen reader, so the missing
 * controls are explained once, at the top, next to the status badge where the
 * eye already goes.
 *
 * There are two reasons the controls can be gone, and saying the wrong one is
 * worse than saying nothing:
 *
 * - `viewer` — a real role in a real team. Somebody can change it, and the
 *   notice names the team so the reader knows who to ask.
 * - `remote` — no accounts are configured at all, so there are no roles and the
 *   server simply does not take changes from this machine. Telling that reader
 *   "your role is viewer" would be false, and would send them looking for a
 *   person who does not exist. They need the setting, not the person.
 */
const props = defineProps<{
  teamName?: string
  /** Defaults to `viewer` so existing call sites keep their wording. */
  reason?: 'viewer' | 'remote'
}>()

const explanation = computed(() => {
  if (props.reason === 'remote') {
    return 'This server accepts changes only from the machine it runs on. To edit from here, turn on accounts with auth_enabled, or connect a tool with the api_token.'
  }
  return props.teamName
    ? `You have read-only access to this project through the team ${props.teamName}.`
    : 'You have read-only access to this project.'
})
</script>
