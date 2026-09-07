<template>
  <div>
    <PageHeader title="Projects">
      <template #actions>
        <NuxtLink to="/templates" class="btn btn-sm">
          <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M4 19.5A2.5 2.5 0 0 1 6.5 17H20"/><path d="M6.5 2H20v20H6.5A2.5 2.5 0 0 1 4 19.5v-15A2.5 2.5 0 0 1 6.5 2z"/></svg>
          Methodologies
        </NuxtLink>
        <!--
          Hidden when the server will refuse the write. useResearchRole is
          scoped to one research and there is none here, so this page had no
          write gate at all — invisible while auth-off meant "everything is
          permitted", and an invitation to a permanent error toast once it
          stopped meaning that.
        -->
        <button v-if="canWriteHere" class="btn btn-sm" @click="triggerImport" :disabled="importing">
            <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z"/><polyline points="14 2 14 8 20 8"/><line x1="12" y1="12" x2="12" y2="18"/><polyline points="9 15 12 12 15 15"/></svg>
            {{ importing ? 'Importing...' : 'Import JSON' }}
          </button>
          <input ref="fileInput" type="file" accept=".json" style="display:none" @change="handleImportFile" />
      </template>
    </PageHeader>

    <!-- Filters -->
    <div class="filter-bar">
      <select v-model="statusFilter" class="select">
        <option value="">All statuses</option>
        <option value="active">Active</option>
        <option value="completed">Completed</option>
        <option value="archived">Archived</option>
      </select>
      <!-- Only when there is a choice to make. A solo user never sees this. -->
      <select v-if="teams.length > 1" v-model="teamFilter" :disabled="teamsLoading" class="select" aria-label="Filter by team">
        <option value="">All teams</option>
        <option v-for="team in teams" :key="team.id" :value="team.id">{{ team.name }}</option>
      </select>
      <span v-if="tagFilter" class="active-tag-filter">
        Tag: <strong>{{ tagFilter }}</strong>
        <button class="tag-clear" @click="tagFilter = ''">&times;</button>
      </span>
    </div>

    <!-- Onboarding, for the person this product was set up by. Someone who was
         invited into a team has an agent's worth of advice they did not ask
         for: "install an MCP server" is not why they followed a link. -->
    <GettingStartedBanner v-if="!teamFilter && !invitedElsewhere" :has-researches="researches.length > 0" />

    <!-- Loading -->
    <div v-if="pending" class="skeleton-list">
      <div v-for="i in 4" :key="i" class="skeleton-card"></div>
    </div>

    <!-- List -->
    <div v-else-if="filtered.length" class="grid grid-2">
      <ResearchCard
        v-for="r in filtered"
        :key="r.id"
        :research="r"
        @tag-click="tagFilter = $event"
        @status-changed="refreshList"
      />
    </div>

    <!-- Empty because of the team filter, not because there is nothing. What to
         do about it depends entirely on whether the reader can move work into
         the team: an owner has a control, a viewer has a colleague. -->
    <EmptyState
      v-else-if="teamFilter"
      icon="&#x1F4C1;"
      :title="`No projects in ${filteredTeamName} yet`"
      :description="
        filteredTeamOwned
          ? 'Move a project here to give this team access. Your other projects stay in your personal team.'
          : 'Ask a team owner to move a project here so everyone in the team can access it.'
      "
    >
      <NuxtLink v-if="filteredTeamOwned" class="btn" :to="`/teams/${teamFilter}`">Move projects here</NuxtLink>
      <NuxtLink v-else class="btn" :to="`/teams/${teamFilter}`">View team</NuxtLink>
      <button class="btn" @click="teamFilter = ''">Show all teams</button>
    </EmptyState>

    <!-- Empty, and the reader was invited here by somebody. Telling them to
         connect an agent answers a question they did not ask, and reads as the
         invitation having failed. -->
    <EmptyState
      v-else-if="invitedElsewhere"
      icon="&#x1F91D;"
      title="Nothing has been shared with you yet"
      description="Your team has no projects yet. Ask the person who invited you to share a project with the team."
    >
      <NuxtLink class="btn" to="/teams">Your teams</NuxtLink>
    </EmptyState>

    <!-- Empty -->
    <EmptyState
      v-else
      icon="&#x1F52C;"
      title="Start your first project"
      description="Ask your connected AI assistant to help you define a goal and get started:"
      command="Use the research/initialize prompt to start a new project in Dovod."
    />
  </div>
</template>

<script setup lang="ts">
const { authFetch } = useAuth()
const config = useRuntimeConfig()
const base = config.public.apiBase || ''

const route = useRoute()
const router = useRouter()
const { teams, loading: teamsLoading, load: loadTeams } = useTeams()

const statusFilter = ref('active')
const tagFilter = ref('')

// The team filter lives in the URL rather than in global state, so a scoped
// list is a link someone can send — which is the only real advantage a
// workspace switcher would have had.
const teamFilter = computed({
  get: () => (typeof route.query.team === 'string' ? route.query.team : ''),
  set: (value: string) => {
    const query = { ...route.query }
    if (value) query.team = value
    else delete query.team
    router.replace({ query })
  },
})

const filteredTeam = computed(() => teams.value.find((t) => t.id === teamFilter.value) ?? null)
const filteredTeamName = computed(() => filteredTeam.value?.name || 'this team')
const filteredTeamOwned = computed(() => filteredTeam.value?.role === 'owner')

/**
 * True when somebody else brought this reader here.
 *
 * A team they are in but do not own is an invitation that was accepted, and it
 * changes what an empty screen means: not "you have not started yet" but "the
 * work has not arrived yet". The founder's onboarding is wrong in that case,
 * and reads as the invitation having failed.
 */
const invitedElsewhere = computed(() => teams.value.some((t) => !t.personal && t.role !== 'owner'))

onMounted(() => loadTeams())
const importing = ref(false)
const fileInput = ref<HTMLInputElement | null>(null)

function triggerImport() {
  fileInput.value?.click()
}

async function handleImportFile(event: Event) {
  const input = event.target as HTMLInputElement
  const file = input.files?.[0]
  if (!file) return

  importing.value = true
  try {
    const text = await file.text()
    const data = JSON.parse(text)
    const result = await authFetch<{ research_id: string; code: string; name: string }>(
      `${base}/api/researches/import`,
      { method: 'POST', body: data }
    )
    await navigateTo(`/research/${result.code}`)
  } catch (e: any) {
    useToasts().push({ variant: 'error', title: 'Import failed', message: e?.message || String(e), timeout: 0 })
  } finally {
    importing.value = false
    input.value = '' // reset file input
  }
}

const apiUrl = computed(() => {
  const params = new URLSearchParams()
  if (statusFilter.value) params.set('status', statusFilter.value)
  if (teamFilter.value) params.set('team', teamFilter.value)
  const query = params.toString()
  return query ? `/api/researches?${query}` : '/api/researches'
})

// Whether this browser may write anything at all, with no research in hand:
// signed in when there are accounts, and otherwise whatever /api/health says
// about this caller.
const { authEnabled, isAuthenticated } = useAuth()
const { writeApi } = useServerInfo()

const canWriteHere = computed(() => (authEnabled.value ? isAuthenticated.value : writeApi.value))

const { data, pending, refresh } = useApi<{ data: any[] }>(apiUrl.value)

async function refreshList() {
  const res = await authFetch<{ data: any[] }>(`${base}${apiUrl.value}`)
  data.value = res
}

watch([statusFilter, teamFilter], refreshList)

const researches = computed(() => data.value?.data ?? [])

const filtered = computed(() =>
  tagFilter.value
    ? researches.value.filter((r: any) => r.tags?.includes(tagFilter.value))
    : researches.value
)

// Real-time updates
async function reloadResearches() {
  data.value = await authFetch<{ data: any[] }>(`${base}${apiUrl.value}`)
}

useRealtimeUpdates(
  (event) => {
    // A transfer can take a research out of this list as easily as a create
    // puts one in, and access.revoked says a card here is no longer real.
    if (event.entity === 'research' || event.type === 'access.revoked') reloadResearches()
  },
  { onResync: reloadResearches },
)
</script>

<style scoped>
/* Moved out of the global stylesheet: these style this component and
   nothing else, and they were a directory away from the markup they
   describe. What stays global is what three unrelated components share. */
.filter-bar {
  display: flex;
  gap: var(--space-2);
  margin-bottom: var(--space-6);
}
  @media (max-width: 768px) {
  .filter-bar { flex-wrap: wrap; }
}
.skeleton-list {
  /* This page's skeletons stand in for cards in a grid, not for rows. */
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(400px, 1fr));
  gap: var(--space-6);
}
.skeleton-card {
  height: 120px;
  border-radius: var(--radius);
}
.active-tag-filter {
  display: inline-flex;
  align-items: center;
  gap: var(--space-2);
  padding: var(--space-1) var(--space-3);
  background: var(--color-primary-muted);
  border: 1px solid rgba(var(--color-primary-rgb), 0.15);
  border-radius: var(--radius-sm);
  font-size: var(--type-xs);
  color: var(--color-primary);
}
.tag-clear {
  background: none;
  border: none;
  color: var(--color-primary);
  cursor: pointer;
  font-size: var(--type-sm);
  padding: 0;
  line-height: 1;
  opacity: 0.7;
  transition: opacity var(--transition-fast);
}
.tag-clear:hover { opacity: 1; }

/* Responsive */
@media (max-width: 768px) {
  .skeleton-list { grid-template-columns: 1fr; }
}
</style>
