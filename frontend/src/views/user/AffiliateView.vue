<template>
  <AppLayout>
    <div class="space-y-6">
      <div v-if="loading" class="flex justify-center py-12">
        <div
          class="h-8 w-8 animate-spin rounded-full border-2 border-primary-500 border-t-transparent"
        ></div>
      </div>

      <template v-else-if="detail">
        <div class="grid gap-4 sm:grid-cols-2 xl:grid-cols-5">
          <div class="card p-5">
            <p class="flex items-center gap-1.5 text-sm text-gray-500 dark:text-dark-400">
              <Icon name="dollar" size="sm" class="text-primary-500" />
              {{ t('affiliate.stats.rebateRate') }}
            </p>
            <p class="mt-2 text-2xl font-semibold text-primary-600 dark:text-primary-400">
              {{ formattedRebateRate }}<span class="ml-0.5 text-base font-medium">%</span>
            </p>
            <p class="mt-1 text-xs text-gray-400 dark:text-dark-500">
              {{ t('affiliate.stats.rebateRateHint') }}
            </p>
          </div>
          <div class="card p-5">
            <p class="text-sm text-gray-500 dark:text-dark-400">{{ t('affiliate.stats.invitedUsers') }}</p>
            <p class="mt-2 text-2xl font-semibold text-gray-900 dark:text-white">
              {{ formatCount(detail.aff_count) }}
            </p>
          </div>
          <div class="card p-5">
            <p class="text-sm text-gray-500 dark:text-dark-400">{{ t('affiliate.stats.settlementMode') }}</p>
            <p class="mt-2 text-2xl font-semibold text-emerald-600 dark:text-emerald-400">
              {{ t('affiliate.stats.realtimeBalance') }}
            </p>
            <p class="mt-1 text-xs text-gray-400 dark:text-dark-500">
              {{ t('affiliate.stats.realtimeBalanceHint') }}
            </p>
          </div>
          <div class="card p-5">
            <div class="flex items-start justify-between gap-3">
              <p class="text-sm text-gray-500 dark:text-dark-400">{{ periodIncomeTitle }}</p>
              <div class="flex shrink-0 items-center gap-1">
                <button
                  v-for="preset in periodPresets"
                  :key="preset"
                  type="button"
                  class="rounded-md px-2 py-1 text-xs font-medium transition-colors"
                  :class="periodPreset === preset ? 'bg-primary-100 text-primary-700 dark:bg-primary-900/40 dark:text-primary-300' : 'text-gray-500 hover:bg-gray-100 dark:text-dark-400 dark:hover:bg-dark-800'"
                  @click="setPeriodPreset(preset)"
                >
                  {{ t(`affiliate.period.presets.${preset}`) }}
                </button>
              </div>
            </div>
            <p class="mt-2 text-2xl font-semibold text-gray-900 dark:text-white">
              {{ formatCurrency(detail.period_rebate) }}
            </p>
            <div class="mt-3 grid grid-cols-2 gap-2">
              <input
                v-model="periodStartDate"
                type="date"
                class="input h-9 text-xs"
                :aria-label="t('affiliate.period.start')"
                @change="setCustomPeriod"
              />
              <input
                v-model="periodEndDate"
                type="date"
                class="input h-9 text-xs"
                :aria-label="t('affiliate.period.end')"
                @change="setCustomPeriod"
              />
            </div>
          </div>
          <div class="card p-5">
            <p class="text-sm text-gray-500 dark:text-dark-400">{{ t('affiliate.stats.totalQuota') }}</p>
            <p class="mt-2 text-2xl font-semibold text-gray-900 dark:text-white">
              {{ formatCurrency(detail.aff_history_quota) }}
            </p>
          </div>
        </div>

        <div class="card p-6">
          <h3 class="text-base font-semibold text-gray-900 dark:text-white">{{ t('affiliate.title') }}</h3>
          <p class="mt-1 text-sm text-gray-500 dark:text-dark-400">{{ t('affiliate.description') }}</p>

          <div class="mt-5 grid gap-4 md:grid-cols-2">
            <div class="space-y-2">
              <p class="text-sm font-medium text-gray-700 dark:text-gray-300">{{ t('affiliate.yourCode') }}</p>
              <div class="flex items-center gap-2 rounded-xl border border-gray-200 bg-gray-50 px-3 py-2 dark:border-dark-700 dark:bg-dark-900">
                <code class="flex-1 truncate text-sm font-semibold text-gray-900 dark:text-white">{{ detail.aff_code }}</code>
                <button class="btn btn-secondary btn-sm" @click="copyCode">
                  <Icon name="copy" size="sm" />
                  <span>{{ t('affiliate.copyCode') }}</span>
                </button>
              </div>
            </div>

            <div class="space-y-2">
              <p class="text-sm font-medium text-gray-700 dark:text-gray-300">{{ t('affiliate.inviteLink') }}</p>
              <div class="flex items-center gap-2 rounded-xl border border-gray-200 bg-gray-50 px-3 py-2 dark:border-dark-700 dark:bg-dark-900">
                <code class="flex-1 truncate text-sm text-gray-700 dark:text-gray-300">{{ inviteLink }}</code>
                <button class="btn btn-secondary btn-sm" @click="copyInviteLink">
                  <Icon name="copy" size="sm" />
                  <span>{{ t('affiliate.copyLink') }}</span>
                </button>
              </div>
            </div>
          </div>

          <div class="mt-4 grid gap-3 md:grid-cols-2">
            <div class="rounded-lg border border-gray-200 px-3 py-2 dark:border-dark-700">
              <p class="text-xs text-gray-500 dark:text-dark-400">{{ t('affiliate.weeklyQuota') }}</p>
              <p class="mt-1 text-sm font-medium text-gray-900 dark:text-white">{{ weeklyQuotaText }}</p>
            </div>
            <div class="rounded-lg border border-gray-200 px-3 py-2 dark:border-dark-700">
              <p class="text-xs text-gray-500 dark:text-dark-400">{{ t('affiliate.codePolicy.title') }}</p>
              <p class="mt-1 text-sm font-medium text-gray-900 dark:text-white">{{ codePolicyText }}</p>
              <p class="mt-0.5 text-xs text-gray-400 dark:text-dark-500">{{ codeExpiryText }}</p>
            </div>
          </div>

          <div class="mt-5 rounded-xl border border-primary-200 bg-primary-50 p-4 dark:border-primary-900/40 dark:bg-primary-900/20">
            <p class="text-sm font-medium text-primary-800 dark:text-primary-200">{{ t('affiliate.tips.title') }}</p>
            <ul class="mt-2 space-y-1 text-sm text-primary-700 dark:text-primary-300">
              <li>1. {{ t('affiliate.tips.line1') }}</li>
              <li>2. {{ t('affiliate.tips.line2', { rate: `${formattedRebateRate}%` }) }}</li>
              <li>3. {{ t('affiliate.tips.line3') }}</li>
            </ul>
          </div>
        </div>

        <div class="card p-6">
          <h3 class="text-base font-semibold text-gray-900 dark:text-white">{{ t('affiliate.invitees.title') }}</h3>
          <div v-if="detail.invitees.length === 0" class="mt-4 rounded-xl border border-dashed border-gray-300 p-6 text-center text-sm text-gray-500 dark:border-dark-700 dark:text-dark-400">
            {{ t('affiliate.invitees.empty') }}
          </div>
          <div v-else class="mt-4 overflow-x-auto">
            <table class="w-full min-w-[920px] text-left text-sm">
              <thead>
                <tr class="border-b border-gray-200 text-gray-500 dark:border-dark-700 dark:text-dark-400">
                  <th class="px-3 py-2 font-medium">{{ t('affiliate.invitees.columns.user') }}</th>
                  <th class="px-3 py-2 font-medium">{{ t('affiliate.invitees.columns.bindSource') }}</th>
                  <th class="px-3 py-2 font-medium">
                    <button type="button" class="inline-flex items-center gap-1 hover:text-gray-900 dark:hover:text-white" @click="toggleSort('bound_at')">
                      {{ t('affiliate.invitees.columns.joinedAt') }}
                      <span>{{ sortIndicator('bound_at') }}</span>
                    </button>
                  </th>
                  <th class="px-3 py-2 font-medium">{{ t('affiliate.invitees.columns.status') }}</th>
                  <th class="px-3 py-2 text-right font-medium">
                    <button type="button" class="inline-flex items-center gap-1 hover:text-gray-900 dark:hover:text-white" @click="toggleSort('period_consumption')">
                      {{ t('affiliate.invitees.columns.periodConsumption') }}
                      <span>{{ sortIndicator('period_consumption') }}</span>
                    </button>
                  </th>
                  <th class="px-3 py-2 text-right font-medium">
                    <button type="button" class="inline-flex items-center gap-1 hover:text-gray-900 dark:hover:text-white" @click="toggleSort('period_rebate')">
                      {{ t('affiliate.invitees.columns.periodRebate') }}
                      <span>{{ sortIndicator('period_rebate') }}</span>
                    </button>
                  </th>
                  <th class="px-3 py-2 text-right font-medium">
                    <button type="button" class="inline-flex items-center gap-1 hover:text-gray-900 dark:hover:text-white" @click="toggleSort('history_consumption')">
                      {{ t('affiliate.invitees.columns.historyConsumption') }}
                      <span>{{ sortIndicator('history_consumption') }}</span>
                    </button>
                  </th>
                  <th class="px-3 py-2 text-right font-medium">
                    <button type="button" class="inline-flex items-center gap-1 hover:text-gray-900 dark:hover:text-white" @click="toggleSort('total_rebate')">
                      {{ t('affiliate.invitees.columns.rebate') }}
                      <span>{{ sortIndicator('total_rebate') }}</span>
                    </button>
                  </th>
                </tr>
              </thead>
              <tbody>
                <tr
                  v-for="item in sortedInvitees"
                  :key="item.user_id"
                  class="border-b border-gray-100 last:border-b-0 dark:border-dark-800"
                >
                  <td class="px-3 py-3">
                    <div class="font-medium text-gray-900 dark:text-white">{{ item.email || '-' }}</div>
                    <div class="mt-0.5 text-xs text-gray-500 dark:text-dark-400">{{ item.username || '-' }}</div>
                  </td>
                  <td class="px-3 py-3 text-gray-700 dark:text-gray-300">{{ formatBindSource(item.invite_bind_source) }}</td>
                  <td class="px-3 py-3 text-gray-700 dark:text-gray-300">{{ formatDateTime(item.created_at) || '-' }}</td>
                  <td class="px-3 py-3 text-gray-700 dark:text-gray-300">{{ formatInviteeStatus(item.status) }}</td>
                  <td class="px-3 py-3 text-right text-gray-700 dark:text-gray-300">{{ formatCurrency(item.period_consumption) }}</td>
                  <td class="px-3 py-3 text-right font-medium text-emerald-600 dark:text-emerald-400">{{ formatCurrency(item.period_rebate) }}</td>
                  <td class="px-3 py-3 text-right text-gray-700 dark:text-gray-300">{{ formatCurrency(item.history_consumption) }}</td>
                  <td class="px-3 py-3 text-right font-medium text-gray-900 dark:text-white">{{ formatCurrency(item.total_rebate) }}</td>
                </tr>
              </tbody>
            </table>
          </div>
        </div>
      </template>
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import AppLayout from '@/components/layout/AppLayout.vue'
import Icon from '@/components/icons/Icon.vue'
import userAPI from '@/api/user'
import type { AffiliateInvitee, UserAffiliateDetail } from '@/types'
import { useAppStore } from '@/stores/app'
import { useClipboard } from '@/composables/useClipboard'
import { formatCurrency, formatDateTime } from '@/utils/format'
import { extractApiErrorMessage } from '@/utils/apiError'

const { t } = useI18n()
const appStore = useAppStore()
const { copyToClipboard } = useClipboard()

const loading = ref(true)
const detail = ref<UserAffiliateDetail | null>(null)
const periodPresets = ['today', 'yesterday', 'last7'] as const
type PeriodPreset = typeof periodPresets[number] | 'custom'
type SortKey = 'bound_at' | 'period_consumption' | 'period_rebate' | 'history_consumption' | 'total_rebate'

const periodPreset = ref<PeriodPreset>('today')
const periodStartDate = ref(toDateInputValue(startOfLocalDay(new Date())))
const periodEndDate = ref(toDateInputValue(startOfLocalDay(new Date())))
const sortKey = ref<SortKey>('bound_at')
const sortDirection = ref<'asc' | 'desc'>('desc')

const inviteLink = computed(() => {
  if (!detail.value) return ''
  if (typeof window === 'undefined') return `/register?aff=${encodeURIComponent(detail.value.aff_code)}`
  return `${window.location.origin}/register?aff=${encodeURIComponent(detail.value.aff_code)}`
})

// Rebate rate is a percentage in the range [0, 100]; backend already clamps it.
// We trim trailing zeros (e.g. 20.00 → "20", 12.50 → "12.5") for a cleaner UI.
const formattedRebateRate = computed(() => {
  const v = detail.value?.effective_rebate_rate_percent ?? 0
  const rounded = Math.round(v * 100) / 100
  return Number.isInteger(rounded) ? String(rounded) : rounded.toString()
})

const periodIncomeTitle = computed(() => {
  if (periodPreset.value === 'today') return t('affiliate.stats.todayQuota')
  if (periodPreset.value === 'yesterday') return t('affiliate.stats.yesterdayQuota')
  if (periodPreset.value === 'last7') return t('affiliate.stats.last7Quota')
  return t('affiliate.stats.periodQuota')
})

const weeklyQuotaText = computed(() => {
  const used = detail.value?.aff_weekly_used ?? 0
  const limit = detail.value?.aff_weekly_limit ?? 0
  const remaining = detail.value?.aff_weekly_remaining ?? Math.max(0, limit - used)
  return t('affiliate.weeklyQuotaText', { used, limit, remaining })
})

const codePolicyText = computed(() => {
  if (!detail.value) return ''
  return detail.value.aff_code_auto_rotate
    ? t('affiliate.codePolicy.rotateWeekly')
    : t('affiliate.codePolicy.keepCode')
})

const codeExpiryText = computed(() => {
  if (!detail.value) return ''
  if (!detail.value.aff_code_auto_rotate) {
    return t('affiliate.codePolicy.noExpiry')
  }
  const expiresAt = formatDateTime(detail.value.aff_code_expires_at)
  return expiresAt
    ? t('affiliate.codePolicy.expiresAt', { time: expiresAt })
    : t('affiliate.codePolicy.expiresUnknown')
})

const sortedInvitees = computed(() => {
  const rows = [...(detail.value?.invitees ?? [])]
  const direction = sortDirection.value === 'asc' ? 1 : -1
  return rows.sort((a, b) => {
    const left = sortableValue(a, sortKey.value)
    const right = sortableValue(b, sortKey.value)
    if (left === right) return 0
    return left > right ? direction : -direction
  })
})

function formatCount(value: number): string {
  return value.toLocaleString()
}

async function loadAffiliateDetail(silent = false): Promise<void> {
  if (!silent) {
    loading.value = true
  }
  try {
    detail.value = await userAPI.getAffiliateDetail(buildPeriodParams())
  } catch (error) {
    appStore.showError(extractApiErrorMessage(error, t('affiliate.loadFailed')))
  } finally {
    if (!silent) {
      loading.value = false
    }
  }
}

function buildPeriodParams(): { period_start_at?: string; period_end_at?: string } {
  const start = parseDateInputStart(periodStartDate.value)
  const end = parseDateInputStart(periodEndDate.value)
  return {
    period_start_at: start?.toISOString(),
    period_end_at: end ? addDays(end, 1).toISOString() : undefined
  }
}

function setPeriodPreset(preset: typeof periodPresets[number]): void {
  periodPreset.value = preset
  const today = startOfLocalDay(new Date())
  if (preset === 'today') {
    periodStartDate.value = toDateInputValue(today)
    periodEndDate.value = toDateInputValue(today)
  } else if (preset === 'yesterday') {
    periodStartDate.value = toDateInputValue(addDays(today, -1))
    periodEndDate.value = toDateInputValue(addDays(today, -1))
  } else {
    periodStartDate.value = toDateInputValue(addDays(today, -6))
    periodEndDate.value = toDateInputValue(today)
  }
  void loadAffiliateDetail(true)
}

function setCustomPeriod(): void {
  periodPreset.value = 'custom'
  const start = parseDateInputStart(periodStartDate.value)
  const end = parseDateInputStart(periodEndDate.value)
  if (!start || !end || start > end) {
    appStore.showError(t('affiliate.period.invalid'))
    return
  }
  void loadAffiliateDetail(true)
}

function toggleSort(key: SortKey): void {
  if (sortKey.value === key) {
    sortDirection.value = sortDirection.value === 'asc' ? 'desc' : 'asc'
    return
  }
  sortKey.value = key
  sortDirection.value = 'desc'
}

function sortIndicator(key: SortKey): string {
  if (sortKey.value !== key) return '↕'
  return sortDirection.value === 'asc' ? '↑' : '↓'
}

function sortableValue(item: AffiliateInvitee, key: SortKey): number {
  if (key === 'bound_at') {
    const time = item.created_at ? new Date(item.created_at).getTime() : 0
    return Number.isFinite(time) ? time : 0
  }
  return item[key] ?? 0
}

function formatBindSource(source?: string): string {
  if (source === 'registration') return t('affiliate.invitees.bindSources.registration')
  if (source === 'admin') return t('affiliate.invitees.bindSources.admin')
  return t('affiliate.invitees.bindSources.legacy')
}

function formatInviteeStatus(status: string): string {
  if (status === 'active') return t('affiliate.invitees.status.active')
  if (status === 'disabled') return t('affiliate.invitees.status.disabled')
  return status || '-'
}

function startOfLocalDay(date: Date): Date {
  return new Date(date.getFullYear(), date.getMonth(), date.getDate())
}

function addDays(date: Date, days: number): Date {
  const next = new Date(date)
  next.setDate(next.getDate() + days)
  return next
}

function toDateInputValue(date: Date): string {
  const year = date.getFullYear()
  const month = String(date.getMonth() + 1).padStart(2, '0')
  const day = String(date.getDate()).padStart(2, '0')
  return `${year}-${month}-${day}`
}

function parseDateInputStart(value: string): Date | null {
  if (!value) return null
  const [year, month, day] = value.split('-').map(Number)
  if (!year || !month || !day) return null
  const date = new Date(year, month - 1, day)
  return Number.isNaN(date.getTime()) ? null : date
}

async function copyCode(): Promise<void> {
  if (!detail.value?.aff_code) return
  await copyToClipboard(detail.value.aff_code, t('affiliate.codeCopied'))
}

async function copyInviteLink(): Promise<void> {
  if (!inviteLink.value) return
  await copyToClipboard(inviteLink.value, t('affiliate.linkCopied'))
}

onMounted(() => {
  void loadAffiliateDetail()
})
</script>
