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
        <label class="delete-research__label" :for="fieldId">Type {{ confirmWord }} to confirm</label>
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
          class="btn btn-sm btn-danger btn-danger--solid"
          :disabled="!matches"
          :aria-disabled="busy || permanentlyRefused ? 'true' : undefined"
          :aria-describedby="hintId"
          @click="submit"
        >
          {{ busy ? 'Deleting…' : failure ? 'Try again' : 'Delete' }}
        </button>
      </div>
    </div>
  </ModalOverlay>
</template>

<script setup lang="ts">
import {
  deletionConsequences,
  deletionLines,
  type DeletionSummary,
} from '~/composables/useResearchDelete'

/**
 * Destroying a project, confirmed by typing its short code.
 *
 * Not a variant of ConfirmModal. That one has no input, and giving it one would
 * put a field on every other confirmation in the product — the same call
 * SendBackModal already made and recorded. This dialog additionally carries the
 * summary list and an export offer, neither of which belongs on a generic
 * confirm.
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

/**
 * A project always has a short code, but the payload's is optional and the list
 * falls back to the id. A uuid cannot be typed into a 12-character field, so
 * without this the confirm could never match and the project could never be
 * deleted from that screen.
 */
const confirmWord = computed(() =>
  props.code && props.code.length <= 12 ? props.code : 'DELETE',
)

const matches = computed(
  () => typed.value.trim().toUpperCase() === confirmWord.value.trim().toUpperCase(),
)

// Both from the composable that owns DeletionSummary, so this dialog and the
// danger-zone row on the settings page cannot drift. They had: the row counted
// neither roadmaps nor marks, so a project holding only those read "Nothing has
// been filed under this project yet" in the row and listed two items here.
const lines = computed<string[]>(() => deletionLines(props.summary))
const consequences = computed(() => deletionConsequences(props.summary))

const hint = computed(() => {
  if (props.failure) return props.failure
  if (mismatch.value) return `That is not the code. Type ${confirmWord.value} exactly as it appears above.`
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
  // useDownload's messages are deliberately noun-less fragments, so the lead-in
  // has to end mid-sentence for them to read as one.
  else downloadError.value = `A copy could not be downloaded — ${download.error.value?.message ?? 'the request failed.'}`
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

/* The global rule adds 20px on top of the column's 16px gap, and flex gaps do
   not collapse with margins — so the action row sat 36px below the field while
   every other section was 16px apart. */
.delete-research .modal-actions {
  margin-top: 0;
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
