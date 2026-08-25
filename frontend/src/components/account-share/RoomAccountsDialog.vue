<template>
  <BaseDialog
    :show="show"
    :title="t('accountShare.roomAccounts.title', { name: roomDisplayName })"
    width="wide"
    :close-disabled="operating"
    @close="requestClose"
  >
    <div class="space-y-4">
      <div
        v-if="listing"
        class="grid grid-cols-1 gap-3 rounded-2xl border border-gray-200 bg-gray-50 p-4 dark:border-dark-600 dark:bg-dark-800 sm:grid-cols-3"
      >
        <div class="room-summary-cell">
          <span>{{ t('accountShare.roomAccounts.platform') }}</span>
          <strong>{{ listing.platform }}</strong>
        </div>
        <div class="room-summary-cell">
          <span>{{ t('accountShare.roomAccounts.level') }}</span>
          <strong>{{ listing.account_level || 'unknown' }}</strong>
        </div>
        <div class="room-summary-cell">
          <span>{{ t('accountShare.roomAccounts.health') }}</span>
          <strong>{{ healthyCount }}/{{ accounts.length }}</strong>
        </div>
      </div>

      <div
        class="grid grid-cols-2 gap-2 rounded-xl bg-gray-100 p-1 dark:bg-dark-800"
        role="tablist"
        :aria-label="t('accountShare.roomAccounts.tabsLabel')"
      >
        <button
          type="button"
          class="room-tab"
          :class="{ 'room-tab-active': activeTab === 'members' }"
          role="tab"
          :aria-selected="activeTab === 'members'"
          :disabled="operating"
          data-testid="room-accounts-members-tab"
          @click="activeTab = 'members'"
        >
          {{ t('accountShare.roomAccounts.membersTab', { count: accounts.length }) }}
        </button>
        <button
          type="button"
          class="room-tab"
          :class="{ 'room-tab-active': activeTab === 'add' }"
          role="tab"
          :aria-selected="activeTab === 'add'"
          :disabled="operating"
          data-testid="room-accounts-add-tab"
          @click="activeTab = 'add'"
        >
          {{ t('accountShare.roomAccounts.addTab', { count: addableCandidateCount }) }}
        </button>
      </div>

      <div
        v-if="operationSummary"
        class="rounded-2xl border p-4 text-sm"
        :class="operationSummaryClass"
        role="status"
        data-testid="room-accounts-operation-summary"
      >
        <p class="font-medium">{{ operationSummary.text }}</p>
        <ul v-if="operationFailures.length > 0" class="mt-2 space-y-1 text-xs">
          <li v-for="failure in operationFailures" :key="failure.accountID">
            {{ failure.name }} (#{{ failure.accountID }})：{{ failure.error }}
          </li>
        </ul>
      </div>

      <section v-if="activeTab === 'members'" class="space-y-3" role="tabpanel">
        <div
          class="rounded-2xl border border-blue-200 bg-blue-50 p-4 text-sm text-blue-800 dark:border-blue-900/70 dark:bg-blue-950/25 dark:text-blue-200"
        >
          {{ t('accountShare.roomAccounts.removeHint') }}
        </div>

        <div class="flex flex-col gap-2 sm:flex-row sm:items-center sm:justify-between">
          <label class="min-w-0 flex-1">
            <span class="sr-only">{{ t('accountShare.roomAccounts.searchMembers') }}</span>
            <input
              v-model.trim="memberSearch"
              type="search"
              class="input min-h-11 w-full"
              :placeholder="t('accountShare.roomAccounts.searchMembers')"
              :disabled="operating"
            />
          </label>
          <button
            type="button"
            class="btn btn-secondary min-h-11"
            :disabled="filteredMemberAccounts.length === 0 || operating"
            data-testid="select-all-room-members"
            @click="toggleAllMembers"
          >
            {{ allVisibleMembersSelected
              ? t('accountShare.roomAccounts.clearSelection')
              : t('accountShare.roomAccounts.selectAll') }}
          </button>
        </div>

        <div
          v-if="loadingAccounts"
          class="flex min-h-32 items-center justify-center gap-2 rounded-2xl border border-gray-200 text-sm text-gray-500 dark:border-dark-600 dark:text-dark-300"
        >
          <Icon name="refresh" size="sm" class="animate-spin" />
          {{ t('common.loading') }}
        </div>

        <div
          v-else-if="accountsErrorMessage"
          class="rounded-2xl border border-red-200 bg-red-50 p-4 text-sm text-red-700 dark:border-red-900/70 dark:bg-red-950/25 dark:text-red-300"
          role="alert"
        >
          {{ accountsErrorMessage }}
        </div>

        <div
          v-else-if="filteredMemberAccounts.length === 0"
          class="rounded-2xl border border-dashed border-gray-300 p-8 text-center text-sm text-gray-500 dark:border-dark-600 dark:text-dark-300"
        >
          {{ memberSearch
            ? t('accountShare.roomAccounts.noSearchResults')
            : t('accountShare.roomAccounts.empty') }}
        </div>

        <div v-else class="grid grid-cols-1 gap-3 md:grid-cols-2">
          <label
            v-for="account in filteredMemberAccounts"
            :key="account.account_id"
            class="room-account-card cursor-pointer"
            :class="{ 'room-account-card-selected': selectedMemberIDs.has(account.account_id) }"
          >
            <span class="room-checkbox-control">
              <input
                type="checkbox"
                class="h-5 w-5 rounded border-gray-300 text-primary-600 focus:ring-primary-500"
                :checked="selectedMemberIDs.has(account.account_id)"
                :disabled="operating"
                :aria-label="t('accountShare.roomAccounts.selectAccount', { name: account.account_name })"
                @change="toggleMember(account.account_id)"
              />
            </span>
            <span class="min-w-0 flex-1">
              <span class="flex min-w-0 items-start justify-between gap-3">
                <span class="min-w-0">
                  <strong class="block truncate text-sm text-gray-900 dark:text-white">
                    {{ account.account_name }}
                  </strong>
                  <span class="mt-1 block text-xs text-gray-500 dark:text-dark-300">
                    #{{ account.account_id }} · {{ account.platform }} · {{ account.account_level }}
                  </span>
                </span>
                <span :class="roomAccountHealthBadgeClass(account)">
                  {{ roomAccountHealthLabel(account) }}
                </span>
              </span>
              <span class="mt-4 grid grid-cols-2 gap-3 text-sm">
                <span>
                  <span class="block text-xs text-gray-500 dark:text-dark-300">
                    {{ t('accountShare.roomAccounts.concurrency') }}
                  </span>
                  <strong class="mt-1 block text-gray-900 dark:text-white">
                    {{ account.current_concurrency }}
                  </strong>
                </span>
                <span>
                  <span class="block text-xs text-gray-500 dark:text-dark-300">
                    {{ t('accountShare.roomAccounts.priority') }}
                  </span>
                  <strong class="mt-1 block text-gray-900 dark:text-white">
                    {{ account.priority }}
                  </strong>
                </span>
              </span>
              <span
                v-if="account.last_used_at"
                class="mt-3 block text-xs text-gray-500 dark:text-dark-300"
              >
                {{ t('accountShare.roomAccounts.lastUsed', { time: formatDateTime(account.last_used_at) }) }}
              </span>
            </span>
          </label>
        </div>

        <div class="flex justify-end">
          <button
            type="button"
            class="btn min-h-11 bg-red-600 text-white hover:bg-red-700 disabled:cursor-not-allowed disabled:opacity-50"
            :disabled="selectedMemberIDs.size === 0 || operating || loadingAccounts"
            data-testid="remove-selected-room-accounts"
            @click="openRemoveConfirmation"
          >
            <Icon
              :name="operating ? 'refresh' : 'trash'"
              size="sm"
              class="mr-2"
              :class="{ 'animate-spin': operating }"
            />
            {{ t('accountShare.roomAccounts.removeSelected', { count: selectedMemberIDs.size }) }}
          </button>
        </div>
      </section>

      <section v-else class="space-y-3" role="tabpanel">
        <div
          class="rounded-2xl border border-emerald-200 bg-emerald-50 p-4 text-sm text-emerald-800 dark:border-emerald-900/70 dark:bg-emerald-950/25 dark:text-emerald-200"
        >
          {{ t('accountShare.roomAccounts.addHint', {
            platform: listing?.platform || '',
            level: listing?.account_level || 'unknown'
          }) }}
        </div>

        <div class="create-compatible-account-card">
          <div class="min-w-0">
            <strong>{{ t('accountShare.roomAccounts.createCompatibleAccount') }}</strong>
            <small>
              {{ canCreateCompatibleAccount
                ? t('accountShare.roomAccounts.createCompatibleAccountHint')
                : t('accountShare.roomAccounts.createCompatibleAccountUnavailable') }}
            </small>
          </div>
          <button
            type="button"
            class="btn btn-primary min-h-11"
            :disabled="operating || !canCreateCompatibleAccount"
            data-testid="create-compatible-room-account"
            @click="openCreateAccountFlow"
          >
            <Icon name="plus" size="sm" class="mr-2" />
            {{ t('userAccounts.createAccount') }}
          </button>
        </div>

        <div class="flex flex-col gap-2 sm:flex-row sm:items-center sm:justify-between">
          <label class="min-w-0 flex-1">
            <span class="sr-only">{{ t('accountShare.roomAccounts.searchCandidates') }}</span>
            <input
              v-model.trim="candidateSearch"
              type="search"
              class="input min-h-11 w-full"
              :placeholder="t('accountShare.roomAccounts.searchCandidates')"
              :disabled="operating"
            />
          </label>
          <button
            type="button"
            class="btn btn-secondary min-h-11"
            :disabled="visibleEligibleCandidates.length === 0 || operating"
            data-testid="select-all-room-candidates"
            @click="toggleAllCandidates"
          >
            {{ allVisibleCandidatesSelected
              ? t('accountShare.roomAccounts.clearSelection')
              : t('accountShare.roomAccounts.selectAllCompatible') }}
          </button>
        </div>

        <div
          v-if="loadingCandidates"
          class="flex min-h-32 items-center justify-center gap-2 rounded-2xl border border-gray-200 text-sm text-gray-500 dark:border-dark-600 dark:text-dark-300"
        >
          <Icon name="refresh" size="sm" class="animate-spin" />
          {{ t('common.loading') }}
        </div>

        <div
          v-else-if="candidatesErrorMessage"
          class="rounded-2xl border border-red-200 bg-red-50 p-4 text-sm text-red-700 dark:border-red-900/70 dark:bg-red-950/25 dark:text-red-300"
          role="alert"
        >
          {{ candidatesErrorMessage }}
        </div>

        <div
          v-else-if="filteredCandidateAccounts.length === 0"
          class="rounded-2xl border border-dashed border-gray-300 p-8 text-center text-sm text-gray-500 dark:border-dark-600 dark:text-dark-300"
        >
          {{ candidateSearch
            ? t('accountShare.roomAccounts.noSearchResults')
            : t('accountShare.roomAccounts.noCandidates') }}
        </div>

        <div v-else class="grid grid-cols-1 gap-3 md:grid-cols-2">
          <label
            v-for="account in filteredCandidateAccounts"
            :key="account.id"
            class="room-account-card"
            :class="{
              'cursor-pointer': isCandidateEligible(account),
              'cursor-not-allowed opacity-70': !isCandidateEligible(account),
              'room-account-card-selected': selectedCandidateIDs.has(account.id)
            }"
          >
            <span class="room-checkbox-control">
              <input
                type="checkbox"
                class="h-5 w-5 rounded border-gray-300 text-primary-600 focus:ring-primary-500"
                :checked="selectedCandidateIDs.has(account.id)"
                :disabled="operating || !isCandidateEligible(account)"
                :aria-label="t('accountShare.roomAccounts.selectAccount', { name: account.name })"
                @change="toggleCandidate(account)"
              />
            </span>
            <span class="min-w-0 flex-1">
              <span class="flex min-w-0 items-start justify-between gap-3">
                <span class="min-w-0">
                  <strong class="block truncate text-sm text-gray-900 dark:text-white">
                    {{ account.name }}
                  </strong>
                  <span class="mt-1 flex flex-wrap items-center gap-1.5 text-xs text-gray-500 dark:text-dark-300">
                    <span>#{{ account.id }}</span>
                    <span>· {{ account.platform }}</span>
                    <span class="account-level-badge">{{ account.account_level }}</span>
                  </span>
                </span>
                <span :class="candidateHealthBadgeClass(account)">
                  {{ candidateHealthLabel(account) }}
                </span>
              </span>
              <span class="mt-3 block text-xs text-gray-600 dark:text-dark-200">
                {{ t('accountShare.roomAccounts.currentMode') }}：
                <strong>{{ candidateModeLabel(account) }}</strong>
              </span>
              <span
                v-if="candidateDisabledReason(account)"
                class="mt-2 block rounded-lg bg-amber-50 px-3 py-2 text-xs text-amber-800 dark:bg-amber-950/30 dark:text-amber-200"
              >
                {{ candidateDisabledReason(account) }}
              </span>
            </span>
          </label>
        </div>

        <div class="flex justify-end">
          <button
            type="button"
            class="btn btn-primary min-h-11"
            :disabled="selectedCandidateIDs.size === 0 || operating || loadingCandidates"
            data-testid="add-selected-room-accounts"
            @click="submitBatchOperation('add')"
          >
            <Icon
              :name="operating ? 'refresh' : 'plus'"
              size="sm"
              class="mr-2"
              :class="{ 'animate-spin': operating }"
            />
            {{ t('accountShare.roomAccounts.addSelected', { count: selectedCandidateIDs.size }) }}
          </button>
        </div>
      </section>
    </div>

    <template #footer>
      <div class="flex w-full flex-col-reverse gap-3 sm:flex-row sm:justify-end">
        <button
          type="button"
          class="btn btn-secondary min-h-11"
          :disabled="operating"
          data-testid="close-room-accounts-dialog"
          @click="requestClose"
        >
          {{ t('common.close') }}
        </button>
        <button
          type="button"
          class="btn btn-secondary min-h-11"
          :disabled="refreshing || operating"
          @click="refreshAll"
        >
          <Icon name="refresh" size="sm" class="mr-2" :class="{ 'animate-spin': refreshing }" />
          {{ t('common.refresh') }}
        </button>
      </div>
    </template>
  </BaseDialog>

  <BaseDialog
    :show="pendingRemoveAccountIDs.length > 0"
    title="确认移出房间账号"
    width="narrow"
    :z-index="65"
    :close-disabled="operating"
    :close-on-escape="!operating"
    :close-on-click-outside="false"
    @close="cancelRemoveConfirmation"
  >
    <div class="room-account-remove-confirmation" data-testid="room-account-remove-confirmation">
      <div class="remove-impact-summary">
        <div>
          <span>本次移出</span>
          <strong>{{ pendingRemoveAccountIDs.length }}</strong>
        </div>
        <div>
          <span>当前账号</span>
          <strong>{{ accounts.length }}</strong>
        </div>
        <div>
          <span>预计剩余</span>
          <strong>{{ remainingAccountCountAfterRemove }}</strong>
        </div>
      </div>

      <div
        class="remove-impact-notice"
        :class="{ 'remove-impact-notice-critical': removingLastRoomAccounts }"
        role="alert"
      >
        <Icon
          :name="removingLastRoomAccounts ? 'exclamationCircle' : 'infoCircle'"
          size="sm"
        />
        <div>
          <strong>
            {{ removingLastRoomAccounts
              ? '这会移出房间的最后一个账号'
              : '成员请求可能需要切换到剩余账号' }}
          </strong>
          <p>
            {{ removingLastRoomAccounts
              ? '操作成功后，房间将没有可调度账号，消费者请求无法继续；请随后暂停房间或立即补充兼容账号。'
              : '若所选账号正在承载成员，服务端会在同一操作中处理重新绑定或必要结算；提交结果以服务端最新状态为准。' }}
          </p>
        </div>
      </div>

      <div class="remove-impact-runtime">
        <span>当前占用/收口席位</span>
        <strong>{{ Number(listing?.active_seats || 0) }}</strong>
        <small>当前接口未返回预约与正在退出的拆分人数，因此这里不做估算。</small>
      </div>

      <div class="remove-account-list">
        <span>将移出的账号</span>
        <ul>
          <li v-for="account in pendingRemoveAccounts" :key="account.account_id">
            <strong>{{ account.account_name }}</strong>
            <small>#{{ account.account_id }}</small>
          </li>
        </ul>
      </div>
    </div>

    <template #footer>
      <div class="remove-confirmation-footer">
        <button
          type="button"
          class="btn btn-secondary min-h-11"
          :disabled="operating"
          data-testid="cancel-remove-room-accounts"
          @click="cancelRemoveConfirmation"
        >
          返回检查
        </button>
        <button
          type="button"
          class="btn min-h-11 bg-red-600 text-white hover:bg-red-700 disabled:cursor-not-allowed disabled:opacity-50"
          :disabled="operating"
          data-testid="confirm-remove-room-accounts"
          @click="confirmRemoveAccounts"
        >
          <Icon
            :name="operating ? 'refresh' : 'trash'"
            size="sm"
            class="mr-2"
            :class="{ 'animate-spin': operating }"
          />
          {{ operating ? '正在安全移出…' : `确认移出 ${pendingRemoveAccountIDs.length} 个账号` }}
        </button>
      </div>
    </template>
  </BaseDialog>

  <CreateRoomAccountFlow
    :show="showCreateAccountFlow"
    :listing="listing"
    :proxies="proxies"
    @close="closeCreateAccountFlow"
    @completed="handleCreatedAccountAttached"
  />
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import BaseDialog from '@/components/common/BaseDialog.vue'
import Icon from '@/components/icons/Icon.vue'
import {
  accountShareAPI,
  loadAllPaginatedItems,
  type AccountShareListing,
  type AccountShareRoomAccount,
  type AccountShareRoomAccountsBatchResult,
  type AccountShareRoomAccountsBatchResponse
} from '@/api/accountShare'
import { accountsAPI } from '@/api/accounts'
import type { Account, Proxy } from '@/types'
import { resolveAccountExternalPlacementTarget } from '@/components/account-share/externalPlacement'
import { extractApiErrorMessage, extractApiErrorCode, extractApiErrorMetadata } from '@/utils/apiError'
import { formatDateTime } from '@/utils/format'
import CreateRoomAccountFlow from '@/components/account-share/CreateRoomAccountFlow.vue'

type RoomAccountsTab = 'members' | 'add'
type RoomAccountOperation = 'add' | 'remove'

const ROOM_ACCOUNT_ERROR_MESSAGES: Record<string, string> = {
  ACCOUNT_SHARE_ROOM_LIMIT_EXCEEDED: '未删除房间数量已达到配额上限',
  ACCOUNT_SHARE_ROOM_CREATE_RATE_EXCEEDED: '最近 24 小时创建房间次数已达到配额上限',
  ACCOUNT_SHARE_ROOM_ACCOUNT_LIMIT_EXCEEDED: '该房间的账号数量已达到上限，请先移出不再使用的账号',
  ACCOUNT_SHARE_OWNER_ROOM_ACCOUNT_LIMIT_EXCEEDED: '房主管理的房间账号总数已达到上限，请先整理其他房间',
  ACCOUNT_SHARE_ROOM_OWNER_MISMATCH: '该账号不属于当前房主，不能加入此房间',
  ACCOUNT_SHARE_ROOM_PLATFORM_MISMATCH: '该账号与房间平台不一致',
  ACCOUNT_SHARE_ROOM_LEVEL_MISMATCH: '该账号等级与房间要求不一致',
  ACCOUNT_SHARE_ROOM_UNKNOWN_LEVEL: '该账号等级尚未识别，请先完成账号检测',
  ACCOUNT_SHARE_ROOM_MODE_REQUIRED: '该账号尚未处于可加入房间的账号模式',
  ACCOUNT_SHARE_ACCOUNT_UNAVAILABLE: '该账号当前不可用，请处理账号状态后重试',
  ACCOUNT_SHARE_MODE_UNSUPPORTED_MODEL: '该账号不支持房间要求的全部模型，请先在"我的账号"中补齐该账号的模型白名单',
  OWNED_ACCOUNT_PLACEMENT_CONVERSION_REQUIRED: '该账号处于共享投放中，需先切换账号模式后再修改',
  ACCOUNT_SHARE_ROOM_ACCOUNT_CONFLICT: '该账号已加入其他房间或正在切换归属',
  ACCOUNT_SHARE_LISTING_NOT_FOUND: '房间不存在、已删除或当前无权管理',
  ACCOUNT_SHARE_ROOM_DELETED: '房间已经删除，不能再调整房间账号',
  IDEMPOTENCY_KEY_REQUIRED: '请求缺少安全幂等标识，请刷新页面后重试'
}

const ROOM_ACCOUNT_UNAVAILABLE_BLOCKER_MESSAGES: Record<string, string> = {
  status_not_active: '账号状态不是正常状态，请先恢复账号',
  scheduling_disabled: '账号调度已停用，请先开启账号调度',
  non_positive_concurrency: '账号并发数必须大于 0',
  expired: '账号凭据已过期，请先更新账号凭据',
  overloaded: '账号正处于过载保护期，请稍后重试',
  rate_limited: '账号正处于速率限制期，请等待限制解除后重试',
  temporarily_unschedulable: '账号当前被临时暂停调度，请稍后重试',
  codex_quota_protected: '账号正处于 Codex 额度保护期，请等待保护解除后重试',
  anthropic_quota_protected: '账号正处于 Anthropic 额度保护期，请等待保护解除后重试',
  opencode_quota_protected: '账号正处于 Opencode 额度保护期，请等待保护解除后重试'
}

interface OperationSummary {
  tone: 'success' | 'warning' | 'error'
  text: string
}

interface OperationFailure {
  accountID: number
  name: string
  error: string
}

interface RoomAccountsChangedEvent {
  operation: RoomAccountOperation
  success: number
  failed: number
}

const props = withDefaults(defineProps<{
  show: boolean
  listing: AccountShareListing | null
  proxies?: Proxy[]
}>(), {
  proxies: () => []
})

const emit = defineEmits<{
  (event: 'close'): void
  (event: 'changed', payload: RoomAccountsChangedEvent): void
}>()

const { t } = useI18n()
const activeTab = ref<RoomAccountsTab>('members')
const accounts = ref<AccountShareRoomAccount[]>([])
const candidates = ref<Account[]>([])
const loadingAccounts = ref(false)
const loadingCandidates = ref(false)
const operating = ref(false)
const accountsErrorMessage = ref('')
const candidatesErrorMessage = ref('')
const operationSummary = ref<OperationSummary | null>(null)
const operationFailures = ref<OperationFailure[]>([])
const selectedMemberIDs = ref(new Set<number>())
const selectedCandidateIDs = ref(new Set<number>())
const memberSearch = ref('')
const candidateSearch = ref('')
const showCreateAccountFlow = ref(false)
const pendingRemoveAccountIDs = ref<number[]>([])
let accountsRequestVersion = 0
let candidatesRequestVersion = 0
let candidatesRequestController: AbortController | null = null
let pendingOperationSignature = ''
let pendingOperationIdempotencyKey = ''

const roomDisplayName = computed(() => (
  props.listing?.room_name
  || props.listing?.account_name
  || (props.listing ? `#${props.listing.id}` : '')
))

const healthyCount = computed(() => (
  accounts.value.filter((account) => isRoomAccountHealthy(account)).length
))

const currentAccountIDs = computed(() => (
  new Set(accounts.value.map((account) => account.account_id))
))

const candidateAccounts = computed(() => {
  return candidates.value.filter((account) => !currentAccountIDs.value.has(account.id))
})

const normalizedMemberSearch = computed(() => memberSearch.value.trim().toLocaleLowerCase())
const normalizedCandidateSearch = computed(() => candidateSearch.value.trim().toLocaleLowerCase())

const filteredMemberAccounts = computed(() => {
  const search = normalizedMemberSearch.value
  if (!search) return accounts.value
  return accounts.value.filter((account) => (
    account.account_name.toLocaleLowerCase().includes(search)
    || String(account.account_id).includes(search)
  ))
})

const filteredCandidateAccounts = computed(() => {
  const search = normalizedCandidateSearch.value
  if (!search) return candidateAccounts.value
  return candidateAccounts.value.filter((account) => (
    account.name.toLocaleLowerCase().includes(search)
    || String(account.id).includes(search)
  ))
})

const visibleEligibleCandidates = computed(() => (
  filteredCandidateAccounts.value.filter((account) => isCandidateEligible(account))
))

const addableCandidateCount = computed(() => (
  candidateAccounts.value.filter((account) => isCandidateEligible(account)).length
))

const canCreateCompatibleAccount = computed(() => {
  const platform = normalizeComparableValue(props.listing?.platform)
  return (platform === 'openai' || platform === 'anthropic' || platform === 'opencode')
    && (platform === 'opencode' || isKnownLevel(props.listing?.account_level))
})

const allVisibleMembersSelected = computed(() => (
  filteredMemberAccounts.value.length > 0
  && filteredMemberAccounts.value.every((account) => selectedMemberIDs.value.has(account.account_id))
))

const allVisibleCandidatesSelected = computed(() => (
  visibleEligibleCandidates.value.length > 0
  && visibleEligibleCandidates.value.every((account) => selectedCandidateIDs.value.has(account.id))
))

const refreshing = computed(() => loadingAccounts.value || loadingCandidates.value)

const pendingRemoveAccounts = computed(() => {
  const selected = new Set(pendingRemoveAccountIDs.value)
  return accounts.value.filter(account => selected.has(account.account_id))
})

const remainingAccountCountAfterRemove = computed(() => (
  Math.max(0, accounts.value.length - pendingRemoveAccounts.value.length)
))

const removingLastRoomAccounts = computed(() => (
  pendingRemoveAccounts.value.length > 0
  && remainingAccountCountAfterRemove.value === 0
))

const operationSummaryClass = computed(() => {
  if (operationSummary.value?.tone === 'success') {
    return 'border-emerald-200 bg-emerald-50 text-emerald-800 dark:border-emerald-900/70 dark:bg-emerald-950/25 dark:text-emerald-200'
  }
  if (operationSummary.value?.tone === 'warning') {
    return 'border-amber-200 bg-amber-50 text-amber-800 dark:border-amber-900/70 dark:bg-amber-950/25 dark:text-amber-200'
  }
  return 'border-red-200 bg-red-50 text-red-800 dark:border-red-900/70 dark:bg-red-950/25 dark:text-red-200'
})

watch(
  () => [props.show, props.listing?.id] as const,
  ([show]) => {
    accountsRequestVersion += 1
    candidatesRequestVersion += 1
    candidatesRequestController?.abort()
    candidatesRequestController = null
    loadingAccounts.value = false
    loadingCandidates.value = false
    operating.value = false
    accounts.value = []
    candidates.value = []
    accountsErrorMessage.value = ''
    candidatesErrorMessage.value = ''
    operationSummary.value = null
    operationFailures.value = []
    selectedMemberIDs.value = new Set()
    selectedCandidateIDs.value = new Set()
    memberSearch.value = ''
    candidateSearch.value = ''
    showCreateAccountFlow.value = false
    pendingRemoveAccountIDs.value = []
    activeTab.value = 'members'
    pendingOperationSignature = ''
    pendingOperationIdempotencyKey = ''
    if (show && props.listing) void refreshAll()
  },
  { immediate: true }
)

function normalizeComparableValue(value: unknown): string {
  return typeof value === 'string' ? value.trim().toLocaleLowerCase() : ''
}

function isKnownLevel(value: unknown): boolean {
  const level = normalizeComparableValue(value)
  return Boolean(level && level !== 'unknown')
}

function isOpencodePlatform(value: unknown): boolean {
  return normalizeComparableValue(value) === 'opencode'
}

function isRoomAccountHealthy(account: AccountShareRoomAccount): boolean {
  return account.status === 'active'
    && account.schedulable
    && Number(account.current_concurrency) > 0
    && account.placement_state === 'active'
}

function roomAccountHealthLabel(account: AccountShareRoomAccount): string {
  return isRoomAccountHealthy(account)
    ? t('accountShare.roomAccounts.healthy')
    : t('accountShare.roomAccounts.unavailable')
}

function badgeClass(healthy: boolean): string {
  const base = 'shrink-0 rounded-full px-2.5 py-1 text-xs font-semibold'
  return healthy
    ? `${base} bg-emerald-100 text-emerald-700 dark:bg-emerald-900/35 dark:text-emerald-300`
    : `${base} bg-amber-100 text-amber-700 dark:bg-amber-900/35 dark:text-amber-300`
}

function roomAccountHealthBadgeClass(account: AccountShareRoomAccount): string {
  return badgeClass(isRoomAccountHealthy(account))
}

function isCandidateHealthy(account: Account): boolean {
  return candidateAvailabilityDisabledReason(account) === ''
}

function candidateHealthLabel(account: Account): string {
  return isCandidateHealthy(account)
    ? t('accountShare.roomAccounts.healthy')
    : t('accountShare.roomAccounts.unavailable')
}

function candidateHealthBadgeClass(account: Account): string {
  return badgeClass(isCandidateHealthy(account))
}

function candidateAvailabilityDisabledReason(account: Account): string {
  if (account.status !== 'active') return t('accountShare.roomAccounts.accountInactive')
  if (!account.schedulable) return t('accountShare.roomAccounts.accountUnschedulable')
  if (!Number.isFinite(Number(account.concurrency)) || Number(account.concurrency) <= 0) {
    return t('accountShare.roomAccounts.accountConcurrencyUnavailable')
  }
  if (!account.external_placement || account.external_placement.state !== 'active') {
    return t('accountShare.roomAccounts.placementInactive')
  }
  const listingID = Number(props.listing?.id || 0)
  const boundRoomID = Number(
    account.account_share_mode_listing_id
    || account.external_placement.room_id
    || 0
  )
  if (boundRoomID > 0 && boundRoomID !== listingID) {
    return t('accountShare.roomAccounts.accountOccupiedByOtherRoom')
  }
  return ''
}

function candidateDisabledReason(account: Account): string {
  const listing = props.listing
  if (!listing) return t('accountShare.roomAccounts.roomUnavailable')

  if (
    account.owner_user_id == null
    || Number(account.owner_user_id) !== Number(listing.owner_user_id)
  ) {
    return t('accountShare.roomAccounts.ownerMismatch')
  }

  const listingPlatform = normalizeComparableValue(listing.platform)
  const accountPlatform = normalizeComparableValue(account.platform)
  if (!accountPlatform || accountPlatform !== listingPlatform) {
    return t('accountShare.roomAccounts.platformMismatch', {
      platform: listing.platform
    })
  }

  // opencode 账号没有等级概念（account_level 恒为 unknown），跳过等级校验。
  if (!isOpencodePlatform(listing.platform)) {
    if (!isKnownLevel(listing.account_level)) {
      return t('accountShare.roomAccounts.roomLevelUnknown')
    }
    if (!isKnownLevel(account.account_level)) {
      return t('accountShare.roomAccounts.accountLevelUnknown')
    }
    if (normalizeComparableValue(account.account_level) !== normalizeComparableValue(listing.account_level)) {
      return t('accountShare.roomAccounts.levelMismatch', {
        level: listing.account_level
      })
    }
  }

  const availabilityReason = candidateAvailabilityDisabledReason(account)
  if (availabilityReason) return availabilityReason

  if (resolveAccountExternalPlacementTarget(account) !== 'room') {
    return t('accountShare.roomAccounts.platformModeRequired', {
      mode: platformModeLabel(listing.platform)
    })
  }
  return ''
}

function isCandidateEligible(account: Account): boolean {
  return candidateDisabledReason(account) === ''
}

function candidateModeLabel(account: Account): string {
  const target = resolveAccountExternalPlacementTarget(account)
  if (target === 'public_pool') return t('accountShare.roomAccounts.modePublicPool')
  if (target === 'room') return platformModeLabel(account.platform)
  return t('accountShare.roomAccounts.modePrivate')
}

function platformModeLabel(platform: unknown): string {
  const normalized = normalizeComparableValue(platform)
  const displayName = normalized === 'openai'
    ? 'OpenAI'
    : normalized === 'anthropic'
      ? 'Anthropic'
      : normalized === 'opencode'
        ? 'Opencode'
        : String(platform || '')
  return t('accountShare.roomAccounts.modePlatform', { platform: displayName })
}

function toggleMember(accountID: number): void {
  if (operating.value) return
  const next = new Set(selectedMemberIDs.value)
  if (next.has(accountID)) next.delete(accountID)
  else next.add(accountID)
  selectedMemberIDs.value = next
}

function requestClose(): void {
  if (
    operating.value
    || showCreateAccountFlow.value
    || pendingRemoveAccountIDs.value.length > 0
  ) {
    return
  }
  emit('close')
}

function openCreateAccountFlow(): void {
  if (operating.value || !canCreateCompatibleAccount.value) return
  showCreateAccountFlow.value = true
}

function closeCreateAccountFlow(): void {
  showCreateAccountFlow.value = false
}

async function handleCreatedAccountAttached(): Promise<void> {
  showCreateAccountFlow.value = false
  emit('changed', {
    operation: 'add',
    success: 1,
    failed: 0
  })
  await refreshAll()
  activeTab.value = 'members'
}

function toggleCandidate(account: Account): void {
  if (operating.value || !isCandidateEligible(account)) return
  const next = new Set(selectedCandidateIDs.value)
  if (next.has(account.id)) next.delete(account.id)
  else next.add(account.id)
  selectedCandidateIDs.value = next
}

function toggleAllMembers(): void {
  const next = new Set(selectedMemberIDs.value)
  if (allVisibleMembersSelected.value) {
    for (const account of filteredMemberAccounts.value) next.delete(account.account_id)
  } else {
    for (const account of filteredMemberAccounts.value) next.add(account.account_id)
  }
  selectedMemberIDs.value = next
}

function toggleAllCandidates(): void {
  const next = new Set(selectedCandidateIDs.value)
  if (allVisibleCandidatesSelected.value) {
    for (const account of visibleEligibleCandidates.value) next.delete(account.id)
  } else {
    for (const account of visibleEligibleCandidates.value) next.add(account.id)
  }
  selectedCandidateIDs.value = next
}

function openRemoveConfirmation(): void {
  if (operating.value || loadingAccounts.value || selectedMemberIDs.value.size === 0) return
  const currentAccountIDs = new Set(accounts.value.map(account => account.account_id))
  pendingRemoveAccountIDs.value = Array.from(selectedMemberIDs.value)
    .filter(accountID => currentAccountIDs.has(accountID))
    .sort((left, right) => left - right)
}

function cancelRemoveConfirmation(): void {
  if (operating.value) return
  pendingRemoveAccountIDs.value = []
}

async function confirmRemoveAccounts(): Promise<void> {
  const accountIDs = [...pendingRemoveAccountIDs.value]
  if (accountIDs.length === 0 || operating.value) return
  try {
    await submitBatchOperation('remove', accountIDs)
  } finally {
    pendingRemoveAccountIDs.value = []
  }
}

function buildIdempotencyKey(
  operation: RoomAccountOperation,
  roomID: number,
  accountIDs: number[]
): string {
  const signature = JSON.stringify({
    operation,
    roomID,
    accountIDs: [...accountIDs].sort((left, right) => left - right)
  })
  if (
    pendingOperationIdempotencyKey
    && pendingOperationSignature === signature
  ) {
    return pendingOperationIdempotencyKey
  }
  const requestID = globalThis.crypto?.randomUUID?.()
  if (!requestID) {
    throw new Error(t('accountShare.roomAccounts.uuidUnavailable'))
  }
  pendingOperationSignature = signature
  pendingOperationIdempotencyKey = `room-${operation}-${roomID}-${requestID}`
  return pendingOperationIdempotencyKey
}

function accountNameForOperation(operation: RoomAccountOperation, accountID: number): string {
  if (operation === 'remove') {
    return accounts.value.find((account) => account.account_id === accountID)?.account_name
      || t('accountShare.roomAccounts.unknownAccount')
  }
  return candidates.value.find((account) => account.id === accountID)?.name
    || t('accountShare.roomAccounts.unknownAccount')
}

function summarizeOperation(
  operation: RoomAccountOperation,
  result: AccountShareRoomAccountsBatchResponse
): OperationSummary {
  const success = result.success || 0
  const failed = result.failed || 0
  if (success > 0 && failed === 0) {
    return {
      tone: 'success',
      text: t(
        operation === 'add'
          ? 'accountShare.roomAccounts.addSuccess'
          : 'accountShare.roomAccounts.removeSuccess',
        { count: success }
      )
    }
  }
  if (success > 0) {
    return {
      tone: 'warning',
      text: t(
        operation === 'add'
          ? 'accountShare.roomAccounts.addPartial'
          : 'accountShare.roomAccounts.removePartial',
        { success, failed }
      )
    }
  }
  return {
    tone: 'error',
    text: t(
      operation === 'add'
        ? 'accountShare.roomAccounts.addFailed'
        : 'accountShare.roomAccounts.removeFailed',
      { count: failed }
    )
  }
}

function collectOperationFailures(
  operation: RoomAccountOperation,
  result: AccountShareRoomAccountsBatchResponse
): OperationFailure[] {
  return result.results
    .filter((item) => !item.success)
    .map((item) => ({
      accountID: item.account_id,
      name: accountNameForOperation(operation, item.account_id),
      error: roomAccountOperationFailureMessage(item)
    }))
}

// 后端把"缺哪个模型/哪个账号"放在错误 metadata 里；不带出来的话用户只能看到
// 一句无从下手的"不支持请求的模型"。
function describeRoomAccountOperationError(error: unknown, operation: RoomAccountOperation): string {
  const base = extractApiErrorMessage(
    error,
    operation === 'add'
      ? t('accountShare.roomAccounts.addRequestFailed')
      : t('accountShare.roomAccounts.removeRequestFailed'),
    ROOM_ACCOUNT_ERROR_MESSAGES
  )
  if (extractApiErrorCode(error) !== 'ACCOUNT_SHARE_MODE_UNSUPPORTED_MODEL') return base
  const metadata = extractApiErrorMetadata(error) || {}
  const model = typeof metadata.model === 'string' ? metadata.model.trim() : ''
  const accountID = typeof metadata.account_id === 'string' ? metadata.account_id.trim() : ''
  if (!model) return base
  const who = accountID ? `账号 #${accountID}` : '该账号'
  return `${who}缺少房间要求的模型「${model}」。请在"我的账号"中为该账号补上这个模型，或把它从房间允许模型中移除后再试。`
}

function roomAccountOperationFailureMessage(item: AccountShareRoomAccountsBatchResult): string {
  const reason = item.reason?.trim() || ''
  const metadata = item.metadata || {}

  if (reason === 'ACCOUNT_SHARE_MODE_UNSUPPORTED_MODEL') {
    const model = metadata.model?.trim()
    if (model) {
      return `缺少房间要求的模型「${model}」。请在"我的账号"中补上该模型，或将其从房间模型白名单移除后重试。`
    }
  }

  if (reason === 'ACCOUNT_SHARE_ACCOUNT_UNAVAILABLE') {
    const blocker = metadata.blocker?.trim()
    if (blocker) {
      return ROOM_ACCOUNT_UNAVAILABLE_BLOCKER_MESSAGES[blocker]
        || `账号当前不可用（阻塞原因：${blocker}）`
    }
  }

  if (reason && ROOM_ACCOUNT_ERROR_MESSAGES[reason]) {
    return ROOM_ACCOUNT_ERROR_MESSAGES[reason]
  }

  const detail = item.message?.trim() || item.error?.trim() || ''
  if (detail && ROOM_ACCOUNT_ERROR_MESSAGES[detail]) {
    return ROOM_ACCOUNT_ERROR_MESSAGES[detail]
  }
  if (reason && detail) return `操作失败（错误码：${reason}）：${detail}`
  if (reason) return `操作失败（错误码：${reason}）`
  if (detail) return `操作失败：${detail}`
  return t('accountShare.roomAccounts.unknownFailure')
}

async function submitBatchOperation(
  operation: RoomAccountOperation,
  accountIDsOverride?: number[]
): Promise<void> {
  const listingID = props.listing?.id
  if (!listingID || operating.value) return

  const accountIDs = accountIDsOverride
    ? [...accountIDsOverride]
    : operation === 'add'
      ? Array.from(selectedCandidateIDs.value)
      : Array.from(selectedMemberIDs.value)
  const eligibleAccountIDs = operation === 'add'
    ? accountIDs.filter((accountID) => {
        const account = candidates.value.find((item) => item.id === accountID)
        return Boolean(account && isCandidateEligible(account))
      })
    : accountIDs
  if (operation === 'add' && eligibleAccountIDs.length !== accountIDs.length) {
    selectedCandidateIDs.value = new Set(eligibleAccountIDs)
  }
  if (eligibleAccountIDs.length === 0) return

  operationSummary.value = null
  operationFailures.value = []
  operating.value = true
  try {
    const payload = {
      account_ids: eligibleAccountIDs,
      idempotency_key: buildIdempotencyKey(operation, listingID, eligibleAccountIDs)
    }
    const result = operation === 'add'
      ? await accountShareAPI.attachRoomAccounts(listingID, payload)
      : await accountShareAPI.detachRoomAccounts(listingID, payload)
    pendingOperationSignature = ''
    pendingOperationIdempotencyKey = ''
    operationSummary.value = summarizeOperation(operation, result)
    operationFailures.value = collectOperationFailures(operation, result)

    if ((result.success || 0) > 0) {
      // 仅移除已经成功的选择；部分失败项继续保留，方便用户处理阻塞原因后原样重试。
      const successfulAccountIDs = new Set(result.success_ids)
      if (operation === 'add') {
        selectedCandidateIDs.value = new Set(
          Array.from(selectedCandidateIDs.value)
            .filter((accountID) => !successfulAccountIDs.has(accountID))
        )
      } else {
        selectedMemberIDs.value = new Set(
          Array.from(selectedMemberIDs.value)
            .filter((accountID) => !successfulAccountIDs.has(accountID))
        )
      }
      emit('changed', {
        operation,
        success: result.success || 0,
        failed: result.failed || 0
      })
    }
    // 无论成功与否都刷新：整体失败时服务端仍可能已发生部分变化，
    // 不刷新会让面板停留在过期状态。
    await refreshAll()
  } catch (error) {
    operationSummary.value = {
      tone: 'error',
      text: describeRoomAccountOperationError(error, operation)
    }
  } finally {
    operating.value = false
  }
}

async function refreshAll(): Promise<void> {
  await Promise.all([loadAccounts(), loadCandidates()])
}

async function loadAccounts(): Promise<void> {
  const listingID = props.listing?.id
  if (!listingID) return

  const currentVersion = ++accountsRequestVersion
  loadingAccounts.value = true
  accountsErrorMessage.value = ''
  try {
    const result = await accountShareAPI.listRoomAccounts(listingID)
    if (currentVersion !== accountsRequestVersion) return
    accounts.value = result
    const validIDs = new Set(result.map((account) => account.account_id))
    selectedMemberIDs.value = new Set(
      Array.from(selectedMemberIDs.value).filter((accountID) => validIDs.has(accountID))
    )
  } catch (error) {
    if (currentVersion !== accountsRequestVersion) return
    accounts.value = []
    accountsErrorMessage.value = extractApiErrorMessage(
      error,
      t('accountShare.roomAccounts.loadFailed'),
      ROOM_ACCOUNT_ERROR_MESSAGES
    )
  } finally {
    if (currentVersion === accountsRequestVersion) {
      loadingAccounts.value = false
    }
  }
}

async function loadCandidates(): Promise<void> {
  const platform = props.listing?.platform
  if (!platform) return

  const currentVersion = ++candidatesRequestVersion
  candidatesRequestController?.abort()
  const controller = new AbortController()
  candidatesRequestController = controller
  loadingCandidates.value = true
  candidatesErrorMessage.value = ''
  try {
    const loadedAccounts = await loadAllPaginatedItems(
      (page) => accountsAPI.list(page, 100, { platform }, { signal: controller.signal }),
      {
        signal: controller.signal,
        isCurrent: () => currentVersion === candidatesRequestVersion,
        concurrency: 3
      }
    )
    const accountByID = new Map<number, Account>()
    for (const account of loadedAccounts) accountByID.set(account.id, account)
    candidates.value = Array.from(accountByID.values()).sort((left, right) => (
      left.name.localeCompare(right.name, undefined, { numeric: true, sensitivity: 'base' })
    ))
  } catch (error) {
    if (
      currentVersion !== candidatesRequestVersion
      || controller.signal.aborted
      || isCanceledRequest(error)
    ) return
    candidates.value = []
    candidatesErrorMessage.value = extractApiErrorMessage(
      error,
      t('accountShare.roomAccounts.candidateLoadFailed'),
      ROOM_ACCOUNT_ERROR_MESSAGES
    )
  } finally {
    if (currentVersion === candidatesRequestVersion) {
      loadingCandidates.value = false
      if (candidatesRequestController === controller) {
        candidatesRequestController = null
      }
    }
  }
}

function isCanceledRequest(error: unknown): boolean {
  if (typeof error !== 'object' || error === null) return false
  const canceled = error as { code?: string; name?: string }
  return canceled.code === 'ERR_CANCELED'
    || canceled.name === 'CanceledError'
    || canceled.name === 'AbortError'
}
</script>

<style scoped>
.room-summary-cell {
  display: flex;
  min-width: 0;
  flex-direction: column;
  gap: 0.25rem;
}

.room-account-remove-confirmation {
  display: grid;
  min-width: 0;
  gap: 1rem;
}

.remove-impact-summary {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 0.5rem;
}

.remove-impact-summary > div {
  min-width: 0;
  border: 1px solid rgb(226 232 240);
  border-radius: 0.75rem;
  background: rgb(248 250 252);
  padding: 0.75rem;
  text-align: center;
}

.remove-impact-summary span,
.remove-impact-summary strong {
  display: block;
}

.remove-impact-summary span {
  color: rgb(100 116 139);
  font-size: 0.6875rem;
}

.remove-impact-summary strong {
  margin-top: 0.25rem;
  color: rgb(15 23 42);
  font-size: 1.125rem;
}

.remove-impact-notice {
  display: flex;
  min-width: 0;
  align-items: flex-start;
  gap: 0.75rem;
  border: 1px solid rgb(253 230 138);
  border-radius: 0.875rem;
  background: rgb(255 251 235);
  padding: 0.875rem;
  color: rgb(146 64 14);
}

.remove-impact-notice-critical {
  border-color: rgb(254 202 202);
  background: rgb(254 242 242);
  color: rgb(153 27 27);
}

.remove-impact-notice > svg {
  margin-top: 0.125rem;
  flex-shrink: 0;
}

.remove-impact-notice strong,
.remove-impact-notice p {
  display: block;
}

.remove-impact-notice strong {
  font-size: 0.8125rem;
}

.remove-impact-notice p {
  margin-top: 0.25rem;
  font-size: 0.75rem;
  line-height: 1.25rem;
}

.remove-impact-runtime {
  display: grid;
  grid-template-columns: minmax(0, 1fr) auto;
  gap: 0.25rem 0.75rem;
  border-radius: 0.75rem;
  background: rgb(241 245 249);
  padding: 0.75rem;
}

.remove-impact-runtime span,
.remove-impact-runtime strong {
  color: rgb(51 65 85);
  font-size: 0.8125rem;
}

.remove-impact-runtime strong {
  font-weight: 750;
}

.remove-impact-runtime small {
  grid-column: 1 / -1;
  color: rgb(100 116 139);
  font-size: 0.6875rem;
  line-height: 1.125rem;
}

.remove-account-list {
  display: grid;
  gap: 0.5rem;
}

.remove-account-list > span {
  color: rgb(51 65 85);
  font-size: 0.8125rem;
  font-weight: 650;
}

.remove-account-list ul {
  display: grid;
  max-height: 12rem;
  gap: 0.375rem;
  overflow-y: auto;
  margin: 0;
  padding: 0;
  list-style: none;
}

.remove-account-list li {
  display: flex;
  min-width: 0;
  align-items: center;
  justify-content: space-between;
  gap: 0.75rem;
  border: 1px solid rgb(226 232 240);
  border-radius: 0.75rem;
  padding: 0.625rem 0.75rem;
}

.remove-account-list strong {
  overflow: hidden;
  color: rgb(30 41 59);
  font-size: 0.8125rem;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.remove-account-list small {
  flex-shrink: 0;
  color: rgb(100 116 139);
  font-size: 0.6875rem;
}

.remove-confirmation-footer {
  display: flex;
  width: 100%;
  flex-direction: column-reverse;
  gap: 0.625rem;
}

.room-summary-cell span {
  font-size: 0.75rem;
  color: rgb(107 114 128);
}

.room-summary-cell strong {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  font-size: 0.875rem;
  color: rgb(17 24 39);
}

.room-tab {
  min-height: 2.75rem;
  border-radius: 0.625rem;
  padding: 0.5rem 0.75rem;
  font-size: 0.875rem;
  font-weight: 600;
  color: rgb(75 85 99);
  transition: color 150ms ease, background-color 150ms ease, box-shadow 150ms ease;
}

.room-tab:focus-visible {
  outline: 2px solid rgb(59 130 246 / 0.55);
  outline-offset: 2px;
}

.room-tab:disabled {
  cursor: not-allowed;
  opacity: 0.55;
}

.room-tab-active {
  background: rgb(255 255 255);
  color: rgb(17 24 39);
  box-shadow: 0 1px 2px rgb(15 23 42 / 0.08);
}

.room-account-card {
  display: flex;
  min-width: 0;
  gap: 0.75rem;
  border: 1px solid rgb(229 231 235);
  border-radius: 1rem;
  background: rgb(255 255 255);
  padding: 1rem;
  transition: border-color 150ms ease, box-shadow 150ms ease, background-color 150ms ease;
}

.room-account-card-selected {
  border-color: rgb(59 130 246);
  background: rgb(239 246 255);
  box-shadow: 0 0 0 2px rgb(59 130 246 / 0.12);
}

.create-compatible-account-card {
  display: flex;
  min-width: 0;
  flex-direction: column;
  gap: 0.875rem;
  border: 1px solid rgb(191 219 254);
  border-radius: 1rem;
  background: linear-gradient(135deg, rgb(239 246 255), rgb(240 253 250));
  padding: 1rem;
}

.create-compatible-account-card strong,
.create-compatible-account-card small {
  display: block;
}

.create-compatible-account-card strong {
  color: rgb(30 64 175);
  font-size: 0.875rem;
}

.create-compatible-account-card small {
  margin-top: 0.375rem;
  color: rgb(71 85 105);
  font-size: 0.75rem;
  line-height: 1.25rem;
}

.room-checkbox-control {
  display: inline-flex;
  min-width: 2.75rem;
  min-height: 2.75rem;
  flex: 0 0 2.75rem;
  align-items: center;
  justify-content: center;
  margin: -0.625rem 0 -0.625rem -0.625rem;
}

.account-level-badge {
  display: inline-flex;
  align-items: center;
  border-radius: 9999px;
  background: rgb(238 242 255);
  padding: 0.125rem 0.5rem;
  color: rgb(67 56 202);
  font-weight: 600;
}

:global(.dark) .room-summary-cell span {
  color: rgb(156 163 175);
}

:global(.dark) .room-summary-cell strong {
  color: rgb(255 255 255);
}

:global(.dark) .remove-impact-summary > div,
:global(.dark) .remove-account-list li {
  border-color: rgb(63 63 70);
  background: rgb(39 39 42 / 0.55);
}

:global(.dark) .remove-impact-summary strong,
:global(.dark) .remove-account-list strong {
  color: rgb(244 244 245);
}

:global(.dark) .remove-impact-summary span,
:global(.dark) .remove-account-list small {
  color: rgb(161 161 170);
}

:global(.dark) .remove-impact-notice {
  border-color: rgb(120 53 15);
  background: rgb(69 26 3 / 0.28);
  color: rgb(253 186 116);
}

:global(.dark) .remove-impact-notice-critical {
  border-color: rgb(127 29 29);
  background: rgb(69 10 10 / 0.28);
  color: rgb(252 165 165);
}

:global(.dark) .remove-impact-runtime {
  background: rgb(39 39 42 / 0.75);
}

:global(.dark) .remove-impact-runtime span,
:global(.dark) .remove-impact-runtime strong,
:global(.dark) .remove-account-list > span {
  color: rgb(212 212 216);
}

:global(.dark) .remove-impact-runtime small {
  color: rgb(161 161 170);
}

:global(.dark) .room-tab {
  color: rgb(209 213 219);
}

:global(.dark) .room-tab-active {
  background: rgb(55 65 81);
  color: rgb(255 255 255);
}

:global(.dark) .room-account-card {
  border-color: rgb(75 85 99);
  background: rgb(31 41 55);
}

:global(.dark) .room-account-card-selected {
  border-color: rgb(96 165 250);
  background: rgb(30 58 138 / 0.2);
}

:global(.dark) .create-compatible-account-card {
  border-color: rgb(30 64 175 / 0.7);
  background: linear-gradient(135deg, rgb(30 58 138 / 0.24), rgb(6 78 59 / 0.2));
}

:global(.dark) .create-compatible-account-card strong {
  color: rgb(191 219 254);
}

:global(.dark) .create-compatible-account-card small {
  color: rgb(161 161 170);
}

:global(.dark) .account-level-badge {
  background: rgb(49 46 129 / 0.45);
  color: rgb(199 210 254);
}

@media (min-width: 640px) {
  .create-compatible-account-card {
    flex-direction: row;
    align-items: center;
    justify-content: space-between;
  }

  .remove-confirmation-footer {
    flex-direction: row;
    justify-content: flex-end;
  }
}
</style>
