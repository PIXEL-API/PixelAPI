<template>
  <AppLayout>
    <div class="activity-page">
      <div v-if="loading" class="activity-loading">
        <div class="h-8 w-8 animate-spin rounded-full border-2 border-primary-500 border-t-transparent"></div>
      </div>

      <template v-else>
        <section class="activity-summary">
          <div class="metric-tile">
            <span>{{ t('activities.stats.activeCampaigns') }}</span>
            <strong>{{ openCampaigns.length }}</strong>
          </div>
          <div class="metric-tile">
            <span>{{ t('activities.stats.availableTickets') }}</span>
            <strong>{{ availableTicketCount }}</strong>
          </div>
          <div class="metric-tile">
            <span>{{ t('activities.stats.joinedTickets') }}</span>
            <strong>{{ joinedTicketCount }}</strong>
          </div>
          <div class="metric-tile">
            <span>{{ t('activities.stats.pendingClaims') }}</span>
            <strong>{{ pendingClaimWinners.length }}</strong>
          </div>
          <button type="button" class="btn btn-secondary activity-refresh" :disabled="loading" @click="loadAll">
            <Icon name="refresh" size="sm" />
            <span>{{ t('common.refresh') }}</span>
          </button>
        </section>

        <section class="activity-tabs" role="tablist" :aria-label="t('activities.tabs.label')">
          <button
            v-for="tab in tabs"
            :key="tab.key"
            type="button"
            role="tab"
            class="activity-tab"
            :class="{ 'activity-tab-active': activeTab === tab.key }"
            :aria-selected="activeTab === tab.key"
            @click="activeTab = tab.key"
          >
            <span>{{ tab.label }}</span>
            <em>{{ tab.count }}</em>
          </button>
        </section>

        <section v-if="activeTab === 'open'" class="activity-section">
          <ActivityEmpty v-if="openCampaigns.length === 0" :text="t('activities.lottery.empty')" />
          <article v-for="campaign in openCampaigns" :key="campaign.id" class="activity-card">
            <div class="activity-card-main">
              <div class="activity-card-head">
                <div class="min-w-0">
                  <div class="flex flex-wrap items-center gap-2">
                    <h2>{{ campaign.name }}</h2>
                    <span :class="['badge', participationBadgeClass(campaign)]">{{ participationStatusText(campaign) }}</span>
                  </div>
                  <div class="activity-meta-line">
                    <span>{{ metricValueText(campaign.rule_config.metric, campaign.user_progress?.metric_value || 0) }}</span>
                    <span>/</span>
                    <span>{{ metricValueText(campaign.rule_config.metric, campaign.rule_config.threshold) }}</span>
                    <span>{{ periodLabel(campaign) }}</span>
                    <span>{{ formatShortDateTime(campaign.draw_at) }}</span>
                    <span v-if="publicParticipantText(campaign)">{{ publicParticipantText(campaign) }}</span>
                  </div>
                </div>
                <div class="activity-head-actions">
                  <div class="activity-ticket-box">
                    <span>{{ t('activities.lottery.myTickets') }}</span>
                    <strong>{{ campaign.user_progress?.ticket_count || 0 }}</strong>
                  </div>
                  <button type="button" class="btn btn-primary" :disabled="!canJoinCampaign(campaign)" @click="joinCampaign(campaign)">
                    {{ joinButtonLabel(campaign) }}
                  </button>
                </div>
              </div>

              <div class="activity-progress-row">
                <div class="progress-track">
                  <div class="progress-fill" :style="{ width: `${progressPercent(campaign)}%` }"></div>
                </div>
                <span>{{ progressPercent(campaign) }}%</span>
              </div>

              <div class="activity-stepper">
                <div :class="['activity-step', campaign.user_progress?.ticket_count ? 'activity-step-done' : '']">
                  <span>1</span>
                  <p>{{ t('activities.steps.qualify') }}</p>
                </div>
                <div :class="['activity-step', campaign.user_progress?.joined ? 'activity-step-done' : '']">
                  <span>2</span>
                  <p>{{ t('activities.steps.join') }}</p>
                </div>
                <div class="activity-step">
                  <span>3</span>
                  <p>{{ t('activities.steps.waitDraw') }}</p>
                </div>
              </div>

              <div class="info-grid">
                <InfoCell :label="metricLabel(campaign.rule_config.metric)" :value="metricValueText(campaign.rule_config.metric, campaign.user_progress?.metric_value || 0)" />
                <InfoCell :label="t('activities.rule.threshold')" :value="metricValueText(campaign.rule_config.metric, campaign.rule_config.threshold)" />
                <InfoCell :label="t('activities.rule.period')" :value="periodLabel(campaign)" />
                <InfoCell :label="t('activities.rule.drawAt')" :value="formatShortDateTime(campaign.draw_at)" />
              </div>
            </div>

            <aside class="activity-card-aside">
              <PrizeList :campaign="campaign" />
              <WinnerList :title="t('activities.winners.yesterday')" :empty="t('activities.winners.emptyYesterday')" :winners="campaign.yesterday_winners || []" />
            </aside>
          </article>
        </section>

        <section v-else-if="activeTab === 'joined'" class="activity-section">
          <ActivityEmpty v-if="joinedCampaigns.length === 0" :text="t('activities.joined.empty')" />
          <article v-for="campaign in joinedCampaigns" :key="campaign.id" class="activity-panel joined-row">
            <div class="min-w-0">
              <h2>{{ campaign.name }}</h2>
              <p>{{ t('activities.joined.summary', { count: campaign.user_progress?.joined_tickets || 0, time: formatDateTime(campaign.user_progress?.joined_at) || '-' }) }}</p>
            </div>
            <div class="joined-cells">
              <InfoCell :label="t('activities.rule.drawAt')" :value="formatDateTime(campaign.draw_at) || '-'" />
              <InfoCell :label="t('activities.joined.currentTickets')" :value="String(campaign.user_progress?.ticket_count || 0)" />
              <InfoCell :label="t('activities.joined.joinedTickets')" :value="String(campaign.user_progress?.joined_tickets || 0)" />
            </div>
            <div v-if="canUpdateJoinedTickets(campaign)" class="joined-update">
              <button type="button" class="btn btn-primary btn-sm" :disabled="isJoining(campaign.id)" @click="joinCampaign(campaign)">
                {{ t('activities.lottery.updateJoin') }}
              </button>
              <span>{{ t('activities.joined.updateHint') }}</span>
            </div>
          </article>
        </section>

        <section v-else-if="activeTab === 'winners'" class="activity-panel">
          <div class="panel-title-row">
            <h2>{{ t('activities.myWinners.title') }}</h2>
          </div>
          <ActivityEmpty v-if="winners.length === 0" :text="t('activities.myWinners.empty')" />
          <div v-else class="table-wrap">
            <table class="activity-table min-w-[760px]">
              <thead>
                <tr>
                  <th>{{ t('activities.myWinners.columns.campaign') }}</th>
                  <th>{{ t('activities.myWinners.columns.prize') }}</th>
                  <th>{{ t('activities.myWinners.columns.status') }}</th>
                  <th>{{ t('activities.myWinners.columns.createdAt') }}</th>
                  <th class="text-right">{{ t('common.actions') }}</th>
                </tr>
              </thead>
              <tbody>
                <tr v-for="winner in winners" :key="winner.id">
                  <td class="font-medium text-gray-900 dark:text-white">{{ winner.campaign_name || `#${winner.campaign_id}` }}</td>
                  <td>{{ winner.prize_name }}</td>
                  <td><span :class="['badge', winnerStatusBadgeClass(winner.status)]">{{ winnerStatusLabel(winner.status) }}</span></td>
                  <td>{{ formatDateTime(winner.created_at) }}</td>
                  <td class="text-right">
                    <button v-if="winner.status === 'pending_claim'" type="button" class="btn btn-primary btn-sm" @click="openClaim(winner)">{{ t('activities.myWinners.submitClaim') }}</button>
                    <span v-else class="text-xs text-gray-400 dark:text-dark-500">-</span>
                  </td>
                </tr>
              </tbody>
            </table>
          </div>
        </section>

        <section v-else-if="activeTab === 'past'" class="activity-section">
          <ActivityEmpty v-if="pastCampaigns.length === 0" :text="t('activities.past.empty')" />
          <article v-for="campaign in pastCampaigns" :key="campaign.id" class="activity-panel past-row">
            <div class="min-w-0">
              <div class="flex flex-wrap items-center gap-2">
                <h2>{{ campaign.name }}</h2>
                <span class="badge badge-gray">{{ t('activities.status.ended') }}</span>
                <span v-if="campaign.user_progress?.joined" class="badge badge-primary">{{ t('activities.past.joined') }}</span>
              </div>
              <p v-if="campaign.description">{{ campaign.description }}</p>
              <div class="mt-4 grid gap-3 md:grid-cols-3">
                <InfoCell :label="t('activities.rule.drawAt')" :value="formatDateTime(campaign.draw_at) || '-'" />
                <InfoCell :label="t('activities.joined.joinedTickets')" :value="String(campaign.user_progress?.joined_tickets || 0)" />
                <InfoCell :label="t('activities.rule.period')" :value="periodLabel(campaign)" />
              </div>
            </div>
            <WinnerList :title="t('activities.winners.recent')" :empty="t('activities.winners.emptyRecent')" :winners="campaign.recent_winners || []" />
          </article>
        </section>

        <section v-else class="affiliate-grid">
          <div class="activity-panel affiliate-overview">
            <div class="panel-title-row">
              <div>
                <h2>{{ t('activities.affiliate.title') }}</h2>
                <p>{{ t('activities.affiliate.description') }}</p>
              </div>
              <div class="period-control">
                <button
                  v-for="preset in periodPresets"
                  :key="preset"
                  type="button"
                  class="period-button"
                  :class="{ 'period-button-active': periodPreset === preset }"
                  @click="setPeriodPreset(preset)"
                >
                  {{ t(`affiliate.period.presets.${preset}`) }}
                </button>
              </div>
            </div>

            <div v-if="affiliateLoading" class="activity-loading compact">
              <div class="h-6 w-6 animate-spin rounded-full border-2 border-primary-500 border-t-transparent"></div>
            </div>
            <ActivityEmpty v-else-if="affiliateError" :text="affiliateError" />
            <ActivityEmpty v-else-if="!affiliateDetail" :text="t('affiliate.loadFailed')" />
            <template v-else>
              <div class="affiliate-metrics">
                <div class="affiliate-metric primary">
                  <span>{{ t('affiliate.stats.rebateRate') }}</span>
                  <strong>{{ formattedRebateRate }}%</strong>
                  <p>{{ t('affiliate.stats.rebateRateHint') }}</p>
                </div>
                <div class="affiliate-metric">
                  <span>{{ t('affiliate.stats.invitedUsers') }}</span>
                  <strong>{{ formatCount(affiliateDetail.aff_count) }}</strong>
                </div>
                <div class="affiliate-metric">
                  <span>{{ periodIncomeTitle }}</span>
                  <strong>{{ formatCurrency(affiliateDetail.period_rebate) }}</strong>
                </div>
                <div class="affiliate-metric">
                  <span>{{ t('affiliate.stats.totalQuota') }}</span>
                  <strong>{{ formatCurrency(affiliateDetail.aff_history_quota) }}</strong>
                </div>
                <div class="affiliate-metric">
                  <span>{{ t('affiliate.stats.settlementMode') }}</span>
                  <strong>{{ t('affiliate.stats.realtimeBalance') }}</strong>
                  <p>{{ t('affiliate.stats.realtimeBalanceHint') }}</p>
                </div>
              </div>

              <div class="affiliate-period-row">
                <input
                  v-model="periodStartDate"
                  type="date"
                  class="input h-10 text-sm"
                  :aria-label="t('affiliate.period.start')"
                  @change="setCustomPeriod"
                />
                <input
                  v-model="periodEndDate"
                  type="date"
                  class="input h-10 text-sm"
                  :aria-label="t('affiliate.period.end')"
                  @change="setCustomPeriod"
                />
              </div>
            </template>
          </div>

          <template v-if="affiliateDetail && !affiliateLoading && !affiliateError">
            <div class="activity-panel affiliate-share">
              <div class="copy-section">
                <CopyBox :label="t('affiliate.yourCode')" :value="affiliateDetail.aff_code" :button-text="t('affiliate.copyCode')" @copy="copyCode" />
                <CopyBox :label="t('affiliate.inviteLink')" :value="inviteLink" :button-text="t('affiliate.copyLink')" @copy="copyInviteLink" />
              </div>
              <div class="affiliate-policy-grid">
                <div>
                  <span>{{ t('affiliate.weeklyQuota') }}</span>
                  <strong>{{ weeklyQuotaText }}</strong>
                </div>
                <div>
                  <span>{{ t('affiliate.codePolicy.title') }}</span>
                  <strong>{{ codePolicyText }}</strong>
                  <p>{{ codeExpiryText }}</p>
                </div>
              </div>
            </div>

            <div class="activity-panel affiliate-tips">
              <p>{{ t('affiliate.tips.title') }}</p>
              <ul>
                <li>{{ t('affiliate.tips.line1') }}</li>
                <li>{{ t('affiliate.tips.line2', { rate: `${formattedRebateRate}%` }) }}</li>
                <li>{{ t('affiliate.tips.line3') }}</li>
              </ul>
            </div>

            <div class="activity-panel affiliate-table-panel">
              <div class="panel-title-row">
                <div>
                  <h2>{{ t('affiliate.invitees.title') }}</h2>
                  <p>{{ t('affiliate.description') }}</p>
                </div>
              </div>
              <ActivityEmpty v-if="affiliateDetail.invitees.length === 0" :text="t('affiliate.invitees.empty')" />
              <div v-else class="table-wrap">
                <table class="activity-table min-w-[920px]">
                  <thead>
                    <tr>
                      <th>{{ t('affiliate.invitees.columns.user') }}</th>
                      <th>{{ t('affiliate.invitees.columns.bindSource') }}</th>
                      <th>
                        <button type="button" class="table-sort" @click="toggleSort('bound_at')">
                          {{ t('affiliate.invitees.columns.joinedAt') }}
                          <span>{{ sortIndicator('bound_at') }}</span>
                        </button>
                      </th>
                      <th>{{ t('affiliate.invitees.columns.status') }}</th>
                      <th class="text-right">
                        <button type="button" class="table-sort justify-end" @click="toggleSort('period_consumption')">
                          {{ t('affiliate.invitees.columns.periodConsumption') }}
                          <span>{{ sortIndicator('period_consumption') }}</span>
                        </button>
                      </th>
                      <th class="text-right">
                        <button type="button" class="table-sort justify-end" @click="toggleSort('period_rebate')">
                          {{ t('affiliate.invitees.columns.periodRebate') }}
                          <span>{{ sortIndicator('period_rebate') }}</span>
                        </button>
                      </th>
                      <th class="text-right">
                        <button type="button" class="table-sort justify-end" @click="toggleSort('history_consumption')">
                          {{ t('affiliate.invitees.columns.historyConsumption') }}
                          <span>{{ sortIndicator('history_consumption') }}</span>
                        </button>
                      </th>
                      <th class="text-right">
                        <button type="button" class="table-sort justify-end" @click="toggleSort('total_rebate')">
                          {{ t('affiliate.invitees.columns.rebate') }}
                          <span>{{ sortIndicator('total_rebate') }}</span>
                        </button>
                      </th>
                    </tr>
                  </thead>
                  <tbody>
                    <tr v-for="item in sortedInvitees" :key="item.user_id">
                      <td>
                        <div class="font-medium text-gray-900 dark:text-white">{{ item.email || '-' }}</div>
                        <div class="mt-0.5 text-xs text-gray-500 dark:text-dark-400">{{ item.username || '-' }}</div>
                      </td>
                      <td>{{ formatBindSource(item.invite_bind_source) }}</td>
                      <td>{{ formatDateTime(item.created_at) || '-' }}</td>
                      <td>{{ formatInviteeStatus(item.status) }}</td>
                      <td class="text-right">{{ formatCurrency(item.period_consumption) }}</td>
                      <td class="text-right font-semibold text-emerald-600 dark:text-emerald-400">{{ formatCurrency(item.period_rebate) }}</td>
                      <td class="text-right">{{ formatCurrency(item.history_consumption) }}</td>
                      <td class="text-right font-semibold text-gray-900 dark:text-white">{{ formatCurrency(item.total_rebate) }}</td>
                    </tr>
                  </tbody>
                </table>
              </div>
            </div>
          </template>
        </section>
      </template>
    </div>

    <Teleport to="body">
      <div v-if="claimDialogOpen && selectedWinner" class="fixed inset-0 z-50 flex items-center justify-center bg-black/50 p-4" @click.self="claimDialogOpen = false">
        <form class="w-full max-w-lg rounded-lg bg-white p-5 shadow-xl dark:bg-dark-900" @submit.prevent="submitClaim">
          <h2 class="text-lg font-semibold text-gray-900 dark:text-white">{{ t('activities.claim.title') }}</h2>
          <p class="mt-1 text-sm text-gray-500 dark:text-dark-400">{{ selectedWinner.prize_name }}</p>
          <div class="mt-5 space-y-4">
            <template v-if="selectedWinner.claim_fields?.length">
              <div v-for="field in selectedWinner.claim_fields" :key="field.key">
                <label class="input-label">
                  {{ field.label }}
                  <span v-if="field.required" class="text-red-500">*</span>
                </label>
                <textarea v-if="field.type === 'textarea'" v-model.trim="claimForm[field.key]" class="input min-h-24" :required="field.required"></textarea>
                <input v-else v-model.trim="claimForm[field.key]" class="input" :type="field.type === 'phone' ? 'tel' : 'text'" :required="field.required" />
              </div>
            </template>
            <div v-else class="rounded-lg border border-amber-200 bg-amber-50 p-3 text-sm text-amber-900 dark:border-amber-900/50 dark:bg-amber-950/30 dark:text-amber-200">
              {{ t('activities.claim.noFields') }}
            </div>
          </div>
          <div class="mt-5 flex justify-end gap-2">
            <button type="button" class="btn btn-secondary" @click="claimDialogOpen = false">{{ t('common.cancel') }}</button>
            <button type="submit" class="btn btn-primary" :disabled="claimSubmitting">{{ claimSubmitting ? t('common.saving') : t('activities.claim.submit') }}</button>
          </div>
        </form>
      </div>
    </Teleport>
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, defineComponent, h, onMounted, reactive, ref, type PropType } from 'vue'
import { useI18n } from 'vue-i18n'
import AppLayout from '@/components/layout/AppLayout.vue'
import Icon from '@/components/icons/Icon.vue'
import { useAppStore } from '@/stores/app'
import userAPI from '@/api/user'
import { activityAPI } from '@/api/activity'
import type { AffiliateInvitee, UserAffiliateDetail } from '@/types'
import type { ActivityCampaign, ActivityMetric, ActivityPrizeType, ActivityWinner, ActivityWinnerPublic, ActivityWinnerStatus } from '@/types/activity'
import { useClipboard } from '@/composables/useClipboard'
import { extractApiErrorMessage } from '@/utils/apiError'
import { formatCurrency, formatDateTime } from '@/utils/format'

type ActivityTab = 'open' | 'joined' | 'winners' | 'past' | 'affiliate'
type SortKey = 'bound_at' | 'period_consumption' | 'period_rebate' | 'history_consumption' | 'total_rebate'

const { t } = useI18n()
const appStore = useAppStore()
const { copyToClipboard } = useClipboard()

const loading = ref(true)
const affiliateLoading = ref(false)
const claimSubmitting = ref(false)
const claimDialogOpen = ref(false)
const activeTab = ref<ActivityTab>('open')
const campaigns = ref<ActivityCampaign[]>([])
const winners = ref<ActivityWinner[]>([])
const affiliateDetail = ref<UserAffiliateDetail | null>(null)
const affiliateError = ref('')
const selectedWinner = ref<ActivityWinner | null>(null)
const joiningCampaignIds = ref<Set<number>>(new Set())
const claimForm = reactive<Record<string, string>>({})

const periodPresets = ['today', 'yesterday', 'last7'] as const
type PeriodPreset = typeof periodPresets[number] | 'custom'
const periodPreset = ref<PeriodPreset>('today')
const periodStartDate = ref(toDateInputValue(startOfLocalDay(new Date())))
const periodEndDate = ref(toDateInputValue(startOfLocalDay(new Date())))
const sortKey = ref<SortKey>('bound_at')
const sortDirection = ref<'asc' | 'desc'>('desc')

const openCampaigns = computed(() => campaigns.value.filter(campaign => !isCampaignEnded(campaign)))
const joinedCampaigns = computed(() => openCampaigns.value.filter(campaign => campaign.user_progress?.joined))
const pastCampaigns = computed(() => campaigns.value.filter(isCampaignEnded))
const pendingClaimWinners = computed(() => winners.value.filter(item => item.status === 'pending_claim'))
const availableTicketCount = computed(() => openCampaigns.value.reduce((sum, item) => sum + (item.user_progress?.ticket_count || 0), 0))
const joinedTicketCount = computed(() => campaigns.value.reduce((sum, item) => sum + (item.user_progress?.joined_tickets || 0), 0))
const tabs = computed(() => [
  { key: 'open' as const, label: t('activities.tabs.open'), count: openCampaigns.value.length },
  { key: 'joined' as const, label: t('activities.tabs.joined'), count: joinedCampaigns.value.length },
  { key: 'winners' as const, label: t('activities.tabs.winners'), count: winners.value.length },
  { key: 'past' as const, label: t('activities.tabs.past'), count: pastCampaigns.value.length },
  { key: 'affiliate' as const, label: t('activities.tabs.affiliate'), count: affiliateDetail.value?.aff_count ?? 0 },
])
const formattedRebateRate = computed(() => {
  const v = affiliateDetail.value?.effective_rebate_rate_percent ?? 0
  const rounded = Math.round(v * 100) / 100
  return Number.isInteger(rounded) ? String(rounded) : rounded.toString()
})
const inviteLink = computed(() => {
  if (!affiliateDetail.value) return ''
  if (typeof window === 'undefined') return `/register?aff=${encodeURIComponent(affiliateDetail.value.aff_code)}`
  return `${window.location.origin}/register?aff=${encodeURIComponent(affiliateDetail.value.aff_code)}`
})
const periodIncomeTitle = computed(() => {
  if (periodPreset.value === 'today') return t('affiliate.stats.todayQuota')
  if (periodPreset.value === 'yesterday') return t('affiliate.stats.yesterdayQuota')
  if (periodPreset.value === 'last7') return t('affiliate.stats.last7Quota')
  return t('affiliate.stats.periodQuota')
})
const weeklyQuotaText = computed(() => {
  const used = affiliateDetail.value?.aff_weekly_used ?? 0
  const limit = affiliateDetail.value?.aff_weekly_limit ?? 0
  const remaining = affiliateDetail.value?.aff_weekly_remaining ?? Math.max(0, limit - used)
  return t('affiliate.weeklyQuotaText', { used, limit, remaining })
})
const codePolicyText = computed(() => {
  if (!affiliateDetail.value) return ''
  return affiliateDetail.value.aff_code_auto_rotate
    ? t('affiliate.codePolicy.rotateWeekly')
    : t('affiliate.codePolicy.keepCode')
})
const codeExpiryText = computed(() => {
  if (!affiliateDetail.value) return ''
  if (!affiliateDetail.value.aff_code_auto_rotate) return t('affiliate.codePolicy.noExpiry')
  const expiresAt = formatDateTime(affiliateDetail.value.aff_code_expires_at)
  return expiresAt
    ? t('affiliate.codePolicy.expiresAt', { time: expiresAt })
    : t('affiliate.codePolicy.expiresUnknown')
})
const sortedInvitees = computed(() => {
  const rows = [...(affiliateDetail.value?.invitees ?? [])]
  const direction = sortDirection.value === 'asc' ? 1 : -1
  return rows.sort((a, b) => {
    const left = sortableValue(a, sortKey.value)
    const right = sortableValue(b, sortKey.value)
    if (left === right) return 0
    return left > right ? direction : -direction
  })
})

async function loadAll(): Promise<void> {
  loading.value = true
  try {
    const [activityItems, winnerItems] = await Promise.all([
      activityAPI.listWelfareActivities(),
      activityAPI.listMyWinners(),
    ])
    campaigns.value = activityItems
    winners.value = winnerItems
    await loadAffiliateDetail(true)
  } catch (error) {
    appStore.showError(extractApiErrorMessage(error, t('activities.loadFailed')))
  } finally {
    loading.value = false
  }
}

async function loadAffiliateDetail(silent = false): Promise<void> {
  if (!silent) affiliateLoading.value = true
  affiliateError.value = ''
  try {
    affiliateDetail.value = await userAPI.getAffiliateDetail(buildPeriodParams())
  } catch (error) {
    affiliateDetail.value = null
    affiliateError.value = extractApiErrorMessage(error, t('affiliate.loadFailed'))
  } finally {
    if (!silent) affiliateLoading.value = false
  }
}

async function joinCampaign(campaign: ActivityCampaign): Promise<void> {
  if (!canJoinCampaign(campaign)) return
  setJoining(campaign.id, true)
  try {
    const progress = await activityAPI.joinDraw(campaign.id)
    campaigns.value = campaigns.value.map(item => item.id === campaign.id ? { ...item, user_progress: progress } : item)
    appStore.showSuccess(t(progress.joined_tickets > (campaign.user_progress?.joined_tickets || 0) ? 'activities.lottery.joinSuccess' : 'activities.lottery.joined'))
  } catch (error) {
    appStore.showError(extractApiErrorMessage(error, t('activities.lottery.joinFailed')))
  } finally {
    setJoining(campaign.id, false)
  }
}

function canJoinCampaign(campaign: ActivityCampaign): boolean {
  const progress = campaign.user_progress
  if (!progress || progress.ticket_count <= 0) return false
  if (isCampaignEnded(campaign) || isDrawClosed(campaign) || isJoining(campaign.id)) return false
  if (!progress.joined) return true
  return canUpdateJoinedTickets(campaign)
}

function canUpdateJoinedTickets(campaign: ActivityCampaign): boolean {
  const progress = campaign.user_progress
  if (!progress || !progress.joined) return false
  return !isCampaignEnded(campaign) && !isDrawClosed(campaign) && progress.ticket_count > progress.joined_tickets
}

function joinButtonLabel(campaign: ActivityCampaign): string {
  if (isJoining(campaign.id)) return t('activities.lottery.joining')
  const progress = campaign.user_progress
  if (!progress || progress.ticket_count <= 0) return t('activities.lottery.notQualified')
  if (progress.joined && progress.ticket_count <= progress.joined_tickets) return t('activities.lottery.joined')
  if (progress.joined) return t('activities.lottery.updateJoin')
  return t('activities.lottery.joinNow')
}

function participationStatusText(campaign: ActivityCampaign): string {
  const progress = campaign.user_progress
  if (progress?.joined) return t('activities.participation.joined')
  if ((progress?.ticket_count || 0) > 0) return t('activities.participation.qualified')
  return t('activities.participation.unqualified')
}

function participationBadgeClass(campaign: ActivityCampaign): string {
  const progress = campaign.user_progress
  if (progress?.joined) return 'badge-success'
  if ((progress?.ticket_count || 0) > 0) return 'badge-warning'
  return 'badge-gray'
}

function publicParticipantText(campaign: ActivityCampaign): string {
  const stats = campaign.public_stats
  if (!stats || stats.participant_count_mode === 'off') return ''
  if (stats.participant_count_mode === 'exact') {
    return t('activities.publicStats.exactParticipants', { count: stats.participant_count || 0 })
  }
  if (!stats.participant_count_bucket) return ''
  return t('activities.publicStats.fuzzyParticipants', { count: stats.participant_count_bucket })
}

function setJoining(id: number, joining: boolean): void {
  const next = new Set(joiningCampaignIds.value)
  if (joining) next.add(id)
  else next.delete(id)
  joiningCampaignIds.value = next
}

function isJoining(id: number): boolean {
  return joiningCampaignIds.value.has(id)
}

function isCampaignEnded(campaign: ActivityCampaign): boolean {
  if (campaign.status === 'ended') return true
  const endAt = new Date(campaign.ends_at).getTime()
  return Number.isFinite(endAt) && endAt < Date.now()
}

function isDrawClosed(campaign: ActivityCampaign): boolean {
  if (!campaign.draw_at) return true
  const drawAt = new Date(campaign.draw_at).getTime()
  return !Number.isFinite(drawAt) || drawAt <= Date.now()
}

function progressPercent(campaign: ActivityCampaign): number {
  const current = campaign.user_progress?.metric_value || 0
  const threshold = campaign.rule_config.threshold || 0
  if (threshold <= 0) return 0
  return Math.max(0, Math.min(100, Math.round((current / threshold) * 100)))
}

function metricLabel(metric: ActivityMetric): string {
  return metric === 'api_request_count' ? t('activities.metrics.apiRequestCount') : t('activities.metrics.apiCostAmount')
}

function metricValueText(metric: ActivityMetric, value: number): string {
  if (metric === 'api_request_count') return t('activities.metrics.requestValue', { count: Math.floor(value || 0) })
  return formatCurrency(value || 0)
}

function formatShortDateTime(value?: string | Date | null): string {
  if (!value) return '-'
  const date = new Date(value)
  if (!Number.isFinite(date.getTime())) return '-'
  const month = String(date.getMonth() + 1).padStart(2, '0')
  const day = String(date.getDate()).padStart(2, '0')
  const hour = String(date.getHours()).padStart(2, '0')
  const minute = String(date.getMinutes()).padStart(2, '0')
  return `${month}/${day} ${hour}:${minute}`
}

function periodLabel(campaign: ActivityCampaign): string {
  const rule = campaign.rule_config
  if (rule.period_type === 'today') return t('activities.periodTypes.today')
  if (rule.period_type === 'rolling_days') return t('activities.periodTypes.rollingDays', { days: rule.rolling_days || 1 })
  if (rule.period_type === 'campaign') return t('activities.periodTypes.campaign')
  const start = formatDateTime(rule.period_start_at)
  const end = formatDateTime(rule.period_end_at)
  return start && end ? `${start} - ${end}` : t('activities.periodTypes.fixedRange')
}

function prizeTypeLabel(type: ActivityPrizeType): string {
  return t(`activities.prizeTypes.${type}`)
}

function prizeAmountText(type: ActivityPrizeType, amount: number): string {
  if (type === 'manual') return t('activities.prizes.manual')
  if (type === 'points') return t('activities.prizes.pointsAmount', { count: amount })
  if (type === 'load_factor_credits') return t('activities.prizes.loadCreditsAmount', { count: amount })
  return formatCurrency(amount)
}

function winnerStatusLabel(status: ActivityWinnerStatus): string {
  return t(`activities.winnerStatus.${status}`)
}

function winnerStatusBadgeClass(status: ActivityWinnerStatus): string {
  if (status === 'delivered') return 'badge-success'
  if (status === 'pending_claim' || status === 'pending_delivery') return 'badge-warning'
  if (status === 'rejected' || status === 'expired') return 'badge-danger'
  return 'badge-gray'
}

function openClaim(winner: ActivityWinner): void {
  selectedWinner.value = winner
  for (const key of Object.keys(claimForm)) delete claimForm[key]
  for (const field of winner.claim_fields || []) {
    claimForm[field.key] = String((winner.claim_info?.[field.key] as string | undefined) || '')
  }
  claimDialogOpen.value = true
}

async function submitClaim(): Promise<void> {
  if (!selectedWinner.value) return
  claimSubmitting.value = true
  try {
    const payload: Record<string, string> = {}
    for (const field of selectedWinner.value.claim_fields || []) {
      payload[field.key] = claimForm[field.key]?.trim() || ''
    }
    const updated = await activityAPI.submitWinnerClaim(selectedWinner.value.id, payload)
    winners.value = winners.value.map(item => item.id === updated.id ? updated : item)
    appStore.showSuccess(t('activities.claim.success'))
    claimDialogOpen.value = false
  } catch (error) {
    appStore.showError(extractApiErrorMessage(error, t('activities.claim.failed')))
  } finally {
    claimSubmitting.value = false
  }
}

async function copyCode(): Promise<void> {
  if (!affiliateDetail.value?.aff_code) return
  await copyToClipboard(affiliateDetail.value.aff_code, t('affiliate.codeCopied'))
}

async function copyInviteLink(): Promise<void> {
  if (!inviteLink.value) return
  await copyToClipboard(inviteLink.value, t('affiliate.linkCopied'))
}

function buildPeriodParams(): { period_start_at?: string; period_end_at?: string } {
  const start = parseDateInputStart(periodStartDate.value)
  const end = parseDateInputStart(periodEndDate.value)
  return {
    period_start_at: start?.toISOString(),
    period_end_at: end ? addDays(end, 1).toISOString() : undefined,
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
  void loadAffiliateDetail()
}

function setCustomPeriod(): void {
  periodPreset.value = 'custom'
  const start = parseDateInputStart(periodStartDate.value)
  const end = parseDateInputStart(periodEndDate.value)
  if (!start || !end || start > end) {
    appStore.showError(t('affiliate.period.invalid'))
    return
  }
  void loadAffiliateDetail()
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

function formatCount(value: number): string {
  return value.toLocaleString()
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

const ActivityEmpty = defineComponent({
  props: { text: { type: String, required: true } },
  setup(props) {
    return () => h('div', { class: 'activity-empty' }, props.text)
  },
})

const InfoCell = defineComponent({
  props: {
    label: { type: String, required: true },
    value: { type: String, required: true },
  },
  setup(props) {
    return () => h('div', { class: 'info-cell' }, [
      h('span', props.label),
      h('strong', props.value),
    ])
  },
})

const PrizeList = defineComponent({
  props: {
    campaign: { type: Object as PropType<ActivityCampaign>, required: true },
  },
  setup(props) {
    return () => h('div', { class: 'side-box' }, [
      h('h3', t('activities.prizes.title')),
      ...(props.campaign.prizes?.length
        ? props.campaign.prizes.map(prize => h('div', { class: 'side-row', key: prize.id || prize.name }, [
          h('div', [h('p', prize.name), h('span', `${prizeTypeLabel(prize.prize_type)} · ${t('activities.prizes.quantity', { count: prize.quantity })}`)]),
          h('strong', prizeAmountText(prize.prize_type, prize.amount)),
        ]))
        : [h('p', { class: 'side-empty' }, t('activities.prizes.empty'))]),
    ])
  },
})

const WinnerList = defineComponent({
  props: {
    title: { type: String, required: true },
    empty: { type: String, required: true },
    winners: { type: Array as PropType<ActivityWinnerPublic[]>, required: true },
  },
  setup(props) {
    return () => h('div', { class: 'side-box' }, [
      h('h3', props.title),
      ...(props.winners.length
        ? props.winners.slice(0, 8).map(winner => h('div', { class: 'winner-row', key: winner.id }, [
          h('span', winner.masked_user),
          h('strong', winner.prize_name),
        ]))
        : [h('p', { class: 'side-empty' }, props.empty)]),
    ])
  },
})

const CopyBox = defineComponent({
  props: {
    label: { type: String, required: true },
    value: { type: String, required: true },
    buttonText: { type: String, required: true },
  },
  emits: ['copy'],
  setup(props, { emit }) {
    return () => h('div', { class: 'copy-card' }, [
      h('p', props.label),
      h('div', { class: 'copy-box' }, [
        h('code', props.value),
        h('button', { class: 'btn btn-secondary btn-sm', type: 'button', onClick: () => emit('copy') }, props.buttonText),
      ]),
    ])
  },
})

onMounted(() => {
  void loadAll()
})
</script>

<style scoped>
.activity-page {
  width: 100%;
  max-width: 1320px;
  margin: 0 auto;
  padding: 0.875rem clamp(1rem, 2vw, 1.5rem) 2rem;
  display: flex;
  flex-direction: column;
  gap: 0.875rem;
}

.activity-card h2,
.activity-panel h2 {
  margin: 0;
  color: rgb(15 23 42);
  font-weight: 750;
  letter-spacing: 0;
}

.activity-card-head p,
.activity-panel > p,
.past-row p,
.panel-title-row p {
  margin-top: 0.375rem;
  color: rgb(71 85 105);
  font-size: 0.875rem;
  line-height: 1.55;
}

.activity-refresh {
  min-height: 3.5rem;
  align-self: stretch;
  white-space: nowrap;
}

.activity-loading {
  display: flex;
  justify-content: center;
  padding: 3rem 0;
}

.activity-loading.compact {
  padding: 2rem 0;
}

.activity-summary {
  display: grid;
  grid-template-columns: repeat(4, minmax(0, 1fr)) auto;
  align-items: stretch;
  gap: 0.5rem;
  border: 1px solid rgb(226 232 240);
  border-radius: 0.5rem;
  background: rgba(255, 255, 255, 0.96);
  padding: 0.5rem;
  box-shadow: 0 10px 26px rgba(15, 23, 42, 0.04);
}

.metric-tile,
.activity-panel,
.activity-tabs,
.activity-card {
  border: 1px solid rgb(226 232 240);
  border-radius: 0.5rem;
  background: rgba(255, 255, 255, 0.96);
  box-shadow: 0 10px 28px rgba(15, 23, 42, 0.045);
}

.metric-tile {
  position: relative;
  min-height: 3.5rem;
  overflow: hidden;
  border: 0;
  background: rgb(248 250 252);
  box-shadow: none;
  padding: 0.625rem 0.75rem;
}

.metric-tile svg {
  position: absolute;
  right: 1rem;
  top: 1rem;
  opacity: 0.36;
}

.metric-tile span,
.info-cell span {
  display: block;
  font-size: 0.75rem;
  color: rgb(100 116 139);
}

.metric-tile strong {
  display: block;
  margin-top: 0.125rem;
  color: rgb(15 23 42);
  font-size: 1.375rem;
  line-height: 1.625rem;
  font-weight: 760;
}

.activity-tabs {
  display: flex;
  gap: 0.375rem;
  overflow-x: auto;
  padding: 0.375rem;
}

.activity-tab {
  display: inline-flex;
  min-height: 2.75rem;
  flex: 0 0 auto;
  align-items: center;
  gap: 0.5rem;
  border-radius: 0.5rem;
  padding: 0 0.925rem;
  color: rgb(71 85 105);
  font-size: 0.875rem;
  font-weight: 650;
  transition: background 0.18s ease, color 0.18s ease, box-shadow 0.18s ease;
  cursor: pointer;
}

.activity-tab:hover {
  background: rgb(248 250 252);
  color: rgb(15 23 42);
}

.activity-tab em {
  min-width: 1.45rem;
  border-radius: 9999px;
  background: rgb(241 245 249);
  padding: 0.125rem 0.375rem;
  color: rgb(100 116 139);
  font-style: normal;
  font-size: 0.75rem;
  text-align: center;
}

.activity-tab-active {
  background: rgb(37 99 235);
  color: white;
  box-shadow: 0 8px 18px rgba(37, 99, 235, 0.22);
}

.activity-tab-active:hover {
  background: rgb(37 99 235);
  color: white;
}

.activity-tab-active em {
  background: rgba(255, 255, 255, 0.22);
  color: white;
}

.activity-section {
  display: flex;
  flex-direction: column;
  gap: 0.75rem;
}

.activity-card {
  display: grid;
  grid-template-columns: minmax(0, 1fr) 300px;
  gap: 0.875rem;
  padding: 0.875rem;
}

.activity-card-main {
  min-width: 0;
  display: flex;
  flex-direction: column;
  gap: 0.75rem;
}

.activity-card-aside {
  display: grid;
  gap: 0.875rem;
  align-content: start;
}

.activity-card-head,
.joined-row,
.past-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 1rem;
}

.activity-card h2,
.activity-panel h2 {
  font-size: 1rem;
  line-height: 1.5rem;
}

.activity-meta-line {
  margin-top: 0.375rem;
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 0.375rem 0.625rem;
  color: rgb(100 116 139);
  font-size: 0.8125rem;
}

.activity-meta-line span:first-child {
  color: rgb(15 23 42);
  font-weight: 750;
}

.activity-head-actions {
  display: flex;
  flex: 0 0 auto;
  align-items: center;
  gap: 0.625rem;
}

.activity-ticket-box {
  min-width: 5.5rem;
  border: 1px solid rgb(219 234 254);
  border-radius: 0.5rem;
  background: rgb(248 250 252);
  padding: 0.5rem 0.625rem;
  text-align: right;
}

.activity-ticket-box span {
  display: block;
  font-size: 0.75rem;
  color: rgb(29 78 216);
}

.activity-ticket-box strong {
  display: block;
  color: rgb(30 64 175);
  font-size: 1.25rem;
  line-height: 1.5rem;
  font-weight: 800;
}

.activity-progress-row {
  display: grid;
  grid-template-columns: minmax(0, 1fr) 3rem;
  align-items: center;
  gap: 0.625rem;
}

.activity-progress-row span {
  color: rgb(100 116 139);
  font-size: 0.75rem;
  text-align: right;
}

.activity-stepper {
  display: grid;
  gap: 0.5rem;
  grid-template-columns: repeat(3, minmax(0, 1fr));
}

.activity-step {
  display: flex;
  min-height: 2.5rem;
  align-items: center;
  gap: 0.5rem;
  border: 0;
  border-radius: 0.5rem;
  background: rgb(248 250 252);
  padding: 0.5rem 0.625rem;
  color: rgb(71 85 105);
}

.activity-step span {
  display: inline-flex;
  height: 1.5rem;
  width: 1.5rem;
  flex: 0 0 auto;
  align-items: center;
  justify-content: center;
  border-radius: 9999px;
  background: rgb(226 232 240);
  font-size: 0.75rem;
  font-weight: 800;
}

.activity-step p {
  margin: 0;
  font-size: 0.8125rem;
  font-weight: 700;
}

.activity-step-done {
  border-color: rgb(167 243 208);
  background: rgb(236 253 245);
  color: rgb(4 120 87);
}

.activity-step-done span {
  background: rgb(16 185 129);
  color: white;
}

.info-grid {
  display: grid;
  grid-template-columns: repeat(4, minmax(0, 1fr));
  gap: 0.5rem;
}

.info-cell {
  min-width: 0;
  border: 1px solid rgb(226 232 240);
  border-radius: 0.5rem;
  background: white;
  padding: 0.625rem;
}

.info-cell strong {
  display: block;
  min-height: 1.25rem;
  margin-top: 0.25rem;
  color: rgb(15 23 42);
  font-size: 0.8125rem;
  font-weight: 750;
  overflow-wrap: anywhere;
}

.progress-track {
  height: 0.375rem;
  overflow: hidden;
  border-radius: 9999px;
  background: rgb(226 232 240);
}

.progress-fill {
  height: 100%;
  border-radius: inherit;
  background: linear-gradient(90deg, rgb(37 99 235), rgb(16 185 129));
  transition: width 0.22s ease;
}

.joined-update span {
  color: rgb(100 116 139);
  font-size: 0.75rem;
}

.activity-panel {
  padding: 1.125rem;
}

.activity-empty {
  border: 1px dashed rgb(203 213 225);
  border-radius: 0.5rem;
  padding: 2.5rem 1rem;
  color: rgb(100 116 139);
  text-align: center;
  font-size: 0.875rem;
}

.joined-row {
  flex-wrap: wrap;
}

.joined-cells {
  display: grid;
  width: min(100%, 620px);
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 0.75rem;
}

.joined-row p {
  margin-top: 0.375rem;
  color: rgb(100 116 139);
  font-size: 0.875rem;
}

.joined-update {
  width: 100%;
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 0.625rem;
}

.past-row {
  display: grid;
  grid-template-columns: minmax(0, 1fr) minmax(300px, 0.36fr);
}

.side-box {
  border: 1px solid rgb(226 232 240);
  border-radius: 0.5rem;
  background: white;
  padding: 0.75rem;
}

.side-box h3 {
  margin: 0;
  color: rgb(15 23 42);
  font-size: 0.875rem;
  font-weight: 800;
}

.side-row,
.winner-row {
  margin-top: 0.5rem;
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 0.75rem;
  border-top: 1px solid rgb(241 245 249);
  border-radius: 0;
  background: transparent;
  padding: 0.5rem 0 0;
}

.side-row p,
.side-row strong,
.winner-row strong {
  color: rgb(15 23 42);
  font-size: 0.8125rem;
  font-weight: 750;
}

.side-row span,
.winner-row span,
.side-empty {
  color: rgb(100 116 139);
  font-size: 0.75rem;
}

.panel-title-row {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 1rem;
  margin-bottom: 1rem;
}

.table-wrap {
  width: 100%;
  overflow-x: auto;
}

.activity-table {
  width: 100%;
  border-collapse: separate;
  border-spacing: 0;
  text-align: left;
  font-size: 0.875rem;
}

.activity-table th {
  border-bottom: 1px solid rgb(226 232 240);
  padding: 0.75rem;
  color: rgb(100 116 139);
  font-weight: 700;
}

.activity-table td {
  border-bottom: 1px solid rgb(241 245 249);
  padding: 0.875rem 0.75rem;
  color: rgb(51 65 85);
}

.activity-table tbody tr:last-child td {
  border-bottom: 0;
}

.affiliate-grid {
  display: grid;
  grid-template-columns: minmax(0, 1fr) 360px;
  gap: 1rem;
}

.affiliate-overview,
.affiliate-table-panel {
  grid-column: 1 / -1;
}

.affiliate-metrics {
  display: grid;
  gap: 0.75rem;
  grid-template-columns: repeat(5, minmax(0, 1fr));
}

.affiliate-metric {
  min-height: 6rem;
  border: 1px solid rgb(226 232 240);
  border-radius: 0.5rem;
  background: rgb(248 250 252);
  padding: 0.875rem;
}

.affiliate-metric.primary {
  background: linear-gradient(135deg, rgb(239 246 255), rgb(236 253 245));
}

.affiliate-metric span,
.affiliate-policy-grid span {
  display: block;
  color: rgb(100 116 139);
  font-size: 0.75rem;
}

.affiliate-metric strong,
.affiliate-policy-grid strong {
  display: block;
  margin-top: 0.45rem;
  color: rgb(15 23 42);
  font-size: 1rem;
  font-weight: 800;
  overflow-wrap: anywhere;
}

.affiliate-metric.primary strong {
  color: rgb(37 99 235);
  font-size: 1.75rem;
  line-height: 2rem;
}

.affiliate-metric p,
.affiliate-policy-grid p {
  margin-top: 0.375rem;
  color: rgb(100 116 139);
  font-size: 0.75rem;
  line-height: 1.45;
}

.period-control {
  display: flex;
  flex-wrap: wrap;
  gap: 0.375rem;
}

.period-button {
  min-height: 2.25rem;
  border-radius: 0.5rem;
  padding: 0 0.75rem;
  color: rgb(71 85 105);
  font-size: 0.8125rem;
  font-weight: 700;
  transition: background 0.18s ease, color 0.18s ease;
  cursor: pointer;
}

.period-button:hover {
  background: rgb(248 250 252);
}

.period-button-active {
  background: rgb(219 234 254);
  color: rgb(29 78 216);
}

.affiliate-period-row {
  margin-top: 1rem;
  display: grid;
  max-width: 460px;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 0.75rem;
}

.affiliate-share,
.affiliate-tips {
  align-self: start;
}

.copy-section {
  display: grid;
  gap: 0.875rem;
}

.copy-card p {
  margin-bottom: 0.375rem;
  color: rgb(51 65 85);
  font-size: 0.875rem;
  font-weight: 700;
}

.copy-box {
  display: flex;
  align-items: center;
  gap: 0.5rem;
  border: 1px solid rgb(226 232 240);
  border-radius: 0.5rem;
  background: rgb(248 250 252);
  padding: 0.5rem 0.625rem;
}

.copy-box code {
  min-width: 0;
  flex: 1 1 auto;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  color: rgb(15 23 42);
  font-size: 0.875rem;
}

.affiliate-policy-grid {
  margin-top: 0.875rem;
  display: grid;
  gap: 0.75rem;
  grid-template-columns: repeat(2, minmax(0, 1fr));
}

.affiliate-policy-grid > div {
  border: 1px solid rgb(226 232 240);
  border-radius: 0.5rem;
  background: rgb(248 250 252);
  padding: 0.75rem;
}

.affiliate-tips {
  border-color: rgb(187 247 208);
  background: linear-gradient(135deg, rgb(240 253 244), rgb(255 255 255));
}

.affiliate-tips p {
  color: rgb(22 101 52);
  font-weight: 800;
}

.affiliate-tips ul {
  margin-top: 0.625rem;
  display: grid;
  gap: 0.375rem;
  color: rgb(21 128 61);
  font-size: 0.875rem;
  line-height: 1.55;
}

.table-sort {
  display: inline-flex;
  width: 100%;
  align-items: center;
  gap: 0.25rem;
  color: inherit;
  cursor: pointer;
}

.table-sort:hover {
  color: rgb(15 23 42);
}

.dark .activity-summary,
.dark .metric-tile,
.dark .activity-panel,
.dark .activity-tabs,
.dark .activity-card {
  border-color: rgb(51 65 85);
  background: rgb(15 23 42);
  box-shadow: none;
}

.dark .activity-card h2,
.dark .activity-panel h2,
.dark .activity-meta-line span:first-child,
.dark .metric-tile strong,
.dark .info-cell strong,
.dark .side-box h3,
.dark .side-row p,
.dark .side-row strong,
.dark .winner-row strong,
.dark .affiliate-metric strong,
.dark .affiliate-policy-grid strong,
.dark .copy-box code {
  color: white;
}

.dark .activity-card-head p,
.dark .activity-meta-line,
.dark .activity-panel > p,
.dark .past-row p,
.dark .panel-title-row p,
.dark .metric-tile span,
.dark .info-cell span,
.dark .joined-update span,
.dark .side-row span,
.dark .winner-row span,
.dark .side-empty,
.dark .activity-empty,
.dark .affiliate-metric span,
.dark .affiliate-metric p,
.dark .affiliate-policy-grid span,
.dark .affiliate-policy-grid p {
  color: rgb(148 163 184);
}

.dark .info-cell,
.dark .side-box,
.dark .activity-ticket-box,
.dark .activity-step,
.dark .affiliate-metric,
.dark .affiliate-policy-grid > div,
.dark .copy-box {
  border-color: rgb(51 65 85);
  background: rgb(30 41 59);
}

.dark .side-row,
.dark .winner-row {
  border-color: rgb(30 41 59);
  background: transparent;
}

.dark .activity-tab {
  color: rgb(203 213 225);
}

.dark .activity-tab:hover {
  background: rgb(30 41 59);
  color: white;
}

.dark .activity-tab-active,
.dark .activity-tab-active:hover {
  background: rgb(37 99 235);
}

.dark .activity-table th {
  border-color: rgb(51 65 85);
  color: rgb(148 163 184);
}

.dark .activity-table td {
  border-color: rgb(30 41 59);
  color: rgb(203 213 225);
}

.dark .affiliate-tips {
  border-color: rgba(34, 197, 94, 0.35);
  background: rgba(20, 83, 45, 0.22);
}

.dark .affiliate-tips p,
.dark .affiliate-tips ul {
  color: rgb(187 247 208);
}

@media (max-width: 1280px) {
  .activity-summary,
  .affiliate-metrics {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }

  .activity-refresh {
    grid-column: 1 / -1;
  }

  .activity-card,
  .past-row,
  .affiliate-grid {
    grid-template-columns: 1fr;
  }

  .activity-card-aside {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }
}

@media (max-width: 768px) {
  .activity-page {
    padding-inline: 0.75rem;
  }

  .activity-card-head,
  .joined-row,
  .panel-title-row {
    flex-direction: column;
    align-items: stretch;
  }

  .activity-summary,
  .affiliate-metrics,
  .info-grid,
  .joined-cells,
  .activity-stepper,
  .activity-card-aside,
  .affiliate-policy-grid,
  .affiliate-period-row {
    grid-template-columns: 1fr;
  }

  .activity-ticket-box {
    text-align: left;
  }

  .activity-head-actions {
    width: 100%;
    justify-content: space-between;
  }
}
</style>
