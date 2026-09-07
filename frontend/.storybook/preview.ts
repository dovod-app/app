import { setTheme } from '../composables/useTheme'
import type { Preview } from '@storybook/vue3'
import { setup } from '@storybook/vue3'
import { createMemoryHistory, createRouter } from 'vue-router'
import { INITIAL_VIEWPORTS } from '@storybook/addon-viewport'
import ModalOverlay from '../components/ModalOverlay.vue'
import EmptyState from '../components/EmptyState.vue'
import ActivityIndicator from '../components/ActivityIndicator.vue'
import BlocksBlockRenderer from '../components/blocks/BlockRenderer.vue'
import BlocksTaskRefBlock from '../components/blocks/TaskRefBlock.vue'
import BlocksTranscriptBlock from '../components/blocks/TranscriptBlock.vue'
import ProgressBar from '../components/ProgressBar.vue'
import EntryArtifactFrame from '../components/entry/ArtifactFrame.vue'
import CopyableSecret from '../components/CopyableSecret.vue'
import ResearchShareRowList from '../components/research/ShareRowList.vue'
import ResearchShareIncludeFields from '../components/research/ShareIncludeFields.vue'
import ThemeToggle from '../components/ThemeToggle.vue'
import EntryDiffView from '../components/entry/DiffView.vue'
import EntryAuthorBadge from '../components/entry/AuthorBadge.vue'
import EntryFieldChanges from '../components/entry/FieldChanges.vue'
import EntryRevisionRow from '../components/entry/RevisionRow.vue'
import EntryUpdateBadge from '../components/entry/UpdateBadge.vue'
import ResearchUpdatesRow from '../components/research/UpdatesRow.vue'
import ShortCode from '../components/ShortCode.vue'
import ActionMenu from '../components/ActionMenu.vue'
import ConfirmModal from '../components/ConfirmModal.vue'
import TeamChip from '../components/team/TeamChip.vue'
import TeamViewerNotice from '../components/team/ViewerNotice.vue'
import ModalHeader from '../components/ModalHeader.vue'
import TeamRoleSelect from '../components/team/RoleSelect.vue'
import StatusBadge from '../components/StatusBadge.vue'
import TagList from '../components/TagList.vue'
import EntryCard from '../components/EntryCard.vue'
import ResearchResumeRow from '../components/research/ResumeRow.vue'
import ResearchEntriesToolbar from '../components/research/EntriesToolbar.vue'
import SegmentedToggle from '../components/SegmentedToggle.vue'
import ResearchImportDropZone from '../components/research/ImportDropZone.vue'
import ResearchImportPreviewDialog from '../components/research/ImportPreviewDialog.vue'
import ResearchImportNoteGroup from '../components/research/ImportNoteGroup.vue'
import EditableField from '../components/EditableField.vue'
import AnnotationsKindChip from '../components/annotations/KindChip.vue'
import AnnotationsAnchorBadge from '../components/annotations/AnchorBadge.vue'
import AnnotationsAnnotationRow from '../components/annotations/AnnotationRow.vue'
import AnnotationsAnnotationList from '../components/annotations/AnnotationList.vue'
import ResearchSectionInstruction from '../components/research/SectionInstruction.vue'
import ResearchSettingsInstructionEditor from '../components/research/settings/InstructionEditor.vue'
import { resetMockApi, resetMockApiData } from '../__mocks__/api'
import { resetMockDownload } from '../__mocks__/download'
import { resetMockAuth } from '../__mocks__/auth'
import '../assets/css/tokens.css'
import '../assets/css/base.css'
import '../assets/css/brand.css'
import '../assets/css/system.css'
import '../assets/css/markdown.css'
import '../assets/css/mermaid.css'

const router = createRouter({
  history: createMemoryHistory(),
  routes: [
    { path: '/', component: { template: '<div>Home</div>' } },
    { path: '/:catchAll(.*)', component: { template: '<div>Page</div>' } },
  ],
})

setup((app) => {
  app.use(router)

  // Nuxt component stubs
  app.component('NuxtLink', {
    props: ['to', 'href'],
    // RouterLink understands both string and `{ path, query }` destinations,
    // so stories can verify exact deep links instead of rendering
    // `href="[object Object]"` for the latter.
    template: '<RouterLink v-if="to" :to="to"><slot /></RouterLink><a v-else :href="href"><slot /></a>',
  })
  app.component('ClientOnly', {
    template: '<slot />',
  })
  app.component('NuxtPage', {
    template: '<div>[NuxtPage stub]</div>',
  })
  // Render Teleport content inline for modal stories
  app.component('Teleport', {
    template: '<slot />',
  })

  // Nuxt auto-registers components by their path-derived name; Storybook does
  // not. These are the ones a rendered component looks up inside its own
  // template, under the exact name Nuxt would give them.
  app.component('ModalOverlay', ModalOverlay)
  app.component('EmptyState', EmptyState)
  app.component('EntryDiffView', EntryDiffView)
  app.component('EntryAuthorBadge', EntryAuthorBadge)
  app.component('EntryFieldChanges', EntryFieldChanges)
  app.component('EntryRevisionRow', EntryRevisionRow)
  app.component('EntryUpdateBadge', EntryUpdateBadge)
  app.component('ResearchUpdatesRow', ResearchUpdatesRow)
  app.component('ShortCode', ShortCode)
  app.component('ActionMenu', ActionMenu)
  // Three components raise a confirmation through this without importing it,
  // and FieldSpecList is the newest. Unregistered, the destructive button is still
  // there and still pressable and simply nothing happens — a delete flow whose
  // catalogue entry says the confirmation does not exist.
  app.component('ConfirmModal', ConfirmModal)
  // ResearchCard names both of these in its footer, under the folder-prefixed
  // name Nuxt derives (`team/TeamChip.vue` deduplicates to `TeamChip`). Without
  // them the four stories written to show a shared team, and the one written to
  // show a viewer's read-only marker, rendered a card with an empty tag row —
  // each silently documenting the opposite of its own doc comment.
  app.component('TeamChip', TeamChip)
  app.component('TeamViewerNotice', TeamViewerNotice)
  app.component('ModalHeader', ModalHeader)
  app.component('TeamRoleSelect', TeamRoleSelect)
  app.component('StatusBadge', StatusBadge)
  app.component('TagList', TagList)
  // ResumeBlock renders its rows through this; without it the Continue block
  // would render as a head with nothing under it and no error to say why.
  app.component('ResearchResumeRow', ResearchResumeRow)
  // EntriesView renders its list through EntryCard and its filter row through
  // the toolbar; without these two the grouped stories were section headings
  // over nothing, and the catalogue said so to nobody.
  app.component('EntryCard', EntryCard)
  app.component('ResearchEntriesToolbar', ResearchEntriesToolbar)
  // Four components reach for this by name without importing it — the two
  // roadmap toggles, and now the section view switch in EntriesView. Unregistered,
  // Vue resolves nothing and the control silently does not render, which is the
  // one failure mode a catalogue must not have.
  app.component('SegmentedToggle', SegmentedToggle)
  app.component('ActivityIndicator', ActivityIndicator)
  app.component('BlocksBlockRenderer', BlocksBlockRenderer)
  // BlockRenderer delegates two block types to components it looks up by the
  // folder-prefixed name Nuxt derives — a `task_ref` and a `transcript`. Both
  // are the ea31ebb failure if left unregistered: the branch matches, Vue
  // resolves nothing, and the document renders with the block simply absent.
  app.component('BlocksTaskRefBlock', BlocksTaskRefBlock)
  app.component('BlocksTranscriptBlock', BlocksTranscriptBlock)
  // TaskRefBlock draws its completion count with this and imports nothing.
  app.component('ProgressBar', ProgressBar)
  // BlockRenderer reaches for this by name for an `html` block, so without it
  // the WithHtmlBlock story rendered an unresolved component and showed nothing.
  app.component('EntryArtifactFrame', EntryArtifactFrame)
  app.component('CopyableSecret', CopyableSecret)
  app.component('ResearchShareRowList', ResearchShareRowList)
  // ShareDialog draws the same four checkboxes in its create form and its edit
  // form through this one component, by the folder-prefixed name Nuxt derives.
  // Unregistered, both forms lose the only control that says what the link
  // shows — and the dialog still renders, which is the ea31ebb failure exactly.
  app.component('ResearchShareIncludeFields', ResearchShareIncludeFields)
  // The share banner, the password gate and the standalone share screens each
  // reach for this by name — it is the only theme control a visitor has, and a
  // visitor has no other chrome to find one in.
  app.component('ThemeToggle', ThemeToggle)
  // EntriesView reaches for both of these by name.
  app.component('ResearchImportDropZone', ResearchImportDropZone)
  app.component('ResearchImportPreviewDialog', ResearchImportPreviewDialog)
  app.component('ResearchImportNoteGroup', ResearchImportNoteGroup)
  // BUG, not a Storybook quirk: ImportPreviewDialog's template says

  // The annotation components lean on each other four deep — PassReviewModal
  // draws AnnotationList, which draws AnnotationRow, which draws KindChip and
  // AnchorBadge — and every one of those lookups is by the folder-prefixed name
  // Nuxt derives. Unregistered they resolve to nothing and the review modal
  // renders an empty rail, which is the failure that took the catalogue out in
  // ea31ebb.
  app.component('AnnotationsKindChip', AnnotationsKindChip)
  app.component('AnnotationsAnchorBadge', AnnotationsAnchorBadge)
  app.component('AnnotationsAnnotationRow', AnnotationsAnnotationRow)
  app.component('AnnotationsAnnotationList', AnnotationsAnnotationList)
  // ThreadCard puts the note behind this rather than a bare textarea.
  app.component('EditableField', EditableField)
  // The section instruction, on both surfaces that carry it, by the
  // folder-prefixed name Nuxt derives. EntriesView draws the read block above
  // the documents and FieldSpecList draws the editor under the field rows —
  // and both are behind a `v-if`, so unregistered they are not an error but a
  // section whose instruction has silently vanished from the catalogue. That
  // is ea31ebb, and `mockSpecSection` now carries an instruction, so three
  // existing EntriesView stories were already in that state.
  app.component('ResearchSectionInstruction', ResearchSectionInstruction)
  app.component('ResearchSettingsInstructionEditor', ResearchSettingsInstructionEditor)
})

const preview: Preview = {
  globalTypes: {
    theme: {
      description: 'Dovod color theme',
      toolbar: { icon: 'paintbrush', items: [{ value: 'light', title: 'Light' }, { value: 'dark', title: 'Dark' }], dynamicTitle: true },
    },
  },
  initialGlobals: { theme: 'light' },
  parameters: {
    backgrounds: { disable: true },
    layout: 'padded',
    viewport: {
      viewports: {
        mobile: {
          name: 'Mobile',
          styles: { width: '375px', height: '812px' },
        },
        mobileLarge: {
          name: 'Mobile Large',
          styles: { width: '414px', height: '896px' },
        },
        tablet: {
          name: 'Tablet',
          styles: { width: '768px', height: '1024px' },
        },
        desktop: {
          name: 'Desktop',
          styles: { width: '1280px', height: '800px' },
        },
        ...INITIAL_VIEWPORTS,
      },
    },
    controls: {
      matchers: {
        color: /(background|color)$/i,
        date: /Date$/i,
      },
    },
    docs: {
      toc: true,
    },
  },
  decorators: [
    (story, context) => {
      setTheme(context.globals.theme === 'dark' ? 'dark' : 'light', false)
      return ({
      components: { story },
      // A decorator's setup runs before the story's, and the story's before the
      // component's — so this clears the `useApi` routing table in time for a
      // story to install its own, and in time for a story that installs none to
      // see nothing. Module state that outlives one story is how a catalogue
      // starts depending on the order it was clicked through in. `authFetch`'s
      // table is cleared here for the same reason: a story that routes a write
      // to a request which never settles used to leave every later story's
      // checkbox hanging.
      setup: () => {
        resetMockApi()
        resetMockApiData()
        resetMockDownload()
        resetMockAuth()
      },
      template: `
        <div style="background: var(--color-bg); color: var(--color-text); padding: 1.5rem; font-family: 'Outfit', system-ui, sans-serif;">
          <story />
        </div>
      `,
    })
    },
  ],
}
export default preview
