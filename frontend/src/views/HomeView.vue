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
          <svg v-else viewBox="0 0 24 24" fill="none">
            <path
              d="M7 8.5h7.5a4 4 0 0 1 0 8H9.2"
              stroke="currentColor"
              stroke-width="1.8"
              stroke-linecap="round"
            />
            <path
              d="M17 15.5H9.5a4 4 0 0 1 0-8h5.3"
              stroke="currentColor"
              stroke-width="1.8"
              stroke-linecap="round"
            />
          </svg>
        </span>
        <span>{{ siteName }}</span>
      </router-link>

      <nav class="home-nav-links" aria-label="Home sections">
        <a href="#model">新范式</a>
        <a href="#settle">结算</a>
        <a href="#invite">邀请</a>
      </nav>

      <div class="home-actions">
        <LocaleSwitcher />

        <a
          v-if="docUrl"
          :href="docUrl"
          target="_blank"
          rel="noopener noreferrer"
          class="icon-action"
          :title="t('home.viewDocs')"
        >
          <Icon name="book" size="md" />
        </a>

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
        <span class="eyebrow">开放式 AI 账号协作网络</span>
        <h1>让账号能力在用户之间<span class="title-flow">自由流动</span></h1>
        <p class="lead">
          {{ siteName }}不再只是平台向用户提供服务，而是让个人用户提供账号、其他用户消费额度，平台完成调度中转、收益结算、邀请分成。
        </p>
        <div class="hero-actions" id="start">
          <router-link :to="isAuthenticated ? dashboardPath : '/login'" class="button primary">
            {{ isAuthenticated ? t('home.goToDashboard') : t('home.getStarted') }}
          </router-link>
          <a class="button secondary" href="#model">查看新模式</a>
        </div>
        <div class="essentials" aria-label="核心特性">
          <span id="model">B2C 转 C2C</span>
          <span id="settle">收益自动入账</span>
          <span id="invite">邀请消费 5% 分成</span>
          <span>倍率与消耗透明</span>
        </div>
      </section>

      <aside class="visual" aria-label="2开版协作网络示意">
        <div class="flowfield"></div>
        <div class="flow-panel">
          <span class="visual-kicker">settlement flow</span>
          <div class="flow-hero">
            <div class="flow-top">
              <div class="flow-title">
                <span>{{ siteName }} clearing layer</span>
                <strong>消费额度自动结算</strong>
              </div>
              <span class="live-pill">实时清算</span>
            </div>
            <div class="flow-mid">
              <div class="role"><b>账号提供者</b><span>接入可用账号</span></div>
              <div class="flow-core" aria-hidden="true">
                <span class="flow-light"></span>
                <span class="flow-chip">调度 · 计量 · 结算</span>
              </div>
              <div class="role"><b>收益账户</b><span>消费额度回流</span></div>
            </div>
            <div class="flow-bottom">
              <div class="metric-tile"><span>模式</span><strong>C2C</strong></div>
              <div class="metric-tile"><span>邀请分成</span><strong>5%</strong></div>
              <div class="metric-tile"><span>计价</span><strong>透明</strong></div>
            </div>
          </div>
          <div class="split">
            <div class="split-card">
              <b>账号供给市场化</b>
              <span>平台不再只依赖自有账号，个人账号能力可以进入供给网络。</span>
            </div>
            <div class="split-card">
              <b>邀请关系持续分成</b>
              <span>被邀请用户消费额度的 5% 结算给邀请人。</span>
            </div>
          </div>
        </div>
      </aside>
    </main>

    <footer class="home-shell home-footer">
      <span>&copy; {{ currentYear }} {{ siteName }}. {{ t('home.footer.allRightsReserved') }}</span>
      <span class="footer-links">
        <a
          v-if="docUrl"
          :href="docUrl"
          target="_blank"
          rel="noopener noreferrer"
        >
          {{ t('home.docs') }}
        </a>
        <a :href="githubUrl" target="_blank" rel="noopener noreferrer">GitHub</a>
      </span>
    </footer>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAuthStore, useAppStore } from '@/stores'
import LocaleSwitcher from '@/components/common/LocaleSwitcher.vue'
import Icon from '@/components/icons/Icon.vue'

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

// GitHub URL
const githubUrl = 'https://github.com/Wei-Shaw/sub2api'

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
})
</script>

<style scoped>
.home-page {
  position: relative;
  min-height: 100vh;
  overflow: hidden;
  color: #10223a;
  background: #f8fcff;
  font-family:
    "Segoe UI",
    "PingFang SC",
    "Microsoft YaHei",
    system-ui,
    sans-serif;
}

.home-page::before {
  position: absolute;
  inset: -22%;
  z-index: 0;
  background:
    radial-gradient(circle at 18% 24%, rgba(91, 168, 255, 0.38), transparent 26%),
    radial-gradient(circle at 82% 18%, rgba(34, 198, 243, 0.28), transparent 24%),
    radial-gradient(circle at 55% 86%, rgba(167, 208, 255, 0.34), transparent 30%),
    linear-gradient(125deg, #ffffff 0%, #eaf6ff 36%, #ffffff 58%, #dceeff 100%);
  background-size: 130% 130%;
  animation: ambient-shift 18s ease-in-out infinite alternate;
  content: "";
  pointer-events: none;
}

.home-page::after {
  position: absolute;
  inset: 0;
  z-index: 0;
  background:
    radial-gradient(circle at 50% 0%, rgba(255, 255, 255, 0.9), transparent 34%),
    linear-gradient(to bottom, rgba(255, 255, 255, 0.18), rgba(255, 255, 255, 0.72));
  content: "";
  pointer-events: none;
}

.home-shell {
  position: relative;
  z-index: 1;
  width: min(1180px, calc(100% - 32px));
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
  font-weight: 760;
}

.brand-mark {
  display: grid;
  width: 38px;
  height: 38px;
  place-items: center;
  overflow: hidden;
  border: 1px solid rgba(107, 155, 240, 0.14);
  border-radius: 12px;
  background: rgba(255, 255, 255, 0.72);
  color: #6b9bf0;
  box-shadow: 0 14px 34px rgba(107, 155, 240, 0.12);
  backdrop-filter: blur(16px);
}

.brand-mark img {
  width: 100%;
  height: 100%;
  object-fit: contain;
}

.brand-mark svg {
  width: 21px;
  height: 21px;
}

.home-nav-links {
  display: none;
  align-items: center;
  gap: 28px;
  color: #60738d;
  font-size: 0.94rem;
}

.home-nav-links a {
  transition: color 160ms ease;
}

.home-nav-links a:hover {
  color: #6b9bf0;
}

.home-actions {
  display: flex;
  align-items: center;
  gap: 10px;
}

.icon-action {
  display: inline-flex;
  min-width: 44px;
  min-height: 44px;
  align-items: center;
  justify-content: center;
  border: 1px solid rgba(107, 155, 240, 0.12);
  border-radius: 999px;
  background: rgba(255, 255, 255, 0.45);
  color: #60738d;
  transition:
    color 160ms ease,
    background 160ms ease,
    transform 160ms ease;
  backdrop-filter: blur(14px);
}

.icon-action:hover {
  transform: translateY(-1px);
  color: #6b9bf0;
  background: rgba(255, 255, 255, 0.72);
}

.button {
  display: inline-flex;
  min-height: 44px;
  align-items: center;
  justify-content: center;
  border: 1px solid transparent;
  border-radius: 999px;
  padding: 0 18px;
  cursor: pointer;
  font-size: 0.94rem;
  font-weight: 720;
  transition:
    transform 180ms ease,
    box-shadow 180ms ease,
    background 180ms ease,
    border-color 180ms ease;
}

.button:hover {
  transform: translateY(-1px);
}

.button.primary {
  background: #10223a;
  color: #fff;
  box-shadow: 0 18px 42px rgba(16, 34, 58, 0.18);
}

.button.secondary {
  border-color: rgba(107, 155, 240, 0.18);
  background: rgba(255, 255, 255, 0.54);
  color: #537fd9;
  backdrop-filter: blur(14px);
}

.nav-cta {
  gap: 8px;
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
  min-height: calc(100vh - 150px);
  align-items: center;
  gap: 54px;
  padding: 18px 0 76px;
}

.copy {
  max-width: 720px;
}

.eyebrow {
  display: inline-flex;
  width: fit-content;
  align-items: center;
  gap: 10px;
  border: 1px solid rgba(107, 155, 240, 0.14);
  border-radius: 999px;
  background: rgba(255, 255, 255, 0.54);
  color: #537fd9;
  padding: 8px 14px;
  font-size: 0.9rem;
  font-weight: 750;
  box-shadow: 0 12px 34px rgba(107, 155, 240, 0.08);
  backdrop-filter: blur(16px);
  animation: rise 620ms ease both;
}

.eyebrow::before {
  width: 8px;
  height: 8px;
  border-radius: 50%;
  background: #6b9bf0;
  content: "";
  box-shadow: 0 0 0 7px rgba(107, 155, 240, 0.1);
}

h1 {
  max-width: 780px;
  margin: 24px 0 0;
  font-size: 2.82rem;
  font-weight: 760;
  line-height: 1.12;
  letter-spacing: 0;
  animation: rise 700ms 80ms ease both;
}

.title-flow {
  display: inline-block;
  background: linear-gradient(92deg, #537fd9 0%, #63b4e1 46%, #10223a 100%);
  -webkit-background-clip: text;
  background-clip: text;
  color: transparent;
  font-weight: 820;
}

.lead {
  max-width: 650px;
  margin: 22px 0 0;
  color: #60738d;
  font-size: 1.09rem;
  line-height: 1.78;
  animation: rise 760ms 150ms ease both;
}

.hero-actions {
  display: flex;
  flex-wrap: wrap;
  gap: 12px;
  margin-top: 32px;
  animation: rise 820ms 220ms ease both;
}

.essentials {
  display: flex;
  flex-wrap: wrap;
  gap: 10px;
  margin-top: 34px;
  animation: rise 880ms 280ms ease both;
}

.essentials span {
  display: inline-flex;
  min-height: 40px;
  align-items: center;
  border: 1px solid rgba(107, 155, 240, 0.14);
  border-radius: 999px;
  background: rgba(255, 255, 255, 0.48);
  color: #29415d;
  padding: 0 14px;
  font-size: 0.91rem;
  font-weight: 680;
  backdrop-filter: blur(14px);
}

.visual {
  position: relative;
  min-height: 560px;
  animation: rise 780ms 160ms ease both;
}

.flowfield {
  position: absolute;
  inset: 0;
  overflow: hidden;
  border: 1px solid rgba(107, 155, 240, 0.06);
  border-radius: 46px;
  background:
    linear-gradient(145deg, rgba(255, 255, 255, 0.5), rgba(245, 251, 255, 0.2)),
    rgba(255, 255, 255, 0.2);
  box-shadow: 0 26px 76px rgba(38, 86, 143, 0.08);
  backdrop-filter: blur(20px);
}

.flowfield::before {
  position: absolute;
  inset: -20%;
  background:
    radial-gradient(circle at 30% 24%, rgba(107, 155, 240, 0.1), transparent 26%),
    radial-gradient(circle at 72% 66%, rgba(99, 180, 225, 0.12), transparent 28%);
  animation: field-breathe 9s ease-in-out infinite;
  content: "";
}

.flow-panel {
  position: absolute;
  z-index: 1;
  inset: 30px;
  display: grid;
  grid-template-rows: auto 1fr auto;
  gap: 16px;
}

.visual-kicker {
  color: #8da0b7;
  font-size: 0.84rem;
  font-weight: 760;
}

.flow-hero {
  position: relative;
  display: grid;
  min-height: 364px;
  align-content: space-between;
  overflow: hidden;
  border: 0;
  border-radius: 0;
  background: transparent;
  box-shadow: none;
  padding: 26px;
}

.flow-hero::before {
  position: absolute;
  inset: -30%;
  background:
    radial-gradient(circle at 20% 18%, rgba(107, 155, 240, 0.11), transparent 26%),
    radial-gradient(circle at 86% 78%, rgba(99, 180, 225, 0.13), transparent 28%);
  animation: field-breathe 9s ease-in-out infinite;
  content: "";
}

.flow-top,
.flow-bottom,
.flow-mid {
  position: relative;
  z-index: 1;
}

.flow-top {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 16px;
}

.flow-title span {
  display: block;
  color: #8da0b7;
  font-size: 0.8rem;
  font-weight: 720;
}

.flow-title strong {
  display: block;
  margin-top: 5px;
  color: #537fd9;
  font-size: 1.72rem;
  line-height: 1.08;
}

.live-pill {
  display: inline-flex;
  min-height: 36px;
  align-items: center;
  border-radius: 999px;
  background: rgba(107, 155, 240, 0.08);
  color: #537fd9;
  padding: 0 13px;
  font-size: 0.82rem;
  font-weight: 780;
}

.flow-mid {
  display: grid;
  grid-template-columns: 1fr 1.1fr 1fr;
  align-items: center;
  gap: 10px;
  margin: 26px 0 20px;
}

.role {
  position: relative;
  padding: 8px 4px;
}

.role::before {
  display: block;
  width: 32px;
  height: 3px;
  border-radius: 999px;
  margin-bottom: 14px;
  background: linear-gradient(90deg, #6b9bf0, rgba(99, 180, 225, 0.3));
  content: "";
}

.role b {
  display: block;
  font-size: 1.06rem;
}

.role span {
  display: block;
  margin-top: 5px;
  color: #60738d;
  font-size: 0.8rem;
}

.flow-core {
  position: relative;
  display: grid;
  height: 128px;
  place-items: center;
}

.flow-core::before,
.flow-core::after {
  position: absolute;
  left: 4%;
  right: 4%;
  height: 58px;
  overflow: hidden;
  border: 1px solid rgba(107, 155, 240, 0.16);
  border-top-color: rgba(107, 155, 240, 0.3);
  border-right-color: transparent;
  border-bottom-color: transparent;
  border-radius: 50%;
  background: radial-gradient(circle at 50% 0%, rgba(107, 155, 240, 0.08), transparent 58%);
  filter: drop-shadow(0 0 12px rgba(107, 155, 240, 0.14));
  content: "";
}

.flow-core::before {
  top: 18px;
  transform: rotate(8deg);
}

.flow-core::after {
  bottom: 18px;
  opacity: 0.66;
  transform: rotate(188deg);
}

.flow-light {
  position: absolute;
  z-index: 1;
  width: 12px;
  height: 12px;
  border-radius: 999px;
  background: #fff;
  box-shadow:
    0 0 0 6px rgba(107, 155, 240, 0.1),
    0 0 24px rgba(107, 155, 240, 0.3);
  animation: light-orbit 4.8s ease-in-out infinite;
}

.flow-chip {
  position: relative;
  z-index: 2;
  display: inline-flex;
  min-height: 44px;
  align-items: center;
  border-radius: 999px;
  background: rgba(255, 255, 255, 0.6);
  color: #537fd9;
  padding: 0 16px;
  font-size: 0.9rem;
  font-weight: 800;
  box-shadow: 0 12px 32px rgba(107, 155, 240, 0.08);
  backdrop-filter: blur(14px);
}

.flow-bottom {
  display: grid;
  grid-template-columns: repeat(3, 1fr);
  gap: 10px;
}

.metric-tile {
  position: relative;
  padding: 12px 0 0;
}

.metric-tile::before {
  display: block;
  width: 100%;
  height: 1px;
  margin-bottom: 12px;
  background: linear-gradient(90deg, rgba(107, 155, 240, 0.22), transparent);
  content: "";
}

.metric-tile span {
  display: block;
  color: #8da0b7;
  font-size: 0.75rem;
}

.metric-tile strong {
  display: block;
  margin-top: 5px;
  font-size: 1.14rem;
}

.split {
  display: grid;
  grid-template-columns: 1.15fr 0.85fr;
  gap: 12px;
}

.split-card {
  border-radius: 0;
  background: transparent;
  padding: 4px 0 0;
}

.split-card::before {
  display: block;
  width: 34px;
  height: 3px;
  border-radius: 999px;
  margin-bottom: 12px;
  background: rgba(107, 155, 240, 0.24);
  content: "";
}

.split-card b {
  display: block;
  margin-bottom: 5px;
  font-size: 0.98rem;
}

.split-card span {
  color: #60738d;
  font-size: 0.82rem;
  line-height: 1.5;
}

.home-footer {
  display: flex;
  padding: 22px 0;
  color: #8da0b7;
  font-size: 0.88rem;
  gap: 16px;
  justify-content: space-between;
}

.footer-links {
  display: flex;
  gap: 14px;
}

.footer-links a {
  transition: color 160ms ease;
}

.footer-links a:hover {
  color: #537fd9;
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

@keyframes field-breathe {
  0%,
  100% {
    transform: translate3d(-1%, -1%, 0) scale(1);
  }
  50% {
    transform: translate3d(1%, 1%, 0) scale(1.08);
  }
}

@keyframes light-orbit {
  0% {
    transform: translate(-92px, 18px);
    opacity: 0;
  }
  18% {
    opacity: 1;
  }
  50% {
    transform: translate(0, -26px);
    opacity: 1;
  }
  82% {
    opacity: 1;
  }
  100% {
    transform: translate(92px, 18px);
    opacity: 0;
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
    grid-template-columns: 0.96fr 1.04fr;
  }

  h1 {
    font-size: 4.5rem;
  }
}

@media (max-width: 760px) {
  .hero {
    min-height: auto;
  }

  .visual {
    min-height: 520px;
  }

  .flow-panel {
    inset: 20px;
  }

  .flow-mid,
  .flow-bottom,
  .split {
    grid-template-columns: 1fr;
  }

  .flow-core {
    height: 76px;
  }

  .flow-core::before,
  .flow-core::after {
    left: 22%;
    right: 22%;
    transform: rotate(90deg);
  }
}

@media (max-width: 640px) {
  .home-actions :deep(.locale-switcher) {
    display: none;
  }
}

@media (max-width: 430px) {
  .home-nav {
    min-height: 70px;
  }

  .nav-cta {
    display: none;
  }

  h1 {
    font-size: 2.62rem;
  }

  .lead {
    font-size: 1rem;
  }

  .hero-actions .button {
    width: 100%;
  }

  .flow-panel {
    inset: 16px;
  }

  .visual {
    min-height: 620px;
  }

  .flow-top {
    align-items: flex-start;
    flex-direction: column;
  }

  .flow-title strong {
    font-size: 1.36rem;
  }

  .role,
  .flow-hero,
  .split-card {
    border-radius: 18px;
  }

  .flow-bottom {
    grid-template-columns: 1fr;
  }

  .home-footer {
    flex-direction: column;
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
