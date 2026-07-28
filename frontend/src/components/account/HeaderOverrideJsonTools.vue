<template>
  <button type="button" class="rounded-lg bg-primary-50 px-3 py-1 text-xs text-primary-700" @click="toggleImport">
    {{ t('admin.accounts.headerOverride.importJson') }}
  </button>
  <button
    type="button"
    class="rounded-lg bg-primary-50 px-3 py-1 text-xs text-primary-700 disabled:opacity-50"
    :disabled="!hasNamedRows"
    @click="copyAsJson"
  >
    {{ t('admin.accounts.headerOverride.copyJson') }}
  </button>
  <div v-if="showImport" ref="importPanelRef" class="w-full space-y-2">
    <textarea
      ref="importTextareaRef"
      v-model="importText"
      rows="5"
      class="input font-mono text-xs"
      :placeholder="IMPORT_JSON_PLACEHOLDER"
    />
    <div class="flex gap-2">
      <button type="button" class="btn btn-primary py-1 text-xs" @click="applyImport">
        {{ t('admin.accounts.headerOverride.importJsonApply') }}
      </button>
      <button type="button" class="btn btn-secondary py-1 text-xs" @click="closeImport">
        {{ t('admin.accounts.headerOverride.importJsonCancel') }}
      </button>
    </div>
    <p class="text-xs text-gray-500 dark:text-gray-400">
      {{ t('admin.accounts.headerOverride.importJsonHint') }}
    </p>
  </div>
</template>

<script setup lang="ts">
import { computed, nextTick, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAppStore } from '@/stores/app'
import { useClipboard } from '@/composables/useClipboard'
import {
  parseHeaderOverridesJson,
  serializeHeaderOverrideRows,
  type HeaderOverrideRow
} from './credentialsBuilder'

const props = defineProps<{ rows: HeaderOverrideRow[] }>()
const emit = defineEmits<{ 'update:rows': [rows: HeaderOverrideRow[]] }>()
const { t } = useI18n()
const appStore = useAppStore()
const { copyToClipboard } = useClipboard()
const showImport = ref(false)
const importText = ref('')
const importPanelRef = ref<HTMLElement | null>(null)
const importTextareaRef = ref<HTMLTextAreaElement | null>(null)
const IMPORT_JSON_PLACEHOLDER =
  '{"user-agent": "my-client/1.0", "x-relay-token": "..."}'
const hasNamedRows = computed(() => props.rows.some((row) => row.name.trim()))
const toggleImport = async () => {
  showImport.value = !showImport.value
  if (!showImport.value) return
  await nextTick()
  importPanelRef.value?.scrollIntoView({ block: 'nearest', behavior: 'smooth' })
  importTextareaRef.value?.focus({ preventScroll: true })
}
const closeImport = () => {
  showImport.value = false
  importText.value = ''
}
const applyImport = () => {
  const rows = parseHeaderOverridesJson(importText.value)
  if (!rows) {
    appStore.showError(t('admin.accounts.headerOverride.importJsonInvalid'))
    return
  }
  emit('update:rows', rows)
  closeImport()
}
const copyAsJson = () => void copyToClipboard(serializeHeaderOverrideRows(props.rows))
</script>
