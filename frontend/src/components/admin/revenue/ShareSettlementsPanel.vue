<template>
  <section class="card p-5">
    <div class="mb-4 flex flex-col gap-3 xl:flex-row xl:items-start xl:justify-between">
      <div>
        <h3 class="text-base font-semibold text-gray-900 dark:text-white">
          {{ t('admin.revenue.shareSettlements.title') }}
        </h3>
        <p class="mt-1 max-w-3xl text-sm leading-6 text-gray-500 dark:text-gray-400">
          {{ t('admin.revenue.shareSettlements.description') }}
        </p>
      </div>
      <div class="flex flex-wrap items-end gap-2">
        <div class="grid grid-cols-2 gap-2 sm:w-[300px]">
          <div>
            <label class="input-label">{{ t('dates.startDate') }}</label>
            <input v-model="startDate" type="date" class="input h-10" @change="applyCustomDateRange" />
          </div>
          <div>
            <label class="input-label">{{ t('dates.endDate') }}</label>
            <input v-model="endDate" type="date" class="input h-10" @change="applyCustomDateRange" />
          </div>
        </div>
        <div class="inline-flex rounded-lg border border-gray-200 bg-white p-1 dark:border-dark-600 dark:bg-dark-800">
          <button
            v-for="option in rangeOptions"
            :key="option"
            type="button"
            class="min-w-[64px] rounded-md px-3 py-1.5 text-sm font-medium transition-colors"
            :class="selectedRangeDays === option
              ? 'bg-emerald-600 text-white shadow-sm'
              : 'text-gray-600 hover:bg-gray-100 dark:text-gray-300 dark:hover:bg-dark-700'"
            @click="setRange(option)"
          >
            {{ t('admin.revenue.controls.rangeDays', { days: option }) }}
          </button>
        </div>
        <select v-model="status" class="input h-10 w-32" @change="resetAndLoad">
          <option value="all">{{ t('common.all') }}</option>
          <option value="applied">{{ t('admin.revenue.shareSettlements.status.applied') }}</option>
          <option value="reversed">{{ t('admin.revenue.shareSettlements.status.reversed') }}</option>
          <option value="frozen">{{ t('admin.revenue.shareSettlements.status.frozen') }}</option>
        </select>
        <div class="relative">
          <Icon name="search" size="sm" class="pointer-events-none absolute left-3 top-1/2 -translate-y-1/2 text-gray-400" />
          <input
            v-model="search"
            class="input h-10 w-64 pl-9"
            :placeholder="t('admin.revenue.shareSettlements.searchPlaceholder')"
            @keyup.enter="resetAndLoad"
          />
        </div>
        <button
          type="button"
          class="btn btn-secondary h-10 gap-2"
          :disabled="exportCreating || isExportActive"
          :title="t('admin.revenue.shareSettlements.exportCurrentFilter')"
          @click="createExport"
        >
          <Icon
            :name="exportCreating ? 'refresh' : 'download'"
            size="md"
            :class="exportCreating ? 'animate-spin' : ''"
          />
          <span class="hidden sm:inline">
            {{ exportCreating ? t('admin.revenue.shareSettlements.exporting') : t('admin.revenue.shareSettlements.export') }}
          </span>
        </button>
        <button type="button" class="btn btn-secondary h-10" :disabled="loading" @click="loadSettlements">
          <Icon name="refresh" size="md" :class="loading ? 'animate-spin' : ''" />
        </button>
      </div>
    </div>

    <div
      v-if="exportTask"
      class="mb-4 rounded-lg border border-gray-200 bg-gray-50 px-4 py-3 dark:border-dark-700 dark:bg-dark-800/70"
    >
      <div class="flex flex-col gap-3 lg:flex-row lg:items-center lg:justify-between">
        <div class="min-w-0 flex-1">
          <div class="flex flex-wrap items-center gap-2">
            <span class="text-sm font-semibold text-gray-900 dark:text-white">
              {{ t('admin.revenue.shareSettlements.exportTask') }}
            </span>
            <span class="rounded-full px-2.5 py-1 text-xs font-medium" :class="exportStatusClass(exportTask.status)">
              {{ exportStatusLabel(exportTask.status) }}
            </span>
            <span v-if="exportTask.file_name" class="truncate text-xs text-gray-500 dark:text-gray-400">
              {{ exportTask.file_name }}
            </span>
          </div>
          <div class="mt-2 h-1.5 overflow-hidden rounded-full bg-gray-200 dark:bg-dark-700">
            <div
              class="h-full rounded-full bg-emerald-600 transition-all"
              :style="{ width: `${exportProgressPercent}%` }"
            />
          </div>
          <div class="mt-2 flex flex-wrap items-center gap-x-4 gap-y-1 text-xs text-gray-500 dark:text-gray-400">
            <span>
              {{ t('admin.revenue.shareSettlements.exportProgress', {
                done: formatInteger(exportTask.exported_rows),
                total: formatInteger(exportTask.total_rows)
              }) }}
            </span>
            <span v-if="exportTask.file_count">
              {{ t('admin.revenue.shareSettlements.exportFiles', { count: exportTask.file_count }) }}
            </span>
            <span v-if="exportTask.file_size_bytes">
              {{ formatFileSize(exportTask.file_size_bytes) }}
            </span>
          </div>
          <p v-if="exportTask.status === 'failed' && exportTask.error_message" class="mt-2 text-xs text-rose-600 dark:text-rose-300">
            {{ exportTask.error_message }}
          </p>
        </div>
        <div class="flex flex-wrap items-center gap-2">
          <button
            v-if="exportTask.status === 'completed'"
            type="button"
            class="btn btn-primary h-9 gap-2"
            :disabled="exportDownloading"
            @click="downloadExport"
          >
            <Icon name="download" size="sm" />
            <span>{{ t('admin.revenue.shareSettlements.exportDownload') }}</span>
          </button>
          <button
            v-if="isExportActive"
            type="button"
            class="btn btn-secondary h-9 gap-2"
            @click="cancelExport"
          >
            <Icon name="x" size="sm" />
            <span>{{ t('admin.revenue.shareSettlements.exportCancel') }}</span>
          </button>
        </div>
      </div>
    </div>

    <div v-if="loading && !items.length" class="flex items-center justify-center py-16">
      <LoadingSpinner />
    </div>

    <template v-else>
      <div v-if="items.length" class="overflow-x-auto">
        <table class="min-w-[1780px] divide-y divide-gray-200 dark:divide-dark-700">
          <thead>
            <tr>
              <th class="px-3 py-3 text-left text-xs font-semibold uppercase tracking-wide text-gray-500 dark:text-gray-400">
                {{ t('admin.revenue.shareSettlements.table.createdAt') }}
              </th>
              <th class="px-3 py-3 text-left text-xs font-semibold uppercase tracking-wide text-gray-500 dark:text-gray-400">
                {{ t('admin.revenue.shareSettlements.table.request') }}
              </th>
              <th class="px-3 py-3 text-left text-xs font-semibold uppercase tracking-wide text-gray-500 dark:text-gray-400">
                {{ t('admin.revenue.shareSettlements.table.consumer') }}
              </th>
              <th class="px-3 py-3 text-left text-xs font-semibold uppercase tracking-wide text-gray-500 dark:text-gray-400">
                {{ t('admin.revenue.shareSettlements.table.owner') }}
              </th>
              <th class="px-3 py-3 text-left text-xs font-semibold uppercase tracking-wide text-gray-500 dark:text-gray-400">
                {{ t('admin.revenue.shareSettlements.table.inviter') }}
              </th>
              <th class="px-3 py-3 text-left text-xs font-semibold uppercase tracking-wide text-gray-500 dark:text-gray-400">
                {{ t('admin.revenue.shareSettlements.table.account') }}
              </th>
              <th class="px-3 py-3 text-right text-xs font-semibold uppercase tracking-wide text-gray-500 dark:text-gray-400">
                {{ t('admin.revenue.shareSettlements.table.consumerCharge') }}
              </th>
              <th class="px-3 py-3 text-right text-xs font-semibold uppercase tracking-wide text-gray-500 dark:text-gray-400">
                {{ t('admin.revenue.shareSettlements.table.accountCost') }}
              </th>
              <th class="px-3 py-3 text-right text-xs font-semibold uppercase tracking-wide text-gray-500 dark:text-gray-400">
                {{ t('admin.revenue.shareSettlements.table.ownerShareRatio') }}
              </th>
              <th class="px-3 py-3 text-right text-xs font-semibold uppercase tracking-wide text-gray-500 dark:text-gray-400">
                {{ t('admin.revenue.shareSettlements.table.ownerCredit') }}
              </th>
              <th class="px-3 py-3 text-right text-xs font-semibold uppercase tracking-wide text-gray-500 dark:text-gray-400">
                {{ t('admin.revenue.shareSettlements.table.inviteShareRatio') }}
              </th>
              <th class="px-3 py-3 text-right text-xs font-semibold uppercase tracking-wide text-gray-500 dark:text-gray-400">
                {{ t('admin.revenue.shareSettlements.table.inviteCredit') }}
              </th>
              <th class="px-3 py-3 text-right text-xs font-semibold uppercase tracking-wide text-gray-500 dark:text-gray-400">
                {{ t('admin.revenue.shareSettlements.table.platformFee') }}
              </th>
              <th class="px-3 py-3 text-right text-xs font-semibold uppercase tracking-wide text-gray-500 dark:text-gray-400">
                {{ t('admin.revenue.shareSettlements.table.platformNetProfit') }}
              </th>
              <th class="px-3 py-3 text-left text-xs font-semibold uppercase tracking-wide text-gray-500 dark:text-gray-400">
                {{ t('common.status') }}
              </th>
            </tr>
          </thead>
          <tbody class="divide-y divide-gray-100 dark:divide-dark-700">
            <tr v-for="item in items" :key="item.id" class="align-top hover:bg-gray-50 dark:hover:bg-dark-800">
              <td class="whitespace-nowrap px-3 py-3 text-sm text-gray-700 dark:text-gray-300">
                {{ formatDateTime(item.created_at) }}
              </td>
              <td class="max-w-[220px] px-3 py-3">
                <div class="truncate font-mono text-xs text-gray-900 dark:text-white" :title="item.request_id">
                  {{ item.request_id }}
                </div>
                <div class="mt-1 text-xs text-gray-500 dark:text-gray-400">
                  {{ item.api_key_name || `#${item.api_key_id}` }}
                </div>
              </td>
              <td class="max-w-[180px] px-3 py-3">
                <div class="truncate text-sm font-medium text-gray-900 dark:text-white">{{ item.consumer_email }}</div>
                <div class="text-xs text-gray-500 dark:text-gray-400">#{{ item.consumer_user_id }}</div>
              </td>
              <td class="max-w-[180px] px-3 py-3">
                <div class="truncate text-sm font-medium text-gray-900 dark:text-white">{{ item.owner_email }}</div>
                <div class="text-xs text-gray-500 dark:text-gray-400">#{{ item.owner_user_id }}</div>
              </td>
              <td class="max-w-[180px] px-3 py-3">
                <template v-if="item.inviter_user_id">
                  <div class="truncate text-sm font-medium text-gray-900 dark:text-white">{{ item.inviter_email || '-' }}</div>
                  <div class="text-xs text-gray-500 dark:text-gray-400">#{{ item.inviter_user_id }}</div>
                </template>
                <span v-else class="text-sm text-gray-400">--</span>
              </td>
              <td class="max-w-[220px] px-3 py-3">
                <div class="truncate text-sm font-medium text-gray-900 dark:text-white">{{ item.account_name }}</div>
                <div class="text-xs text-gray-500 dark:text-gray-400">
                  {{ item.account_platform || '-' }} · #{{ item.account_id }}
                  <span v-if="item.policy_id"> · {{ t('admin.revenue.shareSettlements.policy', { id: item.policy_id, version: item.policy_version }) }}</span>
                </div>
              </td>
              <td class="px-3 py-3 text-right text-sm text-gray-700 dark:text-gray-300">{{ formatAmount(item.consumer_charge) }}</td>
              <td class="px-3 py-3 text-right text-sm text-gray-700 dark:text-gray-300">{{ formatAmount(item.account_cost) }}</td>
              <td class="px-3 py-3 text-right text-sm text-gray-700 dark:text-gray-300">{{ formatPercent(item.owner_share_ratio) }}</td>
              <td class="px-3 py-3 text-right text-sm font-medium text-gray-900 dark:text-white">{{ formatAmount(item.owner_credit) }}</td>
              <td class="px-3 py-3 text-right text-sm text-gray-700 dark:text-gray-300">{{ formatPercent(item.invite_share_ratio) }}</td>
              <td class="px-3 py-3 text-right text-sm font-medium text-gray-900 dark:text-white">{{ formatAmount(item.invite_credit) }}</td>
              <td class="px-3 py-3 text-right text-sm text-gray-700 dark:text-gray-300">{{ formatAmount(item.platform_fee) }}</td>
              <td class="px-3 py-3 text-right text-sm font-medium text-gray-900 dark:text-white">{{ formatAmount(item.platform_net_profit) }}</td>
              <td class="px-3 py-3">
                <span class="rounded-full px-2.5 py-1 text-xs font-medium" :class="statusClass(item.status)">
                  {{ statusLabel(item.status) }}
                </span>
              </td>
            </tr>
          </tbody>
        </table>
      </div>
      <div v-else class="flex h-40 items-center justify-center text-sm text-gray-500 dark:text-gray-400">
        {{ t('admin.revenue.shareSettlements.noData') }}
      </div>

      <Pagination
        v-if="pagination.total > 0"
        class="mt-4"
        :page="pagination.page"
        :total="pagination.total"
        :page-size="pagination.page_size"
        @update:page="handlePageChange"
        @update:pageSize="handlePageSizeChange"
      />
    </template>
  </section>
</template>

<script setup lang="ts">
import { computed, onMounted, onUnmounted, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '@/components/icons/Icon.vue'
import LoadingSpinner from '@/components/common/LoadingSpinner.vue'
import Pagination from '@/components/common/Pagination.vue'
import { revenueAPI } from '@/api/admin/revenue'
import type {
  RevenueShareSettlementExportStatus,
  RevenueShareSettlementExportTask,
  RevenueShareSettlementItem,
  RevenueShareSettlementParams
} from '@/api/admin/revenue'
import { useAppStore } from '@/stores/app'
import { extractI18nErrorMessage } from '@/utils/apiError'

type RangeDays = 1 | 2 | 3
type SettlementStatus = NonNullable<RevenueShareSettlementParams['status']>

const rangeOptions: RangeDays[] = [1, 2, 3]
const MAX_SETTLEMENT_RANGE_DAYS = 3

const { t, locale } = useI18n()
const appStore = useAppStore()

const rangeDays = ref<RangeDays>(1)
const initialRange = getDateRange(rangeDays.value)
const startDate = ref(initialRange.start)
const endDate = ref(initialRange.end)
const selectedRangeDays = ref<RangeDays | null>(rangeDays.value)
const status = ref<SettlementStatus>('all')
const search = ref('')
const loading = ref(false)
const exportCreating = ref(false)
const exportDownloading = ref(false)
const exportPolling = ref(false)
const exportTask = ref<RevenueShareSettlementExportTask | null>(null)
const items = ref<RevenueShareSettlementItem[]>([])
const pagination = reactive({
  page: 1,
  page_size: 20,
  total: 0,
  pages: 1
})

const amountFormatter = computed(() => new Intl.NumberFormat(locale.value, {
  minimumFractionDigits: 2,
  maximumFractionDigits: 6
}))

const percentFormatter = computed(() => new Intl.NumberFormat(locale.value, {
  style: 'percent',
  minimumFractionDigits: 2,
  maximumFractionDigits: 2
}))

const integerFormatter = computed(() => new Intl.NumberFormat(locale.value, {
  maximumFractionDigits: 0
}))

const isExportActive = computed(() => exportTask.value?.status === 'pending' || exportTask.value?.status === 'running')

const exportProgressPercent = computed(() => {
  const task = exportTask.value
  if (!task) return 0
  if (task.total_rows <= 0) return task.status === 'completed' ? 100 : 0
  return Math.max(0, Math.min(100, Math.round((task.exported_rows / task.total_rows) * 100)))
})

let exportPollTimer: number | null = null

async function loadSettlements() {
  if (!validateDateRange()) return

  loading.value = true
  try {
    const result = await revenueAPI.listShareSettlements({
      page: pagination.page,
      page_size: pagination.page_size,
      start_date: startDate.value,
      end_date: endDate.value,
      status: status.value,
      search: search.value.trim() || undefined
    })
    items.value = result.items
    pagination.total = result.total || 0
    pagination.pages = result.pages || 1
    pagination.page = result.page || pagination.page
    pagination.page_size = result.page_size || pagination.page_size
  } catch (err: unknown) {
    appStore.showError(extractI18nErrorMessage(err, t, 'admin.revenue.shareSettlements.errors', t('admin.revenue.shareSettlements.loadFailed')))
  } finally {
    loading.value = false
  }
}

async function createExport() {
  if (!validateDateRange()) return
  exportCreating.value = true
  try {
    exportTask.value = await revenueAPI.createShareSettlementExport({
      start_date: startDate.value,
      end_date: endDate.value,
      timezone: getBrowserTimezone(),
      status: status.value,
      search: search.value.trim() || undefined
    })
    appStore.showSuccess(t('admin.revenue.shareSettlements.exportCreated'))
    startExportPolling()
  } catch (err: unknown) {
    appStore.showError(extractI18nErrorMessage(err, t, 'admin.revenue.shareSettlements.errors', t('admin.revenue.shareSettlements.exportCreateFailed')))
  } finally {
    exportCreating.value = false
  }
}

async function refreshExportTask(showError = false) {
  if (!exportTask.value || exportPolling.value) return
  exportPolling.value = true
  try {
    exportTask.value = await revenueAPI.getShareSettlementExport(exportTask.value.id)
    if (!isExportActive.value) {
      stopExportPolling()
    }
  } catch (err: unknown) {
    stopExportPolling()
    if (showError) {
      appStore.showError(extractI18nErrorMessage(err, t, 'admin.revenue.shareSettlements.errors', t('admin.revenue.shareSettlements.exportLoadFailed')))
    }
  } finally {
    exportPolling.value = false
  }
}

async function cancelExport() {
  if (!exportTask.value || !isExportActive.value) return
  try {
    exportTask.value = await revenueAPI.cancelShareSettlementExport(exportTask.value.id)
    stopExportPolling()
    appStore.showSuccess(t('admin.revenue.shareSettlements.exportCanceledToast'))
  } catch (err: unknown) {
    appStore.showError(extractI18nErrorMessage(err, t, 'admin.revenue.shareSettlements.errors', t('admin.revenue.shareSettlements.exportCancelFailed')))
  }
}

async function downloadExport() {
  if (!exportTask.value || exportTask.value.status !== 'completed') return
  exportDownloading.value = true
  try {
    await revenueAPI.downloadShareSettlementExport(exportTask.value.id, exportTask.value.file_name)
    appStore.showSuccess(t('admin.revenue.shareSettlements.exportDownloaded'))
  } catch (err: unknown) {
    appStore.showError(extractI18nErrorMessage(err, t, 'admin.revenue.shareSettlements.errors', t('admin.revenue.shareSettlements.exportDownloadFailed')))
  } finally {
    exportDownloading.value = false
  }
}

function startExportPolling() {
  stopExportPolling()
  if (!isExportActive.value) return
  exportPollTimer = window.setInterval(() => {
    void refreshExportTask()
  }, 2000)
}

function stopExportPolling() {
  if (exportPollTimer !== null) {
    window.clearInterval(exportPollTimer)
    exportPollTimer = null
  }
}

function resetAndLoad() {
  pagination.page = 1
  void loadSettlements()
}

function setRange(days: RangeDays) {
  if (rangeDays.value === days && selectedRangeDays.value === days) return
  rangeDays.value = days
  selectedRangeDays.value = days
  const range = getDateRange(days)
  startDate.value = range.start
  endDate.value = range.end
  resetAndLoad()
}

function applyCustomDateRange() {
  selectedRangeDays.value = null
  resetAndLoad()
}

function validateDateRange(): boolean {
  if (!startDate.value || !endDate.value || startDate.value > endDate.value) {
    appStore.showError(t('admin.revenue.shareSettlements.errors.REVENUE_TIME_RANGE_INVALID'))
    return false
  }
  if (getInclusiveDateSpanDays(startDate.value, endDate.value) > MAX_SETTLEMENT_RANGE_DAYS) {
    appStore.showError(t('admin.revenue.shareSettlements.errors.REVENUE_TIME_RANGE_TOO_LARGE'))
    return false
  }
  return true
}

function handlePageChange(page: number) {
  pagination.page = page
  void loadSettlements()
}

function handlePageSizeChange(pageSize: number) {
  pagination.page_size = pageSize
  pagination.page = 1
  void loadSettlements()
}

function getDateRange(days: RangeDays): { start: string; end: string } {
  const end = new Date()
  const start = new Date()
  start.setDate(end.getDate() - days + 1)
  return {
    start: formatDateParam(start),
    end: formatDateParam(end)
  }
}

function formatDateParam(date: Date): string {
  const year = date.getFullYear()
  const month = String(date.getMonth() + 1).padStart(2, '0')
  const day = String(date.getDate()).padStart(2, '0')
  return `${year}-${month}-${day}`
}

function getInclusiveDateSpanDays(start: string, end: string): number {
  const startTime = parseDateParam(start).getTime()
  const endTime = parseDateParam(end).getTime()
  return Math.floor((endTime - startTime) / 86_400_000) + 1
}

function parseDateParam(value: string): Date {
  const [year, month, day] = value.split('-').map(Number)
  return new Date(year, month - 1, day)
}

function formatAmount(value: number): string {
  return amountFormatter.value.format(Number.isFinite(value) ? value : 0)
}

function formatPercent(value: number): string {
  return percentFormatter.value.format(Number.isFinite(value) ? value : 0)
}

function formatInteger(value: number): string {
  return integerFormatter.value.format(Number.isFinite(value) ? value : 0)
}

function formatFileSize(value: number): string {
  if (!Number.isFinite(value) || value <= 0) return '0 B'
  const units = ['B', 'KB', 'MB', 'GB']
  let size = value
  let index = 0
  while (size >= 1024 && index < units.length - 1) {
    size /= 1024
    index++
  }
  return `${size.toFixed(index === 0 ? 0 : 1)} ${units[index]}`
}

function formatDateTime(value: string): string {
  const parsed = Date.parse(value)
  if (!Number.isFinite(parsed)) return '--'
  return new Intl.DateTimeFormat(locale.value, {
    year: 'numeric',
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit'
  }).format(new Date(parsed))
}

function statusLabel(value: string): string {
  const key = `admin.revenue.shareSettlements.status.${value}`
  const translated = t(key)
  return translated === key ? value : translated
}

function exportStatusLabel(value: RevenueShareSettlementExportStatus): string {
  const key = `admin.revenue.shareSettlements.exportStatus.${value}`
  const translated = t(key)
  return translated === key ? value : translated
}

function exportStatusClass(value: RevenueShareSettlementExportStatus): string {
  switch (value) {
    case 'pending':
      return 'bg-sky-50 text-sky-700 dark:bg-sky-900/30 dark:text-sky-300'
    case 'running':
      return 'bg-emerald-50 text-emerald-700 dark:bg-emerald-900/30 dark:text-emerald-300'
    case 'completed':
      return 'bg-indigo-50 text-indigo-700 dark:bg-indigo-900/30 dark:text-indigo-300'
    case 'failed':
      return 'bg-rose-50 text-rose-700 dark:bg-rose-900/30 dark:text-rose-300'
    case 'canceled':
      return 'bg-gray-100 text-gray-600 dark:bg-dark-700 dark:text-gray-300'
    default:
      return 'bg-gray-100 text-gray-600 dark:bg-dark-700 dark:text-gray-300'
  }
}

function statusClass(value: string): string {
  switch (value) {
    case 'applied':
      return 'bg-emerald-50 text-emerald-700 dark:bg-emerald-900/30 dark:text-emerald-300'
    case 'reversed':
      return 'bg-rose-50 text-rose-700 dark:bg-rose-900/30 dark:text-rose-300'
    case 'frozen':
      return 'bg-amber-50 text-amber-700 dark:bg-amber-900/30 dark:text-amber-300'
    default:
      return 'bg-gray-100 text-gray-600 dark:bg-dark-700 dark:text-gray-300'
  }
}

function getBrowserTimezone(): string {
  try {
    return Intl.DateTimeFormat().resolvedOptions().timeZone || 'UTC'
  } catch {
    return 'UTC'
  }
}

onMounted(() => {
  void loadSettlements()
})

onUnmounted(() => {
  stopExportPolling()
})
</script>
