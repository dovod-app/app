<template>
  <div class="card card--list">
    <div class="list-head">
      <h3 class="card-section-title">Cross-references</h3>
      <p class="group-blurb">
        A reference like <code class="short-code">[[E12]]</code> is indexed when a document is
        saved, and one written before its target resolves as soon as that target is created.
        Rebuilding re-reads every document, task result and answer, and rewrites the index.
      </p>
    </div>

    <div class="data-rows">
      <div
        class="data-row data-row--split"
        :class="{ 'data-row--busy': busy }"
        :aria-busy="busy || undefined"
      >
        <div>
          <span class="maintenance-label">Reference index</span>
          <p class="maintenance-note">{{ note }}</p>

          <div v-if="codes.length" class="maintenance-codes">
            <span v-for="code in codes" :key="code" class="short-code maintenance-code">{{ code }}</span>
            <span v-if="moreCodes" class="maintenance-more">and more</span>
          </div>

          <div v-if="loading" class="skeleton-card maintenance-skeleton"></div>
        </div>

        <!-- A failed read never withholds the repair: the diagnosis and the
             cure are separate requests, and the one that matters is the cure. -->
        <button
          type="button"
          class="btn btn-sm maintenance-action"
          :aria-disabled="busy"
          @click="rebuild"
        >{{ busy ? 'Rebuilding…' : 'Rebuild' }}</button>
      </div>
    </div>

    <!-- Announced, and only when something has happened. The visible note above
         is not the live region: it fills on first load, when nothing has
         happened yet, and this panel remounts on every return to the tab. -->
    <p class="sr-only" role="status">{{ announcement }}</p>

    <p v-if="forbidden" class="inline-error maintenance-error" role="alert">
      {{ forbidden }}
    </p>
    <p v-else-if="writeError" class="inline-error inline-error--action maintenance-error" role="alert">
      <span>Couldn't rebuild the index.</span>
      <button type="button" class="btn btn-sm" @click="rebuild">Try again</button>
    </p>
    <p v-else-if="readError" class="inline-error inline-error--action maintenance-error" role="alert">
      <span>Couldn't read the reference index.</span>
      <button type="button" class="btn btn-sm" @click="load()">Try again</button>
    </p>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { relativeTime } from '~/composables/useRelativeTime'

interface Summary { total: number; unresolved: number; dangling: string[]; dangling_total?: number }

const props = defineProps<{ researchId: string }>()

const { authFetch } = useAuth()
const base = useRuntimeConfig().public.apiBase || ''

const summary = ref<Summary | null>(null)
const loading = ref(true)
const busy = ref(false)
const readError = ref(false)
const writeError = ref(false)
const forbidden = ref('')
const rebuiltAt = ref('')
const announcement = ref('')

const total = computed(() => summary.value?.total ?? 0)
const unresolved = computed(() => summary.value?.unresolved ?? 0)
/* Rendered as the server sent them. The server already decided how many codes
   are worth listing and deduplicated them; capping again here produced a
   "+N more" computed from rows against a list of codes — two different units,
   and the number was wrong in every case but one. */
const codes = computed(() => (loading.value ? [] : summary.value?.dangling ?? []))
/* Said, not counted: past the server's cap the client cannot know how many
   further distinct codes exist, and inventing a number from the row count is
   what made the old line wrong. */
const moreCodes = computed(() => {
  const totalCodes = summary.value?.dangling_total ?? codes.value.length
  return totalCodes > codes.value.length
})

const note = computed(() => {
  if (loading.value) return ''
  const rebuilt = rebuiltAt.value ? `Rebuilt ${relativeTime(rebuiltAt.value)}. ` : ''
  if (readError.value && !rebuiltAt.value) return ''
  if (total.value === 0) {
    // Zero indexed references is the exact symptom of a stale index, so this
    // says so rather than "nothing here yet" — except right after a rebuild,
    // which would be advising the action just taken.
    return rebuilt + (rebuiltAt.value
      ? 'No cross-references found. A document cites another by writing [[E12]].'
      : 'No cross-references indexed yet. A document cites another by writing [[E12]]. '
        + 'If documents here do cite each other, the index is out of date — rebuild it.')
  }
  if (unresolved.value === 0) {
    return `${rebuilt}All ${total.value} references resolve.`
  }
  // No cause is asserted. A code the resolver has never been able to resolve —
  // a session or a question reference — sits in this list too, and calling that
  // a typo would be a confident lie about something a rebuild cannot fix.
  const tail = rebuiltAt.value ? 'still point at nothing.' : 'point at nothing.'
  return `${rebuilt}${unresolved.value} of ${total.value} references ${tail}`
})

/**
 * Reads the index summary.
 *
 * `keepOnFailure` is set when this runs after a rebuild that already succeeded:
 * the counts in hand are correct and only the chip list is being refreshed, so
 * a failure here must leave the result standing. Clearing it printed "No
 * cross-references found" over an index with forty-seven rows.
 */
async function load(keepOnFailure = false) {
  readError.value = false
  loading.value = true
  try {
    const res = await authFetch<any>(`${base}/api/researches/${props.researchId}/crossrefs`)
    // Read directly. Gating on a sibling field meant a response whose `data`
    // was null threw a perfectly good summary away and reported "no
    // cross-references indexed yet" — the one false diagnosis this copy exists
    // to avoid.
    summary.value = res?.summary ?? null
  } catch (e: any) {
    const status = e?.status ?? e?.response?.status
    if (status === 403 || status === 404) {
      forbidden.value = 'You no longer have access to this project. Reload the page.'
    } else if (status === 401) {
      forbidden.value = 'Your session expired. Sign in again.'
    } else {
      readError.value = true
    }
    // The rebuild's own numbers survive; only the codes beside them are stale.
    if (!keepOnFailure) summary.value = null
  } finally {
    loading.value = false
  }
}

async function rebuild() {
  if (busy.value) return
  busy.value = true
  writeError.value = false
  forbidden.value = ''
  try {
    const res = await authFetch<any>(`${base}/api/researches/${props.researchId}/crossrefs/rebuild`, {
      method: 'POST',
    })
    const stillBroken = (res?.unresolved ?? 0) > 0
    summary.value = {
      total: res?.references ?? 0,
      unresolved: res?.unresolved ?? 0,
      // Cleared when nothing is broken any more. Carrying them forward printed
      // "All 12 references resolve." above a row of chips naming broken codes,
      // on the success path — the one a reader is most likely to see.
      dangling: stillBroken ? summary.value?.dangling ?? [] : [],
      dangling_total: stillBroken ? summary.value?.dangling_total : 0,
    }
    rebuiltAt.value = new Date().toISOString()
    readError.value = false
    announcement.value = note.value
    // Only to refresh which codes are still dangling. The counts above are
    // already right, and a failure here must not erase the result of a rebuild
    // that succeeded — so the read error is shown beneath the note, not
    // instead of it.
    if (stillBroken) await load(true)
  } catch (e: any) {
    const status = e?.status ?? e?.response?.status
    if (status === 403 || status === 404) {
      // 404 as well as 403: losing the team outright answers not-found, and it
      // is at least as likely as a demotion. Retrying either forever is the
      // failure this branch exists to prevent.
      forbidden.value = 'You no longer have access to change this project. Reload the page.'
    } else if (status === 401) {
      forbidden.value = 'Your session expired. Sign in again.'
    } else {
      // The inline row carries the retry, so it is the whole refusal — a toast
      // beside it would be the same sentence twice, and error toasts here never
      // dismiss themselves.
      writeError.value = true
    }
  } finally {
    busy.value = false
  }
}

onMounted(() => load())
</script>

<style scoped>
.maintenance-label {
  display: block;
  font-size: var(--type-sm);
  color: var(--color-text);
}

.maintenance-note {
  margin: var(--space-1) 0 0;
  max-width: var(--measure-prose);
  font-size: var(--type-xs);
  color: var(--color-text-muted);
}
.maintenance-note:empty { display: none; }

.maintenance-codes {
  display: flex; flex-wrap: wrap; align-items: center;
  gap: var(--space-2);
  margin-top: var(--space-2);
}
/* A person can write anything between the brackets, including four hundred
   characters with no spaces. Held to one line and elided, the way `.tag-text`
   holds a tag — a chip that grows to twenty lines is not a chip. */
.maintenance-code {
  max-width: var(--tag-max);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.maintenance-more { font-size: var(--type-xs); color: var(--color-text-muted); }

/* Stands in for the note, which is one line of --type-xs — not for a control.
   Full width, because the row's first child sizes to its content and an empty
   div contributes nothing, so this was a 105px stub in front of a 544px
   sentence. */
.maintenance-skeleton {
  width: 100%;
  max-width: var(--measure-prose);
  height: calc(var(--type-xs) * var(--line-base));
  margin-top: var(--space-1);
}

.maintenance-action { flex-shrink: 0; }

.maintenance-error { margin: var(--space-3) var(--row-inset, var(--space-5)) var(--space-4); }

@media (max-width: 768px) {
  .maintenance-action { width: 100%; }
}
</style>
