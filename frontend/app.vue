<script setup lang="ts">
import { useKeyboardNav } from '~/composables/useKeyboardNav'
import { useTheme } from '~/composables/useTheme'
import { readThemeColors } from '~/utils/theme'

useHead({ titleTemplate: title => title && title !== 'Dovod' ? `${title} — Dovod` : 'Dovod' })
const { theme } = useTheme()
useHead(() => {
  // Depend on the preference as well as the resolved CSS on first load.
  void theme.value
  return { meta: [{ name: 'theme-color', content: readThemeColors().background }] }
})

const hasRecentUpdate = ref(false)
let activityTimer: ReturnType<typeof setTimeout>

// The blip means "somebody else changed something". It used to fire on your own
// writes too, which made it noise: the one event you already know about was the
// one it reported loudest.
useRealtimeUpdates((event) => {
  if (isSelf(event)) return
  hasRecentUpdate.value = true
  clearTimeout(activityTimer)
  activityTimer = setTimeout(() => {
    hasRecentUpdate.value = false
  }, 5000)
})

const { active: revoked } = useAccessRevoked()

// The state machine — grace window, escalation, the toast — lives in the
// composable; the footer indicator is handed its verdict and renders it.
const {
  display: connection,
  reason: connectionReason,
  lastSyncedAt,
  retryNow,
} = useConnectionBanner()

useKeyboardNav()

const { user, isAuthenticated, authEnabled, logout } = useAuth()
const { version, writeApi } = useServerInfo()

// Same sentence TeamViewerNotice gives for its `remote` reason. The nav badge
// is the only explanation on pages that are not research-scoped.
const readOnlyExplanation =
  'This server accepts changes only from the machine it runs on. To edit from here, turn on accounts with auth_enabled, or connect a tool with the api_token.'

const route = useRoute()
// Pages that render without nav or footer. The invitation page joins them
// because its reader may have no account at all, and a nav bar full of links
// they cannot follow is a worse welcome than none.
// A shared view joins them for the strongest version of the same reason: its
// reader has no account at all, and the nav's search box, team menu and
// research list are not merely unusable to them — every one of them reaches
// past the single research the link was for.
const isChromeless = computed(
  () => route.path === '/login' || route.path === '/register'
    || route.path.startsWith('/invite/') || route.path.startsWith('/s/')
    // The API reference renders signed out, so the nav's search box, activity
    // indicator and user menu are all unusable to its reader — the same reason
    // the invitation and share pages are chromeless. It also carries its own
    // sticky sidebar, which does not stack under a sticky nav.
    || isApiDocsPath(route.path),
)

const userMenuOpen = ref(false)

function toggleUserMenu() {
  userMenuOpen.value = !userMenuOpen.value
}

function closeUserMenu() {
  userMenuOpen.value = false
}

function handleLogout() {
  userMenuOpen.value = false
  logout()
}

/**
 * A file dropped anywhere but a real target does nothing.
 *
 * The browser's default for a dropped file is to navigate to it, which in a
 * single-page app means the whole application is gone and replaced by
 * `file:///…`. That was live the moment section import shipped: the drop zone
 * covers the entries list, and the sidebar section name — the most natural
 * place to aim "put this document in that section" — is not it.
 *
 * Capture phase and a `defaultPrevented` check, so a real target still wins:
 * `ImportDropZone` calls `preventDefault` on its own handlers, and this only
 * refuses what nothing else claimed.
 */
function swallowStrayDrop(e: DragEvent) {
  if (e.defaultPrevented) return
  e.preventDefault()
  if (e.type === 'drop' && e.dataTransfer) e.dataTransfer.dropEffect = 'none'
}

onMounted(() => {
  document.addEventListener('click', (e) => {
    const el = (e.target as HTMLElement).closest('.user-menu')
    if (!el) userMenuOpen.value = false
  })
  // Bubble phase, so a zone that handled it has already marked the event.
  window.addEventListener('dragover', swallowStrayDrop)
  window.addEventListener('drop', swallowStrayDrop)
})

onUnmounted(() => {
  window.removeEventListener('dragover', swallowStrayDrop)
  window.removeEventListener('drop', swallowStrayDrop)
})
</script>

<template>
  <div>
    <!-- Every page in this app awaits its data at the top level, which makes it
         a Suspense boundary: the page you came from stays on screen until the
         new one resolves, and each page's own `v-if="pending"` skeleton is
         already false by the time its template first runs. So a navigation was
         several sequential round trips of nothing happening at all. This is the
         one feedback that does not require restructuring those awaits. -->
    <NuxtLoadingIndicator :color="'var(--color-primary)'" />

    <!-- One host for the whole app, outside the page chrome so a notification
         survives navigation and shows on the auth pages too. -->
    <ToastHost />

    <!-- Auth and invitation pages: no nav, no footer, no container -->
    <template v-if="isChromeless">
      <NuxtPage />
    </template>

    <!-- Normal pages: full chrome -->
    <template v-else>
      <div class="app-shell">
        <a href="#main" class="skip-link">Skip to content</a>
        <nav class="app-nav">
          <div class="container nav-inner">
            <NuxtLink :to="{ name: 'index' }" class="logo" aria-label="Dovod home"><BrandLogo /></NuxtLink>
            <div class="nav-right">
              <ThemeToggle />
              <SearchModal />
              <ActivityIndicator :active="hasRecentUpdate" label="Updating" />
              <template v-if="authEnabled && isAuthenticated">
                <div class="user-menu">
                  <button class="user-menu-trigger" @click="toggleUserMenu">
                    <span class="user-avatar">{{ (user?.name || user?.email || '?')[0].toUpperCase() }}</span>
                    <span class="user-name">{{ user?.name || user?.email }}</span>
                    <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><polyline points="6 9 12 15 18 9"/></svg>
                  </button>
                  <div v-if="userMenuOpen" class="user-dropdown">
                    <NuxtLink to="/templates" class="user-dropdown-item" @click="closeUserMenu">
                      <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M4 19.5A2.5 2.5 0 0 1 6.5 17H20"/><path d="M6.5 2H20v20H6.5A2.5 2.5 0 0 1 4 19.5v-15A2.5 2.5 0 0 1 6.5 2z"/></svg>
                      Methodologies
                    </NuxtLink>
                    <NuxtLink to="/teams" class="user-dropdown-item" @click="closeUserMenu">
                      <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M17 21v-2a4 4 0 0 0-4-4H5a4 4 0 0 0-4 4v2"/><circle cx="9" cy="7" r="4"/><path d="M23 21v-2a4 4 0 0 0-3-3.87"/><path d="M16 3.13a4 4 0 0 1 0 7.75"/></svg>
                      Teams
                    </NuxtLink>
                    <NuxtLink to="/settings" class="user-dropdown-item" @click="closeUserMenu">
                      <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="3"/><path d="M19.4 15a1.65 1.65 0 0 0 .33 1.82l.06.06a2 2 0 0 1 0 2.83 2 2 0 0 1-2.83 0l-.06-.06a1.65 1.65 0 0 0-1.82-.33 1.65 1.65 0 0 0-1 1.51V21a2 2 0 0 1-2 2 2 2 0 0 1-2-2v-.09A1.65 1.65 0 0 0 9 19.4a1.65 1.65 0 0 0-1.82.33l-.06.06a2 2 0 0 1-2.83 0 2 2 0 0 1 0-2.83l.06-.06A1.65 1.65 0 0 0 4.68 15a1.65 1.65 0 0 0-1.51-1H3a2 2 0 0 1-2-2 2 2 0 0 1 2-2h.09A1.65 1.65 0 0 0 4.6 9a1.65 1.65 0 0 0-.33-1.82l-.06-.06a2 2 0 0 1 0-2.83 2 2 0 0 1 2.83 0l.06.06A1.65 1.65 0 0 0 9 4.68a1.65 1.65 0 0 0 1-1.51V3a2 2 0 0 1 2-2 2 2 0 0 1 2 2v.09a1.65 1.65 0 0 0 1 1.51 1.65 1.65 0 0 0 1.82-.33l.06-.06a2 2 0 0 1 2.83 0 2 2 0 0 1 0 2.83l-.06.06a1.65 1.65 0 0 0-.33 1.82V9a1.65 1.65 0 0 0 1.51 1H21a2 2 0 0 1 2 2 2 2 0 0 1-2 2h-.09a1.65 1.65 0 0 0-1.51 1z"/></svg>
                      Settings
                    </NuxtLink>
                    <div class="user-dropdown-divider"></div>
                    <button class="user-dropdown-item user-dropdown-danger" @click="handleLogout">
                      <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M9 21H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h4"/><polyline points="16 17 21 12 16 7"/><line x1="21" y1="12" x2="9" y2="12"/></svg>
                      Sign out
                    </button>
                  </div>
                </div>
              </template>
              <!--
                Only when the server will actually refuse a write. This badge
                used to appear whenever accounts were off, including on a local
                run where every edit control beside it worked — a label that
                contradicted the buttons under it. Now it says what /api/health
                says about this caller, and it says why: the word on its own is
                the one thing a reader cannot act on, and a screen reader gets
                nothing else anywhere on the page.
              -->
              <span v-else-if="!authEnabled && !writeApi" class="readonly-badge" :title="readOnlyExplanation">
                Read-only
                <span class="sr-only">. {{ readOnlyExplanation }}</span>
              </span>
            </div>
          </div>
        </nav>

        <main id="main" class="container main-content" tabindex="-1">
          <WarningBanner />
          <!-- The page's subject was taken away while it was open. Rendering it
               would show a research the next request will refuse. -->
          <AccessRevokedNotice v-if="revoked" :revocation="revoked" />
          <NuxtPage v-else />
        </main>

        <footer class="app-footer">
          <div class="container footer-inner">
            <!-- The product name said nothing the logo two lines up does not
                 already say. The build is the one fact worth a permanent slot:
                 it is what somebody reporting a problem is asked for first. -->
            <span class="card-meta app-version" :title="version ? `Running build ${version}` : ''">{{ version }}</span>
            <div class="footer-right">
              <NuxtLink :to="API_DOCS_PATH" class="footer-link card-meta">API reference</NuxtLink>
              <ConnectionStatus
                :state="connection"
                :reason="connectionReason"
                :last-synced-at="lastSyncedAt"
                @retry="retryNow"
              />
            </div>
          </div>
        </footer>
      </div>
    </template>
  </div>
</template>

<style>
.nav-inner  { display: flex; align-items: center; justify-content: space-between; }
.nav-right  { display: flex; align-items: center; gap: var(--space-3); }
/* The footer sits at the bottom of a short page without the page being taller
   than the window.
   `min-height: calc(100dvh - 120px)` on the main column was a guess at how tall
   the chrome is, and the chrome had outgrown it: nav 53px + main padding 32px +
   footer 69px is 154px, so every page — including one with three cards on it —
   was 34px taller than the viewport and carried a scrollbar that had nothing to
   scroll. A flex column measures itself instead of being told. */
.app-shell { display: flex; flex-direction: column; min-height: 100dvh; }
.app-shell > .main-content { flex: 1; }
.main-content { padding-top: var(--space-4); padding-bottom: var(--space-4); }
.app-footer {
  border-top: 1px solid var(--color-border);
  padding: var(--space-4) 0;
  margin-top: var(--space-4);
}
.footer-inner { display: flex; align-items: center; justify-content: space-between; }
.app-version { font-family: 'JetBrains Mono', monospace; font-size: var(--type-xs); }
.footer-right { display: flex; align-items: center; gap: var(--space-4); }
.footer-link  { text-decoration: none; transition: color var(--transition-fast); }
.footer-link:hover { color: var(--color-primary); text-decoration: none; }

/* Responsive nav */
@media (max-width: 768px) {
  .nav-right { gap: var(--space-2); }
  .user-name { display: none; }
}
@media (max-width: 480px) {
  .nav-right { gap: var(--space-1); }
}

/* User dropdown menu */
.user-menu {
  position: relative;
}
.user-menu-trigger {
  display: flex;
  align-items: center;
  gap: var(--space-2);
  background: none;
  border: 1px solid transparent;
  color: var(--color-text-muted);
  cursor: pointer;
  font-family: inherit;
  font-size: var(--type-sm);
  padding: var(--space-1) var(--space-2);
  border-radius: var(--radius-sm);
  transition: all var(--transition-fast);
}
.user-menu-trigger:hover {
  color: var(--color-text);
  border-color: var(--color-border);
}
.user-name {
  max-width: 120px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.user-dropdown {
  position: absolute;
  right: 0;
  top: calc(100% + var(--space-1));
  min-width: 180px;
  background: var(--color-surface);
  border: 1px solid var(--color-border-strong);
  border-radius: var(--radius);
  padding: var(--space-1);
  box-shadow: var(--shadow-2);
  z-index: 100;
  animation: dropdown-in 0.12s ease both;
}
@keyframes dropdown-in {
  from { opacity: 0; transform: translateY(-4px); }
  to { opacity: 1; transform: translateY(0); }
}
.user-dropdown-item {
  display: flex;
  align-items: center;
  gap: var(--space-2);
  width: 100%;
  padding: var(--space-2) var(--space-3);
  border: none;
  background: none;
  color: var(--color-text-muted);
  font-family: inherit;
  font-size: var(--type-sm);
  cursor: pointer;
  border-radius: var(--radius-sm);
  text-decoration: none;
  transition: all var(--transition-fast);
}
.user-dropdown-item:hover {
  background: var(--color-surface-hover);
  color: var(--color-text);
}
.user-dropdown-danger:hover {
  color: var(--color-danger, #dc2626);
}
.user-dropdown-divider {
  height: 1px;
  background: var(--color-border);
  margin: var(--space-1) var(--space-2);
}
</style>

<style scoped>
/* Moved out of the global stylesheet: these style this component and
   nothing else, and they were a directory away from the markup they
   describe. What stays global is what three unrelated components share. */
.app-nav {
  background: var(--color-nav);
  backdrop-filter: blur(12px) saturate(1.2);
  -webkit-backdrop-filter: blur(12px) saturate(1.2);
  border-bottom: 1px solid var(--color-border);
  padding: var(--space-3) 0;
  position: sticky;
  top: 0;
  z-index: var(--z-elevated);
}
.app-nav .container {
  display: flex;
  align-items: center;
  gap: var(--space-8);
}
.app-nav .logo {
  display: inline-flex;
  width: 6.5rem;
  flex-shrink: 0;
  font-size: var(--type-lg);
  font-weight: var(--weight-bold);
  color: var(--color-text);
  letter-spacing: -0.025em;
  text-decoration: none;
}
.app-nav .logo:hover { text-decoration: none; color: var(--color-primary); }
</style>
