import type { Meta, StoryObj } from '@storybook/vue3'
import EntriesView from './EntriesView.vue'
import { mockEntry, mockEntryDraft } from '../../__mocks__/entry'
import { markupDescription, markupImg } from '../../__mocks__/markup'
import { mockSections, mockSection, mockSectionCompleted } from '../../__mocks__/section'
import { withShare, withoutShare } from '../../__mocks__/share'
import { mockApi } from '../../__mocks__/api'
import { mockSectionInstruction, mockSpecEntries, mockSpecSection, specAtCap, specSingleField } from '../../__mocks__/metadata'

/**
 * The entry grid, either grouped by section or filtered to one.
 *
 * Three grids live in this one component and each is reached by a different
 * `v-if`: the search results, the grouped list, and the single section's list
 * (which a declaring section can swap for a table). All three draw `EntryCard`
 * and all three therefore link through `entryPath()`, which is what lets the
 * same grid render under a share link without dropping an anonymous visitor
 * onto the login wall.
 *
 * Above them sits one `EntriesToolbar` — search and tag filter in a row. The
 * two filters compose: a query narrows to matches, a tag narrows those further,
 * and each of the four outcomes has its own empty state and its own story here.
 */
const meta: Meta<typeof EntriesView> = {
  title: 'Research/EntriesView',
  component: EntriesView,
  tags: ['autodocs'],
  decorators: [
    // Share state is module state; this gives the ordinary stories a known
    // starting point rather than whatever the last story left behind. The
    // trade-offs are in __mocks__/share.ts.
    withoutShare(),
    () => ({
      template: '<div style="max-width: 800px"><story /></div>',
    }),
  ],
  argTypes: {
    mode: { control: 'select', options: ['all', 'section'] },
    loading: { control: 'boolean' },
  },
}
export default meta
type Story = StoryObj<typeof EntriesView>

const entriesWithSections = [
  { ...mockEntry, section_id: 'sec_001' },
  { ...mockEntryDraft, section_id: 'sec_001' },
  { ...mockEntry, id: 'ent_004', code: 'E4', title: 'Slots and Render Functions', tags: ['vue', 'slots'], status: 'active', section_id: 'sec_002' },
  { ...mockEntry, id: 'ent_005', code: 'E5', title: 'Performance Optimization', tags: ['vue', 'performance'], status: 'pending', section_id: 'sec_002' },
]

const tags = [
  { tag: 'vue', count: 4 },
  { tag: 'composables', count: 2 },
  { tag: 'slots', count: 1 },
  { tag: 'performance', count: 1 },
  { tag: 'reactivity', count: 1 },
]

export const AllEntriesGrouped: Story = {
  args: {
    entries: entriesWithSections,
    sections: [mockSection, mockSectionCompleted],
    researchSlug: 'R1',
    loading: false,
    mode: 'all',
    tags,
  },
}

export const Loading: Story = {
  args: {
    entries: [],
    sections: mockSections,
    researchSlug: 'R1',
    loading: true,
    mode: 'all',
    tags: [],
  },
}

export const SectionMode: Story = {
  args: {
    entries: [
      { ...mockEntry, section_id: 'sec_001' },
      { ...mockEntryDraft, section_id: 'sec_001' },
    ],
    sections: mockSections,
    researchSlug: 'R1',
    loading: false,
    mode: 'section',
    sectionInfo: mockSection,
    tags: [],
  },
}

/**
 * The same section, carrying a writing instruction.
 *
 * The block sits between the section's description and the documents, and the
 * two are meant to be told apart at a glance: a label, full-strength text
 * against the description's muted grey, and the 2px rule the product already
 * uses for a blockquote. Read it beside `SectionMode` directly above, which is
 * the same section without one — most sections have none, and they render
 * exactly as they did before this feature: no ghost row, no reserved height.
 */
export const SectionWithInstruction: Story = {
  args: {
    ...SectionMode.args,
    sectionInfo: { ...mockSection, instruction: mockSectionInstruction },
  },
}

/**
 * The same section while somebody is searching — **the instruction is gone**.
 *
 * A search turns the pane into a result list, and the rule for writing in this
 * section has nothing to say about a list of matches; leaving it there pushes
 * the first result down the page on every keystroke. It comes back when the box
 * is cleared. The threshold is one character, so a stray keypress does not make
 * the block flicker.
 */
export const SectionWithInstructionWhileSearching: Story = {
  decorators: [withSearchResults([{ ...mockEntry, section_id: 'sec_001' }])],
  args: { ...SectionWithInstruction.args },
  play: async ({ canvasElement }: { canvasElement: HTMLElement }) => {
    await typeQuery(canvasElement, 'composition')
  },
}

/**
 * A tag applied. The chip moves out of the quick row into its fixed place after
 * the search box, with a `×`; the list narrows to the two entries tagged
 * `composables`; the other groups disappear rather than showing empty headings.
 */
export const TagFilterActive: Story = {
  args: {
    entries: entriesWithSections,
    sections: [mockSection, mockSectionCompleted],
    researchSlug: 'R1',
    loading: false,
    mode: 'all',
    tags,
  },
  play: async ({ canvasElement }: { canvasElement: HTMLElement }) => {
    await clickButton(canvasElement, 'composables')
  },
}

/**
 * The state that motivated the toolbar: a research whose sections between them
 * carry a hundred and twenty tags. Before, they were a cloud twenty rows deep
 * above the first entry. Now the row is one control tall — six chips and
 * `Tags 120` — and the list starts where the eye expects it.
 */
export const ManyTags: Story = {
  args: {
    entries: entriesWithSections,
    sections: [mockSection, mockSectionCompleted],
    researchSlug: 'R1',
    loading: false,
    mode: 'all',
    tags: [
      ...tags,
      ...Array.from({ length: 115 }, (_, i) => ({ tag: `topic-${String(i + 1).padStart(3, '0')}`, count: 1 })),
    ],
  },
}

/**
 * A filter whose tag no longer exists on the surface — a realtime update
 * removed the last entry carrying it. The filter is never cleared on the
 * user's behalf; the empty state says the filter is still on and offers the
 * way out.
 */
export const TagFilterWithNoEntries: Story = {
  args: {
    entries: entriesWithSections,
    sections: [mockSection, mockSectionCompleted],
    researchSlug: 'R1',
    loading: false,
    mode: 'all',
    tags: [...tags, { tag: 'orphan', count: 1 }],
  },
  play: async ({ canvasElement }: { canvasElement: HTMLElement }) => {
    await clickButton(canvasElement, 'orphan')
  },
}

/**
 * Section mode with a tag applied. The counts are the section's own — `vue`
 * reads 2 here and 4 in all-entries mode, and both are right for the list they
 * sit above.
 */
export const SectionModeTagFilter: Story = {
  args: {
    entries: [
      { ...mockEntry, section_id: 'sec_001' },
      { ...mockEntryDraft, section_id: 'sec_001' },
    ],
    sections: mockSections,
    researchSlug: 'R1',
    loading: false,
    mode: 'section',
    sectionInfo: mockSection,
    tags: [],
  },
  play: async ({ canvasElement }: { canvasElement: HTMLElement }) => {
    await clickButton(canvasElement, 'reactivity')
  },
}

/**
 * Nothing in the research, and therefore **no toolbar**: a search box above
 * nothing is furniture, and with no entries there is nothing to filter and
 * nothing to clear. The row comes back the moment there is a query, a tag or
 * one entry — including while loading, which is why `Loading` shows it.
 */
/**
 * A query and a tag at once.
 *
 * The search is the server's — `/api/search`, debounced, research-wide — and it
 * replaces the list while it stands. The tag then narrows *the results*, which
 * is the only rule nobody has to learn: two filters that compose. The meta line
 * beside the box says `1 of 4 matches` rather than `1 match`, because the four
 * is the fact a reader needs to decide whether the tag was worth applying.
 *
 * The four results are routed through the mock `authFetch`; nothing here
 * touches a network.
 */
export const SearchNarrowedByTag: Story = {
  decorators: [withSearchResults(entriesWithSections)],
  args: {
    entries: entriesWithSections,
    sections: [mockSection, mockSectionCompleted],
    researchSlug: 'R1',
    researchId: 'res_001',
    loading: false,
    mode: 'all',
    tags,
  },
  play: async ({ canvasElement }: { canvasElement: HTMLElement }) => {
    await typeQuery(canvasElement, 'vue')
    await clickButton(canvasElement, 'composables')
  },
}

/**
 * The search found something and the tag excludes all of it.
 *
 * Distinct from "nothing matches the query", and worth its own state: the
 * entries are there, one filter is hiding them from the other, and a reader who
 * is told only "nothing matches “vue”" would go looking for a search problem.
 * So the count of what the query alone found is in the description, and both
 * filters are offered separately for clearing — the component never clears
 * either one on the reader's behalf.
 */
export const SearchWithNoTagOverlap: Story = {
  decorators: [withSearchResults([{ ...mockEntryDraft, section_id: 'sec_001' }])],
  args: {
    entries: entriesWithSections,
    sections: [mockSection, mockSectionCompleted],
    researchSlug: 'R1',
    researchId: 'res_001',
    loading: false,
    mode: 'all',
    tags,
  },
  play: async ({ canvasElement }: { canvasElement: HTMLElement }) => {
    await typeQuery(canvasElement, 'vue')
    await clickButton(canvasElement, 'composables')
  },
}

export const Empty: Story = {
  args: {
    entries: [],
    sections: mockSections,
    researchSlug: 'R1',
    loading: false,
    mode: 'all',
    tags: [],
  },
}

const markupEntry = {
  ...mockEntry,
  id: 'ent_markup',
  code: 'E9',
  title: 'Author-supplied HTML in a description',
  description: markupDescription,
  section_id: 'sec_001',
}

/**
 * A description with markup in it, in the **grouped** branch.
 *
 * The escaping itself is `EntryCard`'s now — this component used to write the
 * card three times and each copy called `renderRefs` into `v-html` of its own.
 * What these two stories still check is that each branch actually routes through
 * that one implementation: a branch that ever stops doing so is invisible from
 * `EntryCard`'s own stories, and there are three branches.
 *
 * The markup must read as text and `[[E3]]` must still be a link. An executed
 * payload prints `XSS EXECUTED` in place of the image tag.
 */
export const MarkupInDescription: Story = {
  args: {
    entries: [markupEntry, ...entriesWithSections],
    sections: [mockSection, mockSectionCompleted],
    researchSlug: 'R1',
    loading: false,
    mode: 'all',
    tags,
  },
}

/** The same description in the **section** branch — the second of the three
 *  grids, reached by a different `v-if` from the one above. */
export const MarkupInDescriptionSectionMode: Story = {
  args: {
    entries: [markupEntry, { ...mockEntryDraft, section_id: 'sec_001' }],
    sections: mockSections,
    researchSlug: 'R1',
    loading: false,
    mode: 'section',
    sectionInfo: mockSection,
    tags: [],
  },
}

/** The grouped grid inside a share link: every card points at
 *  `/s/{token}/entry/…`, in both the grouped branch and the flat one. */
export const InsideAShare: Story = {
  decorators: [withShare()],
  args: {
    entries: entriesWithSections,
    sections: [mockSection, mockSectionCompleted],
    researchSlug: 'R7',
    loading: false,
    mode: 'all',
    tags,
  },
}

/**
 * An empty section inside a share link.
 *
 * The copy changes: the owner is told "Claude will populate this section with
 * research entries", which is true and useful to them and meaningless to a
 * client — it describes a tool they have never heard of doing work they have no
 * part in. A visitor gets "No entries in this section / There's nothing here
 * yet". Compare with `Empty` above.
 */
export const InsideAShareEmptySection: Story = {
  decorators: [withShare()],
  args: {
    entries: [],
    sections: mockSections,
    researchSlug: 'R7',
    loading: false,
    mode: 'section',
    sectionInfo: mockSection,
    tags: [],
  },
}

/** One section, inside a share link — the visitor's default view, since the
 *  shared overview opens on the first section rather than on "all entries". */
export const InsideAShareSectionMode: Story = {
  decorators: [withShare()],
  args: {
    entries: [
      { ...mockEntry, section_id: 'sec_001' },
      { ...mockEntryDraft, section_id: 'sec_001' },
    ],
    sections: mockSections,
    researchSlug: 'R7',
    loading: false,
    mode: 'section',
    sectionInfo: mockSection,
    tags: [],
  },
}

/**
 * The same section, with an instruction, inside a share link — and **the
 * instruction is not there**.
 *
 * An instruction is working process, like `instruction` on the research itself:
 * it says how this team writes, which is not what the link was sent to show.
 * The share payload is redacted server-side, so this `v-if` is the second
 * defence rather than the first — but it is the one a story can check, and a
 * component that renders whatever it is handed would leak the day a route
 * forgets. Compare with `SectionWithInstruction` above: identical props apart
 * from the share.
 */
export const InsideAShareInstructionHidden: Story = {
  decorators: [withShare()],
  args: {
    entries: [
      { ...mockEntry, section_id: 'sec_001' },
      { ...mockEntryDraft, section_id: 'sec_001' },
    ],
    sections: mockSections,
    researchSlug: 'R7',
    loading: false,
    mode: 'section',
    sectionInfo: { ...mockSection, instruction: mockSectionInstruction },
    tags: [],
  },
}

/**
 * A visitor typing in the box.
 *
 * `/api/search` is not on the share sub-mux and a visitor has no research id to
 * scope it by, so a shared page firing that query would have been an unscoped
 * search of the whole database on the second keystroke. Instead the entries the
 * page already holds are filtered by title, description and tags — and the box
 * must not promise more than that, which is why the placeholder reads “Filter
 * these entries…” and the no-match copy names the three fields it read.
 *
 * Both entries match `vue` on their tags alone; neither title contains it.
 *
 * The line "Results from the whole research, not just this section" is absent
 * here on purpose — under a share the search never left the section's entries.
 */
export const InsideAShareFiltering: Story = {
  decorators: [withShare()],
  args: {
    entries: [
      { ...mockEntry, section_id: 'sec_001' },
      { ...mockEntryDraft, section_id: 'sec_001' },
    ],
    sections: mockSections,
    researchSlug: 'R7',
    loading: false,
    mode: 'section',
    sectionInfo: mockSection,
    tags: [],
  },
  play: async ({ canvasElement }: { canvasElement: HTMLElement }) => {
    await typeQuery(canvasElement, 'vue')
  },
}

/* --- Sections that declare fields -------------------------------------------
 *
 * Everything below needs `sectionInfo.field_spec`. Without it the component is
 * exactly what it was before this feature: no toggle, no chips, no table — which
 * is what `SectionMode` above still shows, and is the right behaviour for a
 * section that is a topic rather than a class of document.
 *
 * The table only exists in section mode — one declaration measures one set of
 * columns. The "N missing" chip is on both grids, because the grouped one holds
 * a section per group and can measure each against its own declaration; that is
 * the surface people actually browse a research from, and the chip is worth
 * least where it cannot be seen.
 */

/**
 * Six sibling specifications in a section that declares six fields, as cards.
 *
 * The "N missing" chips are the point of this view: E52 answers no owner and
 * E54 answers nothing at all, and both say so before anyone opens them. E53
 * carries an explicit unknown, which is *not* a gap and correctly shows no chip.
 */
export const SectionWithFields: Story = {
  args: {
    entries: mockSpecEntries,
    sections: [mockSpecSection],
    researchSlug: 'R21',
    loading: false,
    mode: 'section',
    sectionInfo: mockSpecSection,
    tags: [],
  },
}

/**
 * All entries, grouped, where one of the groups declares fields.
 *
 * The chips appear under "Спецификации" and nowhere else on the page: the
 * research's other sections declare nothing, so their documents cannot be
 * incomplete and are not decorated as if they were. Two sections behaving
 * differently in one list is the intended reading — most sections are topics.
 */
export const AllEntriesWithFields: Story = {
  args: {
    entries: [...mockSpecEntries.slice(0, 3), { ...mockEntry, section_id: 'sec_001' }],
    sections: [mockSpecSection, mockSection],
    researchSlug: 'R21',
    loading: false,
    mode: 'all',
    tags: [{ tag: 'spec', count: 3 }, { tag: 'vue', count: 1 }],
  },
}

/**
 * The same section, tabulated. **This is the story to read.**
 *
 * The table is not the payoff of declaring fields, it is the enforcement
 * mechanism — a blank cell beside filled ones is the strongest force there is on
 * whether an optional field ever gets answered, and nothing else in the product
 * puts eighteen documents' worth of blanks in one place.
 *
 * `Registry` holds a bare `E47` and comes out a link, because the field is
 * declared `ref` and the table runs values through the same renderer the
 * document's own block does. A table that printed `[[E47]]` while the page
 * linked it would be one string rendered two ways.
 *
 * Three things it says at a glance. `Reviewed` is empty for every row: a column
 * of dashes is the feature's own output, and the answer to whether that field
 * should have been declared at all. `Owner` on E53 reads `unknown`, not a dash —
 * somebody looked, which is a different fact from silence. E54's row is empty
 * end to end.
 */
export const SectionTable: Story = {
  args: { ...SectionWithFields.args },
  play: async ({ canvasElement }: { canvasElement: HTMLElement }) => {
    await clickButton(canvasElement, 'Table')
  },
}

/**
 * The table with a tag applied — the two controls are independent state.
 *
 * Worth a story because the table is the one grid whose `v-if` reads
 * `filteredEntries` while its *columns* come from the section's declaration:
 * narrowing to `config` leaves one row and all six columns, not the columns that
 * row happens to fill. E53's `unknown` owner is the row that survives.
 *
 * Clearing the filter to nothing would drop back to the "No entries tagged"
 * empty state and take the table with it — the toggle stays, so there is a way
 * back.
 */
export const SectionTableTagFilter: Story = {
  args: { ...SectionWithFields.args },
  play: async ({ canvasElement }: { canvasElement: HTMLElement }) => {
    await clickButton(canvasElement, 'Table')
    await clickButton(canvasElement, 'config')
  },
}

/**
 * A section where every document leaves every declared field blank.
 *
 * A table of nothing but dashes, which is a legitimate production state — the
 * fields were declared and no agent has written one since. It reads as an
 * indictment of the declaration rather than of the documents, which is the
 * correct reading.
 */
export const SectionTableAllEmpty: Story = {
  args: {
    ...SectionWithFields.args,
    entries: mockSpecEntries.map(e => ({ ...e, metadata: {} })),
  },
  play: async ({ canvasElement }: { canvasElement: HTMLElement }) => {
    await clickButton(canvasElement, 'Table')
  },
}

/**
 * One declared field: two fixed columns and one of its own.
 *
 * Worth keeping in the catalogue because the toggle is offered here — the
 * component asks only whether the section declares anything, not whether it
 * declares enough to be worth tabulating.
 */
export const SectionTableSingleField: Story = {
  args: {
    ...SectionWithFields.args,
    sectionInfo: { ...mockSpecSection, field_spec: specSingleField },
  },
  play: async ({ canvasElement }: { canvasElement: HTMLElement }) => {
    await clickButton(canvasElement, 'Table')
  },
}

/**
 * Twelve columns — the cap — with Cyrillic values in them.
 *
 * The frame scrolls sideways, the page body must not. That is the only reason
 * `.meta-table-wrap` exists, and a wide table with long values is the only way
 * to find out whether it still holds. Drag the table horizontally; the story
 * container around it should not move.
 */
export const SectionTableWide: Story = {
  args: {
    ...SectionWithFields.args,
    sectionInfo: { ...mockSpecSection, field_spec: specAtCap },
    entries: mockSpecEntries.map(e => ({
      ...e,
      metadata: {
        ...(e.metadata as Record<string, unknown>),
        component: 'сканер площадок',
        transport: 'temporal',
        reviewer: 'команда платформы наблюдаемости площадок',
        schema_url: 'https://example.invalid/schemas/site-payload.json',
        retries: 3,
        supersedes: ['E48', 'E49'],
      },
    })),
  },
  play: async ({ canvasElement }: { canvasElement: HTMLElement }) => {
    await clickButton(canvasElement, 'Table')
  },
}

/**
 * The declaring section inside a share link.
 *
 * A visitor gets the chips and the table like anyone else — the values are part
 * of the document, not working process. Every row still links through
 * `entryPath()` to `/s/{token}/entry/…`.
 */
export const SectionTableInsideAShare: Story = {
  decorators: [withShare()],
  args: { ...SectionWithFields.args, researchSlug: 'R7' },
  play: async ({ canvasElement }: { canvasElement: HTMLElement }) => {
    await clickButton(canvasElement, 'Table')
  },
}

/**
 * Markup inside a table cell.
 *
 * The table cell is the only `v-html` left in this component — the card copies
 * that used to surround it are `EntryCard`'s — and field values are
 * agent-authored text like every other string here. `<b>bold</b>` must read as text and `[[E47]]` must
 * still be a link; an executed payload prints `XSS EXECUTED` where the image
 * tag was.
 *
 * Cells are `white-space: nowrap`, so a value long enough to matter widens the
 * column and the frame scrolls. That is the intended behaviour and this is
 * where to check it has not become the page scrolling.
 */
export const SectionTableMarkup: Story = {
  args: {
    ...SectionWithFields.args,
    entries: [
      {
        ...mockSpecEntries[0],
        metadata: {
          stage: `<b>draft</b> ${markupImg}`,
          produces: ['<script>alert(1)</script>', 'scanner-watchdog'],
          owner: 'platform — см. [[E47]]',
        },
      },
      ...mockSpecEntries.slice(1, 3),
    ],
  },
  play: async ({ canvasElement }: { canvasElement: HTMLElement }) => {
    await clickButton(canvasElement, 'Table')
  },
}

/**
 * Answers the one request this component makes — `/api/search` — through the
 * mock `authFetch`. A decorator rather than a `render`, so the stories keep
 * their args and their controls.
 */
function withSearchResults(entries: any[]) {
  return (story: any) => ({
    components: { story },
    setup() {
      mockApi({ '/api/search': { entries } })
      return {}
    },
    template: '<story />',
  })
}

/**
 * Types into the toolbar's search box. The component debounces by 200ms before
 * it asks the server, so the results land a moment after the play function has
 * returned — that is the real timing and the story shows it.
 */
async function typeQuery(root: HTMLElement, text: string): Promise<void> {
  for (let i = 0; i < 50; i++) {
    const input = root.querySelector<HTMLInputElement>('input[type="search"]')
    if (input) {
      input.value = text
      input.dispatchEvent(new Event('input', { bubbles: true }))
      return
    }
    await new Promise((resolve) => setTimeout(resolve, 20))
  }
}

/**
 * Clicks the first button whose label matches, once it exists. No
 * `@storybook/test` in this project, so the catalogue polls — same helper shape
 * as `HistoryPanel.stories.ts`.
 */
async function clickButton(root: HTMLElement, label: string): Promise<void> {
  for (let i = 0; i < 50; i++) {
    // A chip's text is `name` + a count span, so the match is on the name
    // alone; a plain button's whole label is its name.
    const button = Array.from(root.querySelectorAll('button'))
      .find(b => (b.querySelector('.tag-text')?.textContent ?? b.textContent)?.trim() === label) as HTMLElement | undefined
    if (button) {
      button.click()
      return
    }
    await new Promise((resolve) => setTimeout(resolve, 20))
  }
}
