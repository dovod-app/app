export interface ServerInfo {
  status: string
  version: string
  in_memory: boolean
  write_api: boolean
  auth_enabled: boolean
}

/**
 * What the server says about itself.
 *
 * Module state and one request for the life of the tab: `/api/health` is
 * already fetched by the in-memory warning banner, and the footer wanting the
 * version is not a reason to ask twice. It is deliberately not reactive to
 * anything — a running server's version does not change, and if it did, the
 * page it is printed on came from the old one.
 */
const info = ref<ServerInfo | null>(null)
let inFlight: Promise<void> | null = null
// Asked and answered, even if the answer was a failure. Without this the route
// guard's `await load()` reissues the request on every navigation once the
// server is unreachable — and awaits it, so each click stalls for a connect
// timeout on a page that has nothing new to show, exactly when the app is
// already degraded.
let settled = false

export function useServerInfo() {
  const base = useRuntimeConfig().public.apiBase || ''

  function load() {
    if (import.meta.server || settled) return Promise.resolve()
    if (inFlight) return inFlight
    // No credential: /api/health is public, and the footer must render on a
    // page where nobody is signed in.
    //
    // This is no longer only the footer's version string: `write_api` decides
    // whether the UI renders edit controls at all. A failure therefore means
    // "no writes" — the safe direction, since the alternative is a button that
    // returns 401 — and the read-only notice is what explains it on the page.
    inFlight = $fetch<ServerInfo>(`${base}/api/health`, { signal: AbortSignal.timeout(5000) })
      .then((res) => { info.value = res })
      .catch(() => { /* left null: no version, and no writes */ })
      .finally(() => { settled = true; inFlight = null })
    return inFlight
  }

  if (!import.meta.server && !settled) void load()

  return {
    info: readonly(info),
    /**
     * Whether *this browser* may write.
     *
     * The server answers it per caller, because with no `api_token` and no
     * accounts the answer genuinely differs: a page served to the machine the
     * server runs on may write, one served across a network may not. It is the
     * only way the UI can tell those apart — the browser holds no api_token in
     * either case — and rendering Edit to the second is rendering a button that
     * returns 401.
     *
     * False until `/api/health` has answered. The route guard awaits `load()`
     * before any page renders in that mode, so this is not a flicker.
     */
    writeApi: computed(() => info.value?.write_api === true),
    /** e.g. `v1.4.0`, or `dev` for a local build. Empty until it arrives. */
    version: computed(() => {
      const v = info.value?.version
      if (!v) return ''
      return /^\d/.test(v) ? `v${v}` : v
    }),
    load,
  }
}
