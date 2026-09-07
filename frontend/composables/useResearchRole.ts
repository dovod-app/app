import type { TeamRole } from './useTeams'

/**
 * What the reader may do to the research currently on screen.
 *
 * Every research-scoped page already awaits `GET /api/researches/{id}` before
 * it renders anything, and that payload carries the role — so a page calls
 * `setFromResearch()` with what it already has, and every component beneath it,
 * however deep, asks `useResearchRole()`. No extra request, no moment where the
 * page shows edit buttons and then takes them away.
 *
 * Module state rather than provide/inject: the components that need this most
 * are the ones hardest to reach with a provider — `BlockRenderer`'s content
 * checkboxes, a Teleported modal, a Vue Flow node popover. A missed `inject`
 * fails silently by returning its default, and the safe default here would have
 * to be "cannot write", which would break the local single-user mode instead.
 * There is exactly one research on screen at a time, so one value is honest.
 */
const role = ref<TeamRole | null>(null)
const researchId = ref<string | null>(null)
const teamName = ref<string>('')

/**
 * A share link is being read, and nothing on the page may be written.
 *
 * This exists because `canWrite` returns true when auth is disabled — correct
 * for the local single-binary mode, and catastrophic on a public share URL
 * served by the same binary, where it would render Edit, Delete, the status
 * dropdown and the block checkboxes to a stranger.
 *
 * It is the second of two defences. The first is that the share pages never
 * import the edit machinery at all; this one protects the components they *do*
 * reuse — BlockRenderer's checkboxes, RoadmapNodePopover's status chips,
 * KanbanBoard's drag.
 */
const shareLocked = ref(false)

export function useResearchRole() {
  const { authEnabled } = useAuth()
  const { writeApi } = useServerInfo()

  function setFromResearch(research: { id?: string; role?: TeamRole | ''; team_name?: string } | null) {
    if (!research) return clear()
    researchId.value = research.id ?? null
    role.value = (research.role || null) as TeamRole | null
    teamName.value = research.team_name ?? ''
    // An owner navigating out of a shared view and back into the app publishes
    // their real role here, which is the moment the lock should lift. Clearing
    // it on unmount instead would race the incoming page, for the reason
    // `clear()` documents.
    shareLocked.value = false
  }

  /** Called by the shared-view shell, before its fetch resolves. */
  function lockForShare() {
    shareLocked.value = true
    role.value = 'viewer' as TeamRole
  }

  /**
   * Forgets the current research.
   *
   * Pages must **not** call this on unmount. Every research page awaits its
   * fetch, which makes it a Suspense boundary, and Vue resolves a navigation
   * by running the incoming setup *before* unmounting the outgoing one — so an
   * unmount clear lands after the next page has already published its role and
   * leaves the reader with no role at all: no edit controls, and no read-only
   * badge to explain them. It is kept for a caller that genuinely leaves the
   * research behind, and for tests.
   */
  function clear() {
    role.value = null
    researchId.value = null
    teamName.value = ''
  }

  // With auth off there are no roles, so the question is not "what may this
  // person do" but "will this server take a write from this browser at all".
  //
  // It used to be answered `true` unconditionally, which was right for the local
  // single-binary mode and wrong everywhere else it applied: a server reached
  // across a network with no api_token now refuses those writes, and a server
  // with an api_token always did — the browser never holds one. Both rendered a
  // full set of edit controls that returned 401. `write_api` on /api/health is
  // the server answering it per caller.
  const canWrite = computed(() => {
    if (shareLocked.value) return false
    if (!authEnabled.value) return writeApi.value
    return role.value === 'editor' || role.value === 'owner'
  })

  const canAdmin = computed(() => {
    if (shareLocked.value) return false
    if (!authEnabled.value) return writeApi.value
    return role.value === 'owner'
  })

  return {
    role: readonly(role),
    teamName: readonly(teamName),
    researchId: readonly(researchId),
    canWrite,
    canAdmin,
    /**
     * True only for a real viewer — drives the one badge that explains the
     * missing controls. A share visitor is deliberately excluded: the banner
     * across the top is the right explanation, and "your role in this team is
     * viewer" would be a confusing thing to say to somebody with no account.
     */
    isViewer: computed(() => !shareLocked.value && !!authEnabled.value && role.value === 'viewer'),
    /**
     * Why the edit controls are missing, or null when they are not.
     *
     * `isViewer` above cannot answer this on its own: it requires
     * `authEnabled`, and the second reason exists precisely where there are no
     * accounts. Without this the eight pages that explain a viewer's missing
     * controls explained nothing at all to a reader on a server that simply
     * does not take writes from their machine — every control gone, and a 24px
     * chip in the corner of the nav the only clue.
     */
    readOnlyReason: computed<'viewer' | 'remote' | null>(() => {
      if (shareLocked.value) return null // the banner across the top already says it
      if (!authEnabled.value) return writeApi.value ? null : 'remote'
      return role.value === 'viewer' ? 'viewer' : null
    }),
    setFromResearch,
    lockForShare,
    clear,
  }
}
