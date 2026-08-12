<template>
  <AppLayout>
    <div class="space-y-6">
      <div class="card p-2">
        <div class="flex flex-wrap gap-2">
          <button
            type="button"
            class="rounded-md px-4 py-2 text-sm font-medium transition-colors"
            :class="activeAdminUsageTab === 'requests'
              ? 'bg-primary-600 text-white shadow-sm'
              : 'text-gray-600 hover:bg-gray-100 dark:text-gray-300 dark:hover:bg-dark-700'"
            @click="switchAdminUsageTab('requests')"
          >
            {{ t('usage.tabs.requests') }}
          </button>
          <button
            type="button"
            class="rounded-md px-4 py-2 text-sm font-medium transition-colors"
            :class="activeAdminUsageTab === 'balanceLedger'
              ? 'bg-primary-600 text-white shadow-sm'
              : 'text-gray-600 hover:bg-gray-100 dark:text-gray-300 dark:hover:bg-dark-700'"
            @click="switchAdminUsageTab('balanceLedger')"
          >
            {{ t('usage.tabs.balanceLedger') }}
          </button>
        </div>
      </div>

      <template v-if="activeAdminUsageTab === 'requests'">
      <UsageStatsCards :stats="usageStats" />
      <!-- Charts Section -->
      <div class="space-y-4">
        <div class="card p-4">
          <div class="flex flex-wrap items-center gap-4">
            <div class="flex items-center gap-2">
              <span class="text-sm font-medium text-gray-700 dark:text-gray-300">{{ t('admin.dashboard.timeRange') }}:</span>
              <DateRangePicker
                v-model:start-date="startDate"
                v-model:end-date="endDate"
                @change="onDateRangeChange"
              />
            </div>
            <div class="flex flex-wrap items-end gap-2">
              <div class="w-full sm:w-[220px]">
                <label class="input-label">{{ t('usage.startTime') }}</label>
                <input
                  v-model="startDateTime"
                  type="datetime-local"
                  step="1"
                  class="input"
                  @change="onExactTimeRangeChange"
                />
              </div>
              <div class="w-full sm:w-[220px]">
                <label class="input-label">{{ t('usage.endTime') }}</label>
                <input
                  v-model="endDateTime"
                  type="datetime-local"
                  step="1"
                  class="input"
                  @change="onExactTimeRangeChange"
                />
              </div>
            </div>
            <div class="ml-auto flex items-center gap-2">
              <span class="text-sm font-medium text-gray-700 dark:text-gray-300">{{ t('admin.dashboard.granularity') }}:</span>
              <div class="w-28">
                <Select v-model="granularity" :options="granularityOptions" @change="loadChartData" />
              </div>
            </div>
          </div>
        </div>
        <div class="grid grid-cols-1 gap-6 lg:grid-cols-2">
          <ModelDistributionChart
            v-model:source="modelDistributionSource"
            v-model:metric="modelDistributionMetric"
            :model-stats="requestedModelStats"
            :upstream-model-stats="upstreamModelStats"
            :mapping-model-stats="mappingModelStats"
            :loading="modelStatsLoading"
            :show-source-toggle="true"
            :show-metric-toggle="true"
            :start-date="startDate"
            :end-date="endDate"
            :filters="breakdownFilters"
          />
          <GroupDistributionChart
            v-model:metric="groupDistributionMetric"
            :group-stats="groupStats"
            :loading="chartsLoading"
            :show-metric-toggle="true"
            :start-date="startDate"
            :end-date="endDate"
            :filters="breakdownFilters"
          />
        </div>
        <div class="grid grid-cols-1 gap-6 lg:grid-cols-2">
          <EndpointDistributionChart
            v-model:source="endpointDistributionSource"
            v-model:metric="endpointDistributionMetric"
            :endpoint-stats="inboundEndpointStats"
            :upstream-endpoint-stats="upstreamEndpointStats"
            :endpoint-path-stats="endpointPathStats"
            :loading="endpointStatsLoading"
            :show-source-toggle="true"
            :show-metric-toggle="true"
            :title="t('usage.endpointDistribution')"
            :start-date="startDate"
            :end-date="endDate"
            :filters="breakdownFilters"
          />
          <TokenUsageTrend :trend-data="trendData" :loading="chartsLoading" />
        </div>
      </div>
      <div class="card overflow-hidden">
        <div
          class="flex overflow-x-auto border-b border-gray-200 px-2 dark:border-dark-700 sm:px-4"
          role="tablist"
          :aria-label="t('admin.usage.title')"
        >
          <button
            v-for="tab in detailTabs"
            :key="tab.key"
            :id="getDetailTabId(tab.key)"
            type="button"
            role="tab"
            class="-mb-px inline-flex min-h-11 min-w-11 shrink-0 items-center gap-1.5 border-b-2 px-3 py-3 text-sm font-medium transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-primary-500 sm:px-4"
            :aria-controls="getDetailPanelId(tab.key)"
            :aria-selected="activeDetailTab === tab.key"
            :tabindex="activeDetailTab === tab.key ? 0 : -1"
            :class="activeDetailTab === tab.key
              ? 'border-primary-500 text-primary-600 dark:text-primary-400'
              : 'border-transparent text-gray-500 hover:border-gray-300 hover:text-gray-700 dark:text-gray-400 dark:hover:border-dark-500 dark:hover:text-gray-200'"
            @click="switchDetailTab(tab.key)"
            @keydown="handleDetailTabKeydown($event, tab.key)"
          >
            <Icon :name="tab.icon" size="sm" />
            {{ tab.label }}
          </button>
        </div>

        <UsageFilters
          ref="usageFiltersRef"
          v-model="filters"
          flat
          :mode="activeDetailTab"
          class="border-b border-gray-100 dark:border-dark-700/50"
          :start-date="startDate"
          :end-date="endDate"
          :exporting="exporting"
          @change="applyFilters"
          @refresh="refreshData"
          @reset="resetFilters"
          @cleanup="openCleanupDialog"
          @export="exportToExcel"
        >
          <template #after-reset>
          <div v-if="activeDetailTab === 'usage'" class="relative" ref="columnDropdownRef">
            <button
              @click="showColumnDropdown = !showColumnDropdown"
              class="btn btn-secondary px-2 md:px-3"
              :title="t('admin.users.columnSettings')"
            >
              <svg class="h-4 w-4 md:mr-1.5" fill="none" stroke="currentColor" viewBox="0 0 24 24" stroke-width="1.5">
                <path stroke-linecap="round" stroke-linejoin="round" d="M9 4.5v15m6-15v15m-10.875 0h15.75c.621 0 1.125-.504 1.125-1.125V5.625c0-.621-.504-1.125-1.125-1.125H4.125C3.504 4.5 3 5.004 3 5.625v12.75c0 .621.504 1.125 1.125 1.125z" />
              </svg>
              <span class="hidden md:inline">{{ t('admin.users.columnSettings') }}</span>
            </button>
            <div
              v-if="showColumnDropdown"
              class="absolute right-0 top-full z-50 mt-1 max-h-80 w-48 overflow-y-auto rounded-lg border border-gray-200 bg-white py-1 shadow-lg dark:border-dark-600 dark:bg-dark-800"
            >
              <button
                v-for="col in toggleableColumns"
                :key="col.key"
                @click="toggleColumn(col.key)"
                class="flex w-full items-center justify-between px-4 py-2 text-left text-sm text-gray-700 hover:bg-gray-100 dark:text-gray-300 dark:hover:bg-dark-700"
              >
                <span>{{ col.label }}</span>
                <Icon
                  v-if="isColumnVisible(col.key)"
                  name="check"
                  size="sm"
                  class="text-primary-500"
                  :stroke-width="2"
                />
              </button>
            </div>
          </div>
          </template>
        </UsageFilters>

        <div
          v-show="activeDetailTab === 'usage'"
          :id="getDetailPanelId('usage')"
          role="tabpanel"
          :aria-labelledby="getDetailTabId('usage')"
        >
          <UsageTable
            flat
            :data="usageLogs"
            :loading="loading"
            :columns="visibleColumns"
            :server-side-sort="true"
            :default-sort-key="'created_at'"
            :default-sort-order="'desc'"
            @sort="handleSort"
            @userClick="handleUserClick"
          />
          <Pagination v-if="pagination.total > 0" :page="pagination.page" :total="pagination.total" :page-size="pagination.page_size" @update:page="handlePageChange" @update:pageSize="handlePageSizeChange" />
        </div>

        <div
          v-show="activeDetailTab === 'errors'"
          :id="getDetailPanelId('errors')"
          role="tabpanel"
          :aria-labelledby="getDetailTabId('errors')"
          class="min-h-[16rem] overflow-hidden"
        >
          <div
            v-if="errorMessage"
            class="m-4 rounded-lg border border-red-200 bg-red-50 px-4 py-3 text-sm text-red-700 dark:border-red-900/50 dark:bg-red-950/30 dark:text-red-300 sm:m-6"
            role="alert"
          >
            <div class="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
              <span>{{ errorMessage }}</span>
              <button
                type="button"
                class="btn btn-secondary min-h-11 min-w-11 shrink-0"
                :disabled="errorLoading"
                @click="loadAdminErrors"
              >
                {{ t('usage.errors.retry') }}
              </button>
            </div>
          </div>
          <OpsErrorLogTable
            v-else
            :rows="errorRows"
            :total="errorTotal"
            :loading="errorLoading"
            :page="errorPage"
            :page-size="errorPageSize"
            @openErrorDetail="openErrorDetail"
            @update:page="handleErrorPageChange"
            @update:pageSize="handleErrorPageSizeChange"
          />
        </div>

        <div
          v-show="activeDetailTab === 'ranking'"
          :id="getDetailPanelId('ranking')"
          role="tabpanel"
          :aria-labelledby="getDetailTabId('ranking')"
        >
          <UserTokenRanking
            v-if="rankingMounted"
            ref="rankingRef"
            :start-date="startDate"
            :end-date="endDate"
            :filters="breakdownFilters"
            :model="filters.model"
            @select-user="handleRankingSelectUser"
          />
        </div>
      </div>
      </template>

      <template v-else>
        <div class="grid grid-cols-1 gap-4 sm:grid-cols-2 xl:grid-cols-4">
          <div
            v-for="card in ledgerSummaryCards"
            :key="card.key"
            class="card p-5"
          >
            <div class="flex items-center gap-4">
              <div :class="['flex h-10 w-10 shrink-0 items-center justify-center rounded-lg', card.iconClass]">
                <Icon :name="card.icon" size="md" :stroke-width="2" />
              </div>
              <div class="min-w-0">
                <div class="text-sm text-gray-500 dark:text-gray-400">{{ card.label }}</div>
                <div :class="['mt-1 truncate text-2xl font-bold', card.valueClass]">
                  {{ ledgerStatsLoading ? '...' : card.value }}
                </div>
                <div class="mt-1 text-xs text-gray-400 dark:text-gray-500">{{ t('usage.inSelectedRange') }}</div>
              </div>
            </div>
          </div>
        </div>

        <div class="card p-6">
          <div class="flex flex-wrap items-end justify-between gap-4">
            <div class="flex flex-1 flex-wrap items-end gap-4">
              <div ref="ledgerUserSearchRef" class="relative w-full sm:w-auto sm:min-w-[260px]">
                <label class="input-label">{{ t('admin.usage.userFilter') }}</label>
                <input
                  v-model="ledgerUserKeyword"
                  type="text"
                  class="input pr-8"
                  :placeholder="t('admin.usage.searchUserPlaceholder')"
                  @input="debounceLedgerUserSearch"
                  @focus="showLedgerUserDropdown = true"
                />
                <button
                  v-if="ledgerFilters.user_id"
                  type="button"
                  class="absolute right-2 top-9 text-gray-400 hover:text-gray-600 dark:hover:text-gray-200"
                  aria-label="Clear user filter"
                  @click="clearLedgerUser"
                >
                  ×
                </button>
                <div
                  v-if="showLedgerUserDropdown && (ledgerUserResults.length > 0 || ledgerUserKeyword)"
                  class="absolute z-50 mt-1 max-h-60 w-full overflow-auto rounded-lg border border-gray-200 bg-white py-1 shadow-lg dark:border-dark-600 dark:bg-dark-800"
                >
                  <button
                    v-for="u in ledgerUserResults"
                    :key="u.id"
                    type="button"
                    class="flex w-full items-center justify-between gap-3 px-4 py-2 text-left text-sm text-gray-700 hover:bg-gray-100 dark:text-gray-200 dark:hover:bg-dark-700"
                    @click="selectLedgerUser(u)"
                  >
                    <span class="truncate">{{ u.email }}</span>
                    <span class="shrink-0 text-xs text-gray-400">#{{ u.id }}</span>
                  </button>
                  <div
                    v-if="ledgerUserResults.length === 0 && ledgerUserKeyword"
                    class="px-4 py-2 text-sm text-gray-500 dark:text-gray-400"
                  >
                    {{ t('empty.noData') }}
                  </div>
                </div>
              </div>

              <div class="w-full sm:w-auto">
                <label class="input-label">{{ t('usage.timeRange') }}</label>
                <DateRangePicker
                  v-model:start-date="startDate"
                  v-model:end-date="endDate"
                  @change="onDateRangeChange"
                />
              </div>

              <div class="w-full sm:w-[220px]">
                <label class="input-label">{{ t('usage.startTime') }}</label>
                <input
                  v-model="startDateTime"
                  type="datetime-local"
                  step="1"
                  class="input"
                  @change="onExactTimeRangeChange"
                />
              </div>

              <div class="w-full sm:w-[220px]">
                <label class="input-label">{{ t('usage.endTime') }}</label>
                <input
                  v-model="endDateTime"
                  type="datetime-local"
                  step="1"
                  class="input"
                  @change="onExactTimeRangeChange"
                />
              </div>

              <div class="w-full sm:w-auto sm:min-w-[150px]">
                <label class="input-label">{{ t('usage.balanceLedger.direction') }}</label>
                <Select v-model="ledgerFilters.direction" :options="ledgerDirectionOptions" @change="applyLedgerFilters" />
              </div>

              <div class="w-full sm:w-auto sm:min-w-[220px]">
                <label class="input-label">{{ t('usage.balanceLedger.reason') }}</label>
                <Select v-model="ledgerFilters.reason" :options="ledgerReasonOptions" @change="applyLedgerFilters" />
              </div>

              <div class="w-full sm:w-auto sm:min-w-[180px]">
                <label class="input-label">{{ t('usage.balanceLedger.labels.referenceType') }}</label>
                <input
                  v-model.trim="ledgerFilters.ref_type"
                  type="text"
                  class="input"
                  placeholder="usage_log"
                  @keyup.enter="applyLedgerFilters"
                />
              </div>

              <div class="w-full sm:w-auto sm:min-w-[160px]">
                <label class="input-label">{{ t('usage.balanceLedger.labels.reference') }}</label>
                <input
                  v-model.number="ledgerFilters.ref_id"
                  type="number"
                  min="1"
                  step="1"
                  class="input"
                  placeholder="ID"
                  @keyup.enter="applyLedgerFilters"
                />
              </div>
            </div>

            <div class="flex w-full flex-wrap items-center justify-end gap-3 sm:w-auto">
              <button type="button" class="btn btn-secondary" :disabled="ledgerLoading" @click="refreshLedgerData">
                {{ t('common.refresh') }}
              </button>
              <button type="button" class="btn btn-secondary" @click="resetLedgerFilters">
                {{ t('common.reset') }}
              </button>
            </div>
          </div>
        </div>

        <DataTable
          :columns="ledgerColumns"
          :data="balanceLedgerRows"
          :loading="ledgerLoading"
          :server-side-sort="true"
          default-sort-key="created_at"
          default-sort-order="desc"
          row-key="id"
          :estimate-row-height="72"
          :overscan="8"
          @sort="handleLedgerSort"
        >
          <template #cell-user="{ row }">
            <button
              type="button"
              class="text-left text-sm font-medium text-primary-600 hover:text-primary-700 dark:text-primary-400 dark:hover:text-primary-300"
              :title="t('admin.usage.clickToViewBalance')"
              @click="handleUserClick(row.user_id)"
            >
              <span class="block max-w-[220px] truncate">{{ row.user?.email || `#${row.user_id}` }}</span>
              <span class="block text-xs font-normal text-gray-400">#{{ row.user_id }}</span>
            </button>
          </template>

          <template #cell-reason="{ row }">
            <span class="inline-flex items-center rounded px-2 py-0.5 text-xs font-medium bg-gray-100 text-gray-700 dark:bg-dark-700 dark:text-gray-200">
              {{ row.reasonLabel }}
            </span>
          </template>

          <template #cell-amount="{ row }">
            <span
              class="font-semibold"
              :class="row.direction === 'credit'
                ? 'text-emerald-600 dark:text-emerald-400'
                : 'text-rose-600 dark:text-rose-400'"
            >
              {{ row.amountLabel }}
            </span>
          </template>

          <template #cell-balance_after="{ row }">
            <span class="font-medium text-gray-900 dark:text-gray-100">{{ row.balanceAfterLabel }}</span>
          </template>

          <template #cell-ref="{ row }">
            <div class="text-sm text-gray-600 dark:text-gray-300">
              <div>{{ row.ref_type || '-' }}</div>
              <div class="text-xs text-gray-400">#{{ row.ref_id || '-' }}</div>
            </div>
          </template>

          <template #cell-remark="{ row }">
            <span class="block max-w-[520px] whitespace-normal break-words text-sm text-gray-600 dark:text-gray-300">
              {{ row.remarkText }}
            </span>
          </template>

          <template #cell-created_at="{ value }">
            <span class="text-sm text-gray-600 dark:text-gray-300">{{ formatDateTime(value) }}</span>
          </template>
        </DataTable>

        <Pagination
          v-if="ledgerPagination.total > 0"
          :page="ledgerPagination.page"
          :total="ledgerPagination.total"
          :page-size="ledgerPagination.page_size"
          @update:page="handleLedgerPageChange"
          @update:pageSize="handleLedgerPageSizeChange"
        />
      </template>
    </div>
  </AppLayout>
  <OpsErrorDetailModal
    v-model:show="showErrorModal"
    :error-id="selectedErrorId"
    error-type="request"
  />
  <UsageExportProgress :show="exportProgress.show" :progress="exportProgress.progress" :current="exportProgress.current" :total="exportProgress.total" :estimated-time="exportProgress.estimatedTime" @cancel="cancelExport" />
  <UsageCleanupDialog
    :show="cleanupDialogVisible"
    :filters="filters"
    :start-date="startDate"
    :end-date="endDate"
    @close="cleanupDialogVisible = false"
  />
  <!-- Balance history modal triggered from usage table user click -->
  <UserBalanceHistoryModal
    :show="showBalanceHistoryModal"
    :user="balanceHistoryUser"
    :hide-actions="true"
    @close="showBalanceHistoryModal = false; balanceHistoryUser = null"
  />
</template>

<script setup lang="ts">
import { ref, reactive, computed, onMounted, onUnmounted, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { saveAs } from 'file-saver'
import { useRoute } from 'vue-router'
import { useAppStore } from '@/stores/app'; import { adminAPI } from '@/api/admin'; import { adminUsageAPI } from '@/api/admin/usage'
import { getPersistedPageSize } from '@/composables/usePersistedPageSize'
import { formatDateTime, formatReasoningEffort } from '@/utils/format'
import { formatCacheHitRate } from '@/utils/formatters'
import { extractApiErrorMessage } from '@/utils/apiError'
import { resolveUsageRequestType, requestTypeToLegacyStream } from '@/utils/usageRequestType'
import AppLayout from '@/components/layout/AppLayout.vue'; import Pagination from '@/components/common/Pagination.vue'; import Select from '@/components/common/Select.vue'; import DateRangePicker from '@/components/common/DateRangePicker.vue'; import DataTable from '@/components/common/DataTable.vue'
import UsageStatsCards from '@/components/admin/usage/UsageStatsCards.vue'; import UsageFilters from '@/components/admin/usage/UsageFilters.vue'
import UsageTable from '@/components/admin/usage/UsageTable.vue'; import UsageExportProgress from '@/components/admin/usage/UsageExportProgress.vue'
import UserTokenRanking from '@/components/admin/usage/UserTokenRanking.vue'
import UsageCleanupDialog from '@/components/admin/usage/UsageCleanupDialog.vue'
import UserBalanceHistoryModal from '@/components/admin/user/UserBalanceHistoryModal.vue'
import OpsErrorLogTable from '@/views/admin/ops/components/OpsErrorLogTable.vue'
import OpsErrorDetailModal from '@/views/admin/ops/components/OpsErrorDetailModal.vue'
import { listRequestErrors } from '@/api/admin/ops'
import type { OpsErrorLog, OpsErrorListQueryParams } from '@/api/admin/ops'
import ModelDistributionChart from '@/components/charts/ModelDistributionChart.vue'; import GroupDistributionChart from '@/components/charts/GroupDistributionChart.vue'; import TokenUsageTrend from '@/components/charts/TokenUsageTrend.vue'
import EndpointDistributionChart from '@/components/charts/EndpointDistributionChart.vue'
import Icon from '@/components/icons/Icon.vue'
import type { AdminUsageLog, TrendDataPoint, ModelStat, GroupStat, EndpointStat, AdminUser, UserBalanceLedgerEntry } from '@/types'; import type { AdminUsageStatsResponse, AdminUsageQueryParams, AdminBalanceLedgerQueryParams, AdminBalanceLedgerStatsResponse, SimpleUser } from '@/api/admin/usage'
import type { Column } from '@/components/common/types'

const { t } = useI18n()
const appStore = useAppStore()
type DistributionMetric = 'tokens' | 'actual_cost'
type EndpointSource = 'inbound' | 'upstream' | 'path'
type ModelDistributionSource = 'requested' | 'upstream' | 'mapping'
type AdminUsageTab = 'requests' | 'balanceLedger'
type DetailTab = 'usage' | 'errors' | 'ranking'
type UsageDetailFilters = AdminUsageQueryParams & {
  error_phase?: string
  status_code?: number
}
type BalanceLedgerTableRow = UserBalanceLedgerEntry & {
  reasonLabel: string
  amountLabel: string
  balanceAfterLabel: string
  remarkText: string
}
const route = useRoute()
const usageStats = ref<AdminUsageStatsResponse | null>(null); const usageLogs = ref<AdminUsageLog[]>([]); const loading = ref(false); const exporting = ref(false)
const activeAdminUsageTab = ref<AdminUsageTab>('requests')
const activeDetailTab = ref<DetailTab>('usage')
const detailTabs = computed(() => [
  { key: 'usage' as const, label: t('usage.tabs.usage'), icon: 'document' as const },
  { key: 'errors' as const, label: t('usage.tabs.errors'), icon: 'exclamationTriangle' as const },
  { key: 'ranking' as const, label: t('usage.tabs.ranking'), icon: 'chart' as const }
])
const detailTabKeyboardActions = {
  ArrowLeft: -1,
  ArrowRight: 1,
  Home: 'first',
  End: 'last'
} as const
const getDetailTabId = (tab: DetailTab) => `admin-usage-detail-tab-${tab}`
const getDetailPanelId = (tab: DetailTab) => `admin-usage-detail-panel-${tab}`
const usageFiltersRef = ref<InstanceType<typeof UsageFilters> | null>(null)
const rankingMounted = ref(false)
const rankingRef = ref<InstanceType<typeof UserTokenRanking> | null>(null)
const errorRows = ref<OpsErrorLog[]>([])
const errorLoading = ref(false)
const errorMessage = ref('')
const errorPage = ref(1)
const errorPageSize = ref(getPersistedPageSize())
const errorTotal = ref(0)
const showErrorModal = ref(false)
const selectedErrorId = ref<number | null>(null)
let errorRequestSequence = 0
const balanceLedger = ref<UserBalanceLedgerEntry[]>([])
const balanceLedgerStats = ref<AdminBalanceLedgerStatsResponse | null>(null)
const ledgerLoading = ref(false)
const ledgerStatsLoading = ref(false)
const ledgerLoaded = ref(false)
const trendData = ref<TrendDataPoint[]>([]); const requestedModelStats = ref<ModelStat[]>([]); const upstreamModelStats = ref<ModelStat[]>([]); const mappingModelStats = ref<ModelStat[]>([]); const groupStats = ref<GroupStat[]>([]); const chartsLoading = ref(false); const modelStatsLoading = ref(false); const granularity = ref<'day' | 'hour'>('hour')
const modelDistributionMetric = ref<DistributionMetric>('tokens')
const modelDistributionSource = ref<ModelDistributionSource>('requested')
const loadedModelSources = reactive<Record<ModelDistributionSource, boolean>>({
  requested: false,
  upstream: false,
  mapping: false,
})
const groupDistributionMetric = ref<DistributionMetric>('tokens')
const endpointDistributionMetric = ref<DistributionMetric>('tokens')
const endpointDistributionSource = ref<EndpointSource>('inbound')
const inboundEndpointStats = ref<EndpointStat[]>([])
const upstreamEndpointStats = ref<EndpointStat[]>([])
const endpointPathStats = ref<EndpointStat[]>([])
const endpointStatsLoading = ref(false)
let abortController: AbortController | null = null; let ledgerAbortController: AbortController | null = null; let exportAbortController: AbortController | null = null
let chartReqSeq = 0
let statsReqSeq = 0
let modelStatsReqSeq = 0
const exportProgress = reactive({ show: false, progress: 0, current: 0, total: 0, estimatedTime: '' })
const cleanupDialogVisible = ref(false)
// Balance history modal state
const showBalanceHistoryModal = ref(false)
const balanceHistoryUser = ref<AdminUser | null>(null)

const breakdownFilters = computed(() => {
  const f: Record<string, any> = {}
  Object.assign(f, buildExactTimeParams())
  if (filters.value.user_id) f.user_id = filters.value.user_id
  if (filters.value.api_key_id) f.api_key_id = filters.value.api_key_id
  if (filters.value.account_id) f.account_id = filters.value.account_id
  if (filters.value.group_id) f.group_id = filters.value.group_id
  if (filters.value.request_type != null) f.request_type = filters.value.request_type
  if (filters.value.billing_type != null) f.billing_type = filters.value.billing_type
  return f
})

const handleUserClick = async (userId: number) => {
  try {
    const user = await adminAPI.users.getById(userId)
    balanceHistoryUser.value = user
    showBalanceHistoryModal.value = true
  } catch {
    appStore.showError(t('admin.usage.failedToLoadUser'))
  }
}

const handleRankingSelectUser = (userId: number, email: string) => {
  filters.value = { ...filters.value, user_id: userId }
  usageFiltersRef.value?.setUserKeyword(email || '')
  activeDetailTab.value = 'usage'
  applyFilters()
}

const granularityOptions = computed(() => [{ value: 'day', label: t('admin.dashboard.day') }, { value: 'hour', label: t('admin.dashboard.hour') }])
const ledgerColumns = computed<Column[]>(() => [
  { key: 'user', label: t('admin.usage.user'), sortable: false },
  { key: 'reason', label: t('usage.balanceLedger.reason'), sortable: false },
  { key: 'amount', label: t('usage.balanceLedger.amount'), sortable: false },
  { key: 'balance_after', label: t('usage.balanceLedger.balanceAfter'), sortable: false },
  { key: 'ref', label: t('usage.balanceLedger.labels.reference'), sortable: false },
  { key: 'remark', label: t('usage.balanceLedger.remark'), sortable: false },
  { key: 'created_at', label: t('usage.time'), sortable: true }
])
const ledgerDirectionOptions = computed(() => [
  { value: '', label: t('usage.balanceLedger.allDirections') },
  { value: 'debit', label: t('usage.balanceLedger.debit') },
  { value: 'credit', label: t('usage.balanceLedger.credit') }
])
const ledgerReasonOptions = computed(() => [
  { value: '', label: t('usage.balanceLedger.allReasons') },
  { value: 'usage_charge', label: t('usage.balanceLedger.reasons.usage_charge') },
  { value: 'private_group_commission', label: t('usage.balanceLedger.reasons.private_group_commission') },
  { value: 'account_share_mode_seat_prepay', label: t('usage.balanceLedger.reasons.account_share_mode_seat_prepay') },
  { value: 'account_share_mode_seat_refund', label: t('usage.balanceLedger.reasons.account_share_mode_seat_refund') },
  { value: 'account_share_mode_seat_waiver_refund', label: t('usage.balanceLedger.reasons.account_share_mode_seat_waiver_refund') },
  { value: 'account_share_mode_income', label: t('usage.balanceLedger.reasons.account_share_mode_income') },
  { value: 'account_share_income', label: t('usage.balanceLedger.reasons.account_share_income') },
  { value: 'invite_share_income', label: t('usage.balanceLedger.reasons.invite_share_income') },
  { value: 'redeem_code', label: t('usage.balanceLedger.reasons.redeem_code') },
  { value: 'admin_adjustment', label: t('usage.balanceLedger.reasons.admin_adjustment') }
])
const ledgerReasonLabelMap = computed<Record<string, string>>(() => ({
  usage_charge: t('usage.balanceLedger.reasons.usage_charge'),
  private_group_commission: t('usage.balanceLedger.reasons.private_group_commission'),
  account_share_mode_seat_prepay: t('usage.balanceLedger.reasons.account_share_mode_seat_prepay'),
  account_share_mode_seat_refund: t('usage.balanceLedger.reasons.account_share_mode_seat_refund'),
  account_share_mode_seat_waiver_refund: t('usage.balanceLedger.reasons.account_share_mode_seat_waiver_refund'),
  account_share_mode_income: t('usage.balanceLedger.reasons.account_share_mode_income'),
  account_share_income: t('usage.balanceLedger.reasons.account_share_income'),
  invite_share_income: t('usage.balanceLedger.reasons.invite_share_income'),
  redeem_code: t('usage.balanceLedger.reasons.redeem_code'),
  admin_adjustment: t('usage.balanceLedger.reasons.admin_adjustment')
}))
// Use local timezone to avoid UTC timezone issues
const formatLD = (d: Date) => {
  const year = d.getFullYear()
  const month = String(d.getMonth() + 1).padStart(2, '0')
  const day = String(d.getDate()).padStart(2, '0')
  return `${year}-${month}-${day}`
}
const formatLocalDateTime = (d: Date): string => {
  const year = d.getFullYear()
  const month = String(d.getMonth() + 1).padStart(2, '0')
  const day = String(d.getDate()).padStart(2, '0')
  const hours = String(d.getHours()).padStart(2, '0')
  const minutes = String(d.getMinutes()).padStart(2, '0')
  const seconds = String(d.getSeconds()).padStart(2, '0')
  return `${year}-${month}-${day}T${hours}:${minutes}:${seconds}`
}
const dateAtStartOfDayInput = (date: string): string => `${date}T00:00:00`
const dateAtEndOfDayInput = (date: string): string => `${date}T23:59:59`
const getLast24HoursRange = (): { start: string; end: string; startTime: string; endTime: string } => {
  const end = new Date()
  const start = new Date(end.getTime() - 24 * 60 * 60 * 1000)
  return {
    start: formatLD(start),
    end: formatLD(end),
    startTime: formatLocalDateTime(start),
    endTime: formatLocalDateTime(end)
  }
}
const getGranularityForRange = (start: string, end: string): 'day' | 'hour' => {
  const startTime = new Date(`${start}T00:00:00`).getTime()
  const endTime = new Date(`${end}T00:00:00`).getTime()
  const daysDiff = Math.ceil((endTime - startTime) / (1000 * 60 * 60 * 24))
  return daysDiff <= 1 ? 'hour' : 'day'
}
const defaultExactRange = getLast24HoursRange()
const defaultRange = { start: defaultExactRange.start, end: defaultExactRange.end }
const startDate = ref(defaultRange.start); const endDate = ref(defaultRange.end)
const startDateTime = ref(defaultExactRange.startTime)
const endDateTime = ref(defaultExactRange.endTime)
const filters = ref<UsageDetailFilters>({ user_id: undefined, model: undefined, group_id: undefined, request_type: undefined, billing_type: null, upstream_model_mismatch: null, start_date: startDate.value, end_date: endDate.value })
const pagination = reactive({ page: 1, page_size: getPersistedPageSize(), total: 0 })
const ledgerFilters = reactive<AdminBalanceLedgerQueryParams>({
  user_id: undefined,
  direction: '',
  reason: '',
  ref_type: '',
  ref_id: undefined,
  start_date: startDate.value,
  end_date: endDate.value,
  sort_order: 'desc'
})
const ledgerPagination = reactive({ page: 1, page_size: getPersistedPageSize(), total: 0 })
const sortState = reactive({
  sort_by: 'created_at',
  sort_order: 'desc' as 'asc' | 'desc'
})
const ledgerSortState = reactive({
  sort_order: 'desc' as 'asc' | 'desc'
})

const ledgerUserSearchRef = ref<HTMLElement | null>(null)
const ledgerUserKeyword = ref('')
const ledgerUserResults = ref<SimpleUser[]>([])
const showLedgerUserDropdown = ref(false)
let ledgerUserSearchTimeout: number | null = null

const getSingleQueryValue = (value: string | null | Array<string | null> | undefined): string | undefined => {
  if (Array.isArray(value)) return value.find((item): item is string => typeof item === 'string' && item.length > 0)
  return typeof value === 'string' && value.length > 0 ? value : undefined
}

const getNumericQueryValue = (value: string | null | Array<string | null> | undefined): number | undefined => {
  const raw = getSingleQueryValue(value)
  if (!raw) return undefined
  const parsed = Number(raw)
  return Number.isFinite(parsed) ? parsed : undefined
}

const normalizeDateTimeInput = (value: string): string => {
  if (!value) return ''
  return value.length === 16 ? `${value}:00` : value
}

const dateTimeInputToDate = (value: string): Date | null => {
  const normalized = normalizeDateTimeInput(value)
  if (!normalized) return null
  const date = new Date(normalized)
  return Number.isNaN(date.getTime()) ? null : date
}

const dateTimeInputToISOString = (value: string): string | undefined => {
  const date = dateTimeInputToDate(value)
  return date ? date.toISOString() : undefined
}

const queryTimeToDateTimeInput = (value?: string): string | undefined => {
  if (!value) return undefined
  const parsed = new Date(value)
  if (!Number.isNaN(parsed.getTime())) return formatLocalDateTime(parsed)
  return normalizeDateTimeInput(value)
}

const buildExactTimeParams = (): Pick<AdminUsageQueryParams, 'start_time' | 'end_time' | 'timezone'> => ({
  start_time: dateTimeInputToISOString(startDateTime.value),
  end_time: dateTimeInputToISOString(endDateTime.value),
  timezone: getClientTimezone()
})

const loadAdminErrors = async () => {
  const sequence = ++errorRequestSequence
  errorLoading.value = true
  errorMessage.value = ''
  const exactTime = buildExactTimeParams()
  const params: OpsErrorListQueryParams = {
    page: errorPage.value,
    page_size: errorPageSize.value,
    view: 'all',
    start_time: exactTime.start_time,
    end_time: exactTime.end_time,
    account_id: filters.value.account_id,
    group_id: filters.value.group_id,
    phase: filters.value.error_phase || undefined,
    status_codes: filters.value.status_code != null ? String(filters.value.status_code) : undefined
  }

  try {
    const response = await listRequestErrors(params)
    if (sequence !== errorRequestSequence) return
    errorRows.value = response.items
    errorTotal.value = response.total
  } catch (error) {
    if (sequence !== errorRequestSequence) return
    console.error('Failed to load admin errors:', error)
    errorRows.value = []
    errorTotal.value = 0
    errorMessage.value = extractApiErrorMessage(error, t('usage.errors.failedToLoad'))
    appStore.showError(errorMessage.value)
  } finally {
    if (sequence === errorRequestSequence) errorLoading.value = false
  }
}

const switchDetailTab = (tab: DetailTab) => {
  activeDetailTab.value = tab
  if (tab === 'errors') void loadAdminErrors()
  if (tab === 'ranking') rankingMounted.value = true
}

const focusDetailTab = (tab: DetailTab) => {
  window.requestAnimationFrame(() => {
    document.getElementById(getDetailTabId(tab))?.focus()
  })
}

const handleDetailTabKeydown = (event: KeyboardEvent, tab: DetailTab) => {
  const action = detailTabKeyboardActions[event.key as keyof typeof detailTabKeyboardActions]
  if (action === undefined) return

  event.preventDefault()
  const currentIndex = detailTabs.value.findIndex((item) => item.key === tab)
  let nextIndex = currentIndex < 0 ? 0 : currentIndex

  if (action === 'first') {
    nextIndex = 0
  } else if (action === 'last') {
    nextIndex = detailTabs.value.length - 1
  } else {
    nextIndex = (nextIndex + action + detailTabs.value.length) % detailTabs.value.length
  }

  const nextTab = detailTabs.value[nextIndex]?.key
  if (!nextTab) return

  switchDetailTab(nextTab)
  focusDetailTab(nextTab)
}

const handleErrorPageChange = (page: number) => {
  errorPage.value = page
  void loadAdminErrors()
}

const handleErrorPageSizeChange = (pageSize: number) => {
  errorPageSize.value = pageSize
  errorPage.value = 1
  void loadAdminErrors()
}

const openErrorDetail = (id: number) => {
  selectedErrorId.value = id
  showErrorModal.value = true
}

const validateExactTimeRange = (): boolean => {
  const start = dateTimeInputToDate(startDateTime.value)
  const end = dateTimeInputToDate(endDateTime.value)
  if (!start || !end) {
    appStore.showWarning(t('usage.timeRangeRequired'))
    return false
  }
  if (end.getTime() <= start.getTime()) {
    appStore.showWarning(t('usage.invalidTimeRange'))
    return false
  }
  return true
}

const syncDateFieldsFromExactTime = () => {
  const start = dateTimeInputToDate(startDateTime.value)
  const end = dateTimeInputToDate(endDateTime.value)
  if (start) startDate.value = formatLD(start)
  if (end) endDate.value = formatLD(end)
  filters.value = {
    ...filters.value,
    start_date: startDate.value,
    end_date: endDate.value
  }
  ledgerFilters.start_date = startDate.value
  ledgerFilters.end_date = endDate.value
  granularity.value = getGranularityForRange(startDate.value, endDate.value)
}

const syncExactTimeFromDateRange = (range: { startDate: string; endDate: string; preset: string | null }) => {
  if (range.preset === 'last24Hours') {
    const exactRange = getLast24HoursRange()
    startDateTime.value = exactRange.startTime
    endDateTime.value = exactRange.endTime
    return
  }
  startDateTime.value = dateAtStartOfDayInput(range.startDate)
  endDateTime.value = dateAtEndOfDayInput(range.endDate)
}

const onExactTimeRangeChange = () => {
  if (!validateExactTimeRange()) return
  syncDateFieldsFromExactTime()
  if (activeAdminUsageTab.value === 'balanceLedger') {
    applyLedgerFilters()
  } else {
    applyFilters()
  }
}

const applyRouteQueryFilters = () => {
  const queryStartDate = getSingleQueryValue(route.query.start_date)
  const queryEndDate = getSingleQueryValue(route.query.end_date)
  const queryStartTime = queryTimeToDateTimeInput(getSingleQueryValue(route.query.start_time))
  const queryEndTime = queryTimeToDateTimeInput(getSingleQueryValue(route.query.end_time))
  const queryUserId = getNumericQueryValue(route.query.user_id)

  if (queryStartDate) {
    startDate.value = queryStartDate
  }
  if (queryEndDate) {
    endDate.value = queryEndDate
  }
  if (queryStartTime) {
    startDateTime.value = queryStartTime
  }
  if (queryEndTime) {
    endDateTime.value = queryEndTime
  }
  if (queryStartTime || queryEndTime) {
    syncDateFieldsFromExactTime()
  }

  filters.value = {
    ...filters.value,
    ...buildExactTimeParams(),
    user_id: queryUserId,
    start_date: startDate.value,
    end_date: endDate.value
  }
  ledgerFilters.user_id = queryUserId
  ledgerFilters.start_date = startDate.value
  ledgerFilters.end_date = endDate.value
  ledgerUserKeyword.value = queryUserId ? `#${queryUserId}` : ''
  granularity.value = getGranularityForRange(startDate.value, endDate.value)
}

const onDateRangeChange = (range: { startDate: string; endDate: string; preset: string | null }) => {
  startDate.value = range.startDate
  endDate.value = range.endDate
  syncExactTimeFromDateRange(range)
  filters.value = {
    ...filters.value,
    ...buildExactTimeParams(),
    start_date: range.startDate,
    end_date: range.endDate
  }
  ledgerFilters.start_date = range.startDate
  ledgerFilters.end_date = range.endDate
  granularity.value = getGranularityForRange(range.startDate, range.endDate)
  if (activeAdminUsageTab.value === 'balanceLedger') {
    applyLedgerFilters()
  } else {
    applyFilters()
  }
}

const buildUsageApiFilters = (): AdminUsageQueryParams => {
  const params = { ...filters.value }
  delete params.error_phase
  delete params.status_code
  if (params.upstream_model_mismatch == null) delete params.upstream_model_mismatch
  return params
}

const buildUsageListParams = (
  page: number,
  pageSize: number,
  exactTotal: boolean
): AdminUsageQueryParams => {
  const requestType = filters.value.request_type
  const legacyStream = requestType ? requestTypeToLegacyStream(requestType) : filters.value.stream
  return {
    page,
    page_size: pageSize,
    exact_total: exactTotal,
    ...buildUsageApiFilters(),
    ...buildExactTimeParams(),
    stream: legacyStream === null ? undefined : legacyStream,
    sort_by: sortState.sort_by,
    sort_order: sortState.sort_order
  }
}

const loadLogs = async () => {
  abortController?.abort(); const c = new AbortController(); abortController = c; loading.value = true
  try {
    const res = await adminAPI.usage.list(
      buildUsageListParams(pagination.page, pagination.page_size, false),
      { signal: c.signal }
    )
    if(!c.signal.aborted) { usageLogs.value = res.items; pagination.total = res.total }
  } catch (error: any) { if(error?.name !== 'AbortError') console.error('Failed to load usage logs:', error) } finally { if(abortController === c) loading.value = false }
}

const getClientTimezone = (): string | undefined => {
  try {
    return Intl.DateTimeFormat().resolvedOptions().timeZone
  } catch {
    return undefined
  }
}

const buildLedgerParams = (): AdminBalanceLedgerQueryParams => {
  const refID = Number(ledgerFilters.ref_id)
  return {
    page: ledgerPagination.page,
    page_size: ledgerPagination.page_size,
    exact_total: false,
    user_id: ledgerFilters.user_id || undefined,
    direction: ledgerFilters.direction || undefined,
    reason: ledgerFilters.reason || undefined,
    ref_type: ledgerFilters.ref_type?.trim() || undefined,
    ref_id: Number.isFinite(refID) && refID > 0 ? refID : undefined,
    start_date: ledgerFilters.start_date || startDate.value,
    end_date: ledgerFilters.end_date || endDate.value,
    ...buildExactTimeParams(),
    sort_order: ledgerSortState.sort_order,
    timezone: getClientTimezone()
  }
}

const buildLedgerStatsParams = (): AdminBalanceLedgerQueryParams => {
  const params = { ...buildLedgerParams() }
  delete params.page
  delete params.page_size
  delete params.exact_total
  delete params.sort_order
  return params
}

const loadBalanceLedger = async () => {
  ledgerAbortController?.abort()
  const c = new AbortController()
  ledgerAbortController = c
  ledgerLoading.value = true
  ledgerStatsLoading.value = true
  try {
    const [res, stats] = await Promise.all([
      adminUsageAPI.listBalanceLedger(buildLedgerParams(), { signal: c.signal }),
      adminUsageAPI.getBalanceLedgerStats(buildLedgerStatsParams(), { signal: c.signal })
    ])
    if (!c.signal.aborted) {
      balanceLedger.value = res.items
      balanceLedgerStats.value = stats
      ledgerPagination.total = res.total
      ledgerLoaded.value = true
    }
  } catch (error: any) {
    if (error?.name !== 'AbortError') {
      console.error('Failed to load balance ledger:', error)
      appStore.showError(t('usage.balanceLedger.failedToLoad'))
    }
  } finally {
    if (ledgerAbortController === c) {
      ledgerLoading.value = false
      ledgerStatsLoading.value = false
    }
  }
}

const loadStats = async () => {
  const seq = ++statsReqSeq
  endpointStatsLoading.value = true
  try {
    const requestType = filters.value.request_type
    const legacyStream = requestType ? requestTypeToLegacyStream(requestType) : filters.value.stream
    const s = await adminAPI.usage.getStats({
      ...buildUsageApiFilters(),
      ...buildExactTimeParams(),
      upstream_model_mismatch: filters.value.upstream_model_mismatch ?? undefined,
      stream: legacyStream === null ? undefined : legacyStream
    })
    if (seq !== statsReqSeq) return
    usageStats.value = s
    inboundEndpointStats.value = s.endpoints || []
    upstreamEndpointStats.value = s.upstream_endpoints || []
    endpointPathStats.value = s.endpoint_paths || []
  } catch (error) {
    if (seq !== statsReqSeq) return
    console.error('Failed to load usage stats:', error)
    inboundEndpointStats.value = []
    upstreamEndpointStats.value = []
    endpointPathStats.value = []
  } finally {
    if (seq === statsReqSeq) endpointStatsLoading.value = false
  }
}

const resetModelStatsCache = () => {
  requestedModelStats.value = []
  upstreamModelStats.value = []
  mappingModelStats.value = []
  loadedModelSources.requested = false
  loadedModelSources.upstream = false
  loadedModelSources.mapping = false
}

const loadModelStats = async (source: ModelDistributionSource, force = false) => {
  if (!force && loadedModelSources[source]) {
    return
  }

  const seq = ++modelStatsReqSeq
  modelStatsLoading.value = true
  try {
    const requestType = filters.value.request_type
    const legacyStream = requestType ? requestTypeToLegacyStream(requestType) : filters.value.stream
    const baseParams = {
      start_date: filters.value.start_date || startDate.value,
      end_date: filters.value.end_date || endDate.value,
      ...buildExactTimeParams(),
      user_id: filters.value.user_id,
      model: filters.value.model,
      api_key_id: filters.value.api_key_id,
      account_id: filters.value.account_id,
      group_id: filters.value.group_id,
      request_type: requestType,
      stream: legacyStream === null ? undefined : legacyStream,
      billing_type: filters.value.billing_type,
      upstream_model_mismatch: filters.value.upstream_model_mismatch ?? undefined
    }

    const response = await adminAPI.dashboard.getModelStats({ ...baseParams, model_source: source })

    if (seq !== modelStatsReqSeq) return

    const models = response.models || []
    if (source === 'requested') {
      requestedModelStats.value = models
    } else if (source === 'upstream') {
      upstreamModelStats.value = models
    } else {
      mappingModelStats.value = models
    }
    loadedModelSources[source] = true
  } catch (error) {
    if (seq !== modelStatsReqSeq) return
    console.error('Failed to load model stats:', error)
    if (source === 'requested') {
      requestedModelStats.value = []
    } else if (source === 'upstream') {
      upstreamModelStats.value = []
    } else {
      mappingModelStats.value = []
    }
    loadedModelSources[source] = false
  } finally {
    if (seq === modelStatsReqSeq) modelStatsLoading.value = false
  }
}

const loadChartData = async () => {
  const seq = ++chartReqSeq
  chartsLoading.value = true
  try {
    const requestType = filters.value.request_type
    const legacyStream = requestType ? requestTypeToLegacyStream(requestType) : filters.value.stream
    const snapshot = await adminAPI.dashboard.getSnapshotV2({
      start_date: filters.value.start_date || startDate.value,
      end_date: filters.value.end_date || endDate.value,
      ...buildExactTimeParams(),
      granularity: granularity.value,
      user_id: filters.value.user_id,
      model: filters.value.model,
      api_key_id: filters.value.api_key_id,
      account_id: filters.value.account_id,
      group_id: filters.value.group_id,
      request_type: requestType,
      stream: legacyStream === null ? undefined : legacyStream,
      billing_type: filters.value.billing_type,
      upstream_model_mismatch: filters.value.upstream_model_mismatch ?? undefined,
      include_stats: false,
      include_trend: true,
      include_model_stats: false,
      include_group_stats: true,
      include_users_trend: false
    })
    if (seq !== chartReqSeq) return
    trendData.value = snapshot.trend || []
    groupStats.value = snapshot.groups || []
  } catch (error) { console.error('Failed to load chart data:', error) } finally { if (seq === chartReqSeq) chartsLoading.value = false }
}
const applyFilters = () => {
  if (!validateExactTimeRange()) return
  syncDateFieldsFromExactTime()
  pagination.page = 1
  resetModelStatsCache()
  loadLogs()
  loadStats()
  loadModelStats(modelDistributionSource.value, true)
  loadChartData()
  errorPage.value = 1
  if (activeDetailTab.value === 'errors') {
    void loadAdminErrors()
  } else {
    errorRows.value = []
    errorTotal.value = 0
    errorMessage.value = ''
  }
}
const applyLedgerFilters = () => {
  if (!validateExactTimeRange()) return
  syncDateFieldsFromExactTime()
  ledgerPagination.page = 1
  loadBalanceLedger()
}
const refreshData = () => {
  if (!validateExactTimeRange()) return
  syncDateFieldsFromExactTime()
  resetModelStatsCache()
  loadLogs()
  loadStats()
  loadModelStats(modelDistributionSource.value, true)
  loadChartData()
  if (activeDetailTab.value === 'errors') void loadAdminErrors()
  if (rankingMounted.value) rankingRef.value?.reload()
}
const refreshLedgerData = () => {
  if (!validateExactTimeRange()) return
  syncDateFieldsFromExactTime()
  loadBalanceLedger()
}
const resetFilters = () => {
  const range = getLast24HoursRange()
  startDate.value = range.start
  endDate.value = range.end
  startDateTime.value = range.startTime
  endDateTime.value = range.endTime
  filters.value = {
    ...buildExactTimeParams(),
    start_date: startDate.value,
    end_date: endDate.value,
    request_type: undefined,
    billing_type: null,
    billing_mode: undefined,
    upstream_model_mismatch: null,
    error_phase: undefined,
    status_code: undefined
  }
  ledgerFilters.start_date = startDate.value
  ledgerFilters.end_date = endDate.value
  granularity.value = getGranularityForRange(startDate.value, endDate.value)
  applyFilters()
}
const resetLedgerFilters = () => {
  const range = getLast24HoursRange()
  startDate.value = range.start
  endDate.value = range.end
  startDateTime.value = range.startTime
  endDateTime.value = range.endTime
  ledgerFilters.user_id = undefined
  ledgerFilters.direction = ''
  ledgerFilters.reason = ''
  ledgerFilters.ref_type = ''
  ledgerFilters.ref_id = undefined
  ledgerFilters.start_date = startDate.value
  ledgerFilters.end_date = endDate.value
  ledgerSortState.sort_order = 'desc'
  ledgerUserKeyword.value = ''
  ledgerUserResults.value = []
  showLedgerUserDropdown.value = false
  applyLedgerFilters()
}
const handlePageChange = (p: number) => {
  if (!validateExactTimeRange()) return
  syncDateFieldsFromExactTime()
  pagination.page = p
  loadLogs()
}
const handlePageSizeChange = (s: number) => {
  if (!validateExactTimeRange()) return
  syncDateFieldsFromExactTime()
  pagination.page_size = s
  pagination.page = 1
  loadLogs()
}
const handleSort = (key: string, order: 'asc' | 'desc') => {
  if (!validateExactTimeRange()) return
  syncDateFieldsFromExactTime()
  sortState.sort_by = key
  sortState.sort_order = order
  pagination.page = 1
  loadLogs()
}
const handleLedgerPageChange = (p: number) => {
  if (!validateExactTimeRange()) return
  syncDateFieldsFromExactTime()
  ledgerPagination.page = p
  loadBalanceLedger()
}
const handleLedgerPageSizeChange = (s: number) => {
  if (!validateExactTimeRange()) return
  syncDateFieldsFromExactTime()
  ledgerPagination.page_size = s
  ledgerPagination.page = 1
  loadBalanceLedger()
}
const handleLedgerSort = (_key: string, order: 'asc' | 'desc') => {
  if (!validateExactTimeRange()) return
  syncDateFieldsFromExactTime()
  ledgerSortState.sort_order = order
  ledgerPagination.page = 1
  loadBalanceLedger()
}
const switchAdminUsageTab = (tab: AdminUsageTab) => {
  activeAdminUsageTab.value = tab
  if (tab === 'balanceLedger' && !ledgerLoaded.value) {
    if (!validateExactTimeRange()) return
    syncDateFieldsFromExactTime()
    loadBalanceLedger()
  }
  if (tab === 'requests' && activeDetailTab.value === 'errors') {
    void loadAdminErrors()
  }
}

const searchLedgerUsers = async () => {
  const keyword = ledgerUserKeyword.value.trim()
  if (!keyword) {
    ledgerUserResults.value = []
    return
  }
  try {
    ledgerUserResults.value = await adminAPI.usage.searchUsers(keyword)
  } catch (error) {
    console.error('Failed to search ledger users:', error)
    ledgerUserResults.value = []
  }
}

const debounceLedgerUserSearch = () => {
  if (ledgerUserSearchTimeout) {
    clearTimeout(ledgerUserSearchTimeout)
  }
  ledgerUserSearchTimeout = window.setTimeout(() => {
    searchLedgerUsers()
  }, 250)
}

const selectLedgerUser = (user: SimpleUser) => {
  ledgerFilters.user_id = user.id
  ledgerUserKeyword.value = `${user.email} #${user.id}`
  showLedgerUserDropdown.value = false
  applyLedgerFilters()
}

const clearLedgerUser = () => {
  ledgerFilters.user_id = undefined
  ledgerUserKeyword.value = ''
  ledgerUserResults.value = []
  showLedgerUserDropdown.value = false
  applyLedgerFilters()
}
const cancelExport = () => exportAbortController?.abort()
const openCleanupDialog = () => { cleanupDialogVisible.value = true }
const getRequestTypeLabel = (log: AdminUsageLog): string => {
  const requestType = resolveUsageRequestType(log)
  if (requestType === 'ws_v2') return t('usage.ws')
  if (requestType === 'stream') return t('usage.stream')
  if (requestType === 'sync') return t('usage.sync')
  return t('usage.unknown')
}

const getLedgerReasonLabel = (reason: string): string => {
  return ledgerReasonLabelMap.value[reason] || reason || t('usage.unknown')
}

const normalizeLedgerDecimal = (value?: string | number | null, digits = 10): string => {
  if (value === undefined || value === null || value === '') return '0'
  const raw = String(value).trim()
  const match = raw.match(/^(-?)(\d+)(?:\.(\d+))?$/)
  if (!match) {
    const amount = Number(value)
    if (!Number.isFinite(amount)) return '0'
    return amount.toFixed(digits).replace(/\.?0+$/, '') || '0'
  }
  const sign = match[1]
  const integer = match[2].replace(/^0+(?=\d)/, '') || '0'
  const fraction = (match[3] || '').slice(0, digits).replace(/0+$/, '')
  const normalized = fraction ? `${integer}.${fraction}` : integer
  if (normalized === '0') return '0'
  return `${sign}${normalized}`
}

const absoluteLedgerDecimal = (value?: string | number | null): string => {
  return normalizeLedgerDecimal(value).replace(/^-/, '')
}

const formatLedgerCurrency = (value?: string | number | null, digits = 10): string => {
  const normalized = normalizeLedgerDecimal(value, digits)
  return normalized.startsWith('-')
    ? `-$${normalized.slice(1)}`
    : `$${normalized}`
}

const formatOptionalLedgerCurrency = (value?: number | null, digits = 10): string => {
  return value == null ? '' : formatLedgerCurrency(value, digits)
}

const formatLedgerCount = (value?: number | null): string => Number(value || 0).toLocaleString()

const isLedgerNegative = (value?: string | number | null): boolean => normalizeLedgerDecimal(value).startsWith('-')

const formatLedgerNetCurrency = (value?: string | number | null): string => {
  const normalized = normalizeLedgerDecimal(value)
  if (normalized === '0') return '$0'
  return normalized.startsWith('-') ? `-$${normalized.slice(1)}` : `+$${normalized}`
}

const ledgerSummaryCards = computed(() => {
  const stats = balanceLedgerStats.value
  const netNegative = isLedgerNegative(stats?.net_amount)
  return [
    {
      key: 'total_entries',
      label: t('usage.balanceLedger.statsTotalEntries'),
      value: formatLedgerCount(stats?.total_entries),
      icon: 'document' as const,
      iconClass: 'bg-blue-50 text-blue-600 dark:bg-blue-500/15 dark:text-blue-300',
      valueClass: 'text-gray-900 dark:text-white'
    },
    {
      key: 'credit_amount',
      label: t('usage.balanceLedger.statsCreditAmount'),
      value: formatLedgerCurrency(stats?.credit_amount),
      icon: 'arrowUp' as const,
      iconClass: 'bg-emerald-50 text-emerald-600 dark:bg-emerald-500/15 dark:text-emerald-300',
      valueClass: 'text-emerald-600 dark:text-emerald-300'
    },
    {
      key: 'debit_amount',
      label: t('usage.balanceLedger.statsDebitAmount'),
      value: formatLedgerCurrency(stats?.debit_amount),
      icon: 'arrowDown' as const,
      iconClass: 'bg-rose-50 text-rose-600 dark:bg-rose-500/15 dark:text-rose-300',
      valueClass: 'text-rose-600 dark:text-rose-300'
    },
    {
      key: 'net_amount',
      label: t('usage.balanceLedger.statsNetAmount'),
      value: formatLedgerNetCurrency(stats?.net_amount),
      icon: 'trendingUp' as const,
      iconClass: netNegative
        ? 'bg-rose-50 text-rose-600 dark:bg-rose-500/15 dark:text-rose-300'
        : 'bg-amber-50 text-amber-600 dark:bg-amber-500/15 dark:text-amber-300',
      valueClass: netNegative
        ? 'text-rose-600 dark:text-rose-300'
        : 'text-amber-600 dark:text-amber-300'
    }
  ]
})

const formatLedgerAmount = (row: UserBalanceLedgerEntry): string => {
  const sign = row.direction === 'credit' ? '+' : '-'
  return `${sign}${formatLedgerCurrency(absoluteLedgerDecimal(row.amount))}`
}

const metadataValue = (row: UserBalanceLedgerEntry, key: string): unknown => {
  return row.metadata ? row.metadata[key] : undefined
}

const metadataString = (row: UserBalanceLedgerEntry, key: string): string => {
  const value = metadataValue(row, key)
  if (value === undefined || value === null) return ''
  return String(value)
}

const metadataNumber = (row: UserBalanceLedgerEntry, key: string): number | null => {
  const value = Number(metadataValue(row, key))
  return Number.isFinite(value) ? value : null
}

const formatMetadataDateTime = (row: UserBalanceLedgerEntry, key: string): string => {
  const value = metadataString(row, key)
  return value ? formatDateTime(value) : ''
}

const formatLedgerDuration = (milliseconds?: number | null): string => {
  const ms = Number(milliseconds || 0)
  if (!Number.isFinite(ms) || ms <= 0) return ''
  const minutes = ms / 60000
  if (minutes < 1) return `${Math.round(ms / 1000)}s`
  if (minutes < 60) return `${Number(minutes.toFixed(1))}m`
  const hours = minutes / 60
  if (hours < 24) return `${Number(hours.toFixed(2))}h`
  const days = hours / 24
  return `${Number(days.toFixed(2))}d`
}

const ledgerPart = (labelKey: string, value: string | number | null | undefined): string => {
  if (value === undefined || value === null || value === '') return ''
  return `${t(labelKey)}: ${value}`
}

const compactLedgerParts = (parts: string[]): string => {
  const clean = parts.filter(Boolean)
  return clean.length > 0 ? clean.join(' · ') : t('usage.balanceLedger.noRemark')
}

const getLedgerStageLabel = (stage: string): string => {
  if (stage === 'renew') return t('usage.balanceLedger.stages.renew')
  if (stage === 'join') return t('usage.balanceLedger.stages.join')
  return stage
}

const getLedgerRemark = (row: UserBalanceLedgerEntry): string => {
  const duration = formatLedgerDuration(metadataNumber(row, 'duration_ms'))
  const rate = metadataNumber(row, 'hourly_rate')
  const periodStarted = metadataString(row, 'period_started')
  const periodEnded = metadataString(row, 'period_ended')
  const period = periodStarted && periodEnded
    ? `${formatDateTime(periodStarted)} - ${formatDateTime(periodEnded)}`
    : ''

  switch (row.reason) {
    case 'usage_charge':
      return compactLedgerParts([
        ledgerPart('usage.balanceLedger.labels.requestId', metadataString(row, 'request_id')),
        ledgerPart('usage.balanceLedger.labels.apiKey', metadataString(row, 'api_key_id')),
        ledgerPart('usage.balanceLedger.labels.account', metadataString(row, 'account_id'))
      ])
    case 'private_group_commission':
      return compactLedgerParts([
        ledgerPart('usage.balanceLedger.labels.group', metadataString(row, 'group_id')),
        ledgerPart('usage.balanceLedger.labels.baseCost', formatOptionalLedgerCurrency(metadataNumber(row, 'base_cost'))),
        ledgerPart('usage.balanceLedger.labels.requestId', metadataString(row, 'request_id'))
      ])
    case 'account_share_mode_seat_prepay':
      return compactLedgerParts([
        ledgerPart('usage.balanceLedger.labels.stage', getLedgerStageLabel(metadataString(row, 'prepay_stage'))),
        ledgerPart('usage.balanceLedger.labels.hourlyRate', rate == null ? '' : formatLedgerCurrency(rate, 4)),
        ledgerPart('usage.balanceLedger.labels.duration', duration),
        ledgerPart('usage.balanceLedger.labels.paidUntil', formatMetadataDateTime(row, 'paid_until')),
        ledgerPart('usage.balanceLedger.labels.account', metadataString(row, 'account_id')),
        ledgerPart('usage.balanceLedger.labels.membership', metadataString(row, 'membership_id'))
      ])
    case 'account_share_mode_seat_refund':
      return compactLedgerParts([
        ledgerPart('usage.balanceLedger.labels.hourlyRate', rate == null ? '' : formatLedgerCurrency(rate, 4)),
        ledgerPart('usage.balanceLedger.labels.duration', duration),
        ledgerPart('usage.balanceLedger.labels.refundUntil', formatMetadataDateTime(row, 'refund_until')),
        ledgerPart('usage.balanceLedger.labels.account', metadataString(row, 'account_id'))
      ])
    case 'account_share_mode_seat_waiver_refund':
      return compactLedgerParts([
        ledgerPart('usage.balanceLedger.labels.period', period),
        ledgerPart('usage.balanceLedger.labels.duration', duration),
        ledgerPart('usage.balanceLedger.labels.waiverRequired', formatOptionalLedgerCurrency(metadataNumber(row, 'waiver_required'))),
        ledgerPart('usage.balanceLedger.labels.waiverUsage', formatOptionalLedgerCurrency(metadataNumber(row, 'waiver_usage'))),
        ledgerPart('usage.balanceLedger.labels.account', metadataString(row, 'account_id'))
      ])
    case 'account_share_mode_income':
      return compactLedgerParts([
        ledgerPart('usage.balanceLedger.labels.consumer', metadataString(row, 'consumer_user_id')),
        ledgerPart('usage.balanceLedger.labels.period', period),
        ledgerPart('usage.balanceLedger.labels.settlement', metadataString(row, 'settlement_id')),
        ledgerPart('usage.balanceLedger.labels.account', metadataString(row, 'account_id')),
        ledgerPart('usage.balanceLedger.labels.requestId', metadataString(row, 'request_id'))
      ])
    case 'account_share_income':
    case 'invite_share_income':
      return compactLedgerParts([
        ledgerPart('usage.balanceLedger.labels.consumer', metadataString(row, 'consumer_user_id')),
        ledgerPart('usage.balanceLedger.labels.apiKey', metadataString(row, 'api_key_id')),
        ledgerPart('usage.balanceLedger.labels.account', metadataString(row, 'account_id')),
        ledgerPart('usage.balanceLedger.labels.requestId', metadataString(row, 'request_id'))
      ])
    case 'redeem_code':
      return compactLedgerParts([
        ledgerPart('usage.balanceLedger.labels.code', metadataString(row, 'code')),
        ledgerPart('usage.balanceLedger.labels.reference', row.ref_id || '')
      ])
    case 'admin_adjustment':
      return compactLedgerParts([
        ledgerPart('usage.balanceLedger.labels.notes', metadataString(row, 'notes')),
        ledgerPart('usage.balanceLedger.labels.operation', metadataString(row, 'operation')),
        ledgerPart('usage.balanceLedger.labels.code', metadataString(row, 'code')),
        ledgerPart('usage.balanceLedger.labels.reference', row.ref_id || '')
      ])
    default:
      return compactLedgerParts([
        ledgerPart('usage.balanceLedger.labels.reference', row.ref_id || ''),
        ledgerPart('usage.balanceLedger.labels.referenceType', row.ref_type)
      ])
  }
}

const balanceLedgerRows = computed<BalanceLedgerTableRow[]>(() =>
  balanceLedger.value.map((row) => ({
    ...row,
    reasonLabel: getLedgerReasonLabel(row.reason),
    amountLabel: formatLedgerAmount(row),
    balanceAfterLabel: formatLedgerCurrency(row.balance_after),
    remarkText: getLedgerRemark(row)
  }))
)

type CsvCell = string | number | boolean | null | undefined

const CSV_FORMULA_PREFIX_PATTERN = /^\s*[=+\-@]/
const CSV_ESCAPE_PATTERN = /[",\r\n]/
const CSV_BOM = '\uFEFF'

const escapeCsvCell = (value: CsvCell): string => {
  if (value == null) return ''
  if (typeof value === 'number' || typeof value === 'boolean') return String(value)

  let text = value
  if (CSV_FORMULA_PREFIX_PATTERN.test(text)) {
    text = `'${text}`
  }

  if (CSV_ESCAPE_PATTERN.test(text)) {
    return `"${text.replace(/"/g, '""')}"`
  }

  return text
}

const toCsvRow = (row: CsvCell[]): string => row.map(escapeCsvCell).join(',')

const exportToExcel = async () => {
  if (!validateExactTimeRange()) return
  syncDateFieldsFromExactTime()
  if (exporting.value) return; exporting.value = true; exportProgress.show = true
  const c = new AbortController(); exportAbortController = c
  try {
    let p = 1; let total = pagination.total; let exportedCount = 0
    const headers = [
      t('usage.time'), t('admin.usage.user'), t('usage.apiKeyFilter'),
      t('admin.usage.account'), t('usage.requestedModel'), t('usage.sentUpstreamModel'), t('usage.upstreamResponseModel'), t('usage.upstreamModelMismatch'), t('usage.reasoningEffort'), t('admin.usage.group'),
      t('usage.inboundEndpoint'), t('usage.upstreamEndpoint'),
      t('usage.type'),
      t('admin.usage.inputTokens'), t('admin.usage.outputTokens'),
      t('admin.usage.cacheReadTokens'), t('admin.usage.cacheCreationTokens'),
      t('usage.cacheHitRate'),
      t('admin.usage.inputCost'), t('admin.usage.outputCost'),
      t('admin.usage.cacheReadCost'), t('admin.usage.cacheCreationCost'),
      t('usage.rate'), t('usage.accountMultiplier'), t('usage.original'), t('usage.userBilled'), t('usage.accountBilled'),
      t('usage.firstToken'), t('usage.duration'),
      t('admin.usage.requestId'), t('usage.userAgent'), t('admin.usage.ipAddress')
    ]
    const csvRows = [toCsvRow(headers)]
    while (true) {
      const res = await adminUsageAPI.list(
        buildUsageListParams(p, 100, true),
        { signal: c.signal }
      )
      if (c.signal.aborted) break; if (p === 1) { total = res.total; exportProgress.total = total }
      const rows = (res.items || []).map((log: AdminUsageLog) => [
        log.created_at, log.user?.email || '', log.api_key?.name || '', log.account?.name || '', log.model,
        log.upstream_model || log.model, log.upstream_response_model || '', log.upstream_model_mismatch == null ? '' : t(log.upstream_model_mismatch ? 'common.yes' : 'common.no'), formatReasoningEffort(log.reasoning_effort), log.group?.name || '',
        log.inbound_endpoint || '', log.upstream_endpoint || '', getRequestTypeLabel(log),
        log.input_tokens, log.output_tokens, log.cache_read_tokens, log.cache_creation_tokens,
        formatCacheHitRate(log.input_tokens, log.cache_read_tokens),
        log.input_cost?.toFixed(6) || '0.000000', log.output_cost?.toFixed(6) || '0.000000',
        log.cache_read_cost?.toFixed(6) || '0.000000', log.cache_creation_cost?.toFixed(6) || '0.000000',
        log.rate_multiplier?.toPrecision(4) || '1.00', (log.account_rate_multiplier ?? 1).toPrecision(4),
        log.total_cost?.toFixed(6) || '0.000000', log.actual_cost?.toFixed(6) || '0.000000',
        ((log.account_stats_cost ?? log.total_cost) * (log.account_rate_multiplier ?? 1)).toFixed(6), log.first_token_ms ?? '', log.duration_ms,
        log.request_id || '', log.user_agent || '', log.ip_address || ''
      ])
      if (rows.length) {
        csvRows.push(...rows.map(toCsvRow))
      }
      exportedCount += rows.length
      exportProgress.current = exportedCount
      exportProgress.progress = total > 0 ? Math.min(100, Math.round(exportedCount / total * 100)) : 0
      if (exportedCount >= total || res.items.length < 100) break; p++
    }
    if(!c.signal.aborted) {
      saveAs(new Blob([CSV_BOM, csvRows.join('\r\n')], { type: 'text/csv;charset=utf-8' }), `usage_${filters.value.start_date}_to_${filters.value.end_date}.csv`)
      appStore.showSuccess(t('usage.exportSuccess'))
    }
  } catch (error) { console.error('Failed to export:', error); appStore.showError('Export Failed') }
  finally { if(exportAbortController === c) { exportAbortController = null; exporting.value = false; exportProgress.show = false } }
}

// Column visibility
const ALWAYS_VISIBLE = ['user', 'created_at']
const DEFAULT_HIDDEN_COLUMNS = ['reasoning_effort', 'user_agent']
const HIDDEN_COLUMNS_KEY = 'usage-hidden-columns'

const allColumns = computed(() => [
  { key: 'user', label: t('admin.usage.user'), sortable: false },
  { key: 'api_key', label: t('usage.apiKeyFilter'), sortable: false },
  { key: 'account', label: t('admin.usage.account'), sortable: false },
  { key: 'model', label: t('usage.model'), sortable: true },
  { key: 'reasoning_effort', label: t('usage.reasoningEffort'), sortable: false },
  { key: 'endpoint', label: t('usage.endpoint'), sortable: false },
  { key: 'group', label: t('admin.usage.group'), sortable: false },
  { key: 'stream', label: t('usage.type'), sortable: false },
  { key: 'billing_mode', label: t('admin.usage.billingMode'), sortable: false },
  { key: 'tokens', label: t('usage.tokens'), sortable: false },
  { key: 'cost', label: t('usage.cost'), sortable: false },
  { key: 'latency', label: t('usage.latency'), sortable: false },
  { key: 'created_at', label: t('usage.time'), sortable: true },
  { key: 'user_agent', label: t('usage.userAgent'), sortable: false },
  { key: 'ip_address', label: t('admin.usage.ipAddress'), sortable: false }
])

const hiddenColumns = reactive<Set<string>>(new Set())

const toggleableColumns = computed(() =>
  allColumns.value.filter(col => !ALWAYS_VISIBLE.includes(col.key))
)

const visibleColumns = computed(() =>
  allColumns.value.filter(col =>
    ALWAYS_VISIBLE.includes(col.key) || !hiddenColumns.has(col.key)
  )
)

const isColumnVisible = (key: string) => !hiddenColumns.has(key)

const toggleColumn = (key: string) => {
  if (hiddenColumns.has(key)) {
    hiddenColumns.delete(key)
  } else {
    hiddenColumns.add(key)
  }
  try {
    localStorage.setItem(HIDDEN_COLUMNS_KEY, JSON.stringify([...hiddenColumns]))
  } catch (e) {
    console.error('Failed to save columns:', e)
  }
}

const loadSavedColumns = () => {
  try {
    const saved = localStorage.getItem(HIDDEN_COLUMNS_KEY)
    if (saved) {
      (JSON.parse(saved) as string[]).forEach((key) => {
        hiddenColumns.add(key)
      })
    } else {
      DEFAULT_HIDDEN_COLUMNS.forEach((key) => {
        hiddenColumns.add(key)
      })
    }
  } catch {
    DEFAULT_HIDDEN_COLUMNS.forEach((key) => {
      hiddenColumns.add(key)
    })
  }
}

const showColumnDropdown = ref(false)
const columnDropdownRef = ref<HTMLElement | null>(null)

const handleColumnClickOutside = (event: MouseEvent) => {
  if (columnDropdownRef.value && !columnDropdownRef.value.contains(event.target as HTMLElement)) {
    showColumnDropdown.value = false
  }
}

const handleLedgerUserClickOutside = (event: MouseEvent) => {
  if (ledgerUserSearchRef.value && !ledgerUserSearchRef.value.contains(event.target as HTMLElement)) {
    showLedgerUserDropdown.value = false
  }
}

onMounted(() => {
  applyRouteQueryFilters()
  loadLogs()
  loadStats()
  loadModelStats(modelDistributionSource.value, true)
  window.setTimeout(() => {
    void loadChartData()
  }, 120)
  loadSavedColumns()
  document.addEventListener('click', handleColumnClickOutside)
  document.addEventListener('click', handleLedgerUserClickOutside)
})
onUnmounted(() => {
  errorRequestSequence++
  abortController?.abort()
  ledgerAbortController?.abort()
  exportAbortController?.abort()
  if (ledgerUserSearchTimeout) {
    clearTimeout(ledgerUserSearchTimeout)
    ledgerUserSearchTimeout = null
  }
  document.removeEventListener('click', handleColumnClickOutside)
  document.removeEventListener('click', handleLedgerUserClickOutside)
})

watch(modelDistributionSource, (source) => {
  void loadModelStats(source)
})
</script>
