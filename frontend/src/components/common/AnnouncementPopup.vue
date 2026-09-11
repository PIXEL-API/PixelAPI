<template>
  <BaseDialog
    :show="Boolean(announcementStore.currentPopup)"
    :title="announcementStore.currentPopup?.title || t('announcements.title')"
    width="wide"
    panel-class="announcement-popup-panel"
    body-class="announcement-popup-body"
    :close-on-click-outside="false"
    @close="handleDismiss"
  >
    <template #title-prefix>
      <div class="announcement-popup-icon flex h-10 w-10 flex-none items-center justify-center rounded-xl text-white shadow-lg">
        <svg class="h-5 w-5" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2">
          <path stroke-linecap="round" stroke-linejoin="round" d="M15 17h5l-1.405-1.405A2.032 2.032 0 0118 14.158V11a6.002 6.002 0 00-4-5.659V5a2 2 0 10-4 0v.341C7.67 6.165 6 8.388 6 11v3.159c0 .538-.214 1.055-.595 1.436L4 17h5m6 0v1a3 3 0 11-6 0v-1m6 0H9" />
        </svg>
      </div>
    </template>

    <template #title-extra>
      <div class="announcement-popup-meta hidden items-center gap-2 text-xs sm:flex">
        <span class="inline-flex items-center gap-1.5 rounded-lg px-2.5 py-1 font-medium">
          <span class="relative flex h-2 w-2">
            <span class="absolute inline-flex h-full w-full animate-ping rounded-full bg-current opacity-75"></span>
            <span class="relative inline-flex h-2 w-2 rounded-full bg-current"></span>
          </span>
          {{ t('announcements.unread') }}
        </span>
        <time class="text-gray-500 dark:text-gray-400">
          {{ announcementStore.currentPopup ? formatRelativeWithDateTime(announcementStore.currentPopup.created_at) : '' }}
        </time>
      </div>
    </template>

    <div class="relative">
      <div class="absolute bottom-0 left-0 top-0 w-1 rounded-full bg-gradient-to-b from-amber-500 via-orange-500 to-yellow-500"></div>
      <div class="pl-6">
        <div
          class="markdown-body prose prose-sm max-w-none dark:prose-invert"
          v-html="renderedContent"
        ></div>
      </div>
    </div>

    <template #footer>
      <button
        type="button"
        @click="handleDismiss"
        class="btn btn-primary announcement-popup-dismiss"
      >
        <span class="flex items-center gap-2">
          <svg class="h-4 w-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2">
            <path stroke-linecap="round" stroke-linejoin="round" d="M5 13l4 4L19 7" />
          </svg>
          {{ t('announcements.markRead') }}
        </span>
      </button>
    </template>
  </BaseDialog>
</template>
<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { marked } from 'marked'
import DOMPurify from 'dompurify'
import BaseDialog from '@/components/common/BaseDialog.vue'
import { useAnnouncementStore } from '@/stores/announcements'
import { formatRelativeWithDateTime } from '@/utils/format'

const { t } = useI18n()
const announcementStore = useAnnouncementStore()

marked.setOptions({
  breaks: true,
  gfm: true,
})

const renderedContent = computed(() => {
  const content = announcementStore.currentPopup?.content
  if (!content) return ''
  const html = marked.parse(content) as string
  return DOMPurify.sanitize(html)
})

function handleDismiss() {
  announcementStore.dismissPopup()
}
</script>
<style scoped>
:deep(.announcement-popup-panel) {
  max-width: min(680px, calc(100vw - 2rem));
  overflow: hidden;
}

:deep(.announcement-popup-panel .modal-header) {
  position: relative;
  overflow: hidden;
  border-bottom-color: color-mix(in srgb, #f59e0b 18%, transparent);
  background: linear-gradient(135deg, color-mix(in srgb, #fff7ed 90%, transparent), color-mix(in srgb, #ffedd5 50%, transparent));
}

:deep(.dark .announcement-popup-panel .modal-header) {
  border-bottom-color: color-mix(in srgb, #78350f 35%, transparent);
  background: linear-gradient(135deg, color-mix(in srgb, #451a03 28%, transparent), color-mix(in srgb, #7c2d12 18%, transparent));
}

:deep(.announcement-popup-panel .modal-header::after) {
  position: absolute;
  inset: 0 0 0 auto;
  width: 16rem;
  background: linear-gradient(to left, color-mix(in srgb, #fed7aa 30%, transparent), transparent);
  content: '';
  pointer-events: none;
}

:deep(.announcement-popup-panel .modal-header > *) {
  position: relative;
  z-index: 1;
}

:deep(.announcement-popup-panel .modal-title) {
  font-size: 1.25rem;
  font-weight: 700;
  line-height: 1.35;
}

.announcement-popup-icon {
  background: linear-gradient(135deg, #f59e0b, #ea580c);
  box-shadow: 0 12px 24px -12px rgb(245 158 11 / 0.75);
}

.announcement-popup-meta > span {
  color: #c2410c;
  background: rgb(255 237 213 / 0.8);
}

:deep(.dark .announcement-popup-meta > span) {
  color: #fdba74;
  background: rgb(124 45 18 / 0.3);
}

:deep(.announcement-popup-body) {
  max-height: 50vh;
  padding: 2rem;
}

.announcement-popup-dismiss {
  background: linear-gradient(135deg, #f59e0b, #ea580c);
  box-shadow: 0 12px 24px -12px rgb(245 158 11 / 0.75);
}

.announcement-popup-dismiss:hover {
  filter: brightness(1.05);
  transform: translateY(-1px);
}

@media (max-width: 639px) {
  :deep(.announcement-popup-body) {
    padding: 1.25rem;
  }
}
</style>
