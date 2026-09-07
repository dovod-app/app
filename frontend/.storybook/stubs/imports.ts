import { ref, computed, isRef, watch, toValue } from 'vue'
import { resolveMockApiData, runMockFetch } from '../../__mocks__/api'

export {
  ref,
  computed,
  reactive,
  watch,
  watchEffect,
  onMounted,
  onUnmounted,
  onBeforeMount,
  onBeforeUnmount,
  nextTick,
  provide,
  inject,
  toRef,
  toRefs,
  unref,
  isRef,
  shallowRef,
  triggerRef,
  defineProps,
  defineEmits,
  defineExpose,
  withDefaults,
} from 'vue'

export { useRoute, useRouter } from 'vue-router'

// Nuxt-specific stubs
export const navigateTo = (path: string) => {
  console.log('[Storybook stub] navigateTo:', path)
}
export const useRuntimeConfig = () => ({
  public: { apiBase: '' },
})
export const useFetch = () => ({ data: ref(null), pending: ref(false), error: ref(null) })
export const useAsyncData = () => ({ data: ref(null), pending: ref(false), error: ref(null) })
export const useCookie = (_name: string) => ref('')
export const useHead = () => {}
export const useSeoMeta = () => {}
export const definePageMeta = () => {}

// Project composable stubs
//
// The URL used to be ignored and `data` was always null. It is read now so a
// story can answer a component that fetches for itself, through
// `mockApiData()` in __mocks__/api.ts. Unrouted URLs still resolve to null, so
// every story written before this behaves exactly as it did.
export const useApi = (url: any) => {
  const data = ref<any>(null)
  const read = () => {
    const resolved = toValue(url)
    data.value = typeof resolved === 'string' ? resolveMockApiData(resolved) : null
  }
  read()
  // The real composable refetches when a computed URL changes — SearchModal's
  // query is one — so the stub has to as well, or a story can only ever show
  // the first keystroke's results.
  if (isRef(url) || typeof url === 'function') watch(() => toValue(url), read)
  return {
    data,
    pending: ref(false),
    error: ref(null),
    refresh: () => {
      read()
      return Promise.resolve()
    },
  }
}

/**
 * What the server says about itself.
 *
 * `writeApi` must be `true` here. `useResearchRole().canWrite` returns it when
 * `authEnabled` is false, which is the posture every story runs in — a stub
 * returning false would not crash anything, it would quietly render the whole
 * catalogue in its viewer variant, with no edit control anywhere and no failing
 * assertion to say so. RoadmapNodePopover.stories.ts documents its own reliance
 * on this being true.
 */
export const useServerInfo = () => ({
  info: ref({ status: 'ok', version: 'dev', in_memory: true, write_api: true, auth_enabled: false }),
  writeApi: ref(true),
  version: ref('dev'),
  load: () => Promise.resolve(),
})

export const useAuth = () => ({
  user: ref(null),
  token: ref(null),
  authEnabled: ref(false),
  allowRegistration: ref(true),
  loading: ref(false),
  isAuthenticated: computed(() => false),
  fetchAuthInfo: () => Promise.resolve(),
  checkAuth: () => Promise.resolve(),
  login: () => Promise.resolve(),
  register: () => Promise.resolve(),
  logout: () => {},
  // Routed through __mocks__/api so a story can render a component that fetches
  // its own data. Unrouted URLs resolve to {}, as before.
  authFetch: (url: string, opts?: any) => runMockFetch(url, opts),
})

export const renderRefs = (text: string, _slug?: string) => text ?? ''
export const useCrossRefs = () => ({ renderRefs })

export const useRealtimeUpdates = (_handler?: any) => {}
export const useKeyboardNav = () => {}
