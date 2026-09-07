import { ref } from 'vue'

/**
 * A stand-in for `useDownload()`, whose real implementation reaches for
 * `$fetch.raw`, an `AbortController` and `URL.createObjectURL`.
 *
 * The catalogue must not save a file to the reader's disk when they click
 * through a story, and the three states worth documenting — preparing, saved,
 * refused — are all *outcomes of a request*, so there is no prop to set them
 * with. This is the only way to reach them.
 *
 * Usage from a story:
 *
 *   setup() { mockDownload({ pending: true }) }   // "Preparing…", forever
 *   setup() { mockDownload({ ok: false, error: { status: 404, message: '…' } }) }
 */
export interface DownloadOutcome {
  /** False takes the failure branch; the caller turns `error` into its own sentence. */
  ok?: boolean
  /** The name the server chose, which the real composable reads off a header. */
  filename?: string
  error?: { status: number; message: string }
  /** Never settles, so `pending` stays true — a "Preparing…" label with no end. */
  pending?: boolean
}

const DEFAULT: DownloadOutcome = { ok: true, filename: 'R3.json' }

let outcome: DownloadOutcome = DEFAULT

/** Installs the outcome for the story about to render. Call it in `setup()`. */
export function mockDownload(next: DownloadOutcome): void {
  outcome = next
}

/** Called from the global decorator, so one story's failure is not the next one's. */
export function resetMockDownload(): void {
  outcome = DEFAULT
}

/** What the `useDownload` stub in .storybook/stubs/imports.ts returns. */
export function createMockDownload() {
  const pending = ref(false)
  const slow = ref(false)
  const filename = ref<string | null>(null)
  const error = ref<{ status: number; message: string } | null>(null)

  async function start(_path: string, fallbackName: string): Promise<boolean> {
    if (pending.value) return false
    pending.value = true
    filename.value = null
    error.value = null
    // The real one is a network round trip, so a story that renders the state
    // *after* it has to await something. A macrotask is enough.
    if (outcome.pending) return new Promise<boolean>(() => {})
    await new Promise((resolve) => setTimeout(resolve, 0))
    pending.value = false
    if (outcome.ok === false) {
      error.value = outcome.error ?? {
        status: 0,
        message: 'no answer from the server. Check your connection and try again.',
      }
      return false
    }
    filename.value = outcome.filename ?? fallbackName
    return true
  }

  function reset() {
    filename.value = null
    error.value = null
    slow.value = false
  }

  return { pending, slow, filename, error, start, reset, abort: () => { pending.value = false } }
}
