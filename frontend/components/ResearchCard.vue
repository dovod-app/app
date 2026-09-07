<template>
  <NuxtLink :to="`/research/${research.code || research.id}`" class="card research-card">
    <div class="card-header">
      <div class="card-title-row">
        <span v-if="research.code" class="short-code">{{ research.code }}</span>
        <h3 class="card-title">{{ research.name }}</h3>
      </div>
      <!-- .prevent as well as .stop, and on the wrapper: the card's root is a
           NuxtLink, and ActionMenu's trigger stops propagation without
           preventing the default — which blocks the router but not the
           browser, so the `<a>` still navigates and the SPA reloads. The
           wrapper's preventDefault runs after the inner button's handler and
           has no effect on a `<button>`, so the items keep working. Do not put
           NuxtLinks inside this menu. -->
      <div class="card-header-actions" @click.prevent.stop>
        <ActionMenu v-if="canArchive" title="Project actions" align="right">
          <button class="action-menu-item" @click="toggleArchive">
            {{ research.status === 'archived' ? 'Restore from archive' : 'Archive' }}
          </button>
          <template v-if="canDelete">
            <div class="action-menu-divider" />
            <button class="action-menu-item action-menu-item--danger" @click="emit('delete')">
              Delete project
            </button>
          </template>
        </ActionMenu>
        <StatusBadge :status="research.status" />
      </div>
    </div>

    <p v-if="research.goal" class="card-meta goal-text">{{ research.goal }}</p>

    <div class="card-footer">
      <div v-if="showChips || research.tags?.length" class="tags-row">
        <!-- The chip goes ahead of the tags rather than beside the title: the
             title row already carries a code and a status badge, and a long
             Cyrillic team name there pushes the badge off the card. -->
        <TeamChip v-if="showTeam" :name="research.team_name!" />
        <TeamViewerNotice v-if="isViewer" :team-name="research.team_name" />
        <span
          v-for="tag in research.tags"
          :key="tag"
          :class="['tag', 'tag-clickable', `tag-hue-${tagHue(tag)}`]"
          @click.prevent.stop="emit('tagClick', tag)"
        >{{ tag }}</span>
      </div>
      <span v-if="research.updated_at" class="card-meta timestamp">
        {{ relativeTime(research.updated_at) }}
      </span>
    </div>
  </NuxtLink>
</template>

<script setup lang="ts">
import { tagHue } from '~/composables/useTagHue'
import { relativeTime } from '~/composables/useRelativeTime'

const props = defineProps<{
  research: {
    id: string
    code?: string
    name: string
    goal: string
    status: string
    tags: string[]
    updated_at?: string
    team_name?: string
    team_is_personal?: boolean
    role?: 'viewer' | 'editor' | 'owner'
  }
}>()

// Your own team's name on every card is noise, not information; only a shared
// team earns a chip.
const showTeam = computed(() => !!props.research.team_name && !props.research.team_is_personal)
// Role is shown only where it takes something away. An editor and an owner get
// no label — the working state needs none, and labelling it turns every card
// into a permissions report.
const isViewer = computed(() => props.research.role === 'viewer')
const canArchive = computed(() => props.research.role !== 'viewer')
// Deleting destroys work belonging to everyone else in the team, so an editor
// is not enough. With auth off there are no roles and the field is absent,
// which is the local single-user case: that person owns everything.
const canDelete = computed(() => !props.research.role || props.research.role === 'owner')
const showChips = computed(() => showTeam.value || isViewer.value)

const emit = defineEmits<{ tagClick: [tag: string]; statusChanged: []; delete: [] }>()

const { authFetch } = useAuth()
const config = useRuntimeConfig()
const base = config.public.apiBase || ''
const toasts = useToasts()

async function toggleArchive() {
  const newStatus = props.research.status === 'archived' ? 'active' : 'archived'
  try {
    await authFetch(`${base}/api/researches/${props.research.id}`, {
      method: 'PUT',
      body: { status: newStatus },
    })
    emit('statusChanged')
  } catch (e: any) {
    // This used to have no catch at all, and `@click.prevent.stop` kills the
    // navigation too — so a refusal gave the reader nothing whatsoever: no
    // toast, no state change, no page move.
    toasts.error(e?.data?.error ?? 'The server refused it.', 'Could not archive project')
  }
}
</script>

<style scoped>
.research-card {
  display: flex;
  flex-direction: column;
  text-decoration: none;
  color: inherit;
}
.card-header { display: flex; justify-content: space-between; align-items: flex-start; gap: var(--space-3); }
.card-header-actions { display: flex; align-items: center; gap: var(--space-2); }
.btn-icon {
  display: flex;
  align-items: center;
  justify-content: center;
  width: 28px;
  height: 28px;
  border: none;
  background: none;
  color: var(--color-text-muted);
  border-radius: var(--radius-sm);
  cursor: pointer;
  transition: all var(--transition-fast);
  opacity: 0;
}
.research-card:hover .btn-icon { opacity: 1; }
.btn-icon:hover { background: var(--color-surface-hover); color: var(--color-text); }
.card-title-row { display: flex; align-items: center; gap: var(--space-2); min-width: 0; }
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
.goal-text {
  margin-top: var(--space-2);
  display: -webkit-box;
  -webkit-line-clamp: 2;
  -webkit-box-orient: vertical;
  overflow: hidden;
  line-height: 1.5;
}
.card-footer {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-top: auto;
  padding-top: var(--space-3);
  gap: var(--space-2);
}
.tags-row { display: flex; gap: var(--space-2); flex-wrap: wrap; }
.timestamp { white-space: nowrap; flex-shrink: 0; }
.tag-clickable { cursor: pointer; transition: all var(--transition-fast); }
.tag-clickable:hover { background: var(--color-primary-muted); color: var(--color-primary); }
</style>
