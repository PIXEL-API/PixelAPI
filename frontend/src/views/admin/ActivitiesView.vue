<template>
  <AppLayout>
    <div class="admin-activity-page data-page" :aria-busy="activePanelLoading">
      <header class="admin-mobile-heading">
        <div>
          <h1>{{ t('admin.activities.title') }}</h1>
          <p>{{ t('admin.activities.description') }}</p>
        </div>
        <button type="button" class="btn btn-primary admin-mobile-create" @click="openCreate">
          <Icon name="plus" size="sm" />
          <span>{{ t('admin.activities.create') }}</span>
        </button>
      </header>

      <div class="activity-workspace-bar">
        <nav class="activity-workspace-tabs" role="tablist" :aria-label="t('admin.activities.workspace.tabsLabel')">
          <button
            v-for="(tab, index) in workspaceTabs"
            :id="workspaceTabId(tab.key)"
            :key="tab.key"
            type="button"
            role="tab"
            class="activity-workspace-tab"
            :class="{ 'activity-workspace-tab-active': activeWorkspace === tab.key }"
            :aria-controls="workspacePanelId(tab.key)"
            :aria-selected="activeWorkspace === tab.key"
            :tabindex="activeWorkspace === tab.key ? 0 : -1"
            @click="selectWorkspace(tab.key)"
            @keydown="handleWorkspaceKeydown($event, index)"
          >
            <Icon :name="tab.icon" size="sm" />
            <span>{{ tab.label }}</span>
            <em v-if="tab.count !== null">{{ formatCount(tab.count) }}</em>
          </button>
        </nav>

        <div class="activity-workspace-actions">
          <button
            type="button"
            class="btn btn-secondary btn-icon workspace-refresh"
            :disabled="activePanelLoading || (activeWorkspace === 'progress' && !selectedStatsCampaignId)"
            :title="t('common.refresh')"
            :aria-label="t('common.refresh')"
            @click="refreshActiveWorkspace"
          >
            <Icon name="refresh" size="sm" :class="{ 'activity-spin': activePanelLoading }" />
          </button>
          <button type="button" class="btn btn-primary workspace-create" @click="openCreate">
            <Icon name="plus" size="sm" />
            <span>{{ t('admin.activities.create') }}</span>
          </button>
        </div>
      </div>

      <section
        v-if="activeWorkspace === 'campaigns'"
        :id="workspacePanelId('campaigns')"
        role="tabpanel"
        :aria-labelledby="workspaceTabId('campaigns')"
        class="activity-workspace-panel"
      >
        <div class="activity-list-toolbar">
          <div class="activity-toolbar-copy">
            <h2>{{ t('admin.activities.workspace.campaignsTitle') }}</h2>
            <p>{{ t('admin.activities.workspace.campaignsDescription') }}</p>
          </div>
          <div class="activity-filter-row">
            <label class="activity-search-field">
              <span class="sr-only">{{ t('admin.activities.searchPlaceholder') }}</span>
              <Icon name="search" size="sm" />
              <input
                v-model.trim="keyword"
                type="search"
                :placeholder="t('admin.activities.searchPlaceholder')"
                @input="handleSearch"
              />
            </label>
            <label class="activity-filter-field">
              <span class="sr-only">{{ t('common.status') }}</span>
              <select v-model="statusFilter" @change="reloadCampaigns">
                <option value="">{{ t('admin.activities.allStatus') }}</option>
                <option v-for="option in statusOptions" :key="option.value" :value="option.value">{{ option.label }}</option>
              </select>
              <Icon name="chevronDown" size="xs" />
            </label>
          </div>
        </div>

        <div class="activity-table-shell">
          <div class="activity-desktop-table">
            <div class="activity-table-scroll">
              <table>
                <thead>
                  <tr>
                    <th>{{ t('admin.activities.columns.name') }}</th>
                    <th>{{ t('common.status') }}</th>
                    <th>{{ t('admin.activities.columns.rule') }}</th>
                    <th>{{ t('admin.activities.columns.prizes') }}</th>
                    <th>{{ t('admin.activities.columns.drawAt') }}</th>
                    <th>{{ t('admin.activities.columns.public') }}</th>
                    <th class="activity-actions-heading">{{ t('common.actions') }}</th>
                  </tr>
                </thead>
                <tbody>
                  <tr v-if="loading">
                    <td colspan="7">
                      <div class="activity-table-state">
                        <Icon name="refresh" size="md" class="activity-spin" />
                        <span>{{ t('common.loading') }}</span>
                      </div>
                    </td>
                  </tr>
                  <tr v-else-if="campaigns.length === 0">
                    <td colspan="7">
                      <div class="activity-table-state">
                        <span class="activity-state-icon"><Icon name="gift" size="lg" /></span>
                        <strong>{{ t('admin.activities.empty') }}</strong>
                        <p>{{ t('admin.activities.workspace.emptyCampaignsHint') }}</p>
                      </div>
                    </td>
                  </tr>
                  <tr v-for="campaign in campaigns" v-else :key="campaign.id">
                    <td>
                      <div class="activity-name-cell">
                        <strong>{{ campaign.name }}</strong>
                        <span>{{ campaign.description || t('admin.activities.workspace.noDescription') }}</span>
                      </div>
                    </td>
                    <td>
                      <span :class="['admin-status-badge', campaignStatusClass(campaign.status)]">
                        <i></i>
                        {{ statusLabel(campaign.status) }}
                      </span>
                    </td>
                    <td>
                      <div class="activity-rule-cell">
                        <strong>{{ metricLabel(campaign.rule_config.metric) }} ≥ {{ metricValueText(campaign.rule_config.metric, campaign.rule_config.threshold) }}</strong>
                        <span>{{ periodLabel(campaign) }} · {{ ticketModeLabel(campaign.rule_config.ticket_mode) }}</span>
                      </div>
                    </td>
                    <td><span class="activity-prize-summary">{{ prizeSummary(campaign) }}</span></td>
                    <td><span class="activity-date-cell">{{ formatDateTime(campaign.draw_at) || '-' }}</span></td>
                    <td>
                      <span :class="['activity-visibility', { 'activity-visibility-off': !campaign.public_enabled }]">
                        <Icon :name="campaign.public_enabled ? 'eye' : 'eyeOff'" size="xs" />
                        {{ campaign.public_enabled ? t('common.enabled') : t('common.disabled') }}
                      </span>
                    </td>
                    <td>
                      <div class="activity-row-actions">
                        <button
                          type="button"
                          class="activity-icon-action"
                          :title="t('admin.activities.progress.view')"
                          :aria-label="t('admin.activities.progress.view')"
                          @click="openProgress(campaign)"
                        >
                          <Icon name="chartBar" size="sm" />
                        </button>
                        <button
                          type="button"
                          class="activity-icon-action"
                          :title="t('common.edit')"
                          :aria-label="t('common.edit')"
                          @click="openEdit(campaign)"
                        >
                          <Icon name="edit" size="sm" />
                        </button>
                        <button
                          type="button"
                          class="activity-icon-action activity-icon-action-primary"
                          :disabled="campaign.status !== 'active'"
                          :title="t('admin.activities.drawNow')"
                          :aria-label="t('admin.activities.drawNow')"
                          @click="openCampaignAction(campaign, 'draw')"
                        >
                          <Icon name="play" size="sm" />
                        </button>
                        <button
                          type="button"
                          class="activity-icon-action activity-icon-action-danger"
                          :disabled="campaign.status === 'ended'"
                          :title="t('admin.activities.end')"
                          :aria-label="t('admin.activities.end')"
                          @click="openCampaignAction(campaign, 'end')"
                        >
                          <Icon name="xCircle" size="sm" />
                        </button>
                      </div>
                    </td>
                  </tr>
                </tbody>
              </table>
            </div>
          </div>

          <div class="activity-mobile-list">
            <div v-if="loading" class="activity-card-state">
              <Icon name="refresh" size="md" class="activity-spin" />
              <span>{{ t('common.loading') }}</span>
            </div>
            <div v-else-if="campaigns.length === 0" class="activity-card-state">
              <span class="activity-state-icon"><Icon name="gift" size="lg" /></span>
              <strong>{{ t('admin.activities.empty') }}</strong>
              <p>{{ t('admin.activities.workspace.emptyCampaignsHint') }}</p>
            </div>
            <article v-for="campaign in campaigns" v-else :key="campaign.id" class="activity-mobile-card">
              <header>
                <div>
                  <strong>{{ campaign.name }}</strong>
                  <p>{{ campaign.description || t('admin.activities.workspace.noDescription') }}</p>
                </div>
                <span :class="['admin-status-badge', campaignStatusClass(campaign.status)]">
                  <i></i>
                  {{ statusLabel(campaign.status) }}
                </span>
              </header>
              <dl>
                <div>
                  <dt>{{ t('admin.activities.columns.rule') }}</dt>
                  <dd>{{ metricLabel(campaign.rule_config.metric) }} ≥ {{ metricValueText(campaign.rule_config.metric, campaign.rule_config.threshold) }}</dd>
                </div>
                <div>
                  <dt>{{ t('admin.activities.columns.drawAt') }}</dt>
                  <dd>{{ formatDateTime(campaign.draw_at) || '-' }}</dd>
                </div>
                <div>
                  <dt>{{ t('admin.activities.columns.prizes') }}</dt>
                  <dd>{{ prizeSummary(campaign) }}</dd>
                </div>
              </dl>
              <footer>
                <button type="button" class="btn btn-secondary btn-sm" @click="openProgress(campaign)">
                  <Icon name="chartBar" size="sm" />
                  {{ t('admin.activities.progress.view') }}
                </button>
                <button type="button" class="btn btn-secondary btn-sm" @click="openEdit(campaign)">
                  <Icon name="edit" size="sm" />
                  {{ t('common.edit') }}
                </button>
                <button
                  type="button"
                  class="btn btn-secondary btn-sm activity-mobile-draw"
                  :disabled="campaign.status !== 'active'"
                  @click="openCampaignAction(campaign, 'draw')"
                >
                  <Icon name="play" size="sm" />
                  {{ t('admin.activities.drawNow') }}
                </button>
                <button
                  type="button"
                  class="btn btn-secondary btn-sm activity-mobile-end"
                  :disabled="campaign.status === 'ended'"
                  @click="openCampaignAction(campaign, 'end')"
                >
                  <Icon name="xCircle" size="sm" />
                  {{ t('admin.activities.end') }}
                </button>
              </footer>
            </article>
          </div>

          <footer v-if="campaignPagination.total > 0" class="activity-table-footer">
            <Pagination
              :page="campaignPagination.page"
              :page-size="campaignPagination.page_size"
              :total="campaignPagination.total"
              @update:page="setCampaignPage"
              @update:pageSize="setCampaignPageSize"
            />
          </footer>
        </div>
      </section>

      <section
        v-else-if="activeWorkspace === 'progress'"
        :id="workspacePanelId('progress')"
        role="tabpanel"
        :aria-labelledby="workspaceTabId('progress')"
        class="activity-workspace-panel"
      >
        <div class="activity-progress-toolbar">
          <div class="activity-toolbar-copy">
            <h2>{{ t('admin.activities.progress.title') }}</h2>
            <p>{{ t('admin.activities.progress.description') }}</p>
          </div>
          <label class="activity-campaign-select">
            <span>{{ t('admin.activities.workspace.selectedCampaign') }}</span>
            <span class="activity-select-control">
              <select v-model.number="selectedStatsCampaignId" @change="loadCampaignStats">
                <option :value="0">{{ t('admin.activities.progress.selectCampaign') }}</option>
                <option v-for="campaign in campaigns" :key="campaign.id" :value="campaign.id">{{ campaign.name }}</option>
              </select>
              <Icon name="chevronDown" size="xs" />
            </span>
          </label>
        </div>

        <div v-if="statsLoading" class="activity-progress-empty">
          <Icon name="refresh" size="lg" class="activity-spin" />
          <strong>{{ t('common.loading') }}</strong>
        </div>
        <div v-else-if="!campaignStats" class="activity-progress-empty">
          <span class="activity-state-icon"><Icon name="chartBar" size="lg" /></span>
          <strong>{{ t('admin.activities.progress.empty') }}</strong>
          <p>{{ t('admin.activities.workspace.progressEmptyHint') }}</p>
        </div>
        <div v-else class="activity-progress-dashboard">
          <header class="activity-progress-header">
            <div class="activity-progress-title">
              <div class="activity-progress-badges">
                <span :class="['admin-status-badge', campaignStatusClass(campaignStats.status)]">
                  <i></i>
                  {{ statusLabel(campaignStats.status) }}
                </span>
                <span :class="['admin-readiness-badge', { 'admin-readiness-ready': campaignStats.can_run_draw }]">
                  <Icon :name="campaignStats.can_run_draw ? 'checkCircle' : 'clock'" size="xs" />
                  {{ drawReadinessLabel(campaignStats) }}
                </span>
              </div>
              <h3>{{ campaignStats.campaign_name }}</h3>
              <p>
                {{ t('admin.activities.progress.periodRange', {
                  start: formatDateTime(campaignStats.period_start_at),
                  end: formatDateTime(campaignStats.period_end_at)
                }) }}
              </p>
            </div>
            <div class="activity-progress-actions">
              <button
                type="button"
                class="btn btn-primary"
                :disabled="!campaignStats.can_run_draw || selectedStatsCampaign?.status !== 'active'"
                @click="selectedStatsCampaign && openCampaignAction(selectedStatsCampaign, 'draw')"
              >
                <Icon name="play" size="sm" />
                {{ t('admin.activities.drawNow') }}
              </button>
            </div>
          </header>

          <div class="activity-progress-timebar">
            <div>
              <Icon name="calendar" size="sm" />
              <span>
                <small>{{ t('admin.activities.progress.drawAt') }}</small>
                <strong>{{ formatDateTime(campaignStats.draw_at) || '-' }}</strong>
              </span>
            </div>
            <div>
              <Icon name="users" size="sm" />
              <span>
                <small>{{ t('admin.activities.progress.lastJoinedAt') }}</small>
                <strong>{{ formatDateTime(campaignStats.last_joined_at) || '-' }}</strong>
              </span>
            </div>
          </div>

          <div v-if="campaignStats.no_participant_warning" class="activity-progress-warning" role="status">
            <Icon name="exclamationTriangle" size="sm" />
            <span>{{ t('admin.activities.progress.noParticipantWarning') }}</span>
          </div>

          <div class="activity-metric-grid">
            <article>
              <span><Icon name="users" size="sm" /></span>
              <small>{{ t('admin.activities.progress.metrics.participants') }}</small>
              <strong>{{ formatCount(campaignStats.joined_user_count) }}</strong>
            </article>
            <article>
              <span><Icon name="badge" size="sm" /></span>
              <small>{{ t('admin.activities.progress.metrics.tickets') }}</small>
              <strong>{{ formatCount(campaignStats.joined_ticket_count) }}</strong>
            </article>
            <article>
              <span><Icon name="chartBar" size="sm" /></span>
              <small>{{ statsMetricLabel }}</small>
              <strong>{{ statsMetricValueText(campaignStats.joined_metric_total) }}</strong>
            </article>
            <article>
              <span><Icon name="chart" size="sm" /></span>
              <small>{{ t('admin.activities.progress.metrics.averageTickets') }}</small>
              <strong>{{ formatDecimal(campaignStats.average_tickets_per_user) }}</strong>
            </article>
            <article>
              <span><Icon name="trendingUp" size="sm" /></span>
              <small>{{ t('admin.activities.progress.metrics.maxTickets') }}</small>
              <strong>{{ formatCount(campaignStats.max_ticket_count) }}</strong>
            </article>
            <article>
              <span><Icon name="gift" size="sm" /></span>
              <small>{{ t('admin.activities.progress.metrics.prizeQuantity') }}</small>
              <strong>{{ formatCount(campaignStats.prize_total_quantity) }}</strong>
            </article>
          </div>

          <div class="activity-progress-detail-grid">
            <article class="activity-delivery-panel">
              <header>
                <div>
                  <h4>{{ t('admin.activities.progress.deliveryTitle') }}</h4>
                  <p>{{ t('admin.activities.workspace.deliveryDescription') }}</p>
                </div>
                <strong>{{ deliveryProgressPercent }}%</strong>
              </header>
              <div class="activity-delivery-track" role="progressbar" aria-valuemin="0" aria-valuemax="100" :aria-valuenow="deliveryProgressPercent">
                <span :style="{ width: `${deliveryProgressPercent}%` }"></span>
              </div>
              <dl>
                <div>
                  <dt>{{ t('admin.activities.progress.metrics.pendingClaim') }}</dt>
                  <dd>{{ formatCount(campaignStats.pending_claim_count) }}</dd>
                </div>
                <div>
                  <dt>{{ t('admin.activities.progress.metrics.pendingDelivery') }}</dt>
                  <dd>{{ formatCount(campaignStats.pending_delivery_count) }}</dd>
                </div>
                <div>
                  <dt>{{ t('admin.activities.progress.metrics.delivered') }}</dt>
                  <dd>{{ formatCount(campaignStats.delivered_count) }}</dd>
                </div>
                <div>
                  <dt>{{ t('admin.activities.progress.metrics.rejected') }}</dt>
                  <dd>{{ formatCount(campaignStats.rejected_count) }}</dd>
                </div>
                <div class="activity-delivery-pending">
                  <dt>{{ t('admin.activities.progress.metrics.pendingAction') }}</dt>
                  <dd>{{ formatCount(campaignStats.pending_action_count) }}</dd>
                </div>
              </dl>
            </article>

            <article class="activity-latest-draw">
              <span class="activity-latest-draw-icon"><Icon name="sparkles" size="md" /></span>
              <div>
                <small>{{ t('admin.activities.progress.latestDrawTitle') }}</small>
                <template v-if="campaignStats.latest_draw">
                  <strong>{{ formatDateTime(campaignStats.latest_draw.executed_at) }}</strong>
                  <p>
                    {{ t('admin.activities.progress.latestDrawSummary', {
                      users: formatCount(campaignStats.latest_draw.total_users),
                      tickets: formatCount(campaignStats.latest_draw.total_tickets),
                      winners: formatCount(campaignStats.latest_draw.winner_count)
                    }) }}
                  </p>
                </template>
                <strong v-else>{{ t('admin.activities.progress.noDrawYet') }}</strong>
              </div>
            </article>
          </div>
        </div>
      </section>

      <section
        v-else
        :id="workspacePanelId('winners')"
        role="tabpanel"
        :aria-labelledby="workspaceTabId('winners')"
        class="activity-workspace-panel"
      >
        <div class="activity-progress-toolbar">
          <div class="activity-toolbar-copy">
            <h2>{{ t('admin.activities.winners.title') }}</h2>
            <p>{{ t('admin.activities.winners.description') }}</p>
          </div>
          <label class="activity-campaign-select">
            <span>{{ t('admin.activities.workspace.filterCampaign') }}</span>
            <span class="activity-select-control">
              <select v-model.number="winnerCampaignId" @change="reloadWinners">
                <option :value="0">{{ t('admin.activities.winners.allCampaigns') }}</option>
                <option v-for="campaign in campaigns" :key="campaign.id" :value="campaign.id">{{ campaign.name }}</option>
              </select>
              <Icon name="chevronDown" size="xs" />
            </span>
          </label>
        </div>

        <div class="activity-table-shell winner-table-shell">
          <div class="activity-desktop-table">
            <div class="activity-table-scroll">
              <table class="winner-table">
                <thead>
                  <tr>
                    <th>{{ t('admin.activities.winners.columns.user') }}</th>
                    <th>{{ t('admin.activities.winners.columns.campaign') }}</th>
                    <th>{{ t('admin.activities.winners.columns.prize') }}</th>
                    <th>{{ t('admin.activities.winners.columns.claim') }}</th>
                    <th>{{ t('common.status') }}</th>
                    <th>{{ t('admin.activities.winners.columns.createdAt') }}</th>
                    <th class="activity-actions-heading">{{ t('common.actions') }}</th>
                  </tr>
                </thead>
                <tbody>
                  <tr v-if="winnersLoading">
                    <td colspan="7">
                      <div class="activity-table-state">
                        <Icon name="refresh" size="md" class="activity-spin" />
                        <span>{{ t('common.loading') }}</span>
                      </div>
                    </td>
                  </tr>
                  <tr v-else-if="winners.length === 0">
                    <td colspan="7">
                      <div class="activity-table-state">
                        <span class="activity-state-icon"><Icon name="badge" size="lg" /></span>
                        <strong>{{ t('admin.activities.winners.empty') }}</strong>
                        <p>{{ t('admin.activities.workspace.emptyWinnersHint') }}</p>
                      </div>
                    </td>
                  </tr>
                  <tr v-for="winner in winners" v-else :key="winner.id">
                    <td>
                      <div class="activity-name-cell">
                        <strong>{{ winner.user_email || winner.user_username || winner.masked_user }}</strong>
                        <span>#{{ winner.user_id }}</span>
                      </div>
                    </td>
                    <td><span class="winner-campaign-name">{{ winner.campaign_name || `#${winner.campaign_id}` }}</span></td>
                    <td>
                      <div class="activity-rule-cell">
                        <strong>{{ winner.prize_name }}</strong>
                        <span>{{ prizeAmountText(winner.prize_type, winner.prize_amount) }}</span>
                      </div>
                    </td>
                    <td>
                      <button v-if="winner.claim_info" type="button" class="activity-text-action" @click="showClaimInfo(winner)">
                        {{ t('admin.activities.winners.viewClaim') }}
                      </button>
                      <span v-else class="activity-muted-text">{{ claimStatusLabel(winner.claim_status) }}</span>
                    </td>
                    <td>
                      <span :class="['admin-status-badge', winnerStatusClass(winner.status)]">
                        <i></i>
                        {{ winnerStatusLabel(winner.status) }}
                      </span>
                    </td>
                    <td><span class="activity-date-cell">{{ formatDateTime(winner.created_at) }}</span></td>
                    <td>
                      <div class="winner-row-actions">
                        <button
                          type="button"
                          class="btn btn-secondary btn-sm"
                          :disabled="!canDeliverWinner(winner)"
                          @click="openWinnerAction(winner, 'deliver')"
                        >
                          <Icon name="checkCircle" size="sm" />
                          {{ t('admin.activities.winners.deliver') }}
                        </button>
                        <button
                          type="button"
                          class="activity-icon-action activity-icon-action-danger"
                          :disabled="winner.status === 'delivered' || winner.status === 'rejected'"
                          :title="t('admin.activities.winners.reject')"
                          :aria-label="t('admin.activities.winners.reject')"
                          @click="openWinnerAction(winner, 'reject')"
                        >
                          <Icon name="xCircle" size="sm" />
                        </button>
                      </div>
                    </td>
                  </tr>
                </tbody>
              </table>
            </div>
          </div>

          <div class="activity-mobile-list">
            <div v-if="winnersLoading" class="activity-card-state">
              <Icon name="refresh" size="md" class="activity-spin" />
              <span>{{ t('common.loading') }}</span>
            </div>
            <div v-else-if="winners.length === 0" class="activity-card-state">
              <span class="activity-state-icon"><Icon name="badge" size="lg" /></span>
              <strong>{{ t('admin.activities.winners.empty') }}</strong>
              <p>{{ t('admin.activities.workspace.emptyWinnersHint') }}</p>
            </div>
            <article v-for="winner in winners" v-else :key="winner.id" class="winner-mobile-card">
              <header>
                <div>
                  <strong>{{ winner.user_email || winner.user_username || winner.masked_user }}</strong>
                  <span>#{{ winner.user_id }}</span>
                </div>
                <span :class="['admin-status-badge', winnerStatusClass(winner.status)]">
                  <i></i>
                  {{ winnerStatusLabel(winner.status) }}
                </span>
              </header>
              <div class="winner-mobile-prize">
                <span class="winner-prize-icon"><Icon name="gift" size="sm" /></span>
                <div>
                  <small>{{ winner.campaign_name || `#${winner.campaign_id}` }}</small>
                  <strong>{{ winner.prize_name }}</strong>
                  <span>{{ prizeAmountText(winner.prize_type, winner.prize_amount) }}</span>
                </div>
              </div>
              <div class="winner-mobile-meta">
                <span>{{ t('admin.activities.winners.columns.createdAt') }}</span>
                <strong>{{ formatDateTime(winner.created_at) }}</strong>
              </div>
              <footer>
                <button v-if="winner.claim_info" type="button" class="btn btn-secondary btn-sm" @click="showClaimInfo(winner)">
                  <Icon name="eye" size="sm" />
                  {{ t('admin.activities.winners.viewClaim') }}
                </button>
                <button
                  type="button"
                  class="btn btn-primary btn-sm"
                  :disabled="!canDeliverWinner(winner)"
                  @click="openWinnerAction(winner, 'deliver')"
                >
                  <Icon name="checkCircle" size="sm" />
                  {{ t('admin.activities.winners.deliver') }}
                </button>
                <button
                  type="button"
                  class="btn btn-secondary btn-sm activity-mobile-end"
                  :disabled="winner.status === 'delivered' || winner.status === 'rejected'"
                  @click="openWinnerAction(winner, 'reject')"
                >
                  <Icon name="xCircle" size="sm" />
                  {{ t('admin.activities.winners.reject') }}
                </button>
              </footer>
            </article>
          </div>

          <footer v-if="winnerPagination.total > 0" class="activity-table-footer">
            <Pagination
              :page="winnerPagination.page"
              :page-size="winnerPagination.page_size"
              :total="winnerPagination.total"
              @update:page="setWinnerPage"
              @update:pageSize="setWinnerPageSize"
            />
          </footer>
        </div>
      </section>
    </div>

    <Teleport to="body">
      <div v-if="dialogOpen" class="activity-drawer-backdrop" role="presentation" @click.self="closeEditor">
        <form
          class="activity-editor-drawer"
          role="dialog"
          aria-modal="true"
          :aria-label="editingCampaign ? t('admin.activities.edit') : t('admin.activities.create')"
          @submit.prevent="submitForm"
        >
          <header class="activity-editor-header">
            <div>
              <span class="activity-editor-eyebrow">{{ t('admin.activities.workspace.configuration') }}</span>
              <h2>{{ editingCampaign ? t('admin.activities.edit') : t('admin.activities.create') }}</h2>
              <p>{{ t('admin.activities.formDescription') }}</p>
            </div>
            <button
              type="button"
              class="activity-dialog-close"
              :disabled="saving"
              :aria-label="t('common.close')"
              @click="closeEditor"
            >
              <Icon name="x" size="sm" />
            </button>
          </header>

          <div class="activity-editor-body">
            <section class="activity-form-section">
              <header>
                <span><Icon name="document" size="sm" /></span>
                <div>
                  <h3>{{ t('admin.activities.workspace.basicSection') }}</h3>
                  <p>{{ t('admin.activities.workspace.basicSectionHint') }}</p>
                </div>
              </header>
              <div class="activity-form-grid">
                <label class="activity-form-field activity-form-field-wide">
                  <span>{{ t('admin.activities.fields.name') }}</span>
                  <input v-model.trim="form.name" class="input" required />
                </label>
                <label class="activity-form-field">
                  <span>{{ t('common.status') }}</span>
                  <select v-model="form.status" class="input">
                    <option v-for="option in statusOptions" :key="option.value" :value="option.value">{{ option.label }}</option>
                  </select>
                </label>
                <label class="activity-form-field">
                  <span>{{ t('admin.activities.fields.sortOrder') }}</span>
                  <input v-model.number="form.sort_order" type="number" class="input" />
                </label>
                <label class="activity-form-field">
                  <span>{{ t('admin.activities.fields.startsAt') }}</span>
                  <input v-model="form.starts_at_str" type="datetime-local" class="input" required />
                </label>
                <label class="activity-form-field">
                  <span>{{ t('admin.activities.fields.endsAt') }}</span>
                  <input v-model="form.ends_at_str" type="datetime-local" class="input" required />
                </label>
                <label class="activity-form-field">
                  <span>{{ t('admin.activities.fields.drawAt') }}</span>
                  <input v-model="form.draw_at_str" type="datetime-local" class="input" />
                </label>
                <label class="activity-form-field">
                  <span>{{ t('admin.activities.fields.timezone') }}</span>
                  <input v-model.trim="form.timezone" class="input" required />
                </label>
                <label class="activity-form-field activity-form-field-wide">
                  <span>{{ t('admin.activities.fields.coverUrl') }}</span>
                  <input v-model.trim="form.cover_url" class="input" type="url" placeholder="https://" />
                </label>
                <label class="activity-form-field activity-form-field-wide">
                  <span>{{ t('admin.activities.fields.description') }}</span>
                  <textarea v-model.trim="form.description" class="input activity-description-input"></textarea>
                </label>
              </div>
              <div class="activity-switch-grid">
                <label class="activity-switch-row">
                  <input v-model="form.public_enabled" type="checkbox" />
                  <span class="activity-switch-track"><i></i></span>
                  <span>
                    <strong>{{ t('admin.activities.fields.publicEnabled') }}</strong>
                    <small>{{ t('admin.activities.workspace.publicEnabledHint') }}</small>
                  </span>
                </label>
                <label class="activity-form-field activity-public-count-field">
                  <span>{{ t('admin.activities.fields.publicParticipantCount') }}</span>
                  <select v-model="form.public_participant_count" class="input">
                    <option v-for="option in publicParticipantCountOptions" :key="option.value" :value="option.value">{{ option.label }}</option>
                  </select>
                </label>
              </div>
            </section>

            <section class="activity-form-section">
              <header>
                <span><Icon name="chartBar" size="sm" /></span>
                <div>
                  <h3>{{ t('admin.activities.rule.title') }}</h3>
                  <p>{{ t('admin.activities.workspace.ruleSectionHint') }}</p>
                </div>
              </header>
              <div class="activity-form-grid activity-rule-grid">
                <label class="activity-form-field">
                  <span>{{ t('admin.activities.rule.metric') }}</span>
                  <select v-model="form.rule_config.metric" class="input">
                    <option v-for="option in metricOptions" :key="option.value" :value="option.value">{{ option.label }}</option>
                  </select>
                </label>
                <label class="activity-form-field">
                  <span>{{ t('admin.activities.rule.threshold') }}</span>
                  <input v-model.number="form.rule_config.threshold" type="number" min="0" step="0.0001" class="input" />
                </label>
                <label class="activity-form-field">
                  <span>{{ t('admin.activities.rule.periodType') }}</span>
                  <select v-model="form.rule_config.period_type" class="input">
                    <option v-for="option in periodOptions" :key="option.value" :value="option.value">{{ option.label }}</option>
                  </select>
                </label>
                <label v-if="form.rule_config.period_type === 'rolling_days'" class="activity-form-field">
                  <span>{{ t('admin.activities.rule.rollingDays') }}</span>
                  <input v-model.number="form.rule_config.rolling_days" type="number" min="1" class="input" />
                </label>
                <label v-if="form.rule_config.period_type === 'fixed_range'" class="activity-form-field">
                  <span>{{ t('admin.activities.rule.periodStart') }}</span>
                  <input v-model="form.rule_period_start_str" type="datetime-local" class="input" />
                </label>
                <label v-if="form.rule_config.period_type === 'fixed_range'" class="activity-form-field">
                  <span>{{ t('admin.activities.rule.periodEnd') }}</span>
                  <input v-model="form.rule_period_end_str" type="datetime-local" class="input" />
                </label>
                <label class="activity-form-field">
                  <span>{{ t('admin.activities.rule.ticketMode') }}</span>
                  <select v-model="form.rule_config.ticket_mode" class="input">
                    <option v-for="option in ticketModeOptions" :key="option.value" :value="option.value">{{ option.label }}</option>
                  </select>
                </label>
                <label v-if="form.rule_config.ticket_mode === 'fixed'" class="activity-form-field">
                  <span>{{ t('admin.activities.rule.fixedTickets') }}</span>
                  <input v-model.number="form.rule_config.fixed_tickets" type="number" min="1" class="input" />
                </label>
                <template v-if="form.rule_config.ticket_mode === 'proportional'">
                  <label class="activity-form-field">
                    <span>{{ t('admin.activities.rule.unitAmount') }}</span>
                    <input v-model.number="form.rule_config.unit_amount" type="number" min="0.0001" step="0.0001" class="input" />
                  </label>
                  <label class="activity-form-field">
                    <span>{{ t('admin.activities.rule.ticketsPerUnit') }}</span>
                    <input v-model.number="form.rule_config.tickets_per_unit" type="number" min="1" class="input" />
                  </label>
                  <label class="activity-form-field">
                    <span>{{ t('admin.activities.rule.maxTicketsPerUser') }}</span>
                    <input v-model.number="form.rule_config.max_tickets_per_user" type="number" min="0" class="input" />
                  </label>
                </template>
                <label v-if="form.rule_config.ticket_mode === 'tiered'" class="activity-form-field">
                  <span>{{ t('admin.activities.rule.tierMode') }}</span>
                  <select v-model="form.rule_config.tier_mode" class="input">
                    <option v-for="option in tierModeOptions" :key="option.value" :value="option.value">{{ option.label }}</option>
                  </select>
                </label>
              </div>
              <div v-if="form.rule_config.ticket_mode === 'tiered'" class="activity-tier-list">
                <div v-for="(tier, index) in form.rule_config.tiers" :key="index">
                  <label class="activity-form-field">
                    <span>{{ t('admin.activities.rule.tierThreshold') }}</span>
                    <input v-model.number="tier.threshold" type="number" min="0" step="0.0001" class="input" />
                  </label>
                  <label class="activity-form-field">
                    <span>{{ t('admin.activities.rule.tierTickets') }}</span>
                    <input v-model.number="tier.tickets" type="number" min="1" class="input" />
                  </label>
                  <button
                    type="button"
                    class="activity-icon-action activity-icon-action-danger"
                    :aria-label="t('common.delete')"
                    @click="removeTier(index)"
                  >
                    <Icon name="trash" size="sm" />
                  </button>
                </div>
                <button type="button" class="activity-add-inline" @click="addTier">
                  <Icon name="plus" size="sm" />
                  {{ t('admin.activities.rule.addTier') }}
                </button>
              </div>
            </section>

            <section class="activity-form-section">
              <header class="activity-prize-section-header">
                <span><Icon name="gift" size="sm" /></span>
                <div>
                  <h3>{{ t('admin.activities.prizes.title') }}</h3>
                  <p>{{ t('admin.activities.workspace.prizeSectionHint') }}</p>
                </div>
                <button type="button" class="btn btn-secondary btn-sm" @click="addPrize">
                  <Icon name="plus" size="sm" />
                  {{ t('admin.activities.prizes.add') }}
                </button>
              </header>
              <div class="activity-prize-list">
                <article v-for="(prize, prizeIndex) in form.prizes" :key="prizeIndex" class="activity-prize-editor">
                  <header>
                    <div>
                      <span>{{ t('admin.activities.workspace.prizeNumber', { number: prizeIndex + 1 }) }}</span>
                      <strong>{{ prize.name || t('admin.activities.prizes.defaultName') }}</strong>
                    </div>
                    <button
                      type="button"
                      class="activity-icon-action activity-icon-action-danger"
                      :disabled="form.prizes.length <= 1"
                      :aria-label="t('common.delete')"
                      @click="removePrize(prizeIndex)"
                    >
                      <Icon name="trash" size="sm" />
                    </button>
                  </header>
                  <div class="activity-form-grid">
                    <label class="activity-form-field activity-form-field-wide">
                      <span>{{ t('admin.activities.prizes.name') }}</span>
                      <input v-model.trim="prize.name" class="input" required />
                    </label>
                    <label class="activity-form-field">
                      <span>{{ t('admin.activities.prizes.type') }}</span>
                      <select v-model="prize.prize_type" class="input" @change="syncPrizeClaimFields(prize)">
                        <option v-for="option in prizeTypeOptions" :key="option.value" :value="option.value">{{ option.label }}</option>
                      </select>
                    </label>
                    <label class="activity-form-field">
                      <span>{{ t('admin.activities.prizes.amount') }}</span>
                      <input v-model.number="prize.amount" type="number" min="0" step="0.0001" class="input" />
                    </label>
                    <label class="activity-form-field">
                      <span>{{ t('admin.activities.prizes.quantity') }}</span>
                      <input v-model.number="prize.quantity" type="number" min="1" class="input" />
                    </label>
                    <label class="activity-form-field">
                      <span>{{ t('admin.activities.fields.sortOrder') }}</span>
                      <input v-model.number="prize.sort_order" type="number" class="input" />
                    </label>
                  </div>
                  <div class="activity-prize-switches">
                    <label class="activity-switch-row">
                      <input v-model="prize.enabled" type="checkbox" />
                      <span class="activity-switch-track"><i></i></span>
                      <span><strong>{{ t('common.enabled') }}</strong></span>
                    </label>
                    <label class="activity-switch-row">
                      <input v-model="prize.requires_claim_info" type="checkbox" @change="syncPrizeClaimFields(prize)" />
                      <span class="activity-switch-track"><i></i></span>
                      <span><strong>{{ t('admin.activities.prizes.requiresClaim') }}</strong></span>
                    </label>
                  </div>
                  <div v-if="prize.requires_claim_info" class="activity-claim-field-list">
                    <header>
                      <div>
                        <strong>{{ t('admin.activities.prizes.claimFields') }}</strong>
                        <span>{{ t('admin.activities.workspace.claimFieldsHint') }}</span>
                      </div>
                      <button type="button" class="activity-add-inline" @click="addClaimField(prize)">
                        <Icon name="plus" size="sm" />
                        {{ t('admin.activities.prizes.addClaimField') }}
                      </button>
                    </header>
                    <div v-for="(field, fieldIndex) in prize.claim_fields" :key="fieldIndex" class="activity-claim-field-row">
                      <input v-model.trim="field.key" class="input" :placeholder="t('admin.activities.prizes.claimFieldKey')" />
                      <input v-model.trim="field.label" class="input" :placeholder="t('admin.activities.prizes.claimFieldLabel')" />
                      <select v-model="field.type" class="input">
                        <option value="text">text</option>
                        <option value="phone">phone</option>
                        <option value="email">email</option>
                        <option value="textarea">textarea</option>
                      </select>
                      <label class="activity-required-field">
                        <input v-model="field.required" type="checkbox" />
                        <span>{{ t('admin.activities.prizes.required') }}</span>
                      </label>
                      <button
                        type="button"
                        class="activity-icon-action activity-icon-action-danger"
                        :aria-label="t('common.delete')"
                        @click="removeClaimField(prize, fieldIndex)"
                      >
                        <Icon name="trash" size="sm" />
                      </button>
                    </div>
                  </div>
                </article>
              </div>
            </section>
          </div>

          <footer class="activity-editor-footer">
            <span>{{ t('admin.activities.workspace.requiredHint') }}</span>
            <div>
              <button type="button" class="btn btn-secondary" :disabled="saving" @click="closeEditor">{{ t('common.cancel') }}</button>
              <button type="submit" class="btn btn-primary" :disabled="saving">
                <Icon v-if="saving" name="refresh" size="sm" class="activity-spin" />
                {{ saving ? t('common.saving') : t('common.save') }}
              </button>
            </div>
          </footer>
        </form>
      </div>
    </Teleport>

    <Teleport to="body">
      <div v-if="campaignActionDialog.open" class="activity-dialog-backdrop" @click.self="closeCampaignAction">
        <section class="activity-confirm-dialog" role="alertdialog" aria-modal="true" :aria-labelledby="'campaign-action-title'">
          <span :class="['activity-confirm-icon', { 'activity-confirm-icon-danger': campaignActionDialog.type === 'end' }]">
            <Icon :name="campaignActionDialog.type === 'draw' ? 'sparkles' : 'exclamationTriangle'" size="md" />
          </span>
          <div>
            <h2 id="campaign-action-title">{{ campaignActionTitle }}</h2>
            <p>{{ campaignActionMessage }}</p>
          </div>
          <footer>
            <button type="button" class="btn btn-secondary" :disabled="campaignActionDialog.busy" @click="closeCampaignAction">{{ t('common.cancel') }}</button>
            <button
              type="button"
              :class="['btn', campaignActionDialog.type === 'draw' ? 'btn-primary' : 'btn-danger']"
              :disabled="campaignActionDialog.busy"
              @click="confirmCampaignAction"
            >
              <Icon v-if="campaignActionDialog.busy" name="refresh" size="sm" class="activity-spin" />
              {{ campaignActionDialog.type === 'draw' ? t('admin.activities.drawNow') : t('admin.activities.end') }}
            </button>
          </footer>
        </section>
      </div>
    </Teleport>

    <Teleport to="body">
      <div v-if="winnerActionDialog.open" class="activity-dialog-backdrop" @click.self="closeWinnerAction">
        <form class="winner-action-dialog" role="dialog" aria-modal="true" @submit.prevent="confirmWinnerAction">
          <header>
            <span :class="{ 'winner-action-danger': winnerActionDialog.type === 'reject' }">
              <Icon :name="winnerActionDialog.type === 'deliver' ? 'checkCircle' : 'xCircle'" size="md" />
            </span>
            <div>
              <h2>{{ winnerActionDialog.type === 'deliver' ? t('admin.activities.winners.deliver') : t('admin.activities.winners.reject') }}</h2>
              <p>{{ winnerActionDialog.winner?.prize_name }} · {{ winnerActionDialog.winner?.campaign_name }}</p>
            </div>
          </header>
          <label class="activity-form-field">
            <span>{{ winnerActionDialog.type === 'deliver' ? t('admin.activities.winners.deliverNotePrompt') : t('admin.activities.winners.rejectNotePrompt') }}</span>
            <textarea v-model="winnerActionDialog.note" class="input winner-action-note" autofocus></textarea>
          </label>
          <footer>
            <button type="button" class="btn btn-secondary" :disabled="winnerActionDialog.busy" @click="closeWinnerAction">{{ t('common.cancel') }}</button>
            <button
              type="submit"
              :class="['btn', winnerActionDialog.type === 'deliver' ? 'btn-primary' : 'btn-danger']"
              :disabled="winnerActionDialog.busy"
            >
              <Icon v-if="winnerActionDialog.busy" name="refresh" size="sm" class="activity-spin" />
              {{ t('common.confirm') }}
            </button>
          </footer>
        </form>
      </div>
    </Teleport>

    <Teleport to="body">
      <div v-if="claimInfoWinner" class="activity-dialog-backdrop" @click.self="closeClaimInfo">
        <section class="claim-info-dialog" role="dialog" aria-modal="true" :aria-labelledby="'claim-info-title'">
          <header>
            <div>
              <span>{{ t('admin.activities.winners.claimInfoEyebrow') }}</span>
              <h2 id="claim-info-title">{{ t('admin.activities.winners.claimInfoTitle') }}</h2>
              <p>{{ claimInfoWinner.user_email || claimInfoWinner.user_username || claimInfoWinner.masked_user }}</p>
            </div>
            <button type="button" class="activity-dialog-close" :aria-label="t('common.close')" @click="closeClaimInfo">
              <Icon name="x" size="sm" />
            </button>
          </header>
          <dl>
            <div v-for="entry in claimInfoEntries" :key="entry.key">
              <dt>{{ entry.key }}</dt>
              <dd>{{ entry.value }}</dd>
            </div>
          </dl>
          <footer>
            <button type="button" class="btn btn-primary" @click="closeClaimInfo">{{ t('common.close') }}</button>
          </footer>
        </section>
      </div>
    </Teleport>
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import AppLayout from '@/components/layout/AppLayout.vue'
import Pagination from '@/components/common/Pagination.vue'
import Icon from '@/components/icons/Icon.vue'
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
type ActivityWorkspace = 'campaigns' | 'progress' | 'winners'
type CampaignActionType = 'draw' | 'end'
type WinnerActionType = 'deliver' | 'reject'

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
const activeWorkspace = ref<ActivityWorkspace>('campaigns')
const keyword = ref('')
const statusFilter = ref('')
const winnerCampaignId = ref(0)
const selectedStatsCampaignId = ref(0)
const editingCampaign = ref<ActivityCampaign | null>(null)
const claimInfoWinner = ref<ActivityWinner | null>(null)
const campaignPagination = reactive({ page: 1, page_size: 20, total: 0 })
const winnerPagination = reactive({ page: 1, page_size: 20, total: 0 })
const campaignActionDialog = reactive<{
  open: boolean
  type: CampaignActionType
  campaign: ActivityCampaign | null
  busy: boolean
}>({
  open: false,
  type: 'draw',
  campaign: null,
  busy: false,
})
const winnerActionDialog = reactive<{
  open: boolean
  type: WinnerActionType
  winner: ActivityWinner | null
  note: string
  busy: boolean
}>({
  open: false,
  type: 'deliver',
  winner: null,
  note: '',
  busy: false,
})

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
const activePanelLoading = computed(() => {
  if (activeWorkspace.value === 'progress') return statsLoading.value
  if (activeWorkspace.value === 'winners') return winnersLoading.value
  return loading.value
})
const workspaceTabs = computed(() => [
  {
    key: 'campaigns' as const,
    label: t('admin.activities.workspace.tabs.campaigns'),
    icon: 'gift' as const,
    count: campaignPagination.total,
  },
  {
    key: 'progress' as const,
    label: t('admin.activities.workspace.tabs.progress'),
    icon: 'chartBar' as const,
    count: null,
  },
  {
    key: 'winners' as const,
    label: t('admin.activities.workspace.tabs.winners'),
    icon: 'badge' as const,
    count: winnerPagination.total,
  },
])
const deliveryProgressPercent = computed(() => {
  const stats = campaignStats.value
  if (!stats || stats.winner_count <= 0) return 0
  return Math.min(100, Math.round((stats.delivered_count / stats.winner_count) * 100))
})
const campaignActionTitle = computed(() => (
  campaignActionDialog.type === 'draw'
    ? t('admin.activities.dialogs.drawTitle')
    : t('admin.activities.dialogs.endTitle')
))
const campaignActionMessage = computed(() => {
  const campaign = campaignActionDialog.campaign
  if (!campaign) return ''
  return campaignActionDialog.type === 'draw'
    ? t('admin.activities.confirmDraw', { name: campaign.name })
    : t('admin.activities.confirmEnd', { name: campaign.name })
})
const claimInfoEntries = computed(() => Object.entries(claimInfoWinner.value?.claim_info || {}).map(([key, value]) => ({
  key,
  value: formatClaimInfoValue(value),
})))

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
  activeWorkspace.value = 'progress'
  selectedStatsCampaignId.value = campaign.id
  void loadCampaignStats()
}

function selectWorkspace(workspace: ActivityWorkspace): void {
  activeWorkspace.value = workspace
  if (workspace === 'progress' && selectedStatsCampaignId.value && !campaignStats.value) {
    void loadCampaignStats()
  }
}

function handleWorkspaceKeydown(event: KeyboardEvent, index: number): void {
  const keys = workspaceTabs.value.map(tab => tab.key)
  let nextIndex = index
  if (event.key === 'ArrowRight') nextIndex = (index + 1) % keys.length
  else if (event.key === 'ArrowLeft') nextIndex = (index - 1 + keys.length) % keys.length
  else if (event.key === 'Home') nextIndex = 0
  else if (event.key === 'End') nextIndex = keys.length - 1
  else return

  event.preventDefault()
  const nextKey = keys[nextIndex]
  if (!nextKey) return
  selectWorkspace(nextKey)
  requestAnimationFrame(() => document.getElementById(workspaceTabId(nextKey))?.focus())
}

function workspaceTabId(workspace: ActivityWorkspace): string {
  return `admin-activity-tab-${workspace}`
}

function workspacePanelId(workspace: ActivityWorkspace): string {
  return `admin-activity-panel-${workspace}`
}

function refreshActiveWorkspace(): void {
  if (activeWorkspace.value === 'progress') {
    void loadCampaignStats()
    return
  }
  if (activeWorkspace.value === 'winners') {
    void reloadWinners()
    return
  }
  void reloadCampaigns()
}

function resetForm(): void {
  Object.assign(form, createDefaultForm())
}

function openCreate(): void {
  editingCampaign.value = null
  resetForm()
  dialogOpen.value = true
}

function closeEditor(): void {
  if (saving.value) return
  dialogOpen.value = false
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

async function runDraw(campaign: ActivityCampaign): Promise<boolean> {
  try {
    selectedStatsCampaignId.value = campaign.id
    const result = await adminActivityAPI.runDraw(campaign.id)
    appStore.showSuccess(t('admin.activities.drawSuccess', { count: result.winner_count }))
    await Promise.all([reloadCampaigns(), reloadWinners()])
    return true
  } catch (error) {
    appStore.showError(extractApiErrorMessage(error, t('admin.activities.drawFailed')))
    return false
  }
}

async function endCampaign(campaign: ActivityCampaign): Promise<boolean> {
  try {
    selectedStatsCampaignId.value = campaign.id
    await adminActivityAPI.endCampaign(campaign.id)
    appStore.showSuccess(t('admin.activities.endSuccess'))
    await reloadCampaigns()
    return true
  } catch (error) {
    appStore.showError(extractApiErrorMessage(error, t('common.error')))
    return false
  }
}

function openCampaignAction(campaign: ActivityCampaign, type: CampaignActionType): void {
  campaignActionDialog.campaign = campaign
  campaignActionDialog.type = type
  campaignActionDialog.open = true
}

function closeCampaignAction(): void {
  if (campaignActionDialog.busy) return
  campaignActionDialog.open = false
  campaignActionDialog.campaign = null
}

async function confirmCampaignAction(): Promise<void> {
  const campaign = campaignActionDialog.campaign
  if (!campaign || campaignActionDialog.busy) return
  campaignActionDialog.busy = true
  try {
    const completed = campaignActionDialog.type === 'draw'
      ? await runDraw(campaign)
      : await endCampaign(campaign)
    if (!completed) return
    campaignActionDialog.open = false
    campaignActionDialog.campaign = null
  } finally {
    campaignActionDialog.busy = false
  }
}

function openWinnerAction(winner: ActivityWinner, type: WinnerActionType): void {
  if (type === 'deliver' && !canDeliverWinner(winner)) return
  if (winner.status === 'delivered' || winner.status === 'rejected') return
  winnerActionDialog.winner = winner
  winnerActionDialog.type = type
  winnerActionDialog.note = winner.admin_note || ''
  winnerActionDialog.open = true
}

function closeWinnerAction(): void {
  if (winnerActionDialog.busy) return
  winnerActionDialog.open = false
  winnerActionDialog.winner = null
  winnerActionDialog.note = ''
}

async function confirmWinnerAction(): Promise<void> {
  const winner = winnerActionDialog.winner
  if (!winner || winnerActionDialog.busy) return
  winnerActionDialog.busy = true
  try {
    const updated = winnerActionDialog.type === 'deliver'
      ? await adminActivityAPI.markWinnerDelivered(winner.id, winnerActionDialog.note)
      : await adminActivityAPI.rejectWinner(winner.id, winnerActionDialog.note)
    winners.value = winners.value.map(item => item.id === updated.id ? updated : item)
    appStore.showSuccess(t(
      winnerActionDialog.type === 'deliver'
        ? 'admin.activities.winners.deliverSuccess'
        : 'admin.activities.winners.rejectSuccess',
    ))
    if (selectedStatsCampaignId.value === updated.campaign_id) await loadCampaignStats()
    winnerActionDialog.open = false
    winnerActionDialog.winner = null
    winnerActionDialog.note = ''
  } catch (error) {
    appStore.showError(extractApiErrorMessage(error, t('common.error')))
  } finally {
    winnerActionDialog.busy = false
  }
}

function showClaimInfo(winner: ActivityWinner): void {
  claimInfoWinner.value = winner
}

function closeClaimInfo(): void {
  claimInfoWinner.value = null
}

function formatClaimInfoValue(value: unknown): string {
  if (value === null || value === undefined || value === '') return '-'
  if (typeof value === 'string') return value
  if (typeof value === 'number' || typeof value === 'boolean') return String(value)
  return JSON.stringify(value)
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

function campaignStatusClass(status: ActivityStatus): string {
  if (status === 'active') return 'admin-status-positive'
  if (status === 'paused') return 'admin-status-warning'
  if (status === 'ended') return 'admin-status-neutral'
  return 'admin-status-primary'
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

function winnerStatusClass(status: ActivityWinnerStatus): string {
  if (status === 'delivered') return 'admin-status-positive'
  if (status === 'pending_claim' || status === 'pending_delivery') return 'admin-status-warning'
  if (status === 'rejected' || status === 'expired') return 'admin-status-danger'
  return 'admin-status-neutral'
}

function canDeliverWinner(winner: ActivityWinner): boolean {
  return winner.status === 'pending_delivery'
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

<style scoped src="./ActivitiesView.css"></style>
