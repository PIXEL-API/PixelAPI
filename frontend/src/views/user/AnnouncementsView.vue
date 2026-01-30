<template>
  <AppLayout>
    <TablePageLayout>
      <template #actions>
        <div class="flex justify-end gap-3">
          <button
            @click="loadAnnouncements"
            :disabled="loading"
            class="btn btn-secondary"
            :title="t('common.refresh')"
          >
            <Icon name="refresh" size="md" :class="loading ? 'animate-spin' : ''" />
          </button>
        </div>
      </template>

      <template #filters>
        <div class="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
          <label class="flex items-center gap-2 text-sm text-gray-700 dark:text-gray-300">
            <input v-model="unreadOnly" type="checkbox" class="h-4 w-4 rounded border-gray-300" />
            <span>{{ t('announcements.unreadOnly') }}</span>
          </label>
        </div>
      </template>

      <template #table>
        <div v-if="loading" class="flex items-center justify-center py-10">
          <Icon name="refresh" size="lg" class="animate-spin text-gray-400" />
        </div>

        <div v-else-if="announcements.length === 0" class="py-12 text-center text-gray-500 dark:text-gray-400">
          {{ unreadOnly ? t('announcements.emptyUnread') : t('announcements.empty') }}
        </div>

        <div v-else class="space-y-4">
          <div
            v-for="item in announcements"
            :key="item.id"
            class="rounded-2xl border border-gray-200 bg-white p-5 shadow-sm dark:border-dark-700 dark:bg-dark-800"
          >
            <div class="flex items-start justify-between gap-4">
              <div class="min-w-0 flex-1">
                <div class="flex items-center gap-2">
                  <h3 class="truncate text-base font-semibold text-gray-900 dark:text-white">
                    {{ item.title }}
                  </h3>
                  <span v-if="!item.read_at" class="badge badge-warning">
                    {{ t('announcements.unread') }}
                  </span>
                  <span v-else class="badge badge-success">
                    {{ t('announcements.read') }}
                  </span>
                </div>
                <div class="mt-1 flex flex-wrap items-center gap-x-4 gap-y-1 text-xs text-gray-500 dark:text-dark-400">
                  <span>{{ formatDateTime(item.created_at) }}</span>
                  <span v-if="item.starts_at">
                    {{ t('announcements.startsAt') }}: {{ formatDateTime(item.starts_at) }}
                  </span>
                  <span v-if="item.ends_at">
                    {{ t('announcements.endsAt') }}: {{ formatDateTime(item.ends_at) }}
                  </span>
                </div>
              </div>

              <div class="flex flex-shrink-0 items-center gap-2">
                <button
                  v-if="!item.read_at"
                  class="btn btn-secondary"
                  :disabled="markingReadId === item.id"
                  @click="markRead(item.id)"
                >
                  {{ markingReadId === item.id ? t('common.processing') : t('announcements.markRead') }}
                </button>
                <span v-else class="text-xs text-gray-500 dark:text-dark-400">
                  {{ t('announcements.readAt') }}: {{ formatDateTime(item.read_at) }}
                </span>
              </div>
            </div>

            <div class="mt-4 whitespace-pre-wrap text-sm text-gray-700 dark:text-gray-200">
              {{ item.content }}
            </div>
          </div>
        </div>
      </template>
    </TablePageLayout>
  </AppLayout>
</template>

<script setup lang="ts">
import { onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { announcementsAPI } from '@/api'
import { useAppStore } from '@/stores/app'
import { formatDateTime } from '@/utils/format'
import type { UserAnnouncement } from '@/types'
import AppLayout from '@/components/layout/AppLayout.vue'
import TablePageLayout from '@/components/layout/TablePageLayout.vue'
import Icon from '@/components/icons/Icon.vue'

const { t } = useI18n()
const appStore = useAppStore()

const announcements = ref<UserAnnouncement[]>([])
const loading = ref(false)
const unreadOnly = ref(false)
const markingReadId = ref<number | null>(null)

async function loadAnnouncements() {
  try {
    loading.value = true
    announcements.value = await announcementsAPI.list(unreadOnly.value)
  } catch (err: any) {
    appStore.showError(err?.message || t('common.unknownError'))
  } finally {
    loading.value = false
  }
}

async function markRead(id: number) {
  if (markingReadId.value) return
  try {
    markingReadId.value = id
    await announcementsAPI.markRead(id)
    await loadAnnouncements()
  } catch (err: any) {
    appStore.showError(err?.message || t('common.unknownError'))
  } finally {
    markingReadId.value = null
  }
}

watch(unreadOnly, () => {
  loadAnnouncements()
})

onMounted(() => {
  loadAnnouncements()
})
</script>
