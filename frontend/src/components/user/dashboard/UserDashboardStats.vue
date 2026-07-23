<template>
  <!-- Row 1: Core Stats -->
  <div class="dashboard-stat-grid" :class="{ 'xl:grid-cols-3': isSimple }">
    <!-- Balance -->
    <div v-if="!isSimple" class="dashboard-metric-card">
      <div class="dashboard-metric-content">
        <div class="dashboard-metric-icon dashboard-metric-icon-positive">
          <svg class="h-5 w-5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
            <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M2.25 18.75a60.07 60.07 0 0115.797 2.101c.727.198 1.453-.342 1.453-1.096V18.75M3.75 4.5v.75A.75.75 0 013 6h-.75m0 0v-.375c0-.621.504-1.125 1.125-1.125H20.25M2.25 6v9m18-10.5v.75c0 .414.336.75.75.75h.75m-1.5-1.5h.375c.621 0 1.125.504 1.125 1.125v9.75c0 .621-.504 1.125-1.125 1.125h-.375m1.5-1.5H21a.75.75 0 00-.75.75v.75m0 0H3.75m0 0h-.375a1.125 1.125 0 01-1.125-1.125V15m1.5 1.5v-.75A.75.75 0 003 15h-.75M15 10.5a3 3 0 11-6 0 3 3 0 016 0zm3 0h.008v.008H18V10.5zm-12 0h.008v.008H6V10.5z" />
          </svg>
        </div>
        <div class="min-w-0 flex-1">
          <p class="dashboard-metric-label">{{ t('dashboard.balance') }}</p>
          <p class="dashboard-metric-value text-positive">${{ formatBalance(balance) }}</p>
          <p class="dashboard-metric-meta">{{ t('common.available') }}</p>
        </div>
      </div>
    </div>

    <!-- API Keys -->
    <div class="dashboard-metric-card">
      <div class="dashboard-metric-content">
        <div class="dashboard-metric-icon">
          <Icon name="key" size="md" :stroke-width="2" />
        </div>
        <div class="min-w-0 flex-1">
          <p class="dashboard-metric-label">{{ t('dashboard.apiKeys') }}</p>
          <p class="dashboard-metric-value">{{ stats?.total_api_keys || 0 }}</p>
          <p class="dashboard-metric-meta text-positive">
            {{ stats?.active_api_keys || 0 }} {{ t('common.active') }}
          </p>
        </div>
      </div>
    </div>

    <!-- Today Requests -->
    <div class="dashboard-metric-card">
      <div class="dashboard-metric-content">
        <div class="dashboard-metric-icon">
          <Icon name="chart" size="md" :stroke-width="2" />
        </div>
        <div class="min-w-0 flex-1">
          <p class="dashboard-metric-label">{{ t('dashboard.todayRequests') }}</p>
          <p class="dashboard-metric-value">{{ stats?.today_requests || 0 }}</p>
          <p class="dashboard-metric-meta">
            {{ t('common.total') }}: {{ formatNumber(stats?.total_requests || 0) }}
          </p>
        </div>
      </div>
    </div>

    <!-- Today Cost -->
    <div class="dashboard-metric-card">
      <div class="dashboard-metric-content">
        <div class="dashboard-metric-icon">
          <Icon name="dollar" size="md" :stroke-width="2" />
        </div>
        <div class="min-w-0 flex-1">
          <p class="dashboard-metric-label">{{ t('dashboard.todayCost') }}</p>
          <p class="dashboard-metric-value dashboard-metric-value-wrap">
            <span class="text-brand" :title="t('dashboard.actual')">
              ${{ formatCost(stats?.today_actual_cost || 0) }}
            </span>
            <span class="text-sm font-normal text-content-subtle" :title="t('dashboard.standard')">
              / ${{ formatCost(stats?.today_cost || 0) }}
            </span>
          </p>
          <p class="dashboard-metric-meta flex flex-wrap items-center gap-x-2 gap-y-0.5">
            <span class="text-brand" :title="t('usage.requestBilled')">
              {{ t('usage.requestBilled') }} ${{ formatCost(stats?.today_request_actual_cost || 0) }}
            </span>
            <span :title="t('usage.hourlyBilled')">
              {{ t('usage.hourlyBilled') }} ${{ formatCost(stats?.today_hourly_cost || 0) }}
            </span>
          </p>
          <p class="dashboard-metric-meta">
            <span>{{ t('common.total') }}: </span>
            <span class="text-brand" :title="t('dashboard.actual')">${{ formatCost(stats?.total_actual_cost || 0) }}</span>
            <span :title="t('dashboard.standard')"> / ${{ formatCost(stats?.total_cost || 0) }}</span>
          </p>
        </div>
      </div>
    </div>
  </div>

  <!-- Row 2: Token Stats -->
  <div class="dashboard-stat-grid">
    <!-- Today Tokens -->
    <div class="dashboard-metric-card">
      <div class="dashboard-metric-content">
        <div class="dashboard-metric-icon">
          <Icon name="cube" size="md" :stroke-width="2" />
        </div>
        <div class="min-w-0 flex-1">
          <p class="dashboard-metric-label">{{ t('dashboard.todayTokens') }}</p>
          <p class="dashboard-metric-value">{{ formatTokens(stats?.today_tokens || 0) }}</p>
          <p class="dashboard-metric-meta">
            {{ t('dashboard.input') }}: {{ formatTokens(stats?.today_input_tokens || 0) }} /
            {{ t('dashboard.output') }}: {{ formatTokens(stats?.today_output_tokens || 0) }}
          </p>
        </div>
      </div>
    </div>

    <!-- Total Tokens -->
    <div class="dashboard-metric-card">
      <div class="dashboard-metric-content">
        <div class="dashboard-metric-icon">
          <Icon name="database" size="md" :stroke-width="2" />
        </div>
        <div class="min-w-0 flex-1">
          <p class="dashboard-metric-label">{{ t('dashboard.totalTokens') }}</p>
          <p class="dashboard-metric-value">{{ formatTokens(stats?.total_tokens || 0) }}</p>
          <p class="dashboard-metric-meta">
            {{ t('dashboard.input') }}: {{ formatTokens(stats?.total_input_tokens || 0) }} /
            {{ t('dashboard.output') }}: {{ formatTokens(stats?.total_output_tokens || 0) }}
          </p>
        </div>
      </div>
    </div>

    <!-- Performance (RPM/TPM) -->
    <div class="dashboard-metric-card">
      <div class="dashboard-metric-content">
        <div class="dashboard-metric-icon">
          <Icon name="bolt" size="md" :stroke-width="2" />
        </div>
        <div class="min-w-0 flex-1">
          <p class="dashboard-metric-label">{{ t('dashboard.performance') }}</p>
          <div class="flex items-baseline gap-2">
            <p class="dashboard-metric-value mt-0">{{ formatTokens(stats?.rpm || 0) }}</p>
            <span class="text-xs text-content-subtle">RPM</span>
          </div>
          <div class="flex items-baseline gap-2">
            <p class="text-sm font-semibold text-brand">{{ formatTokens(stats?.tpm || 0) }}</p>
            <span class="text-xs text-content-subtle">TPM</span>
          </div>
        </div>
      </div>
    </div>

    <!-- Avg Response Time -->
    <div class="dashboard-metric-card">
      <div class="dashboard-metric-content">
        <div class="dashboard-metric-icon">
          <Icon name="clock" size="md" :stroke-width="2" />
        </div>
        <div class="min-w-0 flex-1">
          <p class="dashboard-metric-label">{{ t('dashboard.avgResponse') }}</p>
          <p class="dashboard-metric-value">{{ formatDuration(stats?.average_duration_ms || 0) }}</p>
          <p class="dashboard-metric-meta">{{ t('dashboard.averageTime') }}</p>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import Icon from '@/components/icons/Icon.vue'
import type { UserDashboardStats as UserStatsType } from '@/api/usage'

defineProps<{
  stats: UserStatsType
  balance: number
  isSimple: boolean
}>()
const { t } = useI18n()

const formatBalance = (b: number) =>
  new Intl.NumberFormat('en-US', {
    minimumFractionDigits: 2,
    maximumFractionDigits: 2
  }).format(b)

const formatNumber = (n: number) => n.toLocaleString()
const formatCost = (c: number) => c.toFixed(4)
const formatTokens = (t: number) => {
  if (t >= 1_000_000) return `${(t / 1_000_000).toFixed(1)}M`
  if (t >= 1000) return `${(t / 1000).toFixed(1)}K`
  return t.toString()
}
const formatDuration = (ms: number) => ms >= 1000 ? `${(ms / 1000).toFixed(2)}s` : `${ms.toFixed(0)}ms`
</script>
