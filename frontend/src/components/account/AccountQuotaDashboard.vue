<template>
  <section class="rounded-lg border border-gray-200 bg-white p-3 shadow-sm dark:border-dark-700 dark:bg-dark-800">
    <div class="flex flex-wrap items-center justify-between gap-3">
      <div class="flex min-w-0 items-center gap-3">
        <div class="rounded-lg bg-emerald-100 p-2 text-emerald-700 dark:bg-emerald-900/30 dark:text-emerald-300">
          <Icon name="calculator" size="md" />
        </div>
        <div class="min-w-0">
          <h2 class="text-sm font-semibold text-gray-900 dark:text-white">
            {{ panelTitle }}
          </h2>
          <p class="mt-0.5 text-xs text-gray-500 dark:text-gray-400">
            {{ panelSubtitle }}
          </p>
        </div>
      </div>
      <div class="flex items-center gap-2">
        <span v-if="dashboard" class="hidden text-xs text-gray-400 dark:text-gray-500 sm:inline">
          {{ t('admin.accounts.quotaDashboard.generatedAt', { time: formatDateTime(new Date(dashboard.generated_at)) }) }}
        </span>
        <button
          type="button"
          class="btn btn-secondary px-2 py-1.5 text-xs"
          :disabled="loading"
          @click="emit('refresh')"
        >
          <Icon name="refresh" size="sm" :class="{ 'animate-spin': loading }" />
          <span class="hidden sm:inline">{{ t('common.refresh') }}</span>
        </button>
      </div>
    </div>

    <div class="mt-3 grid grid-cols-2 gap-2 md:grid-cols-4">
      <div class="rounded-md bg-gray-50 p-2 dark:bg-dark-700/60">
        <div class="text-xs text-gray-500 dark:text-gray-400">{{ t('admin.accounts.quotaDashboard.totalAccounts') }}</div>
        <div class="mt-1 text-lg font-semibold text-gray-900 dark:text-white">{{ totals.account_count }}</div>
      </div>
      <div class="rounded-md bg-gray-50 p-2 dark:bg-dark-700/60">
        <div class="text-xs text-gray-500 dark:text-gray-400">{{ t('admin.accounts.quotaDashboard.quotaAccounts') }}</div>
        <div class="mt-1 text-lg font-semibold text-gray-900 dark:text-white">{{ totals.quota_account_count }}</div>
      </div>
      <div class="rounded-md bg-gray-50 p-2 dark:bg-dark-700/60">
        <div class="text-xs text-gray-500 dark:text-gray-400">{{ t('admin.accounts.quotaDashboard.totalRemaining') }}</div>
        <div class="mt-1 text-lg font-semibold text-emerald-600 dark:text-emerald-300">{{ formatCurrency(totals.total.remaining) }}</div>
      </div>
      <div class="rounded-md bg-gray-50 p-2 dark:bg-dark-700/60">
        <div class="text-xs text-gray-500 dark:text-gray-400">{{ t('admin.accounts.quotaDashboard.schedulableAccounts') }}</div>
        <div class="mt-1 text-lg font-semibold text-gray-900 dark:text-white">{{ totals.schedulable_account_count }}</div>
      </div>
    </div>

    <div v-if="loading && !dashboard" class="mt-3 grid grid-cols-1 gap-3 md:grid-cols-2 xl:grid-cols-3">
      <div v-for="idx in 3" :key="idx" class="h-36 animate-pulse rounded-lg bg-gray-100 dark:bg-dark-700" />
    </div>

    <div v-else-if="error" class="mt-3 rounded-md border border-red-200 bg-red-50 px-3 py-2 text-sm text-red-700 dark:border-red-900/50 dark:bg-red-900/20 dark:text-red-300">
      {{ loadFailedText }}
    </div>

    <div v-else-if="visibleSummaries.length === 0" class="mt-3 rounded-md border border-dashed border-gray-200 px-3 py-4 text-sm text-gray-500 dark:border-dark-600 dark:text-gray-400">
      {{ emptyText }}
    </div>

    <div v-else class="mt-3 grid grid-cols-1 gap-3 md:grid-cols-2 xl:grid-cols-3">
      <article
        v-for="summary in visibleSummaries"
        :key="`${summary.platform}:${summary.type}`"
        class="rounded-lg border border-gray-200 p-3 dark:border-dark-700"
      >
        <div class="flex items-start justify-between gap-3">
          <div class="flex min-w-0 items-center gap-2">
            <PlatformIcon :platform="platformIconValue(summary.platform)" size="md" />
            <div class="min-w-0">
              <div class="truncate text-sm font-semibold text-gray-900 dark:text-white">
                {{ platformLabel(summary.platform) }} / {{ typeLabel(summary.type) }}
              </div>
              <div class="mt-0.5 text-xs text-gray-500 dark:text-gray-400">
                {{ t('admin.accounts.quotaDashboard.accountMeta', {
                  total: summary.account_count,
                  active: summary.active_account_count,
                  schedulable: summary.schedulable_account_count
                }) }}
              </div>
            </div>
          </div>
          <span
            v-if="summary.quota_account_count > 0"
            class="rounded-md bg-emerald-50 px-2 py-0.5 text-xs font-medium text-emerald-700 dark:bg-emerald-900/30 dark:text-emerald-300"
          >
            {{ summary.quota_account_count }}
          </span>
        </div>

        <div v-if="quotaDimensions(summary).length > 0" class="mt-3 space-y-2">
          <div v-for="dimension in quotaDimensions(summary)" :key="dimension.key">
            <div class="flex items-center justify-between gap-2 text-xs">
              <span class="font-medium text-gray-600 dark:text-gray-300">{{ dimension.label }}</span>
              <span class="font-mono font-semibold text-gray-900 dark:text-white">{{ formatCurrency(dimension.value.remaining) }}</span>
            </div>
            <div class="mt-1 h-1.5 overflow-hidden rounded-full bg-gray-100 dark:bg-dark-700">
              <div
                class="h-full rounded-full"
                :class="quotaBarClass(dimension.value.utilization)"
                :style="{ width: `${progressWidth(dimension.value.utilization)}%` }"
              />
            </div>
            <div class="mt-1 flex items-center justify-between gap-2 text-[11px] text-gray-500 dark:text-gray-400">
              <span>{{ formatCurrency(dimension.value.used) }} / {{ formatCurrency(dimension.value.limit) }}</span>
              <span>{{ t('admin.accounts.quotaDashboard.exhaustedCount', { count: dimension.value.exhausted_account_count }) }}</span>
            </div>
          </div>
        </div>

        <div v-if="summary.usage_windows?.length" class="mt-3 space-y-2">
          <div
            v-for="window in summary.usage_windows"
            :key="window.window"
            class="rounded-md bg-gray-50 p-2 dark:bg-dark-700/60"
          >
            <div class="flex items-center justify-between gap-2 text-xs">
              <span class="font-medium text-gray-700 dark:text-gray-200">
                {{ windowLabel(window.window) }}
              </span>
              <span class="font-mono font-semibold text-gray-900 dark:text-white">
                {{ formatPercent(window.average_utilization) }}
              </span>
            </div>
            <div class="mt-1 h-1.5 overflow-hidden rounded-full bg-gray-200 dark:bg-dark-600">
              <div
                class="h-full rounded-full"
                :class="quotaBarClass(window.average_utilization)"
                :style="{ width: `${progressWidth(window.average_utilization)}%` }"
              />
            </div>
            <div class="mt-1 flex items-center justify-between gap-2 text-[11px] text-gray-500 dark:text-gray-400">
              <span>{{ t('admin.accounts.quotaDashboard.knownSnapshots', { known: window.known_account_count, total: window.account_count }) }}</span>
              <span>{{ t('admin.accounts.quotaDashboard.remainingAccountsEquivalent', { count: formatAccountEquivalent(window.remaining_capacity_percent) }) }}</span>
            </div>
          </div>
        </div>
      </article>
    </div>
  </section>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { AccountQuotaDashboard, AccountQuotaDimensionSummary, AccountQuotaSummary, GroupPlatform } from '@/types'
import Icon from '@/components/icons/Icon.vue'
import PlatformIcon from '@/components/common/PlatformIcon.vue'
import { formatDateTime } from '@/utils/format'
import { platformLabel } from '@/utils/platformColors'

const props = defineProps<{
  dashboard: AccountQuotaDashboard | null
  loading: boolean
  error: boolean
  title?: string
  subtitle?: string
  emptyMessage?: string
  loadFailedMessage?: string
}>()

const emit = defineEmits<{
  refresh: []
}>()

const { t } = useI18n()

const panelTitle = computed(() => props.title ?? t('admin.accounts.quotaDashboard.title'))
const panelSubtitle = computed(() => props.subtitle ?? t('admin.accounts.quotaDashboard.subtitle'))
const emptyText = computed(() => props.emptyMessage ?? t('admin.accounts.quotaDashboard.empty'))
const loadFailedText = computed(() => props.loadFailedMessage ?? t('admin.accounts.quotaDashboard.loadFailed'))

const emptyDimension: AccountQuotaDimensionSummary = {
  enabled_account_count: 0,
  exhausted_account_count: 0,
  limit: 0,
  used: 0,
  remaining: 0,
  utilization: 0
}

const totals = computed<AccountQuotaSummary>(() => props.dashboard?.totals ?? {
  platform: 'all',
  type: 'all',
  account_count: 0,
  active_account_count: 0,
  schedulable_account_count: 0,
  quota_account_count: 0,
  unlimited_account_count: 0,
  total: emptyDimension,
  daily: emptyDimension,
  weekly: emptyDimension,
  usage_windows: []
})

const visibleSummaries = computed(() => {
  return (props.dashboard?.summaries ?? []).filter((summary) => {
    return (
      summary.quota_account_count > 0 ||
      (summary.usage_windows?.some(window => window.account_count > 0) ?? false)
    )
  })
})

function typeLabel(type: string): string {
  switch (type) {
    case 'oauth':
      return 'OAuth'
    case 'setup-token':
      return 'Token'
    case 'apikey':
      return 'Key'
    case 'bedrock':
      return 'AWS'
    case 'service_account':
      return 'Vertex'
    case 'all':
      return t('common.total')
    default:
      return type
  }
}

function platformIconValue(platform: string): GroupPlatform | undefined {
  if (platform === 'anthropic' || platform === 'openai' || platform === 'gemini' || platform === 'antigravity') {
    return platform
  }
  return undefined
}

function windowLabel(window: string): string {
  if (window === '5h') return t('admin.accounts.quotaDashboard.window5h')
  if (window === '7d') return t('admin.accounts.quotaDashboard.window7d')
  return window
}

function quotaDimensions(summary: AccountQuotaSummary) {
  return [
    { key: 'daily', label: t('admin.accounts.quotaDashboard.daily'), value: summary.daily },
    { key: 'weekly', label: t('admin.accounts.quotaDashboard.weekly'), value: summary.weekly },
    { key: 'total', label: t('admin.accounts.quotaDashboard.total'), value: summary.total }
  ].filter(item => item.value.enabled_account_count > 0)
}

function formatCurrency(value: number): string {
  return `$${(Number.isFinite(value) ? value : 0).toFixed(2)}`
}

function formatPercent(value: number): string {
  return `${(Number.isFinite(value) ? value : 0).toFixed(1)}%`
}

function formatAccountEquivalent(value: number): string {
  return ((Number.isFinite(value) ? value : 0) / 100).toFixed(2)
}

function progressWidth(value: number): number {
  if (!Number.isFinite(value) || value <= 0) return 0
  return Math.min(value, 100)
}

function quotaBarClass(value: number): string {
  if (value >= 100) return 'bg-red-500'
  if (value >= 80) return 'bg-amber-500'
  return 'bg-emerald-500'
}
</script>
