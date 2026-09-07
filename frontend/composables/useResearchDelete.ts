export interface CitingResearch {
  code: string
  name: string
}

export interface DeletionSummary {
  sections: number
  entries: number
  sessions: number
  questions: number
  tasks: number
  roadmaps: number
  annotations: number
  shares: number
  incoming_refs: number
  incoming_from: CitingResearch[] | null
  /** How many cite it in total, before the cap on the list above. */
  incoming_from_total?: number
}

function plural(n: number, one: string, many: string) {
  return `${n} ${n === 1 ? one : many}`
}

/**
 * What a deletion would destroy, as the list the dialog renders.
 *
 * One entity table, used by both surfaces. They were written twice and had
 * already drifted: the danger-zone sentence counted neither roadmaps nor marks,
 * so a project holding two roadmaps and five marks read "Nothing has been filed
 * under this project yet" in the row and listed two items in the dialog one
 * click later.
 *
 * Sessions and questions share a line because a session with no questions is
 * not a separate idea. Zero terms are omitted throughout — a note that reads
 * "0 sections, 0 documents" invites the reader past the one number that is not
 * zero.
 */
export function deletionLines(s: DeletionSummary | null): string[] {
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
}

/** The same list as one sentence, for a row that has no room for a list. */
export function deletionSentence(s: DeletionSummary | null): string {
  const parts = deletionLines(s)
  if (!parts.length) return 'Nothing has been filed under this project yet. This cannot be undone.'
  const last = parts.pop()
  const list = parts.length ? `${parts.join(', ')} and ${last}` : last
  return `Removes ${list}. This cannot be undone.`
}

/**
 * What deleting this would do outside the project — the part that changes a
 * decision, and the reason it is worded separately from the list above.
 */
export function deletionConsequences(s: DeletionSummary | null): string {
  if (!s) return ''
  const parts: string[] = []
  if (s.shares) {
    parts.push(`${plural(s.shares, 'share link', 'share links')} stop${s.shares === 1 ? 's' : ''} working.`)
  }
  if (s.incoming_refs) {
    const from = s.incoming_from ?? []
    // The total, not the length of the list: the list is capped at ten, so
    // counting it said "and 9 others" when eighty projects cited this one — in
    // the one sentence written to change a decision.
    const total = s.incoming_from_total ?? from.length
    let where = ''
    if (total === 1 && from.length) where = ` from ${from[0]!.code}`
    else if (total > 1 && from.length) {
      const others = total - 1
      where = ` from ${from[0]!.code} and ${others} other${others === 1 ? '' : 's'}`
    }
    parts.push(`${plural(s.incoming_refs, 'reference', 'references')}${where} stop resolving.`)
  }
  return parts.join(' ')
}

/**
 * Turns a failed delete into a sentence, naming no noun.
 *
 * The caller supplies the noun in its own toast title — the rule `useDownload`
 * already states and the reason its messages say "the file" nowhere. Baking
 * "project" in here is what made the first version of this unusable by the
 * section, session and question deletes, which then each grew their own
 * `catch` and none of them implemented the two rules that matter.
 */
export function describeDeleteFailure(status: number | undefined): string {
  switch (status) {
    case 403:
      return 'You are no longer allowed to delete this.'
    case 409:
      return 'The server refused it.'
    case undefined:
      return 'No answer from the server. It may or may not have been deleted.'
    default:
      // The cascade runs in one transaction, so a 5xx means it rolled back.
      // This sentence is a promise about the database; if that ever stops being
      // true, it has to change with it.
      return 'Nothing was deleted, and nothing was changed — try again.'
  }
}

/**
 * The outcome of any delete in this product.
 *
 * `already-gone` is a success: somebody else got there first and the caller's
 * intent is satisfied. It is the branch every hand-written `catch` gets wrong,
 * which is why it is a named outcome rather than a status code.
 */
export type DeleteOutcome = 'deleted' | 'already-gone' | 'failed'

/**
 * Runs one DELETE and classifies the result, for the deletes that need no
 * preview and no typed code — a section, a session, a question, a document.
 *
 * `noun` is lower case and singular ("section"), used in the failure toast's
 * title. Every one of these was written by hand first, and every one of them
 * got the same two things wrong: a 404 reported as an error to somebody whose
 * intent had already been satisfied, and a 403 offered as retryable.
 */
export async function deleteEntity(
  authFetch: (url: string, opts?: any) => Promise<any>,
  url: string,
  noun: string,
  toasts: { push: (t: any) => void; error: (m: string, t?: string) => void },
  name?: string,
): Promise<DeleteOutcome> {
  const label = name ? `“${name}”` : `The ${noun}`
  try {
    await authFetch(url, { method: 'DELETE' })
    toasts.push({
      variant: 'success',
      title: `${noun[0]!.toUpperCase()}${noun.slice(1)} deleted`,
      message: `${label} has been removed.`,
    })
    return 'deleted'
  } catch (e: any) {
    const status = e?.statusCode ?? e?.response?.status
    if (status === 404) {
      // Neutral on purpose. The service answers 404 for two situations — the
      // thing is gone, and the caller is no longer a member — because
      // confirming existence to a non-member is itself information. Saying "had
      // already been removed" would state as fact that data was destroyed in
      // the second case, when it is alive and the rest of the team is working
      // in it.
      toasts.push({ variant: 'info', title: 'No longer available', message: `${label} is no longer available here.` })
      return 'already-gone'
    }
    const server = e?.data?.error
    toasts.error(server ?? describeDeleteFailure(status), `Could not delete ${noun}`)
    return 'failed'
  }
}

/**
 * Deleting a project: the preview, the call, and the sentence to show when it
 * fails.
 *
 * The failure mapping lives here rather than at each call site because the two
 * statuses that are not errors are the ones a call site gets wrong. A 404 means
 * somebody else deleted it first, which satisfies the user's intent and must
 * read as success. A 403 means the delete can never succeed from this browser,
 * so retrying is the one thing not to offer.
 */
export function useResearchDelete() {
  const { authFetch } = useAuth()
  const config = useRuntimeConfig()
  const base = config.public.apiBase || ''

  const summary = ref<DeletionSummary | null>(null)
  const loading = ref(false)
  const summaryFailed = ref(false)
  const busy = ref(false)
  const failure = ref<string | null>(null)
  const permanentlyRefused = ref(false)

  // Sequence number, because the dialog can be closed and reopened for another
  // project while nine COUNT(*)s are in flight: without it the first response
  // lands second and fills the second dialog with the first project's counts,
  // in the one dialog whose entire purpose is to be accurate.
  let request = 0

  async function loadSummary(idOrCode: string) {
    const mine = ++request
    loading.value = true
    summaryFailed.value = false
    try {
      const res = await authFetch<{ data: DeletionSummary }>(
        `${base}/api/researches/${idOrCode}/delete-preview`,
      )
      if (mine !== request) return
      summary.value = res.data
    } catch {
      // Not surfaced as a banner: a failed count is not a reason to interrupt
      // somebody, and the dialog says so in its own body.
      if (mine !== request) return
      summary.value = null
      summaryFailed.value = true
    } finally {
      if (mine === request) loading.value = false
    }
  }

  /**
   * Returns 'deleted' | 'already-gone' | 'failed'. The caller navigates and
   * toasts; both of the first two are outcomes the user asked for.
   */
  async function remove(idOrCode: string): Promise<'deleted' | 'already-gone' | 'failed'> {
    busy.value = true
    failure.value = null
    try {
      await authFetch(`${base}/api/researches/${idOrCode}`, { method: 'DELETE' })
      return 'deleted'
    } catch (e: any) {
      const status = e?.statusCode ?? e?.response?.status
      if (status === 404) return 'already-gone'
      if (status === 403) {
        permanentlyRefused.value = true
        failure.value = 'You are no longer an owner of this project, so it cannot be deleted here.'
        return 'failed'
      }
      if (status === 409) {
        failure.value = `The server refused: ${e?.data?.error ?? 'it could not be deleted.'}`
        return 'failed'
      }
      if (status === undefined) {
        failure.value = 'No answer from the server. The project may or may not have been deleted.'
        return 'failed'
      }
      // The service runs the whole cascade in one transaction, so a 5xx means
      // it rolled back. This sentence is a promise about the database; if the
      // delete ever stops being atomic, it has to change with it.
      failure.value = 'The project was not deleted. Nothing was changed — try again.'
      return 'failed'
    } finally {
      busy.value = false
    }
  }

  function reset() {
    summary.value = null
    summaryFailed.value = false
    failure.value = null
    permanentlyRefused.value = false
  }

  return { summary, loading, summaryFailed, busy, failure, permanentlyRefused, loadSummary, remove, reset }
}
