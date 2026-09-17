<template>
  <section class="card border border-gray-100 bg-white/90 p-5 dark:border-dark-700 dark:bg-dark-900/50 md:p-6">
    <div class="flex flex-col gap-4 lg:flex-row lg:items-start lg:justify-between">
      <div class="min-w-0">
        <h3 class="text-lg font-semibold text-gray-900 dark:text-white">{{ t('withdrawal.title') }}</h3>
        <p class="mt-1 max-w-3xl text-sm text-gray-500 dark:text-gray-400">
          {{ t('withdrawal.subtitle') }}
          <span v-if="rateLimitDescription">{{ rateLimitDescription }}</span>
        </p>
      </div>

      <div class="grid grid-cols-2 gap-2 sm:min-w-[18rem]">
        <div class="rounded-lg bg-primary-50 px-4 py-3 dark:bg-primary-900/20">
          <p class="text-xs text-primary-600 dark:text-primary-300">{{ t('withdrawal.currentBalance') }}</p>
          <p class="mt-1 text-xl font-semibold text-gray-900 dark:text-white">${{ balance.toFixed(2) }}</p>
        </div>
        <div class="rounded-lg bg-gray-50 px-4 py-3 dark:bg-dark-800/70">
          <p class="text-xs text-gray-500 dark:text-gray-400">{{ t('withdrawal.statusLabel') }}</p>
          <p class="mt-1 text-sm font-semibold" :class="hasPendingWithdrawal ? 'text-yellow-700 dark:text-yellow-300' : 'text-green-700 dark:text-green-300'">
            {{ hasPendingWithdrawal ? t('withdrawal.hasPending') : t('withdrawal.canSubmit') }}
          </p>
        </div>
      </div>
    </div>

    <div class="mt-5 grid items-start gap-5 xl:grid-cols-[minmax(18rem,0.95fr)_minmax(18rem,1fr)_minmax(18rem,1.05fr)]">
      <div class="rounded-lg border border-gray-100 bg-gray-50/70 p-4 dark:border-dark-700 dark:bg-dark-900/30">
        <div class="flex items-center justify-between gap-3">
          <div>
            <p class="text-sm font-semibold text-gray-900 dark:text-white">{{ t('withdrawal.submitTitle') }}</p>
            <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">{{ t('withdrawal.submitHint') }}</p>
          </div>
          <Icon name="dollar" size="lg" class="text-primary-500" />
        </div>

        <label class="mt-4 block text-sm font-medium text-gray-700 dark:text-gray-300">{{ t('withdrawal.amount') }}</label>
        <input
          v-model="amountText"
          type="text"
          inputmode="decimal"
          pattern="^\d+(\.\d{1,2})?$"
          class="input mt-2"
          placeholder="1.00"
        >

        <div class="mt-4">
          <p class="text-sm font-medium text-gray-700 dark:text-gray-300">{{ t('withdrawal.qrThisTime') }}</p>
          <div class="mt-2 grid grid-cols-2 gap-2">
            <button
              v-for="method in paymentMethods"
              :key="method"
              type="button"
              class="min-h-11 rounded-lg border px-3 py-2 text-sm font-medium transition"
              :class="selectedMethod === method
                ? 'border-primary-300 bg-primary-50 text-primary-700 dark:border-primary-700 dark:bg-primary-900/30 dark:text-primary-200'
                : 'border-gray-200 bg-white text-gray-600 hover:border-gray-300 dark:border-dark-700 dark:bg-dark-900/60 dark:text-gray-300 dark:hover:border-dark-600'"
              @click="selectMethod(method)"
            >
              {{ methodLabel(method) }}
            </button>
          </div>
        </div>

        <div class="mt-4 rounded-lg border border-gray-100 bg-white p-3 text-sm dark:border-dark-700 dark:bg-dark-900/60">
          <div class="flex justify-between gap-3">
            <span class="text-gray-500 dark:text-gray-400">{{ t('withdrawal.amount') }}</span>
            <span class="font-medium text-gray-900 dark:text-white">${{ normalizedAmount.toFixed(2) }}</span>
          </div>
          <div class="mt-2 flex justify-between gap-3">
            <span class="text-gray-500 dark:text-gray-400">{{ t('withdrawal.firstFee') }}</span>
            <span class="font-medium text-gray-900 dark:text-white">${{ feeAmount.toFixed(2) }}</span>
          </div>
          <div class="mt-2 flex justify-between gap-3 border-t border-gray-100 pt-2 dark:border-dark-700">
            <span class="text-gray-500 dark:text-gray-400">{{ t('withdrawal.deduction') }}</span>
            <span class="font-semibold text-gray-900 dark:text-white">${{ totalDeducted.toFixed(2) }}</span>
          </div>
        </div>

        <p v-if="submitHint" class="mt-3 text-xs text-gray-500 dark:text-gray-400">{{ submitHint }}</p>

        <button
          type="button"
          class="btn btn-primary mt-4 w-full"
          :disabled="submitting || !canSubmit"
          @click="submit"
        >
          {{ submitting ? t('common.processing') : t('withdrawal.submitButton') }}
        </button>
      </div>

      <div class="rounded-lg border border-gray-100 bg-gray-50/70 p-4 dark:border-dark-700 dark:bg-dark-900/30">
        <div class="flex flex-wrap items-start justify-between gap-3">
          <div>
            <p class="text-sm font-semibold text-gray-900 dark:text-white">{{ t('withdrawal.qrManagement') }}</p>
            <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">{{ t('withdrawal.qrHint') }}</p>
          </div>
          <button class="btn btn-secondary btn-sm" :disabled="loading" @click="load">
            <Icon name="refresh" size="sm" :class="loading ? 'animate-spin' : ''" />
          </button>
        </div>

        <div
          class="mt-4 flex aspect-square w-full items-center justify-center overflow-hidden rounded-lg border border-dashed border-gray-200 bg-white dark:border-dark-700 dark:bg-dark-900/60"
        >
          <img
            v-if="previewUrl"
            :src="previewUrl"
            :alt="methodLabel(selectedMethod)"
            class="h-full w-full object-contain"
          >
          <div v-else class="flex flex-col items-center gap-2 text-gray-400 dark:text-gray-500">
            <Icon name="creditCard" size="xl" />
            <span class="text-sm">{{ t('withdrawal.qrMissing', { selectedMethod: methodLabel(selectedMethod) }) }}</span>
          </div>
        </div>

        <div class="mt-4 min-h-12 text-sm text-gray-600 dark:text-gray-300">
          <p>
            {{ currentReceiptCode ? `已保存于 ${formatDateTime(currentReceiptCode.updated_at)}` : '当前方式还未保存收款码' }}
          </p>
          <p v-if="draftFile" class="mt-1 text-xs text-primary-600 dark:text-primary-300">
            {{ t('withdrawal.qrPendingSave') }}
          </p>
          <p v-else-if="currentReceiptCode" class="mt-1 truncate text-xs text-gray-500 dark:text-gray-400">
            SHA256: {{ currentReceiptCode.sha256 }}
          </p>
        </div>

        <div class="mt-4 grid grid-cols-1 gap-2 sm:grid-cols-3">
          <label class="btn btn-secondary btn-sm min-h-11 cursor-pointer justify-center">
            <input
              type="file"
              accept="image/png,image/jpeg,image/gif,image/webp"
              class="hidden"
              @change="handleFileChange"
            >
            <Icon name="upload" size="sm" class="mr-1.5" />
            {{ t('withdrawal.upload') }}
          </label>

          <button
            type="button"
            class="btn btn-primary btn-sm min-h-11"
            :disabled="saving || !draftFile"
            @click="handleSave"
          >
            {{ saving ? t('common.loading') : t('common.save') }}
          </button>

          <button
            type="button"
            class="btn btn-secondary btn-sm min-h-11 text-red-600 hover:text-red-700 dark:text-red-400"
            :disabled="saving || (!currentReceiptCode && !draftFile)"
            @click="handleDelete"
          >
            <Icon name="trash" size="sm" class="mr-1.5" />
            {{ draftFile ? t('withdrawal.clear') : t('withdrawal.delete') }}
          </button>
        </div>
      </div>

      <div class="rounded-lg border border-gray-100 bg-gray-50/70 p-4 dark:border-dark-700 dark:bg-dark-900/30">
        <div class="flex flex-wrap items-start justify-between gap-3">
          <div>
            <p class="text-sm font-semibold text-gray-900 dark:text-white">{{ t('withdrawal.records') }}</p>
            <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">{{ t('withdrawal.recordsHint') }}</p>
          </div>
          <span class="rounded-full bg-white px-2.5 py-1 text-xs text-gray-500 ring-1 ring-gray-100 dark:bg-dark-900 dark:text-gray-400 dark:ring-dark-700">
            {{ t('withdrawal.recentCount', { length: withdrawals.length }) }}
          </span>
        </div>

        <div v-if="withdrawals.length" class="mt-4 max-h-[22rem] space-y-3 overflow-y-auto pr-1">
          <div
            v-for="item in withdrawals"
            :key="item.id"
            class="rounded-lg border border-gray-100 bg-white p-3 dark:border-dark-700 dark:bg-dark-900/60"
          >
            <div class="flex flex-wrap items-center justify-between gap-2">
              <span class="font-mono text-sm text-gray-700 dark:text-gray-300">#{{ item.id }}</span>
              <span class="rounded-full px-2 py-0.5 text-xs font-medium" :class="statusClass(item.status)">
                {{ statusLabel(item.status) }}
              </span>
            </div>
            <div class="mt-3 grid grid-cols-2 gap-3 text-sm">
              <div>
                <p class="text-xs text-gray-500 dark:text-gray-400">{{ t('withdrawal.amount') }}</p>
                <p class="font-medium text-gray-900 dark:text-white">${{ item.amount.toFixed(2) }}</p>
              </div>
              <div>
                <p class="text-xs text-gray-500 dark:text-gray-400">{{ t('withdrawal.deducted') }}</p>
                <p class="font-medium text-gray-900 dark:text-white">${{ item.total_deducted.toFixed(2) }}</p>
              </div>
            </div>
            <div
              v-if="item.status === 'REJECTED'"
              class="mt-3 rounded-lg border border-red-100 bg-red-50 px-3 py-2.5 text-sm dark:border-red-900/50 dark:bg-red-900/20"
            >
              <p class="text-xs font-medium text-red-700 dark:text-red-300">{{ t('withdrawal.rejectReason') }}</p>
              <p class="mt-1 break-words text-red-700 dark:text-red-200">
                {{ rejectionReason(item) }}
              </p>
            </div>
            <div class="mt-3 flex flex-wrap items-center justify-between gap-2">
              <span class="text-xs text-gray-500 dark:text-gray-400">{{ formatDate(item.created_at) }}</span>
              <button
                v-if="item.status === 'PENDING'"
                type="button"
                class="btn btn-secondary btn-sm text-red-600 hover:text-red-700 dark:text-red-400"
                :disabled="actionLoading"
                @click="cancel(item.id)"
              >
                {{ t('withdrawal.cancel') }}
              </button>
            </div>
          </div>
        </div>
        <p v-else class="mt-4 rounded-lg border border-dashed border-gray-200 bg-white p-4 text-sm text-gray-500 dark:border-dark-700 dark:bg-dark-900/60 dark:text-gray-400">
          {{ t('withdrawal.empty') }}
        </p>
      </div>
    </div>
  </section>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { userAPI } from '@/api'
import Icon from '@/components/icons/Icon.vue'
import { useAppStore } from '@/stores/app'
import { useAuthStore } from '@/stores/auth'
import type { ReceiptCode, ReceiptCodePaymentMethod, WithdrawalRequest, WithdrawalStatus } from '@/types'
import { extractApiErrorMessage, extractI18nErrorMessage } from '@/utils/apiError'

const props = withDefaults(defineProps<{
  rateLimitWindowDays?: number
  rateLimitMax?: number
  rateLimitExemptAmount?: number
}>(), {
  rateLimitWindowDays: 1,
  rateLimitMax: 0,
  rateLimitExemptAmount: 500,
})

const { t } = useI18n()
const appStore = useAppStore()
const authStore = useAuthStore()

const paymentMethods: ReceiptCodePaymentMethod[] = ['alipay', 'wechat']
const selectedMethod = ref<ReceiptCodePaymentMethod>('alipay')
const receiptCodes = ref<Partial<Record<ReceiptCodePaymentMethod, ReceiptCode | null>>>({})
const withdrawals = ref<WithdrawalRequest[]>([])
const amountText = ref<string | number>('')
const draftFile = ref<File | null>(null)
const draftPreviewUrl = ref('')
const loading = ref(false)
const saving = ref(false)
const submitting = ref(false)
const actionLoading = ref(false)

const balance = computed(() => Number(authStore.user?.balance || 0))
const rateLimitDescription = computed(() => {
  if (props.rateLimitMax <= 0) return ''
  const base = t('withdrawal.rateLimit', { windowDays: props.rateLimitWindowDays, max: props.rateLimitMax })
  if (props.rateLimitExemptAmount <= 0) return base
  return t('withdrawal.rateLimitExempt', { base, amount: props.rateLimitExemptAmount.toFixed(2) })
})
const currentReceiptCode = computed(() => receiptCodes.value[selectedMethod.value] ?? null)
const previewUrl = computed(() => draftPreviewUrl.value || currentReceiptCode.value?.url?.trim() || '')
const hasAnyWithdrawal = computed(() => withdrawals.value.length > 0)
const hasPendingWithdrawal = computed(() => withdrawals.value.some(item => item.status === 'PENDING'))
const feeAmount = computed(() => hasAnyWithdrawal.value ? 0 : 0.1)
const amountRawText = computed(() => String(amountText.value).trim())
const normalizedAmount = computed(() => {
  const value = Number(amountRawText.value)
  return Number.isFinite(value) ? Math.round(value * 100) / 100 : 0
})
const amountIsValid = computed(() => /^\d+(\.\d{1,2})?$/.test(amountRawText.value) && normalizedAmount.value >= 1)
const totalDeducted = computed(() => normalizedAmount.value + feeAmount.value)
const canSubmit = computed(() => {
  return amountIsValid.value
    && !!currentReceiptCode.value
    && !draftFile.value
    && !hasPendingWithdrawal.value
    && balance.value + 1e-9 >= totalDeducted.value
})
const submitHint = computed(() => {
  if (hasPendingWithdrawal.value) return t('withdrawal.errPendingExists')
  if (draftFile.value) return t('withdrawal.errQrUnsaved')
  if (!currentReceiptCode.value) return t('withdrawal.errQrRequired')
  if (amountRawText.value && !amountIsValid.value) return t('withdrawal.errAmount')
  if (amountIsValid.value && balance.value + 1e-9 < totalDeducted.value) return t('withdrawal.errBalance')
  return ''
})

onMounted(() => {
  void load()
})

onBeforeUnmount(() => {
  revokeDraftPreview()
})

async function load() {
  loading.value = true
  try {
    const [alipay, wechat, list] = await Promise.all([
      userAPI.getReceiptCode('alipay'),
      userAPI.getReceiptCode('wechat'),
      userAPI.listWithdrawals({ page: 1, page_size: 5 }),
    ])
    receiptCodes.value.alipay = alipay
    receiptCodes.value.wechat = wechat
    withdrawals.value = list.items || []
  } catch (error: unknown) {
    appStore.showError(extractApiErrorMessage(error, t('withdrawal.loadFailed')))
  } finally {
    loading.value = false
  }
}

function selectMethod(method: ReceiptCodePaymentMethod) {
  if (selectedMethod.value === method) {
    return
  }
  selectedMethod.value = method
  clearDraft()
}

function methodLabel(method: ReceiptCodePaymentMethod): string {
  return method === 'alipay' ? t('withdrawal.methodAlipay') : t('withdrawal.methodWechat')
}

function handleFileChange(event: Event) {
  const input = event.target as HTMLInputElement | null
  const file = input?.files?.[0]
  if (input) {
    input.value = ''
  }
  if (!file) {
    return
  }
  if (!['image/png', 'image/jpeg', 'image/gif', 'image/webp'].includes(file.type)) {
    appStore.showError(t('withdrawal.errQrFormat'))
    return
  }
  if (file.size > 1024 * 1024) {
    appStore.showError(t('withdrawal.errQrSize'))
    return
  }
  revokeDraftPreview()
  draftFile.value = file
  draftPreviewUrl.value = URL.createObjectURL(file)
}

async function handleSave() {
  if (!draftFile.value) {
    appStore.showError(t('withdrawal.errQrSelect'))
    return
  }

  const method = selectedMethod.value
  saving.value = true
  try {
    receiptCodes.value[method] = await userAPI.uploadReceiptCode(method, draftFile.value)
    clearDraft()
    appStore.showSuccess(t('withdrawal.qrSaved'))
  } catch (error: unknown) {
    appStore.showError(extractApiErrorMessage(error, t('withdrawal.qrSaveFailed')))
  } finally {
    saving.value = false
  }
}

async function handleDelete() {
  if (draftFile.value) {
    clearDraft()
    return
  }
  if (!currentReceiptCode.value) {
    return
  }

  const method = selectedMethod.value
  saving.value = true
  try {
    await userAPI.deleteReceiptCode(method)
    receiptCodes.value[method] = null
    appStore.showSuccess(t('withdrawal.qrDeleted'))
  } catch (error: unknown) {
    appStore.showError(extractApiErrorMessage(error, t('withdrawal.qrDeleteFailed')))
  } finally {
    saving.value = false
  }
}

async function submit() {
  if (!canSubmit.value) {
    appStore.showError(submitHint.value || t('withdrawal.errConfirm'))
    return
  }
  submitting.value = true
  try {
    await userAPI.submitWithdrawal({
      amount: normalizedAmount.value,
      payment_method: selectedMethod.value,
    })
    amountText.value = ''
    await Promise.all([load(), authStore.refreshUser()])
    appStore.showSuccess(t('withdrawal.submitSuccess'))
  } catch (error: unknown) {
    appStore.showError(
      extractI18nErrorMessage(error, t, 'withdrawal.errors', t('withdrawal.submitFailed'))
    )
  } finally {
    submitting.value = false
  }
}

async function cancel(id: number) {
  actionLoading.value = true
  try {
    await userAPI.cancelWithdrawal(id)
    await Promise.all([load(), authStore.refreshUser()])
    appStore.showSuccess(t('withdrawal.cancelSuccess'))
  } catch (error: unknown) {
    appStore.showError(extractApiErrorMessage(error, t('withdrawal.cancelFailed')))
  } finally {
    actionLoading.value = false
  }
}

function clearDraft() {
  revokeDraftPreview()
  draftFile.value = null
}

function revokeDraftPreview() {
  if (draftPreviewUrl.value) {
    URL.revokeObjectURL(draftPreviewUrl.value)
    draftPreviewUrl.value = ''
  }
}

function statusLabel(status: WithdrawalStatus): string {
  const map: Record<WithdrawalStatus, string> = {
    PENDING: t('withdrawal.statusPending'),
    SETTLED: t('withdrawal.statusSettled'),
    CANCELLED: t('withdrawal.statusCancelled'),
    REJECTED: t('withdrawal.statusRejected'),
  }
  return map[status]
}

function statusClass(status: WithdrawalStatus): string {
  const map: Record<WithdrawalStatus, string> = {
    PENDING: 'bg-yellow-100 text-yellow-700 dark:bg-yellow-900/30 dark:text-yellow-300',
    SETTLED: 'bg-green-100 text-green-700 dark:bg-green-900/30 dark:text-green-300',
    CANCELLED: 'bg-gray-100 text-gray-700 dark:bg-dark-700 dark:text-gray-300',
    REJECTED: 'bg-red-100 text-red-700 dark:bg-red-900/30 dark:text-red-300',
  }
  return map[status]
}

function rejectionReason(item: WithdrawalRequest): string {
  return item.rejection_reason?.trim() || t('withdrawal.noReason')
}

function formatDate(raw: string): string {
  const date = new Date(raw)
  if (Number.isNaN(date.getTime())) {
    return '-'
  }
  return date.toLocaleString()
}

function formatDateTime(raw: string): string {
  const date = new Date(raw)
  if (Number.isNaN(date.getTime())) {
    return '-'
  }
  return new Intl.DateTimeFormat(undefined, {
    year: 'numeric',
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
  }).format(date)
}
</script>
