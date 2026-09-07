<template>
  <div v-if="pending" class="skeleton-page">
    <div class="skeleton-card skeleton-header"></div>
    <div class="skeleton-card skeleton-content"></div>
  </div>

  <div v-else-if="research" class="updates-page">
    <PageHeader
      :crumbs="[
        { label: 'Projects', to: { name: 'index' } },
        { label: research.name, to: `/research/${researchSlug}` },
        { label: 'Updates' },
      ]"
      :code="research.code"
      title="Updates"
      :count="updates.count"
      lead="Documents created or revised since you last opened them."
    >
      <template #actions>
        <TeamViewerNotice v-if="readOnlyReason" :reason="readOnlyReason" :team-name="research?.team_name" />
        <button
          v-if="updates.count"
          type="button"
          class="btn btn-sm btn-primary"
          :disabled="markingAll"
          @click="markAll"
        >
          {{ markingAll ? 'Marking…' : 'Mark all as seen' }}
        </button>
      </template>
    </PageHeader>

    <p v-if="bulkError" class="inline-error inline-error--action updates-error" role="alert">
      <span>{{ bulkError }}</span>
      <button v-if="bulkNeedsRefresh" type="button" class="btn btn-sm" @click="retryUpdates()">Try again</button>
    </p>
    <p v-if="refreshError && !bulkError" class="inline-error inline-error--action updates-error" role="alert">
      <span>{{ refreshError }}</span>
      <button type="button" class="btn btn-sm" @click="retryUpdates()">Try again</button>
    </p>
    <p class="sr-only" role="status" aria-live="polite">{{ announcement }}</p>

    <div ref="listState" tabindex="-1" role="region" aria-label="Document updates">
      <ResearchUpdatesList
        :updates="updates.entries"
        :sections="sections"
        :research-slug="researchSlug"
        :loading="updatesPending && !updates.entries.length"
        :refreshing="updatesPending && !!updates.entries.length"
        :error="updatesError"
        @retry="retryUpdates"
      />
    </div>
  </div>

  <EmptyState
    v-else-if="researchError && researchError.statusCode !== 404"
    icon="&#x26A0;"
    title="Could not load this project"
    description="The server did not return the project or its sections."
  >
    <button type="button" class="btn btn-sm" @click="refreshResearch()">Try again</button>
  </EmptyState>
  <EmptyState v-else icon="&#x1F50D;" title="Project not found">
    <NuxtLink :to="{ name: 'index' }" class="btn btn-sm">Back to projects</NuxtLink>
  </EmptyState>
</template>

<script setup lang="ts">
const route = useRoute()
const id = route.params.id as string

const {
  data: researchData,
  pending,
  error: researchError,
  refresh: refreshResearch,
} = await useApi<{ data: any }>(`/api/researches/${id}`)
const research = computed(() => researchData.value?.data?.research)
const sections = computed<any[]>(() => researchData.value?.data?.sections ?? [])
const researchSlug = computed(() => research.value?.code || id)

const { readOnlyReason, setFromResearch } = useResearchRole()
watch(research, (value) => setFromResearch(value), { immediate: true })

const {
  updates,
  pending: updatesPending,
  error: updatesFetchError,
  refresh: refreshUpdates,
} = await useEntryUpdates(id)
const updatesError = computed(() => updatesFetchError.value && !updates.value.entries.length
  ? 'Could not load updates'
  : null)
const refreshError = computed(() => updatesFetchError.value && updates.value.entries.length
  ? 'Could not refresh Updates. The list below may be out of date.'
  : '')

const { authFetch } = useAuth()
const apiBase = useRuntimeConfig().public.apiBase || ''
const markingAll = ref(false)
const bulkError = ref('')
const bulkNeedsRefresh = ref(false)
const announcement = ref('')
const listState = ref<HTMLElement | null>(null)

async function markAll() {
  if (!updates.value.entries.length || markingAll.value) return
  // This is the snapshot the reader can see. Asking the server to mark its
  // newest revisions instead would clear a write that lands during the click.
  const snapshot = updates.value.entries.map((entry) => ({
    entry_id: entry.entry_id,
    revision: entry.current_revision,
  }))
  markingAll.value = true
  bulkError.value = ''
  bulkNeedsRefresh.value = false
  try {
    await authFetch(`${apiBase}/api/researches/${id}/updates/seen`, {
      method: 'POST',
      body: { entries: snapshot },
    })
  } catch {
    // A transport failure does not prove the transaction failed: the server
    // may have committed before its response was lost. Read the canonical
    // queue before telling the reader what happened.
    try {
      await refreshUpdates()
      if (updatesFetchError.value) {
        bulkError.value = 'Could not confirm whether the updates were marked as seen. The list may be out of date.'
        bulkNeedsRefresh.value = true
      } else {
        bulkError.value = 'The operation could not be confirmed. The current server state is shown below.'
      }
    } catch {
      bulkError.value = 'Could not confirm whether the updates were marked as seen. The list may be out of date.'
      bulkNeedsRefresh.value = true
    }
    announcement.value = bulkError.value
    markingAll.value = false
    return
  }

  try {
    await refreshUpdates()
    if (updatesFetchError.value) {
      bulkError.value = `${snapshot.length} updates were marked as seen, but the list could not be refreshed.`
      bulkNeedsRefresh.value = true
      announcement.value = bulkError.value
      return
    }
    const remaining = updates.value.count
    announcement.value = remaining
      ? `${snapshot.length} updates marked as seen. ${remaining} newer updates remain.`
      : `${snapshot.length} updates marked as seen.`
    if (!remaining) await nextTick(() => listState.value?.focus())
  } catch {
    bulkError.value = `${snapshot.length} updates were marked as seen, but the list could not be refreshed.`
    bulkNeedsRefresh.value = true
    announcement.value = bulkError.value
  } finally {
    markingAll.value = false
  }
}

async function retryUpdates() {
  await refreshUpdates()
  clearStaleRefreshError()
}

function clearStaleRefreshError() {
  if (!updatesFetchError.value && bulkNeedsRefresh.value) {
    bulkError.value = ''
    bulkNeedsRefresh.value = false
  }
}

async function refreshFromRealtime() {
  const focusedElement = import.meta.client ? document.activeElement : null
  const focused = !!focusedElement && !!listState.value?.contains(focusedElement)
  await refreshUpdates()
  clearStaleRefreshError()
  await nextTick()
  if (focused && import.meta.client && !focusedElement?.isConnected) {
    announcement.value = 'Updates changed in another tab. The list has been refreshed.'
    listState.value?.focus()
  }
}

useResearchRealtime(
  () => id,
  (event) => {
    if (event.entity === 'entry' || (event.entity === 'entry_view' && !isSelf(event))) {
      void refreshFromRealtime()
    }
  },
  { researchId: () => research.value?.id, onResync: () => { void refreshFromRealtime() } },
)
</script>

<style scoped>
.updates-error { margin-bottom: var(--space-4); }
</style>
