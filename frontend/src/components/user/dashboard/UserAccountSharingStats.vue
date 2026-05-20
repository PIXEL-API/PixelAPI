<template>
  <div class="card relative overflow-hidden p-4">
    <div v-if="loading" class="absolute inset-0 z-10 flex items-center justify-center bg-white/50 backdrop-blur-sm dark:bg-dark-800/50">
      <LoadingSpinner size="md" />
    </div>

    <div class="mb-4 flex flex-wrap items-start justify-between gap-3">
      <div>
        <h3 class="text-sm font-semibold text-gray-900 dark:text-white">{{ t('dashboard.accountSharingTitle') }}</h3>
        <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">
          {{ stats?.start_date || '-' }} - {{ stats?.end_date || '-' }}
        </p>
      </div>
      <div class="flex items-center gap-2 text-xs text-gray-500 dark:text-gray-400">
        <Icon name="shield" size="sm" />
        <span>{{ t('dashboard.accountSharingSettlement') }}</span>
      </div>
    </div>

    <div v-if="error" class="mb-4 rounded-md border border-red-200 bg-red-50 px-3 py-2 text-sm text-red-700 dark:border-red-900/60 dark:bg-red-950/30 dark:text-red-300">
      {{ error }}
    </div>

    <div v-else-if="summary" class="space-y-4">
      <div class="grid grid-cols-2 gap-4 lg:grid-cols-5">
        <div>
          <p class="text-xs font-medium text-gray-500 dark:text-gray-400">{{ t('dashboard.ownedAccounts') }}</p>
          <p class="mt-1 text-xl font-bold text-gray-900 dark:text-white">{{ formatNumber(summary.owned_accounts) }}</p>
          <p class="text-xs text-gray-500 dark:text-gray-400">
            {{ t('dashboard.publicApproved') }} {{ summary.public_approved_accounts }}
          </p>
        </div>
        <div>
          <p class="text-xs font-medium text-gray-500 dark:text-gray-400">{{ t('dashboard.selfAccountCost') }}</p>
          <p class="mt-1 text-xl font-bold text-gray-900 dark:text-white">${{ formatCost(summary.self_account_cost) }}</p>
          <p class="text-xs text-gray-500 dark:text-gray-400">
            {{ formatNumber(summary.self_requests) }} {{ t('dashboard.requests') }}
          </p>
        </div>
        <div>
          <p class="text-xs font-medium text-gray-500 dark:text-gray-400">{{ t('dashboard.externalConsumerCharge') }}</p>
          <p class="mt-1 text-xl font-bold text-blue-600 dark:text-blue-400">${{ formatCost(summary.external_consumer_charge) }}</p>
          <p class="text-xs text-gray-500 dark:text-gray-400">
            {{ formatNumber(summary.external_requests) }} {{ t('dashboard.requests') }}
          </p>
        </div>
        <div>
          <p class="text-xs font-medium text-gray-500 dark:text-gray-400">{{ t('dashboard.ownerCredit') }}</p>
          <p class="mt-1 text-xl font-bold text-emerald-600 dark:text-emerald-400">${{ formatCost(summary.external_owner_credit) }}</p>
          <p class="text-xs text-gray-500 dark:text-gray-400">
            {{ t('dashboard.platformFee') }} ${{ formatCost(summary.external_platform_fee) }}
          </p>
        </div>
        <div>
          <p class="text-xs font-medium text-gray-500 dark:text-gray-400">{{ t('dashboard.balanceNetChange') }}</p>
          <p class="mt-1 text-xl font-bold" :class="summary.balance_net_change >= 0 ? 'text-emerald-600 dark:text-emerald-400' : 'text-rose-600 dark:text-rose-400'">
            {{ summary.balance_net_change >= 0 ? '+' : '-' }}${{ formatCost(Math.abs(summary.balance_net_change)) }}
          </p>
          <p class="text-xs text-gray-500 dark:text-gray-400">
            {{ t('dashboard.selfActualCost') }} ${{ formatCost(summary.self_actual_cost) }}
          </p>
        </div>
      </div>

      <div class="grid grid-cols-2 gap-3 text-xs text-gray-600 dark:text-gray-300 lg:grid-cols-4">
        <div class="flex items-center justify-between border-t border-gray-100 pt-3 dark:border-gray-700">
          <span>{{ t('dashboard.privateMode') }}</span>
          <span class="font-semibold text-gray-900 dark:text-white">{{ summary.private_accounts }}</span>
        </div>
        <div class="flex items-center justify-between border-t border-gray-100 pt-3 dark:border-gray-700">
          <span>{{ t('dashboard.publicPending') }}</span>
          <span class="font-semibold text-amber-600 dark:text-amber-400">{{ summary.public_pending_accounts }}</span>
        </div>
        <div class="flex items-center justify-between border-t border-gray-100 pt-3 dark:border-gray-700">
          <span>{{ t('dashboard.publicApproved') }}</span>
          <span class="font-semibold text-emerald-600 dark:text-emerald-400">{{ summary.public_approved_accounts }}</span>
        </div>
        <div class="flex items-center justify-between border-t border-gray-100 pt-3 dark:border-gray-700">
          <span>{{ t('dashboard.publicSuspended') }}</span>
          <span class="font-semibold text-rose-600 dark:text-rose-400">{{ summary.public_suspended_accounts }}</span>
        </div>
      </div>

      <div>
        <div class="mb-2 flex items-center justify-between">
          <h4 class="text-xs font-semibold uppercase tracking-wide text-gray-500 dark:text-gray-400">
            {{ t('dashboard.ownedAccountBreakdown') }}
          </h4>
          <span class="text-xs text-gray-400 dark:text-gray-500">{{ t('dashboard.totalAccountCost') }} ${{ formatCost(summary.total_account_cost) }}</span>
        </div>

        <div v-if="topAccounts.length" class="overflow-x-auto">
          <table class="w-full text-xs">
            <thead>
              <tr class="border-b border-gray-100 text-gray-500 dark:border-gray-700 dark:text-gray-400">
                <th class="pb-2 text-left">{{ t('dashboard.account') }}</th>
                <th class="pb-2 text-left">{{ t('dashboard.shareStatus') }}</th>
                <th class="pb-2 text-right">{{ t('dashboard.selfUsage') }}</th>
                <th class="pb-2 text-right">{{ t('dashboard.externalUsage') }}</th>
                <th class="pb-2 text-right">{{ t('dashboard.ownerCredit') }}</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="account in topAccounts" :key="account.account_id" class="border-b border-gray-50 dark:border-gray-800">
                <td class="max-w-[180px] py-2">
                  <div class="truncate font-medium text-gray-900 dark:text-white" :title="account.name">{{ account.name }}</div>
                  <div class="text-gray-400 dark:text-gray-500">{{ account.platform }}</div>
                </td>
                <td class="py-2">
                  <span class="inline-flex items-center rounded-full px-2 py-0.5 text-[11px] font-medium" :class="statusClass(account)">
                    {{ statusLabel(account) }}
                  </span>
                </td>
                <td class="py-2 text-right text-gray-700 dark:text-gray-300">
                  ${{ formatCost(account.self_account_cost) }}
                  <div class="text-gray-400 dark:text-gray-500">{{ formatNumber(account.self_requests) }}</div>
                </td>
                <td class="py-2 text-right text-blue-600 dark:text-blue-400">
                  ${{ formatCost(account.external_consumer_charge) }}
                  <div class="text-gray-400 dark:text-gray-500">{{ formatNumber(account.external_requests) }}</div>
                </td>
                <td class="py-2 text-right font-semibold text-emerald-600 dark:text-emerald-400">
                  ${{ formatCost(account.external_owner_credit) }}
                </td>
              </tr>
            </tbody>
          </table>
        </div>

        <div v-else class="py-6 text-center text-sm text-gray-500 dark:text-gray-400">
          {{ t('dashboard.noOwnedAccountStats') }}
        </div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '@/components/icons/Icon.vue'
import LoadingSpinner from '@/components/common/LoadingSpinner.vue'
import type { AccountSharingAccountStat, AccountSharingDashboardStats } from '@/api/usage'
import { formatCostFixed as formatCost, formatNumberLocaleString as formatNumber } from '@/utils/format'

const props = defineProps<{
  stats: AccountSharingDashboardStats | null
  loading: boolean
  error?: string
}>()

const { t } = useI18n()

const summary = computed(() => props.stats?.summary ?? null)
const topAccounts = computed(() => props.stats?.accounts?.slice(0, 8) ?? [])

function statusLabel(account: AccountSharingAccountStat): string {
  if (account.share_mode === 'private') {
    return t('dashboard.privateMode')
  }
  if (account.share_status === 'approved') {
    return t('dashboard.publicApproved')
  }
  if (account.share_status === 'suspended') {
    return t('dashboard.publicSuspended')
  }
  return t('dashboard.publicPending')
}

function statusClass(account: AccountSharingAccountStat): string {
  if (account.share_mode === 'private') {
    return 'bg-gray-100 text-gray-700 dark:bg-gray-800 dark:text-gray-300'
  }
  if (account.share_status === 'approved') {
    return 'bg-emerald-100 text-emerald-700 dark:bg-emerald-900/30 dark:text-emerald-300'
  }
  if (account.share_status === 'suspended') {
    return 'bg-rose-100 text-rose-700 dark:bg-rose-900/30 dark:text-rose-300'
  }
  return 'bg-amber-100 text-amber-700 dark:bg-amber-900/30 dark:text-amber-300'
}
</script>
