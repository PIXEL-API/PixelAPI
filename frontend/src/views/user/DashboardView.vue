<template>
  <AppLayout>
    <div class="dashboard-page">
      <div v-if="loading" class="flex items-center justify-center py-12">
        <LoadingSpinner />
      </div>
      <template v-else-if="stats">
        <UserDashboardStats :stats="stats" :balance="user?.balance || 0" :is-simple="authStore.isSimpleMode" />
        <UserDashboardCharts
          v-model:startDate="startDate"
          v-model:endDate="endDate"
          v-model:granularity="granularity"
          :loading="loadingCharts"
          :trend="trendData"
          :models="modelStats"
          @dateRangeChange="loadTimeRangeData"
          @granularityChange="loadTimeRangeData"
          @refresh="refreshAll"
        />
        <UserAccountSharingStats
          :stats="accountSharingStats"
          :loading="loadingAccountSharing"
          :error="accountSharingError"
        />
        <div class="dashboard-content-grid">
          <div class="xl:col-span-2">
            <UserDashboardRecentUsage :data="recentUsage" :loading="loadingUsage" />
          </div>
          <div class="xl:col-span-1">
            <UserDashboardQuickActions />
          </div>
        </div>
      </template>
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref } from 'vue'
import { useAuthStore } from '@/stores/auth'
import {
  usageAPI,
  type AccountSharingDashboardStats,
  type DashboardTimeRangeParams,
  type UserDashboardStats as UserStatsType
} from '@/api/usage'
import AppLayout from '@/components/layout/AppLayout.vue'
import LoadingSpinner from '@/components/common/LoadingSpinner.vue'
import UserDashboardStats from '@/components/user/dashboard/UserDashboardStats.vue'
import UserDashboardCharts from '@/components/user/dashboard/UserDashboardCharts.vue'
import UserDashboardRecentUsage from '@/components/user/dashboard/UserDashboardRecentUsage.vue'
import UserDashboardQuickActions from '@/components/user/dashboard/UserDashboardQuickActions.vue'
import UserAccountSharingStats from '@/components/user/dashboard/UserAccountSharingStats.vue'
import type { ModelStat, TrendDataPoint, UsageLog } from '@/types'

const authStore = useAuthStore()
const user = computed(() => authStore.user)

const stats = ref<UserStatsType | null>(null)
const loading = ref(false)
const loadingUsage = ref(false)
const loadingCharts = ref(false)
const loadingAccountSharing = ref(false)
const accountSharingError = ref('')

const trendData = ref<TrendDataPoint[]>([])
const modelStats = ref<ModelStat[]>([])
const recentUsage = ref<UsageLog[]>([])
const accountSharingStats = ref<AccountSharingDashboardStats | null>(null)

let chartsRequestSequence = 0
let accountSharingRequestSequence = 0
let isUnmounted = false

const formatLocalDate = (date: Date) => {
  const year = date.getFullYear()
  const month = String(date.getMonth() + 1).padStart(2, '0')
  const day = String(date.getDate()).padStart(2, '0')
  return `${year}-${month}-${day}`
}
const startDate = ref(formatLocalDate(new Date(Date.now() - 6 * 86400000)))
const endDate = ref(formatLocalDate(new Date()))
const granularity = ref<'day' | 'hour'>('day')
const activeRangePreset = ref<string | null>(null)

const getLast24HoursParams = (): Required<Pick<
  DashboardTimeRangeParams,
  'start_time' | 'end_time' | 'timezone'
>> => {
  const end = new Date()
  const start = new Date(end.getTime() - 24 * 60 * 60 * 1000)
  const clientTimezone = Intl.DateTimeFormat().resolvedOptions().timeZone
  if (!clientTimezone) {
    throw new Error('Unable to resolve the client timezone')
  }
  return {
    start_time: start.toISOString(),
    end_time: end.toISOString(),
    timezone: clientTimezone
  }
}

const buildDashboardTimeRangeParams = (): DashboardTimeRangeParams => {
  if (activeRangePreset.value === 'last24Hours') {
    return getLast24HoursParams()
  }
  return {
    start_date: startDate.value,
    end_date: endDate.value
  }
}

const loadStats = async () => {
  if (isUnmounted) return

  loading.value = true
  try {
    await authStore.refreshUser()
    if (isUnmounted) return

    const nextStats = await usageAPI.getDashboardStats()
    if (isUnmounted) return

    stats.value = nextStats
  } catch (error) {
    if (isUnmounted) return
    console.error('Failed to load dashboard stats:', error)
  } finally {
    if (!isUnmounted) {
      loading.value = false
    }
  }
}

const loadCharts = async (timeRange = buildDashboardTimeRangeParams()) => {
  if (isUnmounted) return

  const requestSequence = ++chartsRequestSequence
  const requestGranularity = granularity.value
  loadingCharts.value = true
  try {
    const [trend, models] = await Promise.all([
      usageAPI.getDashboardTrend({
        ...timeRange,
        granularity: requestGranularity
      }),
      usageAPI.getDashboardModels(timeRange)
    ])
    if (isUnmounted || requestSequence !== chartsRequestSequence) return

    trendData.value = trend.trend || []
    modelStats.value = models.models || []
  } catch (error) {
    if (isUnmounted || requestSequence !== chartsRequestSequence) return
    console.error('Failed to load charts:', error)
  } finally {
    if (!isUnmounted && requestSequence === chartsRequestSequence) {
      loadingCharts.value = false
    }
  }
}

const loadAccountSharing = async (timeRange = buildDashboardTimeRangeParams()) => {
  if (isUnmounted) return

  const requestSequence = ++accountSharingRequestSequence
  const requestGranularity = granularity.value
  loadingAccountSharing.value = true
  accountSharingError.value = ''
  try {
    const nextAccountSharingStats = await usageAPI.getDashboardAccountSharing({
      ...timeRange,
      granularity: requestGranularity
    })
    if (isUnmounted || requestSequence !== accountSharingRequestSequence) return

    accountSharingStats.value = nextAccountSharingStats
  } catch (error: any) {
    if (isUnmounted || requestSequence !== accountSharingRequestSequence) return
    console.error('Failed to load account sharing stats:', error)
    accountSharingStats.value = null
    accountSharingError.value = error?.message || 'Failed to load account sharing stats'
  } finally {
    if (!isUnmounted && requestSequence === accountSharingRequestSequence) {
      loadingAccountSharing.value = false
    }
  }
}

const loadRecent = async () => {
  if (isUnmounted) return

  loadingUsage.value = true
  try {
    const res = await usageAPI.getByDateRange(startDate.value, endDate.value)
    if (isUnmounted) return

    recentUsage.value = res.items.slice(0, 5)
  } catch (error) {
    if (isUnmounted) return
    console.error('Failed to load recent usage:', error)
  } finally {
    if (!isUnmounted) {
      loadingUsage.value = false
    }
  }
}

const loadTimeRangeData = (range?: { startDate: string; endDate: string; preset: string | null }) => {
  if (range) {
    startDate.value = range.startDate
    endDate.value = range.endDate
    activeRangePreset.value = range.preset
  }
  const timeRange = buildDashboardTimeRangeParams()
  void loadCharts(timeRange)
  void loadAccountSharing(timeRange)
}

const refreshAll = () => {
  const timeRange = buildDashboardTimeRangeParams()
  void loadStats()
  void loadCharts(timeRange)
  void loadAccountSharing(timeRange)
  void loadRecent()
}

onMounted(() => {
  refreshAll()
})

onUnmounted(() => {
  isUnmounted = true
  chartsRequestSequence += 1
  accountSharingRequestSequence += 1
})
</script>
