import type { Meta, StoryObj } from '@storybook/vue3'
import ResearchCard from './ResearchCard.vue'
import ToastHost from './ToastHost.vue'
import { fails, mockApi } from '../__mocks__/api'
import { mockAuth } from '../__mocks__/auth'
import { mockResearch, mockResearchCompleted, mockResearchArchived } from '../__mocks__/research'

/**
 * The card the research list is made of.
 *
 * This used to render a hand-written stub of the markup, which drifted: the
 * catalogue went on showing a card the product had stopped rendering. It draws
 * the real component now — `useAuth` and `useRuntimeConfig` come from the
 * Storybook stubs, and `relativeTime` / `tagHue` from the real composables, so
 * what is on screen is what ships.
 *
 * **The `⋯` menu is decided by two different things.** With accounts on, by the
 * role on the record — an editor may archive, only an owner may delete. With
 * accounts off there are no roles, so it follows `write_api` from `/api/health`
 * instead, which is what stops a card offering Delete to a reader whose DELETE
 * comes back 401 from a remote server with no `api_token`.
 *
 * Storybook runs with accounts off by default, so every story below that means
 * to show a *role* has to say so — `mockAuth({ authEnabled: true })`, through
 * the `withAccounts` decorator. Without it a viewer's card renders the
 * write-enabled menu and the story quietly documents the wrong branch.
 *
 * **Known defect, reproduced here:** pressing the `⋯` also follows the card's
 * link, because `ActionMenu`'s trigger stops propagation and the wrapper's
 * `@click.prevent` therefore never runs. The menu stories cancel that default
 * themselves — see `openMenu` — so that they can show a menu at all.
 */
const meta: Meta<typeof ResearchCard> = {
  title: 'Cards/ResearchCard',
  component: ResearchCard,
  tags: ['autodocs'],
}
export default meta
type Story = StoryObj<typeof ResearchCard>

/** Accounts exist, so the card decides by the role on the record. */
const withAccounts = (story: any) => ({
  components: { story },
  setup() {
    mockAuth({ authEnabled: true })
  },
  template: '<story />',
})

/**
 * No `role` on the record at all, which is what an install with auth off sends.
 * There is nobody to be a viewer, so the menu follows `write_api` — true here,
 * as it is for the local single binary — and carries both Archive and Delete.
 */
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
  decorators: [withAccounts],
}

export const InYourPersonalTeam: Story = {
  args: {
    research: { ...mockResearch, team_name: 'Pavel Buchnev', team_is_personal: true, role: 'owner' },
  },
  decorators: [withAccounts],
}

/**
 * An editor gets the `⋯` menu with **Archive** only — shown open here, because
 * a closed menu documents nothing. Deleting destroys work belonging to everyone
 * else in the team, so it is not an editor's to take however many confirmations
 * stand in front of it.
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
  decorators: [withAccounts],
  play: async ({ canvasElement }: { canvasElement: HTMLElement }) => {
    await openMenu(canvasElement)
  },
}

/**
 * The owner's menu carries **Delete project** below a divider, in the danger
 * style. The `play` opens it, so the divider and the danger colour are what the
 * story actually shows.
 *
 * The trigger is always visible, unlike the hover-revealed archive icon it
 * replaced — an action menu that appears on hover is undiscoverable on touch
 * and invisible-while-focusable on a keyboard.
 *
 * The item emits `delete` and does nothing else. The card never deletes
 * anything itself: the list page owns the dialog, because the summary, the
 * typed code and the toast all outlive the card that is about to disappear.
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
  decorators: [withAccounts],
  play: async ({ canvasElement }: { canvasElement: HTMLElement }) => {
    await openMenu(canvasElement)
  },
}

/**
 * The same menu on an archived project: the first item reads **Restore from
 * archive**. One item that flips, not two — the state and its undo are the same
 * act from the reader's side.
 */
export const MenuOnAnArchivedProject: Story = {
  args: { research: { ...mockResearchArchived, role: 'owner' } },
  decorators: [withAccounts],
  play: async ({ canvasElement }: { canvasElement: HTMLElement }) => {
    await openMenu(canvasElement)
  },
}

/**
 * Archiving refused by the server.
 *
 * This is worth a story because the failure used to be completely silent: the
 * click handler carries `@click.prevent.stop` — it has to, or the card's own
 * `NuxtLink` navigates — so a rejected write left no toast, no status change
 * and no page move. Nothing on screen said anything at all. The toast is the
 * only report there is.
 */
export const ArchiveRefused: Story = {
  render: (args: any) => ({
    components: { ResearchCard, ToastHost },
    setup() {
      mockApi({ '/api/researches': () => fails('403') })
      return { args }
    },
    template: '<div><ResearchCard v-bind="args" /><ToastHost /></div>',
  }),
  args: { research: { ...mockResearch, role: 'owner' } },
  decorators: [withAccounts],
  play: async ({ canvasElement }: { canvasElement: HTMLElement }) => {
    await openMenu(canvasElement)
    await clickMenuItem(canvasElement, 'Archive')
  },
}

/**
 * A read-only server: no accounts, and `/api/health` reports `write_api: false`.
 * No menu at all, on a record carrying no role.
 *
 * This is the case the rule was rewritten for. "No role means you own it" is
 * true of the local single binary and false of a remote one started without an
 * `api_token` — where it put an Archive and a Delete on every card, both of
 * which come back 401.
 */
export const ReadOnlyServer: Story = {
  args: { research: mockResearch },
  decorators: [
    (story: any) => ({
      components: { story },
      setup() {
        mockAuth({ authEnabled: false, writeApi: false })
      },
      template: '<story />',
    }),
  ],
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
  decorators: [withAccounts],
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
  decorators: [withAccounts],
}

/**
 * Opens the `⋯`, once the card has rendered. There is no `@storybook/test` in
 * this project, so the catalogue polls — same helper shape as
 * `FieldSpecList.stories.ts`.
 *
 * **The `preventDefault` is compensating for a live defect in the card, not for
 * anything about Storybook.** Pressing the `⋯` today also follows the card's
 * link: `ActionMenu`'s trigger calls `stopPropagation()`, so the event never
 * reaches the `@click.prevent.stop` wrapper that was added to stop exactly
 * this, nothing prevents the default, and the browser navigates to
 * `/research/R1` — a full document load, which in the product means the menu
 * opens and the SPA reloads out from under it. Verified in headless Chrome:
 * the story's own iframe navigated away and every menu story rendered a 404.
 *
 * Without this line the four menu stories cannot exist at all. Delete it when
 * the card is fixed; if the menu still opens afterwards, nothing is lost.
 */
async function openMenu(root: HTMLElement): Promise<void> {
  const swallowNavigation = (e: Event) => e.preventDefault()
  // Capture phase: runs before the trigger's own handler, so the menu still
  // opens — only the anchor's default action is cancelled.
  document.addEventListener('click', swallowNavigation, true)
  try {
    for (let i = 0; i < 50; i++) {
      const trigger = root.querySelector<HTMLElement>('.action-menu > button')
      if (trigger) {
        trigger.click()
        await new Promise((resolve) => setTimeout(resolve, 20))
        return
      }
      await new Promise((resolve) => setTimeout(resolve, 20))
    }
  } finally {
    document.removeEventListener('click', swallowNavigation, true)
  }
}

/** Presses an item in the open panel by its visible label. */
async function clickMenuItem(root: HTMLElement, label: string): Promise<void> {
  for (let i = 0; i < 50; i++) {
    const item = Array.from(root.querySelectorAll('.action-menu-item'))
      .find((b) => b.textContent?.trim() === label) as HTMLElement | undefined
    if (item) {
      item.click()
      await new Promise((resolve) => setTimeout(resolve, 40))
      return
    }
    await new Promise((resolve) => setTimeout(resolve, 20))
  }
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
