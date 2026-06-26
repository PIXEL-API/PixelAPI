<template>
  <div v-if="visible" class="space-y-1">
    <div class="flex flex-wrap items-center gap-1.5">
      <button
        type="button"
        class="inline-flex items-center gap-0.5 rounded px-1.5 py-0.5 text-[10px] font-medium text-blue-600 transition-colors hover:bg-blue-50 disabled:cursor-not-allowed disabled:opacity-50 dark:text-blue-400 dark:hover:bg-blue-900/30"
        :disabled="loading || resetting || !hasQuotaAPI"
        :title="countButtonTitle"
        @click="handleQuery"
      >
        <svg
          class="h-2.5 w-2.5"
          :class="{ 'animate-spin': loading }"
          fill="none"
          stroke="currentColor"
          viewBox="0 0 24 24"
        >
          <path
            stroke-linecap="round"
            stroke-linejoin="round"
            stroke-width="2"
            d="M4 4v5h.582m15.356 2A8.001 8.001 0 004.582 9m0 0H9m11 11v-5h-.581m0 0a8.003 8.003 0 01-15.357-2m15.357 2H15"
          />
        </svg>
        {{ t('admin.accounts.openaiQuotaReset.count') }}<span v-if="data"> {{ availableResetCount }}</span>
      </button>

      <button
        type="button"
        class="inline-flex items-center gap-0.5 rounded px-1.5 py-0.5 text-[10px] font-medium text-orange-600 transition-colors hover:bg-orange-50 disabled:cursor-not-allowed disabled:opacity-50 dark:text-orange-400 dark:hover:bg-orange-900/30"
        :disabled="resetting || loading || !canReset"
        :title="resetButtonTitle"
        @click="handleResetRequest"
      >
        <svg
          class="h-2.5 w-2.5"
          :class="{ 'animate-spin': resetting }"
          fill="none"
          stroke="currentColor"
          viewBox="0 0 24 24"
        >
          <path
            stroke-linecap="round"
            stroke-linejoin="round"
            stroke-width="2"
            d="M20 12a8 8 0 11-2.343-5.657L20 8m0 0V4m0 4h-4"
          />
        </svg>
        {{ t('admin.accounts.openaiQuotaReset.reset') }}
      </button>
    </div>

    <div v-if="displayError" class="text-[10px] text-red-600 dark:text-red-400" :title="displayError">
      {{ truncatedDisplayError }}
    </div>
    <div v-else-if="resetMessage" class="text-[10px] text-emerald-600 dark:text-emerald-400">
      {{ resetMessage }}
    </div>
  </div>

  <ConfirmDialog
    :show="showResetConfirm"
    :title="t('admin.accounts.openaiQuotaReset.confirmTitle')"
    :message="t('admin.accounts.openaiQuotaReset.confirmMessage', { name: account.name, count: availableResetCount })"
    :confirm-text="t('admin.accounts.openaiQuotaReset.confirmAction')"
    :cancel-text="t('common.cancel')"
    :danger="true"
    @confirm="confirmReset"
    @cancel="showResetConfirm = false"
  />
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import type { PropType } from 'vue'
import { useI18n } from 'vue-i18n'
import type { Account, OpenAIQuotaResetResult, OpenAIQuotaUsage } from '@/types'
import { extractApiErrorMessage } from '@/utils/apiError'
import ConfirmDialog from '@/components/common/ConfirmDialog.vue'

type QueryOpenAIQuota = (id: number) => Promise<OpenAIQuotaUsage>
type ResetOpenAIQuota = (id: number) => Promise<OpenAIQuotaResetResult>

const props = defineProps({
  account: {
    type: Object as PropType<Account>,
    required: true,
  },
  queryOpenaiQuota: {
    type: Function as PropType<QueryOpenAIQuota>,
    required: false,
  },
  resetOpenaiQuota: {
    type: Function as PropType<ResetOpenAIQuota>,
    required: false,
  },
})

const { t } = useI18n()

const visible = computed(() => props.account.platform === 'openai' && props.account.type === 'oauth')
const loading = ref(false)
const resetting = ref(false)
const error = ref<string | null>(null)
const data = ref<OpenAIQuotaUsage | null>(null)
const resetMessage = ref<string | null>(null)
const showResetConfirm = ref(false)

const hasQuotaAPI = computed(() => (
  typeof props.queryOpenaiQuota === 'function' &&
  typeof props.resetOpenaiQuota === 'function'
))
const configurationError = computed(() => (
  visible.value && !hasQuotaAPI.value
    ? t('admin.accounts.openaiQuotaReset.apiNotConfigured')
    : null
))
const displayError = computed(() => error.value || configurationError.value)
const availableResetCount = computed(() => data.value?.rate_limit_reset_credits?.available_count ?? 0)
const canReset = computed(() => hasQuotaAPI.value && availableResetCount.value > 0)

const countButtonTitle = computed(() => (
  !hasQuotaAPI.value
    ? t('admin.accounts.openaiQuotaReset.apiNotConfigured')
    : (
        data.value
          ? t('admin.accounts.openaiQuotaReset.countTooltipRefresh')
          : t('admin.accounts.openaiQuotaReset.countTooltipLoad')
      )
))

const resetButtonTitle = computed(() => {
  if (!hasQuotaAPI.value) return t('admin.accounts.openaiQuotaReset.apiNotConfigured')
  if (!data.value) return t('admin.accounts.openaiQuotaReset.resetTooltipNeedQuery')
  if (!canReset.value) return t('admin.accounts.openaiQuotaReset.resetTooltipNoCredits')
  return t('admin.accounts.openaiQuotaReset.resetTooltipReady')
})

const truncatedDisplayError = computed(() => {
  if (!displayError.value) return ''
  return displayError.value.length > 80 ? `${displayError.value.slice(0, 80)}...` : displayError.value
})

function extractErrorMessage(e: unknown): string {
  return extractApiErrorMessage(e, t('common.error'))
}

async function handleQuery() {
  if (loading.value) return
  if (!props.queryOpenaiQuota) {
    error.value = t('admin.accounts.openaiQuotaReset.apiNotConfigured')
    return
  }
  loading.value = true
  error.value = null
  resetMessage.value = null
  try {
    data.value = await props.queryOpenaiQuota(props.account.id)
  } catch (e) {
    error.value = extractErrorMessage(e)
  } finally {
    loading.value = false
  }
}

function handleResetRequest() {
  if (resetting.value) return
  if (!props.resetOpenaiQuota) {
    error.value = t('admin.accounts.openaiQuotaReset.apiNotConfigured')
    return
  }
  if (!canReset.value) {
    error.value = t('admin.accounts.openaiQuotaReset.noCreditsAvailable')
    return
  }
  showResetConfirm.value = true
}

async function confirmReset() {
  showResetConfirm.value = false
  if (resetting.value) return
  if (!props.resetOpenaiQuota) {
    error.value = t('admin.accounts.openaiQuotaReset.apiNotConfigured')
    return
  }
  if (!canReset.value) {
    error.value = t('admin.accounts.openaiQuotaReset.noCreditsAvailable')
    return
  }
  resetting.value = true
  error.value = null
  resetMessage.value = null
  try {
    const result: OpenAIQuotaResetResult = await props.resetOpenaiQuota(props.account.id)
    await handleQuery()
    resetMessage.value = t('admin.accounts.openaiQuotaReset.resetSuccess', { windows: result.windows_reset })
  } catch (e) {
    error.value = extractErrorMessage(e)
  } finally {
    resetting.value = false
  }
}

watch(
  () => props.account.id,
  () => {
    data.value = null
    error.value = null
    resetMessage.value = null
    loading.value = false
    resetting.value = false
    showResetConfirm.value = false
  }
)
</script>
