<template>
  <div v-if="pending" class="skeleton-page">
    <div class="skeleton-card skeleton-header"></div>
    <div class="layout-sidebar">
      <div class="skeleton-card skeleton-sidebar"></div>
      <div>
        <div v-for="i in 3" :key="i" class="skeleton-card skeleton-entry"></div>
      </div>
    </div>
  </div>

  <div v-else-if="research">
    <!-- Header -->
    <PageHeader
      :crumbs="[{ label: 'Projects', to: { name: 'index' } }, { label: research.name }]"
      :code="research.code"
      :title="research.name"
    >
      <template #actions>
        <StatusBadge :status="research.status" />
          <TeamChip v-if="showTeamChip" :name="research.team_name" />
          <TeamViewerNotice v-if="readOnlyReason" :reason="readOnlyReason" :team-name="research.team_name" />

          <!-- Icon nav buttons -->
          <NuxtLink v-if="entryUpdates.count > 0" :to="updatesPath(researchSlug)" class="btn btn-icon" title="New and changed documents" :aria-label="`${entryUpdates.count} new or changed documents`">
            <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M3 12a9 9 0 1 0 3-6.7L3 8"/><path d="M3 3v5h5"/><path d="M12 7v5l3 2"/></svg>
            <span class="btn-count" aria-hidden="true">{{ entryUpdates.count }}</span>
          </NuxtLink>
          <NuxtLink :to="`/research/${researchSlug}/tasks`" class="btn btn-icon" title="Tasks" aria-label="Tasks">
            <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M9 11l3 3L22 4"/><path d="M21 12v7a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h11"/></svg>
            <span v-if="tasks.length" class="btn-count">{{ tasks.length }}</span>
          </NuxtLink>
          <NuxtLink
            v-if="annotationsPath(researchSlug)"
            :to="annotationsPath(researchSlug)"
            class="btn btn-icon"
            title="Marks"
            aria-label="Marks"
          >
            <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M4 4h16v12H8l-4 4z"/><path d="M8 9h8"/><path d="M8 12.5h5"/></svg>
            <span v-if="openMarks" class="btn-count">{{ openMarks }}</span>
          </NuxtLink>

          <!-- Keep sharing and its live-link count visible in the toolbar. -->
          <button v-if="canWrite" class="btn" @click="openShares()">
            <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><circle cx="18" cy="5" r="3"/><circle cx="6" cy="12" r="3"/><circle cx="18" cy="19" r="3"/><line x1="8.59" y1="13.51" x2="15.42" y2="17.49"/><line x1="15.41" y1="6.51" x2="8.59" y2="10.49"/></svg>
            Share
            <span v-if="activeShareCount" class="btn-count">{{ activeShareCount }}</span>
          </button>

          <!-- More menu -->
          <ActionMenu title="Project menu">
            <NuxtLink :to="`/research/${researchSlug}/roadmaps`" class="action-menu-item">
              <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M2 3h6a4 4 0 0 1 4 4v14a3 3 0 0 0-3-3H2z"/><path d="M22 3h-6a4 4 0 0 0-4 4v14a3 3 0 0 1 3-3h7z"/></svg>
              Roadmaps
              <span v-if="roadmaps.length" class="btn-count">{{ roadmaps.length }}</span>
            </NuxtLink>
            <NuxtLink :to="mindmapPath(researchSlug)" class="action-menu-item">
              <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="3"/><circle cx="4" cy="6" r="2"/><circle cx="20" cy="6" r="2"/><circle cx="4" cy="18" r="2"/><circle cx="20" cy="18" r="2"/><path d="M9.5 10.5 5.5 7.5"/><path d="M14.5 10.5l4-3"/><path d="M9.5 13.5 5.5 16.5"/><path d="M14.5 13.5l4 3"/></svg>
              Mind map
            </NuxtLink>
            <NuxtLink :to="graphPath(researchSlug)" class="action-menu-item">
              <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><circle cx="6" cy="6" r="3"/><circle cx="18" cy="6" r="3"/><circle cx="6" cy="18" r="3"/><circle cx="18" cy="18" r="3"/><path d="M8.5 8.5 15.5 15.5"/><path d="M15.5 8.5 8.5 15.5"/><path d="M6 9v6"/><path d="M18 9v6"/></svg>
              Knowledge graph
            </NuxtLink>
            <NuxtLink :to="`/research/${researchSlug}/sessions`" class="action-menu-item">
              <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M21 15a2 2 0 0 1-2 2H7l-4 4V5a2 2 0 0 1 2-2h14a2 2 0 0 1 2 2z"/></svg>
              Sessions
            </NuxtLink>
            <div class="action-menu-divider"></div>
            <NuxtLink :to="`/research/${researchSlug}/settings`" class="action-menu-item">
              <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="3"/><path d="M19.4 15a1.65 1.65 0 0 0 .33 1.82l.06.06a2 2 0 0 1-2.83 2.83l-.06-.06a1.65 1.65 0 0 0-1.82-.33 1.65 1.65 0 0 0-1 1.51V21a2 2 0 0 1-4 0v-.09A1.65 1.65 0 0 0 9 19.4a1.65 1.65 0 0 0-1.82.33l-.06.06a2 2 0 0 1-2.83-2.83l.06-.06A1.65 1.65 0 0 0 4.68 15a1.65 1.65 0 0 0-1.51-1H3a2 2 0 0 1 0-4h.09A1.65 1.65 0 0 0 4.6 9a1.65 1.65 0 0 0-.33-1.82l-.06-.06a2 2 0 0 1 2.83-2.83l.06.06A1.65 1.65 0 0 0 9 4.68a1.65 1.65 0 0 0 1-1.51V3a2 2 0 0 1 4 0v.09a1.65 1.65 0 0 0 1 1.51 1.65 1.65 0 0 0 1.82-.33l.06-.06a2 2 0 0 1 2.83 2.83l-.06.06A1.65 1.65 0 0 0 19.4 9a1.65 1.65 0 0 0 1.51 1H21a2 2 0 0 1 0 4h-.09a1.65 1.65 0 0 0-1.51 1z"/></svg>
              Settings
            </NuxtLink>
            <NuxtLink :to="`/research/${researchSlug}/export`" class="action-menu-item">
              <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4"/><polyline points="7 10 12 15 17 10"/><line x1="12" y1="15" x2="12" y2="3"/></svg>
              Export
            </NuxtLink>
            <button class="action-menu-item" :disabled="exporting" @click="downloadPortableJSON()">
              <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z"/><polyline points="14 2 14 8 20 8"/><line x1="12" y1="18" x2="12" y2="12"/><polyline points="9 15 12 18 15 15"/></svg>
              {{ exporting ? 'Saving...' : 'Download JSON' }}
            </button>
            <NuxtLink
              v-if="research.team_id && authEnabled"
              :to="`/teams/${research.team_id}`"
              class="action-menu-item"
            >
              <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M17 21v-2a4 4 0 0 0-4-4H5a4 4 0 0 0-4 4v2"/><circle cx="9" cy="7" r="4"/><path d="M23 21v-2a4 4 0 0 0-3-3.87"/><path d="M16 3.13a4 4 0 0 1 0 7.75"/></svg>
              Members
            </NuxtLink>
            <button v-if="canAdmin && authEnabled" class="action-menu-item" @click="openTransfer()">
              <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><polyline points="17 1 21 5 17 9"/><path d="M3 11V9a4 4 0 0 1 4-4h14"/><polyline points="7 23 3 19 7 15"/><path d="M21 13v2a4 4 0 0 1-4 4H3"/></svg>
              Move to team…
            </button>
            <div v-if="canWrite" class="action-menu-divider"></div>
            <button
              v-if="canWrite"
              class="action-menu-item"
              :class="{ 'action-menu-item--danger': research.status !== 'archived' }"
              @click="toggleArchive()"
            >
              <svg v-if="research.status === 'archived'" width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><polyline points="1 4 1 10 7 10"/><path d="M3.51 15a9 9 0 1 0 2.13-9.36L1 10"/></svg>
              <svg v-else width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><polyline points="21 8 21 21 3 21 3 8"/><rect x="1" y="3" width="22" height="5"/><line x1="10" y1="12" x2="14" y2="12"/></svg>
              {{ research.status === 'archived' ? 'Restore' : 'Archive' }}
            </button>
          </ActionMenu>
      </template>
      <template #below>
        <p v-if="research.goal" class="card-meta mt-2">{{ research.goal }}</p>
      </template>
    </PageHeader>

    <p v-if="entryUpdatesFetchError" class="inline-error inline-error--action updates-load-error" role="alert">
      <span>Could not load document updates. Counts and badges may be unavailable.</span>
      <button type="button" class="btn btn-sm" @click="reloadUpdates()">Try again</button>
    </p>

    <!-- What to continue. It takes the place of the session grid rather than
         standing above it: two rows of session cards give the page two focal
         points and neither wins. The block's head names the selected session
         and links to it, which is the grid's job moved. -->
    <ResearchResumeBlock
      v-if="!resume.unavailable.value"
      :summary="resume.summary.value"
      :research-slug="researchSlug"
      :loading="resume.loading.value"
      :refreshing="resume.refreshing.value"
      :error="resume.error.value"
      :archived="research.status === 'archived' || research.status === 'completed'"
      :updates-by-entry="updatesByEntry"
      :can-write="canWrite"
      @refresh="resume.refresh()"
      @select-session="resume.selectSession($event)"
    />

    <!-- The fallback for one state only: a server with no resume route, where
         the block does not render at all. A failed request is the block's own
         business — it says so and offers a retry — and showing both would give
         the page two answers to one question. -->
    <ResearchActiveSessionsGrid
      v-if="resume.unavailable.value"
      :sessions="activeSessions" :research-slug="researchSlug"
      :research-id="research?.id"
      :research-name="research?.name" />

    <!-- Sidebar layout: sections + entries -->
    <div class="layout-sidebar">
      <!-- Sidebar -->
      <ResearchSidebar
        :sections="sections"
        :active-section="activeSection"
        :total-entry-count="totalEntryCount"
        :links-total="researchLinksTotal"
        @update:active-section="activeSection = $event"
      />

      <!-- Main: entries -->
      <div>
        <!-- All entries view -->
        <ResearchEntriesView
          v-if="isAllEntries"
          :entries="allEntries"
          :sections="sections"
          :research-slug="researchSlug"
          :research-id="research?.id"
          :research-name="research?.name"
          :loading="allEntriesPending"
          mode="all"
          :tags="globalTags"
          :updates="updatesByEntry"
        />

        <!-- Single section view -->
        <ResearchEntriesView
          v-else-if="currentSection"
          :entries="entries"
          :sections="sections"
          :research-slug="researchSlug"
          :research-id="research?.id"
          :research-name="research?.name"
          :loading="entriesPending"
          mode="section"
          :section-info="currentSection"
          :tags="[]"
          :updates="updatesByEntry"
          @imported="onImported"
        />

        <!-- External links view -->
        <ResearchExternalLinksView
          v-else-if="isLinksView"
          :groups="researchLinksGrouped"
          :loading="researchLinksLoading"
        />

        <EmptyState
          v-else
          icon="&#x1F448;"
          title="Select a section"
          description="Choose a section from the sidebar to view its documents."
        />
      </div>
    </div>
  </div>

  <EmptyState v-else icon="&#x1F50D;" title="Project not found" />
    <ResearchShareDialog
      :visible="sharesOpen"
      :research-name="research?.name || ''"
      :shares="shares"
      :loading="sharesLoading"
      :creating="creatingShare"
      :error="shareError"
      :issued-url="issuedShareUrl"
      :busy-id="revokingShareId || savingShareId"
      :recoverable-links="recoverableShareLinks"
      :saving="!!savingShareId"
      :save-error="shareSaveError"
      :saved-tick="shareSavedTick"
      @create="createShare"
      @revoke="askRevokeShare"
      @update="updateShare"
      @refresh="refreshSharesAfterFailure"
      @clear-error="shareSaveError = ''"
      @dismiss-reveal="issuedShareUrl = ''"
      @close="closeShares"
    />
    <!-- Widening confirms; narrowing does not. Neither is optimistic: a row
         that showed "sessions" before the server agreed could lie about what a
         stranger can read. -->
    <ConfirmModal
      :visible="!!pendingShareUpdate"
      title="Show more to everyone who has this link?"
      :message="wideningMessage"
      confirm-label="Show more"
      variant="danger"
      :loading="!!savingShareId"
      @confirm="confirmShareUpdate"
      @cancel="pendingShareUpdate = null"
    />
    <ConfirmModal
      :visible="!!shareToRevoke"
      title="Revoke this link?"
      message="Anyone holding it stops being able to open this project. A page someone already has open goes blank within a minute. This cannot be undone — you can always make a new link."
      confirm-label="Revoke"
      variant="danger"
      :loading="!!revokingShareId"
      @confirm="confirmRevokeShare"
      @cancel="shareToRevoke = null"
    />
    <ResearchTransferModal
      :visible="transferOpen"
      :research="{ code: research?.code, name: research?.name || '' }"
      :current-team-id="research?.team_id || ''"
      :current-team-name="research?.team_name || ''"
      :teams="[...ownedTeams]"
      :busy="transferring"
      :error="transferError"
      @transfer="transfer"
      @close="transferOpen = false"
    />
</template>

<script setup lang="ts">
import type { ShareInclude } from '~/composables/useShare'
const route = useRoute()
const id = route.params.id as string

// Research data
const { data: researchData, pending } = await useApi<{ data: any }>(`/api/researches/${id}`)

const research = computed(() => researchData.value?.data?.research)

// The role rides on the payload every research page already awaits, so no
// screen ever renders edit controls and then takes them away.
const { authEnabled } = useAuth()
const { canWrite, canAdmin, readOnlyReason, setFromResearch } = useResearchRole()
watch(research, (r) => setFromResearch(r), { immediate: true })

const showTeamChip = computed(() => !!research.value?.team_name && !research.value?.team_is_personal)

// Moving a research is the only action that changes who can see it, so it gets
// a dialog that says so rather than a menu item that just does it.
const { ownedTeams, load: loadTeams } = useTeams()
const transferOpen = ref(false)
const transferring = ref(false)
const transferError = ref('')

async function openTransfer() {
  transferError.value = ''
  // Awaited: opening first showed "you own only this team" and then a picker
  // with nothing selected and Move disabled, on every deep-linked page load.
  await loadTeams()
  transferOpen.value = true
}

async function transfer(teamId: string) {
  transferring.value = true
  transferError.value = ''
  try {
    const { authFetch } = useAuth()
    const cfg = useRuntimeConfig()
    await authFetch(`${cfg.public.apiBase || ''}/api/researches/${research.value.id}/transfer`, {
      method: 'POST',
      body: { team_id: teamId },
    })
    transferOpen.value = false
    const target = ownedTeams.value.find((t) => t.id === teamId)
    useToasts().success(`Moved to ${target?.name || 'the other team'}`)
    // Permissions may have changed under the reader — refetch rather than
    // leaving the page showing what they could do a moment ago.
    await refreshNuxtData()
  } catch (e: any) {
    transferError.value = e?.data?.error || 'The server refused the move'
  } finally {
    transferring.value = false
  }
}
const researchSlug = computed(() => research.value?.code || id)
const sections = computed(() => researchData.value?.data?.sections ?? [])
const activeSession = computed(() => researchData.value?.data?.active_session)

const totalEntryCount = computed(() =>
  sections.value.reduce((sum: number, s: any) => sum + (s.entries_count || 0), 0)
)

// Active section (default: first, or '__all__' for all entries)
const activeSection = ref(route.query.section as string || '')
const isAllEntries = computed(() => activeSection.value === '__all__')
const isLinksView = computed(() => activeSection.value === '__links__')
const router = useRouter()

watch(activeSection, (val) => {
  router.replace({ query: { ...route.query, section: val || undefined } })
})

watch(sections, (secs) => {
  // Also when the current one is gone: a section deleted in another tab left
  // `activeSection` holding a dead id, so the page fell through to "Select a
  // section" with no explanation and a `?section=` that repeated it on reload.
  const stillThere = secs.some((s: any) => s.id === activeSection.value)
  if ((!activeSection.value || !stillThere) && secs.length) activeSection.value = secs[0].id
}, { immediate: true })

const currentSection = computed(() =>
  isAllEntries.value ? null : sections.value.find((s: any) => s.id === activeSection.value) ?? null
)

// --- Section entries ---
const entriesUrl = computed(() =>
  !isAllEntries.value && activeSection.value
    ? `/api/researches/${id}/sections/${activeSection.value}/entries`
    : null
)
const { data: entriesData, pending: entriesPending } = useApi<{ data: any[] }>(
  computed(() => entriesUrl.value ?? '/api/researches/__none__/sections/__none__/entries')
)
const entries = computed(() =>
  !isAllEntries.value && activeSection.value ? (entriesData.value?.data ?? []) : []
)

// --- All entries ---
const { data: allEntriesData, pending: allEntriesPending } = useApi<{ data: any[] }>(
  computed(() => isAllEntries.value ? `/api/researches/${id}/entries` : '/api/researches/__none__/entries')
)
const allEntries = computed(() => isAllEntries.value ? (allEntriesData.value?.data ?? []) : [])

// Global tags from API
const { data: tagsData } = useApi<{ data: any[] }>(
  computed(() => isAllEntries.value ? `/api/researches/${id}/tags` : '/api/researches/__none__/tags')
)
const globalTags = computed(() => isAllEntries.value ? (tagsData.value?.data ?? []) : [])

// Tasks (count only, for header button)
const { data: tasksData } = await useApi<{ data: any[] }>(`/api/researches/${id}/tasks`)
const tasks = computed(() => tasksData.value?.data ?? [])

// Marks: the count alone. The queue read resolves anchors against every
// document it touches, so a header badge must not ask for the list to print one
// number — `counts` comes off the envelope, and the request takes one row.
const { data: marksData } = await useApi<{ data: any[]; meta?: { counts?: Record<string, number> } }>(
  `/api/researches/${id}/annotations?status=open`,
)
const openMarks = computed(() => marksData.value?.meta?.counts?.open ?? 0)

const { data: roadmapsData } = await useApi<{ data: any[] }>(`/api/researches/${id}/roadmaps`)
const roadmaps = computed(() => roadmapsData.value?.data ?? [])

// Personal update queue. Besides the header count, this supplies the New /
// Changed markers to cards and search results on this page.
const {
  updates: entryUpdates,
  byEntry: updatesByEntry,
  error: entryUpdatesFetchError,
  refresh: reloadUpdates,
} = await useEntryUpdates(id)

// All sessions
const { data: sessionsData } = await useApi<{ data: any[] }>(`/api/researches/${id}/sessions`)
const allSessions = computed(() => sessionsData.value?.data ?? [])
const activeSessions = computed(() => allSessions.value.filter((s: any) => s.status === 'active'))

// External links
const { data: researchLinksData, pending: researchLinksLoading } = useApi<{ data: any[]; total: number }>(
  computed(() => isLinksView.value ? `/api/researches/${id}/links` : '/api/researches/__none__/links')
)
const researchLinksGrouped = computed(() => researchLinksData.value?.data ?? [])
const researchLinksTotal = computed(() => researchLinksData.value?.total ?? 0)

// Auth & API
const { authFetch } = useAuth()
const rtBase = useRuntimeConfig().public.apiBase || ''

// --- Share links ---
//
// The management surface lives with the action that creates it rather than in
// the details panel: that panel is the research's content and is open to
// viewers, and shares are an access-control surface.
const sharesOpen = ref(false)
const shares = ref<any[]>([])
const sharesLoading = ref(false)
const creatingShare = ref(false)
const shareError = ref('')
const issuedShareUrl = ref('')
const revokingShareId = ref('')
const shareToRevoke = ref<any | null>(null)
// The URL of a link this tab issued, kept for the life of the tab and never
// persisted. The server holds only a hash, so after a reload it is gone — which
// is what "shown once" means.
const recoverableShareLinks = ref<Record<string, string>>({})

const activeShareCount = computed(() => {
  const fromList = shares.value.filter(isShareLive).length
  // Before the dialog has ever been opened the list is empty and the count on
  // the research payload is the only source.
  return shares.value.length ? fromList : (researchData.value?.data?.active_share_count ?? 0)
})

async function loadShares() {
  sharesLoading.value = true
  try {
    const res = await authFetch<{ data: any[] }>(`${rtBase}/api/researches/${id}/shares`)
    shares.value = res.data ?? []
  } catch {
    shares.value = []
  } finally {
    sharesLoading.value = false
  }
}

async function openShares() {
  shareError.value = ''
  issuedShareUrl.value = ''
  sharesOpen.value = true
  await loadShares()
}

async function createShare(payload: any) {
  creatingShare.value = true
  shareError.value = ''
  try {
    const res = await authFetch<{ data: { share: any; url: string } }>(
      `${rtBase}/api/researches/${id}/shares`,
      { method: 'POST', body: payload },
    )
    recoverableShareLinks.value = { ...recoverableShareLinks.value, [res.data.share.id]: res.data.url }
    issuedShareUrl.value = res.data.url
    await loadShares()
  } catch (e: any) {
    shareError.value = e?.data?.error || 'Couldn\'t create the link. Try again.'
  } finally {
    creatingShare.value = false
  }
}

function askRevokeShare(share: any) {
  shareToRevoke.value = share
}

/**
 * Revoking is not optimistic.
 *
 * A row that flips to "Revoked" before the server agrees tells an owner that
 * access is closed when it may not be, which is the one lie this dialog must
 * never tell.
 */
async function confirmRevokeShare() {
  const share = shareToRevoke.value
  if (!share) return
  revokingShareId.value = share.id
  try {
    await authFetch(`${rtBase}/api/shares/${share.id}`, { method: 'DELETE' })
    shareToRevoke.value = null
    await loadShares()
  } catch {
    // The modal closes on failure too. Leaving it up with the spinner gone is
    // indistinguishable from "nothing happened", and the toast is what carries
    // the news.
    shareToRevoke.value = null
    useToasts().error('Couldn\'t revoke that link. It is still active.')
  } finally {
    revokingShareId.value = ''
  }
}

// --- Editing a link in place ---
//
// The address survives, which is the point: a link pasted into a report or a
// website keeps working while what it shows changes underneath it.
const savingShareId = ref('')
const shareSaveError = ref('')
const shareSavedTick = ref(0)
const pendingShareUpdate = ref<{ share: any; payload: any; added: string[] } | null>(null)

const SHARE_INCLUDE_WORDS: Record<string, string> = {
  roadmaps: 'roadmaps',
  sessions: 'sessions',
  tasks: 'tasks',
  export: 'downloading the project as a file',
}

const wideningMessage = computed(() => {
  const p = pendingShareUpdate.value
  if (!p) return ''
  const what = p.added.join(' and ')
  const n = p.share.view_count ?? 0
  const opened = n
    ? `It has been opened ${n === 1 ? 'once' : n + ' times'}.`
    : "It hasn't been opened yet, but the link is already out."
  return `Adding ${what} means anyone already holding this link can see them, including people you shared it with weeks ago. ${opened} You can narrow it again at any time, but you cannot un-show what somebody has already read.`
})

function updateShare(payload: { id: string; label: string; include: ShareInclude }) {
  const current = shares.value.find((s: any) => s.id === payload.id)
  if (!current) return
  shareSaveError.value = ''
  const wanted = payload.include as unknown as Record<string, boolean>
  const added = Object.keys(SHARE_INCLUDE_WORDS).filter(k => wanted[k] && !current.include?.[k])
  // Removing exposure needs no ceremony. Adding it, on a link that is live and
  // out in the world, does.
  if (added.length && isShareLive(current)) {
    pendingShareUpdate.value = { share: current, payload, added: added.map(k => SHARE_INCLUDE_WORDS[k] ?? k) }
    return
  }
  void performShareUpdate(payload)
}

function confirmShareUpdate() {
  const p = pendingShareUpdate.value
  if (p) void performShareUpdate(p.payload)
}

async function performShareUpdate(payload: { id: string; label: string; include: ShareInclude }) {
  savingShareId.value = payload.id
  shareSaveError.value = ''
  try {
    const res = await authFetch<{ data: { share: any } }>(`${rtBase}/api/shares/${payload.id}`, {
      method: 'PUT',
      body: { label: payload.label, include: payload.include },
    })
    // The row repaints from the server's answer and from nothing else.
    shares.value = shares.value.map((s: any) => (s.id === res.data.share.id ? res.data.share : s))
    shareSavedTick.value++
    useToasts().success('Link updated.')
  } catch (e: any) {
    const status = e?.response?.status ?? e?.statusCode
    // 409 is the owner-surface answer for "revoked or expired"; 404 is a row
    // that is gone altogether. Both mean the edit has nothing to land on.
    shareSaveError.value = status === 409 || status === 404
      ? 'dead'
      : "Couldn't save the change. The link is unchanged — try again."
  } finally {
    savingShareId.value = ''
    pendingShareUpdate.value = null
  }
}

async function refreshSharesAfterFailure() {
  shareSaveError.value = ''
  await loadShares()
}

function closeShares() {
  sharesOpen.value = false
  shareSaveError.value = ''
}

// Download portable JSON
const exporting = ref(false)
async function downloadPortableJSON() {
  exporting.value = true
  try {
    const data = await authFetch<any>(`${rtBase}/api/researches/${id}/export/portable`)
    const blob = new Blob([JSON.stringify(data, null, 2)], { type: 'application/json' })
    const url = URL.createObjectURL(blob)
    const a = document.createElement('a')
    a.href = url
    a.download = `${research.value?.name || 'research'}.json`
    a.click()
    URL.revokeObjectURL(url)
  } catch (e: any) {
    useToasts().push({ variant: 'error', title: 'Export failed', message: e?.message || String(e), timeout: 0 })
  } finally {
    exporting.value = false
  }
}

// Archive toggle
async function toggleArchive() {
  const newStatus = research.value.status === 'archived' ? 'active' : 'archived'
  await authFetch(`${rtBase}/api/researches/${id}`, {
    method: 'PUT',
    body: { status: newStatus },
  })
  researchData.value = await authFetch<any>(`${rtBase}/api/researches/${id}`)
}

// Real-time updates
// Each of these is one screen's worth of data; the handler below picks the ones
// an event can have invalidated, and the resync takes the lot. The resync has to
// be the superset — it is the one path that exists to repair anything a dropped
// connection missed, so a view it skips is a view that stays wrong forever.
async function reloadResearch() {
  researchData.value = await authFetch<any>(`${rtBase}/api/researches/${id}`)
}
async function reloadEntries() {
  if (entriesUrl.value) entriesData.value = await authFetch<any>(`${rtBase}${entriesUrl.value}`)
  if (isAllEntries.value) {
    allEntriesData.value = await authFetch<any>(`${rtBase}/api/researches/${id}/entries`)
    tagsData.value = await authFetch<any>(`${rtBase}/api/researches/${id}/tags`)
  }
}
// An imported document is a write this tab made, so the WebSocket event comes
// back stamped with this client id and is deliberately ignored — which is right
// for a change already on screen and wrong for this one, because the list and
// the sidebar count are not.
async function onImported() {
  await Promise.all([reloadEntries(), reloadResearch()])
}

async function reloadLinks() {
  if (isLinksView.value) researchLinksData.value = await authFetch<any>(`${rtBase}/api/researches/${id}/links`)
}
async function reloadSessions() {
  sessionsData.value = await authFetch<any>(`${rtBase}/api/researches/${id}/sessions`)
}
async function reloadRoadmaps() {
  roadmapsData.value = await authFetch<any>(`${rtBase}/api/researches/${id}/roadmaps`)
}
async function reloadTasks() {
  tasksData.value = await authFetch<any>(`${rtBase}/api/researches/${id}/tasks`)
}
async function reloadEverything() {
  await Promise.all([
    reloadResearch(), reloadEntries(), reloadLinks(),
    reloadSessions(), reloadRoadmaps(), reloadTasks(), reloadUpdates(),
    // Events sent while the socket was down are gone, so the summary is
    // refetched rather than waiting for the next write to notice.
    resume.refresh(),
  ])
}

// The summary is derived from tasks, questions, marks and entries, so almost
// any write in this research changes it. `entry_view` is excluded because it is
// the reader's own checkpoint and the summary deliberately knows nothing about
// it; `share` because a link being issued changes no work.
const resume = useResearchResume(id, researchSlug.value)
onMounted(() => {
  // The remembered session is read first, so the first request already asks
  // about the thread this reader was on.
  resume.restoreSession()
  resume.load(true)
})

useResearchRealtime(() => id, async (event) => {
  // Not on a server without the route: one 404 per write burst, for the life
  // of the page, buys nothing.
  if (!resume.unavailable.value && !['entry_view', 'share'].includes(event.entity)) resume.scheduleRefresh()
  if (['research', 'section', 'session'].includes(event.entity)) await reloadResearch()
  if (event.entity === 'entry') {
    // The sidebar's totals and per-section counts come off the research
    // payload, not off the entry list — so refetching only the list left the
    // count beside it disagreeing, and every later write widened the gap.
    await Promise.all([reloadEntries(), reloadLinks(), reloadResearch(), reloadUpdates()])
  }
  if (event.entity === 'entry_view') await reloadUpdates()
  if (['question', 'session'].includes(event.entity)) await reloadSessions()
  if (event.entity === 'roadmap') await reloadRoadmaps()
  // The badge is rendered from this list and nothing else refetched it, so it
  // sat frozen at its page-load value for the life of the page.
  if (event.entity === 'task') await reloadTasks()
  // The link tables were rewritten wholesale; every view built on them is stale.
  if (event.entity === 'crossref') await reloadLinks()
  // A share created or revoked in another tab, or by a colleague. The badge is
  // a security signal — "this research is exposed to N links" — and a security
  // signal that quietly stops being true is worse than none. The open dialog
  // reads from the same list.
  if (event.entity === 'share') {
    await reloadResearch()
    if (sharesOpen.value) await loadShares()
  }
}, { researchId: () => research.value?.id, onResync: reloadEverything })
</script>

<style scoped>

.updates-load-error { margin-bottom: var(--space-4); }

/* Responsive */
@media (max-width: 768px) {
  .title-with-code { flex-wrap: wrap; }
}
</style>
