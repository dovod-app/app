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

  async function loadSummary(idOrCode: string) {
    loading.value = true
    summaryFailed.value = false
    try {
      const res = await authFetch<{ data: DeletionSummary }>(
        `${base}/api/researches/${idOrCode}/delete-preview`,
      )
      summary.value = res.data
    } catch {
      // Not surfaced as a banner: a failed count is not a reason to interrupt
      // somebody, and the dialog says so in its own body.
      summary.value = null
      summaryFailed.value = true
    } finally {
      loading.value = false
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
