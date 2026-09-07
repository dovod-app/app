import type { Meta, StoryObj } from '@storybook/vue3'
import FieldSpecList from './FieldSpecList.vue'
import { fails, neverResolves } from '../../../__mocks__/api'
import {
  fieldCaps,
  fieldTypes,
  mockSpecSection,
  mockTopicSection,
  reservedKeys,
  specAtCap,
} from '../../../__mocks__/metadata'

/**
 * Where a person declares what a section's documents record.
 *
 * Most sections declare nothing and should — a section is usually a topic
 * ("Вопросы на повестку", "Отвергнутые гипотезы"), not a class of document. The
 * feature only earns its place where a section genuinely holds one kind of
 * thing, which is why the empty state says so rather than nagging.
 *
 * The caps, the type list and the reserved keys are all mocked from
 * `domain.FieldSchema()` rather than invented, because the component takes them
 * as props for exactly that reason: a cap the client believes and the server
 * enforces would disagree once, at the worst moment.
 *
 * Under each section's field rows sits the writing instruction — `How to write
 * here` — drawn by `Research/Settings/InstructionEditor`, which has its own
 * stories for its own states. It is **under** the rows on purpose: the
 * instruction is supposed to name the keys the section declares, and keeping
 * those keys on screen directly above the textarea is the whole anti-drift
 * argument for the placement.
 */
const meta: Meta<typeof FieldSpecList> = {
  title: 'Research/Settings/FieldSpecList',
  component: FieldSpecList,
  tags: ['autodocs'],
  decorators: [
    () => ({ template: '<div style="max-width: 860px"><story /></div>' }),
  ],
}
export default meta
type Story = StoryObj<typeof FieldSpecList>

/**
 * `onSaveInstruction` is in the baseline rather than in one story, because the
 * settings page — the only caller there is — always passes it. Leaving it out
 * would make every story here a card the product never renders: no "How to
 * write here" row under the field rows, on a surface whose own lead paragraph
 * now opens by promising one.
 *
 * `Mixed` is the pair worth reading: one section with an instruction and one
 * without, since the row is present either way and only its text changes.
 */
const base = {
  editable: true,
  caps: fieldCaps,
  types: fieldTypes,
  reservedKeys,
  onSaveInstruction: async () => {},
}

/** One section that declares fields, one that does not — the ordinary research. */
export const Mixed: Story = {
  args: { ...base, sections: [mockSpecSection, mockTopicSection] },
}

/** A section that declares nothing — the normal case, said out loud. */
export const NothingDeclared: Story = {
  args: { ...base, sections: [mockTopicSection] },
}

/**
 * Every declared type at once, which is the only place the read view's type
 * column can be compared.
 *
 * `enum` is the one that matters: converting a field to four to six named
 * options is what moves it from filled-a-tenth-of-the-time to filled most of
 * the time, while `date` and `text` fill no better than an untyped field. The
 * list reads as six equals and is not.
 */
export const AllFieldTypes: Story = {
  args: {
    ...base,
    sections: [{
      ...mockSpecSection,
      field_spec: [
        { key: 'stage', label: 'Стадия', type: 'enum', options: ['draft', 'in-review', 'agreed'], required: true },
        { key: 'registry', label: 'Registry', type: 'ref' },
        { key: 'reviewed', label: 'Reviewed', type: 'date' },
        { key: 'owner', label: 'Owner', type: 'text', required: true, help: 'The service name from the repo.' },
        { key: 'retries', label: 'Retries', type: 'number' },
        { key: 'schema_url', label: 'Schema', type: 'url' },
      ],
    }],
  },
}

/**
 * At the cap: twelve declared fields, five of them required.
 *
 * The two caps are independent and both are sitting on their limit here. The
 * count turns amber; `EditorAtTheCap` below is where "Add field" goes
 * unavailable, since that control only exists once the editor is open.
 */
export const AtTheCap: Story = {
  args: { ...base, sections: [{ ...mockSpecSection, field_spec: specAtCap }] },
}

/**
 * The editor, opened.
 *
 * This is half the component and none of it renders until a button is pressed:
 * a row per field with key, label, type, the two checkboxes and Remove, the
 * options box that appears only for `enum`, and the help box whose placeholder
 * is the instruction — "Where does this value come from? The agent reads this."
 * A required field without that note is an invitation to invent one.
 *
 * The reserved-key line underneath is the other thing worth reading. Those
 * eleven keys are what the Obsidian export already emits as front matter, and
 * YAML is last-wins, so a field keyed `status` would silently overwrite the
 * system value in every exported note.
 */
export const EditorOpen: Story = {
  args: { ...base, sections: [mockSpecSection] },
  play: async ({ canvasElement }: { canvasElement: HTMLElement }) => {
    await clickButton(canvasElement, 'Edit fields')
  },
}

/** The same editor on a section that declares nothing: the button reads
 *  "Declare fields" and opens onto no rows at all. */
export const EditorFromNothing: Story = {
  args: { ...base, sections: [mockTopicSection] },
  play: async ({ canvasElement }: { canvasElement: HTMLElement }) => {
    await clickButton(canvasElement, 'Declare fields')
  },
}

/** The editor at the cap: twelve rows and "Add field" disabled, with the cap
 *  itself as the button's title rather than a silent refusal. */
export const EditorAtTheCap: Story = {
  args: { ...base, sections: [{ ...mockSpecSection, field_spec: specAtCap }] },
  play: async ({ canvasElement }: { canvasElement: HTMLElement }) => {
    await clickButton(canvasElement, 'Edit fields')
  },
}

/**
 * Mid-save: the button reads "Saving...", the row controls stay put.
 *
 * Reachable only because saving is a function prop the component awaits. An
 * emit returns undefined, so an awaited emit would clear the busy flag before
 * the request landed and make the error box below unreachable.
 */
export const EditorSaving: Story = {
  args: { ...base, sections: [mockSpecSection], onSave: () => neverResolves() },
  play: async ({ canvasElement }: { canvasElement: HTMLElement }) => {
    await clickButton(canvasElement, 'Edit fields')
    await clickButton(canvasElement, 'Save')
  },
}

/**
 * The server refused the declaration.
 *
 * The refusals worth seeing here are the ones a client cannot make on its own:
 * a reserved key, a cap, a key that does not match the pattern. The editor
 * stays open with the rows intact — the message is only useful next to the
 * field that caused it. Nothing is applied optimistically for the same reason:
 * showing a declaration before the server agrees misstates what documents are
 * being held to.
 */
export const EditorSaveFails: Story = {
  args: {
    ...base,
    sections: [mockSpecSection],
    onSave: () => fails('field 3: "status" is a reserved key — the export already emits it'),
  },
  play: async ({ canvasElement }: { canvasElement: HTMLElement }) => {
    await clickButton(canvasElement, 'Edit fields')
    await clickButton(canvasElement, 'Save')
  },
}

/** A viewer reads the declaration and cannot change it — including the
 *  instruction row, whose Edit button is removed rather than disabled. */
export const ReadOnly: Story = {
  args: { ...base, sections: [mockSpecSection, mockTopicSection], editable: false },
}

/**
 * Without `onSaveInstruction` — the card as it looked before this feature.
 *
 * The prop is optional and the row is behind a `v-if` on it, so a caller that
 * passes no handler gets no instruction row at all rather than a row whose Save
 * does nothing. Kept as one story rather than as the default, because it is a
 * shape no page in the product renders; its job is to show what the `v-if`
 * decides, and to be the thing to compare against if the row ever goes missing
 * for a reason nobody meant.
 */
export const WithoutInstructionRow: Story = {
  args: {
    editable: true,
    caps: fieldCaps,
    types: fieldTypes,
    reservedKeys,
    sections: [mockSpecSection, mockTopicSection],
  },
}

/**
 * With `onDelete`, each section carries a Delete button — and an empty one is
 * the only kind it will act on.
 *
 * The refusal is **visible text beside the disabled button**, not a `title`: a
 * disabled control's tooltip reaches neither a keyboard nor a screen reader,
 * which is the rule `DangerRow` already states. And there is no "delete anyway"
 * — the refusal *is* the feature. The API's `force` exists for callers that are
 * explicit by construction, and offering it here would re-create exactly the
 * accident the refusal prevents.
 *
 * "Empty it first" is the honest instruction because a document cannot be moved
 * between sections anywhere in this product yet. When it can, this copy changes.
 */
export const WithDeleteControls: Story = {
  args: {
    ...base,
    researchSlug: 'R3',
    onDelete: async () => {},
    sections: [
      { ...mockSpecSection, entries_count: 8 },
      { ...mockTopicSection, entries_count: 0 },
    ],
  },
}

/** One document, so the refusal has to read "Holds 1 document." */
export const DeleteRefusedForOneDocument: Story = {
  args: {
    ...base,
    researchSlug: 'R3',
    onDelete: async () => {},
    sections: [{ ...mockTopicSection, entries_count: 1 }],
  },
}

/** Without `onDelete` the control is absent rather than disabled — the same
 *  rule the rest of the settings page follows. */
export const WithoutDeleteControls: Story = {
  args: { ...base, sections: [{ ...mockTopicSection, entries_count: 0 }] },
}

/**
 * Clicks the first button whose label matches, once it exists. There is no
 * `@storybook/test` in this project, so the catalogue polls — same helper shape
 * as `HistoryPanel.stories.ts`.
 */
async function clickButton(root: HTMLElement, label: string): Promise<void> {
  for (let i = 0; i < 50; i++) {
    const button = Array.from(root.querySelectorAll('button'))
      .find(b => b.textContent?.trim() === label) as HTMLElement | undefined
    if (button) {
      button.click()
      return
    }
    await new Promise((resolve) => setTimeout(resolve, 20))
  }
}

/**
 * The confirmation, opened.
 *
 * The button does not delete: it raises `ConfirmModal`, whose message names the
 * section and states the thing that makes this safe — it is empty, so nothing
 * is filed under it. A one-click delete beside a field editor is a misclick
 * waiting to happen, and the row above it is a row of editable inputs.
 */
export const DeleteConfirmation: Story = {
  args: {
    ...base,
    researchSlug: 'R3',
    onDelete: async () => {},
    sections: [{ ...mockTopicSection, entries_count: 0 }],
  },
  play: async ({ canvasElement }: { canvasElement: HTMLElement }) => {
    await clickButton(canvasElement, 'Delete')
  },
}

/**
 * The delete in flight: the row's button reads "Deleting…" and is disabled, and
 * the confirmation's own button reads "Wait...".
 *
 * Both, not either — the modal covers the card on a narrow screen and the card
 * is what the reader looks back at when it closes, so a busy state on only one
 * of them leaves half the surface claiming nothing is happening.
 */
export const DeleteInFlight: Story = {
  args: {
    ...base,
    researchSlug: 'R3',
    onDelete: () => neverResolves(),
    sections: [{ ...mockTopicSection, entries_count: 0 }],
  },
  play: async ({ canvasElement }: { canvasElement: HTMLElement }) => {
    await clickButton(canvasElement, 'Delete')
    await clickConfirm(canvasElement)
  },
}

/**
 * Presses the confirmation's own danger button.
 *
 * Scoped to `.confirm-actions` because the row's control and the modal's carry
 * the same label — which is deliberate, a confirmation that renames the act is
 * asking the reader to decide about a different thing.
 *
 * Searched from the document, not the canvas: `ConfirmModal` sits in a
 * `<Teleport to="body">`, so a query rooted at `canvasElement` finds nothing,
 * times out in silence, and leaves the story documenting the state before the
 * click.
 */
async function clickConfirm(root: HTMLElement): Promise<void> {
  for (let i = 0; i < 50; i++) {
    const button = (root.ownerDocument ?? document).querySelector<HTMLElement>('.confirm-actions .btn-danger')
    if (button) {
      button.click()
      await new Promise((resolve) => setTimeout(resolve, 20))
      return
    }
    await new Promise((resolve) => setTimeout(resolve, 20))
  }
}
