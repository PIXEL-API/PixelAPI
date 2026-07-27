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
            完整消费记录
          </h2>
          <p class="mt-1 text-sm leading-6 text-slate-600 dark:text-dark-300">
            每次加入都会单独保留条款、账号、消费与评价快照；房间删除后仍可追溯。
          </p>
        </div>
      </div>
      <span
        class="inline-flex min-h-11 flex-none items-center justify-center rounded-xl bg-slate-100 px-4 text-sm font-semibold text-slate-700 dark:bg-dark-800 dark:text-dark-200"
      >
        共 {{ total }} 次
      </span>
    </div>

    <div
      v-if="errorMessage"
      class="flex flex-col gap-3 rounded-2xl border border-red-200 bg-red-50 p-4 text-red-800 sm:flex-row sm:items-center sm:justify-between dark:border-red-900/60 dark:bg-red-950/30 dark:text-red-200"
      role="alert"
    >
      <span class="text-sm leading-6">{{ errorMessage }}</span>
      <button type="button" class="btn btn-secondary min-h-11 flex-none" @click="emit('reload')">
        重新加载
      </button>
    </div>

    <div
      v-else-if="loading"
      class="rounded-2xl border border-slate-200 bg-white p-8 text-center text-sm text-slate-600 shadow-sm dark:border-dark-700 dark:bg-dark-900 dark:text-dark-300"
      role="status"
    >
      正在读取完整消费记录...
    </div>

    <div
      v-else-if="items.length === 0"
      class="rounded-2xl border border-dashed border-slate-300 bg-white p-8 text-center dark:border-dark-700 dark:bg-dark-900"
      data-testid="membership-history-empty"
    >
      <strong class="text-base text-slate-900 dark:text-white">还没有已结束的消费记录</strong>
      <p class="mt-2 text-sm leading-6 text-slate-600 dark:text-dark-300">
        使用或预约结束后，每次记录都会在这里独立保存。
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
                房间已删除
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
              记录 #{{ entry.membership_id }}
            </span>
            <span class="inline-flex min-h-8 items-center rounded-lg bg-slate-100 px-3 text-xs font-medium text-slate-700 dark:bg-dark-800 dark:text-dark-200">
              {{ membershipStatusLabel(entry.status) }}
            </span>
          </div>
        </header>

        <div class="space-y-4 p-4">
          <div class="grid gap-3 sm:grid-cols-2 xl:grid-cols-4">
            <div class="rounded-xl bg-slate-50 p-3 dark:bg-dark-800">
              <span class="text-xs font-medium text-slate-500 dark:text-dark-400">使用时间</span>
              <strong class="mt-1 block break-words text-sm leading-6 text-slate-900 dark:text-white">
                {{ formatDate(entry.joined_at) }}
              </strong>
              <small class="mt-1 block break-words text-xs leading-5 text-slate-600 dark:text-dark-300">
                至 {{ entry.ended_at ? formatDate(entry.ended_at) : '尚未记录结束时间' }}
              </small>
            </div>
            <div class="rounded-xl bg-slate-50 p-3 dark:bg-dark-800">
              <span class="text-xs font-medium text-slate-500 dark:text-dark-400">结束原因</span>
              <strong class="mt-1 block text-sm leading-6 text-slate-900 dark:text-white">
                {{ endedReasonLabel(entry.ended_reason) }}
              </strong>
              <small class="mt-1 block break-words text-xs leading-5 text-slate-600 dark:text-dark-300">
                最近请求 {{ entry.last_request_at ? formatDate(entry.last_request_at) : '无' }}
              </small>
            </div>
            <div class="rounded-xl bg-slate-50 p-3 dark:bg-dark-800">
              <span class="text-xs font-medium text-slate-500 dark:text-dark-400">号主与 Key</span>
              <strong class="mt-1 block break-words text-sm leading-6 text-slate-900 dark:text-white">
                {{ entry.owner_username || `用户 #${entry.owner_user_id}` }}
              </strong>
              <small class="mt-1 block break-all text-xs leading-5 text-slate-600 dark:text-dark-300">
                {{ entry.api_key_name || `Key #${entry.api_key_id}` }}
              </small>
            </div>
            <div class="rounded-xl bg-slate-50 p-3 dark:bg-dark-800">
              <span class="text-xs font-medium text-slate-500 dark:text-dark-400">结算边界</span>
              <strong class="mt-1 block text-sm leading-6 text-slate-900 dark:text-white">
                已计费至 {{ entry.billed_until ? formatDate(entry.billed_until) : '未记录' }}
              </strong>
              <small class="mt-1 block text-xs leading-5 text-slate-600 dark:text-dark-300">
                已支付至 {{ entry.paid_until ? formatDate(entry.paid_until) : '未记录' }}
              </small>
            </div>
          </div>

          <div class="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
            <div class="history-metric">
              <span>已结算请求</span>
              <strong>{{ entry.usage_request_count }}</strong>
            </div>
            <div class="history-metric">
              <span>请求消费</span>
              <strong>{{ formatAmount(entry.usage_request_cost) }}</strong>
            </div>
            <div class="history-metric">
              <span>小时费率</span>
              <strong>{{ formatAmount(entry.hourly_rate_snapshot) }}</strong>
            </div>
            <div class="history-metric">
              <span>免小时费低消</span>
              <strong>{{ formatAmount(entry.hourly_fee_waiver_minimum_snapshot) }}</strong>
            </div>
            <div class="history-metric">
              <span>配置并发</span>
              <strong>{{ entry.configured_concurrency_snapshot || '-' }}</strong>
            </div>
            <div class="history-metric">
              <span>空闲退出</span>
              <strong>{{ entry.idle_timeout_minutes }} 分钟</strong>
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
                查看本次条款快照
              </span>
              <span class="text-xs text-slate-500 group-open:hidden dark:text-dark-400">展开</span>
              <span class="hidden text-xs text-slate-500 group-open:inline dark:text-dark-400">收起</span>
            </summary>
            <div class="grid gap-3 border-t border-slate-200 p-3 text-sm sm:grid-cols-2 lg:grid-cols-3 dark:border-dark-700">
              <HistoryTerm label="成员上限" :value="`${entry.terms_snapshot.seat_limit} 人`" />
              <HistoryTerm label="单人并发" :value="String(entry.terms_snapshot.per_user_concurrency)" />
              <HistoryTerm label="费率倍率" :value="`${formatAmount(entry.terms_snapshot.rate_multiplier)}x`" />
              <HistoryTerm label="小时费" :value="formatAmount(entry.terms_snapshot.hourly_rate)" />
              <HistoryTerm
                label="免小时费低消"
                :value="formatAmount(entry.terms_snapshot.hourly_fee_waiver_minimum)"
              />
              <HistoryTerm label="最低余额" :value="formatAmount(entry.terms_snapshot.min_balance_required)" />
              <HistoryTerm label="空闲退出" :value="`${entry.idle_timeout_minutes} 分钟`" />
              <HistoryTerm
                v-if="entry.platform === 'openai'"
                label="Codex CLI"
                :value="entry.terms_snapshot.codex_cli_only ? '仅允许 Codex CLI' : '不限制'"
              />
              <HistoryTerm
                v-if="entry.platform === 'openai'"
                label="Codex 5小时 / 7天阈值"
                :value="formatPercentPair(entry.terms_snapshot.codex_5h_limit_percent, entry.terms_snapshot.codex_7d_limit_percent)"
              />
              <HistoryTerm
                v-else-if="entry.platform === 'anthropic'"
                label="Claude 5小时 / 7天阈值"
                :value="formatPercentPair(entry.terms_snapshot.anthropic_5h_limit_percent, entry.terms_snapshot.anthropic_7d_limit_percent)"
              />
              <div class="sm:col-span-2 lg:col-span-3">
                <span class="text-xs font-medium text-slate-500 dark:text-dark-400">允许模型</span>
                <div class="mt-2 flex flex-wrap gap-2">
                  <span
                    v-for="model in entry.terms_snapshot.allowed_models"
                    :key="model"
                    class="max-w-full break-all rounded-lg bg-white px-2.5 py-1 text-xs text-slate-700 ring-1 ring-slate-200 dark:bg-dark-900 dark:text-dark-200 dark:ring-dark-600"
                  >
                    {{ model }}
                  </span>
                  <span v-if="entry.terms_snapshot.allowed_models.length === 0" class="text-sm text-slate-500 dark:text-dark-400">
                    未记录
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
              <strong class="text-sm text-emerald-950 dark:text-emerald-100">我的评价 {{ entry.review.score }}/10</strong>
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
              处理说明：{{ entry.review.comment_reject_reason }}
            </p>
          </div>
          <div
            v-else
            class="flex flex-col gap-3 rounded-xl border border-slate-200 bg-slate-50 p-3 sm:flex-row sm:items-center sm:justify-between dark:border-dark-700 dark:bg-dark-800"
          >
            <div class="min-w-0">
              <strong class="text-sm text-slate-900 dark:text-white">
                {{ entry.usage_request_count > 0 ? '还没有评价本次使用' : '本次暂无可评价请求' }}
              </strong>
              <p class="mt-1 text-xs leading-5 text-slate-600 dark:text-dark-300">
                {{ entry.usage_request_count > 0
                  ? '评价会绑定这一次使用记录，不会覆盖同房间的其他历史。'
                  : '只有产生已结算实际请求的使用记录可以评价。' }}
              </p>
            </div>
            <button
              v-if="entry.usage_request_count > 0"
              type="button"
              class="btn btn-secondary min-h-11 flex-none"
              data-testid="membership-history-review"
              @click="emit('review', entry)"
            >
              评价本次使用
            </button>
          </div>

          <footer class="flex flex-wrap gap-x-4 gap-y-1 text-xs leading-5 text-slate-500 dark:text-dark-400">
            <span>房间 #{{ entry.listing_id }}</span>
            <span v-if="entry.listing_revision_id">条款版本 #{{ entry.listing_revision_id }}</span>
            <span v-if="entry.listing_version_snapshot">房间版本 {{ entry.listing_version_snapshot }}</span>
            <span v-if="entry.room_deleted_at">删除于 {{ formatDate(entry.room_deleted_at) }}</span>
          </footer>
        </div>
      </article>
    </div>

    <Pagination
      v-if="!loading && total > pageSize"
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

defineProps<{
  items: AccountShareMembershipHistoryEntry[]
  loading: boolean
  errorMessage: string
  page: number
  pageSize: number
  total: number
}>()

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
    typeof value === 'number' && Number.isFinite(value) ? `${formatAmount(value)}%` : '未记录'
  return `${formatPercent(first)} / ${formatPercent(second)}`
}

function platformLabel(platform: string): string {
  if (platform === 'openai') return 'OpenAI'
  if (platform === 'anthropic') return 'Anthropic'
  return platform || '未知平台'
}

function membershipStatusLabel(status: string): string {
  if (status === 'ended') return '已结束'
  if (status === 'ending') return '结算中'
  if (status === 'active') return '曾在使用'
  if (status === 'queued') return '曾在预约'
  return status || '历史记录'
}

function endedReasonLabel(reason?: string): string {
  switch (reason) {
    case 'manual':
      return '主动结束'
    case 'idle_timeout':
      return '空闲超时'
    case 'prepay_insufficient':
      return '预付余额不足'
    case 'account_unavailable':
      return '账号不可用'
    case 'queue_expired':
      return '预约过期'
    case 'room_draining':
      return '房间停止接入'
    default:
      return reason || '未记录'
  }
}

function snapshotQualityLabel(quality: string): string {
  if (quality === 'exact') return '精确快照'
  if (quality === 'backfilled_current') return '历史回填'
  if (quality === 'unknown') return '快照缺失'
  return '快照状态未知'
}

function snapshotQualityDescription(quality: string): string {
  if (quality === 'backfilled_current') {
    return '该记录由当前或最终房间信息回填，不代表本次使用当时的完整条款。'
  }
  if (quality === 'unknown') {
    return '该记录早于历史快照功能，迁移前的详细条款无法恢复；页面不会用当前房间参数冒充历史数据。'
  }
  return '服务端未提供可识别的快照精度，已按未知历史记录展示。'
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
      return '评价已展示'
    case 'pending':
      return '评价审核中'
    case 'rejected':
      return '评价未通过'
    case 'failed':
      return '评价处理失败'
    default:
      return '仅评分'
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
