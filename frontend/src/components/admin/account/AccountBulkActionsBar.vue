<template>
  <div
    class="mb-4 flex flex-col gap-3 rounded-lg bg-primary-50 p-3 sm:flex-row sm:items-center sm:justify-between dark:bg-primary-900/20"
  >
    <div class="flex flex-wrap items-center gap-2">
      <span v-if="selectedIds.length > 0" class="text-sm font-medium text-primary-900 dark:text-primary-100">
        {{ t('admin.accounts.bulkActions.selected', { count: selectedIds.length }) }}
      </span>
      <span v-else class="text-sm font-medium text-primary-900 dark:text-primary-100">
        {{ t('admin.accounts.bulkEdit.title') }}
      </span>
      <template v-if="selectedIds.length > 0">
      <button
        @click="$emit('select-page')"
        class="text-xs font-medium text-primary-700 hover:text-primary-800 dark:text-primary-300 dark:hover:text-primary-200"
      >
        {{ t('admin.accounts.bulkActions.selectCurrentPage') }}
      </button>
      <span class="text-gray-300 dark:text-primary-800">•</span>
      <button
        @click="$emit('clear')"
        class="text-xs font-medium text-primary-700 hover:text-primary-800 dark:text-primary-300 dark:hover:text-primary-200"
      >
        {{ t('admin.accounts.bulkActions.clear') }}
      </button>
      </template>
    </div>
    <div class="flex w-full flex-wrap gap-2 sm:w-auto sm:justify-end">
      <template v-if="selectedIds.length > 0">
        <button @click="$emit('delete')" class="btn btn-danger btn-sm min-h-11">{{ t('admin.accounts.bulkActions.delete') }}</button>
        <button @click="$emit('reset-status')" class="btn btn-secondary btn-sm min-h-11">{{ t('admin.accounts.bulkActions.resetStatus') }}</button>
        <button
          :disabled="testingConnections"
          @click="$emit('test-connection')"
          class="btn btn-secondary btn-sm min-h-11"
        >
          {{ t('admin.accounts.bulkActions.testConnection') }}
        </button>
        <button @click="$emit('refresh-token')" class="btn btn-secondary btn-sm min-h-11">{{ t('admin.accounts.bulkActions.refreshToken') }}</button>
        <button @click="$emit('toggle-schedulable', true)" class="btn btn-success btn-sm min-h-11">{{ t('admin.accounts.bulkActions.enableScheduling') }}</button>
        <button @click="$emit('toggle-schedulable', false)" class="btn btn-warning btn-sm min-h-11">{{ t('admin.accounts.bulkActions.disableScheduling') }}</button>
        <button @click="$emit('edit-selected')" class="btn btn-primary btn-sm min-h-11">{{ t('admin.accounts.bulkActions.edit') }}</button>
      </template>
      <button @click="$emit('edit-filtered')" class="btn btn-primary btn-sm min-h-11">
        {{ t('admin.accounts.bulkEdit.submit') }}
      </button>
    </div>
  </div>
</template>

<script setup lang="ts">
import { useI18n } from 'vue-i18n'

withDefaults(
  defineProps<{
    selectedIds: number[]
    testingConnections?: boolean
  }>(),
  {
    testingConnections: false
  }
)

defineEmits<{
  (event: 'delete'): void
  (event: 'edit-selected'): void
  (event: 'edit-filtered'): void
  (event: 'clear'): void
  (event: 'select-page'): void
  (event: 'toggle-schedulable', enabled: boolean): void
  (event: 'reset-status'): void
  (event: 'test-connection'): void
  (event: 'refresh-token'): void
}>()

const { t } = useI18n()
</script>
