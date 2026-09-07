<template>
  <div v-if="pending" class="skeleton-page">
    <div class="skeleton-card skeleton-header"></div>
    <div class="kanban-skeleton">
      <div v-for="i in 4" :key="i" class="skeleton-card skeleton-column"></div>
    </div>
  </div>

  <div v-else-if="research" class="tasks-page">
    <!-- Header -->
    <PageHeader
      :crumbs="[
        { label: 'Projects', to: { name: 'index' } },
        { label: research.name, to: `/research/${researchSlug}` },
        { label: 'Tasks' },
      ]"
      :code="research.code"
      title="Tasks"
      :count="tasks.length"
    >
      <template #actions>
        <TeamViewerNotice v-if="readOnlyReason" :reason="readOnlyReason" :team-name="research?.team_name" />
            <button v-if="canWrite" class="btn btn-sm btn-primary" @click="showCreateModal = true">
              <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><line x1="12" y1="5" x2="12" y2="19"/><line x1="5" y1="12" x2="19" y2="12"/></svg>
              New task
            </button>
      </template>
    </PageHeader>

    <!-- Kanban Board -->
    <TasksKanbanBoard
      :columns="columns"
      :tasks="tasks"
      :research-slug="researchSlug"
      @task-click="openTaskDetail"
      @task-drop="onTaskDrop"
    />

    <!-- Task Detail Modal -->
    <TasksTaskDetailModal
      :task="detailTask"
      :research-slug="researchSlug"
      @close="detailTask = null"
      @save="saveField"
      @update-priority="updatePriority"
      @update-status="onDetailStatusChange"
    />

    <!-- Status Change Modal -->
    <TasksStatusChangeModal
      :visible="statusModal.visible"
      :task="statusModal.task"
      :target-status="statusModal.targetStatus"
      :status-label="statusLabel(statusModal.targetStatus)"
      @confirm="confirmStatusChange"
      @cancel="cancelStatusChange"
    />

    <!-- Create Task Modal -->
    <TasksCreateTaskModal
      :visible="showCreateModal"
      @create="createTask"
      @cancel="showCreateModal = false"
    />
  </div>

  <EmptyState v-else icon="&#x1F50D;" title="Project not found" />
</template>

<script setup lang="ts">
const route = useRoute()
const id = route.params.id as string

// Research data
const { data: researchData, pending } = await useApi<{ data: any }>(`/api/researches/${id}`)
const research = computed(() => researchData.value?.data?.research)

// Every research-scoped page publishes the caller's role from the payload it
// already fetches, so the controls beneath it — down to a checkbox inside
// rendered content — know whether they may write.
const { canWrite, canAdmin, readOnlyReason, setFromResearch } = useResearchRole()
watch(researchData, (d) => setFromResearch(d?.data?.research), { immediate: true })

const researchSlug = computed(() => research.value?.code || id)

// Tasks
const { data: tasksData } = await useApi<{ data: any[] }>(`/api/researches/${id}/tasks`)
const tasks = computed(() => tasksData.value?.data ?? [])

// Kanban columns
const columns = [
  { status: 'pending', label: 'Todo' },
  { status: 'in_progress', label: 'In Progress' },
  { status: 'completed', label: 'Completed' },
  { status: 'failed', label: 'Failed' },
]

/* Every status, not only the four the board has a column for. Looking the
   label up in `columns` meant `blocked` and `deferred` — the two the detail
   picker exists to offer — fell through to the raw enum, so the confirmation
   asked about "blocked" and, if a comment was typed, wrote `**[blocked]**`
   into the task result while every other status wrote a display label. */
const STATUS_LABELS: Record<string, string> = {
  pending: 'Todo',
  in_progress: 'In Progress',
  blocked: 'Blocked',
  deferred: 'Deferred',
  completed: 'Completed',
  failed: 'Failed',
}

function statusLabel(status: string): string {
  return STATUS_LABELS[status] ?? status
}

// Task detail modal
const detailTask = ref<any>(null)

function openTaskDetail(task: any) {
  detailTask.value = task
}

/**
 * `?task=T4` opens that task.
 *
 * A task has no page of its own — it is a card and a modal — so this is the
 * only address one can be linked to. A `task_ref` row links here from its code
 * chip, and so does a bare `[[T4]]` written in prose; both used to land on a
 * board with nothing selected, which reads as a link that lost its target.
 *
 * A code naming nothing is ignored rather than reported: the deep link is a
 * convenience, and an error dialog over a board is not the answer to a task
 * somebody deleted.
 *
 * The parameter is dropped from the URL as soon as it is honoured, and that is
 * the whole of the second half of this. The list is replaced on every `task`
 * event — an agent, a colleague dragging a card, this tab creating one — and a
 * parameter left in the address bar made each of those pop the modal back open
 * over a board the reader had closed it on.
 */
const router = useRouter()
watch([() => route.query.task, tasks], ([code]) => {
  if (!code) return
  const want = String(code).toUpperCase()
  const found = tasks.value.find((t: any) => t.code?.toUpperCase() === want || t.id === code)
  if (found && !detailTask.value) detailTask.value = found
  if (!tasks.value.length) return // nothing loaded yet; the code may still resolve
  const { task: _dropped, ...rest } = route.query
  void router.replace({ query: rest })
}, { immediate: true })

const { authFetch } = useAuth()
const rtBase = useRuntimeConfig().public.apiBase || ''

async function saveField(field: string, value: string) {
  if (!detailTask.value) return
  const body: Record<string, any> = {}
  body[field] = value
  await authFetch(`${rtBase}/api/tasks/${detailTask.value.id}`, { method: 'PUT', body })
  tasksData.value = await authFetch<any>(`${rtBase}/api/researches/${id}/tasks`)
  // Refresh detailTask reference
  detailTask.value = tasks.value.find((t: any) => t.id === detailTask.value.id) ?? null
}

async function updatePriority(priority: string) {
  if (!detailTask.value || detailTask.value.priority === priority) return
  await authFetch(`${rtBase}/api/tasks/${detailTask.value.id}`, {
    method: 'PUT',
    body: { priority },
  })
  tasksData.value = await authFetch<any>(`${rtBase}/api/researches/${id}/tasks`)
  detailTask.value = tasks.value.find((t: any) => t.id === detailTask.value.id) ?? null
}

// Drag & drop -> status change
const statusModal = reactive({
  visible: false,
  task: null as any,
  targetStatus: '' as string,
})

function onTaskDrop(task: any, targetStatus: string) {
  statusModal.visible = true
  statusModal.task = task
  statusModal.targetStatus = targetStatus
}

/* The status picker in the detail modal routes into the same confirmation the
   board's drag does, so a keyboard user and a mouse user land in one place and
   the comment prompt is not a privilege of the gesture. */
function onDetailStatusChange(status: string) {
  const task = detailTask.value
  if (!task || task.status === status) return
  onTaskDrop(task, status)
}

function cancelStatusChange() {
  statusModal.visible = false
  statusModal.task = null
}

async function confirmStatusChange(comment: string) {
  if (!statusModal.task) return

  const body: Record<string, any> = { status: statusModal.targetStatus }

  // Append comment to result if provided
  if (comment.trim()) {
    const existing = statusModal.task.result || ''
    const commentBlock = `**[${statusLabel(statusModal.targetStatus)}]** ${comment.trim()}`
    body.result = existing ? `${existing}\n\n${commentBlock}` : commentBlock
  }

  await authFetch(`${rtBase}/api/tasks/${statusModal.task.id}`, {
    method: 'PUT',
    body,
  })

  tasksData.value = await authFetch<any>(`${rtBase}/api/researches/${id}/tasks`)
  cancelStatusChange()
}

// Create task
const showCreateModal = ref(false)

async function createTask(data: { title: string; description: string; priority: string }) {
  await authFetch(`${rtBase}/api/tasks`, {
    method: 'POST',
    body: {
      research_id: id,
      title: data.title,
      description: data.description,
      priority: data.priority,
    },
  })

  tasksData.value = await authFetch<any>(`${rtBase}/api/researches/${id}/tasks`)
  showCreateModal.value = false
}

// Real-time updates
async function reloadTasks() {
  tasksData.value = await authFetch<any>(`${rtBase}/api/researches/${id}/tasks`)
  // The open modal holds an object out of the previous array. Replacing the
  // array leaves it pointing at a snapshot, so an agent completing the task on
  // screen changed nothing in the modal and the next action posted against the
  // old state. The page already re-resolves it after its own writes; a remote
  // write deserves the same.
  if (detailTask.value) {
    detailTask.value = tasks.value.find((t: any) => t.id === detailTask.value.id) ?? null
  }
  if (statusModal.task) {
    statusModal.task = tasks.value.find((t: any) => t.id === statusModal.task.id) ?? null
    if (!statusModal.task) statusModal.visible = false
  }
}

useResearchRealtime(
  () => id,
  (event) => { if (event.entity === 'task') reloadTasks() },
  { researchId: () => research.value?.id, onResync: reloadTasks },
)
</script>

<style scoped>
/* Reduce page-header spacing on tasks page */
.page-header {
  margin-bottom: var(--space-3);
  padding-bottom: var(--space-2);
}

/* Header */

/* Skeleton */
.kanban-skeleton {
  display: grid;
  grid-template-columns: repeat(4, 1fr);
  gap: var(--space-4);
}
.skeleton-column { height: 400px; }

/* Responsive */
@media (max-width: 768px) {
  .kanban-skeleton { grid-template-columns: 1fr; }
}
</style>
