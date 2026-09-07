import type { StorybookConfig } from '@storybook/vue3-vite'
import { mergeConfig } from 'vite'
import path from 'path'

const config: StorybookConfig = {
  stories: ['../components/**/*.stories.ts'],
  addons: [
    '@storybook/addon-essentials',
    '@storybook/addon-interactions',
  ],
  framework: {
    name: '@storybook/vue3-vite',
    options: {},
  },
  docs: {
    autodocs: 'tag',
  },
  staticDirs: ['../public'],
  viteFinal: async (config) => {
    const AutoImport = (await import('unplugin-auto-import/vite')).default
    const vue = (await import('@vitejs/plugin-vue')).default

    return mergeConfig(config, {
      resolve: {
        alias: {
          '~': path.resolve(__dirname, '..'),
          '@': path.resolve(__dirname, '..'),
          '#imports': path.resolve(__dirname, './stubs/imports.ts'),
          '#app': path.resolve(__dirname, './stubs/app.ts'),
          '#components': path.resolve(__dirname, './stubs/components.ts'),
        },
      },
      plugins: [
        vue(),
        AutoImport({
          imports: [
            'vue',
            'vue-router',
            {
              // `renderRefs` escapes its input; `linkRefs` is the variant for a
              // caller that already holds HTML. A story reaching for the wrong
              // one is exactly the mistake worth catching in the catalogue.
              [path.resolve(__dirname, '../composables/useCrossRefs')]: ['renderRefs', 'linkRefs'],
              [path.resolve(__dirname, '../utils/escapeHtml')]: ['escapeHtml'],
              [path.resolve(__dirname, '../composables/useTagHue')]: ['tagHue'],
              // Pure formatting over a string — the real one, so a card in the
              // catalogue reads the same "2h ago" the product does.
              [path.resolve(__dirname, '../composables/useRelativeTime')]: [
                'relativeTime',
                'absoluteTime',
                'parseTimestamp',
              ],
              // Module-scoped role state. The stories set it through the real
              // composable so a viewer story renders the viewer's card.
              [path.resolve(__dirname, '../composables/useResearchRole')]: ['useResearchRole'],
              // Where a link inside a research points. Real, not stubbed: half
              // the catalogue is components whose only job is to link, and a
              // stub would let a wrong href through the one place that checks.
              [path.resolve(__dirname, '../composables/useResearchPaths')]: [
                'researchPath',
                'entryPath',
                'sessionPath',
                // A mind-map question or answer node navigates through this
                // and imports nothing. It is called from a click handler
                // rather than from render, so an unresolved name is not a
                // blank card — it is a ReferenceError the first time somebody
                // clicks one, which is the state the mind map is opened in.
                'questionPath',
                'graphPath',
                'mindmapPath',
                'roadmapPath',
                'roadmapsPath',
                'tasksPath',
                'taskPath',
                'exportPath',
                'foreignResearchPath',
              ],
              // Module-scoped share state, read by the path helpers above, by
              // `renderRefs`, and by three components that render a target
              // outside the share as inert text. Stories set it through
              // `withShare()` in __mocks__/share.ts.
              [path.resolve(__dirname, '../composables/useShare')]: [
                'useShare',
                'shareActive',
                'shareToken',
                'shareInclude',
                'shareResearchCode',
                // Whether a link is still live. The share row list and the
                // share dialog both take it as a value — `const isLive =
                // isShareLive` — rather than calling it, which is why
                // lint-frontend.py never reported it: that check looks for
                // `name(`. Without it here every story of both components died
                // on a ReferenceError in setup and rendered nothing at all.
                'isShareLive',
              ],
              // Real composable, not a stub: it is module-scoped state plus
              // vue refs, so a component that raises a toast raises a real one.
              [path.resolve(__dirname, '../composables/useToasts')]: ['useToasts'],
              [path.resolve(__dirname, './stubs/imports')]: [
                'useApi',
                'useAuth',
                // useResearchRole calls this, so it has to resolve here even
                // though no component names it: an unresolved identifier inside
                // a composable throws in setup() and the story renders blank.
                'useServerInfo',
                'useRuntimeConfig',
                'useRealtimeUpdates',
                'useKeyboardNav',
                'navigateTo',
                'useFetch',
                'useAsyncData',
                'useCookie',
                'useHead',
                'definePageMeta',
              ],
            },
          ],
          dts: false,
        }),
      ],
    })
  },
}
export default config
