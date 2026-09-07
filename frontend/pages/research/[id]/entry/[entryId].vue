<template>
  <div v-if="pending">
    <div class="skeleton-card skeleton-header"></div>
    <div class="skeleton-card skeleton-content"></div>
  </div>

  <div v-else-if="entry">
    <!-- Header -->
    <div class="page-header">
      <Breadcrumbs :crumbs="[
        { label: 'Projects', to: { name: 'index' } },
        { label: researchName, to: `/research/${researchSlug}` },
        { label: sectionName, to: `/research/${researchSlug}?section=${entry.section_id}` },
        { label: entry.title }
      ]" />

      <!-- View mode header -->
      <template v-if="!editing">
        <PageHeader :code="entry.code" :title="entry.title">
          <template #actions>
            <TeamViewerNotice v-if="readOnlyReason" :reason="readOnlyReason" :team-name="research?.team_name" />

            <!-- Status: a picker for a writer, the badge alone for a reader -->
            <StatusBadge v-if="!canWrite" :status="entry.status" />
            <div v-else class="status-dropdown-wrap" @click.stop>
              <button ref="statusTriggerEl" class="status-dropdown-trigger" @click="toggleStatus">
                <StatusBadge :status="entry.status" />
                <svg width="10" height="10" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5"><polyline points="6 9 12 15 18 9"/></svg>
              </button>
              <Teleport to="body">
                <div v-if="statusOpen" class="status-dropdown-overlay" @click="statusOpen = false">
                  <div class="status-dropdown" :style="statusDropdownStyle" @click.stop>
                    <button
                      v-for="s in statuses"
                      :key="s"
                      :class="['status-option', { active: entry.status === s }]"
                      @click="changeStatus(s)"
                    >
                      <StatusBadge :status="s" />
                    </button>
                  </div>
                </div>
              </Teleport>
            </div>
            <!-- The keyboard route into the marks. The gutter pins are not tab
                 stops on purpose, so this button and the skip link are how
                 somebody reaches them without a mouse. -->
            <button
              v-if="!shareActive() && annotations.length"
              class="btn btn-icon"
              :aria-label="`${annotations.length} marks in this document`"
              title="Marks"
              @click="marksPanelOpen = true"
            >
              <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M4 4h16v12H8l-4 4z"/><path d="M8 9h8"/><path d="M8 12.5h5"/></svg>
              <span class="btn-count">{{ annotations.length }}</span>
            </button>
            <button v-if="canWrite" class="btn" @click="startEditing">
              <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M11 4H4a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h14a2 2 0 0 0 2-2v-7"/><path d="M18.5 2.5a2.121 2.121 0 0 1 3 3L12 15l-4 1 1-4 9.5-9.5z"/></svg>
              Edit
            </button>
            <!-- Everything that is not the main verb or a piece of state the
                 reader is looking at. Six controls in a row had turned the
                 header into a toolbar, and the destructive one was an unlabelled
                 red icon eight pixels from Copy. -->
            <!-- The label does not change. Swapping "Copy" for "Copied" is two
                 characters wider and shoved the ⋯ sideways for two seconds,
                 which is a bigger movement than the feedback is worth. -->
            <button class="btn" :class="{ 'is-copied': copied }" @click="copyMarkdown">
              <svg v-if="!copied" width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><rect width="14" height="14" x="8" y="8" rx="2"/><path d="M4 16c-1.1 0-2-.9-2-2V4c0-1.1.9-2 2-2h10c1.1 0 2 .9 2 2"/></svg>
              <svg v-else width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><polyline points="20 6 9 17 4 12"/></svg>
              Copy
            </button>
            <span class="sr-only" role="status" aria-live="polite">{{ copied ? 'Copied to the clipboard' : '' }}</span>
            <div class="doc-more">
              <ActionMenu ref="docMenu" title="Document actions" width="wide" class="no-print">
                <!-- The fact, then the verbs. It renders with or without a
                     revision: a document nobody has revised still has an author
                     and a timestamp, and one menu that degrades beats two menus
                     that differ. -->
                <!-- Only when somebody recorded it. Falling back to `agent`
                     stated a fact about authorship that nobody had established
                     — and in a product whose point is telling an agent from a
                     person, that is the one default that must not be silent.
                     Documents written before the revisions migration have no
                     row at all, and the deployed database holds some. -->
                <EntryProvenanceMenuHeader
                  v-if="provenance"
                  :revision="provenance.revision"
                  :author-kind="provenance.author_kind"
                  :revised-at="provenance.revised_at"
                  :author-name="provenance.author_name"
                />
                <button class="action-menu-item" @click="openHistory">
                  <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M3 3v5h5"/><path d="M3.05 13A9 9 0 1 0 6 5.3L3 8"/><path d="M12 7v5l4 2"/></svg>
                  Revision history
                </button>
                <button class="action-menu-item" @click="downloadMarkdown">
                  <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4"/><polyline points="7 10 12 15 17 10"/><line x1="12" y1="15" x2="12" y2="3"/></svg>
                  Download .md
                </button>

                <template v-if="canWrite">
                  <div class="action-menu-divider" role="separator"></div>
                  <button class="action-menu-item action-menu-item--danger" @click="showDeleteConfirm = true">
                    <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><polyline points="3 6 5 6 21 6"/><path d="M19 6v14a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2V6m3 0V4a2 2 0 0 1 2-2h4a2 2 0 0 1 2 2v2"/></svg>
                    Delete document
                  </button>
                </template>
              </ActionMenu>
            </div>
          </template>
        </PageHeader>
        <p v-if="entry.description" class="card-meta mt-2" v-html="renderRefs(entry.description, researchSlug)"></p>
        <div v-if="entry.tags?.length" class="entry-tags">
          <span v-for="tag in entry.tags" :key="tag" :class="['tag', `tag-hue-${tagHue(tag)}`]">{{ tag }}</span>
        </div>
        <NuxtLink v-if="linkedSession" :to="`/research/${researchSlug}/session/${linkedSession.code || linkedSession.id}`" class="entry-session-link">
          <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="10"/><path d="M9.09 9a3 3 0 0 1 5.83 1c0 2-3 3-3 3"/><path d="M12 17h.01"/></svg>
          {{ linkedSession.title }}
        </NuxtLink>
      </template>

      <!-- Edit mode header -->
      <template v-else>
        <!-- Somebody else wrote to this entry while the editor was open. Saving
             now replaces their version with a draft that never saw it. The
             revision history keeps both, but finding out afterwards is not the
             same as being told. -->
        <div v-if="remoteChangedWhileEditing" class="edit-remote-change" role="status">
          <span>Someone else changed this document while you were editing. Saving will replace their version.</span>
          <button class="btn btn-sm" @click="discardDraftForRemote">Discard mine and reload</button>
        </div>
        <div class="edit-header-bar">
          <div class="edit-field">
            <label class="edit-label">Title</label>
            <input v-model="editForm.title" class="edit-input" placeholder="Document title..." />
          </div>
          <div class="edit-field">
            <label class="edit-label">Description</label>
            <input v-model="editForm.description" class="edit-input" placeholder="Short description (optional)..." />
          </div>
          <div class="edit-field">
            <label class="edit-label">Tags</label>
            <input v-model="editForm.tagsRaw" class="edit-input" placeholder="comma, separated, tags" />
          </div>
          <div class="edit-actions-bar">
            <button class="btn btn-sm btn-primary" :disabled="saving" @click="saveEntry">
              {{ saving ? 'Saving...' : 'Save' }}
            </button>
            <button class="btn btn-sm" @click="cancelEditing">Cancel</button>
          </div>
        </div>
      </template>
    </div>

    <!-- View mode content -->
    <template v-if="!editing">
      <EntryUpdateNotice
        v-if="viewNotice"
        :state="viewNotice"
        class="no-print"
        @review="reviewUnseenChanges"
      />

      <!-- View toggle -->
      <div class="view-toggle no-print">
        <button :aria-pressed="viewMode === 'rendered'" :class="['btn btn-sm', { active: viewMode === 'rendered' }]" @click="viewMode = 'rendered'">
          <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M1 12s4-8 11-8 11 8 11 8-4 8-11 8-11-8-11-8z"/><circle cx="12" cy="12" r="3"/></svg>
          Document
        </button>
        <button :aria-pressed="viewMode === 'source'" :class="['btn btn-sm', { active: viewMode === 'source' }]" @click="viewMode = 'source'">
          <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><polyline points="16 18 22 12 16 6"/><polyline points="8 6 2 12 8 18"/></svg>
          Source
        </button>

        <div v-if="!shareActive() && viewMode === 'rendered'" class="marks-toggle">
          <span class="card-meta">Marks</span>
        <SegmentedToggle
          v-model="marksMode"
          label="Marks"
          :options="[
            { value: 'all', label: 'All' },
            { value: 'open', label: 'Open' },
            { value: 'off', label: 'Off' },
          ]"
        />
        </div>
      </div>

      <!-- Content -->
      <!-- Only in the rendered view: the source view is a `pre` of stored JSON,
             and a code block running to the card's border reads as a layout
             fault. The condition is in the template rather than folded into the
             computed because `viewMode` is declared further down this file. -->
        <div
          ref="cardEl"
          class="entry-content card"
          :class="{
            'is-artifact': isArtifactOnly && viewMode === 'rendered',
            'has-marks': marksVisible && annotations.length > 0,
          }"
          @mouseup="onSelect"
          @touchend="onSelect"
          @click="onCardClick"
        >
          <a v-if="marksVisible && annotations.length" class="marks-skip" href="#marks-panel" @click.prevent="marksPanelOpen = true">
            {{ annotations.length }} marks in this document
          </a>

          <AnnotationsMarkGutter
            v-if="marksVisible"
            :annotations="annotations"
            :positions="markPositions"
            :active-id="activeThreadId"
            @select="openThread"
          />
        <!-- Inside the content card rather than above it. A second bordered box
             would read as chrome, and chrome is exactly what the stored status
             was when authors started typing the real one into the prose. -->
        <EntryMetadataBlock
          :specs="sectionFieldSpec"
          :values="entry.metadata ?? {}"
          :status="entry.status"
          :research-slug="researchSlug"
          :editable="canWrite"
          :entry-spec-version="entry.spec_version"
          :section-spec-version="sectionSpecVersion"
          :metadata-status="entry.metadata_status"
          :on-save="saveMetadata"
          @editing="metaEditing = $event"
        />
        <BlocksBlockRenderer
          v-if="viewMode === 'rendered' && isBlocks"
          :blocks="blocks"
          :research-slug="researchSlug"
          :bridge-data="blockBridgeData"
          :entry-id="entry?.id"
          :marks-mode="marksMode"
          :tasks="tasks"
          :tasks-status="tasksStatus"
          :expand-block-ids="markedBlockIds"
          @retry-tasks="loadTasks"
        />
        <div v-else-if="viewMode === 'rendered'" ref="contentEl" class="markdown-content" v-html="renderedContent"></div>
        <!-- No header over a block document. Its body here is the stored JSON,
             while the header comes from the markdown rendering of the same
             document — a file that exists nowhere, under a header that is
             evidence for what a download would produce. -->
        <pre v-else class="source-view"><code v-if="sourceFrontmatter && !isBlocks" class="source-frontmatter">{{ sourceFrontmatter }}</code><code v-html="highlightedSource"></code></pre>
      </div>

      <!-- The opened mark.
           Under the card rather than between the blocks: a thread injected into
           the flow would have to live inside a <p>, which is not valid markup,
           and a modal would cover the sentence being discussed. This is the
           compromise — the marked text stays on screen above it. -->
      <AnnotationsThreadCard
        v-if="activeThread"
        :annotation="activeThread"
        :research-slug="researchSlug"
        :can-write="canWrite"
        :busy="threadBusy"
        @close="closeThread"
        @update-body="(v: string) => setBody(activeThread!, v)"
        @accept="setStatus(activeThread!, 'closed')"
        @dismiss="setStatus(activeThread!, 'dismissed')"
        @reopen="(reason: string) => setStatus(activeThread!, 'open', reason)"
        @delete="removeMark(activeThread!)"
        @show-diff="showMarkDiff"
      />

      <AnnotationsSelectionPopover
        ref="popoverEl"
        :visible="popoverOpen"
        :rect="selectionRect"
        :quote="pendingQuote"
        :block-count="pendingSegments.length"
        :entry-type="entry?.entry_type"
        :saving="marking"
        :error="markError"
        @create="createMark"
        @cancel="cancelMark"
      />

      <!-- `flush`, so the header keeps its own padding and the list runs to the
           dialog's edges: a bordered card inside a bordered dialog draws the
           same line twice. -->
      <ModalOverlay :visible="marksPanelOpen" size="lg" flush labelledby="marks-panel" @close="marksPanelOpen = false">
        <ModalHeader title="Marks in this document" title-id="marks-panel" @close="marksPanelOpen = false" />
        <AnnotationsAnnotationList
          class="marks-panel__list"
          :annotations="annotations"
          :research-slug="researchSlug"
          :error="marksError"
          bleed
          empty-variant="document"
          @open="(a) => { marksPanelOpen = false; openThread(a.id) }"
          @retry="loadAnnotations().then(() => repaint())"
        />
      </ModalOverlay>

      <!-- Cross-references -->
      <ConfirmModal
      :visible="incompleteFields.length > 0"
      title="Required fields are unanswered"
      :message="`This document does not answer ${incompleteFields.join(', ')}. Completing it now records that somebody decided to anyway.`"
      confirm-label="Complete anyway"
      @confirm="completeAnyway"
      @cancel="incompleteFields = []"
    />

    <EntryCrossReferencesBlock
        :outgoing="outgoingRefs"
        :incoming="incomingRefs"
        :research-slug="researchSlug"
      />

      <!-- External links -->
      <EntryExternalLinksBlock :links="externalLinks" />

      <!-- Related by tags -->
      <EntryRelatedEntriesBlock
        :entries="relatedEntries"
        :current-tags="entry.tags ?? []"
        :research-slug="researchSlug"
        :research-id="research?.id ?? ''"
      />

      <!-- Prev / Next navigation -->
      <EntryNavigation
        v-if="siblings.length > 1"
        :prev="prevEntry"
        :next="nextEntry"
        :research-slug="researchSlug"
      />
    </template>

    <!-- Edit mode content -->
    <div v-else class="editor-wrap">
      <ClientOnly>
        <MdEditor
          v-model="editForm.content"
          language="en-US"
          :theme="theme"
          :preview="true"
          preview-theme="github"
          :toolbars="editorToolbars"
          :show-code-row-number="true"
          :auto-focus="true"
          style="height: 70vh;"
          @on-save="saveEntry"
        />
      </ClientOnly>
    </div>

    <!-- Revision history -->
    <EntryHistoryPanel
      ref="historyPanel"
      :visible="showHistory"
      :entry-id="entry.id"
      :entry-title="entry.title"
      :entry-code="entry.code"
      :restore-error="restoreFailed"
      @close="onHistoryClosed"
      @restore="askRestore"
    />

    <ConfirmModal
      :visible="restoreTarget !== null"
      title="Restore revision"
      :message="`Restore revision ${restoreTarget} onto this document? The current text is kept in the history as its own revision — nothing is lost.`"
      confirm-label="Restore"
      :loading="restoring"
      @confirm="confirmRestore"
      @cancel="restoreTarget = null"
    />

    <!-- Delete confirmation -->
    <ConfirmModal
      :visible="showDeleteConfirm"
      title="Delete document"
      :message="`Are you sure you want to delete &quot;${entry.title}&quot;? This action cannot be undone.`"
      confirm-label="Delete"
      variant="danger"
      :loading="deleting"
      @confirm="deleteEntry"
      @cancel="cancelDelete"
    />
  </div>

  <EmptyState
    v-else
    icon="&#x1F50D;"
    title="Document not found"
    description="Check the link and make sure you have access to this document."
  >
    <NuxtLink :to="`/research/${researchSlug}`" class="btn btn-sm">Back to project</NuxtLink>
  </EmptyState>
</template>

<script setup lang="ts">
import { useTheme } from '~/composables/useTheme'
const { theme } = useTheme()

import type { Annotation, AnnotationKind } from '~/composables/useAnnotations'
import { MARK_CLASS, type CapturedSegment } from '~/composables/useAnnotationOverlay'
import type { EntryViewState } from '~/composables/useEntryUpdates'
import { parseMarkdown } from '~/composables/useSafeMarkdown'
import { MdEditor } from 'md-editor-v3'
import type { ToolbarNames } from 'md-editor-v3'
import 'md-editor-v3/lib/style.css'
import { tagHue } from '~/composables/useTagHue'
import { renderMermaidBlocks } from '~/composables/useMermaid'

// Vue Router reuses a page component when only a dynamic parameter changes.
// This page's primary fetch is bound to the route at setup time, so a document
// navigation must create a fresh instance (query-only changes still do not).
definePageMeta({ key: (currentRoute) => `${currentRoute.params.id}:${currentRoute.params.entryId}` })

const route = useRoute()
const router = useRouter()
const id = route.params.id as string
const entryId = route.params.entryId as string


const statuses = ['draft', 'active', 'completed', 'archived']

const editorToolbars: ToolbarNames[] = [
  'bold', 'underline', 'italic', 'strikeThrough', '-',
  'title', 'sub', 'sup', 'quote', '-',
  'unorderedList', 'orderedList', 'task', '-',
  'codeRow', 'code', 'link', 'image', 'table', 'mermaid', '-',
  'revoke', 'next', '=',
  // Reading an entry is one job and writing one is another. `pageFullscreen`
  // fills the window without leaving the page, so Escape still belongs to the
  // editor and the browser's own chrome stays put; `fullscreen` hands the
  // screen over entirely for the case where even the browser is a distraction.
  'pageFullscreen', 'fullscreen', '-',
  'preview', 'catalog',
]

// Research + sections for breadcrumb and sibling navigation
const { data: researchData } = await useApi<{ data: any }>(`/api/researches/${id}`)
const research = computed(() => researchData.value?.data?.research)

// Every research-scoped page publishes the caller's role from the payload it
// already fetches, so the controls beneath it — down to a checkbox inside
// rendered content — know whether they may write.
const { canWrite, canAdmin, readOnlyReason, setFromResearch } = useResearchRole()
watch(researchData, (d) => setFromResearch(d?.data?.research), { immediate: true })

const researchName = computed(() => research.value?.name ?? 'Project')
const researchSlug = computed(() => research.value?.code || id)
const sections = computed(() => researchData.value?.data?.sections ?? [])

// Entry data (pass research context for code-based lookup)
const { data, pending, refresh } = await useApi<{ data: any }>(`/api/researches/${id}/entries/${entryId}`)

// The server returns read state beside the exact document revision in this
// response. The notice is a visit snapshot: marking the revision seen must not
// make the explanation disappear while the reader is still using it.
type UnseenEntryState = EntryViewState & { kind: 'new' | 'changed' }
const viewNotice = ref<UnseenEntryState | null>(null)
const pendingSeenRevision = ref(0)
const markedThrough = ref(0)
const { authFetch: viewFetch } = useAuth()
const viewApiBase = useRuntimeConfig().public.apiBase || ''

function currentRevision(): number {
  return Number((data.value as any)?.revision || 0)
}

function unseenState(from = 0): UnseenEntryState | null {
  const current = currentRevision()
  const state = (data.value as any)?.view_state as EntryViewState | undefined
  if (!current) return null
  if (state?.kind === 'changed' && from === state.seen_revision && from < current) {
    return { kind: 'changed', current_revision: current, seen_revision: from, unseen_revisions: current - from }
  }
  if (state?.kind === 'new' || state?.kind === 'changed') return { ...state }
  return null
}

// Data is already awaited above and this application is client-rendered. Seed
// the notice during setup so it is part of the first paint; onMounted performs
// the acknowledgement after the page is actually visible.
viewNotice.value = unseenState(Number(route.query.changes_from || 0))

async function markDisplayedRevision(captureNotice = false) {
  if (shareActive() || !entry.value?.id) return
  const entryID = entry.value.id
  const revision = currentRevision()
  if (!revision || revision <= markedThrough.value) return
  if (captureNotice && !viewNotice.value) {
    const fromQuery = Number(route.query.changes_from || 0)
    viewNotice.value = unseenState(fromQuery)
  }
  await nextTick()
  // Navigation can reuse this page component. Never pair the previous
  // document's revision with the next document's id across that tick.
  if (entry.value?.id !== entryID) return
  if (!import.meta.client || document.visibilityState !== 'visible') {
    pendingSeenRevision.value = Math.max(pendingSeenRevision.value, revision)
    return
  }
  try {
    await viewFetch(`${viewApiBase}/api/entries/${entryID}/seen`, {
      method: 'PUT',
      body: { revision },
    })
    if (entry.value?.id !== entryID) return
    markedThrough.value = Math.max(markedThrough.value, revision)
    if (pendingSeenRevision.value <= markedThrough.value) pendingSeenRevision.value = 0
  } catch {
    // The queue remains conservative when acknowledgement fails, but the
    // reader should not have to guess why the document is still listed.
    pendingSeenRevision.value = Math.max(pendingSeenRevision.value, revision)
    pushToast({
      variant: 'error',
      title: 'Could not mark this document as seen',
      message: 'It remains in Updates.',
      action: { label: 'Try again', onClick: () => { void markDisplayedRevision() } },
    })
  }
}

function onDocumentVisible() {
  if (document.visibilityState === 'visible' && pendingSeenRevision.value) {
    void markDisplayedRevision()
  }
}

function beginEntryVisit() {
  pendingSeenRevision.value = 0
  markedThrough.value = 0
  const fromQuery = Number(route.query.changes_from || 0)
  viewNotice.value = unseenState(fromQuery)
  void markDisplayedRevision()

  if (route.query.review_changes === '1' && viewNotice.value?.kind === 'changed') {
    const requestedTo = Number(route.query.changes_to || 0)
    historyPanel.value?.prepareRange(
      viewNotice.value.seen_revision,
      requestedTo > viewNotice.value.seen_revision ? requestedTo : viewNotice.value.current_revision,
    )
    showHistory.value = true
    // These parameters describe this one arrival. Keeping them in the URL
    // resurrects a stale Changed notice on reload after the checkpoint moved.
    const { review_changes: _review, changes_from: _from, changes_to: _to, ...query } = route.query
    void router.replace({ query })
  }
}

// This was the one reader-facing page with no live updates, which mattered less
// while an agent was the only writer. Now a checkbox writes from here, and the
// same entry can be open twice.
// A tick the reader just made is already on screen; refetching would only make
// the checkbox flicker. Everything else — an agent rewriting the entry — has to
// land, or the next save from this page overwrites it.
//
// Which of the two it is used to be guessed from a 1200 ms window armed before
// the request went out. The event now names the tab that caused it, so the
// question is answered rather than raced: a slow save no longer repainted over
// the reader, and a genuine remote change arriving within the window is no
// longer swallowed.
// True while the metadata block holds a draft. Declared above the realtime
// callback that reads it, not beside the handler that sets it: a value a
// setup-time registration can reach has to exist by then, and the same shape
// one screen down already cost this page a blank error dialog.
const metaEditing = ref(false)

useRealtimeUpdates((event: WsEvent) => {
  // A mark's event names the annotation in entity_id, which says nothing to a
  // page showing a document; the entry it hangs off is what decides whether
  // this is ours, and that travels as parent_id/parent_code.
  if (event.entity === 'annotation') {
    if (isSelf(event)) return
    if (event.parent_id !== entry.value?.id && event.parent_code !== entry.value?.code) return
    loadAnnotations().then(() => repaint())
    return
  }
  // A task_ref row is a live view of a task, so a status changed anywhere —
  // the board, another tab, an agent over MCP — has to land here. Including
  // this tab's own tick: the block already shows it optimistically, and the
  // refetch is what turns that into the server's answer.
  if (event.entity === 'task') {
    // Scoped the way the entry branch below is: the socket carries every
    // research the reader may see, and a task moved on someone else's board is
    // not a reason to refetch this one.
    if (event.research_id !== research.value?.id && event.research_code !== researchSlug.value) return
    if (hasTaskRef.value) void loadTasks()
    return
  }
  if (event.entity !== 'entry') return
  // The event carries the entry's UUID; the route parameter is usually a short
  // code (every link in the app builds /entry/E3). Comparing the two meant the
  // page never refreshed for anyone who arrived by clicking a link.
  if (event.entity_id !== entry.value?.id && event.entity_id !== entryId) return
  if (isSelf(event)) return
  // `metaEditing` is the metadata block's draft, which is a snapshot exactly
  // like `editForm`. It was added to this page after this guard was written and
  // did not participate: a refresh replaced the values under an open editor,
  // and Save then overwrote the agent that had just written them.
  if (editing.value || metaEditing.value) {
    // Refetching would not reach the draft — `editForm` is a snapshot taken when
    // editing began — so the only thing a silent refresh achieves is that Save
    // still overwrites whoever just wrote. Say so instead; the history keeps
    // their version either way, but the reader should not find out afterwards.
    remoteChangedWhileEditing.value = true
    // The panel is a read of the same entry and holds no draft, so it can be
    // brought up to date even while the editor holds the page back.
    reloadHistoryIfOpen()
    return
  }
  refreshWithMarks()
  reloadHistoryIfOpen()
}, { onResync: () => { refreshWithMarks(); reloadHistoryIfOpen() } })

/**
 * A remote write is a new revision, and an open history panel that never hears
 * about it presents a list as complete while it is not.
 *
 * Guarded on `showHistory` because the panel is mounted for the life of the
 * page rather than created on open: without the guard every remote edit would
 * fetch a revision list and a diff nobody is looking at.
 */
function reloadHistoryIfOpen() {
  if (showHistory.value) historyPanel.value?.refreshList()
}



// Set when somebody else changed this entry while the editor was open.
const remoteChangedWhileEditing = ref(false)
const entry = computed(() => data.value?.data)

// The declaration lives on the section, so the block reads it from the research
// payload the page already has rather than fetching one more thing.
const entrySection = computed(() =>
  sections.value.find((sec: any) => sec.id === entry.value?.section_id))
const sectionFieldSpec = computed<any[]>(() => entrySection.value?.field_spec ?? [])
const sectionSpecVersion = computed<number>(() => entrySection.value?.spec_version ?? 0)

async function saveMetadata(values: Record<string, unknown>) {
  if (!entry.value) return
  const res = await authFetch<any>(`${rtBase}/api/entries/${entry.value.id}`, {
    method: 'PUT',
    body: { metadata: values },
  })
  await refresh()
  await markDisplayedRevision()
  // A key the server refused, or a required field still empty, is a thing the
  // person just did and cannot see from the entry they get back.
  const report = res?.metadata_report
  if (report?.unknown_keys?.length || report?.invalid_values?.length) {
    useToasts().push({
      variant: 'error',
      title: 'Some values were not stored as sent',
      message: [
        ...(report.unknown_keys ?? []).map((u: any) => `${u.key}: ${u.reason}`),
        ...(report.invalid_values ?? []).map((u: any) => `${u.key}: ${u.reason}`),
      ].join('; '),
    })
  }
}

const sectionName = computed(() => {
  const sec = sections.value.find((s: any) => s.id === entry.value?.section_id)
  return sec?.display_name || sec?.name || 'Section'
})

// Linked session
const { data: sessionsData } = await useApi<{ data: any[] }>(`/api/researches/${id}/sessions`)
const linkedSession = computed(() => {
  if (!entry.value?.session_id) return null
  return (sessionsData.value?.data ?? []).find((s: any) => s.id === entry.value.session_id) ?? null
})

// A blocks entry stores a JSON document of typed blocks. Parsing failures fall
// back to the markdown path rather than showing nothing: better to display the
// raw document than an empty card.
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

/* ------------------------------------------------------------ task_ref ---
 *
 * A task_ref block holds references; the rows are resolved here, from the
 * research's own task list, and never inside the renderer. Two reasons: the
 * renderer is also mounted by an export preview and by Storybook, neither of
 * which should acquire a network call; and the share page resolves the same
 * block through a different base URL with a different refusal.
 *
 * Fetched only when the document actually contains one of these blocks — an
 * entry page is opened constantly and most documents name no tasks at all.
 */
const tasks = ref<any[]>([])
const tasksStatus = ref<'idle' | 'loading' | 'ready' | 'error' | 'excluded'>('idle')
const hasTaskRef = computed(() => blocks.value.some((b: any) => b?.type === 'task_ref'))

// Read here rather than through `rtBase`, which is declared far below: this
// watcher fires during setup, and a const it has not reached yet is a crash on
// the first render of any document that names a task.
const { authFetch: taskFetch } = useAuth()
const tasksBase = useRuntimeConfig().public.apiBase || ''

async function loadTasks() {
  if (!hasTaskRef.value) return
  // Only the FIRST load says "loading". A refetch — and every tick causes one,
  // because the block writes to a task and the task event comes back — would
  // otherwise replace the rows with skeletons for the length of a round trip,
  // which is exactly the flicker the skeleton exists to prevent. An 'error'
  // counts as a first load: a reader who pressed Try again is owed some sign
  // that something is happening.
  if (tasksStatus.value === 'idle' || tasksStatus.value === 'error') tasksStatus.value = 'loading'
  try {
    const res = await taskFetch<{ data: any[] }>(`${tasksBase}/api/researches/${id}/tasks`)
    tasks.value = res.data ?? []
    tasksStatus.value = 'ready'
  } catch {
    tasksStatus.value = 'error'
  }
}

watch(hasTaskRef, (present) => {
  if (present && tasksStatus.value === 'idle') void loadTasks()
}, { immediate: true })

// A document that is nothing but one artifact. The card gives up its padding
// for these — see `.entry-content.is-artifact` in system.css — because an
// artifact brings its own margins inside its own frame and ours only makes it
// narrower. Strictly one block: a document with prose around the artifact still
// needs the prose to sit where prose sits.
const isArtifactOnly = computed(
  () => isBlocks.value && blocks.value.length === 1 && blocks.value[0]?.type === 'html',
)

const blockBridgeData = computed(() => {
  if (!isBlocks.value) return null
  return {
    research: {
      id: research.value?.id,
      code: research.value?.code,
      name: research.value?.name,
      goal: research.value?.goal,
    },
    entry: {
      id: entry.value?.id,
      code: entry.value?.code,
      title: entry.value?.title,
      tags: entry.value?.tags ?? [],
    },
    sections: sections.value.map((s: any) => ({
      id: s.id,
      name: s.display_name || s.name,
    })),
  }
})

// Rendered markdown
const renderedContent = computed(() => {
  if (!entry.value?.content) return ''
  const html = parseMarkdown(normalizeContent(entry.value.content)) as string
  return linkRefs(html, researchSlug.value)
})

// Syntax-highlighted markdown source
const highlightedSource = computed(() => {
  if (!entry.value?.content) return ''
  return highlightMarkdown(entry.value.content)
})

function highlightMarkdown(src: string): string {
  const esc = (s: string) => s.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;')

  const lines = src.split('\n')
  const result: string[] = []
  let inCodeBlock = false
  let codeLang = ''

  for (const raw of lines) {
    const line = esc(raw)

    // Fenced code blocks
    if (/^```/.test(raw)) {
      if (!inCodeBlock) {
        inCodeBlock = true
        codeLang = raw.slice(3).trim()
        result.push(`<span class="md-fence">${line}</span>`)
      } else {
        inCodeBlock = false
        codeLang = ''
        result.push(`<span class="md-fence">${line}</span>`)
      }
      continue
    }
    if (inCodeBlock) {
      result.push(`<span class="md-code-line">${line}</span>`)
      continue
    }

    // Headings
    if (/^#{1,6}\s/.test(raw)) {
      const match = line.match(/^(#{1,6})\s(.*)$/)
      if (match) {
        result.push(`<span class="md-heading-marker">${match[1]}</span> <span class="md-heading">${inlineHighlight(match[2])}</span>`)
        continue
      }
    }

    // Blockquotes
    if (/^&gt;\s?/.test(line)) {
      result.push(`<span class="md-blockquote">${inlineHighlight(line)}</span>`)
      continue
    }

    // Horizontal rule
    if (/^(---|\*\*\*|___)$/.test(raw)) {
      result.push(`<span class="md-hr">${line}</span>`)
      continue
    }

    // Unordered list
    if (/^(\s*)([-*+])\s/.test(raw)) {
      const match = line.match(/^(\s*)([-*+])\s(.*)$/)
      if (match) {
        result.push(`${match[1]}<span class="md-list-marker">${match[2]}</span> ${inlineHighlight(match[3])}`)
        continue
      }
    }

    // Ordered list
    if (/^\s*\d+\.\s/.test(raw)) {
      const match = line.match(/^(\s*)(\d+\.)\s(.*)$/)
      if (match) {
        result.push(`${match[1]}<span class="md-list-marker">${match[2]}</span> ${inlineHighlight(match[3])}`)
        continue
      }
    }

    // Regular line with inline highlighting
    result.push(inlineHighlight(line))
  }

  return result.join('\n')
}

function inlineHighlight(line: string): string {
  return line
    // Images ![alt](url) — before links
    .replace(/!\[([^\]]*)\]\(([^)]+)\)/g, '<span class="md-image">![$1]($2)</span>')
    // Links [text](url)
    .replace(/\[([^\]]+)\]\(([^)]+)\)/g, '<span class="md-link">[$1](<span class="md-url">$2</span>)</span>')
    // Cross-references [[...]]
    .replace(/\[\[([^\]]+)\]\]/g, '<span class="md-crossref">[[$1]]</span>')
    // Bold **text** or __text__
    .replace(/(\*\*|__)(.+?)\1/g, '<span class="md-bold">$1$2$1</span>')
    // Italic *text* or _text_ (not inside bold markers)
    .replace(/(?<!\*)(\*|_)(?!\*)(.+?)\1(?!\*)/g, '<span class="md-italic">$1$2$1</span>')
    // Inline code `code`
    .replace(/`([^`]+)`/g, '<span class="md-inline-code">`$1`</span>')
}

// View toggle
const viewMode = ref<'rendered' | 'source'>('rendered')

/* ---------------------------------------------------------------- marks ---
 *
 * A mark is born here and nowhere else: there is no MCP tool that creates one,
 * because the gesture the feature exists for is a person reading and pointing.
 *
 * The order of operations in `onSelect` is the whole trick. The selection is
 * captured and repainted as ours BEFORE any UI appears — the moment a popover
 * takes focus the browser drops the real selection, and on iOS the system menu
 * takes it first.
 */
const { listForEntry, create: createAnnotation, patch: patchAnnotation, remove: removeAnnotation } = useAnnotations()
const { capture, paint, clear: clearMarks, positions } = useAnnotationOverlay()
const { push: pushToast } = useToasts()

const annotations = ref<Annotation[]>([])
/**
 * Blocks a mark is anchored in, so the renderer can keep them unfolded.
 *
 * A transcript folds past its first turns, and a mark inside the folded tail
 * measures `getBoundingClientRect()` as zeros — the pin lands above the card
 * rather than beside the sentence. Fixed declaratively rather than by poking
 * the DOM after paint.
 */
const markedBlockIds = computed<string[]>(() =>
  annotations.value.map((a: any) => a?.anchor?.block_id).filter(Boolean) as string[]
)
const cardEl = ref<HTMLElement | null>(null)
const markPositions = ref<Array<{ id: string; code: string; top: number }>>([])
const paintedCount = ref(0)
const marksError = ref<string | null>(null)

const marksPanelOpen = ref(false)
const activeThreadId = ref<string | null>(null)
const threadBusy = ref(false)

const popoverOpen = ref(false)
const popoverEl = ref<{ focusFirst: () => void } | null>(null)
const selectionRect = ref<DOMRect | null>(null)
/** Every block the selection touched. One for an ordinary sentence, more when
 *  somebody selected a passage running across paragraphs. */
const pendingSegments = ref<CapturedSegment[]>([])
const pendingQuote = computed(() => pendingSegments.value.map((s) => s.quote.exact).join(' '))
const marking = ref(false)
const markError = ref<string | null>(null)

/** Remembered per browser, like a folded section. Open is the default: the
 *  point of a queue is that you see it without asking. */
const marksMode = ref<'all' | 'open' | 'off'>('open')

const marksVisible = computed(() =>
  !shareActive() && viewMode.value === 'rendered' && marksMode.value !== 'off')

const activeThread = computed(() =>
  annotations.value.find((a) => a.id === activeThreadId.value) ?? null)

async function loadAnnotations() {
  if (shareActive() || !entry.value?.id) return
  try {
    annotations.value = await listForEntry(entry.value.id)
    marksError.value = null
  } catch (e: any) {
    // A document that will not load its marks is still a document — but a
    // failed fetch used to be indistinguishable from a document with no marks,
    // right down to the counter disappearing. The panel says which it is.
    annotations.value = []
    marksError.value = e?.data?.error || e?.message || 'Could not load the marks on this document'
  }
}

/**
 * Redrawing the marks.
 *
 * Every paint clears first: `v-html` patches the tree underneath, and a wrapper
 * left behind from the previous pass would be found by the next text walk as if
 * it were prose.
 */
async function repaint() {
  if (!import.meta.client) return
  await nextTick()
  const root = cardEl.value
  if (!root) return
  if (!marksVisible.value) {
    clearMarks(root)
    markPositions.value = []
    paintedCount.value = 0
    return
  }
  paint(root, annotations.value, marksMode.value)
  markPositions.value = positions(root)
  paintedCount.value = markPositions.value.length
}

watch([annotations, marksMode, viewMode, () => blocks.value, () => renderedContent.value], () => { repaint() })

function onSelect() {
  if (!canWrite.value || shareActive() || viewMode.value !== 'rendered') return
  const root = cardEl.value
  if (!root) return

  const captured = capture(root)
  // A click that selects nothing is how a reader dismisses the popover, and it
  // has to actually dismiss it: there was no other way out but the Cancel
  // button — not a click elsewhere, not Escape.
  if (!captured) {
    if (popoverOpen.value) closePopover()
    return
  }

  pendingSegments.value = captured.segments
  selectionRect.value = captured.rect
  markError.value = null
  popoverOpen.value = true
}

/**
 * Escape closes the popover from wherever the reader is.
 *
 * The component's own `@keydown.esc` only fires when something inside it has
 * focus, and nothing does: focus is deliberately left in the document so the
 * selection survives. Without this the only way out was the Cancel button.
 */
function onMarkKeydown(event: KeyboardEvent) {
  if (event.key === 'Escape' && popoverOpen.value) {
    event.preventDefault()
    closePopover()
    return
  }
  // Shift+M moves focus into the popover, matching the Shift+G convention
  // already in useKeyboardNav. Typed with a selection made by keyboard, this is
  // the whole keyboard path to creating a mark.
  if (event.key === 'M' && event.shiftKey && !event.metaKey && !event.ctrlKey) {
    const target = event.target as HTMLElement | null
    if (target && /^(INPUT|TEXTAREA)$/.test(target.tagName)) return
    if (!popoverOpen.value) onSelect()
    if (popoverOpen.value) {
      event.preventDefault()
      nextTick(() => popoverEl.value?.focusFirst())
    }
  }
}

async function createMark(payload: { kind: AnnotationKind; body: string }) {
  if (!entry.value?.id) return
  marking.value = true
  markError.value = null
  try {
    // Waits for the server: the code and the anchor are its to decide, and
    // drawing a mark only to take it back is worse than a moment's wait.
    //
    // One request per block the selection covered. A mark addresses a block, so
    // a passage running across three paragraphs is three marks carrying the
    // same note — which is what the reader asked for, and what the queue can
    // actually anchor.
    const created: Annotation[] = []
    for (const seg of pendingSegments.value) {
      created.push(await createAnnotation(entry.value.id, {
        block_id: seg.blockId || undefined,
        quote: seg.quote,
        kind: payload.kind,
        body: payload.body,
      }))
    }
    annotations.value = [...annotations.value, ...created]
    closePopover()
    await repaint()
  } catch (e: any) {
    markError.value = e?.data?.error || e?.message || 'Could not save the mark'
  } finally {
    marking.value = false
  }
}

function cancelMark() {
  closePopover()
}

function closePopover() {
  popoverOpen.value = false
  selectionRect.value = null
  pendingSegments.value = []
}

function openThread(id: string) {
  activeThreadId.value = id
}

/**
 * A click on marked text opens that mark.
 *
 * Delegated from the card, because the spans are written into `v-html` output
 * by the overlay and no template ever sees them. Without this the underline
 * carried `cursor: pointer` and did nothing, while the only way to open a mark
 * you were pointing at was the header counter, the panel and then the row.
 */
function onCardClick(event: MouseEvent) {
  const el = (event.target as HTMLElement | null)?.closest?.(`span.${MARK_CLASS}`) as HTMLElement | null
  if (!el?.dataset.annotationId) return
  event.preventDefault()
  openThread(el.dataset.annotationId)
}

function closeThread() {
  activeThreadId.value = null
}

/** Optimistic, with a visible rollback — the same shape a checklist tick has,
 *  and for the same reason: it is a toggle on a row already on screen. */
async function setStatus(a: Annotation, status: Annotation['status'], reason = '') {
  const previous = { ...a }
  threadBusy.value = true
  patchLocal(a.id, { status })
  try {
    const updated = await patchAnnotation(a.id, reason ? { status, reason } : { status })
    patchLocal(a.id, updated)
  } catch (e: any) {
    patchLocal(a.id, previous)
    pushToast({ variant: 'error', title: 'Could not update the mark', message: e?.data?.error || e?.message || 'The change was rolled back.' })
  } finally {
    threadBusy.value = false
  }
}

/** Open the history comparing the revision a mark was made against with now. */
async function showMarkDiff(revision: number) {
  historyPanel.value?.prepareRange(revision, 0)
  showHistory.value = true
}

/** A mis-drag is the person's own mistake to undo, and dismissing it would
 *  record a judgement nobody made. */
async function removeMark(a: Annotation) {
  threadBusy.value = true
  try {
    await removeAnnotation(a.id)
    annotations.value = annotations.value.filter((x) => x.id !== a.id)
    activeThreadId.value = null
    await repaint()
  } catch (e: any) {
    pushToast({ variant: 'error', title: 'Could not delete the mark', message: e?.data?.error || e?.message || 'It is still there.' })
  } finally {
    threadBusy.value = false
  }
}

async function setBody(a: Annotation, body: string) {
  const previous = a.body
  patchLocal(a.id, { body })
  try {
    await patchAnnotation(a.id, { body })
  } catch {
    patchLocal(a.id, { body: previous })
  }
}

function patchLocal(id: string, changes: Partial<Annotation>) {
  annotations.value = annotations.value.map((a) => (a.id === id ? { ...a, ...changes } : a))
}

/**
 * A document write and its marks are refreshed together, always.
 *
 * Anchors are computed against the document as it stands, so refreshing one
 * without the other paints underlines at coordinates that no longer describe
 * anything. And when a write costs a mark its text, that is said out loud — a
 * doubt quietly buried by a rewrite is the failure this whole feature exists to
 * catch.
 */
async function refreshWithMarks() {
  const beforeRevision = currentRevision()
  const before = new Map(annotations.value.map((a) => [a.id, a.anchor?.state]))
  await refresh()
  const afterRevision = currentRevision()
  if (afterRevision > beforeRevision) {
    // A realtime write is not silently acknowledged. The reader may be in the
    // middle of the old text; the notice gives them an explicit review step.
    const serverState = unseenState()
    if (serverState?.kind === 'new') {
      // A background tab has not shown even the first revision yet. A remote
      // write makes it a newer new document, not "changed since r1" — r1 was
      // never seen either.
      viewNotice.value = serverState
    } else {
      const seenRevision = serverState?.kind === 'changed'
        ? serverState.seen_revision
        : Math.max(1, beforeRevision)
      viewNotice.value = {
        kind: 'changed',
        current_revision: afterRevision,
        seen_revision: seenRevision,
        unseen_revisions: Math.max(1, afterRevision - seenRevision),
      }
    }
  }
  await loadAnnotations()
  await repaint()

  const lost = annotations.value.filter((a) => {
    const now = a.anchor?.state
    return (now === 'drifted' || now === 'orphaned') && before.get(a.id) !== now
  })
  if (lost.length) {
    pushToast({
      variant: 'error',
      title: `${lost.length} ${lost.length === 1 ? 'mark' : 'marks'} lost their text in this edit`,
      message: `${lost.map((a) => a.code).join(', ')} — the text they were attached to changed or is gone.`,
    })
  }
}


// Mermaid rendering
const contentEl = ref<HTMLElement | null>(null)
watch([renderedContent, viewMode], () => {
  if (viewMode.value !== 'rendered') return
  nextTick(() => {
    if (contentEl.value) renderMermaidBlocks(contentEl.value)
  })
}, { immediate: false })
onMounted(() => {
  if (contentEl.value) renderMermaidBlocks(contentEl.value)
  window.addEventListener('keydown', onMarkKeydown)
  onBeforeUnmount(() => window.removeEventListener('keydown', onMarkKeydown))

  // Remembered per browser, like a folded section.
  const stored = localStorage.getItem('entry_marks_density')
  if (stored === 'all' || stored === 'open' || stored === 'off') marksMode.value = stored

  loadAnnotations().then(() => repaint())

  beginEntryVisit()
  document.addEventListener('visibilitychange', onDocumentVisible)
  onBeforeUnmount(() => document.removeEventListener('visibilitychange', onDocumentVisible))

  // Reflow moves the marked text; scrolling does not. Watching the card is what
  // keeps the gutter pins beside their sentences without a scroll listener.
  if (cardEl.value && typeof ResizeObserver !== 'undefined') {
    const observer = new ResizeObserver(() => {
      if (cardEl.value && marksVisible.value) markPositions.value = positions(cardEl.value)
    })
    observer.observe(cardEl.value)
    onBeforeUnmount(() => observer.disconnect())
  }
})

watch(marksMode, (mode) => {
  if (import.meta.client) localStorage.setItem('entry_marks_density', mode)
})

// Arriving at a different document by link keeps the page mounted.
watch(() => entry.value?.id, (nextID, previousID) => {
  activeThreadId.value = null
  loadAnnotations().then(() => repaint())
  if (nextID && previousID && nextID !== previousID) beginEntryVisit()
})

// Copy markdown
// Copy answers on the button that was pressed — a toast for a clipboard write
// is a notification about something the reader is already looking at. The
// download is the one that needs a toast, because a browser says nothing at
// all when a file arrives.
const copied = ref(false)

async function copyMarkdown() {
  if (!entry.value?.content) return

  // The document's prose, never the file: a clipboard paste goes into a message
  // or an editor, and ten lines of YAML in front of it are not what the button
  // promises. What it must not paste is the *stored* string, which on a blocks
  // document is JSON — the one-click control was handing over
  // `{"version":1,"blocks":[…]}` while the two-click Download produced
  // something readable, in a function called copyMarkdown.
  let text = entry.value.content
  if (isBlocks.value) {
    try {
      const file = await loadMarkdownFile()
      const end = file.startsWith('---\n') ? file.indexOf('\n---\n', 4) : -1
      text = end === -1 ? file : file.slice(end + 5).trimStart()
    } catch {
      // A failed fetch is not a clipboard problem and must not be reported as
      // one — the advice would be "use Download instead", which hits the same
      // route and fails the same way.
      useToasts().push({
        variant: 'error',
        title: 'Could not copy',
        message: 'The document could not be rendered as markdown. Try again.',
      })
      return
    }
  }

  try {
    await navigator.clipboard.writeText(text)
    copied.value = true
    setTimeout(() => { copied.value = false }, 2000)
  } catch {
    useToasts().push({
      variant: 'error',
      title: 'Could not copy',
      message: 'The browser refused clipboard access. Use Download instead.',
    })
  }
}

// The token travels as a header, so a plain <a href> arrives unauthenticated
// and the browser paints the JSON error over the app. `useDownload` fetches the
// bytes and hands a blob to a synthetic link; it also reads the filename the
// server chose out of the Content-Disposition header.
const {
  filename: downloadedName,
  error: downloadError,
  start: startDownload,
} = useDownload()

async function downloadMarkdown() {
  if (!entry.value) return
  // The panel closes on the click that started this, so there is no control
  // left to show a pending state on. A toast is the only surface still standing
  // — and a browser says nothing at all while a file is being prepared.
  const pendingId = useToasts().push({ variant: 'info', message: 'Preparing the document…', timeout: 0 })
  const ok = await startDownload(
    `/api/entries/${entry.value.id}/markdown`,
    `${entry.value.code || 'entry'}.md`,
  )
  useToasts().dismiss(pendingId)
  // Leaving the page aborts the request, and an abort returns false with no
  // error — so without this guard, navigating away planted a permanent red
  // toast on the next page blaming the server for something the reader did.
  // The export page has always guarded this; the new caller had not.
  if (!ok && !downloadError.value) return
  if (ok) {
    // The filename goes in the message, not the title: that is the slot with
    // `overflow-wrap: anywhere`, and its own comment says why — "a filename has
    // no spaces to break at". The caveat rides with it, and only for a document
    // that actually holds a reference; on one that has none it was a warning
    // about nothing, three deep if you downloaded three documents.
    const carriesRefs = (entry.value.content || '').includes('[[')
    useToasts().push({
      variant: 'success',
      title: 'Downloaded',
      message: [
        downloadedName.value || '',
        carriesRefs ? 'References like [[E3]] are written exactly as stored, so they will not resolve in another vault.' : '',
      ].filter(Boolean).join(' — '),
    })
    return
  }
  useToasts().push({
    variant: 'error',
    title: 'Download failed',
    message: downloadError.value?.message || 'The server did not return the document.',
    action: retryDownload(downloadError.value?.status),
    timeout: 0,
  })
}

// The two export pages both attach one of these; a failed download here offered
// nothing at all, so a 401 said "sign in again" and gave you no way to.
function retryDownload(status?: number) {
  if (status === 401 || status === 403) {
    return { label: 'Sign in', onClick: () => { navigateTo('/login') } }
  }
  if (status === 404) {
    return { label: 'Reload', onClick: () => { refresh() } }
  }
  return { label: 'Try again', onClick: () => { downloadMarkdown() } }
}

// Opening the history from a menu item is a focus problem: the item unmounts
// with the menu, so whatever the panel saved to restore focus to is a detached
// node by the time it closes. The trigger is the only element still standing.
const docMenu = ref<{ focusTrigger: () => void } | null>(null)

function openHistory() {
  showHistory.value = true
}

function reviewUnseenChanges() {
  const state = viewNotice.value
  if (!state || state.kind !== 'changed') return
  historyPanel.value?.prepareRange(state.seen_revision, state.current_revision)
  showHistory.value = true
  // A realtime revision deliberately stayed unseen until the reader engaged
  // with it. Opening its exact comparison is that engagement.
  void markDisplayedRevision()
}

function cancelDelete() {
  showDeleteConfirm.value = false
  docMenu.value?.focusTrigger()
}

function onHistoryClosed() {
  showHistory.value = false
  docMenu.value?.focusTrigger()
}

// --- Edit mode ---
const { authFetch } = useAuth()
const rtBase = useRuntimeConfig().public.apiBase || ''

/*
 * Declared here, below `viewMode`, `authFetch` and `rtBase`, and not beside the
 * source view where it is used.
 *
 * The watcher runs immediately, so placing it above those three read them
 * before initialisation and the whole page died with "Cannot access 'de' before
 * initialization" — a temporal dead zone, wearing a minified name.
 */
/*
 * The front matter the source view shows is fetched, not built here.
 *
 * A second YAML writer in TypeScript would drift from the one that writes the
 * file — on a list with one element, on a declared field nobody filled, on a
 * date — and the drift would be invisible until somebody compared a download
 * with the screen. So the screen asks the server for the file and shows the
 * header off the front of it. What you read is what you would get.
 */
const sourceFrontmatter = ref('')
const markdownFile = ref('')
const markdownFor = ref('')

/**
 * The document as a file, fetched once per version and shared by everything
 * that needs it: the source view's header and the Copy button.
 *
 * Nothing here renders markdown. A second serializer on the client would drift
 * from the one that writes the file — on a list of one, on an unfilled field,
 * on a block type added later — and it would drift invisibly, which is the
 * whole reason the header is fetched rather than composed.
 *
 * Keyed on the document *and its version*: keyed on the id alone, the guard
 * rejected the one case the watcher exists for — a metadata edit changes
 * updated_at, the watcher fires, and the header kept showing the values from
 * before the save while the body below re-rendered with the new ones.
 */
async function loadMarkdownFile(): Promise<string> {
  const id = entry.value?.id
  if (!id) return ''
  const key = `${id}:${entry.value?.updated_at ?? ''}`
  if (markdownFor.value === key) return markdownFile.value
  const file = await authFetch<string>(`${rtBase}/api/entries/${id}/markdown`, { responseType: 'text' })
  markdownFile.value = typeof file === 'string' ? file : ''
  markdownFor.value = key
  return markdownFile.value
}

async function loadFrontmatter() {
  try {
    const file = await loadMarkdownFile()
    const end = file.startsWith('---\n') ? file.indexOf('\n---\n', 4) : -1
    sourceFrontmatter.value = end === -1 ? '' : file.slice(0, end + 5)
  } catch {
    // The body is the point; a header we could not fetch simply does not show.
    sourceFrontmatter.value = ''
  }
}

// Fetched when the reader asks for source, and again whenever the document
// changes underneath — a metadata edit rewrites the header, not the body.
watch(
  () => [viewMode.value, entry.value?.id, entry.value?.updated_at],
  () => {
    if (viewMode.value === 'source' && !isBlocks.value) loadFrontmatter()
    // Nothing to clear: the cache is keyed by version, so a return to source
    // either reuses a current file or fetches a fresh one.
  },
  { immediate: true },
)
const editing = ref(false)
const saving = ref(false)
const editForm = reactive({
  title: '',
  description: '',
  content: '',
  tagsRaw: '',
})

function startEditing() {
  remoteChangedWhileEditing.value = false
  editForm.title = entry.value?.title ?? ''
  editForm.description = entry.value?.description ?? ''
  editForm.content = entry.value?.content ?? ''
  editForm.tagsRaw = (entry.value?.tags ?? []).join(', ')
  editing.value = true
}

function cancelEditing() {
  editing.value = false
  remoteChangedWhileEditing.value = false
}

async function discardDraftForRemote() {
  editing.value = false
  remoteChangedWhileEditing.value = false
  await refresh()
  viewNotice.value = unseenState() ?? viewNotice.value
  await markDisplayedRevision()
}

// An open editor whose text differs from what was loaded is work that has not
// been written down anywhere else. Losing access to this research must not take
// it off the screen — the save will fail either way, but the reader can still
// copy what they wrote.
useUnsavedWork(() => {
  if (!editing.value) return false
  const e = entry.value
  return editForm.title !== (e?.title ?? '')
    || editForm.description !== (e?.description ?? '')
    || editForm.content !== (e?.content ?? '')
    || editForm.tagsRaw !== ((e?.tags ?? []) as string[]).join(', ')
})

async function saveEntry() {
  if (!entry.value || saving.value) return
  saving.value = true
  try {
    const tags = editForm.tagsRaw
      .split(',')
      .map((t: string) => t.trim())
      .filter(Boolean)

    await authFetch(`${rtBase}/api/entries/${entry.value.id}`, {
      method: 'PUT',
      body: {
        title: editForm.title,
        description: editForm.description || null,
        content: editForm.content,
        tags,
      },
    })
    editing.value = false
    remoteChangedWhileEditing.value = false
    await refresh()
    await markDisplayedRevision()
  } catch (e: any) {
    console.error('Failed to save entry:', e)
    // A native dialog over an editor full of unsaved work is the worst place
    // in the product for one, and it blocks the render loop while it is up.
    // The draft is still in `editForm`, so say so.
    useToasts().push({
      variant: 'error',
      title: 'Could not save',
      message: (e?.data?.error || e?.message || 'The server refused the change.')
        + ' Your text is still here — try again, or copy it somewhere safe.',
      timeout: 0,
    })
  } finally {
    saving.value = false
  }
}

// --- Status change ---
const statusOpen = ref(false)
const statusTriggerEl = ref<HTMLElement | null>(null)
const statusDropdownStyle = ref<Record<string, string>>({})

function toggleStatus() {
  statusOpen.value = !statusOpen.value
  if (statusOpen.value && statusTriggerEl.value) {
    const rect = statusTriggerEl.value.getBoundingClientRect()
    statusDropdownStyle.value = {
      position: 'fixed',
      top: `${rect.bottom + 4}px`,
      left: `${rect.left}px`,
    }
  }
}

async function changeStatus(newStatus: string) {
  statusOpen.value = false
  if (!entry.value || entry.value.status === newStatus) return
  const prev = entry.value.status
  // Optimistic update
  data.value.data = { ...entry.value, status: newStatus }
  try {
    await authFetch(`${rtBase}/api/entries/${entry.value.id}`, {
      method: 'PUT',
      body: { status: newStatus },
    })
    await refresh()
    await markDisplayedRevision()
  } catch (e: any) {
    // Rollback on error
    data.value.data = { ...entry.value, status: prev }
    // The server refuses `completed` while required metadata is unanswered and
    // names the fields. Without this the badge flipped and flipped back with no
    // message at all — the one refusal in this product that the user can act on
    // was the one they were never told about.
    if (e?.data?.code === 'metadata_incomplete') {
      incompleteFields.value = e.data.missing_required ?? []
      return
    }
    useToasts().push({
      variant: 'error',
      title: 'Status not changed',
      message: e?.data?.error || e?.message || 'The server refused it.',
    })
  }
}

// The fields the server named, held while the user decides. Completing anyway
// is a decision, so it is asked for rather than retried.
const incompleteFields = ref<string[]>([])

async function completeAnyway() {
  if (!entry.value) return
  const fields = incompleteFields.value
  incompleteFields.value = []
  try {
    await authFetch(`${rtBase}/api/entries/${entry.value.id}`, {
      method: 'PUT',
      body: { status: 'completed', allow_incomplete: true },
    })
    await refresh()
    await markDisplayedRevision()
  } catch (e: any) {
    incompleteFields.value = fields
    useToasts().push({
      variant: 'error',
      title: 'Status not changed',
      message: e?.data?.error || e?.message || 'The server refused it.',
    })
  }
}


// --- Revision history ---
const showHistory = ref(false)
const historyPanel = ref<{
  reload: () => Promise<void>
  refreshList: () => Promise<void>
  prepareRange: (from: number, to: number) => void
} | null>(null)
const restoreTarget = ref<number | null>(null)
const restoring = ref(false)
const restoreFailed = ref(false)

const provenance = computed(() => {
  const d: any = data.value
  return d?.revision
    ? { revision: d.revision, author_kind: d.author_kind, revised_at: d.revised_at, author_name: d.author_name }
    : null
})

function askRestore(revision: number) {
  restoreFailed.value = false
  restoreTarget.value = revision
}

async function confirmRestore() {
  if (!entry.value || restoreTarget.value === null) return
  restoring.value = true
  restoreFailed.value = false
  try {
    await authFetch(`${rtBase}/api/entries/${entry.value.id}/revisions/${restoreTarget.value}/restore`, {
      method: 'POST',
    })
    restoreTarget.value = null
    await refresh()
    await markDisplayedRevision()
    // The restore itself is a new revision, so the open panel is now stale.
    await historyPanel.value?.reload()
  } catch (e: any) {
    // The confirm closes either way: leaving it open with the button reset is
    // indistinguishable from a click that never registered. The panel shows
    // what happened, where the reader is looking.
    restoreTarget.value = null
    restoreFailed.value = true
    console.error('Failed to restore revision:', e)
  } finally {
    restoring.value = false
  }
}

// --- Delete ---
const showDeleteConfirm = ref(false)
const deleting = ref(false)

async function deleteEntry() {
  if (!entry.value) return
  deleting.value = true
  try {
    await authFetch(`${rtBase}/api/entries/${entry.value.id}`, {
      method: 'DELETE',
    })
    showDeleteConfirm.value = false
    router.push(`/research/${researchSlug.value}?section=${entry.value.section_id}`)
  } catch (e: any) {
    console.error('Failed to delete entry:', e)
    useToasts().push({ variant: 'error', title: 'Could not delete', message: e?.data?.error || e?.message || 'The server refused it.', timeout: 0 })
  } finally {
    deleting.value = false
  }
}

// Cross-references
const { data: refsData } = useApi<{ outgoing: any[]; incoming: any[] }>(
  computed(() => entry.value ? `/api/entries/${entry.value.id}/crossrefs` : `/api/entries/__none__/crossrefs`)
)
const outgoingRefs = computed(() => refsData.value?.outgoing ?? [])
const incomingRefs = computed(() => refsData.value?.incoming ?? [])

// External links
const { data: linksData } = useApi<{ data: any[] }>(
  computed(() => entry.value ? `/api/entries/${entry.value.id}/links` : `/api/entries/__none__/links`)
)
const externalLinks = computed(() => linksData.value?.data ?? [])

// Related by tags
const { data: relatedData } = useApi<{ data: any[] }>(
  computed(() => entry.value ? `/api/entries/${entry.value.id}/related` : `/api/entries/__none__/related`)
)
const relatedEntries = computed(() => relatedData.value?.data ?? [])

// Sibling entries for prev/next navigation
const { data: siblingsData } = useApi<{ data: any[] }>(
  computed(() =>
    entry.value?.section_id
      ? `/api/researches/${id}/sections/${entry.value.section_id}/entries`
      : `/api/researches/__none__/sections/__none__/entries`
  )
)
const siblings = computed(() => siblingsData.value?.data ?? [])
/**
 * Where this entry sits among its section's entries.
 *
 * It used to compare `e.id` against `entryId` — the route param, which is a
 * short code (`E3`) on every link the app builds, against a sibling's UUID. So
 * the index was -1 for anyone who arrived by clicking, which is everyone:
 * "Prev" required `> 0` and never rendered, and "Next" required
 * `< length - 1`, which -1 satisfies, so it pointed at the section's first
 * entry from every page — including from that entry, where it linked to itself.
 *
 * Matching on the loaded entry's own id sidesteps the question of which
 * identity the URL happened to carry.
 */
const currIndex = computed(() => {
  const id = entry.value?.id
  return id ? siblings.value.findIndex((e: any) => e.id === id) : -1
})
const prevEntry = computed(() => currIndex.value > 0 ? siblings.value[currIndex.value - 1] : null)
const nextEntry = computed(() =>
  currIndex.value >= 0 && currIndex.value < siblings.value.length - 1
    ? siblings.value[currIndex.value + 1]
    : null,
)
</script>

<style scoped>
/* The list bleeds to the dialog's edges; its own rows keep the inset the header
   above them has. */
.marks-panel__list {
  gap: 0;
}

/* css-discipline: literal-ok — the source view uses the One Dark palette,
   which is a palette in its own right rather than the product's. It is not
   expressible in the design tokens and should not be: a syntax colour that
   drifted with the brand would stop meaning "string" or "keyword". */
.edit-remote-change {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: var(--space-3);
  flex-wrap: wrap;
  padding: var(--space-3) var(--space-4);
  margin-bottom: var(--space-3);
  border: 1px solid var(--color-warning);
  border-radius: var(--radius);
  background: var(--color-surface);
  color: var(--color-text);
  font-size: var(--type-sm);
}

.entry-session-link {
  display: inline-flex;
  align-items: center;
  gap: var(--space-2);
  margin-top: var(--space-3);
  padding: var(--space-1) var(--space-3);
  font-size: var(--type-xs);
  color: var(--color-text-muted);
  background: var(--color-surface);
  border: 1px solid var(--color-border);
  border-radius: var(--radius-sm);
  text-decoration: none;
  transition: all var(--transition-fast);
}
.entry-session-link:hover { border-color: rgba(var(--color-warning-rgb), 0.3); color: var(--color-text); }
.entry-session-link svg { opacity: 0.6; }

/* Delete button */
.btn-delete {
  color: var(--color-text-muted);
  padding: 0.35rem 0.5rem;
}
.btn-delete:hover { color: var(--color-danger); }

/* Status dropdown */
.status-dropdown-wrap {
  position: relative;
}
/* Three controls stand in this row — the status picker, Edit and the ⋯ — and
   they came out at three heights: 26px from `.btn-sm`, 30px from `.btn-icon`,
   and whatever this one's padding added up to. `.btn-icon`'s own comment claims
   it matches "a text button beside it", which is true beside `.btn` and false
   beside `.btn-sm`. So the row is `.btn` and `.btn-icon`, both --control-h, and
   this one says so rather than deriving it. */
.status-dropdown-trigger {
  display: flex;
  align-items: center;
  gap: var(--space-1);
  height: var(--control-h);
  background: none;
  border: none;
  cursor: pointer;
  padding: 0.2rem 0.4rem;
  border-radius: var(--radius-sm);
  transition: background var(--transition-fast);
}
.btn.is-copied { color: var(--color-success); border-color: var(--color-success); }

.status-dropdown-trigger:hover { background: var(--color-surface-hover); }
.status-dropdown-trigger svg { color: var(--color-text-muted); }

/* Edit mode */
.edit-header-bar {
  display: flex;
  flex-direction: column;
  gap: var(--space-3);
  margin-top: var(--space-4);
}
.edit-field {
  display: flex;
  flex-direction: column;
  gap: var(--space-1);
}
.edit-label {
  font-size: var(--type-xs);
  font-weight: var(--weight-semibold);
  color: var(--color-text-muted);
  text-transform: uppercase;
  letter-spacing: 0.04em;
}
.edit-input {
  background: var(--color-surface);
  border: 1px solid var(--color-border);
  border-radius: var(--radius-sm);
  padding: var(--space-2) var(--space-3);
  color: var(--color-text);
  font-size: var(--type-sm);
  font-family: inherit;
  transition: border-color var(--transition-fast);
}
.edit-input:focus {
  outline: none;
  border-color: var(--color-primary);
}
.edit-actions-bar {
  display: flex;
  gap: var(--space-2);
  padding-top: var(--space-2);
}

/* Editor wrapper */
.editor-wrap {
  margin-top: var(--space-4);
  border-radius: var(--radius-lg);
  overflow: hidden;
  border: 1px solid var(--color-border);
}

/* md-editor-v3 dark theme overrides */
.editor-wrap :deep(.md-editor) {
  --md-bk-color: var(--color-bg) !important;
  --md-border-color: var(--color-border) !important;
  --md-color: var(--color-text) !important;
  --md-hover-color: var(--color-surface-hover) !important;
  --md-bk-hover-color: var(--color-surface-hover) !important;
  font-family: 'JetBrains Mono', 'Fira Code', monospace;
}
.editor-wrap :deep(.md-editor-toolbar-wrapper) {
  background: var(--color-surface) !important;
  border-bottom: 1px solid var(--color-border) !important;
}
.editor-wrap :deep(.md-editor-content) {
  background: var(--color-bg) !important;
}
.editor-wrap :deep(.md-editor-preview-wrapper) {
  background: var(--color-bg) !important;
  padding: var(--space-6) !important;
}
.editor-wrap :deep(.md-editor-input-wrapper) {
  background: var(--color-bg) !important;
}
.editor-wrap :deep(.cm-editor) {
  background: var(--color-bg) !important;
  font-family: 'JetBrains Mono', 'Fira Code', monospace !important;
}
.editor-wrap :deep(.cm-gutters) {
  background: var(--color-surface) !important;
  border-right: 1px solid var(--color-border) !important;
}

/* View toggle */
.marks-toggle { display: inline-flex; align-items: center; gap: var(--space-2); padding-inline: var(--space-2); }
.view-toggle {
  display: inline-flex;
  flex-wrap: wrap;
  max-width: 100%;
  row-gap: var(--space-1);
  gap: 0;
  margin-bottom: var(--space-4);
  background: var(--color-surface);
  border: 1px solid var(--color-border);
  border-radius: var(--radius-sm);
  padding: 2px;
}
.view-toggle .btn {
  color: var(--color-text-muted);
  border: none;
  border-radius: calc(var(--radius-sm) - 2px);
  background: transparent;
}
.view-toggle .btn:hover {
  color: var(--color-text);
  background: transparent;
  transform: none;
  box-shadow: none;
}
.view-toggle .btn.active {
  background: var(--color-surface-hover);
  color: var(--color-text);
}

/* Content */
.source-view {
  background: none;
  padding: 0;
  overflow-x: auto;
  font-size: var(--type-xs);
  line-height: 1.7;
  white-space: pre-wrap;
  word-break: break-word;
  color: var(--color-text-muted);
  margin: 0;
}
.source-view code { background: none; padding: 0; font-size: inherit; }

/* Markdown syntax highlighting */
.source-view :deep(.md-heading-marker) { color: var(--syntax-marker); font-weight: var(--weight-bold); }
.source-view :deep(.md-heading) { color: var(--syntax-heading); font-weight: var(--weight-semibold); }
.source-view :deep(.md-bold) { color: var(--syntax-emphasis); font-weight: var(--weight-semibold); }
.source-view :deep(.md-italic) { color: var(--syntax-keyword); font-style: italic; }
.source-view :deep(.md-inline-code) { color: var(--syntax-string); background: rgba(var(--color-success-rgb), 0.08); border-radius: var(--radius-xs); padding: 0 0.15em; }
.source-view :deep(.md-link) { color: var(--syntax-link); }
.source-view :deep(.md-url) { color: var(--syntax-link); }
.source-view :deep(.md-image) { color: var(--syntax-keyword); }
.source-view :deep(.md-crossref) { color: var(--color-primary); background: rgba(var(--color-primary-rgb), 0.1); border-radius: var(--radius-xs); padding: 0 0.15em; }
.source-view :deep(.md-blockquote) { color: var(--syntax-comment); font-style: italic; border-left: 2px solid var(--syntax-comment); padding-left: 0.75em; display: inline-block; }
.source-view :deep(.md-list-marker) { color: var(--syntax-marker); font-weight: var(--weight-semibold); }
.source-view :deep(.md-hr) { color: var(--syntax-comment); }
.source-view :deep(.md-fence) { color: var(--syntax-string); }
.source-view :deep(.md-code-line) { color: var(--color-text); opacity: 0.85; }

/* Skeleton */
.skeleton-header { height: 60px; margin-bottom: var(--space-4); }

/* Responsive */
/* The header reads as a header: dimmer than the body, and separated from it, so
   nobody mistakes it for the first paragraph of the document. */
.source-frontmatter {
  display: block;
  color: var(--color-text-muted);
  border-bottom: 1px solid var(--color-border);
  padding-bottom: var(--space-3);
  margin-bottom: var(--space-3);
}

/* The header stacks below this width and the trigger lands at the left edge,
   where a right-anchored panel hangs off the viewport. */
@media (max-width: 768px) {
  .doc-more { margin-left: auto; }
}

@media (max-width: 768px) {
  .title-with-code { flex-wrap: wrap; gap: var(--space-2); }
  .entry-content { --entry-pad: var(--space-4); }
}

/* Print */
@media print {
  .entry-content { --entry-pad: 0; border: none; }
  .entry-tags { margin-bottom: var(--space-2); }
}
</style>

<style>
/* Teleported status dropdown — must be non-scoped */
.status-dropdown-overlay {
  position: fixed;
  inset: 0;
  z-index: 9999;
}
.status-dropdown {
  background: var(--color-surface);
  border: 1px solid var(--color-border-strong);
  border-radius: var(--radius);
  padding: var(--space-1);
  box-shadow: var(--shadow-2);
  min-width: 140px;
}
.status-option {
  display: block;
  width: 100%;
  background: none;
  border: none;
  padding: var(--space-2) var(--space-3);
  cursor: pointer;
  border-radius: var(--radius-sm);
  text-align: left;
  transition: background 0.15s;
}
.status-option:hover { background: var(--color-surface-hover); }
.status-option.active { background: var(--color-surface-hover); }
</style>
