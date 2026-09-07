<template>
  <Teleport to="body">
    <div v-if="visible" class="modal-overlay" @click.self="$emit('close')">
      <div
        ref="card"
        :class="['modal-card', sizeClass, { 'modal-flush': props.flush }]"
        role="dialog"
        aria-modal="true"
        :aria-labelledby="labelledby"
        tabindex="-1"
        @keydown.esc.stop="$emit('close')"
        @keydown.tab="trapFocus"
      >
        <slot />
      </div>
    </div>
  </Teleport>
</template>

<script setup lang="ts">
/**
 * The dialog shell every modal in the app sits in.
 *
 * It owns the keyboard contract, because a contract implemented once per modal
 * is a contract seven modals get wrong differently: Escape closes, focus moves
 * into the dialog on open and returns to the trigger on close, and Tab cycles
 * inside rather than walking the page behind — which, since the dialog is
 * teleported to the end of <body>, means walking the entire page.
 */
const props = defineProps<{
  visible: boolean
  size?: 'sm' | 'md' | 'lg' | 'xl'
  flush?: boolean // removes inner padding so slot content controls its own spacing
  /**
   * Id of the element that titles the dialog. Without it a screen reader
   * announces "dialog" and nothing else — and this cannot be passed through as a
   * fallthrough attribute, because the component's root is a Teleport.
   */
  labelledby?: string
  /**
   * Selector for the control that should hold focus when the dialog opens,
   * queried inside the card.
   *
   * Without it the overlay focuses the first focusable element, which in a
   * dialog with a header is the close button — and a child that focuses its own
   * field in its own `visible` watcher loses the race with this one: the parent
   * instance registers first, so its continuation runs last and moves focus
   * away again. A child cannot win that on its own, which is why this is a prop
   * rather than something each dialog solves for itself.
   */
  initialFocus?: string
}>()

defineEmits<{ close: [] }>()

const card = ref<HTMLElement | null>(null)
// The element that had focus when the dialog opened, so it can be given back.
let restoreTo: HTMLElement | null = null

const sizeClass = computed(() => {
  if (props.size === 'xl') return 'modal-xl'
  if (props.size === 'lg') return 'modal-lg'
  if (props.size === 'sm') return 'modal-sm'
  return ''
})

const FOCUSABLE = [
  'a[href]',
  'button:not([disabled])',
  'input:not([disabled])',
  'select:not([disabled])',
  'textarea:not([disabled])',
  '[tabindex]:not([tabindex="-1"])',
].join(',')

function focusable(): HTMLElement[] {
  if (!card.value) return []
  return Array.from(card.value.querySelectorAll<HTMLElement>(FOCUSABLE))
    .filter((el) => el.offsetParent !== null || el === document.activeElement)
}

function trapFocus(event: KeyboardEvent) {
  const items = focusable()
  if (items.length === 0) {
    // Nothing to move to; keep focus on the dialog rather than letting Tab
    // escape into the page behind it.
    event.preventDefault()
    card.value?.focus()
    return
  }
  const first = items[0]!
  const last = items[items.length - 1]!
  const active = document.activeElement as HTMLElement | null

  if (event.shiftKey && (active === first || active === card.value)) {
    event.preventDefault()
    last.focus()
  } else if (!event.shiftKey && active === last) {
    event.preventDefault()
    first.focus()
  }
}

/* The page behind a dialog scrolled under the wheel, which reads as the dialog
   having lost its grip on the page.
   The class, not `body.style.overflow`: an inline release writes a blanket `''`
   and would clear a lock somebody else had taken. `body.scroll-locked` lives in
   base.css and is shared with ArtifactFrame's fullscreen overlay. */
function lockScroll(on: boolean) {
  if (import.meta.server) return
  document.body.classList.toggle('scroll-locked', on)
}

watch(
  () => props.visible,
  async (open) => {
    lockScroll(open)
    if (open) {
      restoreTo = document.activeElement as HTMLElement | null
      await nextTick()
      // The named control if the caller asked for one, else the first, else the
      // dialog itself — either way focus is inside, so the first Tab continues
      // from here.
      const items = focusable()
      const wanted = props.initialFocus
        ? card.value?.querySelector<HTMLElement>(props.initialFocus)
        : null
      ;(wanted ?? items[0] ?? card.value)?.focus()
      return
    }
    restoreTo?.focus?.()
    restoreTo = null
  },
)

onBeforeUnmount(() => {
  lockScroll(false)
  restoreTo?.focus?.()
})
</script>

<style scoped>
.modal-overlay {
  position: fixed;
  inset: 0;
  background: var(--color-backdrop);
  backdrop-filter: blur(4px);
  display: flex;
  align-items: center;
  justify-content: center;
  z-index: var(--z-overlay);
}
.modal-card {
  /* Only the lg and xl sizes carried these, and the overlay centres its child —
     so a `sm` confirm with a long message, or a form on a 375x667 screen with
     the keyboard up, was cut off at the *top*, where nothing can scroll to it. */
  max-height: calc(100dvh - var(--space-8));
  overflow-y: auto;
  background: var(--color-surface);
  border: 1px solid var(--color-border-strong);
  border-radius: var(--radius-lg);
  padding: var(--space-6);
  width: 100%;
  max-width: 460px;
  box-shadow: var(--shadow-3);
}
/* The dialog takes focus when it holds no controls; that is a programmatic
   focus, not a keyboard one, so it should not paint a ring. */
.modal-card:focus {
  outline: none;
}
.modal-lg {
  max-width: 720px;
  max-height: 85vh;
  overflow-y: auto;
}
.modal-sm { max-width: 360px; }

/* A fixed-height workspace rather than a box that grows with its content: the
   panes inside scroll independently, so a column rule runs the full height and
   neither pane is squeezed by the other. Always flush — an xl dialog owns its
   own spacing. */
.modal-xl {
  max-width: min(1040px, calc(100vw - var(--space-8)));
  height: 85vh;
  padding: 0;
  display: flex;
  flex-direction: column;
  overflow: hidden;
}
.modal-flush { padding: 0; }

/* Responsive */
@media (max-width: 768px) {
  .modal-overlay { padding: var(--space-4); }
  .modal-card {
  /* Only the lg and xl sizes carried these, and the overlay centres its child —
     so a `sm` confirm with a long message, or a form on a 375x667 screen with
     the keyboard up, was cut off at the *top*, where nothing can scroll to it. */
  max-height: calc(100dvh - var(--space-8));
  overflow-y: auto; max-width: 100%; padding: var(--space-4); }
  .modal-lg { max-height: calc(100dvh - var(--space-8)); }
  .modal-xl {
    max-width: 100%;
    height: calc(100dvh - var(--space-8));
  }
}
</style>
