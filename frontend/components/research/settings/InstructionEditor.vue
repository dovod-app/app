<template>
  <div class="instruction-row">
    <!-- Mounted for the life of the row, text swapped. A `role` on an element
         that appears when it should speak is announced unreliably. -->
    <p class="sr-only" role="status">{{ announcement }}</p>

    <div class="instruction-head">
      <h4 class="instruction-heading">How to write here</h4>
      <!-- Grouped, not three loose children: `space-between` over three of
           them splits the whole card width into two gaps and leaves the count
           stranded mid-row, 400px from the heading it belongs to. The same
           mistake is written down in EntriesView, about a status badge. -->
      <div v-if="!editing" class="instruction-head-actions">
        <span v-if="instruction" class="cap-count">{{ storedCount }} / {{ limit }}</span>
        <!-- Removed for a viewer rather than disabled: the house rule. -->
        <button v-if="editable" ref="openBtn" type="button" class="btn btn-sm" @click="open">
          {{ instruction ? 'Edit' : 'Add instruction' }}
        </button>
      </div>
    </div>

    <template v-if="!editing">
      <p v-if="instruction" class="instruction-value">{{ instruction }}</p>
      <p v-else class="instruction-value instruction-empty">
        <template v-if="editable">No instruction. Documents here follow the project's memory and skills alone.</template>
        <template v-else>No instruction. An editor or owner of this team can add one.</template>
      </p>
    </template>

    <form v-else :aria-busy="busy" @submit.prevent="save">
      <p :id="helpId" class="instruction-help">
        Three to six imperatives, read before every document written here. Say what a document
        in this section looks like &mdash; not what the project is about or how the team works:
        those are memory and skills, and this wins over both only on the question of the document.
      </p>

      <textarea
        ref="textarea"
        v-model="draft"
        class="form-textarea instruction-input"
        rows="8"
        placeholder="Name the producing service. State the consumer.&#10;One paragraph of rationale, then the payload.&#10;Fill every required field before saving."
        aria-label="How to write in this section"
        :aria-describedby="describedBy"
        @keydown.esc.prevent="cancel"
        @keydown.enter.meta.prevent="save"
        @keydown.enter.ctrl.prevent="save"
      />

      <div class="instruction-meta">
        <!-- Only where there is a vocabulary to name. A section that declares
             nothing already says so in the card's own blurb above. -->
        <p v-if="fieldKeys.length" :id="keysId" class="instruction-keys">
          Name the fields this section declares &mdash; {{ keyList }} &mdash; so the instruction
          and the fields cannot drift apart.
        </p>
        <span :id="countId" class="cap-count" :class="countClass">
          {{ count }} / {{ limit }}<template v-if="over"> &middot; {{ count - limit }} over</template>
        </span>
      </div>

      <p v-if="over" :id="refusalId" class="inline-error">
        {{ count - limit }} characters over the limit. Shorten it to {{ limit }} or fewer &mdash;
        a longer instruction is refused, not cut short.
      </p>
      <p v-if="error" class="inline-error" role="alert">{{ error }}</p>
      <!-- Last-write-wins: a section carries no version to conflict against, so
           the honest thing is to say what saving will do rather than to discard
           five hundred characters of somebody's typing. -->
      <p v-if="stale" class="instruction-stale">
        This instruction changed elsewhere while you were editing. Saving replaces what is there now.
      </p>

      <div class="instruction-actions">
        <button type="submit" class="btn btn-sm btn-primary" :disabled="busy || over">
          {{ busy ? 'Saving…' : 'Save' }}
        </button>
        <button type="button" class="btn btn-sm" :disabled="busy" @click="cancel">Cancel</button>
        <button
          v-if="draft"
          type="button"
          class="link-btn instruction-clear"
          :disabled="busy"
          @click="clear"
        >Clear</button>
      </div>
    </form>
  </div>
</template>

<script setup lang="ts">
import { computed, nextTick, ref, useId, watch } from 'vue'
import { useUnsavedWork } from '~/composables/useUnsavedWork'

const props = withDefaults(defineProps<{
  instruction: string
  /** The keys this section declares, so the writer can name them. */
  fieldKeys: string[]
  editable?: boolean
  /** The server's cap. Passed in rather than held here for the same reason the
   *  field caps are: a number the client believes and the server enforces will
   *  disagree exactly once, at the worst moment. */
  cap?: number
  /**
   * Persists the instruction and resolves when the server has agreed.
   *
   * A function prop, not an emit: `emit` returns undefined, so awaiting it
   * would clear the busy state before the request lands and make the error
   * line below unreachable.
   */
  onSave?: (instruction: string) => Promise<void>
}>(), {
  editable: false,
  cap: 500,
})

const refusalId = useId()
const helpId = useId()
const keysId = useId()
const countId = useId()

const editing = ref(false)
const draft = ref('')
const openedWith = ref('')
const stale = ref(false)
const busy = ref(false)
const error = ref('')
const announcement = ref('')
const textarea = ref<HTMLTextAreaElement | null>(null)
const openBtn = ref<HTMLButtonElement | null>(null)
/* What Escape threw away. The keystroke is kept for consistency with every
   other edit-in-place here, but discarding five hundred characters on one
   keypress with no way back is the thing this component's stale rule and its
   useUnsavedWork registration both exist to prevent. Re-opening restores it. */
const discarded = ref<string | null>(null)

const limit = computed(() => props.cap)

/* Code points, not UTF-16 units. The server counts runes, so `String.length`
   would disagree by one for every astral character — an emoji in an
   instruction is enough to make the counter lie about the refusal. */
function runes(s: string): number {
  return [...s].length
}

const count = computed(() => runes(draft.value.trim()))
const storedCount = computed(() => runes(props.instruction))
const over = computed(() => count.value > limit.value)
const countClass = computed(() => {
  if (over.value) return 'is-over'
  return count.value >= limit.value * 0.9 ? 'is-near' : ''
})

/* Everything the writer needs to hear about this field, in reading order.
   The refusal joins only when there is one — an aria-describedby pointing at an
   absent node is read as nothing at all in some readers and as a stale id in
   others. */
const describedBy = computed(() => {
  const ids = [helpId]
  if (props.fieldKeys.length) ids.push(keysId)
  ids.push(countId)
  if (over.value) ids.push(refusalId)
  return ids.join(' ')
})

const keyList = computed(() => {
  const keys = props.fieldKeys
  const shown = keys.slice(0, 6).join(', ')
  return keys.length > 6 ? `${shown} and ${keys.length - 6} more` : shown
})

function open() {
  draft.value = discarded.value ?? props.instruction
  discarded.value = null
  openedWith.value = props.instruction
  stale.value = false
  error.value = ''
  // Cleared, not left standing: an aria-live region whose text does not change
  // does not speak, so a second save announced nothing at all.
  announcement.value = ''
  editing.value = true
  nextTick(() => textarea.value?.focus())
}

/* Clear empties the draft; it does not save. Focus has to be moved off it
   deliberately — the button removes itself (`v-if="draft"`), and an unmounted
   focused element drops focus to <body>, so the next Tab restarts at the top of
   the settings page. */
function clear() {
  draft.value = ''
  nextTick(() => textarea.value?.focus())
}

function close() {
  editing.value = false
  stale.value = false
  error.value = ''
  nextTick(() => openBtn.value?.focus())
}

function cancel() {
  if (draft.value !== openedWith.value) discarded.value = draft.value
  close()
}

async function save() {
  if (over.value || busy.value) return
  busy.value = true
  error.value = ''
  try {
    // Waited, never optimistic: the server owns the cap and the permission, and
    // showing the new text before it agrees would misstate the rule documents
    // are being held to.
    await props.onSave?.(draft.value.trim())
    announcement.value = 'Instruction saved.'
    discarded.value = null
    close()
  } catch (e: any) {
    error.value = e?.data?.error || e?.message
      || 'The server refused that change. Your text is still here; fix it and press Save again.'
    // Save disabled itself while busy, which blurred it to <body>. The reader
    // has just been told to fix the text; put them back in it.
    nextTick(() => textarea.value?.focus())
  } finally {
    busy.value = false
  }
}

/* The page refetches the whole research after a neighbouring save, so an open
   editor can be handed a new value under the user's hands. An untouched draft
   silently adopts it; a touched one is kept and the row says what saving will
   do. Discarding five hundred characters of typing to win a race nobody was
   running is the worse outcome. */
watch(() => props.instruction, (next) => {
  if (!editing.value) return
  if (draft.value === openedWith.value) {
    draft.value = next
    openedWith.value = next
    return
  }
  if (next !== openedWith.value) stale.value = true
})

/* The one thing outside this component that can throw the draft away: losing
   access to the research replaces the page body with a notice. This declares
   there is something to lose — the same argument the stale rule above makes
   against discarding five hundred characters of somebody's typing. */
useUnsavedWork(() => editing.value && draft.value !== openedWith.value)

watch(over, (isOver) => {
  if (!editing.value) return
  announcement.value = isOver
    ? `Over the limit by ${count.value - limit.value} characters. Save is unavailable until it is shortened.`
    : 'Within the limit.'
})
</script>

<style scoped>
.instruction-row {
  margin-top: var(--space-3);
  padding-top: var(--space-3);
  border-top: 1px solid var(--color-border);
}

.instruction-head {
  display: flex; align-items: baseline; justify-content: space-between;
  flex-wrap: wrap; gap: var(--space-3);
  margin-bottom: var(--space-2);
}
.instruction-head-actions {
  display: flex; align-items: baseline; gap: var(--space-3);
}

.instruction-heading {
  margin: 0;
  font-size: var(--type-xs);
  font-weight: var(--weight-semibold);
  letter-spacing: 0.04em;
  text-transform: uppercase;
  color: var(--color-text-muted);
}

/* The stored text, unclamped: this is the surface for reading it in full.
   Cross-references stay plain here — a link that navigates away from a
   half-written neighbouring editor is a trap on a settings page. */
.instruction-value {
  margin: 0;
  font-size: var(--type-sm);
  line-height: var(--line-base);
  color: var(--color-text);
  white-space: pre-wrap;
  overflow-wrap: anywhere;
}
.instruction-empty { color: var(--color-text-faint); font-style: italic; }

.instruction-help {
  margin: 0 0 var(--space-3);
  max-width: var(--measure-prose);
  font-size: var(--type-xs);
  line-height: var(--line-base);
  color: var(--color-text-muted);
}

/* No maxlength: it silently truncates a paste, which is the one behaviour this
   field exists to refuse. */
.instruction-input { resize: vertical; }

.instruction-meta {
  display: flex; align-items: baseline; justify-content: space-between;
  flex-wrap: wrap; gap: var(--space-2);
  margin-top: var(--space-2);
}

.instruction-keys {
  margin: 0;
  max-width: var(--measure-prose);
  font-size: var(--type-xs);
  color: var(--color-text-muted);
}

.instruction-stale {
  margin: var(--space-2) 0 0;
  font-size: var(--type-xs);
  color: var(--color-warning);
}

.instruction-actions {
  display: flex; align-items: center; flex-wrap: wrap;
  gap: var(--space-2);
  margin-top: var(--space-3);
}
.instruction-clear { font-size: var(--type-xs); }
</style>
