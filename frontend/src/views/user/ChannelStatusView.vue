<template>
  <AppLayout>
    <MonitorHero
      :overall-status="overallStatus"
      :interval-seconds="DEFAULT_INTERVAL_SECONDS"
      :window="currentWindow"
      :loading="loading"
      :auto-refresh="autoRefresh"
      @update:window="handleWindowChange"
      @refresh="manualReload"
    />

    <div class="mb-5 grid grid-cols-1 gap-4 xl:grid-cols-2">
      <AccountQuotaDashboardPanel
        :dashboard="quotaPoolDashboard?.mine ?? null"
        :loading="quotaPoolLoading"
        :error="quotaPoolError"
        :title="t('channelStatus.quotaPool.mineTitle')"
        :subtitle="t('channelStatus.quotaPool.mineSubtitle')"
        :empty-message="t('channelStatus.quotaPool.mineEmpty')"
        :load-failed-message="t('channelStatus.quotaPool.loadFailed')"
        @refresh="reloadQuotaPool(false)"
      />

      <AccountQuotaDashboardPanel
        :dashboard="quotaPoolDashboard?.platform ?? null"
        :loading="quotaPoolLoading"
        :error="quotaPoolError"
        :title="t('channelStatus.quotaPool.platformTitle')"
        :subtitle="t('channelStatus.quotaPool.platformSubtitle')"
        :empty-message="t('channelStatus.quotaPool.platformEmpty')"
        :load-failed-message="t('channelStatus.quotaPool.loadFailed')"
        @refresh="reloadQuotaPool(false)"
      />
    </div>

    <section class="mb-5 rounded-lg border border-gray-200 bg-white p-3 shadow-sm dark:border-dark-700 dark:bg-dark-800">
      <div class="flex flex-wrap items-center justify-between gap-3">
        <div class="flex min-w-0 items-center gap-3">
          <div class="rounded-lg bg-sky-100 p-2 text-sky-700 dark:bg-sky-900/30 dark:text-sky-300">
            <Icon name="server" size="md" />
          </div>
          <div class="min-w-0">
            <h2 class="text-sm font-semibold text-gray-900 dark:text-white">
              {{ t('channelStatus.capacity.title') }}
            </h2>
            <p class="mt-0.5 text-xs text-gray-500 dark:text-gray-400">
              {{ t('channelStatus.capacity.subtitle') }}
            </p>
          </div>
        </div>
        <button
          type="button"
          class="btn btn-secondary px-2 py-1.5 text-xs"
          :disabled="capacityLoading"
          @click="reloadCapacity(false)"
        >
          <Icon name="refresh" size="sm" :class="{ 'animate-spin': capacityLoading }" />
          <span class="hidden sm:inline">{{ t('common.refresh') }}</span>
        </button>
      </div>

      <div v-if="capacityLoading && !capacitySummary" class="mt-3 grid grid-cols-1 gap-3 md:grid-cols-3">
        <div v-for="idx in 3" :key="idx" class="h-16 animate-pulse rounded-lg bg-gray-100 dark:bg-dark-700" />
      </div>
      <div v-else-if="capacityError" class="mt-3 rounded-md border border-red-200 bg-red-50 px-3 py-2 text-sm text-red-700 dark:border-red-900/50 dark:bg-red-900/20 dark:text-red-300">
        {{ t('channelStatus.capacity.loadFailed') }}
      </div>
      <div v-else-if="!hasCapacityItems" class="mt-3 rounded-md border border-dashed border-gray-200 px-3 py-4 text-sm text-gray-500 dark:border-dark-600 dark:text-gray-400">
        {{ t('channelStatus.capacity.empty') }}
      </div>
      <div v-else class="mt-3 space-y-3">
        <div class="rounded-md bg-gray-50 p-3 dark:bg-dark-700/60">
          <div class="mb-2 text-xs font-semibold uppercase text-gray-500 dark:text-gray-400">
            {{ t('channelStatus.capacity.total') }}
          </div>
          <GroupCapacityBadge
            :concurrency-used="capacitySummary!.total.concurrency_used"
            :concurrency-max="capacitySummary!.total.concurrency_max"
            :sessions-used="capacitySummary!.total.sessions_used"
            :sessions-max="capacitySummary!.total.sessions_max"
            :rpm-used="capacitySummary!.total.rpm_used"
            :rpm-max="capacitySummary!.total.rpm_max"
          />
        </div>

        <div class="grid grid-cols-1 gap-2 md:grid-cols-2 xl:grid-cols-3">
          <article
            v-for="item in capacitySummary!.items"
            :key="item.group_id"
            class="rounded-md border border-gray-200 p-3 dark:border-dark-700"
          >
            <div class="mb-2 flex min-w-0 items-center justify-between gap-2">
              <span class="truncate text-sm font-semibold text-gray-900 dark:text-white">
                {{ item.group_name || t('admin.accounts.quotaDashboard.ungrouped') }}
              </span>
              <span class="shrink-0 rounded-md bg-gray-100 px-1.5 py-0.5 text-[10px] font-medium text-gray-600 dark:bg-dark-700 dark:text-gray-300">
                {{ platformLabel(item.group_platform) }}
              </span>
            </div>
            <GroupCapacityBadge
              :concurrency-used="item.concurrency_used"
              :concurrency-max="item.concurrency_max"
              :sessions-used="item.sessions_used"
              :sessions-max="item.sessions_max"
              :rpm-used="item.rpm_used"
              :rpm-max="item.rpm_max"
            />
          </article>
        </div>
      </div>
    </section>

    <MonitorCardGrid
      :items="items"
      :window="currentWindow"
      :countdown-seconds="countdown"
      :loading="loading"
      :detail-cache="detailCache"
      @card-click="openDetail"
    />

    <MonitorDetailDialog
      :show="showDetail"
      :monitor-id="detailTarget?.id ?? null"
      :title="detailTitle"
      @close="closeDetail"
    />
  </AppLayout>
</template>

<script setup lang="ts">
import { ref, reactive, computed, onMounted, onBeforeUnmount, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAppStore } from '@/stores/app'
import { extractApiErrorMessage } from '@/utils/apiError'
import { getQuotaDashboard as fetchQuotaPoolDashboard } from '@/api/accounts'
import {
  list as listChannelMonitorViews,
  status as fetchChannelMonitorDetail,
  capacitySummary as fetchCapacitySummary,
  type UserMonitorView,
  type UserMonitorDetail,
  type ChannelMonitorCapacitySummaryResponse,
} from '@/api/channelMonitor'
import type { UserAccountQuotaPoolDashboard } from '@/types'
import AppLayout from '@/components/layout/AppLayout.vue'
import AccountQuotaDashboardPanel from '@/components/account/AccountQuotaDashboard.vue'
import GroupCapacityBadge from '@/components/common/GroupCapacityBadge.vue'
import MonitorHero, {
  type MonitorWindow,
  type OverallStatus,
} from '@/components/user/monitor/MonitorHero.vue'
import MonitorCardGrid from '@/components/user/monitor/MonitorCardGrid.vue'
import MonitorDetailDialog from '@/components/user/MonitorDetailDialog.vue'
import Icon from '@/components/icons/Icon.vue'
import { DEFAULT_INTERVAL_SECONDS, STATUS_OPERATIONAL } from '@/constants/channelMonitor'
import { useAutoRefresh } from '@/composables/useAutoRefresh'
import { platformLabel } from '@/utils/platformColors'

const { t } = useI18n()
const appStore = useAppStore()

// ── State ──
const items = ref<UserMonitorView[]>([])
const loading = ref(false)
const quotaPoolDashboard = ref<UserAccountQuotaPoolDashboard | null>(null)
const quotaPoolLoading = ref(false)
const quotaPoolError = ref(false)
const capacitySummary = ref<ChannelMonitorCapacitySummaryResponse | null>(null)
const capacityLoading = ref(false)
const capacityError = ref(false)
const currentWindow = ref<MonitorWindow>('7d')
const detailCache = reactive<Record<number, UserMonitorDetail>>({})
const showDetail = ref(false)
const detailTarget = ref<UserMonitorView | null>(null)

let abortController: AbortController | null = null
let quotaPoolAbortController: AbortController | null = null
let capacityAbortController: AbortController | null = null

const autoRefresh = useAutoRefresh({
  storageKey: 'channel-status-auto-refresh',
  intervals: [30, 60, 120] as const,
  defaultInterval: DEFAULT_INTERVAL_SECONDS,
  onRefresh: () => reloadAll(true),
  shouldPause: () => document.hidden || loading.value || quotaPoolLoading.value || capacityLoading.value,
})
const countdown = autoRefresh.countdown

// ── Computed ──
const overallStatus = computed<OverallStatus>(() => {
  if (quotaPoolDashboard.value?.platform?.group_summaries?.some(summary => {
    if (summary.group_status && summary.group_status !== 'active') return true
    if (summary.account_count > 0 && summary.schedulable_account_count === 0) return true
    if (summary.schedulable_account_count < summary.active_account_count) return true
    return summary.usage_windows?.some(window => window.average_utilization >= 80) ?? false
  })) {
    return 'degraded'
  }
  if (items.value.length === 0) return 'operational'
  for (const it of items.value) {
    if (it.primary_status === 'failed' || it.primary_status === 'error') return 'degraded'
    if (it.primary_status !== STATUS_OPERATIONAL) return 'degraded'
  }
  return 'operational'
})

const detailTitle = computed(() => {
  return detailTarget.value?.name || t('channelStatus.detailTitle')
})

const hasCapacityItems = computed(() => (capacitySummary.value?.items?.length ?? 0) > 0)

// ── Loaders ──
async function reload(silent = false) {
  if (abortController) abortController.abort()
  const ctrl = new AbortController()
  abortController = ctrl
  if (!silent) loading.value = true
  try {
    const res = await listChannelMonitorViews({ signal: ctrl.signal })
    if (ctrl.signal.aborted || abortController !== ctrl) return
    items.value = res.items || []
  } catch (err: unknown) {
    const e = err as { name?: string; code?: string }
    if (e?.name === 'AbortError' || e?.code === 'ERR_CANCELED') return
    appStore.showError(extractApiErrorMessage(err, t('channelStatus.loadError')))
  } finally {
    if (abortController === ctrl) {
      if (!silent) loading.value = false
      countdown.value = DEFAULT_INTERVAL_SECONDS
      abortController = null
    }
  }
}

async function reloadQuotaPool(silent = false) {
  if (quotaPoolAbortController) quotaPoolAbortController.abort()
  const ctrl = new AbortController()
  quotaPoolAbortController = ctrl
  if (!silent) quotaPoolLoading.value = true
  quotaPoolError.value = false
  try {
    const dashboard = await fetchQuotaPoolDashboard({ signal: ctrl.signal })
    if (ctrl.signal.aborted || quotaPoolAbortController !== ctrl) return
    quotaPoolDashboard.value = dashboard
  } catch (err: unknown) {
    const e = err as { name?: string; code?: string }
    if (e?.name === 'AbortError' || e?.code === 'ERR_CANCELED') return
    quotaPoolError.value = true
    appStore.showError(extractApiErrorMessage(err, t('channelStatus.quotaPool.loadFailed')))
  } finally {
    if (quotaPoolAbortController === ctrl) {
      if (!silent) quotaPoolLoading.value = false
      quotaPoolAbortController = null
    }
  }
}

async function reloadCapacity(silent = false) {
  if (capacityAbortController) capacityAbortController.abort()
  const ctrl = new AbortController()
  capacityAbortController = ctrl
  if (!silent) capacityLoading.value = true
  capacityError.value = false
  try {
    const summary = await fetchCapacitySummary({ signal: ctrl.signal })
    if (ctrl.signal.aborted || capacityAbortController !== ctrl) return
    capacitySummary.value = summary
  } catch (err: unknown) {
    const e = err as { name?: string; code?: string }
    if (e?.name === 'AbortError' || e?.code === 'ERR_CANCELED') return
    capacityError.value = true
    appStore.showError(extractApiErrorMessage(err, t('channelStatus.capacity.loadFailed')))
  } finally {
    if (capacityAbortController === ctrl) {
      if (!silent) capacityLoading.value = false
      capacityAbortController = null
    }
  }
}

async function reloadAll(silent = false) {
  await Promise.all([
    reload(silent),
    reloadQuotaPool(silent),
    reloadCapacity(silent)
  ])
}

async function manualReload() {
  await reloadAll(false)
  // After base reload, refresh any cached detail records so non-7d availability
  // values stay in sync without forcing the user to switch tabs again.
  if (currentWindow.value !== '7d') {
    await Promise.all(items.value.map(it => loadDetail(it.id, true)))
  }
}

async function loadDetail(id: number, force = false) {
  if (!force && detailCache[id]) return
  try {
    detailCache[id] = await fetchChannelMonitorDetail(id)
  } catch (err: unknown) {
    appStore.showError(extractApiErrorMessage(err, t('channelStatus.detailLoadError')))
  }
}

async function ensureDetailsForWindow() {
  if (currentWindow.value === '7d') return
  await Promise.all(items.value.map(it => loadDetail(it.id)))
}

// ── Handlers ──
async function handleWindowChange(value: MonitorWindow) {
  currentWindow.value = value
  await ensureDetailsForWindow()
}

function openDetail(row: UserMonitorView) {
  detailTarget.value = row
  showDetail.value = true
}

function closeDetail() {
  showDetail.value = false
  detailTarget.value = null
}

watch(items, () => {
  void ensureDetailsForWindow()
})

watch(
  () => appStore.cachedPublicSettings?.channel_monitor_enabled,
  (enabled) => {
    if (enabled === false) autoRefresh.stop()
    else if (autoRefresh.enabled.value) autoRefresh.start()
  },
)

onMounted(() => {
  void reloadAll(false)
  if (appStore.cachedPublicSettings?.channel_monitor_enabled !== false) {
    autoRefresh.setEnabled(true)
  }
})

onBeforeUnmount(() => {
  if (abortController) abortController.abort()
  if (quotaPoolAbortController) quotaPoolAbortController.abort()
  if (capacityAbortController) capacityAbortController.abort()
})
</script>
