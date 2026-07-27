<template>
  <BaseDialog
    :show="show"
    title="账号广场 · 异常计费处置"
    width="extra-wide"
    :close-disabled="waiving"
    :close-on-escape="!waiving"
    :close-on-click-outside="false"
    panel-class="account-share-billing-admin-panel"
    @close="requestClose"
  >
    <div class="billing-admin-shell" data-testid="billing-attention-dialog">
      <header class="billing-admin-summary">
        <span class="billing-admin-summary-icon" aria-hidden="true">
          <Icon name="shield" size="md" />
        </span>
        <div>
          <span>仅管理员可见</span>
          <strong>待人工确认的计费意图</strong>
          <p>这里只展示脱敏记录。豁免会将当前异常意图标记为已取消，并保留操作者、原因与状态版本审计。</p>
        </div>
        <div class="billing-admin-count" aria-live="polite">
          <span>待处理</span>
          <strong>{{ listLoading && !loaded ? '—' : total }}</strong>
        </div>
      </header>

      <div
        v-if="listError"
        class="billing-admin-alert billing-admin-alert-error"
        role="alert"
        data-testid="billing-attention-list-error"
      >
        <Icon name="exclamationCircle" size="sm" />
        <span>{{ listError }}</span>
      </div>

      <div class="billing-admin-layout">
        <section class="billing-admin-list-panel" aria-label="待处理计费意图列表">
          <div class="billing-admin-section-head">
            <div>
              <strong>待处理记录</strong>
              <span>按更新时间查看并选择一条记录</span>
            </div>
            <button
              type="button"
              class="billing-admin-icon-button"
              :disabled="listLoading || waiving"
              aria-label="刷新待处理计费记录"
              data-testid="refresh-billing-attention"
              @click="loadList(page)"
            >
              <Icon name="refresh" size="sm" :class="{ 'animate-spin': listLoading }" />
            </button>
          </div>

          <div
            v-if="listLoading && !loaded"
            class="billing-admin-empty"
            role="status"
          >
            <Icon name="refresh" size="sm" class="animate-spin" />
            <span>正在读取待处理记录…</span>
          </div>

          <div v-else-if="items.length === 0" class="billing-admin-empty" data-testid="billing-attention-empty">
            <Icon name="checkCircle" size="md" />
            <strong>当前没有待处理记录</strong>
            <span>计费恢复任务运行正常时，这里应保持为空。</span>
          </div>

          <div v-else class="billing-admin-intent-list">
            <button
              v-for="item in items"
              :key="item.id"
              type="button"
              class="billing-admin-intent-card"
              :class="{ 'billing-admin-intent-card-active': selectedIntentID === item.id }"
              :aria-pressed="selectedIntentID === item.id"
              :disabled="waiving"
              :data-testid="`billing-intent-${item.id}`"
              @click="selectIntent(item)"
            >
              <span class="billing-admin-intent-card-top">
                <strong>意图 #{{ item.id }}</strong>
                <span>v{{ item.state_token }}</span>
              </span>
              <span class="billing-admin-intent-meta">
                房间 #{{ item.listing_id }} · 成员 #{{ item.membership_id }}
              </span>
              <span class="billing-admin-intent-error">
                {{ item.last_error_code || '未提供错误代码' }}
              </span>
              <time :datetime="item.updated_at">{{ formatDateTime(item.updated_at) }}</time>
            </button>
          </div>

          <div v-if="pages > 1" class="billing-admin-pagination" aria-label="计费意图分页">
            <button
              type="button"
              class="btn btn-secondary min-h-11"
              :disabled="page <= 1 || listLoading || waiving"
              @click="loadList(page - 1)"
            >
              上一页
            </button>
            <span>第 {{ page }} / {{ pages }} 页</span>
            <button
              type="button"
              class="btn btn-secondary min-h-11"
              :disabled="page >= pages || listLoading || waiving"
              @click="loadList(page + 1)"
            >
              下一页
            </button>
          </div>
        </section>

        <section class="billing-admin-detail-panel" aria-label="计费意图详情">
          <div v-if="!selectedIntentID" class="billing-admin-empty billing-admin-detail-empty">
            <Icon name="database" size="md" />
            <strong>选择一条记录查看详情</strong>
            <span>确认房间、成员、请求标识和错误原因后，才能进入豁免确认。</span>
          </div>

          <div v-else-if="detailLoading && !selectedIntent" class="billing-admin-empty" role="status">
            <Icon name="refresh" size="sm" class="animate-spin" />
            <span>正在读取脱敏详情…</span>
          </div>

          <div v-else class="billing-admin-detail">
            <div class="billing-admin-section-head">
              <div>
                <strong>意图 #{{ selectedIntentID }}</strong>
                <span>当前状态版本用于防止并发误操作</span>
              </div>
              <span
                v-if="selectedIntent"
                class="billing-admin-status-badge"
                :class="{ 'billing-admin-status-badge-stale': selectedIntent.status !== 'needs_attention' }"
              >
                {{ billingStatusLabel(selectedIntent.status) }}
              </span>
            </div>

            <div
              v-if="detailError"
              class="billing-admin-alert billing-admin-alert-error"
              role="alert"
              data-testid="billing-attention-detail-error"
            >
              <Icon name="exclamationCircle" size="sm" />
              <span>{{ detailError }}</span>
            </div>

            <template v-if="selectedIntent">
              <dl class="billing-admin-detail-grid">
                <div>
                  <dt>状态版本</dt>
                  <dd>v{{ selectedIntent.state_token }}</dd>
                </div>
                <div>
                  <dt>重试次数</dt>
                  <dd>{{ selectedIntent.attempt_no }}</dd>
                </div>
                <div>
                  <dt>房间</dt>
                  <dd>#{{ selectedIntent.listing_id }}</dd>
                </div>
                <div>
                  <dt>账号</dt>
                  <dd>#{{ selectedIntent.account_id }}</dd>
                </div>
                <div>
                  <dt>成员</dt>
                  <dd>#{{ selectedIntent.membership_id }}</dd>
                </div>
                <div>
                  <dt>API Key</dt>
                  <dd>#{{ selectedIntent.api_key_id }}</dd>
                </div>
                <div class="billing-admin-detail-wide">
                  <dt>请求标识</dt>
                  <dd>{{ selectedIntent.request_id || '—' }}</dd>
                </div>
                <div class="billing-admin-detail-wide">
                  <dt>调度标识</dt>
                  <dd>{{ selectedIntent.dispatch_id || '—' }}</dd>
                </div>
              </dl>

              <div class="billing-admin-error-card">
                <span>最近错误</span>
                <strong>{{ selectedIntent.last_error_code || '未提供错误代码' }}</strong>
                <p>{{ selectedIntent.last_error_message || '后端没有记录额外错误说明。' }}</p>
                <small>更新时间：{{ formatDateTime(selectedIntent.updated_at) }}</small>
              </div>

              <label class="billing-admin-reason">
                <span>豁免原因</span>
                <textarea
                  v-model="waiveReason"
                  class="input"
                  rows="4"
                  maxlength="1000"
                  :disabled="waiving || selectedIntent.status !== 'needs_attention'"
                  placeholder="请写明核对依据、无法恢复的原因和人工处置结论"
                  data-testid="billing-waive-reason"
                ></textarea>
                <small>{{ waiveReason.trim().length }}/1000 · 原因会写入不可变审计记录</small>
              </label>

              <div
                v-if="selectedIntent.status !== 'needs_attention'"
                class="billing-admin-alert billing-admin-alert-warning"
                role="status"
              >
                <Icon name="exclamationCircle" size="sm" />
                <span>该记录状态已经变化，不能再按旧版本豁免。请刷新列表确认最新结果。</span>
              </div>
            </template>
          </div>
        </section>
      </div>
    </div>

    <template #footer>
      <div class="billing-admin-footer">
        <button
          type="button"
          class="btn btn-secondary min-h-11"
          :disabled="waiving"
          @click="requestClose"
        >
          关闭
        </button>
        <button
          type="button"
          class="btn btn-danger min-h-11"
          :disabled="!canPrepareWaiver"
          data-testid="prepare-billing-waive"
          @click="prepareWaiver"
        >
          进入豁免确认
        </button>
      </div>
    </template>
  </BaseDialog>

  <BaseDialog
    :show="waiveConfirmationOpen"
    title="最终确认计费豁免"
    width="narrow"
    :z-index="70"
    :close-disabled="waiving"
    :close-on-escape="!waiving"
    :close-on-click-outside="false"
    @close="cancelWaiver"
  >
    <div class="billing-waive-confirmation">
      <div class="billing-admin-alert billing-admin-alert-warning">
        <Icon name="exclamationCircle" size="sm" />
        <span>此操作不会补做计费，而是把异常意图从 <strong>needs_attention</strong> 变为 <strong>cancelled</strong>。</span>
      </div>

      <dl>
        <div>
          <dt>计费意图</dt>
          <dd>#{{ selectedIntent?.id || '—' }}</dd>
        </div>
        <div>
          <dt>期望版本</dt>
          <dd>v{{ waiverExpectedStateToken || '—' }}</dd>
        </div>
        <div>
          <dt>豁免原因</dt>
          <dd>{{ waiverReasonSnapshot }}</dd>
        </div>
      </dl>

      <label class="billing-admin-confirm-check">
        <input
          v-model="waiverConfirmed"
          type="checkbox"
          :disabled="waiving"
          data-testid="billing-waive-confirmed"
        />
        <span>
          <strong>我已核对房间、成员和错误信息</strong>
          <small>确认当前异常无法安全恢复，并接受本次操作留下管理员审计记录。</small>
        </span>
      </label>

      <div
        v-if="waiveError"
        class="billing-admin-alert billing-admin-alert-error"
        role="alert"
        data-testid="billing-waive-error"
      >
        <Icon name="exclamationCircle" size="sm" />
        <span>{{ waiveError }}</span>
      </div>
    </div>

    <template #footer>
      <div class="billing-admin-footer">
        <button
          type="button"
          class="btn btn-secondary min-h-11"
          :disabled="waiving"
          @click="cancelWaiver"
        >
          返回核对
        </button>
        <button
          type="button"
          class="btn btn-danger min-h-11"
          :disabled="!waiverConfirmed || waiving"
          data-testid="confirm-billing-waive"
          @click="submitWaiver"
        >
          <Icon
            :name="waiving ? 'refresh' : 'checkCircle'"
            size="sm"
            class="mr-2"
            :class="{ 'animate-spin': waiving }"
          />
          {{ waiving ? '提交中…' : '确认豁免' }}
        </button>
      </div>
    </template>
  </BaseDialog>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import accountShareBillingAdminAPI, {
  type AccountShareBillingIntentAdminRecord,
  type AccountShareBillingIntentWaiverResult
} from '@/api/admin/accountShareBilling'
import BaseDialog from '@/components/common/BaseDialog.vue'
import Icon from '@/components/icons/Icon.vue'
import { useAppStore } from '@/stores/app'
import { extractApiErrorCode, extractApiErrorMessage } from '@/utils/apiError'
import { formatDateTime } from '@/utils/format'

const props = defineProps<{
  show: boolean
}>()

const emit = defineEmits<{
  close: []
  resolved: [AccountShareBillingIntentWaiverResult]
}>()

const PAGE_SIZE = 12
const BILLING_ADMIN_ERROR_MESSAGES: Record<string, string> = {
  ACCOUNT_SHARE_BILLING_ADMIN_REQUIRED: '管理员身份已失效，请重新登录后再试。',
  ACCOUNT_SHARE_BILLING_INTENT_NOT_FOUND: '该计费意图不存在或已经被清理。',
  ACCOUNT_SHARE_BILLING_INTENT_ADMIN_CONFLICT: '该计费意图已被其他管理员或后台任务更新，请核对最新状态。',
  ACCOUNT_SHARE_BILLING_WAIVER_INVALID: '豁免参数无效，请重新选择记录并填写原因。',
  ACCOUNT_SHARE_BILLING_WAIVER_REASON_REQUIRED: '请填写清晰、可审计的豁免原因。',
  ACCOUNT_SHARE_BILLING_WAIVER_CONFIRMATION_REQUIRED: '必须完成最终确认后才能提交豁免。'
}

const appStore = useAppStore()
const items = ref<AccountShareBillingIntentAdminRecord[]>([])
const total = ref(0)
const page = ref(1)
const pages = ref(1)
const loaded = ref(false)
const listLoading = ref(false)
const listError = ref('')
const selectedIntentID = ref<number | null>(null)
const selectedIntent = ref<AccountShareBillingIntentAdminRecord | null>(null)
const detailLoading = ref(false)
const detailError = ref('')
const waiveReason = ref('')
const waiveConfirmationOpen = ref(false)
const waiverReasonSnapshot = ref('')
const waiverExpectedStateToken = ref(0)
const waiverConfirmed = ref(false)
const waiving = ref(false)
const waiveError = ref('')
let listRequestVersion = 0
let detailRequestVersion = 0
let listController: AbortController | null = null
let detailController: AbortController | null = null

const canPrepareWaiver = computed(() => {
  const intent = selectedIntent.value
  return Boolean(
    intent
    && intent.status === 'needs_attention'
    && Number.isSafeInteger(intent.state_token)
    && intent.state_token > 0
    && waiveReason.value.trim().length > 0
    && !detailLoading.value
    && !waiving.value
  )
})

watch(
  () => props.show,
  (show) => {
    if (!show) {
      resetDialogState()
      return
    }
    void loadList(1)
  },
  { immediate: true }
)

onBeforeUnmount(() => {
  invalidateRequests()
})

function invalidateRequests(): void {
  listRequestVersion += 1
  detailRequestVersion += 1
  listController?.abort()
  detailController?.abort()
  listController = null
  detailController = null
}

function resetDialogState(): void {
  invalidateRequests()
  items.value = []
  total.value = 0
  page.value = 1
  pages.value = 1
  loaded.value = false
  listLoading.value = false
  listError.value = ''
  clearSelection()
}

function clearSelection(): void {
  detailRequestVersion += 1
  detailController?.abort()
  detailController = null
  selectedIntentID.value = null
  selectedIntent.value = null
  detailLoading.value = false
  detailError.value = ''
  waiveReason.value = ''
  resetWaiverConfirmation()
}

function resetWaiverConfirmation(): void {
  waiveConfirmationOpen.value = false
  waiverReasonSnapshot.value = ''
  waiverExpectedStateToken.value = 0
  waiverConfirmed.value = false
  waiveError.value = ''
}

function requestClose(): void {
  if (waiving.value) return
  resetDialogState()
  emit('close')
}

function isCanceledRequest(error: unknown): boolean {
  if (error instanceof DOMException && error.name === 'AbortError') return true
  if (!error || typeof error !== 'object') return false
  const candidate = error as { code?: unknown; name?: unknown }
  return candidate.code === 'ERR_CANCELED' || candidate.name === 'CanceledError'
}

function billingErrorMessage(error: unknown, fallback: string): string {
  return extractApiErrorMessage(error, fallback, BILLING_ADMIN_ERROR_MESSAGES)
}

async function loadList(targetPage = page.value): Promise<void> {
  if (!props.show) return
  listController?.abort()
  const controller = new AbortController()
  listController = controller
  const requestVersion = ++listRequestVersion
  listLoading.value = true
  listError.value = ''
  try {
    const result = await accountShareBillingAdminAPI.listNeedsAttention(
      Math.max(1, targetPage),
      PAGE_SIZE,
      { signal: controller.signal }
    )
    if (
      controller.signal.aborted
      || requestVersion !== listRequestVersion
      || !props.show
    ) {
      return
    }
    items.value = result.items || []
    total.value = Math.max(0, Number(result.total || 0))
    page.value = Math.max(1, Number(result.page || targetPage || 1))
    pages.value = Math.max(1, Number(result.pages || 1))
    loaded.value = true

    const selectedStillVisible = items.value.find((item) => item.id === selectedIntentID.value)
    if (selectedStillVisible) {
      selectedIntent.value = selectedIntent.value
        ? { ...selectedIntent.value, ...selectedStillVisible }
        : selectedStillVisible
    } else if (items.value.length > 0) {
      void selectIntent(items.value[0])
    } else {
      clearSelection()
    }
  } catch (error: unknown) {
    if (
      controller.signal.aborted
      || requestVersion !== listRequestVersion
      || isCanceledRequest(error)
    ) {
      return
    }
    listError.value = billingErrorMessage(error, '读取待处理计费记录失败，请稍后重试。')
  } finally {
    if (requestVersion === listRequestVersion) {
      listLoading.value = false
      if (listController === controller) listController = null
    }
  }
}

async function selectIntent(item: AccountShareBillingIntentAdminRecord): Promise<void> {
  if (!props.show || waiving.value) return
  selectedIntentID.value = item.id
  selectedIntent.value = null
  detailError.value = ''
  waiveReason.value = ''
  resetWaiverConfirmation()

  detailController?.abort()
  const controller = new AbortController()
  detailController = controller
  const requestVersion = ++detailRequestVersion
  detailLoading.value = true
  try {
    const detail = await accountShareBillingAdminAPI.getDetail(item.id, {
      signal: controller.signal
    })
    if (
      controller.signal.aborted
      || requestVersion !== detailRequestVersion
      || selectedIntentID.value !== item.id
      || !props.show
    ) {
      return
    }
    selectedIntent.value = detail
  } catch (error: unknown) {
    if (
      controller.signal.aborted
      || requestVersion !== detailRequestVersion
      || isCanceledRequest(error)
    ) {
      return
    }
    detailError.value = billingErrorMessage(error, '读取计费意图详情失败，请重新选择或刷新列表。')
  } finally {
    if (requestVersion === detailRequestVersion) {
      detailLoading.value = false
      if (detailController === controller) detailController = null
    }
  }
}

function prepareWaiver(): void {
  const intent = selectedIntent.value
  const reason = waiveReason.value.trim()
  if (
    !canPrepareWaiver.value
    || !intent
    || !reason
  ) {
    return
  }
  waiverReasonSnapshot.value = reason
  waiverExpectedStateToken.value = intent.state_token
  waiverConfirmed.value = false
  waiveError.value = ''
  waiveConfirmationOpen.value = true
}

function cancelWaiver(): void {
  if (waiving.value) return
  resetWaiverConfirmation()
}

async function submitWaiver(): Promise<void> {
  const intentID = selectedIntent.value?.id
  if (
    !waiveConfirmationOpen.value
    || !waiverConfirmed.value
    || waiving.value
    || !intentID
    || waiverExpectedStateToken.value <= 0
    || !waiverReasonSnapshot.value
  ) {
    return
  }

  waiving.value = true
  waiveError.value = ''
  try {
    const result = await accountShareBillingAdminAPI.waive(intentID, {
      expected_state_token: waiverExpectedStateToken.value,
      reason: waiverReasonSnapshot.value,
      confirmed: true
    })
    if (!props.show) return
    resetWaiverConfirmation()
    clearSelection()
    emit('resolved', result)
    appStore.showSuccess(`计费意图 #${intentID} 已豁免并保留审计记录`)
    await loadList(page.value)
  } catch (error: unknown) {
    if (!props.show) return
    const code = extractApiErrorCode(error)
    waiveError.value = billingErrorMessage(error, '计费豁免失败，请核对记录状态后重试。')
    if (
      code === 'ACCOUNT_SHARE_BILLING_INTENT_ADMIN_CONFLICT'
      || code === 'ACCOUNT_SHARE_BILLING_INTENT_NOT_FOUND'
    ) {
      const conflictMessage = waiveError.value
      resetWaiverConfirmation()
      detailError.value = conflictMessage
      await loadList(page.value)
      const refreshed = items.value.find((item) => item.id === intentID)
      if (refreshed) await selectIntent(refreshed)
    }
  } finally {
    waiving.value = false
  }
}

function billingStatusLabel(status: string): string {
  if (status === 'needs_attention') return '需要人工处置'
  if (status === 'cancelled') return '已取消'
  if (status === 'settled') return '已结算'
  if (status === 'processing') return '处理中'
  if (status === 'failed') return '失败'
  return status || '未知状态'
}
</script>

<style scoped>
.billing-admin-shell {
  display: grid;
  min-width: 0;
  gap: 1rem;
}

.billing-admin-summary {
  display: grid;
  min-width: 0;
  grid-template-columns: auto minmax(0, 1fr);
  gap: 0.75rem;
  align-items: start;
  border: 1px solid rgb(191 219 254);
  border-radius: 1rem;
  background: linear-gradient(135deg, rgb(239 246 255), rgb(248 250 252));
  padding: 1rem;
}

.billing-admin-summary-icon {
  display: inline-flex;
  width: 2.75rem;
  height: 2.75rem;
  align-items: center;
  justify-content: center;
  border-radius: 0.875rem;
  background: rgb(219 234 254);
  color: rgb(29 78 216);
}

.billing-admin-summary span,
.billing-admin-summary strong,
.billing-admin-summary p {
  display: block;
}

.billing-admin-summary > div > span {
  color: rgb(37 99 235);
  font-size: 0.75rem;
  font-weight: 700;
  letter-spacing: 0.04em;
}

.billing-admin-summary > div > strong {
  margin-top: 0.125rem;
  color: rgb(15 23 42);
  font-size: 1rem;
}

.billing-admin-summary p {
  margin-top: 0.375rem;
  max-width: 70ch;
  color: rgb(71 85 105);
  font-size: 0.8125rem;
  line-height: 1.35rem;
}

.billing-admin-count {
  display: flex;
  grid-column: 1 / -1;
  min-height: 3.5rem;
  align-items: center;
  justify-content: space-between;
  border-radius: 0.75rem;
  background: rgb(255 255 255 / 0.82);
  padding: 0.625rem 0.875rem;
}

.billing-admin-count span {
  color: rgb(71 85 105);
  font-size: 0.8125rem;
}

.billing-admin-count strong {
  color: rgb(185 28 28);
  font-size: 1.25rem;
}

.billing-admin-layout {
  display: grid;
  min-width: 0;
  gap: 1rem;
}

.billing-admin-list-panel,
.billing-admin-detail-panel {
  display: flex;
  min-width: 0;
  flex-direction: column;
  gap: 0.875rem;
  border: 1px solid rgb(226 232 240);
  border-radius: 1rem;
  background: rgb(255 255 255);
  padding: 0.875rem;
}

.billing-admin-section-head {
  display: flex;
  min-width: 0;
  align-items: flex-start;
  justify-content: space-between;
  gap: 0.75rem;
}

.billing-admin-section-head strong,
.billing-admin-section-head span {
  display: block;
}

.billing-admin-section-head strong {
  color: rgb(15 23 42);
  font-size: 0.9375rem;
}

.billing-admin-section-head > div > span {
  margin-top: 0.25rem;
  color: rgb(100 116 139);
  font-size: 0.75rem;
}

.billing-admin-icon-button {
  display: inline-flex;
  min-width: 2.75rem;
  min-height: 2.75rem;
  flex: 0 0 2.75rem;
  cursor: pointer;
  align-items: center;
  justify-content: center;
  border: 1px solid rgb(226 232 240);
  border-radius: 0.75rem;
  color: rgb(71 85 105);
  transition: border-color 150ms ease, background-color 150ms ease, color 150ms ease;
}

.billing-admin-icon-button:hover:not(:disabled) {
  border-color: rgb(147 197 253);
  background: rgb(239 246 255);
  color: rgb(29 78 216);
}

.billing-admin-icon-button:disabled {
  cursor: not-allowed;
  opacity: 0.5;
}

.billing-admin-icon-button:focus-visible,
.billing-admin-intent-card:focus-visible {
  outline: 2px solid rgb(59 130 246 / 0.7);
  outline-offset: 2px;
}

.billing-admin-intent-list {
  display: grid;
  gap: 0.625rem;
}

.billing-admin-intent-card {
  display: grid;
  min-width: 0;
  min-height: 2.75rem;
  cursor: pointer;
  gap: 0.375rem;
  border: 1px solid rgb(226 232 240);
  border-radius: 0.875rem;
  background: rgb(248 250 252);
  padding: 0.75rem;
  text-align: left;
  transition: border-color 150ms ease, background-color 150ms ease, box-shadow 150ms ease;
}

.billing-admin-intent-card:hover:not(:disabled) {
  border-color: rgb(147 197 253);
  background: rgb(239 246 255);
}

.billing-admin-intent-card:disabled {
  cursor: not-allowed;
  opacity: 0.65;
}

.billing-admin-intent-card-active {
  border-color: rgb(59 130 246);
  background: rgb(239 246 255);
  box-shadow: 0 0 0 2px rgb(59 130 246 / 0.12);
}

.billing-admin-intent-card-top {
  display: flex;
  min-width: 0;
  align-items: center;
  justify-content: space-between;
  gap: 0.75rem;
}

.billing-admin-intent-card-top strong {
  color: rgb(15 23 42);
  font-size: 0.875rem;
}

.billing-admin-intent-card-top > span {
  flex-shrink: 0;
  border-radius: 9999px;
  background: rgb(254 226 226);
  padding: 0.125rem 0.5rem;
  color: rgb(185 28 28);
  font-size: 0.6875rem;
  font-weight: 700;
}

.billing-admin-intent-meta,
.billing-admin-intent-error,
.billing-admin-intent-card time {
  overflow-wrap: anywhere;
  color: rgb(100 116 139);
  font-size: 0.75rem;
}

.billing-admin-intent-error {
  color: rgb(153 27 27);
  font-weight: 600;
}

.billing-admin-pagination {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  justify-content: space-between;
  gap: 0.625rem;
}

.billing-admin-pagination > span {
  color: rgb(100 116 139);
  font-size: 0.75rem;
}

.billing-admin-empty {
  display: flex;
  min-height: 10rem;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 0.5rem;
  border: 1px dashed rgb(203 213 225);
  border-radius: 0.875rem;
  padding: 1.5rem;
  color: rgb(100 116 139);
  text-align: center;
}

.billing-admin-empty strong {
  color: rgb(51 65 85);
  font-size: 0.875rem;
}

.billing-admin-empty span {
  max-width: 42ch;
  font-size: 0.75rem;
  line-height: 1.25rem;
}

.billing-admin-detail {
  display: grid;
  min-width: 0;
  gap: 0.875rem;
}

.billing-admin-status-badge {
  flex-shrink: 0;
  border-radius: 9999px;
  background: rgb(254 226 226);
  padding: 0.375rem 0.625rem;
  color: rgb(185 28 28);
  font-size: 0.6875rem;
  font-weight: 700;
}

.billing-admin-status-badge-stale {
  background: rgb(226 232 240);
  color: rgb(71 85 105);
}

.billing-admin-detail-grid {
  display: grid;
  min-width: 0;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 0.625rem;
  margin: 0;
}

.billing-admin-detail-grid > div {
  min-width: 0;
  border-radius: 0.75rem;
  background: rgb(248 250 252);
  padding: 0.625rem 0.75rem;
}

.billing-admin-detail-grid dt {
  color: rgb(100 116 139);
  font-size: 0.6875rem;
}

.billing-admin-detail-grid dd {
  overflow-wrap: anywhere;
  margin: 0.25rem 0 0;
  color: rgb(30 41 59);
  font-size: 0.8125rem;
  font-weight: 650;
}

.billing-admin-detail-wide {
  grid-column: 1 / -1;
}

.billing-admin-error-card {
  min-width: 0;
  border: 1px solid rgb(254 202 202);
  border-radius: 0.875rem;
  background: rgb(254 242 242);
  padding: 0.875rem;
}

.billing-admin-error-card span,
.billing-admin-error-card strong,
.billing-admin-error-card p,
.billing-admin-error-card small {
  display: block;
}

.billing-admin-error-card span,
.billing-admin-error-card small {
  color: rgb(153 27 27);
  font-size: 0.6875rem;
}

.billing-admin-error-card strong {
  margin-top: 0.25rem;
  overflow-wrap: anywhere;
  color: rgb(127 29 29);
  font-size: 0.8125rem;
}

.billing-admin-error-card p {
  margin-top: 0.375rem;
  overflow-wrap: anywhere;
  color: rgb(153 27 27);
  font-size: 0.75rem;
  line-height: 1.25rem;
}

.billing-admin-error-card small {
  margin-top: 0.5rem;
}

.billing-admin-reason {
  display: grid;
  gap: 0.375rem;
}

.billing-admin-reason > span {
  color: rgb(51 65 85);
  font-size: 0.8125rem;
  font-weight: 650;
}

.billing-admin-reason textarea {
  min-height: 7rem;
  resize: vertical;
}

.billing-admin-reason small {
  color: rgb(100 116 139);
  font-size: 0.6875rem;
}

.billing-admin-alert {
  display: flex;
  min-width: 0;
  align-items: flex-start;
  gap: 0.625rem;
  border: 1px solid;
  border-radius: 0.875rem;
  padding: 0.75rem;
  font-size: 0.8125rem;
  line-height: 1.3rem;
}

.billing-admin-alert svg {
  margin-top: 0.125rem;
  flex-shrink: 0;
}

.billing-admin-alert span {
  min-width: 0;
  overflow-wrap: anywhere;
}

.billing-admin-alert-error {
  border-color: rgb(254 202 202);
  background: rgb(254 242 242);
  color: rgb(153 27 27);
}

.billing-admin-alert-warning {
  border-color: rgb(253 230 138);
  background: rgb(255 251 235);
  color: rgb(146 64 14);
}

.billing-admin-footer {
  display: flex;
  width: 100%;
  flex-direction: column-reverse;
  gap: 0.625rem;
}

.billing-waive-confirmation {
  display: grid;
  min-width: 0;
  gap: 1rem;
}

.billing-waive-confirmation dl {
  display: grid;
  gap: 0.625rem;
  margin: 0;
}

.billing-waive-confirmation dl > div {
  min-width: 0;
  border-radius: 0.75rem;
  background: rgb(248 250 252);
  padding: 0.75rem;
}

.billing-waive-confirmation dt {
  color: rgb(100 116 139);
  font-size: 0.6875rem;
}

.billing-waive-confirmation dd {
  margin: 0.25rem 0 0;
  overflow-wrap: anywhere;
  color: rgb(30 41 59);
  font-size: 0.8125rem;
  font-weight: 650;
}

.billing-admin-confirm-check {
  display: flex;
  min-width: 0;
  cursor: pointer;
  align-items: flex-start;
  gap: 0.75rem;
  border: 1px solid rgb(254 202 202);
  border-radius: 0.875rem;
  background: rgb(254 242 242);
  padding: 0.875rem;
}

.billing-admin-confirm-check input {
  width: 1.25rem;
  height: 1.25rem;
  flex: 0 0 1.25rem;
  margin-top: 0.125rem;
}

.billing-admin-confirm-check strong,
.billing-admin-confirm-check small {
  display: block;
}

.billing-admin-confirm-check strong {
  color: rgb(127 29 29);
  font-size: 0.8125rem;
}

.billing-admin-confirm-check small {
  margin-top: 0.25rem;
  color: rgb(153 27 27);
  font-size: 0.75rem;
  line-height: 1.25rem;
}

:global(.dark) .billing-admin-summary {
  border-color: rgb(30 64 175 / 0.55);
  background: linear-gradient(135deg, rgb(30 64 175 / 0.2), rgb(39 39 42 / 0.75));
}

:global(.dark) .billing-admin-summary-icon {
  background: rgb(30 64 175 / 0.35);
  color: rgb(147 197 253);
}

:global(.dark) .billing-admin-summary > div > span {
  color: rgb(147 197 253);
}

:global(.dark) .billing-admin-summary > div > strong,
:global(.dark) .billing-admin-section-head strong,
:global(.dark) .billing-admin-intent-card-top strong,
:global(.dark) .billing-admin-detail-grid dd,
:global(.dark) .billing-waive-confirmation dd,
:global(.dark) .billing-admin-empty strong {
  color: rgb(244 244 245);
}

:global(.dark) .billing-admin-summary p,
:global(.dark) .billing-admin-section-head > div > span,
:global(.dark) .billing-admin-intent-meta,
:global(.dark) .billing-admin-intent-card time,
:global(.dark) .billing-admin-detail-grid dt,
:global(.dark) .billing-admin-reason small,
:global(.dark) .billing-waive-confirmation dt,
:global(.dark) .billing-admin-empty span {
  color: rgb(161 161 170);
}

:global(.dark) .billing-admin-count,
:global(.dark) .billing-admin-list-panel,
:global(.dark) .billing-admin-detail-panel {
  border-color: rgb(63 63 70);
  background: rgb(39 39 42 / 0.78);
}

:global(.dark) .billing-admin-icon-button,
:global(.dark) .billing-admin-intent-card,
:global(.dark) .billing-admin-detail-grid > div,
:global(.dark) .billing-waive-confirmation dl > div {
  border-color: rgb(63 63 70);
  background: rgb(24 24 27);
  color: rgb(161 161 170);
}

:global(.dark) .billing-admin-intent-card:hover:not(:disabled),
:global(.dark) .billing-admin-intent-card-active {
  border-color: rgb(59 130 246);
  background: rgb(30 64 175 / 0.2);
}

:global(.dark) .billing-admin-empty {
  border-color: rgb(82 82 91);
  color: rgb(161 161 170);
}

:global(.dark) .billing-admin-alert-error,
:global(.dark) .billing-admin-error-card,
:global(.dark) .billing-admin-confirm-check {
  border-color: rgb(127 29 29);
  background: rgb(69 10 10 / 0.28);
  color: rgb(252 165 165);
}

:global(.dark) .billing-admin-error-card span,
:global(.dark) .billing-admin-error-card strong,
:global(.dark) .billing-admin-error-card p,
:global(.dark) .billing-admin-error-card small,
:global(.dark) .billing-admin-confirm-check strong,
:global(.dark) .billing-admin-confirm-check small {
  color: rgb(252 165 165);
}

:global(.dark) .billing-admin-alert-warning {
  border-color: rgb(120 53 15);
  background: rgb(69 26 3 / 0.28);
  color: rgb(253 186 116);
}

@media (min-width: 640px) {
  .billing-admin-summary {
    grid-template-columns: auto minmax(0, 1fr) auto;
  }

  .billing-admin-count {
    grid-column: auto;
    min-width: 7rem;
    flex-direction: column;
    justify-content: center;
  }

  .billing-admin-footer {
    flex-direction: row;
    justify-content: flex-end;
  }
}

@media (min-width: 1024px) {
  .billing-admin-layout {
    grid-template-columns: minmax(18rem, 0.82fr) minmax(0, 1.4fr);
    align-items: stretch;
  }

  .billing-admin-list-panel,
  .billing-admin-detail-panel {
    min-height: min(35rem, 65vh);
  }

  .billing-admin-intent-list {
    max-height: min(28rem, 52vh);
    overflow-y: auto;
    padding-right: 0.25rem;
  }
}
</style>
