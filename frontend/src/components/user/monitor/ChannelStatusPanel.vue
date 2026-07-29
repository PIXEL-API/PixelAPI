<template>
  <div class="min-w-0">
    <MonitorHero
      :overall-status="overallStatus"
      :overall-detail="overallDetail"
      :window="currentWindow"
      :loading="loading"
      :status-loading="statusLoading"
      :minimum-availability="minimumAvailability"
      :availability-loading="availabilityLoading"
      :auto-refresh="autoRefresh"
      @update:window="handleWindowChange"
      @refresh="manualReload"
    />

    <details
      open
      class="group/details min-w-0 overflow-hidden"
    >
      <summary
        v-if="compact"
        class="mb-4 flex min-h-11 cursor-pointer list-none items-center justify-between gap-3 rounded-xl border border-gray-200 bg-gray-50/80 px-4 py-2.5 text-sm font-medium text-gray-700 transition-colors hover:bg-gray-100 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary-500/60 dark:border-dark-700 dark:bg-dark-800/60 dark:text-gray-200 dark:hover:bg-dark-800 [&::-webkit-details-marker]:hidden"
      >
        <span>
          <span class="block">{{ t('channelStatus.details.title') }}</span>
          <span class="mt-0.5 block text-xs font-normal text-gray-500 dark:text-gray-400">
            {{
              t(
                showMonitorGrid
                  ? 'channelStatus.details.description'
                  : 'channelStatus.details.capacityDescription',
              )
            }}
          </span>
        </span>
        <Icon
          name="chevronDown"
          size="sm"
          class="shrink-0 transition-transform duration-200 group-open/details:rotate-180"
        />
      </summary>

      <div class="mb-5 grid min-w-0 grid-cols-1 gap-4 xl:grid-cols-2">
      <AccountQuotaDashboardPanel
        :dashboard="quotaPoolDashboard?.mine ?? null"
        :loading="quotaPoolLoading"
        :error="quotaPoolError"
        :show-summary-breakdown="false"
        :title="t('channelStatus.quotaPool.mineTitle')"
        :subtitle="t('channelStatus.quotaPool.mineSubtitle')"
        :empty-message="t('channelStatus.quotaPool.mineEmpty')"
        :load-failed-message="t('channelStatus.quotaPool.loadFailed')"
        @refresh="reloadQuotaPool(false)"
      />

      <div class="min-w-0">
        <AccountQuotaDashboardPanel
          :dashboard="quotaPoolDashboard?.platform ?? null"
          :loading="quotaPoolLoading"
          :error="quotaPoolError"
          :show-summary-breakdown="false"
          :group-capacity-by-id="groupCapacityById"
          :group-rate-by-id="groupRateById"
          :group-models-by-id="props.groupModelsById"
          :available-groups-by-id="props.availableGroupsById"
          :user-group-rates="props.userGroupRates"
          :title="t('channelStatus.quotaPool.platformTitle')"
          :subtitle="t('channelStatus.quotaPool.platformSubtitle')"
          :empty-message="t('channelStatus.quotaPool.platformEmpty')"
          :load-failed-message="t('channelStatus.quotaPool.loadFailed')"
          @refresh="reloadPlatformPool(false)"
        />
        <div
          v-if="capacityLoading || groupRateLoading"
          class="mt-2 text-xs text-gray-500 dark:text-gray-400"
        >
          {{ t('common.loading') }}
        </div>
        <div
          v-else-if="capacityError"
          class="mt-2 flex items-center justify-between gap-3 rounded-lg border border-red-200 bg-red-50 px-3 py-2 text-xs text-red-700 dark:border-red-900/60 dark:bg-red-950/30 dark:text-red-300"
        >
          <span>{{ t('channelStatus.capacity.loadFailed') }}</span>
          <button
            type="button"
            class="min-h-11 rounded-md px-3 font-medium hover:bg-red-100 dark:hover:bg-red-900/40"
            @click="reloadCapacity(false)"
          >
            {{ t('common.refresh') }}
          </button>
        </div>
        <div
          v-if="groupRateError"
          class="mt-2 flex items-center justify-between gap-3 rounded-lg border border-red-200 bg-red-50 px-3 py-2 text-xs text-red-700 dark:border-red-900/60 dark:bg-red-950/30 dark:text-red-300"
        >
          <span>{{ t('channelStatus.groupRate.loadFailed') }}</span>
          <button
            type="button"
            class="min-h-11 rounded-md px-3 font-medium hover:bg-red-100 dark:hover:bg-red-900/40"
            @click="reloadGroupRates(false)"
          >
            {{ t('common.refresh') }}
          </button>
        </div>
      </div>
      </div>

      <MonitorCardGrid
        v-if="showMonitorGrid"
        :items="items"
        :window="currentWindow"
        :countdown-seconds="countdown"
        :loading="loading"
        :availability-by-id="availabilityById"
        @card-click="openDetail"
      />
    </details>

    <MonitorDetailDialog
      :show="showDetail"
      :monitor-id="detailTarget?.id ?? null"
      :title="detailTitle"
      @close="closeDetail"
    />
  </div>
</template>

<script setup lang="ts">
import { ref, reactive, computed, onMounted, onBeforeUnmount, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAppStore } from '@/stores/app'
import { extractApiErrorMessage } from '@/utils/apiError'
import { getQuotaDashboard as fetchQuotaPoolDashboard } from '@/api/accounts'
import userGroupsAPI from '@/api/groups'
import {
  list as listChannelMonitorViews,
  capacitySummary as fetchChannelCapacitySummary,
  status as fetchChannelMonitorDetail,
  type UserMonitorView,
  type UserMonitorDetail,
} from '@/api/channelMonitor'
import type { UserAccountQuotaPoolDashboard } from '@/types'
import type { UserAvailableGroup, UserSupportedModel } from '@/api/channels'
import AccountQuotaDashboardPanel from '@/components/account/AccountQuotaDashboard.vue'
import {
  resolveAccountQuotaGroupHealth,
  type AccountQuotaGroupHealth,
} from '@/utils/accountQuotaHealth'
import MonitorHero, {
  type MonitorWindow,
  type OverallStatus,
} from '@/components/user/monitor/MonitorHero.vue'
import MonitorCardGrid from '@/components/user/monitor/MonitorCardGrid.vue'
import MonitorDetailDialog from '@/components/user/MonitorDetailDialog.vue'
import { DEFAULT_INTERVAL_SECONDS } from '@/constants/channelMonitor'
import { useAutoRefresh } from '@/composables/useAutoRefresh'

const { t } = useI18n()
const appStore = useAppStore()

const props = withDefaults(defineProps<{
  compact?: boolean
  showMonitorGrid?: boolean
  groupModelsById?: Record<number, UserSupportedModel[]>
  availableGroupsById?: Record<number, UserAvailableGroup>
  userGroupRates?: Record<number, number>
}>(), {
  compact: false,
  showMonitorGrid: true,
  groupModelsById: () => ({}),
  availableGroupsById: () => ({}),
  userGroupRates: () => ({}),
})

const emit = defineEmits<{
  (event: 'itemsChange', items: UserMonitorView[]): void
  (event: 'visibleGroupIdsChange', groupIds: number[]): void
}>()

// ── State ──
const items = ref<UserMonitorView[]>([])
const loading = ref(false)
const monitorError = ref(false)
const monitorStatusLoaded = ref(false)
const quotaPoolDashboard = ref<UserAccountQuotaPoolDashboard | null>(null)
const quotaPoolLoading = ref(false)
const quotaPoolError = ref(false)
const quotaStatusLoaded = ref(false)
const groupCapacityById = ref<Record<number, { concurrency_used: number; concurrency_max: number }>>({})
const capacityLoading = ref(false)
const capacityError = ref(false)
const groupRateById = ref<Record<number, number>>({})
const groupRateLoading = ref(false)
const groupRateError = ref(false)
const currentWindow = ref<MonitorWindow>('7d')
const detailCache = reactive<Record<number, UserMonitorDetail>>({})
const detailLoadingIds = ref<Set<number>>(new Set())
const detailFailedIds = ref<Set<number>>(new Set())
const showDetail = ref(false)
const detailTarget = ref<UserMonitorView | null>(null)
const detailRequests = new Map<number, Promise<void>>()

let abortController: AbortController | null = null
let quotaPoolAbortController: AbortController | null = null
let capacityAbortController: AbortController | null = null
let groupRateAbortController: AbortController | null = null

const autoRefresh = useAutoRefresh({
  storageKey: 'channel-status-auto-refresh',
  intervals: [30, 60, 120] as const,
  defaultInterval: DEFAULT_INTERVAL_SECONDS,
  defaultEnabled: true,
  onRefresh: () => refreshCurrentView(true),
  shouldPause: () =>
    document.hidden ||
    loading.value ||
    quotaPoolLoading.value ||
    capacityLoading.value ||
    groupRateLoading.value,
})
const countdown = autoRefresh.countdown

// ── Computed ──
interface DispatchStatusSummary {
  status: OverallStatus
  totalSignalCount: number
  unavailableGroupCount: number
  unavailableMonitorCount: number
  constrainedGroupCount: number
  degradedSignalCount: number
}

const dispatchStatusSummary = computed<DispatchStatusSummary>(() => {
  const groupStatuses = (quotaPoolDashboard.value?.platform?.group_summaries ?? [])
    .map(resolveAccountQuotaGroupHealth)
  const monitorStatuses: AccountQuotaGroupHealth[] = items.value.map(item => {
    if (item.primary_status === 'failed' || item.primary_status === 'error') {
      return 'unavailable'
    }
    return item.primary_status === 'degraded' ? 'degraded' : 'normal'
  })

  const unavailableGroupCount = groupStatuses.filter(status => status === 'unavailable').length
  const unavailableMonitorCount = monitorStatuses.filter(status => status === 'unavailable').length
  const constrainedGroupCount = groupStatuses.filter(status => status === 'constrained').length
  const degradedSignalCount = [...groupStatuses, ...monitorStatuses]
    .filter(status => status === 'degraded').length
  const totalSignalCount = groupStatuses.length + monitorStatuses.length
  const unavailableSignalCount = unavailableGroupCount + unavailableMonitorCount

  let status: OverallStatus = 'unknown'
  const sourceUnavailable =
    (!monitorStatusLoaded.value && monitorError.value) ||
    (!quotaStatusLoaded.value && quotaPoolError.value)

  if (!sourceUnavailable && totalSignalCount > 0) {
    if (unavailableSignalCount === totalSignalCount) {
      status = 'unavailable'
    } else if (unavailableSignalCount > 0) {
      status = 'degraded'
    } else if (constrainedGroupCount > 0) {
      status = 'constrained'
    } else if (degradedSignalCount > 0) {
      status = 'degraded'
    } else {
      status = 'operational'
    }
  }

  return {
    status,
    totalSignalCount,
    unavailableGroupCount,
    unavailableMonitorCount,
    constrainedGroupCount,
    degradedSignalCount,
  }
})

const overallStatus = computed<OverallStatus>(() => dispatchStatusSummary.value.status)

const statusLoading = computed(() =>
  (!monitorStatusLoaded.value && !monitorError.value) ||
  (!quotaStatusLoaded.value && !quotaPoolError.value)
)

const overallDetail = computed(() => {
  const summary = dispatchStatusSummary.value
  const details: string[] = []

  if (summary.status === 'unknown') {
    details.push(t('channelStatus.summary.noStatusData'))
  } else {
    if (summary.unavailableGroupCount > 0) {
      details.push(t('channelStatus.summary.unavailableGroups', {
        count: summary.unavailableGroupCount,
      }))
    }
    if (summary.unavailableMonitorCount > 0) {
      details.push(t('channelStatus.summary.abnormalMonitors', {
        count: summary.unavailableMonitorCount,
      }))
    }
    if (details.length === 0 && summary.constrainedGroupCount > 0) {
      details.push(t('channelStatus.summary.constrainedGroups', {
        count: summary.constrainedGroupCount,
      }))
    }
    if (details.length === 0 && summary.degradedSignalCount > 0) {
      details.push(t('channelStatus.summary.degradedSignals', {
        count: summary.degradedSignalCount,
      }))
    }
    if (details.length === 0) {
      details.push(t('channelStatus.summary.noDispatchIssues'))
    }
  }

  if ((monitorError.value || quotaPoolError.value) && summary.totalSignalCount > 0) {
    details.push(t('channelStatus.summary.refreshFailed'))
  }
  return details.join(' · ')
})

const availabilityById = computed<Record<number, number | null>>(() => {
  const values: Record<number, number | null> = {}
  for (const item of items.value) {
    let value: number | null = null
    if (currentWindow.value === '7d') {
      value = item.availability_7d ?? null
    } else {
      const primary = detailCache[item.id]?.models.find(model => model.model === item.primary_model)
      if (primary) {
        value = currentWindow.value === '15d'
          ? primary.availability_15d ?? null
          : primary.availability_30d ?? null
      }
    }
    values[item.id] = typeof value === 'number' && Number.isFinite(value) ? value : null
  }
  return values
})

const minimumAvailability = computed<number | null>(() => {
  if (items.value.length === 0) return null
  const values = items.value.map(item => availabilityById.value[item.id])
  if (values.some(value => value == null)) return null
  return Math.min(...values as number[])
})

const availabilityLoading = computed(() => {
  if (!monitorStatusLoaded.value && !monitorError.value) return true
  if (currentWindow.value === '7d') return false
  return items.value.some(item =>
    detailLoadingIds.value.has(item.id) ||
    (!detailCache[item.id] && !detailFailedIds.value.has(item.id))
  )
})

const detailTitle = computed(() => {
  return detailTarget.value?.name || t('channelStatus.detailTitle')
})

// ── Loaders ──
async function reload(silent = false) {
  if (abortController) abortController.abort()
  const ctrl = new AbortController()
  abortController = ctrl
  if (!silent) loading.value = true
  monitorError.value = false
  try {
    const res = await listChannelMonitorViews({ signal: ctrl.signal })
    if (ctrl.signal.aborted || abortController !== ctrl) return
    items.value = res.items || []
    emit('itemsChange', items.value)
    monitorStatusLoaded.value = true
  } catch (err: unknown) {
    const e = err as { name?: string; code?: string }
    if (e?.name === 'AbortError' || e?.code === 'ERR_CANCELED') return
    monitorError.value = true
    appStore.showError(extractApiErrorMessage(err, t('channelStatus.loadError')))
  } finally {
    if (abortController === ctrl) {
      if (!silent) loading.value = false
      autoRefresh.resetCountdown()
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
    emit('visibleGroupIdsChange', visibleQuotaGroupIds(dashboard))
    quotaStatusLoaded.value = true
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

function visibleQuotaGroupIds(dashboard: UserAccountQuotaPoolDashboard): number[] {
  const summaries = dashboard.platform?.group_summaries ?? []
  return [...new Set(
    summaries
      .filter(summary =>
        summary.account_count > 0 ||
        (summary.usage_windows?.some(window => window.account_count > 0) ?? false),
      )
      .map(summary => summary.group_id)
      .filter((groupId): groupId is number => typeof groupId === 'number' && groupId > 0),
  )]
}

async function reloadCapacity(silent = false) {
  capacityAbortController?.abort()
  const ctrl = new AbortController()
  capacityAbortController = ctrl
  if (!silent) capacityLoading.value = true
  capacityError.value = false
  try {
    const capacity = await fetchChannelCapacitySummary({ signal: ctrl.signal })
    if (ctrl.signal.aborted || capacityAbortController !== ctrl) return
    groupCapacityById.value = capacity.items.reduce<Record<number, { concurrency_used: number; concurrency_max: number }>>(
      (acc, item) => {
        acc[item.group_id] = {
          concurrency_used: item.concurrency_used,
          concurrency_max: item.concurrency_max,
        }
        return acc
      },
      {}
    )
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

async function reloadGroupRates(silent = false) {
  groupRateAbortController?.abort()
  const ctrl = new AbortController()
  groupRateAbortController = ctrl
  if (!silent) groupRateLoading.value = true
  groupRateError.value = false
  try {
    const groups = await userGroupsAPI.getAvailable({ signal: ctrl.signal })
    if (ctrl.signal.aborted || groupRateAbortController !== ctrl) return
    groupRateById.value = groups.reduce<Record<number, number>>((acc, group) => {
      acc[group.id] = group.effective_rate_multiplier ?? group.rate_multiplier
      return acc
    }, {})
  } catch (err: unknown) {
    const e = err as { name?: string; code?: string }
    if (e?.name === 'AbortError' || e?.code === 'ERR_CANCELED') return
    groupRateById.value = {}
    groupRateError.value = true
    appStore.showError(extractApiErrorMessage(err, t('channelStatus.groupRate.loadFailed')))
  } finally {
    if (groupRateAbortController === ctrl) {
      if (!silent) groupRateLoading.value = false
      groupRateAbortController = null
    }
  }
}

async function reloadAll(silent = false) {
  await Promise.all([
    reload(silent),
    reloadQuotaPool(silent),
    reloadCapacity(silent),
    reloadGroupRates(silent)
  ])
}

async function refreshCurrentView(silent = false) {
  await reloadAll(silent)
  if (currentWindow.value !== '7d') {
    await Promise.all(items.value.map(item => loadDetail(item.id, true)))
  }
}

async function reloadPlatformPool(silent = false) {
  await Promise.all([
    reloadQuotaPool(silent),
    reloadCapacity(silent),
    reloadGroupRates(silent)
  ])
}

async function manualReload() {
  await refreshCurrentView(false)
}

async function loadDetail(id: number, force = false) {
  if (!force && detailCache[id]) return
  const inFlight = detailRequests.get(id)
  if (inFlight) return inFlight

  setIdState(detailLoadingIds, id, true)
  setIdState(detailFailedIds, id, false)
  const request = (async () => {
    try {
      detailCache[id] = await fetchChannelMonitorDetail(id)
    } catch (err: unknown) {
      setIdState(detailFailedIds, id, true)
      appStore.showError(extractApiErrorMessage(err, t('channelStatus.detailLoadError')))
    } finally {
      setIdState(detailLoadingIds, id, false)
    }
  })()
  detailRequests.set(id, request)
  try {
    await request
  } finally {
    if (detailRequests.get(id) === request) {
      detailRequests.delete(id)
    }
  }
}

function setIdState(target: typeof detailLoadingIds, id: number, present: boolean) {
  const next = new Set(target.value)
  if (present) next.add(id)
  else next.delete(id)
  target.value = next
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
  if (
    appStore.cachedPublicSettings?.channel_monitor_enabled !== false &&
    autoRefresh.enabled.value
  ) {
    autoRefresh.resetCountdown()
    autoRefresh.start()
  }
})

onBeforeUnmount(() => {
  if (abortController) abortController.abort()
  if (quotaPoolAbortController) quotaPoolAbortController.abort()
  if (capacityAbortController) capacityAbortController.abort()
  if (groupRateAbortController) groupRateAbortController.abort()
})
</script>
