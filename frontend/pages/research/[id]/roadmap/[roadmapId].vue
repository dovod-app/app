<template>
  <div class="roadmap-page">
    <!-- Toolbar -->
    <div class="roadmap-toolbar">
      <div class="toolbar-left">
        <NuxtLink :to="`/research/${researchSlug}/roadmaps`" class="btn btn-sm toolbar-back">
          <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="m15 18-6-6 6-6"/></svg>
          Back
        </NuxtLink>
        <span v-if="roadmap" class="toolbar-title">{{ roadmap.title }}</span>
        <span v-if="roadmap?.code" class="toolbar-code">{{ roadmap.code }}</span>
      </div>
      <div class="toolbar-right">
        <!-- View toggle: governs the mode-specific controls after it -->
        <RoadmapViewToggle v-model="view" />

        <!-- Progress -->
        <div v-if="progress.total > 0" class="toolbar-progress">
          <span class="progress-text">{{ progress.completed }}/{{ progress.total }}</span>
          <div class="progress-bar">
            <div class="progress-fill" :style="{ width: progress.percent + '%' }"></div>
          </div>
        </div>

        <TeamViewerNotice v-if="readOnlyReason" :reason="readOnlyReason" :team-name="researchData?.data?.research?.team_name" />

        <template v-if="view === 'graph'">
        <span class="toolbar-sep"></span>

        <!-- Layout toggle -->
        <button
          :class="['btn btn-sm', { active: layoutDirection === 'LR' }]"
          @click="setLayoutDirection('LR')"
          title="Left to right"
        >
          <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M5 12h14"/><path d="m12 5 7 7-7 7"/></svg>
        </button>
        <button
          :class="['btn btn-sm', { active: layoutDirection === 'TB' }]"
          @click="setLayoutDirection('TB')"
          title="Top to bottom"
        >
          <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M12 5v14"/><path d="m19 12-7 7-7-7"/></svg>
        </button>

        <span class="toolbar-sep"></span>

        <!-- Auto layout: it persists positions for everyone, so it is a write.
             LR/TB and Fit view stay — those are local to this browser. -->
        <button v-if="canWrite" class="btn btn-sm" @click="onAutoLayout" title="Auto layout">
          <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><rect x="3" y="3" width="7" height="7"/><rect x="14" y="3" width="7" height="7"/><rect x="14" y="14" width="7" height="7"/><rect x="3" y="14" width="7" height="7"/></svg>
          Auto layout
        </button>

        <!-- Fit view -->
        <button class="btn btn-sm" @click="fitAll" title="Fit view">
          <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M15 3h6v6"/><path d="M9 21H3v-6"/><path d="M21 3l-7 7"/><path d="M3 21l7-7"/></svg>
        </button>
        </template>
      </div>
    </div>

    <!-- Loading -->
    <div v-if="loading" class="roadmap-loading">
      <div class="skeleton-card" style="width: 200px; height: 80px;"></div>
      <p class="card-meta mt-4">Loading roadmap...</p>
    </div>

    <!-- Error -->
    <div v-else-if="error" class="roadmap-loading">
      <p class="card-meta">{{ error }}</p>
      <button class="btn btn-sm mt-4" @click="refresh">Retry</button>
    </div>

    <!-- Canvas -->
    <div v-else class="roadmap-canvas">
      <RoadmapStagesBoard
        v-if="view === 'stages'"
        :stages="roadmap?.stages ?? []"
        :nodes="rawNodes"
        :edges="rawEdges"
        @node-click="openNode"
        @switch-graph="view = 'graph'"
      />
      <RoadmapTimeline
        v-else-if="view === 'timeline'"
        :nodes="rawNodes"
        :edges="rawEdges"
        @node-click="openNode"
        @switch-graph="view = 'graph'"
      />
      <VueFlow
        v-else
        :nodes="nodes"
        :edges="edges"
        :node-types="nodeTypes"
        :default-viewport="{ x: 0, y: 0, zoom: 0.85 }"
        :min-zoom="0.15"
        :max-zoom="2"
        :fit-view-on-init="true"
        :nodes-draggable="canWrite"
        :nodes-connectable="false"
        :edges-updatable="false"
        :pan-on-drag="true"
        :zoom-on-scroll="true"
        class="roadmap-flow"
        @node-click="onNodeClick"
        @node-drag-stop="onNodeDragStop"
        @pane-click="onPaneClick"
      >
        <MiniMap
          :node-color="minimapNodeColor"
          :mask-color="'var(--color-nav)'"
          position="bottom-right"
        />
        <Controls position="bottom-left" />
      </VueFlow>
    </div>

    <!-- Inline write-error banner: a failed save surfaces here, not as a takeover -->
    <div v-if="writeError" class="rm-write-error" role="alert">
      <span>{{ writeError }}</span>
      <button class="rm-write-error-dismiss" @click="clearWriteError" aria-label="Dismiss">&times;</button>
    </div>

    <!-- Node detail modal -->
    <RoadmapNodePopover
      :node="selectedNode"
      :statuses="roadmap?.statuses ?? []"
      :stages="roadmap?.stages ?? []"
      @update-status="onUpdateStatus"
      @update-entity-status="onUpdateEntityStatus"
      @update-stage="onUpdateStage"
      @update-date="onUpdateDate"
      @update-end-date="onUpdateEndDate"
      @navigate="onNavigate"
      @close="selectedNode = null"
    />
  </div>
</template>

<script setup lang="ts">
import { VueFlow, useVueFlow } from '@vue-flow/core'
import { MiniMap } from '@vue-flow/minimap'
import { Controls } from '@vue-flow/controls'
import '@vue-flow/core/dist/style.css'
import '@vue-flow/core/dist/theme-default.css'
import '@vue-flow/minimap/dist/style.css'
import '@vue-flow/controls/dist/style.css'

import RoadmapRootNode from '~/components/roadmap/RoadmapRootNode.vue'
import RoadmapStepNode from '~/components/roadmap/RoadmapStepNode.vue'
import RoadmapRefNode from '~/components/roadmap/RoadmapRefNode.vue'
import RoadmapNodePopover from '~/components/roadmap/RoadmapNodePopover.vue'
import RoadmapViewToggle, { type RoadmapView } from '~/components/roadmap/RoadmapViewToggle.vue'
import RoadmapStagesBoard from '~/components/roadmap/RoadmapStagesBoard.vue'
import RoadmapTimeline from '~/components/roadmap/RoadmapTimeline.vue'

const route = useRoute()
const researchId = route.params.id as string
const roadmapId = route.params.roadmapId as string
const runtimeConfig = useRuntimeConfig()
const apiBase = runtimeConfig.public.apiBase || ''
const { authFetch } = useAuth()

// Resolve research slug for back link
const { data: researchData } = await useApi<{ data: any }>(`/api/researches/${researchId}`)
const researchSlug = computed(() => researchData.value?.data?.research?.code || researchId)

// Every research-scoped page publishes the caller's role from the payload it
// already fetches, so the controls beneath it — down to a checkbox inside
// rendered content — know whether they may write.
const { canWrite, canAdmin, readOnlyReason, setFromResearch } = useResearchRole()
watch(researchData, (d) => setFromResearch(d?.data?.research), { immediate: true })


const nodeTypes = {
  'roadmap-root': markRaw(RoadmapRootNode),
  'roadmap-step': markRaw(RoadmapStepNode),
  'roadmap-ref': markRaw(RoadmapRefNode),
}

const {
  roadmap,
  nodes,
  edges,
  loading,
  error,
  writeError,
  clearWriteError,
  progress,
  refresh,
  updateNodeStatus,
  updateNodeStage,
  updateNodeDate,
  updateNodeEndDate,
  updateNodePosition,
  autoLayout,
  layoutDirection,
  setLayoutDirection,
  isInteracting,
} = useRoadmap(researchId, roadmapId)

// Vue Flow instance
const { fitView } = useVueFlow()

// Active view: initialised from the roadmap's stored default, then local — the
// toggle overrides ephemerally in v1 and does not persist per user.
const view = ref<RoadmapView>('graph')
let viewInitialised = false
watch(roadmap, (rm) => {
  if (rm && !viewInitialised) {
    view.value = (rm.view as RoadmapView) || 'graph'
    viewInitialised = true
  }
}, { immediate: true })

// Board and timeline read the raw API roadmap, not the Vue Flow graph.
const rawNodes = computed(() => roadmap.value?.nodes ?? [])
const rawEdges = computed(() => roadmap.value?.edges ?? [])

function fitAll() {
  fitView({ padding: 0.15, duration: 300 })
}

// Node click → popover
const selectedNode = ref<{
  id: string
  title: string
  description: string
  nodeType: string
  status: string
  refType?: string
  refId?: string
  refData?: any
  stage?: string
  node_date?: string
  node_end_date?: string
} | null>(null)

// Open the popover for a node by id, reading the raw node so stage/date are
// present in every view. Shared by the graph, the board and the timeline.
function openNode(nodeId: string) {
  const n = roadmap.value?.nodes.find(x => x.id === nodeId)
  if (!n) return
  selectedNode.value = {
    id: n.id,
    title: n.title,
    description: n.description || '',
    nodeType: n.node_type || 'step',
    status: n.status || '',
    refType: n.ref_type,
    refId: n.ref_id,
    refData: n.ref_data,
    stage: n.stage || '',
    node_date: n.node_date || '',
    node_end_date: n.node_end_date || '',
  }
}

function onNodeClick({ node }: { node: any }) {
  // Don't show modal for root node
  if (node.type === 'roadmap-root') return
  openNode(node.id)
}

async function onUpdateStatus(nodeId: string, status: string) {
  await updateNodeStatus(nodeId, status)
  selectedNode.value = null
}

// Stage and date changes keep the popover open (you may set both) and move the
// card optimistically. Sync the open popover so its controls reflect the change.
async function onUpdateStage(nodeId: string, stage: string) {
  await updateNodeStage(nodeId, stage)
  if (selectedNode.value?.id === nodeId) selectedNode.value = { ...selectedNode.value, stage }
}
async function onUpdateDate(nodeId: string, date: string) {
  await updateNodeDate(nodeId, date)
  if (selectedNode.value?.id === nodeId) selectedNode.value = { ...selectedNode.value, node_date: date }
}
async function onUpdateEndDate(nodeId: string, date: string) {
  await updateNodeEndDate(nodeId, date)
  if (selectedNode.value?.id === nodeId) selectedNode.value = { ...selectedNode.value, node_end_date: date }
}

async function onUpdateEntityStatus(refType: string, refId: string, status: string) {
  const endpoints: Record<string, string> = {
    task: `${apiBase}/api/tasks/${refId}`,
    entry: `${apiBase}/api/entries/${refId}`,
    session: `${apiBase}/api/sessions/${refId}`,
    research: `${apiBase}/api/researches/${refId}`,
    question: `${apiBase}/api/questions/${refId}`,
  }

  const url = endpoints[refType]
  if (!url) return

  try {
    await authFetch(url, {
      method: 'PUT',
      body: { status },
    })
    // Soft refresh — update data without resetting viewport
    await refresh(true)
    // Update selectedNode with fresh ref_data from roadmap
    if (selectedNode.value && roadmap.value) {
      const fresh = roadmap.value.nodes.find(n => n.id === selectedNode.value!.id)
      if (fresh) {
        selectedNode.value = {
          ...selectedNode.value,
          refData: fresh.ref_data,
        }
      }
    }
  } catch (e: any) {
    console.error('Failed to update entity status:', e)
  }
}

function onNavigate(node: any) {
  const refType = node.refType
  const refId = node.refId
  const refResearchId = node.refData?.research_id || researchId
  if (!refType || !refId) return

  selectedNode.value = null

  let path = ''
  switch (refType) {
    case 'entry':
      path = `/research/${refResearchId}/entry/${refId}`
      break
    case 'task':
      path = `/research/${refResearchId}/tasks`
      break
    case 'session':
      path = `/research/${refResearchId}/session/${refId}`
      break
    case 'research':
      path = `/research/${refResearchId}`
      break
    case 'question':
      path = `/research/${refResearchId}`
      break
  }

  if (path) {
    window.open(path, '_blank')
  }
}

function onNodeDragStop({ node }: { node: any }) {
  updateNodePosition(node.id, node.position.x, node.position.y)
}

async function onAutoLayout() {
  await autoLayout()
  nextTick(() => fitView({ padding: 0.15, duration: 300 }))
}

function minimapNodeColor(node: any): string {
  if (node.type === 'roadmap-root') return 'var(--color-primary)'
  if (node.type === 'roadmap-ref') return 'var(--hue-5)'
  return 'var(--color-text-muted)'
}

// Close popover when clicking on the canvas (pane), not on nodes
function onPaneClick() {
  selectedNode.value = null
}

onMounted(() => {
  refresh()
})

// A change this tab made is already on screen; anything else has to land. The
// roadmap is scoped so a change to another research cannot reach in here.
// A repaint landing mid-drag fights the pointer, so it waits — but it waits,
// rather than being dropped. Two seconds of dragging used to swallow an agent's
// roadmap edit until some unrelated event happened along.
let deferredRefresh: ReturnType<typeof setTimeout> | null = null
function refreshWhenIdle() {
  if (deferredRefresh) return
  // A repaint that restacks the board out from under an open popover is as
  // disruptive as one landing mid-drag, so both defer.
  if (!isInteracting() && !selectedNode.value) {
    refresh(true)
    return
  }
  deferredRefresh = setTimeout(() => {
    deferredRefresh = null
    refreshWhenIdle()
  }, 500)
}
onUnmounted(() => {
  if (deferredRefresh) clearTimeout(deferredRefresh)
})

useResearchRealtime(
  () => researchId,
  (event) => {
    if (event.entity !== 'roadmap' || isSelf(event)) return
    refreshWhenIdle()
  },
  {
    researchId: () => researchData.value?.data?.research?.id,
    onResync: () => refresh(true),
  },
)
</script>

<style scoped>
.rm-write-error {
  position: fixed;
  top: var(--space-4);
  left: 50%;
  transform: translateX(-50%);
  z-index: calc(var(--z-overlay) + 1);
  display: flex;
  align-items: center;
  gap: var(--space-3);
  max-width: min(90vw, 32rem);
  padding: var(--space-2) var(--space-4);
  background: var(--color-surface);
  border: 1px solid rgba(var(--color-error-rgb), 0.5);
  border-radius: var(--radius);
  box-shadow: var(--shadow-2);
  font-size: var(--type-sm);
  color: var(--color-text);
}
.rm-write-error-dismiss {
  appearance: none;
  background: none;
  border: none;
  color: var(--color-text-muted);
  font-size: var(--type-lg);
  line-height: 1;
  cursor: pointer;
  padding: 0;
}
.roadmap-page {
  position: fixed;
  inset: 0;
  display: flex;
  flex-direction: column;
  background: var(--color-bg);
  z-index: var(--z-overlay);
}

.roadmap-toolbar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: var(--space-3) var(--space-5);
  background: var(--color-surface-raised);
  backdrop-filter: blur(12px);
  border-bottom: 1px solid var(--color-border);
  gap: var(--space-4);
  flex-shrink: 0;
}
.toolbar-left {
  display: flex;
  align-items: center;
  gap: var(--space-3);
}
.toolbar-back { gap: var(--space-1); }
.toolbar-title {
  font-size: var(--type-sm);
  font-weight: var(--weight-semibold);
  color: var(--color-text);
  letter-spacing: -0.01em;
}
.toolbar-code {
  font-size: var(--type-xs);
  font-weight: var(--weight-semibold);
  color: var(--color-primary);
  background: var(--color-primary-muted);
  padding: 0.15rem 0.4rem;
  border-radius: var(--radius-xs);
  font-family: 'JetBrains Mono', monospace;
}
.toolbar-right {
  display: flex;
  align-items: center;
  gap: var(--space-2);
}
.toolbar-sep {
  width: 1px;
  height: 20px;
  background: var(--color-border-strong);
  margin: 0 var(--space-1);
}

.toolbar-progress {
  display: flex;
  align-items: center;
  gap: var(--space-2);
}
.progress-text {
  font-size: var(--type-xs);
  color: var(--color-text-muted);
  font-variant-numeric: tabular-nums;
  white-space: nowrap;
}
.progress-bar {
  width: 100px;
  height: 4px;
  background: var(--color-surface-hover);
  border-radius: var(--radius-hair);
  overflow: hidden;
}
.progress-fill {
  height: 100%;
  background: rgba(var(--color-success-rgb), 0.8);
  border-radius: var(--radius-hair);
  transition: width 0.3s ease;
}

.roadmap-canvas {
  flex: 1;
  min-height: 0;
}
.roadmap-flow {
  width: 100%;
  height: 100%;
}
.roadmap-loading {
  flex: 1;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
}

/* Vue Flow overrides */
.roadmap-flow :deep(.vue-flow__edge-path) {
  stroke-linecap: round;
}
.roadmap-flow :deep(.vue-flow__background) {
  background: var(--color-bg);
}
.roadmap-flow :deep(.vue-flow__minimap) {
  background: var(--color-surface);
  border: 1px solid var(--color-border);
  border-radius: var(--radius);
}
.roadmap-flow :deep(.vue-flow__controls) {
  background: var(--color-surface);
  border: 1px solid var(--color-border);
  border-radius: var(--radius);
  box-shadow: var(--shadow-1);
}
.roadmap-flow :deep(.vue-flow__controls-button) {
  background: var(--color-surface);
  border-color: var(--color-border);
  color: var(--color-text-muted);
  fill: var(--color-text-muted);
}
.roadmap-flow :deep(.vue-flow__controls-button:hover) {
  background: var(--color-surface-hover);
}

/* Responsive */
@media (max-width: 768px) {
  .roadmap-toolbar {
    flex-wrap: wrap;
    padding: var(--space-2) var(--space-3);
    gap: var(--space-2);
  }
  .toolbar-right {
    flex-wrap: wrap;
    gap: var(--space-1);
  }
  .toolbar-title { display: none; }
  .toolbar-sep { display: none; }
}
</style>
