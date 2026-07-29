<template>
  <section
    id="redeem-code"
    class="redeem-center-section scroll-mt-24"
    aria-labelledby="redeem-center-title"
  >
    <header class="redeem-section-header">
      <div class="mb-1 flex items-center gap-2 text-sm font-semibold text-primary-600 dark:text-primary-400">
        <span
          class="flex h-7 w-7 items-center justify-center rounded-lg bg-primary-100 dark:bg-primary-900/40"
        >
          <Icon name="gift" size="sm" />
        </span>
        {{ t('redeem.redeemCodeLabel') }}
      </div>
      <h2 id="redeem-center-title" class="text-xl font-bold text-gray-950 dark:text-white sm:text-2xl">
        {{ t('redeem.title') }}
      </h2>
      <p class="mt-1 text-[0.9375rem] leading-6 text-gray-500 dark:text-gray-400">
        {{ t('redeem.description') }}
      </p>
    </header>

    <div class="redeem-surface">
      <form
        class="redeem-entry"
        :aria-busy="submitting"
        @submit.prevent="handleRedeem"
      >
        <label for="redeem-center-code" class="sr-only">
          {{ t('redeem.redeemCodeLabel') }}
        </label>

        <div class="redeem-control-row">
          <div class="redeem-input-shell">
            <span class="redeem-key-icon" aria-hidden="true">
              <Icon name="key" size="md" />
            </span>
            <input
              id="redeem-center-code"
              v-model="redeemCode"
              type="text"
              required
              autocomplete="off"
              autocapitalize="off"
              spellcheck="false"
              :placeholder="t('redeem.redeemCodePlaceholder')"
              :disabled="submitting"
              class="redeem-input"
            />
          </div>

          <button
            type="submit"
            :disabled="!redeemCode.trim() || submitting"
            class="redeem-submit"
          >
            <LoadingSpinner v-if="submitting" size="sm" color="white" />
            <span>{{ submitting ? t('redeem.redeeming') : t('redeem.redeemButton') }}</span>
            <Icon v-if="!submitting" name="arrowRight" size="sm" />
          </button>
        </div>

        <Transition name="redeem-status">
          <div
            v-if="redeemResult"
            class="redeem-feedback redeem-feedback-success"
            role="status"
            aria-live="polite"
          >
            <Icon name="checkCircle" size="md" class="mt-0.5 shrink-0" />
            <div class="min-w-0">
              <p class="font-semibold">{{ t('redeem.redeemSuccess') }}</p>
              <p class="mt-1">
                {{ redeemResult.message || t('redeem.codeRedeemSuccess') }}
              </p>
              <div class="mt-2 flex flex-wrap gap-x-5 gap-y-1 font-medium">
                <span v-if="redeemResult.type === 'balance'">
                  {{ t('redeem.added') }}: ${{ redeemResult.value.toFixed(2) }}
                </span>
                <span v-else-if="redeemResult.type === 'concurrency'">
                  {{ t('redeem.added') }}: {{ redeemResult.value }}
                  {{ t('redeem.concurrentRequests') }}
                </span>
                <span v-else-if="redeemResult.type === 'points'">
                  {{ t('redeem.added') }}: {{ formatPoints(redeemResult.value) }}
                  {{ t('redeem.points') }}
                </span>
                <span v-else-if="redeemResult.type === 'subscription'">
                  {{ t('redeem.subscriptionAssigned') }}
                  <template v-if="redeemResult.group_name">
                    · {{ redeemResult.group_name }}
                  </template>
                  <template v-if="redeemResult.validity_days">
                    · {{ t('redeem.subscriptionDays', { days: redeemResult.validity_days }) }}
                  </template>
                </span>
                <span v-if="redeemResult.new_balance !== undefined">
                  {{ t('redeem.newBalance') }}: ${{ redeemResult.new_balance.toFixed(2) }}
                </span>
                <span v-if="redeemResult.new_concurrency !== undefined">
                  {{ t('redeem.newConcurrency') }}: {{ redeemResult.new_concurrency }}
                  {{ t('redeem.requests') }}
                </span>
                <span v-if="redeemResult.type === 'points' && user?.points_balance !== undefined">
                  {{ t('redeem.newPoints') }}: {{ formatPoints(user.points_balance) }}
                </span>
              </div>
            </div>
          </div>

          <div
            v-else-if="errorMessage"
            class="redeem-feedback redeem-feedback-error"
            role="alert"
            aria-live="assertive"
          >
            <Icon name="exclamationCircle" size="md" class="mt-0.5 shrink-0" />
            <div class="min-w-0">
              <p class="font-semibold">{{ t('redeem.redeemFailed') }}</p>
              <p class="mt-1">{{ errorMessage }}</p>
            </div>
          </div>
        </Transition>
      </form>

      <div class="redeem-activity">
        <div class="flex items-center gap-3">
          <span class="redeem-activity-icon">
            <Icon name="clock" size="md" />
          </span>
          <h3 class="text-base font-semibold text-gray-900 dark:text-white">
            {{ t('redeem.recentActivity') }}
          </h3>
        </div>

        <div v-if="loadingHistory" class="flex min-h-24 items-center justify-center">
          <LoadingSpinner size="md" />
        </div>

        <ul v-else-if="history.length > 0" class="redeem-history-list">
          <li v-for="item in paginatedHistory" :key="item.id" class="redeem-history-row">
            <div class="flex min-w-0 items-center gap-3">
              <span
                :class="[
                  'redeem-history-icon',
                  isBalanceType(item.type)
                    ? item.value >= 0
                      ? 'bg-emerald-50 text-emerald-600 dark:bg-emerald-500/10 dark:text-emerald-300'
                      : 'bg-red-50 text-red-600 dark:bg-red-500/10 dark:text-red-300'
                    : isSubscriptionType(item.type)
                      ? 'bg-violet-50 text-violet-600 dark:bg-violet-500/10 dark:text-violet-300'
                      : isPointsType(item.type)
                        ? item.value >= 0
                          ? 'bg-cyan-50 text-cyan-600 dark:bg-cyan-500/10 dark:text-cyan-300'
                          : 'bg-red-50 text-red-600 dark:bg-red-500/10 dark:text-red-300'
                        : item.value >= 0
                          ? 'bg-blue-50 text-blue-600 dark:bg-blue-500/10 dark:text-blue-300'
                          : 'bg-orange-50 text-orange-600 dark:bg-orange-500/10 dark:text-orange-300'
                ]"
              >
                <Icon v-if="isBalanceType(item.type)" name="dollar" size="md" />
                <Icon v-else-if="isSubscriptionType(item.type)" name="badge" size="md" />
                <Icon v-else-if="isPointsType(item.type)" name="gift" size="md" />
                <Icon v-else name="bolt" size="md" />
              </span>

              <div class="min-w-0">
                <p class="truncate text-sm font-semibold text-gray-900 dark:text-white">
                  {{ getHistoryItemTitle(item) }}
                </p>
                <div class="mt-1 flex flex-wrap gap-x-2 gap-y-1 text-sm text-gray-500 dark:text-gray-400">
                  <span>{{ formatDateTime(item.used_at) }}</span>
                  <span v-if="!isAdminAdjustment(item.type)" class="font-mono">
                    {{ item.code.slice(0, 8) }}...
                  </span>
                  <span v-else>{{ t('redeem.adminAdjustment') }}</span>
                </div>
                <p
                  v-if="item.notes"
                  class="mt-1 truncate text-sm italic text-gray-500 dark:text-gray-400"
                  :title="item.notes"
                >
                  {{ item.notes }}
                </p>
              </div>
            </div>

            <p
              :class="[
                'redeem-history-value',
                isBalanceType(item.type)
                  ? item.value >= 0
                    ? 'text-emerald-600 dark:text-emerald-300'
                    : 'text-red-600 dark:text-red-300'
                  : isSubscriptionType(item.type)
                    ? 'text-violet-600 dark:text-violet-300'
                    : isPointsType(item.type)
                      ? item.value >= 0
                        ? 'text-cyan-600 dark:text-cyan-300'
                        : 'text-red-600 dark:text-red-300'
                      : item.value >= 0
                        ? 'text-blue-600 dark:text-blue-300'
                        : 'text-orange-600 dark:text-orange-300'
              ]"
            >
              {{ formatHistoryValue(item) }}
            </p>
          </li>
        </ul>

        <div v-else class="flex min-h-24 items-center gap-3 text-sm text-gray-500 dark:text-gray-400">
          <p>{{ t('redeem.historyWillAppear') }}</p>
        </div>

        <Pagination
          v-if="!loadingHistory && history.length > HISTORY_PAGE_SIZE"
          class="redeem-history-pagination"
          :page="historyPage"
          :total="history.length"
          :page-size="HISTORY_PAGE_SIZE"
          :show-page-size-selector="false"
          compact
          @update:page="historyPage = $event"
        />
      </div>
    </div>
  </section>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { redeemAPI, type RedeemHistoryItem } from '@/api'
import LoadingSpinner from '@/components/common/LoadingSpinner.vue'
import Pagination from '@/components/common/Pagination.vue'
import Icon from '@/components/icons/Icon.vue'
import { useAppStore } from '@/stores/app'
import { useAuthStore } from '@/stores/auth'
import { useSubscriptionStore } from '@/stores/subscriptions'
import { formatDateTime } from '@/utils/format'

const { t } = useI18n()
const authStore = useAuthStore()
const appStore = useAppStore()
const subscriptionStore = useSubscriptionStore()

const user = computed(() => authStore.user)
const redeemCode = ref('')
const submitting = ref(false)
const redeemResult = ref<{
  message?: string
  type: string
  value: number
  new_balance?: number
  new_concurrency?: number
  group_name?: string
  validity_days?: number
} | null>(null)
const errorMessage = ref('')
const history = ref<RedeemHistoryItem[]>([])
const loadingHistory = ref(false)
const historyPage = ref(1)

const HISTORY_PAGE_SIZE = 10
const paginatedHistory = computed(() => {
  const start = (historyPage.value - 1) * HISTORY_PAGE_SIZE
  return history.value.slice(start, start + HISTORY_PAGE_SIZE)
})

const isBalanceType = (type: string) => type === 'balance' || type === 'admin_balance'
const isSubscriptionType = (type: string) => type === 'subscription'
const isPointsType = (type: string) => type === 'points' || type === 'admin_points'
const isAdminAdjustment = (type: string) =>
  type === 'admin_balance' || type === 'admin_concurrency' || type === 'admin_points'

const getHistoryItemTitle = (item: RedeemHistoryItem) => {
  if (item.type === 'balance') {
    return t('redeem.balanceAddedRedeem')
  }
  if (item.type === 'admin_balance') {
    return item.value >= 0 ? t('redeem.balanceAddedAdmin') : t('redeem.balanceDeductedAdmin')
  }
  if (item.type === 'concurrency') {
    return t('redeem.concurrencyAddedRedeem')
  }
  if (item.type === 'admin_concurrency') {
    return item.value >= 0
      ? t('redeem.concurrencyAddedAdmin')
      : t('redeem.concurrencyReducedAdmin')
  }
  if (item.type === 'points') {
    return t('redeem.pointsAddedRedeem')
  }
  if (item.type === 'admin_points') {
    return item.value >= 0 ? t('redeem.pointsAddedAdmin') : t('redeem.pointsDeductedAdmin')
  }
  if (item.type === 'subscription') {
    return t('redeem.subscriptionAssigned')
  }
  return t('common.unknown')
}

const formatHistoryValue = (item: RedeemHistoryItem) => {
  if (isBalanceType(item.type)) {
    const sign = item.value >= 0 ? '+' : ''
    return `${sign}$${item.value.toFixed(2)}`
  }
  if (isSubscriptionType(item.type)) {
    const days = item.validity_days || Math.round(item.value)
    const groupName = item.group?.name || ''
    return groupName ? `${days}${t('redeem.days')} - ${groupName}` : `${days}${t('redeem.days')}`
  }
  if (isPointsType(item.type)) {
    const sign = item.value >= 0 ? '+' : ''
    return `${sign}${formatPoints(item.value)} ${t('redeem.points')}`
  }

  const sign = item.value >= 0 ? '+' : ''
  return `${sign}${item.value} ${t('redeem.requests')}`
}

function formatPoints(value: number): string {
  return Number(value || 0).toFixed(10).replace(/\.?0+$/, '') || '0'
}

async function fetchHistory() {
  loadingHistory.value = true
  try {
    history.value = await redeemAPI.getHistory()
    historyPage.value = 1
  } catch (error) {
    console.error('Failed to fetch history:', error)
  } finally {
    loadingHistory.value = false
  }
}

async function handleRedeem() {
  if (!redeemCode.value.trim()) {
    appStore.showError(t('redeem.pleaseEnterCode'))
    return
  }

  submitting.value = true
  errorMessage.value = ''
  redeemResult.value = null

  try {
    const result = await redeemAPI.redeem(redeemCode.value.trim())
    redeemResult.value = result
    await authStore.refreshUser()

    if (result.type === 'subscription') {
      try {
        await subscriptionStore.fetchActiveSubscriptions(true)
      } catch (error) {
        console.error('Failed to refresh subscriptions after redeem:', error)
        appStore.showWarning(t('redeem.subscriptionRefreshFailed'))
      }
    }

    redeemCode.value = ''
    await fetchHistory()
    appStore.showSuccess(t('redeem.codeRedeemSuccess'))
  } catch (error: any) {
    errorMessage.value = error.response?.data?.detail || t('redeem.failedToRedeem')
    appStore.showError(t('redeem.redeemFailed'))
  } finally {
    submitting.value = false
  }
}

onMounted(() => {
  void fetchHistory()
})
</script>

<style scoped>
.redeem-center-section {
  display: flex;
  min-height: 100%;
  flex-direction: column;
}

.redeem-section-header {
  margin-bottom: 1rem;
}

.redeem-surface {
  display: grid;
  grid-template-rows: auto minmax(0, 1fr);
  flex: 1;
  overflow: hidden;
  @apply rounded-2xl border border-gray-200 bg-white shadow-card dark:border-dark-700 dark:bg-dark-900;
}

.redeem-entry {
  display: flex;
  flex-direction: column;
  justify-content: center;
  min-width: 0;
  padding: 1.25rem;
  @apply bg-gradient-to-br from-primary-50/80 via-white to-white dark:from-primary-950/35 dark:via-dark-900 dark:to-dark-900;
}

.redeem-control-row {
  display: flex;
  flex-direction: column;
  gap: 0.75rem;
}

.redeem-input-shell {
  @apply flex min-h-14 min-w-0 flex-1 items-center gap-3 rounded-xl border border-gray-200 bg-white px-3 shadow-sm transition-[border-color,box-shadow] duration-200;
  @apply dark:border-dark-700 dark:bg-dark-800;
}

.redeem-input-shell:focus-within {
  @apply border-primary-400 ring-4 ring-primary-500/10;
}

.redeem-key-icon {
  @apply pointer-events-none inline-flex h-9 w-9 shrink-0 items-center justify-center rounded-lg bg-primary-50 text-primary-500;
  @apply dark:bg-primary-500/10 dark:text-primary-300;
}

.redeem-input {
  @apply min-w-0 flex-1 border-0 bg-transparent py-3 font-mono text-base font-medium tracking-[0.04em] text-gray-900 outline-none;
  @apply placeholder:font-sans placeholder:font-normal placeholder:tracking-normal placeholder:text-gray-400;
  @apply disabled:cursor-not-allowed disabled:opacity-60 dark:text-white;
}

.redeem-submit {
  @apply inline-flex min-h-14 w-full cursor-pointer items-center justify-center gap-2 rounded-xl bg-primary-600 px-7 text-base font-semibold text-white shadow-sm;
  @apply transition-[background-color,box-shadow] duration-200 hover:bg-primary-700 hover:shadow-md;
  @apply focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary-500 focus-visible:ring-offset-2;
  @apply disabled:cursor-not-allowed disabled:bg-primary-300 disabled:shadow-none;
}

.redeem-feedback {
  @apply mt-4 flex items-start gap-3 rounded-xl border p-4 text-sm leading-6;
}

.redeem-feedback-success {
  @apply border-emerald-200 bg-emerald-50 text-emerald-800 dark:border-emerald-500/30 dark:bg-emerald-500/10 dark:text-emerald-200;
}

.redeem-feedback-error {
  @apply border-red-200 bg-red-50 text-red-800 dark:border-red-500/30 dark:bg-red-500/10 dark:text-red-200;
}

.redeem-activity {
  display: flex;
  flex-direction: column;
  min-width: 0;
  padding: 1.25rem;
  @apply border-t border-gray-100 bg-gray-50/55 dark:border-dark-700 dark:bg-dark-800/35;
}

.redeem-activity-icon {
  @apply inline-flex h-10 w-10 shrink-0 items-center justify-center rounded-xl bg-white text-primary-600 shadow-sm ring-1 ring-gray-200;
  @apply dark:bg-dark-800 dark:text-primary-300 dark:ring-dark-700;
}

.redeem-history-list {
  flex: 1;
  margin-top: 0.875rem;
}

.redeem-history-row {
  @apply grid min-w-0 grid-cols-1 gap-2 border-t border-gray-200/80 py-3 first:border-t-0 first:pt-0 last:pb-0;
  @apply dark:border-dark-700 sm:grid-cols-[minmax(0,1fr)_auto] sm:items-center;
}

.redeem-history-icon {
  @apply inline-flex h-9 w-9 shrink-0 items-center justify-center rounded-lg;
}

.redeem-history-value {
  @apply pl-12 text-sm font-semibold tabular-nums sm:pl-3 sm:text-right;
}

.redeem-history-pagination {
  @apply mt-4 border-t border-gray-200/80 pt-4 dark:border-dark-700;
}

.redeem-status-enter-active,
.redeem-status-leave-active {
  transition:
    opacity 200ms ease,
    transform 200ms ease;
}

.redeem-status-enter-from,
.redeem-status-leave-to {
  opacity: 0;
  transform: translateY(-0.5rem);
}

@media (min-width: 640px) {
  .redeem-entry,
  .redeem-activity {
    padding: 1.5rem;
  }

  .redeem-control-row {
    flex-direction: row;
  }

  .redeem-submit {
    width: auto;
    min-width: 8.5rem;
  }
}

@container (min-width: 64rem) {
  .redeem-surface {
    grid-template-columns: minmax(0, 1.45fr) minmax(24rem, 0.75fr);
    grid-template-rows: auto;
  }

  .redeem-activity {
    border-top-width: 0;
    @apply border-l border-gray-100 dark:border-dark-700;
  }
}

@media (prefers-reduced-motion: reduce) {
  .redeem-input-shell,
  .redeem-submit,
  .redeem-status-enter-active,
  .redeem-status-leave-active {
    transition: none;
  }
}
</style>
