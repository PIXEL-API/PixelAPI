<template>
  <header class="sticky top-0 z-30 border-b border-line bg-canvas/90 backdrop-blur-md">
    <div class="mx-auto flex min-h-16 max-w-6xl items-center gap-3 px-4 md:px-6">
      <RouterLink
        to="/ideas"
        class="group flex min-w-0 items-center gap-2.5 rounded-control py-1.5 pr-2 transition-colors hover:bg-surface-subtle"
        :aria-label="t('ideas.header.backToPlaza')"
      >
        <span
          class="flex h-9 w-9 flex-none items-center justify-center overflow-hidden rounded-control bg-surface-subtle shadow-sm ring-1 ring-line"
        >
          <img
            v-if="siteLogo"
            :src="siteLogo"
            :alt="t('ideas.header.logoAlt', { siteName })"
            class="h-full w-full object-contain"
          />
          <Icon v-else name="lightbulb" size="sm" class="text-brand" />
        </span>
        <span class="min-w-0">
          <span class="block truncate text-sm font-semibold text-content group-hover:text-brand">
            {{ siteName }}
          </span>
          <span class="hidden text-[11px] text-content-subtle sm:block">{{ t('ideas.header.communitySubtitle') }}</span>
        </span>
      </RouterLink>

      <div class="ml-auto flex items-center gap-1.5 sm:gap-2">
        <RouterLink
          to="/ideas/mine"
          class="inline-flex min-h-11 min-w-11 items-center justify-center gap-1.5 rounded-control px-2.5 text-sm font-medium text-content-muted transition-colors hover:bg-surface-subtle hover:text-content sm:px-3"
          :aria-label="t('ideas.header.myPosts')"
          :title="t('ideas.header.myPosts')"
        >
          <Icon name="document" size="sm" />
          <span class="hidden sm:inline">{{ t('ideas.header.myPosts') }}</span>
        </RouterLink>
        <RouterLink
          :to="dashboardPath"
          class="inline-flex min-h-11 min-w-11 items-center justify-center gap-1.5 rounded-control border border-line bg-surface px-2.5 text-sm font-medium text-content transition-colors hover:border-brand/40 hover:text-brand sm:px-3"
          :aria-label="t('ideas.header.backToDashboard')"
          :title="t('ideas.header.backToDashboard')"
        >
          <Icon name="grid" size="sm" />
          <span class="hidden sm:inline">{{ t('ideas.header.dashboard') }}</span>
        </RouterLink>
        <RouterLink
          to="/home"
          class="inline-flex min-h-11 min-w-11 items-center justify-center gap-1.5 rounded-control bg-gradient-primary px-2.5 text-sm font-semibold text-white shadow-card transition-transform hover:-translate-y-0.5 hover:shadow-elevated sm:px-3"
          :aria-label="t('ideas.header.backToHome')"
          :title="t('ideas.header.backToHome')"
        >
          <Icon name="home" size="sm" />
          <span class="hidden sm:inline">{{ t('ideas.header.home') }}</span>
        </RouterLink>
      </div>
    </div>
  </header>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAppStore } from '@/stores/app'
import { useAuthStore } from '@/stores/auth'
import Icon from '@/components/icons/Icon.vue'
import { sanitizeUrl } from '@/utils/url'

const appStore = useAppStore()
const authStore = useAuthStore()
const { t } = useI18n()
const siteName = computed(() => appStore.siteName || 'Sub2API')
const dashboardPath = computed(() => (authStore.isAdmin ? '/admin/dashboard' : '/dashboard'))
const siteLogo = computed(() =>
  sanitizeUrl(appStore.siteLogo || '', { allowRelative: true, allowDataUrl: true }),
)
</script>
