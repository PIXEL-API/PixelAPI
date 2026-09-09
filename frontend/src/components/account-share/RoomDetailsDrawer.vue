<template>
  <BaseDialog
    :show="listing !== null"
    :title="title"
    placement="right"
    panel-class="account-room-details-panel"
    body-class="account-room-details-body"
    close-on-click-outside
    @close="emit('close')"
  >
    <div v-if="listing" class="room-detail-shell" data-testid="room-details-drawer">
      <div class="room-detail-summary"><slot name="summary" :listing="listing" /></div>
      <nav ref="tabBar" class="room-detail-tabs" role="tablist" aria-label="房间详情内容" @keydown="handleTabKeydown">
        <button
          v-for="tab in tabs"
          :id="`room-detail-tab-${tab.key}`"
          :key="tab.key"
          type="button"
          role="tab"
          :data-tab="tab.key"
          :aria-selected="activeTab === tab.key"
          aria-controls="room-detail-panel"
          :tabindex="activeTab === tab.key ? 0 : -1"
          @click="activeTab = tab.key"
        >{{ tab.label }}</button>
      </nav>
      <div v-if="loading" class="room-detail-state" role="status"><Icon name="refresh" size="md" class="animate-spin" />正在读取房间详情…</div>
      <div v-else-if="error" class="room-detail-state room-detail-error" role="alert">
        <Icon name="exclamationCircle" size="lg" />
        <strong>暂时无法读取详情</strong>
        <p>{{ error }}</p>
        <button type="button" class="btn-secondary min-h-11" @click="emit('refresh')">重新加载</button>
      </div>
      <section v-else id="room-detail-panel" ref="contentPanel" class="room-detail-panel" role="tabpanel" :aria-labelledby="`room-detail-tab-${activeTab}`" tabindex="0">
        <slot :name="activeTab" :listing="listing" />
      </section>
    </div>
    <template #footer>
      <div v-if="listing && !loading && !error" class="room-detail-action-area">
        <slot name="actions" :listing="listing" :show-usage="showUsage" />
      </div>
      <span v-else class="room-detail-footer-note">详情加载完成后，可查看条款并选择使用。</span>
    </template>
  </BaseDialog>
</template>

<script setup lang="ts">
import { nextTick, ref, watch } from 'vue'
import type { AccountShareListing } from '@/api/accountShare'
import BaseDialog from '@/components/common/BaseDialog.vue'
import Icon from '@/components/icons/Icon.vue'

const props = defineProps<{
  listing: AccountShareListing | null
  title: string
  loading: boolean
  error: string
}>()
const emit = defineEmits<{ (event: 'close'): void; (event: 'refresh'): void }>()
const tabs = [
  { key: 'overview', label: '概览' },
  { key: 'models', label: '模型与限制' },
  { key: 'reviews', label: '用户评价' },
  { key: 'usage', label: '使用与管理' }
] as const
type DetailTab = typeof tabs[number]['key']
const activeTab = ref<DetailTab>('overview')
const tabBar = ref<HTMLElement | null>(null)
const contentPanel = ref<HTMLElement | null>(null)

async function selectAndFocusTab(key: DetailTab): Promise<void> {
  activeTab.value = key
  await nextTick()
  tabBar.value?.querySelector<HTMLButtonElement>(`[data-tab="${key}"]`)?.focus()
}

function showUsage(): void {
  void selectAndFocusTab('usage')
}

function handleTabKeydown(event: KeyboardEvent): void {
  const index = tabs.findIndex(tab => tab.key === activeTab.value)
  let nextIndex: number
  if (event.key === 'ArrowRight') nextIndex = (index + 1) % tabs.length
  else if (event.key === 'ArrowLeft') nextIndex = (index + tabs.length - 1) % tabs.length
  else if (event.key === 'Home') nextIndex = 0
  else if (event.key === 'End') nextIndex = tabs.length - 1
  else return
  event.preventDefault()
  void selectAndFocusTab(tabs[nextIndex].key)
}

watch(() => props.listing?.id, () => { activeTab.value = 'overview' })
watch(activeTab, async () => {
  await nextTick()
  const scrollContainer = contentPanel.value?.closest<HTMLElement>('.modal-body')
  if (scrollContainer) scrollContainer.scrollTop = 0
})
</script>

<style scoped>
.room-detail-shell { min-width: 0; }
.room-detail-summary { padding: .75rem 1rem; background: #f8fafc; border-bottom: 1px solid #e8ebef; }
.room-detail-tabs { display: flex; position: sticky; top: 0; z-index: 1; overflow-x: auto; padding: 0 .5rem; border-bottom: 1px solid #e8ebef; background: #fff; }
.room-detail-tabs button { min-height: 2.75rem; flex: 1 0 auto; border-bottom: 2px solid transparent; padding: .375rem .5rem; color: #657182; font-size: .8125rem; line-height: 1.5; white-space: nowrap; }
.room-detail-tabs button[aria-selected='true'] { color: #1673bf; font-weight: 600; border-bottom-color: currentColor; }
.room-detail-tabs button:focus-visible { outline: 2px solid #1673bf; outline-offset: -4px; border-radius: .5rem; }
.room-detail-panel { min-width: 0; padding: .875rem 1rem; }
.room-detail-state { display: flex; min-height: 10rem; flex-direction: column; justify-content: center; align-items: center; gap: .625rem; padding: 1.25rem 1rem; color: #657182; font-size: .875rem; text-align: center; }
.room-detail-state p { max-width: 32rem; margin: 0; font-size: .8125rem; line-height: 1.65; overflow-wrap: anywhere; }
.room-detail-error { color: #b42318; }
.room-detail-action-area { width: 100%; min-width: 0; }
.room-detail-footer-note { color: #657182; font-size: .8125rem; line-height: 1.6; }
.dark .room-detail-summary { background: #141a23; border-color: #303947; }
.dark .room-detail-tabs { background: #191e27; border-color: #303947; }
.dark .room-detail-tabs button, .dark .room-detail-state, .dark .room-detail-footer-note { color: #a0abba; }
.dark .room-detail-tabs button[aria-selected='true'] { color: #67b8ff; }
.dark .room-detail-error { color: #fda29b; }
@media (min-width: 640px) {
  .room-detail-summary { padding: 1rem 1.5rem; }
  .room-detail-panel { padding: 1.25rem 1.5rem; }
  .room-detail-tabs { padding-inline: 1rem; }
  .room-detail-tabs button { font-size: .875rem; }
}
</style>

<style>
.account-room-details-panel { --dialog-drawer-width: 46rem; }
.account-room-details-panel > .modal-header { min-height: 3.5rem; padding: .375rem 1rem; }
.account-room-details-panel .modal-title { font-size: 1rem; line-height: 1.5; overflow-wrap: anywhere; }
.account-room-details-panel > .modal-footer { padding: .625rem 1rem; padding-bottom: max(.625rem, env(safe-area-inset-bottom)); }
.account-room-details-body { padding: 0; overscroll-behavior: contain; }
@media (min-width: 640px) {
  .account-room-details-panel > .modal-header { padding: .5rem 1.5rem; }
  .account-room-details-panel > .modal-footer { padding: .875rem 1.5rem; }
}
</style>
