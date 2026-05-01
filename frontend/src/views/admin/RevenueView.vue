<template>
  <AppLayout>
    <div class="space-y-6">
      <div class="flex flex-col gap-3 xl:flex-row xl:items-center xl:justify-between">
        <div class="inline-flex max-w-full overflow-x-auto rounded-lg border border-gray-200 bg-white p-1 dark:border-dark-600 dark:bg-dark-800">
          <button
            v-for="tab in revenueTabs"
            :key="tab.key"
            type="button"
            class="whitespace-nowrap rounded-md px-3 py-1.5 text-sm font-medium transition-colors"
            :class="activeRevenueTab === tab.key
              ? 'bg-gray-900 text-white dark:bg-white dark:text-gray-900'
              : 'text-gray-600 hover:bg-gray-100 dark:text-gray-300 dark:hover:bg-dark-700'"
            :aria-pressed="activeRevenueTab === tab.key"
            @click="activeRevenueTab = tab.key"
          >
            {{ tab.label }}
          </button>
        </div>

        <div v-if="activeRevenueTab === 'overview'" class="flex flex-col gap-3 md:flex-row md:items-center md:justify-end">
          <div class="inline-flex rounded-lg border border-gray-200 bg-white p-1 dark:border-dark-600 dark:bg-dark-800">
            <button
              v-for="option in DAYS_OPTIONS"
              :key="option"
              type="button"
              class="min-w-[64px] rounded-md px-3 py-1.5 text-sm font-medium transition-colors disabled:cursor-not-allowed disabled:opacity-40"
              :class="rangeDays === option
                ? 'bg-emerald-600 text-white shadow-sm'
                : 'text-gray-600 hover:bg-gray-100 dark:text-gray-300 dark:hover:bg-dark-700'"
              :disabled="isRangeDisabled(option)"
              @click="setRangeDays(option)"
            >
              {{ t('admin.revenue.controls.rangeDays', { days: option }) }}
            </button>
          </div>

          <div class="inline-flex rounded-lg border border-gray-200 bg-white p-1 dark:border-dark-600 dark:bg-dark-800">
            <button
              v-for="option in granularityOptions"
              :key="option.value"
              type="button"
              class="min-w-[72px] rounded-md px-3 py-1.5 text-sm font-medium transition-colors"
              :class="granularity === option.value
                ? 'bg-sky-600 text-white shadow-sm'
                : 'text-gray-600 hover:bg-gray-100 dark:text-gray-300 dark:hover:bg-dark-700'"
              @click="setGranularity(option.value)"
            >
              {{ option.label }}
            </button>
          </div>

          <button
            type="button"
            class="btn btn-secondary h-10"
            :disabled="loading"
            :title="t('common.refresh')"
            @click="loadSummary"
          >
            <Icon name="refresh" size="md" :class="loading ? 'animate-spin' : ''" />
          </button>
        </div>
      </div>

      <div v-if="activeRevenueTab === 'overview' && loading && !summary" class="flex items-center justify-center py-16">
        <LoadingSpinner />
      </div>

      <template v-else-if="activeRevenueTab === 'overview' && summary">
        <div class="grid grid-cols-1 gap-4 md:grid-cols-2 xl:grid-cols-3">
          <div
            v-for="card in statCards"
            :key="card.key"
            class="card min-h-[124px] border-l-4 p-5"
            :class="card.borderClass"
          >
            <div class="flex items-start justify-between gap-3">
              <div class="min-w-0">
                <p class="text-sm font-medium text-gray-500 dark:text-gray-400">{{ card.label }}</p>
                <p class="mt-2 break-words text-2xl font-semibold text-gray-900 dark:text-white">{{ card.value }}</p>
              </div>
              <span class="mt-1 h-2.5 w-2.5 flex-shrink-0 rounded-full" :class="card.dotClass"></span>
            </div>
            <p class="mt-3 text-xs leading-5 text-gray-500 dark:text-gray-400">{{ card.meta }}</p>
          </div>
        </div>

        <section class="card p-5">
          <div class="mb-4 flex items-center justify-between gap-3">
            <h3 class="text-base font-semibold text-gray-900 dark:text-white">{{ t('admin.revenue.chart.title') }}</h3>
            <span class="text-sm text-gray-500 dark:text-gray-400">{{ summary.start_date }} - {{ summary.end_date }}</span>
          </div>
          <div v-if="summary.trend.length" class="h-[320px]">
            <Line :data="chartData" :options="chartOptions" />
          </div>
          <div v-else class="flex h-[320px] items-center justify-center text-sm text-gray-500 dark:text-gray-400">
            {{ t('admin.revenue.noData') }}
          </div>
        </section>

        <div class="grid grid-cols-1 gap-6 xl:grid-cols-2">
          <section class="card p-5">
            <h3 class="mb-4 text-base font-semibold text-gray-900 dark:text-white">{{ t('admin.revenue.sections.cash') }}</h3>
            <div class="divide-y divide-gray-100 dark:divide-dark-700">
              <div v-for="row in cashRows" :key="row.key" class="flex items-center justify-between gap-4 py-3">
                <span class="text-sm text-gray-500 dark:text-gray-400">{{ row.label }}</span>
                <span class="text-right text-sm font-medium text-gray-900 dark:text-white">{{ row.value }}</span>
              </div>
            </div>
          </section>

          <section class="card p-5">
            <h3 class="mb-4 text-base font-semibold text-gray-900 dark:text-white">{{ t('admin.revenue.sections.adjustments') }}</h3>
            <div class="divide-y divide-gray-100 dark:divide-dark-700">
              <div v-for="row in adjustmentRows" :key="row.key" class="flex items-center justify-between gap-4 py-3">
                <span class="text-sm text-gray-500 dark:text-gray-400">{{ row.label }}</span>
                <span class="text-right text-sm font-medium text-gray-900 dark:text-white">{{ row.value }}</span>
              </div>
            </div>
          </section>
        </div>

        <section class="card p-5">
          <div class="mb-4 flex flex-col gap-3 lg:flex-row lg:items-center lg:justify-between">
            <div>
              <h3 class="text-base font-semibold text-gray-900 dark:text-white">{{ t('admin.revenue.sections.breakdown') }}</h3>
              <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">{{ activeBreakdownHint }}</p>
            </div>
            <div class="inline-flex max-w-full overflow-x-auto rounded-lg border border-gray-200 bg-white p-1 dark:border-dark-600 dark:bg-dark-800">
              <button
                v-for="tab in breakdownTabs"
                :key="tab.key"
                type="button"
                class="whitespace-nowrap rounded-md px-3 py-1.5 text-sm font-medium transition-colors"
                :class="activeBreakdown === tab.key
                  ? 'bg-gray-900 text-white dark:bg-white dark:text-gray-900'
                  : 'text-gray-600 hover:bg-gray-100 dark:text-gray-300 dark:hover:bg-dark-700'"
                :aria-pressed="activeBreakdown === tab.key"
                @click="activeBreakdown = tab.key"
              >
                {{ tab.label }}
              </button>
            </div>
          </div>

          <div v-if="activeBreakdownItems.length" class="overflow-x-auto">
            <table class="min-w-full divide-y divide-gray-200 dark:divide-dark-700">
              <thead>
                <tr>
                  <th class="px-3 py-3 text-left text-xs font-semibold uppercase tracking-wide text-gray-500 dark:text-gray-400">
                    {{ t('admin.revenue.table.name') }}
                  </th>
                  <th class="px-3 py-3 text-right text-xs font-semibold uppercase tracking-wide text-gray-500 dark:text-gray-400">
                    {{ t('admin.revenue.table.requests') }}
                  </th>
                  <th class="px-3 py-3 text-right text-xs font-semibold uppercase tracking-wide text-gray-500 dark:text-gray-400">
                    {{ t('admin.revenue.table.tokens') }}
                  </th>
                  <th class="px-3 py-3 text-right text-xs font-semibold uppercase tracking-wide text-gray-500 dark:text-gray-400">
                    {{ breakdownColumnLabels.primary }}
                  </th>
                  <th class="px-3 py-3 text-right text-xs font-semibold uppercase tracking-wide text-gray-500 dark:text-gray-400">
                    {{ breakdownColumnLabels.secondary }}
                  </th>
                  <th class="px-3 py-3 text-right text-xs font-semibold uppercase tracking-wide text-gray-500 dark:text-gray-400">
                    {{ breakdownColumnLabels.tertiary }}
                  </th>
                  <th class="px-3 py-3 text-right text-xs font-semibold uppercase tracking-wide text-gray-500 dark:text-gray-400">
                    {{ breakdownColumnLabels.quaternary }}
                  </th>
                  <th class="px-3 py-3 text-right text-xs font-semibold uppercase tracking-wide text-gray-500 dark:text-gray-400">
                    {{ breakdownColumnLabels.quinary }}
                  </th>
                </tr>
              </thead>
              <tbody class="divide-y divide-gray-100 dark:divide-dark-700">
                <tr v-for="item in activeBreakdownItems" :key="`${activeBreakdown}-${item.id ?? item.name}`" class="hover:bg-gray-50 dark:hover:bg-dark-800">
                  <td class="max-w-[240px] px-3 py-3">
                    <div class="truncate text-sm font-medium text-gray-900 dark:text-white">{{ item.name }}</div>
                    <div v-if="item.secondary" class="truncate text-xs text-gray-500 dark:text-gray-400">{{ item.secondary }}</div>
                  </td>
                  <td class="px-3 py-3 text-right text-sm text-gray-700 dark:text-gray-300">{{ formatInteger(item.requests) }}</td>
                  <td class="px-3 py-3 text-right text-sm text-gray-700 dark:text-gray-300">{{ formatInteger(item.total_tokens) }}</td>
                  <td class="px-3 py-3 text-right text-sm text-gray-700 dark:text-gray-300">{{ formatAmount(item.primary_amount) }}</td>
                  <td class="px-3 py-3 text-right text-sm text-gray-700 dark:text-gray-300">{{ formatAmount(item.secondary_amount) }}</td>
                  <td class="px-3 py-3 text-right text-sm font-medium text-gray-900 dark:text-white">{{ formatAmount(item.tertiary_amount) }}</td>
                  <td class="px-3 py-3 text-right text-sm text-gray-700 dark:text-gray-300">
                    {{ item.quaternary_type === 'percent' ? formatPercent(item.quaternary_amount) : formatAmount(item.quaternary_amount) }}
                  </td>
                  <td class="px-3 py-3 text-right text-sm text-gray-700 dark:text-gray-300">
                    {{ item.quinary_type === 'percent' ? formatPercent(item.quinary_amount) : formatAmount(item.quinary_amount) }}
                  </td>
                </tr>
              </tbody>
            </table>
          </div>
          <div v-else class="flex h-40 items-center justify-center text-sm text-gray-500 dark:text-gray-400">
            {{ t('admin.revenue.noData') }}
          </div>
        </section>
      </template>

      <SharePolicyPanel v-else-if="activeRevenueTab === 'sharePolicy'" />
      <ShareSettlementsPanel v-else-if="activeRevenueTab === 'shareSettlements'" />
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import {
  CategoryScale,
  Chart as ChartJS,
  Filler,
  Legend,
  LinearScale,
  LineElement,
  PointElement,
  Tooltip
} from 'chart.js'
import type { ChartData, ChartOptions } from 'chart.js'
import { Line } from 'vue-chartjs'
import AppLayout from '@/components/layout/AppLayout.vue'
import LoadingSpinner from '@/components/common/LoadingSpinner.vue'
import Icon from '@/components/icons/Icon.vue'
import SharePolicyPanel from '@/components/admin/revenue/SharePolicyPanel.vue'
import ShareSettlementsPanel from '@/components/admin/revenue/ShareSettlementsPanel.vue'
import { revenueAPI } from '@/api/admin/revenue'
import type {
  RevenueBreakdownItem,
  RevenueGranularity,
  RevenueShareOwnerBreakdownItem,
  RevenueSummary
} from '@/api/admin/revenue'
import { useAppStore } from '@/stores/app'
import { extractI18nErrorMessage } from '@/utils/apiError'

ChartJS.register(CategoryScale, LinearScale, PointElement, LineElement, Tooltip, Legend, Filler)

type RangeDays = 7 | 30 | 90
type BreakdownKey = 'consumers' | 'shareOwners' | 'groups' | 'accounts' | 'models'
type RevenueTab = 'overview' | 'sharePolicy' | 'shareSettlements'
type BreakdownValueType = 'amount' | 'percent'

interface RevenueBreakdownDisplayItem {
  id?: number
  name: string
  secondary?: string
  requests: number
  total_tokens: number
  primary_amount: number
  secondary_amount: number
  tertiary_amount: number
  quaternary_amount: number
  quaternary_type: BreakdownValueType
  quinary_amount: number
  quinary_type: BreakdownValueType
}

const DAYS_OPTIONS: RangeDays[] = [7, 30, 90]

const { t, locale } = useI18n()
const appStore = useAppStore()

const rangeDays = ref<RangeDays>(30)
const granularity = ref<RevenueGranularity>('day')
const loading = ref(false)
const summary = ref<RevenueSummary | null>(null)
const activeBreakdown = ref<BreakdownKey>('shareOwners')
const activeRevenueTab = ref<RevenueTab>('overview')
let requestSeq = 0

const amountFormatter = computed(() => new Intl.NumberFormat(locale.value, {
  minimumFractionDigits: 2,
  maximumFractionDigits: 6
}))

const integerFormatter = computed(() => new Intl.NumberFormat(locale.value, {
  maximumFractionDigits: 0
}))

const percentFormatter = computed(() => new Intl.NumberFormat(locale.value, {
  style: 'percent',
  minimumFractionDigits: 2,
  maximumFractionDigits: 2
}))

const granularityOptions = computed(() => [
  { value: 'day' as const, label: t('admin.revenue.controls.day') },
  { value: 'hour' as const, label: t('admin.revenue.controls.hour') }
])

const revenueTabs = computed(() => [
  { key: 'overview' as const, label: t('admin.revenue.tabs.overview') },
  { key: 'sharePolicy' as const, label: t('admin.revenue.tabs.sharePolicy') },
  { key: 'shareSettlements' as const, label: t('admin.revenue.tabs.shareSettlements') }
])

const statCards = computed(() => {
  const data = summary.value
  if (!data) return []

  return [
    {
      key: 'net-cash',
      label: t('admin.revenue.cards.netCash'),
      value: formatAmount(data.cash.net_paid_amount),
      meta: t('admin.revenue.cards.netCashMeta', {
        paid: formatAmount(data.cash.paid_amount),
        refunds: formatAmount(data.cash.refund_amount)
      }),
      borderClass: 'border-emerald-500',
      dotClass: 'bg-emerald-500'
    },
    {
      key: 'consumed',
      label: t('admin.revenue.cards.consumedRevenue'),
      value: formatAmount(data.usage.consumed_revenue),
      meta: t('admin.revenue.cards.consumedRevenueMeta', {
        requests: formatInteger(data.usage.requests)
      }),
      borderClass: 'border-sky-500',
      dotClass: 'bg-sky-500'
    },
    {
      key: 'account-cost',
      label: t('admin.revenue.cards.accountCost'),
      value: formatAmount(data.usage.account_cost),
      meta: t('admin.revenue.cards.accountCostMeta', {
        tokens: formatInteger(data.usage.total_tokens)
      }),
      borderClass: 'border-amber-500',
      dotClass: 'bg-amber-500'
    },
    {
      key: 'gross-profit',
      label: t('admin.revenue.cards.grossProfit'),
      value: formatAmount(data.profit.usage_gross_profit),
      meta: t('admin.revenue.cards.grossProfitMeta', {
        margin: formatPercent(data.profit.usage_gross_margin)
      }),
      borderClass: 'border-indigo-500',
      dotClass: 'bg-indigo-500'
    },
    {
      key: 'estimated-net',
      label: t('admin.revenue.cards.estimatedNetProfit'),
      value: formatAmount(data.profit.estimated_net_profit),
      meta: t('admin.revenue.cards.estimatedNetProfitMeta', {
        margin: formatPercent(data.profit.estimated_net_margin)
      }),
      borderClass: 'border-teal-500',
      dotClass: 'bg-teal-500'
    },
    {
      key: 'pending',
      label: t('admin.revenue.cards.pendingAmount'),
      value: formatAmount(data.cash.pending_amount),
      meta: t('admin.revenue.cards.pendingAmountMeta', {
        count: formatInteger(data.cash.pending_order_count)
      }),
      borderClass: 'border-rose-500',
      dotClass: 'bg-rose-500'
    }
  ]
})

const chartData = computed<ChartData<'line'>>(() => {
  const trend = summary.value?.trend ?? []
  return {
    labels: trend.map(point => point.date),
    datasets: [
      {
        label: t('admin.revenue.chart.paid'),
        data: trend.map(point => point.net_paid_amount),
        borderColor: '#059669',
        backgroundColor: 'rgba(5, 150, 105, 0.08)',
        pointRadius: 2,
        tension: 0.3,
        fill: false
      },
      {
        label: t('admin.revenue.chart.consumed'),
        data: trend.map(point => point.consumed_revenue),
        borderColor: '#0284c7',
        backgroundColor: 'rgba(2, 132, 199, 0.08)',
        pointRadius: 2,
        tension: 0.3,
        fill: false
      },
      {
        label: t('admin.revenue.chart.cost'),
        data: trend.map(point => point.account_cost),
        borderColor: '#d97706',
        backgroundColor: 'rgba(217, 119, 6, 0.08)',
        pointRadius: 2,
        tension: 0.3,
        fill: false
      },
      {
        label: t('admin.revenue.chart.netProfit'),
        data: trend.map(point => point.estimated_net_profit),
        borderColor: '#0f766e',
        backgroundColor: 'rgba(15, 118, 110, 0.08)',
        pointRadius: 2,
        tension: 0.3,
        fill: true
      }
    ]
  }
})

const chartOptions = computed<ChartOptions<'line'>>(() => ({
  responsive: true,
  maintainAspectRatio: false,
  interaction: {
    intersect: false,
    mode: 'index'
  },
  plugins: {
    legend: {
      position: 'bottom',
      labels: {
        boxWidth: 12,
        usePointStyle: true
      }
    },
    tooltip: {
      callbacks: {
        label: context => `${context.dataset.label}: ${formatAmount(Number(context.parsed.y || 0))}`
      }
    }
  },
  scales: {
    x: {
      grid: {
        display: false
      },
      ticks: {
        maxRotation: 0,
        autoSkip: true,
        maxTicksLimit: 10
      }
    },
    y: {
      beginAtZero: true,
      ticks: {
        callback: value => formatAmount(Number(value))
      }
    }
  }
}))

const cashRows = computed(() => {
  const data = summary.value?.cash
  if (!data) return []
  return [
    { key: 'paid', label: t('admin.revenue.fields.paidAmount'), value: formatAmount(data.paid_amount) },
    { key: 'balance', label: t('admin.revenue.fields.balancePaid'), value: formatAmount(data.balance_paid_amount) },
    { key: 'subscription', label: t('admin.revenue.fields.subscriptionPaid'), value: formatAmount(data.subscription_paid_amount) },
    { key: 'refund', label: t('admin.revenue.fields.refundAmount'), value: formatAmount(data.refund_amount) },
    { key: 'pending', label: t('admin.revenue.fields.pendingAmount'), value: formatAmount(data.pending_amount) },
    { key: 'paid-count', label: t('admin.revenue.fields.paidOrders'), value: formatInteger(data.paid_order_count) },
    { key: 'refund-count', label: t('admin.revenue.fields.refundOrders'), value: formatInteger(data.refund_order_count) },
    { key: 'pending-count', label: t('admin.revenue.fields.pendingOrders'), value: formatInteger(data.pending_order_count) }
  ]
})

const adjustmentRows = computed(() => {
  const data = summary.value?.adjustments
  if (!data) return []
  return [
    { key: 'affiliate-rebate', label: t('admin.revenue.fields.affiliateRebate'), value: formatAmount(data.affiliate_rebate) },
    { key: 'affiliate-transfer', label: t('admin.revenue.fields.affiliateTransfer'), value: formatAmount(data.affiliate_transfer) },
    { key: 'affiliate-count', label: t('admin.revenue.fields.affiliateRebateCount'), value: formatInteger(data.affiliate_rebate_count) },
    { key: 'share-consumer', label: t('admin.revenue.fields.shareConsumerCharge'), value: formatAmount(data.share_consumer_charge) },
    { key: 'share-account', label: t('admin.revenue.fields.shareAccountCost'), value: formatAmount(data.share_account_cost) },
    { key: 'share-owner', label: t('admin.revenue.fields.shareOwnerCredit'), value: formatAmount(data.share_owner_credit) },
    { key: 'share-platform', label: t('admin.revenue.fields.sharePlatformFee'), value: formatAmount(data.share_platform_fee) },
    { key: 'share-net', label: t('admin.revenue.fields.shareNetProfit'), value: formatAmount(data.share_net_profit) },
    { key: 'share-count', label: t('admin.revenue.fields.shareSettlementCount'), value: formatInteger(data.share_settlement_count) }
  ]
})

const breakdownTabs = computed(() => [
  { key: 'consumers' as const, label: t('admin.revenue.breakdown.consumers') },
  { key: 'shareOwners' as const, label: t('admin.revenue.breakdown.shareOwners') },
  { key: 'groups' as const, label: t('admin.revenue.breakdown.groups') },
  { key: 'accounts' as const, label: t('admin.revenue.breakdown.accounts') },
  { key: 'models' as const, label: t('admin.revenue.breakdown.models') }
])

const activeBreakdownHint = computed(() => {
  switch (activeBreakdown.value) {
    case 'shareOwners':
      return t('admin.revenue.breakdownHints.shareOwners')
    case 'groups':
      return t('admin.revenue.breakdownHints.groups')
    case 'accounts':
      return t('admin.revenue.breakdownHints.accounts')
    case 'models':
      return t('admin.revenue.breakdownHints.models')
    case 'consumers':
    default:
      return t('admin.revenue.breakdownHints.consumers')
  }
})

const breakdownColumnLabels = computed(() => {
  if (activeBreakdown.value === 'shareOwners') {
    return {
      primary: t('admin.revenue.table.consumerCharge'),
      secondary: t('admin.revenue.table.cost'),
      tertiary: t('admin.revenue.table.ownerCredit'),
      quaternary: t('admin.revenue.table.platformFee'),
      quinary: t('admin.revenue.table.shareRatio')
    }
  }
  return {
    primary: t('admin.revenue.table.platformRevenue'),
    secondary: t('admin.revenue.table.cost'),
    tertiary: t('admin.revenue.table.shareExpense'),
    quaternary: t('admin.revenue.table.platformNetProfit'),
    quinary: t('admin.revenue.table.platformNetMargin')
  }
})

const activeBreakdownItems = computed<RevenueBreakdownDisplayItem[]>(() => {
  const data = summary.value
  if (!data) return []
  switch (activeBreakdown.value) {
    case 'shareOwners':
      return data.top_share_owners.map(mapShareOwnerBreakdownItem)
    case 'groups':
      return data.top_groups.map(mapUsageBreakdownItem)
    case 'accounts':
      return data.top_accounts.map(mapUsageBreakdownItem)
    case 'models':
      return data.top_models.map(mapUsageBreakdownItem)
    case 'consumers':
    default:
      return data.top_users.map(mapUsageBreakdownItem)
  }
})

function mapUsageBreakdownItem(item: RevenueBreakdownItem): RevenueBreakdownDisplayItem {
  return {
    id: item.id,
    name: item.name,
    secondary: item.secondary,
    requests: item.requests,
    total_tokens: item.total_tokens,
    primary_amount: item.consumed_revenue,
    secondary_amount: item.account_cost,
    tertiary_amount: item.share_owner_credit,
    quaternary_amount: item.net_profit,
    quaternary_type: 'amount',
    quinary_amount: item.net_margin,
    quinary_type: 'percent'
  }
}

function mapShareOwnerBreakdownItem(item: RevenueShareOwnerBreakdownItem): RevenueBreakdownDisplayItem {
  return {
    id: item.id,
    name: item.name,
    secondary: item.secondary,
    requests: item.requests,
    total_tokens: item.total_tokens,
    primary_amount: item.consumer_charge,
    secondary_amount: item.account_cost,
    tertiary_amount: item.owner_credit,
    quaternary_amount: item.platform_fee,
    quaternary_type: 'amount',
    quinary_amount: item.owner_share_ratio,
    quinary_type: 'percent'
  }
}

function isRangeDisabled(days: RangeDays): boolean {
  return granularity.value === 'hour' && days > 30
}

function setRangeDays(days: RangeDays) {
  if (isRangeDisabled(days) || rangeDays.value === days) return
  rangeDays.value = days
  void loadSummary()
}

function setGranularity(value: RevenueGranularity) {
  if (granularity.value === value) return
  granularity.value = value
  if (value === 'hour' && rangeDays.value > 30) {
    rangeDays.value = 30
  }
  void loadSummary()
}

async function loadSummary() {
  const currentRequest = ++requestSeq
  loading.value = true
  try {
    const range = getDateRange(rangeDays.value)
    const res = await revenueAPI.getSummary({
      start_date: range.start,
      end_date: range.end,
      granularity: granularity.value,
      top_limit: 10
    })
    if (currentRequest === requestSeq) {
      summary.value = res.data
    }
  } catch (err: unknown) {
    if (currentRequest === requestSeq) {
      appStore.showError(extractI18nErrorMessage(err, t, 'admin.revenue.errors', t('admin.revenue.loadFailed')))
    }
  } finally {
    if (currentRequest === requestSeq) {
      loading.value = false
    }
  }
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

function formatAmount(value: number): string {
  return amountFormatter.value.format(Number.isFinite(value) ? value : 0)
}

function formatInteger(value: number): string {
  return integerFormatter.value.format(Number.isFinite(value) ? value : 0)
}

function formatPercent(value: number): string {
  return percentFormatter.value.format(Number.isFinite(value) ? value : 0)
}

onMounted(() => {
  void loadSummary()
})
</script>
