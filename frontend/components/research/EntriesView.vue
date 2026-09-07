<template>
  <div>
    <!-- One live region for the tag filter, mounted for the life of the pane
         and its text swapped — a `role` on an element that appears when it
         should speak is announced unreliably. Same pattern as the import line
         below and as ToastHost. -->
    <p class="sr-only" role="status">{{ filterAnnouncement }}</p>

    <!-- The header names the thing; the toolbar under it filters it. -->
    <div v-if="mode === 'all'" class="section-header">
      <h2 class="section-title">All documents</h2>
    </div>

    <template v-else-if="sectionInfo">
      <div class="section-header">
        <!-- Grouped, because `space-between` over three loose children put the
             status badge 292px from the title it describes on a section that
             declares no fields — there was nothing else to absorb the row. -->
        <div class="section-identity">
        <h2 class="section-title">{{ sectionInfo.display_name || sectionInfo.name }}</h2>
        <StatusBadge :status="sectionInfo.status" />
        <!-- Offered only where there is something to tabulate. A toggle over a
             table with one column would be a control that promises more than
             the section declares. -->
        <NuxtLink
          v-if="fieldSpec.length"
          :to="`/research/${researchSlug}/settings?tab=sections#fields-${sectionInfo.code || sectionInfo.id}`"
          class="section-fields-link"
        >{{ fieldSpec.length }} fields</NuxtLink>
        <SegmentedToggle
          v-if="fieldSpec.length"
          :model-value="view"
          :options="viewOptions"
          label="Section view"
          class="section-view-toggle"
          @update:model-value="view = $event as 'cards' | 'table'"
        />
        </div>
        <!-- Removed for a viewer rather than disabled: the house rule, and the
             drop handler still runs for them so a dropped file cannot navigate
             the browser away from the app. -->
        <button v-if="canImport" type="button" class="btn btn-sm section-import" @click="dropZone?.open()">
          <svg viewBox="0 0 24 24" width="14" height="14" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M12 15V3M7 8l5-5 5 5M4 17v2a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2v-2"/></svg>
          Import .md
        </button>
      </div>
      <p v-if="sectionInfo.description" class="card-meta mb-4">
        {{ sectionInfo.description }}
      </p>

      <!-- The section's own writing convention, above the documents written
           under it. Absent on a share — the payload is redacted there too, and
           this is the second defence rather than the first — and absent, with
           no placeholder row, on the sections that carry none, which is most
           of them. -->
      <ResearchSectionInstruction
        v-if="sectionInfo.instruction && !shareActive() && query.length <= 1"
        :text="sectionInfo.instruction"
        :research-slug="researchSlug"
        :section-id="sectionInfo.id"
        :section-code="sectionInfo.code"
        :editable="canWrite"
      />

      <p v-if="importer.refusal.value" class="inline-error import-refusal">
        <span>{{ importer.refusal.value.message }}</span>
        <button
          v-if="importer.refusal.value.retryable && importer.file.value"
          type="button"
          class="btn btn-sm"
          @click="importer.reread()"
        >Try again</button>
        <button type="button" class="btn btn-sm" @click="importer.dismissRefusal()">Dismiss</button>
      </p>
      <!-- Mounted always, text swapped — a `role` on an element that does not
           exist until it should speak is announced unreliably. Same pattern as
           ToastHost. -->
      <p class="sr-only" role="status">{{ importAnnouncement }}</p>
    </template>

    <!-- Search and tag filter, one row. Hidden over an empty surface: a search
         box above nothing is furniture, and the EmptyState says what to do. -->
    <ResearchEntriesToolbar
      v-if="showToolbar"
      ref="toolbar"
      v-model="activeTag"
      v-model:query="query"
      :tags="toolbarTags"
      :search-label="shareActive() ? 'Filter documents' : `Search ${researchName || 'this project'}`"
      :search-placeholder="shareActive() ? 'Filter documents…' : 'Search this project…'"
    >
      <template #meta>
        <span v-if="query.length === 1" class="card-meta">Keep typing…</span>
        <span v-else-if="query.length > 1" class="card-meta">
          {{ searching ? 'Searching…' : matchesLabel }}
        </span>
      </template>
    </ResearchEntriesToolbar>

    <!-- The drop zone wraps everything below the toolbar, in both modes and
         whether or not a search stands. It is inert in all-entries mode
         (`enabled` false — its own contract names that case), but mounted, so
         a dropped file cannot navigate the browser away wherever it lands, and
         the header's Import button — rendered whenever the person may import —
         always has a picker to open. It used to unmount during a search, and
         the button then did nothing. -->
    <ResearchImportDropZone
      ref="dropZone"
      :enabled="canImport"
      :section-name="sectionInfo?.display_name || sectionInfo?.name || ''"
      :max-bytes="maxBytes"
      :extensions="extensions"
      :reading="importer.phase.value === 'reading'"
      @file="importer.read"
      @refuse="onRefuse"
    >
    <!-- Results replace the list while a query stands. The active tag filters
         them too: two filters that compose is the only behaviour nobody has to
         learn a rule for. -->
    <div v-if="query.length > 1">
      <!-- The search is research-wide and the section header now stays on
           screen above the results; saying nothing would be a lie of layout. -->
      <p v-if="mode === 'section' && !shareActive()" class="card-meta mb-4">
        Results from all sections in this project.
      </p>
      <!-- A failed request is not "no results". It used to be rendered as one,
           which told the reader their query was wrong when the network was. -->
      <EmptyState
        v-if="searchError"
        icon="&#x26A0;&#xFE0F;"
        title="The search did not go through"
        :description="searchError"
      >
        <button type="button" class="btn btn-sm" @click="retrySearch">Try again</button>
      </EmptyState>
      <div v-else-if="foundFiltered.length" class="grid grid-2">
        <EntryCard
          v-for="entry in foundFiltered"
          :key="entry.id"
          :entry="entry"
          :research-slug="researchSlug"
          :missing-required="entry.metadata_status?.missing_required?.length ?? 0"
          :update="updates?.[entry.id]"
        />
      </div>
      <EmptyState
        v-else-if="!searching && activeTag && found.length"
        icon="&#x1F50D;"
        :title="`No matches with “${activeTag}”`"
        :description="`${found.length} ${found.length === 1 ? 'document matches' : 'documents match'} “${query}”, but none of them carry this tag.`"
      >
        <button type="button" class="btn btn-sm" @click="activeTag = ''">Clear filter</button>
        <button type="button" class="btn btn-sm" @click="query = ''">Clear search</button>
      </EmptyState>
      <EmptyState
        v-else-if="!searching"
        icon="&#x1F50D;"
        :title="`Nothing matches “${query}”`"
        :description="searchScope"
      />
    </div>

    <!-- All entries view -->
    <template v-else-if="mode === 'all'">
      <!-- Loading -->
      <div v-if="loading">
        <div v-for="i in 3" :key="i" class="skeleton-card skeleton-entry"></div>
      </div>

      <template v-else-if="filteredEntries.length">
        <!-- Group by section -->
        <template v-for="group in groupedEntries" :key="group.section.id">
          <h3 class="group-section-title">{{ group.section.display_name || group.section.name }}</h3>
          <div class="grid entries-grid mb-4">
            <EntryCard
              v-for="entry in group.entries"
              :key="entry.id"
              :entry="entry"
              :research-slug="researchSlug"
              :missing-required="missingIn(group.section, entry)"
              :update="updates?.[entry.id]"
            />
          </div>
        </template>
      </template>

      <!-- A tag can vanish from under an active filter — a realtime update
           removed the last entry carrying it. The filter is never cleared on
           the user's behalf; the state says so and offers the way out. -->
      <EmptyState
        v-else-if="activeTag"
        icon="&#x1F4C4;"
        :title="`No documents tagged “${activeTag}”`"
        :description="`The filter is still on. Clear it to see the other ${entries.length} ${entries.length === 1 ? 'document' : 'documents'}.`"
      >
        <button type="button" class="btn btn-sm" @click="activeTag = ''">Clear filter</button>
      </EmptyState>

      <EmptyState
        v-else
        icon="&#x1F4C4;"
        title="No documents yet"
        :description="emptyDescription('research')"
      />
    </template>

    <!-- Single section view -->
    <template v-else-if="sectionInfo">
      <!-- Entries loading -->
      <div v-if="loading">
        <div v-for="i in 3" :key="i" class="skeleton-card skeleton-entry"></div>
      </div>

      <!-- The table is the point of declaring fields at all: a blank cell beside
           filled ones is the strongest force there is on whether an optional
           field ever gets answered. -->
      <!-- Gated on the declaration, not only on the toggle: the component
           instance is reused across sections, so a section with no fields
           inherited the table and had no control to leave it with. -->
      <div v-else-if="view === 'table' && fieldSpec.length && filteredEntries.length" class="meta-table-wrap">
        <table class="meta-table">
          <thead>
            <tr>
              <th scope="col">Document</th>
              <th scope="col">Status</th>
              <th v-for="f in fieldSpec" :key="f.key" scope="col" :title="f.help || undefined">
                {{ fieldLabel(f) }}<span v-if="f.required" class="meta-req" aria-label="required">*</span>
              </th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="entry in filteredEntries" :key="entry.id">
              <th scope="row" class="meta-table-doc">
                <NuxtLink :to="entryPath(researchSlug, entry.code || entry.id)">
                  <EntryUpdateBadge
                    v-if="updates?.[entry.id]"
                    :kind="updates[entry.id]!.kind"
                    :unseen-revisions="updates[entry.id]!.unseen_revisions"
                  />
                  <span v-if="entry.code" class="short-code">{{ entry.code }}</span>
                  {{ entry.title }}
                </NuxtLink>
              </th>
              <td><StatusBadge :status="entry.status" /></td>
              <!-- A blank required cell and a blank optional one used to read
                   the same, on the one screen built to make gaps visible. So
                   did a value outside its vocabulary and a legitimate one. -->
              <td
                v-for="f in fieldSpec"
                :key="f.key"
                :class="{
                  'is-empty': !isFilled(entry.metadata?.[f.key]),
                  'is-missing': f.required && !isFilled(entry.metadata?.[f.key]),
                  'is-invalid': hasIssue(entry, f.key),
                }"
                :title="issueReason(entry, f.key) || undefined"
              >
                <template v-if="isUnknown(entry.metadata?.[f.key])">unknown</template>
                <template v-else-if="isFilled(entry.metadata?.[f.key])">
                  <!-- The same renderer the document's own block uses. Printing
                       the raw string here would show `[[E47]]` in the table and
                       a live link on the page, for one value. -->
                  <span v-html="renderRefs(displayValue(f, entry.metadata?.[f.key]), researchSlug)"></span>
                </template>
                <template v-else>&mdash;</template>
              </td>
            </tr>
          </tbody>
        </table>
      </div>

      <div v-else-if="filteredEntries.length" class="grid entries-grid">
        <EntryCard
          v-for="entry in filteredEntries"
          :key="entry.id"
          :entry="entry"
          :research-slug="researchSlug"
          :missing-required="missingCount(entry)"
          :update="updates?.[entry.id]"
        />
      </div>

      <EmptyState
        v-else-if="activeTag"
        icon="&#x1F4C4;"
        :title="`No documents tagged “${activeTag}”`"
        :description="`The filter is still on. Clear it to see the other ${entries.length} ${entries.length === 1 ? 'document' : 'documents'}.`"
      >
        <button type="button" class="btn btn-sm" @click="activeTag = ''">Clear filter</button>
      </EmptyState>

      <EmptyState
        v-else
        icon="&#x1F4C4;"
        :title="shareActive() ? 'No documents in this section' : 'No documents yet'"
        :description="emptyDescription('section')"
      >
        <button v-if="canImport" type="button" class="btn btn-sm" @click="dropZone?.open()">
          Import .md
        </button>
      </EmptyState>
    </template>
    </ResearchImportDropZone>

    <ResearchImportPreviewDialog
      v-if="sectionInfo"
      :visible="importer.phase.value === 'previewing' || importer.phase.value === 'committing'"
      :file-bytes="importer.file.value?.size ?? 0"
      :section-name="sectionInfo.display_name || sectionInfo.name"
      :preview="importer.preview.value"
      :committing="importer.phase.value === 'committing'"
      :error="importer.commitError.value"
      :stale-spec="importer.staleSpec.value"
      @commit="onCommit"
      @reread="importer.reread"
      @close="importer.reset"
    />
  </div>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue'
import { renderRefs } from '~/composables/useCrossRefs'
import { entryPath } from '~/composables/useResearchPaths'
import { shareActive } from '~/composables/useShare'
import { displayValue, fieldLabel, isFilled, isUnknown, missingRequired } from '~/composables/useFieldSpec'
import { useSectionImport } from '~/composables/useSectionImport'
import { useMetadataSchema } from '~/composables/useMetadataSchema'
import type { EntryUpdate } from '~/composables/useEntryUpdates'

const props = defineProps<{
  entries: any[]
  researchId?: string
  researchName?: string
  sections: any[]
  researchSlug: string
  loading: boolean
  mode: 'all' | 'section'
  sectionInfo?: any
  tags: Array<{ tag: string; count: number }>
  updates?: Record<string, EntryUpdate>
}>()

/* What this section declares, and the two things every surface needs from it.
   The predicate lives in the composable rather than here, because a document
   reading "complete" in the table and "1 missing" on its own page is the same
   class of bug as a stored status disagreeing with the prose. */
const fieldSpec = computed<any[]>(() => props.sectionInfo?.field_spec ?? [])
const view = ref<'cards' | 'table'>('cards')

/* Dropping a markdown file into this section.
   The state machine is in the composable: this component already owns the
   search, the tag filter, the view toggle and the grouping. */
const emit = defineEmits<{ imported: [{ id: string; code: string }] }>()

const dropZone = ref<{ open: () => void } | null>(null)
const sectionId = computed(() => props.sectionInfo?.id ?? '')
const importer = useSectionImport(sectionId)
const { maxBytes, extensions, load: loadCaps } = useMetadataSchema()
const { canWrite } = useResearchRole()
const toasts = useToasts()

// Never on a share, never for a viewer, and never in the all-entries mode where
// there is no one section to drop onto.
const canImport = computed(() => props.mode === 'section' && !shareActive() && canWrite.value)

onMounted(() => {
  if (canImport.value) loadCaps()
})

// A file half-read into one section must not survive a move to another.
watch(sectionId, () => importer.reset())

// The section's declaration can change under an open dialog: the page reloads
// the research on any `section` event, and what the preview was validated
// against is then no longer what the commit will be checked by. Watching the
// spec here rather than being told by the page keeps the knowledge where the
// dialog is.
watch(
  () => JSON.stringify(fieldSpec.value),
  (next, prev) => {
    if (next !== prev) importer.markSpecStale()
  },
)

function onRefuse(r: { message: string; retryable: boolean }) {
  importer.refusal.value = r
}

// One live region, mounted for the life of the pane, whose text changes.
const importAnnouncement = computed(() => {
  if (importer.refusal.value) return importer.refusal.value.message
  if (importer.phase.value === 'reading') return `Reading ${importer.file.value?.name ?? 'the file'}…`
  if (importer.phase.value === 'previewing') return 'The file was read. Review it before creating the document.'
  return ''
})

async function onCommit(overrides: { title: string }) {
  const created = await importer.commit(overrides)
  if (!created) return
  toasts.push({
    variant: 'success',
    message: `${created.code} created from the file.`,
    action: {
      label: `Open ${created.code}`,
      onClick: () => {
        navigateTo(entryPath(props.researchSlug, created.code || created.id))
      },
    },
  })
  // The entries list and the sidebar counts belong to the page.
  // Deliberately staying in the section rather than navigating to the entry:
  // multi-file import is out of scope, so importing five documents is five
  // rounds of this, and leaving the page each time makes that loop miserable.
  emit('imported', created)
}

const viewOptions = [
  { value: 'cards', label: 'Cards' },
  { value: 'table', label: 'Table' },
]

function missingCount(entry: any): number {
  return missingIn(props.sectionInfo, entry)
}

/* The all-entries list groups by section, so it holds the declaration for each
   group and can show the same gap. Leaving it out would have hidden the signal
   on the surface people actually browse a research from. */
function hasIssue(entry: any, key: string): boolean {
  return !!issueReason(entry, key)
}

/* Recomputed on the server and carried on the list payload, not derived here:
   enum membership, date parsing and URL schemes are the server's judgement, and
   a second copy in TypeScript is how the two come to disagree. */
function issueReason(entry: any, key: string): string {
  return entry?.metadata_status?.issues?.find((i: any) => i.key === key)?.reason ?? ''
}

function missingIn(section: any, entry: any): number {
  const specs = section?.field_spec ?? []
  if (!specs.length) return 0
  return missingRequired(specs, entry.metadata ?? {}).length
}

/* The search is the component's own: it holds the query, so nothing above it
   has to thread state through, and it asks the server rather than filtering
   `entries`, because the body is where the answer usually is and the body is
   not in the payload this component is handed. */
const query = ref('')
const searching = ref(false)
const fetched = ref<any[]>([])
const searchError = ref('')

/* The server caps a search at twenty (`server.go`, `/api/search`) and returns
   no total, so twenty is the one count that cannot be read as exact. */
const SEARCH_CAP = 20

/* `/api/search` is not on the share sub-mux, and a visitor has no research id
   to scope it by — so a shared page used to fire an unscoped, whole-database
   query the moment two characters were typed. A visitor gets the entries the
   page already holds, filtered by title, description and tags. It searches less
   (no bodies), and the placeholder says so. */
function localMatches(q: string): any[] {
  const needle = q.toLowerCase()
  return props.entries.filter((e: any) =>
    [e.title, e.description, ...(e.tags ?? [])].some(
      (s: unknown) => typeof s === 'string' && s.toLowerCase().includes(needle),
    ),
  )
}
const found = computed<any[]>(() => {
  if (query.value.trim().length < 2) return []
  return shareActive() ? localMatches(query.value.trim()) : fetched.value
})
const foundFiltered = computed(() =>
  activeTag.value ? found.value.filter((e: any) => e.tags?.includes(activeTag.value)) : found.value,
)

const matchesLabel = computed(() => {
  const total = found.value.length
  const capped = !shareActive() && total === SEARCH_CAP
  const totalText = capped ? `${SEARCH_CAP}+` : String(total)
  if (activeTag.value && foundFiltered.value.length !== total) {
    return `${foundFiltered.value.length} of ${totalText} matches`
  }
  return `${totalText} ${total === 1 ? 'match' : 'matches'}`
})

const searchScope = computed(() =>
  shareActive()
    ? 'Titles, descriptions and tags of the documents on this page were searched.'
    : 'Search covers document titles, descriptions, and content across this project.',
)
const { authFetch } = useAuth()
const base = useRuntimeConfig().public.apiBase || ''

let seq = 0
let timer: ReturnType<typeof setTimeout> | null = null

async function run(q: string) {
  const mine = ++seq
  searching.value = true
  searchError.value = ''
  try {
    const res = await authFetch<{ entries: any[] }>(
      `${base}/api/search?q=${encodeURIComponent(q)}&research=${encodeURIComponent(props.researchId ?? '')}`,
    )
    // An earlier request that lands late must not overwrite a later one.
    if (mine === seq) fetched.value = res?.entries ?? []
  } catch (e: any) {
    if (mine === seq) {
      fetched.value = []
      searchError.value = e?.data?.error || e?.message || 'The server could not be reached.'
    }
  } finally {
    if (mine === seq) searching.value = false
  }
}

/* Debounced by hand rather than by a dependency: a request per keystroke is
   what the global palette still does, and typing "authentication" there fires
   fourteen. */
watch(query, (q) => {
  if (timer) clearTimeout(timer)
  searchError.value = ''
  if (q.trim().length < 2 || shareActive()) { fetched.value = []; searching.value = false; return }
  timer = setTimeout(() => run(q.trim()), 200)
})

function retrySearch() {
  if (query.value.trim().length >= 2) run(query.value.trim())
}

onUnmounted(() => { if (timer) clearTimeout(timer) })

/**
 * What to say about an empty list.
 *
 * "Claude will populate this section with research entries" is true and useful
 * to the person running the research, and meaningless to a client reading a
 * shared link — it describes a tool they have never heard of doing work they
 * have no part in.
 */
function emptyDescription(scope: 'research' | 'section') {
  if (shareActive()) return "There's nothing here yet."
  if (scope === 'research') return 'Ask your AI assistant to add documents to this project.'
  return canImport.value
    ? 'Ask your AI assistant to add documents to this section, or drop a Markdown file here.'
    : 'Documents added to this section will appear here.'
}

const activeTag = ref('')
const toolbar = ref<{ close: () => void } | null>(null)

// A filter belongs to the surface it was set on. Another section or another
// mode is another surface, and a popover left open over it is a list of tags
// the new surface does not have. The query goes too: the header now stays on
// screen during a search, and section B's title over results typed on section
// A is a screen contradicting itself.
//
// Two sources compared one by one, not a getter returning an array. The array
// form is a new value on every evaluation, and `sectionInfo` is a new object on
// every realtime reload — which cleared the filter under the reader's hands
// each time an agent wrote an entry anywhere in the research.
watch([() => props.mode, () => props.sectionInfo?.id], () => {
  activeTag.value = ''
  query.value = ''
  toolbar.value?.close()
})

/* The tags the toolbar offers. All-entries mode gets the research-wide counts
   from the endpoint; a section gets counts over its own entries. The same tag
   legitimately shows two numbers on the two surfaces — each is right for the
   list it sits above. */
const toolbarTags = computed(() => (props.mode === 'all' ? props.tags : sectionTags.value))

// While loading the input is already useful and the row's height is its own,
// so chips landing later shift nothing. Over a surface with no entries and no
// filter standing there is nothing to search and nothing to clear.
const showToolbar = computed(
  () => props.loading || props.entries.length > 0 || query.value.length > 0 || !!activeTag.value,
)

/* One live region for the whole pane, spoken once per change with the count
   that resulted. Set after the DOM settles so the number is the one on screen.
   The visible counter beside the input carries no `role`: a second region
   saying the same number is two utterances, queued or clobbered depending on
   the reader. */
const filterAnnouncement = ref('')
watch(activeTag, (tag, prev) => {
  if (!tag && !prev) return
  const n = query.value.length > 1
    ? matchesLabel.value
    : `${filteredEntries.value.length} ${filteredEntries.value.length === 1 ? 'document' : 'documents'}`
  filterAnnouncement.value = tag ? `Filtered to “${tag}”. ${n}.` : `Tag filter cleared. ${n}.`
}, { flush: 'post' })
watch([found, searching, searchError], () => {
  if (query.value.length < 2 || searching.value) return
  filterAnnouncement.value = searchError.value
    ? 'The search did not go through.'
    : `${matchesLabel.value} for “${query.value}”.`
}, { flush: 'post' })

// Compute section-level tag counts from entries
const sectionTags = computed(() => {
  if (props.mode !== 'section') return []
  const map = new Map<string, number>()
  for (const e of props.entries) {
    for (const t of (e.tags ?? [])) {
      map.set(t, (map.get(t) || 0) + 1)
    }
  }
  return [...map.entries()]
    .sort((a, b) => b[1] - a[1] || a[0].localeCompare(b[0]))
    .map(([tag, count]) => ({ tag, count }))
})

const filteredEntries = computed(() =>
  activeTag.value ? props.entries.filter((e: any) => e.tags?.includes(activeTag.value)) : props.entries
)

// Group entries by section for the all-entries view
const groupedEntries = computed(() => {
  if (props.mode !== 'all') return []
  const groups: { section: any; entries: any[] }[] = []
  const sectionMap = new Map<string, any[]>()

  for (const entry of filteredEntries.value) {
    const list = sectionMap.get(entry.section_id) ?? []
    list.push(entry)
    sectionMap.set(entry.section_id, list)
  }

  for (const section of props.sections) {
    const sectionEntries = sectionMap.get(section.id)
    if (sectionEntries?.length) {
      groups.push({ section, entries: sectionEntries })
    }
  }

  return groups
})
</script>

<style scoped>
/* Wrapping, because a fourth control overflows the row at the widths this pane
   actually gets. */
.section-header { display: flex; flex-wrap: wrap; justify-content: space-between; align-items: center; gap: var(--space-3); margin-bottom: var(--space-4); }
/* `.btn-sm` already declares `min-height: var(--control-h-sm)`, so the row
   cannot come out at three heights; this only adds the icon layout. */
.section-import { display: inline-flex; align-items: center; gap: var(--space-2); }

.section-identity { display: flex; flex-wrap: wrap; align-items: center; gap: var(--space-3); }
/* Above the list, beside the control that caused it. Below it was two screens
   away on a section of any size. */
.import-refusal {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: var(--space-3);
  margin-bottom: var(--space-4);
}
.import-refusal span { min-width: 0; overflow-wrap: anywhere; }
.import-refusal .btn { flex: none; }
.section-title { font-size: var(--type-xl); font-weight: var(--weight-semibold); letter-spacing: -0.02em; }
.group-section-title {
  font-size: var(--type-base);
  font-weight: var(--weight-semibold);
  color: var(--color-text-muted);
  margin-bottom: var(--space-2);
  padding-bottom: var(--space-2);
  border-bottom: 1px solid var(--color-border);
}
.entries-grid { grid-template-columns: 1fr; }
.short-code {
  font-size: var(--type-xs);
  font-weight: var(--weight-semibold);
  color: var(--color-primary);
  background: var(--color-primary-muted);
  padding: 0.15rem 0.4rem;
  border-radius: var(--radius-xs);
  font-family: 'JetBrains Mono', monospace;
  flex-shrink: 0;
  line-height: 1;
}
.skeleton-entry { height: 90px; margin-bottom: var(--space-3); }

/* No auto margins here. `.section-header` is space-between with no gap, and an
   auto margin is resolved first — it absorbed all the free space, so the title
   and the status badge packed flush together and a section that declares fields
   looked broken next to one that does not. */
.section-header { gap: var(--space-3); }
.section-view-toggle { margin-left: auto; }
.section-fields-link {
  font-size: var(--type-xs);
  color: var(--color-text-muted);
  white-space: nowrap;
}
.section-fields-link:hover { color: var(--color-primary); }

.meta-table-wrap {
  /* Without this the wrapper takes its min-content width from the table and
     blows out the `1fr` grid track it sits in, which scrolls the page body
     rather than the table. */
  min-width: 0;
  max-width: 100%;
  border: 1px solid var(--color-border);
  border-radius: var(--radius);
  /* The page body must never scroll sideways; a wide table scrolls inside its
     own frame instead. */
  overflow-x: auto;
}

.meta-table { width: 100%; border-collapse: collapse; font-size: var(--type-sm); }
.meta-table th,
.meta-table td {
  padding: var(--space-2) var(--space-3);
  text-align: left;
  vertical-align: top;
  white-space: nowrap;
  border-bottom: 1px solid var(--color-border);
}
.meta-table thead th {
  font-size: var(--type-xs);
  font-weight: var(--weight-semibold);
  color: var(--color-text-muted);
}
.meta-table tbody tr:last-child th,
.meta-table tbody tr:last-child td { border-bottom: 0; }

.meta-table-doc { font-weight: var(--weight-normal); max-width: 22rem; white-space: normal; }
.meta-table-doc a {
  display: inline-flex;
  flex-wrap: wrap;
  align-items: center;
  gap: var(--space-2);
  color: inherit;
  text-decoration: none;
}
.meta-table-doc a:hover { text-decoration: underline; }

.meta-table td.is-empty { color: var(--color-text-faint); }
/* A required field nobody answered, and a value that is not one of the declared
   options. Both carry a word or a mark, never colour alone. */
.meta-table td.is-missing { color: var(--color-warning); }
.meta-table td.is-invalid { color: var(--color-danger); text-decoration: underline dotted; }
.meta-req { color: var(--color-warning); margin-left: 0.15em; }

</style>
