<template>
  <AppLayout>
    <div class="mx-auto w-full max-w-7xl space-y-5">
      <section class="rounded-lg border border-gray-200 bg-white p-5 dark:border-dark-700 dark:bg-dark-900">
        <div class="flex flex-col gap-4 xl:flex-row xl:items-center xl:justify-between">
          <div>
            <h1 class="text-xl font-semibold text-gray-900 dark:text-white">{{ t('admin.activities.title') }}</h1>
            <p class="mt-1 text-sm text-gray-500 dark:text-dark-400">{{ t('admin.activities.description') }}</p>
          </div>
          <div class="flex flex-wrap items-center gap-2">
            <input v-model.trim="keyword" class="input min-w-56" :placeholder="t('admin.activities.searchPlaceholder')" @input="handleSearch" />
            <select v-model="statusFilter" class="input w-36" @change="reloadCampaigns">
              <option value="">{{ t('admin.activities.allStatus') }}</option>
              <option v-for="option in statusOptions" :key="option.value" :value="option.value">{{ option.label }}</option>
            </select>
            <button type="button" class="btn btn-secondary" :disabled="loading" @click="reloadCampaigns">{{ t('common.refresh') }}</button>
            <button type="button" class="btn btn-primary" @click="openCreate">{{ t('admin.activities.create') }}</button>
          </div>
        </div>
      </section>

      <section class="card overflow-hidden">
        <div class="flex flex-col gap-3 border-b border-gray-100 p-5 dark:border-dark-800 lg:flex-row lg:items-center lg:justify-between">
          <div>
            <h2 class="text-lg font-semibold text-gray-900 dark:text-white">{{ t('admin.activities.progress.title') }}</h2>
            <p class="mt-1 text-sm text-gray-500 dark:text-dark-400">{{ t('admin.activities.progress.description') }}</p>
          </div>
          <div class="flex flex-wrap items-center gap-2">
            <select v-model.number="selectedStatsCampaignId" class="input w-64" @change="loadCampaignStats">
              <option :value="0">{{ t('admin.activities.progress.selectCampaign') }}</option>
              <option v-for="campaign in campaigns" :key="campaign.id" :value="campaign.id">{{ campaign.name }}</option>
            </select>
            <button type="button" class="btn btn-secondary" :disabled="statsLoading || !selectedStatsCampaignId" @click="loadCampaignStats">{{ t('common.refresh') }}</button>
          </div>
        </div>

        <div v-if="statsLoading" class="px-5 py-10 text-center text-sm text-gray-500 dark:text-dark-400">{{ t('common.loading') }}</div>
        <div v-else-if="!campaignStats" class="px-5 py-10 text-center text-sm text-gray-500 dark:text-dark-400">{{ t('admin.activities.progress.empty') }}</div>
        <div v-else class="space-y-4 p-5">
          <div class="flex flex-col gap-3 lg:flex-row lg:items-start lg:justify-between">
            <div>
              <div class="flex flex-wrap items-center gap-2">
                <h3 class="text-base font-semibold text-gray-900 dark:text-white">{{ campaignStats.campaign_name }}</h3>
                <span :class="['badge', statusBadgeClass(campaignStats.status)]">{{ statusLabel(campaignStats.status) }}</span>
                <span :class="['badge', campaignStats.can_run_draw ? 'badge-success' : 'badge-gray']">{{ drawReadinessLabel(campaignStats) }}</span>
              </div>
              <p class="mt-1 text-sm text-gray-500 dark:text-dark-400">
                {{ t('admin.activities.progress.periodRange', { start: formatDateTime(campaignStats.period_start_at), end: formatDateTime(campaignStats.period_end_at) }) }}
              </p>
            </div>
            <div class="grid gap-2 text-sm text-gray-600 dark:text-dark-300 sm:grid-cols-2">
              <span>{{ t('admin.activities.progress.drawAt') }}: {{ formatDateTime(campaignStats.draw_at) || '-' }}</span>
              <span>{{ t('admin.activities.progress.lastJoinedAt') }}: {{ formatDateTime(campaignStats.last_joined_at) || '-' }}</span>
            </div>
          </div>

          <p v-if="campaignStats.no_participant_warning" class="rounded-lg border border-amber-200 bg-amber-50 px-3 py-2 text-sm text-amber-700 dark:border-amber-900/50 dark:bg-amber-950/30 dark:text-amber-300">
            {{ t('admin.activities.progress.noParticipantWarning') }}
          </p>

          <div class="grid gap-3 md:grid-cols-3 xl:grid-cols-6">
            <div class="rounded-lg border border-gray-200 bg-gray-50 p-3 dark:border-dark-700 dark:bg-dark-800">
              <p class="text-xs text-gray-500 dark:text-dark-400">{{ t('admin.activities.progress.metrics.participants') }}</p>
              <p class="mt-1 text-xl font-semibold text-gray-900 dark:text-white">{{ formatCount(campaignStats.joined_user_count) }}</p>
            </div>
            <div class="rounded-lg border border-gray-200 bg-gray-50 p-3 dark:border-dark-700 dark:bg-dark-800">
              <p class="text-xs text-gray-500 dark:text-dark-400">{{ t('admin.activities.progress.metrics.tickets') }}</p>
              <p class="mt-1 text-xl font-semibold text-gray-900 dark:text-white">{{ formatCount(campaignStats.joined_ticket_count) }}</p>
            </div>
            <div class="rounded-lg border border-gray-200 bg-gray-50 p-3 dark:border-dark-700 dark:bg-dark-800">
              <p class="text-xs text-gray-500 dark:text-dark-400">{{ statsMetricLabel }}</p>
              <p class="mt-1 text-xl font-semibold text-gray-900 dark:text-white">{{ statsMetricValueText(campaignStats.joined_metric_total) }}</p>
            </div>
            <div class="rounded-lg border border-gray-200 bg-gray-50 p-3 dark:border-dark-700 dark:bg-dark-800">
              <p class="text-xs text-gray-500 dark:text-dark-400">{{ t('admin.activities.progress.metrics.averageTickets') }}</p>
              <p class="mt-1 text-xl font-semibold text-gray-900 dark:text-white">{{ formatDecimal(campaignStats.average_tickets_per_user) }}</p>
            </div>
            <div class="rounded-lg border border-gray-200 bg-gray-50 p-3 dark:border-dark-700 dark:bg-dark-800">
              <p class="text-xs text-gray-500 dark:text-dark-400">{{ t('admin.activities.progress.metrics.maxTickets') }}</p>
              <p class="mt-1 text-xl font-semibold text-gray-900 dark:text-white">{{ formatCount(campaignStats.max_ticket_count) }}</p>
            </div>
            <div class="rounded-lg border border-gray-200 bg-gray-50 p-3 dark:border-dark-700 dark:bg-dark-800">
              <p class="text-xs text-gray-500 dark:text-dark-400">{{ t('admin.activities.progress.metrics.prizeQuantity') }}</p>
              <p class="mt-1 text-xl font-semibold text-gray-900 dark:text-white">{{ formatCount(campaignStats.prize_total_quantity) }}</p>
            </div>
          </div>

          <div class="grid gap-3 lg:grid-cols-[1.2fr_0.8fr]">
            <div class="rounded-lg border border-gray-200 p-4 dark:border-dark-700">
              <h4 class="text-sm font-semibold text-gray-900 dark:text-white">{{ t('admin.activities.progress.deliveryTitle') }}</h4>
              <div class="mt-3 grid gap-2 sm:grid-cols-5">
                <div class="text-sm">
                  <p class="text-xs text-gray-500 dark:text-dark-400">{{ t('admin.activities.progress.metrics.pendingClaim') }}</p>
                  <p class="mt-1 font-semibold text-gray-900 dark:text-white">{{ formatCount(campaignStats.pending_claim_count) }}</p>
                </div>
                <div class="text-sm">
                  <p class="text-xs text-gray-500 dark:text-dark-400">{{ t('admin.activities.progress.metrics.pendingDelivery') }}</p>
                  <p class="mt-1 font-semibold text-gray-900 dark:text-white">{{ formatCount(campaignStats.pending_delivery_count) }}</p>
                </div>
                <div class="text-sm">
                  <p class="text-xs text-gray-500 dark:text-dark-400">{{ t('admin.activities.progress.metrics.delivered') }}</p>
                  <p class="mt-1 font-semibold text-gray-900 dark:text-white">{{ formatCount(campaignStats.delivered_count) }}</p>
                </div>
                <div class="text-sm">
                  <p class="text-xs text-gray-500 dark:text-dark-400">{{ t('admin.activities.progress.metrics.rejected') }}</p>
                  <p class="mt-1 font-semibold text-gray-900 dark:text-white">{{ formatCount(campaignStats.rejected_count) }}</p>
                </div>
                <div class="text-sm">
                  <p class="text-xs text-gray-500 dark:text-dark-400">{{ t('admin.activities.progress.metrics.pendingAction') }}</p>
                  <p class="mt-1 font-semibold text-gray-900 dark:text-white">{{ formatCount(campaignStats.pending_action_count) }}</p>
                </div>
              </div>
            </div>
            <div class="rounded-lg border border-gray-200 p-4 dark:border-dark-700">
              <h4 class="text-sm font-semibold text-gray-900 dark:text-white">{{ t('admin.activities.progress.latestDrawTitle') }}</h4>
              <template v-if="campaignStats.latest_draw">
                <p class="mt-2 text-sm text-gray-600 dark:text-dark-300">{{ formatDateTime(campaignStats.latest_draw.executed_at) }}</p>
                <p class="mt-1 text-xs text-gray-500 dark:text-dark-400">
                  {{ t('admin.activities.progress.latestDrawSummary', { users: formatCount(campaignStats.latest_draw.total_users), tickets: formatCount(campaignStats.latest_draw.total_tickets), winners: formatCount(campaignStats.latest_draw.winner_count) }) }}
                </p>
              </template>
              <p v-else class="mt-2 text-sm text-gray-500 dark:text-dark-400">{{ t('admin.activities.progress.noDrawYet') }}</p>
            </div>
          </div>
        </div>
      </section>

      <section class="card overflow-hidden">
        <div class="overflow-x-auto">
          <table class="w-full min-w-[980px] text-left text-sm">
            <thead class="bg-gray-50 text-xs uppercase text-gray-500 dark:bg-dark-800 dark:text-dark-400">
              <tr>
                <th class="px-4 py-3 font-medium">{{ t('admin.activities.columns.name') }}</th>
                <th class="px-4 py-3 font-medium">{{ t('common.status') }}</th>
                <th class="px-4 py-3 font-medium">{{ t('admin.activities.columns.rule') }}</th>
                <th class="px-4 py-3 font-medium">{{ t('admin.activities.columns.prizes') }}</th>
                <th class="px-4 py-3 font-medium">{{ t('admin.activities.columns.drawAt') }}</th>
                <th class="px-4 py-3 font-medium">{{ t('admin.activities.columns.public') }}</th>
                <th class="px-4 py-3 text-right font-medium">{{ t('common.actions') }}</th>
              </tr>
            </thead>
            <tbody class="divide-y divide-gray-100 dark:divide-dark-800">
              <tr v-if="loading">
                <td colspan="7" class="px-4 py-12 text-center text-gray-500 dark:text-dark-400">{{ t('common.loading') }}</td>
              </tr>
              <tr v-else-if="campaigns.length === 0">
                <td colspan="7" class="px-4 py-12 text-center text-gray-500 dark:text-dark-400">{{ t('admin.activities.empty') }}</td>
              </tr>
              <tr v-for="campaign in campaigns" v-else :key="campaign.id" class="hover:bg-gray-50 dark:hover:bg-dark-800">
                <td class="px-4 py-4">
                  <div class="font-medium text-gray-900 dark:text-white">{{ campaign.name }}</div>
                  <div class="mt-0.5 max-w-72 truncate text-xs text-gray-500 dark:text-dark-400">{{ campaign.description || '-' }}</div>
                </td>
                <td class="px-4 py-4">
                  <span :class="['badge', statusBadgeClass(campaign.status)]">{{ statusLabel(campaign.status) }}</span>
                </td>
                <td class="px-4 py-4 text-gray-700 dark:text-gray-300">
                  <div>{{ metricLabel(campaign.rule_config.metric) }} ≥ {{ metricValueText(campaign.rule_config.metric, campaign.rule_config.threshold) }}</div>
                  <div class="mt-0.5 text-xs text-gray-500 dark:text-dark-400">{{ ticketModeLabel(campaign.rule_config.ticket_mode) }} · {{ periodLabel(campaign) }}</div>
                </td>
                <td class="px-4 py-4 text-gray-700 dark:text-gray-300">{{ prizeSummary(campaign) }}</td>
                <td class="px-4 py-4 text-gray-700 dark:text-gray-300">{{ formatDateTime(campaign.draw_at) || '-' }}</td>
                <td class="px-4 py-4 text-gray-700 dark:text-gray-300">{{ campaign.public_enabled ? t('common.enabled') : t('common.disabled') }}</td>
                <td class="px-4 py-4">
                  <div class="flex justify-end gap-2">
                    <button type="button" class="btn btn-secondary btn-sm" @click="openProgress(campaign)">{{ t('admin.activities.progress.view') }}</button>
                    <button type="button" class="btn btn-secondary btn-sm" @click="openEdit(campaign)">{{ t('common.edit') }}</button>
                    <button type="button" class="btn btn-primary btn-sm" :disabled="campaign.status !== 'active'" @click="runDraw(campaign)">{{ t('admin.activities.drawNow') }}</button>
                    <button type="button" class="btn btn-danger btn-sm" :disabled="campaign.status === 'ended'" @click="endCampaign(campaign)">{{ t('admin.activities.end') }}</button>
                  </div>
                </td>
              </tr>
            </tbody>
          </table>
        </div>
        <div class="border-t border-gray-100 p-4 dark:border-dark-800">
          <Pagination v-if="campaignPagination.total > 0" :page="campaignPagination.page" :page-size="campaignPagination.page_size" :total="campaignPagination.total" @update:page="setCampaignPage" @update:pageSize="setCampaignPageSize" />
        </div>
      </section>

      <section class="card overflow-hidden">
        <div class="flex flex-col gap-3 border-b border-gray-100 p-5 dark:border-dark-800 lg:flex-row lg:items-center lg:justify-between">
          <div>
            <h2 class="text-lg font-semibold text-gray-900 dark:text-white">{{ t('admin.activities.winners.title') }}</h2>
            <p class="mt-1 text-sm text-gray-500 dark:text-dark-400">{{ t('admin.activities.winners.description') }}</p>
          </div>
          <div class="flex flex-wrap items-center gap-2">
            <select v-model.number="winnerCampaignId" class="input w-56" @change="reloadWinners">
              <option :value="0">{{ t('admin.activities.winners.allCampaigns') }}</option>
              <option v-for="campaign in campaigns" :key="campaign.id" :value="campaign.id">{{ campaign.name }}</option>
            </select>
            <button type="button" class="btn btn-secondary" :disabled="winnersLoading" @click="reloadWinners">{{ t('common.refresh') }}</button>
          </div>
        </div>
        <div class="overflow-x-auto">
          <table class="w-full min-w-[1040px] text-left text-sm">
            <thead class="bg-gray-50 text-xs uppercase text-gray-500 dark:bg-dark-800 dark:text-dark-400">
              <tr>
                <th class="px-4 py-3 font-medium">{{ t('admin.activities.winners.columns.user') }}</th>
                <th class="px-4 py-3 font-medium">{{ t('admin.activities.winners.columns.campaign') }}</th>
                <th class="px-4 py-3 font-medium">{{ t('admin.activities.winners.columns.prize') }}</th>
                <th class="px-4 py-3 font-medium">{{ t('admin.activities.winners.columns.claim') }}</th>
                <th class="px-4 py-3 font-medium">{{ t('common.status') }}</th>
                <th class="px-4 py-3 font-medium">{{ t('admin.activities.winners.columns.createdAt') }}</th>
                <th class="px-4 py-3 text-right font-medium">{{ t('common.actions') }}</th>
              </tr>
            </thead>
            <tbody class="divide-y divide-gray-100 dark:divide-dark-800">
              <tr v-if="winnersLoading">
                <td colspan="7" class="px-4 py-12 text-center text-gray-500 dark:text-dark-400">{{ t('common.loading') }}</td>
              </tr>
              <tr v-else-if="winners.length === 0">
                <td colspan="7" class="px-4 py-12 text-center text-gray-500 dark:text-dark-400">{{ t('admin.activities.winners.empty') }}</td>
              </tr>
              <tr v-for="winner in winners" v-else :key="winner.id" class="hover:bg-gray-50 dark:hover:bg-dark-800">
                <td class="px-4 py-4">
                  <div class="font-medium text-gray-900 dark:text-white">{{ winner.user_email || winner.user_username || winner.masked_user }}</div>
                  <div class="mt-0.5 text-xs text-gray-500 dark:text-dark-400">#{{ winner.user_id }}</div>
                </td>
                <td class="px-4 py-4 text-gray-700 dark:text-gray-300">{{ winner.campaign_name || `#${winner.campaign_id}` }}</td>
                <td class="px-4 py-4 text-gray-700 dark:text-gray-300">{{ winner.prize_name }} · {{ prizeAmountText(winner.prize_type, winner.prize_amount) }}</td>
                <td class="px-4 py-4">
                  <button v-if="winner.claim_info" type="button" class="btn btn-secondary btn-sm" @click="showClaimInfo(winner)">{{ t('admin.activities.winners.viewClaim') }}</button>
                  <span v-else class="text-xs text-gray-400 dark:text-dark-500">{{ claimStatusLabel(winner.claim_status) }}</span>
                </td>
                <td class="px-4 py-4"><span :class="['badge', winnerStatusBadgeClass(winner.status)]">{{ winnerStatusLabel(winner.status) }}</span></td>
                <td class="px-4 py-4 text-gray-700 dark:text-gray-300">{{ formatDateTime(winner.created_at) }}</td>
                <td class="px-4 py-4">
                  <div class="flex justify-end gap-2">
                    <button type="button" class="btn btn-primary btn-sm" :disabled="winner.status === 'delivered' || winner.status === 'rejected'" @click="markDelivered(winner)">{{ t('admin.activities.winners.deliver') }}</button>
                    <button type="button" class="btn btn-danger btn-sm" :disabled="winner.status === 'delivered' || winner.status === 'rejected'" @click="rejectWinner(winner)">{{ t('admin.activities.winners.reject') }}</button>
                  </div>
                </td>
              </tr>
            </tbody>
          </table>
        </div>
        <div class="border-t border-gray-100 p-4 dark:border-dark-800">
          <Pagination v-if="winnerPagination.total > 0" :page="winnerPagination.page" :page-size="winnerPagination.page_size" :total="winnerPagination.total" @update:page="setWinnerPage" @update:pageSize="setWinnerPageSize" />
        </div>
      </section>
    </div>

    <Teleport to="body">
      <div v-if="dialogOpen" class="fixed inset-0 z-50 flex items-center justify-center bg-black/50 p-4" @click.self="dialogOpen = false">
        <form class="max-h-[92vh] w-full max-w-5xl overflow-y-auto rounded-lg bg-white p-5 shadow-xl dark:bg-dark-900" @submit.prevent="submitForm">
          <div class="flex items-start justify-between gap-4">
            <div>
              <h2 class="text-lg font-semibold text-gray-900 dark:text-white">{{ editingCampaign ? t('admin.activities.edit') : t('admin.activities.create') }}</h2>
              <p class="mt-1 text-sm text-gray-500 dark:text-dark-400">{{ t('admin.activities.formDescription') }}</p>
            </div>
            <button type="button" class="btn btn-secondary btn-sm" @click="dialogOpen = false">{{ t('common.cancel') }}</button>
          </div>

          <div class="mt-5 grid gap-4 md:grid-cols-2">
            <div>
              <label class="input-label">{{ t('admin.activities.fields.name') }}</label>
              <input v-model.trim="form.name" class="input" required />
            </div>
            <div>
              <label class="input-label">{{ t('common.status') }}</label>
              <select v-model="form.status" class="input">
                <option v-for="option in statusOptions" :key="option.value" :value="option.value">{{ option.label }}</option>
              </select>
            </div>
            <div>
              <label class="input-label">{{ t('admin.activities.fields.startsAt') }}</label>
              <input v-model="form.starts_at_str" type="datetime-local" class="input" required />
            </div>
            <div>
              <label class="input-label">{{ t('admin.activities.fields.endsAt') }}</label>
              <input v-model="form.ends_at_str" type="datetime-local" class="input" required />
            </div>
            <div>
              <label class="input-label">{{ t('admin.activities.fields.drawAt') }}</label>
              <input v-model="form.draw_at_str" type="datetime-local" class="input" />
            </div>
            <div>
              <label class="input-label">{{ t('admin.activities.fields.sortOrder') }}</label>
              <input v-model.number="form.sort_order" type="number" class="input" />
            </div>
            <div>
              <label class="input-label">{{ t('admin.activities.fields.timezone') }}</label>
              <input v-model.trim="form.timezone" class="input" required />
            </div>
            <label class="flex min-h-11 items-center gap-2 rounded-lg border border-gray-200 px-3 dark:border-dark-700">
              <input v-model="form.public_enabled" type="checkbox" class="h-4 w-4 rounded border-gray-300 text-primary-600 focus:ring-primary-500" />
              <span class="text-sm text-gray-700 dark:text-gray-300">{{ t('admin.activities.fields.publicEnabled') }}</span>
            </label>
            <div>
              <label class="input-label">{{ t('admin.activities.fields.publicParticipantCount') }}</label>
              <select v-model="form.public_participant_count" class="input">
                <option v-for="option in publicParticipantCountOptions" :key="option.value" :value="option.value">{{ option.label }}</option>
              </select>
            </div>
            <div class="md:col-span-2">
              <label class="input-label">{{ t('admin.activities.fields.description') }}</label>
              <textarea v-model.trim="form.description" class="input min-h-24"></textarea>
            </div>
          </div>

          <div class="mt-6 rounded-lg border border-gray-200 p-4 dark:border-dark-700">
            <h3 class="text-base font-semibold text-gray-900 dark:text-white">{{ t('admin.activities.rule.title') }}</h3>
            <div class="mt-4 grid gap-4 md:grid-cols-3">
              <div>
                <label class="input-label">{{ t('admin.activities.rule.metric') }}</label>
                <select v-model="form.rule_config.metric" class="input">
                  <option v-for="option in metricOptions" :key="option.value" :value="option.value">{{ option.label }}</option>
                </select>
              </div>
              <div>
                <label class="input-label">{{ t('admin.activities.rule.periodType') }}</label>
                <select v-model="form.rule_config.period_type" class="input">
                  <option v-for="option in periodOptions" :key="option.value" :value="option.value">{{ option.label }}</option>
                </select>
              </div>
              <div v-if="form.rule_config.period_type === 'rolling_days'">
                <label class="input-label">{{ t('admin.activities.rule.rollingDays') }}</label>
                <input v-model.number="form.rule_config.rolling_days" type="number" min="1" class="input" />
              </div>
              <div v-if="form.rule_config.period_type === 'fixed_range'">
                <label class="input-label">{{ t('admin.activities.rule.periodStart') }}</label>
                <input v-model="form.rule_period_start_str" type="datetime-local" class="input" />
              </div>
              <div v-if="form.rule_config.period_type === 'fixed_range'">
                <label class="input-label">{{ t('admin.activities.rule.periodEnd') }}</label>
                <input v-model="form.rule_period_end_str" type="datetime-local" class="input" />
              </div>
              <div>
                <label class="input-label">{{ t('admin.activities.rule.threshold') }}</label>
                <input v-model.number="form.rule_config.threshold" type="number" min="0" step="0.0001" class="input" />
              </div>
              <div>
                <label class="input-label">{{ t('admin.activities.rule.ticketMode') }}</label>
                <select v-model="form.rule_config.ticket_mode" class="input">
                  <option v-for="option in ticketModeOptions" :key="option.value" :value="option.value">{{ option.label }}</option>
                </select>
              </div>
              <div v-if="form.rule_config.ticket_mode === 'fixed'">
                <label class="input-label">{{ t('admin.activities.rule.fixedTickets') }}</label>
                <input v-model.number="form.rule_config.fixed_tickets" type="number" min="1" class="input" />
              </div>
              <template v-if="form.rule_config.ticket_mode === 'proportional'">
                <div>
                  <label class="input-label">{{ t('admin.activities.rule.unitAmount') }}</label>
                  <input v-model.number="form.rule_config.unit_amount" type="number" min="0.0001" step="0.0001" class="input" />
                </div>
                <div>
                  <label class="input-label">{{ t('admin.activities.rule.ticketsPerUnit') }}</label>
                  <input v-model.number="form.rule_config.tickets_per_unit" type="number" min="1" class="input" />
                </div>
                <div>
                  <label class="input-label">{{ t('admin.activities.rule.maxTicketsPerUser') }}</label>
                  <input v-model.number="form.rule_config.max_tickets_per_user" type="number" min="0" class="input" />
                </div>
              </template>
              <div v-if="form.rule_config.ticket_mode === 'tiered'">
                <label class="input-label">{{ t('admin.activities.rule.tierMode') }}</label>
                <select v-model="form.rule_config.tier_mode" class="input">
                  <option v-for="option in tierModeOptions" :key="option.value" :value="option.value">{{ option.label }}</option>
                </select>
              </div>
            </div>

            <div v-if="form.rule_config.ticket_mode === 'tiered'" class="mt-4 space-y-2">
              <div v-for="(tier, index) in form.rule_config.tiers" :key="index" class="grid gap-2 md:grid-cols-[1fr_1fr_auto]">
                <input v-model.number="tier.threshold" type="number" min="0" step="0.0001" class="input" :placeholder="t('admin.activities.rule.tierThreshold')" />
                <input v-model.number="tier.tickets" type="number" min="1" class="input" :placeholder="t('admin.activities.rule.tierTickets')" />
                <button type="button" class="btn btn-danger" @click="removeTier(index)">{{ t('common.delete') }}</button>
              </div>
              <button type="button" class="btn btn-secondary btn-sm" @click="addTier">{{ t('admin.activities.rule.addTier') }}</button>
            </div>
          </div>

          <div class="mt-6 rounded-lg border border-gray-200 p-4 dark:border-dark-700">
            <div class="flex items-center justify-between gap-3">
              <h3 class="text-base font-semibold text-gray-900 dark:text-white">{{ t('admin.activities.prizes.title') }}</h3>
              <button type="button" class="btn btn-secondary btn-sm" @click="addPrize">{{ t('admin.activities.prizes.add') }}</button>
            </div>
            <div class="mt-4 space-y-4">
              <div v-for="(prize, prizeIndex) in form.prizes" :key="prizeIndex" class="rounded-lg border border-gray-200 p-4 dark:border-dark-700">
                <div class="grid gap-3 md:grid-cols-4">
                  <div class="md:col-span-2">
                    <label class="input-label">{{ t('admin.activities.prizes.name') }}</label>
                    <input v-model.trim="prize.name" class="input" required />
                  </div>
                  <div>
                    <label class="input-label">{{ t('admin.activities.prizes.type') }}</label>
                    <select v-model="prize.prize_type" class="input" @change="syncPrizeClaimFields(prize)">
                      <option v-for="option in prizeTypeOptions" :key="option.value" :value="option.value">{{ option.label }}</option>
                    </select>
                  </div>
                  <div>
                    <label class="input-label">{{ t('admin.activities.prizes.amount') }}</label>
                    <input v-model.number="prize.amount" type="number" min="0" step="0.0001" class="input" />
                  </div>
                  <div>
                    <label class="input-label">{{ t('admin.activities.prizes.quantity') }}</label>
                    <input v-model.number="prize.quantity" type="number" min="1" class="input" />
                  </div>
                  <div>
                    <label class="input-label">{{ t('admin.activities.fields.sortOrder') }}</label>
                    <input v-model.number="prize.sort_order" type="number" class="input" />
                  </div>
                  <label class="flex min-h-11 items-center gap-2 rounded-lg border border-gray-200 px-3 dark:border-dark-700">
                    <input v-model="prize.enabled" type="checkbox" class="h-4 w-4 rounded border-gray-300 text-primary-600 focus:ring-primary-500" />
                    <span class="text-sm text-gray-700 dark:text-gray-300">{{ t('common.enabled') }}</span>
                  </label>
                  <label class="flex min-h-11 items-center gap-2 rounded-lg border border-gray-200 px-3 dark:border-dark-700">
                    <input v-model="prize.requires_claim_info" type="checkbox" class="h-4 w-4 rounded border-gray-300 text-primary-600 focus:ring-primary-500" @change="syncPrizeClaimFields(prize)" />
                    <span class="text-sm text-gray-700 dark:text-gray-300">{{ t('admin.activities.prizes.requiresClaim') }}</span>
                  </label>
                  <div class="flex items-end justify-end">
                    <button type="button" class="btn btn-danger btn-sm" :disabled="form.prizes.length <= 1" @click="removePrize(prizeIndex)">{{ t('common.delete') }}</button>
                  </div>
                </div>

                <div v-if="prize.requires_claim_info" class="mt-4 space-y-2">
                  <div class="flex items-center justify-between gap-3">
                    <p class="text-sm font-medium text-gray-900 dark:text-white">{{ t('admin.activities.prizes.claimFields') }}</p>
                    <button type="button" class="btn btn-secondary btn-sm" @click="addClaimField(prize)">{{ t('admin.activities.prizes.addClaimField') }}</button>
                  </div>
                  <div v-for="(field, fieldIndex) in prize.claim_fields" :key="fieldIndex" class="grid gap-2 md:grid-cols-[1fr_1fr_140px_90px_auto]">
                    <input v-model.trim="field.key" class="input" :placeholder="t('admin.activities.prizes.claimFieldKey')" />
                    <input v-model.trim="field.label" class="input" :placeholder="t('admin.activities.prizes.claimFieldLabel')" />
                    <select v-model="field.type" class="input">
                      <option value="text">text</option>
                      <option value="phone">phone</option>
                      <option value="email">email</option>
                      <option value="textarea">textarea</option>
                    </select>
                    <label class="flex items-center gap-2 rounded-lg border border-gray-200 px-3 dark:border-dark-700">
                      <input v-model="field.required" type="checkbox" class="h-4 w-4 rounded border-gray-300 text-primary-600 focus:ring-primary-500" />
                      <span class="text-sm text-gray-700 dark:text-gray-300">{{ t('admin.activities.prizes.required') }}</span>
                    </label>
                    <button type="button" class="btn btn-danger" @click="removeClaimField(prize, fieldIndex)">{{ t('common.delete') }}</button>
                  </div>
                </div>
              </div>
            </div>
          </div>

          <div class="mt-5 flex justify-end gap-2">
            <button type="button" class="btn btn-secondary" @click="dialogOpen = false">{{ t('common.cancel') }}</button>
            <button type="submit" class="btn btn-primary" :disabled="saving">{{ saving ? t('common.saving') : t('common.save') }}</button>
          </div>
        </form>
      </div>
    </Teleport>
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import AppLayout from '@/components/layout/AppLayout.vue'
import Pagination from '@/components/common/Pagination.vue'
import { useAppStore } from '@/stores/app'
import { adminActivityAPI } from '@/api/admin/activity'
import { extractApiErrorMessage } from '@/utils/apiError'
import { formatCurrency, formatDateTime, formatDateTimeLocalInput, formatNumber, parseDateTimeLocalInput } from '@/utils/format'
import type {
  ActivityCampaign,
  ActivityCampaignPayload,
  ActivityCampaignStats,
  ActivityClaimField,
  ActivityMetric,
  ActivityPrize,
  ActivityPrizeType,
  ActivityPublicParticipantCountMode,
  ActivityRuleConfig,
  ActivityStatus,
  ActivityTicketMode,
  ActivityWinner,
  ActivityWinnerStatus,
  ActivityClaimStatus,
} from '@/types/activity'

type FormPrize = Omit<ActivityPrize, 'created_at' | 'updated_at'>

interface ActivityFormState {
  name: string
  description: string
  cover_url: string
  status: ActivityStatus
  starts_at_str: string
  ends_at_str: string
  draw_at_str: string
  timezone: string
  public_enabled: boolean
  public_participant_count: ActivityPublicParticipantCountMode
  sort_order: number
  rule_period_start_str: string
  rule_period_end_str: string
  rule_config: ActivityRuleConfig
  prizes: FormPrize[]
}

const { t } = useI18n()
const appStore = useAppStore()

const campaigns = ref<ActivityCampaign[]>([])
const winners = ref<ActivityWinner[]>([])
const campaignStats = ref<ActivityCampaignStats | null>(null)
const loading = ref(false)
const winnersLoading = ref(false)
const statsLoading = ref(false)
const saving = ref(false)
const dialogOpen = ref(false)
const keyword = ref('')
const statusFilter = ref('')
const winnerCampaignId = ref(0)
const selectedStatsCampaignId = ref(0)
const editingCampaign = ref<ActivityCampaign | null>(null)
const campaignPagination = reactive({ page: 1, page_size: 20, total: 0 })
const winnerPagination = reactive({ page: 1, page_size: 20, total: 0 })

let searchTimer: ReturnType<typeof setTimeout> | undefined

const form = reactive<ActivityFormState>(createDefaultForm())

const statusOptions = computed(() => [
  { value: 'draft', label: t('activities.status.draft') },
  { value: 'active', label: t('activities.status.active') },
  { value: 'paused', label: t('activities.status.paused') },
  { value: 'ended', label: t('activities.status.ended') },
])
const metricOptions = computed(() => [
  { value: 'api_cost_amount', label: t('activities.metrics.apiCostAmount') },
  { value: 'api_request_count', label: t('activities.metrics.apiRequestCount') },
])
const periodOptions = computed(() => [
  { value: 'today', label: t('activities.periodTypes.today') },
  { value: 'rolling_days', label: t('activities.periodTypes.rollingDaysShort') },
  { value: 'fixed_range', label: t('activities.periodTypes.fixedRange') },
  { value: 'campaign', label: t('activities.periodTypes.campaign') },
])
const ticketModeOptions = computed(() => [
  { value: 'fixed', label: t('activities.ticketModes.fixed') },
  { value: 'proportional', label: t('activities.ticketModes.proportional') },
  { value: 'tiered', label: t('activities.ticketModes.tiered') },
])
const tierModeOptions = computed(() => [
  { value: 'highest', label: t('activities.tierModes.highest') },
  { value: 'cumulative', label: t('activities.tierModes.cumulative') },
])
const prizeTypeOptions = computed(() => [
  { value: 'balance', label: t('activities.prizeTypes.balance') },
  { value: 'points', label: t('activities.prizeTypes.points') },
  { value: 'load_factor_credits', label: t('activities.prizeTypes.load_factor_credits') },
  { value: 'manual', label: t('activities.prizeTypes.manual') },
])
const publicParticipantCountOptions = computed<Array<{ value: ActivityPublicParticipantCountMode; label: string }>>(() => [
  { value: 'off', label: t('admin.activities.publicParticipantCount.off') },
  { value: 'fuzzy', label: t('admin.activities.publicParticipantCount.fuzzy') },
  { value: 'exact', label: t('admin.activities.publicParticipantCount.exact') },
])
const selectedStatsCampaign = computed(() => campaigns.value.find(campaign => campaign.id === selectedStatsCampaignId.value) || null)
const statsMetric = computed<ActivityMetric>(() => selectedStatsCampaign.value?.rule_config.metric || 'api_cost_amount')
const statsMetricLabel = computed(() => metricLabel(statsMetric.value))

function createDefaultForm(): ActivityFormState {
  const now = new Date()
  const end = addHours(now, 24)
  return {
    name: '',
    description: '',
    cover_url: '',
    status: 'draft',
    starts_at_str: toLocalInput(now.toISOString()),
    ends_at_str: toLocalInput(end.toISOString()),
    draw_at_str: toLocalInput(end.toISOString()),
    timezone: 'Asia/Shanghai',
    public_enabled: true,
    public_participant_count: 'off',
    sort_order: 0,
    rule_period_start_str: '',
    rule_period_end_str: '',
    rule_config: createDefaultRule(),
    prizes: [createDefaultPrize()],
  }
}

function createDefaultRule(): ActivityRuleConfig {
  return {
    metric: 'api_cost_amount',
    period_type: 'today',
    threshold: 100,
    ticket_mode: 'fixed',
    fixed_tickets: 1,
    unit_amount: 100,
    tickets_per_unit: 1,
    max_tickets_per_user: 10,
    tier_mode: 'highest',
    tiers: [{ threshold: 100, tickets: 1 }],
  }
}

function createDefaultPrize(): FormPrize {
  return {
    name: t('admin.activities.prizes.defaultName'),
    description: null,
    prize_type: 'balance',
    amount: 10,
    quantity: 1,
    weight: 1,
    requires_claim_info: false,
    claim_fields: [],
    enabled: true,
    sort_order: 0,
  }
}

function createDefaultClaimFields(): ActivityClaimField[] {
  return [
    { key: 'name', label: t('admin.activities.prizes.defaultClaimName'), required: true, type: 'text' },
    { key: 'contact', label: t('admin.activities.prizes.defaultClaimContact'), required: true, type: 'text' },
  ]
}

async function reloadCampaigns(): Promise<void> {
  loading.value = true
  try {
    const page = await adminActivityAPI.listCampaigns({
      page: campaignPagination.page,
      page_size: campaignPagination.page_size,
      status: statusFilter.value || undefined,
      keyword: keyword.value || undefined,
    })
    campaigns.value = page.items || []
    campaignPagination.total = page.total || 0
    syncSelectedStatsCampaign()
    if (selectedStatsCampaignId.value) {
      await loadCampaignStats()
    } else {
      campaignStats.value = null
    }
  } catch (error) {
    appStore.showError(extractApiErrorMessage(error, t('admin.activities.loadFailed')))
  } finally {
    loading.value = false
  }
}

async function reloadWinners(): Promise<void> {
  winnersLoading.value = true
  try {
    const page = await adminActivityAPI.listWinners({
      page: winnerPagination.page,
      page_size: winnerPagination.page_size,
      campaign_id: winnerCampaignId.value || undefined,
    })
    winners.value = page.items || []
    winnerPagination.total = page.total || 0
  } catch (error) {
    appStore.showError(extractApiErrorMessage(error, t('admin.activities.winners.loadFailed')))
  } finally {
    winnersLoading.value = false
  }
}

async function loadCampaignStats(): Promise<void> {
  if (!selectedStatsCampaignId.value) {
    campaignStats.value = null
    return
  }
  statsLoading.value = true
  try {
    campaignStats.value = await adminActivityAPI.getCampaignStats(selectedStatsCampaignId.value)
  } catch (error) {
    campaignStats.value = null
    appStore.showError(extractApiErrorMessage(error, t('admin.activities.progress.loadFailed')))
  } finally {
    statsLoading.value = false
  }
}

function syncSelectedStatsCampaign(): void {
  if (selectedStatsCampaignId.value && campaigns.value.some(campaign => campaign.id === selectedStatsCampaignId.value)) {
    return
  }
  const activeCampaign = campaigns.value.find(campaign => campaign.status === 'active')
  selectedStatsCampaignId.value = activeCampaign?.id || campaigns.value[0]?.id || 0
}

function openProgress(campaign: ActivityCampaign): void {
  selectedStatsCampaignId.value = campaign.id
  void loadCampaignStats()
}

function resetForm(): void {
  Object.assign(form, createDefaultForm())
}

function openCreate(): void {
  editingCampaign.value = null
  resetForm()
  dialogOpen.value = true
}

async function openEdit(campaign: ActivityCampaign): Promise<void> {
  try {
    const full = await adminActivityAPI.getCampaign(campaign.id)
    editingCampaign.value = full
    fillForm(full)
    dialogOpen.value = true
  } catch (error) {
    appStore.showError(extractApiErrorMessage(error, t('admin.activities.loadFailed')))
  }
}

function fillForm(campaign: ActivityCampaign): void {
  form.name = campaign.name
  form.description = campaign.description || ''
  form.cover_url = campaign.cover_url || ''
  form.status = campaign.status
  form.starts_at_str = toLocalInput(campaign.starts_at)
  form.ends_at_str = toLocalInput(campaign.ends_at)
  form.draw_at_str = toLocalInput(campaign.draw_at)
  form.timezone = campaign.timezone || 'Asia/Shanghai'
  form.public_enabled = campaign.public_enabled
  form.public_participant_count = normalizePublicParticipantCountMode(campaign.display_config?.public_participant_count)
  form.sort_order = campaign.sort_order || 0
  form.rule_config = cloneRule(campaign.rule_config)
  form.rule_period_start_str = toLocalInput(campaign.rule_config.period_start_at)
  form.rule_period_end_str = toLocalInput(campaign.rule_config.period_end_at)
  form.prizes = (campaign.prizes && campaign.prizes.length > 0 ? campaign.prizes : [createDefaultPrize()]).map(clonePrize)
}

function cloneRule(rule: ActivityRuleConfig): ActivityRuleConfig {
  return {
    metric: rule.metric || 'api_cost_amount',
    period_type: rule.period_type || 'today',
    period_start_at: rule.period_start_at || null,
    period_end_at: rule.period_end_at || null,
    rolling_days: rule.rolling_days || 1,
    threshold: Number(rule.threshold || 0),
    ticket_mode: rule.ticket_mode || 'fixed',
    fixed_tickets: rule.fixed_tickets || 1,
    unit_amount: rule.unit_amount || 100,
    tickets_per_unit: rule.tickets_per_unit || 1,
    max_tickets_per_user: rule.max_tickets_per_user || 0,
    tier_mode: rule.tier_mode || 'highest',
    tiers: (rule.tiers || []).map(tier => ({ threshold: Number(tier.threshold || 0), tickets: Number(tier.tickets || 1) })),
  }
}

function clonePrize(prize: ActivityPrize): FormPrize {
  return {
    id: prize.id,
    campaign_id: prize.campaign_id,
    name: prize.name,
    description: prize.description || null,
    prize_type: prize.prize_type,
    amount: Number(prize.amount || 0),
    quantity: Number(prize.quantity || 1),
    weight: Number(prize.weight || 1),
    requires_claim_info: !!prize.requires_claim_info,
    claim_fields: (prize.claim_fields || []).map(field => ({ ...field })),
    enabled: prize.enabled !== false,
    sort_order: Number(prize.sort_order || 0),
  }
}

async function submitForm(): Promise<void> {
  const payload = buildPayload()
  if (!payload) return
  saving.value = true
  try {
    if (editingCampaign.value) {
      await adminActivityAPI.updateCampaign(editingCampaign.value.id, payload)
    } else {
      await adminActivityAPI.createCampaign(payload)
    }
    appStore.showSuccess(t('common.saved'))
    dialogOpen.value = false
    await reloadCampaigns()
  } catch (error) {
    appStore.showError(extractApiErrorMessage(error, t('common.error')))
  } finally {
    saving.value = false
  }
}

function buildPayload(): ActivityCampaignPayload | null {
  const startsAt = localInputToISO(form.starts_at_str)
  const endsAt = localInputToISO(form.ends_at_str)
  if (!startsAt || !endsAt) {
    appStore.showError(t('admin.activities.validation.timeRequired'))
    return null
  }
  if (new Date(startsAt) >= new Date(endsAt)) {
    appStore.showError(t('admin.activities.validation.timeOrder'))
    return null
  }
  if (!form.prizes.some(prize => prize.enabled)) {
    appStore.showError(t('admin.activities.validation.prizeRequired'))
    return null
  }
  const rule = normalizeRuleForSubmit()
  if (!rule) return null
  const displayConfig = { ...(editingCampaign.value?.display_config || {}) }
  displayConfig.public_participant_count = form.public_participant_count
  return {
    type: 'consumption_lottery',
    name: form.name.trim(),
    description: form.description.trim() || null,
    cover_url: form.cover_url.trim() || null,
    status: form.status,
    starts_at: startsAt,
    ends_at: endsAt,
    draw_at: localInputToISO(form.draw_at_str),
    timezone: form.timezone.trim() || 'Asia/Shanghai',
    public_enabled: form.public_enabled,
    sort_order: Number(form.sort_order || 0),
    rule_config: rule,
    display_config: displayConfig,
    prizes: form.prizes.map((prize, index) => ({
      id: prize.id,
      name: prize.name.trim(),
      description: prize.description || null,
      prize_type: prize.prize_type,
      amount: Number(prize.amount || 0),
      quantity: Math.max(1, Math.trunc(Number(prize.quantity || 1))),
      weight: 1,
      requires_claim_info: !!prize.requires_claim_info,
      claim_fields: prize.requires_claim_info ? prize.claim_fields.map(field => ({
        key: field.key.trim(),
        label: field.label.trim(),
        required: !!field.required,
        type: field.type || 'text',
      })) : [],
      enabled: prize.enabled !== false,
      sort_order: Number(prize.sort_order ?? index),
    })),
  }
}

function normalizeRuleForSubmit(): ActivityRuleConfig | null {
  const rule = cloneRule(form.rule_config)
  rule.threshold = Number(rule.threshold || 0)
  if (rule.threshold < 0) {
    appStore.showError(t('admin.activities.validation.thresholdInvalid'))
    return null
  }
  if (rule.period_type === 'fixed_range') {
    const start = localInputToISO(form.rule_period_start_str)
    const end = localInputToISO(form.rule_period_end_str)
    if (!start || !end) {
      appStore.showError(t('admin.activities.validation.rulePeriodRequired'))
      return null
    }
    rule.period_start_at = start
    rule.period_end_at = end
  } else {
    rule.period_start_at = null
    rule.period_end_at = null
  }
  if (rule.period_type !== 'rolling_days') rule.rolling_days = 0
  if (rule.ticket_mode === 'fixed') {
    rule.fixed_tickets = Math.max(1, Math.trunc(Number(rule.fixed_tickets || 1)))
  } else if (rule.ticket_mode === 'proportional') {
    rule.unit_amount = Number(rule.unit_amount || 0)
    rule.tickets_per_unit = Math.max(1, Math.trunc(Number(rule.tickets_per_unit || 1)))
    rule.max_tickets_per_user = Math.max(0, Math.trunc(Number(rule.max_tickets_per_user || 0)))
  } else if (rule.ticket_mode === 'tiered') {
    rule.tier_mode = rule.tier_mode || 'highest'
    rule.tiers = (rule.tiers || []).map(tier => ({
      threshold: Number(tier.threshold || 0),
      tickets: Math.max(1, Math.trunc(Number(tier.tickets || 1))),
    }))
    if (rule.tiers.length === 0) {
      appStore.showError(t('admin.activities.validation.tiersRequired'))
      return null
    }
  }
  return rule
}

async function runDraw(campaign: ActivityCampaign): Promise<void> {
  if (!window.confirm(t('admin.activities.confirmDraw', { name: campaign.name }))) return
  try {
    selectedStatsCampaignId.value = campaign.id
    const result = await adminActivityAPI.runDraw(campaign.id)
    appStore.showSuccess(t('admin.activities.drawSuccess', { count: result.winner_count }))
    await Promise.all([reloadCampaigns(), reloadWinners()])
  } catch (error) {
    appStore.showError(extractApiErrorMessage(error, t('admin.activities.drawFailed')))
  }
}

async function endCampaign(campaign: ActivityCampaign): Promise<void> {
  if (!window.confirm(t('admin.activities.confirmEnd', { name: campaign.name }))) return
  try {
    selectedStatsCampaignId.value = campaign.id
    await adminActivityAPI.endCampaign(campaign.id)
    appStore.showSuccess(t('admin.activities.endSuccess'))
    await reloadCampaigns()
  } catch (error) {
    appStore.showError(extractApiErrorMessage(error, t('common.error')))
  }
}

async function markDelivered(winner: ActivityWinner): Promise<void> {
  const note = window.prompt(t('admin.activities.winners.deliverNotePrompt'), winner.admin_note || '')
  if (note === null) return
  try {
    const updated = await adminActivityAPI.markWinnerDelivered(winner.id, note)
    winners.value = winners.value.map(item => item.id === updated.id ? updated : item)
    appStore.showSuccess(t('admin.activities.winners.deliverSuccess'))
    if (selectedStatsCampaignId.value === updated.campaign_id) await loadCampaignStats()
  } catch (error) {
    appStore.showError(extractApiErrorMessage(error, t('common.error')))
  }
}

async function rejectWinner(winner: ActivityWinner): Promise<void> {
  const note = window.prompt(t('admin.activities.winners.rejectNotePrompt'), winner.admin_note || '')
  if (note === null) return
  try {
    const updated = await adminActivityAPI.rejectWinner(winner.id, note)
    winners.value = winners.value.map(item => item.id === updated.id ? updated : item)
    appStore.showSuccess(t('admin.activities.winners.rejectSuccess'))
    if (selectedStatsCampaignId.value === updated.campaign_id) await loadCampaignStats()
  } catch (error) {
    appStore.showError(extractApiErrorMessage(error, t('common.error')))
  }
}

function showClaimInfo(winner: ActivityWinner): void {
  const text = JSON.stringify(winner.claim_info || {}, null, 2)
  window.alert(text)
}

function addPrize(): void {
  form.prizes.push({ ...createDefaultPrize(), sort_order: form.prizes.length })
}

function removePrize(index: number): void {
  if (form.prizes.length <= 1) return
  form.prizes.splice(index, 1)
}

function syncPrizeClaimFields(prize: FormPrize): void {
  if (prize.prize_type === 'manual' && !prize.requires_claim_info) {
    prize.requires_claim_info = true
  }
  if (prize.requires_claim_info && prize.claim_fields.length === 0) {
    prize.claim_fields = createDefaultClaimFields()
  }
  if (!prize.requires_claim_info) {
    prize.claim_fields = []
  }
}

function addClaimField(prize: FormPrize): void {
  prize.claim_fields.push({ key: '', label: '', required: true, type: 'text' })
}

function removeClaimField(prize: FormPrize, index: number): void {
  prize.claim_fields.splice(index, 1)
}

function addTier(): void {
  if (!form.rule_config.tiers) form.rule_config.tiers = []
  form.rule_config.tiers.push({ threshold: 0, tickets: 1 })
}

function removeTier(index: number): void {
  form.rule_config.tiers?.splice(index, 1)
}

function setCampaignPage(page: number): void {
  campaignPagination.page = page
  void reloadCampaigns()
}

function setCampaignPageSize(pageSize: number): void {
  campaignPagination.page_size = pageSize
  campaignPagination.page = 1
  void reloadCampaigns()
}

function setWinnerPage(page: number): void {
  winnerPagination.page = page
  void reloadWinners()
}

function setWinnerPageSize(pageSize: number): void {
  winnerPagination.page_size = pageSize
  winnerPagination.page = 1
  void reloadWinners()
}

function handleSearch(): void {
  clearTimeout(searchTimer)
  searchTimer = setTimeout(() => {
    campaignPagination.page = 1
    void reloadCampaigns()
  }, 300)
}

function normalizePublicParticipantCountMode(value: unknown): ActivityPublicParticipantCountMode {
  return value === 'fuzzy' || value === 'exact' ? value : 'off'
}

function drawReadinessLabel(stats: ActivityCampaignStats): string {
  if (stats.can_run_draw) {
    return stats.no_participant_warning ? t('admin.activities.progress.readyNoParticipants') : t('admin.activities.progress.ready')
  }
  const reason = stats.draw_block_reason || 'unknown'
  return t(`admin.activities.progress.drawBlockReasons.${reason}`)
}

function statsMetricValueText(value: number): string {
  return metricValueText(statsMetric.value, value)
}

function formatCount(value: number): string {
  return formatNumber(value || 0)
}

function formatDecimal(value: number): string {
  return Number(value || 0).toLocaleString(undefined, { maximumFractionDigits: 2 })
}

function statusLabel(status: ActivityStatus): string {
  return t(`activities.status.${status}`)
}

function statusBadgeClass(status: ActivityStatus): string {
  if (status === 'active') return 'badge-success'
  if (status === 'paused') return 'badge-warning'
  if (status === 'ended') return 'badge-gray'
  return 'badge-primary'
}

function metricLabel(metric: ActivityMetric): string {
  return metric === 'api_request_count' ? t('activities.metrics.apiRequestCount') : t('activities.metrics.apiCostAmount')
}

function metricValueText(metric: ActivityMetric, value: number): string {
  if (metric === 'api_request_count') return t('activities.metrics.requestValue', { count: Math.floor(value || 0) })
  return formatCurrency(value || 0)
}

function periodLabel(campaign: ActivityCampaign): string {
  const rule = campaign.rule_config
  if (rule.period_type === 'today') return t('activities.periodTypes.today')
  if (rule.period_type === 'rolling_days') return t('activities.periodTypes.rollingDays', { days: rule.rolling_days || 1 })
  if (rule.period_type === 'campaign') return t('activities.periodTypes.campaign')
  return t('activities.periodTypes.fixedRange')
}

function ticketModeLabel(mode: ActivityTicketMode): string {
  return t(`activities.ticketModes.${mode}`)
}

function prizeSummary(campaign: ActivityCampaign): string {
  const prizes = campaign.prizes || []
  if (prizes.length === 0) return '-'
  return prizes.map(prize => `${prize.name} x${prize.quantity}`).join(' / ')
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

function claimStatusLabel(status: ActivityClaimStatus): string {
  return t(`activities.claimStatus.${status}`)
}

function toLocalInput(value?: string | null): string {
  if (!value) return ''
  const time = new Date(value).getTime()
  if (!Number.isFinite(time)) return ''
  return formatDateTimeLocalInput(Math.floor(time / 1000))
}

function localInputToISO(value: string): string | null {
  const ts = parseDateTimeLocalInput(value)
  return ts ? new Date(ts * 1000).toISOString() : null
}

function addHours(date: Date, hours: number): Date {
  return new Date(date.getTime() + hours * 60 * 60 * 1000)
}

onMounted(async () => {
  await reloadCampaigns()
  await reloadWinners()
})
</script>
