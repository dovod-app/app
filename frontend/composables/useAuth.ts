interface User {
  id: string
  email: string
  name: string
}

interface AuthInfo {
  auth_enabled: boolean
  allow_registration: boolean
  auto_login_token?: string
}

const user = ref<User | null>(null)
const token = ref<string | null>(null)
const authEnabled = ref<boolean | null>(null)
const allowRegistration = ref(true)
const loading = ref(true)

export function useAuth() {
  const config = useRuntimeConfig()
  const baseURL = config.public.apiBase || ''

  async function fetchAuthInfo() {
    try {
      const res = await $fetch<AuthInfo>(`${baseURL}/api/auth/info`)

      // Read the answer, not the fact that something answered.
      //
      // This route used to exist only when auth was on. With auth off the
      // request fell through to the SPA catch-all, which returned index.html
      // with a 200 — so `$fetch` did not throw, this set authEnabled to true,
      // and the route guard sent every page to a login screen that could not be
      // used because /api/auth/login did not exist either. The whole web UI was
      // unreachable in the mode the product documents as its default.
      //
      // The route now always answers and says which mode it is in. Checking the
      // shape as well as the value keeps this honest if it ever stops.
      authEnabled.value = typeof res === 'object' && res !== null && res.auth_enabled === true
      // Only a server that answered "accounts are on, registration is closed"
      // closes the door. A failure below must not: `false` there would be a
      // guess, and register.vue redirects away on it while login.vue hides the
      // link back — so one flaky request would strand somebody on a sign-in
      // page with no way to make an account, for the life of the tab.
      allowRegistration.value = authEnabled.value ? res.allow_registration === true : false

      // Auto-login when default_user is configured (local dev). The server
      // sends this only to a caller on its own machine.
      if (authEnabled.value && res.auto_login_token && !localStorage.getItem('auth_token')) {
        token.value = res.auto_login_token
        localStorage.setItem('auth_token', res.auto_login_token)
      }
    } catch {
      // The route did not answer at all. Treat it as "no accounts" — that is
      // what a server without them looks like from here — but leave
      // allowRegistration at its default, because this branch knows nothing
      // about registration and the cost of guessing wrong is a locked door.
      authEnabled.value = false
    }
  }

  async function checkAuth() {
    loading.value = true
    const stored = localStorage.getItem('auth_token')
    if (!stored) {
      loading.value = false
      return
    }
    token.value = stored
    try {
      const res = await $fetch<User>(`${baseURL}/api/auth/me`, {
        headers: { Authorization: `Bearer ${stored}` },
      })
      user.value = res
    } catch {
      token.value = null
      localStorage.removeItem('auth_token')
    }
    loading.value = false
  }

  async function login(email: string, password: string) {
    const res = await $fetch<{ user: User; token: string }>(`${baseURL}/api/auth/login`, {
      method: 'POST',
      body: { email, password },
    })
    user.value = res.user
    token.value = res.token
    localStorage.setItem('auth_token', res.token)
  }

  /**
   * Creates an account. `inviteToken` lets someone who was handed a link
   * register on a server with registration closed, and joins them to the team
   * in the same request — the invitation is the authorization.
   */
  async function register(email: string, password: string, name: string, inviteToken?: string) {
    const res = await $fetch<{ user: User; token: string }>(`${baseURL}/api/auth/register`, {
      method: 'POST',
      body: { email, password, name, invite_token: inviteToken },
    })
    user.value = res.user
    token.value = res.token
    localStorage.setItem('auth_token', res.token)
  }

  function logout(next = '/login') {
    user.value = null
    token.value = null
    localStorage.removeItem('auth_token')
    // Module-scoped caches survive a client-side route change, so anything
    // keyed to the person has to be dropped here by name. Without this the
    // next account in the same tab renders the previous one's team names,
    // member counts and filter options.
    useTeams().reset()
    navigateTo(next)
  }

  // Authenticated $fetch wrapper for use outside useApi composable
  function authFetch<T>(url: string, opts: Record<string, any> = {}) {
    const headers: Record<string, string> = { ...(opts.headers || {}) }
    if (token.value) {
      headers['Authorization'] = `Bearer ${token.value}`
    }
    // Which tab is writing. The event that comes back carries it, which is how
    // this tab knows not to refetch a change already on its screen.
    if (!import.meta.server) {
      headers['X-Client-Id'] = realtimeClientId()
    }
    return $fetch<T>(url, { ...opts, headers })
  }

  return {
    user: readonly(user),
    token: readonly(token),
    authEnabled: readonly(authEnabled),
    allowRegistration: readonly(allowRegistration),
    loading: readonly(loading),
    isAuthenticated: computed(() => !!user.value),
    fetchAuthInfo,
    checkAuth,
    login,
    register,
    logout,
    authFetch,
  }
}
