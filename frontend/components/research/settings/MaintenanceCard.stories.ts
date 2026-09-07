import type { Meta, StoryObj } from '@storybook/vue3'
import MaintenanceCard from './MaintenanceCard.vue'
import { fails, mockApi, neverResolves } from '../../../__mocks__/api'
import {
  mockCrossRefSummaryBroken,
  mockCrossRefSummaryEmpty,
  mockCrossRefSummaryHealthy,
  mockCrossRefSummaryOverloaded,
  mockCrossRefSummaryPathological,
  mockCrossRefSummaryStillBroken,
  mockRebuildClean,
  mockRebuildStillBroken,
  type CrossRefSummary,
} from '../../../__mocks__/crossref'

/**
 * The one repair a person can run on a project from settings: rebuilding the
 * cross-reference index.
 *
 * The card takes a research id and nothing else. It reads
 * `GET /api/researches/{id}/crossrefs` on mount for the diagnosis, and posts to
 * `.../crossrefs/rebuild` for the cure — two separate requests, which is why a
 * failed read still leaves the Rebuild button in place. Every story here drives
 * both through the mocked `authFetch` in `__mocks__/api.ts`, so what renders is
 * the real component with only the network faked; `'/crossrefs/rebuild'` is the
 * longer key, so it wins over `'/crossrefs'` under that matcher and the read
 * and the write can be routed apart.
 *
 * The states that matter are the ones the sentence changes in. There is no
 * "count" story and no "chips" story: the note, the chips and the error line
 * are one reading, and splitting them would document markup instead of meaning.
 */
const meta: Meta<typeof MaintenanceCard> = {
  title: 'Research/Settings/MaintenanceCard',
  component: MaintenanceCard,
  tags: ['autodocs'],
  argTypes: {
    researchId: { control: 'text' },
  },
  decorators: [
    () => ({ template: '<div style="max-width: 860px"><story /></div>' }),
  ],
}
export default meta
type Story = StoryObj<typeof MaintenanceCard>

const RESEARCH_ID = 'res_001'

type Wiring = {
  /** Answers `GET .../crossrefs`. A function is called per request, so a story can change its answer. */
  read: CrossRefSummary | (() => unknown)
  /** Answers `POST .../crossrefs/rebuild`. Unset means the story is not about the write. */
  rebuild?: unknown | (() => unknown)
}

function card({ read, rebuild }: Wiring): Story['render'] {
  return () => ({
    components: { MaintenanceCard },
    setup() {
      mockApi({
        '/crossrefs': () => (typeof read === 'function' ? read() : { summary: read }),
        '/crossrefs/rebuild': () => (typeof rebuild === 'function' ? rebuild() : rebuild ?? {}),
      })
      return { researchId: RESEARCH_ID }
    },
    template: '<MaintenanceCard :research-id="researchId" />',
  })
}

/**
 * A rejection the way the API client raises one: an `Error` carrying the HTTP
 * status, which is what the card reads to tell a refusal it can do nothing
 * about from one worth retrying.
 */
function refuses(status: number): Promise<never> {
  return Promise.reject(Object.assign(new Error(`HTTP ${status}`), { status }))
}

/** Waits for a node to appear before a play function acts on it. */
async function waitFor(root: HTMLElement, selector: string): Promise<HTMLElement | null> {
  for (let i = 0; i < 50; i++) {
    const found = root.querySelector(selector) as HTMLElement | null
    if (found) return found
    await new Promise((resolve) => setTimeout(resolve, 20))
  }
  return null
}

/** Waits for the diagnosis to land, then presses Rebuild. */
async function rebuildFrom(canvasElement: HTMLElement, settled: string): Promise<void> {
  await waitFor(canvasElement, settled)
  const button = await waitFor(canvasElement, '.maintenance-action')
  button?.click()
}

/**
 * The state the card exists for: three references point at nothing.
 *
 * No cause is asserted, and that is deliberate. A dangling code may be a typo,
 * but it may equally be a session or question reference the resolver has never
 * been able to resolve — calling those a mistake would be a confident lie about
 * something rebuilding cannot fix. The chips name the codes so a person can go
 * and look.
 */
export const Broken: Story = {
  render: card({ read: mockCrossRefSummaryBroken, rebuild: mockRebuildClean }),
}

/** Everything resolves: one sentence, no chips, and the button still there. */
export const Healthy: Story = {
  render: card({ read: mockCrossRefSummaryHealthy, rebuild: { sources: 60, references: 128, unresolved: 0 } }),
}

/**
 * Nothing indexed at all — and the copy here is load-bearing.
 *
 * Zero indexed references is the exact symptom of a stale index, not just of a
 * project whose documents cite nothing. So the empty state says both: it
 * explains the syntax for a project that genuinely has none, and then tells a
 * person whose documents *do* cite each other that the index is out of date and
 * to rebuild it. A bare "nothing here yet" would leave the second reader
 * believing a broken index was an empty one, which is the single false
 * diagnosis this card must not give.
 */
export const Empty: Story = {
  render: card({ read: mockCrossRefSummaryEmpty, rebuild: { sources: 0, references: 0, unresolved: 0 } }),
}

/**
 * Three hundred broken references over forty distinct codes, of which the
 * server returned its cap of twelve.
 *
 * Past that cap the card prints a literal "and more" rather than a number. An
 * earlier version computed `unresolved - shown` — 300 minus 12 — which mixes
 * rows against codes and was wrong in every case but one, since one bad code
 * cited from thirty documents is thirty rows. The client cannot know how many
 * further distinct codes exist, so it says so instead of counting.
 */
export const Overloaded: Story = {
  render: card({ read: mockCrossRefSummaryOverloaded, rebuild: { sources: 410, references: 1240, unresolved: 300 } }),
}

/**
 * Four hundred characters with no spaces between the brackets.
 *
 * A person writes what they like inside `[[ ]]`, and the code comes back
 * through the summary unchanged. The chip is capped at `--tag-max` and
 * ellipsised, the way a tag is: a chip that grows to twenty lines and pushes
 * the Rebuild button off the row is not a chip.
 */
export const PathologicalCode: Story = {
  render: card({ read: mockCrossRefSummaryPathological, rebuild: mockRebuildClean }),
}

/**
 * The first read, still in flight.
 *
 * The skeleton stands in for the note — one line at the note's own width — and
 * the note itself renders empty rather than guessing. Reporting "no
 * cross-references indexed yet" while the answer is still on the wire would be
 * the false diagnosis above, delivered a second earlier.
 */
export const Loading: Story = {
  render: card({ read: () => neverResolves() }),
}

/**
 * The diagnosis failed, and the repair is offered anyway.
 *
 * Read and rebuild are separate requests, and the one that matters is the
 * rebuild — so the error line carries its own "Try again" for the read while
 * the Rebuild button stays live above it. Withholding the cure because the
 * diagnosis timed out would be the wrong way round.
 */
export const ReadFails: Story = {
  render: card({ read: () => fails(), rebuild: mockRebuildClean }),
}

/**
 * Mid-rebuild: the button reads "Rebuilding…" and the row is marked busy.
 *
 * `aria-disabled` rather than `disabled`, so the control keeps focus and a
 * screen reader keeps announcing it; the handler returns early while busy, so a
 * second press cannot start a second rebuild.
 */
export const Rebuilding: Story = {
  render: card({ read: mockCrossRefSummaryBroken, rebuild: () => neverResolves() }),
  play: async ({ canvasElement }: { canvasElement: HTMLElement }) => {
    await rebuildFrom(canvasElement, '.maintenance-code')
  },
}

/**
 * A rebuild from the broken start above that fixed everything — and the most
 * valuable story in this set.
 *
 * The counts come back clean, so the note reads "Rebuilt just now. All 47
 * references resolve." and **the chips are gone**. Carrying them forward is
 * what this used to do: the success path, the one a reader is most likely to
 * see, printed a sentence saying everything resolves directly above a row of
 * chips naming codes that no longer dangle. The card clears them on the clean
 * result rather than waiting for a re-read it does not perform.
 */
export const RebuiltClean: Story = {
  render: card({ read: mockCrossRefSummaryBroken, rebuild: mockRebuildClean }),
  play: async ({ canvasElement }: { canvasElement: HTMLElement }) => {
    await rebuildFrom(canvasElement, '.maintenance-code')
  },
}

/**
 * A rebuild that repaired two of the three and could not repair the last.
 *
 * The tail changes to "still point at nothing", which is the honest reading
 * after an attempt. Only here does the card re-read the summary, to refresh
 * *which* codes are still dangling — the counts it already has are right — so
 * this story's read answers differently the second time.
 */
export const RebuiltStillBroken: Story = {
  render: card({
    read: (() => {
      let call = 0
      return () => ({ summary: call++ === 0 ? mockCrossRefSummaryBroken : mockCrossRefSummaryStillBroken })
    })(),
    rebuild: mockRebuildStillBroken,
  }),
  play: async ({ canvasElement }: { canvasElement: HTMLElement }) => {
    await rebuildFrom(canvasElement, '.maintenance-code')
  },
}

/**
 * The rebuild failed for an ordinary reason, so it is offered again.
 *
 * The refusal lives inline, next to the control that caused it, and carries the
 * retry itself. There is no toast: it would be the same sentence twice, and an
 * error toast here does not dismiss itself.
 */
export const RebuildFails: Story = {
  render: card({ read: mockCrossRefSummaryBroken, rebuild: () => fails('rebuild failed') }),
  play: async ({ canvasElement }: { canvasElement: HTMLElement }) => {
    await rebuildFrom(canvasElement, '.maintenance-code')
  },
}

/**
 * The server refused: the person was demoted, or lost the team, while the page
 * was open.
 *
 * A 403 (and a 404, which is what losing the team outright answers) gets a
 * sentence and no retry button, because retrying would fail forever. It tells
 * them what to do instead — reload — since the page they are looking at is
 * already describing a project they can no longer change.
 */
export const Forbidden: Story = {
  render: card({ read: mockCrossRefSummaryBroken, rebuild: () => refuses(403) }),
  play: async ({ canvasElement }: { canvasElement: HTMLElement }) => {
    await rebuildFrom(canvasElement, '.maintenance-code')
  },
}
