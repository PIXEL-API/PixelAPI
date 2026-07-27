<template>
  <BaseDialog
    :show="show"
    :title="t('admin.accounts.syncFromCrsTitle')"
    width="normal"
    :close-disabled="syncing || previewing"
    :close-on-escape="!syncing && !previewing"
    :close-on-click-outside="false"
    @close="handleClose"
  >
    <!-- Step 1: Input credentials -->
    <form
      v-if="currentStep === 'input'"
      id="sync-from-crs-form"
      class="space-y-4"
      @submit.prevent="handlePreview"
    >
      <div class="text-sm text-gray-600 dark:text-dark-300">
        {{ t('admin.accounts.syncFromCrsDesc') }}
      </div>
      <div
        class="rounded-lg bg-gray-50 p-3 text-xs text-gray-500 dark:bg-dark-700/60 dark:text-dark-400"
      >
        {{ t('admin.accounts.crsUpdateBehaviorNote') }}
      </div>
      <div
        class="rounded-lg border border-amber-200 bg-amber-50 p-3 text-xs text-amber-600 dark:border-amber-800 dark:bg-amber-900/20 dark:text-amber-400"
      >
        {{ t('admin.accounts.crsVersionRequirement') }}
      </div>

      <div class="grid grid-cols-1 gap-4">
        <div>
          <label for="crs-base-url" class="input-label">{{ t('admin.accounts.crsBaseUrl') }}</label>
          <input
            id="crs-base-url"
            v-model="form.base_url"
            type="text"
            class="input"
            required
            :disabled="previewing || syncing"
            :placeholder="t('admin.accounts.crsBaseUrlPlaceholder')"
          />
        </div>

        <div class="grid grid-cols-1 gap-4 sm:grid-cols-2">
          <div>
            <label for="crs-username" class="input-label">{{ t('admin.accounts.crsUsername') }}</label>
            <input
              id="crs-username"
              v-model="form.username"
              type="text"
              class="input"
              required
              autocomplete="username"
              :disabled="previewing || syncing"
            />
          </div>
          <div>
            <label for="crs-password" class="input-label">{{ t('admin.accounts.crsPassword') }}</label>
            <input
              id="crs-password"
              v-model="form.password"
              type="password"
              class="input"
              required
              autocomplete="current-password"
              :disabled="previewing || syncing"
            />
          </div>
        </div>

        <label class="flex items-center gap-2 text-sm text-gray-700 dark:text-dark-300">
          <input
            v-model="form.sync_proxies"
            type="checkbox"
            class="rounded border-gray-300 dark:border-dark-600"
            :disabled="previewing || syncing"
            data-testid="crs-sync-proxies"
          />
          {{ t('admin.accounts.syncProxies') }}
        </label>
      </div>
    </form>

    <!-- Step 2: Preview & select -->
    <div v-else-if="currentStep === 'preview' && previewResult" class="space-y-4">
      <!-- Existing accounts (read-only info) -->
      <div
        v-if="previewResult.existing_accounts.length"
        class="rounded-lg bg-gray-50 p-3 dark:bg-dark-700/60"
      >
        <div class="mb-2 text-sm font-medium text-gray-700 dark:text-dark-300">
          {{ t('admin.accounts.crsExistingAccounts') }}
          <span class="ml-1 text-xs text-gray-400">({{ previewResult.existing_accounts.length }})</span>
        </div>
        <div class="max-h-32 overflow-auto text-xs text-gray-500 dark:text-dark-400">
          <div
            v-for="acc in previewResult.existing_accounts"
            :key="acc.crs_account_id"
            class="flex min-h-11 items-center gap-2 py-1"
          >
            <span
              class="inline-block rounded bg-blue-100 px-1.5 py-0.5 text-[10px] font-medium text-blue-700 dark:bg-blue-900/30 dark:text-blue-400"
            >{{ acc.platform }} / {{ acc.type }}</span>
            <span class="truncate">{{ acc.name }}</span>
            <span v-if="acc.local_account_id" class="text-[10px] text-gray-400">
              本地 #{{ acc.local_account_id }}
            </span>
            <span
              v-if="acc.requires_force_active_edit"
              class="ml-auto rounded bg-amber-100 px-1.5 py-0.5 text-[10px] font-medium text-amber-700 dark:bg-amber-900/30 dark:text-amber-300"
            >
              房间账号 · {{ acc.room_bindings?.length || 0 }} 个房间
            </span>
          </div>
        </div>
      </div>

      <div
        v-if="requiresForceConfirmation"
        class="space-y-3 rounded-lg border border-amber-300 bg-amber-50 p-4 text-sm text-amber-900 dark:border-amber-700 dark:bg-amber-950/30 dark:text-amber-100"
        data-testid="crs-force-confirmation"
      >
        <div>
          <strong>检测到 {{ forceSyncSnapshot.accounts.length }} 个房间账号</strong>
          <p class="mt-1 text-xs leading-5">
            CRS 同步会修改这些账号的凭证或配置，后端默认拒绝。继续前必须确认管理员强制编辑，并按本次预览中的
            {{ forceSyncSnapshot.listingCount }} 个房间版本执行并发校验。
          </p>
        </div>
        <div
          v-if="forceSyncSnapshot.error"
          class="rounded border border-red-300 bg-red-50 px-3 py-2 text-xs text-red-700 dark:border-red-700 dark:bg-red-950/30 dark:text-red-200"
          role="alert"
          data-testid="crs-force-preview-error"
        >
          {{ forceSyncSnapshot.error }}
        </div>
        <template v-else>
          <label class="flex min-h-11 cursor-pointer items-start gap-3 rounded border border-amber-200 bg-white/70 px-3 py-2 dark:border-amber-800 dark:bg-dark-900/50">
            <input
              v-model="forceConfirmed"
              type="checkbox"
              class="mt-1 rounded border-gray-300 dark:border-dark-600"
              :disabled="syncing"
              data-testid="crs-force-confirmed"
            />
            <span>
              <strong class="block">我已核对房间账号，确认使用管理员强制编辑</strong>
              <small class="mt-1 block leading-5 text-amber-700 dark:text-amber-200">
                已有使用记录继续按历史条款结算；本次同步会产生可审计的修改记录。
              </small>
            </span>
          </label>
          <div>
            <label for="crs-force-reason" class="input-label">强制同步原因</label>
            <textarea
              id="crs-force-reason"
              v-model="forceReason"
              class="input min-h-24 resize-y"
              maxlength="500"
              :disabled="syncing"
              placeholder="例如：CRS 凭证轮换，已核对受影响房间与当前使用状态"
              data-testid="crs-force-reason"
            ></textarea>
            <p class="mt-1 text-xs text-amber-700 dark:text-amber-200">
              原因不能为空，并会随管理员操作写入审计记录。
            </p>
          </div>
        </template>
      </div>

      <!-- New accounts (selectable) -->
      <div v-if="previewResult.new_accounts.length">
        <div class="mb-2 flex items-center justify-between">
          <div class="text-sm font-medium text-gray-900 dark:text-white">
            {{ t('admin.accounts.crsNewAccounts') }}
            <span class="ml-1 text-xs text-gray-400">({{ previewResult.new_accounts.length }})</span>
          </div>
          <div class="flex gap-2">
            <button
              type="button"
              class="min-h-11 rounded-md px-2 text-xs text-blue-600 hover:text-blue-700 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-blue-500 focus-visible:ring-offset-2 dark:text-blue-400 dark:focus-visible:ring-offset-dark-800"
              :disabled="syncing"
              data-testid="crs-select-all"
              @click="selectAll"
            >{{ t('admin.accounts.crsSelectAll') }}</button>
            <button
              type="button"
              class="min-h-11 rounded-md px-2 text-xs text-gray-500 hover:text-gray-600 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-blue-500 focus-visible:ring-offset-2 dark:text-gray-400 dark:focus-visible:ring-offset-dark-800"
              :disabled="syncing"
              data-testid="crs-select-none"
              @click="selectNone"
            >{{ t('admin.accounts.crsSelectNone') }}</button>
          </div>
        </div>
        <div
          class="max-h-48 overflow-auto rounded-lg border border-gray-200 p-2 dark:border-dark-600"
        >
          <label
            v-for="acc in previewResult.new_accounts"
            :key="acc.crs_account_id"
            class="flex min-h-11 cursor-pointer items-center gap-2 rounded px-2 py-1.5 hover:bg-gray-50 focus-within:ring-2 focus-within:ring-blue-500 focus-within:ring-offset-2 dark:hover:bg-dark-700/40 dark:focus-within:ring-offset-dark-800"
            data-testid="crs-new-account-row"
          >
            <input
              type="checkbox"
              :checked="selectedIds.has(acc.crs_account_id)"
              class="rounded border-gray-300 dark:border-dark-600"
              :disabled="syncing"
              @change="toggleSelect(acc.crs_account_id)"
            />
            <span
              class="inline-block rounded bg-green-100 px-1.5 py-0.5 text-[10px] font-medium text-green-700 dark:bg-green-900/30 dark:text-green-400"
            >{{ acc.platform }} / {{ acc.type }}</span>
            <span class="truncate text-sm text-gray-700 dark:text-dark-300">{{ acc.name }}</span>
          </label>
        </div>
        <div class="mt-1 text-xs text-gray-400">
          {{ t('admin.accounts.crsSelectedCount', { count: selectedIds.size }) }}
        </div>
      </div>

      <!-- Sync options summary -->
      <div class="flex items-center gap-2 text-xs text-gray-500 dark:text-dark-400">
        <span>{{ t('admin.accounts.syncProxies') }}:</span>
        <span :class="previewConnectionSnapshot?.sync_proxies ? 'text-green-600 dark:text-green-400' : 'text-gray-400 dark:text-dark-500'">
          {{ previewConnectionSnapshot?.sync_proxies ? t('common.yes') : t('common.no') }}
        </span>
      </div>

      <!-- No new accounts -->
      <div
        v-if="!previewResult.new_accounts.length"
        class="rounded-lg bg-gray-50 p-4 text-center text-sm text-gray-500 dark:bg-dark-700/60 dark:text-dark-400"
      >
        {{ t('admin.accounts.crsNoNewAccounts') }}
        <span v-if="previewResult.existing_accounts.length">
          {{ t('admin.accounts.crsWillUpdate', { count: previewResult.existing_accounts.length }) }}
        </span>
      </div>
    </div>

    <!-- Step 3: Result -->
    <div v-else-if="currentStep === 'result' && result" class="space-y-4">
      <div
        class="space-y-2 rounded-xl border border-gray-200 p-4 dark:border-dark-700"
      >
        <div class="text-sm font-medium text-gray-900 dark:text-white">
          {{ t('admin.accounts.syncResult') }}
        </div>
        <div class="text-sm text-gray-700 dark:text-dark-300">
          {{ t('admin.accounts.syncResultSummary', result) }}
        </div>

        <div v-if="errorItems.length" class="mt-2">
          <div class="text-sm font-medium text-red-600 dark:text-red-400">
            {{ t('admin.accounts.syncErrors') }}
          </div>
          <div
            class="mt-2 max-h-48 overflow-auto rounded-lg bg-gray-50 p-3 font-mono text-xs dark:bg-dark-800"
          >
            <div v-for="(item, idx) in errorItems" :key="idx" class="whitespace-pre-wrap">
              {{ item.kind }} {{ item.crs_account_id }} — {{ item.action
              }}{{ item.error ? `: ${item.error}` : '' }}
            </div>
          </div>
        </div>
      </div>
    </div>

    <template #footer>
      <div class="flex justify-end gap-3">
        <!-- Step 1: Input -->
        <template v-if="currentStep === 'input'">
          <button
            class="btn btn-secondary"
            type="button"
            :disabled="previewing"
            @click="handleClose"
          >
            {{ t('common.cancel') }}
          </button>
          <button
            class="btn btn-primary"
            type="submit"
            form="sync-from-crs-form"
            :disabled="previewing"
          >
            {{ previewing ? t('admin.accounts.crsPreviewing') : t('admin.accounts.crsPreview') }}
          </button>
        </template>

        <!-- Step 2: Preview -->
        <template v-else-if="currentStep === 'preview'">
          <button
            class="btn btn-secondary"
            type="button"
            :disabled="syncing"
            @click="handleBack"
          >
            {{ t('admin.accounts.crsBack') }}
          </button>
          <button
            class="btn btn-primary"
            type="button"
            :disabled="syncDisabled"
            @click="handleSync"
          >
            {{ syncing ? t('admin.accounts.syncing') : t('admin.accounts.syncNow') }}
          </button>
        </template>

        <!-- Step 3: Result -->
        <template v-else-if="currentStep === 'result'">
          <button class="btn btn-secondary" type="button" @click="handleClose">
            {{ t('common.close') }}
          </button>
        </template>
      </div>
    </template>
  </BaseDialog>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, reactive, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import BaseDialog from '@/components/common/BaseDialog.vue'
import { useAppStore } from '@/stores/app'
import { adminAPI } from '@/api/admin'
import type { CRSPreviewAccount, PreviewFromCRSResult } from '@/api/admin/accounts'
import { extractApiErrorCode } from '@/utils/apiError'

interface Props {
  show: boolean
}

interface Emits {
  (e: 'close'): void
  (e: 'synced'): void
}

const props = defineProps<Props>()
const emit = defineEmits<Emits>()

const { t } = useI18n()
const appStore = useAppStore()

type Step = 'input' | 'preview' | 'result'
interface CRSConnectionSnapshot {
  readonly base_url: string
  readonly username: string
  readonly password: string
  readonly sync_proxies: boolean
}

const currentStep = ref<Step>('input')
const previewing = ref(false)
const syncing = ref(false)
const previewResult = ref<PreviewFromCRSResult | null>(null)
const previewConnectionSnapshot = ref<Readonly<CRSConnectionSnapshot> | null>(null)
const selectedIds = ref(new Set<string>())
const result = ref<Awaited<ReturnType<typeof adminAPI.accounts.syncFromCrs>> | null>(null)
const forceConfirmed = ref(false)
const forceReason = ref('')
const syncIdempotencyKey = ref('')
const syncPayloadSignature = ref('')
let previewRequestVersion = 0

const form = reactive({
  base_url: '',
  username: '',
  password: '',
  sync_proxies: true
})

const hasNewButNoneSelected = computed(() => {
  if (!previewResult.value) return false
  return previewResult.value.new_accounts.length > 0 && selectedIds.value.size === 0
})

interface ForceSyncSnapshot {
  accounts: CRSPreviewAccount[]
  expectedVersions: Record<number, number>
  listingCount: number
  error: string
}

const forceSyncSnapshot = computed<ForceSyncSnapshot>(() => {
  const accounts = (previewResult.value?.existing_accounts || []).filter(account =>
    account.requires_force_active_edit || (Array.isArray(account.room_bindings) && account.room_bindings.length > 0)
  )
  const expectedVersions: Record<number, number> = {}
  let error = ''

  for (const account of accounts) {
    if (!Number.isInteger(account.local_account_id) || Number(account.local_account_id) <= 0) {
      error = '预览缺少有效的本地账号 ID，无法安全强制同步。请返回并重新预览。'
      break
    }
    if (!Array.isArray(account.room_bindings) || account.room_bindings.length === 0) {
      error = '预览缺少房间版本信息，无法安全强制同步。请返回并重新预览。'
      break
    }
    for (const binding of account.room_bindings) {
      const listingID = Number(binding?.listing_id || 0)
      const rowVersion = Number(binding?.row_version || 0)
      if (!Number.isInteger(listingID) || listingID <= 0 || !Number.isInteger(rowVersion) || rowVersion <= 0) {
        error = '预览包含无效的房间版本，无法安全强制同步。请返回并重新预览。'
        break
      }
      if (expectedVersions[listingID] && expectedVersions[listingID] !== rowVersion) {
        error = `房间 #${listingID} 的预览版本不一致，无法安全强制同步。请返回并重新预览。`
        break
      }
      expectedVersions[listingID] = rowVersion
    }
    if (error) break
  }

  return {
    accounts,
    expectedVersions,
    listingCount: Object.keys(expectedVersions).length,
    error
  }
})

const requiresForceConfirmation = computed(() => forceSyncSnapshot.value.accounts.length > 0)
const syncDisabled = computed(() => {
  if (
    syncing.value
    || hasNewButNoneSelected.value
    || !previewConnectionSnapshot.value
    || !previewResult.value?.preview_token?.trim()
  ) return true
  if (!requiresForceConfirmation.value) return false
  return Boolean(forceSyncSnapshot.value.error)
    || !forceConfirmed.value
    || forceReason.value.trim().length === 0
})

const errorItems = computed(() => {
  if (!result.value?.items) return []
  return result.value.items.filter(
    (i) => i.action === 'failed' || (i.action === 'skipped' && i.error !== 'not selected')
  )
})

watch(
  () => props.show,
  (open) => {
    if (open) {
      resetModalState()
    } else {
      invalidatePreviewRequest()
      previewConnectionSnapshot.value = null
      form.password = ''
    }
  },
  { immediate: true }
)

onBeforeUnmount(() => {
  invalidatePreviewRequest()
  previewConnectionSnapshot.value = null
})

function invalidatePreviewRequest(): void {
  previewRequestVersion += 1
  previewing.value = false
}

function resetModalState(): void {
  invalidatePreviewRequest()
  currentStep.value = 'input'
  previewResult.value = null
  previewConnectionSnapshot.value = null
  selectedIds.value = new Set()
  result.value = null
  forceConfirmed.value = false
  forceReason.value = ''
  syncIdempotencyKey.value = ''
  syncPayloadSignature.value = ''
  form.base_url = ''
  form.username = ''
  form.password = ''
  form.sync_proxies = true
}

function createConnectionSnapshot(): Readonly<CRSConnectionSnapshot> | null {
  const baseURL = form.base_url.trim()
  const username = form.username.trim()
  const password = form.password
  if (!baseURL || !username || !password.trim()) return null
  return Object.freeze({
    base_url: baseURL,
    username,
    password,
    sync_proxies: form.sync_proxies
  })
}

const handleClose = () => {
  if (syncing.value || previewing.value) {
    return
  }
  resetModalState()
  emit('close')
}

const handleBack = () => {
  currentStep.value = 'input'
  previewResult.value = null
  previewConnectionSnapshot.value = null
  selectedIds.value = new Set()
  forceConfirmed.value = false
  forceReason.value = ''
  syncIdempotencyKey.value = ''
  syncPayloadSignature.value = ''
}

const selectAll = () => {
  if (!previewResult.value || syncing.value) return
  selectedIds.value = new Set(previewResult.value.new_accounts.map((a) => a.crs_account_id))
}

const selectNone = () => {
  if (syncing.value) return
  selectedIds.value = new Set()
}

const toggleSelect = (id: string) => {
  if (syncing.value) return
  const s = new Set(selectedIds.value)
  if (s.has(id)) {
    s.delete(id)
  } else {
    s.add(id)
  }
  selectedIds.value = s
}

const handlePreview = async () => {
  if (previewing.value || syncing.value) return
  const connectionSnapshot = createConnectionSnapshot()
  if (!connectionSnapshot) {
    appStore.showError(t('admin.accounts.syncMissingFields'))
    return
  }

  const requestVersion = ++previewRequestVersion
  previewing.value = true
  previewConnectionSnapshot.value = null
  try {
    const res = await adminAPI.accounts.previewFromCrs({
      base_url: connectionSnapshot.base_url,
      username: connectionSnapshot.username,
      password: connectionSnapshot.password
    })
    if (requestVersion !== previewRequestVersion || !props.show) return
    if (
      !res.preview_token?.trim()
      || !Number.isSafeInteger(res.expires_at)
      || res.expires_at <= 0
    ) {
      appStore.showError('CRS 预览响应缺少有效的安全令牌，请检查服务端配置后重试')
      return
    }
    previewResult.value = res
    previewConnectionSnapshot.value = connectionSnapshot
    // Auto-select all new accounts
    selectedIds.value = new Set(res.new_accounts.map((a) => a.crs_account_id))
    forceConfirmed.value = false
    forceReason.value = ''
    syncIdempotencyKey.value = ''
    syncPayloadSignature.value = ''
    currentStep.value = 'preview'
  } catch (error: any) {
    if (requestVersion !== previewRequestVersion || !props.show) return
    appStore.showError(error?.message || t('admin.accounts.crsPreviewFailed'))
  } finally {
    if (requestVersion === previewRequestVersion) {
      previewing.value = false
    }
  }
}

const handleSync = async () => {
  if (syncing.value) return
  const connectionSnapshot = previewConnectionSnapshot.value
  const previewToken = previewResult.value?.preview_token?.trim()
  if (!connectionSnapshot || !previewToken) {
    appStore.showError(t('admin.accounts.syncMissingFields'))
    return
  }
  if (syncDisabled.value) {
    if (forceSyncSnapshot.value.error) {
      appStore.showError(forceSyncSnapshot.value.error)
    } else if (requiresForceConfirmation.value) {
      appStore.showError('房间账号同步前必须确认管理员强制编辑并填写原因')
    }
    return
  }

  const forceActiveEdit = requiresForceConfirmation.value
  const payload = {
    base_url: connectionSnapshot.base_url,
    username: connectionSnapshot.username,
    password: connectionSnapshot.password,
    sync_proxies: connectionSnapshot.sync_proxies,
    selected_account_ids: [...selectedIds.value],
    force_active_edit: forceActiveEdit,
    confirmed: forceActiveEdit && forceConfirmed.value,
    reason: forceActiveEdit ? forceReason.value.trim() : undefined,
    expected_versions: forceActiveEdit ? forceSyncSnapshot.value.expectedVersions : undefined,
    preview_token: previewToken
  }
  const payloadSignature = JSON.stringify(payload)
  if (!syncIdempotencyKey.value || syncPayloadSignature.value !== payloadSignature) {
    if (!globalThis.crypto?.randomUUID) {
      appStore.showError('当前浏览器无法生成安全操作标识，请升级浏览器后重新预览')
      return
    }
    syncIdempotencyKey.value = `crs-sync-${globalThis.crypto.randomUUID()}`
    syncPayloadSignature.value = payloadSignature
  }

  syncing.value = true
  try {
    const res = await adminAPI.accounts.syncFromCrs(payload, syncIdempotencyKey.value)
    result.value = res
    previewConnectionSnapshot.value = null
    form.password = ''
    currentStep.value = 'result'

    if (res.failed > 0) {
      appStore.showError(t('admin.accounts.syncCompletedWithErrors', res))
    } else {
      appStore.showSuccess(t('admin.accounts.syncCompleted', res))
    }
    emit('synced')
  } catch (error: any) {
    const code = extractApiErrorCode(error)
    if (
      code === 'CRS_PREVIEW_TOKEN_REQUIRED'
      || code === 'CRS_PREVIEW_TOKEN_INVALID'
      || code === 'CRS_PREVIEW_TOKEN_EXPIRED'
      || code === 'CRS_PREVIEW_CONTEXT_CONFLICT'
    ) {
      appStore.showError('本次 CRS 预览已失效或数据已变化，请重新预览后再同步')
      handleBack()
    } else {
      appStore.showError(error?.message || t('admin.accounts.syncFailed'))
    }
  } finally {
    syncing.value = false
  }
}
</script>
