import type { Meta, StoryObj } from '@storybook/vue3'
import ModalOverlay from './ModalOverlay.vue'

const meta: Meta<typeof ModalOverlay> = {
  title: 'Base/ModalOverlay',
  component: ModalOverlay,
  tags: ['autodocs'],
  argTypes: {
    visible: { control: 'boolean' },
    size: { control: 'select', options: ['sm', 'md', 'lg', 'xl'] },
    flush: { control: 'boolean' },
    labelledby: { control: 'text' },
    initialFocus: { control: 'text' },
  },
  parameters: {
    docs: {
      description: {
        component:
          'The shell every modal sits in: overlay, card chrome, Escape to close, ' +
          'focus moved inside on open and returned to the trigger on close, and Tab ' +
          'cycling within the card. Pass `labelledby` with the id of the heading ' +
          'inside the slot — without it a screen reader announces only "dialog", ' +
          'and the attribute cannot be handed down as a fallthrough because the ' +
          'root is a `<Teleport>`.',
      },
    },
  },
}
export default meta
type Story = StoryObj<typeof ModalOverlay>

/** The canonical shape: the heading carries an id and `labelledby` points at it,
 *  so the dialog announces by name. */
export const Default: Story = {
  args: { visible: true, labelledby: 'modal-default-title' },
  render: (args: any) => ({
    components: { ModalOverlay },
    setup() { return { args } },
    template: `
      <ModalOverlay v-bind="args">
        <h3 id="modal-default-title" style="margin: 0 0 0.5rem; font-weight: 600;">Rebuild cross-references</h3>
        <p style="color: var(--color-text-muted); font-size: var(--type-sm); margin: 0;">Re-scans every entry in R1 and repairs any [[E3]] link that no longer resolves.</p>
      </ModalOverlay>
    `,
  }),
}

/** No `labelledby`: identical on screen, and announced as nothing but "dialog".
 *  Every modal in the app looked like this before the prop existed. */
export const WithoutLabel: Story = {
  args: { visible: true },
  render: (args: any) => ({
    components: { ModalOverlay },
    setup() { return { args } },
    template: `
      <ModalOverlay v-bind="args">
        <h3 style="margin: 0 0 0.5rem; font-weight: 600;">Rebuild cross-references</h3>
        <p style="color: var(--color-text-muted); font-size: var(--type-sm); margin: 0;">The heading has no id, so nothing names the dialog for a screen reader.</p>
      </ModalOverlay>
    `,
  }),
}

export const Small: Story = {
  args: { visible: true, size: 'sm' },
  render: (args: any) => ({
    components: { ModalOverlay },
    setup() { return { args } },
    template: `
      <ModalOverlay v-bind="args">
        <h3 style="margin: 0 0 0.5rem; font-weight: 600;">Confirm</h3>
        <p style="color: var(--color-text-muted); font-size: var(--type-sm); margin: 0 0 1rem;">Are you sure you want to delete this?</p>
        <div style="display: flex; gap: 0.5rem; justify-content: flex-end;">
          <button class="btn btn-sm">Cancel</button>
          <button class="btn btn-sm" style="background: var(--color-error); color: white;">Delete</button>
        </div>
      </ModalOverlay>
    `,
  }),
}

export const Large: Story = {
  args: { visible: true, size: 'lg' },
  render: (args: any) => ({
    components: { ModalOverlay },
    setup() { return { args } },
    template: `
      <ModalOverlay v-bind="args">
        <h3 style="margin: 0 0 0.5rem; font-weight: 600;">Research Details</h3>
        <p style="color: var(--color-text-muted); font-size: var(--type-sm); margin: 0 0 1rem;">
          A large modal for displaying detailed content, forms, or data tables.
        </p>
        <div style="background: var(--color-surface-hover); border-radius: var(--radius-sm); padding: 1rem; font-size: var(--type-sm); color: var(--color-text-muted);">
          <p style="margin: 0 0 0.5rem;">Lorem ipsum dolor sit amet, consectetur adipiscing elit. Sed do eiusmod tempor incididunt ut labore et dolore magna aliqua.</p>
          <p style="margin: 0;">Ut enim ad minim veniam, quis nostrud exercitation ullamco laboris nisi ut aliquip ex ea commodo consequat.</p>
        </div>
      </ModalOverlay>
    `,
  }),
}

/** `xl` is a fixed-height workspace rather than a box that grows with its
 *  content: always flush, panes scrolling independently. This is the shape the
 *  entry history panel sits in. */
export const ExtraLarge: Story = {
  args: { visible: true, size: 'xl', labelledby: 'modal-xl-title' },
  render: (args: any) => ({
    components: { ModalOverlay },
    setup() { return { args } },
    template: `
      <ModalOverlay v-bind="args">
        <header style="display: flex; align-items: center; justify-content: space-between; gap: 1rem; padding: 0.75rem 1.5rem; border-bottom: 1px solid var(--color-border);">
          <h2 id="modal-xl-title" style="margin: 0; font-size: var(--type-base); font-weight: 600;">History — E3 Vault layout</h2>
          <button class="btn btn-sm">Close</button>
        </header>
        <div style="flex: 1; display: grid; grid-template-columns: 280px 1fr; min-height: 0;">
          <ul style="list-style: none; margin: 0; padding: 0.75rem; overflow-y: auto; border-right: 1px solid var(--color-border); display: flex; flex-direction: column; gap: 0.5rem;">
            <li v-for="i in 14" :key="i" class="card" style="padding: 0.5rem 0.75rem; font-size: var(--type-sm);">Revision {{ 15 - i }}</li>
          </ul>
          <div style="overflow-y: auto; padding: 1rem 1.5rem; font-size: var(--type-sm); color: var(--color-text-muted);">
            <p v-for="i in 12" :key="i" style="margin: 0 0 0.75rem;">Each pane scrolls on its own, so the column rule runs the full height of the dialog and neither side squeezes the other.</p>
          </div>
        </div>
      </ModalOverlay>
    `,
  }),
}

export const Flush: Story = {
  args: { visible: true, size: 'lg', flush: true },
  render: (args: any) => ({
    components: { ModalOverlay },
    setup() { return { args } },
    template: `
      <ModalOverlay v-bind="args">
        <div style="padding: 0.75rem 1.5rem; border-bottom: 1px solid var(--color-border); display: flex; justify-content: space-between; align-items: center;">
          <span style="font-size: var(--type-sm); font-weight: 600; text-transform: uppercase; letter-spacing: 0.04em; color: var(--color-text-muted);">Modal Title</span>
          <span style="color: var(--color-text-muted); cursor: pointer;">&#x2715;</span>
        </div>
        <div style="padding: 1.25rem 1.5rem;">
          <p style="color: var(--color-text-muted); font-size: var(--type-sm); margin: 0;">Flush mode removes card padding so the header border extends edge-to-edge.</p>
        </div>
      </ModalOverlay>
    `,
  }),
}

/**
 * `initialFocus` names the control that should hold focus on open.
 *
 * Without it the overlay focuses the first focusable element, which in a dialog
 * with a header is the close button — and a child that focuses its own field in
 * its own `visible` watcher **loses the race**: the parent instance registers
 * first, so its continuation runs last and takes the focus back. A child cannot
 * win that on its own, which is why this is a prop.
 *
 * Open this story and start typing: the text lands in the field, not nowhere.
 */
export const InitialFocus: Story = {
  args: { visible: true, size: 'md', labelledby: 'modal-focus-title', initialFocus: '[data-confirm-code]' },
  render: (args: any) => ({
    components: { ModalOverlay },
    setup() { return { args } },
    template: `
      <ModalOverlay v-bind="args">
        <h2 id="modal-focus-title" style="margin: 0 0 var(--space-3); font-size: var(--type-md);">Delete project</h2>
        <button type="button" class="btn btn-sm" style="margin-bottom: var(--space-3);">A button that comes first in the DOM</button>
        <label for="sb-confirm" style="display: block; font-size: var(--type-2xs); font-weight: 600;">Type R3 to confirm</label>
        <input id="sb-confirm" data-confirm-code class="form-input" style="max-width: 12ch; font-family: 'JetBrains Mono', monospace;" />
      </ModalOverlay>
    `,
  }),
}

/**
 * A selector that matches nothing — a control renamed, or a dialog that renders
 * its field behind a `v-if` that is false on open.
 *
 * Focus falls back to the first focusable element, then to the card itself. It
 * is a chain rather than a lookup for exactly this reason: the failure mode of
 * "focus the named thing or nothing" is a dialog whose Escape key and Tab cycle
 * are both dead, because focus never entered it at all.
 */
export const InitialFocusNotFound: Story = {
  args: { visible: true, size: 'md', labelledby: 'modal-missing-title', initialFocus: '[data-nothing-here]' },
  render: (args: any) => ({
    components: { ModalOverlay },
    setup() { return { args } },
    template: `
      <ModalOverlay v-bind="args">
        <h2 id="modal-missing-title" style="margin: 0 0 var(--space-3); font-size: var(--type-md);">Nothing matches the selector</h2>
        <button type="button" class="btn btn-sm">This first button takes focus instead</button>
      </ModalOverlay>
    `,
  }),
}

export const Hidden: Story = {
  args: { visible: false },
  render: (args: any) => ({
    components: { ModalOverlay },
    setup() { return { args } },
    template: `
      <div>
        <p style="color: var(--color-text-muted); font-size: var(--type-sm);">Modal is hidden (visible=false). Nothing renders.</p>
        <ModalOverlay v-bind="args">
          <p>You should not see this.</p>
        </ModalOverlay>
      </div>
    `,
  }),
}
