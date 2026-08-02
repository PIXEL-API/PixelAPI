<template>
  <BaseDialog
    :show="listing !== null"
    :title="listing ? `${displayName} · 房间管理` : '房间管理'"
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
        <span>正在读取房间的最新状态...</span>
      </div>

      <div
        v-if="roomLifecycleError"
        class="room-lifecycle-alert room-lifecycle-alert-danger"
        role="alert"
        data-testid="room-lifecycle-error"
      >
        <Icon name="exclamationCircle" size="sm" class="mt-0.5 flex-shrink-0" />
        <div class="min-w-0">
          <strong>操作没有完成</strong>
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
          <strong>房间已软删除</strong>
          <p>房间不会再出现在可用列表中，历史消费、结算和评价记录仍会保留。</p>
        </div>
      </div>

      <template v-else-if="roomLifecycleState">
        <section class="room-lifecycle-overview">
          <div class="room-lifecycle-overview-head">
            <div>
              <span class="room-lifecycle-eyebrow">当前状态</span>
              <div class="mt-1 flex flex-wrap items-center gap-2">
                <strong class="text-base text-gray-950 dark:text-white">
                  {{ roomLifecycleStatusLabel(roomLifecycleState.lifecycle_status) }}
                </strong>
                <span :class="roomLifecycleStatusBadgeClass(roomLifecycleState.lifecycle_status)">
                  {{ roomLifecycleHealthLabel(roomLifecycleState.health_state) }}
                </span>
              </div>
            </div>
            <span class="room-lifecycle-version">版本 {{ roomLifecycleState.row_version }}</span>
          </div>
          <p v-if="roomLifecycleState.status_reason" class="room-lifecycle-status-reason">
            {{ roomLifecycleState.status_reason }}
          </p>
          <div class="room-lifecycle-metrics">
            <div>
              <span>消费者席位</span>
              <strong>{{ roomLifecycleState.active_seats }}/{{ roomLifecycleState.seat_limit }}</strong>
            </div>
            <div>
              <span>排队成员</span>
              <strong>{{ roomLifecycleState.queued_membership_count }}</strong>
            </div>
            <div>
              <span>房间账号</span>
              <strong>{{ roomLifecycleState.room_account_count }}</strong>
            </div>
            <div>
              <span>进行中请求</span>
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
            </div>
          </div>
        </section>

        <template v-if="!roomLifecycleHasPendingOperation">
          <section v-if="roomLifecycleAction === null" class="space-y-3">
            <div>
              <span class="room-lifecycle-eyebrow">可用操作</span>
              <p class="mt-1 text-sm leading-6 text-gray-500 dark:text-dark-300">
                下架后停止新增用户，现有消费者与预约保持不变；需要恢复招募时可重新上架。
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
                  <strong>下架房间</strong>
                  <small>停止新增，已有用户继续使用</small>
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
                  <strong>重新上架</strong>
                  <small>完成账号连通性校验后重新开放</small>
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
                  <strong>紧急停用</strong>
                  <small>仅管理员用于异常处置</small>
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
                  <strong>删除房间</strong>
                  <small>先检查使用、结算与运行时阻塞项</small>
                </span>
              </button>
            </div>
            <p
              v-if="!roomLifecycleHasStateChangeAction"
              class="room-lifecycle-muted-note"
            >
              当前没有可执行的状态变更；你仍可检查删除条件，或刷新状态。
            </p>
          </section>

          <section
            v-else-if="roomLifecycleAction !== 'delete'"
            class="room-lifecycle-confirm-panel"
            data-testid="room-lifecycle-confirm"
          >
            <span class="room-lifecycle-eyebrow">确认操作</span>
            <h4>{{ roomLifecycleActionTitle(roomLifecycleAction) }}</h4>
            <p>{{ roomLifecycleActionDescription(roomLifecycleAction) }}</p>
            <div class="room-lifecycle-alert room-lifecycle-alert-warning">
              <Icon name="infoCircle" size="sm" class="mt-0.5 flex-shrink-0" />
              <p>{{ roomLifecycleActionImpact(roomLifecycleAction) }}</p>
            </div>
            <label v-if="authStore.isAdmin" class="field">
              <span>管理员操作原因</span>
              <textarea
                v-model="roomLifecycleReason"
                class="input min-h-24"
                maxlength="500"
                placeholder="请说明本次生命周期变更原因"
                data-testid="room-lifecycle-reason"
              ></textarea>
              <small>原因会写入房间事件与审计记录。</small>
            </label>
          </section>

          <section
            v-else
            class="room-lifecycle-confirm-panel"
            data-testid="room-delete-confirm"
          >
            <span class="room-lifecycle-eyebrow">删除校验</span>
            <h4>软删除房间</h4>
            <p>系统会先检查使用中成员、请求、结算、编辑会话和其他房间操作，全部清零后才签发两分钟有效的确认令牌。</p>

            <label v-if="authStore.isAdmin" class="field">
              <span>管理员删除原因</span>
              <textarea
                v-model="roomLifecycleReason"
                class="input min-h-24"
                maxlength="500"
                placeholder="请说明为什么需要删除此房间"
                data-testid="room-delete-reason"
              ></textarea>
              <small>必须填写原因后才能检查删除条件，原因会写入审计记录。</small>
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
              检查删除条件
            </button>

            <div
              v-if="roomDeleteIntentLoading"
              class="room-lifecycle-state-message"
              data-testid="room-delete-intent-loading"
            >
              <Icon name="refresh" size="sm" class="animate-spin" />
              <span>正在检查删除条件...</span>
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
                  <strong>{{ roomDeleteIntent.can_delete ? '删除条件已满足' : '暂时不能删除' }}</strong>
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
                <span>输入房间名确认</span>
                <input
                  v-model="roomDeleteNameConfirmation"
                  class="input min-h-11"
                  type="text"
                  autocomplete="off"
                  :placeholder="roomDeleteIntent.room_name"
                  data-testid="room-delete-name-input"
                />
                <small>请完整输入“{{ roomDeleteIntent.room_name }}”。确认令牌将在 {{ formatRoomDeleteIntentExpiry(roomDeleteIntent.expires_at) }} 失效。</small>
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
          返回
        </button>
        <button
          v-else
          type="button"
          class="btn btn-secondary min-h-11"
          :disabled="roomLifecycleCommandBusy"
          @click="closeRoomLifecycleDialog"
        >
          关闭
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
          {{ roomLifecyclePolling ? '自动查询中' : '继续查询' }}
        </button>
        <button
          v-else-if="roomLifecycleAction === null && !roomLifecycleDeleted"
          type="button"
          class="btn btn-secondary min-h-11"
          :disabled="roomLifecycleLoading"
          @click="refreshRoomLifecycleState"
        >
          <Icon name="refresh" size="sm" :class="{ 'animate-spin': roomLifecycleLoading }" />
          刷新状态
        </button>
        <button
          v-else-if="roomLifecycleAction === 'delete' && roomDeleteIntent && (!roomDeleteIntent.can_delete || roomDeleteIntentExpired)"
          type="button"
          class="btn btn-secondary min-h-11"
          :disabled="roomLifecycleCommandBusy"
          @click="loadRoomDeleteIntent"
        >
          {{ roomDeleteIntentExpired ? '重新获取确认' : '重新检查' }}
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
          {{ roomLifecycleSubmitting ? '提交中...' : roomLifecycleSubmitLabel }}
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
// 排空清退在下架请求内同步完成，仅剩进行中请求需要等待；后端另有 30 分钟强制收口。
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
  appendCount('active_membership_count', '正在使用的成员', blockers.active_membership_count)
  appendCount('queued_membership_count', '排队中的成员', blockers.queued_membership_count)
  appendCount('ending_membership_count', '正在退出或结算的成员', blockers.ending_membership_count)
  appendCount('in_flight_request_count', '进行中的请求', blockers.in_flight_request_count)
  appendCount('pending_billing_intent_count', '待处理计费意图', blockers.pending_billing_intent_count)
  appendCount(
    'synchronous_billing_pending_count',
    '同步结算任务',
    blockers.synchronous_billing_pending_count
  )
  if (blockers.valid_edit_session) {
    items.push({ key: 'valid_edit_session', label: '房间编辑会话', value: '仍在占用' })
  }
  if (blockers.conflicting_operation) {
    items.push({
      key: 'conflicting_operation',
      label: '其他生命周期操作',
      value: blockers.conflicting_operation_id || '正在执行'
    })
  }
  if (blockers.runtime_dependency_unavailable) {
    items.push({
      key: 'runtime_dependency_unavailable',
      label: '运行时状态',
      value: '暂时无法确认'
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
      return '确认下架'
    case 'activate':
      return '确认重新上架'
    case 'suspend':
      return '确认紧急停用'
    case 'delete':
      return roomDeleteIntentExpired.value ? '确认已过期' : '确认软删除'
    default:
      return '确认操作'
  }
})

function roomLifecycleStatusLabel(status: AccountShareRoomLifecycleStatus): string {
  switch (status) {
    case 'active':
      return '开放使用'
    case 'paused':
      return '已暂停'
    case 'validating':
      return '恢复校验中'
    case 'draining':
      return '安全排空中'
    case 'suspended':
      return '管理员暂停'
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
      return '健康'
    case 'degraded':
      return '部分可用'
    case 'unavailable':
      return '不可用'
  }
}

function roomLifecycleActionAllowed(action: AccountShareRoomLifecycleAction): boolean {
  return roomLifecycleState.value?.allowed_actions.includes(action) === true
}

function roomLifecycleActionTitle(action: Exclude<AccountShareRoomLifecycleAction, 'delete'>): string {
  switch (action) {
    case 'drain':
      return '下架房间'
    case 'activate':
      return '重新上架'
    case 'suspend':
      return '紧急停用房间'
  }
}

function roomLifecycleActionDescription(action: Exclude<AccountShareRoomLifecycleAction, 'delete'>): string {
  switch (action) {
    case 'drain':
      return '房间将立即停止接收新成员，并清退全部现有成员：预约成员直接释放，使用中的成员按已用时长结算并退还未用预付款。等待进行中的请求结束后房间自动转为“已暂停”，随时可重新上架。'
    case 'activate':
      return '系统会校验房间主账号的连通性和可用状态；只有校验通过才会重新开放。'
    case 'suspend':
      return '管理员将因异常立即停用房间，恢复前不会再分配给消费用户。'
  }
}

function roomLifecycleActionImpact(action: Exclude<AccountShareRoomLifecycleAction, 'delete'>): string {
  switch (action) {
    case 'drain':
      return '下架会立即清退全部成员并完成结算退款，通常在几分钟内自动转为“已暂停”；不会删除房间或历史记录。'
    case 'activate':
      return '恢复校验失败时房间仍保持暂停，并展示失败原因，不会带病开放。'
    case 'suspend':
      return '紧急停用不会删除房间或历史记录，操作原因会被审计。'
  }
}

function roomLifecycleOperationLabel(operation: AccountShareRoomOperation): string {
  const actionLabel = operation.action === 'delete_room' ? '软删除房间' : '房间排空'
  switch (operation.status) {
    case 'succeeded':
      return `${actionLabel}已完成`
    case 'failed':
      return `${actionLabel}失败`
    case 'cancelled':
      return `${actionLabel}已取消`
    case 'needs_attention':
      return `${actionLabel}需要处理阻塞项`
    case 'running':
      return `${actionLabel}执行中`
    case 'pending':
      return `${actionLabel}等待执行`
  }
}

function roomLifecycleOperationStatusDescription(operation: AccountShareRoomOperation): string {
  if (operation.error_message) return operation.error_message
  switch (operation.status) {
    case 'succeeded':
      return '服务端已完成全部状态与历史快照写入。'
    case 'failed':
      return '操作未完成，请根据错误信息处理后重新打开房间状态。'
    case 'cancelled':
      return '操作已经取消，房间未按本次请求继续变更。'
    case 'needs_attention':
      return '仍有运行时或结算阻塞项，系统会继续等待。'
    case 'running':
      return '正在等待请求、成员与结算安全收口。'
    case 'pending':
      return '操作已受理，正在等待后台处理。'
  }
}

function formatRoomDeleteIntentExpiry(value?: string): string {
  const expiresAt = normalizeDateInput(value)
  if (!expiresAt) return '令牌过期时'
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
    setRoomLifecycleError(error, '读取房间生命周期状态失败，请稍后重试。')
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
    roomLifecycleError.value = '管理员必须填写删除原因后再检查删除条件。'
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
    setRoomLifecycleError(error, '检查房间删除条件失败，请稍后重试。')
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
      roomLifecycleError.value = '删除确认已经过期，请重新获取确认后再提交。'
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
        appStore.showSuccess('软删除请求已受理，正在安全收口房间数据')
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
      appStore.showSuccess('房间正在排空收口')
    } else {
      appStore.showSuccess(
        action === 'activate'
          ? '房间已重新上架'
          : action === 'drain'
            ? '房间已下架并清退全部成员'
            : '房间已紧急停用'
      )
    }
  } catch (error: unknown) {
    if (!props.listing) return
    setRoomLifecycleError(error, '房间生命周期操作失败，请稍后重试。')
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
    if (ROOM_LIFECYCLE_TERMINAL_OPERATION_STATUSES.has(operation.status)) {
      roomLifecyclePolling.value = false
      roomLifecycleOperationController = null
      await handleRoomLifecycleTerminalOperation(operation)
      return
    }
    if (Date.now() - roomLifecycleOperationPollStartedAt > ROOM_LIFECYCLE_OPERATION_POLL_MAX_MS) {
      roomLifecyclePolling.value = false
      roomLifecycleOperationController = null
      appStore.showWarning('排空仍在进行，已停止自动查询；可点击“立即查询”手动刷新状态。')
      return
    }
    roomLifecycleOperationPollTimer = window.setTimeout(() => {
      roomLifecycleOperationPollTimer = null
      void pollRoomLifecycleOperation(operationID, pollSeq)
    }, ROOM_LIFECYCLE_OPERATION_POLL_INTERVAL_MS)
  } catch (error: unknown) {
    if (
      pollSeq !== roomLifecycleOperationPollSeq ||
      isCanceledRequest(error)
    ) {
      return
    }
    roomLifecyclePolling.value = false
    setRoomLifecycleError(error, '查询房间操作进度失败；你可以点击“继续查询”重试。')
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
        ? '房间操作已取消，当前房间没有按本次请求继续变更。'
        : '房间操作执行失败，请处理错误后重新打开房间状态。')
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
    appStore.showSuccess('房间已软删除，历史消费、结算和评价记录继续保留')
    return
  }

  appStore.showSuccess('房间已完成排空并暂停')
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
