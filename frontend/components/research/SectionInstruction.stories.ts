import type { Meta, StoryObj } from '@storybook/vue3'
import SectionInstruction from './SectionInstruction.vue'
import { mockSectionInstruction } from '../../__mocks__/metadata'
import { withoutShare } from '../../__mocks__/share'

/**
 * How to write in this section, shown above the documents already written in it.
 *
 * A research already carries two methodology surfaces — its memory, and the
 * skills it follows — and neither can say how to write in one particular
 * section. This is the third, and the most specific of the three: it wins on
 * the question of what a document here looks like, and may not legislate
 * beyond that.
 *
 * Short is the mechanism, not a storage budget. The per-folder notes that
 * actually get followed are three to six imperatives; the ones that get skipped
 * are paragraphs explaining why the folder exists. The server refuses anything
 * over 500 runes rather than truncating it, and this block clamps at six lines
 * so a long one looks like the exception it is.
 *
 * **There is no empty story, and that is the point.** A section with no
 * instruction — which is most of them — does not render this component at all:
 * no ghost row, no "add one" hint, no reserved height. The read pane is
 * byte-identical to what it was before the feature existed.
 */
const meta: Meta<typeof SectionInstruction> = {
  title: 'Research/SectionInstruction',
  component: SectionInstruction,
  tags: ['autodocs'],
  decorators: [
    // `renderRefs` reads module-scoped share state, so whether `[[R2:E5]]`
    // below comes out a link or inert text depends on which story was clicked
    // before this one. Pinned, the way every other story file that renders a
    // cross-reference pins it.
    withoutShare(),
    () => ({ template: '<div style="max-width: 760px"><story /></div>' }),
  ],
  // The read pane passes all five: `editable` is the caller's `canWrite`, and
  // `sectionCode` is what the settings anchor is built from.
  args: { researchSlug: 'R21', sectionId: 'sec_spec', sectionCode: 'S13', editable: true },
}
export default meta
type Story = StoryObj<typeof SectionInstruction>

/**
 * The canonical shape: four imperatives, each naming a declared field.
 *
 * `Edit this instruction` under it is the only route from the rule to the place
 * it is written. It points at the section's own card —
 * `/research/R21/settings?tab=sections#fields-S13` — and it has to exist here
 * because the "N fields" link beside the section title is gated on the section
 * declaring fields, so a section with an instruction and no field spec would
 * otherwise be a rule with no way back to its editor.
 */
export const Canonical: Story = {
  args: { text: mockSectionInstruction },
}

/**
 * A viewer, or a visitor on any surface that is not their own research.
 *
 * The link is removed rather than disabled — the house rule — and nothing else
 * changes: the rule is worth reading whether or not you may change it, which is
 * why this block is not gated on write access the way the editor is.
 */
export const Viewer: Story = {
  args: { text: mockSectionInstruction, editable: false },
}

/**
 * One line. No box and no minimum height, so it does not read as an empty
 * container waiting to be filled.
 */
export const OneLine: Story = {
  args: { text: 'One paragraph of rationale, then the payload.' },
}

/**
 * Six lines exactly — the boundary, and the story worth checking first.
 *
 * No toggle. The clamp is applied whenever the block is collapsed rather than
 * only once it is known to overflow, so "does it overflow six lines" is a
 * question that can be asked honestly; the earlier shape asked an unclamped
 * element and always heard no. Six lines is invisible on six lines, and a
 * `Show the whole instruction` that revealed nothing would teach a reader to
 * distrust every other one.
 */
export const SixLines: Story = {
  args: {
    text: 'Назови производящий сервис в поле service.\n'
      + 'Назови потребителя в поле consumer.\n'
      + 'Заполни owner и status до сохранения.\n'
      + 'Один абзац обоснования, затем полезная нагрузка.\n'
      + 'Схему запроса и схему ответа — отдельными блоками.\n'
      + 'Коды ошибок перечисли таблицей.',
  },
}

/**
 * Eleven lines, 485 runes — under the cap and still far too long.
 *
 * This is the reachable overflow, and it is reachable through the line breaks
 * rather than the length: the block is `pre-wrap` because the breaks are the
 * content, so an instruction the server happily accepts can still be twice the
 * height of the first document under it. A single unbroken 500-rune paragraph
 * cannot reach six lines at this width, which is why the story that used to
 * stand here — 507 runes of prose, over the cap and about five lines tall —
 * documented a state the product refuses and showed no toggle while claiming to.
 *
 * This is also the only story where both controls share the row: `Show the
 * whole instruction` first, then `Edit this instruction`.
 */
export const Overlong: Story = {
  args: {
    text: 'Назови производящий сервис в поле service.\n'
      + 'Назови потребителя в поле consumer.\n'
      + 'Заполни owner и status до сохранения.\n'
      + 'Один абзац обоснования, затем полезная нагрузка.\n'
      + 'Схему запроса и схему ответа — отдельными блоками.\n'
      + 'Коды ошибок перечисли таблицей.\n'
      + 'Ограничения по нагрузке — числами, не словами.\n'
      + 'Сошлись на решение, из которого это следует.\n'
      + 'Не повторяй то, что уже сказано в памяти проекта.\n'
      + 'Не обещай дату без согласования с владельцем сервиса.\n'
      + 'Перед сохранением перечитай этот список.',
  },
}

/**
 * The same instruction after pressing **Show the whole instruction** — the half
 * of the toggle a static story cannot show.
 *
 * Worth its own entry because the pair is the claim: the collapsed story above
 * must be visibly shorter than this one. If they render alike, the measurement
 * has failed and the button is lying, which is the only way this component can
 * be wrong without erroring.
 */
export const Expanded: Story = {
  args: { ...Overlong.args },
  play: async ({ canvasElement }: { canvasElement: HTMLElement }) => {
    await clickButton(canvasElement, 'Show the whole instruction')
  },
}

/**
 * Expanded by a keyboard, without the button.
 *
 * A clamped block can hide a focusable thing: this instruction's `[[E12]]` is
 * on the eighth line, three lines under the clip. Tabbing to it would otherwise
 * put the focus ring on something the reader cannot see, and the page would
 * appear to lose focus entirely. `focusin` on the text opens the block instead,
 * so the link becomes visible at the moment it is reached — the story here
 * focuses that link directly, which is what Tab does.
 *
 * If this story is open in a tab that is not the focused one, Chrome defers the
 * focus event until the tab is focused, so the block may still be shown clamped
 * in a static build; the play function dispatches `focusin` itself in that case
 * rather than documenting a state nobody can see.
 */
export const ExpandedByKeyboardFocus: Story = {
  args: {
    text: 'Назови производящий сервис в поле service.\n'
      + 'Назови потребителя в поле consumer.\n'
      + 'Заполни owner и status до сохранения.\n'
      + 'Один абзац обоснования, затем полезная нагрузка.\n'
      + 'Схему запроса и схему ответа — отдельными блоками.\n'
      + 'Коды ошибок перечисли таблицей.\n'
      + 'Ограничения по нагрузке — числами, не словами.\n'
      + 'Следуй шаблону из [[E12]].',
  },
  play: async ({ canvasElement }: { canvasElement: HTMLElement }) => {
    for (let i = 0; i < 50; i++) {
      const link = canvasElement.querySelector<HTMLElement>('a.crossref-link')
      if (link) {
        link.focus()
        await new Promise((resolve) => setTimeout(resolve, 60))
        if (canvasElement.querySelector('.instruction-text.is-clamped')) {
          link.dispatchEvent(new FocusEvent('focusin', { bubbles: true }))
        }
        return
      }
      await new Promise((resolve) => setTimeout(resolve, 20))
    }
  },
}

/**
 * 500 runes of Cyrillic — the full cap.
 *
 * The cap is counted in runes on the server and in code points in the counter,
 * for the same reason: a Cyrillic instruction is not worth half a Latin one.
 * This story is what that sentence looks like at full length.
 *
 * It fills the clamp exactly — six lines, no toggle — which is the calibration
 * worth knowing: at this width the cap and the clamp were set to the same
 * length, so an instruction can only overflow through its line breaks, never
 * through its length. That is what `Overlong` shows.
 */
export const Cyrillic: Story = {
  args: {
    text: 'Назови производящий сервис и потребителя в первых двух строках. '.repeat(8).slice(0, 500),
  },
}

/**
 * A pasted URL with nothing to break on. It wraps inside the block; the pane
 * never scrolls sideways.
 */
export const UnbrokenString: Story = {
  args: {
    text: 'Ссылка на реестр: https://internal.example.invalid/registry/'
      + 'a'.repeat(220)
      + '?source=spec&revision=latest',
  },
}

/**
 * Cross-references resolve here as they do everywhere else. The text is a raw
 * field, so it is escaped and *then* linked — the other way round is an
 * injection sink.
 */
export const WithCrossRefs: Story = {
  args: {
    text: 'Follow the template in [[E12]].\nRecord the decision it rests on as [[R2:E5]].\n'
      + 'If neither applies, ask in the session before writing.',
  },
}

/**
 * Clicks the first button whose label matches, once it exists. There is no
 * `@storybook/test` in this project, so the catalogue polls — same helper shape
 * as `FieldSpecList.stories.ts`. Here it has to wait on more than a render: the
 * toggle only exists after `onMounted` has measured the block.
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
