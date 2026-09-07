<template>
  <div v-if="pending" class="skeleton-page">
    <div class="skeleton-card skeleton-header"></div>
    <div class="skeleton-card skeleton-content"></div>
  </div>

  <div v-else-if="research" class="marks-page">
    <PageHeader
      :crumbs="[
        { label: 'Projects', to: { name: 'index' } },
        { label: research.name, to: `/research/${researchSlug}` },
        { label: 'Marks' },
      ]"
      :code="research.code"
      title="Marks"
      :count="annotations.length"
      :lead="filterSummary"
    >
      <template #actions>
        <TeamViewerNotice v-if="readOnlyReason" :reason="readOnlyReason" :team-name="research?.team_name" />
        <button
          v-if="canWrite && answered.length"
          class="btn btn-sm btn-primary"
          @click="reviewOpen = true"
        >
          Review pass
          <span class="btn-count">{{ answered.length }}</span>
        </button>
      </template>
    </PageHeader>

    <!-- The one state that is a finding rather than a nuisance: text somebody
         doubted was rewritten or removed, and only a person can say whether the
         doubt was answered or buried. -->
    <div v-if="orphanCount && !filters.anchor" class="marks-orphans" role="status">
      <span>
        {{ orphanCount }} {{ orphanCount === 1 ? 'mark has' : 'marks have' }} lost the text
        {{ orphanCount === 1 ? 'it was' : 'they were' }} attached to.
      </span>
      <button type="button" class="btn btn-sm" @click="setFilters({ ...filters, anchor: 'orphaned' })">
        Show them
      </button>
    </div>

    <AnnotationsAnnotationList
      :annotations="annotations"
      :research-slug="researchSlug"
      :loading="loading"
      :error="error"
      :filters="filters"
      show-filters
      grouped
      empty-variant="research"
      @update:filters="setFilters"
      @open="openMark"
      @retry="load"
    />

    <AnnotationsPassReviewModal
      :visible="reviewOpen"
      :research-slug="researchSlug"
      :annotations="answered"
      :busy="reviewBusy"
      :result="reviewResult"
      @accept="accept"
      @send-back="sendBack"
      @close="reviewOpen = false"
    />
  </div>

  <EmptyState v-else icon="&#x1F50D;" title="Project not found" />
</template>

<script setup lang="ts">
/**
 * The queue.
 *
 * Its job is not to list marks — the document page already does that for one
 * document. Its job is the pass: what the agent answered, accepted or sent back
 * as a batch, because reviewing fifteen marks one request at a time costs more
 * than the work being reviewed and is how a queue stops being read.
 */
import type { Annotation, QueueFilters } from '~/composables/useAnnotations'

const route = useRoute()
const id = route.params.id as string

const { data: researchData, pending } = await useApi<{ data: any }>(`/api/researches/${id}`)
const research = computed(() => researchData.value?.data?.research)

const { canWrite, readOnlyReason, setFromResearch } = useResearchRole()
watch(researchData, (d) => setFromResearch(d?.data?.research), { immediate: true })

const researchSlug = computed(() => research.value?.code || id)

const { listForResearch, bulk } = useAnnotations()
const { push: pushToast } = useToasts()

const annotations = ref<Annotation[]>([])
const meta = ref<Record<string, any>>({})
const loading = ref(true)
const error = ref<string | null>(null)
// Open by default, so this page and the counter that links to it are counting
// the same thing. A queue is a work list; everything ever marked is an archive,
// and the filter is right there for when somebody wants it.
// Open by default, but the route wins: the resume block links here with
// `?status=answered` for the marks waiting on a person, and a page that ignored
// that would land them on a different queue than the one they clicked.
// Open by default, but the route wins: the resume block links here with
// `?status=answered` for the marks waiting on a person, and a page that ignored
// that would land them on a different queue than the one they clicked.
//
// `?a=1&a=2` parses as an array, and a value the API does not know is worse
// than no filter — so anything that is not a single known value is dropped.
function queryFilter<T extends string>(value: unknown, allowed: readonly T[]): T | undefined {
  return typeof value === 'string' && (allowed as readonly string[]).includes(value) ? (value as T) : undefined
}

const initialQuery = useRoute().query
const filters = ref<QueueFilters>({
  status: queryFilter(initialQuery.status, ['open', 'answered', 'closed', 'dismissed'] as const) ?? 'open',
  kind: queryFilter(initialQuery.kind, ['verify', 'dig', 'disagree'] as const),
  anchor: queryFilter(initialQuery.anchor, ['anchored', 'drifted', 'moved', 'orphaned'] as const),
} as QueueFilters)

const reviewOpen = ref(false)
const reviewBusy = ref(false)
const reviewResult = ref<string | null>(null)

/** The count beside the title is of the FILTERED list, so the title has to say
 *  what it is counting — "Marks 3" on a research with forty is otherwise a
 *  number that reads as the total. */
const filterSummary = computed(() => {
  const parts: string[] = []
  if (filters.value.status) parts.push(`${filters.value.status}`)
  if (filters.value.kind) parts.push(`${filters.value.kind}`)
  if (filters.value.anchor) parts.push(`${filters.value.anchor}`)
  return parts.length ? `Showing ${parts.join(' · ')}` : 'Showing everything marked'
})

const answered = ref<Annotation[]>([])
const orphanCount = computed(() => meta.value?.by_anchor?.orphaned ?? 0)

/**
 * What the agent has finished and handed back.
 *
 * Fetched on its own rather than filtered out of the visible list: deriving it
 * from the rows on screen meant filtering the queue to "open" — the most
 * natural thing to do on a queue — silently removed the button that accepts the
 * pass.
 */
async function loadAnswered() {
  try {
    const res = await listForResearch(id, { status: 'answered' })
    answered.value = res.data
  } catch {
    answered.value = []
  }
}

async function load(background = false) {
  // Only a first load or a deliberate one shows the skeleton. An agent
  // finishing a mark used to blank the screen under the person reading it.
  if (!background) loading.value = true
  error.value = null
  try {
    const res = await listForResearch(id, filters.value)
    annotations.value = res.data
    meta.value = res.meta
    await loadAnswered()
  } catch (e: any) {
    error.value = e?.data?.error || e?.message || 'Could not load the marks'
  } finally {
    loading.value = false
  }
}

function setFilters(next: QueueFilters) {
  filters.value = next
  // The URL follows the filter, so a queue somebody is looking at can be sent
  // to a colleague — and so a reload keeps the list they were reading.
  useRouter().replace({
    query: {
      ...useRoute().query,
      status: next.status || undefined,
      kind: next.kind || undefined,
      // The orphaned-marks shortcut on this very page goes through here too;
      // leaving it out of the URL meant reloading dropped you back on the full
      // queue, which is what writing the URL was supposed to prevent.
      anchor: next.anchor || undefined,
    },
  })
  load()
}

function openMark(a: Annotation) {
  // The mark lives on a sentence, so the place to deal with it is the document
  // it marks, with that sentence on screen.
  if (!a.entry_code) return
  navigateTo(entryPath(researchSlug.value, a.entry_code))
}

/** Accepting waits for the server and reports per row: a batch where two rows
 *  failed is neither a success nor a failure, and the screen has to say which. */
async function accept(ids: string[]) {
  await decide(ids, 'closed', '')
}

async function sendBack(ids: string[], reason: string) {
  await decide(ids, 'open', reason)
}

async function decide(ids: string[], status: 'closed' | 'open', reason: string) {
  if (!ids.length) return
  reviewBusy.value = true
  reviewResult.value = null
  try {
    const res = await bulk(id, ids, status, reason)
    const failed = res.data.filter((r) => !r.ok)
    reviewResult.value = failed.length
      ? `${res.applied} of ${res.total} applied. ${failed.map((f) => f.code || f.id).join(', ')} did not.`
      : null
    if (!failed.length) {
      reviewOpen.value = false
      pushToast({
        variant: 'success',
        message: status === 'closed'
          ? `Accepted ${res.applied} ${res.applied === 1 ? 'mark' : 'marks'}.`
          : `Sent ${res.applied} back to the agent.`,
      })
    }
    await load()
  } catch (e: any) {
    reviewResult.value = e?.data?.error || e?.message || 'The batch failed'
  } finally {
    reviewBusy.value = false
  }
}

// A pass finishing while somebody is looking at the queue is exactly when the
// queue must not be stale.
//
// Through useResearchRealtime, not the raw subscription: it scopes to this
// research (an event from any other one was reloading this page) and coalesces
// a burst, which matters here more than anywhere — `Bulk` emits one event per
// row, so accepting fourteen marks fired fourteen reloads.
useResearchRealtime(
  () => id,
  (event) => { if (event.entity === 'annotation' && !isSelf(event)) load(true) },
  { researchId: () => research.value?.id, onResync: () => load(true) },
)

onMounted(load)
</script>

<style scoped>
.marks-page {
  display: flex;
  flex-direction: column;
  gap: var(--space-4);
}

.marks-orphans {
  display: flex;
  align-items: center;
  gap: var(--space-3);
  flex-wrap: wrap;
  padding: var(--space-3);
  border-radius: var(--radius);
  background: rgba(var(--color-warning-rgb), 0.10);
  color: var(--color-warning);
  font-size: var(--type-sm);
}
</style>
