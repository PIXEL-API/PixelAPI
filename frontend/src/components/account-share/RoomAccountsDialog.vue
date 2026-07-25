<template>
  <BaseDialog
    :show="show"
    :title="t('accountShare.roomAccounts.title', { name: roomDisplayName })"
    width="wide"
    @close="emit('close')"
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
            @click="submitBatchOperation('remove')"
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

        <div class="flex flex-col gap-2 sm:flex-row sm:items-center sm:justify-between">
          <label class="min-w-0 flex-1">
            <span class="sr-only">{{ t('accountShare.roomAccounts.searchCandidates') }}</span>
            <input
              v-model.trim="candidateSearch"
              type="search"
              class="input min-h-11 w-full"
              :placeholder="t('accountShare.roomAccounts.searchCandidates')"
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
          @click="emit('close')"
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
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import BaseDialog from '@/components/common/BaseDialog.vue'
import Icon from '@/components/icons/Icon.vue'
import {
  accountShareAPI,
  type AccountShareListing,
  type AccountShareRoomAccount,
  type AccountShareRoomAccountsBatchResponse
} from '@/api/accountShare'
import { accountsAPI } from '@/api/accounts'
import type { Account } from '@/types'
import { resolveAccountExternalPlacementTarget } from '@/components/account-share/externalPlacement'
import { extractApiErrorMessage } from '@/utils/apiError'
import { formatDateTime } from '@/utils/format'

type RoomAccountsTab = 'members' | 'add'
type RoomAccountOperation = 'add' | 'remove'

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

const props = defineProps<{
  show: boolean
  listing: AccountShareListing | null
}>()

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
let accountsRequestVersion = 0
let candidatesRequestVersion = 0
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

const allVisibleMembersSelected = computed(() => (
  filteredMemberAccounts.value.length > 0
  && filteredMemberAccounts.value.every((account) => selectedMemberIDs.value.has(account.account_id))
))

const allVisibleCandidatesSelected = computed(() => (
  visibleEligibleCandidates.value.length > 0
  && visibleEligibleCandidates.value.every((account) => selectedCandidateIDs.value.has(account.id))
))

const refreshing = computed(() => loadingAccounts.value || loadingCandidates.value)

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

function isRoomAccountHealthy(account: AccountShareRoomAccount): boolean {
  return account.status === 'active'
    && account.schedulable
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
  return account.status === 'active'
    && account.schedulable
    && (!account.external_placement || account.external_placement.state === 'active')
}

function candidateHealthLabel(account: Account): string {
  return isCandidateHealthy(account)
    ? t('accountShare.roomAccounts.healthy')
    : t('accountShare.roomAccounts.unavailable')
}

function candidateHealthBadgeClass(account: Account): string {
  return badgeClass(isCandidateHealthy(account))
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
      error: item.error || t('accountShare.roomAccounts.unknownFailure')
    }))
}

async function submitBatchOperation(operation: RoomAccountOperation): Promise<void> {
  const listingID = props.listing?.id
  if (!listingID || operating.value) return

  const accountIDs = operation === 'add'
    ? Array.from(selectedCandidateIDs.value)
    : Array.from(selectedMemberIDs.value)
  if (accountIDs.length === 0) return

  operationSummary.value = null
  operationFailures.value = []
  operating.value = true
  try {
    const payload = {
      account_ids: accountIDs,
      idempotency_key: buildIdempotencyKey(operation, listingID, accountIDs)
    }
    const result = operation === 'add'
      ? await accountShareAPI.attachRoomAccounts(listingID, payload)
      : await accountShareAPI.detachRoomAccounts(listingID, payload)
    pendingOperationSignature = ''
    pendingOperationIdempotencyKey = ''
    operationSummary.value = summarizeOperation(operation, result)
    operationFailures.value = collectOperationFailures(operation, result)
    selectedMemberIDs.value = new Set()
    selectedCandidateIDs.value = new Set()

    if ((result.success || 0) > 0) {
      emit('changed', {
        operation,
        success: result.success || 0,
        failed: result.failed || 0
      })
      await refreshAll()
    }
  } catch (error) {
    operationSummary.value = {
      tone: 'error',
      text: extractApiErrorMessage(
        error,
        operation === 'add'
          ? t('accountShare.roomAccounts.addRequestFailed')
          : t('accountShare.roomAccounts.removeRequestFailed')
      )
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
      t('accountShare.roomAccounts.loadFailed')
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
  loadingCandidates.value = true
  candidatesErrorMessage.value = ''
  try {
    const firstPage = await accountsAPI.list(1, 100, { platform })
    const remainingPages = Array.from(
      { length: Math.max(0, firstPage.pages - 1) },
      (_, index) => index + 2
    )
    const remainingResults = await Promise.all(
      remainingPages.map((page) => accountsAPI.list(page, 100, { platform }))
    )
    if (currentVersion !== candidatesRequestVersion) return

    const accountByID = new Map<number, Account>()
    for (const account of firstPage.items || []) accountByID.set(account.id, account)
    for (const result of remainingResults) {
      for (const account of result.items || []) accountByID.set(account.id, account)
    }
    candidates.value = Array.from(accountByID.values()).sort((left, right) => (
      left.name.localeCompare(right.name, undefined, { numeric: true, sensitivity: 'base' })
    ))
  } catch (error) {
    if (currentVersion !== candidatesRequestVersion) return
    candidates.value = []
    candidatesErrorMessage.value = extractApiErrorMessage(
      error,
      t('accountShare.roomAccounts.candidateLoadFailed')
    )
  } finally {
    if (currentVersion === candidatesRequestVersion) {
      loadingCandidates.value = false
    }
  }
}
</script>

<style scoped>
.room-summary-cell {
  display: flex;
  min-width: 0;
  flex-direction: column;
  gap: 0.25rem;
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

:global(.dark) .account-level-badge {
  background: rgb(49 46 129 / 0.45);
  color: rgb(199 210 254);
}
</style>
