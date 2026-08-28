<template>
  <BaseDialog
    :show="show"
    :title="t('admin.accounts.bulkTest.title')"
    width="normal"
    :close-disabled="submitting"
    @close="requestClose"
  >
    <div class="space-y-5">
      <div
        class="overflow-hidden rounded-2xl border border-sky-200 bg-sky-50/80 dark:border-sky-800/70 dark:bg-sky-950/30"
      >
        <div class="flex items-start gap-3 p-4">
          <div
            class="flex h-11 w-11 shrink-0 items-center justify-center rounded-xl bg-sky-600 text-white shadow-sm shadow-sky-600/20"
          >
            <Icon name="play" size="md" />
          </div>
          <div class="min-w-0">
            <p class="font-semibold text-sky-950 dark:text-sky-100">
              {{ t('admin.accounts.bulkTest.description') }}
            </p>
            <p class="mt-1 text-sm leading-6 text-sky-800 dark:text-sky-300">
              {{ t('admin.accounts.bulkTest.samePlatformHint') }}
            </p>
          </div>
        </div>

        <div
          class="grid grid-cols-1 border-t border-sky-200/80 bg-white/60 sm:grid-cols-2 dark:border-sky-800/70 dark:bg-black/10"
        >
          <div class="px-4 py-3">
            <p class="text-xs font-medium uppercase tracking-wide text-gray-500 dark:text-gray-400">
              {{ t('admin.accounts.bulkTest.selectedCountLabel') }}
            </p>
            <p class="mt-1 text-base font-semibold text-gray-900 dark:text-gray-100">
              {{ t('admin.accounts.bulkTest.selectedCount', { count: selectedCount }) }}
            </p>
          </div>
          <div class="border-t border-sky-200/80 px-4 py-3 sm:border-l sm:border-t-0 dark:border-sky-800/70">
            <p class="text-xs font-medium uppercase tracking-wide text-gray-500 dark:text-gray-400">
              {{ t('admin.accounts.bulkTest.platformLabel') }}
            </p>
            <p class="mt-1 truncate text-base font-semibold capitalize text-gray-900 dark:text-gray-100">
              {{ representativeAccount?.platform ?? '—' }}
            </p>
          </div>
        </div>
      </div>

      <div class="space-y-2">
        <label class="text-sm font-semibold text-gray-800 dark:text-gray-200">
          {{ t('admin.accounts.bulkTest.modelLabel') }}
        </label>
        <Select
          v-model="selectedModelId"
          :options="availableModels"
          :disabled="loadingModels || submitting || modelLoadFailed"
          value-key="id"
          label-key="display_name"
          :placeholder="
            loadingModels
              ? `${t('common.loading')}...`
              : t('admin.accounts.bulkTest.modelPlaceholder')
          "
        />
        <p class="text-sm leading-6 text-gray-500 dark:text-gray-400">
          {{ t('admin.accounts.bulkTest.modelScopeHint') }}
        </p>
      </div>

      <div
        v-if="modelLoadFailed"
        class="flex flex-col gap-3 rounded-xl border border-red-200 bg-red-50 p-3 sm:flex-row sm:items-center sm:justify-between dark:border-red-800/60 dark:bg-red-950/30"
        role="alert"
      >
        <span class="text-sm text-red-700 dark:text-red-300">
          {{ t(modelLoadMessage || 'admin.accounts.bulkTest.modelLoadFailed') }}
        </span>
        <button
          type="button"
          class="btn btn-secondary min-h-11 shrink-0"
          :disabled="loadingModels"
          @click="loadAvailableModels"
        >
          {{ t('admin.accounts.bulkTest.retryModels') }}
        </button>
      </div>
    </div>

    <template #footer>
      <div class="flex w-full flex-col-reverse gap-2 sm:flex-row sm:justify-end">
        <button
          type="button"
          class="btn btn-secondary min-h-11 w-full sm:w-auto"
          :disabled="submitting"
          @click="requestClose"
        >
          {{ t('common.cancel') }}
        </button>
        <button
          type="button"
          class="btn btn-primary min-h-11 w-full sm:w-auto"
          :disabled="!selectedModelId || loadingModels || modelLoadFailed || submitting"
          @click="confirmSelection"
        >
          <Icon v-if="submitting" name="refresh" size="sm" class="mr-1.5 animate-spin" />
          {{
            submitting
              ? t('admin.accounts.bulkTest.submitting')
              : t('admin.accounts.bulkTest.submit')
          }}
        </button>
      </div>
    </template>
  </BaseDialog>
</template>

<script setup lang="ts">
import { ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { adminAPI } from '@/api/admin'
import BaseDialog from '@/components/common/BaseDialog.vue'
import Select from '@/components/common/Select.vue'
import Icon from '@/components/icons/Icon.vue'
import type { Account, ClaudeModel } from '@/types'
import {
  prepareAccountTestModels,
  selectDefaultAccountTestModel
} from '@/utils/accountTestModels'

const props = defineProps<{
  show: boolean
  representativeAccount: Account | null
  accountIds: number[]
  selectedCount: number
  submitting: boolean
}>()

const emit = defineEmits<{
  close: []
  confirm: [modelId: string]
}>()

const { t } = useI18n()
const availableModels = ref<ClaudeModel[]>([])
const selectedModelId = ref('')
const loadingModels = ref(false)
const modelLoadFailed = ref(false)
const modelLoadMessage = ref('')

watch(
  () => props.show,
  async (show) => {
    if (!show) {
      availableModels.value = []
      selectedModelId.value = ''
      modelLoadFailed.value = false
      modelLoadMessage.value = ''
      return
    }
    await loadAvailableModels()
  }
)

async function loadAvailableModels(): Promise<void> {
  const account = props.representativeAccount
  if (!props.accountIds.length || !account) {
    modelLoadFailed.value = true
    modelLoadMessage.value = 'admin.accounts.bulkTest.modelLoadFailed'
    return
  }

  loadingModels.value = true
  modelLoadFailed.value = false
  modelLoadMessage.value = ''
  selectedModelId.value = ''
  try {
    const models = await adminAPI.accounts.getBatchTestModelOptions(props.accountIds)
    availableModels.value = prepareAccountTestModels(models, account.platform)
    selectedModelId.value = selectDefaultAccountTestModel(availableModels.value, account.platform)
    modelLoadFailed.value = availableModels.value.length === 0
  } catch (error) {
    console.error('Failed to load models for batch account test:', error)
    availableModels.value = []
    modelLoadFailed.value = true
    modelLoadMessage.value = mapBatchModelsLoadError(error)
  } finally {
    loadingModels.value = false
  }
}

function mapBatchModelsLoadError(error: unknown): string {
  const reason = (error as { reason?: unknown } | null)?.reason
  switch (reason) {
    case 'ACCOUNT_TEST_MODEL_CATALOG_EMPTY':
      return 'admin.accounts.modelsCatalogEmpty'
    case 'ACCOUNT_TEST_MODEL_WHITELIST_MISSING':
      return 'admin.accounts.modelsWhitelistMissing'
    case 'ACCOUNT_TEST_MODEL_NO_PRICED_INTERSECTION':
      return 'admin.accounts.modelsNoPricedIntersection'
    case 'ACCOUNT_TEST_PROTOCOL_NO_SUPPORTED_MODELS':
      return 'admin.accounts.modelsProtocolNoModels'
    default:
      return 'admin.accounts.bulkTest.modelLoadFailed'
  }
}

function requestClose(): void {
  if (!props.submitting) emit('close')
}

function confirmSelection(): void {
  const modelId = selectedModelId.value.trim()
  if (!modelId || props.submitting) return
  emit('confirm', modelId)
}
</script>
