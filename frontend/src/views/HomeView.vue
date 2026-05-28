<template>
  <!-- Custom Home Content: Full Page Mode -->
  <div v-if="homeContent" class="min-h-screen">
    <!-- iframe mode -->
    <iframe
      v-if="isHomeContentUrl"
      :src="homeContent.trim()"
      class="h-screen w-full border-0"
      allowfullscreen
    ></iframe>
    <!-- HTML mode - SECURITY: homeContent is admin-only setting, XSS risk is acceptable -->
    <div v-else v-html="homeContent"></div>
  </div>

  <!-- Default Home Page -->
  <div v-else class="home-page">
    <header class="home-shell home-nav">
      <router-link class="brand" to="/home" aria-label="Home">
        <span class="brand-mark" aria-hidden="true">
          <img v-if="siteLogo" :src="siteLogo" alt="" />
          <svg v-else viewBox="0 0 100 100" fill="none">
            <defs>
              <linearGradient id="homeLogoLight" x1="0%" y1="0%" x2="100%" y2="100%">
                <stop offset="0%" stop-color="#84aaf7" />
                <stop offset="100%" stop-color="#5c8cf2" />
              </linearGradient>
              <linearGradient id="homeLogoDark" x1="0%" y1="0%" x2="100%" y2="100%">
                <stop offset="0%" stop-color="#5c8cf2" />
                <stop offset="100%" stop-color="#172033" />
              </linearGradient>
            </defs>
            <path d="M 33 41 L 59 41 L 52 56 L 33 56 Z" fill="url(#homeLogoLight)" />
            <path d="M 33 56 L 52 56 L 43 83 L 33 83 Z" fill="url(#homeLogoDark)" />
            <rect x="16" y="19" width="20" height="64" rx="9" fill="url(#homeLogoLight)" />
            <path
              d="M 71 29 L 87 29 Q 91.5 29 89 34 L 61 90 Q 58.5 95 53.5 95 L 38 95 Q 33.5 95 36 90 L 64 34 Q 66.5 29 71 29 Z"
              fill="url(#homeLogoLight)"
            />
          </svg>
        </span>
        <span>{{ siteName }}</span>
      </router-link>

      <nav class="home-nav-links" aria-label="Home navigation">
        <router-link to="/home">首页</router-link>
        <router-link to="/key-usage">用量</router-link>
      </nav>

      <div class="home-actions">
        <a
          v-if="docUrl"
          :href="docUrl"
          target="_blank"
          rel="noopener noreferrer"
          class="doc-action"
          :title="t('home.viewDocs')"
        >
          <Icon name="book" size="sm" />
          <span>{{ t('home.docs') }}</span>
        </a>

        <LocaleSwitcher />

        <button
          type="button"
          class="icon-action"
          :title="isDark ? t('home.switchToLight') : t('home.switchToDark')"
          @click="toggleTheme"
        >
          <Icon v-if="isDark" name="sun" size="md" />
          <Icon v-else name="moon" size="md" />
        </button>

        <router-link
          v-if="isAuthenticated"
          :to="dashboardPath"
          class="button primary nav-cta"
        >
          <span class="user-dot">{{ userInitial }}</span>
          <span>{{ t('home.dashboard') }}</span>
        </router-link>
        <router-link v-else to="/login" class="button primary nav-cta">
          {{ t('home.login') }}
        </router-link>
      </div>
    </header>

    <main class="home-shell hero">
      <section class="copy">
        <span class="eyebrow">
          <svg viewBox="0 0 24 24" fill="none" aria-hidden="true">
            <circle cx="12" cy="12" r="5" fill="currentColor" />
            <path
              d="M12 2v4m0 12v4M2 12h4m12 0h4"
              stroke="currentColor"
              stroke-width="2"
              stroke-linecap="round"
            />
          </svg>
          开放式 AI 账号协作网络
        </span>

        <h1 class="home-title">
          让账号能力在用户之间<span class="flow-text">自由流动</span>
        </h1>

        <p class="lead">
          {{ siteName }}不再只是平台向用户提供服务，而是让个人用户提供账号、其他用户消费额度，平台完成调度中转、收益结算、邀请分成。
        </p>

        <div class="hero-actions">
          <router-link :to="isAuthenticated ? dashboardPath : '/login'" class="button primary">
            {{ isAuthenticated ? t('home.goToDashboard') : t('home.getStarted') }}
          </router-link>
          <router-link class="button secondary" to="/key-usage">查看用量</router-link>
        </div>
      </section>

      <aside class="visual" aria-label="账号协作网络数据面板示意">
        <div class="sphere sphere-one"></div>
        <div class="sphere sphere-two"></div>
        <div class="sphere sphere-three"></div>

        <div class="console-card">
          <div class="console-inner">
            <div class="side-rail">
              <div class="rail-logo">
                <img v-if="siteLogo" :src="siteLogo" alt="" />
                <svg v-else viewBox="0 0 100 100" fill="none" aria-hidden="true">
                  <defs>
                    <linearGradient id="railLogoLight" x1="0%" y1="0%" x2="100%" y2="100%">
                      <stop offset="0%" stop-color="#84aaf7" />
                      <stop offset="100%" stop-color="#5c8cf2" />
                    </linearGradient>
                    <linearGradient id="railLogoDark" x1="0%" y1="0%" x2="100%" y2="100%">
                      <stop offset="0%" stop-color="#5c8cf2" />
                      <stop offset="100%" stop-color="#172033" />
                    </linearGradient>
                  </defs>
                  <path d="M 33 41 L 59 41 L 52 56 L 33 56 Z" fill="url(#railLogoLight)" />
                  <path d="M 33 56 L 52 56 L 43 83 L 33 83 Z" fill="url(#railLogoDark)" />
                  <rect x="16" y="19" width="20" height="64" rx="9" fill="url(#railLogoLight)" />
                  <path
                    d="M 71 29 L 87 29 Q 91.5 29 89 34 L 61 90 Q 58.5 95 53.5 95 L 38 95 Q 33.5 95 36 90 L 64 34 Q 66.5 29 71 29 Z"
                    fill="url(#railLogoLight)"
                  />
                </svg>
              </div>
              <i class="active">
                <svg fill="none" stroke="currentColor" viewBox="0 0 24 24">
                  <path
                    stroke-linecap="round"
                    stroke-linejoin="round"
                    d="M3 12l2-2m0 0l7-7 7 7M5 10v10a1 1 0 001 1h3m10-11l2 2m-2-2v10a1 1 0 01-1 1h-3m-6 0a1 1 0 001-1v-4a1 1 0 011-1h2a1 1 0 011 1v4a1 1 0 001 1m-6 0h6"
                  ></path>
                </svg>
              </i>
              <i>
                <svg fill="none" stroke="currentColor" viewBox="0 0 24 24">
                  <path
                    stroke-linecap="round"
                    stroke-linejoin="round"
                    d="M16 7a4 4 0 11-8 0 4 4 0 018 0zM12 14a7 7 0 00-7 7h14a7 7 0 00-7-7z"
                  ></path>
                </svg>
              </i>
              <i>
                <svg fill="none" stroke="currentColor" viewBox="0 0 24 24">
                  <path
                    stroke-linecap="round"
                    stroke-linejoin="round"
                    d="M9 12h6m-6 4h6m2 5H7a2 2 0 01-2-2V5a2 2 0 012-2h5.586a1 1 0 01.707.293l5.414 5.414a1 1 0 01.293.707V19a2 2 0 01-2 2z"
                  ></path>
                </svg>
              </i>
              <i>
                <svg fill="none" stroke="currentColor" viewBox="0 0 24 24">
                  <path
                    stroke-linecap="round"
                    stroke-linejoin="round"
                    d="M20 7l-8-4-8 4m16 0l-8 4m8-4v10l-8 4m0-10L4 7m8 4v10M4 7v10l8 4"
                  ></path>
                </svg>
              </i>
            </div>

            <div class="dashboard-board">
              <div class="metrics">
                <article class="metric center-align">
                  <span class="metric-icon">
                    <svg fill="none" stroke="currentColor" stroke-width="2.5" viewBox="0 0 24 24" stroke-linecap="round" stroke-linejoin="round">
                      <path d="M21 2l-2 2m-7.61 7.61a5.5 5.5 0 1 1-7.778 7.778 5.5 5.5 0 0 1 7.777-7.777zm0 0L15.5 7.5m0 0l3 3L22 7l-3-3m-3.5 3.5L19 4"></path>
                    </svg>
                  </span>
                  <b>{{ todayRequestsText }}</b>
                  <span>今日请求</span>
                </article>

                <article class="metric">
                  <span class="metric-icon">
                    <svg fill="none" stroke="currentColor" stroke-width="2.2" viewBox="0 0 24 24" stroke-linecap="round" stroke-linejoin="round">
                      <path d="M21 16V8a2 2 0 0 0-1-1.73l-7-4a2 2 0 0 0-2 0l-7 4A2 2 0 0 0 3 8v8a2 2 0 0 0 1 1.73l7 4a2 2 0 0 0 2 0l7-4A2 2 0 0 0 21 16z"></path>
                      <polyline points="3.27 6.96 12 12.01 20.73 6.96"></polyline>
                      <line x1="12" y1="22.08" x2="12" y2="12"></line>
                    </svg>
                  </span>
                  <b>{{ todayTokensText }}</b>
                  <span>今日 Token</span>
                </article>

                <article class="metric">
                  <span class="metric-icon">
                    <svg fill="none" stroke="currentColor" stroke-width="2.2" viewBox="0 0 24 24" stroke-linecap="round" stroke-linejoin="round">
                      <path d="M12 22s8-4 8-10V5l-8-3-8 3v7c0 6 8 10 8 10z"></path>
                      <polyline points="9 12 11 14 15 10"></polyline>
                    </svg>
                  </span>
                  <b>98.2%</b>
                  <span>成功率</span>
                </article>

                <article class="metric center-align">
                  <span class="metric-icon">
                    <svg fill="none" stroke="currentColor" stroke-width="2.2" viewBox="0 0 24 24" stroke-linecap="round" stroke-linejoin="round">
                      <circle cx="12" cy="12" r="10"></circle>
                      <polyline points="12 6 12 12 16 14"></polyline>
                    </svg>
                  </span>
                  <b>124ms</b>
                  <span>平均响应</span>
                </article>
              </div>

              <div class="chart-card">
                <div class="chart-head">
                  <div class="chart-title">请求趋势</div>
                  <div class="chart-legends">
                    <span class="legend-one">请求数</span>
                    <span class="legend-two">Token 数</span>
                  </div>
                </div>

                <svg viewBox="0 0 520 160" preserveAspectRatio="none">
                  <defs>
                    <linearGradient id="homeFadeDark" x1="0" y1="0" x2="0" y2="1">
                      <stop offset="0%" stop-color="#172033" stop-opacity="0.2" />
                      <stop offset="100%" stop-color="#172033" stop-opacity="0" />
                    </linearGradient>
                    <linearGradient id="homeFadeBlue" x1="0" y1="0" x2="0" y2="1">
                      <stop offset="0%" stop-color="#5c8cf2" stop-opacity="0.2" />
                      <stop offset="100%" stop-color="#5c8cf2" stop-opacity="0" />
                    </linearGradient>
                  </defs>

                  <path d="M0 30H520M0 70H520M0 110H520M0 150H520" stroke="#f1f5f9" stroke-dasharray="4 4" stroke-width="1.5" />
                  <path d="M 0 145 C 40 145, 80 80, 120 80 C 160 80, 200 140, 240 140 C 290 140, 310 135, 350 135 C 390 135, 400 120, 440 120 C 480 120, 500 135, 520 135 L 520 160 L 0 160 Z" fill="url(#homeFadeBlue)" />
                  <path d="M 0 130 C 30 130, 50 115, 90 115 C 130 115, 170 140, 210 140 C 240 140, 255 45, 280 45 C 305 45, 320 130, 360 130 C 400 130, 420 110, 460 110 C 490 110, 500 125, 520 125 L 520 160 L 0 160 Z" fill="url(#homeFadeDark)" />
                  <path d="M 0 145 C 40 145, 80 80, 120 80 C 160 80, 200 140, 240 140 C 290 140, 310 135, 350 135 C 390 135, 400 120, 440 120 C 480 120, 500 135, 520 135" fill="none" stroke="#5c8cf2" stroke-width="3" stroke-linecap="round" />
                  <path d="M 0 130 C 30 130, 50 115, 90 115 C 130 115, 170 140, 210 140 C 240 140, 255 45, 280 45 C 305 45, 320 130, 360 130 C 400 130, 420 110, 460 110 C 490 110, 500 125, 520 125" fill="none" stroke="#172033" stroke-width="3" stroke-linecap="round" />
                  <circle cx="120" cy="80" r="4.5" fill="#fff" stroke="#5c8cf2" stroke-width="2.5" />
                  <circle cx="240" cy="140" r="4.5" fill="#fff" stroke="#5c8cf2" stroke-width="2.5" />
                  <circle cx="440" cy="120" r="4.5" fill="#fff" stroke="#5c8cf2" stroke-width="2.5" />
                  <circle cx="90" cy="115" r="4.5" fill="#fff" stroke="#172033" stroke-width="2.5" />
                  <circle cx="280" cy="45" r="4.5" fill="#fff" stroke="#172033" stroke-width="2.5" />
                  <circle cx="360" cy="130" r="4.5" fill="#fff" stroke="#172033" stroke-width="2.5" />
                  <circle cx="460" cy="110" r="4.5" fill="#fff" stroke="#172033" stroke-width="2.5" />
                </svg>
              </div>
            </div>
          </div>
        </div>

        <div class="stack-3d">
          <svg viewBox="0 0 120 150" fill="none" xmlns="http://www.w3.org/2000/svg">
            <path d="M60 140L10 115L60 90L110 115L60 140Z" fill="rgba(92,140,242,0.2)" filter="blur(8px)" />
            <path d="M60 130L10 105V115L60 140L110 115V105L60 130Z" fill="#e2e8f0" />
            <path d="M60 130L10 105L60 80L110 105L60 130Z" fill="#f8fafc" />
            <path d="M10 105L60 130V140L10 115V105Z" fill="#cbd5e1" />
            <circle cx="85" cy="118" r="2" fill="#5c8cf2" />
            <circle cx="92" cy="115" r="2" fill="#5c8cf2" />
            <path d="M60 100L10 75V85L60 110L110 85V75L60 100Z" fill="#e2e8f0" />
            <path d="M60 100L10 75L60 50L110 75L60 100Z" fill="#ffffff" />
            <path d="M10 75L60 100V110L10 85V75Z" fill="#cbd5e1" />
            <circle cx="85" cy="88" r="2" fill="#5c8cf2" />
            <circle cx="92" cy="85" r="2" fill="#5c8cf2" />
            <path d="M60 70L10 45V55L60 80L110 55V45L60 70Z" fill="#e2e8f0" />
            <path d="M60 70L10 45L60 20L110 45L60 70Z" fill="#ffffff" />
            <path d="M10 45L60 70V80L10 55V45Z" fill="#cbd5e1" />
            <path d="M60 35L45 42L52 46L60 41L68 46L75 42L60 35Z" fill="#5c8cf2" opacity="0.8" />
          </svg>
        </div>
      </aside>
    </main>

    <section class="home-shell step-strip" aria-label="接入步骤">
      <h2><span class="flow-text">3</span> 步开始，<span class="flow-text">2</span> 分钟完成迁移</h2>

      <div class="steps">
        <article class="step-card">
          <span class="step-num">1</span>
          <span class="step-icon">
            <svg viewBox="0 0 24 24" fill="none">
              <path d="M15 19a6 6 0 0 0-12 0" stroke="currentColor" stroke-width="2.2" stroke-linecap="round" />
              <path d="M9 11a4 4 0 1 0 0-8 4 4 0 0 0 0 8Z" stroke="currentColor" stroke-width="2.2" />
              <path d="M19 8v6M16 11h6" stroke="currentColor" stroke-width="2.2" stroke-linecap="round" />
            </svg>
          </span>
          <div>
            <h3>注册账号</h3>
            <p>免费注册，即刻获得体验额度</p>
          </div>
        </article>

        <span class="step-arrow" aria-hidden="true"></span>

        <article class="step-card">
          <span class="step-num">2</span>
          <span class="step-icon">
            <svg viewBox="0 0 24 24" fill="none">
              <path d="M21 7a5 5 0 0 1-6.8 4.7L6.5 19.4 3 21l1.6-3.5 7.7-7.7A5 5 0 1 1 21 7Z" stroke="currentColor" stroke-width="2.2" stroke-linejoin="round" />
              <path d="M17.5 6.5h.01" stroke="currentColor" stroke-width="3.5" stroke-linecap="round" />
            </svg>
          </span>
          <div>
            <h3>获取 API Key</h3>
            <p>一键生成，支持多 Key 管理与权限控制</p>
          </div>
        </article>

        <span class="step-arrow" aria-hidden="true"></span>

        <article class="step-card">
          <span class="step-num">3</span>
          <span class="step-icon">
            <svg viewBox="0 0 24 24" fill="none">
              <path d="m8 16-4-4 4-4M16 8l4 4-4 4" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round" />
            </svg>
          </span>
          <div>
            <h3>替换 Base URL</h3>
            <p>接入即可使用，快速完成迁移</p>
          </div>
        </article>
      </div>
    </section>

    <footer class="home-shell home-footer">
      <span>&copy; {{ currentYear }} {{ siteName }}. {{ t('home.footer.allRightsReserved') }}</span>
    </footer>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAuthStore, useAppStore } from '@/stores'
import LocaleSwitcher from '@/components/common/LocaleSwitcher.vue'
import Icon from '@/components/icons/Icon.vue'
import { getPublicTodayStats } from '@/api/usage'

const { t } = useI18n()

const authStore = useAuthStore()
const appStore = useAppStore()

// Site settings - directly from appStore (already initialized from injected config)
const siteName = computed(() => appStore.cachedPublicSettings?.site_name || appStore.siteName || 'Sub2API')
const siteLogo = computed(() => appStore.cachedPublicSettings?.site_logo || appStore.siteLogo || '')
const docUrl = computed(() => appStore.cachedPublicSettings?.doc_url || appStore.docUrl || '')
const homeContent = computed(() => appStore.cachedPublicSettings?.home_content || '')

// Check if homeContent is a URL (for iframe display)
const isHomeContentUrl = computed(() => {
  const content = homeContent.value.trim()
  return content.startsWith('http://') || content.startsWith('https://')
})

// Theme
const isDark = ref(document.documentElement.classList.contains('dark'))

// Auth state
const isAuthenticated = computed(() => authStore.isAuthenticated)
const isAdmin = computed(() => authStore.isAdmin)
const dashboardPath = computed(() => isAdmin.value ? '/admin/dashboard' : '/dashboard')
const userInitial = computed(() => {
  const user = authStore.user
  if (!user || !user.email) return ''
  return user.email.charAt(0).toUpperCase()
})

// Current year for footer
const currentYear = computed(() => new Date().getFullYear())

const publicTodayStats = ref<{
  today_requests: number
  today_tokens: number
} | null>(null)

const todayRequestsText = computed(() => {
  if (!publicTodayStats.value) return '--'
  return formatInteger(publicTodayStats.value.today_requests)
})

const todayTokensText = computed(() => {
  if (!publicTodayStats.value) return '--'
  return formatCompactNumber(publicTodayStats.value.today_tokens)
})

function formatInteger(value: number): string {
  if (!Number.isFinite(value)) return '--'
  return new Intl.NumberFormat('zh-CN', {
    maximumFractionDigits: 0
  }).format(value)
}

function formatCompactNumber(value: number): string {
  if (!Number.isFinite(value)) return '--'

  const absValue = Math.abs(value)
  const units = [
    { value: 1_000_000_000, suffix: 'B' },
    { value: 1_000_000, suffix: 'M' },
    { value: 1_000, suffix: 'K' }
  ]
  const unit = units.find((item) => absValue >= item.value)

  if (!unit) {
    return formatInteger(value)
  }

  return `${(value / unit.value).toFixed(2).replace(/\.?0+$/, '')}${unit.suffix}`
}

async function fetchPublicTodayStats() {
  try {
    publicTodayStats.value = await getPublicTodayStats()
  } catch (error) {
    publicTodayStats.value = null
    console.error('Failed to fetch public today usage stats:', error)
  }
}

// Toggle theme
function toggleTheme() {
  isDark.value = !isDark.value
  document.documentElement.classList.toggle('dark', isDark.value)
  localStorage.setItem('theme', isDark.value ? 'dark' : 'light')
}

// Initialize theme
function initTheme() {
  const savedTheme = localStorage.getItem('theme')
  if (
    savedTheme === 'dark' ||
    (!savedTheme && window.matchMedia('(prefers-color-scheme: dark)').matches)
  ) {
    isDark.value = true
    document.documentElement.classList.add('dark')
  }
}

onMounted(() => {
  initTheme()

  // Check auth state
  authStore.checkAuth()

  // Ensure public settings are loaded (will use cache if already loaded from injected config)
  if (!appStore.publicSettingsLoaded) {
    appStore.fetchPublicSettings()
  }

  fetchPublicTodayStats()
})
</script>

<style scoped>
.home-page {
  --bg: #f5f8ff;
  --surface: rgba(255, 255, 255, 0.82);
  --surface-strong: #ffffff;
  --text: #1a2233;
  --muted: #64748b;
  --line: rgba(15, 23, 42, 0.08);
  --accent: #5c8cf2;
  --accent-2: #84aaf7;
  --accent-soft: rgba(92, 140, 242, 0.1);
  --shadow: 0 24px 70px rgba(15, 23, 42, 0.08);

  position: relative;
  min-height: 100vh;
  overflow: hidden;
  color: var(--text);
  background:
    linear-gradient(180deg, rgba(255, 255, 255, 0.94), rgba(245, 248, 255, 0.98) 42%, #eff4ff),
    radial-gradient(circle at 72% 26%, rgba(92, 140, 242, 0.16), transparent 34%);
  font-family:
    "Segoe UI",
    "PingFang SC",
    "Microsoft YaHei",
    system-ui,
    sans-serif;
  letter-spacing: 0;
}

.home-page::before {
  position: absolute;
  inset: -20%;
  background:
    radial-gradient(circle at 14% 20%, rgba(92, 140, 242, 0.18), transparent 28%),
    radial-gradient(circle at 84% 82%, rgba(132, 170, 247, 0.2), transparent 28%);
  animation: ambient-shift 18s ease-in-out infinite alternate;
  content: "";
  pointer-events: none;
}

:global(html.dark .home-page) {
  --surface: rgba(15, 23, 42, 0.78);
  --surface-strong: #111827;
  --text: #f6f8ff;
  --muted: #9fb0c8;
  --line: rgba(148, 163, 184, 0.16);
  --accent: #7da2ff;
  --accent-2: #a9c3ff;
  --accent-soft: rgba(125, 162, 255, 0.16);
  --shadow: 0 28px 82px rgba(0, 0, 0, 0.36);

  background:
    linear-gradient(180deg, #07111f 0%, #0b1424 44%, #101827 100%),
    radial-gradient(circle at 72% 26%, rgba(92, 140, 242, 0.2), transparent 34%);
}

:global(html.dark .home-page)::before {
  background:
    radial-gradient(circle at 14% 20%, rgba(92, 140, 242, 0.22), transparent 30%),
    radial-gradient(circle at 84% 82%, rgba(20, 184, 166, 0.12), transparent 30%),
    radial-gradient(circle at 42% 88%, rgba(96, 165, 250, 0.12), transparent 28%);
  opacity: 0.88;
}

.home-shell {
  position: relative;
  z-index: 1;
  width: min(1180px, calc(100% - 36px));
  margin: 0 auto;
}

.home-nav {
  display: flex;
  min-height: 82px;
  align-items: center;
  justify-content: space-between;
  gap: 16px;
}

.brand {
  display: inline-flex;
  align-items: center;
  gap: 10px;
  color: var(--text);
  font-size: 1.15rem;
  font-weight: 760;
}

.brand-mark {
  display: grid;
  width: 38px;
  height: 38px;
  place-items: center;
  overflow: hidden;
  border: 1px solid rgba(107, 155, 240, 0.14);
  border-radius: 13px;
  background: rgba(255, 255, 255, 0.78);
  box-shadow: 0 14px 34px rgba(107, 155, 240, 0.12);
  backdrop-filter: blur(16px);
}

:global(html.dark .home-page .brand-mark) {
  border-color: rgba(148, 163, 184, 0.18);
  background: rgba(15, 23, 42, 0.72);
  box-shadow: 0 14px 34px rgba(0, 0, 0, 0.26);
}

.brand-mark img,
.brand-mark svg {
  width: 100%;
  height: 100%;
  object-fit: contain;
}

.home-nav-links {
  display: none;
  align-items: center;
  gap: 42px;
  color: var(--muted);
  font-size: 0.95rem;
  font-weight: 650;
}

.home-nav-links a {
  position: relative;
  transition: color 180ms ease;
}

.home-nav-links a:hover,
.home-nav-links a.router-link-active {
  color: var(--accent);
}

.home-nav-links a.router-link-active::after {
  position: absolute;
  right: 0;
  bottom: -30px;
  left: 0;
  height: 2px;
  border-radius: 99px;
  background: var(--accent);
  content: "";
}

.home-actions {
  display: flex;
  align-items: center;
  gap: 10px;
}

.doc-action {
  display: inline-flex;
  min-height: 44px;
  align-items: center;
  justify-content: center;
  gap: 7px;
  border: 1px solid rgba(107, 155, 240, 0.14);
  border-radius: 999px;
  background: rgba(255, 255, 255, 0.56);
  color: var(--muted);
  padding: 0 14px;
  font-size: 0.9rem;
  font-weight: 700;
  white-space: nowrap;
  transition:
    color 160ms ease,
    background 160ms ease,
    transform 160ms ease;
  backdrop-filter: blur(14px);
}

:global(html.dark .home-page .doc-action) {
  border-color: rgba(148, 163, 184, 0.18);
  background: rgba(15, 23, 42, 0.7);
  color: #c7d2fe;
}

.doc-action:hover {
  transform: translateY(-1px);
  color: var(--accent);
  background: rgba(255, 255, 255, 0.78);
}

.icon-action {
  display: inline-flex;
  min-width: 44px;
  min-height: 44px;
  align-items: center;
  justify-content: center;
  border: 1px solid rgba(107, 155, 240, 0.14);
  border-radius: 999px;
  background: rgba(255, 255, 255, 0.56);
  color: var(--muted);
  transition:
    color 160ms ease,
    background 160ms ease,
    transform 160ms ease;
  backdrop-filter: blur(14px);
}

:global(html.dark .home-page .icon-action) {
  border-color: rgba(148, 163, 184, 0.18);
  background: rgba(15, 23, 42, 0.7);
  color: #c7d2fe;
}

.icon-action:hover {
  transform: translateY(-1px);
  color: var(--accent);
  background: rgba(255, 255, 255, 0.78);
}

.button {
  display: inline-flex;
  min-height: 46px;
  align-items: center;
  justify-content: center;
  gap: 9px;
  border: 1px solid transparent;
  border-radius: 999px;
  padding: 0 22px;
  cursor: pointer;
  font-size: 0.95rem;
  font-weight: 740;
  white-space: nowrap;
  transition:
    transform 180ms ease,
    box-shadow 180ms ease,
    background 180ms ease,
    border-color 180ms ease;
}

.button:hover {
  transform: translateY(-2px);
}

.button.primary {
  background: #172033;
  color: #fff;
  box-shadow: 0 16px 34px rgba(23, 32, 51, 0.24);
}

.button.secondary {
  border-color: var(--line);
  background: rgba(255, 255, 255, 0.72);
  color: var(--text);
  backdrop-filter: blur(14px);
}

:global(html.dark .home-page .button.primary) {
  background: linear-gradient(135deg, #eef4ff, #9fbaff);
  color: #0f172a;
  box-shadow: 0 18px 44px rgba(96, 165, 250, 0.24);
}

:global(html.dark .home-page .button.secondary) {
  border-color: rgba(148, 163, 184, 0.18);
  background: rgba(15, 23, 42, 0.62);
  color: #e5edff;
}

.user-dot {
  display: grid;
  width: 22px;
  height: 22px;
  place-items: center;
  border-radius: 999px;
  background: rgba(255, 255, 255, 0.18);
  font-size: 0.72rem;
}

.hero {
  display: grid;
  min-height: 570px;
  align-items: center;
  gap: 52px;
  padding: 52px 0 34px;
}

.copy {
  max-width: 640px;
}

.eyebrow {
  display: inline-flex;
  width: fit-content;
  align-items: center;
  gap: 8px;
  border-radius: 999px;
  background: var(--accent-soft);
  color: var(--accent);
  padding: 9px 15px;
  font-size: 0.9rem;
  font-weight: 760;
  animation: rise 620ms ease both;
}

.eyebrow svg {
  width: 16px;
  height: 16px;
}

.home-title {
  max-width: 620px;
  margin: 30px 0 20px;
  font-size: clamp(2.75rem, 7vw, 4.25rem);
  font-weight: 860;
  line-height: 1.06;
  letter-spacing: 0;
  animation: rise 700ms 80ms ease both;
}

.home-title span {
  display: inline-block;
}

.flow-text {
  background: linear-gradient(100deg, #5c8cf2 0%, #84aaf7 28%, #172033 56%, #5c8cf2 100%);
  background-size: 260% 100%;
  -webkit-background-clip: text;
  background-clip: text;
  color: transparent;
  animation: text-flow 4.8s ease-in-out infinite;
}

:global(html.dark .home-page .flow-text) {
  background-image: linear-gradient(100deg, #9db8ff 0%, #dbeafe 28%, #60a5fa 56%, #b7c9ff 100%);
}

.lead {
  max-width: 560px;
  margin: 0 0 34px;
  color: var(--muted);
  font-size: clamp(1rem, 1.8vw, 1.25rem);
  font-weight: 460;
  line-height: 1.75;
  animation: rise 760ms 150ms ease both;
}

.hero-actions {
  display: flex;
  flex-wrap: wrap;
  gap: 16px;
  align-items: center;
  animation: rise 820ms 220ms ease both;
}

.visual {
  position: relative;
  z-index: 1;
  display: flex;
  min-height: 480px;
  align-items: center;
  justify-content: center;
  perspective: 1200px;
  animation: rise 780ms 160ms ease both;
}

.sphere {
  position: absolute;
  z-index: -1;
  border-radius: 50%;
  background: radial-gradient(circle at 30% 30%, #fff, #cbe0f3 60%, #8ab2e0);
  box-shadow: 0 10px 20px rgba(92, 140, 242, 0.15), inset 0 -5px 15px rgba(92, 140, 242, 0.1);
}

.sphere-one {
  top: 10%;
  right: 15%;
  width: 40px;
  height: 40px;
  filter: blur(1px);
}

.sphere-two {
  top: 25%;
  right: 5%;
  width: 18px;
  height: 18px;
}

.sphere-three {
  right: -2%;
  bottom: 35%;
  width: 28px;
  height: 28px;
  background: radial-gradient(circle at 30% 30%, #fff, #e2e8f0 60%, #cbd5e1);
}

.console-card {
  width: min(100%, 720px);
  border: 1px solid rgba(255, 255, 255, 0.9);
  border-radius: 28px;
  background: rgba(255, 255, 255, 0.75);
  box-shadow:
    -25px 35px 65px rgba(15, 23, 42, 0.08),
    -10px 15px 25px rgba(92, 140, 242, 0.04),
    inset 0 0 0 1px rgba(255, 255, 255, 0.6);
  transform: rotateX(12deg) rotateY(-16deg) rotateZ(4deg);
  transform-style: preserve-3d;
  transition: transform 500ms cubic-bezier(0.175, 0.885, 0.32, 1.275);
  backdrop-filter: blur(20px);
}

:global(html.dark .home-page .console-card) {
  border-color: rgba(148, 163, 184, 0.18);
  background: rgba(15, 23, 42, 0.74);
  box-shadow:
    -25px 35px 70px rgba(0, 0, 0, 0.34),
    -10px 15px 28px rgba(92, 140, 242, 0.08),
    inset 0 0 0 1px rgba(148, 163, 184, 0.08);
}

.console-card:hover {
  transform: rotateX(8deg) rotateY(-10deg) rotateZ(2deg) translateY(-10px);
}

.console-inner {
  display: grid;
  grid-template-columns: 78px 1fr;
  min-height: 420px;
}

.side-rail {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 16px;
  border-right: 1px solid rgba(15, 23, 42, 0.05);
  border-radius: 28px 0 0 28px;
  background: rgba(255, 255, 255, 0.4);
  padding: 24px 0;
}

:global(html.dark .home-page .side-rail) {
  border-right-color: rgba(148, 163, 184, 0.1);
  background: rgba(2, 6, 23, 0.28);
}

.rail-logo {
  display: grid;
  width: 32px;
  height: 32px;
  place-items: center;
  overflow: hidden;
  margin-bottom: 12px;
  border-radius: 8px;
  background: rgba(255, 255, 255, 0.78);
  box-shadow: 0 10px 24px rgba(92, 140, 242, 0.12);
}

.rail-logo img,
.rail-logo svg {
  width: 100%;
  height: 100%;
  object-fit: contain;
}

:global(html.dark .home-page .rail-logo) {
  background: rgba(15, 23, 42, 0.72);
  box-shadow: 0 10px 24px rgba(0, 0, 0, 0.24);
}

.side-rail i {
  display: flex;
  width: 40px;
  height: 40px;
  align-items: center;
  justify-content: center;
  border-radius: 12px;
  color: var(--muted);
}

.side-rail i.active {
  background: var(--accent-soft);
  color: var(--accent);
  box-shadow: 0 4px 12px rgba(92, 140, 242, 0.1);
}

.side-rail svg {
  width: 20px;
  height: 20px;
  stroke-width: 2;
}

.dashboard-board {
  padding: 32px;
}

.metrics {
  display: grid;
  grid-template-columns: repeat(4, minmax(0, 1fr));
  gap: 16px;
  margin-bottom: 24px;
}

.metric {
  position: relative;
  display: flex;
  min-height: 116px;
  flex-direction: column;
  justify-content: center;
  overflow: hidden;
  border: 1px solid rgba(15, 23, 42, 0.04);
  border-radius: 20px;
  background: #ffffff;
  padding: 16px 14px;
  box-shadow: 0 10px 30px rgba(15, 23, 42, 0.03);
  transition: transform 300ms ease;
}

:global(html.dark .home-page .metric),
:global(html.dark .home-page .chart-card),
:global(html.dark .home-page .step-card) {
  border-color: rgba(148, 163, 184, 0.12);
  background: rgba(15, 23, 42, 0.82);
  box-shadow: 0 16px 38px rgba(0, 0, 0, 0.18);
}

.metric:hover {
  transform: translateY(-3px);
}

.metric.center-align {
  align-items: center;
  text-align: center;
}

.metric-icon {
  display: grid;
  width: 32px;
  height: 32px;
  place-items: center;
  margin-bottom: 12px;
  border-radius: 10px;
}

.metric:nth-child(1) .metric-icon {
  background: #eff6ff;
  color: #3b82f6;
}

.metric:nth-child(2) .metric-icon {
  background: #fff7ed;
  color: #f97316;
}

.metric:nth-child(3) .metric-icon {
  background: #f0fdf4;
  color: #10b981;
}

.metric:nth-child(4) .metric-icon {
  background: #f3e8ff;
  color: #a855f7;
}

.metric-icon svg {
  width: 16px;
  height: 16px;
}

.metric b {
  color: var(--text);
  font-size: 1.25rem;
  line-height: 1.1;
}

.metric span:last-child {
  margin-top: 6px;
  color: var(--accent);
  font-size: 0.82rem;
  font-weight: 650;
  white-space: nowrap;
}

:global(html.dark .home-page .metric:nth-child(1) .metric-icon) {
  background: rgba(59, 130, 246, 0.18);
  color: #93c5fd;
}

:global(html.dark .home-page .metric:nth-child(2) .metric-icon) {
  background: rgba(249, 115, 22, 0.16);
  color: #fdba74;
}

:global(html.dark .home-page .metric:nth-child(3) .metric-icon) {
  background: rgba(16, 185, 129, 0.16);
  color: #6ee7b7;
}

:global(html.dark .home-page .metric:nth-child(4) .metric-icon) {
  background: rgba(168, 85, 247, 0.16);
  color: #d8b4fe;
}

.chart-card {
  border: 1px solid rgba(15, 23, 42, 0.04);
  border-radius: 20px;
  background: #ffffff;
  padding: 24px;
  box-shadow: 0 10px 30px rgba(15, 23, 42, 0.03);
}

.chart-head {
  display: flex;
  align-items: flex-end;
  justify-content: space-between;
  gap: 14px;
  margin-bottom: 16px;
}

.chart-title {
  color: var(--text);
  font-size: 0.95rem;
  font-weight: 800;
}

.chart-legends {
  display: flex;
  gap: 16px;
  color: var(--muted);
  font-size: 0.75rem;
  font-weight: 650;
}

.chart-legends span {
  display: flex;
  align-items: center;
  gap: 6px;
}

.chart-legends span::before {
  width: 8px;
  height: 8px;
  border-radius: 50%;
  content: "";
}

.legend-one::before {
  background: var(--accent);
}

.legend-two::before {
  background: #172033;
}

.chart-card svg {
  display: block;
  width: 100%;
  height: 140px;
}

:global(html.dark .home-page .chart-card path[stroke="#f1f5f9"]) {
  stroke: rgba(148, 163, 184, 0.16);
}

:global(html.dark .home-page .chart-card path[stroke="#172033"]),
:global(html.dark .home-page .chart-card circle[stroke="#172033"]) {
  stroke: #c7d2fe;
}

:global(html.dark .home-page .legend-two)::before {
  background: #c7d2fe;
}

.stack-3d {
  position: absolute;
  right: -10px;
  bottom: -30px;
  z-index: 10;
  width: 140px;
  height: 180px;
  transform: translateZ(80px);
  transition: transform 400ms ease;
}

.stack-3d:hover {
  transform: translateZ(100px) translateY(-5px);
}

.stack-3d svg {
  width: 100%;
  height: 100%;
  filter: drop-shadow(-10px 20px 20px rgba(92, 140, 242, 0.25));
}

.step-strip {
  margin: 20px auto 72px;
  border: 1px solid var(--line);
  border-radius: 34px;
  background: rgba(255, 255, 255, 0.66);
  padding: 40px 32px;
  box-shadow: var(--shadow);
  backdrop-filter: blur(18px);
}

:global(html.dark .home-page .step-strip) {
  border-color: rgba(148, 163, 184, 0.16);
  background: rgba(15, 23, 42, 0.58);
}

.step-strip h2 {
  margin: 0 0 32px;
  text-align: center;
  font-size: 1.25rem;
  font-weight: 840;
}

.steps {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 16px;
}

.step-card {
  position: relative;
  display: flex;
  min-height: 104px;
  flex: 1;
  align-items: center;
  gap: 16px;
  border: 1px solid rgba(15, 23, 42, 0.04);
  border-radius: 20px;
  background: #ffffff;
  padding: 20px 24px;
  box-shadow: 0 10px 30px rgba(15, 23, 42, 0.03);
  transition: transform 300ms ease;
}

.step-card:hover {
  transform: translateY(-3px);
}

.step-num {
  position: absolute;
  top: -12px;
  left: 52px;
  display: grid;
  width: 24px;
  height: 24px;
  place-items: center;
  border-radius: 50%;
  background: var(--accent);
  box-shadow: 0 0 0 4px #ffffff;
  color: #fff;
  font-size: 0.75rem;
  font-weight: 800;
  transform: translateX(-50%);
}

:global(html.dark .home-page .step-num) {
  box-shadow: 0 0 0 4px #111827;
}

.step-icon {
  display: grid;
  width: 56px;
  height: 56px;
  flex-shrink: 0;
  place-items: center;
  border-radius: 16px;
  background: var(--accent-soft);
  color: var(--accent);
}

.step-icon svg {
  width: 28px;
  height: 28px;
}

.step-card h3 {
  margin: 0 0 6px;
  color: var(--text);
  font-size: 1.06rem;
  font-weight: 760;
}

.step-card p {
  margin: 0;
  color: var(--muted);
  font-size: 0.82rem;
  font-weight: 500;
  line-height: 1.5;
}

.step-arrow {
  position: relative;
  width: 32px;
  height: 2px;
  flex-shrink: 0;
  background-image: linear-gradient(to right, #cbd5e1 50%, transparent 50%);
  background-size: 8px 100%;
}

.step-arrow::after {
  position: absolute;
  top: 50%;
  right: -4px;
  border-width: 4px 0 4px 6px;
  border-style: solid;
  border-color: transparent transparent transparent #cbd5e1;
  content: "";
  transform: translateY(-50%);
}

.home-footer {
  display: flex;
  justify-content: center;
  padding: 5px 0 5px;
  color: #8da0b7;
  font-size: 0.88rem;
  text-align: center;
}

:global(html.dark .home-page .home-footer) {
  color: #8fa3c0;
}

@keyframes ambient-shift {
  0% {
    transform: translate3d(-2%, -1%, 0) scale(1);
  }
  100% {
    transform: translate3d(2%, 1%, 0) scale(1.04);
  }
}

@keyframes rise {
  from {
    opacity: 0;
    transform: translateY(16px);
  }
  to {
    opacity: 1;
    transform: translateY(0);
  }
}

@keyframes text-flow {
  0%,
  100% {
    background-position: 0% 50%;
  }
  50% {
    background-position: 100% 50%;
  }
}

@media (min-width: 768px) {
  .home-shell {
    width: min(1180px, calc(100% - 48px));
  }

  .home-nav-links {
    display: flex;
  }

  .hero {
    grid-template-columns: minmax(0, 0.92fr) minmax(440px, 1.08fr);
  }
}

@media (max-width: 900px) {
  .hero {
    grid-template-columns: 1fr;
    min-height: auto;
    gap: 26px;
    padding: 42px 0 28px;
  }

  .visual {
    perspective: none;
  }

  .console-card {
    transform: none !important;
  }

  .stack-3d,
  .sphere {
    display: none;
  }

  .metrics {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }

  .steps {
    flex-direction: column;
    align-items: stretch;
    gap: 24px;
  }

  .step-arrow {
    display: none;
  }

  .step-num {
    left: 50%;
  }
}

@media (max-width: 640px) {
  .home-actions :deep(.locale-switcher) {
    display: none;
  }

  .home-nav {
    min-height: 72px;
  }

  .brand span:last-child {
    max-width: 126px;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .nav-cta {
    display: none;
  }

  .visual {
    min-height: auto;
  }

  .console-inner {
    grid-template-columns: 1fr;
    min-height: auto;
  }

  .side-rail {
    display: none;
  }

  .dashboard-board,
  .chart-card {
    padding: 20px;
  }
}

@media (max-width: 430px) {
  .home-shell {
    width: min(100% - 28px, 1180px);
  }

  .home-title {
    font-size: 2.6rem;
  }

  .hero-actions .button {
    width: 100%;
  }

  .metrics {
    grid-template-columns: 1fr;
  }

  .chart-head {
    align-items: flex-start;
    flex-direction: column;
  }

  .step-strip {
    padding: 34px 18px;
  }
}

@media (prefers-reduced-motion: reduce) {
  *,
  *::before,
  *::after {
    animation-duration: 0.01ms !important;
    animation-iteration-count: 1 !important;
    scroll-behavior: auto !important;
    transition-duration: 0.01ms !important;
  }
}
</style>
