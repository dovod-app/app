<template>
  <!--
    How to write in this section, above the documents already written in it.

    Open, never a disclosure that starts closed. The premise of the feature is
    that a person sees the same rule the agent does, and the agent re-reads it
    on every write without clicking anything. A rule behind a click is a rule
    nobody reads, which is the exact failure this replaces.

    Three things separate it from the section description above it, and none of
    them is colour alone: a label, full-strength text against the description's
    muted grey, and the 2px rule the product already uses for a blockquote.
  -->
  <section class="instruction-note" :aria-labelledby="labelId">
    <h3 :id="labelId" class="instruction-label">How to write here</h3>
    <!-- renderRefs, not linkRefs: this is a raw field, so it escapes and then
         links. linkRefs here would be an injection sink. -->
    <p
      :id="textId"
      ref="textEl"
      class="instruction-text"
      :class="{ 'is-clamped': !expanded }"
      v-html="rendered"
      @focusin="expanded = true"
    />
    <p v-if="clamped || (editable && settingsPath)" class="instruction-controls">
      <button
        v-if="clamped"
        type="button"
        class="link-btn instruction-more"
        :aria-expanded="expanded"
        :aria-controls="textId"
        @click="expanded = !expanded"
      >{{ expanded ? 'Show fewer lines' : 'Show the whole instruction' }}</button>
      <NuxtLink v-if="editable && settingsPath" :to="settingsPath" class="link-btn instruction-edit">
        Edit this instruction
      </NuxtLink>
    </p>
  </section>
</template>

<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, useId, watch } from 'vue'
import { renderRefs } from '~/composables/useCrossRefs'

const props = defineProps<{
  text: string
  researchSlug: string
  /** Which section this belongs to. Collapsing state is per section, not per
   *  text: re-reading a rule you already expanded should not re-collapse it
   *  when a teammate fixes a typo in it. */
  sectionId?: string
  /** The section's short code, for the settings anchor. */
  sectionCode?: string
  /** Whether the reader may change the rule. False hides the link entirely
   *  rather than disabling it — the house rule, and the settings page would
   *  refuse them anyway. */
  editable?: boolean
}>()

/* The one route into the editor. Without it a section that declares no fields
   has no link to its own settings at all: the "N fields" link beside the title
   is gated on the field spec, so the rule a reader is looking at could only be
   corrected by finding it again in a list of section cards. */
const settingsPath = computed(() => {
  const anchor = props.sectionCode || props.sectionId
  if (!props.researchSlug || !anchor) return ''
  return `/research/${props.researchSlug}/settings?tab=sections#fields-${anchor}`
})

const labelId = useId()
const textId = useId()

const rendered = computed(() => renderRefs(props.text, props.researchSlug))

/* Clamped at six lines, which is the shape the instruction is supposed to have
   in the first place. A longer one still reads in full, one click away — but
   it looks like the exception it is, on a block a reader passes dozens of
   times on the way to the documents below it. */
const textEl = ref<HTMLElement | null>(null)
const clamped = ref(false)
const expanded = ref(false)

/* Measured, never guessed from a character count. A "Show all" that reveals
   nothing teaches a reader to distrust every other one.

   The clamp is applied whenever the block is collapsed, not only once it is
   known to overflow — asking an unclamped element whether it overflows is a
   question that always answers no, so the toggle never appeared and a
   twenty-line instruction pushed the documents off the screen. Six lines is
   invisible on an instruction shorter than six lines. */
function measure() {
  const el = textEl.value
  if (!el) return
  // While expanded the clamp is off, so scrollHeight says nothing about whether
  // it would clip. Leave the answer standing until it collapses again.
  if (expanded.value) return
  clamped.value = el.scrollHeight - el.clientHeight > 1
}

let observer: ResizeObserver | null = null

onMounted(() => {
  measure()
  if (typeof ResizeObserver !== 'undefined' && textEl.value) {
    observer = new ResizeObserver(() => measure())
    observer.observe(textEl.value)
  }
})

onBeforeUnmount(() => {
  observer?.disconnect()
  observer = null
})

watch(() => props.text, async () => {
  await nextTick()
  measure()
})

watch(() => props.sectionId, () => {
  expanded.value = false
})
</script>

<style scoped>
.instruction-note {
  margin: 0 0 var(--space-4);
  padding-left: var(--space-4);
  border-left: 2px solid var(--color-primary);
}

.instruction-label {
  margin: 0 0 var(--space-2);
  font-size: var(--type-xs);
  font-weight: var(--weight-semibold);
  letter-spacing: 0.04em;
  text-transform: uppercase;
  color: var(--color-text-muted);
}

/* pre-wrap because the line breaks are the content: three to six imperatives,
   one per line, is the shape that gets followed. anywhere so a pasted URL
   wraps inside the block instead of scrolling the pane sideways. */
.instruction-text {
  margin: 0;
  font-size: var(--type-sm);
  line-height: var(--line-base);
  color: var(--color-text);
  white-space: pre-wrap;
  overflow-wrap: anywhere;
}

.instruction-text.is-clamped {
  display: -webkit-box;
  -webkit-line-clamp: 6;
  line-clamp: 6;
  -webkit-box-orient: vertical;
  overflow: hidden;
}

.instruction-controls {
  display: flex; align-items: baseline; flex-wrap: wrap;
  gap: var(--space-3);
  margin: var(--space-2) 0 0;
}
.instruction-more,
.instruction-edit { font-size: var(--type-xs); }

/* Paper has no toggle. Printing six lines of an eleven-line rule under a button
   nobody can press is worse than printing the rule. */
@media print {
  .instruction-text.is-clamped {
    display: block;
    overflow: visible;
    -webkit-line-clamp: none;
    line-clamp: none;
  }
  .instruction-controls { display: none; }
}
</style>
