import { ref } from 'vue'

/**
 * The two answers a component asks about *this browser* rather than about the
 * record it was handed: whether the server has accounts, and whether it accepts
 * writes at all.
 *
 * They live here because they are module-scoped in the product too — one
 * `useAuth()` and one `useServerInfo()` behind every component — and because a
 * component that branches on them has no prop a story could set instead.
 * `ResearchCard` is the case: its `⋯` menu is `role !== 'viewer'` when accounts
 * exist and `write_api` when they do not, so with the defaults below every role
 * story documented the auth-off branch while claiming to show a role.
 *
 * Defaults are the posture the whole catalogue has always run in — no accounts,
 * writes allowed — so a story that asks for nothing behaves exactly as before.
 */
const authEnabled = ref(false)
const writeApi = ref(true)

export interface AuthPosture {
  /** The server has accounts, so roles decide. */
  authEnabled?: boolean
  /** `write_api` from /api/health: false is a read-only server. */
  writeApi?: boolean
}

/** Installs the posture for the story about to render. Call it in `setup()`. */
export function mockAuth(next: AuthPosture): void {
  authEnabled.value = next.authEnabled ?? false
  writeApi.value = next.writeApi ?? true
}

/** Called from the global decorator, so one story's posture is not the next one's. */
export function resetMockAuth(): void {
  authEnabled.value = false
  writeApi.value = true
}

/** The refs the `useAuth` / `useServerInfo` stubs hand back. */
export function mockAuthState() {
  return { authEnabled, writeApi }
}
