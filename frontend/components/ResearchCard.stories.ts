import type { Meta, StoryObj } from '@storybook/vue3'
import ResearchCard from './ResearchCard.vue'
import { mockResearch, mockResearchCompleted, mockResearchArchived } from '../__mocks__/research'

/**
 * The card the research list is made of.
 *
 * This used to render a hand-written stub of the markup, which drifted: the
 * catalogue went on showing a card the product had stopped rendering. It draws
 * the real component now — `useAuth` and `useRuntimeConfig` come from the
 * Storybook stubs, and `relativeTime` / `tagHue` from the real composables, so
 * what is on screen is what ships.
 */
const meta: Meta<typeof ResearchCard> = {
  title: 'Cards/ResearchCard',
  component: ResearchCard,
  tags: ['autodocs'],
}
export default meta
type Story = StoryObj<typeof ResearchCard>

export const Active: Story = {
  args: { research: mockResearch },
}

export const Completed: Story = {
  args: { research: mockResearchCompleted },
}

export const Archived: Story = {
  args: { research: mockResearchArchived },
}

export const WithoutTags: Story = {
  args: { research: { ...mockResearch, tags: [] } },
}

export const ManyTags: Story = {
  args: {
    research: {
      ...mockResearch,
      tags: ['vue', 'react', 'angular', 'svelte', 'typescript', 'javascript', 'css', 'architecture'],
    },
  },
}

export const LongGoal: Story = {
  args: {
    research: {
      ...mockResearch,
      name: 'Comprehensive Frontend Framework Evaluation',
      goal: 'Evaluate and compare modern frontend frameworks including Vue 3, React 18, Angular 17, Svelte 5, and Solid.js across performance benchmarks, developer experience, ecosystem maturity, and enterprise readiness criteria.',
    },
  },
}

/**
 * A research in a shared team carries its name. Your own personal team does
 * not — labelling every card with your own name is noise.
 */
export const InASharedTeam: Story = {
  args: {
    research: {
      ...mockResearch,
      name: 'Интеграция с 1С',
      goal: 'Свести обмен номенклатурой в одну очередь',
      tags: ['интеграции', '1С'],
      team_name: 'Отдел интеграций',
      team_is_personal: false,
      role: 'editor',
    },
  },
}

export const InYourPersonalTeam: Story = {
  args: {
    research: { ...mockResearch, team_name: 'Pavel Buchnev', team_is_personal: true, role: 'owner' },
  },
}

/**
 * An editor gets the `⋯` menu with **Archive** only. Deleting destroys work
 * belonging to everyone else in the team, so it is not an editor's to take
 * however many confirmations stand in front of it.
 */
export const AsAnEditor: Story = {
  args: {
    research: {
      ...mockResearch,
      team_name: 'Отдел интеграций',
      team_is_personal: false,
      role: 'editor',
    },
  },
}

/**
 * The owner's menu carries **Delete project** below a divider, in the danger
 * style. Open the `⋯` to see it.
 *
 * The trigger is always visible, unlike the hover-revealed archive icon it
 * replaced — an action menu that appears on hover is undiscoverable on touch
 * and invisible-while-focusable on a keyboard.
 */
export const AsAnOwner: Story = {
  args: {
    research: {
      ...mockResearch,
      team_name: 'Отдел интеграций',
      team_is_personal: false,
      role: 'owner',
    },
  },
}

/**
 * A viewer gets the read-only marker and loses the menu entirely — the one
 * place in the list where a role is shown, because it is the one place it
 * takes something away.
 */
export const AsAViewer: Story = {
  args: {
    research: {
      ...mockResearch,
      name: 'Интеграция с 1С',
      team_name: 'Отдел интеграций',
      team_is_personal: false,
      role: 'viewer',
    },
  },
}

/** A long team name has to wrap in the footer rather than push the timestamp off. */
export const LongTeamName: Story = {
  args: {
    research: {
      ...mockResearch,
      team_name: 'Отдел интеграций и сопровождения корпоративных систем',
      team_is_personal: false,
      role: 'viewer',
    },
  },
}

export const AllStatuses: Story = {
  render: () => ({
    components: { ResearchCard },
    setup() {
      return { researches: [mockResearch, mockResearchCompleted, mockResearchArchived] }
    },
    template: `
      <div style="display: grid; grid-template-columns: repeat(auto-fill, minmax(320px, 1fr)); gap: 1rem;">
        <ResearchCard v-for="r in researches" :key="r.id" :research="r" />
      </div>
    `,
  }),
}
