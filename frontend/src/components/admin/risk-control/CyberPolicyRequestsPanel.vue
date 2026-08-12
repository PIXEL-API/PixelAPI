<template>
  <section class="card" data-testid="cyber-policy-requests-panel">
    <div class="flex flex-col gap-4 border-b border-gray-100 px-4 py-4 dark:border-dark-700 sm:px-6 lg:flex-row lg:items-start lg:justify-between">
      <div class="flex min-w-0 items-start gap-3">
        <span class="flex h-10 w-10 flex-shrink-0 items-center justify-center rounded-lg bg-amber-50 text-amber-600 dark:bg-amber-900/20 dark:text-amber-300">
          <Icon name="document" size="md" />
        </span>
        <div class="min-w-0">
          <h2 class="text-lg font-semibold text-gray-900 dark:text-white">
            {{ t('admin.riskControl.cyberRequests.title') }}
          </h2>
          <p class="mt-1 max-w-4xl text-sm leading-6 text-gray-500 dark:text-gray-400">
            {{ t('admin.riskControl.cyberRequests.description') }}
          </p>
        </div>
      </div>
      <button
        type="button"
        class="btn btn-secondary inline-flex min-h-11 w-full flex-shrink-0 items-center justify-center gap-2 sm:w-auto"
        :disabled="loading"
        :aria-busy="loading"
        @click="loadRequests"
      >
        <Icon name="refresh" size="sm" :class="loading ? 'animate-spin' : ''" />
        {{ t('admin.riskControl.refresh') }}
      </button>
    </div>

    <div class="space-y-4 border-b border-gray-100 p-4 dark:border-dark-700 sm:p-6">
      <form class="space-y-4" @submit.prevent="applyFilters">
        <div class="grid grid-cols-1 gap-4 sm:grid-cols-2 xl:grid-cols-3">
          <div>
            <label for="cyber-requests-user-query" class="input-label">
              {{ t('admin.riskControl.cyberRequests.filters.user') }}
            </label>
            <input
              id="cyber-requests-user-query"
              v-model.trim="filters.user_query"
              data-testid="cyber-requests-user-query"
              type="search"
              autocomplete="off"
              class="input min-h-11 w-full"
              :placeholder="t('admin.riskControl.cyberRequests.filters.userPlaceholder')"
            />
          </div>
          <div>
            <label for="cyber-requests-group-query" class="input-label">
              {{ t('admin.riskControl.cyberRequests.filters.group') }}
            </label>
            <input
              id="cyber-requests-group-query"
              v-model.trim="filters.group_query"
              data-testid="cyber-requests-group-query"
              type="search"
              autocomplete="off"
              class="input min-h-11 w-full"
              :placeholder="t('admin.riskControl.cyberRequests.filters.groupPlaceholder')"
            />
          </div>
          <div>
            <label for="cyber-requests-model" class="input-label">
              {{ t('admin.riskControl.cyberRequests.filters.model') }}
            </label>
            <input
              id="cyber-requests-model"
              v-model.trim="filters.model"
              type="search"
              autocomplete="off"
              class="input min-h-11 w-full"
              :placeholder="t('admin.riskControl.cyberRequests.filters.modelPlaceholder')"
            />
          </div>
          <div>
            <label for="cyber-requests-endpoint" class="input-label">
              {{ t('admin.riskControl.cyberRequests.filters.endpoint') }}
            </label>
            <input
              id="cyber-requests-endpoint"
              v-model.trim="filters.endpoint"
              type="search"
              autocomplete="off"
              class="input min-h-11 w-full"
              :placeholder="t('admin.riskControl.cyberRequests.filters.endpointPlaceholder')"
            />
          </div>
          <div>
            <label for="cyber-requests-from" class="input-label">
              {{ t('admin.riskControl.cyberRequests.filters.from') }}
            </label>
            <input
              id="cyber-requests-from"
              v-model="filters.from"
              type="datetime-local"
              class="input min-h-11 w-full"
            />
          </div>
          <div>
            <label for="cyber-requests-to" class="input-label">
              {{ t('admin.riskControl.cyberRequests.filters.to') }}
            </label>
            <input
              id="cyber-requests-to"
              v-model="filters.to"
              type="datetime-local"
              class="input min-h-11 w-full"
            />
          </div>
        </div>

        <div class="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
          <p class="text-xs leading-5 text-gray-500 dark:text-gray-400">
            {{ t('admin.riskControl.cyberRequests.filters.defaultRangeHint') }}
          </p>
          <div class="flex flex-col gap-2 sm:flex-row sm:justify-end">
            <button
              type="button"
              class="btn btn-secondary inline-flex min-h-11 items-center justify-center gap-2"
              :disabled="loading || exporting"
              @click="resetFilters"
            >
              {{ t('common.reset') }}
            </button>
            <button
              type="submit"
              data-testid="cyber-requests-search"
              class="btn btn-primary inline-flex min-h-11 items-center justify-center gap-2"
              :disabled="loading || exporting"
              :aria-busy="loading"
            >
              <Icon name="search" size="sm" />
              {{ t('common.search') }}
            </button>
            <button
              type="button"
              data-testid="cyber-requests-export"
              class="btn btn-secondary inline-flex min-h-11 items-center justify-center gap-2"
              :disabled="loading || exporting"
              :aria-busy="exporting"
              @click="exportRequests"
            >
              <Icon name="download" size="sm" :class="exporting ? 'animate-pulse' : ''" />
              {{ exporting ? t('admin.riskControl.cyberRequests.exporting') : t('admin.riskControl.cyberRequests.export') }}
            </button>
          </div>
        </div>
      </form>

      <div
        v-if="exportTruncatedNotice"
        data-testid="cyber-requests-export-truncated"
        class="flex items-start gap-2 rounded-lg border border-amber-200 bg-amber-50 px-4 py-3 text-sm text-amber-800 dark:border-amber-900/60 dark:bg-amber-950/20 dark:text-amber-200"
        role="status"
      >
        <Icon name="exclamationTriangle" size="sm" class="mt-0.5 flex-shrink-0" />
        <span>{{ exportTruncatedNotice }}</span>
      </div>
    </div>

    <div class="overflow-x-auto" data-testid="cyber-requests-table-scroll">
      <table class="min-w-[1080px] divide-y divide-gray-200 dark:divide-dark-700">
        <thead class="bg-gray-50 dark:bg-dark-800">
          <tr>
            <th class="px-5 py-3 text-left text-xs font-medium uppercase tracking-wider text-gray-500 dark:text-gray-400">{{ t('admin.riskControl.cyberRequests.table.time') }}</th>
            <th class="px-5 py-3 text-left text-xs font-medium uppercase tracking-wider text-gray-500 dark:text-gray-400">{{ t('admin.riskControl.cyberRequests.table.group') }}</th>
            <th class="px-5 py-3 text-left text-xs font-medium uppercase tracking-wider text-gray-500 dark:text-gray-400">{{ t('admin.riskControl.cyberRequests.table.user') }}</th>
            <th class="px-5 py-3 text-left text-xs font-medium uppercase tracking-wider text-gray-500 dark:text-gray-400">{{ t('admin.riskControl.cyberRequests.table.route') }}</th>
            <th class="px-5 py-3 text-left text-xs font-medium uppercase tracking-wider text-gray-500 dark:text-gray-400">{{ t('admin.riskControl.cyberRequests.table.status') }}</th>
            <th class="px-5 py-3 text-left text-xs font-medium uppercase tracking-wider text-gray-500 dark:text-gray-400">{{ t('admin.riskControl.cyberRequests.table.content') }}</th>
          </tr>
        </thead>
        <tbody class="divide-y divide-gray-100 bg-white dark:divide-dark-800 dark:bg-dark-800">
          <tr v-if="loading">
            <td colspan="6" class="px-5 py-12 text-center text-sm text-gray-500 dark:text-gray-400">
              {{ t('common.loading') }}
            </td>
          </tr>
          <tr v-else-if="requests.length === 0">
            <td colspan="6" class="px-5 py-12 text-center text-sm text-gray-500 dark:text-gray-400">
              {{ t('admin.riskControl.cyberRequests.empty') }}
            </td>
          </tr>
          <template v-else>
          <tr
            v-for="row in requests"
            :key="row.id"
            class="transition-colors hover:bg-gray-50 dark:hover:bg-dark-700/60"
          >
            <td class="whitespace-nowrap px-5 py-4 text-sm text-gray-700 dark:text-gray-300">
              <div>{{ formatDateTime(row.created_at) }}</div>
              <div class="mt-1 max-w-44 truncate text-xs text-gray-400" :title="row.request_id">
                {{ row.request_id || '-' }}
              </div>
            </td>
            <td class="px-5 py-4 text-sm text-gray-700 dark:text-gray-300">
              <div class="max-w-48 truncate font-medium" :title="row.group_name">{{ row.group_name || '-' }}</div>
              <div v-if="row.group_id" class="mt-1 text-xs text-gray-400">ID {{ row.group_id }}</div>
            </td>
            <td class="px-5 py-4 text-sm text-gray-700 dark:text-gray-300">
              <div class="max-w-56 truncate font-medium" :title="row.user_name || row.user_email">{{ row.user_name || row.user_email || '-' }}</div>
              <div v-if="row.user_name && row.user_email" class="mt-1 max-w-56 truncate text-xs text-gray-400" :title="row.user_email">{{ row.user_email }}</div>
              <div v-if="row.user_id" class="mt-1 text-xs text-gray-400">UID {{ row.user_id }}</div>
            </td>
            <td class="px-5 py-4 text-sm text-gray-700 dark:text-gray-300">
              <div class="max-w-56 truncate" :title="row.requested_model">{{ row.requested_model || '-' }}</div>
              <div class="mt-1 max-w-56 truncate text-xs text-gray-400" :title="row.inbound_endpoint">{{ row.inbound_endpoint || '-' }}</div>
            </td>
            <td class="px-5 py-4 text-sm text-gray-700 dark:text-gray-300">
              <span class="inline-flex rounded-md bg-red-50 px-2 py-1 text-xs font-medium text-red-700 dark:bg-red-900/20 dark:text-red-300">
                {{ statusText(row) }}
              </span>
              <div v-if="row.provider_error_code" class="mt-1 max-w-44 truncate text-xs text-gray-400" :title="row.provider_error_code">
                {{ row.provider_error_code }}
              </div>
            </td>
            <td class="w-[360px] max-w-[360px] px-5 py-4 text-sm text-gray-700 dark:text-gray-300">
              <button
                type="button"
                class="group flex min-h-11 w-full min-w-0 items-center gap-2 rounded-lg px-2 py-1.5 text-left transition-colors hover:bg-gray-100 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary-500 dark:hover:bg-dark-700"
                :title="contentPreview(row)"
                @click="openDetail(row)"
              >
                <span class="min-w-0 flex-1 truncate">{{ contentPreview(row) }}</span>
                <span
                  v-if="row.request_content_truncated"
                  class="inline-flex flex-shrink-0 rounded bg-amber-50 px-1.5 py-0.5 text-[11px] font-medium text-amber-700 dark:bg-amber-900/20 dark:text-amber-300"
                >
                  {{ t('admin.riskControl.cyberRequests.truncated') }}
                </span>
                <Icon name="eye" size="xs" class="flex-shrink-0 text-gray-300 transition-colors group-hover:text-primary-500 dark:text-gray-500" />
              </button>
            </td>
          </tr>
          </template>
        </tbody>
      </table>
    </div>

    <Pagination
      v-if="pagination.total > 0"
      :page="pagination.page"
      :total="pagination.total"
      :page-size="pagination.page_size"
      @update:page="changePage"
      @update:pageSize="changePageSize"
    />

    <BaseDialog
      :show="selectedRequest !== null"
      :title="t('admin.riskControl.cyberRequests.detailTitle')"
      width="wide"
      @close="closeDetail"
    >
      <div v-if="selectedRequest" class="space-y-5" data-testid="cyber-request-detail">
        <div v-if="detailLoading" class="flex min-h-48 items-center justify-center text-sm text-gray-500 dark:text-gray-400">
          {{ t('common.loading') }}
        </div>
        <div v-else-if="detailFailed" class="rounded-lg border border-red-200 bg-red-50 px-4 py-3 text-sm text-red-700 dark:border-red-900/60 dark:bg-red-950/20 dark:text-red-300">
          {{ t('admin.riskControl.cyberRequests.detailFailed') }}
        </div>
        <template v-else-if="requestDetail">
          <div class="grid grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-4 xl:grid-cols-5">
            <div class="rounded-lg border border-gray-100 bg-gray-50 p-4 dark:border-dark-700 dark:bg-dark-800/70">
              <p class="text-xs font-medium text-gray-500 dark:text-gray-400">{{ t('admin.riskControl.cyberRequests.table.time') }}</p>
              <p class="mt-1 break-words text-sm font-semibold text-gray-900 dark:text-white">{{ formatDateTime(requestDetail.created_at) }}</p>
            </div>
            <div class="rounded-lg border border-gray-100 bg-gray-50 p-4 dark:border-dark-700 dark:bg-dark-800/70">
              <p class="text-xs font-medium text-gray-500 dark:text-gray-400">{{ t('admin.riskControl.cyberRequests.table.user') }}</p>
              <p class="mt-1 break-words text-sm font-semibold text-gray-900 dark:text-white">{{ requestDetail.user_name || requestDetail.user_email || '-' }}</p>
              <p v-if="requestDetail.user_id" class="mt-1 text-xs text-gray-400">UID {{ requestDetail.user_id }}</p>
            </div>
            <div class="rounded-lg border border-gray-100 bg-gray-50 p-4 dark:border-dark-700 dark:bg-dark-800/70">
              <p class="text-xs font-medium text-gray-500 dark:text-gray-400">{{ t('admin.riskControl.cyberRequests.table.group') }}</p>
              <p class="mt-1 break-words text-sm font-semibold text-gray-900 dark:text-white">{{ requestDetail.group_name || '-' }}</p>
              <p v-if="requestDetail.group_id" class="mt-1 text-xs text-gray-400">ID {{ requestDetail.group_id }}</p>
            </div>
            <div class="rounded-lg border border-gray-100 bg-gray-50 p-4 dark:border-dark-700 dark:bg-dark-800/70">
              <p class="text-xs font-medium text-gray-500 dark:text-gray-400">{{ t('admin.riskControl.cyberRequests.table.status') }}</p>
              <p class="mt-1 break-words text-sm font-semibold text-gray-900 dark:text-white">{{ statusText(requestDetail) }}</p>
            </div>
            <div class="rounded-lg border border-gray-100 bg-gray-50 p-4 dark:border-dark-700 dark:bg-dark-800/70">
              <p class="text-xs font-medium text-gray-500 dark:text-gray-400">{{ t('admin.riskControl.cyberRequests.requestBytes') }}</p>
              <p class="mt-1 break-words text-sm font-semibold text-gray-900 dark:text-white">{{ formatRequestBytes(requestDetail.request_content_bytes) }}</p>
            </div>
          </div>

          <div class="rounded-xl border border-gray-100 bg-white p-4 shadow-sm dark:border-dark-700 dark:bg-dark-800">
            <div class="flex flex-col gap-2 sm:flex-row sm:items-start sm:justify-between">
              <div>
                <p class="text-sm font-semibold text-gray-900 dark:text-white">{{ t('admin.riskControl.cyberRequests.contentTitle') }}</p>
                <p class="mt-1 text-xs leading-5 text-gray-500 dark:text-gray-400">
                  {{ t('admin.riskControl.cyberRequests.contentNotice') }}
                </p>
              </div>
              <span
                v-if="requestDetail.request_content_truncated"
                class="inline-flex flex-shrink-0 rounded-md bg-amber-50 px-2.5 py-1 text-xs font-medium text-amber-700 dark:bg-amber-900/20 dark:text-amber-300"
              >
                {{ t('admin.riskControl.cyberRequests.truncated') }}
              </span>
            </div>
            <pre class="mt-4 max-h-[420px] overflow-auto whitespace-pre-wrap break-words rounded-lg bg-gray-950 p-4 text-sm leading-6 text-gray-100 shadow-inner dark:bg-black/50">{{ requestDetail.request_content || t('admin.riskControl.cyberRequests.noContent') }}</pre>
          </div>

          <div v-if="requestDetail.upstream_error_message || requestDetail.upstream_error_detail || requestDetail.upstream_errors" class="rounded-xl border border-gray-100 bg-white p-4 shadow-sm dark:border-dark-700 dark:bg-dark-800">
            <p class="text-sm font-semibold text-gray-900 dark:text-white">{{ t('admin.riskControl.cyberRequests.upstreamErrorTitle') }}</p>
            <pre class="mt-3 max-h-64 overflow-auto whitespace-pre-wrap break-words rounded-lg bg-gray-50 p-4 text-sm leading-6 text-gray-700 dark:bg-dark-900 dark:text-gray-200">{{ upstreamErrorText }}</pre>
          </div>
        </template>
      </div>

      <template #footer>
        <div class="flex justify-end">
          <button type="button" class="btn btn-secondary min-h-11" @click="closeDetail">{{ t('common.close') }}</button>
        </div>
      </template>
    </BaseDialog>
  </section>
</template>

<script setup lang="ts">
import { computed, onMounted, onUnmounted, reactive, ref } from 'vue'
import { saveAs } from 'file-saver'
import { useI18n } from 'vue-i18n'

import { adminAPI } from '@/api/admin'
import type {
  CyberPolicyRequestDetail,
  CyberPolicyRequestFilters,
  CyberPolicyRequestRecord,
} from '@/api/admin/riskControl'
import BaseDialog from '@/components/common/BaseDialog.vue'
import Pagination from '@/components/common/Pagination.vue'
import Icon from '@/components/icons/Icon.vue'
import { useAppStore } from '@/stores/app'
import { extractApiErrorMessage } from '@/utils/apiError'
import { formatDateTime as formatDateTimeValue } from '@/utils/format'

const { t } = useI18n()
const appStore = useAppStore()

const requests = ref<CyberPolicyRequestRecord[]>([])
const loading = ref(false)
const exporting = ref(false)
const exportTruncatedNotice = ref('')
const selectedRequest = ref<CyberPolicyRequestRecord | null>(null)
const requestDetail = ref<CyberPolicyRequestDetail | null>(null)
const detailLoading = ref(false)
const detailFailed = ref(false)

const filters = reactive({
  user_query: '',
  group_query: '',
  model: '',
  endpoint: '',
  from: '',
  to: '',
})

const pagination = reactive({
  page: 1,
  page_size: 20,
  total: 0,
  pages: 1,
})

let listAbortController: AbortController | null = null
let detailAbortController: AbortController | null = null
let exportAbortController: AbortController | null = null

const upstreamErrorText = computed(() => {
  if (!requestDetail.value) return '-'
  return [
    requestDetail.value.upstream_error_message,
    requestDetail.value.upstream_error_detail,
    requestDetail.value.upstream_errors,
  ].filter(Boolean).join('\n\n') || '-'
})

function isCanceled(error: unknown): boolean {
  const candidate = error as { name?: string; code?: string }
  return candidate?.name === 'AbortError'
    || candidate?.name === 'CanceledError'
    || candidate?.code === 'ERR_CANCELED'
}

function normalizeDateTimeLocal(value: string): string | undefined {
  if (!value) return undefined
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return undefined
  return date.toISOString()
}

function validateTimeRange(): boolean {
  if (!filters.from || !filters.to) return true
  const start = new Date(filters.from)
  const end = new Date(filters.to)
  if (Number.isNaN(start.getTime()) || Number.isNaN(end.getTime()) || start >= end) {
    appStore.showError(t('admin.riskControl.cyberRequests.invalidRange'))
    return false
  }
  const maxWindowMs = 31 * 24 * 60 * 60 * 1000
  if (end.getTime() - start.getTime() > maxWindowMs) {
    appStore.showError(t('admin.riskControl.cyberRequests.rangeTooLong'))
    return false
  }
  return true
}

function buildFilters(includePagination: boolean): CyberPolicyRequestFilters {
  return {
    page: includePagination ? pagination.page : undefined,
    page_size: includePagination ? pagination.page_size : undefined,
    user_query: filters.user_query || undefined,
    group_query: filters.group_query || undefined,
    model: filters.model || undefined,
    endpoint: filters.endpoint || undefined,
    from: normalizeDateTimeLocal(filters.from),
    to: normalizeDateTimeLocal(filters.to),
  }
}

async function loadRequests(): Promise<void> {
  if (!validateTimeRange()) return
  listAbortController?.abort()
  const controller = new AbortController()
  listAbortController = controller
  loading.value = true
  try {
    const result = await adminAPI.riskControl.listCyberPolicyRequests(
      buildFilters(true),
      { signal: controller.signal }
    )
    if (controller.signal.aborted) return
    requests.value = result.items || []
    pagination.total = result.total || 0
    pagination.page = result.page || 1
    pagination.page_size = result.page_size || pagination.page_size
    pagination.pages = result.pages || Math.max(1, Math.ceil(pagination.total / pagination.page_size))
  } catch (error: unknown) {
    if (!isCanceled(error)) {
      appStore.showError(extractApiErrorMessage(error, t('admin.riskControl.cyberRequests.loadFailed')))
    }
  } finally {
    if (listAbortController === controller) {
      listAbortController = null
      loading.value = false
    }
  }
}

function applyFilters(): void {
  pagination.page = 1
  void loadRequests()
}

function resetFilters(): void {
  Object.assign(filters, {
    user_query: '',
    group_query: '',
    model: '',
    endpoint: '',
    from: '',
    to: '',
  })
  exportTruncatedNotice.value = ''
  pagination.page = 1
  void loadRequests()
}

function changePage(page: number): void {
  pagination.page = page
  void loadRequests()
}

function changePageSize(pageSize: number): void {
  pagination.page = 1
  pagination.page_size = pageSize
  void loadRequests()
}

async function exportRequests(): Promise<void> {
  if (exporting.value || !validateTimeRange()) return
  exportAbortController?.abort()
  const controller = new AbortController()
  exportAbortController = controller
  exporting.value = true
  exportTruncatedNotice.value = ''
  try {
    const result = await adminAPI.riskControl.exportCyberPolicyRequests(
      buildFilters(false),
      { signal: controller.signal }
    )
    if (controller.signal.aborted) return
    saveAs(result.blob, result.filename)
    if (result.truncated) {
      exportTruncatedNotice.value = t('admin.riskControl.cyberRequests.exportTruncated', {
        limit: result.limit,
      })
      appStore.showWarning(exportTruncatedNotice.value)
    } else {
      appStore.showSuccess(t('admin.riskControl.cyberRequests.exportSuccess'))
    }
  } catch (error: unknown) {
    if (!isCanceled(error)) {
      appStore.showError(extractApiErrorMessage(error, t('admin.riskControl.cyberRequests.exportFailed')))
    }
  } finally {
    if (exportAbortController === controller) {
      exportAbortController = null
      exporting.value = false
    }
  }
}

async function openDetail(row: CyberPolicyRequestRecord): Promise<void> {
  detailAbortController?.abort()
  const controller = new AbortController()
  detailAbortController = controller
  selectedRequest.value = row
  requestDetail.value = null
  detailFailed.value = false
  detailLoading.value = true
  try {
    const detail = await adminAPI.riskControl.getCyberPolicyRequest(row.id, {
      signal: controller.signal,
    })
    if (controller.signal.aborted) return
    requestDetail.value = detail
  } catch (error: unknown) {
    if (!isCanceled(error)) {
      detailFailed.value = true
      appStore.showError(extractApiErrorMessage(error, t('admin.riskControl.cyberRequests.detailFailed')))
    }
  } finally {
    if (detailAbortController === controller) {
      detailAbortController = null
      detailLoading.value = false
    }
  }
}

function closeDetail(): void {
  detailAbortController?.abort()
  detailAbortController = null
  selectedRequest.value = null
  requestDetail.value = null
  detailLoading.value = false
  detailFailed.value = false
}

function contentPreview(row: CyberPolicyRequestRecord): string {
  return row.request_content_preview || row.upstream_error_message || '-'
}

function statusText(row: CyberPolicyRequestRecord): string {
  if (row.upstream_status_code) {
    return `${row.status_code} / ${row.upstream_status_code}`
  }
  return String(row.status_code || '-')
}

function formatRequestBytes(value: number | null): string {
  return value === null ? '-' : value.toLocaleString()
}

function formatDateTime(value: string): string {
  return formatDateTimeValue(value) || '-'
}

onMounted(() => {
  void loadRequests()
})

onUnmounted(() => {
  listAbortController?.abort()
  detailAbortController?.abort()
  exportAbortController?.abort()
})
</script>
