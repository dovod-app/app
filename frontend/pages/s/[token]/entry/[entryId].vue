<template>
  <div v-if="pending">
    <div class="skeleton-card skeleton-header"></div>
    <div class="skeleton-card skeleton-content"></div>
  </div>

  <EmptyState
    v-else-if="!entry && gone"
    title="This document was removed"
    description="It was deleted while you were reading. The rest of the project is still here."
  >
    <NuxtLink class="btn btn-primary" :to="researchPath(slug)">Back to project</NuxtLink>
  </EmptyState>

  <EmptyState
    v-else-if="!entry"
    title="Couldn't load this document"
    description="The server didn't answer. The rest of the project is still here — try again in a moment."
  >
    <button class="btn btn-primary" @click="loadEntry()">Try again</button>
  </EmptyState>

  <div v-else>
    <div class="page-header">
      <Breadcrumbs :crumbs="[
        { label: researchName, to: researchPath(slug) },
        { label: sectionName, to: `${researchPath(slug)}?section=${entry.section_id}` },
        { label: entry.title },
      ]" />

      <PageHeader :code="entry.code" :title="entry.title">
        <template #actions>
          <StatusBadge :status="entry.status" />
        </template>
      </PageHeader>

      <p v-if="entry.description" class="card-meta mt-2" v-html="renderRefs(entry.description, slug)"></p>
      <div v-if="entry.tags?.length" class="entry-tags">
        <span v-for="tag in entry.tags" :key="tag" :class="['tag', `tag-hue-${tagHue(tag)}`]">{{ tag }}</span>
      </div>
    </div>

    <div class="entry-content card" :class="{ 'is-artifact': isArtifactOnly }">
      <BlocksBlockRenderer
        v-if="isBlocks"
        :blocks="blocks"
        :research-slug="slug"
        :bridge-data="null"
        :entry-id="entry.id"
        :tasks="tasks"
        :tasks-status="tasksStatus"
        @retry-tasks="loadTasks"
      />
      <div v-else ref="contentEl" class="markdown-content" v-html="renderedContent"></div>
    </div>

    <EntryCrossReferencesBlock
      :outgoing="outgoingRefs"
      :incoming="incomingRefs"
      :research-slug="slug"
    />

    <EntryExternalLinksBlock :links="externalLinks" />

    <EntryRelatedEntriesBlock
      :entries="relatedEntries"
      :current-tags="entry.tags ?? []"
      :research-slug="slug"
      :research-id="researchId"
    />

    <EntryNavigation
      v-if="siblings.length > 1"
      :prev="prevEntry"
      :next="nextEntry"
      :research-slug="slug"
    />
  </div>
</template>

<script setup lang="ts">
/**
 * One entry, as a visitor sees it.
 *
 * This is a separate page from the owner's rather than a mode of it, and that
 * is the first of the two read-only defences: the editor, the delete
 * confirmation, the status picker and the revision history are not imported
 * here, so no flag can make them appear. Revision history is left out on
 * purpose — who edited what and when is internal working process, of a piece
 * with `instruction` and `memory`.
 */
import { parseMarkdown } from '~/composables/useSafeMarkdown'
import { tagHue } from '~/composables/useTagHue'

const route = useRoute()
const entryCode = computed(() => route.params.entryId as string)

const { shareFetch, research, sections, researchId, researchCode, slug, include } = useShare()
const researchName = computed(() => research.value?.name || 'Project')

const entry = ref<any | null>(null)
const pending = ref(true)
/** The server said it is not there, as opposed to not answering at all. */
const gone = ref(false)
const outgoingRefs = ref<any[]>([])
const incomingRefs = ref<any[]>([])
const externalLinks = ref<any[]>([])
const relatedEntries = ref<any[]>([])
const siblings = ref<any[]>([])

const sectionName = computed(() => {
  const s = sections.value.find((x: any) => x.id === entry.value?.section_id)
  return s?.display_name || s?.name || 'Section'
})

async function loadEntry() {
  pending.value = true
  // The previous entry's siblings are not this entry's siblings; leaving them
  // in place renders a navigation bar belonging to the section just left.
  siblings.value = []
  try {
    const res = await shareFetch<{ data: any }>(
      `/researches/${researchId.value}/entries/${entryCode.value}`,
    )
    entry.value = res.data
    gone.value = false
  } catch (e: any) {
    const status = e?.response?.status ?? e?.statusCode
    gone.value = status === 404
    entry.value = null
    pending.value = false
    return
  }
  pending.value = false
  await loadContext()
}

/**
 * The blocks around the entry — references, links, siblings.
 *
 * Each is allowed to fail on its own: a missing "related by tags" is a missing
 * panel, and it must not take the entry down with it.
 */
async function loadContext() {
  const id = entry.value?.id
  if (!id) return
  const [refs, links, related, sectionEntries] = await Promise.allSettled([
    shareFetch<{ outgoing: any[]; incoming: any[] }>(`/entries/${id}/crossrefs`),
    shareFetch<{ data: any[] }>(`/entries/${id}/links`),
    shareFetch<{ data: any[] }>(`/entries/${id}/related`),
    shareFetch<{ data: any[] }>(
      `/researches/${researchId.value}/sections/${entry.value.section_id}/entries`,
    ),
  ])
  outgoingRefs.value = refs.status === 'fulfilled' ? (refs.value.outgoing ?? []) : []
  incomingRefs.value = refs.status === 'fulfilled' ? (refs.value.incoming ?? []) : []
  externalLinks.value = links.status === 'fulfilled' ? (links.value.data ?? []) : []
  relatedEntries.value = related.status === 'fulfilled' ? (related.value.data ?? []) : []
  siblings.value = sectionEntries.status === 'fulfilled' ? (sectionEntries.value.data ?? []) : []
}

watch([entryCode, researchId], () => void loadEntry(), { immediate: true })

const currentIndex = computed(() => siblings.value.findIndex((e: any) => e.id === entry.value?.id))
const prevEntry = computed(() => (currentIndex.value > 0 ? siblings.value[currentIndex.value - 1] : null))
const nextEntry = computed(() =>
  currentIndex.value >= 0 && currentIndex.value < siblings.value.length - 1
    ? siblings.value[currentIndex.value + 1]
    : null,
)

const isBlocks = computed(() => entry.value?.entry_type === 'blocks' && blocks.value.length > 0)
const blocks = computed<any[]>(() => {
  if (entry.value?.entry_type !== 'blocks' || !entry.value?.content) return []
  try {
    const doc = JSON.parse(entry.value.content)
    return Array.isArray(doc) ? doc : (doc.blocks ?? [])
  } catch {
    return []
  }
})

// A document that is nothing but one artifact. The card gives up its padding
// for these — see `.entry-content.is-artifact` in system.css — because an
// artifact brings its own margins inside its own frame and ours only makes it
// narrower. Strictly one block: a document with prose around the artifact still
// needs the prose to sit where prose sits.
const isArtifactOnly = computed(
  () => isBlocks.value && blocks.value.length === 1 && blocks.value[0]?.type === 'html',
)

/* ------------------------------------------------------------ task_ref ---
 *
 * The same resolution the owner's page does, through the share prefix and with
 * one extra outcome: a link created without tasks has no route to ask, so the
 * block is told `excluded` and says so, rather than retrying a 404 forever.
 *
 * Nothing is ticked here — `shareLocked` turns every checkbox off — so this is
 * a read and only a read.
 */
const tasks = ref<any[]>([])
const tasksStatus = ref<'idle' | 'loading' | 'ready' | 'error' | 'excluded'>('idle')
const hasTaskRef = computed(() => blocks.value.some((b: any) => b?.type === 'task_ref'))

async function loadTasks() {
  if (!hasTaskRef.value) return
  if (!include.value?.tasks) {
    tasksStatus.value = 'excluded'
    return
  }
  // 'error' counts as a first load: a reader who pressed Try again is owed
  // some sign that something is happening.
  if (tasksStatus.value === 'idle' || tasksStatus.value === 'error') tasksStatus.value = 'loading'
  try {
    const res = await shareFetch<{ data: any[] }>(`/researches/${researchId.value}/tasks`)
    tasks.value = res.data ?? []
    tasksStatus.value = 'ready'
  } catch (e: any) {
    // The route is gated by the same flag `include` reports, so a 404 here means
    // the link does not publish tasks — the visitor should be told that, not
    // offered a retry that cannot succeed.
    const status = e?.response?.status ?? e?.statusCode
    tasksStatus.value = status === 404 ? 'excluded' : 'error'
  }
}

watch([hasTaskRef, researchId], () => {
  if (hasTaskRef.value && tasksStatus.value === 'idle') void loadTasks()
}, { immediate: true })


const contentEl = ref<HTMLElement | null>(null)
const renderedContent = computed(() => {
  if (!entry.value?.content) return ''
  const html = parseMarkdown(String(entry.value.content).replace(/\\n/g, '\n')) as string
  return linkRefs(html, slug.value)
})

watch(renderedContent, () => {
  nextTick(() => { if (contentEl.value) renderMermaidBlocks(contentEl.value) })
})
onMounted(() => { if (contentEl.value) renderMermaidBlocks(contentEl.value) })

useResearchRealtime(
  () => slug.value,
  (event) => {
    // `crossref` stays in the list for a rebuild the owner runs, but a share no
    // longer receives that entity — the event fires on task and roadmap creates
    // too, so it was telling a visitor whose link excludes those that one had
    // just appeared. `task` and `roadmap` take its place here: they are gated
    // by the link's own include flags, so this repaints exactly when the
    // visitor is entitled to know.
    if (['entry', 'crossref', 'task', 'roadmap'].includes(event.entity)) void loadEntry()
  },
  { onResync: () => void loadEntry(), researchId: () => researchId.value },
)
</script>
