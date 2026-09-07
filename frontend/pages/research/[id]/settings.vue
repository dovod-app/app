<template>
  <div v-if="pending" class="skeleton-page">
    <div class="skeleton-card skeleton-header"></div>
    <div class="skeleton-card skeleton-content"></div>
  </div>

  <div v-else-if="research" class="settings-page">
    <PageHeader
      :crumbs="[
        { label: 'Projects', to: { name: 'index' } },
        { label: research.name, to: `/research/${researchSlug}` },
        { label: 'Settings' },
      ]"
      :code="research.code"
      title="Settings"
    >
      <template #actions>
        <TeamViewerNotice v-if="isViewer" :team-name="research?.team_name" />
      </template>
    </PageHeader>

    <TabBar v-model="activeTab" :tabs="tabs" label="Project settings" />

    <!-- Overview -->
    <div v-if="activeTab === 'overview'" id="panel-overview" role="tabpanel" aria-labelledby="tab-overview" tabindex="0" class="panel">
      <div class="card">
        <h3 class="card-section-title">Ownership</h3>
        <p class="lead">
          <template v-if="research.team_is_personal">
            In your personal space. Nobody else has a role on it.
          </template>
          <template v-else>
            Owned by <NuxtLink :to="`/teams/${research.team_id}`" class="team-link">{{ research.team_name }}</NuxtLink>,
            where your role is <strong>{{ research.role }}</strong>.
          </template>
          Share links are managed from the project page.
        </p>
      </div>

      <div class="card card--list">
        <div class="field-group">
          <EditableField
            label="Goal"
            :value="research.goal"
            :editable="canWrite"
            placeholder="What do you want to find out or decide?"
            :empty-text="canWrite ? 'Click the pencil to set a goal' : 'Not set'"
            @save="v => save('goal', v)"
          />
          <EditableField
            label="Description"
            :value="research.description"
            :editable="canWrite"
            multiline
            placeholder="What is this project about?"
            :empty-text="canWrite ? 'Click the pencil to add a description' : 'Not set'"
            @save="v => save('description', v)"
          />
          <EditableField
            label="Tags"
            :value="(research.tags ?? []).join(', ')"
            :editable="canWrite"
            placeholder="tag1, tag2, tag3"
            empty-text="No tags yet"
            @save="v => save('tags', v.split(',').map((t: string) => t.trim()).filter(Boolean))"
          >
            <template #default>
              <TagList v-if="research.tags?.length" :tags="research.tags" />
              <span v-else class="field-empty">No tags yet</span>
            </template>
          </EditableField>
        </div>
      </div>

      <!-- Removed for a viewer rather than disabled: the house rule, and the
           route behind it is a write. There is no share twin of this page, so
           "never on a share" is true by construction rather than by a guard. -->
      <ResearchSettingsMaintenanceCard v-if="canWrite" :research-id="id" />
    </div>

    <!-- Skills -->
    <div v-else-if="activeTab === 'skills'" id="panel-skills" role="tabpanel" aria-labelledby="tab-skills" tabindex="0" class="panel">
      <p class="lead">
        Skills guide how your AI assistant works on this project. Project skills take priority
        over team skills, which take priority over built-in skills.
      </p>

      <div v-if="skillsPending" class="skeleton-card" style="height: 200px"></div>

      <template v-else>
        <div class="card card--list">
          <ResearchSettingsSkillRowList
            :skills="chosen"
            :can-write="canWrite"
            :busy-slug="busySlug"
            heading="Project skills"
            :note="`${chosenCount} of ${cap}`"
            blurb="Choose up to six skills to guide this project."
            empty-text="No project skills selected. Your AI assistant uses the built-in skills below."
            @open="openSkill"
            @detach="detach"
          />
        </div>

        <div class="card card--list">
          <ResearchSettingsSkillRowList
            :skills="ambient"
            :can-write="canWrite"
            heading="Always on"
            blurb="Built-in guidance for managing projects and writing documents in Dovod. These skills do not count toward the selection limit."
            empty-text="Built-in skills are unavailable. Contact the server administrator."
            @open="openSkill"
          />
        </div>

        <div v-if="canWrite" class="card card--list">
          <ResearchSettingsSkillRowList
            :skills="library"
            :actions="false"
            :can-write="canWrite"
            :busy-slug="busySlug"
            heading="Available to attach"
            blurb="Built-in methodology and anything your team has written. Attaching one spends a slot in the budget above."
            empty-text="Nothing left to attach — everything available is already on."
            @open="openSkill"
            @attach="attach"
          />
        </div>
      </template>
    </div>

    <!-- Memory -->
    <div v-else-if="activeTab === 'memory'" id="panel-memory" role="tabpanel" aria-labelledby="tab-memory" tabindex="0" class="panel">
      <p v-if="memoryRefreshError" role="alert">{{ memoryRefreshError }}</p>
      <ResearchSettingsMemoryList
        :items="research.memory ?? []"
        :research-id="researchSlug"
        :can-write="canWrite"
        :on-add="addMemory"
        :on-update="updateMemory"
        :on-delete="deleteMemory"
        :on-reload="reloadMemory"
      />
    </div>

    <!-- Sections -->
    <div v-else-if="activeTab === 'sections'" id="panel-sections" role="tabpanel" aria-labelledby="tab-sections" tabindex="0" class="panel">
      <p class="lead">
        A section can declare what its documents record. The vocabulary is closed: an agent
        may write the keys named here and nothing else, and a section that declares nothing
        accepts no metadata at all.
      </p>
      <ResearchSettingsFieldSpecList
        :sections="sections"
        :editable="canWrite"
        :caps="fieldCaps"
        :types="fieldTypes"
        :reserved-keys="reservedKeys"
        :on-save="saveFieldSpec"
      />
    </div>

    <ResearchSettingsSkillDetail
      :visible="!!openSlug"
      :skill="openedSkill"
      :loading="skillLoading"
      :error="skillError"
      @close="closeSkill"
    />
  </div>

  <EmptyState
    v-else
    icon="&#x1F50D;"
    title="Project not found"
    description="Check the link and make sure you have access to this project."
  >
    <NuxtLink :to="{ name: 'index' }" class="btn btn-primary">Back to projects</NuxtLink>
  </EmptyState>
</template>

<script setup lang="ts">
const route = useRoute()
const router = useRouter()
const id = route.params.id as string

const { data: researchData, pending } = await useApi<any>(`/api/researches/${id}`)
const research = computed(() => researchData.value?.data?.research)
const researchSlug = computed(() => research.value?.code || id)

const { canWrite, isViewer, setFromResearch } = useResearchRole()
watch(research, r => setFromResearch(r), { immediate: true })

/* The tab lives in the query string so a link can point at one, and it is
   replaced rather than pushed: Back should leave the page, not walk the tabs. */
const TABS = ['overview', 'skills', 'sections', 'memory'] as const
type Tab = (typeof TABS)[number]
const activeTab = computed<Tab>({
  get: () => (TABS.includes(route.query.tab as Tab) ? (route.query.tab as Tab) : 'overview'),
  set: (tab) => router.replace({ query: { ...route.query, tab: tab === 'overview' ? undefined : tab } }),
})

const { authFetch } = useAuth()
const base = useRuntimeConfig().public.apiBase || ''

// --- Memory ---
const memoryRefreshError = ref('')
async function reloadMemory() {
  const response = await authFetch<any>(`${base}/api/researches/${id}/memory`)
  // Nuxt's fetched data is shallow: replace the root to publish the new list.
  if (research.value) researchData.value = {
    ...researchData.value,
    data: { ...researchData.value.data, research: { ...research.value, memory: response.data } },
  }
  memoryRefreshError.value = ''
}
async function memoryWrite(path: string, method: 'POST' | 'PATCH', body: unknown) {
  await authFetch(`${base}/api/researches/${id}/memory${path}`, { method, body })
  // The mutation succeeded. A failed follow-up read must not invite a retry
  // of the already committed append (which would create a duplicate note).
  try { await reloadMemory() }
  catch { memoryRefreshError.value = 'Saved, but the list could not be refreshed. Click Refresh before making further changes.' }
}
const addMemory = (text: string) => memoryWrite('', 'POST', { text })
const updateMemory = (itemId: string, text: string, version: number) => memoryWrite(`/${itemId}`, 'PATCH', { text, version })
const deleteMemory = (ids: string[]) => memoryWrite('/bulk-delete', 'POST', { ids })

// --- Skills ---
const skillsData = ref<any>(null)
const libraryData = ref<any>(null)
const skillsPending = ref(true)

async function loadSkills() {
  skillsPending.value = true
  try {
    skillsData.value = await authFetch<any>(`${base}/api/researches/${id}/skills`)
    if (canWrite.value) {
      libraryData.value = await authFetch<any>(`${base}/api/researches/${id}/skills/library`)
    }
  } catch {
    skillsData.value = { data: [], cap: 6, chosen: 0 }
  } finally {
    skillsPending.value = false
  }
}
onMounted(loadSkills)

const allSkills = computed<any[]>(() => skillsData.value?.data ?? [])
const chosen = computed(() => allSkills.value.filter(s => !s.ambient))
const ambient = computed(() => allSkills.value.filter(s => s.ambient))
const chosenCount = computed(() => skillsData.value?.chosen ?? chosen.value.length)
const cap = computed(() => skillsData.value?.cap ?? 6)
const library = computed<any[]>(() => (libraryData.value?.data ?? []).filter((s: any) => !s.attached))

const tabs = computed(() => [
  { id: 'overview', label: 'Overview' },
  {
    id: 'skills',
    label: 'Skills',
    count: `${chosenCount.value}/${cap.value}`,
    srCount: `${chosenCount.value} of ${cap.value} chosen`,
  },
  {
    id: 'sections',
    label: 'Sections',
    count: declaredSections.value,
    srCount: `${declaredSections.value} sections declare fields`,
  },
  {
    id: 'memory',
    label: 'Memory',
    count: research.value?.memory?.length ?? 0,
    srCount: `${research.value?.memory?.length ?? 0} notes`,
  },
])

// --- Section field specs ---
const sections = computed<any[]>(() => researchData.value?.data?.sections ?? [])
const declaredSections = computed(() => sections.value.filter(s => (s.field_spec?.length ?? 0) > 0).length)

// The rules come from the server rather than a copy in here. A cap the client
// believes and the server enforces will disagree exactly once, at the worst
// moment, and a reserved-key list hard-coded here drifts the day a twelfth key
// joins the export.
const schema = ref<any>(null)
const fieldCaps = computed(() => schema.value?.caps ?? { fields: 12, required: 5, options: 20 })
// A fallback that keeps the editor usable, not an empty list: without it a
// failed schema fetch rendered the type <select> with zero options, so the one
// control the editor cannot do without stopped working — while the comment on
// the catch claimed the caps were the only thing lost.
const FALLBACK_TYPES = [
  { type: 'enum' }, { type: 'ref' }, { type: 'date' },
  { type: 'text' }, { type: 'number' }, { type: 'url' },
]
const fieldTypes = computed<any[]>(() => {
  const served = schema.value?.types ?? []
  return served.length ? served : FALLBACK_TYPES
})
const reservedKeys = computed<string[]>(() => schema.value?.reserved_keys ?? [])

onMounted(async () => {
  try {
    const res = await authFetch<any>(`${base}/api/metadata/schema`)
    schema.value = res?.data ?? null
  } catch {
    // The editor still works on the fallbacks above; it just cannot warn about
    // a reserved key before the server does.
  }
})

async function saveFieldSpec(sectionId: string, spec: any[]) {
  await authFetch(`${base}/api/sections/${sectionId}`, {
    method: 'PUT',
    body: { field_spec: spec },
  })
  researchData.value = await authFetch<any>(`${base}/api/researches/${id}`)
}

const busySlug = ref<string | null>(null)
const toasts = useToasts()

/* Attach and detach wait for the server rather than moving the row first. The
   cap is a server rule, and a seventh row that appears and then vanishes is a
   worse answer than half a second of nothing. */
async function attach(sk: any) {
  busySlug.value = sk.slug
  try {
    await authFetch(`${base}/api/researches/${id}/skills`, { method: 'POST', body: { slug: sk.slug } })
    await loadSkills()
  } catch (e: any) {
    const code = e?.data?.code
    toasts.push({
      variant: 'error',
      title: code === 'skill_cap_reached' ? 'No room' : "Couldn't attach",
      message: code === 'skill_cap_reached'
        ? `${cap.value} skills are already chosen. Detach one to make room.`
        : e?.data?.error || 'The server refused that.',
    })
  } finally {
    busySlug.value = null
  }
}

async function detach(sk: any) {
  busySlug.value = sk.slug
  try {
    await authFetch(`${base}/api/researches/${id}/skills/${sk.slug}`, { method: 'DELETE' })
    await loadSkills()
  } catch (e: any) {
    toasts.push({ variant: 'error', title: "Couldn't detach", message: e?.data?.error || 'The server refused that.' })
  } finally {
    busySlug.value = null
  }
}

// --- Reading one skill ---
const openSlug = ref<string | null>(null)
const openedSkill = ref<any>(null)
const skillLoading = ref(false)
const skillError = ref(false)

async function openSkill(slug: string) {
  openSlug.value = slug
  openedSkill.value = allSkills.value.find(s => s.slug === slug)
    ?? library.value.find((s: any) => s.slug === slug)
    ?? null
  skillLoading.value = true
  skillError.value = false
  try {
    const res = await authFetch<any>(`${base}/api/researches/${id}/skills/${slug}`)
    openedSkill.value = res.data
  } catch {
    skillError.value = true
  } finally {
    skillLoading.value = false
  }
}

function closeSkill() {
  openSlug.value = null
  openedSkill.value = null
  skillError.value = false
}

// --- Overview writes ---
async function save(field: string, value: any) {
  try {
    await authFetch(`${base}/api/researches/${id}`, { method: 'PUT', body: { [field]: value } })
    researchData.value = await authFetch<any>(`${base}/api/researches/${id}`)
  } catch (e: any) {
    toasts.push({
      variant: 'error',
      title: 'Not saved',
      message: e?.data?.error || 'The server refused that change.',
    })
  }
}
</script>

<style scoped>
.panel { margin-top: var(--space-6); }
.card + .card { margin-top: var(--space-6); }

.lead {
  font-size: var(--type-sm);
  color: var(--color-text-muted);
  max-width: var(--measure-prose);
  margin-bottom: var(--space-5);
}
/* The last thing in a card does not need a gap under it — the card's own
   padding is that gap, and the two were adding up. */
.card > .lead:last-child { margin-bottom: 0; }
.footnote { margin-top: var(--space-5); margin-bottom: 0; font-size: var(--type-xs); }


/* A stack of labelled fields, framed the way a list of rows is: the divider
   belongs between two fields, never around the stack. The class carried no
   rule at all before — it was in the markup and nowhere in any stylesheet — so
   the fields ran together and the card's padding stacked with the field's own,
   putting the text 40px from an edge with nothing marking where one field
   ended and the next began.
   Scoped rather than global: TaskDetailModal and RoadmapNodePopover already own
   this name scoped, and a global one would shadow-clash with both — the exact
   thing css-consistency counts. */
.field-group { display: flex; flex-direction: column; }
.field-group > :deep(* + *) { border-top: 1px solid var(--color-border); }
.team-link { color: var(--color-primary); }

</style>
