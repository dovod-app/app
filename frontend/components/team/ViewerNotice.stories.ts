import type { Meta, StoryObj } from '@storybook/vue3'
import ViewerNotice from './ViewerNotice.vue'

/**
 * The one thing a viewer is told, instead of twelve disabled buttons.
 *
 * Edit controls are removed from a read-only page rather than dimmed — a
 * disabled button still says "you could have done this" and still needs a
 * tooltip nobody reads. But absence is undetectable by a screen reader, so the
 * missing controls are explained once, at the top, next to the status badge
 * where the eye already goes.
 *
 * The visible text is two words; the sentence lives in the `title` and in a
 * screen-reader-only span, and it names the team when there is one, because
 * "why can't I edit this" is answered by "through which team you got here".
 */
const meta: Meta<typeof ViewerNotice> = {
  title: 'Team/ViewerNotice',
  component: ViewerNotice,
  tags: ['autodocs'],
  argTypes: {
    teamName: { control: 'text' },
    reason: { control: 'select', options: ['viewer', 'remote'] },
  },
}
export default meta
type Story = StoryObj<typeof ViewerNotice>

/** With the team named — hover to read the full sentence. */
export const Default: Story = {
  args: { teamName: 'Product Research' },
}

/** The normal case for this product. */
export const CyrillicTeam: Story = {
  args: { teamName: 'Аналитика рынка' },
}

/** A long department name: the badge is fixed text, so only the explanation
 *  grows and the badge keeps its size. */
export const LongTeamName: Story = {
  args: { teamName: 'Аналитика рынка и конкурентная разведка (второй квартал)' },
}

/** No team name — the research page renders this while its payload is still in
 *  flight, and the sentence drops the clause rather than saying "through the
 *  team undefined". */
export const WithoutTeamName: Story = {
  args: {},
}

/**
 * The other reason the controls are gone: no accounts are configured at all, so
 * there are no roles and the server does not take changes from this machine.
 *
 * Deliberately not the viewer wording. "Your role in this team is viewer" would
 * be false here and would send the reader looking for a person who does not
 * exist — this reader needs a setting, so the sentence names the two. Hover, or
 * read it with a screen reader; the visible text is the same two words.
 */
export const RemoteServer: Story = {
  args: { reason: 'remote' },
}

/** A team name is ignored for that reason — there is no team to have got here
 *  through. Guards against a caller passing both. */
export const RemoteIgnoresTeamName: Story = {
  args: { reason: 'remote', teamName: 'Аналитика рынка' },
}

/** Where it sits: in the research header, after the code, the title and the
 *  status badge. */
export const InPageHeader: Story = {
  render: () => ({
    components: { ViewerNotice },
    template: `
      <div style="display: flex; align-items: center; gap: var(--space-3); flex-wrap: wrap;">
        <span class="short-code">R7</span>
        <h1 class="page-title" style="margin: 0;">Ценообразование в SaaS</h1>
        <span class="badge badge-active">active</span>
        <ViewerNotice team-name="Аналитика рынка" />
      </div>
    `,
  }),
}
