<template>
  <div>
    <!-- The head carries the rule the list is no longer allowed to draw: with a
         heading above them, the first line in the card really is a header, and
         a rule under a header means something. Without a heading there is no
         head at all, so the rows meet the card's own edge. -->
    <div v-if="heading || blurb" class="list-head">
      <h3 v-if="heading" class="card-section-title">
        {{ heading }}
        <span v-if="note" class="heading-note">{{ note }}</span>
      </h3>
      <p v-if="blurb" class="group-blurb">{{ blurb }}</p>
    </div>

    <div v-if="skills.length" class="data-rows">
      <div
        v-for="sk in skills"
        :id="`skill-row-${sk.slug}`"
        :key="sk.id || sk.slug"
        :class="['data-row', 'data-row--split', { 'data-row--busy': busySlug === sk.slug }]"
        :aria-busy="busySlug === sk.slug || undefined"
      >
        <div class="skill-main">
          <div class="skill-head">
            <button type="button" class="skill-name" @click="emit('open', sk.slug)">{{ sk.name }}</button>
            <ResearchSettingsSkillOriginBadge
              :tier="sk.tier"
              :ambient="sk.ambient"
              :forked-from="sk.forked_from"
            />
            <span v-if="sk.via_template" class="badge badge-draft" title="A template attached this rather than somebody choosing it.">By template</span>
            <span v-if="sk.needs_trigger" class="badge badge-pending" title="This description was generated when the old instruction moved here. The agent decides from that one line — rewrite it.">Needs trigger</span>
          </div>
          <!-- The trigger line, not a summary: this is the whole of what the
               agent reads before deciding whether to open the skill. -->
          <p class="skill-trigger">{{ sk.description || '— no trigger line, so the agent will never open this —' }}</p>
          <p class="skill-meta">
            <span>{{ sk.body_tokens }} tokens</span>
            <span v-if="sk.attached_at"> · attached {{ relativeTime(sk.attached_at) }}</span>
          </p>
        </div>

        <div v-if="actions" class="skill-actions">
          <button type="button" class="btn btn-sm" @click="emit('open', sk.slug)">Read</button>
          <button
            v-if="canWrite && !sk.ambient"
            type="button"
            class="btn btn-sm btn-danger"
            :disabled="busySlug === sk.slug"
            @click="emit('detach', sk)"
          >Detach</button>
          <span v-else-if="canWrite" class="always-on" title="Product skills are always on and cannot be detached.">Always on</span>
        </div>
        <div v-else-if="canWrite" class="skill-actions">
          <button type="button" class="btn btn-sm" @click="emit('open', sk.slug)">Read</button>
          <button
            type="button"
            class="btn btn-sm btn-primary"
            :disabled="sk.attached || busySlug === sk.slug"
            @click="emit('attach', sk)"
          >{{ sk.attached ? 'Attached' : 'Attach' }}</button>
        </div>
      </div>
    </div>

    <p v-else class="list-empty">{{ emptyText }}</p>
  </div>
</template>

<script setup lang="ts">
import { relativeTime } from '~/composables/useRelativeTime'

withDefaults(defineProps<{
  skills: any[]
  /** true for the attached list, false for the library picker. */
  actions?: boolean
  canWrite?: boolean
  busySlug?: string | null
  heading?: string
  note?: string
  blurb?: string
  emptyText?: string
}>(), {
  actions: true,
  canWrite: false,
  busySlug: null,
  emptyText: 'Nothing here.',
})

const emit = defineEmits<{
  open: [string]
  attach: [any]
  detach: [any]
}>()
</script>

<style scoped>
.heading-note {
  font-size: var(--type-xs);
  font-weight: var(--weight-normal);
  color: var(--color-text-muted);
  font-variant-numeric: tabular-nums;
}
.skill-main { min-width: 0; }
.skill-head {
  display: flex;
  align-items: center;
  gap: var(--space-2);
  flex-wrap: wrap;
}
.skill-name {
  background: none;
  border: none;
  padding: 0;
  font: inherit;
  font-size: var(--type-sm);
  font-weight: var(--weight-medium);
  color: var(--color-text);
  cursor: pointer;
  text-align: left;
  /* A skill name can be long and a pasted one can have no spaces at all. */
  overflow-wrap: anywhere;
}
.skill-name:hover { color: var(--color-primary); }
.skill-trigger {
  font-size: var(--type-xs);
  color: var(--color-text-muted);
  margin-top: var(--space-1);
  max-width: var(--measure-prose);
  overflow-wrap: anywhere;
}
.skill-meta {
  font-size: var(--type-3xs);
  color: var(--color-text-faint);
  margin-top: var(--space-1);
  font-variant-numeric: tabular-nums;
}
.skill-actions {
  display: flex;
  align-items: center;
  gap: var(--space-2);
  flex-shrink: 0;
}
.always-on {
  font-size: var(--type-xs);
  color: var(--color-text-faint);
}

@media (max-width: 768px) {
  .skill-actions { justify-content: flex-end; }
}
</style>
