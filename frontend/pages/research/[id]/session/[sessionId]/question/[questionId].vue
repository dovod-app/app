<template>
  <div v-if="pending">
    <div class="skeleton-card skeleton-header"></div>
    <div class="skeleton-card skeleton-content"></div>
  </div>

  <div v-else-if="question">
    <!-- Header -->
    <div class="page-header">
      <Breadcrumbs :crumbs="[
        { label: 'Projects', to: { name: 'index' } },
        { label: researchName, to: `/research/${researchSlug}` },
        { label: sessionTitle, to: `/research/${researchSlug}/session/${sessionId}` },
        { label: `Q${questionIndex + 1}` }
      ]" />
      <div class="question-header">
        <h1 class="page-title" v-html="renderRefs(question.text, researchSlug)"></h1>
        <div class="question-badges">
          <StatusBadge :status="question.status" />
          <StatusBadge v-if="question.priority" :status="question.priority" />
          <!-- In the header rather than in the awaiting-answer empty state,
               which only renders for a pending question: an answered one is
               exactly the kind somebody wants gone, and there it would have had
               no control at all. A button rather than a `⋯`, because a menu
               holding one item is more work to reach than the item. -->
          <button
            v-if="canWrite"
            class="btn btn-sm btn-danger no-print"
            @click="confirmDelete = true"
          >Delete</button>
        </div>
      </div>
      <div class="question-meta">
        <span v-if="question.area" class="question-area">{{ question.area }}</span>
      </div>
    </div>

    <!-- Rationale -->
    <div v-if="question.rationale" class="card rationale-card">
      <h3 class="card-section-title">Rationale</h3>
      <div class="rationale-text markdown-content" v-html="linkRefs(parseMarkdownInline(normalizeContent(question.rationale)) as string, researchSlug)"></div>
    </div>

    <!-- Answer.
         The server has always accepted `PUT /api/questions/{id}` with a status
         and an answer, and nothing in the frontend called it: a reviewer could
         read a wrong answer and change nothing, while the session page offered
         a form to *add* a question — the half only the agent needs. -->
    <div v-if="question.answer || editing" class="card answer-card">
      <div class="answer-head">
        <h3 class="card-section-title">Answer</h3>
        <button v-if="canWrite && !editing" class="btn btn-sm" @click="startEditing">
          {{ question.answer ? 'Edit' : 'Answer' }}
        </button>
      </div>

      <form v-if="editing" class="answer-form" @submit.prevent="save">
        <label class="sr-only" :for="answerFieldId">Answer</label>
        <textarea
          :id="answerFieldId"
          ref="answerField"
          v-model="draft"
          class="form-textarea answer-input"
          rows="10"
          placeholder="Markdown, and [[E3]] references, render the same here as anywhere else."
        ></textarea>
        <div class="modal-actions">
          <button type="button" class="btn btn-sm" :disabled="saving" @click="cancel">Cancel</button>
          <button type="submit" class="btn btn-sm btn-primary" :disabled="saving || !draft.trim()">
            {{ saving ? 'Saving…' : 'Save answer' }}
          </button>
        </div>
      </form>

      <div v-else ref="answerEl" class="markdown-content" v-html="renderedAnswer"></div>
    </div>

    <EmptyState
      v-else-if="question.status === 'pending' || question.status === 'in_progress'"
      icon="&#x23F3;"
      title="Awaiting answer"
      description="The agent has not answered this yet."
    >
      <button v-if="canWrite" class="btn btn-sm btn-primary" @click="startEditing">Answer it yourself</button>
    </EmptyState>

    <!-- Status.
         Not a radiogroup: each of these is a one-shot write that can be
         refused, so `aria-pressed` states which is current without promising
         arrow-key selection the component does not implement. -->
    <div v-if="canWrite" class="card status-card no-print">
      <h3 class="card-section-title">Status</h3>
      <div class="segmented" role="group" aria-label="Question status">
        <button
          v-for="st in QUESTION_STATUSES"
          :key="st"
          type="button"
          :aria-pressed="question.status === st"
          :aria-disabled="saving || undefined"
          @click="setStatus(st)"
        >{{ QUESTION_STATUS_LABELS[st] }}</button>
      </div>
    </div>

    <!-- Cross-references from this answer -->
    <div v-if="hasRefs" class="crossrefs-block card no-print">
      <h3 class="crossrefs-title">
        <svg width="15" height="15" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M10 13a5 5 0 0 0 7.54.54l3-3a5 5 0 0 0-7.07-7.07l-1.72 1.71"/><path d="M14 11a5 5 0 0 0-7.54-.54l-3 3a5 5 0 0 0 7.07 7.07l1.71-1.71"/></svg>
        Cross-references
      </h3>
      <div class="crossrefs-list">
        <NuxtLink
          v-for="ref in outgoingRefs"
          :key="ref.target_entry_id || ref.target_ref"
          :to="refLink(ref)"
          class="crossref-item"
        >
          <span class="crossref-code">{{ ref.entry_code || ref.target_ref }}</span>
          <span v-if="ref.entry_title" class="crossref-entry-title">{{ ref.entry_title }}</span>
          <span v-if="!ref.resolved" class="crossref-unresolved">unresolved</span>
        </NuxtLink>
      </div>
    </div>

    <!-- Prev / Next question navigation -->
    <div v-if="allQuestions.length > 1" class="question-nav no-print">
      <NuxtLink v-if="prevQuestion" :to="`/research/${researchSlug}/session/${sessionId}/question/${prevQuestion.code || prevQuestion.id}`" class="btn btn-sm nav-btn">
        &larr; {{ truncate(prevQuestion.text, 50) }}
      </NuxtLink>
      <span v-else class="nav-placeholder"></span>
      <NuxtLink v-if="nextQuestion" :to="`/research/${researchSlug}/session/${sessionId}/question/${nextQuestion.code || nextQuestion.id}`" class="btn btn-sm nav-btn nav-next">
        {{ truncate(nextQuestion.text, 50) }} &rarr;
      </NuxtLink>
    </div>
  </div>

  <EmptyState v-else icon="&#x1F50D;" title="Question not found" />

  <ConfirmModal
    :visible="confirmDelete"
    variant="danger"
    title="Delete question"
    :message="deleteMessage"
    confirm-label="Delete"
    :loading="deleting"
    @confirm="deleteQuestion"
    @cancel="confirmDelete = false"
  />
</template>

<script setup lang="ts">
import { truncate } from '~/utils/truncate'
import { parseMarkdown, parseMarkdownInline } from '~/composables/useSafeMarkdown'
import { renderMermaidBlocks } from '~/composables/useMermaid'

const route = useRoute()
const id = route.params.id as string
const sessionId = route.params.sessionId as string
const questionId = route.params.questionId as string

// Research info
const { data: researchData } = await useApi<{ data: any }>(`/api/researches/${id}`)
const researchName = computed(() => researchData.value?.data?.research?.name ?? 'Project')
const researchSlug = computed(() => researchData.value?.data?.research?.code || id)

// Session + questions
const { data, pending } = await useApi<{ data: any }>(`/api/researches/${id}/sessions/${sessionId}`)
const sessionTitle = computed(() => data.value?.data?.session?.title ?? 'Session')

// Flatten all questions from grouped structure
const allQuestions = computed(() => {
  const qs = data.value?.data?.questions ?? {}
  const flat: any[] = []
  for (const status of ['pending', 'in_progress', 'answered', 'deferred', 'skipped']) {
    if (qs[status]) flat.push(...qs[status])
  }
  return flat
})

const question = computed(() =>
  allQuestions.value.find((q: any) => q.id === questionId || q.code === questionId) ?? null
)
/* Writing an answer.
   `PUT /api/questions/{id}` takes a status, an answer, or both, and has been
   reachable from the API since the endpoint was written. The id is the UUID,
   not the short code the URL may carry. */
const { canWrite, setFromResearch } = useResearchRole()
const { authFetch } = useAuth()
const rtBase = useRuntimeConfig().public.apiBase || ''

/* --- Deleting the question --- */
const confirmDelete = ref(false)
const deleting = ref(false)

/* "Its follow-up questions stay in the session" is true because
   questions.parent_id is ON DELETE SET NULL in all three dialects. Like the
   session copy next door, it is a promise about the database and has to change
   if that ever does. */
const deleteMessage = computed(() => {
  const q = question.value
  const label = q?.code || 'This question'
  const head = q?.answer
    ? `${label} and the answer recorded for it are deleted.`
    : `${label} is deleted.`
  const followUps = (q?.children?.length ?? 0) > 0 ? ' Its follow-up questions stay in the session.' : ''
  return `${head}${followUps} This cannot be undone.`
})

async function deleteQuestion() {
  deleting.value = true
  try {
    const label = question.value?.code || 'The question'
    await authFetch(`${rtBase}/api/questions/${question.value.id}`, { method: 'DELETE' })
    confirmDelete.value = false
    await navigateTo(`/research/${researchSlug.value}/session/${sessionId}`)
    useToasts().push({ variant: 'success', title: 'Question deleted', message: `${label} has been removed.` })
  } catch (e: any) {
    useToasts().error(e?.data?.error ?? 'The server refused it.', 'Could not delete question')
  } finally {
    deleting.value = false
  }
}
const answerFieldId = useId()

const QUESTION_STATUSES = ['pending', 'in_progress', 'answered', 'deferred', 'skipped'] as const
const QUESTION_STATUS_LABELS: Record<string, string> = {
  pending: 'Pending',
  in_progress: 'In Progress',
  answered: 'Answered',
  deferred: 'Deferred',
  skipped: 'Skipped',
}

const editing = ref(false)
const saving = ref(false)
const draft = ref('')
const answerField = ref<HTMLTextAreaElement | null>(null)

function startEditing() {
  draft.value = question.value?.answer ?? ''
  editing.value = true
  nextTick(() => answerField.value?.focus())
}

function cancel() {
  editing.value = false
  draft.value = ''
}

async function write(body: Record<string, unknown>) {
  const qid = question.value?.id
  if (!qid) return false
  saving.value = true
  try {
    await authFetch(`${rtBase}/api/questions/${qid}`, { method: 'PUT', body })
    data.value = await authFetch<any>(`${rtBase}/api/researches/${id}/sessions/${sessionId}`)
    return true
  } catch (e: any) {
    // The draft stays on screen: the server refusing it is not a reason for the
    // client to throw the text away on the reader's behalf.
    useToasts().push({
      variant: 'error',
      title: 'Could not save',
      message: e?.data?.error || e?.message || 'The server refused the change. Your text is still here.',
      timeout: 0,
    })
    return false
  } finally {
    saving.value = false
  }
}

async function save() {
  // Answering moves the question along; a reviewer correcting an existing
  // answer is not re-answering it, so the status is left where it is.
  const body: Record<string, unknown> = { answer: draft.value }
  if (question.value?.status !== 'answered') body.status = 'answered'
  if (await write(body)) editing.value = false
}

async function setStatus(status: string) {
  if (saving.value || question.value?.status === status) return
  await write({ status })
}

watch(() => researchData.value?.data?.research, (r) => setFromResearch(r ?? null), { immediate: true })

const questionIndex = computed(() =>
  allQuestions.value.findIndex((q: any) => q.id === questionId || q.code === questionId)
)

// Prev/next
const prevQuestion = computed(() => questionIndex.value > 0 ? allQuestions.value[questionIndex.value - 1] : null)
const nextQuestion = computed(() => questionIndex.value < allQuestions.value.length - 1 ? allQuestions.value[questionIndex.value + 1] : null)

// Rendered answer
const renderedAnswer = computed(() => {
  if (!question.value?.answer) return ''
  const html = parseMarkdown(normalizeContent(question.value.answer)) as string
  return linkRefs(html, researchSlug.value)
})

// Mermaid rendering
const answerEl = ref<HTMLElement | null>(null)
watch(renderedAnswer, () => {
  nextTick(() => {
    if (answerEl.value) renderMermaidBlocks(answerEl.value)
  })
})
onMounted(() => {
  if (answerEl.value) renderMermaidBlocks(answerEl.value)
})

// Cross-references from this question's answer
const { data: refsData } = useApi<{ outgoing: any[]; incoming: any[] }>(
  computed(() => question.value ? `/api/entries/${question.value.id}/crossrefs` : `/api/entries/__none__/crossrefs`)
)
const outgoingRefs = computed(() => refsData.value?.outgoing ?? [])
const hasRefs = computed(() => outgoingRefs.value.length > 0)

function refLink(ref: any): string {
  const rCode = ref.research_code || researchSlug.value
  const eCode = ref.entry_code || ref.target_ref
  return `/research/${rCode}/entry/${eCode}`
}

</script>

<style scoped>
.answer-head { display: flex; align-items: center; justify-content: space-between; gap: var(--space-3); margin-bottom: var(--space-3); }
.answer-head .card-section-title { margin-bottom: 0; }
.answer-form { display: flex; flex-direction: column; gap: var(--space-3); }
.answer-input { min-height: 12rem; resize: vertical; font-family: 'JetBrains Mono', monospace; font-size: var(--type-sm); }
.status-card { margin-top: var(--space-4); }
.status-card .segmented { flex-wrap: wrap; }

.question-header {
  display: flex;
  justify-content: space-between;
  align-items: flex-start;
  gap: var(--space-4);
}
.question-badges {
  display: flex;
  gap: var(--space-2);
  flex-shrink: 0;
}
.question-meta {
  display: flex;
  gap: var(--space-3);
  margin-top: var(--space-2);
}
.question-area {
  font-size: var(--type-xs);
  color: var(--color-text-muted);
  background: var(--color-surface);
  padding: 0.15rem 0.5rem;
  border-radius: var(--radius-xs);
  border: 1px solid var(--color-border);
}
.rationale-card {
  margin-bottom: var(--space-4);
}
.rationale-text {
  color: var(--color-text-muted);
  font-size: var(--type-sm);
  line-height: 1.6;
}
.answer-card {
  padding: var(--space-8);
  border-radius: var(--radius-lg);
}

/* Cross-references (reuse entry page patterns) */
.crossrefs-block {
  margin-top: var(--space-6);
  padding: var(--space-6);
  border-radius: var(--radius-lg);
}
.crossrefs-title {
  display: flex;
  align-items: center;
  gap: var(--space-2);
  font-size: var(--type-sm);
  font-weight: var(--weight-semibold);
  margin: 0 0 var(--space-4) 0;
}
.crossrefs-list {
  display: flex;
  flex-direction: column;
  gap: var(--space-1);
}
.crossref-item {
  display: flex;
  align-items: center;
  gap: var(--space-2);
  padding: var(--space-2) var(--space-3);
  border-radius: var(--radius-sm);
  text-decoration: none;
  color: var(--color-text);
  transition: background 0.15s;
}
.crossref-item:hover { background: var(--color-surface-hover); }
.crossref-code {
  font-family: 'JetBrains Mono', monospace;
  font-size: var(--type-xs);
  font-weight: var(--weight-semibold);
  color: var(--color-primary);
  background: var(--color-primary-muted);
  padding: 0.15rem 0.4rem;
  border-radius: var(--radius-xs);
  flex-shrink: 0;
}
.crossref-entry-title {
  font-size: var(--type-sm);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.crossref-unresolved {
  font-size: var(--type-xs);
  color: var(--color-warning, var(--color-warning));
  font-style: italic;
  margin-left: auto;
}

/* Navigation */
.question-nav {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-top: var(--space-8);
  padding-top: var(--space-6);
  border-top: 1px solid var(--color-border);
  gap: var(--space-4);
}
.nav-btn {
  max-width: 45%;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.nav-next { margin-left: auto; text-align: right; }
.nav-placeholder { flex: 1; }

/* Print */
@media print {
  .question-badges { display: none; }
  .answer-card { padding: 0; border: none; }
  .rationale-card { padding: 0; border: none; }
  .question-area { background: none; border: none; padding: 0; }
}
</style>
