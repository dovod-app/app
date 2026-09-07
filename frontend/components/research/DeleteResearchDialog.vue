<template>
  <ModalOverlay
    :visible="visible"
    size="md"
    labelledby="delete-research-title"
    initial-focus="[data-confirm-code]"
    @close="close"
  >
    <ModalHeader title="Delete project" title-id="delete-research-title" @close="close" />

    <div class="delete-research">
      <p class="delete-research__subject">
        <span class="short-code">{{ code }}</span>
        <span class="delete-research__name">{{ name }}</span>
      </p>

      <p v-if="loading" class="delete-research__lead">Working out what this will remove…</p>

      <template v-else-if="summaryFailed">
        <p class="delete-research__warning">
          Could not load what this will remove. Deleting still removes the project and everything
          filed under it.
        </p>
      </template>

      <template v-else-if="lines.length">
        <p class="delete-research__lead">Deleting this removes, permanently:</p>
        <ul class="delete-research__list">
          <li v-for="line in lines" :key="line">{{ line }}</li>
        </ul>
      </template>

      <p v-else class="delete-research__lead">
        Nothing has been filed under this project yet — there is nothing else to remove.
      </p>

      <p v-if="consequences" class="delete-research__warning">{{ consequences }}</p>

      <p v-if="stale" class="delete-research__stale">
        This project changed while this was open — the counts above may be out of date.
      </p>

      <div class="delete-research__export">
        <button
          type="button"
          class="link-btn"
          :aria-disabled="download.pending.value ? 'true' : undefined"
          @click="downloadCopy"
        >
          ↓ {{ download.pending.value ? 'Preparing…' : 'Download a copy first (JSON)' }}
        </button>
        <span v-if="savedAs" class="delete-research__saved">Saved {{ savedAs }}.</span>
        <span v-if="downloadError" class="delete-research__error">{{ downloadError }}</span>
      </div>

      <div class="delete-research__confirm">
        <label class="delete-research__label" :for="fieldId">Type {{ code }} to confirm</label>
        <input
          :id="fieldId"
          v-model="typed"
          data-confirm-code
          class="form-input delete-research__field"
          type="text"
          autocomplete="off"
          spellcheck="false"
          autocapitalize="characters"
          inputmode="text"
          maxlength="12"
          :aria-describedby="hintId"
          @keydown.enter.prevent="onEnter"
          @input="mismatch = false"
        />
        <p
          :id="hintId"
          class="delete-research__hint"
          :class="{ 'delete-research__hint--error': mismatch || !!failure }"
          :role="mismatch || failure ? 'alert' : undefined"
        >
          {{ hint }}
        </p>
      </div>

      <div class="modal-actions">
        <button type="button" class="btn btn-sm" :disabled="busy" @click="close">Cancel</button>
        <button
          type="button"
          class="btn btn-sm btn-danger"
          :disabled="!matches || permanentlyRefused"
          :aria-disabled="busy ? 'true' : undefined"
          @click="submit"
        >
          {{ busy ? 'Deleting…' : failure ? 'Try again' : 'Delete' }}
        </button>
      </div>
    </div>
  </ModalOverlay>
</template>

<script setup lang="ts">
import type { DeletionSummary } from '~/composables/useResearchDelete'

/**
 * Destroying a project, confirmed by typing its short code.
 *
 * Not a variant of ConfirmModal. That one has no input, and giving it one would
 * put a field on every other confirmation in the product — the same call
 * SendBackModal already made and recorded. This dialog additionally carries the
 * summary list, an export offer and a staleness notice, none of which belong on
 * a generic confirm.
 *
 * The typed code is the whole feature. It is compared case-insensitively and
 * whitespace-trimmed, and **paste is deliberately not blocked**: the safety is
 * in having read which project this is, not in having held Shift, and blocking
 * paste breaks voice control, switch access and password managers while
 * stopping nobody. Do not add it back.
 */
const props = defineProps<{
  visible: boolean
  code: string
  name: string
  summary: DeletionSummary | null
  loading?: boolean
  summaryFailed?: boolean
  /** The project changed under the open dialog, so the counts may be stale. */
  stale?: boolean
  busy?: boolean
  /** A failure to report, already turned into a sentence by the caller. */
  failure?: string | null
  /** 403: the delete can never succeed from here, so the button stays dead. */
  permanentlyRefused?: boolean
}>()

const emit = defineEmits<{ confirm: []; cancel: [] }>()

const typed = ref('')
const mismatch = ref(false)
const fieldId = useId()
const hintId = useId()

const download = useDownload()
const savedAs = ref<string | null>(null)
const downloadError = ref<string | null>(null)

const matches = computed(
  () => typed.value.trim().toUpperCase() === props.code.trim().toUpperCase(),
)

function plural(n: number, one: string, many: string) {
  return `${n} ${n === 1 ? one : many}`
}

/**
 * Only non-zero rows. Sessions and questions share a line because a session
 * with no questions is not a separate idea.
 */
const lines = computed<string[]>(() => {
  const s = props.summary
  if (!s) return []
  const out: string[] = []
  if (s.sections) out.push(plural(s.sections, 'section', 'sections'))
  if (s.entries) out.push(plural(s.entries, 'document', 'documents'))
  if (s.sessions) {
    out.push(
      s.questions
        ? `${plural(s.sessions, 'session', 'sessions')} and ${plural(s.questions, 'question', 'questions')}`
        : plural(s.sessions, 'session', 'sessions'),
    )
  }
  if (s.tasks) out.push(plural(s.tasks, 'task', 'tasks'))
  if (s.roadmaps) out.push(plural(s.roadmaps, 'roadmap', 'roadmaps'))
  if (s.annotations) out.push(plural(s.annotations, 'mark', 'marks'))
  return out
})

/** What happens outside this project — the part that changes a decision. */
const consequences = computed(() => {
  const s = props.summary
  if (!s) return ''
  const parts: string[] = []
  if (s.shares) {
    parts.push(`${plural(s.shares, 'share link', 'share links')} stop${s.shares === 1 ? 's' : ''} working.`)
  }
  if (s.incoming_refs) {
    const from = s.incoming_from ?? []
    let where = ''
    if (from.length === 1) where = ` from ${from[0]!.code}`
    else if (from.length > 1) where = ` from ${from[0]!.code} and ${from.length - 1} other${from.length - 1 === 1 ? '' : 's'}`
    parts.push(`${plural(s.incoming_refs, 'reference', 'references')}${where} stop resolving.`)
  }
  return parts.join(' ')
})

const hint = computed(() => {
  if (props.failure) return props.failure
  if (mismatch.value) return `That is not the code. Type ${props.code} exactly as it appears above.`
  return 'This deletes the project and everything in it.'
})

function onEnter() {
  if (!matches.value) {
    mismatch.value = true
    return
  }
  submit()
}

function submit() {
  if (!matches.value || props.busy || props.permanentlyRefused) return
  emit('confirm')
}

function close() {
  if (props.busy) return
  emit('cancel')
}

async function downloadCopy() {
  if (download.pending.value) return
  savedAs.value = null
  downloadError.value = null
  // A failed optional download must not block or reset the delete: the dialog
  // stays exactly as it was and says so in one line.
  const ok = await download.start(
    `/api/researches/${props.code}/export/portable`,
    `${props.code}.json`,
  )
  if (ok) savedAs.value = download.filename.value
  else downloadError.value = `Could not download a copy. ${download.error.value?.message ?? ''}`.trim()
}

// A reopened dialog starts empty. Leaving the code typed would mean the second
// delete needed no confirmation at all.
watch(
  () => props.visible,
  (open) => {
    if (!open) return
    typed.value = ''
    mismatch.value = false
    savedAs.value = null
    downloadError.value = null
  },
)
</script>

<style scoped>
.delete-research {
  display: flex;
  flex-direction: column;
  gap: var(--space-4);
}

.delete-research__subject {
  display: flex;
  align-items: baseline;
  gap: var(--space-2);
  margin: 0;
  font-size: var(--type-base);
}

.delete-research__name {
  overflow-wrap: anywhere;
  display: -webkit-box;
  -webkit-line-clamp: 2;
  -webkit-box-orient: vertical;
  overflow: hidden;
}

.delete-research__lead {
  margin: 0;
  font-size: var(--type-sm);
  color: var(--color-text-muted);
}

.delete-research__list {
  margin: 0;
  padding-left: var(--space-4);
  display: flex;
  flex-direction: column;
  gap: var(--space-1);
  font-size: var(--type-sm);
  font-variant-numeric: tabular-nums;
}

.delete-research__warning {
  margin: 0;
  font-size: var(--type-xs);
  color: var(--color-warning);
}

.delete-research__stale {
  margin: 0;
  font-size: var(--type-xs);
  color: var(--color-text-muted);
}

.delete-research__export {
  display: flex;
  flex-wrap: wrap;
  align-items: baseline;
  gap: var(--space-2);
  font-size: var(--type-xs);
}

.delete-research__saved {
  color: var(--color-text-muted);
}

.delete-research__error {
  color: var(--color-error);
}

/* The rule separates what will happen from the act, which is the one hierarchy
   in this dialog. */
.delete-research__confirm {
  border-top: 1px solid var(--color-border);
  padding-top: var(--space-4);
  display: flex;
  flex-direction: column;
  gap: var(--space-2);
}

.delete-research__label {
  font-size: var(--type-2xs);
  font-weight: 600;
}

.delete-research__field {
  max-width: 12ch;
  font-family: 'JetBrains Mono', monospace;
}

.delete-research__hint {
  margin: 0;
  font-size: var(--type-xs);
  color: var(--color-text-muted);
}

.delete-research__hint--error {
  color: var(--color-error);
}
</style>
