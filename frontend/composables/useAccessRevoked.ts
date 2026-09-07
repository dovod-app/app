export interface Revocation {
  scope: 'research' | 'team'
  /** The entity's id, and its short code when it has one. */
  id: string
  code?: string
  name: string
  reason: string
}

/**
 * Handles being told, mid-session, that something on your screen is no longer
 * yours to read.
 *
 * Losing access used to be silent by construction: the events that announce it
 * are scoped to the people who still have it, so the one person who needed to
 * know was the only one who could not be told. Their tab went on showing a
 * research that would 404 the moment they touched it. The server now addresses
 * this one event to them directly.
 *
 * Nothing here redirects. Being moved off the page you were reading, without
 * warning, is a worse experience than the stale page it replaces — and if there
 * is an unsaved draft in the editor, it is destructive. The page says what
 * happened and offers the one way out.
 */

/**
 * What to say about a revocation, in one place.
 *
 * It was written twice — once here for the toast, once in the component for the
 * page notice — and the two disagreed: the toast was a two-branch ternary with
 * no default, so any reason it did not recognise (including an empty one, and
 * anything a newer server introduces) confidently asserted that a transfer had
 * happened. The surface with a story was right and the one without it was wrong,
 * which is the argument for there being one.
 */
export function revocationCopy(r: Revocation) {
  const what = r.code || r.name
  switch (r.reason) {
    case 'removed_from_team':
      return {
        title: `You no longer have access to ${what}`,
        short: `You were removed from the team ${r.name}. Its projects are no longer on your list.`,
        long: `Your access ended when you were removed from the team ${r.name}. Ask an owner of that team to invite you again.`,
      }
    case 'research_transferred':
      return {
        title: `You no longer have access to ${what}`,
        short: `“${r.name}” was moved to a team you are not a member of.`,
        long: `“${r.name}” was moved to a team you are not a member of. Ask an owner of that team to invite you.`,
      }
    case 'research_deleted':
      // Not "you no longer have access": nobody does, and telling a reader
      // their access ended sends them to ask for it back from someone who
      // cannot give it. What ended is the project.
      return {
        title: `“${r.name}” has been deleted`,
        short: `“${r.name}” was deleted, along with everything in it.`,
        long: `“${r.name}” was deleted by an owner, along with everything filed under it. This cannot be undone.`,
      }
    case 'team_deleted':
      return {
        title: `The team ${r.name} no longer exists`,
        short: `The team ${r.name} was deleted.`,
        long: `The team ${r.name} was deleted, so there is nothing here to show any more.`,
      }
    default:
      return {
        title: `You no longer have access to ${what}`,
        short: `Your access to ${r.name} ended.`,
        long: `You no longer have access to ${r.name}.`,
      }
  }
}

const active = ref<Revocation | null>(null)

export function useAccessRevoked() {
  if (import.meta.server) return { active: readonly(active) }

  const route = useRoute()
  const toasts = useToasts()
  const { authFetch } = useAuth()
  const base = useRuntimeConfig().public.apiBase || ''

  function onThisPage(r: Revocation) {
    const param = route.params.id
    if (typeof param !== 'string') return false
    // The same identity rule the event subscriptions use. Written out a second
    // time it drifted immediately: this copy did not accept the research's
    // UUID, so a revocation for a research reached by short code, on an event
    // whose code came back empty, landed as a toast on the very page it was
    // meant to replace.
    if (r.scope === 'research') return matchesResearch({ research_id: r.id, research_code: r.code }, param)
    return route.path.startsWith('/teams/') && param === r.id
  }

  /**
   * Re-reads what the signed-in person may do, after somebody changed it.
   *
   * This has to happen in one place rather than in each page's event handler.
   * The event is a directed team event with no research on it — the server
   * cannot scope it, because it does not know which page is open — so every
   * page filter written around `entity` drops it, and a demoted owner keeps
   * Edit and Delete on screen until they press one and collect a 403. Doing it
   * here reaches all seven research pages, including the entry page, which
   * subscribes directly and never sees a research-scoped event at all.
   */
  async function republishRole() {
    useTeams().load(true)
    const param = route.params.id
    if (typeof param !== 'string' || !route.path.startsWith('/research/')) return
    try {
      const res = await authFetch<any>(`${base}/api/researches/${param}`)
      useResearchRole().setFromResearch(res?.data?.research ?? null)
    } catch {
      // A 404 here means the role did not change, it vanished — which arrives
      // as its own directed access.revoked, handled below.
    }
  }

  /**
   * Replace the page — unless doing so would throw away work.
   *
   * The server has already decided the next save will fail. That is not a
   * reason for the client to discard the text: the reader can still select it,
   * copy it, and paste it somewhere that will keep it. So a dirty editor keeps
   * the page and gets told instead.
   */
  function show(r: Revocation) {
    if (hasUnsavedWork()) {
      toasts.push({
        variant: 'error',
        title: 'You no longer have access',
        message: `${revocationCopy(r).short} Your unsaved changes are still on screen, but they can no longer be saved here — copy anything you need before leaving this page.`,
        timeout: 0,
      })
      return
    }
    active.value = r
  }

  /** Can the research on screen still be read? If not, the page must say so. */
  async function confirmStillReadable(r: Revocation) {
    const param = route.params.id
    if (typeof param !== 'string') return
    try {
      await authFetch(`${base}/api/researches/${param}`)
    } catch {
      show({ ...r, scope: 'research' })
    }
  }

  useRealtimeUpdates((event) => {
    // A share visitor has no account to lose access to, and every branch below
    // reaches for an authenticated endpoint. The check is here rather than in
    // setup because app.vue's setup runs once at start-up, before a shared page
    // has claimed its token — a guard up there is always false on a cold load.
    if (shareActive()) return

    if (event.type === 'access.changed') {
      republishRole()
      return
    }
    // A deleted project is the same situation as a revoked one from the
    // reader's side — the page they are on has nothing behind it any more —
    // and it reuses this notice rather than growing a second one that would
    // have to re-learn the unsaved-work handling. It is routed here, and not
    // emitted as access.revoked by the server, because the copy differs: their
    // access did not end, the project did.
    if (event.type === 'research.deleted') {
      // Not at the person who pressed Delete. The event is emitted before the
      // response and directed at every member including the actor, so without
      // this the deleting tab could paint "was deleted by an owner" at itself
      // for a frame — or, with an editor open on the page, raise the sticky
      // no-timeout toast that follows them to the list and never dismisses.
      if (isSelf(event)) return
      const deleted: Revocation = {
        scope: 'research',
        id: event.entity_id,
        code: event.research_code,
        name: event.name || 'this project',
        reason: 'research_deleted',
      }
      if (onThisPage(deleted)) show(deleted)
      return
    }
    if (event.type !== 'access.revoked') return

    const revocation: Revocation = {
      scope: event.entity === 'team' ? 'team' : 'research',
      id: event.entity_id,
      code: event.research_code,
      name: event.name || 'this workspace',
      reason: event.reason || '',
    }

    // Whatever else happens, the list of teams on file is now wrong.
    useTeams().load(true)

    if (onThisPage(revocation)) {
      show(revocation)
      return
    }
    // A team-scoped revocation names a team, not a research, so it cannot say
    // whether the research on screen belonged to it. Asking the server is the
    // only way to find out — and it is the case that matters most, because
    // "remove the member" is how offboarding actually happens and a research
    // page is where they most likely are.
    if (revocation.scope === 'team' && route.path.startsWith('/research/')) {
      void confirmStillReadable(revocation)
    }
    // Elsewhere in the app, a card is about to stop working. Say so once,
    // rather than letting it fail under the next click.
    toasts.push({
      variant: 'info',
      title: 'Access removed',
      message: revocationCopy(revocation).short,
    })
  })

  // Navigating away is the reader accepting it; the notice has no business
  // outliving the page it was about.
  watch(() => route.fullPath, () => { active.value = null })

  return { active: readonly(active) }
}
