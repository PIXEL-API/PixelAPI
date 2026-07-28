<template>
  <div v-if="rows.length" class="space-y-2">
    <div
      v-for="(row, index) in rows"
      :key="getHeaderOverrideRowKey(row)"
      class="flex items-center gap-2"
    >
      <input
        v-model="row.name"
        type="text"
        class="input flex-1"
        :placeholder="t('admin.accounts.headerOverride.namePlaceholder')"
      />
      <input
        v-model="row.value"
        type="text"
        class="input flex-1"
        :placeholder="t('admin.accounts.headerOverride.valuePlaceholder')"
      />
      <button
        type="button"
        class="rounded-lg p-2 text-red-500 transition-colors hover:bg-red-50 hover:text-red-600 dark:hover:bg-red-900/20"
        :aria-label="t('common.delete')"
        @click="removeRow(index)"
      >
        <span aria-hidden="true">×</span>
      </button>
    </div>
  </div>
  <button
    type="button"
    class="w-full rounded-lg border-2 border-dashed border-gray-300 px-4 py-2 text-gray-600 transition-colors hover:border-gray-400 hover:text-gray-700 dark:border-dark-500 dark:text-gray-400 dark:hover:border-dark-400 dark:hover:text-gray-300"
    @click="addRow"
  >
    {{ t('admin.accounts.headerOverride.addRow') }}
  </button>
  <div class="flex flex-wrap gap-2">
    <HeaderOverrideJsonTools :rows="rows" @update:rows="emit('update:rows', $event)" />
  </div>
  <p class="text-xs text-gray-500 dark:text-gray-400">
    {{ t('admin.accounts.headerOverride.emptyValueHint') }}
  </p>
</template>

<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import { createStableObjectKeyResolver } from '@/utils/stableObjectKey'
import HeaderOverrideJsonTools from './HeaderOverrideJsonTools.vue'
import type { HeaderOverrideRow } from './credentialsBuilder'

const props = defineProps<{ rows: HeaderOverrideRow[] }>()
const emit = defineEmits<{ 'update:rows': [rows: HeaderOverrideRow[]] }>()
const { t } = useI18n()
const getHeaderOverrideRowKey =
  createStableObjectKeyResolver<HeaderOverrideRow>('header-override-row')
const addRow = () => emit('update:rows', [...props.rows, { name: '', value: '' }])
const removeRow = (index: number) =>
  emit('update:rows', props.rows.filter((_, rowIndex) => rowIndex !== index))
</script>
