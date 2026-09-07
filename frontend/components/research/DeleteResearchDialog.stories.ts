import type { Meta, StoryObj } from '@storybook/vue3'
import DeleteResearchDialog from './DeleteResearchDialog.vue'

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

/** The project changed under the open dialog. A staleness line rather than live
 *  counts: a number that changes while you read it costs the ceremony its
 *  credibility. */
export const CountsStale: Story = {
  args: { stale: true },
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
