<template>
  <div class="app-shell" :class="{ 'app-shell-viewport': contentLayout === 'viewport' }">
    <!-- Sidebar -->
    <AppSidebar />

    <!-- Main Content Area -->
    <div
      class="app-main-shell"
      :class="[
        sidebarCollapsed ? 'lg:ml-[72px]' : 'lg:ml-64',
        { 'app-main-shell-viewport': contentLayout === 'viewport' },
      ]"
    >
      <!-- Header -->
      <AppHeader :class="{ 'app-header-viewport': contentLayout === 'viewport' }" />

      <!-- Main Content -->
      <main
        class="app-content"
        :class="{ 'app-content-viewport': contentLayout === 'viewport' }"
        :data-ui-skin="uiSkin"
      >
        <slot />
      </main>
    </div>
  </div>
</template>

<script setup lang="ts">
import '@/styles/onboarding.css'
import { computed, onMounted } from 'vue'
import { useAppStore } from '@/stores'
import { useAuthStore } from '@/stores/auth'
import { useOnboardingTour } from '@/composables/useOnboardingTour'
import { useOnboardingStore } from '@/stores/onboarding'
import AppSidebar from './AppSidebar.vue'
import AppHeader from './AppHeader.vue'
import { useUiSkin } from '@/composables/useUiSkin'

withDefaults(defineProps<{
  contentLayout?: 'default' | 'viewport'
}>(), {
  contentLayout: 'default'
})

const appStore = useAppStore()
const authStore = useAuthStore()
const sidebarCollapsed = computed(() => appStore.sidebarCollapsed)
const uiSkin = useUiSkin()
const isAdmin = computed(() => authStore.user?.role === 'admin')

const { replayTour } = useOnboardingTour({
  storageKey: isAdmin.value ? 'admin_guide' : 'user_guide',
  autoStart: true
})

const onboardingStore = useOnboardingStore()

onMounted(() => {
  onboardingStore.setReplayCallback(replayTour)
})

defineExpose({ replayTour })
</script>

<style scoped>
.app-shell.app-shell-viewport {
  height: 100dvh;
  min-height: 0;
  overflow: hidden;
}

.app-main-shell.app-main-shell-viewport {
  display: flex;
  height: 100%;
  min-height: 0;
  flex-direction: column;
  overflow: hidden;
}

.app-header-viewport {
  flex-shrink: 0;
}

.app-content.app-content-viewport {
  width: 100%;
  min-height: 0;
  max-width: none;
  flex: 1;
  overflow: hidden;
  margin: 0;
  padding: 0.75rem;
}

@media (min-width: 1024px) {
  .app-content.app-content-viewport {
    padding: 1rem;
  }
}
</style>
