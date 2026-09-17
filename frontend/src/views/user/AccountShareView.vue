<template>
  <AppLayout content-layout="viewport">
    <div class="account-marketplace">
      <div class="marketplace-header">
        <section class="marketplace-hero" aria-labelledby="marketplace-title">
          <div class="marketplace-intro">
            <h1 id="marketplace-title">{{ t('accountShare.title') }}</h1>
            <p>{{ t('accountShare.page.subtitle') }}</p>
          </div>
          <label v-if="!isMembershipHistoryView" class="filter-search">
            <Icon name="search" size="md" />
            <input v-model.trim="searchQuery" class="filter-search-input" :aria-label="t('accountShare.page.searchPlaceholder')" :placeholder="t('accountShare.page.searchPlaceholder')" />
            <span class="marketplace-search-hint" aria-hidden="true">{{ t('common.search') }}</span>
          </label>
        </section>

        <nav class="marketplace-navigation" :aria-label="t('accountShare.page.categoriesAria')">
          <div class="marketplace-utility-bar">
            <div class="hero-actions">
              <button
                class="btn-primary min-h-11"
                type="button"
                :disabled="capabilitiesLoading || capabilities?.can_create_room === false"
                :title="createRoomCapabilityHint"
                @click="openCreateDialog"
              >
                <Icon name="plus" size="sm" class="mr-2" />
                {{ t('accountShare.createRoom.title') }}
              </button>
              <button class="btn-secondary min-h-11" type="button" @click="openRecommendationDialog">
                <Icon name="sparkles" size="sm" class="mr-2" />
                {{ t('accountShare.page.costEstimate') }}
              </button>
            </div>
            <div class="hero-utility-actions">
              <button
                v-if="authStore.isAdmin"
                class="account-share-admin-quota-button"
                type="button"
                data-testid="open-account-share-quotas"
                @click="openAdminQuotaDialog"
              >
                <Icon name="cog" size="sm" class="mr-2" />
                {{ t('accountShare.page.roomQuota') }}
              </button>
              <button class="account-share-guide-button" type="button" @click="openUsageGuideDialog">
                <Icon name="book" size="sm" class="mr-2" />
                {{ t('affiliate.tips.title') }}
              </button>
              <button class="account-share-spend-button" type="button" @click="openMySpendDialog()">
                <Icon name="dollar" size="sm" class="mr-2" />
                {{ t('accountShare.page.mySpend') }}
              </button>
            </div>
          </div>
          <div class="filter-actions">
            <button
              v-for="filter in filters"
              :key="filter.key"
              type="button"
              class="filter-chip"
              :class="mainViewTab === filter.tab ? 'filter-chip-active' : 'filter-chip-idle'"
              :aria-pressed="mainViewTab === filter.tab"
              @click="setFilter(filter)"
            >
              <Icon :name="filter.tab === 'using' ? 'key' : filter.tab === 'mine' ? 'home' : 'grid'" size="sm" />
              {{ filter.label }}
            </button>
          </div>
        </nav>

        <div
          v-if="(mainViewTab === 'mine' || capabilities?.can_create_room === false || capabilitiesError) && (capabilities || capabilitiesError)"
          class="account-share-capability-strip"
          :class="{ 'account-share-capability-strip-blocked': capabilities?.can_create_room === false }"
          aria-live="polite"
        >
          <template v-if="capabilities">
            <span>
              <strong>{{ capabilities.live_rooms.used }}/{{ capabilities.live_rooms.limit }}</strong>
              {{ t('accountShare.quotaAdmin.liveRooms') }}
            </span>
            <span>
              <strong>{{ capabilities.room_creates_24_hours.used }}/{{ capabilities.room_creates_24_hours.limit }}</strong>
              {{ t('accountShare.quotaAdmin.creates24h') }}
            </span>
            <span>
              <strong>{{ capabilities.owner_room_accounts.used }}/{{ capabilities.owner_room_accounts.limit }}</strong>
              {{ t('accountShare.lifecycle.roomAccounts') }}
            </span>
            <small v-if="capabilities.capability_blockers.length > 0">
              {{ capabilityBlockerMessage(capabilities.capability_blockers[0]) }}
            </small>
            <small v-else>
              {{ t('accountShare.page.quotaHint', { maxAccountsPerRoom: capabilities.max_accounts_per_room, seatLimitMinimum: capabilities.seat_limit_minimum, seatLimitMaximum: capabilities.seat_limit_maximum }) }}
            </small>
          </template>
          <small v-else>{{ capabilitiesError }}</small>
        </div>

        <section
          v-if="isKeyResolutionMode"
          class="key-resolution-panel"
          :class="keyResolutionPanelToneClass"
          role="region"
          :aria-label="t('accountShare.keyResolution.panelLabel')"
          :aria-busy="keyResolutionLoading"
        >
          <div class="key-resolution-main">
            <span class="key-resolution-icon" aria-hidden="true">
              <Icon :name="keyResolutionAllClear ? 'checkCircle' : (keyResolutionError ? 'exclamationCircle' : 'key')" size="md" />
            </span>
            <div class="key-resolution-copy" aria-live="polite">
              <span class="key-resolution-eyebrow">{{ t('accountShare.keyResolution.panelLabel') }}</span>
              <h2>{{ keyResolutionAllClear ? '关联已全部解除' : `正在处理 ${keyResolutionKeyLabel}` }}</h2>
              <p>{{ keyResolutionStatusMessage }}</p>
            </div>
          </div>

          <div class="key-resolution-counts grid grid-cols-1 gap-2 sm:grid-cols-2" :aria-label="t('accountShare.keyResolution.pendingCount')">
            <div>
              <span>{{ t('keys.accountShareConflict.activeLabel') }}</span>
              <strong>{{ (keyResolutionLoading && !keyResolutionLoaded) || keyResolutionError ? '—' : keyResolutionActiveCount }}</strong>
            </div>
            <div>
              <span>{{ t('keys.accountShareConflict.endingLabel') }}</span>
              <strong>{{ (keyResolutionLoading && !keyResolutionLoaded) || keyResolutionError ? '—' : keyResolutionEndingCount }}</strong>
            </div>
          </div>

          <div class="key-resolution-actions">
            <button
              type="button"
              class="key-resolution-refresh-button"
              :disabled="keyResolutionLoading"
              @click="refreshKeyResolutionContext"
            >
              <Icon name="refresh" size="sm" :class="{ 'animate-spin': keyResolutionLoading }" />
              {{ keyResolutionLoading ? t('accountShare.keyResolution.checkingShort') : t('accountShare.lifecycle.refreshStatus') }}
            </button>
            <button type="button" class="key-resolution-return-button" @click="returnToApiKeyManagement">
              <Icon name="arrowLeft" size="sm" />
              {{ t('accountShare.keyResolution.backToKeys') }}
            </button>
          </div>
        </section>

      </div>

      <BaseDialog :show="showUsageGuideDialog" :title="t('accountShare.guide.title')" width="wide" :z-index="55" @close="closeUsageGuideDialog">
        <div class="space-y-5 text-sm leading-7 text-gray-600 dark:text-dark-200">
          <p>{{ t('accountShare.guide.ruleJoin') }}</p>
          <p>{{ t('accountShare.guide.ruleBilling') }}</p>
          <p>{{ t('accountShare.guide.ruleIdle') }}</p>
          <p>{{ t('accountShare.guide.ruleOwner') }}</p>
          <p>{{ t('accountShare.guide.ruleSelfUse') }}</p>
        </div>
        <template #footer>
          <button type="button" class="btn-secondary min-h-11" @click="closeUsageGuideDialog">{{ t('accountShare.guide.gotIt') }}</button>
          <button type="button" class="btn-primary min-h-11" @click="openRecommendationFromUsageGuide">{{ t('accountShare.page.costEstimate') }}</button>
        </template>
      </BaseDialog>

      <BaseDialog
        :show="showRecommendationDialog"
        :title="t('accountShare.estimate.title')"
        width="full"
        :z-index="55"
        @close="closeRecommendationDialog"
      >
        <section class="recommendation-panel recommendation-dialog-panel">
          <div class="recommendation-head">
            <div class="recommendation-heading">
              <span class="recommendation-heading-icon">
                <Icon name="sparkles" size="sm" />
              </span>
              <div class="min-w-0">
                <h2>{{ t('accountShare.estimate.comparison') }}</h2>
                <p>{{ t('accountShare.estimate.sortedBy', { platform: platformLabel(activeListingPlatform), group: accountModeGroupName(activeListingPlatform) }) }}</p>
              </div>
            </div>
            <div class="recommendation-preset-row" :aria-label="t('accountShare.estimate.presets')">
              <button
                v-for="preset in recommendationPresets"
                :key="preset.key"
                type="button"
                class="recommendation-preset"
                :class="{ 'recommendation-preset-active': selectedRecommendationPreset === preset.key }"
                @click="applyRecommendationPreset(preset.key)"
              >
                {{ preset.label }}
              </button>
              <button
                type="button"
                class="recommendation-profile-button"
                :disabled="recommendationUsageProfileLoading || recommendationLoading"
                @click="applyRecentUsageProfile"
              >
                <Icon name="clock" size="sm" class="mr-1.5" :class="{ 'animate-spin': recommendationUsageProfileLoading }" />
                {{ recommendationUsageProfileLoading ? t('accountShare.estimate.loadingAvg') : t('accountShare.estimate.avg3d') }}
              </button>
            </div>
            <p class="recommendation-profile-help">
              {{ t('accountShare.estimate.assumption') }}
            </p>
          </div>

        <div class="recommendation-layout">
          <div class="recommendation-form-grid">
            <label class="field">
              <span>{{ t('accountShare.estimate.modeKey') }}</span>
              <select v-model.number="recommendationForm.api_key_id" class="input h-10" :disabled="modeKeysLoading">
                <option :value="0">{{ modeKeysLoading ? '加载中' : `选择${accountModeGroupName(activeListingPlatform)} Key` }}</option>
                <option v-for="key in recommendationKeyOptions" :key="key.id" :value="key.id">
                  {{ modeKeyLabel(key) }}
                </option>
              </select>
            </label>
            <label class="field">
              <span>{{ t('keyUsage.model') }}</span>
              <select v-model="recommendationForm.model" class="input h-10">
                <option v-for="model in recommendationModelOptions" :key="model" :value="model">
                  {{ model }}
                </option>
              </select>
            </label>
            <label class="field">
              <span>{{ t('accountShare.estimate.requestCount') }}</span>
              <input v-model.number="recommendationForm.request_count" class="input h-10" type="number" min="1" step="1" />
            </label>
            <label class="field">
              <span>{{ t('accountShare.estimate.hours') }}</span>
              <input v-model.number="recommendationForm.active_hours" class="input h-10" type="number" min="0.1" step="0.1" />
            </label>
            <label class="field">
              <span>{{ t('accountShare.estimate.inputTokens') }}</span>
              <input v-model.number="recommendationForm.input_tokens_per_request" class="input h-10" type="number" min="0" step="1" />
            </label>
            <label class="field">
              <span>{{ t('accountShare.estimate.outputTokens') }}</span>
              <input v-model.number="recommendationForm.output_tokens_per_request" class="input h-10" type="number" min="0" step="1" />
            </label>
            <label class="field">
              <span>{{ t('accountShare.estimate.cacheWrite') }}</span>
              <input v-model.number="recommendationForm.cache_creation_tokens_per_request" class="input h-10" type="number" min="0" step="1" />
            </label>
            <label class="field">
              <span>{{ t('accountShare.estimate.cacheRead') }}</span>
              <input v-model.number="recommendationForm.cache_read_tokens_per_request" class="input h-10" type="number" min="0" step="1" />
            </label>
            <label class="field">
              <span>{{ t('accountShare.estimate.imageInput') }}</span>
              <input v-model.number="recommendationForm.image_input_tokens_per_request" class="input h-10" type="number" min="0" step="1" />
            </label>
            <label class="field">
              <span>{{ t('accountShare.estimate.imageOutput') }}</span>
              <input v-model.number="recommendationForm.image_output_tokens_per_request" class="input h-10" type="number" min="0" step="1" />
            </label>
            <label class="field">
              <span>{{ t('accountShare.estimate.imageCacheRead') }}</span>
              <input v-model.number="recommendationForm.image_cache_read_tokens_per_request" class="input h-10" type="number" min="0" step="1" />
            </label>
          </div>

          <div class="recommendation-action-box">
            <button class="btn-primary h-11 w-full" type="button" :disabled="recommendationLoading" @click="runRecommendation">
              <Icon name="sparkles" size="sm" class="mr-2" :class="{ 'animate-spin': recommendationLoading }" />
              {{ recommendationLoading ? t('accountShare.estimate.running') : t('accountShare.estimate.run') }}
            </button>
            <p v-if="recommendationUsageProfileMessage" class="recommendation-profile-message">{{ recommendationUsageProfileMessage }}</p>
            <p v-if="recommendationError" class="recommendation-error">{{ recommendationError }}</p>
            <div v-if="recommendationResult" class="recommendation-summary">
              <small>{{ t('accountShare.estimate.minHourly') }}</small>
              <span>{{ recommendationInputSummary }}</span>
              <strong>{{ recommendationBest ? formatRecommendationCost(recommendationEstimatedHourlyCost(recommendationBest)) : t('accountShare.estimate.noResults') }}</strong>
              <small>{{ t('accountShare.estimate.comparable', { length: recommendationCandidates.length, candidateCount: recommendationResult.candidate_count }) }}</small>
            </div>
          </div>
        </div>

        <div v-if="recommendationResult" class="recommendation-results">
          <div v-if="recommendationCandidates.length === 0" class="recommendation-empty">
            {{ t('accountShare.estimate.noMatch') }}
          </div>
          <template v-else>
            <div class="recommendation-results-head">
              <div>
                <strong>{{ t('accountShare.estimate.comparison') }}</strong>
                <span>{{ t('accountShare.estimate.rangeSort', { range: recommendationPageRangeText }) }}</span>
              </div>
              <div class="recommendation-page-controls">
                <button
                  type="button"
                  class="recommendation-page-button"
                  :disabled="recommendationPage <= 1"
                  :aria-label="t('accountShare.quotaAdmin.prevPage')"
                  @click="setRecommendationPage(recommendationPage - 1)"
                >
                  <Icon name="chevronLeft" size="sm" />
                </button>
                <span>{{ recommendationPage }} / {{ recommendationPageCount }}</span>
                <button
                  type="button"
                  class="recommendation-page-button"
                  :disabled="recommendationPage >= recommendationPageCount"
                  :aria-label="t('accountShare.quotaAdmin.nextPage')"
                  @click="setRecommendationPage(recommendationPage + 1)"
                >
                  <Icon name="chevronRight" size="sm" />
                </button>
              </div>
            </div>
            <article
              v-for="candidate in recommendationPagedCandidates"
              :key="candidate.listing.id"
              class="recommendation-card"
            >
              <div class="recommendation-card-head">
                <div class="recommendation-title">
                  <span class="recommendation-rank">#{{ candidate.rank }}</span>
                  <div class="min-w-0">
                    <strong>{{ listingDisplayName(candidate.listing) }}</strong>
                    <small>{{ ownerDisplayName(candidate.listing) }} · {{ accountLevelBadgeLabel(candidate.listing) }} · {{ listingRatingLabel(candidate.listing) }}</small>
                  </div>
                </div>
                <div class="recommendation-total">
                  <span>{{ t('accountShare.estimate.hourlyQuota') }}</span>
                  <strong>{{ formatRecommendationCost(recommendationEstimatedHourlyCost(candidate)) }}</strong>
                </div>
              </div>

              <div class="recommendation-metrics">
                <div>
                  <span>{{ recommendationRequestCostLabel(candidate) }}</span>
                  <strong>{{ formatRecommendationCost(candidate.estimate.request_cost) }}</strong>
                </div>
                <div>
                  <span>{{ candidate.estimate.owner_self_use ? t('accountShare.estimate.selfUsePerRequest') : t('accountShare.estimate.perRequest') }}</span>
                  <strong>{{ formatRecommendationCost(candidate.estimate.per_request_cost) }}</strong>
                </div>
                <div>
                  <span>{{ candidate.estimate.owner_self_use ? t('accountShare.estimate.selfUseHourly') : t('accountShare.estimate.hourlyTotal') }}</span>
                  <strong>{{ recommendationHourlyCostText(candidate) }}</strong>
                </div>
                <div>
                  <span>{{ candidate.estimate.owner_self_use ? t('accountShare.estimate.selfUseAdmission') : t('accountShare.estimate.admission') }}</span>
                  <strong>{{ recommendationUpfrontCostText(candidate) }}</strong>
                </div>
                <div>
                  <span>{{ candidate.estimate.owner_self_use ? t('accountShare.estimate.selfUseRate') : t('groups.rateLabel') }}</span>
                  <strong>{{ formatNumber(candidate.estimate.effective_rate_multiplier) }}x</strong>
                </div>
              </div>

              <p class="text-xs leading-5 text-gray-500 dark:text-dark-300">{{ candidate.estimate.assumption }}</p>
              <div v-if="candidate.estimate.owner_self_use" class="recommendation-self-use-note">
                <Icon name="infoCircle" size="sm" />
                <span>{{ recommendationOwnerSelfUseSummary(candidate) }}</span>
              </div>

              <div v-if="candidate.warnings?.length" class="recommendation-warnings">
                <span v-for="warning in candidate.warnings" :key="warning">{{ warning }}</span>
              </div>
              <div v-if="candidate.estimate.owner_self_use && selfUseSettingsError" class="recommendation-warnings">
                <span>{{ selfUseSettingsError }}</span>
              </div>

              <div class="recommendation-card-actions">
                <span>{{ t('accountShare.card.seatSummary', { activeSeats: candidate.listing.active_seats, seatLimit: candidate.listing.seat_limit, accountConcurrency: candidate.listing.account_concurrency }) }}</span>
                <button
                  class="btn-primary h-10"
                  type="button"
                  :disabled="preparingJoinId !== null || joiningId !== null || selfUseJoinUnavailable(candidate.listing)"
                  :title="selfUseJoinUnavailable(candidate.listing) ? selfUseSettingsError : undefined"
                  @click="useRecommendedListing(candidate)"
                >
                  <Icon name="login" size="sm" class="mr-2" />
                  {{ preparingJoinId === candidate.listing.id ? t('accountShare.join.preparing') : t('accountShare.join.joinUse') }}
                </button>
              </div>
            </article>
          </template>
        </div>
        </section>
      </BaseDialog>

      <CreateRoomDialog
        :show="showCreate"
        :busy="creating"
        :close-disabled="pendingDraftDiscardTarget === 'create'"
        @close="closeCreateDialog"
        @reset="resetCreateForm"
      >
        <div
          v-if="capabilities"
          class="create-capability-summary"
          :class="{ 'create-capability-summary-blocked': !capabilities.can_create_room }"
        >
          <span>
            {{ t('accountShare.quota.rooms', { used: capabilities.live_rooms.used, limit: capabilities.live_rooms.limit }) }}
          </span>
          <span>
            {{ t('accountShare.quota.created24h', { used: capabilities.room_creates_24_hours.used, limit: capabilities.room_creates_24_hours.limit }) }}
          </span>
          <span>
            {{ t('accountShare.quota.accounts', { used: capabilities.owner_room_accounts.used, limit: capabilities.owner_room_accounts.limit }) }}
          </span>
          <strong v-if="!capabilities.can_create_room">
            {{ capabilities.capability_blockers[0]?.message || t('accountShare.quota.cannotCreate') }}
          </strong>
        </div>

        <div class="create-room-source-stage">
          <div class="create-room-stage-heading">
            <span class="create-room-stage-index">1</span>
            <div>
              <strong>{{ t('accountShare.create.stepSource') }}</strong>
              <small>{{ t('accountShare.create.sourceDesc') }}</small>
            </div>
          </div>
          <button type="button" class="btn-secondary min-h-11" :disabled="creating" data-testid="create-room-new-account" @click="openStandaloneAccountCreator">
            <Icon name="plus" size="sm" class="mr-2" />{{ t('accountShare.create.addAccount') }}
          </button>
          <div class="create-room-account-picker">
            <div class="flex flex-col gap-3 md:flex-row md:items-end">
              <label class="field min-w-0 flex-1">
                <span>{{ t('accountShare.create.existingAccount') }}</span>
                <select
                  v-model.number="selectedOwnedAccountID"
                  class="input"
                  :disabled="ownedAccountsLoading || creating"
                >
                  <option :value="0">
                    {{ ownedAccountsLoading ? t('accountShare.ownerDialog.loadingAccounts') : t('accountShare.create.selectHealthy') }}
                  </option>
                  <option
                    v-for="account in eligibleOwnedAccounts"
                    :key="account.id"
                    :value="account.id"
                  >
                    {{ account.name }} · {{ account.account_level }} · #{{ account.id }}
                  </option>
                </select>
                <small>{{ ownedAccountSelectionHint }}</small>
              </label>
              <button
                type="button"
                class="btn-secondary h-10 shrink-0"
                :disabled="ownedAccountsLoading || creating"
                @click="loadOwnedAccounts(true)"
              >
                <Icon name="refresh" size="sm" class="mr-2" :class="{ 'animate-spin': ownedAccountsLoading }" />
                {{ t('accountShare.create.refreshAccounts') }}
              </button>
            </div>
          </div>
        </div>

        <div class="create-room-workspace">
          <div class="create-room-form-flow">
            <div class="form-section create-room-stage-card">
              <div class="section-heading create-room-stage-heading">
                <span class="create-room-stage-index">2</span>
                <div>
                  <span>{{ t('accountShare.create.stepRules') }}</span>
                <small>{{ t('accountShare.create.rulesDesc') }}</small>
                </div>
              </div>
              <div class="create-room-field-grid">
                <div class="field">
                  <span>{{ t('admin.accounts.bulkTest.platformLabel') }}</span>
                  <div class="grid grid-cols-2 gap-2">
                    <button
                      v-for="option in ACCOUNT_SHARE_PLATFORM_OPTIONS"
                      :key="option.value"
                      type="button"
                      :class="[
                        'h-10 rounded-md border px-3 text-sm font-semibold transition',
                        createPlatform === option.value
                          ? 'border-primary-500 bg-primary-50 text-primary-700 dark:border-primary-400 dark:bg-primary-500/10 dark:text-primary-200'
                          : 'border-gray-200 bg-white text-gray-600 hover:border-gray-300 dark:border-dark-700 dark:bg-dark-900 dark:text-dark-200 dark:hover:border-dark-600'
                      ]"
                      :disabled="creating"
                      @click="selectCreatePlatform(option.value)"
                    >
                      {{ option.label }}
                    </button>
                  </div>
                  <small>{{ t('accountShare.create.platformDesc') }}</small>
                </div>

                <label class="field">
                  <span>{{ t('accountShare.create.roomName') }}</span>
                  <input v-model="createForm.name" class="input" :placeholder="ACCOUNT_NAME_BASE_BY_PLATFORM[createPlatform]" />
                  <small :class="accountNameValidationMessage ? 'text-red-600 dark:text-red-300' : ''">
                    {{ accountNameValidationMessage || t('accountShare.create.roomNameHint') }}
                  </small>
                </label>

                <label class="field">
                  <span>{{ t('accountShare.create.seatLimit') }}</span>
                  <input
                    v-model.number="createForm.seat_limit"
                    class="input"
                    type="number"
                    :min="ACCOUNT_SHARE_MIN_SEATS"
                    :max="ACCOUNT_SHARE_MAX_SEATS"
                    step="1"
                    inputmode="numeric"
                    data-testid="create-room-seat-limit"
                  />
                  <small>{{ t('accountShare.memberLimitHelp') }}</small>
                </label>

                <div class="field">
                  <span>{{ t('accountShare.roomAccounts.concurrency') }}</span>
                  <div class="input flex items-center bg-gray-50 text-gray-700 dark:bg-dark-800 dark:text-dark-200">
                    {{ selectedOwnedAccount?.concurrency ?? '—' }}
                  </div>
                  <small>{{ t('accountShare.create.concurrencyHint') }}</small>
                </div>

                <label class="field">
                  <span>{{ t('accountShare.create.perUserConcurrency') }}</span>
                  <input v-model.number="createForm.per_user_concurrency" class="input" type="number" min="1" :max="maxPerUserConcurrency" step="1" />
                  <small :class="perUserConcurrencyValidationMessage ? 'text-red-600 dark:text-red-300' : ''">
                    {{ perUserConcurrencyValidationMessage || perUserConcurrencyLimitTip }}
                  </small>
                </label>

                <label class="field">
                  <span>{{ t('usage.accountMultiplier') }}</span>
                  <input v-model.number="createForm.rate_multiplier" class="input" type="number" min="0" step="0.01" />
                </label>

                <label class="field">
                  <span>{{ t('accountShare.create.hourlyRate') }}</span>
                  <input v-model.number="createForm.hourly_rate" class="input" type="number" min="0" step="0.0001" />
                  <small>{{ t('accountShare.create.hourlyRateHint') }}</small>
                </label>

                <label class="field">
                  <span>{{ t('accountShare.featureTags.hourlyFeeWaiver') }}</span>
                  <input v-model.number="createForm.hourly_fee_waiver_minimum" class="input" type="number" min="0" step="0.0001" />
                  <small>{{ t('accountShare.create.waiverHint') }}</small>
                </label>

                <label class="field">
                  <span>{{ t('accountShare.create.minBalance') }}</span>
                  <input v-model.number="createForm.min_balance_required" class="input" type="number" min="0" step="0.01" />
                </label>

                <label class="field">
                  <span>{{ t('accountShare.create.password') }}</span>
                  <input
                    v-model="createForm.join_password"
                    class="input"
                    type="password"
                    maxlength="64"
                    autocomplete="new-password"
                    :placeholder="t('accountShare.create.passwordPlaceholder')"
                    data-testid="create-room-join-password"
                  />
                  <small>{{ t('accountShare.create.passwordHint') }}</small>
                </label>
              </div>
            </div>

            <div class="form-section create-room-stage-card">
              <div class="section-heading create-room-stage-heading">
                <span class="create-room-stage-index">3</span>
                <div>
                  <span>{{ t('accountShare.create.stepModels') }}</span>
                <small>{{ t('accountShare.create.modelsDesc') }}</small>
                </div>
              </div>
              <div class="grid gap-4 xl:grid-cols-[minmax(0,1fr)_320px]">
                <div class="field">
                  <span>{{ t('admin.accounts.modelWhitelist') }}</span>
                  <div class="model-selector-shell">
                    <ModelWhitelistSelector
                      v-model="allowedModels"
                      :platform="createPlatform"
                      :allowed-options="roomCatalogModels ?? []"
                    />
                  </div>
                  <small v-if="roomCatalogLoading">{{ t('accountShare.create.loadingModels') }}</small>
                  <small v-else-if="roomCatalogError" class="text-red-600 dark:text-red-300">{{ roomCatalogError }}</small>
                  <small v-else-if="roomCatalogModels !== null && roomCatalogModels.length === 0" class="text-red-600 dark:text-red-300">{{ t('accountShare.create.noPricedModels') }}</small>
                  <small v-else>{{ t('accountShare.create.modelPickerHint') }}</small>
                </div>

                <div v-if="createPlatform === 'openai'" class="grid gap-3 sm:grid-cols-2 lg:grid-cols-1">
                  <label class="field">
                    <span>{{ t('accountShare.create.codex5h') }}</span>
                    <input v-model.number="createForm.codex_5h_limit_percent" class="input" type="number" min="1" max="100" step="1" />
                  </label>
                  <label class="field">
                    <span>{{ t('accountShare.create.codex7d') }}</span>
                    <input v-model.number="createForm.codex_7d_limit_percent" class="input" type="number" min="1" max="100" step="1" />
                  </label>
                </div>
                <div v-else-if="createPlatform === 'anthropic'" class="grid gap-3 sm:grid-cols-2 lg:grid-cols-1">
                  <label class="field">
                    <span>{{ t('accountShare.create.claude5h') }}</span>
                    <input v-model.number="createForm.anthropic_5h_limit_percent" class="input" type="number" min="1" max="100" step="1" />
                  </label>
                  <label class="field">
                    <span>{{ t('accountShare.create.claude7d') }}</span>
                    <input v-model.number="createForm.anthropic_7d_limit_percent" class="input" type="number" min="1" max="100" step="1" />
                  </label>
                </div>
              </div>

              <div v-if="concurrencyNotice" class="notice-row mt-3">
                <Icon name="infoCircle" size="sm" class="mt-0.5 flex-shrink-0" />
                <span>{{ concurrencyNotice }}</span>
              </div>

              <label v-if="createPlatform === 'openai'" class="toggle-row mt-3">
                <input v-model="createForm.codex_cli_only" type="checkbox" />
                <span>
                  <strong>{{ t('admin.accounts.openai.codexCLIOnly') }}</strong>
                  <small>{{ t('accountShare.create.cliOnlyHint') }}</small>
                </span>
              </label>
            </div>
          </div>

          <div class="create-room-submit-stage">
            <div class="create-room-submit-content">
              <div class="create-room-stage-heading">
                <span class="create-room-stage-index">4</span>
                <div>
                  <small class="create-room-summary-eyebrow">{{ t('accountShare.create.stepConfirm') }}</small>
                  <strong>{{ t('accountShare.createRoom.step4Title') }}</strong>
                  <small>{{ t('accountShare.create.confirmDesc') }}</small>
                </div>
              </div>
              <div class="create-room-summary-list" :aria-label="t('accountShare.create.summary')">
                <div>
                  <span>{{ t('accountShare.createRoom.step1Desc') }}</span>
                  <strong>{{ selectedOwnedAccount?.name || t('accountShare.create.notSelected') }}</strong>
                </div>
                <div>
                  <span>{{ t('accountShare.roomAccounts.platform') }}</span>
                  <strong>{{ ACCOUNT_SHARE_PLATFORM_OPTIONS.find((option) => option.value === createPlatform)?.label || createPlatform }}</strong>
                </div>
                <div>
                  <span>{{ t('accountShare.create.roomName') }}</span>
                  <strong>{{ createForm.name || t('accountShare.create.notFilled') }}</strong>
                </div>
                <div>
                  <span>{{ t('admin.accounts.modelWhitelist') }}</span>
                  <strong>{{ allowedModels.length ? `${allowedModels.length} 个模型` : '尚未选择' }}</strong>
                </div>
              </div>
              <p class="create-room-summary-note">
                {{ t('accountShare.create.credentialNote') }}
              </p>
              <p
                v-if="createErrorMessage"
                class="create-room-error-message"
                role="alert"
              >
                {{ createErrorMessage }}
              </p>
              <button
                class="btn-primary create-room-submit-button"
                type="button"
                :disabled="creating || !canCreateRoomFromOwnedAccount"
                @click="createRoomFromOwnedAccount"
              >
                <Icon name="plus" size="sm" class="mr-2" :class="{ 'animate-pulse': creating }" />
                {{ creating ? t('accountShare.create.creating') : t('accountShare.create.submit') }}
              </button>
              <small v-if="!canCreateRoomFromOwnedAccount && !creating" class="create-room-submit-hint">
                {{ t('accountShare.create.submitDisabled') }}
              </small>
            </div>
          </div>
        </div>
      </CreateRoomDialog>
      <CreateAccountModal
        :show="showCreateAccount"
        :proxies="proxies"
        :groups="[]"
        account-scope="user"
        :allow-billing-rate="false"
        :allow-multiple-o-auth="false"
        :initial-platform="createPlatform"
        lock-platform
        @proxy-scope-change="loadCreateAccountProxies"
        @close="showCreateAccount = false"
        @created="handleStandaloneAccountCreated"
      />

      <div class="marketplace-workspace" :class="{ 'marketplace-workspace-history': isMembershipHistoryView }">
      <section ref="filterPanelRef" class="filter-panel" :aria-label="t('accountShare.filters.title')" @keydown.esc="handleFilterPopoverEscape">
        <div class="marketplace-sidebar-title">
          <div><Icon name="filter" size="sm" /><h2>{{ t('accountShare.filters.filterRooms') }}</h2></div>
          <span>{{ t('accountShare.filters.subtitle') }}</span>
        </div>
        <div v-if="!isMembershipHistoryView" class="marketplace-platform-section">
          <span class="filter-section-label">{{ t('admin.accounts.bulkTest.platformLabel') }}</span>
          <div class="account-share-platform-tabs" :aria-label="t('accountShare.filters.platform')">
            <button
              v-for="option in ACCOUNT_SHARE_PLATFORM_OPTIONS"
              :key="option.value"
              type="button"
              class="account-share-platform-tab"
              :class="activeListingPlatform === option.value ? 'account-share-platform-tab-active' : 'account-share-platform-tab-idle'"
              :aria-pressed="activeListingPlatform === option.value"
              @click="setListingPlatform(option.value)"
            >
              <PlatformIcon :platform="option.value" size="sm" />
              <span>{{ option.label }}<small>{{ accountModeGroupName(option.value) }}</small></span>
              <Icon v-if="activeListingPlatform === option.value" name="check" size="sm" />
            </button>
          </div>
        </div>
        <div class="filter-toolbar">
          <div v-if="mainViewTab === 'using'" class="marketplace-subviews" :aria-label="t('accountShare.filters.myUsageView')">
            <button type="button" class="btn-secondary min-h-11" :aria-pressed="!isMembershipHistoryView" @click="setFilter(usingFilter)">{{ t('accountShare.mySpend.tabUsing') }}</button>
            <button type="button" class="btn-secondary min-h-11" :aria-pressed="isMembershipHistoryView" @click="setFilter(historyFilter)">{{ t('accountShare.membership.statusHistory') }}</button>
          </div>
          <div v-if="mainViewTab === 'mine'" class="marketplace-owner-filter">
            <label for="owner-room-state" class="text-sm">{{ t('accountShare.filters.roomStatus') }}</label>
            <select id="owner-room-state" class="input min-h-11 w-auto" :value="isArchiveView ? 'archive' : listingFilters.status" @change="setOwnerRoomState(($event.target as HTMLSelectElement).value)">
              <option value="">{{ t('accountShare.filters.allActiveRooms') }}</option>
              <option value="active">{{ t('accountShare.statusFilter.active') }}</option>
              <option value="paused">{{ t('accountShare.lifecycle.statusDelisted') }}</option>
              <option value="archive">{{ t('ideas.status.deleted') }}</option>
            </select>
          </div>
          <div
            v-if="isArchiveView"
            class="flex min-h-11 items-center gap-2 border-t border-slate-200 px-4 py-3 text-sm leading-6 text-slate-600 dark:border-dark-700 dark:text-dark-300"
            data-testid="archive-readonly-notice"
          >
            <Icon name="document" size="sm" class="flex-none" />
            <span>{{ t('accountShare.filters.archiveHint') }}</span>
          </div>
          <details v-else-if="!isMembershipHistoryView" class="filter-body" :open="isWideMarketplace" data-testid="advanced-listing-filters">
            <summary>{{ t('accountShare.filters.filterSort') }} <span v-if="activeAdvancedFilterCount">{{ t('accountShare.filters.selectedCount', { count: activeAdvancedFilterCount }) }}</span><Icon name="chevronDown" size="sm" /></summary>
            <div class="filter-body-head">
              <div class="filter-body-title">
                <span class="filter-body-icon"><Icon name="filter" size="sm" /></span>
                <div>
                  <small>{{ activeResultFilterCount > 0 ? t('accountShare.filters.enabledCount', { count: activeResultFilterCount }) : t('accountShare.filters.narrowHint') }}</small>
                </div>
              </div>
              <div class="filter-button-row">
                <button class="filter-reset-button" type="button" :disabled="loading || !hasResultFilters" @click="resetListingFilters">
                  <Icon name="x" size="sm" />
                  <span>{{ t('accountShare.createRoom.reset') }}</span>
                </button>
                <button class="filter-apply-button" type="button" :disabled="loading" @click="applyListingFilters">
                  <Icon name="filter" size="sm" />
                  <span>{{ t('keyUsage.apply') }}</span>
                </button>
              </div>
            </div>

            <div class="advanced-filter-grid" :aria-label="t('accountShare.filters.advanced')">
              <div class="filter-popover-wrap">
                <span class="filter-section-label">{{ t('common.status') }}</span>
                <button
                  ref="statusFilterTriggerRef"
                  type="button"
                  class="filter-trigger-button"
                  :class="[listingFilters.status !== '' && 'filter-trigger-selected', openFilterPopover === 'status' && 'filter-trigger-active']"
                  :aria-expanded="openFilterPopover === 'status'"
                  aria-controls="account-share-status-filter"
                  @click="toggleFilterPopover('status')"
                >
                  <Icon name="filter" size="sm" />
                  <span>{{ statusFilterSummary }}</span>
                  <Icon name="chevronDown" size="xs" class="filter-trigger-chevron" />
                </button>
                <div
                  v-if="openFilterPopover === 'status'"
                  id="account-share-status-filter"
                  class="filter-popover status-popover"
                  role="group"
                  :aria-label="t('accountShare.filters.statusOptions')"
                  @keydown.escape.stop="handleFilterPopoverEscape"
                >
                  <button
                    v-for="option in listingStatusFilterOptions"
                    :key="option.value"
                    type="button"
                    class="filter-menu-option"
                    :class="listingFilters.status === option.value && 'filter-menu-option-active'"
                    :aria-pressed="listingFilters.status === option.value"
                    @click="setListingStatusFilter(option.value)"
                  >
                    <span>{{ option.label }}</span>
                    <Icon v-if="listingFilters.status === option.value" name="check" size="sm" />
                  </button>
                </div>
              </div>

              <div v-if="isOpenAIListingPlatform" class="filter-popover-wrap">
                <span class="filter-section-label">{{ t('accountShare.roomAccounts.level') }}</span>
                <button
                  ref="levelFilterTriggerRef"
                  type="button"
                  class="filter-trigger-button"
                  :class="[listingFilters.accountLevel !== 'all' && 'filter-trigger-selected', openFilterPopover === 'level' && 'filter-trigger-active']"
                  :aria-expanded="openFilterPopover === 'level'"
                  aria-controls="account-share-level-filter"
                  @click="toggleFilterPopover('level')"
                >
                  <Icon name="badge" size="sm" />
                  <span>{{ accountLevelFilterSummary }}</span>
                  <Icon name="chevronDown" size="xs" class="filter-trigger-chevron" />
                </button>
                <div
                  v-if="openFilterPopover === 'level'"
                  id="account-share-level-filter"
                  class="filter-popover level-popover"
                  role="group"
                  :aria-label="t('accountShare.filters.levelOptions')"
                  @keydown.escape.stop="handleFilterPopoverEscape"
                >
                  <button
                    v-for="option in accountLevelFilterOptions"
                    :key="option.value"
                    type="button"
                    class="filter-menu-option"
                    :class="listingFilters.accountLevel === option.value && 'filter-menu-option-active'"
                    :aria-pressed="listingFilters.accountLevel === option.value"
                    @click="setAccountLevelFilter(option.value)"
                  >
                    <span>{{ option.label }}</span>
                    <Icon v-if="listingFilters.accountLevel === option.value" name="check" size="sm" />
                  </button>
                </div>
              </div>

              <div class="filter-popover-wrap">
                <span class="filter-section-label">{{ t('accountShare.filters.seats') }}</span>
                <button
                  ref="seatFilterTriggerRef"
                  type="button"
                  class="filter-trigger-button"
                  :class="listingFilters.seatLimits.length > 0 && 'filter-trigger-selected'"
                  :aria-expanded="openFilterPopover === 'seat'"
                  aria-controls="account-share-seat-filter"
                  @click="toggleFilterPopover('seat')"
                >
                  <Icon name="users" size="sm" />
                  <span>{{ seatFilterSummary }}</span>
                  <Icon name="chevronDown" size="xs" class="filter-trigger-chevron" />
                </button>
                <div
                  v-if="openFilterPopover === 'seat'"
                  id="account-share-seat-filter"
                  class="filter-popover seat-popover"
                  role="group"
                  :aria-label="t('accountShare.filters.seatOptions')"
                  @keydown.escape.stop="handleFilterPopoverEscape"
                >
                  <div class="seat-chip-grid">
                    <button
                      v-for="seat in seatOptions"
                      :key="seat"
                      type="button"
                      class="choice-chip"
                      :class="listingFilters.seatLimits.includes(seat) && 'choice-chip-active'"
                      :aria-pressed="listingFilters.seatLimits.includes(seat)"
                      @click="toggleSeatFilter(seat)"
                    >
                      {{ t('accountShare.filters.seatPeople', { seat }) }}
                    </button>
                  </div>
                </div>
              </div>

              <div class="filter-popover-wrap">
                <span class="filter-section-label">{{ t('accountShare.filters.tags') }}</span>
                <button
                  ref="featureFilterTriggerRef"
                  type="button"
                  class="filter-trigger-button"
                  :class="listingFilters.featureTags.length > 0 && 'filter-trigger-selected'"
                  :aria-expanded="openFilterPopover === 'feature'"
                  aria-controls="account-share-feature-filter"
                  @click="toggleFilterPopover('feature')"
                >
                  <Icon name="filter" size="sm" />
                  <span>{{ featureTagFilterSummary }}</span>
                  <Icon name="chevronDown" size="xs" class="filter-trigger-chevron" />
                </button>
                <div
                  v-if="openFilterPopover === 'feature'"
                  id="account-share-feature-filter"
                  class="filter-popover tag-popover"
                  role="group"
                  :aria-label="t('accountShare.filters.tagOptions')"
                  @keydown.escape.stop="handleFilterPopoverEscape"
                >
                  <button
                    v-for="option in visibleListingFeatureTagOptions"
                    :key="option.value"
                    type="button"
                    class="filter-menu-option"
                    :class="listingFilters.featureTags.includes(option.value) && 'filter-menu-option-active'"
                    :aria-pressed="listingFilters.featureTags.includes(option.value)"
                    @click="toggleFeatureTagFilter(option.value)"
                  >
                    <span>{{ option.label }}</span>
                    <Icon v-if="listingFilters.featureTags.includes(option.value)" name="check" size="sm" />
                  </button>
                </div>
              </div>

              <div class="filter-popover-wrap model-filter-wrap">
                <span class="filter-section-label">{{ t('accountShare.filters.models') }}</span>
                <button
                  ref="modelFilterTriggerRef"
                  type="button"
                  class="filter-trigger-button"
                  :class="listingFilters.models.length > 0 && 'filter-trigger-selected'"
                  :aria-expanded="openFilterPopover === 'model'"
                  aria-controls="account-share-model-filter"
                  @click="toggleFilterPopover('model')"
                >
                  <Icon name="filter" size="sm" />
                  <span>{{ modelFilterSummary }}</span>
                  <Icon name="chevronDown" size="xs" class="filter-trigger-chevron" />
                </button>
                <div
                  v-if="openFilterPopover === 'model'"
                  id="account-share-model-filter"
                  class="filter-popover model-popover"
                  role="group"
                  :aria-label="t('accountShare.filters.modelOptions')"
                  @keydown.escape.stop="handleFilterPopoverEscape"
                >
                  <div class="model-filter-options">
                    <button
                      v-for="model in modelFilterOptions"
                      :key="model"
                      type="button"
                      class="filter-menu-option"
                      :class="listingFilters.models.includes(model) && 'filter-menu-option-active'"
                      :aria-pressed="listingFilters.models.includes(model)"
                      @click="toggleModelFilter(model)"
                    >
                      <span>{{ model }}</span>
                      <Icon v-if="listingFilters.models.includes(model)" name="check" size="sm" />
                    </button>
                  </div>
                  <div class="model-filter-input-row">
                    <input
                      v-model.trim="modelFilterInput"
                      class="input h-10"
                      :placeholder="t('accountShare.filters.addModel')"
                      @keydown.enter.prevent="addModelFilterFromInput"
                    />
                    <button type="button" class="btn-secondary h-10" @click="addModelFilterFromInput">{{ t('common.add') }}</button>
                  </div>
                </div>
              </div>
            </div>

            <div class="sort-section" :aria-label="t('accountShare.sort.title')">
              <div class="sort-section-head">
                <span class="filter-section-label">{{ t('admin.activities.fields.sortOrder') }}</span>
              </div>
              <div class="sort-button-grid">
                <button
                  type="button"
                  class="sort-option-button sort-default-button"
                  :class="listingFilters.sortKeys.length === 0 && 'sort-option-active'"
                  :aria-pressed="listingFilters.sortKeys.length === 0"
                  :title="t('accountShare.sort.reset')"
                  @click="clearListingSorts"
                >
                  <Icon name="sort" size="sm" />
                  <span>{{ t('keys.endpoints.default') }}</span>
                  <Icon v-if="listingFilters.sortKeys.length === 0" name="check" size="xs" class="sort-option-check" />
                </button>
                <button
                  v-for="option in listingSortFieldOptions"
                  :key="option.sortBy"
                  type="button"
                  class="sort-option-button sort-field-button"
                  :class="isSortFieldActive(option.sortBy) && 'sort-option-active'"
                  :aria-pressed="isSortFieldActive(option.sortBy)"
                  :title="sortFieldButtonTitle(option)"
                  @click="toggleListingSortField(option.sortBy)"
                >
                  <Icon :name="sortDirectionIcon(option.sortBy)" size="sm" />
                  <span>{{ option.label }}</span>
                  <small v-if="sortPriorityLabel(option.sortBy)" class="sort-priority-badge">
                    {{ sortPriorityLabel(option.sortBy) }}
                  </small>
                  <small v-if="activeSortDirectionLabel(option)" class="sort-direction-pill">
                    {{ activeSortDirectionLabel(option) }}
                  </small>
                </button>
              </div>
            </div>

            <div v-if="activeFilterChips.length > 0" class="active-filter-row" :aria-label="t('accountShare.filterChips.title')">
              <button
                v-for="chip in activeFilterChips"
                :key="chip.key"
                type="button"
                class="active-filter-chip"
                @click="chip.remove"
              >
                <span>{{ chip.label }}</span>
                <Icon name="x" size="xs" />
              </button>
            </div>
          </details>
        </div>
      </section>

        <div class="marketplace-results" :aria-busy="currentViewLoading">
          <div class="marketplace-results-toolbar">
            <div class="marketplace-result-count">
              <h2>{{ isMembershipHistoryView ? t('accountShare.membership.statusHistory') : isArchiveView ? t('accountShare.tabs.deleted') : mainViewTab === 'using' ? t('accountShare.mySpend.tabUsing') : mainViewTab === 'mine' ? t('accountShare.filters.mine') : t('accountShare.tabs.discover') }}</h2>
              <span v-if="!isMembershipHistoryView">{{ loading ? '加载中…' : `${pagination.total_exact ? '' : '至少 '}${pagination.total} 个房间` }}</span>
              <span v-if="!isMembershipHistoryView && !isKeyResolutionMode" class="marketplace-page-capacity">{{ t('accountShare.list.pageSize', { pageSize: pagination.page_size }) }}</span>
            </div>
            <div v-if="!isMembershipHistoryView && !isArchiveView" class="marketplace-result-stats">
              <span>{{ t('accountShare.list.pageSeatsAvailable') }} <strong>{{ loading ? '—' : availableSeatCount }}</strong></span>
              <span>{{ t('accountShare.list.pageSeatsUsed') }} <strong>{{ loading ? '—' : activeSeatCount }}</strong></span>
              <span>{{ t('accountShare.estimate.modeKey') }} <strong>{{ modeKeysLoading && !modeKeysLoaded ? '…' : modeApiKeys.length }}</strong></span>
            </div>
            <button class="marketplace-refresh" type="button" :disabled="currentViewLoading || isAnyModeKeysLoading || selfUseSettingsLoading" @click="refreshPageData">
              <Icon name="refresh" size="sm" :class="{ 'animate-spin': currentViewLoading || isAnyModeKeysLoading || selfUseSettingsLoading }" />
              {{ t('common.refresh') }}
            </button>
          </div>
          <div ref="roomGridViewportRef" class="marketplace-result-body" data-testid="listing-viewport">
            <p v-if="isMembershipHistoryView" class="marketplace-history-note">{{ t('accountShare.list.usageNote') }}</p>
            <MembershipHistoryPanel
              v-if="isMembershipHistoryView"
              :items="membershipHistoryEntries"
              :loading="membershipHistoryLoading"
              :error-message="membershipHistoryError"
              :page="membershipHistoryPagination.page"
              :page-size="membershipHistoryPagination.page_size"
              :total="membershipHistoryPagination.total"
              :show-pagination="false"
              @reload="loadMembershipHistory"
              @update:page="handleMembershipHistoryPageChange"
              @review="openHistoryReviewDialog"
            />

            <template v-else>
              <div v-if="errorMessage" class="rounded-lg border border-red-200 bg-red-50 p-3 text-sm text-red-700 dark:border-red-500/30 dark:bg-red-500/10 dark:text-red-200">
                {{ errorMessage }}
              </div>


              <div v-if="loading" class="marketplace-empty" role="status">
                <Icon name="refresh" size="lg" class="animate-spin" />
                <strong>{{ t('accountShare.list.loading') }}</strong>
                <span>{{ t('accountShare.list.loadingHint') }}</span>
              </div>

              <section v-else-if="displayedListings.length > 0" ref="roomGridRef" class="listing-grid">
                <article
                  v-for="listing in displayedListings"
                  :key="listing.id"
                  class="listing-card room-preview-card"
                  :class="{ 'key-resolution-listing-card': isKeyResolutionListing(listing) }"
                  role="button"
                  tabindex="0"
                  :aria-label="t('accountShare.detail.viewRoomDetails', { name: isUnknownHistorySnapshot(listing) ? t('accountShare.common.roomIdHash', { id: listing.id }) : listingDisplayName(listing) })"
                  aria-haspopup="dialog"
                  :aria-expanded="detailListing?.id === listing.id"
                  @click="openRoomDetails(listing, $event)"
                  @keydown.enter.self.prevent="openRoomDetails(listing)"
                  @keydown.space.self.prevent="openRoomDetails(listing)"
                >
                  <div class="room-preview-heading">
                    <span class="listing-platform-icon" aria-hidden="true"><PlatformIcon :platform="listingPlatform(listing)" size="lg" /></span>
                    <div class="room-preview-name">
                      <h2>{{ isUnknownHistorySnapshot(listing) ? t('accountShare.common.roomIdHash', { id: listing.id }) : listingDisplayName(listing) }}</h2>
                      <span v-if="!isUnknownHistorySnapshot(listing)">{{ platformLabel(listingPlatform(listing)) }}<template v-if="isOpenAIListing(listing)"> · {{ accountLevelBadgeLabel(listing) }}</template></span>
                      <span v-else>{{ t('accountShare.membership.statusHistory') }}</span>
                    </div>
                    <span :class="listingStatusBadgeClass(listing)">{{ isArchiveView ? t('ideas.status.deleted') : listingStatusLabel(listing) }}</span>
                  </div>
                  <template v-if="!isUnknownHistorySnapshot(listing)">
                    <div class="room-preview-prices">
                      <div><span>{{ t('accountShare.card.requestRate') }}</span><strong>{{ formatNumber(listing.rate_multiplier) }}<small>×</small></strong></div>
                      <div><span>{{ t('accountShare.card.holdingFee') }}</span><strong>{{ formatNumber(listing.hourly_rate) }}<small>{{ t('accountShare.card.perHourSuffix') }}</small></strong></div>
                    </div>
                    <div class="room-preview-features">
                      <span v-if="isAPIAggregationListing(listing)" class="room-preview-feature-apikey" :title="t('accountShare.card.apikeyRoomTitle')">APIKEY</span>
                      <span v-if="listing.has_password" class="room-preview-feature-lock" :title="t('accountShare.card.needsPassword')">{{ t('accountShare.card.passwordRequired') }}</span>
                      <span v-if="isOpenAIListing(listing) && supportsImageGeneration(listing)">{{ t('accountShare.featureTags.imageGeneration') }}</span>
                      <span v-if="listing.hourly_fee_waiver_minimum > 0">{{ t('accountShare.card.feeWaived') }}</span>
                      <span v-if="isOpenAIListing(listing) && listing.codex_cli_only">{{ t('accountShare.card.officialClientOnly') }}</span>
                      <span v-if="listing.hourly_rate === 0">{{ t('accountShare.card.noHoldingFee') }}</span>
                    </div>
                    <div class="room-preview-models" :aria-label="t('accountShare.card.mainModels')">
                      <span v-for="model in listing.allowed_models.slice(0, 2)" :key="model" :title="model">{{ model }}</span>
                      <span v-if="listing.allowed_models.length > 2">+{{ listing.allowed_models.length - 2 }}</span>
                      <span v-if="listing.allowed_models.length === 0">{{ isArchiveView ? t('accountShare.card.modelsNotRecorded') : t('accountShare.card.noModels') }}</span>
                    </div>
                    <div v-if="!isArchiveView" class="room-preview-availability">
                      <span><Icon name="users" size="sm" />{{ t('accountShare.sortField.remainingSeats') }} <strong>{{ Math.max(0, listing.seat_limit - listing.active_seats) }}/{{ listing.seat_limit }}</strong></span>
                      <span><Icon name="star" size="sm" aria-hidden="true" />{{ listingRatingLabel(listing) }}</span>
                    </div>
                    <div v-else class="room-preview-history">{{ t('accountShare.card.readonlySnapshot') }}</div>
                  </template>
                  <p v-else class="room-preview-history">{{ t('accountShare.card.snapshotIncomplete') }}</p>
                  <div v-if="!isArchiveView && listingMembershipID(listing) > 0" class="room-preview-membership" :class="{ 'room-preview-membership-ending': isListingMembershipEnding(listing) }">
                    <Icon :name="isListingMembershipEnding(listing) ? 'clock' : 'key'" size="sm" />
                    <span>{{ membershipPanelTitle(listing) }} · {{ boundApiKeyDisplayName(listing) }}</span>
                  </div>
                  <footer class="room-preview-footer">
                    <span v-if="isUnknownHistorySnapshot(listing)">{{ t('accountShare.card.historyIncomplete') }}</span>
                    <span v-else :title="ownerDisplayName(listing)">{{ isOwnListing(listing) ? t('accountShare.card.myRoom') : t('accountShare.card.ownerPrefix') + ownerDisplayName(listing) }}</span>
                    <span class="room-preview-open">{{ t('accountShare.card.viewDetail') }} <Icon name="arrowRight" size="sm" /></span>
                  </footer>
                </article>
              </section>

              <div v-else class="marketplace-empty" role="status">
                <Icon :name="hasResultFilters ? 'search' : 'grid'" size="xl" />
                <strong>{{ isKeyResolutionMode ? (keyResolutionError ? t('accountShare.list.linkedUnavailable') : t('accountShare.list.noLinkedRooms')) : (pagination.total === 0 ? (hasResultFilters ? t('accountShare.list.noMatch') : (isArchiveView ? t('accountShare.list.noDeleted') : (isManagementView ? t('accountShare.list.noManaged') : t('accountShare.list.emptyCategory')))) : t('accountShare.list.emptyPage')) }}</strong>
                <button v-if="hasResultFilters && !isKeyResolutionMode" type="button" class="btn-secondary min-h-11" @click="resetListingFilters">{{ t('accountShare.list.resetFilters') }}</button>
              </div>

            </template>
          </div>
          <div class="marketplace-pagination" data-testid="listing-pagination-footer" :aria-busy="currentViewLoading">
            <span v-if="isKeyResolutionMode" class="marketplace-pagination-note">{{ t('accountShare.list.linkedShown') }}</span>
            <template v-else-if="isMembershipHistoryView">
              <Pagination
                v-if="membershipHistoryPagination.total > 0"
                class="marketplace-page-controls"
                :inert="membershipHistoryLoading || undefined"
                :page="membershipHistoryPagination.page"
                :total="membershipHistoryPagination.total"
                :page-size="membershipHistoryPagination.page_size"
                :show-page-size-selector="false"
                compact
                @update:page="handleMembershipHistoryPageChange"
              />
              <span v-else class="marketplace-pagination-note">{{ membershipHistoryLoading ? t('accountShare.mySpend.loadingHistory') : t('admin.ops.alertEvents.detail.historyEmpty') }}</span>
            </template>
            <template v-else>
              <Pagination
                v-if="pagination.total_exact && pagination.total > 0"
                class="marketplace-page-controls"
                :inert="loading || undefined"
                :page="pagination.page"
                :total="pagination.total"
                :page-size="pagination.page_size"
                :show-page-size-selector="false"
                compact
                @update:page="handlePageChange"
              />
              <LowerBoundPagination
                v-else-if="!pagination.total_exact"
                class="marketplace-page-controls"
                :page="pagination.page"
                :total="pagination.total"
                :has-more="Boolean(pagination.has_more)"
                :loading="loading"
                data-testid="listing-cursor-pagination"
                @update:page="handlePageChange"
              />
              <span v-else class="marketplace-pagination-note">{{ loading ? t('accountShare.list.loadingRooms') : t('accountShare.list.noRoomsMatched') }}</span>
            </template>
          </div>
        </div>
      </div>
    </div>

    <BaseDialog
      :show="pendingJoinConfirmation !== null"
      :title="pendingJoinIsOwnerSelfUse ? t('accountShare.join.confirmOwnAccount') : t('accountShare.join.confirmJoin')"
      width="wide"
      :z-index="60"
      :close-disabled="joinDialogBusy"
      :close-on-escape="!joinDialogBusy"
      @close="closeJoinConfirmation"
    >
      <div v-if="pendingJoinIntent && pendingJoinTerms" class="join-confirmation" data-testid="join-confirmation">
        <div class="join-confirmation-head" :class="{ 'join-confirmation-head-danger': pendingJoinPriceWarnings.length > 0 }">
          <span class="join-confirmation-icon">
            <Icon :name="pendingJoinPriceWarnings.length > 0 ? 'exclamationCircle' : 'infoCircle'" size="md" />
          </span>
          <div class="min-w-0">
            <strong>{{ pendingJoinTerms.room_name || t('accountShare.common.roomIdHash', { id: pendingJoinIntent.listing_id }) }}</strong>
            <span>{{ pendingJoinIsOwnerSelfUse ? `这是你自己的房间。绑定后按全局自用倍率 ${ownerSelfUseRateMultiplierLabel} 计算请求费用，不收小时费，也不占用消费者名额。` : '以下内容来自服务端刚刚签发的条款快照；确认后，该 API Key 会按这份快照加入房间。' }}</span>
          </div>
        </div>

        <div v-if="pendingJoinPriceWarnings.length > 0" class="join-warning-list">
          <div v-for="warning in pendingJoinPriceWarnings" :key="warning" class="join-warning-item">
            <Icon name="exclamationCircle" size="sm" />
            <span>{{ warning }}</span>
          </div>
        </div>

        <div v-if="pendingJoinRequiresUnverifiedAck" class="join-warning-list join-warning-unverified" data-testid="join-unverified-warning">
          <div class="join-warning-item">
            <Icon name="exclamationCircle" size="sm" />
            <span>{{ t('accountShare.join.unverifiedWarning') }}</span>
          </div>
          <label class="join-unverified-ack">
            <input
              v-model="joinUnverifiedAcknowledged"
              type="checkbox"
              data-testid="join-unverified-ack"
            />
            <span>{{ t('accountShare.join.unverifiedAckLabel') }}</span>
          </label>
        </div>

        <div class="join-confirmation-grid">
          <div class="join-confirmation-field">
            <span>{{ t('accountShare.join.termsVersion') }}</span>
            <strong>v{{ pendingJoinTerms.row_version }} · rev {{ pendingJoinTerms.listing_revision_id || 0 }}</strong>
          </div>
          <div class="join-confirmation-field">
            <span>{{ t('accountShare.filters.roomStatus') }}</span>
            <strong>{{ pendingJoinTerms.status === 'active' ? t('accountShare.join.joinable') : pendingJoinTerms.status }}</strong>
          </div>
          <div class="join-confirmation-field">
            <span>{{ t('accountShare.join.consumerSeats') }}</span>
            <strong>{{ pendingJoinTerms.seat_limit }}</strong>
          </div>
          <div class="join-confirmation-field" :class="{ 'join-price-danger': !pendingJoinIsOwnerSelfUse && pendingJoinTerms.rate_multiplier > 1 }">
            <span>{{ pendingJoinIsOwnerSelfUse ? t('accountShare.join.selfUseRate') : t('groups.rateLabel') }}</span>
            <strong>{{ pendingJoinIsOwnerSelfUse ? ownerSelfUseRateMultiplierLabel : `${formatNumber(pendingJoinTerms.rate_multiplier)}x` }}</strong>
          </div>
          <div v-if="pendingJoinIsOwnerSelfUse" class="join-confirmation-field">
            <span>{{ t('accountShare.join.publicRate') }}</span>
            <strong>{{ formatNumber(pendingJoinTerms.rate_multiplier) }}x</strong>
          </div>
          <div class="join-confirmation-field" :class="{ 'join-price-danger': !pendingJoinIsOwnerSelfUse && pendingJoinTerms.hourly_rate > EXPENSIVE_HOURLY_RATE }">
            <span>{{ t('accountShare.membership.fieldHourlyRate') }}</span>
            <strong>{{ pendingJoinIsOwnerSelfUse ? t('accountShare.estimate.notCharged') : formatNumber(pendingJoinTerms.hourly_rate) }}</strong>
          </div>
          <div class="join-confirmation-field">
            <span>{{ t('accountShare.membership.fieldFeeWaiver') }}</span>
            <strong>{{ pendingJoinIsOwnerSelfUse ? t('accountShare.join.notApplicable') : hourlyFeeWaiverLabel(pendingJoinTerms.hourly_fee_waiver_minimum) }}</strong>
          </div>
          <div class="join-confirmation-field">
            <span>{{ t('accountShare.membership.fieldMinBalance') }}</span>
            <strong>{{ pendingJoinIsOwnerSelfUse ? t('accountShare.estimate.notChecked') : formatNumber(pendingJoinTerms.min_balance_required) }}</strong>
          </div>
          <div class="join-confirmation-field">
            <span>{{ t('accountShare.join.perUserConcurrency') }}</span>
            <strong>{{ pendingJoinTerms.per_user_concurrency }}</strong>
          </div>
          <div class="join-confirmation-field">
            <span>{{ t('accountShare.join.boundKey') }}</span>
            <strong>{{ pendingJoinApiKeyLabel }}</strong>
          </div>
          <div class="join-confirmation-field">
            <span>{{ t('accountShare.membership.fieldIdleTimeout') }}</span>
            <strong>{{ pendingJoinIdleTimeoutLabel }}</strong>
          </div>
          <div v-if="pendingJoinHasOpenAIProtection" class="join-confirmation-field">
            <span>{{ t('accountShare.join.codexProtection') }}</span>
            <strong>{{ pendingJoinTerms.codex_5h_limit_percent }}% / {{ pendingJoinTerms.codex_7d_limit_percent }}%</strong>
          </div>
          <div v-if="pendingJoinHasAnthropicProtection" class="join-confirmation-field">
            <span>{{ t('accountShare.join.claudeProtection') }}</span>
            <strong>{{ pendingJoinTerms.anthropic_5h_limit_percent || 0 }}% / {{ pendingJoinTerms.anthropic_7d_limit_percent || 0 }}%</strong>
          </div>
        </div>


        <div v-if="joinIntentError" class="join-intent-state join-intent-state-error" data-testid="join-intent-error">
          <Icon name="exclamationCircle" size="sm" />
          <span>{{ joinIntentError }}</span>
        </div>

        <div class="join-usage-reminder">
          <Icon name="infoCircle" size="sm" />
          <span>{{ pendingJoinIsOwnerSelfUse ? `确认使用后，连续空闲达到 ${pendingJoinIdleTimeoutLabel} 会自动解除绑定；自用期间不产生小时费和号主收益。` : `确认令牌有效至 ${formatDate(pendingJoinIntent.expires_at)}。加入后即可使用；连续空闲达到 ${pendingJoinIdleTimeoutLabel} 会自动退出并停止占位。` }}</span>
        </div>

        <div class="join-model-confirmation">
          <span>{{ t('accountShare.filters.models') }}</span>
          <div>
            <button
              v-for="model in pendingJoinVisibleModels"
              :key="model"
              type="button"
              class="model-copy-chip"
              :title="t('accountShare.common.copyModel', { model })"
              @click="copyModelName(model)"
            >
              {{ model }}
            </button>
            <span v-if="pendingJoinHiddenModelCount > 0" class="join-model-more">+{{ pendingJoinHiddenModelCount }}</span>
          </div>
        </div>
      </div>

      <template #footer>
        <button type="button" class="btn-secondary min-h-11" :disabled="joinDialogBusy" @click="closeJoinConfirmation">{{ t('common.cancel') }}</button>
        <button
          type="button"
          class="btn-primary min-h-11"
          :disabled="!pendingJoinCanSubmit"
          data-testid="join-confirm-submit"
          @click="confirmJoinUse"
        >
          <Icon v-if="!joinDialogBusy" name="checkCircle" size="sm" class="mr-2" />
          <svg v-else class="-ml-1 mr-2 h-4 w-4 animate-spin" fill="none" viewBox="0 0 24 24">
            <circle class="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="4"></circle>
            <path class="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"></path>
          </svg>
          {{ joiningId !== null ? t('accountShare.join.confirming') : (pendingJoinExpired ? t('accountShare.join.checkBinding') : (joinIntentError ? t('accountShare.join.retryConfirm') : (pendingJoinIsOwnerSelfUse ? t('accountShare.join.confirmUse') : t('accountShare.join.confirmJoinShort')))) }}
        </button>
      </template>
    </BaseDialog>

    <BaseDialog
      :show="joinPasswordPrompt !== null"
      :title="t('accountShare.join.enterPassword')"
      width="narrow"
      :z-index="70"
      :close-disabled="preparingJoinId !== null"
      :close-on-escape="preparingJoinId === null"
      @close="closeJoinPasswordPrompt"
    >
      <div v-if="joinPasswordPrompt" class="grid gap-4" data-testid="join-password-prompt">
        <p class="text-sm leading-6 text-slate-600 dark:text-dark-300">
          {{ t('accountShare.join.passwordGate', { id: joinPasswordPrompt.listing.room_name || t('accountShare.common.roomIdHash', { id: joinPasswordPrompt.listing.id }) }) }}
        </p>
        <label class="field">
          <span>{{ t('accountShare.join.passwordLabel') }}</span>
          <input
            v-model="joinPasswordInput"
            class="input"
            type="password"
            maxlength="64"
            autocomplete="off"
            :placeholder="t('accountShare.join.passwordPlaceholder')"
            :disabled="preparingJoinId !== null"
            data-testid="join-password-input"
            @keydown.enter.prevent="submitJoinPassword"
          />
        </label>
        <p v-if="joinPasswordError" class="join-intent-state join-intent-state-error" role="alert">
          <Icon name="exclamationCircle" size="sm" />
          <span>{{ joinPasswordError }}</span>
        </p>
      </div>
      <template #footer>
        <button type="button" class="btn-secondary min-h-11" :disabled="preparingJoinId !== null" @click="closeJoinPasswordPrompt">{{ t('common.cancel') }}</button>
        <button
          type="button"
          class="btn-primary min-h-11"
          :disabled="preparingJoinId !== null || !joinPasswordInput.trim()"
          data-testid="join-password-submit"
          @click="submitJoinPassword"
        >
          {{ preparingJoinId !== null ? t('accountShare.join.verifying') : t('auth.continue') }}
        </button>
      </template>
    </BaseDialog>

    <BaseDialog
      :show="actionErrorDialog.show"
      :title="actionErrorDialog.title"
      width="narrow"
      :z-index="70"
      @close="closeActionErrorDialog"
    >
      <div class="flex items-start gap-3">
        <span class="flex h-10 w-10 flex-shrink-0 items-center justify-center rounded-lg bg-red-50 text-red-600 dark:bg-red-500/10 dark:text-red-300">
          <Icon name="exclamationCircle" size="md" />
        </span>
        <p class="min-w-0 text-sm leading-6 text-gray-700 dark:text-dark-200">
          {{ actionErrorDialog.message }}
        </p>
      </div>

      <template #footer>
        <button type="button" class="btn-secondary" @click="closeActionErrorDialog">{{ t('accountShare.guide.gotIt') }}</button>
        <button
          v-if="actionErrorDialog.action === 'create-mode-key'"
          type="button"
          class="btn-primary"
          @click="goCreateModeApiKey"
        >
          <Icon name="key" size="sm" class="mr-2" />
          {{ t('accountShare.join.goCreateKey') }}
        </button>
      </template>
    </BaseDialog>

    <BaseDialog
      :show="showMySpendDialog"
      :title="t('accountShare.page.mySpend')"
      width="wide"
      :z-index="65"
      @close="closeMySpendDialog"
    >
      <div class="my-spend-panel">
        <div class="my-spend-account-picker">
          <div class="my-spend-account-picker-head">
            <div>
              <span>{{ t('accountShare.mySpend.selectAccount') }}</span>
              <strong>{{ mySpendAccountPickerTitle }}</strong>
              <small>{{ t('accountShare.mySpend.selectHint') }}</small>
            </div>
            <button type="button" class="btn-secondary min-h-11" :disabled="mySpendAccountsLoading" @click="loadMySpendAccountOptions()">
              <Icon name="refresh" size="xs" class="mr-2" :class="{ 'animate-spin': mySpendAccountsLoading }" />
              {{ t('accountShare.create.refreshAccounts') }}
            </button>
          </div>

          <div v-if="mySpendAccountsError" class="notice-row border-red-200 bg-red-50 text-red-700 dark:border-red-900/60 dark:bg-red-900/20 dark:text-red-300">
            <Icon name="exclamationCircle" size="sm" class="mt-0.5 flex-shrink-0" />
            <span>{{ mySpendAccountsError }}</span>
          </div>

          <div class="my-spend-range-tabs my-spend-source-tabs" role="tablist" :aria-label="t('accountShare.mySpend.recordType')">
            <button
              type="button"
              role="tab"
              :class="{ active: mySpendPickerSource === 'using' }"
              :aria-selected="mySpendPickerSource === 'using'"
              :disabled="mySpendAccountsLoading"
              @click="setMySpendPickerSource('using')"
            >
              {{ t('accountShare.mySpend.usingCount', { totalExact: countLabel(mySpendUsingPagination.total, mySpendUsingPagination.totalExact) }) }}
            </button>
            <button
              type="button"
              role="tab"
              :class="{ active: mySpendPickerSource === 'history' }"
              :aria-selected="mySpendPickerSource === 'history'"
              :disabled="mySpendAccountsLoading"
              @click="setMySpendPickerSource('history')"
            >
              {{ t('accountShare.mySpend.historyCount', { total: mySpendHistoryPagination.total }) }}
            </button>
          </div>

          <div v-if="mySpendAccountsLoading && mySpendAccountOptions.length === 0" class="my-spend-loading">
            {{ t('accountShare.mySpend.loadingAccounts') }}
          </div>
          <div v-else-if="!mySpendAccountsLoading && mySpendAccountOptions.length === 0" class="my-spend-empty">
            {{ mySpendPickerSource === 'using'
              ? t('accountShare.mySpend.noUsing')
              : t('accountShare.mySpend.noHistory') }}
          </div>
          <template v-else>
            <div class="my-spend-account-grid">
              <button
                v-for="option in mySpendAccountOptions"
                :key="option.key"
                type="button"
                class="my-spend-account-option"
                :class="{ active: mySpendSelectedOptionKey === option.key }"
                :title="mySpendAccountOptionTitle(option)"
                :disabled="mySpendAccountsLoading"
                @click="selectMySpendAccount(option)"
              >
                <span class="my-spend-account-option-top">
                  <span class="feature-badge">{{ platformLabel(option.platform) }}</span>
                  <span>{{ mySpendAccountSourceLabel(option.source) }}</span>
                </span>
                <strong>{{ mySpendAccountDisplayName(option) }}</strong>
                <small>{{ mySpendAccountUsagePeriod(option) }}</small>
                <span class="my-spend-account-option-foot">
                  <span>{{ t('accountShare.mySpend.recordId', { id: option.membershipID  }) }}</span>
                  <span>{{ mySpendAccountStatusLabel(option) }}</span>
                </span>
              </button>
            </div>
            <Pagination
              v-if="mySpendActivePickerPagination.totalExact && mySpendActivePickerPagination.total > mySpendActivePickerPagination.pageSize"
              class="overflow-hidden rounded-xl border border-slate-200 shadow-sm dark:border-dark-700"
              :page="mySpendActivePickerPagination.page"
              :total="mySpendActivePickerPagination.total"
              :page-size="mySpendActivePickerPagination.pageSize"
              :show-page-size-selector="false"
              @update:page="handleMySpendAccountPageChange"
            />
            <LowerBoundPagination
              v-if="!mySpendActivePickerPagination.totalExact && (mySpendActivePickerPagination.page > 1 || mySpendActivePickerPagination.hasMore)"
              :page="mySpendActivePickerPagination.page"
              :total="mySpendActivePickerPagination.total"
              :has-more="mySpendActivePickerPagination.hasMore"
              :loading="mySpendAccountsLoading"
              :label="t('accountShare.mySpend.pagination')"
              data-testid="my-spend-cursor-pagination"
              @update:page="handleMySpendAccountPageChange"
            />
          </template>
        </div>

        <div v-if="mySpendSelectedOption" class="my-spend-context">
          <span class="my-spend-context-icon">
            <Icon name="dollar" size="md" />
          </span>
          <div class="min-w-0">
            <span class="my-spend-eyebrow">
              {{ t('accountShare.mySpend.platformRoom', { platform: platformLabel(mySpendSelectedOption.platform), listingID: mySpendSelectedOption.listingID }) }}
              <template v-if="mySpendSelectedOption.roomDeleted"> {{ t('accountShare.mySpend.deletedSuffix') }}</template>
            </span>
            <strong>{{ mySpendAccountDisplayName(mySpendSelectedOption) }}</strong>
            <small>
              {{ t('accountShare.mySpend.ownerRecord', { ownerUserID: mySpendSelectedOption.ownerUsername || t('accountShare.common.userId', { id: mySpendSelectedOption.ownerUserID }), membershipID: mySpendSelectedOption.membershipID }) }}
            </small>
          </div>
        </div>

        <div class="my-spend-toolbar">
          <div class="my-spend-range-tabs" role="tablist" :aria-label="t('accountShare.mySpend.statRange')">
            <button
              v-for="option in MY_SPEND_RANGE_OPTIONS"
              :key="option.value"
              type="button"
              :class="{ active: mySpendRange === option.value }"
              :aria-selected="mySpendRange === option.value"
              :disabled="mySpendHistorySelection && option.value !== 'current_membership'"
              :title="mySpendHistorySelection && option.value !== 'current_membership' ? t('accountShare.mySpend.historyPreciseHint') : undefined"
              role="tab"
              @click="setMySpendRange(option.value)"
            >
              {{ option.label }}
            </button>
          </div>
          <button type="button" class="btn-secondary min-h-11" :disabled="mySpendLoading || !mySpendSelectedOption" @click="loadMySpendSummary">
            <Icon name="refresh" size="xs" class="mr-2" />
            {{ t('common.refresh') }}
          </button>
        </div>

        <div
          v-if="mySpendHistorySelection"
          class="notice-row border-sky-200 bg-sky-50 text-sky-800 dark:border-sky-900/60 dark:bg-sky-950/30 dark:text-sky-200"
        >
          <Icon name="infoCircle" size="sm" class="mt-0.5 flex-shrink-0" />
          <span>{{ t('accountShare.mySpend.historyScopeDesc') }}</span>
        </div>

        <div
          v-if="!mySpendLoading && !mySpendSelectedOption && (mySpendUsingPagination.total + mySpendHistoryPagination.total) > 0"
          class="my-spend-empty"
        >
          {{ t('accountShare.mySpend.selectToView') }}
        </div>

        <div v-if="mySpendError" class="notice-row border-red-200 bg-red-50 text-red-700 dark:border-red-900/60 dark:bg-red-900/20 dark:text-red-300">
          <Icon name="exclamationCircle" size="sm" class="mt-0.5 flex-shrink-0" />
          <span>{{ mySpendError }}</span>
        </div>

        <div v-if="mySpendLoading && !mySpendSummary" class="my-spend-loading">
          {{ t('accountShare.mySpend.loadingStats') }}
        </div>

        <template v-else-if="mySpendSummary">
          <div class="my-spend-window">
            <div>
              <span>{{ mySpendRangeLabel(mySpendSummary.range) }}</span>
              <strong>{{ mySpendWindowLabel(mySpendSummary) }}</strong>
            </div>
            <div>
              <span>{{ t('accountShare.mySpend.lastSettled') }}</span>
              <strong>{{ mySpendLastActivityLabel(mySpendSummary) }}</strong>
            </div>
          </div>

          <div class="my-spend-metric-grid">
            <div v-for="metric in mySpendMetrics" :key="metric.key" class="my-spend-metric" :class="`my-spend-metric-${metric.tone}`">
              <span>
                <Icon :name="metric.icon" size="xs" />
                {{ metric.label }}
              </span>
              <strong>{{ metric.value }}</strong>
              <small>{{ metric.note }}</small>
            </div>
          </div>

          <div class="my-spend-detail-grid">
            <div>
              <span>{{ t('accountShare.mySpend.statAccount') }}</span>
              <strong>{{ mySpendAccountName(mySpendSummary) }}</strong>
            </div>
            <div>
              <span>{{ t('accountShare.join.boundKey') }}</span>
              <strong>{{ mySpendBoundApiKeyName(mySpendSummary.membership) }}</strong>
              <small v-if="mySpendSummary.membership?.api_key_id">ID #{{ mySpendSummary.membership.api_key_id }}</small>
            </div>
            <div>
              <span>{{ t('accountShare.mySpend.useStatus') }}</span>
              <strong>{{ mySpendStatusLabel(mySpendSummary.membership?.status) }}</strong>
            </div>
            <div>
              <span>{{ t('accountShare.mySpend.joinedAt') }}</span>
              <strong>{{ formatDate(mySpendSummary.membership?.joined_at) }}</strong>
            </div>
            <div>
              <span>{{ t('accountShare.mySpend.avgRequestCost') }}</span>
              <strong>{{ mySpendAverageRequestCost(mySpendSummary) }}</strong>
            </div>
            <div>
              <span>{{ t('accountShare.mySpend.minSpendThreshold') }}</span>
              <strong>{{ mySpendSummary.membership ? hourlyFeeWaiverLabel(mySpendSummary.membership.waiver_minimum) : '-' }}</strong>
            </div>
          </div>

          <div class="my-spend-hourly-panel">
            <div>
              <span>{{ t('accountShare.mySpend.hourlyPrecharged') }}</span>
              <strong>{{ formatSpendCost(mySpendSummary.hourly_charge) }}</strong>
            </div>
            <div>
              <span>{{ t('accountShare.mySpend.normalRefund') }}</span>
              <strong>{{ formatSpendCost(mySpendSummary.hourly_refund) }}</strong>
            </div>
            <div>
              <span>{{ t('accountShare.mySpend.waiverRefund') }}</span>
              <strong>{{ formatSpendCost(mySpendSummary.hourly_waiver_refund) }}</strong>
            </div>
            <div>
              <span>{{ t('accountShare.mySpend.actualCharge') }}</span>
              <strong>{{ formatSpendCost(mySpendSummary.hourly_net_cost) }}</strong>
            </div>
          </div>

          <div class="my-spend-breakdown">
            <div class="my-spend-section-head">
              <div>
                <strong>{{ t('accountShare.mySpend.byModel') }}</strong>
                <small>{{ t('accountShare.mySpend.byModelHint') }}</small>
              </div>
            </div>
            <div v-if="mySpendSummary.model_breakdown.length === 0" class="my-spend-empty">
              {{ t('accountShare.mySpend.noRecordsInRange') }}
            </div>
            <div v-else class="my-spend-table-wrap">
              <table class="my-spend-table">
                <thead>
                  <tr>
                    <th>{{ t('keyUsage.model') }}</th>
                    <th>{{ t('home.requestsLabel') }}</th>
                    <th>Token</th>
                    <th>{{ t('accountShare.mySpend.requestCost') }}</th>
                    <th>{{ t('accountShare.mySpend.avgPrice') }}</th>
                  </tr>
                </thead>
                <tbody>
                  <tr v-for="item in mySpendSummary.model_breakdown" :key="item.model">
                    <td>{{ item.model }}</td>
                    <td>{{ formatWholeNumber(item.request_count) }}</td>
                    <td>{{ formatWholeNumber(item.total_tokens) }}</td>
                    <td>{{ formatSpendCost(item.request_cost) }}</td>
                    <td>{{ formatSpendCost(item.average_request_cost) }}</td>
                  </tr>
                </tbody>
              </table>
            </div>
          </div>
        </template>
      </div>

      <template #footer>
        <button type="button" class="btn-secondary" @click="closeMySpendDialog">{{ t('common.close') }}</button>
      </template>
    </BaseDialog>

    <RoomAccountsDialog
      :show="roomAccountsListing !== null"
      :listing="roomAccountsListing"
      :proxies="proxies"
      @close="closeRoomAccountsDialog"
      @changed="handleRoomAccountsChanged"
    />

    <BaseDialog
      :show="showConfigEditDialog"
      :title="t('accountShare.roomEdit.title')"
      width="extra-wide"
      :close-disabled="savingConfigEdit || pendingDraftDiscardTarget === 'config'"
      @close="closeConfigEditDialog"
    >
      <div class="space-y-5">
        <div v-if="editingConfigListing" class="edit-context-panel">
          <div class="min-w-0">
            <span class="edit-context-eyebrow">{{ t('accountShare.common.roomIdHash', { id: editingConfigListing.id }) }}</span>
            <strong>{{ listingDisplayName(editingConfigListing) }}</strong>
            <small>
              {{ t('accountShare.roomEdit.seats', { activeSeats: editingConfigListing.active_seats, seatLimit: editingConfigListing.seat_limit }) }}
            </small>
          </div>
          <span v-if="editForceActive" class="edit-force-badge">{{ t('accountShare.roomEdit.adminForce') }}</span>
        </div>

        <div
          v-if="editForceActive"
          class="notice-row border-amber-200 bg-amber-50 text-amber-800 dark:border-amber-900/60 dark:bg-amber-900/20 dark:text-amber-200"
        >
          <Icon name="exclamationTriangle" size="sm" class="mt-0.5 flex-shrink-0" />
          <span>{{ t('accountShare.roomEdit.adminConfirmed') }}</span>
        </div>
        <div
          v-else-if="editConsumerProtected"
          class="notice-row border-blue-200 bg-blue-50 text-blue-800 dark:border-blue-900/60 dark:bg-blue-900/20 dark:text-blue-200"
        >
          <Icon name="exclamationCircle" size="sm" class="mt-0.5 flex-shrink-0" />
          <span>{{ t('accountShare.roomEdit.inUseLimits') }}</span>
        </div>

        <div v-if="editErrorMessage" data-testid="config-edit-error" class="notice-row border-red-200 bg-red-50 text-red-700 dark:border-red-900/60 dark:bg-red-900/20 dark:text-red-300">
          <Icon name="exclamationCircle" size="sm" class="mt-0.5 flex-shrink-0" />
          <div class="min-w-0 flex-1">
            <span>{{ editErrorMessage }}</span>
            <button
              v-if="editVersionConflict"
              type="button"
              class="mt-2 min-h-11 rounded-lg border border-red-300 bg-white px-3 py-2 text-sm font-semibold text-red-700 hover:bg-red-50 dark:border-red-800 dark:bg-dark-900 dark:text-red-200"
              data-testid="reload-conflicted-room-config"
              @click="reloadConfigEditAfterConflict"
            >
              {{ t('accountShare.roomEdit.refreshReopen') }}
            </button>
          </div>
        </div>

        <div class="grid gap-5 xl:grid-cols-[minmax(0,1fr)_320px]">
          <div class="space-y-5">
            <div class="form-section">
              <div class="section-heading">
                <span>{{ t('accountShare.roomEdit.baseConfig') }}</span>
                <small>{{ t('accountShare.roomEdit.baseConfigHint') }}</small>
              </div>
              <div class="grid gap-3 md:grid-cols-2 xl:grid-cols-4">
                <label class="field">
                  <span>{{ t('accountShare.create.roomName') }}</span>
                  <input v-model="editForm.name" class="input" :placeholder="ACCOUNT_NAME_BASE_BY_PLATFORM[listingPlatform(editingConfigListing)]" />
                  <small :class="editAccountNameValidationMessage ? 'text-red-600 dark:text-red-300' : ''">
                    {{ editAccountNameValidationMessage || t('accountShare.roomEdit.nameHint') }}
                  </small>
                </label>

                <label class="field">
                  <span>{{ t('accountShare.create.seatLimit') }}</span>
                  <input
                    v-model.number="editForm.seat_limit"
                    class="input"
                    type="number"
                    :min="ACCOUNT_SHARE_MIN_SEATS"
                    :max="ACCOUNT_SHARE_MAX_SEATS"
                    step="1"
                    inputmode="numeric"
                    data-testid="edit-room-seat-limit"
                  />
                  <small>{{ t('accountShare.memberLimitHelp') }}</small>
                </label>

                <label class="field">
                  <span>{{ t('accountShare.create.perUserConcurrency') }}</span>
                  <input v-model.number="editForm.per_user_concurrency" class="input" type="number" min="1" :max="editMaxPerUserConcurrency" step="1" />
                  <small :class="editPerUserConcurrencyValidationMessage ? 'text-red-600 dark:text-red-300' : ''">
                    {{ editPerUserConcurrencyValidationMessage || editPerUserConcurrencyLimitTip }}
                  </small>
                </label>

                <label class="field">
                  <span>{{ t('usage.accountMultiplier') }}</span>
                  <input v-model.number="editForm.rate_multiplier" class="input" type="number" min="0" step="0.01" />
                </label>

                <label class="field">
                  <span>{{ t('accountShare.create.hourlyRate') }}</span>
                  <input v-model.number="editForm.hourly_rate" class="input" type="number" min="0" step="0.0001" />
                </label>

                <label class="field">
                  <span>{{ t('accountShare.featureTags.hourlyFeeWaiver') }}</span>
                  <input v-model.number="editForm.hourly_fee_waiver_minimum" class="input" type="number" min="0" step="0.0001" />
                </label>

                <label class="field">
                  <span>{{ t('accountShare.create.minBalance') }}</span>
                  <input v-model.number="editForm.min_balance_required" class="input" type="number" min="0" step="0.01" />
                </label>
              </div>
            </div>

            <div class="form-section">
              <div class="section-heading">
                <span>{{ t('accountShare.roomEdit.modelsProtection') }}</span>
                <small>{{ t('accountShare.roomEdit.modelsHint') }}</small>
              </div>
              <div class="grid gap-4 xl:grid-cols-[minmax(0,1fr)_280px]">
                <div class="field">
                  <span>{{ t('admin.accounts.modelWhitelist') }}</span>
                  <div class="model-selector-shell">
                    <ModelWhitelistSelector
                      v-model="editAllowedModels"
                      :platform="listingPlatform(editingConfigListing)"
                      :allowed-options="editingConfigListing?.supported_models"
                      :allow-custom="false"
                    />
                  </div>
                </div>

                <div v-if="listingPlatform(editingConfigListing) === 'openai'" class="grid gap-3 sm:grid-cols-2 xl:grid-cols-1">
                  <label class="field">
                    <span>{{ t('accountShare.create.codex5h') }}</span>
                    <input v-model.number="editForm.codex_5h_limit_percent" class="input" type="number" min="1" max="100" step="1" />
                  </label>
                  <label class="field">
                    <span>{{ t('accountShare.create.codex7d') }}</span>
                    <input v-model.number="editForm.codex_7d_limit_percent" class="input" type="number" min="1" max="100" step="1" />
                  </label>
                </div>
                <div v-else-if="listingPlatform(editingConfigListing) === 'anthropic'" class="grid gap-3 sm:grid-cols-2 xl:grid-cols-1">
                  <label class="field">
                    <span>{{ t('accountShare.create.claude5h') }}</span>
                    <input v-model.number="editForm.anthropic_5h_limit_percent" class="input" type="number" min="1" max="100" step="1" />
                  </label>
                  <label class="field">
                    <span>{{ t('accountShare.create.claude7d') }}</span>
                    <input v-model.number="editForm.anthropic_7d_limit_percent" class="input" type="number" min="1" max="100" step="1" />
                  </label>
                </div>
              </div>

              <div v-if="editConcurrencyNotice" class="notice-row mt-3">
                <Icon name="infoCircle" size="sm" class="mt-0.5 flex-shrink-0" />
                <span>{{ editConcurrencyNotice }}</span>
              </div>

              <label v-if="listingPlatform(editingConfigListing) === 'openai'" class="toggle-row mt-3">
                <input v-model="editForm.codex_cli_only" type="checkbox" />
                <span>
                  <strong>{{ t('admin.accounts.openai.codexCLIOnly') }}</strong>
                  <small>{{ t('accountShare.create.cliOnlyHint') }}</small>
                </span>
              </label>
            </div>

            <div class="form-section">
              <div class="section-heading">
                <span>{{ t('accountShare.join.passwordLabel') }}</span>
                <small>{{ t('accountShare.roomEdit.passwordHint') }}</small>
              </div>
              <label class="field">
                <span>{{ editingConfigListing?.has_password ? t('accountShare.roomEdit.changePassword') : t('accountShare.roomEdit.setPassword') }}</span>
                <input
                  v-model="editForm.join_password"
                  class="input"
                  type="password"
                  maxlength="64"
                  autocomplete="new-password"
                  :disabled="editJoinPasswordClear"
                  :placeholder="editingConfigListing?.has_password ? t('accountShare.roomEdit.passwordKeepPlaceholder') : t('accountShare.create.passwordPlaceholder')"
                  data-testid="edit-room-join-password"
                />
                <small>{{ editingConfigListing?.has_password ? t('accountShare.roomEdit.passwordSet') : t('accountShare.roomEdit.passwordUnset') }}</small>
              </label>
              <label v-if="editingConfigListing?.has_password" class="toggle-row mt-3">
                <input v-model="editJoinPasswordClear" type="checkbox" data-testid="edit-room-join-password-clear" />
                <span>
                  <strong>{{ t('accountShare.roomEdit.removePassword') }}</strong>
                  <small>{{ t('accountShare.roomEdit.removePasswordHint') }}</small>
                </span>
              </label>
            </div>

            <div class="form-section">
              <div class="section-heading">
                <span>{{ t('accountShare.quotaAdmin.reasonLabel') }}</span>
                <small>{{ t('accountShare.roomEdit.reasonHint') }}</small>
              </div>
              <label class="field">
                <span>{{ t('accountShare.roomEdit.reasonLabel') }}</span>
                <textarea
                  v-model="editReason"
                  class="input min-h-24"
                  maxlength="1000"
                  :placeholder="t('accountShare.roomEdit.reasonPlaceholder')"
                  data-testid="room-config-update-reason"
                ></textarea>
                <small :class="!editReason.trim() ? 'text-amber-700 dark:text-amber-300' : ''">
                  {{ t('accountShare.roomEdit.reasonCount', { length: editReason.trim().length }) }}
                </small>
              </label>
            </div>
          </div>

          <aside class="edit-summary-panel">
            <span class="text-xs font-semibold text-gray-500 dark:text-dark-300">{{ t('accountShare.roomEdit.saveSummary') }}</span>
            <div class="mt-3 grid gap-2">
              <div class="compact-metric">
                <span>{{ t('keyUsage.model') }}</span>
                <strong>{{ editAllowedModels.length }}</strong>
              </div>
              <div class="compact-metric">
                <span>{{ t('admin.accounts.quotaDashboard.schedulableAccounts') }}</span>
                <strong>{{ editingConfigListing ? roomEligibleAccountCount(editingConfigListing) : 0 }}/{{ editingConfigListing ? roomAttachedAccountCount(editingConfigListing) : 0 }}</strong>
              </div>
              <div class="compact-metric">
                <span>{{ t('accountShare.create.seatLimit') }}</span>
                <strong>{{ editForm.seat_limit }}</strong>
              </div>
              <div class="compact-metric">
                <span>{{ t('accountShare.join.perUserConcurrency') }}</span>
                <strong>{{ editForm.per_user_concurrency }}</strong>
              </div>
              <div class="compact-metric">
                <span>{{ t('accountShare.roomEdit.perUserCap') }}</span>
                <strong>{{ editMaxPerUserConcurrency }}</strong>
              </div>
              <div class="compact-metric">
                <span>{{ t('accountShare.membership.fieldHourlyRate') }}</span>
                <strong>{{ formatNumber(editForm.hourly_rate) }}</strong>
              </div>
              <div class="compact-metric">
                <span>{{ t('accountShare.membership.fieldFeeWaiver') }}</span>
                <strong>{{ hourlyFeeWaiverLabel(editForm.hourly_fee_waiver_minimum) }}</strong>
              </div>
            </div>
          </aside>
        </div>
      </div>

      <template #footer>
        <span
          v-if="configEditBlockedReason"
          class="mr-auto text-sm text-amber-700 dark:text-amber-300"
          data-testid="config-edit-blocked-reason"
        >{{ configEditBlockedReason }}</span>
        <button type="button" class="btn-secondary" :disabled="savingConfigEdit" @click="() => closeConfigEditDialog()">{{ t('common.cancel') }}</button>
        <button
          type="button"
          class="btn-primary"
          :disabled="savingConfigEdit || editVersionConflict || !editReason.trim() || editAllowedModels.length === 0 || Boolean(editPerUserConcurrencyValidationMessage)"
          @click="saveConfigEdit"
        >
          <Icon v-if="!savingConfigEdit" name="checkCircle" size="sm" class="mr-2" />
          <svg v-else class="-ml-1 mr-2 h-4 w-4 animate-spin" fill="none" viewBox="0 0 24 24">
            <circle class="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="4"></circle>
            <path class="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"></path>
          </svg>
          {{ t('admin.dataManagement.actions.saveConfig') }}
        </button>
      </template>
    </BaseDialog>

<RoomDetailsDrawer
      :listing="detailListing"
      :title="detailListing ? (isUnknownHistorySnapshot(detailListing) ? t('accountShare.common.roomIdHash', { id: detailListing.id }) : listingDisplayName(detailListing)) : t('accountShare.detail.title')"
      :loading="detailLoading"
      :error="detailError"
      @close="closeRoomDetails"
      @refresh="refreshRoomDetails"
    >
      <template #summary="{ listing }">
        <div class="room-detail-scope room-detail-identity">
          <div class="room-detail-identity-main">
            <span class="listing-platform-icon"><PlatformIcon :platform="listingPlatform(listing)" size="lg" /></span>
            <p v-if="isUnknownHistorySnapshot(listing)" class="room-detail-muted">{{ t('accountShare.detail.idIncomplete', { id: listing.id }) }}</p>
            <div v-else>
              <div class="listing-badge-row"><span class="feature-badge">{{ platformLabel(listingPlatform(listing)) }}</span><span v-if="isAPIAggregationListing(listing)" class="apikey-badge">APIKEY</span><span v-if="isOpenAIListing(listing)" :class="accountLevelBadgeClass(listing)">{{ accountLevelBadgeLabel(listing) }}</span><span :class="listingStatusBadgeClass(listing)">{{ detailIsArchive ? t('ideas.status.deleted') : listingStatusLabel(listing) }}</span></div>
              <p v-if="isAPIAggregationListing(listing)" class="room-detail-unverified-note">{{ t('accountShare.detail.unverifiedNote') }}</p>
              <p class="room-detail-owner" :title="t('accountShare.detail.idOwner', { id: listing.id, owner: ownerDisplayName(listing) })">{{ t('accountShare.detail.idOwner', { id: listing.id, owner: ownerDisplayName(listing) }) }}</p>
            </div>
          </div>
          <button v-if="!detailIsArchive" type="button" class="room-detail-text-button" @click="isOwnListing(listing) ? openRoomAccountsDialog(listing) : openOwnerDialog(listing)">
            {{ isOwnListing(listing) ? t('accountShare.detail.manageAccounts') : t('accountShare.detail.viewOwner') }}<Icon name="arrowRight" size="sm" />
          </button>
        </div>
      </template>
      <template #overview="{ listing }">
        <div class="room-detail-scope room-detail-sections">
          <template v-if="isArchiveView">
            <div
              class="space-y-4"
              data-testid="archive-listing-card"
              :data-snapshot-quality="listing.history_snapshot_quality || 'unmarked'"
            >
              <div
                v-if="isUnknownHistorySnapshot(listing)"
                class="space-y-4 rounded-xl border border-amber-200 bg-amber-50/70 p-5 dark:border-amber-800 dark:bg-amber-950/20"
                data-testid="unknown-history-card"
              >
                <div class="flex flex-wrap items-center justify-between gap-3">
                  <h2 class="text-lg font-semibold text-gray-950 dark:text-white">{{ t('accountShare.detail.roomIdLabel', { id: listing.id }) }}</h2>
                  <span class="rounded-full bg-gray-200 px-3 py-1 text-xs font-medium text-gray-700 dark:bg-dark-700 dark:text-dark-200">
                    {{ t('ideas.status.deleted') }}
                  </span>
                </div>
                <div class="space-y-2 text-sm text-gray-600 dark:text-dark-200">
                  <p>{{ t('accountShare.detail.lastUsed', { time: listing.last_used_at ? formatDate(listing.last_used_at) : t('accountShare.detail.timeLost')  }) }}</p>
                  <p class="leading-6 text-amber-800 dark:text-amber-200">
                    {{ t('accountShare.detail.preMigration') }}
                  </p>
                </div>
              </div>

              <template v-else>
                <header class="flex flex-col gap-4 sm:flex-row sm:items-start sm:justify-between">
                  <div class="min-w-0">
                    <div class="flex flex-wrap items-center gap-2">
                      <span class="feature-badge">{{ platformLabel(listingPlatform(listing)) }}</span>
                      <span v-if="isAPIAggregationListing(listing)" class="apikey-badge">APIKEY</span>
                      <span v-if="isOpenAIListing(listing)" :class="accountLevelBadgeClass(listing)">
                        {{ accountLevelBadgeLabel(listing) }}
                      </span>
                      <span class="inline-flex min-h-7 items-center rounded-full bg-slate-200 px-3 text-xs font-semibold text-slate-700 dark:bg-dark-700 dark:text-dark-200">
                        {{ t('ideas.status.deleted') }}
                      </span>
                    </div>
                    <h2 class="mt-3 break-words text-lg font-semibold text-slate-950 dark:text-white">
                      {{ listing.room_name || t('accountShare.common.roomIdHash', { id: listing.id }) }}
                    </h2>
                    <p class="mt-1 break-words text-sm leading-6 text-slate-600 dark:text-dark-300">
                      {{ t('accountShare.detail.ownerRoom', { ownerUserId: listing.owner_username || t('accountShare.common.userId', { id: listing.owner_user_id }), id: listing.id }) }}
                    </p>
                  </div>
                  <span class="inline-flex min-h-11 flex-none items-center justify-center rounded-xl bg-slate-100 px-4 text-sm font-semibold text-slate-700 dark:bg-dark-800 dark:text-dark-200">
                    {{ t('accountShare.detail.readonlySnapshot') }}
                  </span>
                </header>

                <div
                  v-if="isBackfilledHistorySnapshot(listing)"
                  class="rounded-lg border border-amber-200 bg-amber-50 px-4 py-3 text-sm leading-6 text-amber-800 dark:border-amber-800 dark:bg-amber-950/20 dark:text-amber-200"
                  data-testid="backfilled-history-notice"
                >
                  {{ t('accountShare.detail.backfilledNote') }}
                </div>

                <div
                  class="grid gap-3 sm:grid-cols-2 lg:grid-cols-3"
                  data-testid="archive-terms-snapshot"
                >
                  <div class="rounded-xl bg-slate-50 p-3 dark:bg-dark-800">
                    <span class="text-xs font-medium text-slate-500 dark:text-dark-400">{{ t('accountShare.detail.histSeatLimit') }}</span>
                    <strong class="mt-1 block text-sm text-slate-950 dark:text-white">{{ t('accountShare.membership.seatLimitValue', { seatLimit: listing.seat_limit }) }}</strong>
                  </div>
                  <div class="rounded-xl bg-slate-50 p-3 dark:bg-dark-800">
                    <span class="text-xs font-medium text-slate-500 dark:text-dark-400">{{ t('accountShare.detail.histPerUser') }}</span>
                    <strong class="mt-1 block text-sm text-slate-950 dark:text-white">{{ listing.per_user_concurrency }}</strong>
                  </div>
                  <div class="rounded-xl bg-slate-50 p-3 dark:bg-dark-800">
                    <span class="text-xs font-medium text-slate-500 dark:text-dark-400">{{ t('accountShare.detail.histRate') }}</span>
                    <strong class="mt-1 block text-sm text-slate-950 dark:text-white">{{ formatNumber(listing.rate_multiplier) }}x</strong>
                  </div>
                  <div class="rounded-xl bg-slate-50 p-3 dark:bg-dark-800">
                    <span class="text-xs font-medium text-slate-500 dark:text-dark-400">{{ t('accountShare.detail.histHourly') }}</span>
                    <strong class="mt-1 block text-sm text-slate-950 dark:text-white">{{ formatNumber(listing.hourly_rate) }}</strong>
                  </div>
                  <div class="rounded-xl bg-slate-50 p-3 dark:bg-dark-800">
                    <span class="text-xs font-medium text-slate-500 dark:text-dark-400">{{ t('accountShare.detail.histWaiver') }}</span>
                    <strong class="mt-1 block text-sm text-slate-950 dark:text-white">{{ hourlyFeeWaiverLabel(listing.hourly_fee_waiver_minimum) }}</strong>
                  </div>
                  <div class="rounded-xl bg-slate-50 p-3 dark:bg-dark-800">
                    <span class="text-xs font-medium text-slate-500 dark:text-dark-400">{{ t('accountShare.detail.histMinBalance') }}</span>
                    <strong class="mt-1 block text-sm text-slate-950 dark:text-white">{{ formatNumber(listing.min_balance_required) }}</strong>
                  </div>
                </div>

                <div class="rounded-xl border border-slate-200 bg-slate-50/70 p-3 dark:border-dark-700 dark:bg-dark-800/70">
                  <span class="text-xs font-medium text-slate-500 dark:text-dark-400">{{ t('accountShare.detail.histModels') }}</span>
                  <div class="mt-2 flex flex-wrap gap-2">
                    <span
                      v-for="model in listing.allowed_models"
                      :key="model"
                      class="max-w-full break-all rounded-lg bg-white px-2.5 py-1 text-xs text-slate-700 ring-1 ring-slate-200 dark:bg-dark-900 dark:text-dark-200 dark:ring-dark-600"
                    >
                      {{ model }}
                    </span>
                    <span v-if="listing.allowed_models.length === 0" class="text-sm text-slate-500 dark:text-dark-400">
                      {{ t('accountShare.membership.notRecorded') }}
                    </span>
                  </div>
                </div>

                <div class="rounded-xl border border-slate-200 bg-white px-4 py-3 text-sm leading-6 text-slate-600 dark:border-dark-700 dark:bg-dark-900 dark:text-dark-300">
                  {{ deletedHistorySnapshotMessage(listing) }}
                </div>
              </template>
            </div>
          </template>
          <template v-else-if="isUnknownHistorySnapshot(listing)">
            <div
              class="space-y-4 rounded-xl border border-amber-200 bg-amber-50/70 p-5 dark:border-amber-800 dark:bg-amber-950/20"
              data-testid="unknown-history-card"
            >
              <div class="flex flex-wrap items-center justify-between gap-3">
                <h2 class="text-lg font-semibold text-gray-950 dark:text-white">{{ t('accountShare.detail.roomIdLabel', { id: listing.id }) }}</h2>
                <span class="rounded-full bg-gray-200 px-3 py-1 text-xs font-medium text-gray-700 dark:bg-dark-700 dark:text-dark-200">
                  {{ listing.deleted ? t('ideas.status.deleted') : t('accountShare.membership.statusHistory') }}
                </span>
              </div>
              <div class="space-y-2 text-sm text-gray-600 dark:text-dark-200">
                <p>{{ t('accountShare.detail.lastUsed', { time: listing.last_used_at ? formatDate(listing.last_used_at) : t('accountShare.detail.timeLost')  }) }}</p>
                <p class="leading-6 text-amber-800 dark:text-amber-200">
                  {{ t('accountShare.detail.preMigration') }}
                </p>
              </div>
            </div>
          </template>
          <template v-else>
            <section class="room-detail-section">
              <div class="room-detail-section-heading"><h3>{{ t('accountShare.detail.pricingTitle') }}</h3><span>{{ t('accountShare.detail.publicPricing') }}</span></div>
              <div class="listing-price-grid">
                <div class="listing-price-primary">
                  <span>{{ t('accountShare.card.requestRate') }}</span>
                  <strong>{{ formatNumber(listing.rate_multiplier) }}<small>×</small></strong>
                  <small>{{ t('accountShare.detail.modelPricing') }}</small>
                </div>
                <div>
                  <span>{{ t('accountShare.detail.holdingPerHour') }}</span>
                  <strong>{{ formatNumber(listing.hourly_rate) }}</strong>
                  <small>{{ t('accountShare.detail.perActiveMinute') }}</small>
                </div>
                <div>
                  <span :title="t('accountShare.detail.hourlyWaiver')">{{ t('accountShare.detail.waiverShort') }}</span>
                  <strong>{{ hourlyFeeWaiverLabel(listing.hourly_fee_waiver_minimum) }}</strong>
                  <small>{{ t('accountShare.detail.waiverRefund') }}</small>
                </div>
              </div>
              <p v-if="isOwnListing(listing)" class="room-detail-callout">{{ t('accountShare.detail.yourRoom', { rate: ownerSelfUseRateMultiplierLabel }) }}</p>
              <div class="room-detail-facts">
                <div><span>{{ t('accountShare.create.minBalance') }}</span><strong>{{ formatNumber(listing.min_balance_required) }}</strong></div>
                <div><span>{{ t('accountShare.create.perUserConcurrency') }}</span><strong>{{ listing.per_user_concurrency }}</strong></div>
                <div><span>{{ t('accountShare.detail.memberSeats') }}</span><strong>{{ listing.active_seats }} / {{ listing.seat_limit }}</strong></div>
                <div><span>{{ t('accountShare.detail.joinMode') }}</span><strong>{{ listing.has_password ? t('accountShare.detail.passwordNeeded') : t('accountShare.detail.openJoin') }}</strong></div>
                <div><span>{{ t('accountShare.detail.roomRating') }}</span><strong>{{ listingRatingLabel(listing) }}</strong></div>
              </div>
            </section>
            <section class="room-detail-section">
              <div class="room-detail-section-heading"><h3>{{ t('accountShare.detail.runtimeTitle') }}</h3><span>{{ t('accountShare.detail.aggregateInfo') }}</span></div>
              <div class="listing-health-panel">
                <div class="listing-health-grid">
                  <div class="listing-status-stack">
                    <div class="listing-runtime-tile listing-runtime-summary">
                      <Icon name="database" size="sm" />
                      <div class="listing-runtime-summary-content">
                        <strong v-if="!listing.deleted && listing.status === 'active'">{{ roomAggregateAccountCountLabel(listing) }}</strong>
                        <span :class="runtimeInsightClass(roomAggregateInsight(listing).tone)">
                          {{ roomAggregateInsight(listing).badge }}
                        </span>
                      </div>
                    </div>
                    <div class="listing-runtime-tile listing-runtime-summary">
                      <Icon name="chart" size="sm" />
                      <div class="listing-runtime-summary-content">
                        <span>{{ listing.deleted || listing.status !== 'active' ? t('accountShare.detail.concurrencyStatus') : t('accountShare.detail.availableConcurrency') }}</span>
                        <strong>{{ listing.deleted || listing.status !== 'active' ? `当前${roomAvailableConcurrencyLabel(listing)}` : roomAvailableConcurrencyLabel(listing) }}</strong>
                      </div>
                    </div>
                  </div>

                  <div
                    v-if="listing.quota_summary"
                    class="listing-combined-availability"
                    data-testid="room-quota-summary"
                  >
                    <div class="availability-progress-row">
                      <div class="combined-availability-head">
                        <span>{{ t('accountShare.detail.used5h') }}</span>
                        <strong>{{ roomWindowUtilizationLabel(listing.quota_summary.window_5h) }}</strong>
                      </div>
                      <div
                        v-if="roomWindowUtilization(listing.quota_summary.window_5h) !== null"
                        class="combined-availability-track"
                        role="progressbar"
                        :aria-label="t('accountShare.detail.room5h')"
                        aria-valuemin="0"
                        aria-valuemax="100"
                        :aria-valuenow="roomWindowUtilization(listing.quota_summary.window_5h) ?? undefined"
                      >
                        <span
                          :class="roomWindowUtilizationBarClass(listing.quota_summary.window_5h)"
                          :style="{ width: `${roomWindowUtilization(listing.quota_summary.window_5h) ?? 0}%` }"
                        ></span>
                      </div>
                    </div>

                    <div class="availability-progress-row">
                      <div class="combined-availability-head">
                        <span>{{ t('accountShare.detail.used7d') }}</span>
                        <strong>{{ roomWindowUtilizationLabel(listing.quota_summary.window_7d) }}</strong>
                      </div>
                      <div
                        v-if="roomWindowUtilization(listing.quota_summary.window_7d) !== null"
                        class="combined-availability-track"
                        role="progressbar"
                        :aria-label="t('accountShare.detail.room7d')"
                        aria-valuemin="0"
                        aria-valuemax="100"
                        :aria-valuenow="roomWindowUtilization(listing.quota_summary.window_7d) ?? undefined"
                      >
                        <span
                          :class="roomWindowUtilizationBarClass(listing.quota_summary.window_7d)"
                          :style="{ width: `${roomWindowUtilization(listing.quota_summary.window_7d) ?? 0}%` }"
                        ></span>
                      </div>
                    </div>
                  </div>
                </div>

                <div v-if="listing.account_count === 1 && validityInfo(listing)" class="validity-strip">
                  <div class="flex min-w-0 items-center gap-2">
                    <Icon name="calendar" size="sm" />
                    <span>{{ validityInfo(listing)?.label }}</span>
                  </div>
                  <strong>{{ validityInfo(listing)?.expiresAtLabel }}</strong>
                </div>
              </div>
              <p class="room-detail-muted">{{ t('accountShare.detail.seatsVsConcurrency') }}</p>
            </section>
            <section class="room-detail-section">
              <div class="room-detail-section-heading"><h3>{{ t('accountShare.detail.billingExitTitle') }}</h3></div>
              <ol class="room-detail-rules">
                <li><strong>{{ t('accountShare.mySpend.requestCost') }}</strong><p>{{ t('accountShare.detail.billingRule') }}</p></li>
                <li><strong>{{ t('accountShare.detail.holdWaiverTitle') }}</strong><p>{{ t('accountShare.detail.holdWaiverRule') }}</p></li>
                <li><strong>{{ t('accountShare.detail.keyExitTitle') }}</strong><p>{{ t('accountShare.detail.keyExitRule') }}</p></li>
              </ol>
              <div class="room-detail-facts room-detail-dates"><div><span>{{ t('userAccounts.created') }}</span><strong>{{ formatDate(listing.created_at) }}</strong></div><div><span>{{ t('accountShare.detail.lastUpdated') }}</span><strong>{{ formatDate(listing.updated_at) }}</strong></div></div>
            </section>
          </template>
        </div>
      </template>
      <template #models="{ listing }">
        <div class="room-detail-scope room-detail-sections">
          <p v-if="isUnknownHistorySnapshot(listing)" class="room-detail-callout">{{ t('accountShare.detail.histLimitsLost') }}</p>
          <section v-else class="room-detail-section">
            <div class="room-detail-section-heading"><h3>{{ detailIsArchive ? t('accountShare.detail.histModels') : t('accountShare.detail.usableModels') }}</h3><span>{{ t('accountShare.detail.modelsCount', { length: listing.allowed_models.length }) }}</span></div>
            <div class="room-detail-model-list"><button v-for="model in listing.allowed_models" :key="model" type="button" @click="copyModelName(model)"><span>{{ model }}</span><Icon name="copy" size="sm" /></button></div>
            <p v-if="listing.allowed_models.length === 0" class="room-detail-muted">{{ detailIsArchive ? t('accountShare.detail.modelsLost') : t('accountShare.detail.noUsableModels') }}</p>
          </section>
          <section v-if="!isUnknownHistorySnapshot(listing)" class="room-detail-section">
            <div class="room-detail-section-heading"><h3>{{ t('accountShare.detail.limitsTitle') }}</h3></div>
            <div class="room-detail-facts">
              <div><span>{{ t('accountShare.membership.fieldSeatLimit') }}</span><strong>{{ t('accountShare.membership.seatLimitValue', { seatLimit: listing.seat_limit }) }}</strong></div>
              <div><span>{{ t('accountShare.join.perUserConcurrency') }}</span><strong>{{ listing.per_user_concurrency }}</strong></div>
              <div v-if="!detailIsArchive"><span>{{ t('accountShare.detail.totalConcurrency') }}</span><strong>{{ listing.account_concurrency }}</strong></div>
              <div v-if="isOpenAIListing(listing)"><span>{{ t('accountShare.detail.clientReq') }}</span><strong>{{ listing.codex_cli_only ? t('accountShare.detail.codexOnly') : t('accountShare.detail.notCodexOnly') }}</strong></div>
              <template v-if="isOpenAIListing(listing)"><div><span>{{ t('accountShare.detail.codex5hThreshold') }}</span><strong>{{ listing.codex_5h_limit_percent }}%</strong></div><div><span>{{ t('accountShare.detail.codex7dThreshold') }}</span><strong>{{ listing.codex_7d_limit_percent }}%</strong></div></template>
              <template v-else-if="listingPlatform(listing) === 'anthropic'"><div><span>{{ t('accountShare.detail.claude5hThreshold') }}</span><strong>{{ anthropic5hLimitPercent(listing) }}%</strong></div><div><span>{{ t('accountShare.detail.claude7dThreshold') }}</span><strong>{{ anthropic7dLimitPercent(listing) }}%</strong></div></template>
              <div v-else-if="!detailIsArchive && listing.account_count === 1"><span>{{ t('accountShare.detail.opencodeUsage') }}</span><strong>{{ opencodeUsageLabel(listing) }}</strong></div>
            </div>
            <p class="room-detail-muted">{{ t('accountShare.detail.limitsHint') }}</p>
          </section>
        </div>
      </template>
      <template #reviews="{ listing }">
        <p v-if="detailIsArchive" class="room-detail-scope room-detail-muted">{{ t('accountShare.detail.snapshotNoReviews') }}</p>
        <RoomReviewsPanel v-else :listing-id="listing.id" :rating-average="listing.rating_avg" :rating-count="listing.rating_count" />
      </template>
      <template #usage="{ listing }">
        <div class="room-detail-scope room-detail-sections">
          <p v-if="detailIsArchive" class="room-detail-callout">{{ t('accountShare.detail.deletedReadonly') }}</p>
          <template v-else>
            <div
              v-if="listingMembershipID(listing) > 0"
              class="account-share-membership-panel"
              :class="{ 'account-share-membership-panel-ending': isListingMembershipEnding(listing) }"
            >
              <div class="membership-status-head">
                <div>
                  <div class="membership-title">
                    {{ t('accountShare.join.termsIntro', { room: membershipPanelTitle(listing), key: boundApiKeyDisplayName(listing) }) }}
                  </div>
                  <div v-if="!isListingMembershipEnding(listing)" class="membership-subtitle">
                    {{ membershipPanelSubtitle(listing) }}
                    <span v-if="boundApiKeyID(listing)"> · ID #{{ boundApiKeyID(listing) }}</span>
                  </div>
                </div>
                <span v-if="!isListingMembershipEnding(listing)" class="membership-status-pill">{{ t('accountShare.mySpend.tabUsing') }}</span>
              </div>
              <div class="membership-compact-body">
                <div class="membership-main">
                  <div class="membership-detail-grid">
                    <div v-if="listing.current_joined_at">
                      <span>{{ t('accountShare.progress.activeTime') }}</span>
                      <strong>{{ formatDate(listing.current_joined_at) }}</strong>
                    </div>
                    <div v-if="!isListingMembershipEnding(listing) && waiverProgressVisible(listing)">
                      <span>{{ t('accountShare.progress.windowRemaining') }}</span>
                      <strong>{{ waiverProgressRemainingLabel(listing) }}</strong>
                    </div>
                    <div v-else-if="!isListingMembershipEnding(listing) && listing.current_paid_until">
                      <span>{{ t('accountShare.progress.nextPrepay') }}</span>
                      <strong>{{ formatCountdownUntil(listing.current_paid_until) }}</strong>
                    </div>
                    <div v-if="listing.current_last_request_at || listing.current_waiver_progress?.last_request_at">
                      <span>{{ t('accountShare.progress.lastRequest') }}</span>
                      <strong>{{ formatDate(listing.current_waiver_progress?.last_request_at || listing.current_last_request_at) }}</strong>
                    </div>
                    <div v-if="listing.current_billed_until && (isListingMembershipEnding(listing) || !waiverProgressVisible(listing))">
                      <span>{{ t('accountShare.progress.settledTo') }}</span>
                      <strong>{{ formatDate(listing.current_billed_until) }}</strong>
                    </div>
                  </div>

                  <div
                    v-if="!isListingMembershipEnding(listing) && waiverProgressVisible(listing)"
                    class="waiver-progress-card"
                    :class="waiverProgressToneClass(listing)"
                  >
                    <div class="waiver-progress-top">
                      <div>
                        <span>{{ t('accountShare.progress.waiverProgress') }}</span>
                        <strong>{{ waiverProgressTitle(listing) }}</strong>
                      </div>
                      <span class="waiver-progress-badge">{{ waiverProgressStatusLabel(listing) }}</span>
                    </div>
                    <div class="waiver-progress-track" role="progressbar" :aria-valuenow="waiverProgressPercent(listing)" aria-valuemin="0" aria-valuemax="100">
                      <span :style="waiverProgressPercentStyle(listing)"></span>
                    </div>
                    <div class="waiver-progress-foot">
                      <span>{{ waiverProgressAmountLabel(listing) }}</span>
                      <span>{{ waiverProgressMetaLabel(listing) }}</span>
                    </div>
                  </div>
                </div>

                <div class="membership-controls">
                  <div
                    v-if="isListingMembershipEnding(listing)"
                    class="membership-ending-state"
                    role="status"
                    aria-live="polite"
                    data-testid="membership-ending-state"
                  >
                    <Icon
                      :name="pendingMembershipEndForListing(listing)?.operationStatus === 'failed' ? 'exclamationCircle' : 'refresh'"
                      size="sm"
                      :class="{ 'animate-spin': pendingMembershipEndForListing(listing)?.operationStatus !== 'failed' && pendingMembershipEndForListing(listing)?.operationStatus !== 'cancelled' }"
                    />
                    <div>
                      <span>{{ membershipPanelSubtitle(listing) }}</span>
                      <small class="mt-1 block text-xs" data-testid="membership-ending-observation">
                        {{ membershipEndObservation(listing) }}
                      </small>
                      <span v-if="pendingMembershipEndForListing(listing)?.operationQueryError" class="mt-1 text-red-600 dark:text-red-300" role="status">
                        {{ t('accountShare.progress.queryFailed', { error: pendingMembershipEndForListing(listing)?.operationQueryError  }) }}
                      </span>
                      <span v-if="pendingMembershipEndForListing(listing)?.bindingCheckError" class="mt-1 text-red-600 dark:text-red-300" role="status">
                        {{ t('accountShare.progress.bindingCheckFailed', { error: pendingMembershipEndForListing(listing)?.bindingCheckError  }) }}
                      </span>
                    </div>
                  </div>
                  <template v-else>
                    <div class="idle-timeout-control">
                      <label :for="`idle-timeout-current-${listing.id}`">{{ t('accountShare.membership.fieldIdleTimeout') }}</label>
                      <div class="idle-timeout-row">
                        <input
                          :id="`idle-timeout-current-${listing.id}`"
                          v-model.number="idleTimeoutByListing[listing.id]"
                          class="input min-h-11"
                          type="number"
                          min="1"
                          :max="ACCOUNT_SHARE_IDLE_TIMEOUT_MAX_MINUTES"
                          step="1"
                        />
                        <span>{{ t('common.minutes') }}</span>
                        <button
                          class="btn-secondary min-h-11"
                          type="button"
                          :disabled="savingIdleTimeoutId === listingMembershipID(listing)"
                          @click="saveIdleTimeout(listing)"
                        >
                          {{ t('common.save') }}
                        </button>
                      </div>
                    </div>
                    <div class="membership-action-row">
                      <button
                        class="membership-end-button"
                        type="button"
                        :disabled="endingId !== null || isListingMembershipEnding(listing)"
                        @click="handleEndUseClick(listing)"
                      >
                        {{ t('accountShare.endUse.title') }}
                      </button>
                    </div>
                    <div
                      class="idle-timeout-hint"
                      :title="t('accountShare.idle.hint')"
                    >
                      {{ t('accountShare.idle.autoExitDesc') }}
                    </div>
                  </template>
                </div>
              </div>
            </div>
            <section v-if="listingMembershipID(listing) <= 0" class="room-detail-section"><h3>{{ t('accountShare.join.startUsing') }}</h3><p class="room-detail-muted">{{ t('accountShare.join.startUsingDesc') }}</p></section>
            <template v-if="isManagementView || isOwnListing(listing) || authStore.isAdmin">
              <div class="mt-3 rounded-lg border border-gray-200 bg-gray-50 p-3 text-sm dark:border-dark-700 dark:bg-dark-800/60">
                <div class="flex flex-col gap-1 text-gray-600 dark:text-dark-200">
                  <span>{{ t('accountShare.detail.roomIdLabel', { id: listing.id }) }}</span>
                  <span>{{ t('accountShare.join.roomAccountsLabel', { count: roomAggregateAccountCountLabel(listing)  }) }}</span>
                  <span>{{ t('accountShare.join.updatedAt', { time: formatDate(listing.updated_at)  }) }}</span>
                </div>
                <div v-if="!listing.deleted" class="listing-management-actions">
                  <button
                    type="button"
                    class="btn-secondary listing-management-action"
                    @click="openRoomAccountsDialog(listing)"
                  >
                    <Icon name="database" size="xs" />
                    {{ t('accountShare.detail.viewAccounts') }}
                  </button>
                  <button
                    type="button"
                    class="btn-secondary listing-management-action"
                    :disabled="managedActionId === listing.id"

                    @click="requestOpenConfigEdit(listing)"
                  >
                    <Icon name="edit" size="xs" />
                    {{ t('accountShare.detail.editConfig') }}
                  </button>
                  <button
                    v-if="(isOwnListing(listing) || authStore.isAdmin) && capabilities?.lifecycle_enabled !== false"
                    type="button"
                    class="btn-secondary listing-management-action"
                    data-testid="room-lifecycle-entry"
                    @click="openRoomLifecycleDialog(listing)"
                  >
                    <Icon name="cog" size="xs" />
                    {{ listing.status === 'active' ? t('ideas.admin.takedown') : listing.status === 'paused' ? t('accountShare.detail.moreActions') : t('accountShare.detail.viewProgress') }}
                  </button>
                </div>
                <div
                  v-else
                  class="mt-3 rounded-lg border border-gray-200 bg-white px-3 py-2 text-sm text-gray-500 dark:border-dark-700 dark:bg-dark-900 dark:text-dark-300"
                >
                  {{ deletedHistorySnapshotMessage(listing) }}
                </div>
              </div>
            </template>
            <div class="room-detail-secondary-actions"><button type="button" class="btn-secondary min-h-11" @click="openMySpendDialog(listing)"><Icon name="dollar" size="sm" />{{ t('accountShare.page.mySpend') }}</button><button type="button" class="btn-secondary min-h-11" @click="openUsageGuideDialog"><Icon name="book" size="sm" />{{ t('affiliate.tips.title') }}</button></div>
          </template>
        </div>
      </template>
      <template #actions="{ listing, showUsage }">
        <div class="room-detail-scope">
          <div v-if="detailIsArchive || isUnknownHistorySnapshot(listing)" class="room-detail-footer-summary"><span>{{ t('accountShare.detail.readonlyHistory') }}</span><button type="button" class="btn-secondary min-h-11" @click="closeRoomDetails">{{ t('accountShare.detail.close') }}</button></div>
          <template v-else>
            <div v-if="canShowListingJoinSection(listing)" class="listing-join-section">
              <div v-if="listingJoinUnavailableReason(listing)" class="listing-unavailable-note">
                <Icon name="exclamationCircle" size="sm" />
                <span>{{ listingJoinUnavailableReason(listing) }}</span>
              </div>
              <div v-if="isListingMembershipEnding(listing)" class="listing-unavailable-note">
                <Icon name="refresh" size="sm" class="animate-spin" />
                <span>{{ t('accountShare.join.membershipEndingSuffix') }}</span>
              </div>
              <div v-if="isOwnListing(listing) && selfUseSettingsError" class="listing-unavailable-note">
                <Icon name="exclamationCircle" size="sm" />
                <span>{{ selfUseSettingsError }}</span>
              </div>
              <div class="listing-action-row">
                <div v-if="singleModeApiKeyForListing(listing)" class="mode-key-readonly">
                  <Icon name="key" size="sm" />
                  <span>{{ singleModeApiKeyLabelForListing(listing) }}</span>
                </div>
                <Select
                  v-else
                  v-model="selectedKeyByListing[listing.id]"
                  class="mode-key-select"
                  :options="modeApiKeySelectOptionsForListing(listing)"
                  :placeholder="modeApiKeyPlaceholderForListing(listing)"
                  :aria-label="modeApiKeyPlaceholderForListing(listing)"
                  :disabled="modeKeysLoading || !modeKeysLoaded"
                  empty-text="暂无可用 Key"
                >
                  <template #selected="{ option }">
                    <span class="mode-key-select-value">
                      <Icon name="key" size="xs" />
                      <span>{{ option?.label || modeApiKeyPlaceholderForListing(listing) }}</span>
                    </span>
                  </template>
                  <template #option="{ option, selected }">
                    <span class="mode-key-option-icon">
                      <Icon name="key" size="xs" />
                    </span>
                    <span class="mode-key-option-copy">
                      <strong>{{ option.label }}</strong>
                      <small>{{ t('accountShare.estimate.modeKey') }}</small>
                    </span>
                    <Icon v-if="selected" name="check" size="sm" class="text-primary-500" />
                  </template>
                </Select>
                <div class="listing-timeout-row">
                  <label class="idle-timeout-join idle-timeout-join-inline">
                    <span>{{ t('accountShare.membership.fieldIdleTimeout') }}</span>
                    <div class="idle-timeout-input-row">
                      <input
                        v-model.number="idleTimeoutByListing[listing.id]"
                        class="input h-9"
                        type="number"
                        min="1"
                        :max="ACCOUNT_SHARE_IDLE_TIMEOUT_MAX_MINUTES"
                        step="1"
                      />
                      <span class="idle-timeout-join-unit">{{ t('common.minutes') }}</span>
                    </div>
                  </label>
                  <div class="idle-timeout-inline-note" :title="isOwnListing(listing) ? t('accountShare.idle.hintUnbind') : t('accountShare.idle.hintExit')">
                    <Icon name="infoCircle" size="xs" />
                    <span>{{ isOwnListing(listing) ? t('accountShare.idle.autoUnbind') : t('accountShare.idle.autoExit') }}</span>
                  </div>
                </div>
                <button
                  class="btn-primary h-9"
                  type="button"
                  :disabled="Boolean(listingJoinUnavailableReason(listing)) || isListingMembershipEnding(listing) || modeKeysLoading || preparingJoinId !== null || joiningId !== null || selfUseJoinUnavailable(listing)"
                  :title="isListingMembershipEnding(listing) ? t('accountShare.join.settling') : (listingJoinUnavailableReason(listing) || (selfUseJoinUnavailable(listing) ? selfUseSettingsError : undefined))"
                  @click="joinUse(listing)"
                >
                  {{ isListingMembershipEnding(listing) ? t('accountShare.join.settling') : (listingJoinUnavailableReason(listing) ? listingJoinUnavailableReason(listing) : (preparingJoinId === listing.id ? t('accountShare.join.preparing') : (joiningId === listing.id ? (isOwnListing(listing) ? t('accountShare.join.binding') : t('accountShare.join.joining')) : (modeKeysLoading ? t('accountShare.join.loadingKeys') : (isOwnListing(listing) ? (selfUseSettingsLoading ? t('accountShare.join.loadingSelfUse') : (selfUseSettingsError ? t('accountShare.errors.selfUseUnavailable') : t('accountShare.join.useOwnAccount'))) : t('accountShare.join.joinUse')))))) }}
                </button>
              </div>
            </div>
            <div v-else class="room-detail-footer-summary"><span>{{ listingMembershipID(listing) > 0 ? membershipPanelTitle(listing) : listingStatusLabel(listing) }}</span><button type="button" class="btn-primary min-h-11" @click="showUsage">{{ t('accountShare.join.viewUsage') }}<Icon name="arrowRight" size="sm" /></button></div>
          </template>
        </div>
      </template>
    </RoomDetailsDrawer>

    <BaseDialog
      :show="pendingEndUse !== null"
      :title="t('accountShare.endUse.confirmTitle')"
      width="narrow"
      :close-on-escape="endingId === null"
      @close="cancelEndUse"
    >
      <p class="text-sm text-gray-600 dark:text-gray-400">{{ endUseConfirmMessage }}</p>

      <template #footer>
        <div class="flex justify-end space-x-3">
          <button type="button" class="btn btn-secondary" :disabled="endingId !== null" @click="cancelEndUse">
            {{ t('common.cancel') }}
          </button>
          <button type="button" class="btn btn-danger" :disabled="endingId !== null" @click="confirmEndUse">
            <svg v-if="endingId !== null" class="-ml-1 mr-2 h-4 w-4 animate-spin" fill="none" viewBox="0 0 24 24">
              <circle class="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="4"></circle>
              <path class="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"></path>
            </svg>
            {{ endingId !== null ? t('common.processing') : t('accountShare.endUse.title') }}
          </button>
        </div>
      </template>
    </BaseDialog>

    <BaseDialog
      :show="pendingReview !== null"
      :title="t('accountShare.review.title')"
      width="wide"
      :z-index="70"
      @close="closeReviewDialog"
    >
      <div v-if="pendingReview" class="space-y-5">
        <div class="rounded-lg border border-gray-200 bg-gray-50 p-4 dark:border-dark-700 dark:bg-dark-800/60">
          <div class="flex flex-col gap-1">
            <span class="text-xs font-semibold uppercase tracking-wide text-gray-500 dark:text-dark-300">{{ pendingReview.platformLabel }}</span>
            <strong class="text-base text-gray-900 dark:text-dark-50">{{ pendingReview.roomName }}</strong>
            <span class="text-sm text-gray-500 dark:text-dark-300">{{ t('accountShare.review.owner', { name: pendingReview.ownerName  }) }}</span>
          </div>
        </div>

        <div>
          <label class="mb-2 block text-sm font-medium text-gray-700 dark:text-dark-100">{{ t('accountShare.sortField.rating') }}</label>
          <div class="grid grid-cols-6 gap-2 sm:grid-cols-11">
            <button
              v-for="score in reviewScoreOptions"
              :key="score"
              type="button"
              class="rounded-lg border px-0 py-2 text-sm font-semibold transition-colors"
              :class="pendingReview.score === score ? 'border-primary-500 bg-primary-50 text-primary-700 dark:border-primary-400 dark:bg-primary-500/10 dark:text-primary-200' : 'border-gray-200 bg-white text-gray-700 hover:border-primary-300 dark:border-dark-700 dark:bg-dark-900 dark:text-dark-100'"
              @click="pendingReview.score = score"
            >
              {{ score }}
            </button>
          </div>
        </div>

        <label class="field">
          <span>{{ t('accountShare.review.comment') }}</span>
          <textarea
            v-model="pendingReview.comment"
            class="input min-h-[120px] resize-y"
            maxlength="1000"
            :disabled="commentReviewDisabled"
            :placeholder="commentReviewDisabled ? t('accountShare.review.moderationOff') : t('accountShare.review.moderationOn')"
          ></textarea>
          <small v-if="commentReviewDisabled" class="text-amber-600 dark:text-amber-400">{{ t('accountShare.review.commentDisabled') }}</small>
          <small v-else>{{ pendingReview.comment.length }}/1000</small>
        </label>

        <div v-if="pendingReview.error" class="rounded-lg border border-red-200 bg-red-50 p-3 text-sm text-red-700 dark:border-red-500/30 dark:bg-red-500/10 dark:text-red-200">
          {{ pendingReview.error }}
        </div>
      </div>

      <template #footer>
        <button type="button" class="btn-secondary" :disabled="pendingReview?.submitting" @click="closeReviewDialog">{{ t('accountShare.review.skip') }}</button>
        <button type="button" class="btn-primary" :disabled="pendingReview?.submitting || pendingReview?.score === null" @click="submitReview">
          <Icon v-if="!pendingReview?.submitting" name="checkCircle" size="sm" class="mr-2" />
          <svg v-else class="-ml-1 mr-2 h-4 w-4 animate-spin" fill="none" viewBox="0 0 24 24">
            <circle class="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="4"></circle>
            <path class="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"></path>
          </svg>
          {{ t('accountShare.review.submit') }}
        </button>
      </template>
    </BaseDialog>

    <BaseDialog
      :show="ownerDialog.show"
      :title="ownerDialog.ownerUsername ? t('accountShare.ownerDialog.title', { name: ownerDialog.ownerUsername }) : t('accountShare.ownerDialog.titleFallback')"
      width="extra-wide"
      :z-index="70"
      @close="closeOwnerDialog"
    >
      <div class="space-y-4">
        <div class="flex flex-wrap gap-2">
          <button
            type="button"
            class="btn-secondary h-9"
            :class="ownerDialog.tab === 'listings' && 'border-primary-300 bg-primary-50 text-primary-700 dark:border-primary-500/40 dark:bg-primary-500/10 dark:text-primary-200'"
            @click="ownerDialog.tab = 'listings'"
          >
            <Icon name="grid" size="xs" class="mr-2" />
            {{ t('dashboard.account') }}
          </button>
          <button
            type="button"
            class="btn-secondary h-9"
            :class="ownerDialog.tab === 'reviews' && 'border-primary-300 bg-primary-50 text-primary-700 dark:border-primary-500/40 dark:bg-primary-500/10 dark:text-primary-200'"
            @click="ownerDialog.tab = 'reviews'"
          >
            <Icon name="chat" size="xs" class="mr-2" />
            {{ t('accountShare.ownerDialog.reviews') }}
          </button>
        </div>

        <div v-if="ownerDialog.tab === 'listings'" class="space-y-3">
          <div
            v-if="ownerDialog.listingsError"
            class="flex flex-wrap items-center justify-between gap-3 rounded-lg border border-red-200 bg-red-50 p-3 text-sm text-red-700 dark:border-red-500/30 dark:bg-red-500/10 dark:text-red-200"
          >
            <span>{{ ownerDialog.listingsError }}</span>
            <button type="button" class="btn-secondary h-9" :disabled="ownerDialog.loadingListings" @click="loadOwnerListings()">
              {{ t('accountShare.ownerDialog.retryAccounts') }}
            </button>
          </div>
          <div v-if="ownerDialog.loadingListings" class="rounded-lg border border-gray-200 p-6 text-center text-sm text-gray-500 dark:border-dark-700 dark:text-dark-300">
            {{ t('accountShare.ownerDialog.loadingAccounts') }}
          </div>
          <div v-else-if="ownerDialog.listings.length === 0" class="rounded-lg border border-gray-200 p-6 text-center text-sm text-gray-500 dark:border-dark-700 dark:text-dark-300">
            {{ t('accountShare.ownerDialog.noAccounts') }}
          </div>
          <div v-else class="grid gap-3 md:grid-cols-2">
            <button
              v-for="item in ownerDialog.listings"
              :key="item.id"
              type="button"
              class="rounded-lg border border-gray-200 bg-white p-4 text-left transition-colors hover:border-primary-300 dark:border-dark-700 dark:bg-dark-900"
              @click="searchOwnerFromDialog"
            >
              <div class="flex items-start justify-between gap-3">
                <div class="min-w-0">
                  <strong class="block truncate text-sm text-gray-900 dark:text-dark-50">{{ listingDisplayName(item) }}</strong>
                  <span class="mt-1 block text-xs text-gray-500 dark:text-dark-300">{{ platformLabel(listingPlatform(item)) }} · {{ listingRatingLabel(item) }}</span>
                </div>
                <span :class="listingStatusBadgeClass(item)">{{ listingStatusLabel(item) }}</span>
              </div>
              <div class="mt-3 grid grid-cols-3 gap-2 text-xs text-gray-600 dark:text-dark-300">
                <span>{{ t('accountShare.roomEdit.seats', { activeSeats: item.active_seats, seatLimit: item.seat_limit }) }}</span>
                <span>{{ t('accountShare.ownerDialog.rate', { rate: formatNumber(item.rate_multiplier)  }) }}</span>
                <span>{{ t('accountShare.ownerDialog.hourly', { rate: formatNumber(item.hourly_rate)  }) }}</span>
              </div>
            </button>
            <div class="col-span-full flex flex-wrap items-center justify-between gap-3 text-xs text-gray-500 dark:text-dark-300">
              <span>{{ t('accountShare.ownerDialog.listingsShown', { length: ownerDialog.listings.length, listingsTotalExact: countLabel(ownerDialog.listingsTotal, ownerDialog.listingsTotalExact) }) }}</span>
              <button
                v-if="ownerDialog.listingsHasMore"
                type="button"
                class="btn-secondary h-9"
                :disabled="ownerDialog.loadingListings"
                @click="loadMoreOwnerListings"
              >
                {{ t('accountShare.ownerDialog.loadMore') }}
              </button>
            </div>
          </div>
        </div>

        <div v-else class="space-y-3">
          <div
            v-if="ownerDialog.reviewsError"
            class="flex flex-wrap items-center justify-between gap-3 rounded-lg border border-red-200 bg-red-50 p-3 text-sm text-red-700 dark:border-red-500/30 dark:bg-red-500/10 dark:text-red-200"
          >
            <span>{{ ownerDialog.reviewsError }}</span>
            <button type="button" class="btn-secondary h-9" :disabled="ownerDialog.loadingReviews" @click="loadOwnerReviews()">
              {{ t('accountShare.ownerDialog.retryReviews') }}
            </button>
          </div>
          <div v-if="ownerDialog.loadingReviews" class="rounded-lg border border-gray-200 p-6 text-center text-sm text-gray-500 dark:border-dark-700 dark:text-dark-300">
            {{ t('accountShare.ownerDialog.loadingReviews') }}
          </div>
          <div v-else-if="ownerDialog.reviews.length === 0" class="rounded-lg border border-gray-200 p-6 text-center text-sm text-gray-500 dark:border-dark-700 dark:text-dark-300">
            {{ t('accountShare.ownerDialog.noReviews') }}
          </div>
          <div v-else class="space-y-3">
            <article
              v-for="review in ownerDialog.reviews"
              :key="review.id"
              class="rounded-lg border border-gray-200 bg-white p-4 dark:border-dark-700 dark:bg-dark-900"
            >
              <div class="flex flex-wrap items-center justify-between gap-2">
                <strong class="text-sm text-gray-900 dark:text-dark-50">{{ formatRating(review.score) }}/10</strong>
                <span class="text-xs text-gray-500 dark:text-dark-300">{{ formatDate(review.created_at) }}</span>
              </div>
              <p class="mt-2 whitespace-pre-wrap text-sm leading-6 text-gray-700 dark:text-dark-100">{{ review.comment }}</p>
              <div class="mt-3 flex flex-wrap gap-2 text-xs text-gray-500 dark:text-dark-300">
                <span>{{ review.platform ? platformLabel(review.platform) : t('accountShare.ownerDialog.accountRoom') }}</span>
                <span>{{ t('accountShare.ownerDialog.anonymous') }}</span>
              </div>
            </article>
            <div class="flex flex-wrap items-center justify-between gap-3 text-xs text-gray-500 dark:text-dark-300">
              <span>{{ t('accountShare.ownerDialog.reviewsShown', { length: ownerDialog.reviews.length, reviewsTotal: ownerDialog.reviewsTotal }) }}</span>
              <button
                v-if="ownerDialog.reviewsPage < ownerDialog.reviewsPages"
                type="button"
                class="btn-secondary h-9"
                :disabled="ownerDialog.loadingReviews"
                @click="loadMoreOwnerReviews"
              >
                {{ t('accountShare.reviews.loadMore') }}
              </button>
            </div>
          </div>
        </div>
      </div>

      <template #footer>
        <button type="button" class="btn-primary" @click="searchOwnerFromDialog">
          <Icon name="search" size="sm" class="mr-2" />
          {{ t('accountShare.ownerDialog.searchOwner') }}
        </button>
        <button type="button" class="btn-secondary" @click="closeOwnerDialog">{{ t('common.close') }}</button>
      </template>
    </BaseDialog>

    <RoomLifecycleDialog
      v-model:listing="roomLifecycleListing"
      :display-name="roomLifecycleListing ? listingDisplayName(roomLifecycleListing) : ''"
      :now-ms="nowMs"
      :reload-listings="loadListings"
      :reload-capabilities="loadCapabilities"
      :remove-known-listing="removeKnownListing"
      :find-listing="findListingById"
      @sync-now="nowMs = Date.now()"
    />

    <BaseDialog
      :show="pendingForceEditListing !== null"
      :title="t('accountShare.forceEdit.title')"
      width="narrow"
      @close="cancelForceEdit"
    >
      <div class="space-y-4">
        <div class="notice-row border-amber-200 bg-amber-50 text-amber-800 dark:border-amber-900/60 dark:bg-amber-900/20 dark:text-amber-200">
          <Icon name="exclamationTriangle" size="sm" class="mt-0.5 flex-shrink-0" />
          <span>{{ forceEditConfirmMessage }}</span>
        </div>
        <label class="field">
          <span>{{ t('accountShare.forceEdit.reason') }}</span>
          <textarea
            v-model="forceEditReason"
            class="input min-h-24"
            maxlength="500"
            :placeholder="t('accountShare.forceEdit.reasonPlaceholder')"
            data-testid="force-edit-reason"
          ></textarea>
          <small>{{ t('accountShare.forceEdit.reasonHint') }}</small>
        </label>
        <label class="toggle-row">
          <input v-model="forceEditConfirmed" type="checkbox" data-testid="force-edit-confirmed" />
          <span>
            <strong>{{ t('accountShare.forceEdit.confirmCheck') }}</strong>
            <small>{{ t('accountShare.forceEdit.confirmHint') }}</small>
          </span>
        </label>
      </div>

      <template #footer>
        <button type="button" class="btn-secondary min-h-11" @click="cancelForceEdit">{{ t('common.cancel') }}</button>
        <button
          type="button"
          class="btn-danger min-h-11"
          :disabled="!forceEditReason.trim() || !forceEditConfirmed"
          data-testid="confirm-force-edit"
          @click="confirmForceEdit"
        >
          {{ t('accountShare.forceEdit.proceed') }}
        </button>
      </template>
    </BaseDialog>

    <ConfirmDialog
      :show="pendingDraftDiscardTarget !== null"
      :title="t('accountShare.common.discardTitle')"
      :message="draftDiscardMessage"
      confirm-text="放弃修改"
      cancel-text="继续编辑"
      danger
      @confirm="confirmDiscardDraft"
      @cancel="cancelDiscardDraft"
    />

    <AccountShareQuotaAdminDialog
      :show="showAdminQuotaDialog"
      @close="closeAdminQuotaDialog"
      @updated="loadCapabilities"
    />
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, defineAsyncComponent, nextTick, onBeforeUnmount, onMounted, reactive, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useMediaQuery } from '@vueuse/core'
import { useRoute, useRouter } from 'vue-router'
import {
  accountShareAPI,
  loadAllPaginatedItems,
  type AccountShareAPIKeyBindingStatus,
  type AccountShareCapabilities,
  type AccountShareJoinIntent,
  type AccountShareListing,
  type AccountShareListingFeatureTag,
  type AccountShareListingFilterStatus,
  type AccountShareListingFilters,
  type AccountShareListingSortBy,
  type AccountShareListingSortKey,
  type AccountShareListingSortOrder,
  type AccountShareListingStatus,
  type AccountShareListingTab,
  type AccountShareMembership,
  type AccountShareMembershipHistoryEntry,
  type AccountShareMySpendRange,
  type AccountShareMySpendSummary,
  type AccountSharePlatform,
  type AccountShareRecommendationCandidate,
  type AccountShareRecommendationRequest,
  type AccountShareRecommendationResult,
  type AccountShareRecommendationUsageProfile,
  type AccountShareReview,
  type AccountShareRoomManagementState,
  type AccountShareRoomOperation,
  type AccountShareRoomQuotaWindow,
  type CreateAccountShareRoomRequest,
  type UpdateAccountShareListingRequest
} from '@/api/accountShare'
import { accountsAPI, keysAPI } from '@/api'
import type { Account, AccountLevel, ApiKey, Proxy } from '@/types'
import { useAppStore } from '@/stores/app'
import { useAuthStore } from '@/stores/auth'
import { useClipboard } from '@/composables/useClipboard'
import { extractApiErrorCode, extractApiErrorMessage, extractApiErrorMetadata } from '@/utils/apiError'
import { accountShareOperationWaitReason, accountShareOperationWaitDuration } from '@/utils/accountShareOperation'
import {
  createSecureRequestID,
  isCanceledRequest,
  normalizeDateInput
} from '@/utils/requestSafety'
import { normalizeTablePageSize } from '@/utils/tablePreferences'
import {
  normalizeOpenAIAccountLevelConfigs,
  normalizeOpenAIAccountLevelKey,
  openAIAccountLevelLabel,
  openAIAccountLevelOptions
} from '@/utils/openaiAccountLevels'
import AppLayout from '@/components/layout/AppLayout.vue'
import BaseDialog from '@/components/common/BaseDialog.vue'
import ConfirmDialog from '@/components/common/ConfirmDialog.vue'
import Select, { type SelectOption } from '@/components/common/Select.vue'
import Icon from '@/components/icons/Icon.vue'
import PlatformIcon from '@/components/common/PlatformIcon.vue'
import ModelWhitelistSelector from '@/components/account/ModelWhitelistSelector.vue'
import CreateAccountModal from '@/components/account/CreateAccountModal.vue'
import Pagination from '@/components/common/Pagination.vue'
import LowerBoundPagination from '@/components/common/LowerBoundPagination.vue'
import CreateRoomDialog from '@/components/account-share/CreateRoomDialog.vue'
import RoomAccountsDialog from '@/components/account-share/RoomAccountsDialog.vue'
import AccountShareQuotaAdminDialog from '@/components/account-share/AccountShareQuotaAdminDialog.vue'
import MembershipHistoryPanel from '@/components/account-share/MembershipHistoryPanel.vue'
import RoomDetailsDrawer from '@/components/account-share/RoomDetailsDrawer.vue'
import RoomReviewsPanel from '@/components/account-share/RoomReviewsPanel.vue'
import { useRoomGridCapacity } from '@/components/account-share/useRoomGridCapacity'
// 房间生命周期弹窗按需加载：未打开时其模板、状态机与样式都不进入本页首屏产物。
const RoomLifecycleDialog = defineAsyncComponent(
  () => import('@/components/account-share/RoomLifecycleDialog.vue')
)
import {
  ROOM_LIFECYCLE_ERROR_MESSAGES,
  ROOM_LIFECYCLE_TERMINAL_OPERATION_STATUSES
} from '@/components/account-share/roomLifecycleConstants'
import { resolveAccountExternalPlacementTarget } from '@/components/account-share/externalPlacement'

interface FilterOption {
  key: string
  label: string
  tab: AccountShareListingTab
}

type ListingStatusFilterValue = AccountShareListingFilterStatus | 'available'
type AccountLevelFilterValue = AccountLevel | 'all' | ''
type ListingSortKey = AccountShareListingSortKey
type AccountShareListingWithClientMeta = AccountShareListing & {
  waiver_progress_received_at_ms?: number
}

interface WaiverProgressSnapshot {
  status: 'in_progress' | 'met' | string
  requiredAmount: number
  usageAmount: number
  remainingAmount: number
  progressPercent: number
  estimatedHourlyFeeRefund: number
  requestCount: number
  remainingSeconds: number
}

interface ListingSortOption {
  key: ListingSortKey
  label: string
  shortLabel?: string
  sortBy?: AccountShareListingSortBy
  sortOrder?: AccountShareListingSortOrder
}

interface ListingSortFieldOption {
  sortBy: AccountShareListingSortBy
  label: string
  ascLabel: string
  descLabel: string
}

interface ListingFeatureTagOption {
  value: AccountShareListingFeatureTag
  label: string
}

interface ListingFilterState {
  status: ListingStatusFilterValue
  accountLevel: AccountLevelFilterValue
  sortKeys: ListingSortKey[]
  seatLimits: number[]
  featureTags: AccountShareListingFeatureTag[]
  models: string[]
}

interface ListingPreferenceState extends ListingFilterState {
  platform: AccountSharePlatform
  tab: AccountShareListingTab
  search: string
  pageSize: number
}

type ListingFilterPopover = 'status' | 'level' | 'seat' | 'feature' | 'model'

interface ActiveFilterChip {
  key: string
  label: string
  remove: () => void
}

interface CreateFormState {
  name: string
  proxy_id: number | null
  concurrency: number
  seat_limit: number
  rate_multiplier: number
  per_user_concurrency: number
  hourly_rate: number
  hourly_fee_waiver_minimum: number
  min_balance_required: number
  codex_cli_only: boolean
  codex_5h_limit_percent: number
  codex_7d_limit_percent: number
  anthropic_5h_limit_percent: number
  anthropic_7d_limit_percent: number
  join_password: string
}

type DraftDiscardTarget = 'create' | 'config'

interface CreateDraftSnapshot {
  platform: AccountSharePlatform
  selectedOwnedAccountID: number
  form: CreateFormState
  allowedModels: string[]
}

interface ConfigDraftSnapshot {
  form: CreateFormState
  allowedModels: string[]
  reason: string
}

type AccountShareActionErrorAction = 'create-mode-key' | null
type RecommendationPresetKey = 'light' | 'balanced' | 'heavy' | 'history'
type MySpendMetricTone = 'total' | 'request' | 'hourly' | 'usage'
type MySpendMetricIcon = 'dollar' | 'creditCard' | 'clock' | 'chart'
type MySpendAccountOptionSource = 'using' | 'history'

interface RecommendationPreset {
  key: RecommendationPresetKey
  label: string
  request_count: number
  active_hours: number
  input_tokens_per_request: number
  output_tokens_per_request: number
  cache_creation_tokens_per_request: number
  cache_read_tokens_per_request: number
  image_input_tokens_per_request: number
  image_output_tokens_per_request: number
  image_cache_read_tokens_per_request: number
}

interface RecommendationFormState {
  api_key_id: number
  model: string
  request_count: number
  active_hours: number
  input_tokens_per_request: number
  output_tokens_per_request: number
  cache_creation_tokens_per_request: number
  cache_read_tokens_per_request: number
  image_input_tokens_per_request: number
  image_output_tokens_per_request: number
  image_cache_read_tokens_per_request: number
}

interface PendingJoinConfirmation {
  listingID: number
  ownerSelfUse: boolean
  platform: AccountSharePlatform
  apiKeyID: number
  apiKeyLabel: string
  idleTimeoutMinutes: number
  intent: AccountShareJoinIntent
}

interface JoinPasswordPromptState {
  listing: AccountShareListing
  platform: AccountSharePlatform
  apiKeyID: number
  apiKeyLabel: string
  idleTimeoutMinutes: number
}

interface PendingEndUseState {
  membershipID: number
  apiKeyID?: number
  apiKeyName?: string
  status?: string
  listing: AccountShareListing
}

interface PendingMembershipEnd {
  listingID: number
  membershipID: number
  operationID: string
  operationStatus: string
  operationError: string
  operationBlocker?: Record<string, unknown>
  operationCreatedAt?: string
  operationUpdatedAt?: string
  lastOperationAttemptAt?: number
  lastOperationSuccessAt?: number
  operationQueryError?: string
  operationQueryFailures?: number
  lastBindingCheckAt?: number
  bindingCheckError?: string
  apiKeyID?: number
  apiKeyName?: string
  membership: AccountShareMembership
  listingSnapshot: AccountShareListing
}

interface ReviewDialogState {
  membershipID: number
  platformLabel: string
  roomName: string
  ownerName: string
  score: number | null
  comment: string
  submitting: boolean
  error: string
}

interface StableIdempotencyIntent {
  signature: string
  key: string
}

interface MySpendRangeOption {
  value: AccountShareMySpendRange
  label: string
}

interface MySpendMetric {
  key: string
  label: string
  value: string
  note: string
  icon: MySpendMetricIcon
  tone: MySpendMetricTone
}

interface MySpendAccountOption {
  key: string
  source: MySpendAccountOptionSource
  listingID: number
  membershipID: number
  platform: string
  roomName: string
  accountName?: string
  ownerUserID: number
  ownerUsername?: string
  status: string
  joinedAt?: string
  lastRequestAt?: string
  endedAt?: string
  roomDeleted?: boolean
  listing?: AccountShareListing
}

interface MySpendAccountOptionPage {
  options: MySpendAccountOption[]
  page: number
  pageSize: number
  total: number
  pages: number
  totalExact: boolean
  hasMore: boolean
}

type MySpendAccountOptionPagination = Omit<MySpendAccountOptionPage, 'options'>

type OwnerDialogTab = 'listings' | 'reviews'


const DEFAULT_ACCOUNT_CONCURRENCY = 20
const DEFAULT_PER_USER_CONCURRENCY = 5
const DEFAULT_HOURLY_RATE = 0.2
const DEFAULT_ACCOUNT_SHARE_IDLE_TIMEOUT_MINUTES = 10
const MY_SPEND_ACCOUNT_PAGE_SIZE = 12
const EXPENSIVE_HOURLY_RATE = 2
const MAX_ACCOUNT_CONCURRENCY = 50
const MAX_PER_USER_CONCURRENCY = 50
const ACCOUNT_SHARE_MIN_SEATS = 1
const ACCOUNT_SHARE_MAX_SEATS = 30
const ACCOUNT_SHARE_TRANSIENT_STATUS_REFRESH_INTERVAL_MS = 8_000
const ACCOUNT_SHARE_PLATFORM_OPTIONS = computed<Array<{ value: AccountSharePlatform; label: string }>>(() => [
  { value: 'openai', label: 'OpenAI' },
  { value: 'anthropic', label: 'Anthropic' },
  { value: 'opencode', label: 'Opencode' },
  { value: 'kimi', label: t('accountShare.platforms.kimi') },
  { value: 'zhipu', label: t('common.platforms.zhipu') },
  { value: 'deepseek', label: 'DeepSeek' },
  { value: 'minimax', label: 'MiniMax' },
  { value: 'qwen', label: t('common.platforms.qwen') },
  { value: 'devin', label: 'Devin' },
  { value: 'api_aggregation', label: t('accountShare.platforms.apiAggregation') }
])
const ACCOUNT_NAME_BASE_BY_PLATFORM = computed<Record<AccountSharePlatform, string>>(() => ({
  openai: t('accountShare.roomNameBase.openai'),
  anthropic: t('accountShare.roomNameBase.anthropic'),
  opencode: t('accountShare.roomNameBase.opencode'),
  kimi: t('accountShare.roomNameBase.kimi'),
  zhipu: t('accountShare.roomNameBase.zhipu'),
  deepseek: t('accountShare.roomNameBase.deepseek'),
  minimax: t('accountShare.roomNameBase.minimax'),
  qwen: t('accountShare.roomNameBase.qwen'),
  devin: t('accountShare.roomNameBase.devin'),
  api_aggregation: t('accountShare.roomNameBase.apiAggregation')
}))
const ACCOUNT_MODE_GROUP_NAME_BY_PLATFORM = computed<Record<AccountSharePlatform, string>>(() => ({
  openai: t('accountShare.modeGroupName.openai'),
  anthropic: t('accountShare.modeGroupName.anthropic'),
  opencode: t('accountShare.modeGroupName.opencode'),
  kimi: t('accountShare.modeGroupName.kimi'),
  zhipu: t('accountShare.modeGroupName.zhipu'),
  deepseek: t('accountShare.modeGroupName.deepseek'),
  minimax: t('accountShare.modeGroupName.minimax'),
  qwen: t('accountShare.modeGroupName.qwen'),
  devin: t('accountShare.modeGroupName.devin'),
  api_aggregation: t('accountShare.modeGroupName.apiAggregation')
}))
const ACCOUNT_SHARE_RECOMMENDATION_LIMIT = 10
const ACCOUNT_SHARE_RECOMMENDATION_PAGE_SIZE = 5
const OWNER_LISTINGS_PAGE_SIZE = 24
const OWNER_REVIEWS_PAGE_SIZE = 20
const recommendationPresets = computed<RecommendationPreset[]>(() => [
  {
    key: 'light',
    label: t('accountShare.presets.light'),
    request_count: 100,
    active_hours: 1,
    input_tokens_per_request: 1000,
    output_tokens_per_request: 400,
    cache_creation_tokens_per_request: 0,
    cache_read_tokens_per_request: 0,
    image_input_tokens_per_request: 0,
    image_output_tokens_per_request: 0,
    image_cache_read_tokens_per_request: 0
  },
  {
    key: 'balanced',
    label: t('accountShare.presets.balanced'),
    request_count: 500,
    active_hours: 2,
    input_tokens_per_request: 3000,
    output_tokens_per_request: 1000,
    cache_creation_tokens_per_request: 0,
    cache_read_tokens_per_request: 500,
    image_input_tokens_per_request: 0,
    image_output_tokens_per_request: 0,
    image_cache_read_tokens_per_request: 0
  },
  {
    key: 'heavy',
    label: t('accountShare.presets.heavy'),
    request_count: 3000,
    active_hours: 8,
    input_tokens_per_request: 8000,
    output_tokens_per_request: 2500,
    cache_creation_tokens_per_request: 500,
    cache_read_tokens_per_request: 3000,
    image_input_tokens_per_request: 0,
    image_output_tokens_per_request: 0,
    image_cache_read_tokens_per_request: 0
  }
])
const ACCOUNT_SHARE_PAGE_SIZE = 10
const ACCOUNT_SHARE_MODE_KEY_PAGE_SIZE = 100
const ACCOUNT_SHARE_LISTING_PREFERENCES_STORAGE_KEY = 'account-share-listing-preferences'
const MODEL_PREVIEW_LIMIT = 5
const ACCOUNT_SHARE_IDLE_TIMEOUT_MAX_MINUTES = 10080
const ACCOUNT_SHARE_STATUS_REFRESH_THROTTLE_MS = 15_000
const MY_SPEND_RANGE_OPTIONS = computed<MySpendRangeOption[]>(() => [
  { value: 'current_membership', label: t('accountShare.mySpendRange.currentMembership') },
  { value: 'today', label: t('common.today') },
  { value: '7d', label: t('admin.ops.timeRange.7d') }
])

const { t } = useI18n()
const appStore = useAppStore()
const authStore = useAuthStore()
const isWideMarketplace = useMediaQuery('(min-width: 1024px)')
const route = useRoute()
const router = useRouter()
const { copyToClipboard } = useClipboard()
const selfUseSettingsLoading = ref(false)
const selfUseSettingsError = ref('')
const ownerSelfUseRateMultiplier = computed<number | null>(() => {
  const value = appStore.cachedPublicSettings?.user_private_group_commission_rate
  return typeof value === 'number' && Number.isFinite(value) && value >= 0 && value <= 1
    ? value
    : null
})
const ownerSelfUseRateMultiplierLabel = computed(() => {
  const value = ownerSelfUseRateMultiplier.value
  return value === null ? t('accountShare.configUnavailable') : `${formatNumber(value)}x`
})
const seatOptions = Array.from({ length: ACCOUNT_SHARE_MAX_SEATS - ACCOUNT_SHARE_MIN_SEATS + 1 }, (_, index) => index + ACCOUNT_SHARE_MIN_SEATS)
const reviewScoreOptions = Array.from({ length: 11 }, (_, score) => score)
const usingFilter = computed<FilterOption>(() => ({ key: 'using', label: t('accountShare.filters.using'), tab: 'using' }))
const historyFilter = computed<FilterOption>(() => ({ key: 'history', label: t('accountShare.membership.statusHistory'), tab: 'history' }))
const archiveFilter = computed<FilterOption>(() => ({ key: 'archive', label: t('ideas.status.deleted'), tab: 'archive' }))
const ownerFilter = computed<FilterOption>(() => ({ key: 'mine', label: t('accountShare.filters.mine'), tab: 'mine' }))
const filters = computed<FilterOption[]>(() => [
  { key: 'all', label: t('accountShare.filters.all'), tab: 'all' },
  usingFilter.value,
  ownerFilter.value
])
const listingSortFieldOptions = computed<ListingSortFieldOption[]>(() => [
  { sortBy: 'account_concurrency', label: t('accountShare.membership.configuredConcurrency'), ascLabel: t('accountShare.sortDir.ascNumeric'), descLabel: t('accountShare.sortDir.descNumeric') },
  { sortBy: 'per_user_concurrency', label: t('accountShare.membership.fieldPerUserConcurrency'), ascLabel: t('accountShare.sortDir.ascNumeric'), descLabel: t('accountShare.sortDir.descNumeric') },
  { sortBy: 'min_balance_required', label: t('accountShare.membership.fieldMinBalance'), ascLabel: t('accountShare.sortDir.ascNumeric'), descLabel: t('accountShare.sortDir.descNumeric') },
  { sortBy: 'hourly_rate', label: t('accountShare.membership.fieldHourlyRate'), ascLabel: t('accountShare.sortDir.ascNumeric'), descLabel: t('accountShare.sortDir.descNumeric') },
  { sortBy: 'hourly_fee_waiver', label: t('accountShare.sortField.hourlyFeeWaiver'), ascLabel: t('accountShare.sortDir.ascNumeric'), descLabel: t('accountShare.sortDir.descNumeric') },
  { sortBy: 'rate_multiplier', label: t('groups.rateLabel'), ascLabel: t('accountShare.sortDir.ascNumeric'), descLabel: t('accountShare.sortDir.descNumeric') },
  { sortBy: 'remaining_seats', label: t('accountShare.sortField.remainingSeats'), ascLabel: t('accountShare.sortDir.ascCount'), descLabel: t('accountShare.sortDir.descCount') },
  { sortBy: 'rating', label: t('accountShare.sortField.rating'), ascLabel: t('accountShare.sortDir.ascRating'), descLabel: t('accountShare.sortDir.descRating') },
  { sortBy: 'updated_at', label: t('admin.revenue.sharePolicy.updatedAt'), ascLabel: t('accountShare.sortDir.oldestFirst'), descLabel: t('accountShare.sortDir.newestFirst') }
])
const listingSortOptions = computed<ListingSortOption[]>(() => [
  ...listingSortFieldOptions.value.flatMap(field => [
    {
      key: buildListingSortKey(field.sortBy, 'asc'),
      label: `${field.label}${field.ascLabel}`,
      shortLabel: `${field.label} ↑`,
      sortBy: field.sortBy,
      sortOrder: 'asc' as const
    },
    {
      key: buildListingSortKey(field.sortBy, 'desc'),
      label: `${field.label}${field.descLabel}`,
      shortLabel: `${field.label} ↓`,
      sortBy: field.sortBy,
      sortOrder: 'desc' as const
    }
  ])
])
const listingFeatureTagOptions = computed<ListingFeatureTagOption[]>(() => [
  { value: 'hourly_fee_waiver', label: t('accountShare.featureTags.hourlyFeeWaiver') },
  { value: 'image_generation', label: t('accountShare.featureTags.imageGeneration') },
  { value: 'codex_cli_only', label: t('accountShare.featureTags.codexCliOnly') },
  { value: 'non_codex_cli_only', label: t('accountShare.featureTags.nonCodexCliOnly') },
  { value: 'no_hourly_fee', label: t('accountShare.featureTags.noHourlyFee') }
])
const listingStatusFilterOptions = computed<Array<{ value: ListingStatusFilterValue; label: string }>>(() => [
  { value: '', label: t('accountShare.statusFilter.default') },
  { value: 'available', label: t('accountShare.statusFilter.available') },
  { value: 'active', label: t('accountShare.statusFilter.active') },
  { value: 'paused', label: t('accountShare.lifecycle.statusDelisted') },
  { value: 'suspended', label: t('accountShare.lifecycle.statusAdminPaused') },
  { value: 'all', label: t('userAccounts.allStatus') }
])
const openAIAccountLevelConfigs = computed(() =>
  normalizeOpenAIAccountLevelConfigs(appStore.cachedPublicSettings?.openai_account_levels)
)
const accountLevelFilterOptions = computed<Array<{ value: AccountLevelFilterValue; label: string }>>(() =>
  openAIAccountLevelOptions(openAIAccountLevelConfigs.value, {
    includeEmpty: true,
    emptyLabel: t('accountShare.levelFilter.all'),
    includeUnknown: true,
    unknownLabel: 'UNKNOWN'
  }).map(option => ({
    value: (option.value === '' ? 'all' : option.value) as AccountLevelFilterValue,
    label: option.label
  }))
)
function accountShareJoinErrorMessages(): Record<string, string> {
  return {
  ACCOUNT_SHARE_ACCOUNT_UNAVAILABLE: t('accountShare.errors.join.accountUnavailable'),
  ACCOUNT_SHARE_ALREADY_USING: t('accountShare.errors.join.alreadyUsing'),
  ACCOUNT_SHARE_API_KEY_ALREADY_BOUND: t('accountShare.errors.join.apiKeyAlreadyBound'),
  ACCOUNT_SHARE_API_KEY_MUST_USE_MODE_GROUP: t('accountShare.errors.join.apiKeyMustUseModeGroup'),
  ACCOUNT_SHARE_LISTING_NOT_FOUND: t('accountShare.errors.join.listingNotFound'),
  ACCOUNT_SHARE_LISTING_NOT_ACTIVE: t('accountShare.errors.join.listingNotActive'),
  ACCOUNT_SHARE_ROOM_FULL: t('accountShare.errors.join.roomFull'),
  ACCOUNT_SHARE_BALANCE_BELOW_MINIMUM: t('accountShare.errors.join.balanceBelowMinimum'),
  ACCOUNT_SHARE_MODE_GROUP_UNAVAILABLE: t('accountShare.errors.join.modeGroupUnavailable'),
  ACCOUNT_SHARE_MODE_GROUP_UNBOUND: t('accountShare.errors.join.modeGroupUnbound'),
  ACCOUNT_SHARE_MODE_INVALID_IDLE_TIMEOUT: t('accountShare.errors.join.invalidIdleTimeout'),
  ACCOUNT_SHARE_MODE_PREPAY_INSUFFICIENT: t('accountShare.errors.join.prepayInsufficient'),
  ACCOUNT_SHARE_PER_USER_CONCURRENCY_EXCEEDED: t('accountShare.errors.join.perUserConcurrencyExceeded'),
  ACCOUNT_SHARE_OWNER_CANNOT_JOIN: t('accountShare.errors.join.ownerCannotJoin'),
  ACCOUNT_SHARE_JOIN_INTENT_REQUIRED: t('accountShare.errors.join.intentRequired'),
  ACCOUNT_SHARE_JOIN_INTENT_INVALID: t('accountShare.errors.join.intentInvalid'),
  ACCOUNT_SHARE_JOIN_INTENT_CONSUMED: t('accountShare.errors.join.intentConsumed'),
  ACCOUNT_SHARE_JOIN_TERMS_CHANGED: t('accountShare.errors.join.termsChanged'),
  ACCOUNT_SHARE_ROOM_PASSWORD_REQUIRED: t('accountShare.errors.join.passwordRequired'),
  ACCOUNT_SHARE_ROOM_PASSWORD_INVALID: t('accountShare.errors.join.passwordInvalid'),
  ACCOUNT_SHARE_MEMBERSHIP_ENDING: t('accountShare.errors.join.membershipEnding'),
  API_KEY_NOT_FOUND: t('accountShare.errors.join.apiKeyNotFound'),
  INSUFFICIENT_PERMISSIONS: t('accountShare.errors.join.insufficientPermissions'),
  SERVICE_UNAVAILABLE: t('accountShare.errors.join.serviceUnavailable'),
  USER_NOT_FOUND: t('accountShare.errors.join.userNotFound'),
  ACCOUNT_SHARE_UNVERIFIED_ACK_REQUIRED: t('accountShare.errors.join.unverifiedAckRequired')
  }
}
function accountShareRoomCreateErrorMessages(): Record<string, string> {
  return {
  ACCOUNT_SHARE_ROOM_LIMIT_EXCEEDED: t('accountShare.errors.roomCreate.roomLimitExceeded'),
  ACCOUNT_SHARE_ROOM_CREATE_RATE_EXCEEDED: t('accountShare.errors.roomCreate.rateExceeded'),
  ACCOUNT_SHARE_ROOM_ACCOUNT_LIMIT_EXCEEDED: t('accountShare.roomAccounts.errors.roomAccountLimit'),
  ACCOUNT_SHARE_OWNER_ROOM_ACCOUNT_LIMIT_EXCEEDED: t('accountShare.errors.roomCreate.ownerAccountLimit'),
  ACCOUNT_SHARE_ROOM_OWNER_MISMATCH: t('accountShare.errors.roomCreate.ownerMismatch'),
  ACCOUNT_SHARE_ROOM_PLATFORM_MISMATCH: t('accountShare.errors.roomCreate.platformMismatch'),
  ACCOUNT_SHARE_ROOM_LEVEL_MISMATCH: t('accountShare.errors.roomCreate.levelMismatch'),
  ACCOUNT_SHARE_ROOM_UNKNOWN_LEVEL: t('accountShare.errors.roomCreate.unknownLevel'),
  ACCOUNT_SHARE_ROOM_MODE_REQUIRED: t('accountShare.errors.roomCreate.modeRequired'),
  ACCOUNT_SHARE_ROOM_ACCOUNT_CONFLICT: t('accountShare.errors.roomCreate.accountConflict'),
  ACCOUNT_SHARE_QUOTA_HISTORICAL_GROWTH_BLOCKED: t('accountShare.errors.roomCreate.historicalGrowthBlocked'),
  ACCOUNT_SHARE_QUOTA_GRANDFATHER_GROWTH_BLOCKED: t('accountShare.errors.roomCreate.grandfatherGrowthBlocked'),
  ACCOUNT_SHARE_ROOM_PASSWORD_INVALID_LENGTH: t('accountShare.errors.roomCreate.passwordInvalidLength')
  }
}
function accountShareCapabilityBlockerMessages(): Record<string, string> {
  return {
  ACCOUNT_SHARE_ROOM_LIMIT_EXCEEDED: t('accountShare.roomAccounts.errors.roomLimit'),
  ACCOUNT_SHARE_ROOM_CREATE_RATE_EXCEEDED: t('accountShare.roomAccounts.errors.createRate'),
  ACCOUNT_SHARE_ROOM_ACCOUNT_LIMIT_EXCEEDED: t('accountShare.errors.capability.roomAccountLimit'),
  ACCOUNT_SHARE_OWNER_ROOM_ACCOUNT_LIMIT_EXCEEDED: t('accountShare.errors.capability.ownerAccountLimit'),
  ACCOUNT_SHARE_QUOTA_HISTORICAL_GROWTH_BLOCKED: t('accountShare.errors.capability.historicalGrowthBlocked'),
  ACCOUNT_SHARE_QUOTA_GRANDFATHER_GROWTH_BLOCKED: t('accountShare.errors.capability.grandfatherGrowthBlocked')
  }
}
function accountShareRecommendationErrorMessages(): Record<string, string> {
  return {
  ACCOUNT_SHARE_RECOMMENDATION_INVALID: t('accountShare.errors.recommendation.invalid'),
  ACCOUNT_SHARE_API_KEY_MUST_USE_MODE_GROUP: t('accountShare.errors.join.apiKeyMustUseModeGroup'),
  API_KEY_NOT_FOUND: t('accountShare.errors.join.apiKeyNotFound'),
  SERVICE_UNAVAILABLE: t('accountShare.errors.recommendation.serviceUnavailable'),
  USER_NOT_FOUND: t('accountShare.errors.join.userNotFound')
  }
}
function accountShareEndErrorMessages(): Record<string, string> {
  return {
  ...accountShareJoinErrorMessages(),
  ACCOUNT_SHARE_LISTING_NOT_FOUND: t('accountShare.errors.endUse.listingNotFound'),
  ACCOUNT_SHARE_END_STATE_CONFLICT: t('accountShare.errors.endUse.stateConflict'),
  ACCOUNT_SHARE_MEMBERSHIP_NOT_FOUND: t('accountShare.errors.endUse.membershipNotFound'),
  ACCOUNT_SHARE_MEMBERSHIP_ENDING: t('accountShare.errors.endUse.membershipEnding')
  }
}

function getListingPreferencesStorageKey(): string {
  const userID = Number(authStore.user?.id || 0)
  return userID > 0
    ? `${ACCOUNT_SHARE_LISTING_PREFERENCES_STORAGE_KEY}:user:${userID}`
    : ACCOUNT_SHARE_LISTING_PREFERENCES_STORAGE_KEY
}

function defaultListingPreferences(): ListingPreferenceState {
  // 广场默认只看可用房间（状态 active + 账号健康 + 有空位）。后端对普通用户
  // tab=all 也会强制 available_only 兜底；这里把状态筛选项默认置为「可用账号」，
  // 让筛选 chip 明确显示当前处于「仅可用」模式，用户可切到「默认状态」看全部。
  // 管理员例外：admin 浏览广场（tab=all）保持全量，便于监管查看所有房间
  // （后端 listListings 对 !viewerIsAdmin 才注入 available_only，这里保持一致）。
  const defaultStatus = authStore.user?.role === 'admin' ? '' : 'available'
  return {
    platform: 'openai',
    tab: 'all',
    search: '',
    pageSize: ACCOUNT_SHARE_PAGE_SIZE,
    status: defaultStatus,
    accountLevel: 'all',
    sortKeys: [],
    seatLimits: [],
    featureTags: [],
    models: []
  }
}

function filterForListingTab(tab: AccountShareListingTab): FilterOption {
  if (tab === 'history') return historyFilter.value
  if (tab === 'archive') return archiveFilter.value
  return [ownerFilter.value, ...filters.value].find(option => option.tab === tab)
    || filters.value.find(option => option.tab === 'all')
    || filters.value[0]
}

function normalizeListingPlatform(value: unknown): AccountSharePlatform {
  if (ACCOUNT_SHARE_PLATFORM_OPTIONS.value.some(option => option.value === value)) {
    return value as AccountSharePlatform
  }
  return 'openai'
}

function normalizeListingTab(value: unknown): AccountShareListingTab {
  if (typeof value !== 'string') return defaultListingPreferences().tab
  return filterForListingTab(value as AccountShareListingTab).tab
}

function normalizeListingStatus(value: unknown): ListingStatusFilterValue {
  return listingStatusFilterOptions.value.some(option => option.value === value)
    ? value as ListingStatusFilterValue
    : ''
}

function normalizeListingAccountLevel(value: unknown, platform: AccountSharePlatform): AccountLevelFilterValue {
  if (platform !== 'openai') return 'all'
  if (value === 'all' || value === '') return 'all'
  const normalized = normalizeOpenAIAccountLevelKey(value)
  return normalized ? normalized as AccountLevelFilterValue : 'all'
}

function normalizeListingSortKeys(value: unknown): ListingSortKey[] {
  if (!Array.isArray(value)) return []
  const normalized: ListingSortKey[] = []
  const seenSortFields = new Set<AccountShareListingSortBy>()
  for (const item of value) {
    if (typeof item !== 'string') continue
    const option = listingSortOptions.value.find(candidate => candidate.key === item)
    if (!option?.sortBy || seenSortFields.has(option.sortBy)) continue
    seenSortFields.add(option.sortBy)
    normalized.push(option.key)
  }
  return normalized
}

function normalizeListingSeatLimits(value: unknown): number[] {
  if (!Array.isArray(value)) return []
  const validSeats = new Set(seatOptions)
  return Array.from(
    new Set(
      value
        .map(item => Number(item))
        .filter(item => Number.isInteger(item) && validSeats.has(item))
    )
  ).sort((a, b) => a - b)
}

function normalizeListingFeatureTags(value: unknown, platform: AccountSharePlatform): AccountShareListingFeatureTag[] {
  if (!Array.isArray(value)) return []
  const validTags = new Set(listingFeatureTagOptions.value.map(option => option.value))
  const tags: AccountShareListingFeatureTag[] = []
  const seen = new Set<AccountShareListingFeatureTag>()
  for (const item of value) {
    if (!validTags.has(item as AccountShareListingFeatureTag)) continue
    const tag = item as AccountShareListingFeatureTag
    if (seen.has(tag)) continue
    if (platform !== 'openai' && (tag === 'image_generation' || tag === 'codex_cli_only' || tag === 'non_codex_cli_only')) continue
    seen.add(tag)
    tags.push(tag)
  }
  return tags
}

function normalizeListingModels(value: unknown): string[] {
  if (!Array.isArray(value)) return []
  const models: string[] = []
  const seen = new Set<string>()
  for (const item of value) {
    if (typeof item !== 'string') continue
    const model = normalizeModelFilterValue(item)
    if (!model) continue
    const key = model.toLowerCase()
    if (seen.has(key)) continue
    seen.add(key)
    models.push(model)
  }
  return models
}

function normalizeListingPageSize(value: unknown): number {
  return normalizeTablePageSize(value || ACCOUNT_SHARE_PAGE_SIZE)
}

function normalizeListingPreferences(value: unknown): ListingPreferenceState {
  const defaults = defaultListingPreferences()
  if (!value || typeof value !== 'object') return defaults
  const raw = value as Partial<ListingPreferenceState>
  const platform = normalizeListingPlatform(raw.platform)
  return {
    platform,
    tab: normalizeListingTab(raw.tab),
    search: typeof raw.search === 'string' ? raw.search.trim() : '',
    pageSize: normalizeListingPageSize(raw.pageSize),
    status: normalizeListingStatus(raw.status),
    accountLevel: normalizeListingAccountLevel(raw.accountLevel, platform),
    sortKeys: normalizeListingSortKeys(raw.sortKeys),
    seatLimits: normalizeListingSeatLimits(raw.seatLimits),
    featureTags: normalizeListingFeatureTags(raw.featureTags, platform),
    models: normalizeListingModels(raw.models)
  }
}

function readListingPreferences(): ListingPreferenceState {
  if (typeof window === 'undefined') return defaultListingPreferences()
  const storageKey = getListingPreferencesStorageKey()
  const defaults = defaultListingPreferences()
  try {
    const raw = window.localStorage.getItem(storageKey)
    if (!raw) return defaults
    const parsed = normalizeListingPreferences(JSON.parse(raw))
    // 迁移：旧版本持久化的 status=''（旧「默认状态」）现在对普通用户等价于
    // 「可用账号」。检测到旧默认值时归一为当前默认并写回，让筛选 chip 与实际
    // 过滤行为一致（否则老用户 chip 显示「默认状态」但后端兜底只返回可用房间，
    // 用户看不到切换到「看全部」的入口）。
    if (parsed.status === '' && defaults.status !== '') {
      parsed.status = defaults.status
      window.localStorage.setItem(storageKey, JSON.stringify(parsed))
    }
    return parsed
  } catch (error) {
    window.localStorage.removeItem(storageKey)
    console.warn('Failed to read account share listing preferences:', error)
    return defaults
  }
}

function buildCurrentListingPreferences(): ListingPreferenceState {
  return normalizeListingPreferences({
    platform: activeListingPlatform.value,
    tab: activeFilter.value.tab,
    search: searchQuery.value,
    pageSize: initialListingPreferences.pageSize,
    status: listingFilters.status,
    accountLevel: listingFilters.accountLevel,
    sortKeys: listingFilters.sortKeys,
    seatLimits: listingFilters.seatLimits,
    featureTags: listingFilters.featureTags,
    models: listingFilters.models
  })
}

function persistListingPreferences(): void {
  if (typeof window === 'undefined') return
  try {
    window.localStorage.setItem(
      getListingPreferencesStorageKey(),
      JSON.stringify(buildCurrentListingPreferences())
    )
  } catch (error) {
    console.warn('Failed to persist account share listing preferences:', error)
  }
}

function buildDefaultRecommendationForm(): RecommendationFormState {
  const preset = recommendationPresets.value[1]
  return {
    api_key_id: 0,
    model: '',
    request_count: preset.request_count,
    active_hours: preset.active_hours,
    input_tokens_per_request: preset.input_tokens_per_request,
    output_tokens_per_request: preset.output_tokens_per_request,
    cache_creation_tokens_per_request: preset.cache_creation_tokens_per_request,
    cache_read_tokens_per_request: preset.cache_read_tokens_per_request,
    image_input_tokens_per_request: preset.image_input_tokens_per_request,
    image_output_tokens_per_request: preset.image_output_tokens_per_request,
    image_cache_read_tokens_per_request: preset.image_cache_read_tokens_per_request
  }
}

const initialListingPreferences = readListingPreferences()
const activeFilter = ref<FilterOption>(filterForListingTab(initialListingPreferences.tab))
const activeListingPlatform = ref<AccountSharePlatform>(initialListingPreferences.platform)
const listings = ref<AccountShareListing[]>([])
const roomGridViewportRef = ref<HTMLElement | null>(null)
const roomGridRef = ref<HTMLElement | null>(null)
const membershipHistoryEntries = ref<AccountShareMembershipHistoryEntry[]>([])
const visibleValidatingListingIDs = ref(new Set<number>())
const selectedRecommendationPreset = ref<RecommendationPresetKey>('balanced')
const recommendationForm = reactive<RecommendationFormState>(buildDefaultRecommendationForm())
const recommendationLoading = ref(false)
const recommendationUsageProfileLoading = ref(false)
const recommendationUsageProfileMessage = ref('')
const recommendationError = ref('')
const recommendationResult = ref<AccountShareRecommendationResult | null>(null)
const recommendationRequestSnapshot = ref<AccountShareRecommendationRequest | null>(null)
const recommendationPage = ref(1)
const showUsageGuideDialog = ref(false)
const showAdminQuotaDialog = ref(false)
const showRecommendationDialog = ref(false)
const keyResolutionMemberships = ref<AccountShareMembership[]>([])
const keyResolutionListings = ref<AccountShareListing[]>([])
const keyResolutionBindingStatus = ref<AccountShareAPIKeyBindingStatus | null>(null)
const keyResolutionLoading = ref(false)
const keyResolutionLoaded = ref(false)
const keyResolutionError = ref('')
const pagination = reactive({
  page: 1,
  page_size: initialListingPreferences.pageSize,
  total: 0,
  pages: 1,
  total_exact: false,
  has_more: false
})
const membershipHistoryPagination = reactive({
  page: 1,
  page_size: initialListingPreferences.pageSize,
  total: 0,
  pages: 1
})
const loading = ref(false)
const errorMessage = ref('')
const detailSnapshot = ref<AccountShareListing | null>(null)
const detailIsArchive = ref(false)
const detailLoading = ref(false)
const detailError = ref('')
let detailRequestController: AbortController | null = null
let detailRequestSeq = 0
const detailListing = computed(() => {
  const snapshot = detailSnapshot.value
  if (!snapshot || detailIsArchive.value) return snapshot
  return displayedListings.value.find(listing => listing.id === snapshot.id) ?? snapshot
})
const membershipHistoryLoading = ref(false)
const membershipHistoryError = ref('')
const capabilities = ref<AccountShareCapabilities | null>(null)
const capabilitiesLoading = ref(false)
const capabilitiesError = ref('')
const actionErrorDialog = reactive<{
  show: boolean
  title: string
  message: string
  action: AccountShareActionErrorAction
}>({
  show: false,
  title: t('ideas.admin.actionFailed'),
  message: '',
  action: null
})
const createErrorMessage = ref('')
const showCreate = ref(false)
const showCreateAccount = ref(false)
const proxies = ref<Proxy[]>([])
let createAccountProxyRequestSeq = 0
const createPlatform = ref<AccountSharePlatform>('openai')
const ownedAccounts = ref<Account[]>([])
const ownedAccountsLoading = ref(false)
const ownedAccountsError = ref('')
const selectedOwnedAccountID = ref(0)
let ownedAccountsRequestVersion = 0
let ownedAccountsRequestController: AbortController | null = null
let ownedAccountsLoadedPlatform: AccountSharePlatform | null = null
let pendingCreateRoomIntentSignature = ''
let pendingCreateRoomIdempotencyKey = ''
const reviewSubmitIntent: StableIdempotencyIntent = { signature: '', key: '' }
const updateListingIntent: StableIdempotencyIntent = { signature: '', key: '' }
const creating = ref(false)
const preparingJoinId = ref<number | null>(null)
const joiningId = ref<number | null>(null)
const joinIntentError = ref('')
const pendingJoinConfirmation = ref<PendingJoinConfirmation | null>(null)
const joinPasswordPrompt = ref<JoinPasswordPromptState | null>(null)
const joinPasswordInput = ref('')
const joinPasswordError = ref('')
const endingId = ref<number | null>(null)
const pendingEndUse = ref<PendingEndUseState | null>(null)
const pendingMembershipEnds = ref<Record<number, PendingMembershipEnd>>({})
const pendingReview = ref<ReviewDialogState | null>(null)
const roomAccountsListing = ref<AccountShareListing | null>(null)
const roomLifecycleListing = ref<AccountShareListing | null>(null)
const ownerDialog = reactive({
  show: false,
  ownerUserID: 0,
  ownerUsername: '',
  sourceListing: null as AccountShareListing | null,
  tab: 'listings' as OwnerDialogTab,
  loadingListings: false,
  loadingReviews: false,
  listings: [] as AccountShareListing[],
  reviews: [] as AccountShareReview[],
  listingsPage: 1,
  listingsHasMore: false,
  listingsTotalExact: true,
  listingsTotal: 0,
  reviewsPage: 1,
  reviewsPages: 1,
  reviewsTotal: 0,
  listingsError: '',
  reviewsError: ''
})
const showMySpendDialog = ref(false)
const mySpendSelectedOptionKey = ref('')
const mySpendSelectedOption = ref<MySpendAccountOption | null>(null)
const mySpendPickerSource = ref<MySpendAccountOptionSource>('using')
const mySpendUsingAccountOptions = ref<MySpendAccountOption[]>([])
const mySpendHistoryAccountOptions = ref<MySpendAccountOption[]>([])
const mySpendUsingPagination = reactive<MySpendAccountOptionPagination>({
  page: 1,
  pageSize: MY_SPEND_ACCOUNT_PAGE_SIZE,
  total: 0,
  pages: 1,
  totalExact: true,
  hasMore: false,
})
const mySpendHistoryPagination = reactive<MySpendAccountOptionPagination>({
  page: 1,
  pageSize: MY_SPEND_ACCOUNT_PAGE_SIZE,
  total: 0,
  pages: 1,
  totalExact: true,
  hasMore: false,
})
const mySpendAccountsLoading = ref(false)
const mySpendAccountsError = ref('')
const mySpendRange = ref<AccountShareMySpendRange>('current_membership')
const mySpendSummary = ref<AccountShareMySpendSummary | null>(null)
const mySpendLoading = ref(false)
const mySpendError = ref('')
const pendingForceEditListing = ref<AccountShareListing | null>(null)
const pendingForceEditManagementState = ref<AccountShareRoomManagementState | null>(null)
const forceEditReason = ref('')
const forceEditConfirmed = ref(false)
const pendingDraftDiscardTarget = ref<DraftDiscardTarget | null>(null)
const createDraftBaseline = ref<CreateDraftSnapshot | null>(null)
const configDraftBaseline = ref<ConfigDraftSnapshot | null>(null)
const managedActionId = ref<number | null>(null)
const showConfigEditDialog = ref(false)
const editingConfigListing = ref<AccountShareListing | null>(null)
const editAllowedModels = ref<string[]>([])
const editForceActive = ref(false)
const editConsumerProtected = ref(false)
const editJoinPasswordClear = ref(false)
const editReason = ref('')
const editErrorMessage = ref('')
const editVersionConflict = ref(false)
const savingConfigEdit = ref(false)
const selectedKeyByListing = reactive<Record<number, number>>({})
const idleTimeoutByListing = reactive<Record<number, number>>({})
const savingIdleTimeoutId = ref<number | null>(null)
const modeGroupIDsByPlatform = reactive<Record<AccountSharePlatform, number>>({
  openai: 0,
  anthropic: 0,
  opencode: 0,
  kimi: 0,
  zhipu: 0,
  deepseek: 0,
  minimax: 0,
  qwen: 0,
  devin: 0,
  api_aggregation: 0
})
const modeApiKeysByPlatform = reactive<Record<AccountSharePlatform, ApiKey[]>>({
  openai: [],
  anthropic: [],
  opencode: [],
  kimi: [],
  zhipu: [],
  deepseek: [],
  minimax: [],
  qwen: [],
  devin: [],
  api_aggregation: []
})
const modeKeysLoadingByPlatform = reactive<Record<AccountSharePlatform, boolean>>({
  openai: false,
  anthropic: false,
  opencode: false,
  kimi: false,
  zhipu: false,
  deepseek: false,
  minimax: false,
  qwen: false,
  devin: false,
  api_aggregation: false
})
const modeKeysLoadedByPlatform = reactive<Record<AccountSharePlatform, boolean>>({
  openai: false,
  anthropic: false,
  opencode: false,
  kimi: false,
  zhipu: false,
  deepseek: false,
  minimax: false,
  qwen: false,
  devin: false,
  api_aggregation: false
})
const modeKeysErrorByPlatform = reactive<Record<AccountSharePlatform, string>>({
  openai: '',
  anthropic: '',
  opencode: '',
  kimi: '',
  zhipu: '',
  deepseek: '',
  minimax: '',
  qwen: '',
  devin: '',
  api_aggregation: ''
})
const knownListings = ref<AccountShareListing[]>([])
const searchQuery = ref(initialListingPreferences.search)
const selectedOwnerID = ref(0)
const selectedOwnerDisplayName = ref('')
const modelFilterInput = ref('')
const filterPanelRef = ref<HTMLElement | null>(null)
const statusFilterTriggerRef = ref<HTMLButtonElement | null>(null)
const levelFilterTriggerRef = ref<HTMLButtonElement | null>(null)
const seatFilterTriggerRef = ref<HTMLButtonElement | null>(null)
const featureFilterTriggerRef = ref<HTMLButtonElement | null>(null)
const modelFilterTriggerRef = ref<HTMLButtonElement | null>(null)
const openFilterPopover = ref<ListingFilterPopover | null>(null)
const nowMs = ref(Date.now())
let clockTimer: number | null = null
let searchDebounceTimer: number | null = null
let suppressNextSearchRefresh = false
let listingsRequestController: AbortController | null = null
let listingsRequestSeq = 0
let membershipHistoryRequestController: AbortController | null = null
let membershipHistoryRequestSeq = 0
let mySpendAccountsRequestController: AbortController | null = null
let mySpendAccountsRequestSeq = 0
let mySpendRequestController: AbortController | null = null
let mySpendRequestSeq = 0
let recommendationRequestController: AbortController | null = null
let recommendationRequestSeq = 0
let recommendationUsageProfileController: AbortController | null = null
let recommendationUsageProfileRequestSeq = 0
let ownerListingsRequestController: AbortController | null = null
let ownerReviewsRequestController: AbortController | null = null
let ownerDialogRequestSeq = 0
let modeKeysRequestSeq = 0
let keyResolutionRequestSeq = 0
let membershipEndOperationRequestSeq = 0
const membershipEndOperationControllers = new Map<number, AbortController>()
const membershipEndBindingControllers = new Map<number, AbortController>()
const membershipEndBindingLastAttempt = new Map<number, number>()
const MEMBERSHIP_END_BINDING_RECHECK_MS = 30_000
const MEMBERSHIP_END_QUERY_FAILURE_RECHECK_THRESHOLD = 2
// 房间满员时后端事务回滚、join intent 未被消费，可在 token 有效期内
// 自动重试等待空位；指数间隔（1.5s/3s/4.5s）避免对满员房间造成冲击。
const JOIN_ROOM_FULL_RETRY_COUNT = 3
const JOIN_ROOM_FULL_RETRY_BASE_MS = 1_500
const JOIN_ROOM_FULL_MIN_REMAINING_MS = 2_000
let lastMembershipStatusRefreshAt = 0
let membershipStatusRefreshTimer: number | null = null

const listingFilters = reactive<ListingFilterState>({
  status: initialListingPreferences.status,
  accountLevel: initialListingPreferences.accountLevel,
  sortKeys: [...initialListingPreferences.sortKeys],
  seatLimits: [...initialListingPreferences.seatLimits],
  featureTags: [...initialListingPreferences.featureTags],
  models: [...initialListingPreferences.models]
})


function buildDefaultCreateForm(): CreateFormState {
  return {
    name: suggestedAccountName(createPlatform.value),
    proxy_id: null,
    concurrency: DEFAULT_ACCOUNT_CONCURRENCY,
    seat_limit: 5,
    rate_multiplier: 1,
    per_user_concurrency: DEFAULT_PER_USER_CONCURRENCY,
    hourly_rate: DEFAULT_HOURLY_RATE,
    hourly_fee_waiver_minimum: 0,
    min_balance_required: 1,
    codex_cli_only: true,
    codex_5h_limit_percent: 100,
    codex_7d_limit_percent: 100,
    anthropic_5h_limit_percent: 100,
    anthropic_7d_limit_percent: 100,
    join_password: ''
  }
}

const createForm = reactive<CreateFormState>(buildDefaultCreateForm())
const editForm = reactive<CreateFormState>(buildDefaultCreateForm())
const allowedModels = ref<string[]>([])
const roomCatalogModels = ref<string[] | null>(null)
const roomCatalogLoading = ref(false)
const roomCatalogError = ref('')
let roomCatalogRequestVersion = 0

function createDraftSnapshot(): CreateDraftSnapshot {
  return {
    platform: createPlatform.value,
    selectedOwnedAccountID: selectedOwnedAccountID.value,
    form: { ...createForm },
    allowedModels: [...allowedModels.value]
  }
}

function configDraftSnapshot(): ConfigDraftSnapshot {
  return {
    form: { ...editForm },
    allowedModels: [...editAllowedModels.value],
    reason: editReason.value
  }
}

function snapshotsMatch(left: unknown, right: unknown): boolean {
  return JSON.stringify(left) === JSON.stringify(right)
}

function captureCreateDraftBaseline(): void {
  createDraftBaseline.value = createDraftSnapshot()
}

function captureConfigDraftBaseline(): void {
  configDraftBaseline.value = configDraftSnapshot()
}

function createDraftHasChanges(): boolean {
  return Boolean(
    createDraftBaseline.value
    && !snapshotsMatch(createDraftBaseline.value, createDraftSnapshot())
  )
}

function configDraftHasChanges(): boolean {
  return Boolean(
    configDraftBaseline.value
    && (!snapshotsMatch(configDraftBaseline.value, configDraftSnapshot()) || editJoinPasswordClear.value)
  )
}

const isOpenAIListingPlatform = computed(() => activeListingPlatform.value === 'openai')
const visibleListingFeatureTagOptions = computed(() =>
  listingFeatureTagOptions.value.filter(option =>
    isOpenAIListingPlatform.value || (
      option.value !== 'image_generation' &&
      option.value !== 'codex_cli_only' &&
      option.value !== 'non_codex_cli_only'
    )
  )
)

async function loadRoomCatalog(platform: AccountSharePlatform): Promise<void> {
  const requestVersion = ++roomCatalogRequestVersion
  roomCatalogError.value = ''
  roomCatalogLoading.value = true
  roomCatalogModels.value = null
  try {
    const result = await accountsAPI.getModelOptions(platform)
    if (requestVersion !== roomCatalogRequestVersion) return
    const models = Array.from(new Set((result.models || []).map(model => model.trim()).filter(Boolean)))
    roomCatalogModels.value = models
    allowedModels.value = [...models]
  } catch (error) {
    if (requestVersion !== roomCatalogRequestVersion) return
    roomCatalogModels.value = []
    allowedModels.value = []
    roomCatalogError.value = extractApiErrorMessage(error, t('accountShare.errors.loadModelCatalog'))
  } finally {
    if (requestVersion === roomCatalogRequestVersion) {
      roomCatalogLoading.value = false
    }
  }
}

function listingPlatform(listing: AccountShareListing | null | undefined): AccountSharePlatform {
  return normalizeListingPlatform(listing?.platform)
}

function isOpenAIListing(listing: AccountShareListing | null | undefined): boolean {
  return listingPlatform(listing) === 'openai'
}

function isAPIAggregationListing(listing: AccountShareListing | null | undefined): boolean {
  return listingPlatform(listing) === 'api_aggregation'
}






function opencodeUsageLabel(listing: AccountShareListing | null | undefined): string {
  const parts: string[] = []
  if (listing?.opencode_5h_usage) parts.push(`5h ${Math.round(listing.opencode_5h_usage.utilization)}%`)
  if (listing?.opencode_7d_usage) parts.push(`7d ${Math.round(listing.opencode_7d_usage.utilization)}%`)
  if (listing?.opencode_30d_usage) parts.push(`30d ${Math.round(listing.opencode_30d_usage.utilization)}%`)
  const label = parts.join(' / ') || '—'
  return listing?.opencode_quota_protection_reason
    ? t('accountShare.card.protectedLabel', { label: label, reason: listing.opencode_quota_protection_reason  })
    : label
}

function anthropic5hLimitPercent(listing: AccountShareListing | null | undefined): number {
  return normalizeUsageLimitPercent(listing?.anthropic_5h_limit_percent ?? listing?.codex_5h_limit_percent)
}

function anthropic7dLimitPercent(listing: AccountShareListing | null | undefined): number {
  return normalizeUsageLimitPercent(listing?.anthropic_7d_limit_percent ?? listing?.codex_7d_limit_percent)
}

function normalizeUsageLimitPercent(value: unknown): number {
  const numeric = Number(value)
  return Number.isFinite(numeric) && numeric >= 1 && numeric <= 100 ? numeric : 100
}

function normalizeRoomAccountCount(value: unknown): number {
  const numeric = Number(value)
  return Number.isFinite(numeric) && numeric > 0 ? Math.trunc(numeric) : 0
}

function roomAttachedAccountCount(listing: AccountShareListing): number {
  return normalizeRoomAccountCount(
    listing.quota_summary?.attached_count ?? listing.account_count
  )
}

function roomEligibleAccountCount(listing: AccountShareListing): number {
  return normalizeRoomAccountCount(
    listing.quota_summary?.eligible_count ?? listing.healthy_account_count
  )
}

function roomAggregateAccountCountLabel(listing: AccountShareListing): string {
  const prefix = listing.deleted || listing.status !== 'active' ? t('accountShare.roomAccounts.health') : t('admin.accounts.quotaDashboard.schedulableAccounts')
  return `${prefix} ${roomEligibleAccountCount(listing)}/${roomAttachedAccountCount(listing)}`
}

function roomAggregateInsight(listing: AccountShareListing): {
  detail: string
  badge: string
  tone: RuntimeTone
} {
  const attached = roomAttachedAccountCount(listing)
  const eligible = roomEligibleAccountCount(listing)
  const lifecycle = listingStatusLabel(listing)
  if (listing.deleted || listing.status !== 'active') {
    return {
      detail: t('accountShare.card.lifecycleNotJoinable', { lifecycle }),
      badge: lifecycle,
      tone: listing.deleted
        ? 'muted'
        : listing.status === 'suspended' || listing.status === 'disabled'
          ? 'danger'
          : 'warning'
    }
  }
  if (attached === 0) {
    return {
      detail: t('accountShare.card.lifecycleNoAccounts', { lifecycle }),
      badge: t('accountShare.card.badgeNoAccounts'),
      tone: 'danger'
    }
  }
  if (eligible === 0) {
    return {
      detail: t('accountShare.card.lifecycleNoRoutable', { lifecycle }),
      badge: t('accountShare.lifecycle.healthDown'),
      tone: 'danger'
    }
  }
  if (eligible < attached) {
    return {
      detail: t('accountShare.card.lifecyclePartial', { lifecycle }),
      badge: t('accountShare.lifecycle.healthPartial'),
      tone: 'warning'
    }
  }
  return {
    detail: t('accountShare.card.lifecycleRoutable', { lifecycle }),
    badge: t('common.available'),
    tone: 'normal'
  }
}

function roomAvailableConcurrencyLabel(listing: AccountShareListing): string {
  if (listing.deleted || listing.status !== 'active') return t('accountShare.card.notJoinable')
  if (listing.runtime_load_known !== true) return t('accountShare.card.runtimeUnknown')
  const total = Number(listing.account_concurrency)
  const used = Number(listing.current_concurrency)
  if (!Number.isFinite(total) || !Number.isFinite(used)) return t('accountShare.card.runtimeUnknown')
  return `${Math.max(total - used, 0)} / ${Math.max(total, 0)}`
}

function quotaWindowUtilization(value: unknown): number | null {
  const numeric = Number(value)
  return value !== null && value !== undefined && Number.isFinite(numeric)
    ? numeric
    : null
}

function roomWindowUtilization(window?: AccountShareRoomQuotaWindow): number | null {
  if (!window || window.known_count <= 0) return null
  const utilization = quotaWindowUtilization(window.average_utilization)
  if (utilization === null) return null
  return Math.min(100, Math.max(0, utilization))
}

function roomWindowUtilizationLabel(window?: AccountShareRoomQuotaWindow): string {
  const utilization = roomWindowUtilization(window)
  return utilization === null ? t('common.noData') : `${formatNumber(utilization)}%`
}

function roomWindowUtilizationBarClass(window?: AccountShareRoomQuotaWindow): string {
  const utilization = roomWindowUtilization(window)
  if (utilization === null || utilization < 80) return 'combined-availability-fill combined-availability-fill-normal'
  if (utilization < 100) return 'combined-availability-fill combined-availability-fill-warning'
  return 'combined-availability-fill combined-availability-fill-danger'
}

function platformLabel(platform: string): string {
  return ACCOUNT_SHARE_PLATFORM_OPTIONS.value.find(item => item.value === platform)?.label || platform
}

function accountModeGroupName(platform: AccountSharePlatform): string {
  return ACCOUNT_MODE_GROUP_NAME_BY_PLATFORM.value[platform]
}

function isUsableModeApiKey(key: ApiKey, accountModeGroupID: number): boolean {
  if (Number(key.group_id || 0) !== accountModeGroupID || key.status !== 'active') return false

  if (key.expires_at) {
    const expiresAtMs = Date.parse(key.expires_at)
    if (!Number.isFinite(expiresAtMs) || expiresAtMs <= nowMs.value) return false
  }

  const quota = Number(key.quota)
  const quotaUsed = Number(key.quota_used)
  if (!Number.isFinite(quota) || !Number.isFinite(quotaUsed)) return false
  return quota <= 0 || quotaUsed < quota
}

function clearInvalidSelectedModeApiKeys(platform: AccountSharePlatform, keys: ApiKey[]): void {
  const usableIDs = new Set(keys.map(key => key.id))
  for (const listing of knownListings.value) {
    if (listingPlatform(listing) !== platform) continue
    const selectedID = Number(selectedKeyByListing[listing.id] || 0)
    if (selectedID > 0 && !usableIDs.has(selectedID)) selectedKeyByListing[listing.id] = 0
  }
}

function modeApiKeysForPlatform(platform: AccountSharePlatform): ApiKey[] {
  const accountModeGroupID = modeGroupIDsByPlatform[platform]
  return (modeApiKeysByPlatform[platform] || [])
    .filter(key => isUsableModeApiKey(key, accountModeGroupID))
}

function modeApiKeysForListing(listing: AccountShareListing): ApiKey[] {
  return modeApiKeysForPlatform(listingPlatform(listing))
}

function modeApiKeySelectOptionsForListing(listing: AccountShareListing): SelectOption[] {
  return modeApiKeysForListing(listing).map(key => ({
    value: key.id,
    label: modeKeyLabel(key)
  }))
}

function modeKeysLoadingForPlatform(platform: AccountSharePlatform): boolean {
  return modeKeysLoadingByPlatform[platform]
}

function modeKeysLoadedForPlatform(platform: AccountSharePlatform): boolean {
  return modeKeysLoadedByPlatform[platform]
}

function singleModeApiKeyForListing(listing: AccountShareListing): ApiKey | null {
  const keys = modeApiKeysForListing(listing)
  return keys.length === 1 ? keys[0] : null
}

function singleModeApiKeyLabelForListing(listing: AccountShareListing): string {
  const key = singleModeApiKeyForListing(listing)
  return key ? modeKeyLabel(key) : ''
}

const modeApiKeys = computed(() => modeApiKeysForPlatform(activeListingPlatform.value))
const modeKeysLoading = computed(() => modeKeysLoadingForPlatform(activeListingPlatform.value))
const modeKeysLoaded = computed(() => modeKeysLoadedForPlatform(activeListingPlatform.value))
const isAnyModeKeysLoading = computed(() =>
  ACCOUNT_SHARE_PLATFORM_OPTIONS.value.some(option => modeKeysLoadingForPlatform(option.value))
)

function modeApiKeyPlaceholderForListing(listing: AccountShareListing): string {
  const platform = listingPlatform(listing)
  if (modeKeysLoadingForPlatform(platform)) return t('accountShare.card.loadingModeKeys')
  if (!modeKeysLoadedForPlatform(platform)) return t('accountShare.card.modeKeysNotLoaded')
  return t('accountShare.card.selectModeKey', { group: accountModeGroupName(listingPlatform(listing))  })
}
const pendingJoinIntent = computed(() => pendingJoinConfirmation.value?.intent ?? null)
const pendingJoinTerms = computed(() => pendingJoinIntent.value?.terms ?? null)
const pendingJoinIsOwnerSelfUse = computed(() => pendingJoinConfirmation.value?.ownerSelfUse === true)
const pendingJoinPlatform = computed(() => pendingJoinConfirmation.value?.platform ?? 'openai')
// API 聚合（APIKEY）房间的「未验证渠道」风险确认：以服务端下发的
// requires_unverified_ack 为准，平台字段兜底，防止旧版本响应漏字段时放行。
const pendingJoinRequiresUnverifiedAck = computed(() =>
  pendingJoinIntent.value?.requires_unverified_ack === true
  || pendingJoinPlatform.value === 'api_aggregation'
)
const joinUnverifiedAcknowledged = ref(false)
const pendingJoinApiKeyLabel = computed(() => pendingJoinConfirmation.value?.apiKeyLabel || '-')
const pendingJoinIdleTimeoutLabel = computed(() => formatIdleTimeoutSetting(pendingJoinConfirmation.value?.idleTimeoutMinutes ?? 0))
const joinDialogBusy = computed(() => joiningId.value !== null)
const pendingJoinExpired = computed(() => {
  const expiresAt = pendingJoinIntent.value?.expires_at
  if (!expiresAt) return true
  const expiresAtMs = Date.parse(expiresAt)
  return !Number.isFinite(expiresAtMs) || expiresAtMs <= nowMs.value
})
const pendingJoinCanSubmit = computed(() => {
  const intent = pendingJoinIntent.value
  if (!intent || joinDialogBusy.value) return false
  if (pendingJoinRequiresUnverifiedAck.value && !joinUnverifiedAcknowledged.value) return false
  return true
})
const pendingJoinVisibleModels = computed(() =>
  (pendingJoinTerms.value?.allowed_models || []).slice(0, MODEL_PREVIEW_LIMIT)
)
const pendingJoinHiddenModelCount = computed(() =>
  Math.max(0, (pendingJoinTerms.value?.allowed_models || []).length - MODEL_PREVIEW_LIMIT)
)
const pendingJoinHasOpenAIProtection = computed(() => {
  const terms = pendingJoinTerms.value
  return pendingJoinPlatform.value === 'openai' && Boolean(
    terms?.codex_cli_only ||
    Number(terms?.codex_5h_limit_percent || 0) > 0 ||
    Number(terms?.codex_7d_limit_percent || 0) > 0
  )
})
const pendingJoinHasAnthropicProtection = computed(() => {
  const terms = pendingJoinTerms.value
  return pendingJoinPlatform.value === 'anthropic' && Boolean(
    Number(terms?.anthropic_5h_limit_percent || 0) > 0 ||
    Number(terms?.anthropic_7d_limit_percent || 0) > 0
  )
})
const pendingJoinPriceWarnings = computed(() => {
  const terms = pendingJoinTerms.value
  if (!terms) return []
  if (pendingJoinIsOwnerSelfUse.value) return []
  const warnings: string[] = []
  if (Number(terms.rate_multiplier || 0) > 1) {
    warnings.push(t('accountShare.card.rateMultiplierWarning', { rate: formatNumber(terms.rate_multiplier)  }))
  }
  if (Number(terms.hourly_rate || 0) > EXPENSIVE_HOURLY_RATE) {
    warnings.push(t('accountShare.card.hourlyRateWarning', { hourlyRate: formatNumber(terms.hourly_rate) }))
  }
  return warnings
})
const endUseConfirmMessage = computed(() => {
  const apiKeyLabel = pendingEndUse.value ? formatApiKeyDisplayName(pendingEndUse.value.apiKeyName, pendingEndUse.value.apiKeyID, t('accountShare.card.currentKey')) : t('accountShare.card.currentKey')
  return t('accountShare.endUse.confirmEnd', { apiKeyLabel })
})

const eligibleOwnedAccounts = computed(() => (
  ownedAccounts.value
    .filter((account) => {
      if (account.platform.trim().toLowerCase() !== createPlatform.value) return false
      if (account.status !== 'active' || !account.schedulable) return false
      if (!Number.isFinite(Number(account.concurrency)) || Number(account.concurrency) <= 0) return false
      // Account level is only meaningful for the subscription based OpenAI and
      // Anthropic pools.  Kimi, Zhipu, DeepSeek and MiniMax API accounts do not
      // expose a level, so an empty/unknown value must not hide valid accounts.
      if ((createPlatform.value === 'openai' || createPlatform.value === 'anthropic')
        && (!account.account_level || account.account_level.trim().toLowerCase() === 'unknown')) return false
      if (account.external_placement && account.external_placement.state !== 'active') return false
      const placementTarget = resolveAccountExternalPlacementTarget(account)
      if (placementTarget === 'room') {
        const boundRoomID = Number(
          account.account_share_mode_listing_id
          || account.external_placement?.room_id
          || 0
        )
        if (boundRoomID > 0) return false
      }
      return true
    })
    .sort((left, right) => left.name.localeCompare(right.name, 'zh-CN') || left.id - right.id)
))

const selectedOwnedAccount = computed(() => (
  eligibleOwnedAccounts.value.find((account) => account.id === selectedOwnedAccountID.value) || null
))

const ownedAccountSelectionHint = computed(() => {
  if (ownedAccountsLoading.value) return t('accountShare.roomCreate.loadingAccounts')
  if (ownedAccountsError.value) return ownedAccountsError.value
  if (selectedOwnedAccount.value) {
    return selectedOwnedAccount.value.external_placement?.target === 'public_pool'
      || (!selectedOwnedAccount.value.external_placement && selectedOwnedAccount.value.share_mode === 'public')
      ? t('accountShare.roomCreate.publicPoolNote', { id: selectedOwnedAccount.value.id })
      : t('accountShare.roomCreate.keepCredentials', { id: selectedOwnedAccount.value.id })
  }
  if (eligibleOwnedAccounts.value.length === 0) {
    return t('accountShare.roomCreate.noEligibleAccounts')
  }
  return t('accountShare.roomCreate.eligibleCount', { count: eligibleOwnedAccounts.value.length  })
})

const availableSeatCount = computed(() => listings.value.reduce((total, listing) => {
  if (listing.deleted || listing.status !== 'active') return total
  return total + Math.max(0, listing.seat_limit - listing.active_seats)
}, 0))
const activeSeatCount = computed(() => listings.value.reduce((total, listing) => total + Math.max(0, Number(listing.active_seats || 0)), 0))
const createRoomCapabilityHint = computed(() => {
  if (capabilitiesLoading.value) return t('accountShare.roomCreate.loadingCapabilities')
  const blocker = capabilities.value?.capability_blockers[0]
  if (blocker) return capabilityBlockerMessage(blocker)
  if (capabilitiesError.value) return capabilitiesError.value
  return t('accountShare.roomCreate.createManagedRoom')
})
const mainViewTab = computed(() => isMembershipHistoryView.value ? 'using' : isArchiveView.value ? 'mine' : activeFilter.value.tab)
const isManagementView = computed(() => activeFilter.value.tab === 'mine' || activeFilter.value.tab === 'archive')
const isMembershipHistoryView = computed(() => activeFilter.value.tab === 'history')
const isArchiveView = computed(() => activeFilter.value.tab === 'archive')
const currentViewLoading = computed(() =>
  isMembershipHistoryView.value ? membershipHistoryLoading.value : loading.value
)
const activeAdvancedFilterCount = computed(() => {
  let count = 0
  if (listingFilters.status !== '') count += 1
  if (isOpenAIListingPlatform.value && listingFilters.accountLevel !== 'all') count += 1
  count += listingFilters.sortKeys.length
  if (listingFilters.seatLimits.length > 0) count += 1
  if (listingFilters.featureTags.length > 0) count += 1
  if (listingFilters.models.length > 0) count += 1
  return count
})
const hasAdvancedFilters = computed(() => activeAdvancedFilterCount.value > 0)
const activeResultFilterCount = computed(() => {
  const searchCount = searchQuery.value.trim() !== '' ? 1 : 0
  const ownerCount = selectedOwnerID.value > 0 ? 1 : 0
  return isArchiveView.value
    ? searchCount
    : activeAdvancedFilterCount.value + searchCount + ownerCount
})
const hasResultFilters = computed(() =>
  searchQuery.value.trim() !== ''
  || (!isArchiveView.value && (selectedOwnerID.value > 0 || hasAdvancedFilters.value))
)
const maxPerUserConcurrency = computed(() => MAX_PER_USER_CONCURRENCY)
const editMaxPerUserConcurrency = computed(() => MAX_PER_USER_CONCURRENCY)
const accountNameValidationMessage = computed(() =>
  validateAccountName(createForm.name, undefined, Number(authStore.user?.id || 0))
)
const editAccountNameValidationMessage = computed(() =>
  validateAccountName(
    editForm.name,
    editingConfigListing.value?.id,
    Number(editingConfigListing.value?.owner_user_id || 0)
  )
)
const concurrencyValidationMessage = computed(() => {
  const concurrency = Number(createForm.concurrency)
  if (!Number.isFinite(concurrency) || concurrency < 1) return t('accountShare.roomEdit.concurrencyPositive')
  if (!Number.isInteger(concurrency)) return t('accountShare.roomEdit.concurrencyInteger')
  if (concurrency > MAX_ACCOUNT_CONCURRENCY) return t('accountShare.roomEdit.concurrencyMax', { max: MAX_ACCOUNT_CONCURRENCY })
  return ''
})
const editConcurrencyValidationMessage = computed(() => {
  const concurrency = Number(editForm.concurrency)
  if (!Number.isFinite(concurrency) || concurrency < 1) return t('accountShare.roomEdit.concurrencyPositive')
  if (!Number.isInteger(concurrency)) return t('accountShare.roomEdit.concurrencyInteger')
  if (concurrency > MAX_ACCOUNT_CONCURRENCY) return t('accountShare.roomEdit.concurrencyMax', { max: MAX_ACCOUNT_CONCURRENCY })
  return ''
})
const perUserConcurrencyValidationMessage = computed(() =>
  validatePerUserConcurrencyValue(createForm.per_user_concurrency)
)
const editPerUserConcurrencyValidationMessage = computed(() =>
  validatePerUserConcurrencyValue(editForm.per_user_concurrency)
)
// 保存按钮被禁用时的具体原因：必填项缺失（如"本次修改原因"）会把按钮置灰，
// 若不展示原因，用户只会看到"点了没反应"。
const configEditBlockedReason = computed(() => {
  if (savingConfigEdit.value) return ''
  if (editVersionConflict.value) return ''
  if (!editReason.value.trim()) return t('accountShare.roomEdit.reasonRequired')
  if (editAllowedModels.value.length === 0) return t('accountShare.roomEdit.keepOneModel')
  if (editPerUserConcurrencyValidationMessage.value) return editPerUserConcurrencyValidationMessage.value
  return ''
})
const perUserConcurrencyLimitTip = computed(() =>
  buildPerUserConcurrencyLimitTip(maxPerUserConcurrency.value)
)
const editPerUserConcurrencyLimitTip = computed(() =>
  buildPerUserConcurrencyLimitTip(editMaxPerUserConcurrency.value)
)
const concurrencyNotice = computed(() => {
  if (concurrencyValidationMessage.value || perUserConcurrencyValidationMessage.value) return ''
  return perUserConcurrencyLimitTip.value
})
const editConcurrencyNotice = computed(() => {
  if (editConcurrencyValidationMessage.value || editPerUserConcurrencyValidationMessage.value) return ''
  return editPerUserConcurrencyLimitTip.value
})
// 按钮可用性直接复用提交时的权威校验，避免与 validateCreateConfig 漂移：
// 旧实现只检查其中一个子集（模型/并发），却漏掉了 seat_limit 越界、倍率/时费/
// 低消/准入余额为负、以及 5h/7d 保护百分比越界等项，导致按钮可点但提交必失败。
const canCreateRoomFromOwnedAccount = computed(() =>
  selectedOwnedAccount.value !== null
  && validateCreateConfig() === ''
)

const draftDiscardMessage = computed(() => (
  pendingDraftDiscardTarget.value === 'config'
    ? t('accountShare.roomEdit.discardConfig')
    : t('accountShare.roomEdit.discardCreate')
))

function roomEditBlockerLabels(state: AccountShareRoomManagementState): string[] {
  const blockers = state.blockers
  const labels: string[] = []
  if (blockers.active_membership_count > 0) labels.push(t('accountShare.blockers.activeMembership', { count: blockers.active_membership_count  }))
  if (blockers.ending_membership_count > 0) labels.push(t('accountShare.blockers.endingMembership', { count: blockers.ending_membership_count  }))
  if (blockers.in_flight_request_count > 0) labels.push(t('accountShare.blockers.inFlight', { count: blockers.in_flight_request_count  }))
  if (blockers.pending_billing_intent_count > 0) labels.push(t('accountShare.blockers.pendingBilling', { count: blockers.pending_billing_intent_count  }))
  if (blockers.synchronous_billing_pending_count > 0) labels.push(t('accountShare.blockers.syncBilling', { count: blockers.synchronous_billing_pending_count  }))
  if (blockers.conflicting_operation || state.pending_operation_id) labels.push(t('accountShare.blockers.lifecycleOp'))
  return labels
}

function roomRequiresForceEdit(
  _listing: AccountShareListing,
  state: AccountShareRoomManagementState
): boolean {
  const blockers = state.blockers
  // active_seats / ending_seats 是「消费者」口径（SQL 里排除了房主自己的席位），
  // blockers.active_membership_count / ending_membership_count 则把房主自用也算进去。
  // 房主自用是免费且被显式支持的常态，用后者判定会让房主一边用自己的房间、
  // 一边永远改不了它的配置——自己把自己锁死。这里与后端编辑准入口径保持一致。
  return !['active', 'paused'].includes(state.lifecycle_status)
    || state.active_seats > 0
    || state.ending_seats > 0
    || blockers.in_flight_request_count > 0
    || blockers.pending_billing_intent_count > 0
    || blockers.synchronous_billing_pending_count > 0
    || blockers.conflicting_operation
    || Boolean(state.pending_operation_id)
}

const forceEditConfirmMessage = computed(() => {
  const listing = pendingForceEditListing.value
  if (!listing) return ''
  const state = pendingForceEditManagementState.value
  const status = state?.lifecycle_status || listing.status
  const activeSeats = state?.active_seats ?? listing.active_seats
  const seatLimit = state?.seat_limit ?? listing.seat_limit
  const blockers = state ? roomEditBlockerLabels(state) : []
  const blockerText = blockers.length > 0 ? t('accountShare.blockers.suffix', { blockers: blockers.join('、') }) : ''
  return t('accountShare.roomEdit.forceEditNotice', { status: statusLabel(status), activeSeats, seatLimit, blockerText })
})

const isKeyResolutionMode = computed(() => routeQueryString(route.query.mode) === 'resolve-key-binding')
const keyResolutionApiKeyID = computed(() => {
  const value = Number(routeQueryString(route.query.api_key_id))
  return Number.isSafeInteger(value) && value > 0 ? value : 0
})
const keyResolutionApiKeyName = computed(() => routeQueryString(route.query.api_key_name).trim())
const keyResolutionKeyLabel = computed(() => keyResolutionApiKeyName.value || (keyResolutionApiKeyID.value > 0 ? `API Key #${keyResolutionApiKeyID.value}` : t('accountShare.keyResolution.specifiedKey')))
const keyResolutionActiveCount = computed(() => keyResolutionBindingStatus.value?.active_count ?? 0)
const keyResolutionEndingCount = computed(() => keyResolutionBindingStatus.value?.ending_count ?? 0)
const keyResolutionAllClear = computed(() =>
  keyResolutionLoaded.value &&
  !keyResolutionLoading.value &&
  !keyResolutionError.value &&
  keyResolutionBindingStatus.value !== null &&
  keyResolutionBindingStatus.value.blocking_count === 0
)
const keyResolutionListingIDs = computed(() => new Set(keyResolutionMemberships.value.map(item => Number(item.listing_id))))
const keyResolutionPanelToneClass = computed(() => ({
  'key-resolution-panel-loading': keyResolutionLoading.value,
  'key-resolution-panel-error': Boolean(keyResolutionError.value),
  'key-resolution-panel-clear': keyResolutionAllClear.value
}))
const keyResolutionStatusMessage = computed(() => {
  if (keyResolutionLoading.value) return t('accountShare.keyResolution.checking', { label: keyResolutionKeyLabel.value  })
  if (keyResolutionError.value) return keyResolutionError.value
  if (keyResolutionAllClear.value) return t('accountShare.keyResolution.allClear')
  return t('accountShare.keyResolution.endUsageFirst')
})
const displayedListings = computed(() => isKeyResolutionMode.value ? keyResolutionListings.value : listings.value)
const mySpendAccountOptions = computed(() =>
  mySpendPickerSource.value === 'using'
    ? mySpendUsingAccountOptions.value
    : mySpendHistoryAccountOptions.value
)
const mySpendActivePickerPagination = computed(() =>
  mySpendPickerSource.value === 'using' ? mySpendUsingPagination : mySpendHistoryPagination
)
const mySpendHistorySelection = computed(() => mySpendSelectedOption.value?.source === 'history')
const mySpendAccountPickerTitle = computed(() => {
  if (mySpendAccountsLoading.value && mySpendUsingPagination.total === 0 && mySpendHistoryPagination.total === 0) {
    return t('accountShare.mySpend.loading')
  }
  if (mySpendUsingPagination.total === 0 && mySpendHistoryPagination.total === 0) return t('accountShare.mySpend.noRecords')
  return t('accountShare.mySpend.counts', { usingCount: countLabel(mySpendUsingPagination.total, mySpendUsingPagination.totalExact), historyCount: mySpendHistoryPagination.total  })
})
const mySpendMetrics = computed<MySpendMetric[]>(() => {
  const summary = mySpendSummary.value
  if (!summary) return []
  return [
    {
      key: 'total',
      label: t('accountShare.mySpend.totalCost'),
      value: formatSpendCost(summary.total_cost),
      note: mySpendRangeLabel(summary.range),
      icon: 'dollar',
      tone: 'total'
    },
    {
      key: 'request',
      label: t('accountShare.mySpend.requestCost'),
      value: formatSpendCost(summary.request_cost),
      note: t('accountShare.mySpend.requestCount', { count: formatWholeNumber(summary.request_count)  }),
      icon: 'creditCard',
      tone: 'request'
    },
    {
      key: 'hourly',
      label: t('accountShare.mySpend.hourlyNetCost'),
      value: formatSpendCost(summary.hourly_net_cost),
      note: t('accountShare.mySpend.hourlyNote', { charged: formatSpendCost(summary.hourly_charge), refunded: formatSpendCost(summary.hourly_refund + summary.hourly_waiver_refund)  }),
      icon: 'clock',
      tone: 'hourly'
    },
    {
      key: 'tokens',
      label: t('accountShare.mySpend.tokenTotal'),
      value: formatWholeNumber(summary.total_tokens),
      note: t('accountShare.mySpend.tokenNote', { input: formatWholeNumber(summary.input_tokens), output: formatWholeNumber(summary.output_tokens)  }),
      icon: 'chart',
      tone: 'usage'
    }
  ]
})
const modelFilterOptions = computed(() => {
  const models = new Set<string>([
    ...listingFilters.models
  ])
  for (const listing of knownListings.value) {
    if (listingPlatform(listing) !== activeListingPlatform.value) continue
    for (const model of listing.allowed_models) {
      const value = model.trim()
      if (value) models.add(value)
    }
  }
  for (const listing of listings.value) {
    if (listingPlatform(listing) !== activeListingPlatform.value) continue
    for (const model of listing.allowed_models) {
      const value = model.trim()
      if (value) models.add(value)
    }
  }
  return Array.from(models).sort((a, b) => a.localeCompare(b))
})
const recommendationModelOptions = computed(() => {
  const models = new Set<string>(modelFilterOptions.value)
  const current = recommendationForm.model.trim()
  if (current) models.add(current)
  return Array.from(models).sort((a, b) => a.localeCompare(b))
})
const recommendationKeyOptions = computed(() => modeApiKeys.value)
const recommendationCandidates = computed<AccountShareRecommendationCandidate[]>(() => {
  const items = recommendationResult.value?.items || []
  return [...items].sort(compareRecommendationCandidates)
})
const recommendationBest = computed<AccountShareRecommendationCandidate | null>(() => recommendationCandidates.value[0] || null)
const recommendationPageCount = computed(() => Math.max(1, Math.ceil(recommendationCandidates.value.length / ACCOUNT_SHARE_RECOMMENDATION_PAGE_SIZE)))
const recommendationPagedCandidates = computed<AccountShareRecommendationCandidate[]>(() => {
  const safePage = Math.min(Math.max(recommendationPage.value, 1), recommendationPageCount.value)
  const start = (safePage - 1) * ACCOUNT_SHARE_RECOMMENDATION_PAGE_SIZE
  return recommendationCandidates.value.slice(start, start + ACCOUNT_SHARE_RECOMMENDATION_PAGE_SIZE)
})
const recommendationPageRangeText = computed(() => {
  const total = recommendationCandidates.value.length
  if (total === 0) return t('accountShare.mySpend.noDisplayable')
  const safePage = Math.min(Math.max(recommendationPage.value, 1), recommendationPageCount.value)
  const start = (safePage - 1) * ACCOUNT_SHARE_RECOMMENDATION_PAGE_SIZE + 1
  const end = Math.min(start + ACCOUNT_SHARE_RECOMMENDATION_PAGE_SIZE - 1, total)
  return t('accountShare.mySpend.pageRange', { start, end, total })
})
const recommendationInputSummary = computed(() => {
  const input = recommendationResult.value?.input
  if (!input) return ''
  const activeHours = normalizeRecommendationActiveHours(input.active_hours)
  const requestsPerHour = activeHours > 0 ? input.request_count / activeHours : input.request_count
  return t('accountShare.estimate.requestSummary', { count: input.request_count, hours: formatNumber(activeHours), model: input.model, perHour: formatNumber(requestsPerHour)  })
})
const modelFilterSummary = computed(() => {
  if (listingFilters.models.length === 0) return t('admin.usage.allModels')
  if (listingFilters.models.length === 1) return listingFilters.models[0]
  return t('accountShare.filterChips.modelsSelected', { count: listingFilters.models.length  })
})
const seatFilterSummary = computed(() => {
  if (listingFilters.seatLimits.length === 0) return t('accountShare.filterChips.allSeats')
  if (listingFilters.seatLimits.length === 1) return t('accountShare.filterChips.seatCount', { count: listingFilters.seatLimits[0]  })
  return t('accountShare.filterChips.seatsSelected', { count: listingFilters.seatLimits.length  })
})
const featureTagFilterSummary = computed(() => {
  if (listingFilters.featureTags.length === 0) return t('accountShare.filterChips.allTags')
  if (listingFilters.featureTags.length === 1) {
    return visibleListingFeatureTagOptions.value.find(option => option.value === listingFilters.featureTags[0])?.label || t('accountShare.filterChips.tagsSelected')
  }
  return t('accountShare.filterChips.tagsCount', { count: listingFilters.featureTags.length  })
})
const statusFilterSummary = computed(() => (
  listingStatusFilterOptions.value.find(option => option.value === listingFilters.status)?.label || t('accountShare.statusFilter.default')
))
const accountLevelFilterSummary = computed(() => (
  accountLevelFilterOptions.value.find(option => option.value === listingFilters.accountLevel)?.label || t('accountShare.levelFilter.all')
))
const selectedSortOptions = computed(() =>
  listingFilters.sortKeys
    .map(key => listingSortOptions.value.find(option => option.key === key))
    .filter((option): option is ListingSortOption => Boolean(option))
)
const activeFilterChips = computed<ActiveFilterChip[]>(() => {
  const chips: ActiveFilterChip[] = []
  if (selectedOwnerID.value > 0) {
    chips.push({
      key: `owner:${selectedOwnerID.value}`,
      label: t('accountShare.filterChips.owner', { name: selectedOwnerDisplayName.value || t('accountShare.common.userId', { id: selectedOwnerID.value }) }),
      remove: () => {
        selectedOwnerID.value = 0
        selectedOwnerDisplayName.value = ''
      }
    })
  }
  const statusOption = listingStatusFilterOptions.value.find(option => option.value === listingFilters.status)
  if (listingFilters.status !== '' && statusOption) {
    chips.push({
      key: `status:${listingFilters.status}`,
      label: t('accountShare.filterChips.status', { label: statusOption.label }),
      // 清除状态筛选语义：回到「默认状态」(空)。tab=all 下后端对空 status 兜底
      // available_only=true（仍只显示可用），mine/using 管理视图下空 status 即全量。
      remove: () => { listingFilters.status = '' }
    })
  }

  const levelOption = accountLevelFilterOptions.value.find(option => option.value === listingFilters.accountLevel)
  if (isOpenAIListingPlatform.value && listingFilters.accountLevel !== 'all' && levelOption) {
    chips.push({
      key: `level:${listingFilters.accountLevel}`,
      label: t('accountShare.filterChips.level', { label: levelOption.label }),
      remove: () => { listingFilters.accountLevel = 'all' }
    })
  }

  for (const [index, option] of selectedSortOptions.value.entries()) {
    chips.push({
      key: `sort:${option.key}`,
      label: t('accountShare.filterChips.sort', { index: index + 1, label: option.label }),
      remove: () => removeListingSort(option.key)
    })
  }

  for (const seat of listingFilters.seatLimits) {
    chips.push({
      key: `seat:${seat}`,
      label: t('accountShare.filterChips.seatChip', { seat }),
      remove: () => removeSeatFilter(seat)
    })
  }

  for (const tag of listingFilters.featureTags) {
    const option = visibleListingFeatureTagOptions.value.find(item => item.value === tag)
    chips.push({
      key: `tag:${tag}`,
      label: option?.label || tag,
      remove: () => removeFeatureTagFilter(tag)
    })
  }

  for (const model of listingFilters.models) {
    chips.push({
      key: `model:${model}`,
      label: model,
      remove: () => removeModelFilter(model)
    })
  }

  return chips
})

function normalizeAccountName(name: string): string {
  return name.trim().toLowerCase()
}

function hasKnownAccountName(name: string, ownerUserID: number, excludeAccountID?: number): boolean {
  const normalizedName = normalizeAccountName(name)
  if (!normalizedName || !Number.isSafeInteger(ownerUserID) || ownerUserID <= 0) return false
  return [...knownListings.value, ...listings.value].some(listing => {
    if (excludeAccountID && listing.id === excludeAccountID) return false
    if (listing.owner_user_id !== ownerUserID) return false
    return normalizeAccountName(listing.room_name || listing.account_name || '') === normalizedName
  })
}

function suggestedAccountName(platform: AccountSharePlatform = createPlatform.value): string {
  const baseName = ACCOUNT_NAME_BASE_BY_PLATFORM.value[platform]
  const ownerUserID = Number(authStore.user?.id || 0)
  for (let index = 1; index <= 999; index += 1) {
    const candidate = index === 1 ? baseName : `${baseName}${index}`
    if (!hasKnownAccountName(candidate, ownerUserID)) return candidate
  }
  return `${baseName}${Date.now()}`
}

function validateAccountName(name: string, excludeAccountID?: number, ownerUserID = 0): string {
  const value = name.trim()
  if (!value) return t('accountShare.roomForm.nameRequired')
  // 只校验 trim 之后的内容：首尾空格由 trim 消化，提交给后端的也是 trim 后的值。
  // 校验原串会让一个尾随空格把整次保存打回，而用户可能压根没在改房间名。
  if (/\s/.test(value)) return t('accountShare.roomForm.nameNoSpaces')
  if (Array.from(value).length > 100) return t('accountShare.roomForm.nameTooLong')
  if (hasKnownAccountName(value, ownerUserID, excludeAccountID)) return t('accountShare.roomForm.nameExists')
  return ''
}

function buildPerUserConcurrencyLimitTip(maxPerUser: number): string {
  return t('accountShare.roomForm.perUserConcurrencyHelp', { maxPerUser })
}

function validatePerUserConcurrencyValue(value: unknown): string {
  const perUserConcurrency = Number(value)
  if (!Number.isFinite(perUserConcurrency) || perUserConcurrency < 1) return t('accountShare.roomForm.perUserPositive')
  if (!Number.isInteger(perUserConcurrency)) return t('accountShare.roomForm.perUserInteger')
  if (perUserConcurrency > MAX_PER_USER_CONCURRENCY) {
    return t('accountShare.roomForm.perUserMax', { max: MAX_PER_USER_CONCURRENCY })
  }
  return ''
}

function parseAllowedModels(): string[] {
  return normalizeAllowedModelList(allowedModels.value)
}

function normalizeAllowedModelList(models: string[]): string[] {
  return models
    .map(item => item.trim())
    .filter(Boolean)
}

function normalizeModelFilterValue(model: string): string {
  return model.trim()
}

function hasModelFilter(model: string): boolean {
  const normalized = normalizeModelFilterValue(model).toLowerCase()
  if (!normalized) return false
  return listingFilters.models.some(item => item.toLowerCase() === normalized)
}

function addModelFilter(model: string): void {
  const normalized = normalizeModelFilterValue(model)
  if (!normalized || hasModelFilter(normalized)) return
  listingFilters.models.push(normalized)
}

function toggleModelFilter(model: string): void {
  if (hasModelFilter(model)) {
    removeModelFilter(model)
    return
  }
  addModelFilter(model)
}

function removeModelFilter(model: string): void {
  const normalized = normalizeModelFilterValue(model).toLowerCase()
  const index = listingFilters.models.findIndex(item => item.toLowerCase() === normalized)
  if (index >= 0) listingFilters.models.splice(index, 1)
}

function buildListingSortKey(sortBy: AccountShareListingSortBy, sortOrder: AccountShareListingSortOrder): ListingSortKey {
  return `${sortBy}:${sortOrder}` as ListingSortKey
}

function clearListingSorts(): void {
  listingFilters.sortKeys = []
  closeFilterPopover()
}

function findSortOption(key: ListingSortKey): ListingSortOption | undefined {
  return listingSortOptions.value.find(option => option.key === key)
}

function sortFieldIndex(sortBy: AccountShareListingSortBy): number {
  return listingFilters.sortKeys.findIndex(key => findSortOption(key)?.sortBy === sortBy)
}

function isSortFieldActive(sortBy: AccountShareListingSortBy): boolean {
  return sortFieldIndex(sortBy) >= 0
}

function activeSortOrder(sortBy: AccountShareListingSortBy): AccountShareListingSortOrder | null {
  const key = listingFilters.sortKeys[sortFieldIndex(sortBy)]
  if (!key) return null
  return findSortOption(key)?.sortOrder || null
}

function activeSortDirectionLabel(option: ListingSortFieldOption): string {
  const sortOrder = activeSortOrder(option.sortBy)
  if (!sortOrder) return ''
  return sortOrder === 'asc' ? option.ascLabel : option.descLabel
}

function sortPriorityLabel(sortBy: AccountShareListingSortBy): string {
  const index = sortFieldIndex(sortBy)
  return index >= 0 ? `#${index + 1}` : ''
}

function sortDirectionIcon(sortBy: AccountShareListingSortBy): 'sort' | 'arrowUp' | 'arrowDown' {
  const sortOrder = activeSortOrder(sortBy)
  if (sortOrder === 'asc') return 'arrowUp'
  if (sortOrder === 'desc') return 'arrowDown'
  return 'sort'
}

function toggleListingSortField(sortBy: AccountShareListingSortBy): void {
  const activeIndex = sortFieldIndex(sortBy)
  const nextSortOrder: AccountShareListingSortOrder = activeSortOrder(sortBy) === 'asc' ? 'desc' : 'asc'
  const nextSortKey = buildListingSortKey(sortBy, nextSortOrder)
  if (activeIndex >= 0) {
    listingFilters.sortKeys.splice(activeIndex, 1, nextSortKey)
  } else {
    listingFilters.sortKeys.push(nextSortKey)
  }
  closeFilterPopover()
}

function removeListingSort(key: ListingSortKey): void {
  const index = listingFilters.sortKeys.indexOf(key)
  if (index >= 0) listingFilters.sortKeys.splice(index, 1)
}

function sortFieldButtonTitle(option: ListingSortFieldOption): string {
  const sortOrder = activeSortOrder(option.sortBy)
  const priority = sortPriorityLabel(option.sortBy)
  if (!sortOrder) return t('accountShare.sortPicker.add', { label: option.label, asc: option.ascLabel, index: listingFilters.sortKeys.length + 1  })
  const currentLabel = sortOrder === 'asc' ? option.ascLabel : option.descLabel
  const nextLabel = sortOrder === 'asc' ? option.descLabel : option.ascLabel
  return t('accountShare.sortPicker.toggle', { priority: priority, label: option.label, current: currentLabel, next: nextLabel })
}

function toggleFilterPopover(popover: ListingFilterPopover): void {
  openFilterPopover.value = openFilterPopover.value === popover ? null : popover
}

function filterTriggerFor(popover: ListingFilterPopover): HTMLButtonElement | null {
  switch (popover) {
    case 'status':
      return statusFilterTriggerRef.value
    case 'level':
      return levelFilterTriggerRef.value
    case 'seat':
      return seatFilterTriggerRef.value
    case 'feature':
      return featureFilterTriggerRef.value
    case 'model':
      return modelFilterTriggerRef.value
  }
}

function closeFilterPopover(restoreFocus = false): void {
  const closingPopover = openFilterPopover.value
  openFilterPopover.value = null
  if (restoreFocus && closingPopover) {
    void nextTick(() => {
      filterTriggerFor(closingPopover)?.focus()
    })
  }
}

function handleFilterPopoverEscape(): void {
  if (!openFilterPopover.value) return
  closeFilterPopover(true)
}

function handleFilterPanelDocumentClick(event: MouseEvent): void {
  const target = event.target
  if (!(target instanceof Node)) return
  if (filterPanelRef.value?.contains(target)) return
  closeFilterPopover()
}

function setListingStatusFilter(status: ListingStatusFilterValue): void {
  listingFilters.status = status
  closeFilterPopover(true)
}

function setAccountLevelFilter(level: AccountLevelFilterValue): void {
  listingFilters.accountLevel = level
  closeFilterPopover(true)
}

function toggleSeatFilter(seat: number): void {
  const index = listingFilters.seatLimits.indexOf(seat)
  if (index >= 0) {
    listingFilters.seatLimits.splice(index, 1)
    return
  }
  listingFilters.seatLimits.push(seat)
  listingFilters.seatLimits.sort((a, b) => a - b)
}

function removeSeatFilter(seat: number): void {
  const index = listingFilters.seatLimits.indexOf(seat)
  if (index >= 0) listingFilters.seatLimits.splice(index, 1)
}

function toggleFeatureTagFilter(tag: AccountShareListingFeatureTag): void {
  if (!visibleListingFeatureTagOptions.value.some(option => option.value === tag)) return
  const index = listingFilters.featureTags.indexOf(tag)
  if (index >= 0) {
    listingFilters.featureTags.splice(index, 1)
    return
  }
  if (tag === 'codex_cli_only') {
    removeFeatureTagFilter('non_codex_cli_only')
  } else if (tag === 'non_codex_cli_only') {
    removeFeatureTagFilter('codex_cli_only')
  }
  listingFilters.featureTags.push(tag)
}

function removeFeatureTagFilter(tag: AccountShareListingFeatureTag): void {
  const index = listingFilters.featureTags.indexOf(tag)
  if (index >= 0) listingFilters.featureTags.splice(index, 1)
}

function addModelFilterFromInput(): void {
  addModelFilter(modelFilterInput.value)
  modelFilterInput.value = ''
}

function buildListingFilters(tab: AccountShareListingTab = activeFilter.value.tab): AccountShareListingFilters {
  const result: AccountShareListingFilters = {
    tab,
    platform: activeListingPlatform.value
  }
  const search = searchQuery.value.trim()
  if (search) result.search = search
  if (tab === 'archive') return result
  if (selectedOwnerID.value > 0) result.owner_user_id = selectedOwnerID.value
  // 「可用账号」只在浏览广场（tab=all）时翻译成 status=active + available_only。
  // mine/using 是号主/消费者管理视图：仓库按 owner_user_id 或 active membership 全量
  // 展示（paused/满员/账号临时不可用的房间也必须可见，便于维护与结束成员关系），
  // 不能带可用性过滤——否则号主看不到自己需维护的房间、消费者看不到自己正在使用的
  // 满员房间。其它显式状态（已上架/已下架等）在管理视图照常透传。
  if (listingFilters.status === 'available') {
    if (tab === 'all') {
      result.status = 'active'
      result.available_only = true
    }
  } else if (listingFilters.status !== '') {
    result.status = listingFilters.status
  }
  if (isOpenAIListingPlatform.value && listingFilters.accountLevel !== 'all') result.account_level = listingFilters.accountLevel
  if (listingFilters.models.length > 0) result.models = normalizeAllowedModelList(listingFilters.models)
  if (listingFilters.seatLimits.length > 0) result.seat_limits = [...listingFilters.seatLimits]
  const featureTags = listingFilters.featureTags.filter(tag =>
    visibleListingFeatureTagOptions.value.some(option => option.value === tag)
  )
  if (featureTags.length > 0) result.feature_tags = featureTags
  if (selectedSortOptions.value.length > 0) {
    result.sorts = [...listingFilters.sortKeys]
    const firstSort = selectedSortOptions.value[0]
    if (firstSort.sortBy && firstSort.sortOrder) {
      result.sort_by = firstSort.sortBy
      result.sort_order = firstSort.sortOrder
    }
  }
  return result
}

function clearSearchDebounceTimer(): void {
  if (searchDebounceTimer == null) return
  window.clearTimeout(searchDebounceTimer)
  searchDebounceTimer = null
}

function abortActiveListingsRequest(): void {
  listingsRequestSeq += 1
  if (listingsRequestController != null) {
    listingsRequestController.abort()
    listingsRequestController = null
  }
}

function abortMembershipHistoryRequest(): void {
  membershipHistoryRequestSeq += 1
  membershipHistoryRequestController?.abort()
  membershipHistoryRequestController = null
}


function formatAccountShareLoadError(error: unknown, fallback: string): string {
  const message = extractApiErrorMessage(error, fallback)
  if (/Request failed with status code 500/i.test(message)) {
    return t('accountShare.errors.api500')
  }
  if (/Network Error/i.test(message)) {
    return t('accountShare.errors.apiUnreachable')
  }
  return message
}

function applyListingFilters(): void {
  clearSearchDebounceTimer()
  closeFilterPopover()
  pagination.page = 1
  persistListingPreferences()
  void loadListings()
}

function resetListingFilters(): void {
  closeFilterPopover()
  // 重置状态筛选：浏览广场（tab=all）回到「可用账号」默认（只看可用房间，管理员
  // 除外——admin 重置回空看全量）；管理视图（mine/using/archive）回到空——号主/
  // 消费者需要看到正在使用的全部房间（含已下架、满员、账号临时不可用），不应被可用性过滤。
  const defaultStatus = activeFilter.value.tab === 'all' && authStore.user?.role !== 'admin' ? 'available' : ''
  listingFilters.status = defaultStatus
  listingFilters.accountLevel = 'all'
  listingFilters.sortKeys = []
  listingFilters.seatLimits = []
  listingFilters.featureTags = []
  listingFilters.models = []
  selectedOwnerID.value = 0
  selectedOwnerDisplayName.value = ''
  modelFilterInput.value = ''
  if (searchQuery.value !== '') {
    suppressNextSearchRefresh = true
    searchQuery.value = ''
  }
  clearSearchDebounceTimer()
  pagination.page = 1
  persistListingPreferences()
  void loadListings()
}

function handlePageChange(page: number): void {
  clearSearchDebounceTimer()
  pagination.page = page
  void loadListings()
}

function handleMembershipHistoryPageChange(page: number): void {
  membershipHistoryPagination.page = page
  void loadMembershipHistory()
}

function handleRoomGridCapacityChange(pageSize: number): void {
  if (isMembershipHistoryView.value || isKeyResolutionMode.value || pageSize === pagination.page_size) return
  clearSearchDebounceTimer()
  pagination.page_size = pageSize
  pagination.page = 1
  void loadListings()
}

function countLabel(total: number, exact: boolean): string {
  return exact ? String(total) : t('accountShare.estimate.atLeast', { total })
}

function formatNumber(value: number): string {
  return Number(value || 0).toFixed(4).replace(/\.?0+$/, '')
}

function formatRecommendationCost(value: number): string {
  const amount = Number(value || 0)
  if (!Number.isFinite(amount)) return '0'
  if (amount >= 1) return amount.toFixed(4).replace(/\.?0+$/, '')
  if (amount >= 0.0001) return amount.toFixed(6).replace(/\.?0+$/, '')
  return amount.toFixed(8).replace(/\.?0+$/, '')
}

function formatSpendCost(value: number): string {
  return formatRecommendationCost(value)
}

function formatWholeNumber(value: number): string {
  const amount = Math.trunc(Number(value || 0))
  return Number.isFinite(amount) ? amount.toLocaleString() : '0'
}

function normalizeRecommendationActiveHours(value: number): number {
  const activeHours = Number(value || 0)
  return Number.isFinite(activeHours) && activeHours > 0 ? activeHours : 1
}

function recommendationEstimatedHourlyCostForInput(candidate: AccountShareRecommendationCandidate, activeHours: number): number {
  const totalCost = Number(candidate.estimate.total_cost || 0)
  if (!Number.isFinite(totalCost)) return 0
  return totalCost / normalizeRecommendationActiveHours(activeHours)
}

function recommendationEstimatedHourlyCost(candidate: AccountShareRecommendationCandidate): number {
  const activeHours = normalizeRecommendationActiveHours(recommendationResult.value?.input.active_hours || recommendationForm.active_hours)
  return recommendationEstimatedHourlyCostForInput(candidate, activeHours)
}

function compareRecommendationCandidates(left: AccountShareRecommendationCandidate, right: AccountShareRecommendationCandidate): number {
  const activeHours = normalizeRecommendationActiveHours(recommendationResult.value?.input.active_hours || recommendationForm.active_hours)
  const leftHourlyCost = recommendationEstimatedHourlyCostForInput(left, activeHours)
  const rightHourlyCost = recommendationEstimatedHourlyCostForInput(right, activeHours)
  if (leftHourlyCost !== rightHourlyCost) return leftHourlyCost - rightHourlyCost
  const leftRequestCost = Number(left.estimate.request_cost || 0)
  const rightRequestCost = Number(right.estimate.request_cost || 0)
  if (leftRequestCost !== rightRequestCost) return leftRequestCost - rightRequestCost
  const leftHourlyNet = Number(left.estimate.hourly_net_cost || 0)
  const rightHourlyNet = Number(right.estimate.hourly_net_cost || 0)
  if (leftHourlyNet !== rightHourlyNet) return leftHourlyNet - rightHourlyNet
  return left.listing.id - right.listing.id
}

function setRecommendationPage(page: number): void {
  recommendationPage.value = Math.min(Math.max(Math.trunc(Number(page) || 1), 1), recommendationPageCount.value)
}

function recommendationRequestCostLabel(candidate: AccountShareRecommendationCandidate): string {
  const prefix = candidate.estimate.owner_self_use ? t('accountShare.estimate.selfUse') : ''
  return t('accountShare.estimate.billingTotal', { prefix, billingMode: recommendationBillingModeLabel(candidate.estimate.billing_mode) })
}

function recommendationHourlyCostText(candidate: AccountShareRecommendationCandidate): string {
  return candidate.estimate.owner_self_use ? t('accountShare.estimate.notCharged') : formatRecommendationCost(candidate.estimate.hourly_net_cost)
}

function recommendationUpfrontCostText(candidate: AccountShareRecommendationCandidate): string {
  return candidate.estimate.owner_self_use ? t('accountShare.estimate.notChecked') : formatRecommendationCost(candidate.estimate.upfront_required)
}

function recommendationOwnerSelfUseSummary(candidate: AccountShareRecommendationCandidate): string {
  const listing = candidate.listing
  return t('accountShare.estimate.selfUseNote', { effectiveRateMultiplier: formatNumber(candidate.estimate.effective_rate_multiplier), rateMultiplier: formatNumber(listing.rate_multiplier), hourlyRate: formatNumber(listing.hourly_rate), hourlyFeeWaiverMinimum: hourlyFeeWaiverLabel(listing.hourly_fee_waiver_minimum) })
}

function recommendationBillingModeLabel(mode: string): string {
  switch (mode) {
    case 'per_request':
      return t('availableChannels.pricing.billingModePerRequest')
    case 'image':
      return t('keys.openImagePlayground')
    case 'token':
      return 'Token'
    default:
      return mode || 'Token'
  }
}

function formatRating(value: number): string {
  return Number(value || 0).toFixed(1).replace(/\.0$/, '')
}

function listingRatingLabel(listing: AccountShareListing): string {
  const count = Number(listing.rating_count || 0)
  if (count <= 0) return t('accountShare.card.notRated')
  return t('accountShare.card.rating', { rating: formatRating(Number(listing.rating_avg || 0)), count: count })
}

function ownerDisplayName(listing: AccountShareListing | null | undefined): string {
  if (!listing) return ''
  return listing.owner_username || t('accountShare.card.ownerUser', { id: listing.owner_user_id  })
}


function hourlyFeeWaiverLabel(value?: number | null): string {
  const amount = Number(value || 0)
  if (!Number.isFinite(amount) || amount <= 0) return t('accountShare.card.notEnabled')
  return t('accountShare.card.perHour', { amount: formatNumber(amount) })
}

function formatIdleTimeoutSetting(minutes: number): string {
  const normalized = normalizeIdleTimeoutMinutes(minutes)
  if (normalized <= 0) return t('admin.accounts.privacyUnset')
  if (normalized < 60) return t('accountShare.card.minutes', { minutes: normalized })
  const hours = Math.floor(normalized / 60)
  const restMinutes = normalized % 60
  if (hours < 24) return restMinutes > 0 ? t('accountShare.format.hoursMinutes', { hours: hours, minutes: restMinutes }) : t('accountShare.format.hours', { hours })
  const days = Math.floor(hours / 24)
  const restHours = hours % 24
  const hourPart = restHours > 0 ? t('accountShare.format.restHours', { hours: restHours }) : ''
  const minutePart = restMinutes > 0 ? t('accountShare.format.restMinutes', { minutes: restMinutes }) : ''
  return t('accountShare.format.daysHoursMinutes', { days, hourPart, minutePart })
}


function formatDate(value?: string | null): string {
  const date = normalizeDateInput(value)
  return date ? date.toLocaleString() : '-'
}

function formatRelativeUntil(value?: string | null): string {
  const date = normalizeDateInput(value)
  if (!date) return '-'
  const diffMs = date.getTime() - nowMs.value
  if (diffMs <= 0) return t('common.now')
  const totalMinutes = Math.ceil(diffMs / 60_000)
  const days = Math.floor(totalMinutes / 1440)
  const hours = Math.floor((totalMinutes % 1440) / 60)
  const minutes = totalMinutes % 60
  if (days > 0) return t('accountShare.format.daysPart', { days }) + (hours > 0 ? t('accountShare.format.hoursPart', { hours }) : '')
  if (hours > 0) return t('accountShare.format.hoursPart', { hours }) + (minutes > 0 ? t('accountShare.format.minutesPart', { minutes }) : '')
  return t('accountShare.format.minutes', { minutes })
}

function formatCountdownUntil(value?: string | null): string {
  const date = normalizeDateInput(value)
  if (!date) return '-'
  return date.getTime() <= nowMs.value ? t('common.now') : t('accountShare.format.afterRelative', { time: formatRelativeUntil(value)  })
}

function formatDurationCompact(seconds?: number | null): string {
  const totalSeconds = Math.max(0, Math.floor(Number(seconds || 0)))
  if (totalSeconds <= 0) return t('common.now')
  const days = Math.floor(totalSeconds / 86_400)
  const hours = Math.floor((totalSeconds % 86_400) / 3_600)
  const minutes = Math.floor((totalSeconds % 3_600) / 60)
  if (days > 0) return t('accountShare.format.daysPart', { days }) + (hours > 0 ? t('accountShare.format.hoursPart', { hours }) : '')
  if (hours > 0) return t('accountShare.format.hoursPart', { hours }) + (minutes > 0 ? t('accountShare.format.minutesPart', { minutes }) : '')
  if (minutes > 0) return t('accountShare.format.minutes', { minutes })
  return t('accountShare.format.seconds', { seconds: totalSeconds })
}

function waiverProgressVisible(listing: AccountShareListing): boolean {
  const progress = listing.current_waiver_progress
  return Boolean(listing.current_membership_id && progress?.enabled)
}

function finiteNonNegativeNumber(value: unknown): number {
  const amount = Number(value || 0)
  return Number.isFinite(amount) && amount > 0 ? amount : 0
}

function currentWaiverProgressSnapshot(listing: AccountShareListing): WaiverProgressSnapshot | null {
  const progress = listing.current_waiver_progress
  if (!progress?.enabled) return null

  const serverNow = normalizeDateInput(progress.now)
  const windowStart = normalizeDateInput(progress.window_start)
  const windowEnd = normalizeDateInput(progress.window_end)
  const receivedAt = Number((listing as AccountShareListingWithClientMeta).waiver_progress_received_at_ms || 0)
  const baselineNowMs = serverNow?.getTime() || receivedAt
  const effectiveNowMs = baselineNowMs > 0 && receivedAt > 0
    ? baselineNowMs + Math.max(0, nowMs.value - receivedAt)
    : nowMs.value
  const windowStartMs = windowStart?.getTime()
  const windowEndMs = windowEnd?.getTime()
  const effectiveEndMs = typeof windowEndMs === 'number' ? Math.min(effectiveNowMs, windowEndMs) : effectiveNowMs
  const elapsedMs = typeof windowStartMs === 'number'
    ? Math.max(0, effectiveEndMs - windowStartMs)
    : Math.max(0, finiteNonNegativeNumber(progress.elapsed_seconds) * 1000)
  const waiverMinimum = finiteNonNegativeNumber(progress.waiver_minimum)
  const requiredAmount = waiverMinimum > 0
    ? waiverMinimum * elapsedMs / 3_600_000
    : finiteNonNegativeNumber(progress.required_amount)
  const usageAmount = finiteNonNegativeNumber(progress.usage_amount)
  const remainingAmount = Math.max(0, requiredAmount - usageAmount)
  const progressPercent = requiredAmount > 0 ? Math.min(100, usageAmount * 100 / requiredAmount) : 0
  const status = requiredAmount > 0 && usageAmount >= requiredAmount ? 'met' : 'in_progress'
  const hourlyRate = finiteNonNegativeNumber(progress.hourly_rate)
  const estimatedHourlyFeeRefund = hourlyRate > 0 ? hourlyRate * elapsedMs / 3_600_000 : finiteNonNegativeNumber(progress.estimated_hourly_fee_refund)
  const remainingSeconds = typeof windowEndMs === 'number'
    ? Math.max(0, Math.floor((windowEndMs - effectiveNowMs) / 1000))
    : Math.max(0, Math.floor(finiteNonNegativeNumber(progress.remaining_seconds)))

  return {
    status,
    requiredAmount,
    usageAmount,
    remainingAmount,
    progressPercent,
    estimatedHourlyFeeRefund,
    requestCount: Math.max(0, Math.trunc(Number(progress.request_count || 0))),
    remainingSeconds
  }
}

function waiverProgressPercent(listing: AccountShareListing): number {
  const value = currentWaiverProgressSnapshot(listing)?.progressPercent || 0
  if (!Number.isFinite(value) || value <= 0) return 0
  return Math.min(100, value)
}

function waiverProgressPercentStyle(listing: AccountShareListing): Record<string, string> {
  return { width: `${waiverProgressPercent(listing)}%` }
}

function waiverProgressToneClass(listing: AccountShareListing): string {
  const progress = currentWaiverProgressSnapshot(listing)
  if (progress?.status === 'met') return 'waiver-progress-met'
  if (waiverProgressPercent(listing) >= 70) return 'waiver-progress-close'
  return 'waiver-progress-active'
}

function waiverProgressStatusLabel(listing: AccountShareListing): string {
  const progress = currentWaiverProgressSnapshot(listing)
  if (!progress) return t('accountShare.card.notEnabled')
  return progress.status === 'met' ? t('activities.participation.qualified') : t('accountShare.progress.remaining', { amount: formatSpendCost(progress.remainingAmount)  })
}

function waiverProgressTitle(listing: AccountShareListing): string {
  const progress = currentWaiverProgressSnapshot(listing)
  if (!progress) return '-'
  return `${formatSpendCost(progress.usageAmount)} / ${formatSpendCost(progress.requiredAmount)}`
}

function waiverProgressAmountLabel(listing: AccountShareListing): string {
  const progress = currentWaiverProgressSnapshot(listing)
  if (!progress) return ''
  if (progress.status === 'met') {
    return t('accountShare.progress.hourlyRefund', { amount: formatSpendCost(progress.estimatedHourlyFeeRefund)  })
  }
  return t('accountShare.progress.usageVsRequired', { used: formatSpendCost(progress.usageAmount), required: formatSpendCost(progress.requiredAmount)  })
}

function waiverProgressMetaLabel(listing: AccountShareListing): string {
  const progress = currentWaiverProgressSnapshot(listing)
  if (!progress) return ''
  return t('accountShare.progress.remainingTime', { time: formatDurationCompact(progress.remainingSeconds), count: formatWholeNumber(progress.requestCount)  })
}

function waiverProgressRemainingLabel(listing: AccountShareListing): string {
  const remainingSeconds = waiverProgressRemainingSeconds(listing)
  return remainingSeconds <= 0 ? t('accountShare.progress.waitingSettlement') : formatDurationCompact(remainingSeconds)
}

function waiverProgressRemainingSeconds(listing: AccountShareListing): number {
  return currentWaiverProgressSnapshot(listing)?.remainingSeconds || 0
}

function accountLevelLabel(level?: AccountLevel | string): string {
  if (!level || level === 'unknown') return 'UNKNOWN'
  return openAIAccountLevelLabel(level, openAIAccountLevelConfigs.value)
}

function normalizePlanToken(planType?: string | null): string {
  return (planType || '').trim().toLowerCase().replace(/[\s_-]+/g, '')
}

function matchConfiguredLevelFromPlan(planType?: string | null): string {
  const token = normalizePlanToken(planType)
  if (!token) return ''
  for (const level of openAIAccountLevelConfigs.value) {
    const candidates = [level.key, ...(level.aliases || [])]
    for (const candidate of candidates) {
      const normalized = normalizePlanToken(candidate.replace(/\*+$/g, ''))
      if (!normalized) continue
      if (candidate.endsWith('*')) {
        if (token.startsWith(normalized)) return level.key
      } else if (token === normalized) {
        return level.key
      }
    }
  }
  return ''
}

function officialPlanLabel(planType?: string | null): string {
  const raw = (planType || '').trim()
  if (!raw) return ''
  const matchedLevel = matchConfiguredLevelFromPlan(raw)
  if (matchedLevel) return openAIAccountLevelLabel(matchedLevel, openAIAccountLevelConfigs.value)
  const token = normalizePlanToken(raw)
  const proMatch = token.match(/^(?:chatgpt)?pro(\d+)x?$/)
  if (proMatch?.[1]) return `Pro${proMatch[1]}x`
  if (token.startsWith('pro') || token.startsWith('chatgptpro')) {
    const multiplier = token.match(/(\d+)x?/)
    return multiplier?.[1] ? `Pro${multiplier[1]}x` : 'Pro'
  }
  return ''
}

function accountLevelTone(listing: AccountShareListing): string {
  const level = normalizeOpenAIAccountLevelKey(listing.account_level)
  if (level && level !== 'unknown') return level
  const matchedLevel = matchConfiguredLevelFromPlan(listing.account_plan_type)
  if (matchedLevel) return matchedLevel
  const planToken = normalizePlanToken(listing.account_plan_type)
  for (const levelKey of ['team', 'k12', 'pro', 'plus', 'free']) {
    if (planToken.includes(levelKey)) return levelKey
  }
  return 'unknown'
}

function accountLevelBadgeLabel(listing: AccountShareListing): string {
  return officialPlanLabel(listing.account_plan_type) || accountLevelLabel(listing.account_level)
}

function accountLevelBadgeClass(listing: AccountShareListing): string {
  const base = 'account-level-badge'
  switch (accountLevelTone(listing)) {
    case 'pro':
      return `${base} account-level-pro`
    case 'team':
      return `${base} account-level-team`
    case 'k12':
      return `${base} account-level-k12`
    case 'plus':
      return `${base} account-level-plus`
    case 'free':
      return `${base} account-level-free`
    default:
      return `${base} account-level-unknown`
  }
}

function listingDisplayName(listing: AccountShareListing): string {
  if (listing.room_name) return listing.room_name
  if ((isOwnListing(listing) || authStore.isAdmin) && listing.account_name) {
    return listing.account_name
  }
  return t('accountShare.common.roomId', { id: listing.id })
}



function supportsImageGeneration(listing: AccountShareListing): boolean {
  if (!isOpenAIListing(listing)) return false
  return listing.allowed_models.some(model => {
    const value = model.toLowerCase()
    return /(^|[/_:])(?:gpt-image(?:-|$)|dall-e(?:-|$)|dalle(?:-|$))/.test(value)
  })
}

function validityInfo(listing: AccountShareListing): { label: string; expiresAtLabel: string } | null {
  const expiresAt = normalizeDateInput(listing.subscription_expires_at || listing.account_expires_at)
  if (!expiresAt) return null
  const diffMs = expiresAt.getTime() - nowMs.value
  const days = Math.ceil(diffMs / 86_400_000)
  return {
    label: diffMs <= 0 ? t('keys.status.expired') : t('accountShare.card.validDays', { days: Math.max(1, days) }),
    expiresAtLabel: formatDate(expiresAt.toISOString())
  }
}

type RuntimeTone = 'normal' | 'warning' | 'danger' | 'muted'

function isUnknownHistorySnapshot(listing: AccountShareListing): boolean {
  if (!isArchiveView.value && !isMembershipHistoryView.value && !listing.deleted) return false
  return listing.history_snapshot_quality !== 'exact'
    && listing.history_snapshot_quality !== 'backfilled_current'
}

function isBackfilledHistorySnapshot(listing: AccountShareListing): boolean {
  return listing.history_snapshot_quality === 'backfilled_current'
}

function historySnapshotDescription(listing: AccountShareListing, context: 'membership' | 'archive' = 'membership'): string {
  switch (listing.history_snapshot_quality) {
    case 'exact':
      return context === 'archive'
        ? t('accountShare.snapshot.deletedPrecise')
        : t('accountShare.snapshot.historyPrecise')
    case 'backfilled_current':
      return context === 'archive'
        ? t('accountShare.snapshot.deletedBackfilled')
        : t('accountShare.snapshot.historyBackfilled')
    case 'unknown':
      return t('accountShare.snapshot.preMigration')
    default:
      return t('accountShare.snapshot.unmarked')
  }
}

function deletedHistorySnapshotMessage(listing: AccountShareListing): string {
  return t('accountShare.detail.deletedPrefix', { snapshotDesc: historySnapshotDescription(listing, 'archive') })
}

function isOwnListing(listing: AccountShareListing): boolean {
  const currentUserID = Number(authStore.user?.id || 0)
  return currentUserID > 0 && listing.owner_user_id === currentUserID
}

function selfUseJoinUnavailable(listing: AccountShareListing): boolean {
  return isOwnListing(listing) && (selfUseSettingsLoading.value || ownerSelfUseRateMultiplier.value === null)
}

function listingJoinUnavailableReason(listing: AccountShareListing): string {
  if (listing.deleted || listing.status !== 'active') {
    return t('accountShare.detail.notJoinableStatus', { status: statusLabel(listing.status) })
  }
  // 房主自用不占消费者席位，按服务端自用规则处理余额与可路由账号限制。
  if (!isOwnListing(listing)) {
    if (listing.active_seats >= listing.seat_limit) return t('accountShare.detail.roomFull')
    // 余额不足：后端加入前校验 user.Balance < MinBalanceRequired 会拒绝
    // （account_share_mode.go:3876，ErrAccountShareBalanceBelowMinimum）。
    const balance = Number(authStore.user?.balance ?? 0)
    const minBalance = Number(listing.min_balance_required ?? 0)
    if (Number.isFinite(balance) && Number.isFinite(minBalance) && balance < minBalance) {
      return t('accountShare.detail.balanceLow')
    }
    // 只有明确知道「挂载账号数为 0」或「可路由账号数为 0」才判不可用；
    // healthy_account_count 在部分快照/视图可能未填充（缺省 0），此时不拦截，
    // 交给后端加入时的精确校验兜底，避免把可用房间误判为无账号。
    const attached = roomAttachedAccountCount(listing)
    const eligible = roomEligibleAccountCount(listing)
    const attachedKnown = listing.quota_summary?.attached_count != null || Number(listing.account_count) > 0
    const eligibleKnown = listing.quota_summary?.eligible_count != null || Number(listing.healthy_account_count) > 0
    if (attachedKnown && attached <= 0) {
      return t('accountShare.detail.noAccounts')
    }
    if (eligibleKnown && eligible <= 0) {
      return t('accountShare.detail.noRoutable')
    }
  }
  return ''
}

function canShowListingJoinSection(listing: AccountShareListing): boolean {
  return !listing.deleted
    && listingMembershipID(listing) <= 0
    && (!isManagementView.value || isOwnListing(listing))
}

function runtimeInsightClass(tone: RuntimeTone): string {
  const base = 'runtime-badge'
  switch (tone) {
    case 'normal':
      return `${base} runtime-badge-normal`
    case 'warning':
      return `${base} runtime-badge-warning`
    case 'danger':
      return `${base} runtime-badge-danger`
    default:
      return `${base} runtime-badge-muted`
  }
}

function statusLabel(status: AccountShareListingStatus): string {
  switch (status) {
    case 'active':
      return t('accountShare.statusFilter.active')
    case 'paused':
      return t('accountShare.lifecycle.statusDelisted')
    case 'validating':
      return t('accountShare.detail.recovering')
    case 'draining':
      return t('accountShare.lifecycle.statusDelisting')
    case 'suspended':
      return t('accountShare.lifecycle.statusAdminPaused')
    case 'disabled':
      return t('accountShare.lifecycle.statusDelisted')
    default:
      return status
  }
}

function listingStatusLabel(listing: AccountShareListing): string {
  return listing.deleted ? t('ideas.status.deleted') : statusLabel(listing.status)
}

function statusBadgeClass(status: AccountShareListingStatus): string {
  const base = 'rounded-full px-2.5 py-1 text-xs font-semibold'
  switch (status) {
    case 'active':
      return `${base} bg-emerald-50 text-emerald-700 dark:bg-emerald-500/10 dark:text-emerald-200`
    case 'paused':
      return `${base} bg-amber-50 text-amber-700 dark:bg-amber-500/10 dark:text-amber-200`
    case 'validating':
    case 'draining':
      return `${base} bg-blue-50 text-blue-700 dark:bg-blue-500/10 dark:text-blue-200`
    case 'suspended':
    case 'disabled':
      return `${base} bg-gray-100 text-gray-600 dark:bg-dark-700 dark:text-dark-200`
    default:
      return `${base} bg-gray-100 text-gray-600 dark:bg-dark-700 dark:text-dark-200`
  }
}

function listingStatusBadgeClass(listing: AccountShareListing): string {
  if (listing.deleted) {
    return 'rounded-full bg-gray-200 px-2.5 py-1 text-xs font-semibold text-gray-700 dark:bg-dark-700 dark:text-dark-100'
  }
  return statusBadgeClass(listing.status)
}

function modeKeyLabel(key: ApiKey): string {
  return key.name || `Key #${key.id}`
}

function formatApiKeyIDLabel(apiKeyID?: number, emptyLabel = t('accountShare.detail.keyUnknown')): string {
  const normalizedID = Number(apiKeyID || 0)
  return normalizedID > 0 ? `Key #${normalizedID}` : emptyLabel
}

function formatApiKeyDisplayName(apiKeyName?: string, apiKeyID?: number, emptyLabel = t('accountShare.detail.keyUnknown')): string {
  const normalizedName = (apiKeyName || '').trim()
  if (normalizedName) return `Key「${normalizedName}」`
  return formatApiKeyIDLabel(apiKeyID, emptyLabel)
}

function boundApiKeyID(listing: AccountShareListing): number {
  const primaryID = listing.current_membership_id ? listing.current_api_key_id : listing.queue_api_key_id
  return Number(primaryID || listing.queue_api_key_id || listing.current_api_key_id || 0)
}

function boundApiKeyName(listing: AccountShareListing): string {
  const primaryName = listing.current_membership_id ? listing.current_api_key_name : listing.queue_api_key_name
  const apiKeyID = boundApiKeyID(listing)
  if ((primaryName || '').trim()) return primaryName || ''
  const key = modeApiKeysForListing(listing).find(item => item.id === apiKeyID)
  return key?.name || ''
}

function boundApiKeyDisplayName(listing: AccountShareListing): string {
  return formatApiKeyDisplayName(boundApiKeyName(listing), boundApiKeyID(listing))
}

function mySpendBoundApiKeyName(membership?: AccountShareMySpendSummary['membership']): string {
  if (!membership) return '-'
  return formatApiKeyDisplayName(membership.api_key_name, membership.api_key_id)
}

function selectedModeApiKeyID(listing: AccountShareListing): number {
  const singleKey = singleModeApiKeyForListing(listing)
  if (singleKey) return singleKey.id

  const selectedID = Number(selectedKeyByListing[listing.id] || 0)
  return modeApiKeysForListing(listing).some(key => key.id === selectedID) ? selectedID : 0
}

function showActionError(message: string, title = t('ideas.admin.actionFailed'), action: AccountShareActionErrorAction = null): void {
  actionErrorDialog.title = title
  actionErrorDialog.message = message
  actionErrorDialog.action = action
  actionErrorDialog.show = true
}

function closeActionErrorDialog(): void {
  actionErrorDialog.show = false
  actionErrorDialog.title = t('ideas.admin.actionFailed')
  actionErrorDialog.message = ''
  actionErrorDialog.action = null
}

function showModeApiKeyRequiredDialog(listing?: AccountShareListing): void {
  const platform = listingPlatform(listing)
  const groupName = accountModeGroupName(platform)
  if (modeKeysLoadingForPlatform(platform)) {
    showActionError(t('accountShare.join.modeKeysLoading'), t('accountShare.join.loading'))
    return
  }
  if (!modeKeysLoadedForPlatform(platform)) {
    const detail = modeKeysErrorByPlatform[platform]
    showActionError(
      detail ? t('accountShare.join.modeKeysFailed', { detail }) : t('accountShare.join.modeKeysNotLoaded'),
      t('accountShare.join.cannotJoin')
    )
    return
  }
  if (modeGroupIDsByPlatform[platform] <= 0) {
    showActionError(t('accountShare.join.noModeGroup', { groupName }), t('accountShare.join.cannotJoin'))
    return
  }
  if (modeApiKeysForPlatform(platform).length === 0) {
    showActionError(
      t('accountShare.join.noModeKey', { groupName }),
      t('accountShare.join.needModeKey'),
      'create-mode-key'
    )
    return
  }
  showActionError(t('accountShare.join.selectKeyFirst'), t('accountShare.join.selectKey'))
}

function goCreateModeApiKey(): void {
  closeActionErrorDialog()
  void router.push('/keys')
}

function normalizeIdleTimeoutMinutes(value: unknown): number {
  const parsed = Number(value ?? 0)
  if (!Number.isFinite(parsed) || parsed <= 0) return 0
  return Math.min(Math.trunc(parsed), ACCOUNT_SHARE_IDLE_TIMEOUT_MAX_MINUTES)
}

function validateIdleTimeoutMinutes(value: unknown): string {
  const parsed = Number(value ?? 0)
  if (!Number.isFinite(parsed) || !Number.isInteger(parsed)) return t('accountShare.join.idleInteger')
  if (parsed <= 0) return t('accountShare.join.idlePositive')
  if (parsed > ACCOUNT_SHARE_IDLE_TIMEOUT_MAX_MINUTES) return t('accountShare.join.idleMax')
  return ''
}

function syncIdleTimeoutControls(items: AccountShareListing[]): void {
  for (const listing of items) {
    if (listing.current_membership_id && typeof listing.current_idle_timeout_minutes === 'number') {
      idleTimeoutByListing[listing.id] = normalizeIdleTimeoutMinutes(listing.current_idle_timeout_minutes)
      continue
    }
    if (listing.queue_membership_id && typeof listing.queue_idle_timeout_minutes === 'number') {
      idleTimeoutByListing[listing.id] = normalizeIdleTimeoutMinutes(listing.queue_idle_timeout_minutes)
      continue
    }
    const cachedValue = Number(idleTimeoutByListing[listing.id] ?? 0)
    if (!Number.isFinite(cachedValue) || cachedValue <= 0) {
      idleTimeoutByListing[listing.id] = DEFAULT_ACCOUNT_SHARE_IDLE_TIMEOUT_MINUTES
    }
  }
}

function idleTimeoutSummary(listing: AccountShareListing): string {
  const minutes = normalizeIdleTimeoutMinutes(listing.current_idle_timeout_minutes ?? listing.queue_idle_timeout_minutes ?? idleTimeoutByListing[listing.id] ?? 0)
  if (minutes <= 0) return t('accountShare.idle.notEnabled')
  if (!listing.current_idle_expires_at) return t('accountShare.idle.minutes', { minutes })
  const countdown = formatCountdownUntil(listing.current_idle_expires_at)
  if (countdown === t('common.now')) return t('accountShare.idle.reached')
  return t('accountShare.idle.countdown', { countdown })
}

function pendingMembershipEndForListing(
  listing: AccountShareListing
): PendingMembershipEnd | null {
  return pendingMembershipEnds.value[listing.id] || null
}

// 解析查看者在当前房间的活跃成员 ID。queue_membership_id 的 join 不检查
// paid_until/idle 新鲜度，可覆盖 current_membership_id 在计费续期间隙
// 短暂为空的窗口，保证成员始终能看到自己的退出入口。
function listingMembershipID(listing: AccountShareListing): number {
  return Number(listing.current_membership_id || listing.queue_membership_id || 0)
}

function isListingMembershipEnding(listing: AccountShareListing): boolean {
  return listing.queue_status === 'ending'
    || pendingMembershipEndForListing(listing) !== null
}

function membershipPanelTitle(listing: AccountShareListing): string {
  if (isListingMembershipEnding(listing)) return t('accountShare.endUse.exiting')
  return t('keys.accountShareConflict.activeLabel')
}

function membershipPanelSubtitle(listing: AccountShareListing): string {
  if (!isListingMembershipEnding(listing)) {
    return idleTimeoutSummary(listing)
  }
  const pending = pendingMembershipEndForListing(listing)
  const reason = accountShareOperationWaitReason({
    blocker: pending?.operationBlocker || {},
    error_message: pending?.operationError
  })
  if (reason) return reason
  if (pending?.operationStatus === 'needs_attention') {
    return t('accountShare.endUse.blockedRetry')
  }
  if (
    pending?.operationStatus === 'failed'
    || pending?.operationStatus === 'cancelled'
  ) {
    return pending.operationError || t('accountShare.endUse.incomplete')
  }
  if (!pending?.operationID) {
    return t('accountShare.endUse.acceptedNoProgress')
  }
  return t('accountShare.endUse.accepted')
}

function membershipEndObservation(listing: AccountShareListing): string {
  const pending = pendingMembershipEndForListing(listing)
  if (!pending) return t('accountShare.endUse.loadingProgress')
  const parts = [accountShareOperationWaitDuration(pending.operationCreatedAt || pending.membership.ending_requested_at, nowMs.value)]
  if (pending.lastOperationSuccessAt) parts.push(t('accountShare.endUse.lastSuccess', { time: formatDate(new Date(pending.lastOperationSuccessAt).toISOString())  }))
  if (pending.operationQueryError && pending.lastOperationAttemptAt) parts.push(t('accountShare.endUse.lastAttempt', { time: formatDate(new Date(pending.lastOperationAttemptAt).toISOString())  }))
  if (pending.lastBindingCheckAt) parts.push(t('accountShare.endUse.lastBindingCheck', { time: formatDate(new Date(pending.lastBindingCheckAt).toISOString())  }))
  if (pending.operationUpdatedAt) parts.push(t('accountShare.endUse.operationUpdated', { time: formatDate(pending.operationUpdatedAt)  }))
  return parts.join(' · ')
}

function mySpendMembershipID(listing: AccountShareListing | null | undefined): number {
  return Number(listing?.current_membership_id || listing?.queue_membership_id || listing?.last_used_membership_id || 0)
}

function canOpenMySpend(listing: AccountShareListing): boolean {
  return mySpendMembershipID(listing) > 0
}

function mySpendRangeLabel(range: string): string {
  switch (range) {
    case 'today':
      return t('common.today')
    case '7d':
      return t('admin.ops.timeRange.7d')
    default:
      return t('accountShare.mySpendRange.currentMembership')
  }
}

function mySpendStatusLabel(status?: string): string {
  switch (status) {
    case 'active':
      return t('keys.accountShareConflict.activeLabel')
    case 'queued':
      return t('accountShare.membership.reasonQueueRemoved')
    case 'ended':
      return t('accountShare.membership.statusEnded')
    default:
      return status || '-'
  }
}

function mySpendWindowLabel(summary: AccountShareMySpendSummary): string {
  return t('accountShare.mySpend.range', { start: formatDate(summary.start_time), end: formatDate(summary.end_time)  })
}

function mySpendAccountName(summary: AccountShareMySpendSummary): string {
  return summary.listing.account_name || t('accountShare.mySpend.roomId', { id: summary.listing.id })
}

function mySpendLastActivityLabel(summary: AccountShareMySpendSummary): string {
  return summary.last_activity_at ? formatDate(summary.last_activity_at) : t('accountShare.mySpend.empty')
}

function mySpendAverageRequestCost(summary: AccountShareMySpendSummary): string {
  if (summary.request_count <= 0) return '0'
  return formatSpendCost(summary.request_cost / summary.request_count)
}

function mySpendBrowserTimeZone(): string {
  try {
    return Intl.DateTimeFormat().resolvedOptions().timeZone || ''
  } catch {
    return ''
  }
}

function abortMySpendAccountsRequest(): void {
  mySpendAccountsRequestSeq += 1
  if (mySpendAccountsRequestController) {
    mySpendAccountsRequestController.abort()
    mySpendAccountsRequestController = null
  }
}

function abortMySpendRequest(): void {
  mySpendRequestSeq += 1
  if (mySpendRequestController) {
    mySpendRequestController.abort()
    mySpendRequestController = null
  }
}

function mySpendAccountOptionKey(listingID: number, source: MySpendAccountOptionSource, membershipID: number): string {
  return `${source}:${listingID}:${membershipID}`
}

function mySpendAccountSourceLabel(source: MySpendAccountOptionSource): string {
  return source === 'using' ? t('accountShare.mySpend.tabUsing') : t('accountShare.mySpend.tabHistory')
}

function mySpendAccountStatusLabel(option: MySpendAccountOption): string {
  if (option.status === 'active') return t('keys.accountShareConflict.activeLabel')
  if (option.status === 'ending') return t('accountShare.membership.statusSettling')
  if (option.status === 'ended') return t('accountShare.membership.statusEnded')
  return option.status || t('accountShare.mySpend.countable')
}

function mySpendAccountDisplayName(option: MySpendAccountOption): string {
  return option.roomName || option.accountName || t('accountShare.mySpend.optionRoom', { id: option.listingID  })
}

function mySpendAccountUsagePeriod(option: MySpendAccountOption): string {
  if (option.source === 'history') {
    const joinedAt = option.joinedAt ? formatDate(option.joinedAt) : t('accountShare.mySpend.noTime')
    const endedAt = option.endedAt ? formatDate(option.endedAt) : t('accountShare.membership.noEndTime')
    return t('accountShare.mySpend.joinedToEnded', { joinedAt, endedAt })
  }
  if (option.joinedAt) {
    const lastRequest = option.lastRequestAt ? t('accountShare.mySpend.lastRequest', { time: formatDate(option.lastRequestAt)  }) : ''
    return t('accountSpend.joinedAt', { time: formatDate(option.joinedAt), lastRequest: lastRequest })
  }
  return t('accountShare.mySpend.membershipId', { id: option.membershipID  })
}

function mySpendAccountOptionTitle(option: MySpendAccountOption): string {
  return t('accountShare.mySpend.optionLabel', { name: mySpendAccountDisplayName(option), source: mySpendAccountSourceLabel(option.source), id: option.membershipID  })
}

function mySpendAccountOptionSourceForListing(listing: AccountShareListing): MySpendAccountOptionSource {
  return listing.current_membership_id || listing.queue_membership_id ? 'using' : 'history'
}

function buildMySpendListingOption(
  listing: AccountShareListing,
  source: MySpendAccountOptionSource = mySpendAccountOptionSourceForListing(listing)
): MySpendAccountOption | null {
  const normalized = normalizeListingForMerge(listing)
  const membershipID = mySpendMembershipID(normalized)
  if (membershipID <= 0) return null
  const status = normalized.current_membership_id
    ? 'active'
    : (normalized.queue_membership_id ? (normalized.queue_status || 'ended') : 'ended')
  return {
    key: mySpendAccountOptionKey(normalized.id, source, membershipID),
    source,
    listingID: normalized.id,
    membershipID,
    platform: normalized.platform,
    roomName: normalized.room_name || '',
    accountName: normalized.account_name,
    ownerUserID: normalized.owner_user_id,
    ownerUsername: normalized.owner_username,
    status,
    joinedAt: normalized.current_joined_at,
    lastRequestAt: normalized.current_last_request_at,
    endedAt: normalized.last_used_at,
    roomDeleted: Boolean(normalized.deleted),
    listing: normalized,
  }
}

function buildMySpendHistoryOption(entry: AccountShareMembershipHistoryEntry): MySpendAccountOption | null {
  const listingID = Number(entry.listing_id || 0)
  const membershipID = Number(entry.membership_id || 0)
  if (listingID <= 0 || membershipID <= 0) return null
  return {
    key: mySpendAccountOptionKey(listingID, 'history', membershipID),
    source: 'history',
    listingID,
    membershipID,
    platform: entry.platform,
    roomName: entry.room_name || '',
    accountName: entry.account_name,
    ownerUserID: entry.owner_user_id,
    ownerUsername: entry.owner_username,
    status: entry.status,
    joinedAt: entry.joined_at,
    lastRequestAt: entry.last_request_at,
    endedAt: entry.ended_at,
    roomDeleted: entry.room_deleted,
  }
}

async function fetchMySpendAccountOptionsByTab(
  source: MySpendAccountOptionSource,
  page: number,
  pageSize: number,
  signal: AbortSignal
): Promise<MySpendAccountOptionPage> {
  const options: MySpendAccountOption[] = []
  const result = source === 'history'
    ? await accountShareAPI.listMembershipHistory(page, pageSize, { signal })
    : await accountShareAPI.listListings(page, pageSize, { tab: 'using' }, { signal })
  if (source === 'history') {
    for (const entry of result.items as AccountShareMembershipHistoryEntry[] || []) {
      const option = buildMySpendHistoryOption(entry)
      if (option) options.push(option)
    }
  } else {
    for (const listing of result.items as AccountShareListing[] || []) {
      const option = buildMySpendListingOption(listing)
      if (option) options.push(option)
    }
  }
  const normalizedPageSize = Math.max(1, Number(result.page_size || pageSize))
  const total = Math.max(0, Number(result.total || 0))
  return {
    options,
    page: Math.max(1, Number(result.page || page)),
    pageSize: normalizedPageSize,
    total,
    pages: Math.max(1, Number(result.pages || 1)),
    totalExact: source === 'history' || ('total_exact' in result && result.total_exact === true),
    hasMore: 'has_more' in result ? Boolean(result.has_more) : result.page < result.pages,
  }
}

function applyMySpendAccountOptionPage(source: MySpendAccountOptionSource, result: MySpendAccountOptionPage): void {
  const pagination = source === 'using' ? mySpendUsingPagination : mySpendHistoryPagination
  pagination.page = result.page
  pagination.pageSize = result.pageSize
  pagination.total = result.total
  pagination.pages = result.pages
  pagination.totalExact = result.totalExact
  pagination.hasMore = result.hasMore
  if (source === 'using') {
    mySpendUsingAccountOptions.value = result.options
    mergeKnownListings(result.options.flatMap(option => option.listing ? [option.listing] : []))
    return
  }
  mySpendHistoryAccountOptions.value = result.options
}

function setSelectedMySpendAccount(option: MySpendAccountOption): void {
  mySpendSelectedOption.value = option
  mySpendSelectedOptionKey.value = option.key
  mySpendPickerSource.value = option.source
  if (option.source === 'history') mySpendRange.value = 'current_membership'
  mySpendSummary.value = null
  mySpendError.value = ''
}

function selectMySpendAccount(option: MySpendAccountOption): void {
  if (mySpendSelectedOptionKey.value === option.key && mySpendSummary.value) return
  setSelectedMySpendAccount(option)
  void loadMySpendSummary()
}

async function loadMySpendAccountOptions(preferredListing?: AccountShareListing): Promise<void> {
  abortMySpendAccountsRequest()
  abortMySpendRequest()
  mySpendLoading.value = false
  mySpendSummary.value = null
  mySpendError.value = ''
  const controller = new AbortController()
  const requestSeq = ++mySpendAccountsRequestSeq
  mySpendAccountsRequestController = controller
  mySpendAccountsLoading.value = true
  mySpendAccountsError.value = ''
  try {
    const [usingPage, historyPage] = await Promise.all([
      fetchMySpendAccountOptionsByTab(
        'using',
        mySpendUsingPagination.page,
        mySpendUsingPagination.pageSize,
        controller.signal
      ),
      fetchMySpendAccountOptionsByTab(
        'history',
        mySpendHistoryPagination.page,
        mySpendHistoryPagination.pageSize,
        controller.signal
      )
    ])
    if (controller.signal.aborted || requestSeq !== mySpendAccountsRequestSeq) return
    applyMySpendAccountOptionPage('using', usingPage)
    applyMySpendAccountOptionPage('history', historyPage)
    const visibleOptions = [...usingPage.options, ...historyPage.options]
    let preferredOption: MySpendAccountOption | null = null
    if (preferredListing && canOpenMySpend(preferredListing)) {
      const preferredMembershipID = mySpendMembershipID(preferredListing)
      preferredOption = visibleOptions.find(option => option.membershipID === preferredMembershipID)
        || buildMySpendListingOption(preferredListing)
    }
    const selectedOption = preferredOption
      || visibleOptions.find(option => option.key === mySpendSelectedOptionKey.value)
      || visibleOptions[0]
    if (selectedOption) {
      setSelectedMySpendAccount(selectedOption)
      void loadMySpendSummary()
    } else {
      mySpendSelectedOption.value = null
      mySpendSelectedOptionKey.value = ''
      mySpendSummary.value = null
      mySpendError.value = ''
    }
  } catch (error: unknown) {
    if (controller.signal.aborted || requestSeq !== mySpendAccountsRequestSeq || isCanceledRequest(error)) return
    mySpendAccountsError.value = extractApiErrorMessage(error, t('accountShare.errors.loadUsedAccounts'), {
      USER_NOT_FOUND: t('accountShare.errors.join.userNotFound')
    })
  } finally {
    if (requestSeq === mySpendAccountsRequestSeq) {
      mySpendAccountsLoading.value = false
      if (mySpendAccountsRequestController === controller) mySpendAccountsRequestController = null
    }
  }
}

function setMySpendPickerSource(source: MySpendAccountOptionSource): void {
  mySpendPickerSource.value = source
}

async function handleMySpendAccountPageChange(page: number): Promise<void> {
  const source = mySpendPickerSource.value
  const pagination = source === 'using' ? mySpendUsingPagination : mySpendHistoryPagination
  const lastReachablePage = pagination.totalExact ? pagination.pages : pagination.page + (pagination.hasMore ? 1 : 0)
  const normalizedPage = Math.min(Math.max(1, Number(page || 1)), Math.max(1, lastReachablePage))
  if (normalizedPage === pagination.page || mySpendAccountsLoading.value) return
  abortMySpendAccountsRequest()
  const controller = new AbortController()
  const requestSeq = ++mySpendAccountsRequestSeq
  mySpendAccountsRequestController = controller
  mySpendAccountsLoading.value = true
  mySpendAccountsError.value = ''
  try {
    const result = await fetchMySpendAccountOptionsByTab(
      source,
      normalizedPage,
      pagination.pageSize,
      controller.signal
    )
    if (controller.signal.aborted || requestSeq !== mySpendAccountsRequestSeq) return
    applyMySpendAccountOptionPage(source, result)
  } catch (error: unknown) {
    if (controller.signal.aborted || requestSeq !== mySpendAccountsRequestSeq || isCanceledRequest(error)) return
    mySpendAccountsError.value = extractApiErrorMessage(error, t('accountShare.errors.loadAccountPages'), {
      USER_NOT_FOUND: t('accountShare.errors.join.userNotFound')
    })
  } finally {
    if (requestSeq === mySpendAccountsRequestSeq) {
      mySpendAccountsLoading.value = false
      if (mySpendAccountsRequestController === controller) mySpendAccountsRequestController = null
    }
  }
}

function resetMySpendAccountPagination(): void {
  Object.assign(mySpendUsingPagination, {
    page: 1,
    pageSize: MY_SPEND_ACCOUNT_PAGE_SIZE,
    total: 0,
    pages: 1,
    totalExact: true,
    hasMore: false,
  })
  Object.assign(mySpendHistoryPagination, {
    page: 1,
    pageSize: MY_SPEND_ACCOUNT_PAGE_SIZE,
    total: 0,
    pages: 1,
    totalExact: true,
    hasMore: false,
  })
}

function openMySpendDialog(listing?: AccountShareListing): void {
  if (listing && !canOpenMySpend(listing)) return
  abortMySpendRequest()
  mySpendSelectedOption.value = null
  mySpendSelectedOptionKey.value = ''
  mySpendPickerSource.value = 'using'
  mySpendUsingAccountOptions.value = []
  mySpendHistoryAccountOptions.value = []
  resetMySpendAccountPagination()
  mySpendRange.value = 'current_membership'
  mySpendSummary.value = null
  mySpendError.value = ''
  mySpendAccountsError.value = ''
  showMySpendDialog.value = true
  void loadMySpendAccountOptions(listing)
}

function closeMySpendDialog(): void {
  abortMySpendAccountsRequest()
  abortMySpendRequest()
  showMySpendDialog.value = false
  mySpendSelectedOption.value = null
  mySpendSelectedOptionKey.value = ''
  mySpendPickerSource.value = 'using'
  mySpendUsingAccountOptions.value = []
  mySpendHistoryAccountOptions.value = []
  resetMySpendAccountPagination()
  mySpendAccountsError.value = ''
  mySpendAccountsLoading.value = false
  mySpendSummary.value = null
  mySpendError.value = ''
  mySpendLoading.value = false
}

function setMySpendRange(range: AccountShareMySpendRange): void {
  if (mySpendHistorySelection.value && range !== 'current_membership') return
  if (mySpendRange.value === range || mySpendLoading.value) return
  mySpendRange.value = range
  void loadMySpendSummary()
}

async function loadMySpendSummary(): Promise<void> {
  const option = mySpendSelectedOption.value
  if (!option) return
  abortMySpendRequest()
  const controller = new AbortController()
  const requestSeq = ++mySpendRequestSeq
  mySpendRequestController = controller
  mySpendLoading.value = true
  mySpendError.value = ''
  try {
    const membershipID = mySpendRange.value === 'current_membership' ? option.membershipID : 0
    const summary = await accountShareAPI.getMySpendSummary(option.listingID, {
      range: mySpendRange.value,
      membership_id: membershipID > 0 ? membershipID : undefined,
      timezone: mySpendBrowserTimeZone()
    }, {
      signal: controller.signal
    })
    if (requestSeq !== mySpendRequestSeq) return
    mySpendSummary.value = summary
  } catch (error: unknown) {
    if (controller.signal.aborted || isCanceledRequest(error)) return
    if (requestSeq !== mySpendRequestSeq) return
    mySpendError.value = extractApiErrorMessage(error, t('accountShare.errors.loadSpendSummary'), {
      ACCOUNT_SHARE_LISTING_NOT_FOUND: t('accountShare.errors.membershipGone'),
      ACCOUNT_SHARE_SPEND_INVALID_RANGE: t('accountShare.errors.invalidSpendRange'),
      USER_NOT_FOUND: t('accountShare.errors.join.userNotFound')
    })
  } finally {
    if (requestSeq === mySpendRequestSeq) {
      mySpendLoading.value = false
      if (mySpendRequestController === controller) mySpendRequestController = null
    }
  }
}

function routeQueryString(value: unknown): string {
  if (Array.isArray(value)) return typeof value[0] === 'string' ? value[0] : ''
  return typeof value === 'string' ? value : ''
}

function prepareKeyResolutionMode(): void {
  if (!isKeyResolutionMode.value) return
  clearSearchDebounceTimer()
  closeFilterPopover()
  activeFilter.value = filters.value[0]
  pagination.page = 1
}

function clearKeyResolutionState(): void {
  keyResolutionRequestSeq += 1
  keyResolutionBindingStatus.value = null
  keyResolutionMemberships.value = []
  keyResolutionListings.value = []
  keyResolutionLoading.value = false
  keyResolutionLoaded.value = false
  keyResolutionError.value = ''
}

function resolutionListingFromMemberships(
  listing: AccountShareListing,
  memberships: AccountShareMembership[]
): AccountShareListing {
  const next = normalizeListingForMerge(listing)
  delete next.current_membership_id
  delete next.current_api_key_id
  delete next.current_api_key_name
  delete next.current_joined_at
  delete next.current_paid_until
  delete next.current_billed_until
  delete next.current_idle_timeout_minutes
  delete next.current_last_request_at
  delete next.current_idle_expires_at
  delete next.current_waiver_progress
  delete next.queue_membership_id
  delete next.queue_api_key_id
  delete next.queue_api_key_name
  delete next.queue_rank
  delete next.queue_status
  delete next.queue_idle_timeout_minutes
  delete next.queue_dispatch_cooldown_until

  const membership = memberships.find(item => item.status === 'active')
    || memberships.find(item => item.status === 'ending')
  if (!membership) return next
  const apiKeyName = keyResolutionApiKeyName.value
  next.queue_membership_id = membership.id
  next.queue_api_key_id = membership.api_key_id
  next.queue_api_key_name = apiKeyName
  next.queue_rank = membership.queue_rank
  next.queue_status = membership.status
  next.queue_idle_timeout_minutes = membership.idle_timeout_minutes
  next.queue_dispatch_cooldown_until = membership.dispatch_cooldown_until
  if (membership.status === 'active') {
    next.current_membership_id = membership.id
    next.current_api_key_id = membership.api_key_id
    next.current_api_key_name = apiKeyName
    next.current_joined_at = membership.joined_at
    next.current_paid_until = membership.paid_until
    next.current_billed_until = membership.billed_until
    next.current_idle_timeout_minutes = membership.idle_timeout_minutes
    next.current_last_request_at = membership.last_request_at
  }
  return next
}

function syncKeyResolutionEndingMemberships(
  apiKeyID: number,
  memberships: AccountShareMembership[],
  resolvedListings: AccountShareListing[]
): void {
  const endingByListingID = new Map(
    memberships
      .filter(membership => membership.status === 'ending')
      .map(membership => [Number(membership.listing_id), membership] as const)
  )
  const listingsByID = new Map(resolvedListings.map(listing => [Number(listing.id), listing]))
  const boundMembershipIDs = new Set(memberships
    .filter(membership => membership.status === 'active' || membership.status === 'ending')
    .map(membership => membership.id))
  const nextPending = { ...pendingMembershipEnds.value }

  for (const [listingID, pending] of Object.entries(nextPending)) {
    if (Number(pending.apiKeyID || 0) === apiKeyID && !boundMembershipIDs.has(pending.membershipID)) {
      delete nextPending[Number(listingID)]
    }
  }

  for (const [listingID, membership] of endingByListingID) {
    const listing = listingsByID.get(listingID)
    if (!listing) continue
    const existing = nextPending[listingID]
    const operationID = (membership.ending_operation_id || '').trim()
    const operationStatus = (membership.ending_operation_status || '').trim()
    const preserveOperationState = existing?.membershipID === membership.id &&
      existing.operationID === operationID
    nextPending[listingID] = {
      ...(preserveOperationState ? existing : {}),
      listingID,
      membershipID: membership.id,
      operationID,
      operationStatus: operationStatus || (preserveOperationState ? existing.operationStatus : 'pending'),
      operationError: preserveOperationState ? existing.operationError : '',
      apiKeyID,
      apiKeyName: keyResolutionApiKeyName.value,
      membership,
      listingSnapshot: listing
    }
  }

  pendingMembershipEnds.value = nextPending
}

async function loadKeyResolutionState(): Promise<boolean> {
  if (!isKeyResolutionMode.value) {
    clearKeyResolutionState()
    return true
  }

  const apiKeyID = keyResolutionApiKeyID.value
  const requestSeq = ++keyResolutionRequestSeq
  keyResolutionLoading.value = true
  keyResolutionError.value = ''
  if (apiKeyID <= 0) {
    keyResolutionMemberships.value = []
    keyResolutionListings.value = []
    keyResolutionBindingStatus.value = null
    keyResolutionLoaded.value = true
    keyResolutionLoading.value = false
    keyResolutionError.value = t('accountShare.keyResolution.missingKeyId')
    return false
  }

  try {
    const bindingStatus = await accountShareAPI.getAPIKeyBindingStatus(apiKeyID)
    if (requestSeq !== keyResolutionRequestSeq || apiKeyID !== keyResolutionApiKeyID.value) return false
    if (bindingStatus.api_key_id !== apiKeyID) {
      throw new Error(t('accountShare.keyResolution.keyIdMismatch'))
    }
    const memberships = bindingStatus.memberships

    const membershipsByListing = new Map<number, AccountShareMembership[]>()
    for (const membership of memberships) {
      const listingID = Number(membership.listing_id || 0)
      if (!Number.isSafeInteger(listingID) || listingID <= 0) {
        throw new Error(t('accountShare.keyResolution.missingAccountId'))
      }
      const current = membershipsByListing.get(listingID) || []
      current.push(membership)
      membershipsByListing.set(listingID, current)
    }

    const listingIDs = Array.from(membershipsByListing.keys())
    const details = await Promise.all(listingIDs.map(listingID => accountShareAPI.getListing(listingID)))
    if (requestSeq !== keyResolutionRequestSeq || apiKeyID !== keyResolutionApiKeyID.value) return false

    const exactListings = details.map(listing => resolutionListingFromMemberships(
      listing,
      membershipsByListing.get(listing.id) || []
    ))
    syncKeyResolutionEndingMemberships(apiKeyID, memberships, exactListings)
    keyResolutionBindingStatus.value = bindingStatus
    keyResolutionMemberships.value = memberships
    keyResolutionListings.value = exactListings
    keyResolutionLoaded.value = true
    syncIdleTimeoutControls(exactListings)
    mergeKnownListings(exactListings)
    if (exactListings.length > 0) {
      activeListingPlatform.value = listingPlatform(exactListings[0])
    }
    scheduleTransientStatusRefresh()
    return true
  } catch (error: unknown) {
    if (requestSeq !== keyResolutionRequestSeq) return false
    keyResolutionMemberships.value = []
    keyResolutionListings.value = []
    keyResolutionBindingStatus.value = null
    keyResolutionLoaded.value = true
    keyResolutionError.value = extractApiErrorMessage(error, t('accountShare.errors.loadBindingState'))
    return false
  } finally {
    if (requestSeq === keyResolutionRequestSeq) {
      keyResolutionLoading.value = false
    }
  }
}

async function refreshKeyResolutionContext(): Promise<void> {
  await loadKeyResolutionState()
}

function isKeyResolutionListing(listing: AccountShareListing): boolean {
  return isKeyResolutionMode.value && keyResolutionListingIDs.value.has(Number(listing.id))
}

function returnToApiKeyManagement(): void {
  const returnTo = routeQueryString(route.query.return_to)
  void router.push(returnTo === '/keys' ? returnTo : { name: 'Keys' })
}

function setFilter(filter: FilterOption): void {
  closeRoomDetails()
  clearSearchDebounceTimer()
  closeFilterPopover()
  activeFilter.value = filter
  // 「可用账号」只对浏览广场（tab=all）有意义。切到管理视图（mine/using/history）
  // 时把 available 归一为空，避免 chip 显示「可用账号」但实际列表是全量（buildListingFilters
  // 对非 all 标签不翻译 available）造成的误导。
  if (filter.tab !== 'all' && listingFilters.status === 'available') {
    listingFilters.status = ''
  }
  if (filter.tab === 'history') {
    abortActiveListingsRequest()
    clearMembershipStatusRefreshTimer()
    membershipHistoryPagination.page = 1
  } else {
    abortMembershipHistoryRequest()
    pagination.page = 1
  }
  persistListingPreferences()
  void loadCurrentView()
}

function setOwnerRoomState(value: string): void {
  listingFilters.status = value === 'active' || value === 'paused' ? value : ''
  setFilter(value === 'archive' ? archiveFilter.value : ownerFilter.value)
}

function sanitizeListingFiltersForPlatform(platform: AccountSharePlatform): void {
  listingFilters.models = []
  modelFilterInput.value = ''
  if (platform !== 'openai') {
    listingFilters.accountLevel = 'all'
  }
  listingFilters.featureTags = listingFilters.featureTags.filter(tag =>
    platform === 'openai' || (tag !== 'image_generation' && tag !== 'codex_cli_only' && tag !== 'non_codex_cli_only')
  )
}

function setListingPlatform(platform: AccountSharePlatform): void {
  if (activeListingPlatform.value === platform) return
  closeRoomDetails()
  abortRecommendationAsyncRequests()
  if (ownerDialog.show) closeOwnerDialog()
  clearSearchDebounceTimer()
  closeFilterPopover()
  activeListingPlatform.value = platform
  sanitizeListingFiltersForPlatform(platform)
  syncRecommendationFormForPlatform(platform)
  resetRecommendationResult()
  pagination.page = 1
  persistListingPreferences()
  void loadListings()
}

async function loadSelfUseCommissionRate(force = false): Promise<void> {
  if (!force && ownerSelfUseRateMultiplier.value !== null) {
    selfUseSettingsError.value = ''
    return
  }
  selfUseSettingsLoading.value = true
  selfUseSettingsError.value = ''
  try {
    await appStore.fetchPublicSettings(force)
    if (ownerSelfUseRateMultiplier.value === null) {
      selfUseSettingsError.value = t('accountShare.errors.selfUseConfigLoad')
    }
  } catch (error: unknown) {
    selfUseSettingsError.value = extractApiErrorMessage(error, t('accountShare.errors.selfUseConfigLoad'))
  } finally {
    selfUseSettingsLoading.value = false
  }
}

async function loadCapabilities(): Promise<void> {
  if (capabilitiesLoading.value) return
  capabilitiesLoading.value = true
  capabilitiesError.value = ''
  try {
    capabilities.value = await accountShareAPI.getCapabilities()
  } catch (error: unknown) {
    capabilitiesError.value = extractApiErrorMessage(error, t('accountShare.errors.capabilitiesUnavailable'))
  } finally {
    capabilitiesLoading.value = false
  }
}

async function refreshPageData(): Promise<void> {
  const tasks: Promise<unknown>[] = [
    loadCurrentView(),
    loadModeKeys(),
    loadSelfUseCommissionRate(true),
    loadCapabilities()
  ]
  if (isKeyResolutionMode.value) tasks.push(loadKeyResolutionState())
  await Promise.all(tasks)
}

function hasVisibleMembershipState(): boolean {
  if (isMembershipHistoryView.value || isArchiveView.value) return false
  return listings.value.some(listing => Boolean(
    listingMembershipID(listing) > 0
    || listing.status === 'validating'
  )) || hasPendingMembershipEndState()
}

function pendingMembershipEndIsPollable(pending: PendingMembershipEnd): boolean {
  return Boolean(
    pending.operationID
    && !ROOM_LIFECYCLE_TERMINAL_OPERATION_STATUSES.has(pending.operationStatus)
  )
}

function hasPendingMembershipEndState(): boolean {
  return Object.keys(pendingMembershipEnds.value).length > 0
}

function hasVisibleTransientStatus(): boolean {
  if (isMembershipHistoryView.value || isArchiveView.value) return false
  return listings.value.some(listing => listingMembershipID(listing) > 0)
    || visibleValidatingListingIDs.value.size > 0
    || hasPendingMembershipEndState()
    || (
      isKeyResolutionMode.value &&
      (keyResolutionBindingStatus.value?.ending_count ?? 0) > 0
    )
}

function clearMembershipStatusRefreshTimer(): void {
  if (membershipStatusRefreshTimer == null) return
  window.clearTimeout(membershipStatusRefreshTimer)
  membershipStatusRefreshTimer = null
}

function scheduleTransientStatusRefresh(): void {
  clearMembershipStatusRefreshTimer()
  if (
    document.visibilityState !== 'visible'
    || !hasVisibleTransientStatus()
  ) {
    return
  }
  membershipStatusRefreshTimer = window.setTimeout(() => {
    membershipStatusRefreshTimer = null
    void refreshTransientStatuses()
  }, ACCOUNT_SHARE_TRANSIENT_STATUS_REFRESH_INTERVAL_MS)
}

async function refreshTransientStatuses(): Promise<void> {
  if (
    document.visibilityState !== 'visible'
    || !hasVisibleTransientStatus()
  ) {
    clearMembershipStatusRefreshTimer()
    return
  }
  if (loading.value) {
    scheduleTransientStatusRefresh()
    return
  }

  const completedEnds = await pollPendingMembershipEndOperations()
  const refreshed = await loadListings(true)
  const resolutionRefreshed = !isKeyResolutionMode.value || await loadKeyResolutionState()
  if (!refreshed || !resolutionRefreshed || completedEnds.length === 0 || pendingReview.value) return
  const completed = completedEnds.find(item => Boolean(item.membership.last_request_at))
  if (!completed) return
  openReviewDialog(completed.listingSnapshot, completed.membership)
}

function refreshMembershipStatusIfDue(): void {
  if (document.visibilityState !== 'visible' || !hasVisibleMembershipState()) {
    clearMembershipStatusRefreshTimer()
    return
  }

  const remainingThrottleMs = Math.max(
    0,
    ACCOUNT_SHARE_STATUS_REFRESH_THROTTLE_MS - (Date.now() - lastMembershipStatusRefreshAt)
  )
  if (loading.value || remainingThrottleMs > 0) {
    if (membershipStatusRefreshTimer == null) {
      membershipStatusRefreshTimer = window.setTimeout(() => {
        membershipStatusRefreshTimer = null
        refreshMembershipStatusIfDue()
      }, Math.max(500, remainingThrottleMs))
    }
    return
  }

  if (hasVisibleTransientStatus()) {
    void refreshTransientStatuses()
  } else {
    void loadListings()
  }
}

function handleWindowFocus(): void {
  refreshMembershipStatusIfDue()
}

function handleDocumentVisibilityChange(): void {
  refreshMembershipStatusIfDue()
}

function openUsageGuideDialog(): void {
  showUsageGuideDialog.value = true
}

function closeUsageGuideDialog(): void {
  showUsageGuideDialog.value = false
}

function openAdminQuotaDialog(): void {
  if (!authStore.isAdmin) return
  showAdminQuotaDialog.value = true
}

function closeAdminQuotaDialog(): void {
  showAdminQuotaDialog.value = false
}

function capabilityBlockerMessage(blocker: { code: string; message?: string }): string {
  return accountShareCapabilityBlockerMessages()[blocker.code]
    || blocker.message?.trim()
    || t('accountShare.errors.quotaInsufficient')
}

function openRecommendationFromUsageGuide(): void {
  showUsageGuideDialog.value = false
  openRecommendationDialog()
}

function openRecommendationDialog(): void {
  syncRecommendationFormForPlatform(activeListingPlatform.value)
  showRecommendationDialog.value = true
  if (!modeKeysLoaded.value && !modeKeysLoading.value) {
    refreshModeKeysInBackground()
  }
}

function closeRecommendationDialog(): void {
  abortRecommendationAsyncRequests()
  showRecommendationDialog.value = false
}

function openCreateDialog(): void {
  if (showCreate.value || pendingDraftDiscardTarget.value !== null) return
  const blocker = capabilities.value?.capability_blockers[0]
  if (blocker) {
    actionErrorDialog.title = t('accountShare.errors.cannotCreateRoom')
    actionErrorDialog.message = capabilityBlockerMessage(blocker)
    actionErrorDialog.action = null
    actionErrorDialog.show = true
    return
  }
  showCreate.value = true
  if (createPlatform.value !== activeListingPlatform.value) {
    selectCreatePlatform(activeListingPlatform.value)
  } else {
    void loadRoomCatalog(createPlatform.value)
    void loadOwnedAccounts()
  }
  void loadListingNameIndex()
  captureCreateDraftBaseline()
  void nextTick(() => {
    if (showCreate.value && !createDraftHasChanges()) captureCreateDraftBaseline()
  })
}

async function loadCreateAccountProxies(scope: { platform?: string; account_level?: string } = {}): Promise<void> {
  const requestSeq = ++createAccountProxyRequestSeq
  try {
    const result = await accountShareAPI.listProxies({ platform: scope.platform || createPlatform.value, account_level: scope.account_level || 'unknown' })
    if (requestSeq === createAccountProxyRequestSeq) proxies.value = result
  } catch (error) {
    if (requestSeq !== createAccountProxyRequestSeq) return
    proxies.value = []
    showActionError(extractApiErrorMessage(error, t('accountShare.errors.loadProxies')), t('accountShare.errors.loadAccountConfig'))
  }
}

function openStandaloneAccountCreator(): void {
  showCreateAccount.value = true
  void loadCreateAccountProxies()
}

async function handleStandaloneAccountCreated(accounts?: Account[]): Promise<void> {
  showCreateAccount.value = false
  await loadOwnedAccounts(true)
  const created = accounts?.length === 1 ? accounts[0] : undefined
  if (created && eligibleOwnedAccounts.value.some(account => account.id === created.id)) {
    selectedOwnedAccountID.value = created.id
    createErrorMessage.value = ''
  } else {
    createErrorMessage.value = t('accountShare.roomCreate.accountCreated')
  }
}

function closeCreateDialog(): void {
  if (creating.value) return
  if (createDraftHasChanges()) {
    pendingDraftDiscardTarget.value = 'create'
    return
  }
  abortOwnedAccountsRequest()
  showCreate.value = false
  createDraftBaseline.value = null
}

function resetCreateForm(): void {
  Object.assign(createForm, buildDefaultCreateForm())
  allowedModels.value = []
  void loadRoomCatalog(createPlatform.value)
  createErrorMessage.value = ''
  clearPendingCreateRoomIdempotencyKey()
  selectedOwnedAccountID.value = eligibleOwnedAccounts.value[0]?.id || 0
  void nextTick(() => {
    if (showCreate.value) captureCreateDraftBaseline()
  })
}

function restoreCreateDraftBaseline(): void {
  const snapshot = createDraftBaseline.value
  if (!snapshot) {
    resetCreateForm()
    return
  }
  createPlatform.value = snapshot.platform
  selectedOwnedAccountID.value = snapshot.selectedOwnedAccountID
  Object.assign(createForm, snapshot.form)
  allowedModels.value = [...snapshot.allowedModels]
}

function cancelDiscardDraft(): void {
  pendingDraftDiscardTarget.value = null
}

function confirmDiscardDraft(): void {
  const target = pendingDraftDiscardTarget.value
  pendingDraftDiscardTarget.value = null
  if (target === 'create') {
    restoreCreateDraftBaseline()
    abortOwnedAccountsRequest()
    showCreate.value = false
    createDraftBaseline.value = null
    return
  }
  if (target === 'config') {
    void closeConfigEditDialog(true)
  }
}

function selectCreatePlatform(platform: AccountSharePlatform): void {
  if (createPlatform.value === platform || creating.value) return
  const proxyID = createForm.proxy_id
  abortOwnedAccountsRequest()
  createPlatform.value = platform
  Object.assign(createForm, buildDefaultCreateForm(), { proxy_id: proxyID })
  allowedModels.value = []
  void loadRoomCatalog(platform)
  createErrorMessage.value = ''
  selectedOwnedAccountID.value = 0
  ownedAccounts.value = []
  ownedAccountsError.value = ''
  ownedAccountsLoadedPlatform = null
  clearPendingCreateRoomIdempotencyKey()
  void loadOwnedAccounts(true)
}

function validateCreateConfig(): string {
  const accountNameError = validateAccountName(
    createForm.name,
    undefined,
    Number(authStore.user?.id || 0)
  )
  if (accountNameError) return accountNameError
  if (!selectedOwnedAccount.value) return t('accountShare.roomCreate.selectEligible')
  if (!seatOptions.includes(Number(createForm.seat_limit))) return t('accountShare.roomForm.seatRange', { min: ACCOUNT_SHARE_MIN_SEATS, max: ACCOUNT_SHARE_MAX_SEATS })
  if (concurrencyValidationMessage.value) return concurrencyValidationMessage.value
  if (perUserConcurrencyValidationMessage.value) return perUserConcurrencyValidationMessage.value
  if (!Number.isFinite(Number(createForm.rate_multiplier)) || Number(createForm.rate_multiplier) < 0) return t('accountShare.roomForm.rateMin')
  if (!Number.isFinite(Number(createForm.hourly_rate)) || Number(createForm.hourly_rate) < 0) return t('accountShare.roomForm.hourlyMin')
  if (!Number.isFinite(Number(createForm.hourly_fee_waiver_minimum)) || Number(createForm.hourly_fee_waiver_minimum) < 0) return t('accountShare.roomForm.waiverMin')
  if (!Number.isFinite(Number(createForm.min_balance_required)) || Number(createForm.min_balance_required) < 0) return t('accountShare.roomForm.minBalanceMin')
  if (createPlatform.value === 'openai') {
    if (!Number.isFinite(Number(createForm.codex_5h_limit_percent)) || Number(createForm.codex_5h_limit_percent) < 1 || Number(createForm.codex_5h_limit_percent) > 100) return t('accountShare.roomForm.codex5hRange')
    if (!Number.isFinite(Number(createForm.codex_7d_limit_percent)) || Number(createForm.codex_7d_limit_percent) < 1 || Number(createForm.codex_7d_limit_percent) > 100) return t('accountShare.roomForm.codex7dRange')
  } else if (createPlatform.value === 'anthropic') {
    if (!Number.isFinite(Number(createForm.anthropic_5h_limit_percent)) || Number(createForm.anthropic_5h_limit_percent) < 1 || Number(createForm.anthropic_5h_limit_percent) > 100) return t('accountShare.roomForm.claude5hRange')
    if (!Number.isFinite(Number(createForm.anthropic_7d_limit_percent)) || Number(createForm.anthropic_7d_limit_percent) < 1 || Number(createForm.anthropic_7d_limit_percent) > 100) return t('accountShare.roomForm.claude7dRange')
  }
  if (parseAllowedModels().length === 0) return t('accountShare.roomForm.needOneModel')
  const joinPassword = createForm.join_password.trim()
  if (joinPassword && (new TextEncoder().encode(joinPassword).length < 4 || new TextEncoder().encode(joinPassword).length > 64)) {
    return t('accountShare.errors.roomCreate.passwordInvalidLength')
  }
  return ''
}

function parseEditAllowedModels(): string[] {
  return normalizeAllowedModelList(editAllowedModels.value)
}

function validateEditConfig(): string {
  const accountNameError = validateAccountName(
    editForm.name,
    editingConfigListing.value?.id,
    Number(editingConfigListing.value?.owner_user_id || 0)
  )
  if (accountNameError) return accountNameError
  if (!seatOptions.includes(Number(editForm.seat_limit))) return t('accountShare.roomForm.seatRange', { min: ACCOUNT_SHARE_MIN_SEATS, max: ACCOUNT_SHARE_MAX_SEATS })
  if (editPerUserConcurrencyValidationMessage.value) return editPerUserConcurrencyValidationMessage.value
  if (!Number.isFinite(Number(editForm.rate_multiplier)) || Number(editForm.rate_multiplier) < 0) return t('accountShare.roomForm.rateMin')
  if (!Number.isFinite(Number(editForm.hourly_rate)) || Number(editForm.hourly_rate) < 0) return t('accountShare.roomForm.hourlyMin')
  if (!Number.isFinite(Number(editForm.hourly_fee_waiver_minimum)) || Number(editForm.hourly_fee_waiver_minimum) < 0) return t('accountShare.roomForm.waiverMin')
  if (!Number.isFinite(Number(editForm.min_balance_required)) || Number(editForm.min_balance_required) < 0) return t('accountShare.roomForm.minBalanceMin')
  if (listingPlatform(editingConfigListing.value) === 'openai') {
    if (!Number.isFinite(Number(editForm.codex_5h_limit_percent)) || Number(editForm.codex_5h_limit_percent) < 1 || Number(editForm.codex_5h_limit_percent) > 100) return t('accountShare.roomForm.codex5hRange')
    if (!Number.isFinite(Number(editForm.codex_7d_limit_percent)) || Number(editForm.codex_7d_limit_percent) < 1 || Number(editForm.codex_7d_limit_percent) > 100) return t('accountShare.roomForm.codex7dRange')
  } else if (listingPlatform(editingConfigListing.value) === 'anthropic') {
    if (!Number.isFinite(Number(editForm.anthropic_5h_limit_percent)) || Number(editForm.anthropic_5h_limit_percent) < 1 || Number(editForm.anthropic_5h_limit_percent) > 100) return t('accountShare.roomForm.claude5hRange')
    if (!Number.isFinite(Number(editForm.anthropic_7d_limit_percent)) || Number(editForm.anthropic_7d_limit_percent) < 1 || Number(editForm.anthropic_7d_limit_percent) > 100) return t('accountShare.roomForm.claude7dRange')
  }
  if (parseEditAllowedModels().length === 0) return t('accountShare.roomForm.needOneModel')
  // 受保护编辑（房间正在被使用）走无锁定路径，openConsumerProtectedEditDialog 会
  if (
    !Number.isSafeInteger(Number(editingConfigListing.value?.row_version))
    || Number(editingConfigListing.value?.row_version) <= 0
  ) {
    return t('accountShare.roomEdit.staleVersion')
  }
  if (!editReason.value.trim()) return t('accountShare.roomEdit.editReasonRequired')
  if (editForceActive.value && !authStore.isAdmin) return t('accountShare.roomEdit.adminSessionLost')
  const joinPassword = editForm.join_password.trim()
  if (editJoinPasswordClear.value && joinPassword) return t('accountShare.roomEdit.passwordConflict')
  if (joinPassword) {
    const passwordBytes = new TextEncoder().encode(joinPassword).length
    if (passwordBytes < 4 || passwordBytes > 64) return t('accountShare.errors.roomCreate.passwordInvalidLength')
  }
  return ''
}

function listingWithPendingMembershipEnd(
  listing: AccountShareListing
): AccountShareListing {
  const pending = pendingMembershipEnds.value[listing.id]
  if (!pending) return listing
  if (
    listing.current_membership_id
    && listing.current_membership_id !== pending.membershipID
  ) {
    return listing
  }
  const snapshot = pending.listingSnapshot
  const membership = pending.membership
  return {
    ...listing,
    current_membership_id: pending.membershipID,
    current_api_key_id: pending.apiKeyID || membership.api_key_id,
    current_api_key_name: pending.apiKeyName || snapshot.current_api_key_name,
    current_joined_at: membership.joined_at || snapshot.current_joined_at,
    current_paid_until: membership.paid_until || snapshot.current_paid_until,
    current_billed_until: membership.billed_until || snapshot.current_billed_until,
    current_idle_timeout_minutes: membership.idle_timeout_minutes || snapshot.current_idle_timeout_minutes,
    current_last_request_at: membership.last_request_at || snapshot.current_last_request_at,
    current_idle_expires_at: snapshot.current_idle_expires_at,
    current_waiver_progress: snapshot.current_waiver_progress,
    queue_membership_id: undefined,
    queue_api_key_id: undefined,
    queue_api_key_name: undefined,
    queue_rank: undefined,
    queue_status: 'ending',
    queue_idle_timeout_minutes: undefined,
    queue_dispatch_cooldown_until: undefined
  }
}

function syncListingEndingMemberships(resolvedListings: AccountShareListing[]): void {
  const nextPending = { ...pendingMembershipEnds.value }

  for (const listing of resolvedListings) {
    const membershipID = Number(listing.queue_membership_id || 0)
    const isEnding = listing.queue_status === 'ending' && membershipID > 0
    const existing = nextPending[listing.id]

    if (!isEnding) {
      if (
        existing
        && listing.current_membership_id
        && listing.current_membership_id !== existing.membershipID
      ) {
        delete nextPending[listing.id]
      }
      continue
    }

    const operationID = (listing.queue_ending_operation_id || '').trim()
    const operationStatus = (listing.queue_ending_operation_status || '').trim()
    const preserveOperationState = existing?.membershipID === membershipID
      && existing.operationID === operationID
    const membership: AccountShareMembership = {
      id: membershipID,
      listing_id: listing.id,
      account_id: Number(listing.account_id || 0),
      consumer_user_id: authStore.user?.id || 0,
      api_key_id: Number(listing.queue_api_key_id || 0),
      status: 'ending',
      queue_rank: Number(listing.queue_rank || 0),
      idle_timeout_minutes: Number(listing.queue_idle_timeout_minutes || 0),
      joined_at: listing.current_joined_at || listing.updated_at,
      last_request_at: listing.current_last_request_at,
      ending_operation_id: operationID || undefined,
      ending_operation_status: operationStatus || undefined,
      settlement_status: listing.queue_settlement_status,
      paid_until: listing.current_paid_until,
      billed_until: listing.current_billed_until,
      created_at: listing.created_at,
      updated_at: listing.updated_at
    }
    nextPending[listing.id] = {
      ...(preserveOperationState ? existing : {}),
      listingID: listing.id,
      membershipID,
      operationID,
      operationStatus: operationStatus || (preserveOperationState ? existing.operationStatus : 'pending'),
      operationError: preserveOperationState ? existing.operationError : '',
      apiKeyID: listing.queue_api_key_id,
      apiKeyName: listing.queue_api_key_name,
      membership,
      listingSnapshot: listing
    }
  }

  pendingMembershipEnds.value = nextPending
}

function setPendingMembershipEnd(
  pending: PendingEndUseState,
  membership: AccountShareMembership
): void {
  const operationID = (membership.ending_operation_id || '').trim()
  pendingMembershipEnds.value = {
    ...pendingMembershipEnds.value,
    [pending.listing.id]: {
      listingID: pending.listing.id,
      membershipID: membership.id,
      operationID,
      operationStatus: membership.ending_operation_status || 'pending',
      operationCreatedAt: membership.ending_requested_at,
      operationError: '',
      apiKeyID: pending.apiKeyID || membership.api_key_id,
      apiKeyName: pending.apiKeyName,
      membership,
      listingSnapshot: pending.listing
    }
  }
}

function updatePendingMembershipEndOperation(
  listingID: number,
  operation: AccountShareRoomOperation
): void {
  const pending = pendingMembershipEnds.value[listingID]
  if (!pending || pending.operationID !== operation.id) return
  pendingMembershipEnds.value = {
    ...pendingMembershipEnds.value,
    [listingID]: {
      ...pending,
      operationStatus: operation.status,
      operationError: operation.error_message || '',
      operationBlocker: operation.blocker,
      operationCreatedAt: operation.created_at || pending.operationCreatedAt,
      operationUpdatedAt: operation.updated_at,
      lastOperationAttemptAt: Date.now(),
      lastOperationSuccessAt: Date.now(),
      operationQueryError: '',
      operationQueryFailures: 0
    }
  }
}

function removePendingMembershipEnd(listingID: number): PendingMembershipEnd | null {
  const pending = pendingMembershipEnds.value[listingID]
  if (!pending) return null
  const next = { ...pendingMembershipEnds.value }
  delete next[listingID]
  pendingMembershipEnds.value = next
  const controller = membershipEndOperationControllers.get(listingID)
  controller?.abort()
  membershipEndOperationControllers.delete(listingID)
  return pending
}

function loadCurrentView(): Promise<boolean> {
  return isMembershipHistoryView.value ? loadMembershipHistory() : loadListings()
}

async function loadMembershipHistory(): Promise<boolean> {
  abortMembershipHistoryRequest()
  const requestSeq = ++membershipHistoryRequestSeq
  const controller = new AbortController()
  membershipHistoryRequestController = controller
  membershipHistoryLoading.value = true
  membershipHistoryError.value = ''
  try {
    const result = await accountShareAPI.listMembershipHistory(
      membershipHistoryPagination.page,
      membershipHistoryPagination.page_size,
      { signal: controller.signal }
    )
    if (controller.signal.aborted || requestSeq !== membershipHistoryRequestSeq) return false
    membershipHistoryEntries.value = result.items || []
    membershipHistoryPagination.total = result.total || 0
    membershipHistoryPagination.page = result.page || membershipHistoryPagination.page
    membershipHistoryPagination.page_size = result.page_size || ACCOUNT_SHARE_PAGE_SIZE
    membershipHistoryPagination.pages = result.pages || 1
    return true
  } catch (error: unknown) {
    if (
      controller.signal.aborted
      || requestSeq !== membershipHistoryRequestSeq
      || isCanceledRequest(error)
    ) {
      return false
    }
    membershipHistoryEntries.value = []
    membershipHistoryPagination.total = 0
    membershipHistoryPagination.pages = 1
    membershipHistoryError.value = formatAccountShareLoadError(error, t('accountShare.errors.loadFullSpendRecords'))
    return false
  } finally {
    if (requestSeq === membershipHistoryRequestSeq) {
      membershipHistoryLoading.value = false
      if (membershipHistoryRequestController === controller) {
        membershipHistoryRequestController = null
      }
    }
  }
}

async function loadListings(background = false): Promise<boolean> {
  abortActiveListingsRequest()
  const requestSeq = ++listingsRequestSeq
  const controller = new AbortController()
  const requestTab = activeFilter.value.tab
  listingsRequestController = controller
  if (!background) loading.value = true
  errorMessage.value = ''
  try {
    const result = await accountShareAPI.listListings(pagination.page, pagination.page_size, buildListingFilters(requestTab), {
      signal: controller.signal
    })
    if (controller.signal.aborted || requestSeq !== listingsRequestSeq) return false
    const normalizedListings = (result.items || []).map(normalizeListingForMerge)
    syncListingEndingMemberships(normalizedListings)
    const realListings = normalizedListings
      .map(listingWithPendingMembershipEnd)
    pagination.total = result.total || 0
    pagination.total_exact = result.total_exact === true
    pagination.has_more = result.has_more
    pagination.page = result.page || pagination.page
    pagination.page_size = result.page_size || ACCOUNT_SHARE_PAGE_SIZE
    pagination.pages = result.pages || 1
    listings.value = realListings
    if (requestTab === 'archive') {
      visibleValidatingListingIDs.value = new Set()
      clearMembershipStatusRefreshTimer()
      return true
    }
    visibleValidatingListingIDs.value = new Set(
      realListings
        .filter(listing => listing.status === 'validating')
        .map(listing => listing.id)
    )
    syncIdleTimeoutControls(realListings)
    mergeKnownListings(realListings)
    lastMembershipStatusRefreshAt = Date.now()
    scheduleTransientStatusRefresh()
    return true
  } catch (error: unknown) {
    if (controller.signal.aborted || requestSeq !== listingsRequestSeq || isCanceledRequest(error)) return false
    listings.value = []
    pagination.total = 0
    pagination.pages = 1
    errorMessage.value = formatAccountShareLoadError(error, t('accountShare.errors.loadMarketplace'))
    scheduleTransientStatusRefresh()
    return false
  } finally {
    if (requestSeq === listingsRequestSeq) {
      loading.value = false
    }
    if (listingsRequestController === controller) {
      listingsRequestController = null
    }
  }
}

function normalizeListingForMerge(listing: AccountShareListing): AccountShareListing {
  const next: AccountShareListingWithClientMeta = { ...listing }
  if (listing.current_waiver_progress?.enabled) {
    next.waiver_progress_received_at_ms = Date.now()
  } else {
    delete next.waiver_progress_received_at_ms
  }
  return next
}

function mergeListingFields(current: AccountShareListing | undefined, updated: AccountShareListing): AccountShareListing {
  const normalizedUpdate = normalizeListingForMerge(updated)
  if (!current) return normalizedUpdate
  const next = { ...current, ...normalizedUpdate }
  if (!updated.current_waiver_progress?.enabled) {
    next.current_waiver_progress = undefined
    delete (next as AccountShareListingWithClientMeta).waiver_progress_received_at_ms
  }
  return next
}

function mergeKnownListings(items: AccountShareListing[]): void {
  if (items.length === 0) return
  const byID = new Map<number, AccountShareListing>()
  for (const listing of knownListings.value) byID.set(listing.id, listing)
  for (const listing of items) {
    byID.set(listing.id, mergeListingFields(byID.get(listing.id), listing))
  }
  knownListings.value = Array.from(byID.values())
}

function removeKnownListing(listingID: number): void {
  knownListings.value = knownListings.value.filter((listing) => listing.id !== listingID)
}

/** 供房间生命周期弹窗在刷新列表后取回同一房间的最新快照。 */
function findListingById(listingID: number): AccountShareListing | undefined {
  return listings.value.find((listing) => listing.id === listingID)
}

async function loadListingNameIndex(updateSuggestedName = true): Promise<void> {
  try {
    const ownerUserID = Number(authStore.user?.id || 0)
    const result = await accountShareAPI.listListings(1, 100, {
      tab: 'mine',
      status: 'all',
      owner_user_id: ownerUserID > 0 ? ownerUserID : undefined
    })
    mergeKnownListings(result.items || [])
    if (updateSuggestedName && (!createForm.name.trim() || accountNameValidationMessage.value)) {
      createForm.name = suggestedAccountName()
    }
  } catch {
    // 名称重复仍由创建接口兜底，这里只做前端提示索引。
  }
}

function abortOwnerDialogRequests(): void {
  ownerDialogRequestSeq += 1
  ownerListingsRequestController?.abort()
  ownerListingsRequestController = null
  ownerReviewsRequestController?.abort()
  ownerReviewsRequestController = null
  ownerDialog.loadingListings = false
  ownerDialog.loadingReviews = false
}

function closeOwnerDialog(): void {
  abortOwnerDialogRequests()
  ownerDialog.show = false
  ownerDialog.ownerUserID = 0
  ownerDialog.ownerUsername = ''
  ownerDialog.sourceListing = null
  ownerDialog.tab = 'listings'
  ownerDialog.listings = []
  ownerDialog.reviews = []
  ownerDialog.listingsPage = 1
  ownerDialog.listingsHasMore = false
  ownerDialog.listingsTotalExact = true
  ownerDialog.listingsTotal = 0
  ownerDialog.reviewsPage = 1
  ownerDialog.reviewsPages = 1
  ownerDialog.reviewsTotal = 0
  ownerDialog.listingsError = ''
  ownerDialog.reviewsError = ''
}

async function openOwnerDialog(listing: AccountShareListing): Promise<void> {
  abortOwnerDialogRequests()
  ownerDialog.show = true
  ownerDialog.ownerUserID = listing.owner_user_id
  ownerDialog.ownerUsername = ownerDisplayName(listing)
  ownerDialog.sourceListing = listing
  ownerDialog.tab = 'listings'
  ownerDialog.listingsError = ''
  ownerDialog.reviewsError = ''
  ownerDialog.listings = []
  ownerDialog.reviews = []
  ownerDialog.listingsPage = 1
  ownerDialog.listingsHasMore = false
  ownerDialog.listingsTotalExact = true
  ownerDialog.listingsTotal = 0
  ownerDialog.reviewsPage = 1
  ownerDialog.reviewsPages = 1
  ownerDialog.reviewsTotal = 0
  await Promise.all([loadOwnerListings(), loadOwnerReviews()])
}

function searchOwnerFromDialog(): void {
  const ownerUserID = Number(ownerDialog.ownerUserID || 0)
  if (!Number.isSafeInteger(ownerUserID) || ownerUserID <= 0) return
  selectedOwnerID.value = ownerUserID
  selectedOwnerDisplayName.value = ownerDialog.ownerUsername || t('accountShare.common.userId', { id: ownerUserID })
  if (searchQuery.value !== '') {
    suppressNextSearchRefresh = true
    searchQuery.value = ''
  }
  pagination.page = 1
  closeOwnerDialog()
  applyListingFilters()
}

async function loadOwnerListings(append = false): Promise<void> {
  if (!ownerDialog.ownerUserID || ownerDialog.loadingListings) return
  const requestSeq = ownerDialogRequestSeq
  const ownerUserID = ownerDialog.ownerUserID
  const platform = activeListingPlatform.value
  const page = append ? ownerDialog.listingsPage + 1 : 1
  const controller = new AbortController()
  ownerListingsRequestController?.abort()
  ownerListingsRequestController = controller
  ownerDialog.loadingListings = true
  ownerDialog.listingsError = ''
  try {
    const result = await accountShareAPI.listListings(page, OWNER_LISTINGS_PAGE_SIZE, {
      tab: 'all',
      status: 'all',
      platform,
      owner_user_id: ownerUserID,
      sort_by: 'rating',
      sort_order: 'desc'
    }, { signal: controller.signal })
    if (
      controller.signal.aborted
      || requestSeq !== ownerDialogRequestSeq
      || !ownerDialog.show
      || ownerDialog.ownerUserID !== ownerUserID
      || activeListingPlatform.value !== platform
    ) return
    const nextItems = result.items || []
    if (append) {
      const byID = new Map(ownerDialog.listings.map(item => [item.id, item]))
      for (const item of nextItems) byID.set(item.id, item)
      ownerDialog.listings = Array.from(byID.values())
    } else {
      ownerDialog.listings = nextItems
    }
    ownerDialog.listingsPage = result.page || page
    ownerDialog.listingsHasMore = result.has_more
    ownerDialog.listingsTotalExact = result.total_exact
    ownerDialog.listingsTotal = Math.max(ownerDialog.listings.length, result.total || 0)
    ownerDialog.listingsError = ''
  } catch (error: unknown) {
    if (controller.signal.aborted || requestSeq !== ownerDialogRequestSeq || isCanceledRequest(error)) return
    ownerDialog.listingsError = extractApiErrorMessage(error, t('accountShare.errors.loadOwnerAccounts'))
  } finally {
    if (requestSeq === ownerDialogRequestSeq && ownerListingsRequestController === controller) {
      ownerListingsRequestController = null
      ownerDialog.loadingListings = false
    }
  }
}

function loadMoreOwnerListings(): void {
  void loadOwnerListings(true)
}

async function loadOwnerReviews(append = false): Promise<void> {
  if (!ownerDialog.ownerUserID || ownerDialog.loadingReviews) return
  const requestSeq = ownerDialogRequestSeq
  const ownerUserID = ownerDialog.ownerUserID
  const page = append ? ownerDialog.reviewsPage + 1 : 1
  const controller = new AbortController()
  ownerReviewsRequestController?.abort()
  ownerReviewsRequestController = controller
  ownerDialog.loadingReviews = true
  ownerDialog.reviewsError = ''
  try {
    const result = await accountShareAPI.listOwnerReviews(
      ownerUserID,
      page,
      OWNER_REVIEWS_PAGE_SIZE,
      { signal: controller.signal }
    )
    if (
      controller.signal.aborted
      || requestSeq !== ownerDialogRequestSeq
      || !ownerDialog.show
      || ownerDialog.ownerUserID !== ownerUserID
    ) return
    const nextItems = result.items || []
    if (append) {
      const byID = new Map(ownerDialog.reviews.map(item => [item.id, item]))
      for (const item of nextItems) byID.set(item.id, item)
      ownerDialog.reviews = Array.from(byID.values())
    } else {
      ownerDialog.reviews = nextItems
    }
    ownerDialog.reviewsPage = result.page || page
    ownerDialog.reviewsPages = Math.max(ownerDialog.reviewsPage, result.pages || 1)
    ownerDialog.reviewsTotal = Math.max(ownerDialog.reviews.length, result.total || 0)
    ownerDialog.reviewsError = ''
  } catch (error: unknown) {
    if (controller.signal.aborted || requestSeq !== ownerDialogRequestSeq || isCanceledRequest(error)) return
    ownerDialog.reviewsError = extractApiErrorMessage(error, t('accountShare.errors.loadOwnerReviews'))
  } finally {
    if (requestSeq === ownerDialogRequestSeq && ownerReviewsRequestController === controller) {
      ownerReviewsRequestController = null
      ownerDialog.loadingReviews = false
    }
  }
}

function loadMoreOwnerReviews(): void {
  void loadOwnerReviews(true)
}

async function listAllModeApiKeys(
  accountModeGroupID: number,
  requestSeq: number
): Promise<ApiKey[]> {
  const keysByID = new Map<number, ApiKey>()
  let page = 1
  let totalPages = 1

  do {
    if (requestSeq !== modeKeysRequestSeq) return []
    const result = await keysAPI.list(page, ACCOUNT_SHARE_MODE_KEY_PAGE_SIZE, {
      group_id: accountModeGroupID,
      status: 'active'
    })
    if (requestSeq !== modeKeysRequestSeq) return []

    for (const key of result.items || []) {
      if (Number.isSafeInteger(key.id) && key.id > 0) keysByID.set(key.id, key)
    }

    const reportedPages = Number(result.pages ?? 1)
    if (!Number.isSafeInteger(reportedPages) || reportedPages < 0) {
      throw new Error(t('accountShare.errors.modeKeyPaginationInvalid'))
    }
    totalPages = Math.max(totalPages, reportedPages, 1)
    page += 1
  } while (page <= totalPages)

  return Array.from(keysByID.values())
}

async function loadModeKeys(): Promise<void> {
  const requestSeq = ++modeKeysRequestSeq
  for (const option of ACCOUNT_SHARE_PLATFORM_OPTIONS.value) {
    modeKeysLoadingByPlatform[option.value] = true
    modeKeysLoadedByPlatform[option.value] = false
    modeKeysErrorByPlatform[option.value] = ''
  }

  try {
    const modeGroups = await accountShareAPI.listModeGroups()
    if (requestSeq !== modeKeysRequestSeq) return
    for (const option of ACCOUNT_SHARE_PLATFORM_OPTIONS.value) {
      const groupID = Number(modeGroups.find(group => group.platform === option.value)?.group_id || 0)
      modeGroupIDsByPlatform[option.value] = Number.isSafeInteger(groupID) && groupID > 0 ? groupID : 0
      // A deployment may enable new providers incrementally. Keep existing
      // platforms usable when a provider's mode group is not configured yet;
      // that tab will explain the missing mapping and remain non-joinable.
      if (modeGroupIDsByPlatform[option.value] <= 0) {
        modeKeysErrorByPlatform[option.value] = t('accountShare.errors.platformGroupMissing', { label: option.label })
        modeKeysLoadingByPlatform[option.value] = false
      }
    }

    const configuredOptions = ACCOUNT_SHARE_PLATFORM_OPTIONS.value.filter(option => modeGroupIDsByPlatform[option.value] > 0)
    const results = await Promise.allSettled(configuredOptions.map(async option => {
      const platform = option.value
      try {
        const accountModeGroupID = modeGroupIDsByPlatform[platform]
        const allKeys = await listAllModeApiKeys(accountModeGroupID, requestSeq)
        const keys = allKeys.filter(key => isUsableModeApiKey(key, accountModeGroupID))

        if (requestSeq === modeKeysRequestSeq) {
          modeApiKeysByPlatform[platform] = keys
          clearInvalidSelectedModeApiKeys(platform, keys)
          modeKeysLoadedByPlatform[platform] = true
          modeKeysErrorByPlatform[platform] = ''
        }
      } finally {
        if (requestSeq === modeKeysRequestSeq) modeKeysLoadingByPlatform[platform] = false
      }
    }))

    if (requestSeq !== modeKeysRequestSeq) return
    results.forEach((result, index) => {
      const platform = configuredOptions[index].value
      if (result.status === 'fulfilled') return

      modeApiKeysByPlatform[platform] = []
      clearInvalidSelectedModeApiKeys(platform, [])
      modeKeysLoadedByPlatform[platform] = false
      modeKeysErrorByPlatform[platform] = extractApiErrorMessage(result.reason, t('accountShare.errors.loadModeKeys'))
      modeKeysLoadingByPlatform[platform] = false
    })
    syncRecommendationApiKey()
  } catch (error: unknown) {
    if (requestSeq !== modeKeysRequestSeq) return
    const message = extractApiErrorMessage(error, t('accountShare.errors.loadGroups'))
    for (const option of ACCOUNT_SHARE_PLATFORM_OPTIONS.value) {
      modeGroupIDsByPlatform[option.value] = 0
      modeApiKeysByPlatform[option.value] = []
      clearInvalidSelectedModeApiKeys(option.value, [])
      modeKeysLoadedByPlatform[option.value] = false
      modeKeysErrorByPlatform[option.value] = message
    }
  } finally {
    if (requestSeq === modeKeysRequestSeq) {
      for (const option of ACCOUNT_SHARE_PLATFORM_OPTIONS.value) {
        modeKeysLoadingByPlatform[option.value] = false
      }
    }
  }

  if (requestSeq === modeKeysRequestSeq) {
    const failedPlatforms = ACCOUNT_SHARE_PLATFORM_OPTIONS.value
      .filter(option => !modeKeysLoadedByPlatform[option.value] && modeKeysErrorByPlatform[option.value])
      .map(option => option.label)
    if (failedPlatforms.length > 0) {
      const suffix = failedPlatforms.length === ACCOUNT_SHARE_PLATFORM_OPTIONS.value.length
        ? t('accountShare.errors.clickRefresh')
        : t('accountShare.errors.otherPlatformsOk')
      appStore.showWarning(t('accountShare.errors.modeKeysPartial', { platforms: failedPlatforms.join('、'), suffix: suffix }))
    }
  }
}

function refreshModeKeysInBackground(): void {
  void loadModeKeys().catch((error: unknown) => {
    appStore.showWarning(extractApiErrorMessage(error, t('accountShare.errors.modeKeysRefreshFailed')))
  })
}

function abortRecommendationRequest(): void {
  recommendationRequestSeq += 1
  recommendationRequestController?.abort()
  recommendationRequestController = null
  recommendationLoading.value = false
}

function abortRecommendationUsageProfileRequest(): void {
  recommendationUsageProfileRequestSeq += 1
  recommendationUsageProfileController?.abort()
  recommendationUsageProfileController = null
  recommendationUsageProfileLoading.value = false
}

function abortRecommendationAsyncRequests(): void {
  abortRecommendationRequest()
  abortRecommendationUsageProfileRequest()
}

function resetRecommendationResult(options: { keepUsageProfileMessage?: boolean } = {}): void {
  recommendationResult.value = null
  recommendationRequestSnapshot.value = null
  recommendationError.value = ''
  recommendationPage.value = 1
  if (!options.keepUsageProfileMessage) {
    recommendationUsageProfileMessage.value = ''
  }
}

function syncRecommendationApiKey(): void {
  const keys = recommendationKeyOptions.value
  const selectedID = Number(recommendationForm.api_key_id || 0)
  if (selectedID > 0 && keys.some(item => item.id === selectedID)) return
  recommendationForm.api_key_id = keys[0]?.id || 0
}

function syncRecommendationFormForPlatform(platform: AccountSharePlatform = activeListingPlatform.value): void {
  const models = new Set<string>()
  for (const listing of [...knownListings.value, ...listings.value]) {
    if (listingPlatform(listing) !== platform) continue
    for (const model of listing.allowed_models) {
      const value = model.trim()
      if (value) models.add(value)
    }
  }
  if (!models.has(recommendationForm.model)) {
    recommendationForm.model = Array.from(models).sort((a, b) => a.localeCompare(b))[0] || ''
  }
  syncRecommendationApiKey()
}

function applyRecommendationPreset(key: RecommendationPresetKey): void {
  const preset = recommendationPresets.value.find(item => item.key === key)
  if (!preset) return
  selectedRecommendationPreset.value = key
  recommendationForm.request_count = preset.request_count
  recommendationForm.active_hours = preset.active_hours
  recommendationForm.input_tokens_per_request = preset.input_tokens_per_request
  recommendationForm.output_tokens_per_request = preset.output_tokens_per_request
  recommendationForm.cache_creation_tokens_per_request = preset.cache_creation_tokens_per_request
  recommendationForm.cache_read_tokens_per_request = preset.cache_read_tokens_per_request
  recommendationForm.image_input_tokens_per_request = preset.image_input_tokens_per_request
  recommendationForm.image_output_tokens_per_request = preset.image_output_tokens_per_request
  recommendationForm.image_cache_read_tokens_per_request = preset.image_cache_read_tokens_per_request
  resetRecommendationResult()
}

function applyRecommendationUsageProfileToForm(profile: AccountShareRecommendationUsageProfile): void {
  recommendationForm.request_count = profile.request_count
  recommendationForm.active_hours = profile.active_hours
  recommendationForm.input_tokens_per_request = profile.input_tokens_per_request
  recommendationForm.output_tokens_per_request = profile.output_tokens_per_request
  recommendationForm.cache_creation_tokens_per_request = profile.cache_creation_tokens_per_request
  recommendationForm.image_input_tokens_per_request = profile.image_input_tokens_per_request
  recommendationForm.image_output_tokens_per_request = profile.image_output_tokens_per_request
}

function buildRecommendationUsageProfileMessage(profile: AccountShareRecommendationUsageProfile): string {
  const prefix = profile.used_model_fallback
    ? t('accountShare.estimate.filledByAllModels')
    : t('accountShare.estimate.filledByHistory')
  const capped = profile.capped ? t('accountShare.estimate.cappedSuffix') : ''
  const activeHours = normalizeRecommendationActiveHours(profile.active_hours)
  const requestsPerHour = profile.request_count / activeHours
  return t('accountShare.estimate.profileSummary', { prefix, inputTokensPerRequest: formatNumber(profile.input_tokens_per_request), outputTokensPerRequest: formatNumber(profile.output_tokens_per_request), cacheCreationTokensPerRequest: formatNumber(profile.cache_creation_tokens_per_request), cacheReadTokensPerRequest: formatNumber(profile.cache_read_tokens_per_request), imageInputTokensPerRequest: formatNumber(profile.image_input_tokens_per_request), imageOutputTokensPerRequest: formatNumber(profile.image_output_tokens_per_request), requestCount: profile.request_count, activeHours: formatNumber(activeHours), requestsPerHour: formatNumber(requestsPerHour), capped })
}

async function applyRecentUsageProfile(): Promise<void> {
  if (recommendationUsageProfileLoading.value || recommendationLoading.value) return
  recommendationUsageProfileMessage.value = ''
  recommendationError.value = ''
  syncRecommendationFormForPlatform()
  const request = {
    platform: activeListingPlatform.value,
    model: recommendationForm.model.trim(),
    days: 3
  }
  const requestSeq = ++recommendationUsageProfileRequestSeq
  const controller = new AbortController()
  recommendationUsageProfileController?.abort()
  recommendationUsageProfileController = controller
  recommendationUsageProfileLoading.value = true
  try {
    const profile = await accountShareAPI.getRecommendationUsageProfile(
      request,
      { signal: controller.signal }
    )
    if (
      controller.signal.aborted
      || requestSeq !== recommendationUsageProfileRequestSeq
      || !showRecommendationDialog.value
      || activeListingPlatform.value !== request.platform
      || recommendationForm.model.trim() !== request.model
    ) return
    if (!profile.has_history) {
      recommendationUsageProfileMessage.value = t('accountShare.estimate.noHistory')
      return
    }
    recommendationUsageProfileController = null
    recommendationUsageProfileLoading.value = false
    selectedRecommendationPreset.value = 'history'
    applyRecommendationUsageProfileToForm(profile)
    resetRecommendationResult({ keepUsageProfileMessage: true })
    recommendationUsageProfileMessage.value = buildRecommendationUsageProfileMessage(profile)
  } catch (error: unknown) {
    if (controller.signal.aborted || requestSeq !== recommendationUsageProfileRequestSeq || isCanceledRequest(error)) return
    recommendationUsageProfileMessage.value = extractApiErrorMessage(error, t('accountShare.estimate.historyLoadFailed'))
  } finally {
    if (
      requestSeq === recommendationUsageProfileRequestSeq
      && recommendationUsageProfileController === controller
    ) {
      recommendationUsageProfileController = null
      recommendationUsageProfileLoading.value = false
    }
  }
}

function validateRecommendationForm(): string {
  if (modeKeysLoading.value) return t('accountShare.estimate.keysLoading')
  if (!modeKeysLoaded.value) return t('accountShare.estimate.keysNotLoaded')
  if (recommendationKeyOptions.value.length === 0) return t('accountShare.estimate.createKeyFirst', { group: accountModeGroupName(activeListingPlatform.value)  })
  const apiKeyID = Number(recommendationForm.api_key_id || 0)
  if (apiKeyID <= 0 || !recommendationKeyOptions.value.some(item => item.id === apiKeyID)) return t('accountShare.estimate.selectKey')
  if (!recommendationForm.model.trim()) return t('accountShare.estimate.selectModel')
  const requestCount = Number(recommendationForm.request_count)
  if (!Number.isFinite(requestCount) || requestCount <= 0 || !Number.isInteger(requestCount)) return t('accountShare.estimate.countPositive')
  const activeHours = Number(recommendationForm.active_hours)
  if (!Number.isFinite(activeHours) || activeHours <= 0) return t('accountShare.estimate.hoursPositive')
  const tokenFields = [
    recommendationForm.input_tokens_per_request,
    recommendationForm.output_tokens_per_request,
    recommendationForm.cache_creation_tokens_per_request,
    recommendationForm.cache_read_tokens_per_request,
    recommendationForm.image_input_tokens_per_request,
    recommendationForm.image_output_tokens_per_request,
    recommendationForm.image_cache_read_tokens_per_request
  ]
  if (tokenFields.some(value => !Number.isFinite(Number(value)) || Number(value) < 0 || !Number.isInteger(Number(value)))) {
    return t('accountShare.estimate.tokenNonNegative')
  }
  return ''
}

async function runRecommendation(): Promise<void> {
  if (recommendationLoading.value) return
  recommendationError.value = ''
  syncRecommendationFormForPlatform()
  const validationError = validateRecommendationForm()
  if (validationError) {
    recommendationError.value = validationError
    return
  }
  const payload: AccountShareRecommendationRequest = {
    platform: activeListingPlatform.value,
    model: recommendationForm.model.trim(),
    api_key_id: Number(recommendationForm.api_key_id),
    request_count: Number(recommendationForm.request_count),
    active_hours: Number(recommendationForm.active_hours),
    input_tokens_per_request: Number(recommendationForm.input_tokens_per_request),
    output_tokens_per_request: Number(recommendationForm.output_tokens_per_request),
    cache_creation_tokens_per_request: Number(recommendationForm.cache_creation_tokens_per_request),
    cache_read_tokens_per_request: Number(recommendationForm.cache_read_tokens_per_request),
    image_input_tokens_per_request: Number(recommendationForm.image_input_tokens_per_request),
    image_output_tokens_per_request: Number(recommendationForm.image_output_tokens_per_request),
    image_cache_read_tokens_per_request: Number(recommendationForm.image_cache_read_tokens_per_request),
    limit: ACCOUNT_SHARE_RECOMMENDATION_LIMIT
  }
  const requestSeq = ++recommendationRequestSeq
  const controller = new AbortController()
  recommendationRequestController?.abort()
  recommendationRequestController = controller
  recommendationLoading.value = true
  try {
    const result = await accountShareAPI.recommendListings(payload, { signal: controller.signal })
    if (
      controller.signal.aborted
      || requestSeq !== recommendationRequestSeq
      || !showRecommendationDialog.value
    ) return
    recommendationResult.value = result
    recommendationRequestSnapshot.value = payload
    recommendationPage.value = 1
    const recommendedListings = (result.items || []).map(item => item.listing)
    mergeKnownListings(recommendedListings)
    syncIdleTimeoutControls(recommendedListings)
  } catch (error: unknown) {
    if (controller.signal.aborted || requestSeq !== recommendationRequestSeq || isCanceledRequest(error)) return
    recommendationResult.value = null
    recommendationRequestSnapshot.value = null
    recommendationError.value = extractApiErrorMessage(error, t('accountShare.estimate.failed'), accountShareRecommendationErrorMessages())
  } finally {
    if (requestSeq === recommendationRequestSeq && recommendationRequestController === controller) {
      recommendationRequestController = null
      recommendationLoading.value = false
    }
  }
}

function useRecommendedListing(candidate: AccountShareRecommendationCandidate): void {
  const requestSnapshot = recommendationRequestSnapshot.value
  if (!requestSnapshot || !recommendationResult.value) {
    recommendationError.value = t('accountShare.estimate.comparisonStale')
    return
  }
  const currentApiKeyID = Number(recommendationForm.api_key_id || 0)
  if (currentApiKeyID !== requestSnapshot.api_key_id) {
    recommendationError.value = t('accountShare.estimate.keyChanged')
    return
  }
  if (!recommendationKeyOptions.value.some(item => item.id === requestSnapshot.api_key_id)) {
    recommendationError.value = t('accountShare.estimate.keyUnavailable')
    return
  }
  const listing = candidate.listing
  mergeKnownListings([listing])
  selectedKeyByListing[listing.id] = requestSnapshot.api_key_id
  if (!idleTimeoutByListing[listing.id]) {
    idleTimeoutByListing[listing.id] = DEFAULT_ACCOUNT_SHARE_IDLE_TIMEOUT_MINUTES
  }
  void joinUse(listing)
}

async function loadOwnedAccounts(force = false): Promise<void> {
  const platform = createPlatform.value
  if (!force && (ownedAccountsLoading.value || ownedAccountsLoadedPlatform === platform)) return

  const requestVersion = ++ownedAccountsRequestVersion
  ownedAccountsRequestController?.abort()
  const controller = new AbortController()
  ownedAccountsRequestController = controller
  ownedAccountsLoading.value = true
  ownedAccountsError.value = ''
  try {
    const loadedAccounts = await loadAllPaginatedItems(
      (page) => accountsAPI.list(
        page,
        100,
        { platform, status: 'active' },
        { signal: controller.signal }
      ),
      {
        signal: controller.signal,
        isCurrent: () => (
          requestVersion === ownedAccountsRequestVersion
          && platform === createPlatform.value
        ),
        concurrency: 3
      }
    )

    const accountByID = new Map<number, Account>()
    for (const account of loadedAccounts) accountByID.set(account.id, account)
    ownedAccounts.value = Array.from(accountByID.values())
    ownedAccountsLoadedPlatform = platform
    const shouldAdvanceDraftBaseline = showCreate.value && !createDraftHasChanges()
    if (!eligibleOwnedAccounts.value.some(account => account.id === selectedOwnedAccountID.value)) {
      selectedOwnedAccountID.value = eligibleOwnedAccounts.value[0]?.id || 0
    }
    if (shouldAdvanceDraftBaseline) {
      await nextTick()
      if (showCreate.value) captureCreateDraftBaseline()
    }
  } catch (error: unknown) {
    if (
      requestVersion !== ownedAccountsRequestVersion
      || controller.signal.aborted
      || isCanceledRequest(error)
    ) return
    const shouldAdvanceDraftBaseline = showCreate.value && !createDraftHasChanges()
    ownedAccounts.value = []
    ownedAccountsLoadedPlatform = null
    selectedOwnedAccountID.value = 0
    ownedAccountsError.value = extractApiErrorMessage(error, t('accountShare.errors.loadOwnedAccounts'))
    if (shouldAdvanceDraftBaseline) {
      await nextTick()
      if (showCreate.value) captureCreateDraftBaseline()
    }
  } finally {
    if (requestVersion === ownedAccountsRequestVersion) {
      ownedAccountsLoading.value = false
      if (ownedAccountsRequestController === controller) {
        ownedAccountsRequestController = null
      }
    }
  }
}

function abortOwnedAccountsRequest(): void {
  ownedAccountsRequestVersion += 1
  ownedAccountsRequestController?.abort()
  ownedAccountsRequestController = null
  ownedAccountsLoading.value = false
}

function buildCreateRoomPayload(accountID: number): Omit<CreateAccountShareRoomRequest, 'idempotency_key'> {
  return {
    account_id: accountID,
    room_name: createForm.name.trim(),
    seat_limit: Number(createForm.seat_limit),
    rate_multiplier: Number(createForm.rate_multiplier),
    allowed_models: parseAllowedModels(),
    per_user_concurrency: Number(createForm.per_user_concurrency),
    hourly_rate: Number(createForm.hourly_rate),
    hourly_fee_waiver_minimum: Number(createForm.hourly_fee_waiver_minimum),
    min_balance_required: Number(createForm.min_balance_required),
    codex_cli_only: createForm.codex_cli_only,
    codex_5h_limit_percent: Number(createForm.codex_5h_limit_percent),
    codex_7d_limit_percent: Number(createForm.codex_7d_limit_percent),
    anthropic_5h_limit_percent: Number(createForm.anthropic_5h_limit_percent),
    anthropic_7d_limit_percent: Number(createForm.anthropic_7d_limit_percent),
    join_password: createForm.join_password.trim() || undefined
  }
}

function clearPendingCreateRoomIdempotencyKey(): void {
  pendingCreateRoomIntentSignature = ''
  pendingCreateRoomIdempotencyKey = ''
}


function clearStableIdempotencyIntent(intent: StableIdempotencyIntent): void {
  intent.signature = ''
  intent.key = ''
}

function getStableIdempotencyKey(
  intent: StableIdempotencyIntent,
  prefix: string,
  payload: unknown
): string {
  const signature = JSON.stringify(payload)
  if (intent.key && intent.signature === signature) return intent.key
  intent.signature = signature
  intent.key = `${prefix}-${createSecureRequestID()}`
  return intent.key
}

function getCreateRoomIdempotencyKey(
  payload: Omit<CreateAccountShareRoomRequest, 'idempotency_key'>
): string {
  const intentSignature = JSON.stringify(payload)
  if (
    pendingCreateRoomIdempotencyKey
    && pendingCreateRoomIntentSignature === intentSignature
  ) {
    return pendingCreateRoomIdempotencyKey
  }
  const requestID = createSecureRequestID()
  pendingCreateRoomIntentSignature = intentSignature
  pendingCreateRoomIdempotencyKey = `account-share-room-${payload.account_id}-${requestID}`
  return pendingCreateRoomIdempotencyKey
}

async function createRoomFromOwnedAccount(): Promise<void> {
  if (creating.value) return
  createErrorMessage.value = ''
  const validationError = validateCreateConfig()
  if (validationError) {
    createErrorMessage.value = validationError
    return
  }
  const account = selectedOwnedAccount.value
  if (!account) {
    createErrorMessage.value = t('accountShare.roomCreate.accountStale')
    return
  }

  creating.value = true
  try {
    const intentPayload = buildCreateRoomPayload(account.id)
    const created = await accountShareAPI.createRoom({
      ...intentPayload,
      idempotency_key: getCreateRoomIdempotencyKey(intentPayload)
    })
    clearPendingCreateRoomIdempotencyKey()
    mergeKnownListings([created])
    appStore.showSuccess(
      account.external_placement?.target === 'public_pool'
        || (!account.external_placement && account.share_mode === 'public')
        ? t('accountShare.roomCreate.createdFromPool')
        : t('accountShare.roomCreate.created')
    )
    resetCreateForm()
    showCreate.value = false
    createDraftBaseline.value = null
    await Promise.all([loadOwnedAccounts(true), loadListings(), loadCapabilities()])
  } catch (error: unknown) {
    createErrorMessage.value = extractApiErrorMessage(
      error,
      t('accountShare.errors.createRoom'),
      accountShareRoomCreateErrorMessages()
    )
  } finally {
    creating.value = false
  }
}

function openRoomAccountsDialog(listing: AccountShareListing): void {
  if (listing.deleted) {
    showActionError(t('accountShare.detail.deletedNoManage'), t('accountShare.lifecycle.roomDeleted'))
    return
  }
  roomAccountsListing.value = listing
}

function closeRoomAccountsDialog(): void {
  roomAccountsListing.value = null
}

async function handleRoomAccountsChanged(payload: {
  operation: 'add' | 'remove'
  success: number
  failed: number
}): Promise<void> {
  const listingID = roomAccountsListing.value?.id
  if (!listingID) return

  const actionLabel = payload.operation === 'add' ? t('accountShare.roomAccounts.createFlow.attaching') : t('accountShare.actions.leaveRoom')
  if (payload.failed > 0) {
    appStore.showWarning(
      t('accountShare.roomAccounts.partialResult', { action: actionLabel, success: payload.success, failed: payload.failed  })
    )
  } else {
    appStore.showSuccess(
      payload.operation === 'add'
        ? t('accountShare.roomAccounts.joined', { count: payload.success  })
        : t('accountShare.roomAccounts.left', { count: payload.success  })
    )
  }

  const [refreshed] = await Promise.all([loadListings(), loadCapabilities()])
  if (!refreshed) {
    appStore.showWarning(t('accountShare.roomAccounts.refreshFailed'))
    return
  }

  const refreshedListing = listings.value.find((listing) => listing.id === listingID)
  if (refreshedListing) {
    roomAccountsListing.value = refreshedListing
  } else {
    roomAccountsListing.value = null
  }
}


/**
 * 打开房间生命周期弹窗。
 * 权限判定留在父视图；弹窗内部的状态机、轮询与幂等键都在 RoomLifecycleDialog 中。
 */
function openRoomLifecycleDialog(listing: AccountShareListing): void {
  if (listing.deleted || (!authStore.isAdmin && !isOwnListing(listing))) return
  roomLifecycleListing.value = listing
}

async function joinUse(listing: AccountShareListing): Promise<void> {
  if (
    preparingJoinId.value !== null ||
    joiningId.value !== null ||
    pendingJoinConfirmation.value !== null
  ) {
    return
  }
  errorMessage.value = ''
  if (isListingMembershipEnding(listing)) {
    showActionError(t('accountShare.join.membershipEndingSuffix'), t('accountShare.join.tempUnavailable'))
    return
  }
  if (isOwnListing(listing) && ownerSelfUseRateMultiplier.value === null) {
    showActionError(
      selfUseSettingsError.value || t('accountShare.errors.selfUseConfigNotLoaded'),
      t('accountShare.errors.selfUseUnavailable')
    )
    return
  }
  const platform = listingPlatform(listing)
  if (modeKeysLoadingForPlatform(platform) || !modeKeysLoadedForPlatform(platform)) {
    showModeApiKeyRequiredDialog(listing)
    return
  }
  const apiKeyID = selectedModeApiKeyID(listing)
  if (!apiKeyID) {
    showModeApiKeyRequiredDialog(listing)
    return
  }
  const idleTimeoutValue = idleTimeoutByListing[listing.id] ?? 0
  const idleTimeoutError = validateIdleTimeoutMinutes(idleTimeoutValue)
  if (idleTimeoutError) {
    showActionError(idleTimeoutError, t('accountShare.errors.idleInvalid'))
    return
  }

  const idleTimeoutMinutes = normalizeIdleTimeoutMinutes(idleTimeoutValue)
  const key = modeApiKeysForListing(listing).find(item => item.id === apiKeyID)
  const apiKeyLabel = key ? modeKeyLabel(key) : `Key #${apiKeyID}`
  // 设置了加入密码的房间先收集密码；号主自用免密。
  if (listing.has_password && !isOwnListing(listing)) {
    joinPasswordPrompt.value = { listing, platform, apiKeyID, apiKeyLabel, idleTimeoutMinutes }
    joinPasswordInput.value = ''
    joinPasswordError.value = ''
    return
  }
  await issueJoinIntent(listing, platform, apiKeyID, apiKeyLabel, idleTimeoutMinutes)
}

async function issueJoinIntent(
  listing: AccountShareListing,
  platform: AccountSharePlatform,
  apiKeyID: number,
  apiKeyLabel: string,
  idleTimeoutMinutes: number,
  password = ''
): Promise<void> {
  preparingJoinId.value = listing.id
  joinIntentError.value = ''
  try {
    const intent = await requestJoinIntent(
      listing.id,
      apiKeyID,
      idleTimeoutMinutes,
      password || undefined
    )
    joinUnverifiedAcknowledged.value = false
    pendingJoinConfirmation.value = {
      listingID: listing.id,
      ownerSelfUse: isOwnListing(listing),
      platform,
      apiKeyID,
      apiKeyLabel,
      idleTimeoutMinutes,
      intent
    }
    joinPasswordPrompt.value = null
    joinPasswordInput.value = ''
    joinPasswordError.value = ''
  } catch (error: unknown) {
    const errorCode = extractApiErrorCode(error)
    if (
      joinPasswordPrompt.value
      && (errorCode === 'ACCOUNT_SHARE_ROOM_PASSWORD_REQUIRED' || errorCode === 'ACCOUNT_SHARE_ROOM_PASSWORD_INVALID')
    ) {
      // 密码相关错误留在密码框内提示，允许直接重输。
      joinPasswordError.value = extractApiErrorMessage(error, t('accountShare.errors.join.passwordInvalid'), accountShareJoinErrorMessages())
      return
    }
    joinPasswordPrompt.value = null
    joinPasswordInput.value = ''
    joinPasswordError.value = ''
    showActionError(
      extractApiErrorMessage(error, t('accountShare.errors.fetchJoinTerms'), accountShareJoinErrorMessages()),
      t('accountShare.join.cannotConfirm')
    )
  } finally {
    if (preparingJoinId.value === listing.id) preparingJoinId.value = null
  }
}

function closeJoinPasswordPrompt(): void {
  if (preparingJoinId.value !== null) return
  joinPasswordPrompt.value = null
  joinPasswordInput.value = ''
  joinPasswordError.value = ''
}

async function submitJoinPassword(): Promise<void> {
  const prompt = joinPasswordPrompt.value
  if (!prompt || preparingJoinId.value !== null) return
  const password = joinPasswordInput.value.trim()
  if (!password) {
    joinPasswordError.value = t('accountShare.join.passwordPrompt')
    return
  }
  joinPasswordError.value = ''
  await issueJoinIntent(prompt.listing, prompt.platform, prompt.apiKeyID, prompt.apiKeyLabel, prompt.idleTimeoutMinutes, password)
}

async function requestJoinIntent(
  listingID: number,
  apiKeyID: number,
  idleTimeoutMinutes: number,
  password?: string
): Promise<AccountShareJoinIntent> {
  const intent = await accountShareAPI.createJoinIntent(listingID, {
    api_key_id: apiKeyID,
    idle_timeout_minutes: idleTimeoutMinutes,
    ...(password ? { password } : {})
  })
  const terms = intent?.terms
  const expectedVersion = Number(intent?.expected_version || 0)
  const expectedRevisionID = Number(intent?.expected_revision_id || 0)
  const termsVersion = Number(terms?.row_version || 0)
  const termsRevisionID = Number(terms?.listing_revision_id || 0)
  const expiresAtMs = Date.parse(intent?.expires_at || '')
  if (
    Number(intent?.listing_id || 0) !== listingID ||
    Number(intent?.api_key_id || 0) !== apiKeyID ||
    typeof intent?.token !== 'string' ||
    !intent.token.trim() ||
    expectedVersion <= 0 ||
    !Number.isSafeInteger(expectedVersion) ||
    !Number.isSafeInteger(expectedRevisionID) ||
    expectedRevisionID < 0 ||
    !terms ||
    termsVersion !== expectedVersion ||
    termsRevisionID !== expectedRevisionID ||
    !Array.isArray(terms.allowed_models) ||
    !Number.isFinite(expiresAtMs) ||
    expiresAtMs <= Date.now()
  ) {
    throw new Error(t('accountShare.join.intentIncomplete'))
  }
  return {
    ...intent,
    token: intent.token.trim(),
    expected_version: expectedVersion,
    expected_revision_id: expectedRevisionID,
    terms: {
      ...terms,
      row_version: termsVersion,
      listing_revision_id: termsRevisionID,
      allowed_models: [...terms.allowed_models]
    }
  }
}

function closeJoinConfirmation(): void {
  if (joinDialogBusy.value) return
  pendingJoinConfirmation.value = null
  joinIntentError.value = ''
  joinUnverifiedAcknowledged.value = false
}

async function confirmJoinUse(): Promise<void> {
  const pendingJoin = pendingJoinConfirmation.value
  if (!pendingJoin || joinDialogBusy.value) return
  if (Date.parse(pendingJoin.intent.expires_at) <= Date.now()) {
    joiningId.value = pendingJoin.listingID
    try {
      const recovered = await reconcilePendingJoinBinding(pendingJoin)
      if (recovered === 'unbound') {
        await invalidateJoinConfirmation(t('accountShare.join.intentExpired'))
      }
    } finally {
      joiningId.value = null
    }
    return
  }
  await submitJoinUse(pendingJoin)
}

async function invalidateJoinConfirmation(message: string, title = t('accountShare.join.reconfirm')): Promise<void> {
  pendingJoinConfirmation.value = null
  joinIntentError.value = ''
  const refreshed = await loadListings()
  showActionError(
    refreshed ? message : t('accountShare.join.refreshListFailed', { message }),
    title
  )
}

function isJoinSubmissionOutcomeUnknown(error: unknown): boolean {
  if (!error || typeof error !== 'object') return true
  const details = error as { status?: unknown; response?: { status?: unknown } }
  const status = Number(details.status ?? details.response?.status ?? 0)
  return !Number.isFinite(status) || status === 0 || status >= 500
}

async function reconcilePendingJoinBinding(
  pendingJoin: PendingJoinConfirmation
): Promise<'active' | 'ending' | 'unbound' | 'unavailable'> {
  let binding: AccountShareAPIKeyBindingStatus
  try {
    binding = await accountShareAPI.getAPIKeyBindingStatus(pendingJoin.apiKeyID)
    if (binding.api_key_id !== pendingJoin.apiKeyID) {
      throw new Error(t('accountShare.join.bindingMismatch'))
    }
  } catch (error: unknown) {
    joinIntentError.value = t('accountShare.join.pendingCheck', { detail: extractApiErrorMessage(error, t('accountShare.endUse.bindingReadFailed')) })
    return 'unavailable'
  }

  const membership = binding.memberships.find(item =>
    item.api_key_id === pendingJoin.apiKeyID
    && item.listing_id === pendingJoin.listingID
    && (item.status === 'active' || item.status === 'ending')
  )
  if (!membership) return 'unbound'
  if (membership.status === 'ending') {
    await invalidateJoinConfirmation(
      t('accountShare.join.keySettling'),
      t('accountShare.join.settling')
    )
    return 'ending'
  }

  pendingJoinConfirmation.value = null
  joinIntentError.value = ''
  const refreshed = await loadListings()
  if (refreshed) {
    appStore.showSuccess(t('accountShare.join.confirmedUsing'))
  } else {
    appStore.showWarning(t('accountShare.join.confirmedUsingRefreshFailed'))
  }
  return 'active'
}

async function submitJoinUse(pendingJoin: PendingJoinConfirmation): Promise<void> {
  const { listingID, apiKeyID, idleTimeoutMinutes, intent } = pendingJoin
  joiningId.value = listingID
  let joinSucceeded = false
  try {
    // 房间满员时 join intent 未被消费（服务端事务回滚），在 token 有效期
    // 内做有限次自动重试等待空位；其余错误直接抛出走既有处理分支。
    for (let attempt = 0; ; attempt++) {
      try {
        await accountShareAPI.joinListing(listingID, {
          api_key_id: apiKeyID,
          idle_timeout_minutes: idleTimeoutMinutes,
          intent_token: intent.token,
          expected_version: intent.expected_version,
          expected_revision_id: intent.expected_revision_id,
          ...(pendingJoinRequiresUnverifiedAck.value ? { acknowledged_unverified: true } : {})
        })
        break
      } catch (error: unknown) {
        const expiresAtMs = Date.parse(intent.expires_at)
        const canRetry = extractApiErrorCode(error) === 'ACCOUNT_SHARE_ROOM_FULL'
          && attempt < JOIN_ROOM_FULL_RETRY_COUNT
          && Number.isFinite(expiresAtMs)
          && expiresAtMs - Date.now() > JOIN_ROOM_FULL_MIN_REMAINING_MS
        if (!canRetry) throw error
        joinIntentError.value = t('accountShare.join.waitingSeat')
        await new Promise(resolve => setTimeout(resolve, JOIN_ROOM_FULL_RETRY_BASE_MS * (attempt + 1)))
      }
    }
    joinSucceeded = true
    pendingJoinConfirmation.value = null
    joinIntentError.value = ''
    const successMessage = t('accountShare.join.success')
    const refreshed = await loadListings()
    if (refreshed) {
      appStore.showSuccess(successMessage)
    } else {
      const actionLabel = t('accountShare.join.actionLabel')
      appStore.showWarning(t('accountShare.join.successRefreshFailed', { action: actionLabel }))
    }
  } catch (error: unknown) {
    if (joinSucceeded) {
      pendingJoinConfirmation.value = null
      appStore.showWarning(t('accountShare.join.successRefreshError'))
    } else {
      const errorCode = extractApiErrorCode(error)
      if (
        errorCode === 'ACCOUNT_SHARE_JOIN_INTENT_INVALID' ||
        errorCode === 'ACCOUNT_SHARE_JOIN_TERMS_CHANGED'
      ) {
        const message = t('accountShare.join.termsChangedClose')
        await invalidateJoinConfirmation(message)
      } else if (
        errorCode === 'ACCOUNT_SHARE_JOIN_INTENT_CONSUMED'
        || errorCode === 'ACCOUNT_SHARE_MEMBERSHIP_ENDING'
      ) {
        const recovered = await reconcilePendingJoinBinding(pendingJoin)
        if (recovered !== 'active' && recovered !== 'ending') {
          await invalidateJoinConfirmation(
            errorCode === 'ACCOUNT_SHARE_MEMBERSHIP_ENDING'
              ? t('accountShare.join.keySettlementPending')
              : t('accountShare.join.intentUsed'),
            t('accountShare.join.checkState')
          )
        }
      } else if (isJoinSubmissionOutcomeUnknown(error)) {
        joinIntentError.value = t('accountShare.join.pendingMaybeBound')
        await reconcilePendingJoinBinding(pendingJoin)
      } else {
        pendingJoinConfirmation.value = null
        joinIntentError.value = ''
        showActionError(extractApiErrorMessage(error, t('accountShare.errors.joinFailed'), accountShareJoinErrorMessages()), t('accountShare.errors.joinFailed'))
      }
    }
  } finally {
    joiningId.value = null
  }
}

function handleEndUseClick(listing: AccountShareListing): void {
  const membershipID = listingMembershipID(listing)
  if (
    membershipID <= 0
    || endingId.value !== null
    || isListingMembershipEnding(listing)
  ) {
    return
  }
  const pending: PendingEndUseState = {
    membershipID,
    apiKeyID: listing.queue_api_key_id || listing.current_api_key_id,
    apiKeyName: boundApiKeyName(listing),
    status: listing.queue_status || (listing.current_membership_id ? 'active' : ''),
    listing
  }
  pendingEndUse.value = pending
}

function membershipEndNeedsBindingCheck(pending: PendingMembershipEnd): boolean {
  return !pending.operationID
    || (pending.operationQueryFailures || 0) >= MEMBERSHIP_END_QUERY_FAILURE_RECHECK_THRESHOLD
    || ROOM_LIFECYCLE_TERMINAL_OPERATION_STATUSES.has(pending.operationStatus)
}

async function reconcilePendingMembershipEndBindings(requestSeq: number): Promise<void> {
  // 同一轮每个 Key 只查询一次；即使操作接口持续失败，也不会按房间数量放大请求。
  const byKey = new Map<number, PendingMembershipEnd[]>()
  for (const pending of Object.values(pendingMembershipEnds.value)) {
    if (!membershipEndNeedsBindingCheck(pending)) continue
    const keyID = Number(pending.apiKeyID || pending.membership.api_key_id || 0)
    // Key 处置入口随后会统一读取权威绑定，复用该轮结果，避免重复请求。
    if (isKeyResolutionMode.value && keyID === keyResolutionApiKeyID.value) continue
    if (keyID <= 0) {
      pending.bindingCheckError = t('accountShare.endUse.missingKeyRef')
      continue
    }
    const entries = byKey.get(keyID) || []
    entries.push(pending)
    byKey.set(keyID, entries)
  }
  let clearedCount = 0
  await Promise.all([...byKey.entries()].map(async ([keyID, entries]) => {
    const lastAttempt = membershipEndBindingLastAttempt.get(keyID)
    if (membershipEndBindingControllers.has(keyID)
      || (lastAttempt !== undefined && Date.now() - lastAttempt < MEMBERSHIP_END_BINDING_RECHECK_MS)) return
    membershipEndBindingLastAttempt.set(keyID, Date.now())
    const controller = new AbortController()
    membershipEndBindingControllers.set(keyID, controller)
    try {
      const binding = await accountShareAPI.getAPIKeyBindingStatus(keyID, { signal: controller.signal })
      if (controller.signal.aborted || requestSeq !== membershipEndOperationRequestSeq) return
      if (binding.api_key_id !== keyID) throw new Error(t('accountShare.endUse.bindingMismatch'))
      for (const entry of entries) {
        const current = pendingMembershipEnds.value[entry.listingID]
        if (!current || current.membershipID !== entry.membershipID) continue
        const membership = binding.memberships.find(item =>
          item.id === entry.membershipID && (item.status === 'active' || item.status === 'ending')
        )
        if (!membership) {
          removePendingMembershipEnd(entry.listingID)
          clearedCount += 1
          continue
        }
        pendingMembershipEnds.value = {
          ...pendingMembershipEnds.value,
          [entry.listingID]: {
            ...current,
            membership,
            operationID: membership.ending_operation_id || current.operationID,
            operationStatus: membership.ending_operation_status || current.operationStatus,
            operationCreatedAt: current.operationCreatedAt || membership.ending_requested_at,
            lastBindingCheckAt: Date.now(),
            bindingCheckError: ''
          }
        }
      }
    } catch (error: unknown) {
      if (controller.signal.aborted || requestSeq !== membershipEndOperationRequestSeq || isCanceledRequest(error)) return
      for (const entry of entries) {
        const current = pendingMembershipEnds.value[entry.listingID]
        if (!current || current.membershipID !== entry.membershipID) continue
        pendingMembershipEnds.value = {
          ...pendingMembershipEnds.value,
          [entry.listingID]: {
            ...current,
            bindingCheckError: extractApiErrorMessage(error, t('accountShare.endUse.bindingReadFailed'))
          }
        }
      }
    } finally {
      if (membershipEndBindingControllers.get(keyID) === controller) membershipEndBindingControllers.delete(keyID)
    }
  }))
  if (clearedCount > 0) {
    appStore.showSuccess(t('accountShare.endUse.confirmedUnbound'))
  }
}

async function pollPendingMembershipEndOperations(): Promise<PendingMembershipEnd[]> {
  const requestSeq = membershipEndOperationRequestSeq
  const entries = Object.values(pendingMembershipEnds.value)
    .filter(pendingMembershipEndIsPollable)
  const results = await Promise.all(entries.map(async (entry): Promise<PendingMembershipEnd | null> => {
    const previousController = membershipEndOperationControllers.get(entry.listingID)
    previousController?.abort()
    const controller = new AbortController()
    membershipEndOperationControllers.set(entry.listingID, controller)
    try {
      const operation = await accountShareAPI.getRoomOperation(entry.operationID, {
        signal: controller.signal
      })
      const current = pendingMembershipEnds.value[entry.listingID]
      if (
        controller.signal.aborted
        || requestSeq !== membershipEndOperationRequestSeq
        || !current
        || current.operationID !== entry.operationID
      ) {
        return null
      }
      updatePendingMembershipEndOperation(entry.listingID, operation)
      if (operation.status !== 'succeeded') return null

      const resultStatus = String(operation.result?.status || '')
      if (resultStatus !== 'ended') {
        pendingMembershipEnds.value = {
          ...pendingMembershipEnds.value,
          [entry.listingID]: {
            ...pendingMembershipEnds.value[entry.listingID],
            operationStatus: 'failed',
            operationError: t('accountShare.endUse.completionIncomplete')
          }
        }
        return null
      }

      const removed = removePendingMembershipEnd(entry.listingID)
      if (!removed) return null
      return {
        ...removed,
        membership: {
          ...removed.membership,
          status: 'ended' as const,
          ended_at: typeof operation.result?.ended_at === 'string'
            ? operation.result.ended_at
            : operation.completed_at,
          settlement_status: typeof operation.result?.settlement_status === 'string'
            ? operation.result.settlement_status
            : 'settled',
          updated_at: operation.updated_at
        }
      }
    } catch (error: unknown) {
      if (
        controller.signal.aborted
        || requestSeq !== membershipEndOperationRequestSeq
        || isCanceledRequest(error)
      ) {
        return null
      }
      const current = pendingMembershipEnds.value[entry.listingID]
      if (current?.operationID === entry.operationID) {
        pendingMembershipEnds.value = {
          ...pendingMembershipEnds.value,
          [entry.listingID]: {
            ...current,
            lastOperationAttemptAt: Date.now(),
            operationQueryFailures: (current.operationQueryFailures || 0) + 1,
            operationQueryError: extractApiErrorMessage(
              error,
              t('accountShare.endUse.progressQueryFailed'),
              ROOM_LIFECYCLE_ERROR_MESSAGES
            )
          }
        }
      }
      return null
    } finally {
      if (membershipEndOperationControllers.get(entry.listingID) === controller) {
        membershipEndOperationControllers.delete(entry.listingID)
      }
    }
  }))

  await reconcilePendingMembershipEndBindings(requestSeq)
  const completed = results.filter(
    (item): item is PendingMembershipEnd => item !== null
  )
  if (completed.length > 0) {
    appStore.showSuccess(
      completed.length === 1
        ? t('accountShare.endUse.settled')
        : t('accountShare.endUse.settledCount', { count: completed.length  })
    )
  }
  return completed
}

function cancelEndUse(): void {
  if (endingId.value !== null) return
  pendingEndUse.value = null
}

async function confirmEndUse(): Promise<void> {
  const pending = pendingEndUse.value
  const membershipID = pending?.membershipID
  if (!pending || !membershipID || endingId.value !== null) return
  const membership = await endUse(pending)
  if (pendingEndUse.value === pending) pendingEndUse.value = null
  if (
    pending
    && membership?.status === 'ended'
    && membership.last_request_at
  ) {
    openReviewDialog(pending.listing, membership)
  }
}

async function endUse(pending: PendingEndUseState): Promise<AccountShareMembership | null> {
  const membershipID = pending.membershipID
  errorMessage.value = ''
  endingId.value = membershipID
  let endSucceeded = false
  try {
    const membership = await accountShareAPI.endMembership(membershipID)
    endSucceeded = true
    if (membership.status === 'ending') {
      setPendingMembershipEnd(pending, membership)
    } else if (membership.status === 'ended') {
      removePendingMembershipEnd(pending.listing.id)
    }
    if (pendingEndUse.value === pending) pendingEndUse.value = null
    const successMessage = membership.status === 'ending'
        ? t('accountShare.endUse.exitAccepted')
        : t('accountShare.endUse.ended')
    const refreshed = await loadListings()
    const resolutionRefreshed = !isKeyResolutionMode.value || await loadKeyResolutionState()
    if (refreshed && resolutionRefreshed) {
      appStore.showSuccess(successMessage)
    } else {
      appStore.showWarning(t('accountShare.endUse.successRefreshFailed', { successMessage }))
    }
    return membership
  } catch (error: unknown) {
    if (endSucceeded) {
      appStore.showWarning(t('accountShare.endUse.successRefreshError'))
    } else {
      showActionError(extractApiErrorMessage(error, t('accountShare.errors.endUseFailed'), accountShareEndErrorMessages()), t('accountShare.errors.endUseFailed'))
    }
    return null
  } finally {
    if (endingId.value === membershipID) endingId.value = null
  }
}

const commentReviewDisabled = computed(() => capabilities.value?.comment_review_enabled === false)

function ensureCapabilitiesLoaded(): void {
  if (capabilities.value === null && !capabilitiesLoading.value) {
    void loadCapabilities()
  }
}

function openReviewDialog(listing: AccountShareListing, membership: AccountShareMembership): void {
  clearStableIdempotencyIntent(reviewSubmitIntent)
  ensureCapabilitiesLoaded()
  pendingReview.value = {
    membershipID: membership.id,
    platformLabel: platformLabel(listingPlatform(listing)),
    roomName: listingDisplayName(listing),
    ownerName: ownerDisplayName(listing),
    score: null,
    comment: '',
    submitting: false,
    error: ''
  }
}

function openHistoryReviewDialog(entry: AccountShareMembershipHistoryEntry): void {
  if (entry.review || entry.usage_request_count <= 0) return
  clearStableIdempotencyIntent(reviewSubmitIntent)
  ensureCapabilitiesLoaded()
  const normalizedPlatform = entry.platform.trim().toLowerCase()
  pendingReview.value = {
    membershipID: entry.membership_id,
    platformLabel: normalizedPlatform === 'openai' || normalizedPlatform === 'anthropic' || normalizedPlatform === 'opencode'
      ? platformLabel(normalizedPlatform)
      : (entry.platform.trim() || t('accountShare.membership.unknownPlatform')),
    roomName: entry.room_name.trim() || t('accountShare.review.roomId', { id: entry.listing_id  }),
    ownerName: entry.owner_username?.trim() || (entry.owner_user_id > 0 ? t('accountShare.review.ownerId', { id: entry.owner_user_id  }) : t('accountShare.review.historicalOwner')),
    score: null,
    comment: '',
    submitting: false,
    error: ''
  }
}

function closeReviewDialog(): void {
  if (pendingReview.value?.submitting) return
  clearStableIdempotencyIntent(reviewSubmitIntent)
  pendingReview.value = null
}

async function submitReview(): Promise<void> {
  const state = pendingReview.value
  if (!state || state.submitting) return
  if (state.score === null || state.score < 0 || state.score > 10) {
    state.error = t('accountShare.review.scoreRange')
    return
  }
  state.submitting = true
  state.error = ''
  try {
    const comment = commentReviewDisabled.value ? '' : state.comment.trim()
    const payload = {
      score: state.score,
      comment: comment || undefined
    }
    const idempotencyKey = getStableIdempotencyKey(
      reviewSubmitIntent,
      `account-share-review-${state.membershipID}`,
      { membershipID: state.membershipID, payload }
    )
    await accountShareAPI.submitReview(state.membershipID, payload, idempotencyKey)
    clearStableIdempotencyIntent(reviewSubmitIntent)
    pendingReview.value = null
    await loadCurrentView()
    appStore.showSuccess(comment ? t('accountShare.review.submittedPending') : t('accountShare.review.submitted'))
  } catch (error: unknown) {
    state.error = extractApiErrorMessage(error, t('accountShare.errors.submitReview'), {
      ACCOUNT_SHARE_REVIEW_ALREADY_EXISTS: t('accountShare.errors.alreadyReviewed'),
      ACCOUNT_SHARE_REVIEW_NO_USAGE: t('accountShare.errors.noRequestsToReview'),
      ACCOUNT_SHARE_COMMENT_REVIEW_UNAVAILABLE: t('accountShare.errors.reviewModerationOff'),
      ACCOUNT_SHARE_REVIEW_COMMENT_TOO_LONG: t('accountShare.review.commentMax'),
      ACCOUNT_SHARE_REVIEW_INVALID_SCORE: t('accountShare.review.scoreMustBeInRange')
    })
  } finally {
    if (pendingReview.value) pendingReview.value.submitting = false
  }
}

async function saveIdleTimeout(listing: AccountShareListing): Promise<void> {
  const membershipID = listingMembershipID(listing)
  if (membershipID <= 0 || savingIdleTimeoutId.value === membershipID) return
  errorMessage.value = ''
  const idleTimeoutValue = idleTimeoutByListing[listing.id] ?? listing.current_idle_timeout_minutes ?? listing.queue_idle_timeout_minutes ?? 0
  const idleTimeoutError = validateIdleTimeoutMinutes(idleTimeoutValue)
  if (idleTimeoutError) {
    showActionError(idleTimeoutError, t('accountShare.errors.idleInvalid'))
    return
  }
  savingIdleTimeoutId.value = membershipID
  try {
    await accountShareAPI.updateMembershipIdleTimeout(membershipID, normalizeIdleTimeoutMinutes(idleTimeoutValue))
    await loadListings()
    appStore.showSuccess(t('accountShare.idle.saved'))
  } catch (error: unknown) {
    showActionError(extractApiErrorMessage(error, t('accountShare.errors.saveIdle')), t('ideas.editor.saveFailed'))
  } finally {
    savingIdleTimeoutId.value = null
  }
}

function closeRoomDetails(): void {
  detailRequestSeq += 1
  detailRequestController?.abort()
  detailRequestController = null
  detailSnapshot.value = null
  detailLoading.value = false
  detailError.value = ''
}

function openRoomDetails(listing: AccountShareListing, event?: Event): void {
  closeRoomDetails()
  if (event?.currentTarget instanceof HTMLElement) event.currentTarget.focus()
  detailSnapshot.value = listing
  detailIsArchive.value = isArchiveView.value || Boolean(listing.deleted)
  // 归档展示原有快照，不能用实时配置补齐或覆盖历史条款。
  if (!detailIsArchive.value) void refreshRoomDetails()
}

async function refreshRoomDetails(): Promise<void> {
  const listingID = detailSnapshot.value?.id
  if (!listingID || detailIsArchive.value) return
  detailRequestController?.abort()
  const controller = new AbortController()
  const requestSeq = ++detailRequestSeq
  detailRequestController = controller
  detailLoading.value = true
  detailError.value = ''
  try {
    const updated = await accountShareAPI.getListing(listingID, { signal: controller.signal })
    if (controller.signal.aborted || requestSeq !== detailRequestSeq) return
    // 完整详情的可选绑定字段缺失表示当前未绑定，不能与旧卡片做增量合并。
    const normalized = normalizeListingForMerge(updated)
    if (!isKeyResolutionMode.value) syncListingEndingMemberships([updated])
    const current = isKeyResolutionMode.value ? normalized : listingWithPendingMembershipEnd(normalized)
    for (const collection of [listings, knownListings]) {
      const index = collection.value.findIndex(listing => listing.id === listingID)
      if (index >= 0) collection.value[index] = current
    }
    if (isKeyResolutionMode.value) {
      const index = keyResolutionListings.value.findIndex(listing => listing.id === listingID)
      if (index >= 0) {
        keyResolutionListings.value[index] = resolutionListingFromMemberships(
          updated,
          keyResolutionMemberships.value.filter(membership => membership.listing_id === listingID)
        )
      }
    }
    detailSnapshot.value = current
    syncIdleTimeoutControls([detailListing.value || current])
  } catch (error: unknown) {
    if (controller.signal.aborted || requestSeq !== detailRequestSeq || isCanceledRequest(error)) return
    detailError.value = extractApiErrorMessage(error, t('accountShare.errors.loadRoomDetail'))
  } finally {
    if (requestSeq === detailRequestSeq) {
      detailLoading.value = false
      detailRequestController = null
    }
  }
}

function mergeListingUpdate(updated: AccountShareListing): void {
  mergeKnownListings([updated])
  const index = listings.value.findIndex(item => item.id === updated.id)
  if (index >= 0) {
    listings.value[index] = mergeListingFields(listings.value[index], updated)
  }
  if (editingConfigListing.value?.id === updated.id) {
    editingConfigListing.value = mergeListingFields(editingConfigListing.value, updated)
  }
}

function normalizeEditableNumber(value: number | null | undefined, fallback: number): number {
  const numeric = Number(value ?? fallback)
  return Number.isFinite(numeric) ? numeric : fallback
}

function populateEditForm(listing: AccountShareListing): void {
  Object.assign(editForm, {
    name: listing.room_name?.trim() ? listing.room_name : `${ACCOUNT_NAME_BASE_BY_PLATFORM.value[listingPlatform(listing)]}${listing.id}`,
    proxy_id: null,
    concurrency: normalizeEditableNumber(listing.account_concurrency, DEFAULT_ACCOUNT_CONCURRENCY),
    seat_limit: normalizeEditableNumber(listing.seat_limit, 2),
    rate_multiplier: normalizeEditableNumber(listing.rate_multiplier, 1),
    per_user_concurrency: normalizeEditableNumber(listing.per_user_concurrency, DEFAULT_PER_USER_CONCURRENCY),
    hourly_rate: normalizeEditableNumber(listing.hourly_rate, 0),
    hourly_fee_waiver_minimum: normalizeEditableNumber(listing.hourly_fee_waiver_minimum, 0),
    min_balance_required: normalizeEditableNumber(listing.min_balance_required, 0),
    codex_cli_only: Boolean(listing.codex_cli_only),
    codex_5h_limit_percent: normalizeEditableNumber(listing.codex_5h_limit_percent, 100),
    codex_7d_limit_percent: normalizeEditableNumber(listing.codex_7d_limit_percent, 100),
    anthropic_5h_limit_percent: anthropic5hLimitPercent(listing),
    anthropic_7d_limit_percent: anthropic7dLimitPercent(listing),
    join_password: ''
  } satisfies CreateFormState)
  editAllowedModels.value = Array.isArray(listing.allowed_models) ? [...listing.allowed_models] : []
}

function resetConfigEditState(): void {
  clearStableIdempotencyIntent(updateListingIntent)
  showConfigEditDialog.value = false
  editingConfigListing.value = null
  editAllowedModels.value = []
  editForceActive.value = false
  editConsumerProtected.value = false
  editJoinPasswordClear.value = false
  editReason.value = ''
  editErrorMessage.value = ''
  editVersionConflict.value = false
  configDraftBaseline.value = null
  Object.assign(editForm, buildDefaultCreateForm())
}

async function closeConfigEditDialog(discardConfirmed = false): Promise<void> {
  if (savingConfigEdit.value) return
  if (!discardConfirmed && configDraftHasChanges()) {
    pendingDraftDiscardTarget.value = 'config'
    return
  }
  resetConfigEditState()
}

function prepareForceEdit(
  listing: AccountShareListing,
  state: AccountShareRoomManagementState | null = null
): void {
  pendingForceEditListing.value = listing
  pendingForceEditManagementState.value = state
  forceEditReason.value = ''
  forceEditConfirmed.value = false
}

async function openConfigEditDialog(listing: AccountShareListing, force: boolean, forceReason = ''): Promise<void> {
  if (force && !authStore.isAdmin) {
    showActionError(t('accountShare.forceEdit.adminOnly'), t('accountShare.forceEdit.notAllowed'))
    return
  }
  if (listing.deleted) {
    showActionError(t('accountShare.forceEdit.deletedOnly'), t('accountShare.lifecycle.roomDeleted'))
    return
  }
  if (!Number.isSafeInteger(Number(listing.row_version)) || Number(listing.row_version) <= 0) {
    showActionError(t('accountShare.forceEdit.staleVersion'), t('accountShare.forceEdit.cannotEdit'))
    return
  }
  await loadListingNameIndex(false)
  editingConfigListing.value = listing
  editForceActive.value = force
  editConsumerProtected.value = false
  editJoinPasswordClear.value = false
  editReason.value = force ? forceReason.trim() : ''
  editErrorMessage.value = ''
  editVersionConflict.value = false
  populateEditForm(listing)
  captureConfigDraftBaseline()
  showConfigEditDialog.value = true
}

async function openConsumerProtectedEditDialog(listing: AccountShareListing): Promise<void> {
  await loadListingNameIndex(false)
  editingConfigListing.value = listing
  editForceActive.value = false
  editConsumerProtected.value = true
  editJoinPasswordClear.value = false
  editReason.value = ''
  editErrorMessage.value = ''
  editVersionConflict.value = false
  populateEditForm(listing)
  captureConfigDraftBaseline()
  showConfigEditDialog.value = true
}

async function requestOpenConfigEdit(listing: AccountShareListing): Promise<void> {
  if (
    managedActionId.value !== null
    || showConfigEditDialog.value
    || pendingForceEditListing.value !== null
    || pendingDraftDiscardTarget.value !== null
  ) {
    return
  }
  if (listing.deleted) {
    showActionError(t('accountShare.roomEdit.deletedNoEdit'), t('accountShare.lifecycle.roomDeleted'))
    return
  }
  managedActionId.value = listing.id
  try {
    const state = await accountShareAPI.getRoomManagementState(listing.id)
    if (state.listing_id !== listing.id) {
      throw new Error(t('accountShare.roomEdit.manageStateMismatch'))
    }
    if (state.blockers.runtime_dependency_unavailable) {
      showActionError(
        t('accountShare.roomEdit.runtimeUnknown'),
        t('accountShare.roomEdit.unsafeToEdit')
      )
      return
    }
    const currentListing: AccountShareListing = {
      ...listing,
      // 配置与版本必须来自同一快照；实时状态仅用于判断当前允许的操作。
      row_version: listing.row_version,
      status: state.lifecycle_status,
      seat_limit: state.seat_limit,
      active_seats: state.active_seats
    }
    mergeListingUpdate(currentListing)
    if (roomRequiresForceEdit(currentListing, state)) {
      if (authStore.isAdmin) {
        prepareForceEdit(currentListing, state)
      } else {
        await openConsumerProtectedEditDialog(currentListing)
      }
      return
    }
    await openConfigEditDialog(currentListing, false)
  } catch (error: unknown) {
    showActionError(extractApiErrorMessage(error, t('accountShare.errors.loadRoomRuntime')), t('accountShare.errors.openEdit'))
  } finally {
    if (managedActionId.value === listing.id) managedActionId.value = null
  }
}

function cancelForceEdit(): void {
  pendingForceEditListing.value = null
  pendingForceEditManagementState.value = null
  forceEditReason.value = ''
  forceEditConfirmed.value = false
}

function confirmForceEdit(): void {
  const listing = pendingForceEditListing.value
  const reason = forceEditReason.value.trim()
  if (!listing || !reason || !forceEditConfirmed.value || !authStore.isAdmin) return
  pendingForceEditListing.value = null
  pendingForceEditManagementState.value = null
  forceEditReason.value = ''
  forceEditConfirmed.value = false
  void openConfigEditDialog(listing, true, reason)
}

// 错误条渲染在弹窗顶部，而"保存配置"在长表单底部：只写 editErrorMessage 会让
// 用户在滚动后看不到任何反馈（表现为"点了没反应"）。这里同时弹 toast 并把错误条滚入视野。
function setConfigEditError(message: string): void {
  editErrorMessage.value = message
  if (!message) return
  appStore.showError?.(message)
  void nextTick(() => {
    document
      .querySelector('[data-testid="config-edit-error"]')
      ?.scrollIntoView({ behavior: 'smooth', block: 'center' })
  })
}

async function saveConfigEdit(): Promise<void> {
  const listing = editingConfigListing.value
  if (!listing || savingConfigEdit.value) return
  editErrorMessage.value = ''
  const validationError = validateEditConfig()
  if (validationError) {
    setConfigEditError(validationError)
    return
  }

  savingConfigEdit.value = true
  try {
    const payload: UpdateAccountShareListingRequest = {
      expected_version: Number(listing.row_version),
      name: editForm.name.trim(),
      seat_limit: Number(editForm.seat_limit),
      rate_multiplier: Number(editForm.rate_multiplier),
      allowed_models: parseEditAllowedModels(),
      per_user_concurrency: Number(editForm.per_user_concurrency),
      hourly_rate: Number(editForm.hourly_rate),
      hourly_fee_waiver_minimum: Number(editForm.hourly_fee_waiver_minimum),
      min_balance_required: Number(editForm.min_balance_required),
      reason: editReason.value.trim()
    }
    // 加入密码三态：勾选移除传空串、输入新值传新密码、都未动则不带字段保持不变。
    if (editJoinPasswordClear.value) {
      payload.join_password = ''
    } else if (editForm.join_password.trim()) {
      payload.join_password = editForm.join_password.trim()
    }
    if (editForceActive.value && authStore.isAdmin) {
      payload.force_active_edit = true
      payload.confirmed = true
    }
    if (!editConsumerProtected.value && listingPlatform(listing) === 'openai') {
      payload.codex_cli_only = editForm.codex_cli_only
      payload.codex_5h_limit_percent = Number(editForm.codex_5h_limit_percent)
      payload.codex_7d_limit_percent = Number(editForm.codex_7d_limit_percent)
    } else if (!editConsumerProtected.value && listingPlatform(listing) === 'anthropic') {
      payload.anthropic_5h_limit_percent = Number(editForm.anthropic_5h_limit_percent)
      payload.anthropic_7d_limit_percent = Number(editForm.anthropic_7d_limit_percent)
    }
    if (!editConsumerProtected.value && configDraftBaseline.value) {
      // 房间名与单用户并发即使没改也会被后端真校验一遍（重名检查、房间容量上限）。
      // 整表单提交时把它们原样带上，会让「只改价格」被一条完全无关的报错打回。
      const baseline = configDraftBaseline.value
      if (editForm.name.trim() === baseline.form.name.trim()) delete payload.name
      if (Number(editForm.per_user_concurrency) === Number(baseline.form.per_user_concurrency)) {
        delete payload.per_user_concurrency
      }
    }
    if (editConsumerProtected.value && configDraftBaseline.value) {
      const baseline = configDraftBaseline.value
      if (editForm.name.trim() === baseline.form.name.trim()) delete payload.name
      if (Number(editForm.seat_limit) === Number(baseline.form.seat_limit)) delete payload.seat_limit
      if (Number(editForm.rate_multiplier) === Number(baseline.form.rate_multiplier)) delete payload.rate_multiplier
      if (snapshotsMatch(parseEditAllowedModels(), baseline.allowedModels)) delete payload.allowed_models
      if (Number(editForm.per_user_concurrency) === Number(baseline.form.per_user_concurrency)) delete payload.per_user_concurrency
      if (Number(editForm.hourly_rate) === Number(baseline.form.hourly_rate)) delete payload.hourly_rate
      if (Number(editForm.hourly_fee_waiver_minimum) === Number(baseline.form.hourly_fee_waiver_minimum)) delete payload.hourly_fee_waiver_minimum
      if (Number(editForm.min_balance_required) === Number(baseline.form.min_balance_required)) delete payload.min_balance_required
    }
    const idempotencyKey = getStableIdempotencyKey(
      updateListingIntent,
      `account-share-listing-update-${listing.id}`,
      { listingID: listing.id, payload }
    )
    const updated = await accountShareAPI.updateListing(listing.id, payload, idempotencyKey)
    clearStableIdempotencyIntent(updateListingIntent)
    mergeListingUpdate(updated)
    await loadListings()
    appStore.showSuccess(t('accountShare.roomEdit.saved'))
    resetConfigEditState()
  } catch (error: unknown) {
    const errorCode = extractApiErrorCode(error)
    if (errorCode === 'ACCOUNT_SHARE_ROOM_VERSION_CONFLICT') {
      editVersionConflict.value = true
      setConfigEditError(t('accountShare.roomEdit.staleSaved'))
    } else if (errorCode === 'ACCOUNT_SHARE_MODE_UNSUPPORTED_MODEL') {
      // 后端把「缺哪个模型/哪个账号」放在错误 metadata 里；不带出来的话用户只能看到
      // 一句无从下手的英文「不支持请求的模型」。这里与 RoomAccountsDialog 的写法对齐。
      const metadata = extractApiErrorMetadata(error) || {}
      const model = typeof metadata.model === 'string' ? metadata.model.trim() : ''
      const accountID = typeof metadata.account_id === 'string' ? metadata.account_id.trim() : ''
      if (model) {
        const who = accountID ? t('accountShare.roomEdit.accountId', { id: accountID }) : t('accountShare.roomEdit.someAccount')
        setConfigEditError(t('accountShare.roomEdit.accountModelMissing', { who, model }))
      } else {
        setConfigEditError(t('accountShare.roomEdit.modelUnsupported'))
      }
    } else {
      setConfigEditError(extractApiErrorMessage(error, t('accountShare.errors.saveRoomConfig'), {
        ACCOUNT_SHARE_ROOM_UPDATE_REASON_REQUIRED: t('accountShare.roomEdit.editReasonRequired'),
        ACCOUNT_SHARE_ROOM_FORCE_REASON_REQUIRED: t('accountShare.roomEdit.adminReasonRequired'),
        ACCOUNT_SHARE_ROOM_FORCE_CONFIRMATION_REQUIRED: t('accountShare.roomEdit.adminConfirmRequired'),
        ACCOUNT_SHARE_CONSUMER_PROTECTION_VIOLATION: t('accountShare.roomEdit.consumerLimits'),
        ACCOUNT_SHARE_MODE_INVALID_CONCURRENCY: t('accountShare.roomEdit.perUserOutOfRange'),
        ACCOUNT_SHARE_ROOM_UPDATE_REQUIRES_PAUSED: t('accountShare.roomEdit.statusBlocksEdit'),
        ACCOUNT_SHARE_LISTING_IN_USE: t('accountShare.roomEdit.seatsOrSettlement'),
        ACCOUNT_SHARE_ROOM_OPERATION_CONFLICT: t('accountShare.roomEdit.lifecycleOpRunning'),
        ACCOUNT_SHARE_ROOM_PASSWORD_INVALID_LENGTH: t('accountShare.errors.roomCreate.passwordInvalidLength')
      }))
    }
  } finally {
    savingConfigEdit.value = false
  }
}

async function reloadConfigEditAfterConflict(): Promise<void> {
  const listingID = editingConfigListing.value?.id
  if (!listingID || savingConfigEdit.value) return
  resetConfigEditState()
  await loadListings()
  const refreshed = listings.value.find((item) => item.id === listingID)
    || knownListings.value.find((item) => item.id === listingID)
  if (!refreshed) {
    showActionError(t('accountShare.roomEdit.roomGone'), t('accountShare.roomEdit.cannotReopen'))
    return
  }
  requestOpenConfigEdit(refreshed)
}

function copyModelName(model: string): void {
  void copyToClipboard(model, t('accountShare.common.copied', { model }))
}

watch(
  () => selectedOwnedAccount.value,
  (account) => {
    if (!account) return
    createForm.concurrency = account.concurrency
    createForm.per_user_concurrency = Math.min(
      Math.max(1, Number(createForm.per_user_concurrency) || 1),
      MAX_PER_USER_CONCURRENCY
    )
  },
  { immediate: true }
)

watch(
  [
    selectedOwnedAccountID,
    () => ({ ...createForm }),
    allowedModels
  ],
  () => {
    if (!creating.value) clearPendingCreateRoomIdempotencyKey()
  },
  { deep: true }
)

watch(searchQuery, () => {
  if (suppressNextSearchRefresh) {
    suppressNextSearchRefresh = false
    return
  }
  clearSearchDebounceTimer()
  searchDebounceTimer = window.setTimeout(() => {
    if (isMembershipHistoryView.value) return
    pagination.page = 1
    persistListingPreferences()
    void loadListings()
  }, 300)
})

watch(modeApiKeys, keys => {
  clearInvalidSelectedModeApiKeys(activeListingPlatform.value, keys)
  syncRecommendationApiKey()
})

watch(
  () => [route.query.mode, route.query.api_key_id, route.query.api_key_name, route.query.return_to],
  () => {
    if (!isKeyResolutionMode.value) {
      clearKeyResolutionState()
      return
    }
    prepareKeyResolutionMode()
    void Promise.all([loadListings(), loadKeyResolutionState()])
  }
)

watch(recommendationPageCount, pages => {
  if (recommendationPage.value > pages) {
    recommendationPage.value = pages
  }
})

watch(
  () => [
    recommendationForm.api_key_id,
    recommendationForm.model,
    recommendationForm.request_count,
    recommendationForm.active_hours,
    recommendationForm.input_tokens_per_request,
    recommendationForm.output_tokens_per_request,
    recommendationForm.cache_creation_tokens_per_request,
    recommendationForm.cache_read_tokens_per_request,
    recommendationForm.image_input_tokens_per_request,
    recommendationForm.image_output_tokens_per_request,
    recommendationForm.image_cache_read_tokens_per_request
  ],
  () => {
    abortRecommendationAsyncRequests()
    resetRecommendationResult()
  },
  { flush: 'sync' }
)

watch(
  () => authStore.user?.id,
  () => {
    closeRoomDetails()
    abortRecommendationAsyncRequests()
    resetRecommendationResult()
  },
  { flush: 'sync' }
)

// 退出后房间会离开“我的使用”，切换筛选也可能移除当前项；避免抽屉继续展示旧绑定。
watch([displayedListings, loading, keyResolutionLoading], () => {
  if (loading.value || keyResolutionLoading.value || !detailSnapshot.value) return
  if (!displayedListings.value.some(listing => listing.id === detailSnapshot.value?.id)) closeRoomDetails()
}, { flush: 'post' })

useRoomGridCapacity({
  viewport: roomGridViewportRef,
  grid: roomGridRef,
  enabled: computed(() => !isMembershipHistoryView.value && !isKeyResolutionMode.value),
  onCapacityChange: handleRoomGridCapacityChange
})

onMounted(async () => {
  document.addEventListener('click', handleFilterPanelDocumentClick)
  document.addEventListener('visibilitychange', handleDocumentVisibilityChange)
  window.addEventListener('focus', handleWindowFocus)
  clockTimer = window.setInterval(() => {
    nowMs.value = Date.now()
  }, 30_000)
  try {
    prepareKeyResolutionMode()
    const initializationTasks: Promise<unknown>[] = [
      loadCurrentView(),
      loadModeKeys(),
      loadListingNameIndex(),
      loadSelfUseCommissionRate(),
      loadCapabilities()
    ]
    if (isKeyResolutionMode.value) initializationTasks.push(loadKeyResolutionState())
    await Promise.all(initializationTasks)
  } catch (error: unknown) {
    errorMessage.value = extractApiErrorMessage(error, t('accountShare.errors.initMarketplace'))
  }
})

onBeforeUnmount(() => {
  closeRoomDetails()
  document.removeEventListener('click', handleFilterPanelDocumentClick)
  document.removeEventListener('visibilitychange', handleDocumentVisibilityChange)
  window.removeEventListener('focus', handleWindowFocus)
  if (clockTimer != null) {
    window.clearInterval(clockTimer)
    clockTimer = null
  }
  clearSearchDebounceTimer()
  clearMembershipStatusRefreshTimer()
  abortActiveListingsRequest()
  abortMembershipHistoryRequest()
  abortMySpendAccountsRequest()
  abortMySpendRequest()
  abortRecommendationAsyncRequests()
  abortOwnerDialogRequests()
  roomLifecycleListing.value = null
  modeKeysRequestSeq += 1
  keyResolutionRequestSeq += 1
  membershipEndOperationRequestSeq += 1
  for (const controller of membershipEndOperationControllers.values()) {
    controller.abort()
  }
  membershipEndOperationControllers.clear()
  for (const controller of membershipEndBindingControllers.values()) controller.abort()
  membershipEndBindingControllers.clear()
  membershipEndBindingLastAttempt.clear()
})
</script>

<style scoped src="./AccountShareView.css"></style>
