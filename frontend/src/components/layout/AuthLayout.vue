<template>
  <div class="auth-shell">
    <div class="auth-backdrop"></div>

    <!-- Content Container -->
    <div class="auth-content">
      <!-- Logo/Brand -->
      <div class="auth-brand">
        <!-- Custom Logo or Default Logo -->
        <template v-if="settingsLoaded">
          <div class="auth-logo">
            <img :src="siteLogo || '/logo.png'" alt="Logo" class="h-full w-full object-contain" />
          </div>
          <h1 class="auth-title">
            {{ siteName }}
          </h1>
          <p class="auth-subtitle">
            {{ siteSubtitle }}
          </p>
        </template>
      </div>

      <!-- Card Container -->
      <div class="auth-card">
        <slot />
      </div>

      <!-- Footer Links -->
      <div class="auth-footer">
        <slot name="footer" />
      </div>

      <!-- Copyright -->
      <div class="auth-copyright">
        &copy; {{ currentYear }} {{ siteName }}. All rights reserved.
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted } from 'vue'
import { useAppStore } from '@/stores'
import { sanitizeUrl } from '@/utils/url'

const appStore = useAppStore()

const siteName = computed(() => appStore.siteName || 'Sub2API')
const siteLogo = computed(() => sanitizeUrl(appStore.siteLogo || '', { allowRelative: true, allowDataUrl: true }))
const siteSubtitle = computed(() => appStore.cachedPublicSettings?.site_subtitle || 'Subscription to API Conversion Platform')
const settingsLoaded = computed(() => appStore.publicSettingsLoaded)

const currentYear = computed(() => new Date().getFullYear())

onMounted(() => {
  appStore.fetchPublicSettings()
})
</script>

<style scoped>
.auth-shell {
  position: relative;
  display: flex;
  min-height: 100vh;
  align-items: center;
  justify-content: center;
  overflow: hidden;
  padding: 1rem;
  color: #10223a;
  background: #f8fcff;
  font-family:
    "Segoe UI",
    "PingFang SC",
    "Microsoft YaHei",
    system-ui,
    sans-serif;
}

.auth-shell::before {
  position: absolute;
  inset: -22%;
  z-index: 0;
  background:
    radial-gradient(circle at 18% 24%, rgba(91, 168, 255, 0.34), transparent 26%),
    radial-gradient(circle at 82% 18%, rgba(34, 198, 243, 0.22), transparent 24%),
    radial-gradient(circle at 50% 86%, rgba(167, 208, 255, 0.3), transparent 30%),
    linear-gradient(125deg, #ffffff 0%, #eaf6ff 36%, #ffffff 58%, #dceeff 100%);
  background-size: 130% 130%;
  animation: auth-ambient-shift 18s ease-in-out infinite alternate;
  content: "";
  pointer-events: none;
}

.auth-shell::after {
  position: absolute;
  inset: 0;
  z-index: 0;
  background:
    radial-gradient(circle at 50% 0%, rgba(255, 255, 255, 0.92), transparent 34%),
    linear-gradient(to bottom, rgba(255, 255, 255, 0.24), rgba(255, 255, 255, 0.78));
  content: "";
  pointer-events: none;
}

.auth-backdrop {
  position: absolute;
  inset: 0;
  z-index: 0;
  background:
    radial-gradient(circle at 30% 36%, rgba(107, 155, 240, 0.08), transparent 18%),
    radial-gradient(circle at 70% 68%, rgba(99, 180, 225, 0.1), transparent 22%);
  pointer-events: none;
}

.auth-content {
  position: relative;
  z-index: 1;
  width: min(100%, 30rem);
  animation: auth-rise 620ms ease both;
}

.auth-brand {
  margin-bottom: 1.75rem;
  text-align: center;
}

.auth-logo {
  display: inline-flex;
  width: 4rem;
  height: 4rem;
  align-items: center;
  justify-content: center;
  overflow: hidden;
  border: 1px solid rgba(107, 155, 240, 0.14);
  border-radius: 1.125rem;
  background: rgba(255, 255, 255, 0.68);
  box-shadow: 0 18px 42px rgba(107, 155, 240, 0.14);
  backdrop-filter: blur(16px);
}

.auth-title {
  margin: 0.875rem 0 0;
  background: linear-gradient(92deg, #537fd9 0%, #63b4e1 46%, #10223a 100%);
  -webkit-background-clip: text;
  background-clip: text;
  color: transparent;
  font-size: 1.875rem;
  font-weight: 820;
  line-height: 1.18;
  letter-spacing: 0;
}

.auth-subtitle {
  margin-top: 0.5rem;
  color: #60738d;
  font-size: 0.875rem;
}

.auth-card {
  border: 1px solid rgba(107, 155, 240, 0.08);
  border-radius: 1.375rem;
  background: rgba(255, 255, 255, 0.64);
  padding: 2rem;
  box-shadow: 0 28px 78px rgba(38, 86, 143, 0.12);
  backdrop-filter: blur(22px);
}

.auth-footer {
  margin-top: 1.5rem;
  color: #60738d;
  text-align: center;
  font-size: 0.875rem;
}

.auth-copyright {
  margin-top: 2rem;
  color: #8da0b7;
  text-align: center;
  font-size: 0.75rem;
}

.auth-card :deep(h2) {
  color: #10223a;
}

.auth-card :deep(p),
.auth-card :deep(.text-gray-500),
.auth-card :deep(.dark\:text-dark-400) {
  color: #60738d;
}

.auth-card :deep(.input-label) {
  color: #29415d;
}

.auth-card :deep(.input) {
  min-height: 2.875rem;
  border-color: rgba(107, 155, 240, 0.14);
  background: rgba(255, 255, 255, 0.72);
  color: #10223a;
  box-shadow: 0 10px 28px rgba(38, 86, 143, 0.04);
}

.auth-card :deep(.input::placeholder) {
  color: #9aaabe;
}

.auth-card :deep(.input:focus) {
  border-color: #8fb6f4;
  box-shadow:
    0 0 0 3px rgba(107, 155, 240, 0.14),
    0 12px 32px rgba(107, 155, 240, 0.08);
}

.auth-card :deep(.text-gray-400),
.auth-card :deep(.dark\:text-dark-500) {
  color: #9aaabe;
}

.auth-card :deep(.btn-primary) {
  min-height: 2.875rem;
  border-radius: 0.875rem;
  background: linear-gradient(92deg, #8fb6f4 0%, #6b9bf0 54%, #63b4e1 100%);
  box-shadow: 0 14px 34px rgba(107, 155, 240, 0.2);
}

.auth-card :deep(.btn-primary:hover) {
  background: linear-gradient(92deg, #7fa8ee 0%, #5f8fe3 54%, #57a9d8 100%);
  box-shadow: 0 16px 38px rgba(107, 155, 240, 0.24);
}

.auth-card :deep(.text-primary-600),
.auth-footer :deep(.text-primary-600),
.auth-card :deep(.dark\:text-primary-400),
.auth-footer :deep(.dark\:text-primary-400) {
  color: #537fd9;
}

.auth-card :deep(.hover\:text-primary-500:hover),
.auth-footer :deep(.hover\:text-primary-500:hover),
.auth-card :deep(.dark\:hover\:text-primary-300:hover),
.auth-footer :deep(.dark\:hover\:text-primary-300:hover) {
  color: #4167b8;
}

.auth-card :deep(.bg-gray-200),
.auth-card :deep(.dark\:bg-dark-700) {
  background-color: rgba(107, 155, 240, 0.12);
}

@keyframes auth-ambient-shift {
  0% {
    transform: translate3d(-2%, -1%, 0) scale(1);
  }
  100% {
    transform: translate3d(2%, 1%, 0) scale(1.04);
  }
}

@keyframes auth-rise {
  from {
    opacity: 0;
    transform: translateY(14px);
  }
  to {
    opacity: 1;
    transform: translateY(0);
  }
}

@media (max-width: 430px) {
  .auth-card {
    padding: 1.375rem;
  }

  .auth-title {
    font-size: 1.625rem;
  }
}

@media (prefers-reduced-motion: reduce) {
  .auth-shell::before,
  .auth-content {
    animation-duration: 0.01ms !important;
    animation-iteration-count: 1 !important;
  }
}
</style>
