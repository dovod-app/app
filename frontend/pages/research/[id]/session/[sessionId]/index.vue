<template>
  <div v-if="pending">
    <div class="skeleton-card skeleton-header"></div>
    <div class="skeleton-card skeleton-content"></div>
  </div>

  <div v-else-if="session">
    <!-- Header -->
    <div class="page-header">
      <Breadcrumbs :crumbs="[
        { label: 'Projects', to: { name: 'index' } },
        { label: researchName, to: `/research/${researchSlug}` },
        { label: session.title }
      ]" />
      <div class="session-header">
        <h1 class="page-title">{{ session.title }}</h1>
        <div class="session-header-actions">
          <StatusBadge :status="session.status" />
          <TeamViewerNotice v-if="readOnlyReason" :reason="readOnlyReason" :team-name="researchData?.data?.research?.team_name" />
          <ActionMenu>
            <!-- A run that ends without the agent marking the session
                 `completed` leaves the research showing an active session
                 forever, and the UI offered no way to close it. -->
            <button
              v-if="canWrite && session.status === 'active'"
              class="action-menu-item"
              @click="setSessionStatus('completed')"
            >Mark completed</button>
            <button
              v-if="canWrite && session.status === 'completed'"
              class="action-menu-item"
              @click="setSessionStatus('active')"
            >Reopen</button>
            <button
              v-if="canWrite && session.status !== 'archived'"
              class="action-menu-item"
              @click="setSessionStatus('archived')"
            >Archive</button>
            <div v-if="canWrite" class="action-menu-divider"></div>
            <NuxtLink
              :to="`/research/${researchSlug}/session/${sessionSlug}/export`"
              class="action-menu-item"
            >
              <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4"/><polyline points="7 10 12 15 17 10"/><line x1="12" y1="15" x2="12" y2="3"/></svg>
              Export
            </NuxtLink>
          </ActionMenu>
        </div>
      </div>
      <p v-if="session.focus" class="card-meta mt-2" v-html="'Focus: ' + renderRefs(session.focus, researchSlug)"></p>
    </div>

    <!-- Notes. Closed by default: they are the conductor's working record, worth
         keeping and worth reading on purpose, but they stood between the session
         header and the questions on every visit. <details> rather than a button,
         so the disclosure is the browser's and the content is findable by the
         page's own search. -->
    <details v-if="session.notes" class="card notes-card" @toggle="onNotesToggle">
      <summary class="notes-summary">
        <!-- The native marker is hidden, so the affordance has to be drawn:
             without one, a closed card is a heading with a number beside it and
             nothing saying it opens. -->
        <svg class="notes-chevron" width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><polyline points="9 18 15 12 9 6" /></svg>
        <svg class="notes-icon" width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z" /><polyline points="14 2 14 8 20 8" /><line x1="16" y1="13" x2="8" y2="13" /><line x1="16" y1="17" x2="8" y2="17" /></svg>
        <h3 class="card-section-title">Session notes</h3>
      </summary>
      <div ref="notesEl" class="notes-text markdown-content" v-html="linkRefs(parseMarkdown(normalizeContent(session.notes)) as string, researchSlug)"></div>
    </details>

    <!-- Tabs -->
    <div class="tabs content-tabs" role="tablist" aria-label="Session content">
      <button
        :id="`tab-questions`"
        type="button"
        role="tab"
        :aria-selected="activeTab === 'questions'"
        :aria-controls="`panel-questions`"
        :tabindex="activeTab === 'questions' ? 0 : -1"
        :class="['tab', 'content-tab', { active: activeTab === 'questions' }]"
        @click="selectTab('questions')"
        @keydown="onTabKey($event, 'questions')"
      >
        <svg width="15" height="15" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="10"/><path d="M9.09 9a3 3 0 0 1 5.83 1c0 2-3 3-3 3"/><path d="M12 17h.01"/></svg>
        Questions
        <span class="tab-count">{{ progress.answered }} / {{ progress.total }}</span>
      </button>
      <button
        :id="`tab-entries`"
        type="button"
        role="tab"
        :aria-selected="activeTab === 'entries'"
        :aria-controls="`panel-entries`"
        :tabindex="activeTab === 'entries' ? 0 : -1"
        :class="['tab', 'content-tab', { active: activeTab === 'entries' }]"
        @click="selectTab('entries')"
        @keydown="onTabKey($event, 'entries')"
      >
        <svg width="15" height="15" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z"/><polyline points="14 2 14 8 20 8"/></svg>
        Documents
        <span v-if="sessionEntries.length" class="tab-count">{{ sessionEntries.length }}</span>
      </button>
      <button
        :id="`tab-changes`"
        type="button"
        role="tab"
        :aria-selected="activeTab === 'changes'"
        :aria-controls="`panel-changes`"
        :tabindex="activeTab === 'changes' ? 0 : -1"
        :class="['tab', 'content-tab', { active: activeTab === 'changes' }]"
        @click="selectTab('changes')"
        @keydown="onTabKey($event, 'changes')"
      >
        <svg width="15" height="15" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M3 3v5h5"/><path d="M3.05 13A9 9 0 1 0 6 5.3L3 8"/><path d="M12 7v5l4 2"/></svg>
        Changes
        <span
          v-if="changesUnknown"
          class="tab-count tab-count-unknown"
          title="Could not load what this session changed — open the tab to retry"
        >!</span>
        <span v-else-if="changesCount" class="tab-count">{{ changesCount }}</span>
      </button>
    </div>

    <!-- Questions panel -->
    <div v-if="activeTab === 'questions'" id="panel-questions" role="tabpanel" aria-labelledby="tab-questions" tabindex="0">
      <!-- Framed. Unframed it was a full-width bar sitting a gap below the tab
           strip's own full-width rule — two horizontal lines of the same shape,
           reading as one broken divider rather than as a boundary and a
           measurement. -->
      <div class="card panel-progress">
        <div class="progress-stats">
          <span class="stat-answered">{{ progress.answered }} answered</span>
          <span class="stat-pending">{{ progress.pending }} pending</span>
          <span v-if="progress.deferred" class="stat-muted">{{ progress.deferred }} deferred</span>
          <span v-if="progress.skipped" class="stat-skipped">{{ progress.skipped }} skipped</span>
        </div>
        <ProgressBar :value="progress.answered" :total="progress.total" />
      </div>

      <!-- Add question -->
      <button v-if="canWrite && !showAddQuestion" class="btn btn-sm add-btn" @click="showAddQuestion = true">
        <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><line x1="12" y1="5" x2="12" y2="19"/><line x1="5" y1="12" x2="19" y2="12"/></svg>
        Add question
      </button>
      <form v-else-if="canWrite" class="add-form" @submit.prevent="submitQuestion">
        <input v-model="newQuestion.text" class="add-input" placeholder="Question text..." required autofocus />
        <div class="add-form-row">
          <input v-model="newQuestion.area" class="add-input add-input-sm" placeholder="Area (optional)" />
          <select v-model="newQuestion.priority" class="add-select">
            <option value="medium">Medium</option>
            <option value="high">High</option>
            <option value="low">Low</option>
          </select>
          <button type="submit" class="btn btn-sm" :disabled="addingQuestion || !newQuestion.text.trim()">
            {{ addingQuestion ? 'Adding...' : 'Add' }}
          </button>
          <button type="button" class="btn btn-sm" @click="showAddQuestion = false">Cancel</button>
        </div>
      </form>

      <QuestionList :questions="questions" :research-slug="researchSlug" :session-id="sessionId" />
    </div>

    <!-- Entries panel -->
    <div v-else-if="activeTab === 'entries'" id="panel-entries" role="tabpanel" aria-labelledby="tab-entries" tabindex="0">
      <div v-if="sessionEntries.length" class="grid entries-grid">
        <NuxtLink
          v-for="entry in sessionEntries"
          :key="entry.id"
          :to="`/research/${researchSlug}/entry/${entry.code || entry.id}`"
          class="card entry-card"
        >
          <div class="entry-card-header">
            <div class="entry-title-row">
              <span v-if="entry.code" class="short-code">{{ entry.code }}</span>
              <h3 class="card-title">{{ entry.title }}</h3>
            </div>
            <StatusBadge :status="entry.status" />
          </div>
          <p v-if="entry.description" class="card-meta mt-2" v-html="renderRefs(entry.description, researchSlug)"></p>
          <div v-if="entry.tags?.length" class="entry-tags">
            <span v-for="tag in entry.tags" :key="tag" :class="['tag', `tag-hue-${tagHue(tag)}`]">{{ tag }}</span>
          </div>
        </NuxtLink>
      </div>
      <EmptyState v-else icon="&#x1F4C4;" title="No documents" description="Documents linked to this session will appear here." />
    </div>

    <!-- Changes panel.
         The tab's count comes from ?summary=1, which costs no diffs, so the
         list itself stays behind the click: the full route computes an LCS per
         touched entry and serialises every diff, and paying that on every
         session page load to render one number is the wrong trade. -->
    <div v-if="session && activeTab === 'changes'" id="panel-changes" role="tabpanel" aria-labelledby="tab-changes">
      <SessionChangesList
        ref="changesList"
        :session-id="session.id"
        :research-slug="researchSlug"
        @count="onChangesCount"
      />
    </div>
  </div>

  <EmptyState v-else icon="&#x1F50D;" title="Session not found" />
</template>

<script setup lang="ts">
import { parseMarkdown } from '~/composables/useSafeMarkdown'
import { renderMermaidBlocks } from '~/composables/useMermaid'
const route = useRoute()
const id = route.params.id as string
const sessionId = route.params.sessionId as string

const { data: researchData } = await useApi<{ data: any }>(`/api/researches/${id}`)
const researchName = computed(() => researchData.value?.data?.research?.name ?? 'Project')
const researchSlug = computed(() => researchData.value?.data?.research?.code || id)

// Every research-scoped page publishes the caller's role from the payload it
// already fetches, so the controls beneath it — down to a checkbox inside
// rendered content — know whether they may write.
const { canWrite, canAdmin, readOnlyReason, setFromResearch } = useResearchRole()
watch(researchData, (d) => setFromResearch(d?.data?.research), { immediate: true })

const { data, pending } = await useApi<{ data: any }>(`/api/researches/${id}/sessions/${sessionId}`)

const session  = computed(() => data.value?.data?.session ?? data.value?.data?.Session)
const sessionSlug = computed(() => session.value?.code || sessionId)

// Mermaid rendering for session notes
const notesEl = ref<HTMLElement | null>(null)

/* Notes are closed by default now, and a <details> that is closed hides its
   subtree — so mermaid would measure a zero-width container and draw a diagram
   nobody could read until the page was reloaded with it already open. Render on
   disclosure instead, and only once per content change. */
const notesRendered = ref(false)
function renderNotes() {
  nextTick(() => {
    if (notesEl.value?.offsetParent === null) return // still hidden
    if (notesEl.value) {
      renderMermaidBlocks(notesEl.value)
      notesRendered.value = true
    }
  })
}
function onNotesToggle(e: Event) {
  if ((e.target as HTMLDetailsElement).open && !notesRendered.value) renderNotes()
}
watch(() => session.value?.notes, () => {
  notesRendered.value = false
  renderNotes()
})
onMounted(renderNotes)
const questions = computed(() => data.value?.data?.questions ?? data.value?.data?.Questions ?? {})
const progress  = computed(() => ({
  total:    data.value?.data?.progress?.total    ?? 0,
  answered: data.value?.data?.progress?.answered ?? 0,
  pending:  data.value?.data?.progress?.pending  ?? 0,
  deferred: data.value?.data?.progress?.deferred ?? 0,
  skipped:  data.value?.data?.progress?.skipped  ?? 0,
}))

// Session entries (from API response)
const sessionEntries = computed(() => data.value?.data?.entries ?? [])

function tagHue(tag: string): number {
  return [...tag].reduce((acc, c) => acc + c.charCodeAt(0), 0) % 6
}

// Active tab
type Tab = 'questions' | 'entries' | 'changes'
const TABS: Tab[] = ['questions', 'entries', 'changes']

/* The tab lives in the URL, so a teammate can be sent to the Changes view and
   a refresh does not drop back to Questions — the same thing the research page
   already does for its active section. */
const router = useRouter()
const activeTab = ref<Tab>(
  TABS.includes(route.query.tab as Tab) ? (route.query.tab as Tab) : 'questions'
)

function selectTab(tab: Tab) {
  activeTab.value = tab
  router.replace({ query: { ...route.query, tab: tab === 'questions' ? undefined : tab } })
}

/* The ARIA tabs pattern moves selection with the arrows, and the strip was
   three plain buttons. Roving tabindex is on the template. */
function onTabKey(e: KeyboardEvent, current: Tab) {
  const i = TABS.indexOf(current)
  let next: Tab | null = null
  if (e.key === 'ArrowRight') next = TABS[(i + 1) % TABS.length]!
  else if (e.key === 'ArrowLeft') next = TABS[(i - 1 + TABS.length) % TABS.length]!
  else if (e.key === 'Home') next = TABS[0]!
  else if (e.key === 'End') next = TABS[TABS.length - 1]!
  if (!next) return
  e.preventDefault()
  selectTab(next)
  nextTick(() => document.getElementById(`tab-${next}`)?.focus())
}

const { authFetch } = useAuth()
const rtBase = useRuntimeConfig().public.apiBase || ''

// Add question
const showAddQuestion = ref(false)
const addingQuestion = ref(false)
const newQuestion = ref({ text: '', area: '', priority: 'medium' })
const realSessionId = computed(() => session.value?.id || sessionId)

async function setSessionStatus(status: string) {
  try {
    await authFetch(`${rtBase}/api/sessions/${realSessionId.value}`, {
      method: 'PUT',
      body: { status },
    })
    data.value = await authFetch<any>(`${rtBase}/api/researches/${id}/sessions/${sessionId}`)
  } catch (e: any) {
    useToasts().push({
      variant: 'error',
      title: 'Could not change the session status',
      message: e?.data?.error || e?.message || 'The server refused it.',
      timeout: 0,
    })
  }
}

async function submitQuestion() {
  if (!newQuestion.value.text.trim()) return
  addingQuestion.value = true
  try {
    await authFetch(`${rtBase}/api/sessions/${realSessionId.value}/questions`, {
      method: 'POST',
      body: JSON.stringify({
        questions: [{
          text: newQuestion.value.text,
          area: newQuestion.value.area,
          priority: newQuestion.value.priority,
        }],
      }),
      headers: { 'Content-Type': 'application/json' },
    })
    newQuestion.value = { text: '', area: '', priority: 'medium' }
    showAddQuestion.value = false
    data.value = await authFetch<any>(`${rtBase}/api/researches/${id}/sessions/${sessionId}`)
  } catch (e: any) {
    // A refused write that says nothing leaves the form sitting there with the
    // text still in it, indistinguishable from one that worked.
    useToasts().push({
      variant: 'error',
      title: 'Could not add the question',
      message: e?.data?.error || e?.message || 'The server refused it. Your text is still here.',
      timeout: 0,
    })
  }
  addingQuestion.value = false
}

// Real-time updates
async function reloadSession() {
  data.value = await authFetch<any>(`${rtBase}/api/researches/${id}/sessions/${sessionId}`)
}

const changesList = ref<{ reload: () => Promise<void> } | null>(null)
const changesCount = ref<number | null>(null)
// Distinct from a count of zero. Without it the tab after a failed request is
// pixel-identical to the tab of a session that touched nothing — and "nothing
// happened" is the single wrong conclusion this badge exists to prevent.
const changesUnknown = ref(false)

function onChangesCount(count: number | null) {
  changesUnknown.value = count === null
  changesCount.value = count
}

/** The tab badge, without the diffs the full list would compute. */
async function loadChangesCount() {
  if (!session.value) return
  try {
    const res: any = await authFetch(
      `${rtBase}/api/sessions/${session.value.id}/changes?summary=1`,
    )
    onChangesCount(res?.data?.count ?? 0)
  } catch {
    onChangesCount(null)
  }
}

// An `entry` event is an agent writing, which is the one moment somebody is
// actually watching this page — and it was the only entity the page ignored, so
// both the Entries tab and the Changes tab froze exactly then.
async function reloadAll() {
  await Promise.all([
    reloadSession(),
    // The open list refreshes itself and reports its own count; when the tab
    // has never been opened there is nothing mounted, so the cheap count stands
    // in for it.
    changesList.value ? changesList.value.reload() : loadChangesCount(),
  ])
}

watch(() => session.value?.id, (id) => { if (id) loadChangesCount() }, { immediate: true })

useResearchRealtime(
  () => id,
  (event) => {
    if (['question', 'session'].includes(event.entity)) reloadSession()
    else if (event.entity === 'entry') reloadAll()
  },
  {
    researchId: () => researchData.value?.data?.research?.id,
    onResync: reloadAll,
  },
)
</script>

<style scoped>
.session-header { display: flex; justify-content: space-between; align-items: center; gap: var(--space-4); }
.session-header .page-title { min-width: 0; overflow-wrap: anywhere; }
.session-header-actions { display: flex; align-items: center; gap: var(--space-3); flex-shrink: 0; }
.notes-card { margin-bottom: var(--space-6); }
.notes-summary {
  display: flex;
  align-items: center;
  gap: var(--space-2);
  cursor: pointer;
  list-style: none;
}
.notes-summary:hover { color: var(--color-text); }
.notes-chevron,
.notes-icon { flex-shrink: 0; color: var(--color-text-muted); }
.notes-chevron { transition: transform var(--transition-base); }
.notes-card[open] .notes-chevron { transform: rotate(90deg); }
/* The disclosure has a visible focus ring of its own; the summary is the
   control, so the ring belongs on it rather than on the card. */
.notes-summary:focus-visible {
  outline: 2px solid var(--color-primary);
  outline-offset: 2px;
  border-radius: var(--radius-xs);
}
.notes-summary::-webkit-details-marker { display: none; }
/* The card's heading carries its own bottom margin, which would push the
   summary row apart from a body that is not there when it is closed. */
.notes-summary .card-section-title { margin-bottom: 0; }
.notes-card[open] .notes-summary { margin-bottom: var(--space-3); }
/* No `white-space: pre-wrap`. The notes go through parseMarkdown, which already
   turns a blank line into a paragraph with its own margin — so preserving the
   literal newline on top of that spaced every paragraph twice, and a note with
   a diagram in it opened with a screen of gaps. */
.notes-text { color: var(--color-text-muted); font-size: var(--type-sm); line-height: 1.6; }

/* Panel progress */
.panel-progress {
  /* The strip's rule is a boundary; this is a measurement. Enough air between
     them that they are not read as one thing, and a frame so the bar inside is
     obviously a gauge rather than another rule across the page. */
  margin: var(--space-5) 0;
  padding: var(--space-4) var(--space-5);
}
.progress-stats {
  display: flex; gap: var(--space-4); font-size: var(--type-xs);
  margin-bottom: var(--space-3); color: var(--color-text-muted);
}
.stat-skipped  { color: var(--color-error); font-weight: var(--weight-medium); }
.stat-muted    { color: var(--color-text-muted); }

/* Tabs */
.content-tabs {
  display: flex; gap: 0; margin-bottom: var(--space-5);
  border-bottom: 1px solid var(--color-border);
  /* Three labels plus three badges do not fit a 375px screen, and the strip had
     no wrap or scroll rule — so the whole page body scrolled sideways instead.
     Scroll the strip, and stop the tabs being squeezed into wrapped labels. */
  overflow-x: auto;
  scrollbar-width: none;
}
.content-tabs::-webkit-scrollbar { display: none; }
.content-tab { flex-shrink: 0; }
.content-tab {
  display: flex; align-items: center; gap: var(--space-2);
  padding: var(--space-3) var(--space-5);
  background: none; border: none; border-bottom: 2px solid transparent;
  color: var(--color-text-muted); font-size: var(--type-sm); font-weight: var(--weight-medium);
  font-family: inherit; cursor: pointer; transition: all var(--transition-fast);
  margin-bottom: -1px;
}
.content-tab:hover { color: var(--color-text); }
.content-tab.active { color: var(--color-primary); border-bottom-color: var(--color-primary); }
.tab-count {
  font-size: var(--type-xs); color: var(--color-text-muted);
  background: var(--color-surface-hover); border-radius: var(--radius-xs);
  padding: 0.15rem 0.4rem; font-variant-numeric: tabular-nums;
  /* The Entries and Changes counts now move under a live agent. A fixed floor
     and no wrapping keep the tabs beside them from jumping sideways as digits
     appear. */
  min-width: 1.6rem; text-align: center; white-space: nowrap;
}
.tab-count-unknown { color: var(--color-error); }
.content-tab.active .tab-count { background: var(--color-primary-muted); color: var(--color-primary); }

/* Add form */
.add-btn { margin-bottom: var(--space-4); }
.add-form { margin-bottom: var(--space-5); }
.add-input {
  width: 100%; padding: var(--space-2) var(--space-3);
  background: var(--color-surface); border: 1px solid var(--color-border-strong);
  border-radius: var(--radius-sm); color: var(--color-text);
  font-size: var(--type-sm); font-family: inherit;
  margin-bottom: var(--space-2);
}
.add-input:focus { outline: 2px solid var(--color-primary); outline-offset: -1px; }
.add-input-sm { flex: 1; margin-bottom: 0; }
.add-form-row { display: flex; gap: var(--space-2); align-items: center; }
.add-select {
  padding: var(--space-2) var(--space-3);
  background: var(--color-surface); border: 1px solid var(--color-border-strong);
  border-radius: var(--radius-sm); color: var(--color-text-muted);
  font-size: var(--type-sm); font-family: inherit; cursor: pointer;
}

/* Entries grid */
.entries-grid { grid-template-columns: 1fr; }
.entry-card { display: block; text-decoration: none; color: inherit; }
.entry-card-header { display: flex; justify-content: space-between; align-items: flex-start; gap: var(--space-2); }
.entry-title-row { display: flex; align-items: center; gap: var(--space-2); min-width: 0; }

.skeleton-header { height: 60px; margin-bottom: var(--space-4); }

/* Responsive */
@media (max-width: 768px) {
  .session-header { flex-direction: column; align-items: flex-start; gap: var(--space-2); }
  .add-form-row { flex-wrap: wrap; }
  .add-input-sm { flex: 1 1 100%; }
  .progress-stats { flex-wrap: wrap; gap: var(--space-2); }
  .content-tab { padding: var(--space-2) var(--space-3); font-size: var(--type-xs); }
}
</style>
