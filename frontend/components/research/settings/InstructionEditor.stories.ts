import type { Meta, StoryObj } from '@storybook/vue3'
import { ref } from 'vue'
import InstructionEditor from './InstructionEditor.vue'
import { fails, neverResolves } from '../../../__mocks__/api'
import { mockSectionInstruction } from '../../../__mocks__/metadata'

/**
 * Where a section's writing instruction is written, on the settings card that
 * already declares the section's fields.
 *
 * It sits **under** the field list rather than above it, and that is the
 * load-bearing choice on this surface: the instruction is supposed to name the
 * keys the section declares, so those keys have to be on screen directly above
 * the textarea somebody types into. Move it above the list and the anti-drift
 * argument is gone.
 *
 * The cap is refused, never truncated — there is no `maxlength`, because
 * `maxlength` silently swallows half a pasted instruction, which is the one
 * behaviour this field exists to rule out.
 *
 * Half of this component is behind a button, so the stories that document the
 * editor open it themselves in a `play` function. A story whose text says
 * "press Edit to see the refusal" documents nothing to the reader scrolling
 * the docs page, and nothing at all to a screenshot.
 */
const meta: Meta<typeof InstructionEditor> = {
  title: 'Research/Settings/InstructionEditor',
  component: InstructionEditor,
  tags: ['autodocs'],
  decorators: [
    () => ({ template: '<div class="card" style="max-width: 760px; padding: 16px"><story /></div>' }),
  ],
  args: {
    editable: true,
    cap: 500,
    fieldKeys: ['service', 'consumer', 'owner', 'status'],
    onSave: async () => {},
  },
}
export default meta
type Story = StoryObj<typeof InstructionEditor>

/** A section that carries one. The counter says how much of the budget it uses. */
export const Filled: Story = {
  args: { instruction: mockSectionInstruction },
}

/**
 * The normal case on this surface: most sections carry no instruction, and the
 * row says what that means rather than nagging.
 */
export const Empty: Story = {
  args: { instruction: '' },
}

/**
 * A viewer. Controls are removed rather than disabled — the house rule — and
 * the sentence names who can add one, because that is the only useful next
 * step for somebody who cannot.
 */
export const EmptyViewer: Story = {
  args: { instruction: '', editable: false },
}

/** A viewer on a section that has one: the text, and nothing to press. */
export const FilledViewer: Story = {
  args: { instruction: mockSectionInstruction, editable: false },
}

/**
 * The editor, opened on an instruction that already exists.
 *
 * Everything below the heading only exists in this state: the help line that
 * says what belongs here and what belongs in memory or skills, the placeholder
 * showing the three-imperative shape, the "name the declared fields" line, the
 * counter, and Clear beside Save and Cancel.
 */
export const EditorOpen: Story = {
  args: { instruction: mockSectionInstruction },
  play: async ({ canvasElement }: { canvasElement: HTMLElement }) => {
    await clickButton(canvasElement, 'Edit')
  },
}

/**
 * The editor opened from nothing — the button reads "Add instruction" and the
 * textarea shows its placeholder, which is the only place the intended shape is
 * ever demonstrated rather than described.
 */
export const EditorFromEmpty: Story = {
  args: { instruction: '' },
  play: async ({ canvasElement }: { canvasElement: HTMLElement }) => {
    await clickButton(canvasElement, 'Add instruction')
  },
}

/**
 * A section that declares no fields. The "name the declared fields" line is
 * absent rather than saying "no fields" — the card's own blurb above already
 * says that, and repeating it would be two sentences for one fact.
 */
export const NoFieldsDeclared: Story = {
  args: { instruction: '', fieldKeys: [] },
  play: async ({ canvasElement }: { canvasElement: HTMLElement }) => {
    await clickButton(canvasElement, 'Add instruction')
  },
}

/**
 * Twelve declared fields — the field cap — so the line elides: six keys and
 * "and 6 more".
 *
 * The elision is deliberate and it is a trade the story should show rather than
 * hide: past six keys the sentence would be longer than the instruction it is
 * advising on, and a hint that pushes the textarea below the fold advises
 * nobody. The full vocabulary is in the rows directly above on the real card.
 */
export const ManyFieldKeys: Story = {
  args: {
    instruction: '',
    fieldKeys: [
      'service', 'consumer', 'owner', 'status', 'stage', 'registry',
      'reviewed', 'retries', 'schema_url', 'sla', 'runbook', 'oncall',
    ],
  },
  play: async ({ canvasElement }: { canvasElement: HTMLElement }) => {
    await clickButton(canvasElement, 'Add instruction')
  },
}

/**
 * Approaching the cap: 460 of 500. The number is the information; the warning
 * colour only reinforces it, so the state survives a reader who cannot tell the
 * two colours apart.
 */
export const ApproachingCap: Story = {
  args: { instruction: 'Назови производящий сервис и потребителя. '.repeat(11).slice(0, 460) },
  play: async ({ canvasElement }: { canvasElement: HTMLElement }) => {
    await clickButton(canvasElement, 'Edit')
  },
}

/**
 * Over it. Save is disabled and the refusal says by how much and what will
 * happen — not "invalid", and not a quietly shortened instruction.
 *
 * Counted in code points, not UTF-16 units, because the server counts runes:
 * this story is Cyrillic for that reason, and a single emoji would be enough to
 * make a `String.length` counter disagree with the refusal the server sends.
 */
export const OverCap: Story = {
  args: { instruction: 'Назови производящий сервис и потребителя. '.repeat(13).slice(0, 531) },
  play: async ({ canvasElement }: { canvasElement: HTMLElement }) => {
    await clickButton(canvasElement, 'Edit')
  },
}

/**
 * The server refused. The editor stays open and the draft is kept, always: the
 * text is the expensive thing here, and the refusal is fixable in place.
 *
 * The message is the server's own where there is one. The fallback below it —
 * "Your text is still here" — exists because the refusals this field can hit
 * are ones the client cannot predict: a cap it disagrees with, a permission
 * that changed under the editor.
 */
export const ServerRefused: Story = {
  args: {
    instruction: mockSectionInstruction,
    onSave: () => fails('instruction must be 500 characters or fewer'),
  },
  play: async ({ canvasElement }: { canvasElement: HTMLElement }) => {
    await clickButton(canvasElement, 'Edit')
    await clickButton(canvasElement, 'Save')
  },
}

/**
 * Mid-save. The buttons are disabled; the textarea is not — the save is short,
 * and disabling an input under a cursor moves focus out from under the writer.
 *
 * Reachable only because saving is a function prop the component awaits. An
 * emit returns undefined, so an awaited emit would clear the busy flag before
 * the request landed and make the error line above unreachable.
 */
export const Saving: Story = {
  args: { instruction: mockSectionInstruction, onSave: () => neverResolves() },
  play: async ({ canvasElement }: { canvasElement: HTMLElement }) => {
    await clickButton(canvasElement, 'Edit')
    await clickButton(canvasElement, 'Save')
  },
}

/* --- The instruction changing under an open editor ---------------------------
 *
 * The settings page refetches the whole research after any neighbouring save,
 * so a new `instruction` can arrive while somebody is typing into this one. The
 * component has two branches for that and they behave oppositely; both stories
 * below drive the same harness, which re-renders the row with a new value when
 * its "Saved from another tab" button is pressed. That button is part of the
 * story, not of the product.
 */

const remoteText = mockSectionInstruction + '\nСошлись на реестр в поле registry.'

function withRemoteChange() {
  return (args: any) => ({
    components: { InstructionEditor },
    setup() {
      const instruction = ref(args.instruction as string)
      return { args, instruction, applyRemote: () => { instruction.value = remoteText } }
    },
    template: `
      <div>
        <InstructionEditor v-bind="args" :instruction="instruction" />
        <button type="button" class="btn btn-sm" style="margin-top: 24px" @click="applyRemote">
          Saved from another tab
        </button>
      </div>
    `,
  })
}

/**
 * An **untouched** draft adopts the new value silently.
 *
 * Nothing was typed, so there is nothing to protect and nothing to warn about;
 * warning here would be a banner about a change the reader cannot even see the
 * old side of. The textarea now holds the newer text — note the extra line.
 */
export const RemoteChangeAdopted: Story = {
  args: { instruction: mockSectionInstruction },
  render: withRemoteChange(),
  play: async ({ canvasElement }: { canvasElement: HTMLElement }) => {
    await clickButton(canvasElement, 'Edit')
    await clickButton(canvasElement, 'Saved from another tab')
  },
}

/**
 * A **touched** draft is kept, and the row says what saving will do.
 *
 * Last-write-wins, stated rather than enforced: a section carries no version to
 * conflict against, so the honest move is to warn and let the writer decide.
 * Discarding five hundred characters of somebody's typing to win a race nobody
 * was running is the worse outcome, and it is the one a naive `watch` produces.
 */
export const StaleAfterRemoteChange: Story = {
  args: { instruction: mockSectionInstruction },
  render: withRemoteChange(),
  play: async ({ canvasElement }: { canvasElement: HTMLElement }) => {
    await clickButton(canvasElement, 'Edit')
    await typeInto(canvasElement, mockSectionInstruction + '\nПроверь пример запроса перед сохранением.')
    await clickButton(canvasElement, 'Saved from another tab')
  },
}

/**
 * Clicks the first button whose label matches, once it exists. There is no
 * `@storybook/test` in this project, so the catalogue polls — same helper shape
 * as `FieldSpecList.stories.ts`.
 */
async function clickButton(root: HTMLElement, label: string): Promise<void> {
  for (let i = 0; i < 50; i++) {
    const button = Array.from(root.querySelectorAll('button'))
      .find(b => b.textContent?.trim() === label) as HTMLElement | undefined
    if (button) {
      button.click()
      await new Promise((resolve) => setTimeout(resolve, 20))
      return
    }
    await new Promise((resolve) => setTimeout(resolve, 20))
  }
}

/**
 * Types into the open textarea the way a person does — through an `input`
 * event, so `v-model` sees it and the draft genuinely diverges from what the
 * editor was opened with. Setting `.value` alone would leave the component
 * believing the draft untouched, which is the exact distinction the two stories
 * above turn on.
 */
async function typeInto(root: HTMLElement, text: string): Promise<void> {
  for (let i = 0; i < 50; i++) {
    const field = root.querySelector('textarea') as HTMLTextAreaElement | null
    if (field) {
      field.value = text
      field.dispatchEvent(new Event('input', { bubbles: true }))
      await new Promise((resolve) => setTimeout(resolve, 20))
      return
    }
    await new Promise((resolve) => setTimeout(resolve, 20))
  }
}
