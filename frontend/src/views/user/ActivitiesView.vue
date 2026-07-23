<template>
  <AppLayout>
    <div class="benefits-page" :aria-busy="loading">
      <div v-if="loading && !hasLoaded" class="editorial-skeleton" aria-hidden="true">
        <div class="skeleton-rule"></div>
        <div class="skeleton-masthead">
          <div>
            <i></i>
            <strong></strong>
            <span></span>
          </div>
          <b></b>
        </div>
        <div class="skeleton-statline">
          <span v-for="index in 4" :key="index"></span>
        </div>
        <div class="skeleton-layout">
          <aside>
            <i v-for="index in 5" :key="index"></i>
          </aside>
          <main>
            <i></i>
            <strong></strong>
            <span></span>
          </main>
        </div>
      </div>

      <template v-else>
        <header class="benefits-masthead">
          <div class="masthead-topline">
            <span>{{ t('activities.editorial.issue') }}</span>
            <button
              type="button"
              class="refresh-link"
              :disabled="loading"
              :aria-label="t('common.refresh')"
              @click="loadAll"
            >
              <Icon name="refresh" size="sm" :class="{ 'refresh-icon-spinning': loading }" />
              <span>{{ t('common.refresh') }}</span>
            </button>
          </div>

          <div class="masthead-composition">
            <div class="masthead-title">
              <span class="masthead-kicker">{{ t('activities.title') }}</span>
              <h1>{{ t('activities.editorial.headline') }}</h1>
            </div>
            <p class="masthead-lead">{{ t('activities.editorial.lead') }}</p>
            <div class="masthead-mark" aria-hidden="true">
              <span>{{ t('activities.editorial.currentIssue') }}</span>
              <strong>{{ formatSerial(openCampaigns.length) }}</strong>
              <em>{{ t('activities.editorial.activityUnit', { count: openCampaigns.length }) }}</em>
            </div>
          </div>

          <dl class="benefits-statline" :aria-label="t('activities.editorial.overview')">
            <div>
              <dt>{{ t('activities.stats.activeCampaigns') }}</dt>
              <dd>{{ formatSerial(openCampaigns.length) }}</dd>
            </div>
            <div>
              <dt>{{ t('activities.stats.availableTickets') }}</dt>
              <dd>{{ formatSerial(availableTicketCount) }}</dd>
            </div>
            <div>
              <dt>{{ t('activities.stats.joinedTickets') }}</dt>
              <dd>{{ formatSerial(joinedTicketCount) }}</dd>
            </div>
            <div :class="{ 'statline-alert': pendingClaimWinners.length > 0 }">
              <dt>{{ t('activities.stats.pendingClaims') }}</dt>
              <dd>{{ formatSerial(pendingClaimWinners.length) }}</dd>
            </div>
          </dl>
        </header>

        <div class="benefits-workspace">
          <aside class="benefits-index">
            <div class="benefits-index-inner">
              <div class="index-heading">
                <span>{{ t('activities.editorial.index') }}</span>
                <p>{{ t('activities.editorial.indexHint') }}</p>
              </div>
              <nav
                ref="tablistRef"
                class="index-tabs"
                role="tablist"
                :aria-label="t('activities.tabs.label')"
              >
                <button
                  v-for="(tab, index) in tabs"
                  :id="tabId(tab.key)"
                  :key="tab.key"
                  type="button"
                  role="tab"
                  class="index-tab"
                  :class="{ 'index-tab-active': activeTab === tab.key }"
                  :aria-selected="activeTab === tab.key"
                  :aria-controls="panelId(tab.key)"
                  :tabindex="activeTab === tab.key ? 0 : -1"
                  @click="activeTab = tab.key"
                  @keydown="handleTabKeydown($event, index)"
                >
                  <span class="index-tab-number">{{ formatSerial(index + 1) }}</span>
                  <span class="index-tab-copy">
                    <strong>{{ tab.label }}</strong>
                    <small>{{ tab.note }}</small>
                  </span>
                  <em>{{ tab.count }}</em>
                </button>
              </nav>
            </div>
          </aside>

          <main class="benefits-stage">
            <header class="stage-heading">
              <span>{{ activeSection.eyebrow }}</span>
              <div>
                <h2>{{ activeSection.title }}</h2>
                <p>{{ activeSection.description }}</p>
              </div>
            </header>

            <Transition name="section-shift" mode="out-in">
              <section
                v-if="activeTab === 'open'"
                :id="panelId('open')"
                key="open"
                class="stage-panel campaign-editions"
                role="tabpanel"
                :aria-labelledby="tabId('open')"
              >
                <div v-if="openCampaigns.length === 0" class="editorial-empty">
                  <span><Icon name="gift" size="lg" /></span>
                  <strong>{{ t('activities.lottery.empty') }}</strong>
                  <p>{{ t('activities.editorial.emptyHint') }}</p>
                </div>

                <article
                  v-for="(campaign, index) in openCampaigns"
                  :key="campaign.id"
                  class="campaign-edition"
                  :class="campaignToneClass(index)"
                >
                  <div class="campaign-edition-bar">
                    <span>{{ t('activities.editorial.campaignPrefix') }} / {{ formatSerial(index + 1) }}</span>
                    <span class="campaign-state">
                      <i></i>
                      {{ participationStatusText(campaign) }}
                    </span>
                    <span>{{ t('activities.rule.drawAt') }} · {{ formatShortDateTime(campaign.draw_at) }}</span>
                  </div>

                  <div class="campaign-edition-main">
                    <div class="campaign-story">
                      <div class="campaign-context">
                        <span>{{ periodLabel(campaign) }}</span>
                        <span v-if="publicParticipantText(campaign)">{{ publicParticipantText(campaign) }}</span>
                      </div>
                      <h3>{{ campaign.name }}</h3>
                      <p>{{ campaign.description || t('activities.editorial.noDescription') }}</p>

                      <div class="campaign-progress">
                        <div class="campaign-progress-copy">
                          <span>{{ t('activities.editorial.progress') }}</span>
                          <strong>
                            {{ metricValueText(campaign.rule_config.metric, campaign.user_progress?.metric_value || 0) }}
                            <small>/ {{ metricValueText(campaign.rule_config.metric, campaign.rule_config.threshold) }}</small>
                          </strong>
                        </div>
                        <div
                          class="campaign-progress-track"
                          role="progressbar"
                          :aria-label="metricLabel(campaign.rule_config.metric)"
                          aria-valuemin="0"
                          aria-valuemax="100"
                          :aria-valuenow="progressPercent(campaign)"
                        >
                          <span :style="{ width: `${progressPercent(campaign)}%` }"></span>
                        </div>
                        <em>{{ progressPercent(campaign) }}%</em>
                      </div>

                      <ol class="campaign-route" :aria-label="t('activities.editorial.route')">
                        <li :class="{ 'route-complete': (campaign.user_progress?.ticket_count || 0) > 0 }">
                          <span>01</span>
                          <p>{{ t('activities.steps.qualify') }}</p>
                        </li>
                        <li :class="{ 'route-complete': Boolean(campaign.user_progress?.joined) }">
                          <span>02</span>
                          <p>{{ t('activities.steps.join') }}</p>
                        </li>
                        <li>
                          <span>03</span>
                          <p>{{ t('activities.steps.waitDraw') }}</p>
                        </li>
                      </ol>
                    </div>

                    <aside class="campaign-entry">
                      <div class="campaign-ticket-count">
                        <span>{{ t('activities.lottery.myTickets') }}</span>
                        <strong>{{ formatSerial(campaign.user_progress?.ticket_count || 0) }}</strong>
                        <em>{{ t('activities.editorial.ticketUnit') }}</em>
                      </div>
                      <p>{{ campaignGuidance(campaign) }}</p>
                      <button
                        type="button"
                        class="campaign-action"
                        :disabled="!canJoinCampaign(campaign)"
                        @click="joinCampaign(campaign)"
                      >
                        <span>{{ joinButtonLabel(campaign) }}</span>
                        <Icon name="arrowRight" size="sm" />
                      </button>
                    </aside>
                  </div>

                  <div class="campaign-ledger">
                    <section class="ledger-section prize-ledger">
                      <header>
                        <span>{{ t('activities.prizes.title') }}</span>
                        <em>{{ t('activities.editorial.itemsCount', { count: campaign.prizes?.length || 0 }) }}</em>
                      </header>
                      <div v-if="campaign.prizes?.length" class="ledger-list">
                        <div v-for="(prize, prizeIndex) in campaign.prizes" :key="prize.id || prize.name" class="ledger-row">
                          <span>{{ formatSerial(prizeIndex + 1) }}</span>
                          <div>
                            <strong>{{ prize.name }}</strong>
                            <small>{{ prizeTypeLabel(prize.prize_type) }} · {{ t('activities.prizes.quantity', { count: prize.quantity }) }}</small>
                          </div>
                          <em>{{ prizeAmountText(prize.prize_type, prize.amount) }}</em>
                        </div>
                      </div>
                      <p v-else class="ledger-empty">{{ t('activities.prizes.empty') }}</p>
                    </section>

                    <section class="ledger-section winner-ledger">
                      <header>
                        <span>{{ t('activities.winners.yesterday') }}</span>
                        <em>{{ t('activities.editorial.publicBoard') }}</em>
                      </header>
                      <div v-if="campaign.yesterday_winners?.length" class="winner-ticker">
                        <div v-for="winner in campaign.yesterday_winners.slice(0, 8)" :key="winner.id">
                          <span>{{ winner.masked_user }}</span>
                          <strong>{{ winner.prize_name }}</strong>
                        </div>
                      </div>
                      <p v-else class="ledger-empty">{{ t('activities.winners.emptyYesterday') }}</p>
                    </section>
                  </div>
                </article>
              </section>

              <section
                v-else-if="activeTab === 'joined'"
                :id="panelId('joined')"
                key="joined"
                class="stage-panel entry-journal"
                role="tabpanel"
                :aria-labelledby="tabId('joined')"
              >
                <div v-if="joinedCampaigns.length === 0" class="editorial-empty">
                  <span><Icon name="checkCircle" size="lg" /></span>
                  <strong>{{ t('activities.joined.empty') }}</strong>
                  <p>{{ t('activities.editorial.emptyHint') }}</p>
                </div>

                <article v-for="(campaign, index) in joinedCampaigns" :key="campaign.id" class="journal-entry">
                  <div class="journal-number">
                    <span>{{ t('activities.editorial.entry') }}</span>
                    <strong>{{ formatSerial(index + 1) }}</strong>
                  </div>
                  <div class="journal-story">
                    <span class="journal-status">{{ participationStatusText(campaign) }}</span>
                    <h3>{{ campaign.name }}</h3>
                    <p>
                      {{ t('activities.joined.summary', {
                        count: campaign.user_progress?.joined_tickets || 0,
                        time: formatDateTime(campaign.user_progress?.joined_at) || '-',
                      }) }}
                    </p>
                    <div class="journal-progress">
                      <span :style="{ width: `${progressPercent(campaign)}%` }"></span>
                    </div>
                  </div>
                  <dl class="journal-facts">
                    <div>
                      <dt>{{ t('activities.rule.drawAt') }}</dt>
                      <dd>{{ formatShortDateTime(campaign.draw_at) }}</dd>
                    </div>
                    <div>
                      <dt>{{ t('activities.joined.currentTickets') }}</dt>
                      <dd>{{ formatSerial(campaign.user_progress?.ticket_count || 0) }}</dd>
                    </div>
                    <div>
                      <dt>{{ t('activities.joined.joinedTickets') }}</dt>
                      <dd>{{ formatSerial(campaign.user_progress?.joined_tickets || 0) }}</dd>
                    </div>
                  </dl>
                  <div v-if="canUpdateJoinedTickets(campaign)" class="journal-action">
                    <p>{{ t('activities.joined.updateHint') }}</p>
                    <button
                      type="button"
                      class="text-action"
                      :disabled="isJoining(campaign.id)"
                      @click="joinCampaign(campaign)"
                    >
                      {{ t('activities.lottery.updateJoin') }}
                      <Icon name="arrowRight" size="sm" />
                    </button>
                  </div>
                </article>
              </section>

              <section
                v-else-if="activeTab === 'winners'"
                :id="panelId('winners')"
                key="winners"
                class="stage-panel winning-slips"
                role="tabpanel"
                :aria-labelledby="tabId('winners')"
              >
                <div v-if="winners.length === 0" class="editorial-empty">
                  <span><Icon name="badge" size="lg" /></span>
                  <strong>{{ t('activities.myWinners.empty') }}</strong>
                  <p>{{ t('activities.editorial.winnerEmptyHint') }}</p>
                </div>

                <article v-for="(winner, index) in winners" :key="winner.id" class="winning-slip">
                  <div class="winning-slip-index">
                    <span>WIN</span>
                    <strong>{{ formatSerial(index + 1) }}</strong>
                  </div>
                  <div class="winning-slip-main">
                    <span>{{ winner.campaign_name || `#${winner.campaign_id}` }}</span>
                    <h3>{{ winner.prize_name }}</h3>
                    <p>{{ formatDateTime(winner.created_at) }}</p>
                  </div>
                  <div class="winning-slip-status">
                    <span :class="winnerStatusClass(winner.status)">{{ winnerStatusLabel(winner.status) }}</span>
                    <button
                      v-if="winner.status === 'pending_claim'"
                      type="button"
                      class="claim-action"
                      @click="openClaim(winner)"
                    >
                      {{ t('activities.myWinners.submitClaim') }}
                      <Icon name="arrowRight" size="sm" />
                    </button>
                    <span v-else class="winning-slip-complete">
                      <Icon name="check" size="sm" />
                      {{ t('activities.editorial.recorded') }}
                    </span>
                  </div>
                </article>
              </section>

              <section
                v-else-if="activeTab === 'past'"
                :id="panelId('past')"
                key="past"
                class="stage-panel archive-list"
                role="tabpanel"
                :aria-labelledby="tabId('past')"
              >
                <div v-if="pastCampaigns.length === 0" class="editorial-empty">
                  <span><Icon name="inbox" size="lg" /></span>
                  <strong>{{ t('activities.past.empty') }}</strong>
                  <p>{{ t('activities.editorial.archiveEmptyHint') }}</p>
                </div>

                <article v-for="(campaign, index) in pastCampaigns" :key="campaign.id" class="archive-entry">
                  <div class="archive-date">
                    <span>{{ t('activities.editorial.archive') }}</span>
                    <strong>{{ formatSerial(index + 1) }}</strong>
                    <em>{{ formatShortDateTime(campaign.draw_at) }}</em>
                  </div>
                  <div class="archive-story">
                    <div>
                      <span class="archive-status">{{ t('activities.status.ended') }}</span>
                      <span v-if="campaign.user_progress?.joined" class="archive-joined">{{ t('activities.past.joined') }}</span>
                    </div>
                    <h3>{{ campaign.name }}</h3>
                    <p>{{ campaign.description || t('activities.editorial.noDescription') }}</p>
                    <dl>
                      <div>
                        <dt>{{ t('activities.rule.period') }}</dt>
                        <dd>{{ periodLabel(campaign) }}</dd>
                      </div>
                      <div>
                        <dt>{{ t('activities.joined.joinedTickets') }}</dt>
                        <dd>{{ campaign.user_progress?.joined_tickets || 0 }}</dd>
                      </div>
                    </dl>
                  </div>
                  <div class="archive-winners">
                    <span>{{ t('activities.winners.recent') }}</span>
                    <div v-if="campaign.recent_winners?.length">
                      <p v-for="winner in campaign.recent_winners.slice(0, 6)" :key="winner.id">
                        <span>{{ winner.masked_user }}</span>
                        <strong>{{ winner.prize_name }}</strong>
                      </p>
                    </div>
                    <p v-else class="ledger-empty">{{ t('activities.winners.emptyRecent') }}</p>
                  </div>
                </article>
              </section>

              <section
                v-else
                :id="panelId('affiliate')"
                key="affiliate"
                class="stage-panel affiliate-edition"
                role="tabpanel"
                :aria-labelledby="tabId('affiliate')"
              >
                <div v-if="affiliateLoading" class="affiliate-loading">
                  <span></span>
                  <p>{{ t('common.loading') }}</p>
                </div>
                <div v-else-if="affiliateError || !affiliateDetail" class="editorial-empty">
                  <span><Icon name="users" size="lg" /></span>
                  <strong>{{ affiliateError || t('affiliate.loadFailed') }}</strong>
                  <button type="button" class="text-action" @click="loadAffiliateDetail()">
                    {{ t('common.tryAgain') }}
                    <Icon name="arrowRight" size="sm" />
                  </button>
                </div>

                <template v-else>
                  <div class="affiliate-feature">
                    <div class="affiliate-feature-title">
                      <span>{{ t('activities.editorial.shareLabel') }}</span>
                      <h3>{{ t('activities.editorial.shareHeadline') }}</h3>
                      <p>{{ t('activities.affiliate.description') }}</p>
                    </div>
                    <div class="affiliate-rate">
                      <span>{{ t('affiliate.stats.rebateRate') }}</span>
                      <strong>{{ formattedRebateRate }}<small>%</small></strong>
                      <p>{{ t('affiliate.stats.rebateRateHint') }}</p>
                    </div>
                  </div>

                  <dl class="affiliate-statline">
                    <div>
                      <dt>{{ t('affiliate.stats.invitedUsers') }}</dt>
                      <dd>{{ formatCount(affiliateDetail.aff_count) }}</dd>
                    </div>
                    <div>
                      <dt>{{ periodIncomeTitle }}</dt>
                      <dd>{{ formatCurrency(affiliateDetail.period_rebate) }}</dd>
                    </div>
                    <div>
                      <dt>{{ t('affiliate.stats.totalQuota') }}</dt>
                      <dd>{{ formatCurrency(affiliateDetail.aff_history_quota) }}</dd>
                    </div>
                    <div>
                      <dt>{{ t('affiliate.stats.settlementMode') }}</dt>
                      <dd>{{ t('affiliate.stats.realtimeBalance') }}</dd>
                    </div>
                  </dl>

                  <div class="affiliate-toolbar">
                    <div class="period-presets" :aria-label="t('activities.editorial.periodFilter')">
                      <button
                        v-for="preset in periodPresets"
                        :key="preset"
                        type="button"
                        :class="{ 'period-preset-active': periodPreset === preset }"
                        @click="setPeriodPreset(preset)"
                      >
                        {{ t(`affiliate.period.presets.${preset}`) }}
                      </button>
                    </div>
                    <div class="period-dates">
                      <label>
                        <span>{{ t('affiliate.period.start') }}</span>
                        <input v-model="periodStartDate" type="date" @change="setCustomPeriod" />
                      </label>
                      <i aria-hidden="true"></i>
                      <label>
                        <span>{{ t('affiliate.period.end') }}</span>
                        <input v-model="periodEndDate" type="date" @change="setCustomPeriod" />
                      </label>
                    </div>
                  </div>

                  <div class="affiliate-share-sheet">
                    <div class="share-row">
                      <span>{{ t('affiliate.yourCode') }}</span>
                      <code>{{ affiliateDetail.aff_code }}</code>
                      <button type="button" @click="copyCode">
                        <Icon name="copy" size="sm" />
                        {{ t('affiliate.copyCode') }}
                      </button>
                    </div>
                    <div class="share-row">
                      <span>{{ t('affiliate.inviteLink') }}</span>
                      <code>{{ inviteLink }}</code>
                      <button type="button" @click="copyInviteLink">
                        <Icon name="copy" size="sm" />
                        {{ t('affiliate.copyLink') }}
                      </button>
                    </div>
                  </div>

                  <div class="affiliate-notes">
                    <div>
                      <span>01</span>
                      <p>{{ t('affiliate.weeklyQuota') }}</p>
                      <strong>{{ weeklyQuotaText }}</strong>
                    </div>
                    <div>
                      <span>02</span>
                      <p>{{ t('affiliate.codePolicy.title') }}</p>
                      <strong>{{ codePolicyText }}</strong>
                      <small>{{ codeExpiryText }}</small>
                    </div>
                    <div>
                      <span>03</span>
                      <p>{{ t('affiliate.tips.title') }}</p>
                      <strong>{{ t('affiliate.tips.line3') }}</strong>
                    </div>
                  </div>

                  <section class="invitee-ledger">
                    <header>
                      <div>
                        <span>{{ t('activities.editorial.inviteeLedger') }}</span>
                        <h3>{{ t('affiliate.invitees.title') }}</h3>
                      </div>
                      <p>{{ t('affiliate.description') }}</p>
                    </header>

                    <div v-if="affiliateDetail.invitees.length === 0" class="ledger-empty-block">
                      {{ t('affiliate.invitees.empty') }}
                    </div>
                    <div v-else class="invitee-table-wrap">
                      <table class="invitee-table">
                        <thead>
                          <tr>
                            <th>{{ t('affiliate.invitees.columns.user') }}</th>
                            <th>{{ t('affiliate.invitees.columns.bindSource') }}</th>
                            <th>
                              <button type="button" class="sort-link" @click="toggleSort('bound_at')">
                                {{ t('affiliate.invitees.columns.joinedAt') }}
                                <span>{{ sortIndicator('bound_at') }}</span>
                              </button>
                            </th>
                            <th>{{ t('affiliate.invitees.columns.status') }}</th>
                            <th>
                              <button type="button" class="sort-link" @click="toggleSort('period_consumption')">
                                {{ t('affiliate.invitees.columns.periodConsumption') }}
                                <span>{{ sortIndicator('period_consumption') }}</span>
                              </button>
                            </th>
                            <th>
                              <button type="button" class="sort-link" @click="toggleSort('period_rebate')">
                                {{ t('affiliate.invitees.columns.periodRebate') }}
                                <span>{{ sortIndicator('period_rebate') }}</span>
                              </button>
                            </th>
                            <th>
                              <button type="button" class="sort-link" @click="toggleSort('history_consumption')">
                                {{ t('affiliate.invitees.columns.historyConsumption') }}
                                <span>{{ sortIndicator('history_consumption') }}</span>
                              </button>
                            </th>
                            <th>
                              <button type="button" class="sort-link" @click="toggleSort('total_rebate')">
                                {{ t('affiliate.invitees.columns.rebate') }}
                                <span>{{ sortIndicator('total_rebate') }}</span>
                              </button>
                            </th>
                          </tr>
                        </thead>
                        <tbody>
                          <tr v-for="item in sortedInvitees" :key="item.user_id">
                            <td :data-label="t('affiliate.invitees.columns.user')">
                              <strong>{{ item.email || '-' }}</strong>
                              <small>{{ item.username || '-' }}</small>
                            </td>
                            <td :data-label="t('affiliate.invitees.columns.bindSource')">{{ formatBindSource(item.invite_bind_source) }}</td>
                            <td :data-label="t('affiliate.invitees.columns.joinedAt')">{{ formatDateTime(item.created_at) || '-' }}</td>
                            <td :data-label="t('affiliate.invitees.columns.status')">{{ formatInviteeStatus(item.status) }}</td>
                            <td :data-label="t('affiliate.invitees.columns.periodConsumption')">{{ formatCurrency(item.period_consumption) }}</td>
                            <td :data-label="t('affiliate.invitees.columns.periodRebate')" class="positive-value">{{ formatCurrency(item.period_rebate) }}</td>
                            <td :data-label="t('affiliate.invitees.columns.historyConsumption')">{{ formatCurrency(item.history_consumption) }}</td>
                            <td :data-label="t('affiliate.invitees.columns.rebate')" class="total-value">{{ formatCurrency(item.total_rebate) }}</td>
                          </tr>
                        </tbody>
                      </table>
                    </div>
                  </section>
                </template>
              </section>
            </Transition>
          </main>
        </div>
      </template>
    </div>

    <Teleport to="body">
      <div
        v-if="claimDialogOpen && selectedWinner"
        class="claim-sheet-backdrop"
        @click.self="closeClaimDialog"
        @keydown.esc="closeClaimDialog"
      >
        <form
          ref="claimDialogRef"
          class="claim-sheet"
          role="dialog"
          aria-modal="true"
          aria-labelledby="claim-dialog-title"
          tabindex="-1"
          @submit.prevent="submitClaim"
        >
          <div class="claim-sheet-accent"></div>
          <header class="claim-sheet-header">
            <div>
              <span>{{ t('activities.editorial.claimLabel') }}</span>
              <h2 id="claim-dialog-title">{{ t('activities.claim.title') }}</h2>
              <p>{{ selectedWinner.prize_name }}</p>
            </div>
            <button type="button" :aria-label="t('common.close')" @click="closeClaimDialog">
              <Icon name="x" size="sm" />
            </button>
          </header>
          <div class="claim-sheet-body">
            <template v-if="selectedWinner.claim_fields?.length">
              <label v-for="field in selectedWinner.claim_fields" :key="field.key" :for="`claim-${field.key}`">
                <span>
                  {{ field.label }}
                  <em v-if="field.required">*</em>
                </span>
                <textarea
                  v-if="field.type === 'textarea'"
                  :id="`claim-${field.key}`"
                  v-model.trim="claimForm[field.key]"
                  :required="field.required"
                ></textarea>
                <input
                  v-else
                  :id="`claim-${field.key}`"
                  v-model.trim="claimForm[field.key]"
                  :type="field.type === 'phone' ? 'tel' : 'text'"
                  :required="field.required"
                />
              </label>
            </template>
            <div v-else class="claim-sheet-notice">{{ t('activities.claim.noFields') }}</div>
          </div>
          <footer class="claim-sheet-actions">
            <button type="button" class="claim-cancel" @click="closeClaimDialog">{{ t('common.cancel') }}</button>
            <button type="submit" class="claim-submit" :disabled="claimSubmitting">
              {{ claimSubmitting ? t('common.saving') : t('activities.claim.submit') }}
              <Icon name="arrowRight" size="sm" />
            </button>
          </footer>
        </form>
      </div>
    </Teleport>
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, nextTick, onMounted, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import AppLayout from '@/components/layout/AppLayout.vue'
import Icon from '@/components/icons/Icon.vue'
import { useAppStore } from '@/stores/app'
import userAPI from '@/api/user'
import { activityAPI } from '@/api/activity'
import type { AffiliateInvitee, UserAffiliateDetail } from '@/types'
import type { ActivityCampaign, ActivityMetric, ActivityPrizeType, ActivityWinner, ActivityWinnerStatus } from '@/types/activity'
import { useClipboard } from '@/composables/useClipboard'
import { extractApiErrorMessage } from '@/utils/apiError'
import { formatCurrency, formatDateTime } from '@/utils/format'
import { buildAffiliateInviteLink } from '@/utils/oauthAffiliate'

type ActivityTab = 'open' | 'joined' | 'winners' | 'past' | 'affiliate'
type SortKey = 'bound_at' | 'period_consumption' | 'period_rebate' | 'history_consumption' | 'total_rebate'

const { t } = useI18n()
const appStore = useAppStore()
const { copyToClipboard } = useClipboard()

const loading = ref(true)
const hasLoaded = ref(false)
const affiliateLoading = ref(false)
const claimSubmitting = ref(false)
const claimDialogOpen = ref(false)
const claimDialogRef = ref<HTMLElement | null>(null)
const tablistRef = ref<HTMLElement | null>(null)
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
let dialogReturnFocus: HTMLElement | null = null

const openCampaigns = computed(() => campaigns.value.filter(campaign => !isCampaignEnded(campaign)))
const joinedCampaigns = computed(() => openCampaigns.value.filter(campaign => campaign.user_progress?.joined))
const pastCampaigns = computed(() => campaigns.value.filter(isCampaignEnded))
const pendingClaimWinners = computed(() => winners.value.filter(item => item.status === 'pending_claim'))
const availableTicketCount = computed(() => openCampaigns.value.reduce((sum, item) => sum + (item.user_progress?.ticket_count || 0), 0))
const joinedTicketCount = computed(() => campaigns.value.reduce((sum, item) => sum + (item.user_progress?.joined_tickets || 0), 0))
const tabs = computed(() => [
  {
    key: 'open' as const,
    label: t('activities.tabs.open'),
    note: t('activities.editorial.tabNotes.open'),
    count: openCampaigns.value.length,
  },
  {
    key: 'joined' as const,
    label: t('activities.tabs.joined'),
    note: t('activities.editorial.tabNotes.joined'),
    count: joinedCampaigns.value.length,
  },
  {
    key: 'winners' as const,
    label: t('activities.tabs.winners'),
    note: t('activities.editorial.tabNotes.winners'),
    count: winners.value.length,
  },
  {
    key: 'past' as const,
    label: t('activities.tabs.past'),
    note: t('activities.editorial.tabNotes.past'),
    count: pastCampaigns.value.length,
  },
  {
    key: 'affiliate' as const,
    label: t('activities.tabs.affiliate'),
    note: t('activities.editorial.tabNotes.affiliate'),
    count: affiliateDetail.value?.aff_count ?? 0,
  },
])
const activeSection = computed(() => ({
  eyebrow: t(`activities.editorial.sections.${activeTab.value}.eyebrow`),
  title: t(`activities.editorial.sections.${activeTab.value}.title`),
  description: t(`activities.editorial.sections.${activeTab.value}.description`),
}))
const formattedRebateRate = computed(() => {
  const v = affiliateDetail.value?.effective_rebate_rate_percent ?? 0
  const rounded = Math.round(v * 100) / 100
  return Number.isInteger(rounded) ? String(rounded) : rounded.toString()
})
const inviteLink = computed(() => {
  return buildAffiliateInviteLink(affiliateDetail.value?.aff_code)
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
    hasLoaded.value = true
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

function winnerStatusClass(status: ActivityWinnerStatus): string {
  if (status === 'delivered') return 'winner-state winner-state-success'
  if (status === 'pending_claim' || status === 'pending_delivery') return 'winner-state winner-state-pending'
  if (status === 'rejected' || status === 'expired') return 'winner-state winner-state-danger'
  return 'winner-state'
}

function openClaim(winner: ActivityWinner): void {
  dialogReturnFocus = document.activeElement instanceof HTMLElement ? document.activeElement : null
  selectedWinner.value = winner
  for (const key of Object.keys(claimForm)) delete claimForm[key]
  for (const field of winner.claim_fields || []) {
    claimForm[field.key] = String((winner.claim_info?.[field.key] as string | undefined) || '')
  }
  claimDialogOpen.value = true
  void nextTick(() => claimDialogRef.value?.focus())
}

function closeClaimDialog(): void {
  claimDialogOpen.value = false
  void nextTick(() => {
    dialogReturnFocus?.focus()
    dialogReturnFocus = null
  })
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
    closeClaimDialog()
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

function tabId(tab: ActivityTab): string {
  return `activity-tab-${tab}`
}

function panelId(tab: ActivityTab): string {
  return `activity-panel-${tab}`
}

function handleTabKeydown(event: KeyboardEvent, currentIndex: number): void {
  const lastIndex = tabs.value.length - 1
  let nextIndex = currentIndex

  if (event.key === 'ArrowRight' || event.key === 'ArrowDown') {
    nextIndex = currentIndex === lastIndex ? 0 : currentIndex + 1
  } else if (event.key === 'ArrowLeft' || event.key === 'ArrowUp') {
    nextIndex = currentIndex === 0 ? lastIndex : currentIndex - 1
  }
  else if (event.key === 'Home') nextIndex = 0
  else if (event.key === 'End') nextIndex = lastIndex
  else return

  event.preventDefault()
  activeTab.value = tabs.value[nextIndex].key
  void nextTick(() => {
    const tabButtons = tablistRef.value?.querySelectorAll<HTMLButtonElement>('[role="tab"]')
    tabButtons?.[nextIndex]?.focus()
  })
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

function formatSerial(value: number): string {
  return Math.max(0, Math.floor(value || 0)).toString().padStart(2, '0')
}

function campaignToneClass(index: number): string {
  return `campaign-tone-${index % 3}`
}

function campaignGuidance(campaign: ActivityCampaign): string {
  const progress = campaign.user_progress
  if (!campaign.draw_at) return t('activities.lottery.noDrawTime')
  if (isDrawClosed(campaign)) return t('activities.lottery.drawClosed')
  if (!progress || progress.ticket_count <= 0) {
    return t('activities.lottery.noTicketHint', {
      value: metricValueText(campaign.rule_config.metric, campaign.rule_config.threshold),
    })
  }
  if (progress.joined) {
    return t('activities.lottery.joinedHint', { count: progress.joined_tickets })
  }
  return t('activities.lottery.joinHint', { count: progress.ticket_count })
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

onMounted(() => {
  void loadAll()
})
</script>

<style scoped src="./ActivitiesView.css"></style>
