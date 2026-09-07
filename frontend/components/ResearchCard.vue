<template>
  <NuxtLink :to="`/research/${research.code || research.id}`" class="card research-card">
    <div class="card-header">
      <div class="card-title-row">
        <span v-if="research.code" class="short-code">{{ research.code }}</span>
        <h3 class="card-title">{{ research.name }}</h3>
      </div>
      <div class="card-header-actions">
        <!-- `.capture.prevent`, and every part of that is load-bearing.
             The card's root is a NuxtLink. ActionMenu's trigger calls
             stopPropagation but not preventDefault, so a bubble-phase handler
             here never runs — the first version was `@click.prevent.stop` on
             the row and the anchor navigated anyway, taking the whole page with
             it and breaking Archive, which had worked before.
             Capture runs on the way *down*, before the button's handler, so the
             preventDefault lands and the `<a>`'s navigation is cancelled. No
             `.stop`: stopping here would keep the click from reaching the button
             at all. Do not put NuxtLinks inside this menu — their navigation
             dies to the same call.
             And it wraps the menu alone, not the row: on the row it also
             swallowed clicks on the status badge, which is a fat target at the
             corner of a card that is otherwise entirely a link. -->
        <span v-if="canArchive" @click.capture.prevent>
          <ActionMenu title="Project actions" align="right">
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
        </span>
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
/**
 * What this browser may do to the card.
 *
 * `useResearchRole` owns these rules but cannot be used here: it is module
 * state scoped to *the one research on screen*, and this list renders many. So
 * the auth-off branch is mirrored rather than re-invented — and mirroring it is
 * the point. Answering `true` whenever there is no role was right for the local
 * single-binary mode and wrong everywhere else it applied: a server reached
 * across a network with no `api_token` refuses those writes, and the card would
 * have offered Delete to a reader whose DELETE comes back 401. That is the
 * exact bug `useResearchRole`'s own comment records having already fixed once.
 */
const { authFetch, authEnabled } = useAuth()
const { writeApi } = useServerInfo()
const canArchive = computed(() =>
  authEnabled.value ? props.research.role !== 'viewer' : writeApi.value,
)
// Deleting destroys work belonging to everyone else in the team, so an editor
// is not enough.
const canDelete = computed(() =>
  authEnabled.value ? props.research.role === 'owner' : writeApi.value,
)
const showChips = computed(() => showTeam.value || isViewer.value)

const emit = defineEmits<{ tagClick: [tag: string]; statusChanged: []; delete: [] }>()

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
  /* The card runs a `fade-up` animation, whose transform makes it a stacking
     context — so an open menu was painted under the next card and its last row,
     Delete, disappeared behind it on a card with no goal. The same hazard
     `.page-header` already documents. */
  position: relative;
  z-index: var(--z-in-page);
}
.card-header { display: flex; justify-content: space-between; align-items: flex-start; gap: var(--space-3); }
.card-header-actions { display: flex; align-items: center; gap: var(--space-2); }
/* The hover-revealed `.btn-icon` that used to live here went with the archive
   button it styled. ActionMenu's own trigger is always visible and takes its
   styling from the global `.btn-icon`, which is the point: a control that
   appears on hover is undiscoverable on touch and invisible-while-focusable on
   a keyboard. */
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
