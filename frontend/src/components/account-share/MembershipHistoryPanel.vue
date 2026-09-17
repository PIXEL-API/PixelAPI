<template>
  <section
    class="space-y-4"
    aria-labelledby="membership-history-title"
    :aria-busy="loading"
    data-testid="membership-history-panel"
  >
    <div
      class="flex flex-col gap-3 rounded-2xl border border-slate-200 bg-white p-4 shadow-sm sm:flex-row sm:items-center sm:justify-between dark:border-dark-700 dark:bg-dark-900"
    >
      <div class="flex min-w-0 items-start gap-3">
        <span
          class="flex h-11 w-11 flex-none items-center justify-center rounded-xl bg-sky-50 text-sky-700 dark:bg-sky-950/40 dark:text-sky-300"
          aria-hidden="true"
        >
          <Icon name="clock" size="sm" />
        </span>
        <div class="min-w-0">
          <h2 id="membership-history-title" class="text-base font-semibold text-slate-950 dark:text-white">
            {{ t('accountShare.membership.title') }}
          </h2>
          <p class="mt-1 text-sm leading-6 text-slate-600 dark:text-dark-300">
            {{ t('accountShare.membership.subtitle') }}
          </p>
        </div>
      </div>
      <span
        class="inline-flex min-h-11 flex-none items-center justify-center rounded-xl bg-slate-100 px-4 text-sm font-semibold text-slate-700 dark:bg-dark-800 dark:text-dark-200"
      >
        {{ t('accountShare.membership.totalCount', { total }) }}
      </span>
    </div>

    <div
      v-if="errorMessage"
      class="flex flex-col gap-3 rounded-2xl border border-red-200 bg-red-50 p-4 text-red-800 sm:flex-row sm:items-center sm:justify-between dark:border-red-900/60 dark:bg-red-950/30 dark:text-red-200"
      role="alert"
    >
      <span class="text-sm leading-6">{{ errorMessage }}</span>
      <button type="button" class="btn btn-secondary min-h-11 flex-none" @click="emit('reload')">
        {{ t('accountShare.membership.reload') }}
      </button>
    </div>

    <div
      v-else-if="loading"
      class="rounded-2xl border border-slate-200 bg-white p-8 text-center text-sm text-slate-600 shadow-sm dark:border-dark-700 dark:bg-dark-900 dark:text-dark-300"
      role="status"
    >
      {{ t('accountShare.membership.loading') }}
    </div>

    <div
      v-else-if="items.length === 0"
      class="rounded-2xl border border-dashed border-slate-300 bg-white p-8 text-center dark:border-dark-700 dark:bg-dark-900"
      data-testid="membership-history-empty"
    >
      <strong class="text-base text-slate-900 dark:text-white">{{ t('accountShare.membership.empty') }}</strong>
      <p class="mt-2 text-sm leading-6 text-slate-600 dark:text-dark-300">
        {{ t('accountShare.membership.emptyHint') }}
      </p>
    </div>

    <div v-else class="grid gap-4" data-testid="membership-history-list">
      <article
        v-for="entry in items"
        :key="entry.membership_id"
        class="overflow-hidden rounded-2xl border border-slate-200 bg-white shadow-sm dark:border-dark-700 dark:bg-dark-900"
        data-testid="membership-history-card"
      >
        <header class="flex flex-col gap-3 border-b border-slate-100 p-4 sm:flex-row sm:items-start sm:justify-between dark:border-dark-700">
          <div class="min-w-0">
            <div class="flex flex-wrap items-center gap-2">
              <h3 class="break-words text-base font-semibold text-slate-950 dark:text-white">
                {{ entry.room_name || `房间 #${entry.listing_id}` }}
              </h3>
              <span
                v-if="entry.room_deleted"
                class="inline-flex min-h-7 items-center rounded-full bg-slate-200 px-2.5 text-xs font-semibold text-slate-700 dark:bg-dark-700 dark:text-dark-200"
              >
                {{ t('accountShare.membership.roomDeleted') }}
              </span>
              <span
                class="inline-flex min-h-7 items-center rounded-full px-2.5 text-xs font-semibold"
                :class="snapshotBadgeClass(entry.snapshot_quality)"
              >
                {{ snapshotQualityLabel(entry.snapshot_quality) }}
              </span>
            </div>
            <p class="mt-2 break-words text-sm leading-6 text-slate-600 dark:text-dark-300">
              {{ platformLabel(entry.platform) }}
              <template v-if="entry.account_level"> · {{ entry.account_level }}</template>
              · {{ entry.account_name || (entry.account_id ? `账号 #${entry.account_id}` : '账号信息未保留') }}
            </p>
          </div>
          <div class="flex flex-wrap items-center gap-2 sm:justify-end">
            <span class="inline-flex min-h-8 items-center rounded-lg bg-sky-50 px-3 text-xs font-semibold text-sky-700 dark:bg-sky-950/40 dark:text-sky-300">
              {{ t('accountShare.membership.recordRef', { membershipId: entry.membership_id }) }}
            </span>
            <span class="inline-flex min-h-8 items-center rounded-lg bg-slate-100 px-3 text-xs font-medium text-slate-700 dark:bg-dark-800 dark:text-dark-200">
              {{ membershipStatusLabel(entry.status) }}
            </span>
          </div>
        </header>

        <div class="space-y-4 p-4">
          <div class="grid gap-3 sm:grid-cols-2 xl:grid-cols-4">
            <div class="rounded-xl bg-slate-50 p-3 dark:bg-dark-800">
              <span class="text-xs font-medium text-slate-500 dark:text-dark-400">{{ t('accountShare.membership.usageTime') }}</span>
              <strong class="mt-1 block break-words text-sm leading-6 text-slate-900 dark:text-white">
                {{ formatDate(entry.joined_at) }}
              </strong>
              <small class="mt-1 block break-words text-xs leading-5 text-slate-600 dark:text-dark-300">
                {{ t('accountShare.membership.untilTime', { endedAt: entry.ended_at ? formatDate(entry.ended_at) : t('accountShare.membership.noEndTime') }) }}
              </small>
            </div>
            <div class="rounded-xl bg-slate-50 p-3 dark:bg-dark-800">
              <span class="text-xs font-medium text-slate-500 dark:text-dark-400">{{ t('accountShare.membership.endReason') }}</span>
              <strong class="mt-1 block text-sm leading-6 text-slate-900 dark:text-white">
                {{ endedReasonLabel(entry.ended_reason) }}
              </strong>
              <small class="mt-1 block break-words text-xs leading-5 text-slate-600 dark:text-dark-300">
                {{ t('accountShare.membership.lastRequest', { lastRequestAt: entry.last_request_at ? formatDate(entry.last_request_at) : t('common.none') }) }}
              </small>
            </div>
            <div class="rounded-xl bg-slate-50 p-3 dark:bg-dark-800">
              <span class="text-xs font-medium text-slate-500 dark:text-dark-400">{{ t('accountShare.membership.ownerAndKey') }}</span>
              <strong class="mt-1 block break-words text-sm leading-6 text-slate-900 dark:text-white">
                {{ entry.owner_username || `用户 #${entry.owner_user_id}` }}
              </strong>
              <small class="mt-1 block break-all text-xs leading-5 text-slate-600 dark:text-dark-300">
                {{ entry.api_key_name || `Key #${entry.api_key_id}` }}
              </small>
            </div>
            <div class="rounded-xl bg-slate-50 p-3 dark:bg-dark-800">
              <span class="text-xs font-medium text-slate-500 dark:text-dark-400">{{ t('accountShare.membership.settlementBoundary') }}</span>
              <strong class="mt-1 block text-sm leading-6 text-slate-900 dark:text-white">
                {{ t('accountShare.membership.billedUntil', { billedUntil: entry.billed_until ? formatDate(entry.billed_until) : t('accountShare.membership.notRecorded') }) }}
              </strong>
              <small class="mt-1 block text-xs leading-5 text-slate-600 dark:text-dark-300">
                {{ t('accountShare.membership.paidUntil', { paidUntil: entry.paid_until ? formatDate(entry.paid_until) : t('accountShare.membership.notRecorded') }) }}
              </small>
            </div>
          </div>

          <div class="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
            <div class="history-metric">
              <span>{{ t('accountShare.membership.settledRequests') }}</span>
              <strong>{{ entry.usage_request_count }}</strong>
            </div>
            <div class="history-metric">
              <span>{{ t('accountShare.membership.requestSpend') }}</span>
              <strong>{{ formatAmount(entry.usage_request_cost) }}</strong>
            </div>
            <div class="history-metric">
              <span>{{ t('accountShare.membership.hourlyRate') }}</span>
              <strong>{{ formatAmount(entry.hourly_rate_snapshot) }}</strong>
            </div>
            <div class="history-metric">
              <span>{{ t('accountShare.membership.fieldFeeWaiver') }}</span>
              <strong>{{ formatAmount(entry.hourly_fee_waiver_minimum_snapshot) }}</strong>
            </div>
            <div class="history-metric">
              <span>{{ t('accountShare.membership.configuredConcurrency') }}</span>
              <strong>{{ entry.configured_concurrency_snapshot || '-' }}</strong>
            </div>
            <div class="history-metric">
              <span>{{ t('accountShare.membership.fieldIdleTimeout') }}</span>
              <strong>{{ t('accountShare.membership.idleMinutes', { idleTimeoutMinutes: entry.idle_timeout_minutes }) }}</strong>
            </div>
          </div>

          <div
            v-if="entry.snapshot_quality !== 'exact'"
            class="rounded-xl border px-3 py-2 text-sm leading-6"
            :class="entry.snapshot_quality === 'unknown'
              ? 'border-amber-200 bg-amber-50 text-amber-800 dark:border-amber-900/60 dark:bg-amber-950/30 dark:text-amber-200'
              : 'border-sky-200 bg-sky-50 text-sky-800 dark:border-sky-900/60 dark:bg-sky-950/30 dark:text-sky-200'"
          >
            {{ snapshotQualityDescription(entry.snapshot_quality) }}
          </div>

          <details
            v-if="entry.terms_snapshot"
            class="group rounded-xl border border-slate-200 bg-slate-50/70 dark:border-dark-700 dark:bg-dark-800/70"
          >
            <summary
              class="flex min-h-11 cursor-pointer list-none items-center justify-between gap-3 px-3 py-2 text-sm font-semibold text-slate-800 focus:outline-none focus-visible:ring-2 focus-visible:ring-primary-500/50 dark:text-dark-100"
            >
              <span class="flex items-center gap-2">
                <Icon name="document" size="sm" />
                {{ t('accountShare.membership.viewSnapshot') }}
              </span>
              <span class="text-xs text-slate-500 group-open:hidden dark:text-dark-400">{{ t('common.expand') }}</span>
              <span class="hidden text-xs text-slate-500 group-open:inline dark:text-dark-400">{{ t('common.collapse') }}</span>
            </summary>
            <div class="grid gap-3 border-t border-slate-200 p-3 text-sm sm:grid-cols-2 lg:grid-cols-3 dark:border-dark-700">
              <HistoryTerm :label="t('accountShare.membership.fieldSeatLimit')" :value="t('accountShare.membership.seatLimitValue', { seatLimit: entry.terms_snapshot.seat_limit })" />
              <HistoryTerm :label="t('accountShare.membership.fieldPerUserConcurrency')" :value="String(entry.terms_snapshot.per_user_concurrency)" />
              <HistoryTerm :label="t('accountShare.membership.fieldRateMultiplier')" :value="`${formatAmount(entry.terms_snapshot.rate_multiplier)}x`" />
              <HistoryTerm :label="t('accountShare.membership.fieldHourlyRate')" :value="formatAmount(entry.terms_snapshot.hourly_rate)" />
              <HistoryTerm
                :label="t('accountShare.membership.fieldFeeWaiver')"
                :value="formatAmount(entry.terms_snapshot.hourly_fee_waiver_minimum)"
              />
              <HistoryTerm :label="t('accountShare.membership.fieldMinBalance')" :value="formatAmount(entry.terms_snapshot.min_balance_required)" />
              <HistoryTerm :label="t('accountShare.membership.fieldIdleTimeout')" :value="t('accountShare.membership.idleMinutes', { idleTimeoutMinutes: entry.idle_timeout_minutes })" />
              <HistoryTerm
                v-if="entry.platform === 'openai'"
                label="Codex CLI"
                :value="entry.terms_snapshot.codex_cli_only ? t('accountShare.membership.codexCliOnly') : t('accountShare.membership.unlimited')"
              />
              <HistoryTerm
                v-if="entry.platform === 'openai'"
                :label="t('accountShare.membership.fieldCodexThreshold')"
                :value="formatPercentPair(entry.terms_snapshot.codex_5h_limit_percent, entry.terms_snapshot.codex_7d_limit_percent)"
              />
              <HistoryTerm
                v-else-if="entry.platform === 'anthropic'"
                :label="t('accountShare.membership.fieldClaudeThreshold')"
                :value="formatPercentPair(entry.terms_snapshot.anthropic_5h_limit_percent, entry.terms_snapshot.anthropic_7d_limit_percent)"
              />
              <div class="sm:col-span-2 lg:col-span-3">
                <span class="text-xs font-medium text-slate-500 dark:text-dark-400">{{ t('accountShare.membership.allowedModels') }}</span>
                <div class="mt-2 flex flex-wrap gap-2">
                  <span
                    v-for="model in entry.terms_snapshot.allowed_models"
                    :key="model"
                    class="max-w-full break-all rounded-lg bg-white px-2.5 py-1 text-xs text-slate-700 ring-1 ring-slate-200 dark:bg-dark-900 dark:text-dark-200 dark:ring-dark-600"
                  >
                    {{ model }}
                  </span>
                  <span v-if="entry.terms_snapshot.allowed_models.length === 0" class="text-sm text-slate-500 dark:text-dark-400">
                    {{ t('accountShare.membership.notRecorded') }}
                  </span>
                </div>
              </div>
            </div>
          </details>

          <div
            v-if="entry.review"
            class="rounded-xl border border-emerald-200 bg-emerald-50/70 p-3 dark:border-emerald-900/60 dark:bg-emerald-950/20"
          >
            <div class="flex flex-wrap items-center justify-between gap-2">
              <strong class="text-sm text-emerald-950 dark:text-emerald-100">{{ t('accountShare.membership.myReview', { score: entry.review.score }) }}</strong>
              <span class="text-xs font-medium text-emerald-800 dark:text-emerald-200">
                {{ reviewStatusLabel(entry.review.comment_status) }}
              </span>
            </div>
            <p v-if="entry.review.comment" class="mt-2 break-words text-sm leading-6 text-emerald-900 dark:text-emerald-100">
              {{ entry.review.comment }}
            </p>
            <p
              v-if="entry.review.comment_reject_reason"
              class="mt-2 break-words text-xs leading-5 text-amber-800 dark:text-amber-200"
            >
              {{ t('accountShare.membership.rejectNote', { commentRejectReason: entry.review.comment_reject_reason }) }}
            </p>
          </div>
          <div
            v-else
            class="flex flex-col gap-3 rounded-xl border border-slate-200 bg-slate-50 p-3 sm:flex-row sm:items-center sm:justify-between dark:border-dark-700 dark:bg-dark-800"
          >
            <div class="min-w-0">
              <strong class="text-sm text-slate-900 dark:text-white">
                {{ entry.usage_request_count > 0 ? t('accountShare.membership.noReviewYet') : t('accountShare.membership.noReviewableRequests') }}
              </strong>
              <p class="mt-1 text-xs leading-5 text-slate-600 dark:text-dark-300">
                {{ entry.usage_request_count > 0
                  ? t('accountShare.membership.reviewBound')
                  : t('accountShare.membership.reviewRequiresSettled') }}
              </p>
            </div>
            <button
              v-if="entry.usage_request_count > 0"
              type="button"
              class="btn btn-secondary min-h-11 flex-none"
              data-testid="membership-history-review"
              @click="emit('review', entry)"
            >
              {{ t('accountShare.membership.reviewThisUse') }}
            </button>
          </div>

          <footer class="flex flex-wrap gap-x-4 gap-y-1 text-xs leading-5 text-slate-500 dark:text-dark-400">
            <span>{{ t('accountShare.membership.listingRef', { listingId: entry.listing_id }) }}</span>
            <span v-if="entry.listing_revision_id">{{ t('accountShare.membership.revisionRef', { listingRevisionId: entry.listing_revision_id }) }}</span>
            <span v-if="entry.listing_version_snapshot">{{ t('accountShare.membership.versionSnapshot', { listingVersionSnapshot: entry.listing_version_snapshot }) }}</span>
            <span v-if="entry.room_deleted_at">{{ t('accountShare.membership.deletedAt', { roomDeletedAt: formatDate(entry.room_deleted_at) }) }}</span>
          </footer>
        </div>
      </article>
    </div>

    <Pagination
      v-if="showPagination && !loading && total > pageSize"
      class="overflow-hidden rounded-xl border border-slate-200 shadow-sm dark:border-dark-700"
      :page="page"
      :total="total"
      :page-size="pageSize"
      :show-page-size-selector="false"
      @update:page="emit('update:page', $event)"
    />
  </section>
</template>

<script setup lang="ts">
import type { AccountShareMembershipHistoryEntry } from '@/api/accountShare'
import Icon from '@/components/icons/Icon.vue'
import Pagination from '@/components/common/Pagination.vue'
import HistoryTerm from './MembershipHistoryTerm.vue'
import { useI18n } from 'vue-i18n'

const { t } = useI18n()

withDefaults(defineProps<{
  items: AccountShareMembershipHistoryEntry[]
  loading: boolean
  errorMessage: string
  page: number
  pageSize: number
  total: number
  showPagination?: boolean
}>(), { showPagination: true })

const emit = defineEmits<{
  reload: []
  'update:page': [page: number]
  review: [entry: AccountShareMembershipHistoryEntry]
}>()

function formatDate(value?: string): string {
  if (!value) return '-'
  const parsed = new Date(value)
  if (Number.isNaN(parsed.getTime())) return value
  return parsed.toLocaleString('zh-CN', {
    year: 'numeric',
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit'
  })
}

function formatAmount(value: number): string {
  const amount = Number(value || 0)
  if (!Number.isFinite(amount)) return '0'
  return amount.toFixed(6).replace(/\.?0+$/, '')
}

function formatPercentPair(first?: number, second?: number): string {
  const formatPercent = (value?: number): string =>
    typeof value === 'number' && Number.isFinite(value) ? `${formatAmount(value)}%` : t('accountShare.membership.notRecorded')
  return `${formatPercent(first)} / ${formatPercent(second)}`
}

function platformLabel(platform: string): string {
  if (platform === 'openai') return 'OpenAI'
  if (platform === 'anthropic') return 'Anthropic'
  if (platform === 'opencode') return 'OpenCode'
  if (platform === 'kimi') return 'Kimi'
  if (platform === 'zhipu') return t('common.platforms.zhipu')
  if (platform === 'deepseek') return 'DeepSeek'
  if (platform === 'minimax') return 'MiniMax'
  if (platform === 'qwen') return t('common.platforms.qwen')
  if (platform === 'devin') return 'Devin'
  if (platform === 'api_aggregation') return 'APIKEY'
  return platform || t('accountShare.membership.unknownPlatform')
}

function membershipStatusLabel(status: string): string {
  if (status === 'ended') return t('accountShare.membership.statusEnded')
  if (status === 'ending') return t('accountShare.membership.statusSettling')
  if (status === 'active') return t('accountShare.membership.statusActive')
  if (status === 'queued') return t('accountShare.membership.statusQueued')
  return status || t('accountShare.membership.statusHistory')
}

function endedReasonLabel(reason?: string): string {
  switch (reason) {
    case 'manual':
      return t('accountShare.membership.reasonManual')
    case 'idle_timeout':
      return t('accountShare.membership.reasonIdleTimeout')
    case 'prepay_insufficient':
      return t('accountShare.membership.reasonPrepayInsufficient')
    case 'account_unavailable':
      return t('accountShare.membership.reasonAccountUnavailable')
    case 'queue_expired':
      return t('accountShare.membership.reasonQueueExpired')
    case 'queue_removed':
      return t('accountShare.membership.reasonQueueRemoved')
    case 'room_draining':
      return t('accountShare.membership.reasonRoomDraining')
    default:
      return reason || t('accountShare.membership.notRecorded')
  }
}

function snapshotQualityLabel(quality: string): string {
  if (quality === 'exact') return t('accountShare.membership.snapshotExact')
  if (quality === 'backfilled_current') return t('accountShare.membership.snapshotBackfilled')
  if (quality === 'unknown') return t('accountShare.membership.snapshotMissing')
  return t('accountShare.membership.snapshotUnknown')
}

function snapshotQualityDescription(quality: string): string {
  if (quality === 'backfilled_current') {
    return t('accountShare.membership.snapshotBackfilledDesc')
  }
  if (quality === 'unknown') {
    return t('accountShare.membership.snapshotUnknownDesc')
  }
  return t('accountShare.membership.snapshotFallbackDesc')
}

function snapshotBadgeClass(quality: string): string {
  if (quality === 'exact') {
    return 'bg-emerald-50 text-emerald-700 dark:bg-emerald-950/40 dark:text-emerald-300'
  }
  if (quality === 'backfilled_current') {
    return 'bg-sky-50 text-sky-700 dark:bg-sky-950/40 dark:text-sky-300'
  }
  return 'bg-amber-50 text-amber-800 dark:bg-amber-950/40 dark:text-amber-200'
}

function reviewStatusLabel(status: string): string {
  switch (status) {
    case 'approved':
      return t('accountShare.membership.reviewApproved')
    case 'pending':
      return t('accountShare.membership.reviewPending')
    case 'rejected':
      return t('accountShare.membership.reviewRejected')
    case 'failed':
      return t('accountShare.membership.reviewFailed')
    default:
      return t('accountShare.membership.reviewRatingOnly')
  }
}
</script>

<style scoped>
.history-metric {
  display: flex;
  min-width: 0;
  min-height: 4.5rem;
  flex-direction: column;
  justify-content: center;
  border-radius: 0.75rem;
  border: 1px solid rgb(226 232 240);
  padding: 0.75rem;
  background: rgb(255 255 255);
}

.history-metric span {
  color: rgb(100 116 139);
  font-size: 0.75rem;
  font-weight: 500;
}

.history-metric strong {
  margin-top: 0.25rem;
  color: rgb(15 23 42);
  font-size: 1rem;
  line-height: 1.5rem;
  overflow-wrap: anywhere;
}

:global(.dark) .history-metric {
  border-color: rgb(63 63 70);
  background: rgb(24 24 27);
}

:global(.dark) .history-metric span {
  color: rgb(161 161 170);
}

:global(.dark) .history-metric strong {
  color: white;
}
</style>
