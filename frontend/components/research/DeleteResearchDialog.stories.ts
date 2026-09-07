import type { Meta, StoryObj } from '@storybook/vue3'
import DeleteResearchDialog from './DeleteResearchDialog.vue'
import { mockDownload, type DownloadOutcome } from '../../__mocks__/download'

/**
 * Destroying a project, confirmed by typing its short code.
 *
 * The typed code is the whole feature. It is the substitute for being able to
 * look at the screen and see what you are about to lose: a project is the one
 * thing in this product whose contents are larger than any view of it, and
 * "4 sections, 12 documents, 3 sessions and 21 questions" is not something a
 * page can show you at the moment you decide.
 *
 * Matching is case-insensitive and whitespace-trimmed, and **paste is not
 * blocked**. Blocking it is theatre — the safety is in having read which
 * project this is, not in having held Shift — and it breaks voice control,
 * switch access and password managers.
 */
const meta: Meta<typeof DeleteResearchDialog> = {
  title: 'Research/DeleteResearchDialog',
  component: DeleteResearchDialog,
  tags: ['autodocs'],
  parameters: { layout: 'fullscreen' },
  args: {
    visible: true,
    code: 'R3',
    name: 'Market sizing for the EU launch',
    summary: {
      sections: 4,
      entries: 12,
      sessions: 3,
      questions: 21,
      tasks: 6,
      roadmaps: 2,
      annotations: 0,
      shares: 0,
      incoming_refs: 0,
      incoming_from: null,
    },
  },
}
export default meta
type Story = StoryObj<typeof DeleteResearchDialog>

/** The ordinary case: a project with work in it. */
export const Default: Story = {}

/**
 * The consequences that reach outside the project. These are the two lines that
 * change a decision, so they are the only warning-coloured thing in the dialog.
 */
export const WithConsequencesElsewhere: Story = {
  args: {
    summary: {
      sections: 4, entries: 12, sessions: 3, questions: 21, tasks: 6, roadmaps: 2,
      annotations: 3, shares: 1, incoming_refs: 5,
      incoming_from: [{ code: 'R2', name: 'Pricing' }, { code: 'R9', name: 'Competitors' }],
    },
  },
}

/** Nothing filed yet. The code is still required — the confirmation is about
 *  *which* project, not about how much is in it. */
export const EmptyProject: Story = {
  args: {
    summary: {
      sections: 0, entries: 0, sessions: 0, questions: 0, tasks: 0, roadmaps: 0,
      annotations: 0, shares: 0, incoming_refs: 0, incoming_from: null,
    },
  },
}

/** The counts are still being fetched. The field and the button work anyway: a
 *  person who has already decided is not held up by a number. */
export const CountsLoading: Story = {
  args: { summary: null, loading: true },
}

/** The count request failed. No retry button — the reader is one Escape from a
 *  page that will fetch it again, and a retry here would compete with the two
 *  buttons that matter. */
export const CountsFailed: Story = {
  args: { summary: null, summaryFailed: true },
}

/** The delete is in flight. Cancel is disabled; the confirm button is
 *  `aria-disabled` rather than `disabled`, so focus is not dropped. */
export const Deleting: Story = {
  args: { busy: true },
}

/** A 5xx. The copy promises atomicity because the cascade is one transaction —
 *  if that ever stops being true, this sentence has to change with it. */
export const AfterFailure: Story = {
  args: { failure: 'The project was not deleted. Nothing was changed — try again.' },
}

/** A 403: the role changed under the reader. The button stays dead, because
 *  retrying is the one thing that cannot help. */
export const NoLongerOwner: Story = {
  args: {
    failure: 'You are no longer an owner of this project, so it cannot be deleted here.',
    permanentlyRefused: true,
  },
}

/** A long name wraps and clamps rather than pushing the code off the row. */
export const LongCyrillicName: Story = {
  args: {
    code: 'R17',
    name: 'Аналитика рынка и конкурентная разведка по направлению корпоративных подписок на 2026 год',
  },
}

/**
 * One of everything, which is the only place the singulars can be read: the
 * sessions line reads "1 session and 1 question", and a mark is "1 mark".
 *
 * Pluralisation is hand-rolled per row rather than an "(s)", because the
 * sessions row joins two counts into one sentence and no suffix survives that.
 */
export const SingularCounts: Story = {
  args: {
    code: 'R8',
    name: 'Sales call, 14 March',
    summary: {
      sections: 1, entries: 1, sessions: 1, questions: 1, tasks: 1, roadmaps: 1,
      annotations: 1, shares: 1, incoming_refs: 1,
      incoming_from: [{ code: 'R2', name: 'Pricing' }],
    },
  },
}

/**
 * Exactly one project cites this one, so the sentence names it: "from R2". Two
 * or more collapse to "R2 and 1 other" — see `WithConsequencesElsewhere` — and
 * this is the branch that must not say "and 0 others".
 */
export const CitedByOneProject: Story = {
  args: {
    summary: {
      sections: 4, entries: 12, sessions: 0, questions: 0, tasks: 0, roadmaps: 0,
      annotations: 0, shares: 0, incoming_refs: 3,
      incoming_from: [{ code: 'R2', name: 'Pricing' }],
    },
  },
}

/**
 * References counted but not attributed — `incoming_from` is null, which the
 * type permits. The sentence drops the "from …" clause rather than rendering
 * "from undefined", and the count still carries the warning.
 *
 * Sessions with no questions is the other branch here: that line reads
 * "2 sessions" alone, because a session with no questions is not two ideas.
 */
export const CitedByProjectsNotNamed: Story = {
  args: {
    summary: {
      sections: 4, entries: 12, sessions: 2, questions: 0, tasks: 0, roadmaps: 0,
      annotations: 0, shares: 2, incoming_refs: 7, incoming_from: null,
    },
  },
}

/**
 * A large project. The counts are `tabular-nums` so the list reads as a column
 * of numbers rather than a ragged paragraph — which is the one thing the reader
 * is meant to weigh before typing the code.
 */
export const ALargeProject: Story = {
  args: {
    code: 'R42',
    name: 'Customer research programme, 2024–2026',
    summary: {
      sections: 18, entries: 1284, sessions: 96, questions: 743, tasks: 210,
      roadmaps: 7, annotations: 388, shares: 4, incoming_refs: 152,
      incoming_from: [
        { code: 'R2', name: 'Pricing' },
        { code: 'R9', name: 'Competitors' },
        { code: 'R11', name: 'Churn interviews' },
      ],
    },
  },
}

/**
 * The code typed correctly, which is the only state in which Delete is live.
 *
 * It is typed in **lower case, with spaces around it** on purpose: matching is
 * case-insensitive and trimmed, so ` r3 ` is the same act as `R3` — which is
 * what makes pasting the code from the card above it work.
 */
export const CodeTyped: Story = {
  play: async ({ canvasElement }: { canvasElement: HTMLElement }) => {
    await typeCode(canvasElement, ' r3 ')
  },
}

/**
 * The wrong code, submitted with Enter.
 *
 * Enter is the only way to reach this state — the button is `disabled` until
 * the code matches — and that is why the field handles the key at all: someone
 * who types into a confirmation field and presses Enter has submitted, and
 * silence there reads as a broken dialog. The hint turns red, takes
 * `role="alert"`, and clears on the next keystroke.
 */
export const WrongCode: Story = {
  play: async ({ canvasElement }: { canvasElement: HTMLElement }) => {
    await typeCode(canvasElement, 'R4')
    const field = dialog(canvasElement).querySelector<HTMLInputElement>('[data-confirm-code]')
    field?.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', bubbles: true, cancelable: true }))
  },
}

/**
 * The export in flight. The offer is a `link-btn` rather than a second solid
 * button because it must not compete with Delete, and it goes `aria-disabled`
 * rather than `disabled` while it works — a control that leaves the tab order
 * mid-press drops the reader's place.
 */
export const PreparingACopy: Story = {
  render: withDownload({ pending: true }),
  play: async ({ canvasElement }: { canvasElement: HTMLElement }) => {
    await clickDownload(canvasElement)
  },
}

/** The copy landed. The dialog names the file, because the browser's own
 *  download shelf is the one place the reader is not looking. */
export const CopySaved: Story = {
  render: withDownload({ ok: true, filename: 'R3.json' }),
  play: async ({ canvasElement }: { canvasElement: HTMLElement }) => {
    await clickDownload(canvasElement)
  },
}

/**
 * The copy failed, and the dialog is otherwise untouched: the typed code, the
 * counts and both buttons stay exactly as they were.
 *
 * That is the whole design of this row. The export is optional, so a failure in
 * it must not interrupt, reset or block the act the reader came for — it says
 * one line and gets out of the way.
 */
export const CopyFailed: Story = {
  render: withDownload({
    ok: false,
    error: { status: 404, message: 'this is no longer available. Reload the page to check.' },
  }),
  play: async ({ canvasElement }: { canvasElement: HTMLElement }) => {
    await clickDownload(canvasElement)
  },
}

/**
 * `useDownload` is stubbed in Storybook — the real one calls `$fetch.raw` and
 * ends by handing a blob to a synthetic `<a download>`, which would put files on
 * the reader's disk for clicking through a catalogue. Its outcome is chosen per
 * story; see `__mocks__/download.ts`.
 */
function withDownload(outcome: DownloadOutcome) {
  return (args: any) => ({
    components: { DeleteResearchDialog },
    setup() {
      mockDownload(outcome)
      return { args }
    },
    template: '<DeleteResearchDialog v-bind="args" />',
  })
}

/**
 * The dialog is not inside the story canvas.
 *
 * `ModalOverlay` is a `<Teleport to="body">`, so `canvasElement` is empty and
 * every `play` here searches the document instead. A helper that queried the
 * canvas found nothing, timed out silently, and left the story showing its
 * untouched default state while claiming to show what a click produces.
 */
function dialog(root: HTMLElement): ParentNode {
  return root.ownerDocument ?? document
}

/** Types into the confirmation field the way a person does — through `input`,
 *  so `v-model` and the mismatch reset both see it. */
async function typeCode(root: HTMLElement, value: string): Promise<void> {
  for (let i = 0; i < 50; i++) {
    const field = dialog(root).querySelector<HTMLInputElement>('[data-confirm-code]')
    if (field) {
      field.value = value
      field.dispatchEvent(new Event('input', { bubbles: true }))
      await new Promise((resolve) => setTimeout(resolve, 20))
      return
    }
    await new Promise((resolve) => setTimeout(resolve, 20))
  }
}

/** Presses the export offer, once it exists, and lets the stubbed request settle. */
async function clickDownload(root: HTMLElement): Promise<void> {
  for (let i = 0; i < 50; i++) {
    const button = Array.from(dialog(root).querySelectorAll('button'))
      .find((b) => b.textContent?.includes('Download a copy'))
    if (button) {
      button.click()
      await new Promise((resolve) => setTimeout(resolve, 40))
      return
    }
    await new Promise((resolve) => setTimeout(resolve, 20))
  }
}
