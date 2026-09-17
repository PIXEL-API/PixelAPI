<template>
  <BaseDialog
    :show="listing !== null"
    :title="listing ? t('accountShare.lifecycle.titleWithName', { name: displayName }) : t('accountShare.lifecycle.title')"
    width="normal"
    :close-disabled="roomLifecycleCommandBusy"
    @close="closeRoomLifecycleDialog"
  >
    <div class="room-lifecycle-dialog" data-testid="room-lifecycle-dialog">
      <div
        v-if="roomLifecycleLoading"
        class="room-lifecycle-state-message"
        data-testid="room-lifecycle-loading"
      >
        <Icon name="refresh" size="sm" class="animate-spin" />
        <span>{{ t('accountShare.lifecycle.loading') }}</span>
      </div>

      <div
        v-if="roomLifecycleError"
        class="room-lifecycle-alert room-lifecycle-alert-danger"
        role="alert"
        data-testid="room-lifecycle-error"
      >
        <Icon name="exclamationCircle" size="sm" class="mt-0.5 flex-shrink-0" />
        <div class="min-w-0">
          <strong>{{ t('accountShare.lifecycle.opIncomplete') }}</strong>
          <p>{{ roomLifecycleError }}</p>
          <code v-if="roomLifecycleErrorCode">{{ roomLifecycleErrorCode }}</code>
        </div>
      </div>

      <div
        v-if="roomLifecycleDeleted"
        class="room-lifecycle-alert room-lifecycle-alert-success"
        data-testid="room-lifecycle-deleted"
      >
        <Icon name="checkCircle" size="sm" class="mt-0.5 flex-shrink-0" />
        <div>
          <strong>{{ t('accountShare.lifecycle.roomDeleted') }}</strong>
          <p>{{ t('accountShare.lifecycle.roomDeletedDesc') }}</p>
        </div>
      </div>

      <template v-else-if="roomLifecycleState">
        <section class="room-lifecycle-overview">
          <div class="room-lifecycle-overview-head">
            <div>
              <span class="room-lifecycle-eyebrow">{{ t('accountShare.lifecycle.currentStatus') }}</span>
              <div class="mt-1 flex flex-wrap items-center gap-2">
                <strong class="text-base text-gray-950 dark:text-white">
                  {{ roomLifecycleStatusLabel(roomLifecycleState.lifecycle_status) }}
                </strong>
                <span :class="roomLifecycleStatusBadgeClass(roomLifecycleState.lifecycle_status)">
                  {{ roomLifecycleHealthLabel(roomLifecycleState.health_state) }}
                </span>
              </div>
            </div>
            <span class="room-lifecycle-version">{{ t('accountShare.lifecycle.version', { rowVersion: roomLifecycleState.row_version }) }}</span>
          </div>
          <p v-if="roomLifecycleState.status_reason" class="room-lifecycle-status-reason">
            {{ roomLifecycleState.status_reason }}
          </p>
          <div class="room-lifecycle-metrics">
            <div>
              <span>{{ t('accountShare.lifecycle.consumerSeats') }}</span>
              <strong>{{ roomLifecycleState.active_seats }}/{{ roomLifecycleState.seat_limit }}</strong>
            </div>
            <div>
              <span>{{ t('accountShare.lifecycle.roomAccounts') }}</span>
              <strong>{{ roomLifecycleState.room_account_count }}</strong>
            </div>
            <div>
              <span>{{ t('accountShare.lifecycle.inFlightRequests') }}</span>
              <strong>{{ roomLifecycleState.in_flight_concurrency }}</strong>
            </div>
          </div>
        </section>

        <section
          v-if="roomLifecycleOperation"
          class="room-lifecycle-operation"
          data-testid="room-lifecycle-operation"
        >
          <div class="flex min-w-0 items-start gap-3">
            <Icon
              :name="roomLifecycleOperationTerminal ? (roomLifecycleOperation.status === 'succeeded' ? 'checkCircle' : 'exclamationCircle') : 'refresh'"
              size="sm"
              class="mt-0.5 flex-shrink-0"
              :class="{ 'animate-spin': roomLifecyclePolling }"
            />
            <div class="min-w-0">
              <strong>{{ roomLifecycleOperationLabel(roomLifecycleOperation) }}</strong>
              <p>{{ roomLifecycleOperationStatusDescription(roomLifecycleOperation) }}</p>
              <code>{{ roomLifecycleOperation.id }}</code>
              <small class="mt-1 block">{{ accountShareOperationWaitDuration(roomLifecycleOperation.created_at, props.nowMs) }}</small>
            </div>
          </div>
        </section>

        <p v-if="roomLifecycleLastQueryAt" class="text-xs text-gray-500 dark:text-dark-300" data-testid="room-operation-query-observation">
          {{ t('accountShare.lifecycle.lastQuery', { result: roomLifecycleQueryFailed ? t('accountShare.lifecycle.queryFailed') : t('accountShare.lifecycle.querySuccess'), time: new Date(roomLifecycleLastQueryAt).toLocaleTimeString() }) }}
          <span v-if="roomLifecycleQueryFailed && roomLifecycleLastSuccessAt"> {{ t('accountShare.lifecycle.lastSuccessAt', { time: new Date(roomLifecycleLastSuccessAt).toLocaleTimeString() }) }}</span>
          <span v-if="roomLifecycleQueryFailed && roomLifecyclePolling"> {{ t('accountShare.lifecycle.willRetry') }}</span>
        </p>
        <p v-if="roomLifecycleQueryStopped" class="text-sm text-amber-700 dark:text-amber-300" role="status" data-testid="room-operation-query-stopped">
          {{ t('accountShare.lifecycle.pollStopped') }}
        </p>

        <template v-if="!roomLifecycleHasPendingOperation">
          <section v-if="roomLifecycleAction === null" class="space-y-3">
            <div>
              <span class="room-lifecycle-eyebrow">{{ t('accountShare.lifecycle.availableActions') }}</span>
              <p class="mt-1 text-sm leading-6 text-gray-500 dark:text-dark-300">
                {{ t('accountShare.lifecycle.delistHint') }}
              </p>
            </div>
            <div class="room-lifecycle-action-grid">
              <button
                v-if="roomLifecycleActionAllowed('drain')"
                type="button"
                class="room-lifecycle-action-card"
                data-testid="room-lifecycle-action-drain"
                @click="selectRoomLifecycleAction('drain')"
              >
                <Icon name="clock" size="sm" />
                <span>
                  <strong>{{ t('accountShare.lifecycle.delist') }}</strong>
                  <small>{{ t('accountShare.lifecycle.delistDesc') }}</small>
                </span>
              </button>
              <button
                v-if="roomLifecycleActionAllowed('activate')"
                type="button"
                class="room-lifecycle-action-card"
                data-testid="room-lifecycle-action-activate"
                @click="selectRoomLifecycleAction('activate')"
              >
                <Icon name="play" size="sm" />
                <span>
                  <strong>{{ t('accountShare.lifecycle.relist') }}</strong>
                  <small>{{ t('accountShare.lifecycle.relistDesc') }}</small>
                </span>
              </button>
              <button
                v-if="roomLifecycleActionAllowed('suspend')"
                type="button"
                class="room-lifecycle-action-card"
                data-testid="room-lifecycle-action-suspend"
                @click="selectRoomLifecycleAction('suspend')"
              >
                <Icon name="ban" size="sm" />
                <span>
                  <strong>{{ t('accountShare.lifecycle.emergencyStop') }}</strong>
                  <small>{{ t('accountShare.lifecycle.emergencyStopDesc') }}</small>
                </span>
              </button>
              <button
                type="button"
                class="room-lifecycle-action-card room-lifecycle-action-card-danger"
                data-testid="room-lifecycle-action-delete"
                @click="selectRoomLifecycleAction('delete')"
              >
                <Icon name="trash" size="sm" />
                <span>
                  <strong>{{ t('accountShare.lifecycle.deleteRoom') }}</strong>
                  <small>{{ t('accountShare.lifecycle.deleteRoomDesc') }}</small>
                </span>
              </button>
            </div>
            <p
              v-if="!roomLifecycleHasStateChangeAction"
              class="room-lifecycle-muted-note"
            >
              {{ t('accountShare.lifecycle.noActions') }}
            </p>
          </section>

          <section
            v-else-if="roomLifecycleAction !== 'delete'"
            class="room-lifecycle-confirm-panel"
            data-testid="room-lifecycle-confirm"
          >
            <span class="room-lifecycle-eyebrow">{{ t('accountShare.lifecycle.confirmAction') }}</span>
            <h4>{{ roomLifecycleActionTitle(roomLifecycleAction) }}</h4>
            <p>{{ roomLifecycleActionDescription(roomLifecycleAction) }}</p>
            <div class="room-lifecycle-alert room-lifecycle-alert-warning">
              <Icon name="infoCircle" size="sm" class="mt-0.5 flex-shrink-0" />
              <p>{{ roomLifecycleActionImpact(roomLifecycleAction) }}</p>
            </div>
            <label v-if="authStore.isAdmin" class="field">
              <span>{{ t('accountShare.lifecycle.adminReason') }}</span>
              <textarea
                v-model="roomLifecycleReason"
                class="input min-h-24"
                maxlength="500"
                :placeholder="t('accountShare.lifecycle.reasonPlaceholder')"
                data-testid="room-lifecycle-reason"
              ></textarea>
              <small>{{ t('accountShare.lifecycle.reasonNote') }}</small>
            </label>
          </section>

          <section
            v-else
            class="room-lifecycle-confirm-panel"
            data-testid="room-delete-confirm"
          >
            <span class="room-lifecycle-eyebrow">{{ t('accountShare.lifecycle.deleteCheck') }}</span>
            <h4>{{ t('accountShare.lifecycle.deleteRoom') }}</h4>
            <p>{{ t('accountShare.lifecycle.deleteCheckDesc') }}</p>

            <label v-if="authStore.isAdmin" class="field">
              <span>{{ t('accountShare.lifecycle.adminDeleteReason') }}</span>
              <textarea
                v-model="roomLifecycleReason"
                class="input min-h-24"
                maxlength="500"
                :placeholder="t('accountShare.lifecycle.deleteReasonPlaceholder')"
                data-testid="room-delete-reason"
              ></textarea>
              <small>{{ t('accountShare.lifecycle.deleteReasonNote') }}</small>
            </label>

            <button
              v-if="authStore.isAdmin && !roomDeleteIntent"
              type="button"
              class="btn btn-secondary min-h-11"
              :disabled="roomDeleteIntentLoading || !roomLifecycleReason.trim()"
              data-testid="room-delete-intent-submit"
              @click="loadRoomDeleteIntent"
            >
              <Icon name="search" size="sm" />
              {{ t('accountShare.lifecycle.checkDelete') }}
            </button>

            <div
              v-if="roomDeleteIntentLoading"
              class="room-lifecycle-state-message"
              data-testid="room-delete-intent-loading"
            >
              <Icon name="refresh" size="sm" class="animate-spin" />
              <span>{{ t('accountShare.lifecycle.checkingDelete') }}</span>
            </div>

            <template v-else-if="roomDeleteIntent">
              <div
                :class="[
                  'room-lifecycle-alert',
                  roomDeleteIntent.can_delete
                    ? 'room-lifecycle-alert-warning'
                    : 'room-lifecycle-alert-danger'
                ]"
                data-testid="room-delete-intent-result"
              >
                <Icon
                  :name="roomDeleteIntent.can_delete ? 'exclamationTriangle' : 'exclamationCircle'"
                  size="sm"
                  class="mt-0.5 flex-shrink-0"
                />
                <div>
                  <strong>{{ roomDeleteIntent.can_delete ? t('accountShare.lifecycle.deleteReady') : t('accountShare.lifecycle.deleteBlocked') }}</strong>
                  <p>{{ roomDeleteIntent.history_notice }}</p>
                </div>
              </div>

              <ul
                v-if="roomLifecycleBlockerItems.length > 0"
                class="room-lifecycle-blocker-list"
                data-testid="room-delete-blockers"
              >
                <li v-for="item in roomLifecycleBlockerItems" :key="item.key">
                  <span>{{ item.label }}</span>
                  <strong>{{ item.value }}</strong>
                </li>
              </ul>

              <label v-if="roomDeleteIntent.can_delete" class="field">
                <span>{{ t('accountShare.lifecycle.confirmNameLabel') }}</span>
                <input
                  v-model="roomDeleteNameConfirmation"
                  class="input min-h-11"
                  type="text"
                  autocomplete="off"
                  :placeholder="roomDeleteIntent.room_name"
                  data-testid="room-delete-name-input"
                />
                <small>{{ t('accountShare.lifecycle.confirmNameHint', { roomName: roomDeleteIntent.room_name, expiresAt: formatRoomDeleteIntentExpiry(roomDeleteIntent.expires_at) }) }}</small>
              </label>
            </template>
          </section>
        </template>
      </template>
    </div>

    <template #footer>
      <div class="room-lifecycle-footer">
        <button
          v-if="roomLifecycleAction !== null && !roomLifecycleHasPendingOperation && !roomLifecycleDeleted"
          type="button"
          class="btn btn-secondary min-h-11"
          :disabled="roomLifecycleCommandBusy"
          @click="resetRoomLifecycleAction"
        >
          {{ t('common.back') }}
        </button>
        <button
          v-else
          type="button"
          class="btn btn-secondary min-h-11"
          :disabled="roomLifecycleCommandBusy"
          @click="closeRoomLifecycleDialog"
        >
          {{ t('common.close') }}
        </button>
        <button
          v-if="roomLifecycleHasPendingOperation && !roomLifecycleDeleted"
          type="button"
          class="btn btn-secondary min-h-11"
          :disabled="roomLifecyclePolling"
          data-testid="room-operation-refresh"
          @click="pollRoomLifecycleOperationNow"
        >
          <Icon name="refresh" size="sm" :class="{ 'animate-spin': roomLifecyclePolling }" />
          {{ roomLifecyclePolling ? t('accountShare.lifecycle.polling') : t('accountShare.lifecycle.resumeQuery') }}
        </button>
        <button
          v-else-if="roomLifecycleAction === null && !roomLifecycleDeleted"
          type="button"
          class="btn btn-secondary min-h-11"
          :disabled="roomLifecycleLoading"
          @click="refreshRoomLifecycleState"
        >
          <Icon name="refresh" size="sm" :class="{ 'animate-spin': roomLifecycleLoading }" />
          {{ t('accountShare.lifecycle.refreshStatus') }}
        </button>
        <button
          v-else-if="roomLifecycleAction === 'delete' && roomDeleteIntent && (!roomDeleteIntent.can_delete || roomDeleteIntentExpired)"
          type="button"
          class="btn btn-secondary min-h-11"
          :disabled="roomLifecycleCommandBusy"
          @click="loadRoomDeleteIntent"
        >
          {{ roomDeleteIntentExpired ? t('accountShare.lifecycle.reconfirm') : t('accountShare.lifecycle.recheck') }}
        </button>
        <button
          v-else-if="roomLifecycleAction !== null && !roomLifecycleDeleted"
          type="button"
          :class="roomLifecycleAction === 'delete' ? 'btn btn-danger min-h-11' : 'btn btn-primary min-h-11'"
          :disabled="!canSubmitRoomLifecycleAction"
          data-testid="room-lifecycle-submit"
          @click="submitRoomLifecycleAction"
        >
          <Icon
            :name="roomLifecycleAction === 'delete' ? 'trash' : 'checkCircle'"
            size="sm"
            :class="{ 'animate-pulse': roomLifecycleSubmitting }"
          />
          {{ roomLifecycleSubmitting ? t('common.submitting') : roomLifecycleSubmitLabel }}
        </button>
      </div>
    </template>
  </BaseDialog>
</template>

<script setup lang="ts">
/**
 * 房间生命周期管理弹窗。
 *
 * 由 AccountShareView.vue 拆出：下架/上架/紧急停用/软删除四类操作、删除前置校验、
 * 异步操作轮询与幂等键管理全部内聚在此。父视图只负责决定「打开哪个房间」，
 * 并提供列表刷新回调——弹窗不直接触碰父组件的列表状态。
 *
 * 组件按需异步加载，未打开时其代码与状态都不会进入首屏。
 */
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import {
  accountShareAPI,
  type AccountShareListing,
  type AccountShareRoomBlockers,
  type AccountShareRoomDeleteIntent,
  type AccountShareRoomHealthState,
  type AccountShareRoomLifecycleAction,
  type AccountShareRoomLifecycleStatus,
  type AccountShareRoomManagementState,
  type AccountShareRoomOperation
} from '@/api/accountShare'
import { useAppStore } from '@/stores/app'
import { useAuthStore } from '@/stores/auth'
import { extractApiErrorCode, extractApiErrorMessage } from '@/utils/apiError'
import { accountShareOperationWaitReason, accountShareOperationWaitDuration } from '@/utils/accountShareOperation'
import {
  createSecureRequestID,
  isCanceledRequest,
  normalizeDateInput
} from '@/utils/requestSafety'
import BaseDialog from '@/components/common/BaseDialog.vue'
import Icon from '@/components/icons/Icon.vue'
import {
  ROOM_LIFECYCLE_ERROR_MESSAGES,
  ROOM_LIFECYCLE_TERMINAL_OPERATION_STATUSES
} from './roomLifecycleConstants'
import { useI18n } from 'vue-i18n'

const { t } = useI18n()

interface RoomLifecycleBlockerItem {
  key: keyof AccountShareRoomBlockers
  label: string
  value: string
}

const props = defineProps<{
  /** 当前打开的房间；null 表示弹窗关闭。 */
  listing: AccountShareListing | null
  /** 房间展示名，由父视图按归属/管理员权限解析后传入。 */
  displayName: string
  /** 父视图共享的时钟，用于判断删除确认令牌是否过期。 */
  nowMs: number
  /** 重新拉取房间列表；操作成功后需要等待其完成再读取最新房间。 */
  reloadListings: () => Promise<unknown>
  /** 重新拉取可用能力，软删除成功后调用。 */
  reloadCapabilities: () => Promise<unknown>
  /** 从父视图的本地缓存中移除已删除房间。 */
  removeKnownListing: (listingID: number) => void
  /** 按 id 在父视图最新列表中查找房间。 */
  findListing: (listingID: number) => AccountShareListing | undefined
}>()

const emit = defineEmits<{
  /** 关闭弹窗，或把刷新后的房间对象回传给父视图。 */
  (event: 'update:listing', listing: AccountShareListing | null): void
  /** 请求父视图把共享时钟推进到当前时间。 */
  (event: 'sync-now'): void
}>()

const appStore = useAppStore()
const authStore = useAuthStore()

const ROOM_LIFECYCLE_OPERATION_POLL_INTERVAL_MS = 1500
// 下架需要等待进行中的请求和结算，前端仅跟踪后端操作状态。
// 前端轮询 10 分钟后停止并提示手动刷新，避免对着永不推进的状态无限轮询。
const ROOM_LIFECYCLE_OPERATION_POLL_MAX_MS = 10 * 60 * 1000

const roomLifecycleState = ref<AccountShareRoomManagementState | null>(null)
const roomLifecycleAction = ref<AccountShareRoomLifecycleAction | null>(null)
const roomLifecycleOperation = ref<AccountShareRoomOperation | null>(null)
const roomDeleteIntent = ref<AccountShareRoomDeleteIntent | null>(null)
const roomDeleteNameConfirmation = ref('')
const roomLifecycleReason = ref('')
const roomLifecycleLoading = ref(false)
const roomDeleteIntentLoading = ref(false)
const roomLifecycleSubmitting = ref(false)
const roomLifecyclePolling = ref(false)
const roomLifecycleDeleted = ref(false)
const roomLifecycleError = ref('')
const roomLifecycleErrorCode = ref('')
const roomLifecycleLastQueryAt = ref<number | null>(null)
const roomLifecycleLastSuccessAt = ref<number | null>(null)
const roomLifecycleQueryFailed = ref(false)
const roomLifecycleQueryStopped = ref(false)

let roomLifecycleStateController: AbortController | null = null
let roomLifecycleOperationController: AbortController | null = null
let roomLifecycleStateRequestSeq = 0
let roomLifecycleOperationPollSeq = 0
let roomLifecycleOperationPollTimer: number | null = null
let roomLifecycleOperationPollStartedAt = 0
let roomLifecycleIdempotencySignature = ''
let roomLifecycleIdempotencyKey = ''

const roomLifecycleCommandBusy = computed(() =>
  roomLifecycleSubmitting.value || roomDeleteIntentLoading.value
)
const roomLifecycleOperationTerminal = computed(() => {
  const status = roomLifecycleOperation.value?.status
  return Boolean(status && ROOM_LIFECYCLE_TERMINAL_OPERATION_STATUSES.has(status))
})
const roomLifecycleHasPendingOperation = computed(() => {
  if (roomLifecycleOperation.value) return !roomLifecycleOperationTerminal.value
  return Boolean(roomLifecycleState.value?.pending_operation_id)
})
const roomLifecycleHasStateChangeAction = computed(() => {
  const allowedActions = roomLifecycleState.value?.allowed_actions ?? []
  return allowedActions.some(action => action === 'drain' || action === 'activate' || action === 'suspend')
})
const roomDeleteIntentExpired = computed(() => {
  const expiresAt = normalizeDateInput(roomDeleteIntent.value?.expires_at)
  return Boolean(expiresAt && expiresAt.getTime() <= props.nowMs)
})
const roomLifecycleBlockerItems = computed<RoomLifecycleBlockerItem[]>(() => {
  const blockers = roomDeleteIntent.value?.blockers
  if (!blockers) return []

  const items: RoomLifecycleBlockerItem[] = []
  const appendCount = (
    key: keyof AccountShareRoomBlockers,
    label: string,
    value: number
  ) => {
    if (value > 0) items.push({ key, label, value: String(value) })
  }
  appendCount('active_membership_count', t('accountShare.lifecycle.blockerActiveMembers'), blockers.active_membership_count)
  appendCount('ending_membership_count', t('accountShare.lifecycle.blockerEndingMembers'), blockers.ending_membership_count)
  appendCount('in_flight_request_count', t('accountShare.lifecycle.blockerInFlight'), blockers.in_flight_request_count)
  appendCount('pending_billing_intent_count', t('accountShare.lifecycle.blockerBillingIntents'), blockers.pending_billing_intent_count)
  appendCount(
    'synchronous_billing_pending_count',
    t('accountShare.lifecycle.blockerSyncBilling'),
    blockers.synchronous_billing_pending_count
  )
  if (blockers.conflicting_operation) {
    items.push({
      key: 'conflicting_operation',
      label: t('accountShare.lifecycle.blockerOtherOp'),
      value: blockers.conflicting_operation_id || t('accountShare.lifecycle.blockerRunning')
    })
  }
  if (blockers.runtime_dependency_unavailable) {
    items.push({
      key: 'runtime_dependency_unavailable',
      label: t('accountShare.lifecycle.blockerRuntime'),
      value: t('accountShare.lifecycle.blockerUnknown')
    })
  }
  return items
})
const canSubmitRoomLifecycleAction = computed(() => {
  const action = roomLifecycleAction.value
  const state = roomLifecycleState.value
  if (
    !action ||
    !state ||
    roomLifecycleCommandBusy.value ||
    roomLifecycleHasPendingOperation.value
  ) {
    return false
  }
  if (authStore.isAdmin && !roomLifecycleReason.value.trim()) return false
  if (action !== 'delete') return state.allowed_actions.includes(action)
  const intent = roomDeleteIntent.value
  return Boolean(
    intent?.can_delete &&
    intent.token &&
    !roomDeleteIntentExpired.value &&
    roomDeleteNameConfirmation.value === intent.room_name
  )
})
const roomLifecycleSubmitLabel = computed(() => {
  switch (roomLifecycleAction.value) {
    case 'drain':
      return t('accountShare.lifecycle.confirmDelist')
    case 'activate':
      return t('accountShare.lifecycle.confirmRelist')
    case 'suspend':
      return t('accountShare.lifecycle.confirmEmergency')
    case 'delete':
      return roomDeleteIntentExpired.value ? t('accountShare.lifecycle.confirmExpired') : t('accountShare.lifecycle.confirmDelete')
    default:
      return t('accountShare.lifecycle.confirmAction')
  }
})

function roomLifecycleStatusLabel(status: AccountShareRoomLifecycleStatus): string {
  switch (status) {
    case 'active':
      return t('accountShare.lifecycle.statusOpen')
    case 'paused':
      return t('accountShare.lifecycle.statusDelisted')
    case 'validating':
      return t('accountShare.lifecycle.statusRelisting')
    case 'draining':
      return t('accountShare.lifecycle.statusDelisting')
    case 'suspended':
      return t('accountShare.lifecycle.statusAdminPaused')
  }
}

function roomLifecycleStatusBadgeClass(status: AccountShareRoomLifecycleStatus): string {
  const base = 'rounded-full px-2.5 py-1 text-xs font-semibold'
  switch (status) {
    case 'active':
      return `${base} bg-emerald-50 text-emerald-700 dark:bg-emerald-500/10 dark:text-emerald-200`
    case 'validating':
    case 'draining':
      return `${base} bg-blue-50 text-blue-700 dark:bg-blue-500/10 dark:text-blue-200`
    case 'paused':
      return `${base} bg-amber-50 text-amber-700 dark:bg-amber-500/10 dark:text-amber-200`
    case 'suspended':
      return `${base} bg-gray-100 text-gray-600 dark:bg-dark-700 dark:text-dark-200`
  }
}

function roomLifecycleHealthLabel(healthState: AccountShareRoomHealthState): string {
  switch (healthState) {
    case 'healthy':
      return t('accountShare.lifecycle.healthOk')
    case 'degraded':
      return t('accountShare.lifecycle.healthPartial')
    case 'unavailable':
      return t('accountShare.lifecycle.healthDown')
  }
}

function roomLifecycleActionAllowed(action: AccountShareRoomLifecycleAction): boolean {
  return roomLifecycleState.value?.allowed_actions.includes(action) === true
}

function roomLifecycleActionTitle(action: Exclude<AccountShareRoomLifecycleAction, 'delete'>): string {
  switch (action) {
    case 'drain':
      return t('accountShare.lifecycle.delist')
    case 'activate':
      return t('accountShare.lifecycle.relist')
    case 'suspend':
      return t('accountShare.lifecycle.actionEmergencyStop')
  }
}

function roomLifecycleActionDescription(action: Exclude<AccountShareRoomLifecycleAction, 'delete'>): string {
  switch (action) {
    case 'drain':
      return t('accountShare.lifecycle.delistDetail')
    case 'activate':
      return t('accountShare.lifecycle.relistDetail')
    case 'suspend':
      return t('accountShare.lifecycle.emergencyDetail')
  }
}

function roomLifecycleActionImpact(action: Exclude<AccountShareRoomLifecycleAction, 'delete'>): string {
  switch (action) {
    case 'drain':
      return t('accountShare.lifecycle.delistConsequence')
    case 'activate':
      return t('accountShare.lifecycle.relistFailNote')
    case 'suspend':
      return t('accountShare.lifecycle.emergencyNote')
  }
}

function roomLifecycleOperationLabel(operation: AccountShareRoomOperation): string {
  const actionLabel = operation.action === 'delete_room' ? t('accountShare.lifecycle.deleteRoom') : t('accountShare.lifecycle.opDelistLabel')
  switch (operation.status) {
    case 'succeeded':
      return t('accountShare.lifecycle.opDone', { action: actionLabel })
    case 'failed':
      return t('accountShare.lifecycle.opFailed', { action: actionLabel })
    case 'cancelled':
      return t('accountShare.lifecycle.opCancelled', { action: actionLabel })
    case 'needs_attention':
      return t('accountShare.lifecycle.opBlocked', { action: actionLabel })
    case 'running':
      return t('accountShare.lifecycle.opRunning', { action: actionLabel })
    case 'pending':
      return t('accountShare.lifecycle.opPending', { action: actionLabel })
  }
}

function roomLifecycleOperationStatusDescription(operation: AccountShareRoomOperation): string {
  const reason = accountShareOperationWaitReason(operation)
  if (reason) return reason
  switch (operation.status) {
    case 'succeeded':
      return t('accountShare.lifecycle.opDoneDesc')
    case 'failed':
      return t('accountShare.lifecycle.opFailedDesc')
    case 'cancelled':
      return t('accountShare.lifecycle.opCancelledDesc')
    case 'needs_attention':
      return t('accountShare.lifecycle.opBlockedDesc')
    case 'running':
      return t('accountShare.lifecycle.opRunningDesc')
    case 'pending':
      return t('accountShare.lifecycle.opPendingDesc')
  }
}

function formatRoomDeleteIntentExpiry(value?: string): string {
  const expiresAt = normalizeDateInput(value)
  if (!expiresAt) return t('accountShare.lifecycle.tokenExpiredAt')
  return expiresAt.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit', second: '2-digit' })
}

function clearRoomLifecycleError(): void {
  roomLifecycleError.value = ''
  roomLifecycleErrorCode.value = ''
}

function setRoomLifecycleError(error: unknown, fallback: string): void {
  roomLifecycleErrorCode.value = extractApiErrorCode(error) || ''
  roomLifecycleError.value = extractApiErrorMessage(
    error,
    fallback,
    ROOM_LIFECYCLE_ERROR_MESSAGES
  )
}

function clearRoomLifecycleIdempotencyKey(): void {
  roomLifecycleIdempotencySignature = ''
  roomLifecycleIdempotencyKey = ''
}

function getRoomLifecycleIdempotencyKey(
  listingID: number,
  action: AccountShareRoomLifecycleAction,
  expectedVersion: number,
  token = ''
): string {
  const signature = JSON.stringify({ listingID, action, expectedVersion, token })
  if (
    roomLifecycleIdempotencyKey &&
    roomLifecycleIdempotencySignature === signature
  ) {
    return roomLifecycleIdempotencyKey
  }
  roomLifecycleIdempotencySignature = signature
  roomLifecycleIdempotencyKey = `account-share-room-${listingID}-${action}-${createSecureRequestID()}`
  return roomLifecycleIdempotencyKey
}

function stopRoomLifecycleOperationPolling(): void {
  roomLifecycleOperationPollSeq += 1
  if (roomLifecycleOperationPollTimer !== null) {
    window.clearTimeout(roomLifecycleOperationPollTimer)
    roomLifecycleOperationPollTimer = null
  }
  roomLifecycleOperationController?.abort()
  roomLifecycleOperationController = null
  roomLifecyclePolling.value = false
}

function resetRoomLifecycleAction(): void {
  if (roomLifecycleCommandBusy.value || roomLifecycleHasPendingOperation.value) return
  roomLifecycleAction.value = null
  roomDeleteIntent.value = null
  roomDeleteNameConfirmation.value = ''
  roomLifecycleReason.value = ''
  clearRoomLifecycleIdempotencyKey()
  clearRoomLifecycleError()
}

/**
 * 打开新房间时重置全部会话状态。
 * 对应拆分前 AccountShareView 的 openRoomLifecycleDialog（权限判断留在父视图）。
 */
function beginRoomLifecycleSession(): void {
  stopRoomLifecycleOperationPolling()
  roomLifecycleStateController?.abort()
  roomLifecycleStateController = null
  roomLifecycleState.value = null
  roomLifecycleOperation.value = null
  roomLifecycleAction.value = null
  roomDeleteIntent.value = null
  roomDeleteNameConfirmation.value = ''
  roomLifecycleReason.value = ''
  roomLifecycleDeleted.value = false
  roomLifecycleLastQueryAt.value = null
  roomLifecycleLastSuccessAt.value = null
  roomLifecycleQueryFailed.value = false
  roomLifecycleQueryStopped.value = false
  roomLifecycleLoading.value = false
  roomDeleteIntentLoading.value = false
  roomLifecycleSubmitting.value = false
  clearRoomLifecycleIdempotencyKey()
  clearRoomLifecycleError()
  void refreshRoomLifecycleState()
}

function closeRoomLifecycleDialog(): void {
  if (roomLifecycleCommandBusy.value) return
  roomLifecycleStateRequestSeq += 1
  roomLifecycleStateController?.abort()
  roomLifecycleStateController = null
  stopRoomLifecycleOperationPolling()
  roomLifecycleState.value = null
  roomLifecycleOperation.value = null
  roomLifecycleAction.value = null
  roomDeleteIntent.value = null
  roomDeleteNameConfirmation.value = ''
  roomLifecycleReason.value = ''
  roomLifecycleDeleted.value = false
  roomLifecycleLoading.value = false
  clearRoomLifecycleIdempotencyKey()
  clearRoomLifecycleError()
  emit('update:listing', null)
}

async function refreshRoomLifecycleState(): Promise<void> {
  const listing = props.listing
  if (!listing) return

  stopRoomLifecycleOperationPolling()
  roomLifecycleStateController?.abort()
  const controller = new AbortController()
  roomLifecycleStateController = controller
  const requestSeq = ++roomLifecycleStateRequestSeq
  roomLifecycleLoading.value = true
  roomLifecycleAction.value = null
  roomLifecycleOperation.value = null
  roomDeleteIntent.value = null
  roomDeleteNameConfirmation.value = ''
  roomLifecycleReason.value = ''
  clearRoomLifecycleIdempotencyKey()
  clearRoomLifecycleError()
  try {
    const state = await accountShareAPI.getRoomManagementState(listing.id, {
      signal: controller.signal
    })
    if (
      requestSeq !== roomLifecycleStateRequestSeq ||
      props.listing?.id !== listing.id
    ) {
      return
    }
    roomLifecycleState.value = state
    if (state.pending_operation_id) {
      startRoomLifecycleOperationPolling(state.pending_operation_id)
    }
  } catch (error: unknown) {
    if (
      requestSeq !== roomLifecycleStateRequestSeq ||
      isCanceledRequest(error)
    ) {
      return
    }
    setRoomLifecycleError(error, t('accountShare.lifecycle.loadFailed'))
  } finally {
    if (requestSeq === roomLifecycleStateRequestSeq) {
      roomLifecycleLoading.value = false
      if (roomLifecycleStateController === controller) {
        roomLifecycleStateController = null
      }
    }
  }
}

function selectRoomLifecycleAction(action: AccountShareRoomLifecycleAction): void {
  if (
    roomLifecycleCommandBusy.value ||
    roomLifecycleHasPendingOperation.value ||
    (action !== 'delete' && !roomLifecycleActionAllowed(action))
  ) {
    return
  }
  roomLifecycleAction.value = action
  roomDeleteIntent.value = null
  roomDeleteNameConfirmation.value = ''
  roomLifecycleReason.value = ''
  clearRoomLifecycleIdempotencyKey()
  clearRoomLifecycleError()
  if (action === 'delete' && !authStore.isAdmin) {
    void loadRoomDeleteIntent()
  }
}

async function loadRoomDeleteIntent(): Promise<void> {
  const listing = props.listing
  const state = roomLifecycleState.value
  if (!listing || !state || roomLifecycleAction.value !== 'delete') return
  if (roomDeleteIntentLoading.value || roomLifecycleSubmitting.value) return
  const reason = roomLifecycleReason.value.trim()
  if (authStore.isAdmin && !reason) {
    roomLifecycleErrorCode.value = 'ACCOUNT_SHARE_ROOM_REASON_REQUIRED'
    roomLifecycleError.value = t('accountShare.lifecycle.deleteReasonRequired')
    return
  }

  roomDeleteIntentLoading.value = true
  roomDeleteIntent.value = null
  roomDeleteNameConfirmation.value = ''
  clearRoomLifecycleIdempotencyKey()
  clearRoomLifecycleError()
  try {
    const intent = await accountShareAPI.createRoomDeleteIntent(listing.id, {
      expected_version: state.row_version,
      ...(authStore.isAdmin ? { reason } : {})
    })
    if (
      props.listing?.id !== listing.id ||
      roomLifecycleAction.value !== 'delete' ||
      roomLifecycleState.value?.row_version !== state.row_version
    ) {
      return
    }
    roomDeleteIntent.value = intent
  } catch (error: unknown) {
    if (!props.listing) return
    setRoomLifecycleError(error, t('accountShare.lifecycle.checkDeleteFailed'))
  } finally {
    roomDeleteIntentLoading.value = false
  }
}

async function submitRoomLifecycleAction(): Promise<void> {
  const listing = props.listing
  const state = roomLifecycleState.value
  const action = roomLifecycleAction.value
  if (
    !listing ||
    !state ||
    !action ||
    roomLifecycleSubmitting.value
  ) {
    return
  }
  if (action === 'delete') {
    const expiresAt = normalizeDateInput(roomDeleteIntent.value?.expires_at)
    if (expiresAt && expiresAt.getTime() <= Date.now()) {
      emit('sync-now')
      roomLifecycleErrorCode.value = 'ACCOUNT_SHARE_ROOM_DELETION_TOKEN_INVALID'
      roomLifecycleError.value = t('accountShare.lifecycle.confirmTokenExpired')
      return
    }
  }
  if (!canSubmitRoomLifecycleAction.value) return

  roomLifecycleSubmitting.value = true
  clearRoomLifecycleError()
  try {
    if (action === 'delete') {
      const intent = roomDeleteIntent.value
      if (!intent?.token) return
      const operation = await accountShareAPI.deleteRoom(
        listing.id,
        {
          expected_version: intent.row_version,
          room_name: roomDeleteNameConfirmation.value,
          token: intent.token,
          confirmed: true,
          ...(authStore.isAdmin ? { reason: roomLifecycleReason.value.trim() } : {})
        },
        getRoomLifecycleIdempotencyKey(
          listing.id,
          action,
          intent.row_version,
          intent.token
        )
      )
      if (props.listing?.id !== listing.id) return
      roomLifecycleOperation.value = operation
      clearRoomLifecycleIdempotencyKey()
      if (ROOM_LIFECYCLE_TERMINAL_OPERATION_STATUSES.has(operation.status)) {
        await handleRoomLifecycleTerminalOperation(operation)
      } else {
        startRoomLifecycleOperationPolling(operation.id)
        appStore.showSuccess(t('accountShare.lifecycle.deleteAccepted'))
      }
      return
    }

    const payload = {
      expected_version: state.row_version,
      confirmed: true,
      ...(authStore.isAdmin ? { reason: roomLifecycleReason.value.trim() } : {})
    }
    const idempotencyKey = getRoomLifecycleIdempotencyKey(
      listing.id,
      action,
      state.row_version
    )
    const updatedState = action === 'drain'
      ? await accountShareAPI.drainRoom(listing.id, payload, idempotencyKey)
      : action === 'activate'
        ? await accountShareAPI.activateRoom(listing.id, payload, idempotencyKey)
        : await accountShareAPI.suspendRoom(listing.id, payload, idempotencyKey)

    if (props.listing?.id !== listing.id) return
    roomLifecycleState.value = updatedState
    roomLifecycleAction.value = null
    clearRoomLifecycleIdempotencyKey()
    await props.reloadListings()
    const refreshedListing = props.findListing(listing.id)
    if (refreshedListing) emit('update:listing', refreshedListing)
    if (updatedState.pending_operation_id) {
      startRoomLifecycleOperationPolling(updatedState.pending_operation_id)
      appStore.showSuccess(t('accountShare.lifecycle.delisting'))
    } else {
      appStore.showSuccess(
        action === 'activate'
          ? t('accountShare.lifecycle.relisted')
          : action === 'drain'
            ? t('accountShare.lifecycle.delistedAll')
            : t('accountShare.lifecycle.emergencyStopped')
      )
    }
  } catch (error: unknown) {
    if (!props.listing) return
    setRoomLifecycleError(error, t('accountShare.lifecycle.actionFailed'))
  } finally {
    roomLifecycleSubmitting.value = false
  }
}

function startRoomLifecycleOperationPolling(operationID: string): void {
  const normalizedOperationID = operationID.trim()
  if (!normalizedOperationID || !props.listing) return
  stopRoomLifecycleOperationPolling()
  const pollSeq = roomLifecycleOperationPollSeq
  roomLifecycleOperationPollStartedAt = Date.now()
  roomLifecycleQueryStopped.value = false
  roomLifecyclePolling.value = true
  void pollRoomLifecycleOperation(normalizedOperationID, pollSeq)
}

function pollRoomLifecycleOperationNow(): void {
  if (roomLifecyclePolling.value) return
  const operationID = roomLifecycleOperation.value?.id ||
    roomLifecycleState.value?.pending_operation_id ||
    ''
  if (!operationID) {
    void refreshRoomLifecycleState()
    return
  }
  clearRoomLifecycleError()
  startRoomLifecycleOperationPolling(operationID)
}

function scheduleRoomLifecycleOperationPoll(operationID: string, pollSeq: number): void {
  if (Date.now() - roomLifecycleOperationPollStartedAt >= ROOM_LIFECYCLE_OPERATION_POLL_MAX_MS) {
    roomLifecyclePolling.value = false
    roomLifecycleQueryStopped.value = true
    return
  }
  roomLifecyclePolling.value = true
  roomLifecycleOperationPollTimer = window.setTimeout(() => {
    roomLifecycleOperationPollTimer = null
    void pollRoomLifecycleOperation(operationID, pollSeq)
  }, ROOM_LIFECYCLE_OPERATION_POLL_INTERVAL_MS)
}

async function pollRoomLifecycleOperation(
  operationID: string,
  pollSeq: number
): Promise<void> {
  if (
    pollSeq !== roomLifecycleOperationPollSeq ||
    !props.listing
  ) {
    return
  }

  roomLifecycleOperationController?.abort()
  const controller = new AbortController()
  roomLifecycleOperationController = controller
  try {
    const operation = await accountShareAPI.getRoomOperation(operationID, {
      signal: controller.signal
    })
    if (
      pollSeq !== roomLifecycleOperationPollSeq ||
      !props.listing
    ) {
      return
    }
    roomLifecycleOperation.value = operation
    roomLifecycleLastQueryAt.value = Date.now()
    roomLifecycleLastSuccessAt.value = Date.now()
    roomLifecycleQueryFailed.value = false
    clearRoomLifecycleError()
    if (ROOM_LIFECYCLE_TERMINAL_OPERATION_STATUSES.has(operation.status)) {
      roomLifecyclePolling.value = false
      roomLifecycleOperationController = null
      await handleRoomLifecycleTerminalOperation(operation)
      return
    }
    scheduleRoomLifecycleOperationPoll(operationID, pollSeq)
  } catch (error: unknown) {
    if (
      pollSeq !== roomLifecycleOperationPollSeq ||
      isCanceledRequest(error)
    ) {
      return
    }
    roomLifecycleLastQueryAt.value = Date.now()
    roomLifecycleQueryFailed.value = true
    setRoomLifecycleError(error, t('accountShare.lifecycle.pollFailed'))
    scheduleRoomLifecycleOperationPoll(operationID, pollSeq)
  } finally {
    if (roomLifecycleOperationController === controller) {
      roomLifecycleOperationController = null
    }
  }
}

async function handleRoomLifecycleTerminalOperation(
  operation: AccountShareRoomOperation
): Promise<void> {
  if (operation.status !== 'succeeded') {
    roomLifecycleErrorCode.value = operation.error_code || operation.status
    roomLifecycleError.value = operation.error_message ||
      (operation.status === 'cancelled'
        ? t('accountShare.lifecycle.opCancelledNotice')
        : t('accountShare.lifecycle.opFailedNotice'))
    return
  }

  clearRoomLifecycleError()
  if (operation.action === 'delete_room') {
    props.removeKnownListing(operation.listing_id)
    roomLifecycleDeleted.value = true
    roomLifecycleAction.value = null
    roomDeleteIntent.value = null
    roomDeleteNameConfirmation.value = ''
    roomLifecycleReason.value = ''
    await Promise.all([props.reloadListings(), props.reloadCapabilities()])
    appStore.showSuccess(t('accountShare.lifecycle.deletedNotice'))
    return
  }

  appStore.showSuccess(t('accountShare.lifecycle.delistDone'))
  await Promise.all([props.reloadListings(), refreshRoomLifecycleState()])
  const refreshedListing = props.findListing(operation.listing_id)
  if (refreshedListing) emit('update:listing', refreshedListing)
}

// 父视图切换房间即开启新会话；关闭（listing 变为 null）时只需停掉在途请求。
watch(
  () => props.listing?.id ?? null,
  (listingID, previousListingID) => {
    if (listingID === null) {
      roomLifecycleStateRequestSeq += 1
      roomLifecycleStateController?.abort()
      roomLifecycleStateController = null
      stopRoomLifecycleOperationPolling()
      return
    }
    if (listingID !== previousListingID) beginRoomLifecycleSession()
  },
  { immediate: true }
)

onBeforeUnmount(() => {
  roomLifecycleStateRequestSeq += 1
  roomLifecycleStateController?.abort()
  roomLifecycleStateController = null
  stopRoomLifecycleOperationPolling()
})
</script>

<style scoped>
@import './dialogPrimitives.css';

.room-lifecycle-dialog {
  display: grid;
  min-width: 0;
  gap: 1rem;
}

.room-lifecycle-state-message,
.room-lifecycle-alert,
.room-lifecycle-operation {
  display: flex;
  min-width: 0;
  gap: 0.75rem;
  border: 1px solid rgb(226 232 240);
  border-radius: 0.75rem;
  padding: 0.875rem;
  font-size: 0.875rem;
  line-height: 1.5;
}

.room-lifecycle-state-message {
  align-items: center;
  color: rgb(71 85 105);
  background: rgb(248 250 252);
}

.room-lifecycle-alert strong,
.room-lifecycle-operation strong {
  display: block;
  color: rgb(15 23 42);
}

.room-lifecycle-alert p,
.room-lifecycle-operation p {
  margin-top: 0.25rem;
}

.room-lifecycle-alert code,
.room-lifecycle-operation code {
  display: block;
  margin-top: 0.375rem;
  overflow-wrap: anywhere;
  color: currentColor;
  font-size: 0.75rem;
}

.room-lifecycle-alert-danger {
  border-color: rgb(254 202 202);
  color: rgb(185 28 28);
  background: rgb(254 242 242);
}

.room-lifecycle-alert-warning {
  border-color: rgb(253 230 138);
  color: rgb(146 64 14);
  background: rgb(255 251 235);
}

.room-lifecycle-alert-success {
  border-color: rgb(167 243 208);
  color: rgb(4 120 87);
  background: rgb(236 253 245);
}

.room-lifecycle-overview,
.room-lifecycle-confirm-panel {
  min-width: 0;
  border: 1px solid rgb(226 232 240);
  border-radius: 0.875rem;
  padding: 1rem;
  background: rgb(255 255 255);
}

.room-lifecycle-overview-head {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 0.75rem;
}

.room-lifecycle-eyebrow {
  display: block;
  color: rgb(100 116 139);
  font-size: 0.75rem;
  font-weight: 700;
  letter-spacing: 0.06em;
  text-transform: uppercase;
}

.room-lifecycle-version {
  flex-shrink: 0;
  border-radius: 9999px;
  padding: 0.3rem 0.625rem;
  color: rgb(71 85 105);
  background: rgb(241 245 249);
  font-size: 0.75rem;
  font-weight: 600;
}

.room-lifecycle-status-reason {
  margin-top: 0.75rem;
  color: rgb(71 85 105);
  font-size: 0.875rem;
  line-height: 1.5;
}

.room-lifecycle-metrics {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 0.625rem;
  margin-top: 1rem;
}

.room-lifecycle-metrics > div {
  min-width: 0;
  border-radius: 0.625rem;
  padding: 0.75rem;
  background: rgb(248 250 252);
}

.room-lifecycle-metrics span,
.room-lifecycle-metrics strong {
  display: block;
}

.room-lifecycle-metrics span {
  color: rgb(100 116 139);
  font-size: 0.75rem;
}

.room-lifecycle-metrics strong {
  margin-top: 0.25rem;
  color: rgb(15 23 42);
  font-size: 1rem;
}

.room-lifecycle-operation {
  color: rgb(30 64 175);
  background: rgb(239 246 255);
  border-color: rgb(191 219 254);
}

.room-lifecycle-action-grid {
  display: grid;
  grid-template-columns: minmax(0, 1fr);
  gap: 0.625rem;
}

.room-lifecycle-action-card {
  display: flex;
  min-height: 3.25rem;
  min-width: 0;
  align-items: flex-start;
  gap: 0.75rem;
  border: 1px solid rgb(226 232 240);
  border-radius: 0.75rem;
  padding: 0.875rem;
  color: rgb(51 65 85);
  background: rgb(255 255 255);
  text-align: left;
  transition: border-color 160ms ease, background-color 160ms ease, transform 160ms ease;
}

.room-lifecycle-action-card:hover {
  border-color: rgb(129 140 248);
  background: rgb(248 250 252);
  transform: translateY(-1px);
}

.room-lifecycle-action-card:focus-visible {
  outline: 2px solid rgb(99 102 241 / 0.55);
  outline-offset: 2px;
}

.room-lifecycle-action-card > span {
  min-width: 0;
}

.room-lifecycle-action-card strong,
.room-lifecycle-action-card small {
  display: block;
}

.room-lifecycle-action-card strong {
  color: rgb(15 23 42);
  font-size: 0.875rem;
}

.room-lifecycle-action-card small {
  margin-top: 0.2rem;
  color: rgb(100 116 139);
  font-size: 0.75rem;
  line-height: 1.45;
}

.room-lifecycle-action-card-danger {
  border-color: rgb(254 202 202);
  color: rgb(220 38 38);
}

.room-lifecycle-action-card-danger:hover {
  border-color: rgb(248 113 113);
  background: rgb(254 242 242);
}

.room-lifecycle-confirm-panel h4 {
  margin-top: 0.375rem;
  color: rgb(15 23 42);
  font-size: 1rem;
  font-weight: 700;
}

.room-lifecycle-confirm-panel > p {
  margin-top: 0.5rem;
  color: rgb(71 85 105);
  font-size: 0.875rem;
  line-height: 1.6;
}

.room-lifecycle-confirm-panel > .room-lifecycle-alert,
.room-lifecycle-confirm-panel > .field,
.room-lifecycle-confirm-panel > .room-lifecycle-state-message {
  margin-top: 1rem;
}

.room-lifecycle-blocker-list {
  display: grid;
  gap: 0.5rem;
  margin-top: 0.875rem;
}

.room-lifecycle-blocker-list li {
  display: flex;
  min-width: 0;
  align-items: center;
  justify-content: space-between;
  gap: 0.75rem;
  border-radius: 0.625rem;
  padding: 0.625rem 0.75rem;
  color: rgb(71 85 105);
  background: rgb(248 250 252);
  font-size: 0.875rem;
}

.room-lifecycle-blocker-list strong {
  overflow-wrap: anywhere;
  color: rgb(185 28 28);
  text-align: right;
}

.room-lifecycle-muted-note {
  color: rgb(100 116 139);
  font-size: 0.875rem;
  line-height: 1.5;
}

.room-lifecycle-footer {
  display: grid;
  width: 100%;
  grid-template-columns: minmax(0, 1fr);
  gap: 0.625rem;
}

.room-lifecycle-footer > button {
  width: 100%;
  min-width: 0;
  white-space: nowrap;
}

.dark .room-lifecycle-state-message,
.dark .room-lifecycle-overview,
.dark .room-lifecycle-confirm-panel,
.dark .room-lifecycle-action-card {
  border-color: rgb(51 65 85);
  background: rgb(15 23 42);
}

.dark .room-lifecycle-state-message,
.dark .room-lifecycle-status-reason,
.dark .room-lifecycle-confirm-panel > p,
.dark .room-lifecycle-action-card,
.dark .room-lifecycle-action-card small,
.dark .room-lifecycle-muted-note {
  color: rgb(148 163 184);
}

.dark .room-lifecycle-alert strong,
.dark .room-lifecycle-operation strong,
.dark .room-lifecycle-metrics strong,
.dark .room-lifecycle-action-card strong,
.dark .room-lifecycle-confirm-panel h4 {
  color: rgb(248 250 252);
}

.dark .room-lifecycle-version,
.dark .room-lifecycle-metrics > div,
.dark .room-lifecycle-blocker-list li {
  color: rgb(148 163 184);
  background: rgb(30 41 59);
}

.dark .room-lifecycle-alert-danger {
  border-color: rgb(127 29 29);
  color: rgb(254 202 202);
  background: rgb(127 29 29 / 0.25);
}

.dark .room-lifecycle-alert-warning {
  border-color: rgb(120 53 15);
  color: rgb(253 230 138);
  background: rgb(120 53 15 / 0.24);
}

.dark .room-lifecycle-alert-success {
  border-color: rgb(6 78 59);
  color: rgb(167 243 208);
  background: rgb(6 78 59 / 0.28);
}

.dark .room-lifecycle-operation {
  border-color: rgb(30 64 175);
  color: rgb(191 219 254);
  background: rgb(30 58 138 / 0.25);
}

.dark .room-lifecycle-action-card:hover {
  border-color: rgb(99 102 241);
  background: rgb(30 41 59);
}

.dark .room-lifecycle-action-card-danger {
  border-color: rgb(127 29 29);
  color: rgb(248 113 113);
}

.dark .room-lifecycle-action-card-danger:hover {
  border-color: rgb(239 68 68);
  background: rgb(127 29 29 / 0.22);
}
</style>
