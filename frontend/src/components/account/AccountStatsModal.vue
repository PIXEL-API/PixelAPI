<template>
  <BaseDialog
    :show="show"
    :title="t('admin.accounts.stats.accountLedger')"
    width="full"
    @close="handleClose"
  >
    <div class="space-y-5">
      <section
        v-if="account"
        class="relative overflow-hidden rounded-2xl border border-slate-200 bg-slate-950 px-4 py-4 text-white shadow-sm dark:border-slate-700 sm:px-5"
      >
        <div
          class="pointer-events-none absolute -right-16 -top-24 h-56 w-56 rounded-full bg-blue-500/20 blur-3xl"
          aria-hidden="true"
        ></div>
        <div class="relative flex flex-col gap-4 lg:flex-row lg:items-center lg:justify-between">
          <div class="min-w-0">
            <div class="flex flex-wrap items-center gap-2">
              <h2 class="truncate text-lg font-semibold tracking-tight sm:text-xl">
                {{ account.name }}
              </h2>
              <span
                class="rounded-full border px-2 py-0.5 text-[11px] font-semibold uppercase tracking-wide"
                :class="
                  account.status === 'active'
                    ? 'border-emerald-400/30 bg-emerald-400/10 text-emerald-300'
                    : 'border-slate-500/40 bg-slate-500/10 text-slate-300'
                "
              >
                {{ account.status }}
              </span>
            </div>
            <div class="mt-1.5 flex flex-wrap items-center gap-x-2 gap-y-1 text-xs text-slate-300">
              <span class="font-mono uppercase">{{ account.platform }}</span>
              <span class="text-slate-600">/</span>
              <span>{{ account.type }}</span>
              <span class="text-slate-600">/</span>
              <span>#{{ account.id }}</span>
            </div>
            <p class="mt-2 max-w-2xl text-xs leading-5 text-slate-400">
              {{ t('admin.accounts.stats.accountLedgerSubtitle') }}
            </p>
          </div>

          <div class="flex w-full flex-col gap-2 lg:w-auto lg:items-end">
            <form
              class="rounded-xl border border-white/10 bg-white/5 p-2"
              :aria-label="t('admin.accounts.stats.customRange')"
              @submit.prevent="applyDateRange"
            >
              <div class="grid gap-2 sm:grid-cols-[minmax(9rem,1fr)_minmax(9rem,1fr)_auto] sm:items-end">
                <label class="block min-w-0">
                  <span class="mb-1 block text-[10px] font-semibold uppercase tracking-wide text-slate-400">
                    {{ t('admin.accounts.stats.startDate') }}
                  </span>
                  <input
                    v-model="draftRange.startDate"
                    type="date"
                    :max="draftRange.endDate || maxSelectableDate"
                    class="min-h-11 w-full rounded-lg border border-white/10 bg-slate-900/70 px-3 text-sm text-white outline-none transition-colors [color-scheme:dark] focus:border-blue-400 focus:ring-2 focus:ring-blue-400/30"
                  />
                </label>
                <label class="block min-w-0">
                  <span class="mb-1 block text-[10px] font-semibold uppercase tracking-wide text-slate-400">
                    {{ t('admin.accounts.stats.endDate') }}
                  </span>
                  <input
                    v-model="draftRange.endDate"
                    type="date"
                    :min="draftRange.startDate"
                    :max="draftEndMax"
                    class="min-h-11 w-full rounded-lg border border-white/10 bg-slate-900/70 px-3 text-sm text-white outline-none transition-colors [color-scheme:dark] focus:border-blue-400 focus:ring-2 focus:ring-blue-400/30"
                  />
                </label>
                <button
                  type="submit"
                  class="min-h-11 rounded-lg bg-white px-4 text-xs font-semibold text-slate-950 transition-colors hover:bg-blue-50 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-blue-400 disabled:cursor-not-allowed disabled:opacity-45"
                  :disabled="Boolean(dateRangeError) || !hasPendingRangeChanges || statsLoading"
                >
                  {{ t('admin.accounts.stats.applyRange') }}
                </button>
              </div>
              <p
                class="mt-1.5 text-[11px] leading-4"
                :class="dateRangeError ? 'text-rose-300' : 'text-slate-400'"
                aria-live="polite"
              >
                {{ dateRangeError || t('admin.accounts.stats.rangeLimitHint') }}
              </p>
            </form>
            <button
              type="button"
              class="inline-flex min-h-11 w-full items-center justify-center gap-2 rounded-xl border border-white/10 bg-white/5 px-4 text-xs font-semibold text-white transition-colors hover:bg-white/10 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-blue-400 disabled:cursor-not-allowed disabled:opacity-50 sm:w-auto"
              :disabled="statsLoading || usageLoading"
              @click="reloadAll"
            >
              <svg
                class="h-4 w-4"
                :class="{ 'animate-spin': statsLoading || usageLoading }"
                viewBox="0 0 24 24"
                fill="none"
                stroke="currentColor"
                aria-hidden="true"
              >
                <path
                  stroke-linecap="round"
                  stroke-linejoin="round"
                  stroke-width="2"
                  d="M20 11a8.1 8.1 0 0 0-15.5-2M4 4v5h5m-5 4a8.1 8.1 0 0 0 15.5 2m.5 5v-5h-5"
                />
              </svg>
              {{ t('admin.accounts.stats.refresh') }}
            </button>
          </div>
        </div>
      </section>

      <section v-if="usageLoader" aria-labelledby="quota-window-title">
        <div class="mb-3 flex flex-col gap-1 sm:flex-row sm:items-end sm:justify-between">
          <div>
            <h3
              id="quota-window-title"
              class="text-sm font-semibold text-slate-950 dark:text-white"
            >
              {{ t('admin.accounts.stats.currentQuotaWindows') }}
            </h3>
            <p class="mt-1 text-xs leading-5 text-slate-500 dark:text-slate-400">
              {{ t('admin.accounts.stats.currentQuotaWindowsHint') }}
            </p>
          </div>
          <span v-if="usage?.updated_at" class="text-[11px] text-slate-400">
            {{ t('admin.accounts.stats.snapshotAt', { time: formatDateTime(usage.updated_at) }) }}
          </span>
        </div>

        <div
          v-if="usageError"
          class="mb-3 rounded-xl border border-rose-200 bg-rose-50 px-4 py-3 text-xs text-rose-700 dark:border-rose-900/60 dark:bg-rose-950/30 dark:text-rose-300"
          role="alert"
        >
          {{ usageError }}
        </div>

        <div
          v-if="usageLoading && !usage"
          class="grid min-h-48 place-items-center rounded-2xl border border-slate-200 bg-slate-50 dark:border-slate-700 dark:bg-slate-900/40"
        >
          <LoadingSpinner />
        </div>
        <div v-else class="grid gap-3 lg:grid-cols-2">
          <AccountUsageWindowCard
            label="5H"
            :title="t('admin.accounts.stats.fiveHourWindow')"
            :progress="usage?.five_hour"
          />
          <AccountUsageWindowCard
            label="7D"
            :title="t('admin.accounts.stats.sevenDayWindow')"
            :progress="usage?.seven_day"
          />
        </div>
        <p
          class="mt-3 rounded-xl border border-amber-200/70 bg-amber-50 px-3 py-2 text-[11px] leading-5 text-amber-800 dark:border-amber-900/60 dark:bg-amber-950/20 dark:text-amber-300"
        >
          {{ t('admin.accounts.stats.previousWindowUnavailable') }}
        </p>
      </section>

      <div
        v-if="statsError"
        class="rounded-xl border border-rose-200 bg-rose-50 px-4 py-3 text-sm text-rose-700 dark:border-rose-900/60 dark:bg-rose-950/30 dark:text-rose-300"
        role="alert"
      >
        <div class="font-semibold">{{ t('admin.accounts.stats.statsLoadFailed') }}</div>
        <div class="mt-1 text-xs opacity-90">{{ statsError }}</div>
      </div>

      <div
        v-if="statsLoading && !stats"
        class="grid min-h-72 place-items-center rounded-2xl border border-slate-200 bg-white dark:border-slate-700 dark:bg-slate-900/50"
      >
        <LoadingSpinner />
      </div>

      <template v-else-if="stats">
        <section aria-labelledby="range-summary-title">
          <div class="mb-3 flex items-end justify-between gap-3">
            <div>
              <h3
                id="range-summary-title"
                class="text-sm font-semibold text-slate-950 dark:text-white"
              >
                {{ t('admin.accounts.stats.rangeSummary', { range: formattedAppliedRange }) }}
              </h3>
              <p class="mt-1 text-xs text-slate-500 dark:text-slate-400">
                {{ t('admin.accounts.stats.pixelBillingOnly') }}
              </p>
            </div>
            <span
              v-if="statsLoading"
              class="inline-flex items-center gap-2 text-xs text-blue-600 dark:text-blue-400"
            >
              <span class="h-1.5 w-1.5 animate-pulse rounded-full bg-current"></span>
              {{ t('common.loading') }}
            </span>
          </div>

          <div class="grid grid-cols-2 gap-px overflow-hidden rounded-2xl border border-slate-200 bg-slate-200 dark:border-slate-700 dark:bg-slate-700 lg:grid-cols-4">
            <div
              v-for="item in summaryCards"
              :key="item.label"
              class="min-w-0 bg-white px-4 py-4 dark:bg-slate-900/80 sm:px-5"
            >
              <div class="text-[11px] font-semibold uppercase tracking-wide text-slate-500 dark:text-slate-400">
                {{ item.label }}
              </div>
              <div
                class="mt-2 truncate font-mono text-xl font-semibold tabular-nums text-slate-950 dark:text-white sm:text-2xl"
                :title="item.value"
              >
                {{ item.value }}
              </div>
              <div class="mt-1 truncate text-[11px] text-slate-400">
                {{ item.note }}
              </div>
            </div>
          </div>
        </section>

        <section class="grid gap-4 xl:grid-cols-3">
          <div
            class="rounded-2xl border border-slate-200 bg-white p-4 dark:border-slate-700 dark:bg-slate-900/70 xl:col-span-2"
          >
            <div class="mb-4 flex flex-col gap-1 sm:flex-row sm:items-end sm:justify-between">
              <div>
                <h3 class="text-sm font-semibold text-slate-950 dark:text-white">
                  {{ t('admin.accounts.stats.dailyTrend') }}
                </h3>
                <p class="mt-1 text-xs text-slate-500 dark:text-slate-400">
                  {{ t('admin.accounts.stats.dailyTrendHint') }}
                </p>
              </div>
              <div class="flex items-center gap-3 text-[11px] text-slate-500 dark:text-slate-400">
                <span class="inline-flex items-center gap-1.5">
                  <span class="h-0.5 w-4 rounded-full bg-blue-600"></span>
                  {{ t('admin.accounts.stats.pixelCost') }}
                </span>
                <span class="inline-flex items-center gap-1.5">
                  <span class="h-0.5 w-4 rounded-full bg-amber-500"></span>
                  {{ t('admin.accounts.stats.requests') }}
                </span>
              </div>
            </div>
            <div class="h-64 sm:h-72">
              <Line
                v-if="trendChartData"
                :data="trendChartData"
                :options="lineChartOptions"
              />
              <div
                v-else
                class="grid h-full place-items-center rounded-xl border border-dashed border-slate-200 text-sm text-slate-400 dark:border-slate-700"
              >
                {{ t('admin.accounts.stats.noData') }}
              </div>
            </div>
          </div>

          <aside
            class="flex flex-col rounded-2xl border border-slate-200 bg-slate-950 p-4 text-white dark:border-slate-700 sm:p-5"
          >
            <div>
              <div class="text-[11px] font-semibold uppercase tracking-[0.16em] text-blue-300">
                {{ t('admin.accounts.stats.lifetimeSummary') }}
              </div>
              <div class="mt-2 font-mono text-3xl font-semibold tabular-nums">
                ${{ formatCost(stats.lifetime?.total_cost || 0) }}
              </div>
              <p class="mt-1 text-xs leading-5 text-slate-400">
                {{ t('admin.accounts.stats.lifetimeCostHint') }}
              </p>
            </div>

            <dl class="mt-5 divide-y divide-white/10 border-y border-white/10">
              <div class="flex items-center justify-between gap-3 py-3">
                <dt class="text-xs text-slate-400">{{ t('admin.accounts.stats.requests') }}</dt>
                <dd class="font-mono text-sm font-semibold tabular-nums">
                  {{ formatCompact(stats.lifetime?.total_requests || 0) }}
                </dd>
              </div>
              <div class="flex items-center justify-between gap-3 py-3">
                <dt class="text-xs text-slate-400">{{ t('admin.accounts.stats.tokens') }}</dt>
                <dd class="font-mono text-sm font-semibold tabular-nums">
                  {{ formatCompact(stats.lifetime?.total_tokens || 0) }}
                </dd>
              </div>
              <div class="flex items-center justify-between gap-3 py-3">
                <dt class="text-xs text-slate-400">
                  {{ t('admin.accounts.stats.mergedAccountRecords') }}
                </dt>
                <dd class="font-mono text-sm font-semibold tabular-nums">
                  {{ stats.lifetime?.source_account_count || 1 }}
                </dd>
              </div>
            </dl>

            <div class="mt-auto pt-4">
              <div class="text-[11px] font-semibold uppercase tracking-wide text-slate-500">
                {{ t('admin.accounts.stats.availableCoverage') }}
              </div>
              <div class="mt-1 text-xs leading-5 text-slate-300">
                {{ lifetimeCoverage }}
              </div>
              <p class="mt-2 text-[11px] leading-4 text-slate-500">
                {{ t('admin.accounts.stats.lifetimeMergeHint') }}
              </p>
            </div>
          </aside>
        </section>

        <section
          class="grid gap-px overflow-hidden rounded-2xl border border-slate-200 bg-slate-200 dark:border-slate-700 dark:bg-slate-700 sm:grid-cols-3"
        >
          <div
            v-for="item in activityHighlights"
            :key="item.label"
            class="flex items-center justify-between gap-4 bg-slate-50 px-4 py-3 dark:bg-slate-900/50"
          >
            <div class="min-w-0">
              <div class="text-xs font-medium text-slate-500 dark:text-slate-400">
                {{ item.label }}
              </div>
              <div class="mt-0.5 truncate text-xs text-slate-400">{{ item.detail }}</div>
            </div>
            <div class="shrink-0 font-mono text-sm font-semibold tabular-nums text-slate-950 dark:text-white">
              {{ item.value }}
            </div>
          </div>
        </section>

        <section
          class="overflow-hidden rounded-2xl border border-slate-200 bg-white dark:border-slate-700 dark:bg-slate-900/70"
          aria-labelledby="daily-details-title"
        >
          <div class="border-b border-slate-200 px-4 py-3 dark:border-slate-700 sm:px-5">
            <h3 id="daily-details-title" class="text-sm font-semibold text-slate-950 dark:text-white">
              {{ t('admin.accounts.stats.dailyDetails') }}
            </h3>
            <p class="mt-1 text-xs text-slate-500 dark:text-slate-400">
              {{ t('admin.accounts.stats.dailyDetailsHint', { range: formattedAppliedRange }) }}
            </p>
          </div>
          <div class="max-h-80 overflow-auto">
            <table class="w-full min-w-[560px] text-left text-xs">
              <thead class="sticky top-0 z-10 bg-slate-50 text-slate-500 dark:bg-slate-900 dark:text-slate-400">
                <tr>
                  <th class="px-4 py-3 font-semibold sm:px-5">{{ t('admin.accounts.stats.date') }}</th>
                  <th class="px-4 py-3 text-right font-semibold">{{ t('admin.accounts.stats.pixelCost') }}</th>
                  <th class="px-4 py-3 text-right font-semibold">{{ t('admin.accounts.stats.requests') }}</th>
                  <th class="px-4 py-3 text-right font-semibold sm:pr-5">{{ t('admin.accounts.stats.tokens') }}</th>
                </tr>
              </thead>
              <tbody class="divide-y divide-slate-100 dark:divide-slate-800">
                <tr
                  v-for="item in reversedHistory"
                  :key="item.date"
                  class="text-slate-600 hover:bg-slate-50/80 dark:text-slate-300 dark:hover:bg-slate-800/40"
                >
                  <td class="whitespace-nowrap px-4 py-3 font-medium text-slate-900 dark:text-white sm:px-5">
                    {{ item.label || item.date }}
                  </td>
                  <td class="px-4 py-3 text-right font-mono tabular-nums">
                    ${{ formatCost(item.actual_cost) }}
                  </td>
                  <td class="px-4 py-3 text-right font-mono tabular-nums">
                    {{ formatNumber(item.requests) }}
                  </td>
                  <td class="px-4 py-3 text-right font-mono tabular-nums sm:pr-5">
                    {{ formatCompact(item.tokens) }}
                  </td>
                </tr>
                <tr v-if="reversedHistory.length === 0">
                  <td colspan="4" class="px-4 py-10 text-center text-slate-400">
                    {{ t('admin.accounts.stats.noData') }}
                  </td>
                </tr>
              </tbody>
            </table>
          </div>
        </section>

        <section
          class="overflow-hidden rounded-2xl border border-slate-200 bg-white dark:border-slate-700 dark:bg-slate-900/70"
          aria-labelledby="breakdowns-title"
        >
          <div class="flex flex-col gap-3 border-b border-slate-200 px-4 py-3 dark:border-slate-700 sm:flex-row sm:items-center sm:justify-between sm:px-5">
            <div>
              <h3 id="breakdowns-title" class="text-sm font-semibold text-slate-950 dark:text-white">
                {{ t('admin.accounts.stats.breakdowns') }}
              </h3>
              <p class="mt-1 text-xs text-slate-500 dark:text-slate-400">
                {{ t('admin.accounts.stats.breakdownCoverageHint') }}
              </p>
            </div>
            <div class="grid min-h-11 grid-cols-3 rounded-xl bg-slate-100 p-1 dark:bg-slate-800">
              <button
                v-for="option in breakdownOptions"
                :key="option.key"
                type="button"
                class="min-h-11 rounded-lg px-3 text-xs font-semibold transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-blue-500"
                :class="
                  activeBreakdown === option.key
                    ? 'bg-white text-slate-950 shadow-sm dark:bg-slate-700 dark:text-white'
                    : 'text-slate-500 hover:text-slate-900 dark:text-slate-400 dark:hover:text-white'
                "
                :aria-pressed="activeBreakdown === option.key"
                @click="activeBreakdown = option.key"
              >
                {{ option.label }}
              </button>
            </div>
          </div>
          <div class="max-h-80 overflow-auto">
            <table class="w-full min-w-[620px] text-left text-xs">
              <thead class="sticky top-0 z-10 bg-slate-50 text-slate-500 dark:bg-slate-900 dark:text-slate-400">
                <tr>
                  <th class="px-4 py-3 font-semibold sm:px-5">{{ breakdownNameLabel }}</th>
                  <th class="px-4 py-3 text-right font-semibold">{{ t('admin.accounts.stats.pixelCost') }}</th>
                  <th class="px-4 py-3 text-right font-semibold">{{ t('admin.accounts.stats.requests') }}</th>
                  <th class="px-4 py-3 text-right font-semibold sm:pr-5">{{ t('admin.accounts.stats.tokens') }}</th>
                </tr>
              </thead>
              <tbody class="divide-y divide-slate-100 dark:divide-slate-800">
                <tr
                  v-for="row in breakdownRows"
                  :key="row.name"
                  class="text-slate-600 hover:bg-slate-50/80 dark:text-slate-300 dark:hover:bg-slate-800/40"
                >
                  <td
                    class="max-w-[320px] truncate px-4 py-3 font-medium text-slate-900 dark:text-white sm:px-5"
                    :title="row.name"
                  >
                    {{ row.name }}
                  </td>
                  <td class="px-4 py-3 text-right font-mono tabular-nums">
                    ${{ formatCost(row.cost) }}
                  </td>
                  <td class="px-4 py-3 text-right font-mono tabular-nums">
                    {{ formatNumber(row.requests) }}
                  </td>
                  <td class="px-4 py-3 text-right font-mono tabular-nums sm:pr-5">
                    {{ formatCompact(row.tokens) }}
                  </td>
                </tr>
                <tr v-if="breakdownRows.length === 0">
                  <td colspan="4" class="px-4 py-10 text-center text-slate-400">
                    {{ t('admin.accounts.stats.noData') }}
                  </td>
                </tr>
              </tbody>
            </table>
          </div>
        </section>
      </template>

      <div
        v-else-if="!statsLoading && !statsError"
        class="grid min-h-64 place-items-center rounded-2xl border border-dashed border-slate-200 text-center dark:border-slate-700"
      >
        <div>
          <div class="text-sm font-semibold text-slate-700 dark:text-slate-200">
            {{ t('admin.accounts.stats.noData') }}
          </div>
          <div class="mt-1 text-xs text-slate-400">
            {{ t('admin.accounts.stats.noDataHint') }}
          </div>
        </div>
      </div>
    </div>

    <template #footer>
      <button
        type="button"
        class="min-h-11 rounded-xl bg-slate-950 px-5 text-sm font-semibold text-white transition-colors hover:bg-slate-800 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-blue-500 dark:bg-white dark:text-slate-950 dark:hover:bg-slate-200"
        @click="handleClose"
      >
        {{ t('common.close') }}
      </button>
    </template>
  </BaseDialog>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
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
import { Line } from 'vue-chartjs'
import BaseDialog from '@/components/common/BaseDialog.vue'
import LoadingSpinner from '@/components/common/LoadingSpinner.vue'
import AccountUsageWindowCard from '@/components/account/AccountUsageWindowCard.vue'
import type {
  Account,
  AccountStatsRange,
  AccountUsageInfo,
  AccountUsageStatsResponse,
  EndpointStat,
  ModelStat
} from '@/types'
import { formatCompactNumber } from '@/utils/format'

ChartJS.register(CategoryScale, LinearScale, PointElement, LineElement, Tooltip, Legend, Filler)

type UsageSource = 'local' | 'passive' | 'active'
type BreakdownKey = 'models' | 'inbound' | 'upstream'
type StatsLoader = (id: number, range: AccountStatsRange) => Promise<AccountUsageStatsResponse>
type UsageLoader = (
  id: number,
  source: UsageSource,
  options?: { signal?: AbortSignal }
) => Promise<AccountUsageInfo>

const props = defineProps<{
  show: boolean
  account: Account | null
  statsLoader: StatsLoader
  usageLoader?: UsageLoader
}>()

const emit = defineEmits<{
  (e: 'close'): void
}>()

const { t, locale } = useI18n()
const MAX_RANGE_DAYS = 31
const DEFAULT_RANGE_DAYS = 7
const BUSINESS_TIME_ZONE = 'Asia/Shanghai'
const MILLISECONDS_PER_DAY = 86_400_000

const toDateString = (date: Date, timeZone = 'UTC'): string => {
  const parts = new Intl.DateTimeFormat('en-US', {
    timeZone,
    year: 'numeric',
    month: '2-digit',
    day: '2-digit'
  }).formatToParts(date)
  const values = Object.fromEntries(parts.map((part) => [part.type, part.value]))
  return `${values.year}-${values.month}-${values.day}`
}

const shiftCalendarDate = (value: string, days: number): string => {
  const [year, month, day] = value.split('-').map(Number)
  return toDateString(new Date(Date.UTC(year, month - 1, day + days)))
}

const createDefaultRange = (): AccountStatsRange => {
  const endDate = toDateString(new Date(), BUSINESS_TIME_ZONE)
  return {
    startDate: shiftCalendarDate(endDate, -DEFAULT_RANGE_DAYS + 1),
    endDate
  }
}

const initialRange = createDefaultRange()
const maxSelectableDate = ref(initialRange.endDate)
const draftRange = ref<AccountStatsRange>({ ...initialRange })
const appliedRange = ref<AccountStatsRange>({ ...initialRange })
const stats = ref<AccountUsageStatsResponse | null>(null)
const usage = ref<AccountUsageInfo | null>(null)
const statsLoading = ref(false)
const usageLoading = ref(false)
const statsError = ref('')
const usageError = ref('')
const activeBreakdown = ref<BreakdownKey>('models')
let requestVersion = 0
let usageController: AbortController | null = null

const formatCost = (value: number): string => {
  const amount = Number(value)
  if (!Number.isFinite(amount)) return '0.0000'
  if (amount >= 1000) return `${(amount / 1000).toFixed(2)}K`
  if (amount >= 1) return amount.toFixed(2)
  if (amount >= 0.01) return amount.toFixed(3)
  return amount.toFixed(4)
}

const formatNumber = (value: number): string =>
  Number.isFinite(Number(value)) ? Number(value).toLocaleString(locale.value) : '0'

const formatCompact = (value: number): string => formatCompactNumber(Number(value) || 0)

const formatDuration = (milliseconds: number): string => {
  if (!Number.isFinite(milliseconds) || milliseconds <= 0) return '0 ms'
  if (milliseconds >= 1000) return `${(milliseconds / 1000).toFixed(2)} s`
  return `${Math.round(milliseconds)} ms`
}

const formatDateTime = (value?: string | null): string => {
  if (!value) return t('admin.accounts.stats.unknown')
  const date = new Date(value)
  if (!Number.isFinite(date.getTime())) return t('admin.accounts.stats.unknown')
  return new Intl.DateTimeFormat(locale.value, {
    year: 'numeric',
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit'
  }).format(date)
}

const formatCoverageDate = (value?: string | null): string => {
  if (!value) return ''
  const date = new Date(value)
  if (!Number.isFinite(date.getTime())) return ''
  return new Intl.DateTimeFormat(locale.value, {
    year: 'numeric',
    month: '2-digit',
    day: '2-digit'
  }).format(date)
}

const formatCalendarDate = (value: string): string => {
  const [year, month, day] = value.split('-').map(Number)
  if (!year || !month || !day) return value
  return new Intl.DateTimeFormat(locale.value, {
    year: 'numeric',
    month: '2-digit',
    day: '2-digit',
    timeZone: 'UTC'
  }).format(new Date(Date.UTC(year, month - 1, day)))
}

const isValidCalendarDate = (value: string): boolean =>
  /^\d{4}-\d{2}-\d{2}$/.test(value) && shiftCalendarDate(value, 0) === value

const getInclusiveDayCount = (range: AccountStatsRange): number => {
  if (!isValidCalendarDate(range.startDate) || !isValidCalendarDate(range.endDate)) return 0
  const [startYear, startMonth, startDay] = range.startDate.split('-').map(Number)
  const [endYear, endMonth, endDay] = range.endDate.split('-').map(Number)
  const difference =
    Date.UTC(endYear, endMonth - 1, endDay) - Date.UTC(startYear, startMonth - 1, startDay)
  return Math.floor(difference / MILLISECONDS_PER_DAY) + 1
}

const draftEndMax = computed(() => {
  if (!isValidCalendarDate(draftRange.value.startDate)) return maxSelectableDate.value
  const rangeMaximum = shiftCalendarDate(draftRange.value.startDate, MAX_RANGE_DAYS - 1)
  return rangeMaximum < maxSelectableDate.value ? rangeMaximum : maxSelectableDate.value
})

const dateRangeError = computed(() => {
  const { startDate, endDate } = draftRange.value
  if (!startDate || !endDate) return t('admin.accounts.stats.rangeRequired')
  if (!isValidCalendarDate(startDate) || !isValidCalendarDate(endDate)) {
    return t('admin.accounts.stats.invalidDateRange')
  }
  if (endDate < startDate) return t('admin.accounts.stats.endBeforeStart')
  if (endDate > maxSelectableDate.value) return t('admin.accounts.stats.futureDateNotAllowed')
  if (getInclusiveDayCount(draftRange.value) > MAX_RANGE_DAYS) {
    return t('admin.accounts.stats.rangeTooLong', { days: MAX_RANGE_DAYS })
  }
  return ''
})

const hasPendingRangeChanges = computed(
  () =>
    draftRange.value.startDate !== appliedRange.value.startDate ||
    draftRange.value.endDate !== appliedRange.value.endDate
)

const selectedDayCount = computed(() => getInclusiveDayCount(appliedRange.value))

const formattedAppliedRange = computed(
  () =>
    `${formatCalendarDate(appliedRange.value.startDate)} — ${formatCalendarDate(appliedRange.value.endDate)}`
)

const getErrorMessage = (error: unknown): string => {
  if (error instanceof Error && error.message.trim()) return error.message
  return t('admin.accounts.stats.unknownError')
}

const loadStats = async (): Promise<void> => {
  const account = props.account
  if (!props.show || !account) return
  const currentVersion = ++requestVersion
  statsLoading.value = true
  statsError.value = ''
  try {
    const response = await props.statsLoader(account.id, { ...appliedRange.value })
    if (currentVersion !== requestVersion || !props.show || props.account?.id !== account.id) return
    stats.value = response
  } catch (error) {
    if (currentVersion !== requestVersion) return
    statsError.value = getErrorMessage(error)
  } finally {
    if (currentVersion === requestVersion) statsLoading.value = false
  }
}

const loadUsage = async (): Promise<void> => {
  const account = props.account
  if (!props.show || !account || !props.usageLoader) return
  usageController?.abort()
  const controller = new AbortController()
  usageController = controller
  usageLoading.value = true
  usageError.value = ''
  try {
    const response = await props.usageLoader(account.id, 'local', { signal: controller.signal })
    if (!controller.signal.aborted && props.show && props.account?.id === account.id) {
      usage.value = response
    }
  } catch (error) {
    if (!controller.signal.aborted) {
      usageError.value = `${t('admin.accounts.stats.usageLoadFailed')}: ${getErrorMessage(error)}`
    }
  } finally {
    if (usageController === controller) {
      usageController = null
      usageLoading.value = false
    }
  }
}

const reloadAll = (): void => {
  void Promise.all([loadStats(), loadUsage()])
}

const applyDateRange = (): void => {
  if (dateRangeError.value || !hasPendingRangeChanges.value) return
  appliedRange.value = { ...draftRange.value }
  stats.value = null
  void loadStats()
}

watch(
  () => [props.show, props.account?.id] as const,
  ([isOpen, accountID], previous) => {
    const wasOpen = previous?.[0]
    const previousID = previous?.[1]
    if (!isOpen || !accountID) {
      requestVersion += 1
      usageController?.abort()
      usageController = null
      stats.value = null
      usage.value = null
      statsError.value = ''
      usageError.value = ''
      statsLoading.value = false
      usageLoading.value = false
      return
    }
    if (!wasOpen || accountID !== previousID) {
      const defaultRange = createDefaultRange()
      maxSelectableDate.value = defaultRange.endDate
      draftRange.value = { ...defaultRange }
      appliedRange.value = { ...defaultRange }
      activeBreakdown.value = 'models'
      stats.value = null
      usage.value = null
    }
    reloadAll()
  },
  { immediate: true }
)

const summaryCards = computed(() => {
  const summary = stats.value?.summary
  return [
    {
      label: t('admin.accounts.stats.periodCost'),
      value: `$${formatCost(summary?.total_cost || 0)}`,
      note: t('admin.accounts.stats.pixelCostNote')
    },
    {
      label: t('admin.accounts.stats.periodRequests'),
      value: formatCompact(summary?.total_requests || 0),
      note: t('admin.accounts.stats.requestCountNote')
    },
    {
      label: t('admin.accounts.stats.periodTokens'),
      value: formatCompact(summary?.total_tokens || 0),
      note: t('admin.accounts.stats.tokenCountNote')
    },
    {
      label: t('admin.accounts.stats.activeDays'),
      value: `${pixelHistory.value.length} / ${selectedDayCount.value}`,
      note: t('admin.accounts.stats.activeDaysNote')
    }
  ]
})

const pixelHistory = computed(() =>
  (stats.value?.history || []).filter(
    (item) => item.requests > 0 || item.tokens > 0 || Math.abs(item.actual_cost) > 0.0000001
  )
)

const reversedHistory = computed(() => [...pixelHistory.value].reverse())

const highestPixelCostDay = computed(() =>
  pixelHistory.value.reduce<(typeof pixelHistory.value)[number] | null>(
    (highest, item) => (!highest || item.actual_cost > highest.actual_cost ? item : highest),
    null
  )
)

const lifetimeCoverage = computed(() => {
  const lifetime = stats.value?.lifetime
  const start = formatCoverageDate(lifetime?.available_from)
  const end = formatCoverageDate(lifetime?.available_to)
  if (!start || !end) return t('admin.accounts.stats.noCoverage')
  return t('admin.accounts.stats.coverageRange', { start, end })
})

const activityHighlights = computed(() => {
  const summary = stats.value?.summary
  const highest = highestPixelCostDay.value
  return [
    {
      label: t('admin.accounts.stats.todayOverview'),
      value: `$${formatCost(summary?.today?.cost || 0)}`,
      detail: t('admin.accounts.stats.requestsWithCount', {
        count: formatNumber(summary?.today?.requests || 0)
      })
    },
    {
      label: t('admin.accounts.stats.highestCostDay'),
      value: `$${formatCost(highest?.actual_cost || 0)}`,
      detail: highest?.label || highest?.date || t('admin.accounts.stats.noData')
    },
    {
      label: t('admin.accounts.stats.avgResponseTime'),
      value: formatDuration(summary?.avg_duration_ms || 0),
      detail: t('admin.accounts.stats.selectedRangeAverage')
    }
  ]
})

const trendChartData = computed(() => {
  if (!pixelHistory.value.length) return null
  return {
    labels: pixelHistory.value.map((item) => item.label || item.date),
    datasets: [
      {
        label: t('admin.accounts.stats.pixelCost'),
        data: pixelHistory.value.map((item) => item.actual_cost),
        borderColor: '#2563eb',
        backgroundColor: 'rgba(37, 99, 235, 0.12)',
        pointBackgroundColor: '#2563eb',
        pointRadius: selectedDayCount.value > 30 ? 0 : 2,
        pointHoverRadius: 4,
        borderWidth: 2,
        fill: true,
        tension: 0.32,
        yAxisID: 'cost'
      },
      {
        label: t('admin.accounts.stats.requests'),
        data: pixelHistory.value.map((item) => item.requests),
        borderColor: '#f59e0b',
        backgroundColor: 'rgba(245, 158, 11, 0.08)',
        pointBackgroundColor: '#f59e0b',
        pointRadius: selectedDayCount.value > 30 ? 0 : 2,
        pointHoverRadius: 4,
        borderWidth: 1.5,
        fill: false,
        tension: 0.25,
        yAxisID: 'requests'
      }
    ]
  }
})

const lineChartOptions = computed(() => ({
  responsive: true,
  maintainAspectRatio: false,
  interaction: {
    intersect: false,
    mode: 'index' as const
  },
  plugins: {
    legend: { display: false },
    tooltip: {
      callbacks: {
        label: (context: { dataset: { yAxisID?: string; label?: string }; raw: unknown }) => {
          const value = Number(context.raw) || 0
          return context.dataset.yAxisID === 'cost'
            ? `${context.dataset.label}: $${formatCost(value)}`
            : `${context.dataset.label}: ${formatNumber(value)}`
        }
      }
    }
  },
  scales: {
    x: {
      grid: { display: false },
      ticks: {
        color: '#94a3b8',
        maxTicksLimit: selectedDayCount.value > 30 ? 8 : 12,
        maxRotation: 0,
        autoSkip: true
      }
    },
    cost: {
      type: 'linear' as const,
      position: 'left' as const,
      beginAtZero: true,
      grid: { color: 'rgba(148, 163, 184, 0.15)' },
      ticks: {
        color: '#64748b',
        callback: (value: string | number) => `$${formatCost(Number(value))}`
      }
    },
    requests: {
      type: 'linear' as const,
      position: 'right' as const,
      beginAtZero: true,
      grid: { drawOnChartArea: false },
      ticks: {
        color: '#d97706',
        callback: (value: string | number) => formatCompact(Number(value))
      }
    }
  }
}))

const breakdownOptions = computed<Array<{ key: BreakdownKey; label: string }>>(() => [
  { key: 'models', label: t('admin.accounts.stats.models') },
  { key: 'inbound', label: t('admin.accounts.stats.inbound') },
  { key: 'upstream', label: t('admin.accounts.stats.upstream') }
])

const breakdownNameLabel = computed(() =>
  activeBreakdown.value === 'models'
    ? t('admin.accounts.stats.model')
    : t('admin.accounts.stats.endpoint')
)

const mapModelRow = (item: ModelStat) => ({
  name: item.model || t('admin.accounts.stats.unknown'),
  requests: item.requests,
  tokens: item.total_tokens,
  cost: item.account_cost
})

const mapEndpointRow = (item: EndpointStat) => ({
  name: item.endpoint || t('admin.accounts.stats.unknown'),
  requests: item.requests,
  tokens: item.total_tokens,
  cost: item.actual_cost
})

const breakdownRows = computed(() => {
  if (!stats.value) return []
  if (activeBreakdown.value === 'models') {
    return stats.value.models.map(mapModelRow).sort((a, b) => b.cost - a.cost)
  }
  const source =
    activeBreakdown.value === 'inbound' ? stats.value.endpoints : stats.value.upstream_endpoints
  return source.map(mapEndpointRow).sort((a, b) => b.cost - a.cost)
})

const handleClose = (): void => {
  emit('close')
}
</script>
