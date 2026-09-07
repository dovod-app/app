export default defineNuxtRouteMiddleware(async (to) => {
  const { authEnabled, isAuthenticated, fetchAuthInfo, checkAuth, loading } = useAuth()

  // Both of these run before the app mounts, so neither is in flight yet when
  // this guard first runs. Start the health request without awaiting it, so it
  // overlaps the auth-info request instead of queueing behind it — otherwise a
  // cold load pays two serial round trips on a blank page, which is a real
  // second of nothing on the remote deployments this gating exists for.
  const serverInfo = useServerInfo()
  const health = serverInfo.load()

  // Fetch auth info once
  if (authEnabled.value === null) {
    await fetchAuthInfo()
  }

  // Not an auth-enabled server — skip all checks.
  //
  // One thing still has to be known before the page renders: whether this
  // browser may write. With no accounts the server decides that from where the
  // request came from, or from an api_token the browser does not hold, and the
  // page has no way to know which. Awaiting it here is what stops `canWrite`
  // starting false and flipping true under the reader. It resolves once per
  // tab, failure included.
  if (!authEnabled.value) {
    await health
    return
  }

  // Check token validity if we haven't yet
  if (loading.value) {
    await checkAuth()
  }

  // An invitation is read before there is an account to read it with, so this
  // page renders either way: signed in it offers Join, signed out it offers a
  // way to get an account. It asks for a session only when Join is pressed.
  if (to.path.startsWith('/invite/')) {
    return
  }

  // A share link is read by somebody with no account, and the token in the URL
  // is the whole credential. Sending them to /login would be exactly the wall
  // this feature exists to remove.
  if (to.path.startsWith('/s/')) {
    return
  }

  // The API reference renders a document this server already hands to anyone
  // who asks for it — `/api/openapi.yaml` and `/api/openapi.json` are public
  // routes, and llms.txt publishes the URL. Requiring a session to read what is
  // already public only stops the people who would have read it politely, and
  // the integrator who has not signed up yet is exactly who the page is for.
  //
  // If the route inventory should be private, the fix is the access kind on
  // those two routes in internal/api/server.go — this page will then show its
  // fetch-failure state, which is correct and not a regression.
  if (isApiDocsPath(to.path)) {
    return
  }

  if (to.path === '/login' || to.path === '/register') {
    // Already signed in: go where they were headed, or home.
    if (isAuthenticated.value) {
      return navigateTo(safeNext(to.query.next) ?? useRouter().resolve({ name: 'index' }).path)
    }
    return
  }

  // Protected page — must be authenticated. The destination rides along so
  // signing in returns here instead of dropping the reader on the research
  // list, which is how an invitation link gets lost between two clicks.
  if (!isAuthenticated.value) {
    return navigateTo(`/login?next=${encodeURIComponent(to.fullPath)}`)
  }
})

