<template>
  <AppLayout>
    <TablePageLayout>
      <template #actions>
        <div class="flex flex-wrap items-center justify-end gap-3">
          <button
            type="button"
            class="btn btn-secondary"
            :disabled="loading"
            :title="t('common.refresh')"
            @click="loadAccounts"
          >
            <Icon name="refresh" size="md" :class="loading ? 'animate-spin' : ''" />
          </button>
          <button
            type="button"
            class="btn btn-secondary"
            :disabled="selectedCount === 0"
            @click="openBulkEditModal"
          >
            <Icon name="edit" size="md" class="mr-2" />
            {{ t('admin.accounts.bulkActions.edit') }}
          </button>
          <button type="button" class="btn btn-secondary" @click="showImportModal = true">
            <Icon name="upload" size="md" class="mr-2" />
            {{ t('userAccounts.importAccounts') }}
          </button>
          <button type="button" class="btn btn-primary" @click="showCreateModal = true">
            <Icon name="plus" size="md" class="mr-2" />
            {{ t('userAccounts.createAccount') }}
          </button>
        </div>
      </template>

      <template #filters>
        <div class="flex flex-wrap items-center gap-3">
          <SearchInput
            v-model="filterSearch"
            :placeholder="t('userAccounts.searchPlaceholder')"
            class="w-full sm:w-64"
            @search="onFilterChange"
          />
          <Select
            :model-value="filterPlatform"
            class="w-44"
            :options="platformFilterOptions"
            @update:model-value="onPlatformFilterChange"
          />
          <Select
            :model-value="filterType"
            class="w-40"
            :options="typeFilterOptions"
            @update:model-value="onTypeFilterChange"
          />
          <Select
            :model-value="filterStatus"
            class="w-40"
            :options="statusFilterOptions"
            @update:model-value="onStatusFilterChange"
          />
          <Select
            :model-value="filterGroupId"
            class="w-44"
            :options="groupFilterOptions"
            @update:model-value="onGroupFilterChange"
          />
        </div>
      </template>

      <template #table>
        <div
          v-if="selectedCount > 0"
          class="mb-4 flex flex-wrap items-center justify-between gap-3 rounded-lg bg-primary-50 p-3 dark:bg-primary-900/20"
        >
          <div class="flex flex-wrap items-center gap-2">
            <span class="text-sm font-medium text-primary-900 dark:text-primary-100">
              {{ t('admin.accounts.bulkActions.selected', { count: selectedCount }) }}
            </span>
            <button
              type="button"
              class="text-xs font-medium text-primary-700 hover:text-primary-800 dark:text-primary-300 dark:hover:text-primary-200"
              @click="selectVisible"
            >
              {{ t('admin.accounts.bulkActions.selectCurrentPage') }}
            </button>
            <span class="text-gray-300 dark:text-primary-800">/</span>
            <button
              type="button"
              class="text-xs font-medium text-primary-700 hover:text-primary-800 dark:text-primary-300 dark:hover:text-primary-200"
              @click="clearSelection"
            >
              {{ t('admin.accounts.bulkActions.clear') }}
            </button>
          </div>
          <button type="button" class="btn btn-primary btn-sm" @click="openBulkEditModal">
            {{ t('admin.accounts.bulkActions.edit') }}
          </button>
        </div>
        <DataTable
          :columns="columns"
          :data="accounts"
          :loading="loading"
          row-key="id"
          :server-side-sort="true"
          default-sort-key="created_at"
          default-sort-order="desc"
          :estimate-row-height="72"
          :overscan="5"
          @sort="handleSort"
        >
          <template #header-select>
            <input
              type="checkbox"
              class="h-4 w-4 cursor-pointer rounded border-gray-300 text-primary-600 focus:ring-primary-500"
              :checked="allVisibleSelected"
              @click.stop
              @change="toggleSelectAllVisible($event)"
            />
          </template>
          <template #cell-select="{ row }">
            <input
              type="checkbox"
              class="rounded border-gray-300 text-primary-600 focus:ring-primary-500"
              :checked="isSelected(row.id)"
              @change="toggleSelection(row.id)"
            />
          </template>
          <template #cell-name="{ row, value }">
            <div class="flex min-w-[180px] flex-col">
              <span class="font-medium text-gray-900 dark:text-white">{{ value }}</span>
              <span
                v-if="row.extra?.email_address"
                class="max-w-[220px] truncate text-xs text-gray-500 dark:text-gray-400"
                :title="row.extra.email_address"
              >
                {{ row.extra.email_address }}
              </span>
            </div>
          </template>

          <template #cell-platform_type="{ row }">
            <div class="flex flex-wrap items-center gap-1">
              <PlatformTypeBadge
                :platform="row.platform"
                :type="row.type"
                :plan-type="row.credentials?.plan_type"
                :privacy-mode="row.extra?.privacy_mode"
                :subscription-expires-at="row.credentials?.subscription_expires_at"
              />
              <span
                v-if="getOpenAICompactLabel(row)"
                :class="['inline-block rounded px-1.5 py-0.5 text-[10px] font-medium', getOpenAICompactClass(row)]"
                :title="getOpenAICompactTitle(row)"
              >
                {{ getOpenAICompactLabel(row) }}
              </span>
              <span
                v-if="getAntigravityTierLabel(row)"
                :class="['inline-block rounded px-1.5 py-0.5 text-[10px] font-medium', getAntigravityTierClass(row)]"
              >
                {{ getAntigravityTierLabel(row) }}
              </span>
            </div>
          </template>

          <template #cell-share="{ row }">
            <div class="flex flex-col gap-1">
              <span :class="shareModeBadgeClass(row.share_mode)">
                {{ shareModeLabel(row.share_mode) }}
              </span>
              <span v-if="row.share_mode === 'public'" :class="shareStatusBadgeClass(row.share_status)">
                {{ shareStatusLabel(row.share_status) }}
              </span>
            </div>
          </template>

          <template #cell-capacity="{ row }">
            <AccountCapacityCell :account="row" />
          </template>

          <template #cell-status="{ row }">
            <AccountStatusIndicator :account="row" @show-temp-unsched="handleShowTempUnsched" />
          </template>

          <template #cell-schedulable="{ row }">
            <button
              type="button"
              class="relative inline-flex h-5 w-9 flex-shrink-0 cursor-pointer rounded-full border-2 border-transparent transition-colors duration-200 ease-in-out focus:outline-none focus:ring-2 focus:ring-primary-500 focus:ring-offset-2 disabled:cursor-not-allowed disabled:opacity-50 dark:focus:ring-offset-dark-800"
              :class="row.schedulable ? 'bg-primary-500 hover:bg-primary-600' : 'bg-gray-200 hover:bg-gray-300 dark:bg-dark-600 dark:hover:bg-dark-500'"
              :disabled="togglingSchedulableId === row.id"
              :title="row.schedulable ? t('admin.accounts.schedulableEnabled') : t('admin.accounts.schedulableDisabled')"
              @click="toggleSchedulable(row)"
            >
              <span
                class="pointer-events-none inline-block h-4 w-4 transform rounded-full bg-white shadow ring-0 transition duration-200 ease-in-out"
                :class="row.schedulable ? 'translate-x-4' : 'translate-x-0'"
              />
            </button>
          </template>

          <template #cell-today_stats="{ row }">
            <AccountTodayStatsCell
              :stats="todayStatsByAccountId[String(row.id)] ?? null"
              :loading="todayStatsLoading"
              :error="todayStatsError"
            />
          </template>

          <template #cell-groups="{ row }">
            <AccountGroupsCell :groups="row.groups" :max-display="4" />
          </template>

          <template #cell-usage="{ row }">
            <AccountUsageCell
              :account="row"
              :today-stats="todayStatsByAccountId[String(row.id)] ?? null"
              :today-stats-loading="todayStatsLoading"
              :usage-loader="accountsAPI.getUsage"
              usage-cache-scope="user"
              :manual-refresh-token="usageManualRefreshToken"
            />
          </template>

          <template #cell-priority="{ value }">
            <span class="text-sm text-gray-700 dark:text-gray-300">{{ value }}</span>
          </template>

          <template #cell-last_used_at="{ value }">
            <span class="text-sm text-gray-500 dark:text-dark-400">{{ formatRelativeTime(value) }}</span>
          </template>

          <template #cell-expires_at="{ row, value }">
            <div class="flex flex-col items-start gap-1">
              <span class="text-sm text-gray-500 dark:text-dark-400">{{ formatExpiresAt(value) }}</span>
              <div v-if="isExpired(value) || (row.auto_pause_on_expired && value)" class="flex items-center gap-1">
                <span
                  v-if="isExpired(value)"
                  class="inline-flex items-center rounded-md bg-amber-100 px-2 py-0.5 text-xs font-medium text-amber-700 dark:bg-amber-900/30 dark:text-amber-300"
                >
                  {{ t('admin.accounts.expired') }}
                </span>
                <span
                  v-if="row.auto_pause_on_expired && value"
                  class="inline-flex items-center rounded-md bg-emerald-100 px-2 py-0.5 text-xs font-medium text-emerald-700 dark:bg-emerald-900/30 dark:text-emerald-300"
                >
                  {{ t('admin.accounts.autoPauseOnExpired') }}
                </span>
              </div>
            </div>
          </template>

          <template #cell-notes="{ value }">
            <span v-if="value" :title="value" class="block max-w-xs truncate text-sm text-gray-600 dark:text-gray-300">
              {{ value }}
            </span>
            <span v-else class="text-sm text-gray-400 dark:text-dark-500">-</span>
          </template>

          <template #cell-actions="{ row }">
            <div class="flex items-center justify-end gap-1">
              <button
                type="button"
                class="rounded-lg p-2 text-gray-500 transition-colors hover:bg-gray-100 hover:text-gray-700 dark:text-dark-300 dark:hover:bg-dark-700 dark:hover:text-white"
                :title="t('common.edit')"
                @click="openEditModal(row)"
              >
                <Icon name="edit" size="sm" />
              </button>
              <button
                type="button"
                class="rounded-lg p-2 text-gray-500 transition-colors hover:bg-gray-100 hover:text-gray-700 dark:text-dark-300 dark:hover:bg-dark-700 dark:hover:text-white"
                :disabled="togglingStatusId === row.id"
                :title="isAccountActive(row) ? t('userAccounts.disable') : t('userAccounts.enable')"
                @click="toggleAccountStatus(row)"
              >
                <Icon :name="isAccountActive(row) ? 'ban' : 'checkCircle'" size="sm" />
              </button>
              <button
                type="button"
                class="rounded-lg p-2 text-red-500 transition-colors hover:bg-red-50 hover:text-red-600 dark:hover:bg-red-900/20"
                :title="t('common.delete')"
                @click="openDeleteDialog(row)"
              >
                <Icon name="trash" size="sm" />
              </button>
            </div>
          </template>

          <template #empty>
            <EmptyState
              :title="t('userAccounts.noAccountsYet')"
              :description="t('userAccounts.createFirstAccount')"
              :action-text="t('userAccounts.createAccount')"
              @action="showCreateModal = true"
            />
          </template>
        </DataTable>
      </template>

      <template #pagination>
        <Pagination
          v-if="pagination.total > 0"
          :page="pagination.page"
          :total="pagination.total"
          :page-size="pagination.page_size"
          @update:page="handlePageChange"
          @update:pageSize="handlePageSizeChange"
        />
      </template>
    </TablePageLayout>

    <CreateAccountModal
      :show="showCreateModal"
      :proxies="[]"
      :groups="modalGroups"
      account-scope="user"
      :allow-proxy="false"
      :allow-billing-rate="false"
      @close="showCreateModal = false"
      @created="handleAccountCreated"
    />

    <EditAccountModal
      :show="showEditModal"
      :account="editingAccount"
      :proxies="[]"
      :groups="modalGroups"
      account-scope="user"
      :allow-proxy="false"
      :allow-billing-rate="false"
      @close="closeEditModal"
      @updated="handleAccountUpdated"
    />

    <BulkEditAccountModal
      :show="showBulkEditModal"
      :account-ids="selectedIds"
      :selected-platforms="selectedPlatforms"
      :selected-types="selectedTypes"
      :proxies="[]"
      :groups="modalGroups"
      account-scope="user"
      :allow-proxy="false"
      :allow-billing-rate="false"
      :allow-base-url="false"
      @close="showBulkEditModal = false"
      @updated="handleBulkAccountsUpdated"
    />

    <ConfirmDialog
      :show="showDeleteDialog"
      :title="t('userAccounts.deleteAccount')"
      :message="deleteConfirmMessage"
      :confirm-text="t('common.delete')"
      :cancel-text="t('common.cancel')"
      danger
      @confirm="deleteAccount"
      @cancel="closeDeleteDialog"
    />

    <ImportAccountsModal
      :show="showImportModal"
      @close="showImportModal = false"
      @imported="handleAccountsImported"
    />
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { accountsAPI, userGroupsAPI } from '@/api'
import { useAppStore } from '@/stores/app'
import { getPersistedPageSize } from '@/composables/usePersistedPageSize'
import { useTableSelection } from '@/composables/useTableSelection'
import AppLayout from '@/components/layout/AppLayout.vue'
import TablePageLayout from '@/components/layout/TablePageLayout.vue'
import DataTable from '@/components/common/DataTable.vue'
import Pagination from '@/components/common/Pagination.vue'
import ConfirmDialog from '@/components/common/ConfirmDialog.vue'
import EmptyState from '@/components/common/EmptyState.vue'
import Select from '@/components/common/Select.vue'
import SearchInput from '@/components/common/SearchInput.vue'
import Icon from '@/components/icons/Icon.vue'
import PlatformTypeBadge from '@/components/common/PlatformTypeBadge.vue'
import AccountCapacityCell from '@/components/account/AccountCapacityCell.vue'
import AccountStatusIndicator from '@/components/account/AccountStatusIndicator.vue'
import AccountGroupsCell from '@/components/account/AccountGroupsCell.vue'
import AccountUsageCell from '@/components/account/AccountUsageCell.vue'
import AccountTodayStatsCell from '@/components/account/AccountTodayStatsCell.vue'
import CreateAccountModal from '@/components/account/CreateAccountModal.vue'
import EditAccountModal from '@/components/account/EditAccountModal.vue'
import BulkEditAccountModal from '@/components/account/BulkEditAccountModal.vue'
import ImportAccountsModal from '@/components/user/ImportAccountsModal.vue'
import type { Account, AccountPlatform, AccountType, AdminGroup, Group, WindowStats } from '@/types'
import type { Column } from '@/components/common/types'
import { formatDateTime, formatRelativeTime } from '@/utils/format'

type UserAccountStatus = 'active' | 'disabled'

const { t } = useI18n()
const appStore = useAppStore()

const accounts = ref<Account[]>([])
const groups = ref<Group[]>([])
const loading = ref(false)
const showCreateModal = ref(false)
const showEditModal = ref(false)
const showImportModal = ref(false)
const showBulkEditModal = ref(false)
const showDeleteDialog = ref(false)
const editingAccount = ref<Account | null>(null)
const accountToDelete = ref<Account | null>(null)
const togglingStatusId = ref<number | null>(null)
const togglingSchedulableId = ref<number | null>(null)
const usageManualRefreshToken = ref(0)
const todayStatsByAccountId = ref<Record<string, WindowStats>>({})
const todayStatsLoading = ref(false)
const todayStatsError = ref<string | null>(null)
const todayStatsReqSeq = ref(0)
let abortController: AbortController | null = null

const pagination = ref({
  page: 1,
  page_size: getPersistedPageSize(),
  total: 0,
  pages: 0
})

const sortState = ref({
  sort_by: 'created_at',
  sort_order: 'desc' as 'asc' | 'desc'
})

const filterSearch = ref('')
const filterPlatform = ref('')
const filterType = ref('')
const filterStatus = ref('')
const filterGroupId = ref<string | number>('')

const modalGroups = computed(() => groups.value as unknown as AdminGroup[])

const {
  selectedIds,
  selectedCount,
  allVisibleSelected,
  isSelected,
  toggle: toggleSelection,
  clear: clearSelection,
  selectVisible,
  toggleVisible
} = useTableSelection<Account>({
  rows: accounts,
  getId: (account) => account.id
})

const selectedAccounts = computed(() => accounts.value.filter((account) => isSelected(account.id)))
const selectedPlatforms = computed<AccountPlatform[]>(() => [
  ...new Set(selectedAccounts.value.map((account) => account.platform))
])
const selectedTypes = computed<AccountType[]>(() => [
  ...new Set(selectedAccounts.value.map((account) => account.type))
])

const columns = computed<Column[]>(() => [
  { key: 'select', label: '', sortable: false, class: 'w-10' },
  { key: 'name', label: t('admin.accounts.columns.name'), sortable: true },
  { key: 'platform_type', label: t('admin.accounts.columns.platformType'), sortable: false, class: 'min-w-[150px]' },
  { key: 'share', label: t('userAccounts.share'), sortable: false },
  { key: 'capacity', label: t('admin.accounts.columns.capacity'), sortable: false },
  { key: 'status', label: t('admin.accounts.columns.status'), sortable: true },
  { key: 'schedulable', label: t('admin.accounts.columns.schedulable'), sortable: true },
  { key: 'today_stats', label: t('admin.accounts.columns.todayStats'), sortable: false },
  { key: 'groups', label: t('admin.accounts.columns.groups'), sortable: false },
  { key: 'usage', label: t('admin.accounts.columns.usageWindows'), sortable: false, class: 'min-w-[180px]' },
  { key: 'priority', label: t('admin.accounts.columns.priority'), sortable: true },
  { key: 'last_used_at', label: t('admin.accounts.columns.lastUsed'), sortable: true },
  { key: 'expires_at', label: t('admin.accounts.columns.expiresAt'), sortable: true },
  { key: 'notes', label: t('admin.accounts.columns.notes'), sortable: false },
  { key: 'actions', label: t('admin.accounts.columns.actions'), sortable: false }
])

const platformOptions = computed<Array<{ value: AccountPlatform; label: string }>>(() => [
  { value: 'anthropic', label: 'Anthropic' },
  { value: 'openai', label: 'OpenAI' },
  { value: 'gemini', label: 'Gemini' },
  { value: 'antigravity', label: 'Antigravity' }
])

const typeOptions = computed<Array<{ value: AccountType; label: string }>>(() => [
  { value: 'oauth', label: 'OAuth' },
  { value: 'setup-token', label: 'Setup Token' },
  { value: 'apikey', label: 'API Key' },
  { value: 'upstream', label: 'Upstream' },
  { value: 'bedrock', label: 'Bedrock' }
])

const platformFilterOptions = computed(() => [
  { value: '', label: t('userAccounts.allPlatforms') },
  ...platformOptions.value
])

const typeFilterOptions = computed(() => [
  { value: '', label: t('userAccounts.allTypes') },
  ...typeOptions.value
])

const statusFilterOptions = computed(() => [
  { value: '', label: t('userAccounts.allStatus') },
  { value: 'active', label: t('userAccounts.status.active') },
  { value: 'disabled', label: t('userAccounts.status.disabled') },
  { value: 'error', label: t('userAccounts.status.error') }
])

const groupFilterOptions = computed(() => [
  { value: '', label: t('keys.allGroups') },
  { value: -1, label: t('keys.noGroup') },
  ...groups.value.map((group) => ({
    value: group.id,
    label: group.name
  }))
])

const deleteConfirmMessage = computed(() =>
  t('userAccounts.deleteConfirmMessage', { name: accountToDelete.value?.name ?? '' })
)

function isAccountActive(account: Account): boolean {
  return account.status === 'active'
}

function buildDefaultTodayStats(): WindowStats {
  return {
    requests: 0,
    tokens: 0,
    cost: 0,
    standard_cost: 0,
    user_cost: 0
  }
}

async function refreshTodayStatsBatch(): Promise<void> {
  const accountIDs = accounts.value.map((account) => account.id)
  const reqSeq = ++todayStatsReqSeq.value
  if (accountIDs.length === 0) {
    todayStatsByAccountId.value = {}
    todayStatsError.value = null
    todayStatsLoading.value = false
    return
  }

  todayStatsLoading.value = true
  todayStatsError.value = null

  try {
    const result = await accountsAPI.getBatchTodayStats(accountIDs)
    if (reqSeq !== todayStatsReqSeq.value) return
    const serverStats = result.stats ?? {}
    const nextStats: Record<string, WindowStats> = {}
    for (const accountID of accountIDs) {
      const key = String(accountID)
      nextStats[key] = serverStats[key] ?? buildDefaultTodayStats()
    }
    todayStatsByAccountId.value = nextStats
  } catch (error) {
    if (reqSeq !== todayStatsReqSeq.value) return
    todayStatsError.value = 'Failed'
    console.error('Failed to load user account today stats:', error)
  } finally {
    if (reqSeq === todayStatsReqSeq.value) {
      todayStatsLoading.value = false
    }
  }
}

function getAntigravityTierFromRow(row: Account): string | null {
  if (row.platform !== 'antigravity') return null
  const loadCodeAssist = row.extra?.load_code_assist
  if (!loadCodeAssist || typeof loadCodeAssist !== 'object') return null
  const lca = loadCodeAssist as Record<string, unknown>
  const paid = lca.paidTier
  if (paid && typeof paid === 'object' && typeof (paid as Record<string, unknown>).id === 'string') {
    return (paid as Record<string, string>).id
  }
  const current = lca.currentTier
  if (current && typeof current === 'object' && typeof (current as Record<string, unknown>).id === 'string') {
    return (current as Record<string, string>).id
  }
  return null
}

function getAntigravityTierLabel(row: Account): string | null {
  const tier = getAntigravityTierFromRow(row)
  switch (tier) {
    case 'free-tier':
      return t('admin.accounts.tier.free')
    case 'g1-pro-tier':
      return t('admin.accounts.tier.pro')
    case 'g1-ultra-tier':
      return t('admin.accounts.tier.ultra')
    default:
      return null
  }
}

function getAntigravityTierClass(row: Account): string {
  const tier = getAntigravityTierFromRow(row)
  switch (tier) {
    case 'free-tier':
      return 'bg-gray-100 text-gray-600 dark:bg-gray-700 dark:text-gray-300'
    case 'g1-pro-tier':
      return 'bg-blue-100 text-blue-600 dark:bg-blue-900/40 dark:text-blue-300'
    case 'g1-ultra-tier':
      return 'bg-purple-100 text-purple-600 dark:bg-purple-900/40 dark:text-purple-300'
    default:
      return ''
  }
}

function getOpenAICompactState(row: Account): 'supported' | 'unsupported' | 'unknown' | null {
  if (row.platform !== 'openai' || (row.type !== 'oauth' && row.type !== 'apikey')) return null
  const mode = typeof row.extra?.openai_compact_mode === 'string' ? row.extra.openai_compact_mode : 'auto'
  if (mode === 'force_on') return 'supported'
  if (mode === 'force_off') return 'unsupported'
  if (typeof row.extra?.openai_compact_supported === 'boolean') {
    return row.extra.openai_compact_supported ? 'supported' : 'unsupported'
  }
  return 'unknown'
}

function getOpenAICompactLabel(row: Account): string | null {
  switch (getOpenAICompactState(row)) {
    case 'supported':
      return t('admin.accounts.openai.compactSupported')
    case 'unsupported':
      return t('admin.accounts.openai.compactUnsupported')
    case 'unknown':
      return t('admin.accounts.openai.compactUnknown')
    default:
      return null
  }
}

function getOpenAICompactClass(row: Account): string {
  switch (getOpenAICompactState(row)) {
    case 'supported':
      return 'bg-emerald-100 text-emerald-700 dark:bg-emerald-900/40 dark:text-emerald-300'
    case 'unsupported':
      return 'bg-rose-100 text-rose-700 dark:bg-rose-900/40 dark:text-rose-300'
    case 'unknown':
      return 'bg-amber-100 text-amber-700 dark:bg-amber-900/40 dark:text-amber-300'
    default:
      return ''
  }
}

function getOpenAICompactTitle(row: Account): string {
  const checkedAt = typeof row.extra?.openai_compact_checked_at === 'string' ? row.extra.openai_compact_checked_at : ''
  if (!checkedAt) return getOpenAICompactLabel(row) || ''
  return `${getOpenAICompactLabel(row)} | ${t('admin.accounts.openai.compactLastChecked')}: ${formatDateTime(new Date(checkedAt))}`
}

function shareModeLabel(mode?: string): string {
  return mode === 'public' ? t('userAccounts.publicMode') : t('userAccounts.privateMode')
}

function shareStatusLabel(status?: string): string {
  switch (status) {
    case 'pending':
      return t('userAccounts.pendingReview')
    case 'suspended':
      return t('userAccounts.suspended')
    default:
      return t('userAccounts.approved')
  }
}

function shareModeBadgeClass(mode?: string): string {
  const base = 'inline-flex w-fit rounded-md px-2 py-0.5 text-xs font-medium'
  return mode === 'public'
    ? `${base} bg-blue-50 text-blue-700 dark:bg-blue-900/30 dark:text-blue-300`
    : `${base} bg-gray-100 text-gray-700 dark:bg-dark-700 dark:text-dark-200`
}

function shareStatusBadgeClass(status?: string): string {
  const base = 'inline-flex w-fit rounded-md px-2 py-0.5 text-xs font-medium'
  switch (status) {
    case 'pending':
      return `${base} bg-amber-50 text-amber-700 dark:bg-amber-900/30 dark:text-amber-300`
    case 'suspended':
      return `${base} bg-red-50 text-red-700 dark:bg-red-900/30 dark:text-red-300`
    default:
      return `${base} bg-emerald-50 text-emerald-700 dark:bg-emerald-900/30 dark:text-emerald-300`
  }
}

function isAbortError(error: unknown): boolean {
  if (!error || typeof error !== 'object') return false
  const { name, code } = error as { name?: string; code?: string }
  return name === 'AbortError' || code === 'ERR_CANCELED'
}

async function loadAccounts(): Promise<void> {
  abortController?.abort()
  const controller = new AbortController()
  abortController = controller
  const { signal } = controller
  loading.value = true

  try {
    const filters: {
      search?: string
      platform?: string
      type?: string
      status?: string
      group_id?: string | number
      sort_by: string
      sort_order: 'asc' | 'desc'
    } = {
      sort_by: sortState.value.sort_by,
      sort_order: sortState.value.sort_order
    }
    if (filterSearch.value.trim()) filters.search = filterSearch.value.trim()
    if (filterPlatform.value) filters.platform = filterPlatform.value
    if (filterType.value) filters.type = filterType.value
    if (filterStatus.value) filters.status = filterStatus.value
    if (filterGroupId.value !== '') filters.group_id = filterGroupId.value

    const response = await accountsAPI.list(
      pagination.value.page,
      pagination.value.page_size,
      filters,
      { signal }
    )
    if (signal.aborted) return
    accounts.value = response.items
    pagination.value.total = response.total
    pagination.value.pages = response.pages
    await refreshTodayStatsBatch()
  } catch (error) {
    if (!isAbortError(error)) {
      console.error('Failed to load user accounts:', error)
      appStore.showError(t('userAccounts.failedToLoad'))
    }
  } finally {
    if (!signal.aborted) {
      loading.value = false
    }
  }
}

async function loadGroups(): Promise<void> {
  try {
    groups.value = await userGroupsAPI.getAvailable()
  } catch (error) {
    console.error('Failed to load available groups:', error)
  }
}

function onFilterChange(): void {
  pagination.value.page = 1
  loadAccounts()
}

function onPlatformFilterChange(value: string | number | boolean | null): void {
  filterPlatform.value = String(value ?? '')
  onFilterChange()
}

function onTypeFilterChange(value: string | number | boolean | null): void {
  filterType.value = String(value ?? '')
  onFilterChange()
}

function onStatusFilterChange(value: string | number | boolean | null): void {
  filterStatus.value = String(value ?? '')
  onFilterChange()
}

function onGroupFilterChange(value: string | number | boolean | null): void {
  filterGroupId.value = value === null || typeof value === 'boolean' ? '' : value
  onFilterChange()
}

function toggleSelectAllVisible(event: Event): void {
  toggleVisible((event.target as HTMLInputElement).checked)
}

function handleSort(key: string, order: 'asc' | 'desc'): void {
  sortState.value.sort_by = key
  sortState.value.sort_order = order
  pagination.value.page = 1
  loadAccounts()
}

function handlePageChange(page: number): void {
  pagination.value.page = page
  loadAccounts()
}

function handlePageSizeChange(pageSize: number): void {
  pagination.value.page_size = pageSize
  pagination.value.page = 1
  loadAccounts()
}

function openEditModal(account: Account): void {
  editingAccount.value = account
  showEditModal.value = true
}

function closeEditModal(): void {
  showEditModal.value = false
  editingAccount.value = null
}

function openBulkEditModal(): void {
  if (selectedCount.value === 0) {
    appStore.showError(t('admin.accounts.bulkEdit.noSelection'))
    return
  }
  showBulkEditModal.value = true
}

async function handleAccountCreated(): Promise<void> {
  showCreateModal.value = false
  clearSelection()
  pagination.value.page = 1
  usageManualRefreshToken.value += 1
  await Promise.all([loadGroups(), loadAccounts()])
}

async function handleAccountUpdated(account: Account): Promise<void> {
  showEditModal.value = false
  editingAccount.value = null
  patchAccountInList(account)
  usageManualRefreshToken.value += 1
  await loadAccounts()
}

async function handleBulkAccountsUpdated(): Promise<void> {
  showBulkEditModal.value = false
  clearSelection()
  usageManualRefreshToken.value += 1
  await Promise.all([loadGroups(), loadAccounts()])
}

async function handleAccountsImported(payload?: { close: boolean }): Promise<void> {
  if (payload?.close !== false) {
    showImportModal.value = false
  }
  clearSelection()
  pagination.value.page = 1
  usageManualRefreshToken.value += 1
  await loadAccounts()
}

function openDeleteDialog(account: Account): void {
  accountToDelete.value = account
  showDeleteDialog.value = true
}

function closeDeleteDialog(): void {
  showDeleteDialog.value = false
  accountToDelete.value = null
}

function patchAccountInList(account: Account): void {
  accounts.value = accounts.value.map((item) => (item.id === account.id ? account : item))
}

async function toggleAccountStatus(account: Account): Promise<void> {
  togglingStatusId.value = account.id
  try {
    const nextStatus: UserAccountStatus = isAccountActive(account) ? 'disabled' : 'active'
    const updated = await accountsAPI.toggleStatus(account.id, nextStatus)
    patchAccountInList(updated)
    await refreshTodayStatsBatch()
    appStore.showSuccess(
      nextStatus === 'active'
        ? t('userAccounts.accountEnabledSuccess')
        : t('userAccounts.accountDisabledSuccess')
    )
  } catch (error) {
    console.error('Failed to toggle user account status:', error)
    appStore.showError(t('userAccounts.failedToUpdateStatus'))
  } finally {
    togglingStatusId.value = null
  }
}

async function toggleSchedulable(account: Account): Promise<void> {
  togglingSchedulableId.value = account.id
  try {
    const updated = await accountsAPI.update(account.id, { schedulable: !account.schedulable })
    patchAccountInList(updated)
    await refreshTodayStatsBatch()
  } catch (error) {
    console.error('Failed to toggle user account schedulable:', error)
    appStore.showError(t('admin.accounts.failedToToggleSchedulable'))
  } finally {
    togglingSchedulableId.value = null
  }
}

async function deleteAccount(): Promise<void> {
  if (!accountToDelete.value) return
  try {
    await accountsAPI.delete(accountToDelete.value.id)
    appStore.showSuccess(t('userAccounts.accountDeletedSuccess'))
    closeDeleteDialog()
    clearSelection()
    await loadAccounts()
  } catch (error: any) {
    console.error('Failed to delete user account:', error)
    appStore.showError(error?.response?.data?.message || t('userAccounts.failedToDelete'))
  }
}

function formatExpiresAt(value: number | null): string {
  if (!value) return '-'
  return formatDateTime(
    new Date(value * 1000),
    {
      year: 'numeric',
      month: '2-digit',
      day: '2-digit',
      hour: '2-digit',
      minute: '2-digit',
      hour12: false
    },
    'sv-SE'
  )
}

function isExpired(value: number | null): boolean {
  if (!value) return false
  return value * 1000 <= Date.now()
}

function handleShowTempUnsched(_account: Account): void {
  appStore.showInfo(t('admin.accounts.status.viewTempUnschedDetails'))
}

onMounted(async () => {
  await Promise.all([loadGroups(), loadAccounts()])
})

onUnmounted(() => {
  abortController?.abort()
})
</script>
