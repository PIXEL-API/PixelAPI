<template>
  <AppLayout>
    <div class="mx-auto flex w-full max-w-[1680px] flex-col gap-5">
      <div class="lg:hidden">
        <h1 class="text-2xl font-semibold text-gray-950 dark:text-white">账号广场</h1>
        <p class="mt-1 text-sm text-gray-500 dark:text-dark-300">预约不会仅靠页面等待在后台激活；下一次使用绑定 Key 发出 API 请求时，系统会按顺序尝试激活并接续。</p>
      </div>

      <section class="account-share-hero">
        <div class="account-share-hero-head">
          <div class="flex min-w-0 items-start gap-3">
            <div class="hero-icon">
              <Icon name="users" size="lg" />
            </div>
            <div class="min-w-0">
              <h2 class="text-base font-semibold text-gray-950 dark:text-white">账号模式共享席位</h2>
              <p class="mt-1 max-w-3xl text-sm leading-6 text-gray-500 dark:text-dark-300">
                OpenAI 与 Anthropic OAuth 账号会按账号模式上架。每个账号模式 Key 最多预约 5 个账号，并在下一次 API 请求时按顺序尝试激活。
              </p>
            </div>
          </div>
          <div class="hero-utility-actions">
            <button class="account-share-guide-button" type="button" @click="openUsageGuideDialog">
              <Icon name="book" size="sm" class="mr-2" />
              使用说明
            </button>
            <button class="account-share-spend-button" type="button" @click="openMySpendDialog()">
              <Icon name="dollar" size="sm" class="mr-2" />
              我的消费
            </button>
          </div>
          <div class="hero-actions">
            <button class="btn-secondary h-10" type="button" :disabled="loading || isAnyModeKeysLoading" @click="refreshPageData">
              <Icon name="refresh" size="sm" class="mr-2" :class="{ 'animate-spin': loading || isAnyModeKeysLoading }" />
              刷新
            </button>
            <button class="btn-primary h-10" type="button" @click="toggleCreatePanel">
              <Icon :name="showCreate ? 'chevronUp' : 'plus'" size="sm" class="mr-2" />
              {{ showCreate ? '收起新增' : '新增账号' }}
            </button>
            <button class="btn-secondary h-10" type="button" @click="openRecommendationDialog">
              <Icon name="sparkles" size="sm" class="mr-2" />
              选号助手
            </button>
          </div>
        </div>

        <div class="account-share-platform-tabs" role="tablist" aria-label="账号模式平台">
          <button
            v-for="option in ACCOUNT_SHARE_PLATFORM_OPTIONS"
            :key="option.value"
            type="button"
            role="tab"
            class="account-share-platform-tab"
            :class="activeListingPlatform === option.value ? 'account-share-platform-tab-active' : 'account-share-platform-tab-idle'"
            :aria-selected="activeListingPlatform === option.value"
            @click="setListingPlatform(option.value)"
          >
            <span>{{ option.label }}</span>
            <small>{{ accountModeGroupName(option.value) }}</small>
          </button>
        </div>

        <div class="account-share-summary-grid">
          <div class="summary-cell">
            <span class="summary-icon summary-icon-blue"><Icon name="grid" size="sm" /></span>
            <div>
              <span>当前结果</span>
              <strong>{{ pagination.total }}</strong>
            </div>
          </div>
          <div class="summary-cell">
            <span class="summary-icon summary-icon-emerald"><Icon name="users" size="sm" /></span>
            <div>
              <span>本页可用席位</span>
              <strong>{{ availableSeatCount }}</strong>
            </div>
          </div>
          <div class="summary-cell">
            <span class="summary-icon summary-icon-amber"><Icon name="bolt" size="sm" /></span>
            <div>
              <span>本页已用席位</span>
              <strong>{{ activeSeatCount }}</strong>
            </div>
          </div>
          <div class="summary-cell">
            <span class="summary-icon summary-icon-violet"><Icon name="key" size="sm" /></span>
            <div>
              <span>账号模式 Key</span>
              <strong>{{ modeKeysLoading && !modeKeysLoaded ? '加载中' : modeApiKeys.length }}</strong>
            </div>
          </div>
        </div>
      </section>

      <section
        v-if="isKeyResolutionMode"
        class="key-resolution-panel"
        :class="keyResolutionPanelToneClass"
        role="region"
        aria-label="API Key 关联处置"
        :aria-busy="keyResolutionLoading"
      >
        <div class="key-resolution-main">
          <span class="key-resolution-icon" aria-hidden="true">
            <Icon :name="keyResolutionAllClear ? 'checkCircle' : (keyResolutionError ? 'exclamationCircle' : 'key')" size="md" />
          </span>
          <div class="key-resolution-copy" aria-live="polite">
            <span class="key-resolution-eyebrow">API Key 关联处置</span>
            <h2>{{ keyResolutionAllClear ? '关联已全部解除' : `正在处理 ${keyResolutionKeyLabel}` }}</h2>
            <p>{{ keyResolutionStatusMessage }}</p>
          </div>
        </div>

        <div class="key-resolution-counts" aria-label="待处理关联数量">
          <div>
            <span>正在使用</span>
            <strong>{{ (keyResolutionLoading && !keyResolutionLoaded) || keyResolutionError ? '—' : keyResolutionActiveCount }}</strong>
          </div>
          <div>
            <span>预约中</span>
            <strong>{{ (keyResolutionLoading && !keyResolutionLoaded) || keyResolutionError ? '—' : keyResolutionQueuedCount }}</strong>
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
            {{ keyResolutionLoading ? '核对中' : '刷新状态' }}
          </button>
          <button type="button" class="key-resolution-return-button" @click="returnToApiKeyManagement">
            <Icon name="arrowLeft" size="sm" />
            返回 API Key 管理
          </button>
        </div>
      </section>

      <BaseDialog
        :show="showUsageGuideDialog"
        title="账号模式使用说明"
        width="wide"
        :z-index="55"
        @close="closeUsageGuideDialog"
      >
        <section class="account-share-guide">
          <div class="account-share-guide-summary">
            <span>核心逻辑</span>
            <strong>激活后按分钟预扣小时费，最长 1 小时窗口做最终核销。</strong>
            <p>
              账号模式 Key 会固定调度当前激活账号；预约中的账号不收小时费。系统先按实际激活分钟预扣占位费用，窗口结束或达到 1 小时时再判断请求消费是否满足低消，达标后退回该窗口已预扣的小时费。
            </p>
          </div>

          <div class="account-share-guide-flow" aria-label="账号模式计费流程">
            <div class="account-share-guide-step">
              <span>1</span>
              <strong>加入或预约</strong>
              <p>选择账号并绑定账号模式 Key。当前账号满员时进入预约队列，等待期间不产生小时费。</p>
            </div>
            <div class="account-share-guide-step">
              <span>2</span>
              <strong>激活使用</strong>
              <p>预约不会仅靠页面等待自动激活；下一次使用绑定 Key 发出 API 请求时，系统才会按预约顺序尝试激活并开始占用席位。</p>
            </div>
            <div class="account-share-guide-step">
              <span>3</span>
              <strong>窗口核销</strong>
              <p>按激活时长核算低消，单个核销窗口最长 1 小时；达标则退回该窗口小时费。</p>
            </div>
          </div>

          <div class="account-share-guide-section">
            <h4>费用组成</h4>
            <div class="account-share-guide-rule-list">
              <div>
                <Icon name="calculator" size="sm" />
                <p><strong>请求费用</strong>按模型实际用量乘以账号倍率。例如原始费用 0.10、倍率 1.5x，实际扣费为 0.15。</p>
              </div>
              <div>
                <Icon name="clock" size="sm" />
                <p><strong>小时费</strong>不会一次性扣满 1 小时，而是激活期间按分钟预扣。例如 0.60/小时，即每分钟预扣 0.01。</p>
              </div>
              <div>
                <Icon name="shield" size="sm" />
                <p><strong>免小时费低消</strong>按激活时长折算，最长 1 小时一结。达标后退回该窗口预扣小时费；未达标则不退。</p>
              </div>
            </div>
          </div>

          <div class="account-share-guide-section">
            <h4>退款示例</h4>
            <div class="account-share-guide-example">
              <p>账号小时费 0.60/小时，免小时费低消 0.30/小时。用户在 10:00 到 10:05 激活使用 5 分钟，系统先预扣小时费 0.05。</p>
              <p class="account-share-guide-formula">低消要求 = 0.30 × 5 / 60 = 0.025</p>
              <p>如果这 5 分钟内请求消费达到 0.03，则满足低消，退回 0.05 小时费；如果请求消费只有 0.01，则未满足低消，0.05 小时费不退。</p>
            </div>
            <div class="account-share-guide-example">
              <p>如果一次请求从 10:00:20 执行到 10:01:40，系统按这 80 秒的实际执行区间计入核销窗口，不会只算到完成时所在的某一分钟。</p>
            </div>
          </div>

          <div class="account-share-guide-section">
            <h4>参数说明</h4>
            <dl class="account-share-guide-param-grid">
              <div>
                <dt>倍率</dt>
                <dd>请求费用倍率，倍率越低，请求本身越便宜。</dd>
              </div>
              <div>
                <dt>最低余额</dt>
                <dd>加入前需要满足的余额门槛，避免激活后余额不足。</dd>
              </div>
              <div>
                <dt>账号并发</dt>
                <dd>共享账号整体最多同时处理的请求数量。</dd>
              </div>
              <div>
                <dt>单用户并发</dt>
                <dd>同一个用户在该账号上最多同时占用的请求数量。</dd>
              </div>
              <div>
                <dt>小时费</dt>
                <dd>激活占位期间按分钟预扣的费用。</dd>
              </div>
              <div>
                <dt>免小时费低消</dt>
                <dd>窗口内请求消费达到该标准后，退回对应窗口小时费。</dd>
              </div>
              <div>
                <dt>空闲退出</dt>
                <dd>连续空闲达到设定时间后自动释放席位并停止预扣。</dd>
              </div>
              <div>
                <dt>可用模型</dt>
                <dd>该账号允许调度的模型，请求其他模型不会进入该账号。</dd>
              </div>
            </dl>
          </div>

          <div class="account-share-guide-section account-share-guide-assistant">
            <div class="account-share-guide-assistant-head">
              <span><Icon name="sparkles" size="sm" /></span>
              <div>
                <h4>优先使用选号助手</h4>
                <p>如果不确定哪个账号更划算，建议先用选号助手按你的实际请求量测算，再决定加入哪个账号。</p>
              </div>
            </div>
            <div class="account-share-guide-assistant-grid">
              <div>
                <strong>1. 选择 Key 和模型</strong>
                <p>选择准备用的账号模式 Key 和模型，系统只会推荐支持该模型、且符合当前平台的账号。</p>
              </div>
              <div>
                <strong>2. 填写预计用量</strong>
                <p>填写预计请求次数、使用时长、单次输入/输出 Token；也可以使用近 3 天均值快速带入。</p>
              </div>
              <div>
                <strong>3. 查看推荐结果</strong>
                <p>结果会综合倍率、小时费、低消、席位、并发和可用量，给出预计每小时成本与推荐原因。</p>
              </div>
              <div>
                <strong>4. 再加入使用</strong>
                <p>优先选择成本清晰、席位充足、可用量健康的账号，避免只看单个倍率或小时费。</p>
              </div>
            </div>
          </div>

          <div class="account-share-guide-note">
            <Icon name="infoCircle" size="sm" />
            <p>自用自己的上架账号不收小时费，也不产生号主收益；共享使用时才会进入上述预扣和核销流程。</p>
          </div>
        </section>

        <template #footer>
          <button type="button" class="btn-secondary" @click="closeUsageGuideDialog">我知道了</button>
          <button type="button" class="btn-primary" @click="openRecommendationFromUsageGuide">
            <Icon name="sparkles" size="sm" class="mr-2" />
            打开选号助手
          </button>
        </template>
      </BaseDialog>

      <BaseDialog
        :show="showRecommendationDialog"
        title="账号模式选号助手"
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
                <h2>智能测算</h2>
                <p>{{ platformLabel(activeListingPlatform) }} · {{ accountModeGroupName(activeListingPlatform) }} · 按预计每小时额度升序推荐</p>
              </div>
            </div>
            <div class="recommendation-preset-row" aria-label="测算预设">
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
                {{ recommendationUsageProfileLoading ? '读取中' : '近3天均值' }}
              </button>
            </div>
            <p class="recommendation-profile-help">
              近3天均值会读取你近 3 天历史请求中的单次输入 Token、单次输出 Token、单次 Cache 写入和单次 Cache 读取均值，再按每小时请求量测算预计使用额度。
            </p>
          </div>

        <div class="recommendation-layout">
          <div class="recommendation-form-grid">
            <label class="field">
              <span>账号模式 Key</span>
              <select v-model.number="recommendationForm.api_key_id" class="input h-10" :disabled="modeKeysLoading">
                <option :value="0">{{ modeKeysLoading ? '加载中' : `选择${accountModeGroupName(activeListingPlatform)} Key` }}</option>
                <option v-for="key in recommendationKeyOptions" :key="key.id" :value="key.id">
                  {{ modeKeyLabel(key) }}
                </option>
              </select>
            </label>
            <label class="field">
              <span>模型</span>
              <select v-model="recommendationForm.model" class="input h-10">
                <option v-for="model in recommendationModelOptions" :key="model" :value="model">
                  {{ model }}
                </option>
              </select>
            </label>
            <label class="field">
              <span>请求次数</span>
              <input v-model.number="recommendationForm.request_count" class="input h-10" type="number" min="1" step="1" />
            </label>
            <label class="field">
              <span>使用时长（小时）</span>
              <input v-model.number="recommendationForm.active_hours" class="input h-10" type="number" min="0.1" step="0.1" />
            </label>
            <label class="field">
              <span>单次输入 Token</span>
              <input v-model.number="recommendationForm.input_tokens_per_request" class="input h-10" type="number" min="0" step="1" />
            </label>
            <label class="field">
              <span>单次输出 Token</span>
              <input v-model.number="recommendationForm.output_tokens_per_request" class="input h-10" type="number" min="0" step="1" />
            </label>
            <label class="field">
              <span>单次 Cache 写入</span>
              <input v-model.number="recommendationForm.cache_creation_tokens_per_request" class="input h-10" type="number" min="0" step="1" />
            </label>
            <label class="field">
              <span>单次 Cache 读取</span>
              <input v-model.number="recommendationForm.cache_read_tokens_per_request" class="input h-10" type="number" min="0" step="1" />
            </label>
          </div>

          <div class="recommendation-action-box">
            <button class="btn-primary h-11 w-full" type="button" :disabled="recommendationLoading" @click="runRecommendation">
              <Icon name="sparkles" size="sm" class="mr-2" :class="{ 'animate-spin': recommendationLoading }" />
              {{ recommendationLoading ? '测算中' : '测算并推荐' }}
            </button>
            <p v-if="recommendationUsageProfileMessage" class="recommendation-profile-message">{{ recommendationUsageProfileMessage }}</p>
            <p v-if="recommendationError" class="recommendation-error">{{ recommendationError }}</p>
            <div v-if="recommendationResult" class="recommendation-summary">
              <small>最低预计每小时额度</small>
              <span>{{ recommendationInputSummary }}</span>
              <strong>{{ recommendationBest ? formatRecommendationCost(recommendationEstimatedHourlyCost(recommendationBest)) : '无可用推荐' }}</strong>
              <small>可推荐 {{ recommendationCandidates.length }} 个 / 扫描候选 {{ recommendationResult.candidate_count }} 个</small>
            </div>
          </div>
        </div>

        <div v-if="recommendationResult" class="recommendation-results">
          <div v-if="recommendationCandidates.length === 0" class="recommendation-empty">
            当前平台没有匹配模型、席位和可用状态的账号。
          </div>
          <template v-else>
            <div class="recommendation-results-head">
              <div>
                <strong>推荐结果</strong>
                <span>{{ recommendationPageRangeText }} · 按预计每小时额度从小到大</span>
              </div>
              <div class="recommendation-page-controls">
                <button
                  type="button"
                  class="recommendation-page-button"
                  :disabled="recommendationPage <= 1"
                  aria-label="上一页"
                  @click="setRecommendationPage(recommendationPage - 1)"
                >
                  <Icon name="chevronLeft" size="sm" />
                </button>
                <span>{{ recommendationPage }} / {{ recommendationPageCount }}</span>
                <button
                  type="button"
                  class="recommendation-page-button"
                  :disabled="recommendationPage >= recommendationPageCount"
                  aria-label="下一页"
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
                  <span>预计每小时额度</span>
                  <strong>{{ formatRecommendationCost(recommendationEstimatedHourlyCost(candidate)) }}</strong>
                </div>
              </div>

              <div class="recommendation-tag-row">
                <span v-for="tag in candidate.tags" :key="tag">{{ tag }}</span>
              </div>

              <div class="recommendation-score-panel">
                <div class="recommendation-score-overview">
                  <span>综合匹配度</span>
                  <strong>{{ formatRecommendationScore(recommendationScoreBreakdown(candidate).overall_score) }}</strong>
                </div>
                <div class="recommendation-score-grid">
                  <div
                    v-for="item in recommendationScoreItems(candidate)"
                    :key="item.key"
                    class="recommendation-score-item"
                  >
                    <div>
                      <span>{{ item.label }}</span>
                      <strong>{{ formatRecommendationScore(item.value) }}</strong>
                    </div>
                    <i class="recommendation-score-bar" :style="{ '--score-width': recommendationScoreWidth(item.value) }"></i>
                  </div>
                </div>
              </div>

              <div class="recommendation-metrics">
                <div>
                  <span>{{ recommendationRequestCostLabel(candidate) }}</span>
                  <strong>{{ formatRecommendationCost(candidate.estimate.request_cost) }}</strong>
                </div>
                <div>
                  <span>{{ candidate.estimate.owner_self_use ? '自用单次均摊' : '单次均摊' }}</span>
                  <strong>{{ formatRecommendationCost(candidate.estimate.per_request_cost) }}</strong>
                </div>
                <div>
                  <span>{{ candidate.estimate.owner_self_use ? '自用小时费' : '小时费合计' }}</span>
                  <strong>{{ recommendationHourlyCostText(candidate) }}</strong>
                </div>
                <div>
                  <span>{{ candidate.estimate.owner_self_use ? '自用准入' : '准入预估' }}</span>
                  <strong>{{ recommendationUpfrontCostText(candidate) }}</strong>
                </div>
                <div>
                  <span>{{ candidate.estimate.owner_self_use ? '自用倍率' : '倍率' }}</span>
                  <strong>{{ formatNumber(candidate.estimate.effective_rate_multiplier) }}x</strong>
                </div>
              </div>

              <div v-if="candidate.estimate.owner_self_use" class="recommendation-self-use-note">
                <Icon name="infoCircle" size="sm" />
                <span>{{ recommendationOwnerSelfUseSummary(candidate) }}</span>
              </div>

              <div class="recommendation-reasons">
                <span v-for="reason in candidate.reasons.slice(0, 3)" :key="reason">{{ reason }}</span>
              </div>
              <div v-if="candidate.warnings?.length" class="recommendation-warnings">
                <span v-for="warning in candidate.warnings" :key="warning">{{ warning }}</span>
              </div>

              <div class="recommendation-card-actions">
                <span>席位 {{ candidate.listing.active_seats }}/{{ candidate.listing.seat_limit }} · 并发 {{ recommendationConcurrencyLabel(candidate.listing) }}</span>
                <button class="btn-primary h-10" type="button" :disabled="joiningId === candidate.listing.id" @click="useRecommendedListing(candidate)">
                  <Icon name="login" size="sm" class="mr-2" />
                  加入使用
                </button>
              </div>
            </article>
          </template>
        </div>
        </section>
      </BaseDialog>

      <section v-if="showCreate" class="overflow-hidden rounded-lg border border-gray-200 bg-white shadow-sm dark:border-dark-700 dark:bg-dark-900">
        <div class="flex flex-col gap-3 border-b border-gray-100 p-4 dark:border-dark-800 lg:flex-row lg:items-center lg:justify-between">
          <div>
            <h2 class="text-base font-semibold text-gray-950 dark:text-white">新增共享账号</h2>
            <p class="mt-1 text-sm text-gray-500 dark:text-dark-300">选择平台和代理后走 OAuth 授权，提交后自动创建账号并发布到账号广场。</p>
          </div>
          <button class="btn-secondary h-9 w-fit" type="button" @click="resetCreateForm">
            <Icon name="refresh" size="sm" class="mr-2" />
            重置
          </button>
        </div>

        <div class="grid xl:grid-cols-[minmax(0,1fr)_minmax(420px,520px)]">
          <div class="space-y-5 p-4 xl:p-5">
            <div class="form-section">
              <div class="section-heading">
                <span>基础配置</span>
                <small>账号广场需要代理和席位配置，授权前请先确认。</small>
              </div>
              <div class="grid gap-3 md:grid-cols-2 2xl:grid-cols-4">
                <div class="field">
                  <span>账号平台</span>
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
                      :disabled="creating || generatingOAuthURL"
                      @click="selectCreatePlatform(option.value)"
                    >
                      {{ option.label }}
                    </button>
                  </div>
                  <small>所有账号模式仅支持 OAuth，授权前必须先选择代理。</small>
                </div>

                <label class="field">
                  <span>账号名称</span>
                  <input v-model="createForm.name" class="input" :placeholder="ACCOUNT_NAME_BASE_BY_PLATFORM[createPlatform]" />
                  <small :class="accountNameValidationMessage ? 'text-red-600 dark:text-red-300' : ''">
                    {{ accountNameValidationMessage || '名称必须唯一，且不能包含空格、换行或制表符。' }}
                  </small>
                </label>

                <div class="field md:col-span-2 2xl:col-span-2">
                  <span>代理 IP</span>
                  <ProxySelector
                    v-model="selectedProxyId"
                    :proxies="proxies"
                    :disabled="creating || generatingOAuthURL"
                    :allow-empty="false"
                    :can-test="authStore.isAdmin"
                    disable-full
                    hide-endpoint
                  >
                    <template #actions="{ close }">
                      <div class="grid gap-2 sm:grid-cols-2">
                        <button
                          type="button"
                          class="proxy-action-option"
                          @click.stop="openProxyPurchase(close)"
                        >
                          <span class="proxy-action-icon bg-amber-50 text-amber-600 dark:bg-amber-500/10 dark:text-amber-300">
                            <Icon name="externalLink" size="sm" />
                          </span>
                          <span>
                            <strong>购买 seekproxy</strong>
                            <small>打开 seekproxy 新窗口</small>
                          </span>
                        </button>
                        <button
                          type="button"
                          class="proxy-action-option"
                          @click.stop="openAddProxyDialog(close)"
                        >
                          <span class="proxy-action-icon bg-primary-50 text-primary-600 dark:bg-primary-500/10 dark:text-primary-300">
                            <Icon name="plus" size="sm" />
                          </span>
                          <span>
                            <strong>添加代理 IP</strong>
                            <small>使用自己的动态或静态代理</small>
                          </span>
                        </button>
                      </div>
                    </template>
                  </ProxySelector>
                  <small :class="createProxyCapacityValidationMessage ? 'text-red-600 dark:text-red-300' : ''">
                    {{ createProxyHelperText }}
                  </small>
                </div>

                <label class="field">
                  <span>可使用人数</span>
                  <select v-model.number="createForm.seat_limit" class="input">
                    <option v-for="seat in seatOptions" :key="seat" :value="seat">{{ seat }} 人</option>
                  </select>
                </label>

                <label class="field">
                  <span>账号并发上限</span>
                  <input v-model.number="createForm.concurrency" class="input" type="number" min="1" :max="MAX_ACCOUNT_CONCURRENCY" step="1" />
                  <small :class="concurrencyValidationMessage ? 'text-red-600 dark:text-red-300' : ''">
                    {{ concurrencyValidationMessage || `1-${MAX_ACCOUNT_CONCURRENCY}，推荐默认 20。` }}
                  </small>
                </label>

                <label class="field">
                  <span>单用户最高并发</span>
                  <input v-model.number="createForm.per_user_concurrency" class="input" type="number" min="1" :max="maxPerUserConcurrency" step="1" />
                  <small :class="perUserConcurrencyValidationMessage ? 'text-red-600 dark:text-red-300' : ''">
                    {{ perUserConcurrencyValidationMessage || perUserConcurrencyLimitTip }}
                  </small>
                </label>

                <label class="field">
                  <span>账号倍率</span>
                  <input v-model.number="createForm.rate_multiplier" class="input" type="number" min="0" step="0.01" />
                </label>

                <label class="field">
                  <span>每小时扣费额度</span>
                  <input v-model.number="createForm.hourly_rate" class="input" type="number" min="0" step="0.0001" />
                  <small>默认 0.2，加入后按占位时长预付，用于防止长期占位不使用。</small>
                </label>

                <label class="field">
                  <span>满低消免小时费</span>
                  <input v-model.number="createForm.hourly_fee_waiver_minimum" class="input" type="number" min="0" step="0.0001" />
                  <small>填 0 表示关闭；按每小时低消门槛折算到实际占用时长。</small>
                </label>

                <label class="field">
                  <span>最低余额准入</span>
                  <input v-model.number="createForm.min_balance_required" class="input" type="number" min="0" step="0.01" />
                </label>
              </div>
            </div>

            <div class="form-section">
              <div class="section-heading">
                <span>模型与保护</span>
                <small>{{ createPlatform === 'openai' ? '后端会强制账号模式、ctx_pool 和 Compact 配置，前端只提交可变策略。' : 'Anthropic 账号模式提交 OAuth 凭证、代理、模型白名单和 Claude 额度保护。' }}</small>
              </div>
              <div class="grid gap-4 xl:grid-cols-[minmax(0,1fr)_320px]">
                <div class="field">
                  <span>模型白名单</span>
                  <div class="model-selector-shell">
                    <ModelWhitelistSelector v-model="allowedModels" :platform="createPlatform" />
                  </div>
                  <small>复用“我的账号”新增账号的模型选择器，可搜索、多选并添加自定义模型。</small>
                </div>

                <div v-if="createPlatform === 'openai'" class="grid gap-3 sm:grid-cols-2 lg:grid-cols-1">
                  <label class="field">
                    <span>Codex 5h 保护 %</span>
                    <input v-model.number="createForm.codex_5h_limit_percent" class="input" type="number" min="1" max="100" step="1" />
                  </label>
                  <label class="field">
                    <span>Codex 7d 保护 %</span>
                    <input v-model.number="createForm.codex_7d_limit_percent" class="input" type="number" min="1" max="100" step="1" />
                  </label>
                </div>
                <div v-else class="grid gap-3 sm:grid-cols-2 lg:grid-cols-1">
                  <label class="field">
                    <span>Claude 5h 保护 %</span>
                    <input v-model.number="createForm.anthropic_5h_limit_percent" class="input" type="number" min="1" max="100" step="1" />
                  </label>
                  <label class="field">
                    <span>Claude 7d 保护 %</span>
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
                  <strong>仅允许 Codex 官方客户端</strong>
                  <small>关闭后会允许更多客户端加入该共享账号。</small>
                </span>
              </label>
            </div>
          </div>

          <aside class="border-t border-gray-100 p-4 dark:border-dark-800 xl:border-l xl:border-t-0 xl:p-5">
            <div class="sticky top-4 space-y-4">
              <div class="rounded-lg border border-gray-200 bg-gray-50 p-3 text-sm dark:border-dark-700 dark:bg-dark-800/60">
                <div class="flex items-center justify-between gap-3">
                  <span class="text-gray-500 dark:text-dark-300">发布摘要</span>
                  <span class="rounded-full bg-white px-2 py-1 text-xs font-semibold text-gray-700 dark:bg-dark-700 dark:text-dark-100">
                    {{ createForm.seat_limit }} 人共享
                  </span>
                </div>
                <div class="mt-3 grid grid-cols-2 gap-2">
                  <div class="compact-metric">
                    <span>代理</span>
                    <strong>{{ currentProxyLabel }}</strong>
                  </div>
                  <div class="compact-metric">
                    <span>模型</span>
                    <strong>{{ parsedAllowedModelCount }}</strong>
                  </div>
                  <div class="compact-metric">
                    <span>账号并发</span>
                    <strong>{{ createForm.concurrency }}</strong>
                  </div>
                  <div class="compact-metric">
                    <span>单用户并发</span>
                    <strong>{{ createForm.per_user_concurrency }}</strong>
                  </div>
                  <div class="compact-metric">
                    <span>每人上限</span>
                    <strong>{{ maxPerUserConcurrency }}</strong>
                  </div>
                  <div class="compact-metric">
                    <span>小时费</span>
                    <strong>{{ formatNumber(createForm.hourly_rate) }}</strong>
                  </div>
                  <div class="compact-metric">
                    <span>免小时费低消</span>
                    <strong>{{ hourlyFeeWaiverLabel(createForm.hourly_fee_waiver_minimum) }}</strong>
                  </div>
                </div>
              </div>

              <OAuthAuthorizationFlow
                ref="oauthFlowRef"
                add-method="oauth"
                :auth-url="authURL"
                :session-id="authSessionID"
                :loading="creating || generatingOAuthURL"
                :error="createErrorMessage"
                :show-help="false"
                :show-proxy-warning="false"
                :allow-multiple="false"
                :show-cookie-option="false"
                :show-refresh-token-option="false"
                :show-mobile-refresh-token-option="false"
                :show-session-token-option="false"
                :show-access-token-option="false"
                :platform="createPlatform"
                :show-project-id="false"
                @generate-url="startOAuth"
              />

              <button class="btn-primary h-11 w-full" type="button" :disabled="creating || !canSubmitOAuth" @click="submitOAuth">
                <svg v-if="creating" class="-ml-1 mr-2 h-4 w-4 animate-spin" fill="none" viewBox="0 0 24 24">
                  <circle class="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="4"></circle>
                  <path class="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"></path>
                </svg>
                <Icon v-else name="checkCircle" size="sm" class="mr-2" />
                {{ creating ? '创建中...' : `完成 ${platformLabel(createPlatform)} OAuth 并上架` }}
              </button>
            </div>
          </aside>
        </div>
      </section>

      <BaseDialog
        :show="showProxyDialog"
        title="添加代理 IP"
        width="wide"
        @close="closeProxyDialog"
      >
        <div class="space-y-6">
          <div class="proxy-dialog-section">
            <label class="proxy-dialog-label">智能识别（支持动态/静态代理 IP）</label>
            <textarea
              v-model="proxySmartInput"
              class="proxy-smart-textarea"
              rows="4"
              placeholder="示例：
192.168.0.1:8000:用户名:密码
用户名:密码@192.168.0.1:8000"
              @blur="applySmartProxyInput(false)"
            ></textarea>
            <div class="mt-2 flex flex-wrap items-center gap-2">
              <button type="button" class="btn-secondary h-9" @click="applySmartProxyInput(true)">
                <Icon name="sync" size="sm" class="mr-2" />
                识别填入
              </button>
              <span class="text-xs text-gray-500 dark:text-dark-300">支持 socks5/http/https URL，也支持账号密码前置或冒号分隔格式。</span>
            </div>
          </div>

          <div class="proxy-dialog-divider"></div>

          <label class="proxy-dialog-section">
            <span class="proxy-dialog-label">代理名称</span>
            <input v-model.trim="proxyForm.name" class="input" maxlength="100" placeholder="例如：Roxy 独立 IP / 家宽代理" />
            <small class="text-xs text-gray-500 dark:text-dark-300">用于在下拉框中识别该代理，仅自己可见；不填会按主机和端口自动生成。</small>
          </label>

          <div class="proxy-dialog-section">
            <label class="proxy-dialog-label">代理 IP 类型</label>
            <div class="proxy-ip-type-grid">
              <button
                type="button"
                :class="['proxy-ip-type-option', proxyForm.ip_type === 'ipv4' && 'proxy-ip-type-option-active']"
                @click="proxyForm.ip_type = 'ipv4'"
              >
                <span class="proxy-radio-dot"></span>
                IPV4
              </button>
              <button
                type="button"
                :class="['proxy-ip-type-option', proxyForm.ip_type === 'ipv6' && 'proxy-ip-type-option-active']"
                @click="proxyForm.ip_type = 'ipv6'"
              >
                <span class="proxy-radio-dot"></span>
                IPV6
              </button>
            </div>
          </div>

          <div class="proxy-dialog-section">
            <label class="proxy-dialog-label">代理 IP 信息</label>
            <div class="proxy-endpoint-row">
              <select v-model="proxyForm.protocol" class="proxy-protocol-select">
                <option value="socks5">SOCKS5</option>
                <option value="socks5h">SOCKS5H</option>
                <option value="http">HTTP</option>
                <option value="https">HTTPS</option>
              </select>
              <input v-model.trim="proxyForm.host" class="proxy-host-input" placeholder="主机" />
              <span class="proxy-colon">:</span>
              <input v-model.number="proxyForm.port" class="proxy-port-input" type="number" min="1" max="65535" placeholder="端口" />
            </div>
          </div>

          <div class="grid gap-4 md:grid-cols-2">
            <label class="proxy-dialog-section">
              <span class="proxy-dialog-label">用户名</span>
              <input v-model.trim="proxyForm.username" class="input" placeholder="请输入用户名" />
            </label>
            <label class="proxy-dialog-section">
              <span class="proxy-dialog-label">密码</span>
              <input v-model.trim="proxyForm.password" class="input" type="password" placeholder="请输入密码" />
            </label>
          </div>

          <div v-if="proxyDialogError" class="notice-row border-red-200 bg-red-50 text-red-700 dark:border-red-900/60 dark:bg-red-900/20 dark:text-red-300">
            <Icon name="exclamationCircle" size="sm" class="mt-0.5 flex-shrink-0" />
            <span>{{ proxyDialogError }}</span>
          </div>
        </div>

        <template #footer>
          <button type="button" class="btn-secondary" :disabled="savingProxy" @click="closeProxyDialog">取消</button>
          <button type="button" class="btn-primary" :disabled="savingProxy" @click="saveUserProxy">
            <svg v-if="savingProxy" class="-ml-1 mr-2 h-4 w-4 animate-spin" fill="none" viewBox="0 0 24 24">
              <circle class="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="4"></circle>
              <path class="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"></path>
            </svg>
            <Icon v-else name="checkCircle" size="sm" class="mr-2" />
            保存并使用
          </button>
        </template>
      </BaseDialog>

      <section ref="filterPanelRef" class="filter-panel" @keydown.esc="closeFilterPopover">
        <div class="filter-toolbar">
          <div class="filter-primary-row">
            <label class="filter-search">
              <Icon name="search" size="sm" />
              <input v-model.trim="searchQuery" class="filter-search-input" placeholder="搜索账号、号主或模型" />
            </label>
            <div class="filter-actions" aria-label="账号广场分类">
              <button
                type="button"
                class="owner-filter-button"
                :class="isManagementView && 'owner-filter-button-active'"
                @click="setFilter(ownerFilter)"
              >
                <Icon name="userCircle" size="sm" />
                <span>我的账号</span>
                <small>{{ authStore.isAdmin ? '全部号主' : '号主管理' }}</small>
              </button>
              <span class="filter-divider" aria-hidden="true"></span>
              <button
                v-for="filter in filters"
                :key="filter.key"
                type="button"
                class="filter-chip"
                :class="activeFilter.key === filter.key ? 'filter-chip-active' : 'filter-chip-idle'"
                @click="setFilter(filter)"
              >
                {{ filter.label }}
              </button>
            </div>
          </div>

          <div class="filter-body">
            <div class="filter-body-head">
              <div class="filter-body-title">
                <span class="filter-body-icon"><Icon name="filter" size="sm" /></span>
                <div>
                  <strong>排序与筛选</strong>
                  <small>{{ activeResultFilterCount > 0 ? `已启用 ${activeResultFilterCount} 项` : '默认展示全部可见账号' }}</small>
                </div>
              </div>
              <div class="filter-button-row">
                <button class="filter-reset-button" type="button" :disabled="loading || !hasResultFilters" @click="resetListingFilters">
                  <Icon name="x" size="sm" />
                  <span>重置</span>
                </button>
                <button class="filter-apply-button" type="button" :disabled="loading" @click="applyListingFilters">
                  <Icon name="filter" size="sm" />
                  <span>应用</span>
                </button>
              </div>
            </div>

            <div class="advanced-filter-grid" aria-label="账号广场高级筛选">
              <div class="filter-popover-wrap">
                <span class="filter-section-label">状态</span>
                <button
                  type="button"
                  class="filter-trigger-button"
                  :class="[listingFilters.status !== '' && 'filter-trigger-selected', openFilterPopover === 'status' && 'filter-trigger-active']"
                  :aria-expanded="openFilterPopover === 'status'"
                  aria-haspopup="menu"
                  @click="toggleFilterPopover('status')"
                >
                  <Icon name="filter" size="sm" />
                  <span>{{ statusFilterSummary }}</span>
                  <Icon name="chevronDown" size="xs" class="filter-trigger-chevron" />
                </button>
                <div v-if="openFilterPopover === 'status'" class="filter-popover status-popover" role="menu">
                  <button
                    v-for="option in listingStatusFilterOptions"
                    :key="option.value"
                    type="button"
                    class="filter-menu-option"
                    :class="listingFilters.status === option.value && 'filter-menu-option-active'"
                    @click="setListingStatusFilter(option.value)"
                  >
                    <span>{{ option.label }}</span>
                    <Icon v-if="listingFilters.status === option.value" name="check" size="sm" />
                  </button>
                </div>
              </div>

              <div v-if="isOpenAIListingPlatform" class="filter-popover-wrap">
                <span class="filter-section-label">账号等级</span>
                <button
                  type="button"
                  class="filter-trigger-button"
                  :class="[listingFilters.accountLevel !== 'all' && 'filter-trigger-selected', openFilterPopover === 'level' && 'filter-trigger-active']"
                  :aria-expanded="openFilterPopover === 'level'"
                  aria-haspopup="menu"
                  @click="toggleFilterPopover('level')"
                >
                  <Icon name="badge" size="sm" />
                  <span>{{ accountLevelFilterSummary }}</span>
                  <Icon name="chevronDown" size="xs" class="filter-trigger-chevron" />
                </button>
                <div v-if="openFilterPopover === 'level'" class="filter-popover level-popover" role="menu">
                  <button
                    v-for="option in accountLevelFilterOptions"
                    :key="option.value"
                    type="button"
                    class="filter-menu-option"
                    :class="listingFilters.accountLevel === option.value && 'filter-menu-option-active'"
                    @click="setAccountLevelFilter(option.value)"
                  >
                    <span>{{ option.label }}</span>
                    <Icon v-if="listingFilters.accountLevel === option.value" name="check" size="sm" />
                  </button>
                </div>
              </div>

              <div class="filter-popover-wrap">
                <span class="filter-section-label">账号席位</span>
                <button
                  type="button"
                  class="filter-trigger-button"
                  :class="listingFilters.seatLimits.length > 0 && 'filter-trigger-selected'"
                  :aria-expanded="openFilterPopover === 'seat'"
                  aria-haspopup="menu"
                  @click="toggleFilterPopover('seat')"
                >
                  <Icon name="users" size="sm" />
                  <span>{{ seatFilterSummary }}</span>
                  <Icon name="chevronDown" size="xs" class="filter-trigger-chevron" />
                </button>
                <div v-if="openFilterPopover === 'seat'" class="filter-popover seat-popover" role="menu">
                  <div class="seat-chip-grid">
                    <button
                      v-for="seat in seatOptions"
                      :key="seat"
                      type="button"
                      class="choice-chip"
                      :class="listingFilters.seatLimits.includes(seat) && 'choice-chip-active'"
                      @click="toggleSeatFilter(seat)"
                    >
                      {{ seat }}人
                    </button>
                  </div>
                </div>
              </div>

              <div class="filter-popover-wrap">
                <span class="filter-section-label">标签</span>
                <button
                  type="button"
                  class="filter-trigger-button"
                  :class="listingFilters.featureTags.length > 0 && 'filter-trigger-selected'"
                  :aria-expanded="openFilterPopover === 'feature'"
                  aria-haspopup="menu"
                  @click="toggleFilterPopover('feature')"
                >
                  <Icon name="filter" size="sm" />
                  <span>{{ featureTagFilterSummary }}</span>
                  <Icon name="chevronDown" size="xs" class="filter-trigger-chevron" />
                </button>
                <div v-if="openFilterPopover === 'feature'" class="filter-popover tag-popover" role="menu">
                  <button
                    v-for="option in visibleListingFeatureTagOptions"
                    :key="option.value"
                    type="button"
                    class="filter-menu-option"
                    :class="listingFilters.featureTags.includes(option.value) && 'filter-menu-option-active'"
                    @click="toggleFeatureTagFilter(option.value)"
                  >
                    <span>{{ option.label }}</span>
                    <Icon v-if="listingFilters.featureTags.includes(option.value)" name="check" size="sm" />
                  </button>
                </div>
              </div>

              <div class="filter-popover-wrap model-filter-wrap">
                <span class="filter-section-label">可用模型</span>
                <button
                  type="button"
                  class="filter-trigger-button"
                  :class="listingFilters.models.length > 0 && 'filter-trigger-selected'"
                  :aria-expanded="openFilterPopover === 'model'"
                  aria-haspopup="menu"
                  @click="toggleFilterPopover('model')"
                >
                  <Icon name="filter" size="sm" />
                  <span>{{ modelFilterSummary }}</span>
                  <Icon name="chevronDown" size="xs" class="filter-trigger-chevron" />
                </button>
                <div v-if="openFilterPopover === 'model'" class="filter-popover model-popover" role="menu">
                  <div class="model-filter-options">
                    <button
                      v-for="model in modelFilterOptions"
                      :key="model"
                      type="button"
                      class="filter-menu-option"
                      :class="listingFilters.models.includes(model) && 'filter-menu-option-active'"
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
                      placeholder="输入模型名回车添加"
                      @keydown.enter.prevent="addModelFilterFromInput"
                    />
                    <button type="button" class="btn-secondary h-10" @click="addModelFilterFromInput">添加</button>
                  </div>
                </div>
              </div>
            </div>

            <div class="sort-section" aria-label="账号广场排序">
              <div class="sort-section-head">
                <span class="filter-section-label">排序</span>
              </div>
              <div class="sort-button-grid">
                <button
                  type="button"
                  class="sort-option-button sort-default-button"
                  :class="listingFilters.sortKeys.length === 0 && 'sort-option-active'"
                  :aria-pressed="listingFilters.sortKeys.length === 0"
                  title="清空所有排序条件，恢复账号广场默认排序"
                  @click="clearListingSorts"
                >
                  <Icon name="sort" size="sm" />
                  <span>默认</span>
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

            <div v-if="activeFilterChips.length > 0" class="active-filter-row" aria-label="已选筛选">
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
          </div>
        </div>
      </section>

      <div v-if="errorMessage" class="rounded-lg border border-red-200 bg-red-50 p-3 text-sm text-red-700 dark:border-red-500/30 dark:bg-red-500/10 dark:text-red-200">
        {{ errorMessage }}
      </div>

      <div v-if="visibleQueueSnapshotWarning" class="rounded-lg border border-amber-200 bg-amber-50 p-3 text-sm text-amber-700 dark:border-amber-500/30 dark:bg-amber-500/10 dark:text-amber-200">
        {{ visibleQueueSnapshotWarning }}
      </div>

      <div v-if="loading" class="rounded-lg border border-gray-200 bg-white p-8 text-center text-sm text-gray-500 shadow-sm dark:border-dark-700 dark:bg-dark-900 dark:text-dark-300">
        正在加载账号广场...
      </div>

      <section v-else-if="displayedListings.length > 0" class="listing-grid">
        <article
          v-for="listing in displayedListings"
          :key="listing.id"
          class="listing-card"
          :class="{ 'key-resolution-listing-card': isKeyResolutionListing(listing) }"
        >
          <div class="listing-card-head">
            <div class="listing-card-identity">
              <div class="listing-badge-row">
                <span class="feature-badge">{{ platformLabel(listingPlatform(listing)) }}</span>
                <span v-if="isOpenAIListing(listing)" :class="accountLevelBadgeClass(listing)">
                  {{ accountLevelBadgeLabel(listing) }}
                </span>
                <span v-if="isOpenAIListing(listing) && supportsImageGeneration(listing)" class="feature-badge feature-badge-image">支持生图</span>
                <span v-if="isOpenAIListing(listing) && listing.codex_cli_only" class="feature-badge feature-badge-client-only">仅客户端</span>
                <span
                  v-if="listing.hourly_fee_waiver_minimum > 0"
                  class="feature-badge feature-badge-waiver"
                  :title="`每小时消费满 ${formatNumber(listing.hourly_fee_waiver_minimum)} 免小时费`"
                >
                  满低消免小时费
                </span>
              </div>
              <div class="listing-title-row">
                <h2 class="listing-title">{{ listing.account_name || `共享账号 #${listing.id}` }}</h2>
                <span class="listing-owner">
                  号主：{{ listing.owner_username || `用户 ${listing.owner_user_id}` }}
                  <button
                    type="button"
                    class="owner-inline-button"
                    :title="ownerDialogButtonTitle(listing)"
                    @click="openOwnerDialog(listing)"
                  >
                    <Icon name="eye" size="xs" />
                    <span>其他账号</span>
                  </button>
                </span>
              </div>
            </div>
            <div class="listing-card-state">
              <span class="listing-rating-pill">
                <Icon name="sparkles" size="xs" />
                <span>评分</span>
                <strong>{{ listingRatingLabel(listing) }}</strong>
              </span>
              <span :class="statusBadgeClass(listing.status)">
                {{ statusLabel(listing.status) }}
              </span>
              <span class="listing-seat-pill">
                {{ listing.active_seats }}/{{ listing.seat_limit }}
              </span>
            </div>
          </div>

          <div class="listing-health-panel">
            <div class="listing-health-grid">
              <div class="listing-status-stack">
                <div class="listing-runtime-tile">
                  <Icon name="users" size="sm" />
                  <div>
                    <span class="listing-runtime-label">账号状态</span>
                    <div class="listing-runtime-value-row">
                      <strong>{{ runtimeInsight(listing).label }}</strong>
                      <span :class="runtimeInsightClass(runtimeInsight(listing).tone)">
                        {{ runtimeInsight(listing).badge }}
                      </span>
                    </div>
                    <p v-if="runtimeInsight(listing).detail">{{ runtimeInsight(listing).detail }}</p>
                  </div>
                </div>

                <div class="capacity-panel">
                  <div class="flex items-center justify-between gap-3">
                    <span><Icon name="chart" size="sm" />实时容量</span>
                    <strong>{{ currentConcurrencyLabel(listing) }}</strong>
                  </div>
                  <div class="capacity-track" aria-hidden="true">
                    <div
                      class="capacity-fill"
                      :class="capacityFillClass(listing)"
                      :style="{ width: capacityWidth(listing) }"
                    ></div>
                  </div>
                </div>
              </div>

              <div v-if="showOpenAIUsageWindows(listing)" class="listing-usage-grid">
                <div class="usage-window-row">
                  <div class="usage-window-title">
                    <Icon name="clock" size="sm" />
                    <span>5小时可用量</span>
                    <strong>{{ usageAvailableLabel(listing.codex_5h_usage) }}</strong>
                  </div>
                  <UsageProgressBar
                    v-if="listing.codex_5h_usage"
                    label="5h"
                    :utilization="listing.codex_5h_usage.utilization"
                    :resets-at="listing.codex_5h_usage.resets_at"
                    :window-stats="listing.codex_5h_usage.window_stats"
                    :limit-percent="listing.codex_5h_limit_percent"
                    color="indigo"
                    show-now-when-idle
                  />
                  <span v-else class="usage-empty">暂无快照</span>
                </div>

                <div class="usage-window-row">
                  <div class="usage-window-title">
                    <Icon name="calendar" size="sm" />
                    <span>7天可用量</span>
                    <strong>{{ usageAvailableLabel(listing.codex_7d_usage) }}</strong>
                  </div>
                  <UsageProgressBar
                    v-if="listing.codex_7d_usage"
                    label="7d"
                    :utilization="listing.codex_7d_usage.utilization"
                    :resets-at="listing.codex_7d_usage.resets_at"
                    :window-stats="listing.codex_7d_usage.window_stats"
                    :limit-percent="listing.codex_7d_limit_percent"
                    color="emerald"
                    show-now-when-idle
                  />
                  <span v-else class="usage-empty">暂无快照</span>
                </div>
              </div>
              <div v-else-if="showAnthropicUsageWindows(listing)" class="listing-usage-grid">
                <div class="usage-window-row">
                  <div class="usage-window-title">
                    <Icon name="clock" size="sm" />
                    <span>5小时 Claude 额度</span>
                    <strong>{{ usageAvailableLabel(listing.anthropic_5h_usage) }}</strong>
                  </div>
                  <UsageProgressBar
                    v-if="listing.anthropic_5h_usage"
                    label="5h"
                    :utilization="listing.anthropic_5h_usage.utilization"
                    :resets-at="listing.anthropic_5h_usage.resets_at"
                    :window-stats="listing.anthropic_5h_usage.window_stats"
                    :limit-percent="anthropic5hLimitPercent(listing)"
                    color="indigo"
                    show-now-when-idle
                  />
                  <span v-else class="usage-empty">暂无快照</span>
                </div>

                <div class="usage-window-row">
                  <div class="usage-window-title">
                    <Icon name="calendar" size="sm" />
                    <span>7天 Claude 额度</span>
                    <strong>{{ usageAvailableLabel(listing.anthropic_7d_usage) }}</strong>
                  </div>
                  <UsageProgressBar
                    v-if="listing.anthropic_7d_usage"
                    label="7d"
                    :utilization="listing.anthropic_7d_usage.utilization"
                    :resets-at="listing.anthropic_7d_usage.resets_at"
                    :window-stats="listing.anthropic_7d_usage.window_stats"
                    :limit-percent="anthropic7dLimitPercent(listing)"
                    color="emerald"
                    show-now-when-idle
                  />
                  <span v-else class="usage-empty">暂无快照</span>
                </div>
              </div>
            </div>

            <div class="listing-health-foot" :class="{ 'listing-health-foot-empty': !listingHealthFootVisible(listing) }">
              <span v-if="showOpenAIUsageWindows(listing) && listing.codex_usage_updated_at">用量更新：{{ formatDate(listing.codex_usage_updated_at) }}</span>
              <span v-if="showOpenAIUsageWindows(listing) && listing.codex_quota_protection_reset_at">保护解除：{{ formatRelativeUntil(listing.codex_quota_protection_reset_at) }}</span>
              <span v-if="showAnthropicUsageWindows(listing) && listing.anthropic_usage_updated_at">用量更新：{{ formatDate(listing.anthropic_usage_updated_at) }}</span>
              <span v-if="showAnthropicUsageWindows(listing) && listing.anthropic_quota_protection_reset_at">保护解除：{{ formatRelativeUntil(listing.anthropic_quota_protection_reset_at) }}</span>
              <span v-if="listing.rate_limit_reset_at">限流解除：{{ formatRelativeUntil(listing.rate_limit_reset_at) }}</span>
            </div>

            <div v-if="validityInfo(listing)" class="validity-strip">
              <div class="flex min-w-0 items-center gap-2">
                <Icon name="calendar" size="sm" />
                <span>{{ validityInfo(listing)?.label }}</span>
              </div>
              <strong>{{ validityInfo(listing)?.expiresAtLabel }}</strong>
            </div>
          </div>

          <div class="listing-metric-grid">
            <div class="metric metric-billing" :class="{ 'metric-price-danger': isRateMultiplierExpensive(listing) }">
              <span class="metric-label"><Icon name="bolt" size="xs" />倍率</span>
              <strong>{{ formatNumber(listing.rate_multiplier) }}x</strong>
            </div>
            <div class="metric metric-billing">
              <span class="metric-label"><Icon name="creditCard" size="xs" />最低余额</span>
              <strong>{{ formatNumber(listing.min_balance_required) }}</strong>
            </div>
            <div class="metric">
              <span class="metric-label"><Icon name="users" size="xs" />账号并发</span>
              <strong>{{ listing.account_concurrency }}</strong>
            </div>
            <div class="metric">
              <span class="metric-label"><Icon name="user" size="xs" />单用户并发</span>
              <strong>{{ listing.per_user_concurrency }}</strong>
            </div>
            <div class="metric metric-billing" :class="{ 'metric-price-danger': isHourlyRateExpensive(listing) }">
              <span class="metric-label"><Icon name="clock" size="xs" />小时费</span>
              <strong>{{ formatNumber(listing.hourly_rate) }}</strong>
            </div>
            <div class="metric metric-billing">
              <span class="metric-label"><Icon name="shield" size="xs" />免小时费低消</span>
              <strong>{{ hourlyFeeWaiverLabel(listing.hourly_fee_waiver_minimum) }}</strong>
            </div>
            <div v-if="showOpenAIUsageWindows(listing)" class="metric">
              <span class="metric-label"><Icon name="lock" size="xs" />Codex保护</span>
              <strong>{{ listing.codex_5h_limit_percent }}% / {{ listing.codex_7d_limit_percent }}%</strong>
            </div>
            <div v-else-if="showAnthropicUsageWindows(listing)" class="metric">
              <span class="metric-label"><Icon name="lock" size="xs" />Claude保护</span>
              <strong>{{ anthropic5hLimitPercent(listing) }}% / {{ anthropic7dLimitPercent(listing) }}%</strong>
            </div>
          </div>

          <div class="listing-bottom-bar">
            <div class="listing-model-row">
              <button
                v-for="model in visibleModels(listing)"
                :key="model"
                type="button"
                class="model-copy-chip"
                :title="`复制 ${model}`"
                @click="copyModelName(model)"
              >
                {{ model }}
              </button>
              <span v-if="hiddenModels(listing).length > 0" class="model-overflow-wrapper">
                <button type="button" class="model-overflow-chip" :aria-label="`还有 ${hiddenModels(listing).length} 个模型`">
                  +{{ hiddenModels(listing).length }}
                </button>
                <span class="model-overflow-popover" role="tooltip">
                  <button
                    v-for="model in hiddenModels(listing)"
                    :key="model"
                    type="button"
                    class="model-overflow-model"
                    :title="`复制 ${model}`"
                    @click="copyModelName(model)"
                  >
                    {{ model }}
                  </button>
                </span>
              </span>
            </div>

            <div v-if="canShowListingJoinSection(listing)" class="listing-join-section">
              <div v-if="listingEditLocked(listing)" class="edit-lock-strip">
                <Icon name="exclamationCircle" size="sm" />
                <span>账号配置正在编辑中，暂时不能加入使用，避免使用修改前的旧配置。</span>
              </div>
              <div class="listing-action-row">
                <div v-if="singleModeApiKeyForListing(listing)" class="mode-key-readonly">
                  <Icon name="key" size="sm" />
                  <span>{{ singleModeApiKeyLabelForListing(listing) }}</span>
                </div>
                <select v-else v-model.number="selectedKeyByListing[listing.id]" class="input h-9" :disabled="modeKeysLoading || !modeKeysLoaded">
                  <option :value="0">{{ modeApiKeyPlaceholderForListing(listing) }}</option>
                  <option v-for="key in modeApiKeysForListing(listing)" :key="key.id" :value="key.id">{{ key.name || `Key #${key.id}` }}</option>
                </select>
                <div class="listing-timeout-row">
                  <label class="idle-timeout-join idle-timeout-join-inline">
                    <span>空闲退出</span>
                    <div class="idle-timeout-input-row">
                      <input
                        v-model.number="idleTimeoutByListing[listing.id]"
                        class="input h-9"
                        type="number"
                        min="1"
                        :max="ACCOUNT_SHARE_IDLE_TIMEOUT_MAX_MINUTES"
                        step="1"
                      />
                      <span class="idle-timeout-join-unit">分钟</span>
                    </div>
                  </label>
                  <div class="idle-timeout-inline-note">
                    <Icon name="infoCircle" size="xs" />
                    <span>{{ isOwnListing(listing) ? '默认 10 分钟。连续空闲到设定时间后会自动解除绑定，不能填 0。' : '默认 10 分钟。连续空闲到设定时间后会自动退出并停止占位，不能填 0。' }}</span>
                  </div>
                </div>
                <button class="btn-primary h-9" type="button" :disabled="listingEditLocked(listing) || modeKeysLoading || joiningId === listing.id" @click="joinUse(listing)">
                  {{ joiningId === listing.id ? (isOwnListing(listing) ? '绑定中' : '加入中') : (modeKeysLoading ? '加载 Key 中' : (isOwnListing(listing) ? '使用自己的账号' : '加入使用')) }}
                </button>
              </div>
            </div>

          </div>

          <template v-if="isManagementView">
            <div class="mt-3 rounded-lg border border-gray-200 bg-gray-50 p-3 text-sm dark:border-dark-700 dark:bg-dark-800/60">
              <div class="flex flex-col gap-1 text-gray-600 dark:text-dark-200">
                <span>账号 ID：#{{ listing.account_id }}</span>
                <span>更新：{{ formatDate(listing.updated_at) }}</span>
              </div>
              <div class="mt-3 flex flex-wrap gap-2">
                <button
                  type="button"
                  class="btn-secondary h-9"
                  :disabled="managedActionId === listing.id"
                  :title="listingEditLockedByOther(listing) ? listingEditLockLabel(listing) : ''"
                  @click="requestOpenConfigEdit(listing)"
                >
                  <Icon name="edit" size="xs" class="mr-2" />
                  编辑配置
                </button>
                <button
                  type="button"
                  class="btn-secondary h-9"
                  :disabled="savingModelsId === listing.id"
                  @click="openModelEditDialog(listing)"
                >
                  <Icon name="edit" size="xs" class="mr-2" />
                  编辑模型
                </button>
                <button
                  type="button"
                  class="btn-secondary h-9"
                  :disabled="managedActionId === listing.id"
                  @click="openManagedAccountModal(listing, 'test')"
                >
                  <Icon name="play" size="xs" class="mr-2" />
                  测试连接
                </button>
                <button
                  type="button"
                  class="btn-secondary h-9"
                  :disabled="managedActionId === listing.id"
                  @click="openManagedAccountModal(listing, 'stats')"
                >
                  <Icon name="chart" size="xs" class="mr-2" />
                  统计
                </button>
                <button
                  type="button"
                  class="btn-secondary h-9"
                  :disabled="managedActionId === listing.id"
                  @click="openManagedAccountModal(listing, 'reauth')"
                >
                  <Icon name="link" size="xs" class="mr-2" />
                  重新授权
                </button>
                <button
                  type="button"
                  class="btn-secondary h-9"
                  :disabled="managedActionId === listing.id"
                  @click="refreshManagedAccountToken(listing)"
                >
                  <Icon name="refresh" size="xs" class="mr-2" :class="{ 'animate-spin': managedActionId === listing.id }" />
                  刷新 Token
                </button>
                <button
                  v-if="hasRecoverableListingState(listing)"
                  type="button"
                  class="btn-secondary h-9 text-emerald-700 dark:text-emerald-200"
                  :disabled="managedActionId === listing.id"
                  @click="recoverManagedAccountState(listing)"
                >
                  <Icon name="sync" size="xs" class="mr-2" />
                  恢复状态
                </button>
                <button
                  v-if="canOwnerRelistListing(listing)"
                  type="button"
                  class="btn-primary h-9"
                  :disabled="managingId === listing.id"
                  title="重新上架前会自动测试账号可用性"
                  @click="updateManagedListingStatus(listing, 'active')"
                >
                  <Icon name="play" size="xs" class="mr-2" />
                  {{ managingId === listing.id ? '测试中...' : '重新上架' }}
                </button>
                <button
                  v-if="authStore.isAdmin && listing.status !== 'active'"
                  type="button"
                  class="btn-primary h-9"
                  :disabled="managingId === listing.id"
                  @click="updateManagedListingStatus(listing, 'active')"
                >
                  <Icon name="play" size="xs" class="mr-2" />
                  重新上架
                </button>
                <button
                  v-if="authStore.isAdmin && listing.status === 'active'"
                  type="button"
                  class="btn-secondary h-9"
                  :disabled="managingId === listing.id"
                  @click="updateManagedListingStatus(listing, 'paused')"
                >
                  <Icon name="ban" size="xs" class="mr-2" />
                  暂停
                </button>
                <button
                  v-if="authStore.isAdmin && listing.status !== 'disabled'"
                  type="button"
                  class="btn-danger-soft h-9"
                  :disabled="managingId === listing.id"
                  @click="updateManagedListingStatus(listing, 'disabled')"
                >
                  <Icon name="xCircle" size="xs" class="mr-2" />
                  下架
                </button>
              </div>
              <div v-if="listingEditLocked(listing)" class="edit-lock-strip mt-3">
                <Icon name="exclamationCircle" size="sm" />
                <span>{{ listingEditLockLabel(listing) }}</span>
              </div>
            </div>
          </template>
          <div v-if="listing.queue_membership_id" class="account-share-membership-panel">
            <div class="membership-status-head">
              <div>
                <div class="membership-title">
                  {{ listing.current_membership_id ? '正在使用' : '预约队列' }}，绑定 {{ boundApiKeyDisplayName(listing) }}
                </div>
                <div class="membership-subtitle">
                  {{ listing.current_membership_id ? idleTimeoutSummary(listing) : queueIdleTimeoutSummary(listing) }}
                  <span v-if="boundApiKeyID(listing)"> · ID #{{ boundApiKeyID(listing) }}</span>
                </div>
              </div>
              <span :class="queueStatusPillClass(listing)">{{ queueStatusLabel(listing) }}</span>
            </div>
            <div class="membership-compact-body">
              <div class="membership-main">
                <div class="membership-detail-grid">
                  <div v-if="listing.queue_rank">
                    <span>预约顺序</span>
                    <strong>第 {{ listing.queue_rank }} 位</strong>
                  </div>
                  <div v-if="listing.current_joined_at">
                    <span>激活时间</span>
                    <strong>{{ formatDate(listing.current_joined_at) }}</strong>
                  </div>
                  <div v-if="waiverProgressVisible(listing)">
                    <span>窗口剩余</span>
                    <strong>{{ waiverProgressRemainingLabel(listing) }}</strong>
                  </div>
                  <div v-else-if="listing.current_paid_until">
                    <span>下次预付</span>
                    <strong>{{ formatCountdownUntil(listing.current_paid_until) }}</strong>
                  </div>
                  <div v-if="listing.current_last_request_at || listing.current_waiver_progress?.last_request_at">
                    <span>最近请求</span>
                    <strong>{{ formatDate(listing.current_waiver_progress?.last_request_at || listing.current_last_request_at) }}</strong>
                  </div>
                  <div v-if="listing.current_billed_until && !waiverProgressVisible(listing)">
                    <span>已结算到</span>
                    <strong>{{ formatDate(listing.current_billed_until) }}</strong>
                  </div>
                  <div v-if="listing.queue_dispatch_cooldown_until">
                    <span>失败冷却</span>
                    <strong>{{ formatRelativeUntil(listing.queue_dispatch_cooldown_until) }}</strong>
                  </div>
                </div>

                <div
                  v-if="waiverProgressVisible(listing)"
                  class="waiver-progress-card"
                  :class="waiverProgressToneClass(listing)"
                >
                  <div class="waiver-progress-top">
                    <div>
                      <span>低消进度</span>
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
                <div class="idle-timeout-control">
                  <label :for="`idle-timeout-current-${listing.id}`">空闲退出</label>
                  <div class="idle-timeout-row">
                    <input
                      :id="`idle-timeout-current-${listing.id}`"
                      v-model.number="idleTimeoutByListing[listing.id]"
                      class="input h-9"
                      type="number"
                      min="1"
                      :max="ACCOUNT_SHARE_IDLE_TIMEOUT_MAX_MINUTES"
                      step="1"
                    />
                    <span>分钟</span>
                    <button
                      class="btn-secondary h-9"
                      type="button"
                      :disabled="savingIdleTimeoutId === listing.queue_membership_id"
                      @click="saveIdleTimeout(listing)"
                    >
                      保存
                    </button>
                  </div>
                </div>
                <div class="membership-action-row">
                  <button
                    class="btn-secondary h-9"
                    type="button"
                    :disabled="!canMoveQueueItem(listing, -1)"
                    @click="moveQueueItem(listing, -1)"
                  >
                    <Icon name="chevronUp" size="xs" class="mr-2" />
                    上移
                  </button>
                  <button
                    class="btn-secondary h-9"
                    type="button"
                    :disabled="!canMoveQueueItem(listing, 1)"
                    @click="moveQueueItem(listing, 1)"
                  >
                    <Icon name="chevronDown" size="xs" class="mr-2" />
                    下移
                  </button>
                  <button
                    class="membership-end-button"
                    type="button"
                    :disabled="endingId !== null"
                    @click="openEndUseConfirm(listing)"
                  >
                    {{ listing.current_membership_id ? '结束使用' : '移出预约' }}
                  </button>
                </div>
                <div
                  class="idle-timeout-hint"
                  :title="listing.current_membership_id ? (isOwnListing(listing) ? '连续空闲达到设定分钟数后会自动解除绑定，不能填 0。' : '连续空闲达到设定分钟数后会自动退出并停止占位，不能填 0。') : '该设置会在预约项被激活后生效，不能填 0。'"
                >
                  {{ listing.current_membership_id ? (isOwnListing(listing) ? '空闲到时自动解除绑定' : '空闲到时自动退出并停止占位') : '预约激活后生效' }}
                </div>
              </div>
            </div>
          </div>
        </article>
      </section>

      <div v-else class="rounded-lg border border-dashed border-gray-300 bg-white p-8 text-center text-sm text-gray-500 dark:border-dark-700 dark:bg-dark-900 dark:text-dark-300">
        {{ isKeyResolutionMode ? (keyResolutionError ? '关联账号详情暂时无法加载，请在上方刷新状态后重试。' : '当前 API Key 没有需要处理的关联账号。') : (pagination.total === 0 ? (hasResultFilters ? '没有匹配的共享账号。' : (isManagementView ? '暂无可管理账号。' : '当前分类暂无账号。')) : '当前页暂无账号。') }}
      </div>

      <Pagination
        v-if="!isKeyResolutionMode && !loading && pagination.total > pagination.page_size"
        class="overflow-hidden rounded-lg border border-gray-200 shadow-sm dark:border-dark-700"
        :page="pagination.page"
        :total="pagination.total"
        :page-size="pagination.page_size"
        :show-page-size-selector="false"
        @update:page="handlePageChange"
        @update:pageSize="handlePageSizeChange"
      />
    </div>

    <BaseDialog
      :show="showModelEditDialog"
      title="编辑模型白名单"
      width="wide"
      @close="closeModelEditDialog"
    >
      <ModelWhitelistSelector v-model="editingAllowedModels" :platform="listingPlatform(editingModelListing)" />

      <template #footer>
        <button type="button" class="btn-secondary" :disabled="savingModelsId !== null" @click="closeModelEditDialog">取消</button>
        <button
          type="button"
          class="btn-primary"
          :disabled="savingModelsId !== null || editingAllowedModels.length === 0"
          @click="saveModelEdit"
        >
          <Icon v-if="savingModelsId === null" name="checkCircle" size="sm" class="mr-2" />
          <svg v-else class="-ml-1 mr-2 h-4 w-4 animate-spin" fill="none" viewBox="0 0 24 24">
            <circle class="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="4"></circle>
            <path class="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"></path>
          </svg>
          保存
        </button>
      </template>
    </BaseDialog>

    <BaseDialog
      :show="pendingJoinConfirmation !== null"
      :title="pendingJoinIsOwnerSelfUse ? '确认使用自己的账号' : '确认加入共享账号'"
      width="wide"
      :z-index="60"
      @close="closeJoinConfirmation"
    >
      <div v-if="pendingJoinListing" class="join-confirmation">
        <div class="join-confirmation-head" :class="{ 'join-confirmation-head-danger': pendingJoinPriceWarnings.length > 0 }">
          <span class="join-confirmation-icon">
            <Icon :name="pendingJoinPriceWarnings.length > 0 ? 'exclamationCircle' : 'infoCircle'" size="md" />
          </span>
          <div class="min-w-0">
            <strong>{{ listingDisplayName(pendingJoinListing) }}</strong>
            <span>{{ pendingJoinIsOwnerSelfUse ? '这是你自己上架的账号，绑定后只按 0.005x 计算请求费用，不收小时费，也不占用共享席位。' : '加入后该 API Key 会绑定到这个账号，请确认价格、并发和模型限制后再继续。' }}</span>
          </div>
        </div>

        <div v-if="pendingJoinPriceWarnings.length > 0" class="join-warning-list">
          <div v-for="warning in pendingJoinPriceWarnings" :key="warning" class="join-warning-item">
            <Icon name="exclamationCircle" size="sm" />
            <span>{{ warning }}</span>
          </div>
        </div>

        <div class="join-confirmation-grid">
          <div v-if="isOpenAIListing(pendingJoinListing)" class="join-confirmation-field">
            <span>账号等级</span>
            <strong>{{ accountLevelBadgeLabel(pendingJoinListing) }}</strong>
          </div>
          <div class="join-confirmation-field" :class="{ 'join-price-danger': isRateMultiplierExpensive(pendingJoinListing) }">
            <span>倍率</span>
            <strong>{{ pendingJoinIsOwnerSelfUse ? `${formatNumber(OWNER_SELF_USE_RATE_MULTIPLIER)}x` : `${formatNumber(pendingJoinListing.rate_multiplier)}x` }}</strong>
          </div>
          <div class="join-confirmation-field" :class="{ 'join-price-danger': isHourlyRateExpensive(pendingJoinListing) }">
            <span>小时费</span>
            <strong>{{ pendingJoinIsOwnerSelfUse ? '不收取' : formatNumber(pendingJoinListing.hourly_rate) }}</strong>
          </div>
          <div class="join-confirmation-field">
            <span>免小时费低消</span>
            <strong>{{ pendingJoinIsOwnerSelfUse ? '不适用' : hourlyFeeWaiverLabel(pendingJoinListing.hourly_fee_waiver_minimum) }}</strong>
          </div>
          <div class="join-confirmation-field">
            <span>最低余额</span>
            <strong>{{ pendingJoinIsOwnerSelfUse ? '不校验' : formatNumber(pendingJoinListing.min_balance_required) }}</strong>
          </div>
          <div class="join-confirmation-field">
            <span>账号并发</span>
            <strong>{{ pendingJoinListing.account_concurrency }}</strong>
          </div>
          <div class="join-confirmation-field">
            <span>单用户并发</span>
            <strong>{{ pendingJoinListing.per_user_concurrency }}</strong>
          </div>
          <div class="join-confirmation-field">
            <span>绑定 Key</span>
            <strong>{{ pendingJoinApiKeyLabel }}</strong>
          </div>
          <div class="join-confirmation-field">
            <span>空闲退出</span>
            <strong>{{ pendingJoinIdleTimeoutLabel }}</strong>
          </div>
          <div v-if="isOpenAIListing(pendingJoinListing)" class="join-confirmation-field">
            <span>Codex保护</span>
            <strong>{{ pendingJoinListing.codex_5h_limit_percent }}% / {{ pendingJoinListing.codex_7d_limit_percent }}%</strong>
          </div>
          <div v-else-if="showAnthropicUsageWindows(pendingJoinListing)" class="join-confirmation-field">
            <span>Claude保护</span>
            <strong>{{ anthropic5hLimitPercent(pendingJoinListing) }}% / {{ anthropic7dLimitPercent(pendingJoinListing) }}%</strong>
          </div>
        </div>

        <div class="join-usage-reminder">
          <Icon name="infoCircle" size="sm" />
          <span>{{ pendingJoinIsOwnerSelfUse ? `确认使用后，连续空闲达到 ${pendingJoinIdleTimeoutLabel} 会自动解除绑定；自用期间不产生小时费和号主收益。` : `若进入预约，下一次使用该 Key 发出 API 请求时才会按顺序尝试激活。激活后小时费按分钟预扣，连续空闲达到 ${pendingJoinIdleTimeoutLabel} 会自动退出并停止占位。` }}</span>
        </div>

        <div class="join-model-confirmation">
          <span>可用模型</span>
          <div>
            <button
              v-for="model in visibleModels(pendingJoinListing)"
              :key="model"
              type="button"
              class="model-copy-chip"
              :title="`复制 ${model}`"
              @click="copyModelName(model)"
            >
              {{ model }}
            </button>
            <span v-if="hiddenModels(pendingJoinListing).length > 0" class="join-model-more">+{{ hiddenModels(pendingJoinListing).length }}</span>
          </div>
        </div>
      </div>

      <template #footer>
        <button type="button" class="btn-secondary" :disabled="joiningId !== null" @click="closeJoinConfirmation">取消</button>
        <button type="button" class="btn-primary" :disabled="joiningId !== null" @click="confirmJoinUse">
          <Icon v-if="joiningId === null" name="checkCircle" size="sm" class="mr-2" />
          <svg v-else class="-ml-1 mr-2 h-4 w-4 animate-spin" fill="none" viewBox="0 0 24 24">
            <circle class="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="4"></circle>
            <path class="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"></path>
          </svg>
          {{ pendingJoinIsOwnerSelfUse ? '确认使用' : '确认加入' }}
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
        <button type="button" class="btn-secondary" @click="closeActionErrorDialog">我知道了</button>
        <button
          v-if="actionErrorDialog.action === 'create-mode-key'"
          type="button"
          class="btn-primary"
          @click="goCreateModeApiKey"
        >
          <Icon name="key" size="sm" class="mr-2" />
          去创建 API Key
        </button>
      </template>
    </BaseDialog>

    <BaseDialog
      :show="showMySpendDialog"
      title="我的消费"
      width="wide"
      :z-index="65"
      @close="closeMySpendDialog"
    >
      <div class="my-spend-panel">
        <div class="my-spend-account-picker">
          <div class="my-spend-account-picker-head">
            <div>
              <span>选择使用过的账号</span>
              <strong>{{ mySpendAccountPickerTitle }}</strong>
              <small>包含正在使用、预约中和历史使用记录；选择账号后下方统计会按该账号刷新。</small>
            </div>
            <button type="button" class="btn-secondary h-9" :disabled="mySpendAccountsLoading" @click="loadMySpendAccountOptions()">
              <Icon name="refresh" size="xs" class="mr-2" :class="{ 'animate-spin': mySpendAccountsLoading }" />
              刷新账号
            </button>
          </div>

          <div v-if="mySpendAccountsError" class="notice-row border-red-200 bg-red-50 text-red-700 dark:border-red-900/60 dark:bg-red-900/20 dark:text-red-300">
            <Icon name="exclamationCircle" size="sm" class="mt-0.5 flex-shrink-0" />
            <span>{{ mySpendAccountsError }}</span>
          </div>

          <div v-if="mySpendAccountsLoading && mySpendAccountOptions.length === 0" class="my-spend-loading">
            正在加载使用过的账号...
          </div>
          <div v-else-if="!mySpendAccountsLoading && mySpendAccountOptions.length === 0" class="my-spend-empty">
            暂无可统计的账号。加入或使用账号后，这里会展示账号选择和消费统计。
          </div>
          <div v-else class="my-spend-account-grid">
            <button
              v-for="option in mySpendAccountOptions"
              :key="option.key"
              type="button"
              class="my-spend-account-option"
              :class="{ active: mySpendSelectedOptionKey === option.key }"
              :title="mySpendAccountOptionTitle(option)"
              @click="selectMySpendAccount(option)"
            >
              <span class="my-spend-account-option-top">
                <span class="feature-badge">{{ platformLabel(listingPlatform(option.listing)) }}</span>
                <span>{{ mySpendAccountSourceLabel(option.source) }}</span>
              </span>
              <strong>{{ listingDisplayName(option.listing) }}</strong>
              <small>{{ mySpendAccountUsagePeriod(option.listing) }}</small>
              <span class="my-spend-account-option-foot">
                <span>记录 #{{ option.membershipID }}</span>
                <span>{{ mySpendAccountStatusLabel(option.listing) }}</span>
              </span>
            </button>
          </div>
        </div>

        <div v-if="mySpendListing" class="my-spend-context">
          <span class="my-spend-context-icon">
            <Icon name="dollar" size="md" />
          </span>
          <div class="min-w-0">
            <span class="my-spend-eyebrow">{{ platformLabel(listingPlatform(mySpendListing)) }} · 共享账号 #{{ mySpendListing.id }}</span>
            <strong>{{ mySpendListing.account_name || `共享账号 #${mySpendListing.id}` }}</strong>
            <small>
              号主：{{ mySpendListing.owner_username || `用户 ${mySpendListing.owner_user_id}` }}
              <template v-if="mySpendMembershipID(mySpendListing) > 0">
                · 使用记录 #{{ mySpendMembershipID(mySpendListing) }}
              </template>
            </small>
          </div>
        </div>

        <div class="my-spend-toolbar">
          <div class="my-spend-range-tabs" role="tablist" aria-label="消费统计范围">
            <button
              v-for="option in MY_SPEND_RANGE_OPTIONS"
              :key="option.value"
              type="button"
              :class="{ active: mySpendRange === option.value }"
              :aria-selected="mySpendRange === option.value"
              role="tab"
              @click="setMySpendRange(option.value)"
            >
              {{ option.label }}
            </button>
          </div>
          <button type="button" class="btn-secondary h-9" :disabled="mySpendLoading || !mySpendListing" @click="loadMySpendSummary">
            <Icon name="refresh" size="xs" class="mr-2" />
            刷新
          </button>
        </div>

        <div v-if="!mySpendLoading && !mySpendListing && mySpendAccountOptions.length > 0" class="my-spend-empty">
          请选择一个账号查看使用时间段、费用明细和统计面板。
        </div>

        <div v-if="mySpendError" class="notice-row border-red-200 bg-red-50 text-red-700 dark:border-red-900/60 dark:bg-red-900/20 dark:text-red-300">
          <Icon name="exclamationCircle" size="sm" class="mt-0.5 flex-shrink-0" />
          <span>{{ mySpendError }}</span>
        </div>

        <div v-if="mySpendLoading && !mySpendSummary" class="my-spend-loading">
          正在加载消费统计...
        </div>

        <template v-else-if="mySpendSummary">
          <div class="my-spend-window">
            <div>
              <span>{{ mySpendRangeLabel(mySpendSummary.range) }}</span>
              <strong>{{ mySpendWindowLabel(mySpendSummary) }}</strong>
            </div>
            <div>
              <span>最近入账</span>
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
              <span>统计账号</span>
              <strong>{{ mySpendAccountName(mySpendSummary) }}</strong>
            </div>
            <div>
              <span>绑定 Key</span>
              <strong>{{ mySpendBoundApiKeyName(mySpendSummary.membership) }}</strong>
              <small v-if="mySpendSummary.membership?.api_key_id">ID #{{ mySpendSummary.membership.api_key_id }}</small>
            </div>
            <div>
              <span>使用状态</span>
              <strong>{{ mySpendStatusLabel(mySpendSummary.membership?.status) }}</strong>
            </div>
            <div>
              <span>加入时间</span>
              <strong>{{ formatDate(mySpendSummary.membership?.joined_at) }}</strong>
            </div>
            <div>
              <span>请求均价</span>
              <strong>{{ mySpendAverageRequestCost(mySpendSummary) }}</strong>
            </div>
            <div>
              <span>低消门槛</span>
              <strong>{{ mySpendSummary.membership ? hourlyFeeWaiverLabel(mySpendSummary.membership.waiver_minimum) : '-' }}</strong>
            </div>
          </div>

          <div class="my-spend-hourly-panel">
            <div>
              <span>小时费已预扣</span>
              <strong>{{ formatSpendCost(mySpendSummary.hourly_charge) }}</strong>
            </div>
            <div>
              <span>普通退回</span>
              <strong>{{ formatSpendCost(mySpendSummary.hourly_refund) }}</strong>
            </div>
            <div>
              <span>低消退回</span>
              <strong>{{ formatSpendCost(mySpendSummary.hourly_waiver_refund) }}</strong>
            </div>
            <div>
              <span>实际扣费</span>
              <strong>{{ formatSpendCost(mySpendSummary.hourly_net_cost) }}</strong>
            </div>
          </div>

          <div class="my-spend-breakdown">
            <div class="my-spend-section-head">
              <div>
                <strong>按模型请求费用</strong>
                <small>仅统计账号模式请求消费，小时费在上方单独列出。</small>
              </div>
            </div>
            <div v-if="mySpendSummary.model_breakdown.length === 0" class="my-spend-empty">
              当前范围内暂无请求消费记录。
            </div>
            <div v-else class="my-spend-table-wrap">
              <table class="my-spend-table">
                <thead>
                  <tr>
                    <th>模型</th>
                    <th>请求数</th>
                    <th>Token</th>
                    <th>请求费用</th>
                    <th>均价</th>
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
        <button type="button" class="btn-secondary" @click="closeMySpendDialog">关闭</button>
      </template>
    </BaseDialog>

    <AccountTestModal
      :show="showTestModal"
      :account="testingAccount"
      :account-scope="managedAccountScope"
      :test-endpoint-base="accountTestEndpointBase"
      @close="closeTestModal"
      @test-success="handleManagedTestSuccess"
    />

    <AccountStatsModal
      :show="showStatsModal"
      :account="statsAccount"
      :stats-loader="managedStatsLoader"
      @close="closeStatsModal"
    />

    <ReAuthAccountModal
      :show="showReAuthModal"
      :account="reAuthAccount"
      :proxies="proxies"
      :account-scope="managedAccountScope"
      @close="closeReAuthModal"
      @reauthorized="handleManagedAccountReauthorized"
    />

    <BaseDialog
      :show="showConfigEditDialog"
      title="编辑共享账号配置"
      width="extra-wide"
      @close="closeConfigEditDialog"
    >
      <div class="space-y-5">
        <div v-if="editingConfigListing" class="edit-context-panel">
          <div class="min-w-0">
            <span class="edit-context-eyebrow">账号 #{{ editingConfigListing.account_id }}</span>
            <strong>{{ editingConfigListing.account_name || `共享账号 #${editingConfigListing.account_id}` }}</strong>
            <small>
              使用中席位 {{ editingConfigListing.active_seats }} / {{ editingConfigListing.seat_limit }}
              <template v-if="editingConfigListing.editing_expires_at">
                · 编辑锁 {{ formatCountdownUntil(editingConfigListing.editing_expires_at) }}到期
              </template>
            </small>
          </div>
          <span v-if="editForceActive" class="edit-force-badge">管理员强制编辑</span>
        </div>

        <div v-if="editErrorMessage" class="notice-row border-red-200 bg-red-50 text-red-700 dark:border-red-900/60 dark:bg-red-900/20 dark:text-red-300">
          <Icon name="exclamationCircle" size="sm" class="mt-0.5 flex-shrink-0" />
          <span>{{ editErrorMessage }}</span>
        </div>

        <div class="grid gap-5 xl:grid-cols-[minmax(0,1fr)_320px]">
          <div class="space-y-5">
            <div class="form-section">
              <div class="section-heading">
                <span>基础配置</span>
                <small>这些字段会同步到账号模式调度配置；保存前会保持编辑锁，防止新用户加入旧配置。</small>
              </div>
              <div class="grid gap-3 md:grid-cols-2 xl:grid-cols-4">
                <label class="field">
                  <span>账号名称</span>
                  <input v-model="editForm.name" class="input" :placeholder="ACCOUNT_NAME_BASE_BY_PLATFORM[listingPlatform(editingConfigListing)]" />
                  <small :class="editAccountNameValidationMessage ? 'text-red-600 dark:text-red-300' : ''">
                    {{ editAccountNameValidationMessage || '名称必须唯一，且不能包含空格、换行或制表符。' }}
                  </small>
                </label>

                <div class="field md:col-span-2">
                  <span>代理 IP</span>
                  <ProxySelector
                    v-model="selectedEditProxyId"
                    :proxies="proxies"
                    :disabled="savingConfigEdit || releasingConfigEdit"
                    :allow-empty="false"
                    :can-test="authStore.isAdmin"
                    disable-full
                    hide-endpoint
                  >
                    <template #actions="{ close }">
                      <div class="grid gap-2 sm:grid-cols-2">
                        <button
                          type="button"
                          class="proxy-action-option"
                          @click.stop="openProxyPurchase(close)"
                        >
                          <span class="proxy-action-icon bg-amber-50 text-amber-600 dark:bg-amber-500/10 dark:text-amber-300">
                            <Icon name="externalLink" size="sm" />
                          </span>
                          <span>
                            <strong>购买 seekproxy</strong>
                            <small>打开 seekproxy 新窗口</small>
                          </span>
                        </button>
                        <button
                          type="button"
                          class="proxy-action-option"
                          @click.stop="openAddProxyDialog(close, 'edit')"
                        >
                          <span class="proxy-action-icon bg-primary-50 text-primary-600 dark:bg-primary-500/10 dark:text-primary-300">
                            <Icon name="plus" size="sm" />
                          </span>
                          <span>
                            <strong>添加代理 IP</strong>
                            <small>使用自己的动态或静态代理</small>
                          </span>
                        </button>
                      </div>
                    </template>
                  </ProxySelector>
                  <small :class="editProxyCapacityValidationMessage ? 'text-red-600 dark:text-red-300' : ''">
                    {{ editProxyHelperText }}
                  </small>
                </div>

                <label class="field">
                  <span>可使用人数</span>
                  <select v-model.number="editForm.seat_limit" class="input">
                    <option v-for="seat in seatOptions" :key="seat" :value="seat">{{ seat }} 人</option>
                  </select>
                </label>

                <label class="field">
                  <span>账号并发上限</span>
                  <input v-model.number="editForm.concurrency" class="input" type="number" min="1" :max="MAX_ACCOUNT_CONCURRENCY" step="1" />
                  <small :class="editConcurrencyValidationMessage ? 'text-red-600 dark:text-red-300' : ''">
                    {{ editConcurrencyValidationMessage || `1-${MAX_ACCOUNT_CONCURRENCY}。` }}
                  </small>
                </label>

                <label class="field">
                  <span>单用户最高并发</span>
                  <input v-model.number="editForm.per_user_concurrency" class="input" type="number" min="1" :max="editMaxPerUserConcurrency" step="1" />
                  <small :class="editPerUserConcurrencyValidationMessage ? 'text-red-600 dark:text-red-300' : ''">
                    {{ editPerUserConcurrencyValidationMessage || editPerUserConcurrencyLimitTip }}
                  </small>
                </label>

                <label class="field">
                  <span>账号倍率</span>
                  <input v-model.number="editForm.rate_multiplier" class="input" type="number" min="0" step="0.01" />
                </label>

                <label class="field">
                  <span>每小时扣费额度</span>
                  <input v-model.number="editForm.hourly_rate" class="input" type="number" min="0" step="0.0001" />
                </label>

                <label class="field">
                  <span>满低消免小时费</span>
                  <input v-model.number="editForm.hourly_fee_waiver_minimum" class="input" type="number" min="0" step="0.0001" />
                </label>

                <label class="field">
                  <span>最低余额准入</span>
                  <input v-model.number="editForm.min_balance_required" class="input" type="number" min="0" step="0.01" />
                </label>
              </div>
            </div>

            <div class="form-section">
              <div class="section-heading">
                <span>模型与保护</span>
                <small>模型编辑仍可单独保存；这里保存时会与其他账号模式参数一起提交。</small>
              </div>
              <div class="grid gap-4 xl:grid-cols-[minmax(0,1fr)_280px]">
                <div class="field">
                  <span>模型白名单</span>
                  <div class="model-selector-shell">
                    <ModelWhitelistSelector v-model="editAllowedModels" :platform="listingPlatform(editingConfigListing)" />
                  </div>
                </div>

                <div v-if="listingPlatform(editingConfigListing) === 'openai'" class="grid gap-3 sm:grid-cols-2 xl:grid-cols-1">
                  <label class="field">
                    <span>Codex 5h 保护 %</span>
                    <input v-model.number="editForm.codex_5h_limit_percent" class="input" type="number" min="1" max="100" step="1" />
                  </label>
                  <label class="field">
                    <span>Codex 7d 保护 %</span>
                    <input v-model.number="editForm.codex_7d_limit_percent" class="input" type="number" min="1" max="100" step="1" />
                  </label>
                </div>
                <div v-else-if="listingPlatform(editingConfigListing) === 'anthropic'" class="grid gap-3 sm:grid-cols-2 xl:grid-cols-1">
                  <label class="field">
                    <span>Claude 5h 保护 %</span>
                    <input v-model.number="editForm.anthropic_5h_limit_percent" class="input" type="number" min="1" max="100" step="1" />
                  </label>
                  <label class="field">
                    <span>Claude 7d 保护 %</span>
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
                  <strong>仅允许 Codex 官方客户端</strong>
                  <small>关闭后会允许更多客户端加入该共享账号。</small>
                </span>
              </label>
            </div>
          </div>

          <aside class="edit-summary-panel">
            <span class="text-xs font-semibold text-gray-500 dark:text-dark-300">保存摘要</span>
            <div class="mt-3 grid gap-2">
              <div class="compact-metric">
                <span>代理</span>
                <strong>{{ currentEditProxyLabel }}</strong>
              </div>
              <div class="compact-metric">
                <span>模型</span>
                <strong>{{ editAllowedModels.length }}</strong>
              </div>
              <div class="compact-metric">
                <span>账号并发</span>
                <strong>{{ editForm.concurrency }}</strong>
              </div>
              <div class="compact-metric">
                <span>共享人数</span>
                <strong>{{ editForm.seat_limit }}</strong>
              </div>
              <div class="compact-metric">
                <span>单用户并发</span>
                <strong>{{ editForm.per_user_concurrency }}</strong>
              </div>
              <div class="compact-metric">
                <span>每人上限</span>
                <strong>{{ editMaxPerUserConcurrency }}</strong>
              </div>
              <div class="compact-metric">
                <span>小时费</span>
                <strong>{{ formatNumber(editForm.hourly_rate) }}</strong>
              </div>
              <div class="compact-metric">
                <span>免小时费低消</span>
                <strong>{{ hourlyFeeWaiverLabel(editForm.hourly_fee_waiver_minimum) }}</strong>
              </div>
            </div>
          </aside>
        </div>
      </div>

      <template #footer>
        <button type="button" class="btn-secondary" :disabled="savingConfigEdit || releasingConfigEdit" @click="closeConfigEditDialog">取消</button>
        <button
          type="button"
          class="btn-primary"
          :disabled="savingConfigEdit || releasingConfigEdit || editAllowedModels.length === 0 || Boolean(editConcurrencyValidationMessage) || Boolean(editPerUserConcurrencyValidationMessage)"
          @click="saveConfigEdit"
        >
          <Icon v-if="!savingConfigEdit" name="checkCircle" size="sm" class="mr-2" />
          <svg v-else class="-ml-1 mr-2 h-4 w-4 animate-spin" fill="none" viewBox="0 0 24 24">
            <circle class="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="4"></circle>
            <path class="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"></path>
          </svg>
          保存配置
        </button>
      </template>
    </BaseDialog>

    <BaseDialog
      :show="pendingEndUse !== null"
      title="确认结束使用"
      width="narrow"
      :close-on-escape="endingId === null"
      @close="cancelEndUse"
    >
      <p class="text-sm text-gray-600 dark:text-gray-400">{{ endUseConfirmMessage }}</p>

      <template #footer>
        <div class="flex justify-end space-x-3">
          <button type="button" class="btn btn-secondary" :disabled="endingId !== null" @click="cancelEndUse">
            取消
          </button>
          <button type="button" class="btn btn-danger" :disabled="endingId !== null" @click="confirmEndUse">
            <svg v-if="endingId !== null" class="-ml-1 mr-2 h-4 w-4 animate-spin" fill="none" viewBox="0 0 24 24">
              <circle class="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="4"></circle>
              <path class="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"></path>
            </svg>
            {{ endingId !== null ? '处理中...' : (pendingEndUse?.status === 'queued' ? '移出预约' : '结束使用') }}
          </button>
        </div>
      </template>
    </BaseDialog>

    <BaseDialog
      :show="pendingReview !== null"
      title="为本次使用评分"
      width="wide"
      :z-index="70"
      @close="closeReviewDialog"
    >
      <div v-if="pendingReview" class="space-y-5">
        <div class="rounded-lg border border-gray-200 bg-gray-50 p-4 dark:border-dark-700 dark:bg-dark-800/60">
          <div class="flex flex-col gap-1">
            <span class="text-xs font-semibold uppercase tracking-wide text-gray-500 dark:text-dark-300">{{ platformLabel(listingPlatform(pendingReview.listing)) }}</span>
            <strong class="text-base text-gray-900 dark:text-dark-50">{{ listingDisplayName(pendingReview.listing) }}</strong>
            <span class="text-sm text-gray-500 dark:text-dark-300">号主：{{ ownerDisplayName(pendingReview.listing) }}</span>
          </div>
        </div>

        <div>
          <label class="mb-2 block text-sm font-medium text-gray-700 dark:text-dark-100">评分</label>
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
          <span>留言</span>
          <textarea
            v-model="pendingReview.comment"
            class="input min-h-[120px] resize-y"
            maxlength="1000"
            placeholder="可以留空；填写后会先进入平台审核"
          ></textarea>
          <small>{{ pendingReview.comment.length }}/1000</small>
        </label>

        <div v-if="pendingReview.error" class="rounded-lg border border-red-200 bg-red-50 p-3 text-sm text-red-700 dark:border-red-500/30 dark:bg-red-500/10 dark:text-red-200">
          {{ pendingReview.error }}
        </div>
      </div>

      <template #footer>
        <button type="button" class="btn-secondary" :disabled="pendingReview?.submitting" @click="closeReviewDialog">暂不评分</button>
        <button type="button" class="btn-primary" :disabled="pendingReview?.submitting || pendingReview?.score === null" @click="submitReview">
          <Icon v-if="!pendingReview?.submitting" name="checkCircle" size="sm" class="mr-2" />
          <svg v-else class="-ml-1 mr-2 h-4 w-4 animate-spin" fill="none" viewBox="0 0 24 24">
            <circle class="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="4"></circle>
            <path class="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"></path>
          </svg>
          提交评分
        </button>
      </template>
    </BaseDialog>

    <BaseDialog
      :show="ownerDialog.show"
      :title="ownerDialog.ownerUsername ? `${ownerDialog.ownerUsername} 的账号` : '号主账号'"
      width="extra-wide"
      :z-index="70"
      @close="closeOwnerDialog"
    >
      <div class="space-y-4">
        <div v-if="ownerDialog.error" class="rounded-lg border border-red-200 bg-red-50 p-3 text-sm text-red-700 dark:border-red-500/30 dark:bg-red-500/10 dark:text-red-200">
          {{ ownerDialog.error }}
        </div>

        <div class="flex flex-wrap gap-2">
          <button
            type="button"
            class="btn-secondary h-9"
            :class="ownerDialog.tab === 'listings' && 'border-primary-300 bg-primary-50 text-primary-700 dark:border-primary-500/40 dark:bg-primary-500/10 dark:text-primary-200'"
            @click="ownerDialog.tab = 'listings'"
          >
            <Icon name="grid" size="xs" class="mr-2" />
            账号
          </button>
          <button
            type="button"
            class="btn-secondary h-9"
            :class="ownerDialog.tab === 'reviews' && 'border-primary-300 bg-primary-50 text-primary-700 dark:border-primary-500/40 dark:bg-primary-500/10 dark:text-primary-200'"
            @click="ownerDialog.tab = 'reviews'"
          >
            <Icon name="chat" size="xs" class="mr-2" />
            评论
          </button>
        </div>

        <div v-if="ownerDialog.tab === 'listings'" class="space-y-3">
          <div v-if="ownerDialog.loadingListings" class="rounded-lg border border-gray-200 p-6 text-center text-sm text-gray-500 dark:border-dark-700 dark:text-dark-300">
            正在加载账号...
          </div>
          <div v-else-if="ownerDialog.listings.length === 0" class="rounded-lg border border-gray-200 p-6 text-center text-sm text-gray-500 dark:border-dark-700 dark:text-dark-300">
            暂无可展示账号
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
                <span :class="statusBadgeClass(item.status)">{{ statusLabel(item.status) }}</span>
              </div>
              <div class="mt-3 grid grid-cols-3 gap-2 text-xs text-gray-600 dark:text-dark-300">
                <span>席位 {{ item.active_seats }}/{{ item.seat_limit }}</span>
                <span>倍率 {{ formatNumber(item.rate_multiplier) }}x</span>
                <span>小时费 {{ formatNumber(item.hourly_rate) }}</span>
              </div>
            </button>
          </div>
        </div>

        <div v-else class="space-y-3">
          <div v-if="ownerDialog.loadingReviews" class="rounded-lg border border-gray-200 p-6 text-center text-sm text-gray-500 dark:border-dark-700 dark:text-dark-300">
            正在加载评论...
          </div>
          <div v-else-if="ownerDialog.reviews.length === 0" class="rounded-lg border border-gray-200 p-6 text-center text-sm text-gray-500 dark:border-dark-700 dark:text-dark-300">
            暂无已审核评论
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
                <span>{{ review.account_name || '共享账号' }}</span>
                <span v-if="review.consumer_username">来自 {{ review.consumer_username }}</span>
              </div>
            </article>
          </div>
        </div>
      </div>

      <template #footer>
        <button type="button" class="btn-primary" @click="searchOwnerFromDialog">
          <Icon name="search" size="sm" class="mr-2" />
          在广场搜索该号主
        </button>
        <button type="button" class="btn-secondary" @click="closeOwnerDialog">关闭</button>
      </template>
    </BaseDialog>

    <ConfirmDialog
      :show="pendingForceEditListing !== null"
      title="强制编辑使用中账号"
      :message="forceEditConfirmMessage"
      confirm-text="继续编辑"
      cancel-text="取消"
      danger
      @confirm="confirmForceEdit"
      @cancel="cancelForceEdit"
    />
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, reactive, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { accountShareAPI, type AccountShareListing, type AccountShareListingFeatureTag, type AccountShareListingFilters, type AccountShareListingSortBy, type AccountShareListingSortKey, type AccountShareListingSortOrder, type AccountShareListingStatus, type AccountShareListingTab, type AccountShareMembership, type AccountShareMySpendRange, type AccountShareMySpendSummary, type AccountShareRecommendationCandidate, type AccountShareRecommendationResult, type AccountShareRecommendationScoreBreakdown, type AccountShareRecommendationUsageProfile, type AccountShareReview, type UpdateAccountShareListingRequest } from '@/api/accountShare'
import { accountsAPI, adminAPI, keysAPI } from '@/api'
import type { Account, AccountLevel, AccountUsageStatsResponse, ApiKey, Proxy, ProxyProtocol, UsageProgress } from '@/types'
import { useAppStore } from '@/stores/app'
import { useAuthStore } from '@/stores/auth'
import { useClipboard } from '@/composables/useClipboard'
import { extractApiErrorCode, extractApiErrorMessage } from '@/utils/apiError'
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
import Icon from '@/components/icons/Icon.vue'
import AccountStatsModal from '@/components/account/AccountStatsModal.vue'
import AccountTestModal from '@/components/account/AccountTestModal.vue'
import ModelWhitelistSelector from '@/components/account/ModelWhitelistSelector.vue'
import OAuthAuthorizationFlow from '@/components/account/OAuthAuthorizationFlow.vue'
import ProxySelector from '@/components/common/ProxySelector.vue'
import ReAuthAccountModal from '@/components/account/ReAuthAccountModal.vue'
import UsageProgressBar from '@/components/account/UsageProgressBar.vue'
import Pagination from '@/components/common/Pagination.vue'

interface FilterOption {
  key: string
  label: string
  tab: AccountShareListingTab
}

type ListingStatusFilterValue = AccountShareListingStatus | 'available' | 'all' | ''
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
}

interface OAuthFlowInstance {
  authCode?: string
  oauthState?: string
  reset: () => void
}

interface UserProxyFormState {
  ip_type: 'ipv4' | 'ipv6'
  name: string
  protocol: ProxyProtocol
  host: string
  port: number | null
  username: string
  password: string
}

type ManagedAccountModalAction = 'test' | 'stats' | 'reauth'
type ProxyTargetForm = 'create' | 'edit'
type AccountShareActionErrorAction = 'create-mode-key' | null
type AccountSharePlatform = 'openai' | 'anthropic'
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
}

interface RecommendationScoreItem {
  key: 'cost' | 'stable' | 'available' | 'risk'
  label: string
  value: number
}

interface PendingJoinConfirmation {
  listing: AccountShareListing
  apiKeyID: number
  idleTimeoutMinutes: number
}

interface PendingEndUseState {
  membershipID: number
  apiKeyID?: number
  apiKeyName?: string
  status?: string
  listing: AccountShareListing
}

interface QueueSnapshotLoadResult {
  snapshots: Record<number, AccountShareMembership[]>
  failedApiKeyIDs: number[]
}

interface ReviewDialogState {
  membershipID: number
  listing: AccountShareListing
  score: number | null
  comment: string
  submitting: boolean
  error: string
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
  listing: AccountShareListing
  source: MySpendAccountOptionSource
  membershipID: number
}

type OwnerDialogTab = 'listings' | 'reviews'

const DEFAULT_ACCOUNT_CONCURRENCY = 20
const DEFAULT_PER_USER_CONCURRENCY = 5
const DEFAULT_HOURLY_RATE = 0.2
const DEFAULT_ACCOUNT_SHARE_IDLE_TIMEOUT_MINUTES = 10
const PLUS_EXPENSIVE_RATE_MULTIPLIER = 0.15
const PRO_EXPENSIVE_RATE_MULTIPLIER = 0.25
const EXPENSIVE_HOURLY_RATE = 2
const OWNER_SELF_USE_RATE_MULTIPLIER = 0.005
const MAX_ACCOUNT_CONCURRENCY = 50
const ACCOUNT_SHARE_MIN_SEATS = 2
const ACCOUNT_SHARE_MAX_SEATS = 12
const PROXY_PURCHASE_URL = 'https://www.seekproxy.com/user/reg?invite_id=105978'
const ACCOUNT_SHARE_PLATFORM_OPTIONS: Array<{ value: AccountSharePlatform; label: string }> = [
  { value: 'openai', label: 'OpenAI' },
  { value: 'anthropic', label: 'Anthropic' }
]
const ACCOUNT_NAME_BASE_BY_PLATFORM: Record<AccountSharePlatform, string> = {
  openai: 'OpenAI共享账号',
  anthropic: 'Anthropic共享账号'
}
const ACCOUNT_MODE_GROUP_NAME_BY_PLATFORM: Record<AccountSharePlatform, string> = {
  openai: 'OpenAI账号模式',
  anthropic: 'Anthropic账号模式'
}
const DEFAULT_ACCOUNT_SHARE_ALLOWED_MODELS_BY_PLATFORM: Record<AccountSharePlatform, string[]> = {
  openai: ['gpt-5.5', 'gpt-5.4', 'gpt-5.4-mini', 'codex-auto-review'],
  anthropic: ['claude-sonnet-4-6', 'claude-opus-4-8', 'claude-opus-4-7', 'claude-fable-5', 'claude-opus-4-6', 'claude-haiku-4-5']
}
const ACCOUNT_SHARE_RECOMMENDATION_LIMIT = 10
const ACCOUNT_SHARE_RECOMMENDATION_PAGE_SIZE = 5
const recommendationPresets: RecommendationPreset[] = [
  {
    key: 'light',
    label: '轻量',
    request_count: 100,
    active_hours: 1,
    input_tokens_per_request: 1000,
    output_tokens_per_request: 400,
    cache_creation_tokens_per_request: 0,
    cache_read_tokens_per_request: 0
  },
  {
    key: 'balanced',
    label: '均衡',
    request_count: 500,
    active_hours: 2,
    input_tokens_per_request: 3000,
    output_tokens_per_request: 1000,
    cache_creation_tokens_per_request: 0,
    cache_read_tokens_per_request: 500
  },
  {
    key: 'heavy',
    label: '重度',
    request_count: 3000,
    active_hours: 8,
    input_tokens_per_request: 8000,
    output_tokens_per_request: 2500,
    cache_creation_tokens_per_request: 500,
    cache_read_tokens_per_request: 3000
  }
]
const ACCOUNT_SHARE_PAGE_SIZE = 10
const ACCOUNT_SHARE_MODE_KEY_PAGE_SIZE = 100
const ACCOUNT_SHARE_LISTING_PREFERENCES_STORAGE_KEY = 'account-share-listing-preferences'
const MODEL_PREVIEW_LIMIT = 5
const ACCOUNT_SHARE_IDLE_TIMEOUT_MAX_MINUTES = 10080
const ACCOUNT_SHARE_STATUS_REFRESH_THROTTLE_MS = 15_000
const ACCOUNT_SHARE_QUEUE_WARNING_THROTTLE_MS = 30_000
const MY_SPEND_RANGE_OPTIONS: MySpendRangeOption[] = [
  { value: 'current_membership', label: '本次使用' },
  { value: 'today', label: '今天' },
  { value: '7d', label: '近7天' }
]

const appStore = useAppStore()
const authStore = useAuthStore()
const route = useRoute()
const router = useRouter()
const { copyToClipboard } = useClipboard()
const seatOptions = Array.from({ length: ACCOUNT_SHARE_MAX_SEATS - ACCOUNT_SHARE_MIN_SEATS + 1 }, (_, index) => index + ACCOUNT_SHARE_MIN_SEATS)
const reviewScoreOptions = Array.from({ length: 11 }, (_, score) => score)
const filters: FilterOption[] = [
  { key: 'using', label: '使用/预约', tab: 'using' },
  { key: 'history', label: '历史使用', tab: 'history' },
  { key: 'all', label: '全部', tab: 'all' }
]
const ownerFilter: FilterOption = { key: 'mine', label: '我的账号', tab: 'mine' }
const listingSortFieldOptions: ListingSortFieldOption[] = [
  { sortBy: 'account_concurrency', label: '账号并发', ascLabel: '从小到大', descLabel: '从大到小' },
  { sortBy: 'per_user_concurrency', label: '单人并发', ascLabel: '从小到大', descLabel: '从大到小' },
  { sortBy: 'min_balance_required', label: '最低余额', ascLabel: '从小到大', descLabel: '从大到小' },
  { sortBy: 'hourly_rate', label: '小时费', ascLabel: '从小到大', descLabel: '从大到小' },
  { sortBy: 'hourly_fee_waiver', label: '免小时低消', ascLabel: '从小到大', descLabel: '从大到小' },
  { sortBy: 'rate_multiplier', label: '倍率', ascLabel: '从小到大', descLabel: '从大到小' },
  { sortBy: 'remaining_seats', label: '剩余席位', ascLabel: '从少到多', descLabel: '从多到少' },
  { sortBy: 'rating', label: '评分', ascLabel: '从低到高', descLabel: '从高到低' },
  { sortBy: 'updated_at', label: '更新时间', ascLabel: '最早优先', descLabel: '最近优先' }
]
const listingSortOptions: ListingSortOption[] = [
  ...listingSortFieldOptions.flatMap(field => [
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
]
const listingFeatureTagOptions: ListingFeatureTagOption[] = [
  { value: 'hourly_fee_waiver', label: '满低消免小时费' },
  { value: 'image_generation', label: '支持生图' },
  { value: 'codex_cli_only', label: '仅客户端' },
  { value: 'non_codex_cli_only', label: '非仅客户端' },
  { value: 'no_hourly_fee', label: '无小时费' }
]
const listingStatusFilterOptions: Array<{ value: ListingStatusFilterValue; label: string }> = [
  { value: '', label: '默认状态' },
  { value: 'available', label: '可用账号' },
  { value: 'active', label: '已上架' },
  { value: 'paused', label: '已暂停' },
  { value: 'disabled', label: '已下架' },
  { value: 'all', label: '全部状态' }
]
const openAIAccountLevelConfigs = computed(() =>
  normalizeOpenAIAccountLevelConfigs(appStore.cachedPublicSettings?.openai_account_levels)
)
const accountLevelFilterOptions = computed<Array<{ value: AccountLevelFilterValue; label: string }>>(() =>
  openAIAccountLevelOptions(openAIAccountLevelConfigs.value, {
    includeEmpty: true,
    emptyLabel: '全部等级',
    includeUnknown: true,
    unknownLabel: 'UNKNOWN'
  }).map(option => ({
    value: (option.value === '' ? 'all' : option.value) as AccountLevelFilterValue,
    label: option.label
  }))
)
const accountShareJoinErrorMessages: Record<string, string> = {
  ACCOUNT_SHARE_ACCOUNT_UNAVAILABLE: '该共享账号当前不可加入，请换一个账号或稍后再试',
  ACCOUNT_SHARE_ALREADY_USING: '你当前已有正在使用的共享账号，请先结束后再加入新的账号',
  ACCOUNT_SHARE_API_KEY_ALREADY_BOUND: '当前账号模式 Key 已绑定其他共享账号，请先结束原使用记录',
  ACCOUNT_SHARE_QUEUE_FULL: '当前账号模式 Key 的预约列表已满，最多只能保留 5 个账号',
  ACCOUNT_SHARE_QUEUE_INVALID: '预约列表顺序无效，请刷新后重试',
  ACCOUNT_SHARE_API_KEY_MUST_USE_MODE_GROUP: '请选择绑定对应平台账号模式分组的 API Key',
  ACCOUNT_SHARE_LISTING_NOT_FOUND: '该共享账号不存在或已下架，请刷新账号广场后再试',
  ACCOUNT_SHARE_LISTING_NOT_ACTIVE: '该共享账号当前未上架，暂时不能加入',
  ACCOUNT_SHARE_LISTING_FULL: '该共享账号席位已满，请换一个账号',
  ACCOUNT_SHARE_BALANCE_BELOW_MINIMUM: '余额低于该账号最低要求，暂时不能加入',
  ACCOUNT_SHARE_MODE_GROUP_UNAVAILABLE: '账号模式分组尚未配置，请联系管理员处理',
  ACCOUNT_SHARE_MODE_GROUP_UNBOUND: '当前账号模式分组未绑定共享账号，请先在账号广场加入一个账号',
  ACCOUNT_SHARE_MODE_INVALID_IDLE_TIMEOUT: '空闲自动退出时间必须在 1-10080 分钟之间',
  ACCOUNT_SHARE_MODE_PREPAY_INSUFFICIENT: '余额不足以预付本次使用，请充值后再试',
  ACCOUNT_SHARE_PER_USER_CONCURRENCY_EXCEEDED: '该共享账号当前单用户并发已达到上限，请稍后再试',
  ACCOUNT_SHARE_OWNER_CANNOT_JOIN: '不能加入自己上架的共享账号',
  ACCOUNT_SHARE_LISTING_EDITING: '账号配置正在编辑中，暂时不能加入使用',
  API_KEY_NOT_FOUND: '该 API Key 不存在或已被删除，请重新选择',
  INSUFFICIENT_PERMISSIONS: '你没有权限使用这个 API Key，请重新选择自己的账号模式 Key',
  SERVICE_UNAVAILABLE: '账号广场服务暂时不可用，请稍后再试',
  USER_NOT_FOUND: '当前用户状态异常，请重新登录后再试'
}
const accountShareRecommendationErrorMessages: Record<string, string> = {
  ACCOUNT_SHARE_RECOMMENDATION_INVALID: '测算参数无效，请检查模型、请求次数、使用时长和 token 输入',
  ACCOUNT_SHARE_API_KEY_MUST_USE_MODE_GROUP: '请选择绑定对应平台账号模式分组的 API Key',
  API_KEY_NOT_FOUND: '该 API Key 不存在或已被删除，请重新选择',
  SERVICE_UNAVAILABLE: '账号推荐服务暂时不可用，请稍后再试',
  USER_NOT_FOUND: '当前用户状态异常，请重新登录后再试'
}
const accountShareEndErrorMessages: Record<string, string> = {
  ...accountShareJoinErrorMessages,
  ACCOUNT_SHARE_LISTING_NOT_FOUND: '这次使用或预约状态已变化，请刷新账号广场后确认'
}

function getListingPreferencesStorageKey(): string {
  const userID = Number(authStore.user?.id || 0)
  return userID > 0
    ? `${ACCOUNT_SHARE_LISTING_PREFERENCES_STORAGE_KEY}:user:${userID}`
    : ACCOUNT_SHARE_LISTING_PREFERENCES_STORAGE_KEY
}

function defaultListingPreferences(): ListingPreferenceState {
  return {
    platform: 'openai',
    tab: 'all',
    search: '',
    pageSize: ACCOUNT_SHARE_PAGE_SIZE,
    status: '',
    accountLevel: 'all',
    sortKeys: [],
    seatLimits: [],
    featureTags: [],
    models: []
  }
}

function filterForListingTab(tab: AccountShareListingTab): FilterOption {
  return [ownerFilter, ...filters].find(option => option.tab === tab) || filters[2]
}

function normalizeListingPlatform(value: unknown): AccountSharePlatform {
  return value === 'anthropic' ? 'anthropic' : 'openai'
}

function normalizeListingTab(value: unknown): AccountShareListingTab {
  if (typeof value !== 'string') return defaultListingPreferences().tab
  return filterForListingTab(value as AccountShareListingTab).tab
}

function normalizeListingStatus(value: unknown): ListingStatusFilterValue {
  return listingStatusFilterOptions.some(option => option.value === value)
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
    const option = listingSortOptions.find(candidate => candidate.key === item)
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
  const validTags = new Set(listingFeatureTagOptions.map(option => option.value))
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
  try {
    const raw = window.localStorage.getItem(storageKey)
    if (!raw) return defaultListingPreferences()
    return normalizeListingPreferences(JSON.parse(raw))
  } catch (error) {
    window.localStorage.removeItem(storageKey)
    console.warn('Failed to read account share listing preferences:', error)
    return defaultListingPreferences()
  }
}

function buildCurrentListingPreferences(): ListingPreferenceState {
  return normalizeListingPreferences({
    platform: activeListingPlatform.value,
    tab: activeFilter.value.tab,
    search: searchQuery.value,
    pageSize: pagination.page_size,
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
  const preset = recommendationPresets[1]
  return {
    api_key_id: 0,
    model: DEFAULT_ACCOUNT_SHARE_ALLOWED_MODELS_BY_PLATFORM.openai[0],
    request_count: preset.request_count,
    active_hours: preset.active_hours,
    input_tokens_per_request: preset.input_tokens_per_request,
    output_tokens_per_request: preset.output_tokens_per_request,
    cache_creation_tokens_per_request: preset.cache_creation_tokens_per_request,
    cache_read_tokens_per_request: preset.cache_read_tokens_per_request
  }
}

const initialListingPreferences = readListingPreferences()
const activeFilter = ref<FilterOption>(filterForListingTab(initialListingPreferences.tab))
const activeListingPlatform = ref<AccountSharePlatform>(initialListingPreferences.platform)
const listings = ref<AccountShareListing[]>([])
const selectedRecommendationPreset = ref<RecommendationPresetKey>('balanced')
const recommendationForm = reactive<RecommendationFormState>(buildDefaultRecommendationForm())
const recommendationLoading = ref(false)
const recommendationUsageProfileLoading = ref(false)
const recommendationUsageProfileMessage = ref('')
const recommendationError = ref('')
const recommendationResult = ref<AccountShareRecommendationResult | null>(null)
const recommendationPage = ref(1)
const showUsageGuideDialog = ref(false)
const showRecommendationDialog = ref(false)
const queueMembershipsByApiKey = ref<Record<number, AccountShareMembership[]>>({})
const keyResolutionMemberships = ref<AccountShareMembership[]>([])
const keyResolutionListings = ref<AccountShareListing[]>([])
const keyResolutionLoading = ref(false)
const keyResolutionLoaded = ref(false)
const keyResolutionError = ref('')
const pagination = reactive({
  page: 1,
  page_size: initialListingPreferences.pageSize,
  total: 0,
  pages: 1
})
const loading = ref(false)
const errorMessage = ref('')
const actionErrorDialog = reactive<{
  show: boolean
  title: string
  message: string
  action: AccountShareActionErrorAction
}>({
  show: false,
  title: '操作失败',
  message: '',
  action: null
})
const createErrorMessage = ref('')
const showCreate = ref(false)
const createPlatform = ref<AccountSharePlatform>('openai')
const authURL = ref('')
const authSessionID = ref('')
const creating = ref(false)
const generatingOAuthURL = ref(false)
const joiningId = ref<number | null>(null)
const pendingJoinConfirmation = ref<PendingJoinConfirmation | null>(null)
const endingId = ref<number | null>(null)
const pendingEndUse = ref<PendingEndUseState | null>(null)
const pendingReview = ref<ReviewDialogState | null>(null)
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
  error: ''
})
const showMySpendDialog = ref(false)
const mySpendListing = ref<AccountShareListing | null>(null)
const mySpendSelectedMembershipID = ref(0)
const mySpendSelectedOptionKey = ref('')
const mySpendAccountOptions = ref<MySpendAccountOption[]>([])
const mySpendAccountsLoading = ref(false)
const mySpendAccountsError = ref('')
const mySpendRange = ref<AccountShareMySpendRange>('current_membership')
const mySpendSummary = ref<AccountShareMySpendSummary | null>(null)
const mySpendLoading = ref(false)
const mySpendError = ref('')
const reorderingQueueId = ref<number | null>(null)
const pendingForceEditListing = ref<AccountShareListing | null>(null)
const managingId = ref<number | null>(null)
const managedActionId = ref<number | null>(null)
const showTestModal = ref(false)
const showStatsModal = ref(false)
const showReAuthModal = ref(false)
const showConfigEditDialog = ref(false)
const showModelEditDialog = ref(false)
const testingAccount = ref<Account | null>(null)
const statsAccount = ref<Account | null>(null)
const reAuthAccount = ref<Account | null>(null)
const editingConfigListing = ref<AccountShareListing | null>(null)
const editingModelListing = ref<AccountShareListing | null>(null)
const editingAllowedModels = ref<string[]>([])
const editAllowedModels = ref<string[]>([])
const editSessionID = ref('')
const editForceActive = ref(false)
const editErrorMessage = ref('')
const savingConfigEdit = ref(false)
const releasingConfigEdit = ref(false)
const savingModelsId = ref<number | null>(null)
const selectedKeyByListing = reactive<Record<number, number>>({})
const idleTimeoutByListing = reactive<Record<number, number>>({})
const savingIdleTimeoutId = ref<number | null>(null)
const modeGroupIDsByPlatform = reactive<Record<AccountSharePlatform, number>>({
  openai: 0,
  anthropic: 0
})
const modeApiKeysByPlatform = reactive<Record<AccountSharePlatform, ApiKey[]>>({
  openai: [],
  anthropic: []
})
const modeKeysLoadingByPlatform = reactive<Record<AccountSharePlatform, boolean>>({
  openai: false,
  anthropic: false
})
const modeKeysLoadedByPlatform = reactive<Record<AccountSharePlatform, boolean>>({
  openai: false,
  anthropic: false
})
const modeKeysErrorByPlatform = reactive<Record<AccountSharePlatform, string>>({
  openai: '',
  anthropic: ''
})
const unavailableQueueSnapshotApiKeyIDs = ref<Set<number>>(new Set())
const visibleQueueSnapshotWarning = ref('')
const proxies = ref<Proxy[]>([])
const knownListings = ref<AccountShareListing[]>([])
const proxyLoading = ref(false)
const proxyLoadMessage = ref('')
const searchQuery = ref(initialListingPreferences.search)
const modelFilterInput = ref('')
const filterPanelRef = ref<HTMLElement | null>(null)
const openFilterPopover = ref<ListingFilterPopover | null>(null)
const oauthFlowRef = ref<OAuthFlowInstance | null>(null)
const showProxyDialog = ref(false)
const savingProxy = ref(false)
const proxyDialogError = ref('')
const proxySmartInput = ref('')
const nowMs = ref(Date.now())
const proxyTargetForm = ref<ProxyTargetForm>('create')
let clockTimer: number | null = null
let searchDebounceTimer: number | null = null
let editSessionRenewTimer: number | null = null
let suppressNextSearchRefresh = false
let listingsRequestController: AbortController | null = null
let listingsRequestSeq = 0
let mySpendAccountsRequestController: AbortController | null = null
let mySpendAccountsRequestSeq = 0
let mySpendRequestController: AbortController | null = null
let mySpendRequestSeq = 0
let modeKeysRequestSeq = 0
let keyResolutionRequestSeq = 0
let lastMembershipStatusRefreshAt = 0
let lastQueueSnapshotWarningAt = 0
let membershipStatusRefreshTimer: number | null = null

const listingFilters = reactive<ListingFilterState>({
  status: initialListingPreferences.status,
  accountLevel: initialListingPreferences.accountLevel,
  sortKeys: [...initialListingPreferences.sortKeys],
  seatLimits: [...initialListingPreferences.seatLimits],
  featureTags: [...initialListingPreferences.featureTags],
  models: [...initialListingPreferences.models]
})

const proxyForm = reactive<UserProxyFormState>({
  ip_type: 'ipv4',
  name: '',
  protocol: 'socks5',
  host: '',
  port: null,
  username: '',
  password: ''
})

function buildDefaultCreateForm(): CreateFormState {
  return {
    name: suggestedAccountName(createPlatform.value),
    proxy_id: null,
    concurrency: DEFAULT_ACCOUNT_CONCURRENCY,
    seat_limit: 2,
    rate_multiplier: 1,
    per_user_concurrency: DEFAULT_PER_USER_CONCURRENCY,
    hourly_rate: DEFAULT_HOURLY_RATE,
    hourly_fee_waiver_minimum: 0,
    min_balance_required: 1,
    codex_cli_only: true,
    codex_5h_limit_percent: 100,
    codex_7d_limit_percent: 100,
    anthropic_5h_limit_percent: 100,
    anthropic_7d_limit_percent: 100
  }
}

const createForm = reactive<CreateFormState>(buildDefaultCreateForm())
const editForm = reactive<CreateFormState>(buildDefaultCreateForm())
const allowedModels = ref<string[]>(defaultAllowedModelsForPlatform(createPlatform.value))

const isOpenAIListingPlatform = computed(() => activeListingPlatform.value === 'openai')
const visibleListingFeatureTagOptions = computed(() =>
  listingFeatureTagOptions.filter(option =>
    isOpenAIListingPlatform.value || (
      option.value !== 'image_generation' &&
      option.value !== 'codex_cli_only' &&
      option.value !== 'non_codex_cli_only'
    )
  )
)

function defaultAllowedModelsForPlatform(platform: AccountSharePlatform): string[] {
  return [...DEFAULT_ACCOUNT_SHARE_ALLOWED_MODELS_BY_PLATFORM[platform]]
}

function listingPlatform(listing: AccountShareListing | null | undefined): AccountSharePlatform {
  return listing?.platform === 'anthropic' ? 'anthropic' : 'openai'
}

function isOpenAIListing(listing: AccountShareListing | null | undefined): boolean {
  return listingPlatform(listing) === 'openai'
}

function isAnthropicListing(listing: AccountShareListing | null | undefined): boolean {
  return listingPlatform(listing) === 'anthropic'
}

function showOpenAIUsageWindows(listing: AccountShareListing | null | undefined): boolean {
  return isOpenAIListing(listing)
}

function showAnthropicUsageWindows(listing: AccountShareListing | null | undefined): boolean {
  return isAnthropicListing(listing)
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

function listingHealthFootVisible(listing: AccountShareListing): boolean {
  return Boolean(
    listing.rate_limit_reset_at ||
    (showOpenAIUsageWindows(listing) && (listing.codex_usage_updated_at || listing.codex_quota_protection_reset_at)) ||
    (showAnthropicUsageWindows(listing) && (listing.anthropic_usage_updated_at || listing.anthropic_quota_protection_reset_at))
  )
}

function platformLabel(platform: AccountSharePlatform): string {
  return ACCOUNT_SHARE_PLATFORM_OPTIONS.find(item => item.value === platform)?.label || platform
}

function accountModeGroupName(platform: AccountSharePlatform): string {
  return ACCOUNT_MODE_GROUP_NAME_BY_PLATFORM[platform]
}

function isUsableModeApiKey(key: ApiKey, accountModeGroupID: number): boolean {
  if (Number(key.group_id || 0) !== accountModeGroupID || key.status !== 'active') return false

  if (key.expires_at) {
    const expiresAtMs = Date.parse(key.expires_at)
    if (!Number.isFinite(expiresAtMs) || expiresAtMs <= Date.now()) return false
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
  return modeApiKeysByPlatform[platform] || []
}

function modeApiKeysForListing(listing: AccountShareListing): ApiKey[] {
  return modeApiKeysForPlatform(listingPlatform(listing))
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
  ACCOUNT_SHARE_PLATFORM_OPTIONS.some(option => modeKeysLoadingForPlatform(option.value))
)

function modeApiKeyPlaceholderForListing(listing: AccountShareListing): string {
  const platform = listingPlatform(listing)
  if (modeKeysLoadingForPlatform(platform)) return '正在加载账号模式 API Key'
  if (!modeKeysLoadedForPlatform(platform)) return '账号模式 API Key 未加载'
  return `选择${accountModeGroupName(listingPlatform(listing))} Key`
}
const pendingJoinListing = computed(() => pendingJoinConfirmation.value?.listing ?? null)
const pendingJoinIsOwnerSelfUse = computed(() => {
  const listing = pendingJoinListing.value
  return listing ? isOwnListing(listing) : false
})
const pendingJoinApiKeyLabel = computed(() => {
  const apiKeyID = pendingJoinConfirmation.value?.apiKeyID
  if (!apiKeyID) return '-'
  const listing = pendingJoinConfirmation.value?.listing
  const key = listing ? modeApiKeysForListing(listing).find(item => item.id === apiKeyID) : undefined
  return key ? modeKeyLabel(key) : `Key #${apiKeyID}`
})
const pendingJoinIdleTimeoutLabel = computed(() => formatIdleTimeoutSetting(pendingJoinConfirmation.value?.idleTimeoutMinutes ?? 0))
const pendingJoinPriceWarnings = computed(() => {
  const listing = pendingJoinListing.value
  if (!listing) return []
  if (pendingJoinIsOwnerSelfUse.value) return []
  const warnings: string[] = []
  if (isRateMultiplierExpensive(listing)) {
    const accountLabel = isOpenAIListing(listing) ? accountLevelBadgeLabel(listing) : platformLabel(listingPlatform(listing))
    warnings.push(`${accountLabel} 账号倍率 ${formatNumber(listing.rate_multiplier)}x 偏高，后续请求消耗会明显增加。`)
  }
  if (isHourlyRateExpensive(listing)) {
    warnings.push(`小时费 ${formatNumber(listing.hourly_rate)} 偏高，空闲或长时间使用时费用压力较大。`)
  }
  return warnings
})
const endUseConfirmMessage = computed(() => {
  const apiKeyLabel = pendingEndUse.value ? formatApiKeyDisplayName(pendingEndUse.value.apiKeyName, pendingEndUse.value.apiKeyID, '当前 Key') : '当前 Key'
  if (pendingEndUse.value?.status === 'queued') {
    return `确认将该账号从${apiKeyLabel}的预约列表中移出？`
  }
  return `结束后${apiKeyLabel}会立即失去账号模式绑定，后续请求会显示“分组未绑定账号”。确认结束使用？`
})

const selectedProxyId = computed<number | null>({
  get: () => createForm.proxy_id && createForm.proxy_id > 0 ? createForm.proxy_id : null,
  set: value => {
    createForm.proxy_id = value
  }
})

const selectedEditProxyId = computed<number | null>({
  get: () => editForm.proxy_id && editForm.proxy_id > 0 ? editForm.proxy_id : null,
  set: value => {
    editForm.proxy_id = value
  }
})

const currentProxyID = computed(() => {
  const proxyID = Number(createForm.proxy_id || 0)
  return Number.isFinite(proxyID) && proxyID > 0 ? proxyID : 0
})

const currentEditProxyID = computed(() => {
  const proxyID = Number(editForm.proxy_id || 0)
  return Number.isFinite(proxyID) && proxyID > 0 ? proxyID : 0
})

const currentProxyLabel = computed(() => {
  const proxyID = currentProxyID.value
  if (proxyID <= 0) return '未选择'
  const proxy = proxies.value.find(item => item.id === proxyID)
  return proxy ? `${proxy.name} #${proxy.id}` : `#${proxyID}`
})

const currentEditProxyLabel = computed(() => {
  const proxyID = currentEditProxyID.value
  if (proxyID <= 0) return '未选择'
  const proxy = proxies.value.find(item => item.id === proxyID)
  return proxy ? `${proxy.name} #${proxy.id}` : `#${proxyID}`
})

const selectedCreateProxy = computed(() => findProxyByID(currentProxyID.value))
const selectedEditProxy = computed(() => findProxyByID(currentEditProxyID.value))
const originalEditProxyID = computed(() => {
  const listing = editingConfigListing.value
  return listing ? normalizeEditableProxyID(listing) : null
})

const createProxyCapacityValidationMessage = computed(() =>
  proxyCapacityValidationMessage(selectedCreateProxy.value)
)
const editProxyCapacityValidationMessage = computed(() => {
  if (currentEditProxyID.value > 0 && currentEditProxyID.value === originalEditProxyID.value) {
    return ''
  }
  return proxyCapacityValidationMessage(selectedEditProxy.value)
})

const parsedAllowedModelCount = computed(() => allowedModels.value.length)
const availableSeatCount = computed(() => listings.value.reduce((total, listing) => total + Math.max(0, listing.seat_limit - listing.active_seats), 0))
const activeSeatCount = computed(() => listings.value.reduce((total, listing) => total + Math.max(0, Number(listing.active_seats || 0)), 0))
const isManagementView = computed(() => activeFilter.value.key === ownerFilter.key)
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
const activeResultFilterCount = computed(() => activeAdvancedFilterCount.value + (searchQuery.value.trim() !== '' ? 1 : 0))
const hasResultFilters = computed(() => hasAdvancedFilters.value || searchQuery.value.trim() !== '')
const managedAccountScope = computed<'admin' | 'user'>(() => authStore.isAdmin ? 'admin' : 'user')
const accountTestEndpointBase = computed(() =>
  managedAccountScope.value === 'admin' ? '/api/v1/admin/accounts' : '/api/v1/accounts'
)
const managedStatsLoader = computed<(id: number, days?: number) => Promise<AccountUsageStatsResponse>>(() =>
  managedAccountScope.value === 'admin' ? adminAPI.accounts.getStats : accountsAPI.getStats
)
const maxPerUserConcurrency = computed(() => calculateMaxPerUserConcurrency(createForm.concurrency, createForm.seat_limit))
const editMaxPerUserConcurrency = computed(() => calculateMaxPerUserConcurrency(editForm.concurrency, editForm.seat_limit))
const accountNameValidationMessage = computed(() => validateAccountName(createForm.name))
const editAccountNameValidationMessage = computed(() => validateAccountName(editForm.name, editingConfigListing.value?.account_id))
const concurrencyValidationMessage = computed(() => {
  const concurrency = Number(createForm.concurrency)
  if (!Number.isFinite(concurrency) || concurrency < 1) return '账号并发上限必须大于 0'
  if (!Number.isInteger(concurrency)) return '账号并发上限必须是整数'
  if (concurrency > MAX_ACCOUNT_CONCURRENCY) return `账号并发上限不能超过 ${MAX_ACCOUNT_CONCURRENCY}`
  return ''
})
const editConcurrencyValidationMessage = computed(() => {
  const concurrency = Number(editForm.concurrency)
  if (!Number.isFinite(concurrency) || concurrency < 1) return '账号并发上限必须大于 0'
  if (!Number.isInteger(concurrency)) return '账号并发上限必须是整数'
  if (concurrency > MAX_ACCOUNT_CONCURRENCY) return `账号并发上限不能超过 ${MAX_ACCOUNT_CONCURRENCY}`
  return ''
})
const perUserConcurrencyValidationMessage = computed(() =>
  validatePerUserConcurrencyValue(createForm.per_user_concurrency, createForm.concurrency, createForm.seat_limit, maxPerUserConcurrency.value)
)
const editPerUserConcurrencyValidationMessage = computed(() =>
  validatePerUserConcurrencyValue(editForm.per_user_concurrency, editForm.concurrency, editForm.seat_limit, editMaxPerUserConcurrency.value)
)
const perUserConcurrencyLimitTip = computed(() =>
  buildPerUserConcurrencyLimitTip(createForm.concurrency, createForm.seat_limit, maxPerUserConcurrency.value)
)
const editPerUserConcurrencyLimitTip = computed(() =>
  buildPerUserConcurrencyLimitTip(editForm.concurrency, editForm.seat_limit, editMaxPerUserConcurrency.value)
)
const concurrencyNotice = computed(() => {
  if (concurrencyValidationMessage.value || perUserConcurrencyValidationMessage.value) return ''
  return perUserConcurrencyLimitTip.value
})
const editConcurrencyNotice = computed(() => {
  if (editConcurrencyValidationMessage.value || editPerUserConcurrencyValidationMessage.value) return ''
  return editPerUserConcurrencyLimitTip.value
})
const canSubmitOAuth = computed(() =>
  authSessionID.value &&
  currentProxyID.value > 0 &&
  parsedAllowedModelCount.value > 0 &&
  !accountNameValidationMessage.value &&
  !concurrencyValidationMessage.value &&
  !perUserConcurrencyValidationMessage.value
)

const proxyHelperText = computed(() => {
  if (proxyLoading.value) return '正在加载代理列表...'
  if (proxyLoadMessage.value) return proxyLoadMessage.value
  if (proxies.value.length > 0) {
    return authStore.isAdmin
      ? '可选择平台代理或我的代理，支持名称/IP 模糊搜索并测试连通性。'
      : '可选择平台代理或我的代理，支持名称/IP 模糊搜索；如需测试连通性，请联系管理员。'
  }
  return '暂无可选代理，可在下拉菜单中购买独立 IP 或添加自己的代理 IP。'
})
const createProxyHelperText = computed(() => createProxyCapacityValidationMessage.value || proxyHelperText.value)
const editProxyHelperText = computed(() => editProxyCapacityValidationMessage.value || proxyHelperText.value)
const forceEditConfirmMessage = computed(() => {
  const listing = pendingForceEditListing.value
  if (!listing) return ''
  return `当前账号已有 ${listing.active_seats}/${listing.seat_limit} 个席位正在使用。强制编辑可能导致正在使用的用户短时间内看到旧配置，请确认已理解风险后再继续。`
})

const isKeyResolutionMode = computed(() => routeQueryString(route.query.mode) === 'resolve-key-binding')
const keyResolutionApiKeyID = computed(() => {
  const value = Number(routeQueryString(route.query.api_key_id))
  return Number.isSafeInteger(value) && value > 0 ? value : 0
})
const keyResolutionApiKeyName = computed(() => routeQueryString(route.query.api_key_name).trim())
const keyResolutionKeyLabel = computed(() => keyResolutionApiKeyName.value || (keyResolutionApiKeyID.value > 0 ? `API Key #${keyResolutionApiKeyID.value}` : '指定 API Key'))
const keyResolutionActiveCount = computed(() => keyResolutionMemberships.value.filter(item => item.status === 'active').length)
const keyResolutionQueuedCount = computed(() => keyResolutionMemberships.value.filter(item => item.status === 'queued').length)
const keyResolutionConflictCount = computed(() => keyResolutionActiveCount.value + keyResolutionQueuedCount.value)
const keyResolutionAllClear = computed(() =>
  keyResolutionLoaded.value && !keyResolutionLoading.value && !keyResolutionError.value && keyResolutionConflictCount.value === 0
)
const keyResolutionListingIDs = computed(() => new Set(keyResolutionMemberships.value.map(item => Number(item.listing_id))))
const keyResolutionPanelToneClass = computed(() => ({
  'key-resolution-panel-loading': keyResolutionLoading.value,
  'key-resolution-panel-error': Boolean(keyResolutionError.value),
  'key-resolution-panel-clear': keyResolutionAllClear.value
}))
const keyResolutionStatusMessage = computed(() => {
  if (keyResolutionLoading.value) return `正在核对 ${keyResolutionKeyLabel.value} 的使用与预约记录，请稍候。`
  if (keyResolutionError.value) return keyResolutionError.value
  if (keyResolutionAllClear.value) return '可以返回 API Key 管理重新执行删除或更换分组；系统不会自动继续原操作。'
  return '请在下方关联账号中结束使用或移出预约。全部处理完成后，状态会自动重新核对。'
})
const displayedListings = computed(() => isKeyResolutionMode.value ? keyResolutionListings.value : listings.value)
const mySpendAccountPickerTitle = computed(() => {
  if (mySpendAccountsLoading.value && mySpendAccountOptions.value.length === 0) return '加载中'
  const usingCount = mySpendAccountOptions.value.filter(option => option.source === 'using').length
  const historyCount = mySpendAccountOptions.value.filter(option => option.source === 'history').length
  if (mySpendAccountOptions.value.length === 0) return '暂无记录'
  return `使用/预约 ${usingCount} 个 · 历史 ${historyCount} 个`
})
const mySpendMetrics = computed<MySpendMetric[]>(() => {
  const summary = mySpendSummary.value
  if (!summary) return []
  return [
    {
      key: 'total',
      label: '合计扣费',
      value: formatSpendCost(summary.total_cost),
      note: mySpendRangeLabel(summary.range),
      icon: 'dollar',
      tone: 'total'
    },
    {
      key: 'request',
      label: '请求费用',
      value: formatSpendCost(summary.request_cost),
      note: `${formatWholeNumber(summary.request_count)} 次请求`,
      icon: 'creditCard',
      tone: 'request'
    },
    {
      key: 'hourly',
      label: '小时费实际扣费',
      value: formatSpendCost(summary.hourly_net_cost),
      note: `已预扣 ${formatSpendCost(summary.hourly_charge)} · 已退回 ${formatSpendCost(summary.hourly_refund + summary.hourly_waiver_refund)}`,
      icon: 'clock',
      tone: 'hourly'
    },
    {
      key: 'tokens',
      label: 'Token 总量',
      value: formatWholeNumber(summary.total_tokens),
      note: `输入 ${formatWholeNumber(summary.input_tokens)} · 输出 ${formatWholeNumber(summary.output_tokens)}`,
      icon: 'chart',
      tone: 'usage'
    }
  ]
})
const modelFilterOptions = computed(() => {
  const models = new Set<string>([
    ...DEFAULT_ACCOUNT_SHARE_ALLOWED_MODELS_BY_PLATFORM[activeListingPlatform.value],
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
  if (total === 0) return '暂无可展示结果'
  const safePage = Math.min(Math.max(recommendationPage.value, 1), recommendationPageCount.value)
  const start = (safePage - 1) * ACCOUNT_SHARE_RECOMMENDATION_PAGE_SIZE + 1
  const end = Math.min(start + ACCOUNT_SHARE_RECOMMENDATION_PAGE_SIZE - 1, total)
  return `第 ${start}-${end} 条，共 ${total} 条`
})
const recommendationInputSummary = computed(() => {
  const input = recommendationResult.value?.input
  if (!input) return ''
  const activeHours = normalizeRecommendationActiveHours(input.active_hours)
  const requestsPerHour = activeHours > 0 ? input.request_count / activeHours : input.request_count
  return `${input.request_count} 次请求 / ${formatNumber(activeHours)} 小时 / ${input.model} · ${formatNumber(requestsPerHour)} 次/小时`
})
const modelFilterSummary = computed(() => {
  if (listingFilters.models.length === 0) return '全部模型'
  if (listingFilters.models.length === 1) return listingFilters.models[0]
  return `已选 ${listingFilters.models.length} 个模型`
})
const seatFilterSummary = computed(() => {
  if (listingFilters.seatLimits.length === 0) return '全部席位'
  if (listingFilters.seatLimits.length === 1) return `${listingFilters.seatLimits[0]}人`
  return `已选 ${listingFilters.seatLimits.length} 个席位`
})
const featureTagFilterSummary = computed(() => {
  if (listingFilters.featureTags.length === 0) return '全部标签'
  if (listingFilters.featureTags.length === 1) {
    return visibleListingFeatureTagOptions.value.find(option => option.value === listingFilters.featureTags[0])?.label || '已选标签'
  }
  return `已选 ${listingFilters.featureTags.length} 个标签`
})
const statusFilterSummary = computed(() => (
  listingStatusFilterOptions.find(option => option.value === listingFilters.status)?.label || '默认状态'
))
const accountLevelFilterSummary = computed(() => (
  accountLevelFilterOptions.value.find(option => option.value === listingFilters.accountLevel)?.label || '全部等级'
))
const selectedSortOptions = computed(() =>
  listingFilters.sortKeys
    .map(key => listingSortOptions.find(option => option.key === key))
    .filter((option): option is ListingSortOption => Boolean(option))
)
const activeFilterChips = computed<ActiveFilterChip[]>(() => {
  const chips: ActiveFilterChip[] = []
  const statusOption = listingStatusFilterOptions.find(option => option.value === listingFilters.status)
  if (listingFilters.status !== '' && statusOption) {
    chips.push({
      key: `status:${listingFilters.status}`,
      label: `状态：${statusOption.label}`,
      remove: () => { listingFilters.status = '' }
    })
  }

  const levelOption = accountLevelFilterOptions.value.find(option => option.value === listingFilters.accountLevel)
  if (isOpenAIListingPlatform.value && listingFilters.accountLevel !== 'all' && levelOption) {
    chips.push({
      key: `level:${listingFilters.accountLevel}`,
      label: `等级：${levelOption.label}`,
      remove: () => { listingFilters.accountLevel = 'all' }
    })
  }

  for (const [index, option] of selectedSortOptions.value.entries()) {
    chips.push({
      key: `sort:${option.key}`,
      label: `排序${index + 1}：${option.label}`,
      remove: () => removeListingSort(option.key)
    })
  }

  for (const seat of listingFilters.seatLimits) {
    chips.push({
      key: `seat:${seat}`,
      label: `${seat}人席位`,
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

function hasKnownAccountName(name: string, excludeAccountID?: number): boolean {
  const normalizedName = normalizeAccountName(name)
  if (!normalizedName) return false
  return [...knownListings.value, ...listings.value].some(listing => {
    if (excludeAccountID && listing.account_id === excludeAccountID) return false
    return normalizeAccountName(listing.account_name || '') === normalizedName
  })
}

function suggestedAccountName(platform: AccountSharePlatform = createPlatform.value): string {
  const baseName = ACCOUNT_NAME_BASE_BY_PLATFORM[platform]
  for (let index = 1; index <= 999; index += 1) {
    const candidate = index === 1 ? baseName : `${baseName}${index}`
    if (!hasKnownAccountName(candidate)) return candidate
  }
  return `${baseName}${Date.now()}`
}

function validateAccountName(name: string, excludeAccountID?: number): string {
  const value = name.trim()
  if (!value) return '请填写账号名称'
  if (/\s/.test(name)) return '账号名称不能包含空格、换行或制表符'
  if (hasKnownAccountName(value, excludeAccountID)) return '账号名称已存在，请换一个名称'
  return ''
}

function calculateMaxPerUserConcurrency(accountConcurrency: unknown, seatLimit: unknown): number {
  const concurrency = Number(accountConcurrency)
  const seats = Number(seatLimit)
  if (!Number.isFinite(concurrency) || !Number.isFinite(seats) || concurrency <= 0 || seats <= 0) return 0
  return Math.max(0, Math.floor(concurrency / seats))
}

function buildPerUserConcurrencyLimitTip(accountConcurrency: unknown, seatLimit: unknown, maxPerUser: number): string {
  const concurrency = Number(accountConcurrency)
  const seats = Number(seatLimit)
  const concurrencyLabel = Number.isFinite(concurrency) ? Math.floor(concurrency) : 0
  const seatLabel = Number.isFinite(seats) ? Math.floor(seats) : 0
  return `当前账号并发 ${concurrencyLabel}、席位 ${seatLabel}，每人最高可设 ${maxPerUser} 并发。`
}

function validatePerUserConcurrencyValue(value: unknown, accountConcurrency: unknown, seatLimit: unknown, maxPerUser: number): string {
  const perUserConcurrency = Number(value)
  if (!Number.isFinite(perUserConcurrency) || perUserConcurrency < 1) return '单用户最高并发必须大于 0'
  if (!Number.isInteger(perUserConcurrency)) return '单用户最高并发必须是整数'

  const concurrency = Number(accountConcurrency)
  const seats = Number(seatLimit)
  if (!Number.isFinite(concurrency) || concurrency < 1 || !Number.isFinite(seats) || seats < ACCOUNT_SHARE_MIN_SEATS) return ''
  if (maxPerUser < 1) return `当前账号并发 ${Math.floor(concurrency)}、席位 ${Math.floor(seats)}，无法分配每人至少 1 并发`
  if (perUserConcurrency > maxPerUser) return `当前账号并发 ${Math.floor(concurrency)}、席位 ${Math.floor(seats)}，单用户最高并发不能超过 ${maxPerUser}`
  if (perUserConcurrency * seats > concurrency) return `单用户最高并发 × 席位人数不能超过账号并发上限`
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

function visibleModels(listing: AccountShareListing): string[] {
  return listing.allowed_models.slice(0, MODEL_PREVIEW_LIMIT)
}

function hiddenModels(listing: AccountShareListing): string[] {
  return listing.allowed_models.slice(MODEL_PREVIEW_LIMIT)
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
  return listingSortOptions.find(option => option.key === key)
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
  if (!sortOrder) return `添加${option.label}${option.ascLabel}为第 ${listingFilters.sortKeys.length + 1} 排序`
  const currentLabel = sortOrder === 'asc' ? option.ascLabel : option.descLabel
  const nextLabel = sortOrder === 'asc' ? option.descLabel : option.ascLabel
  return `${priority} 当前${option.label}${currentLabel}，再次点击切换为${nextLabel}`
}

function toggleFilterPopover(popover: ListingFilterPopover): void {
  openFilterPopover.value = openFilterPopover.value === popover ? null : popover
}

function closeFilterPopover(): void {
  openFilterPopover.value = null
}

function handleFilterPanelDocumentClick(event: MouseEvent): void {
  const target = event.target
  if (!(target instanceof Node)) return
  if (filterPanelRef.value?.contains(target)) return
  closeFilterPopover()
}

function setListingStatusFilter(status: ListingStatusFilterValue): void {
  listingFilters.status = status
  closeFilterPopover()
}

function setAccountLevelFilter(level: AccountLevelFilterValue): void {
  listingFilters.accountLevel = level
  closeFilterPopover()
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

function buildListingFilters(): AccountShareListingFilters {
  const result: AccountShareListingFilters = {
    tab: activeFilter.value.tab,
    platform: activeListingPlatform.value
  }
  const search = searchQuery.value.trim()
  if (search) result.search = search
  if (listingFilters.status === 'available') {
    result.status = 'active'
    result.available_only = true
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

function isCanceledRequest(error: unknown): boolean {
  if (typeof error !== 'object' || error === null) return false
  const maybeCanceled = error as { code?: string; name?: string }
  return maybeCanceled.code === 'ERR_CANCELED' || maybeCanceled.name === 'CanceledError' || maybeCanceled.name === 'AbortError'
}

function formatAccountShareLoadError(error: unknown, fallback: string): string {
  const message = extractApiErrorMessage(error, fallback)
  if (/Request failed with status code 500/i.test(message)) {
    return '账号广场接口返回 500，请确认后端服务已启动，或查看后端日志定位原因。'
  }
  if (/Network Error/i.test(message)) {
    return '账号广场接口暂时无法连接，请确认后端服务已启动。'
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
  listingFilters.status = ''
  listingFilters.accountLevel = 'all'
  listingFilters.sortKeys = []
  listingFilters.seatLimits = []
  listingFilters.featureTags = []
  listingFilters.models = []
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

function handlePageSizeChange(pageSize: number): void {
  clearSearchDebounceTimer()
  pagination.page_size = normalizeListingPageSize(pageSize)
  pagination.page = 1
  persistListingPreferences()
  void loadListings()
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

function recommendationScoreBreakdown(candidate: AccountShareRecommendationCandidate): AccountShareRecommendationScoreBreakdown {
  return candidate.score_breakdown
}

function recommendationScoreItems(candidate: AccountShareRecommendationCandidate): RecommendationScoreItem[] {
  const score = recommendationScoreBreakdown(candidate)
  return [
    { key: 'cost', label: '省钱', value: score.cost_saving_score },
    { key: 'stable', label: '稳定', value: score.stability_score },
    { key: 'available', label: '可用', value: score.availability_score },
    { key: 'risk', label: '控险', value: score.risk_control_score }
  ]
}

function recommendationScoreWidth(value: number): string {
  const amount = Math.min(Math.max(Number(value), 0), 100)
  return `${Number.isFinite(amount) ? amount : 0}%`
}

function recommendationOverallScore(candidate: AccountShareRecommendationCandidate): number {
  const score = Number(recommendationScoreBreakdown(candidate).overall_score)
  return Number.isFinite(score) ? score : 0
}

function formatRecommendationScore(value: number): string {
  const score = Math.min(Math.max(Number(value), 0), 100)
  return (Number.isFinite(score) ? score : 0).toFixed(1).replace(/\.0$/, '')
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
  const leftScore = recommendationOverallScore(left)
  const rightScore = recommendationOverallScore(right)
  if (leftScore !== rightScore) return rightScore - leftScore
  const leftRating = Number(left.listing.rating_avg || 0)
  const rightRating = Number(right.listing.rating_avg || 0)
  if (leftRating !== rightRating) return rightRating - leftRating
  return left.listing.id - right.listing.id
}

function setRecommendationPage(page: number): void {
  recommendationPage.value = Math.min(Math.max(Math.trunc(Number(page) || 1), 1), recommendationPageCount.value)
}

function recommendationConcurrencyLabel(listing: AccountShareListing): string {
  const current = listing.current_concurrency ?? 0
  return `${current}/${listing.account_concurrency}`
}

function recommendationRequestCostLabel(candidate: AccountShareRecommendationCandidate): string {
  const prefix = candidate.estimate.owner_self_use ? '自用' : ''
  return `${prefix}${recommendationBillingModeLabel(candidate.estimate.billing_mode)}总费用`
}

function recommendationHourlyCostText(candidate: AccountShareRecommendationCandidate): string {
  return candidate.estimate.owner_self_use ? '不收取' : formatRecommendationCost(candidate.estimate.hourly_net_cost)
}

function recommendationUpfrontCostText(candidate: AccountShareRecommendationCandidate): string {
  return candidate.estimate.owner_self_use ? '不校验' : formatRecommendationCost(candidate.estimate.upfront_required)
}

function recommendationOwnerSelfUseSummary(candidate: AccountShareRecommendationCandidate): string {
  const listing = candidate.listing
  return `这是你自己上架的账号，推荐测算按自用规则执行：${formatNumber(OWNER_SELF_USE_RATE_MULTIPLIER)}x、不收小时费、不校验最低余额；公开参数 ${formatNumber(listing.rate_multiplier)}x / 小时费 ${formatNumber(listing.hourly_rate)} / 低消 ${hourlyFeeWaiverLabel(listing.hourly_fee_waiver_minimum)} 仍用于其他用户。`
}

function recommendationBillingModeLabel(mode: string): string {
  switch (mode) {
    case 'per_request':
      return '按次'
    case 'image':
      return '图片'
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
  if (count <= 0) return '未评分'
  return `${formatRating(Number(listing.rating_avg || 0))}/10 · ${count}人`
}

function ownerDisplayName(listing: AccountShareListing | null | undefined): string {
  if (!listing) return ''
  return listing.owner_username || `用户 ${listing.owner_user_id}`
}

function ownerDialogButtonTitle(listing: AccountShareListing): string {
  return `查看 ${ownerDisplayName(listing)} 的其他账号和评论`
}

function hourlyFeeWaiverLabel(value?: number | null): string {
  const amount = Number(value || 0)
  if (!Number.isFinite(amount) || amount <= 0) return '未开启'
  return `${formatNumber(amount)}/小时`
}

function formatIdleTimeoutSetting(minutes: number): string {
  const normalized = normalizeIdleTimeoutMinutes(minutes)
  if (normalized <= 0) return '未设置'
  if (normalized < 60) return `${normalized} 分钟`
  const hours = Math.floor(normalized / 60)
  const restMinutes = normalized % 60
  if (hours < 24) return restMinutes > 0 ? `${hours} 小时 ${restMinutes} 分钟` : `${hours} 小时`
  const days = Math.floor(hours / 24)
  const restHours = hours % 24
  const hourPart = restHours > 0 ? ` ${restHours} 小时` : ''
  const minutePart = restMinutes > 0 ? ` ${restMinutes} 分钟` : ''
  return `${days} 天${hourPart}${minutePart}`
}

function normalizeDateInput(value?: string | null): Date | null {
  if (!value) return null
  const date = new Date(value)
  return Number.isNaN(date.getTime()) ? null : date
}

function formatDate(value?: string | null): string {
  const date = normalizeDateInput(value)
  return date ? date.toLocaleString() : '-'
}

function formatRelativeUntil(value?: string | null): string {
  const date = normalizeDateInput(value)
  if (!date) return '-'
  const diffMs = date.getTime() - nowMs.value
  if (diffMs <= 0) return '现在'
  const totalMinutes = Math.ceil(diffMs / 60_000)
  const days = Math.floor(totalMinutes / 1440)
  const hours = Math.floor((totalMinutes % 1440) / 60)
  const minutes = totalMinutes % 60
  if (days > 0) return `${days}天${hours > 0 ? ` ${hours}小时` : ''}`
  if (hours > 0) return `${hours}小时${minutes > 0 ? ` ${minutes}分钟` : ''}`
  return `${minutes}分钟`
}

function formatCountdownUntil(value?: string | null): string {
  const date = normalizeDateInput(value)
  if (!date) return '-'
  return date.getTime() <= nowMs.value ? '现在' : `${formatRelativeUntil(value)}后`
}

function formatDurationCompact(seconds?: number | null): string {
  const totalSeconds = Math.max(0, Math.floor(Number(seconds || 0)))
  if (totalSeconds <= 0) return '现在'
  const days = Math.floor(totalSeconds / 86_400)
  const hours = Math.floor((totalSeconds % 86_400) / 3_600)
  const minutes = Math.floor((totalSeconds % 3_600) / 60)
  if (days > 0) return `${days}天${hours > 0 ? `${hours}小时` : ''}`
  if (hours > 0) return `${hours}小时${minutes > 0 ? `${minutes}分钟` : ''}`
  if (minutes > 0) return `${minutes}分钟`
  return `${totalSeconds}秒`
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
  if (!progress) return '未开启'
  return progress.status === 'met' ? '已达标' : `还差 ${formatSpendCost(progress.remainingAmount)}`
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
    return `预计退回小时费 ${formatSpendCost(progress.estimatedHourlyFeeRefund)}`
  }
  return `已消费 ${formatSpendCost(progress.usageAmount)}，低消要求 ${formatSpendCost(progress.requiredAmount)}`
}

function waiverProgressMetaLabel(listing: AccountShareListing): string {
  const progress = currentWaiverProgressSnapshot(listing)
  if (!progress) return ''
  return `剩余 ${formatDurationCompact(progress.remainingSeconds)} · 请求 ${formatWholeNumber(progress.requestCount)} 次`
}

function waiverProgressRemainingLabel(listing: AccountShareListing): string {
  const remainingSeconds = waiverProgressRemainingSeconds(listing)
  return remainingSeconds <= 0 ? '等待结算' : formatDurationCompact(remainingSeconds)
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
  return listing.account_name || `共享账号 #${listing.id}`
}

function isRateMultiplierExpensive(listing: AccountShareListing): boolean {
  if (!isOpenAIListing(listing)) return false
  const multiplier = Number(listing.rate_multiplier || 0)
  if (!Number.isFinite(multiplier)) return false
  switch (accountLevelTone(listing)) {
    case 'plus':
      return multiplier > PLUS_EXPENSIVE_RATE_MULTIPLIER
    case 'pro':
      return multiplier > PRO_EXPENSIVE_RATE_MULTIPLIER
    default:
      return false
  }
}

function isHourlyRateExpensive(listing: AccountShareListing): boolean {
  const hourlyRate = Number(listing.hourly_rate || 0)
  return Number.isFinite(hourlyRate) && hourlyRate > EXPENSIVE_HOURLY_RATE
}

function supportsImageGeneration(listing: AccountShareListing): boolean {
  if (!isOpenAIListing(listing)) return false
  return listing.allowed_models.some(model => {
    const value = model.toLowerCase()
    return /(^|[/_:])(?:gpt-image(?:-|$)|dall-e(?:-|$)|dalle(?:-|$))/.test(value)
  })
}

function usageAvailableLabel(progress?: UsageProgress | null): string {
  if (!progress) return '暂无'
  const available = Math.max(0, 100 - Number(progress.utilization || 0))
  return `${formatNumber(available)}%可用`
}

function currentConcurrencyLabel(listing: AccountShareListing): string {
  const current = Math.max(0, Number(listing.current_concurrency || 0))
  const max = Number(listing.account_concurrency || 0)
  return max > 0 ? `${current} / ${max}` : `${current} / 不限`
}

function capacityPercent(listing: AccountShareListing): number {
  const max = Number(listing.account_concurrency || 0)
  if (max <= 0) return 0
  return Math.max(0, Math.min(100, (Number(listing.current_concurrency || 0) / max) * 100))
}

function capacityWidth(listing: AccountShareListing): string {
  return `${capacityPercent(listing)}%`
}

function capacityFillClass(listing: AccountShareListing): string {
  const percent = capacityPercent(listing)
  if (percent >= 90) return 'capacity-fill-danger'
  if (percent >= 70) return 'capacity-fill-warning'
  return 'capacity-fill-normal'
}

function validityInfo(listing: AccountShareListing): { label: string; expiresAtLabel: string } | null {
  const expiresAt = normalizeDateInput(listing.subscription_expires_at || listing.account_expires_at)
  if (!expiresAt) return null
  const diffMs = expiresAt.getTime() - nowMs.value
  const days = Math.ceil(diffMs / 86_400_000)
  return {
    label: diffMs <= 0 ? '已过期' : `有效期 ${Math.max(1, days)}天`,
    expiresAtLabel: formatDate(expiresAt.toISOString())
  }
}

type RuntimeTone = 'normal' | 'warning' | 'danger' | 'muted'

function runtimeInsight(listing: AccountShareListing): { label: string; detail: string; badge: string; tone: RuntimeTone } {
  if (showOpenAIUsageWindows(listing) && listing.codex_quota_protection_reason) {
    const windowLabel = listing.codex_quota_protection_reason === '7d' ? '7天' : '5小时'
    return {
      label: `${windowLabel}保护中`,
      detail: listing.codex_quota_protection_reset_at ? `预计 ${formatRelativeUntil(listing.codex_quota_protection_reset_at)} 后解除` : '',
      badge: '保护',
      tone: 'warning'
    }
  }
  if (showAnthropicUsageWindows(listing) && listing.anthropic_quota_protection_reason) {
    const windowLabel = listing.anthropic_quota_protection_reason === '7d' ? '7天' : '5小时'
    return {
      label: `Claude ${windowLabel}保护中`,
      detail: listing.anthropic_quota_protection_reset_at ? `预计 ${formatRelativeUntil(listing.anthropic_quota_protection_reset_at)} 后解除` : '',
      badge: '保护',
      tone: 'warning'
    }
  }
  if (isFuture(listing.rate_limit_reset_at)) {
    return {
      label: '限流中',
      detail: `预计 ${formatRelativeUntil(listing.rate_limit_reset_at)} 后解除`,
      badge: '限流',
      tone: 'danger'
    }
  }
  if (isFuture(listing.overload_until)) {
    return {
      label: '过载冷却',
      detail: `预计 ${formatRelativeUntil(listing.overload_until)} 后恢复`,
      badge: '冷却',
      tone: 'warning'
    }
  }
  if (isFuture(listing.temp_unschedulable_until)) {
    return {
      label: '临时不可调度',
      detail: listing.temp_unschedulable_reason || `预计 ${formatRelativeUntil(listing.temp_unschedulable_until)} 后恢复`,
      badge: '暂停',
      tone: 'warning'
    }
  }
  if (listing.account_status && listing.account_status !== 'active') {
    return {
      label: runtimeStatusLabel(listing.account_status),
      detail: '',
      badge: '异常',
      tone: 'danger'
    }
  }
  if (listing.account_schedulable === false) {
    return {
      label: '不可调度',
      detail: '',
      badge: '暂停',
      tone: 'muted'
    }
  }
  if (listing.status !== 'active') {
    return {
      label: statusLabel(listing.status),
      detail: '',
      badge: '未上架',
      tone: 'muted'
    }
  }
  return {
    label: '正常可用',
    detail: '',
    badge: '正常',
    tone: 'normal'
  }
}

function hasRecoverableListingState(listing: AccountShareListing): boolean {
  return (
    listing.account_status === 'error' ||
    isFuture(listing.rate_limit_reset_at) ||
    isFuture(listing.overload_until) ||
    isFuture(listing.temp_unschedulable_until)
  )
}

function isOwnListing(listing: AccountShareListing): boolean {
  const currentUserID = Number(authStore.user?.id || 0)
  return currentUserID > 0 && listing.owner_user_id === currentUserID
}

function canShowListingJoinSection(listing: AccountShareListing): boolean {
  return !listing.queue_membership_id && !listing.current_membership_id && (!isManagementView.value || isOwnListing(listing))
}

function canOwnerRelistListing(listing: AccountShareListing): boolean {
  return !authStore.isAdmin &&
    listing.status !== 'active' &&
    isOwnListing(listing)
}

function isFuture(value?: string | null): boolean {
  const date = normalizeDateInput(value)
  return Boolean(date && date.getTime() > nowMs.value)
}

function listingEditLocked(listing: AccountShareListing): boolean {
  return isFuture(listing.editing_expires_at)
}

function listingEditLockedByOther(listing: AccountShareListing): boolean {
  return listingEditLocked(listing) && !listing.editing_mine
}

function listingEditLockLabel(listing: AccountShareListing): string {
  const editor = listing.editing_mine ? '你' : (listing.editing_by_username || '其他用户')
  const until = listing.editing_expires_at ? formatCountdownUntil(listing.editing_expires_at) : '稍后'
  return `${editor}正在编辑账号配置，${until}前暂时不能加入使用。`
}

function runtimeStatusLabel(status: string): string {
  switch (status) {
    case 'active':
      return '正常'
    case 'inactive':
      return '未激活'
    case 'disabled':
      return '已禁用'
    case 'error':
      return '异常'
    default:
      return status
  }
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
      return '已上架'
    case 'paused':
      return '已暂停'
    case 'disabled':
      return '已下架'
    default:
      return status
  }
}

function statusBadgeClass(status: AccountShareListingStatus): string {
  const base = 'rounded-full px-2.5 py-1 text-xs font-semibold'
  switch (status) {
    case 'active':
      return `${base} bg-emerald-50 text-emerald-700 dark:bg-emerald-500/10 dark:text-emerald-200`
    case 'paused':
      return `${base} bg-amber-50 text-amber-700 dark:bg-amber-500/10 dark:text-amber-200`
    case 'disabled':
      return `${base} bg-gray-100 text-gray-600 dark:bg-dark-700 dark:text-dark-200`
    default:
      return `${base} bg-gray-100 text-gray-600 dark:bg-dark-700 dark:text-dark-200`
  }
}

function modeKeyLabel(key: ApiKey): string {
  return key.name || `Key #${key.id}`
}

function formatApiKeyIDLabel(apiKeyID?: number, emptyLabel = 'Key 未知'): string {
  const normalizedID = Number(apiKeyID || 0)
  return normalizedID > 0 ? `Key #${normalizedID}` : emptyLabel
}

function formatApiKeyDisplayName(apiKeyName?: string, apiKeyID?: number, emptyLabel = 'Key 未知'): string {
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

function showActionError(message: string, title = '操作失败', action: AccountShareActionErrorAction = null): void {
  actionErrorDialog.title = title
  actionErrorDialog.message = message
  actionErrorDialog.action = action
  actionErrorDialog.show = true
}

function closeActionErrorDialog(): void {
  actionErrorDialog.show = false
  actionErrorDialog.title = '操作失败'
  actionErrorDialog.message = ''
  actionErrorDialog.action = null
}

function showModeApiKeyRequiredDialog(listing?: AccountShareListing): void {
  const platform = listingPlatform(listing)
  const groupName = accountModeGroupName(platform)
  if (modeKeysLoadingForPlatform(platform)) {
    showActionError('账号模式 API Key 正在加载，请稍候再加入使用。', '正在加载')
    return
  }
  if (!modeKeysLoadedForPlatform(platform)) {
    const detail = modeKeysErrorByPlatform[platform]
    showActionError(
      detail ? `账号模式 API Key 加载失败：${detail}。请点击页面顶部“刷新”后重试。` : '账号模式 API Key 尚未加载成功，请点击页面顶部“刷新”后重试。',
      '无法加入使用'
    )
    return
  }
  if (modeGroupIDsByPlatform[platform] <= 0) {
    showActionError(`当前账号没有可用的「${groupName}」分组，请联系管理员开通后再加入。`, '无法加入使用')
    return
  }
  if (modeApiKeysForPlatform(platform).length === 0) {
    showActionError(
      `你还没有账号模式 API Key，请先到「API 密钥」页面创建一个绑定「${groupName}」分组的 Key。`,
      '需要账号模式 API Key',
      'create-mode-key'
    )
    return
  }
  showActionError('请先选择一个账号模式 API Key，再加入使用。', '请选择 API Key')
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
  if (!Number.isFinite(parsed) || !Number.isInteger(parsed)) return '空闲自动退出时间必须是整数分钟'
  if (parsed <= 0) return '空闲自动退出时间必须大于 0 分钟'
  if (parsed > ACCOUNT_SHARE_IDLE_TIMEOUT_MAX_MINUTES) return '空闲自动退出时间不能超过 10080 分钟'
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
  const minutes = normalizeIdleTimeoutMinutes(listing.current_idle_timeout_minutes ?? idleTimeoutByListing[listing.id] ?? 0)
  if (minutes <= 0) return '未开启空闲自动退出'
  if (!listing.current_idle_expires_at) return `${minutes} 分钟无请求后自动退出`
  const countdown = formatCountdownUntil(listing.current_idle_expires_at)
  if (countdown === '现在') return '已达到空闲退出时间，系统会自动清理'
  return `${countdown}自动退出`
}

function queueIdleTimeoutSummary(listing: AccountShareListing): string {
  const minutes = normalizeIdleTimeoutMinutes(listing.queue_idle_timeout_minutes ?? idleTimeoutByListing[listing.id] ?? 0)
  if (minutes <= 0) return '激活后使用默认空闲退出'
  return `激活后 ${formatIdleTimeoutSetting(minutes)} 无请求会自动退出`
}

function queueStatusLabel(listing: AccountShareListing): string {
  if (listing.queue_status === 'active' || listing.current_membership_id) return '当前使用'
  if (isFuture(listing.queue_dispatch_cooldown_until)) return `冷却中，${formatRelativeUntil(listing.queue_dispatch_cooldown_until)} 后重试`
  return `预约第 ${listing.queue_rank || '-'} 位`
}

function queueStatusPillClass(listing: AccountShareListing): string {
  if (listing.queue_status === 'active' || listing.current_membership_id) return 'membership-status-pill'
  if (isFuture(listing.queue_dispatch_cooldown_until)) return 'membership-status-pill membership-status-pill-waiting'
  return 'membership-status-pill membership-status-pill-queued'
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
      return '今天'
    case '7d':
      return '近7天'
    default:
      return '本次使用'
  }
}

function mySpendStatusLabel(status?: string): string {
  switch (status) {
    case 'active':
      return '正在使用'
    case 'queued':
      return '预约中'
    case 'ended':
      return '已结束'
    default:
      return status || '-'
  }
}

function mySpendWindowLabel(summary: AccountShareMySpendSummary): string {
  return `${formatDate(summary.start_time)} 至 ${formatDate(summary.end_time)}`
}

function mySpendAccountName(summary: AccountShareMySpendSummary): string {
  return summary.listing.account_name || `共享账号 #${summary.listing.id}`
}

function mySpendLastActivityLabel(summary: AccountShareMySpendSummary): string {
  return summary.last_activity_at ? formatDate(summary.last_activity_at) : '暂无消费记录'
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

function mySpendAccountOptionKey(listing: AccountShareListing, source: MySpendAccountOptionSource, membershipID: number): string {
  return `${source}:${listing.id}:${membershipID}`
}

function mySpendAccountSourceLabel(source: MySpendAccountOptionSource): string {
  return source === 'using' ? '使用/预约' : '历史使用'
}

function mySpendAccountStatusLabel(listing: AccountShareListing): string {
  if (listing.current_membership_id) return '正在使用'
  if (listing.queue_membership_id) return listing.queue_status === 'queued' ? `预约第 ${listing.queue_rank || '-'} 位` : '预约中'
  if (listing.last_used_membership_id) return '已使用'
  return '可统计'
}

function mySpendAccountUsagePeriod(listing: AccountShareListing): string {
  if (listing.current_joined_at) {
    const lastRequest = listing.current_last_request_at ? ` · 最近请求 ${formatDate(listing.current_last_request_at)}` : ''
    return `加入 ${formatDate(listing.current_joined_at)}${lastRequest}`
  }
  if (listing.last_used_at) return `最近使用 ${formatDate(listing.last_used_at)}`
  if (listing.queue_membership_id) return `预约记录 #${listing.queue_membership_id}`
  return `使用记录 #${mySpendMembershipID(listing)}`
}

function mySpendAccountOptionTitle(option: MySpendAccountOption): string {
  return `${listingDisplayName(option.listing)} · ${mySpendAccountSourceLabel(option.source)} · 记录 #${option.membershipID}`
}

function buildMySpendAccountOption(listing: AccountShareListing, source: MySpendAccountOptionSource): MySpendAccountOption | null {
  const normalized = normalizeListingForMerge(listing)
  const membershipID = mySpendMembershipID(normalized)
  if (membershipID <= 0) return null
  return {
    key: mySpendAccountOptionKey(normalized, source, membershipID),
    listing: normalized,
    source,
    membershipID
  }
}

function mySpendAccountOptionSourceForListing(listing: AccountShareListing): MySpendAccountOptionSource {
  return listing.current_membership_id || listing.queue_membership_id ? 'using' : 'history'
}

async function fetchMySpendAccountOptionsByTab(source: MySpendAccountOptionSource, signal: AbortSignal): Promise<MySpendAccountOption[]> {
  const options: MySpendAccountOption[] = []
  let page = 1
  let pages = 1
  do {
    const result = await accountShareAPI.listListings(page, 100, { tab: source }, { signal })
    const items = result.items || []
    for (const listing of items) {
      const option = buildMySpendAccountOption(listing, source)
      if (option) options.push(option)
    }
    pages = Math.max(1, Number(result.pages || 1))
    page += 1
  } while (page <= pages && !signal.aborted)
  return options
}

function mergeMySpendAccountOptions(optionGroups: MySpendAccountOption[][]): MySpendAccountOption[] {
  const options: MySpendAccountOption[] = []
  const seenMemberships = new Set<number>()
  for (const group of optionGroups) {
    for (const option of group) {
      if (seenMemberships.has(option.membershipID)) continue
      seenMemberships.add(option.membershipID)
      options.push(option)
    }
  }
  return options
}

function setSelectedMySpendAccount(option: MySpendAccountOption): void {
  mySpendListing.value = option.listing
  mySpendSelectedMembershipID.value = option.membershipID
  mySpendSelectedOptionKey.value = option.key
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
  const controller = new AbortController()
  const requestSeq = ++mySpendAccountsRequestSeq
  mySpendAccountsRequestController = controller
  mySpendAccountsLoading.value = true
  mySpendAccountsError.value = ''
  try {
    const [usingOptions, historyOptions] = await Promise.all([
      fetchMySpendAccountOptionsByTab('using', controller.signal),
      fetchMySpendAccountOptionsByTab('history', controller.signal)
    ])
    if (controller.signal.aborted || requestSeq !== mySpendAccountsRequestSeq) return
    const mergedOptions = mergeMySpendAccountOptions([usingOptions, historyOptions])
    if (preferredListing && canOpenMySpend(preferredListing)) {
      const preferredMembershipID = mySpendMembershipID(preferredListing)
      const hasPreferred = mergedOptions.some(option => option.membershipID === preferredMembershipID)
      if (!hasPreferred) {
        const preferredOption = buildMySpendAccountOption(preferredListing, mySpendAccountOptionSourceForListing(preferredListing))
        if (preferredOption) mergedOptions.unshift(preferredOption)
      }
    }
    mySpendAccountOptions.value = mergedOptions
    mergeKnownListings(mergedOptions.map(option => option.listing))

    const preferredMembershipID = preferredListing ? mySpendMembershipID(preferredListing) : 0
    const selectedOption = mergedOptions.find(option => option.membershipID === preferredMembershipID)
      || mergedOptions.find(option => option.key === mySpendSelectedOptionKey.value)
      || mergedOptions[0]
    if (selectedOption) {
      setSelectedMySpendAccount(selectedOption)
      void loadMySpendSummary()
    } else {
      mySpendListing.value = null
      mySpendSelectedMembershipID.value = 0
      mySpendSelectedOptionKey.value = ''
      mySpendSummary.value = null
      mySpendError.value = ''
    }
  } catch (error: unknown) {
    if (controller.signal.aborted || requestSeq !== mySpendAccountsRequestSeq || isCanceledRequest(error)) return
    mySpendAccountOptions.value = []
    mySpendListing.value = null
    mySpendSelectedMembershipID.value = 0
    mySpendSelectedOptionKey.value = ''
    mySpendSummary.value = null
    mySpendAccountsError.value = extractApiErrorMessage(error, '加载使用过的账号失败', {
      USER_NOT_FOUND: '当前用户状态异常，请重新登录后再试'
    })
  } finally {
    if (requestSeq === mySpendAccountsRequestSeq) {
      mySpendAccountsLoading.value = false
      if (mySpendAccountsRequestController === controller) mySpendAccountsRequestController = null
    }
  }
}

function openMySpendDialog(listing?: AccountShareListing): void {
  if (listing && !canOpenMySpend(listing)) return
  abortMySpendRequest()
  mySpendListing.value = null
  mySpendSelectedMembershipID.value = 0
  mySpendSelectedOptionKey.value = ''
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
  mySpendListing.value = null
  mySpendSelectedMembershipID.value = 0
  mySpendSelectedOptionKey.value = ''
  mySpendAccountOptions.value = []
  mySpendAccountsError.value = ''
  mySpendAccountsLoading.value = false
  mySpendSummary.value = null
  mySpendError.value = ''
  mySpendLoading.value = false
}

function setMySpendRange(range: AccountShareMySpendRange): void {
  if (mySpendRange.value === range || mySpendLoading.value) return
  mySpendRange.value = range
  void loadMySpendSummary()
}

async function loadMySpendSummary(): Promise<void> {
  const listing = mySpendListing.value
  if (!listing) return
  abortMySpendRequest()
  const controller = new AbortController()
  const requestSeq = ++mySpendRequestSeq
  mySpendRequestController = controller
  mySpendLoading.value = true
  mySpendError.value = ''
  try {
    const membershipID = mySpendRange.value === 'current_membership' ? mySpendSelectedMembershipID.value : 0
    const summary = await accountShareAPI.getMySpendSummary(listing.id, {
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
    mySpendError.value = extractApiErrorMessage(error, '加载消费统计失败', {
      ACCOUNT_SHARE_LISTING_NOT_FOUND: '没有找到这次使用记录或账号已不可查看',
      ACCOUNT_SHARE_SPEND_INVALID_RANGE: '统计范围无效，请切换范围后重试',
      USER_NOT_FOUND: '当前用户状态异常，请重新登录后再试'
    })
  } finally {
    if (requestSeq === mySpendRequestSeq) {
      mySpendLoading.value = false
      if (mySpendRequestController === controller) mySpendRequestController = null
    }
  }
}

function queueMembershipsForApiKey(apiKeyID?: number): AccountShareMembership[] {
  const normalizedApiKeyID = Number(apiKeyID || 0)
  if (normalizedApiKeyID <= 0) return []
  return (queueMembershipsByApiKey.value[normalizedApiKeyID] || [])
    .slice()
    .sort((a, b) => Number(a.queue_rank || 0) - Number(b.queue_rank || 0))
}

async function refreshMembershipQueue(apiKeyID: number): Promise<AccountShareMembership[]> {
  const normalizedApiKeyID = Number(apiKeyID || 0)
  if (normalizedApiKeyID <= 0) return []
  const memberships = await accountShareAPI.listMembershipQueue(normalizedApiKeyID)
  queueMembershipsByApiKey.value = {
    ...queueMembershipsByApiKey.value,
    [normalizedApiKeyID]: memberships
  }
  return queueMembershipsForApiKey(normalizedApiKeyID)
}

function routeQueryString(value: unknown): string {
  if (Array.isArray(value)) return typeof value[0] === 'string' ? value[0] : ''
  return typeof value === 'string' ? value : ''
}

function prepareKeyResolutionMode(): void {
  if (!isKeyResolutionMode.value) return
  clearSearchDebounceTimer()
  closeFilterPopover()
  activeFilter.value = filters[0]
  pagination.page = 1
}

function clearKeyResolutionState(): void {
  keyResolutionRequestSeq += 1
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

  const membership = memberships.find(item => item.status === 'active') || memberships.find(item => item.status === 'queued')
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
    keyResolutionLoaded.value = true
    keyResolutionLoading.value = false
    keyResolutionError.value = '处置链接缺少有效的 API Key ID，请返回 API Key 管理后重新进入。'
    return false
  }

  try {
    const memberships = (await accountShareAPI.listMembershipQueue(apiKeyID))
      .filter(item => item.status === 'active' || item.status === 'queued')
    if (requestSeq !== keyResolutionRequestSeq || apiKeyID !== keyResolutionApiKeyID.value) return false

    const membershipsByListing = new Map<number, AccountShareMembership[]>()
    for (const membership of memberships) {
      const listingID = Number(membership.listing_id || 0)
      if (!Number.isSafeInteger(listingID) || listingID <= 0) {
        throw new Error('关联记录缺少有效的账号 ID，无法安全展示处置入口。')
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
    keyResolutionMemberships.value = memberships
    keyResolutionListings.value = exactListings
    keyResolutionLoaded.value = true
    queueMembershipsByApiKey.value = {
      ...queueMembershipsByApiKey.value,
      [apiKeyID]: memberships
    }
    syncIdleTimeoutControls(exactListings)
    mergeKnownListings(exactListings)
    if (exactListings.length > 0) {
      activeListingPlatform.value = listingPlatform(exactListings[0])
    }
    return true
  } catch (error: unknown) {
    if (requestSeq !== keyResolutionRequestSeq) return false
    keyResolutionMemberships.value = []
    keyResolutionListings.value = []
    keyResolutionLoaded.value = true
    keyResolutionError.value = extractApiErrorMessage(error, '加载 API Key 关联状态失败，请稍后重试。')
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

async function loadQueueSnapshotsForListings(
  items: AccountShareListing[],
  signal: AbortSignal
): Promise<QueueSnapshotLoadResult> {
  const apiKeyIDs = queueApiKeyIDsForListings(items)
  if (apiKeyIDs.length === 0) {
    return { snapshots: {}, failedApiKeyIDs: [] }
  }

  const entries = await Promise.all(apiKeyIDs.map(async apiKeyID => {
    try {
      const memberships = await accountShareAPI.listMembershipQueue(apiKeyID, { signal })
      return { apiKeyID, memberships, failed: false }
    } catch (error: unknown) {
      if (isCanceledRequest(error)) throw error
      if (extractApiErrorCode(error) === 'API_KEY_NOT_FOUND') {
        return { apiKeyID, memberships: [] as AccountShareMembership[], failed: false }
      }
      return { apiKeyID, memberships: [] as AccountShareMembership[], failed: true }
    }
  }))

  const snapshots: Record<number, AccountShareMembership[]> = {}
  const failedApiKeyIDs: number[] = []
  for (const entry of entries) {
    if (entry.failed) {
      failedApiKeyIDs.push(entry.apiKeyID)
    } else {
      snapshots[entry.apiKeyID] = entry.memberships
    }
  }
  return { snapshots, failedApiKeyIDs }
}

function queueApiKeyIDsForListings(items: AccountShareListing[]): number[] {
  return Array.from(new Set(items
    .map(item => Number(item.queue_api_key_id || 0))
    .filter(apiKeyID => apiKeyID > 0)))
}

async function refreshQueueSnapshotsForListings(
  items: AccountShareListing[],
  controller: AbortController,
  requestSeq: number
): Promise<void> {
  try {
    const result = await loadQueueSnapshotsForListings(items, controller.signal)
    if (controller.signal.aborted || requestSeq !== listingsRequestSeq) return

    queueMembershipsByApiKey.value = {
      ...queueMembershipsByApiKey.value,
      ...result.snapshots
    }
    unavailableQueueSnapshotApiKeyIDs.value = new Set(result.failedApiKeyIDs)
    visibleQueueSnapshotWarning.value = result.failedApiKeyIDs.length > 0
      ? '部分预约顺序暂时无法同步，排序操作已禁用；账号列表和预约状态仍已正常加载。'
      : ''
    if (
      result.failedApiKeyIDs.length > 0 &&
      Date.now() - lastQueueSnapshotWarningAt >= ACCOUNT_SHARE_QUEUE_WARNING_THROTTLE_MS
    ) {
      lastQueueSnapshotWarningAt = Date.now()
      appStore.showWarning('账号列表已更新，但部分预约顺序暂时无法同步；排序操作已禁用，请稍后刷新。')
    }
  } catch (error: unknown) {
    if (controller.signal.aborted || requestSeq !== listingsRequestSeq || isCanceledRequest(error)) return
    const failedApiKeyIDs = queueApiKeyIDsForListings(items)
    unavailableQueueSnapshotApiKeyIDs.value = new Set(failedApiKeyIDs)
    visibleQueueSnapshotWarning.value = failedApiKeyIDs.length > 0
      ? '预约顺序暂时无法同步，排序操作已禁用；账号列表和预约状态仍已正常加载。'
      : ''
  } finally {
    if (requestSeq === listingsRequestSeq && listingsRequestController === controller) {
      listingsRequestController = null
    }
  }
}

function canMoveQueueItem(listing: AccountShareListing, direction: -1 | 1): boolean {
  if (!listing.queue_membership_id || reorderingQueueId.value !== null) return false
  if (unavailableQueueSnapshotApiKeyIDs.value.has(Number(listing.queue_api_key_id || 0))) return false
  const queue = queueMembershipsForApiKey(listing.queue_api_key_id)
  const index = queue.findIndex(item => item.id === listing.queue_membership_id)
  if (index < 0) return false
  return direction < 0 ? index > 0 : index < queue.length - 1
}

async function moveQueueItem(listing: AccountShareListing, direction: -1 | 1): Promise<void> {
  const apiKeyID = Number(listing.queue_api_key_id || 0)
  const membershipID = Number(listing.queue_membership_id || 0)
  if (apiKeyID <= 0 || membershipID <= 0 || reorderingQueueId.value !== null) return
  reorderingQueueId.value = membershipID
  try {
    const queue = await refreshMembershipQueue(apiKeyID)
    const index = queue.findIndex(item => Number(item.id || 0) === membershipID)
    const targetIndex = index + direction
    if (index < 0 || targetIndex < 0 || targetIndex >= queue.length) {
      showActionError('预约列表已变化，请刷新后重试。', '排序失败')
      return
    }
    const reordered = queue.map(item => Number(item.id || 0))
    const target = reordered[targetIndex]
    reordered[targetIndex] = reordered[index]
    reordered[index] = target
    const memberships = await accountShareAPI.reorderMembershipQueue({
      api_key_id: apiKeyID,
      membership_ids: reordered
    })
    queueMembershipsByApiKey.value = {
      ...queueMembershipsByApiKey.value,
      [apiKeyID]: memberships
    }
    await loadListings()
    appStore.showSuccess('预约顺序已更新')
  } catch (error: unknown) {
    showActionError(extractApiErrorMessage(error, '更新预约顺序失败', accountShareJoinErrorMessages), '排序失败')
  } finally {
    reorderingQueueId.value = null
  }
}

function setFilter(filter: FilterOption): void {
  clearSearchDebounceTimer()
  closeFilterPopover()
  activeFilter.value = filter
  pagination.page = 1
  persistListingPreferences()
  void loadListings()
}

function sanitizeListingFiltersForPlatform(platform: AccountSharePlatform): void {
  if (platform !== 'openai') {
    listingFilters.accountLevel = 'all'
  }
  listingFilters.featureTags = listingFilters.featureTags.filter(tag =>
    platform === 'openai' || (tag !== 'image_generation' && tag !== 'codex_cli_only' && tag !== 'non_codex_cli_only')
  )
}

function setListingPlatform(platform: AccountSharePlatform): void {
  if (activeListingPlatform.value === platform) return
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

async function refreshPageData(): Promise<void> {
  const tasks: Promise<unknown>[] = [loadListings(), loadModeKeys()]
  if (isKeyResolutionMode.value) tasks.push(loadKeyResolutionState())
  await Promise.all(tasks)
}

function hasVisibleMembershipState(): boolean {
  return listings.value.some(listing => Boolean(listing.current_membership_id || listing.queue_membership_id))
}

function clearMembershipStatusRefreshTimer(): void {
  if (membershipStatusRefreshTimer == null) return
  window.clearTimeout(membershipStatusRefreshTimer)
  membershipStatusRefreshTimer = null
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

  void loadListings()
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
  showRecommendationDialog.value = false
}

function toggleCreatePanel(): void {
  showCreate.value = !showCreate.value
  if (showCreate.value) {
    selectCreatePlatform(activeListingPlatform.value)
    void loadProxies()
    void loadListingNameIndex()
  }
}

function resetOAuthState(): void {
  authURL.value = ''
  authSessionID.value = ''
  oauthFlowRef.value?.reset()
}

function resetCreateForm(): void {
  Object.assign(createForm, buildDefaultCreateForm())
  allowedModels.value = defaultAllowedModelsForPlatform(createPlatform.value)
  createErrorMessage.value = ''
  resetOAuthState()
}

function selectCreatePlatform(platform: AccountSharePlatform): void {
  if (createPlatform.value === platform || creating.value || generatingOAuthURL.value) return
  const proxyID = createForm.proxy_id
  createPlatform.value = platform
  Object.assign(createForm, buildDefaultCreateForm(), { proxy_id: proxyID })
  allowedModels.value = defaultAllowedModelsForPlatform(platform)
  createErrorMessage.value = ''
  resetOAuthState()
}

function resetProxyForm(): void {
  proxySmartInput.value = ''
  proxyDialogError.value = ''
  Object.assign(proxyForm, {
    ip_type: 'ipv4',
    name: '',
    protocol: 'socks5',
    host: '',
    port: null,
    username: '',
    password: ''
  } satisfies UserProxyFormState)
}

function openProxyPurchase(close?: () => void): void {
  close?.()
  window.open(PROXY_PURCHASE_URL, '_blank', 'noopener,noreferrer')
}

function openAddProxyDialog(close?: () => void, target: ProxyTargetForm = 'create'): void {
  close?.()
  proxyTargetForm.value = target
  resetProxyForm()
  showProxyDialog.value = true
}

function closeProxyDialog(): void {
  if (savingProxy.value) return
  showProxyDialog.value = false
  proxyDialogError.value = ''
}

function extractProxyRemark(raw: string): { value: string; remark: string } {
  let remark = ''
  const value = raw
    .replace(/\{([^}]*)}/g, (_, match: string) => {
      remark = match.trim()
      return ''
    })
    .replace(/\[[^\]]*]/g, '')
    .trim()
  return { value, remark }
}

function buildDefaultProxyName(host: string, port: number): string {
  return `我的代理 ${host}:${port}`
}

function updateProxyNameFromParsedInput(host: string, port: number, remark: string): void {
  if (remark) {
    proxyForm.name = remark
    return
  }
  if (!proxyForm.name.trim()) {
    proxyForm.name = buildDefaultProxyName(host, port)
  }
}

function applyParsedProxyURL(raw: string, fallbackProtocol: ProxyProtocol, remark: string): boolean {
  const withProtocol = /^[a-z][a-z0-9+.-]*:\/\//i.test(raw) ? raw : `${fallbackProtocol}://${raw}`
  try {
    const parsed = new URL(withProtocol)
    const protocol = parsed.protocol.replace(':', '').toLowerCase() as ProxyProtocol
    if (!['http', 'https', 'socks5', 'socks5h'].includes(protocol)) return false
    const port = Number(parsed.port)
    if (!parsed.hostname || !Number.isInteger(port) || port < 1 || port > 65535) return false
    proxyForm.protocol = protocol
    proxyForm.host = parsed.hostname
    proxyForm.port = port
    proxyForm.username = decodeURIComponent(parsed.username || '')
    proxyForm.password = decodeURIComponent(parsed.password || '')
    updateProxyNameFromParsedInput(parsed.hostname, port, remark)
    proxyForm.ip_type = parsed.hostname.includes(':') ? 'ipv6' : 'ipv4'
    return true
  } catch {
    return false
  }
}

function applySmartProxyInput(showError: boolean): void {
  const raw = proxySmartInput.value.trim()
  if (!raw) return
  const firstLine = raw.split(/\r?\n/).map(line => line.trim()).filter(Boolean)[0] || ''
  const { value, remark } = extractProxyRemark(firstLine)
  if (!value) return

  if (value.includes('://') || value.includes('@')) {
    if (applyParsedProxyURL(value, proxyForm.protocol, remark)) {
      proxyDialogError.value = ''
      return
    }
  }

  const parts = value.split(':')
  if (parts.length >= 2) {
    const host = parts[0]?.trim()
    const port = Number(parts[1])
    if (host && Number.isInteger(port) && port >= 1 && port <= 65535) {
      proxyForm.host = host
      proxyForm.port = port
      proxyForm.username = (parts[2] || '').trim()
      proxyForm.password = parts.slice(3).join(':').trim()
      proxyForm.ip_type = host.includes(':') ? 'ipv6' : 'ipv4'
      updateProxyNameFromParsedInput(host, port, remark)
      proxyDialogError.value = ''
      return
    }
  }

  if (showError) {
    proxyDialogError.value = '无法识别代理格式，请检查主机、端口、用户名和密码。'
  }
}

function validateUserProxyForm(): string {
  if (!['http', 'https', 'socks5', 'socks5h'].includes(proxyForm.protocol)) return '请选择代理协议'
  if (!proxyForm.host.trim()) return '请输入代理主机'
  if (/\s/.test(proxyForm.host)) return '代理主机不能包含空格'
  const port = Number(proxyForm.port || 0)
  if (!Number.isInteger(port) || port < 1 || port > 65535) return '代理端口必须在 1-65535 之间'
  return ''
}

function upsertProxy(proxy: Proxy): void {
  const index = proxies.value.findIndex(item => item.id === proxy.id)
  if (index >= 0) {
    proxies.value[index] = { ...proxies.value[index], ...proxy }
    return
  }
  proxies.value = [proxy, ...proxies.value]
}

function mergeListingProxyOption(listing: AccountShareListing): void {
  if (!listing.proxy) return
  upsertProxy({
    ...listing.proxy,
    username: listing.proxy.username ?? null
  })
}

async function saveUserProxy(): Promise<void> {
  applySmartProxyInput(false)
  proxyDialogError.value = validateUserProxyForm()
  if (proxyDialogError.value) return

  savingProxy.value = true
  try {
    const created = await accountShareAPI.createProxy({
      name: proxyForm.name.trim() || undefined,
      protocol: proxyForm.protocol,
      host: proxyForm.host.trim(),
      port: Number(proxyForm.port),
      username: proxyForm.username.trim() || undefined,
      password: proxyForm.password.trim() || undefined
    })
    upsertProxy(created)
    if (proxyTargetForm.value === 'edit') {
      editForm.proxy_id = created.id
    } else {
      createForm.proxy_id = created.id
    }
    proxyLoadMessage.value = ''
    showProxyDialog.value = false
  } catch (error: unknown) {
    proxyDialogError.value = extractApiErrorMessage(error, '添加代理 IP 失败')
  } finally {
    savingProxy.value = false
  }
}

function findProxyByID(proxyID: number): Proxy | null {
  if (!Number.isFinite(proxyID) || proxyID <= 0) return null
  return proxies.value.find(proxy => proxy.id === proxyID) || null
}

function proxyCapacityValidationMessage(proxy: Proxy | null | undefined): string {
  if (!proxy) return ''
  const maxAccounts = Number(proxy.max_accounts || 0)
  if (!Number.isFinite(maxAccounts) || maxAccounts <= 0) return ''
  const accountCount = Number(proxy.account_count || 0)
  if (!Number.isFinite(accountCount) || accountCount < maxAccounts) return ''
  return `代理 IP ${proxy.name} 已达到账号容量上限（${accountCount}/${maxAccounts}），请选择其它 IP。`
}

function validateCreateConfig(): string {
  const accountNameError = validateAccountName(createForm.name)
  if (accountNameError) return accountNameError
  if (currentProxyID.value <= 0) return '请选择代理 IP，或先添加自己的代理 IP'
  if (createProxyCapacityValidationMessage.value) return createProxyCapacityValidationMessage.value
  if (!seatOptions.includes(Number(createForm.seat_limit))) return `可使用人数必须在 ${ACCOUNT_SHARE_MIN_SEATS}-${ACCOUNT_SHARE_MAX_SEATS} 人之间`
  if (concurrencyValidationMessage.value) return concurrencyValidationMessage.value
  if (perUserConcurrencyValidationMessage.value) return perUserConcurrencyValidationMessage.value
  if (!Number.isFinite(Number(createForm.rate_multiplier)) || Number(createForm.rate_multiplier) < 0) return '账号倍率不能小于 0'
  if (!Number.isFinite(Number(createForm.hourly_rate)) || Number(createForm.hourly_rate) < 0) return '每小时扣费额度不能小于 0'
  if (!Number.isFinite(Number(createForm.hourly_fee_waiver_minimum)) || Number(createForm.hourly_fee_waiver_minimum) < 0) return '免小时费低消不能小于 0'
  if (!Number.isFinite(Number(createForm.min_balance_required)) || Number(createForm.min_balance_required) < 0) return '最低余额准入不能小于 0'
  if (createPlatform.value === 'openai') {
    if (!Number.isFinite(Number(createForm.codex_5h_limit_percent)) || Number(createForm.codex_5h_limit_percent) < 1 || Number(createForm.codex_5h_limit_percent) > 100) return 'Codex 5h 保护必须在 1-100 之间'
    if (!Number.isFinite(Number(createForm.codex_7d_limit_percent)) || Number(createForm.codex_7d_limit_percent) < 1 || Number(createForm.codex_7d_limit_percent) > 100) return 'Codex 7d 保护必须在 1-100 之间'
  } else {
    if (!Number.isFinite(Number(createForm.anthropic_5h_limit_percent)) || Number(createForm.anthropic_5h_limit_percent) < 1 || Number(createForm.anthropic_5h_limit_percent) > 100) return 'Claude 5h 保护必须在 1-100 之间'
    if (!Number.isFinite(Number(createForm.anthropic_7d_limit_percent)) || Number(createForm.anthropic_7d_limit_percent) < 1 || Number(createForm.anthropic_7d_limit_percent) > 100) return 'Claude 7d 保护必须在 1-100 之间'
  }
  if (parseAllowedModels().length === 0) return '至少填写一个模型白名单'
  return ''
}

function parseEditAllowedModels(): string[] {
  return normalizeAllowedModelList(editAllowedModels.value)
}

function validateEditConfig(): string {
  const accountNameError = validateAccountName(editForm.name, editingConfigListing.value?.account_id)
  if (accountNameError) return accountNameError
  if (currentEditProxyID.value <= 0) return '请选择代理 IP，或先添加自己的代理 IP'
  if (editProxyCapacityValidationMessage.value) return editProxyCapacityValidationMessage.value
  if (!seatOptions.includes(Number(editForm.seat_limit))) return `可使用人数必须在 ${ACCOUNT_SHARE_MIN_SEATS}-${ACCOUNT_SHARE_MAX_SEATS} 人之间`
  if (editConcurrencyValidationMessage.value) return editConcurrencyValidationMessage.value
  if (editPerUserConcurrencyValidationMessage.value) return editPerUserConcurrencyValidationMessage.value
  if (!Number.isFinite(Number(editForm.rate_multiplier)) || Number(editForm.rate_multiplier) < 0) return '账号倍率不能小于 0'
  if (!Number.isFinite(Number(editForm.hourly_rate)) || Number(editForm.hourly_rate) < 0) return '每小时扣费额度不能小于 0'
  if (!Number.isFinite(Number(editForm.hourly_fee_waiver_minimum)) || Number(editForm.hourly_fee_waiver_minimum) < 0) return '免小时费低消不能小于 0'
  if (!Number.isFinite(Number(editForm.min_balance_required)) || Number(editForm.min_balance_required) < 0) return '最低余额准入不能小于 0'
  if (listingPlatform(editingConfigListing.value) === 'openai') {
    if (!Number.isFinite(Number(editForm.codex_5h_limit_percent)) || Number(editForm.codex_5h_limit_percent) < 1 || Number(editForm.codex_5h_limit_percent) > 100) return 'Codex 5h 保护必须在 1-100 之间'
    if (!Number.isFinite(Number(editForm.codex_7d_limit_percent)) || Number(editForm.codex_7d_limit_percent) < 1 || Number(editForm.codex_7d_limit_percent) > 100) return 'Codex 7d 保护必须在 1-100 之间'
  } else if (listingPlatform(editingConfigListing.value) === 'anthropic') {
    if (!Number.isFinite(Number(editForm.anthropic_5h_limit_percent)) || Number(editForm.anthropic_5h_limit_percent) < 1 || Number(editForm.anthropic_5h_limit_percent) > 100) return 'Claude 5h 保护必须在 1-100 之间'
    if (!Number.isFinite(Number(editForm.anthropic_7d_limit_percent)) || Number(editForm.anthropic_7d_limit_percent) < 1 || Number(editForm.anthropic_7d_limit_percent) > 100) return 'Claude 7d 保护必须在 1-100 之间'
  }
  if (parseEditAllowedModels().length === 0) return '至少填写一个模型白名单'
  if (!editSessionID.value) return '编辑会话已失效，请关闭后重新编辑'
  return ''
}

async function loadListings(): Promise<boolean> {
  abortActiveListingsRequest()
  const requestSeq = ++listingsRequestSeq
  const controller = new AbortController()
  listingsRequestController = controller
  let queueSnapshotRefreshStarted = false
  loading.value = true
  errorMessage.value = ''
  try {
    const result = await accountShareAPI.listListings(pagination.page, pagination.page_size, buildListingFilters(), {
      signal: controller.signal
    })
    if (controller.signal.aborted || requestSeq !== listingsRequestSeq) return false
    const realListings = (result.items || []).map(normalizeListingForMerge)
    pagination.total = result.total || 0
    pagination.page = result.page || pagination.page
    pagination.page_size = result.page_size || ACCOUNT_SHARE_PAGE_SIZE
    pagination.pages = result.pages || 1
    listings.value = realListings
    syncIdleTimeoutControls(realListings)
    mergeKnownListings(realListings)
    unavailableQueueSnapshotApiKeyIDs.value = new Set(queueApiKeyIDsForListings(realListings))
    visibleQueueSnapshotWarning.value = ''
    lastMembershipStatusRefreshAt = Date.now()
    clearMembershipStatusRefreshTimer()
    queueSnapshotRefreshStarted = true
    void refreshQueueSnapshotsForListings(realListings, controller, requestSeq)
    return true
  } catch (error: unknown) {
    if (controller.signal.aborted || requestSeq !== listingsRequestSeq || isCanceledRequest(error)) return false
    listings.value = []
    pagination.total = 0
    pagination.pages = 1
    visibleQueueSnapshotWarning.value = ''
    errorMessage.value = formatAccountShareLoadError(error, '加载账号广场失败')
    return false
  } finally {
    if (requestSeq === listingsRequestSeq) {
      loading.value = false
    }
    if (!queueSnapshotRefreshStarted && listingsRequestController === controller) {
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
  if (!listing.editing_expires_at || !isFuture(listing.editing_expires_at)) {
    next.editing_by_user_id = undefined
    next.editing_by_username = ''
    next.editing_expires_at = undefined
    next.edit_session_id = ''
    next.editing_mine = false
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
  if (!updated.editing_expires_at || !isFuture(updated.editing_expires_at)) {
    next.editing_by_user_id = undefined
    next.editing_by_username = ''
    next.editing_expires_at = undefined
    next.edit_session_id = ''
    next.editing_mine = false
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

async function loadListingNameIndex(updateSuggestedName = true): Promise<void> {
  try {
    const result = await accountShareAPI.listListings(1, 100, { tab: 'all', status: 'all' })
    mergeKnownListings(result.items || [])
    if (updateSuggestedName && (!createForm.name.trim() || accountNameValidationMessage.value)) {
      createForm.name = suggestedAccountName()
    }
  } catch {
    // 名称重复仍由创建接口兜底，这里只做前端提示索引。
  }
}

function closeOwnerDialog(): void {
  ownerDialog.show = false
  ownerDialog.ownerUserID = 0
  ownerDialog.ownerUsername = ''
  ownerDialog.sourceListing = null
  ownerDialog.tab = 'listings'
  ownerDialog.listings = []
  ownerDialog.reviews = []
  ownerDialog.error = ''
}

async function openOwnerDialog(listing: AccountShareListing): Promise<void> {
  ownerDialog.show = true
  ownerDialog.ownerUserID = listing.owner_user_id
  ownerDialog.ownerUsername = ownerDisplayName(listing)
  ownerDialog.sourceListing = listing
  ownerDialog.tab = 'listings'
  ownerDialog.error = ''
  ownerDialog.listings = []
  ownerDialog.reviews = []
  await Promise.all([loadOwnerListings(), loadOwnerReviews()])
}

function searchOwnerFromDialog(): void {
  const keyword = ownerDialog.ownerUsername || (ownerDialog.ownerUserID ? String(ownerDialog.ownerUserID) : '')
  if (!keyword) return
  searchQuery.value = keyword
  pagination.page = 1
  closeOwnerDialog()
  applyListingFilters()
}

async function loadOwnerListings(): Promise<void> {
  if (!ownerDialog.ownerUserID) return
  ownerDialog.loadingListings = true
  try {
    const result = await accountShareAPI.listListings(1, 24, {
      tab: 'all',
      status: 'all',
      platform: activeListingPlatform.value,
      owner_user_id: ownerDialog.ownerUserID,
      sort_by: 'rating',
      sort_order: 'desc'
    })
    ownerDialog.listings = result.items || []
  } catch (error: unknown) {
    ownerDialog.error = extractApiErrorMessage(error, '加载号主账号失败')
  } finally {
    ownerDialog.loadingListings = false
  }
}

async function loadOwnerReviews(): Promise<void> {
  if (!ownerDialog.ownerUserID) return
  ownerDialog.loadingReviews = true
  try {
    const result = await accountShareAPI.listOwnerReviews(ownerDialog.ownerUserID, 1, 20)
    ownerDialog.reviews = result.items || []
  } catch (error: unknown) {
    ownerDialog.error = extractApiErrorMessage(error, '加载号主评论失败')
  } finally {
    ownerDialog.loadingReviews = false
  }
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
      throw new Error('账号模式 API Key 分页信息无效')
    }
    totalPages = Math.max(totalPages, reportedPages, 1)
    page += 1
  } while (page <= totalPages)

  return Array.from(keysByID.values())
}

async function loadModeKeys(): Promise<void> {
  const requestSeq = ++modeKeysRequestSeq
  for (const option of ACCOUNT_SHARE_PLATFORM_OPTIONS) {
    modeKeysLoadingByPlatform[option.value] = true
    modeKeysLoadedByPlatform[option.value] = false
    modeKeysErrorByPlatform[option.value] = ''
  }

  try {
    const modeGroups = await accountShareAPI.listModeGroups()
    if (requestSeq !== modeKeysRequestSeq) return
    for (const option of ACCOUNT_SHARE_PLATFORM_OPTIONS) {
      const groupID = Number(modeGroups.find(group => group.platform === option.value)?.group_id || 0)
      if (!Number.isSafeInteger(groupID) || groupID <= 0) {
        throw new Error(`${option.label} 账号模式分组映射无效`)
      }
      modeGroupIDsByPlatform[option.value] = groupID
    }

    const results = await Promise.allSettled(ACCOUNT_SHARE_PLATFORM_OPTIONS.map(async option => {
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
      const platform = ACCOUNT_SHARE_PLATFORM_OPTIONS[index].value
      if (result.status === 'fulfilled') return

      modeApiKeysByPlatform[platform] = []
      clearInvalidSelectedModeApiKeys(platform, [])
      modeKeysLoadedByPlatform[platform] = false
      modeKeysErrorByPlatform[platform] = extractApiErrorMessage(result.reason, '加载账号模式 API Key 失败')
      modeKeysLoadingByPlatform[platform] = false
    })
    syncRecommendationApiKey()
  } catch (error: unknown) {
    if (requestSeq !== modeKeysRequestSeq) return
    const message = extractApiErrorMessage(error, '加载可用分组失败')
    for (const option of ACCOUNT_SHARE_PLATFORM_OPTIONS) {
      modeGroupIDsByPlatform[option.value] = 0
      modeApiKeysByPlatform[option.value] = []
      clearInvalidSelectedModeApiKeys(option.value, [])
      modeKeysLoadedByPlatform[option.value] = false
      modeKeysErrorByPlatform[option.value] = message
    }
  } finally {
    if (requestSeq === modeKeysRequestSeq) {
      for (const option of ACCOUNT_SHARE_PLATFORM_OPTIONS) {
        modeKeysLoadingByPlatform[option.value] = false
      }
    }
  }

  if (requestSeq === modeKeysRequestSeq) {
    const failedPlatforms = ACCOUNT_SHARE_PLATFORM_OPTIONS
      .filter(option => !modeKeysLoadedByPlatform[option.value] && modeKeysErrorByPlatform[option.value])
      .map(option => option.label)
    if (failedPlatforms.length > 0) {
      const suffix = failedPlatforms.length === ACCOUNT_SHARE_PLATFORM_OPTIONS.length
        ? '请点击页面顶部“刷新”后重试。'
        : '其他已成功加载的平台仍可正常使用。'
      appStore.showWarning(`${failedPlatforms.join('、')} 账号模式 Key 加载失败；${suffix}`)
    }
  }
}

function refreshModeKeysInBackground(): void {
  void loadModeKeys().catch((error: unknown) => {
    appStore.showWarning(extractApiErrorMessage(error, '账号模式 Key 刷新失败'))
  })
}

function resetRecommendationResult(options: { keepUsageProfileMessage?: boolean } = {}): void {
  recommendationResult.value = null
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
  const models = new Set<string>(DEFAULT_ACCOUNT_SHARE_ALLOWED_MODELS_BY_PLATFORM[platform])
  for (const listing of [...knownListings.value, ...listings.value]) {
    if (listingPlatform(listing) !== platform) continue
    for (const model of listing.allowed_models) {
      const value = model.trim()
      if (value) models.add(value)
    }
  }
  if (!models.has(recommendationForm.model)) {
    recommendationForm.model = DEFAULT_ACCOUNT_SHARE_ALLOWED_MODELS_BY_PLATFORM[platform][0]
  }
  syncRecommendationApiKey()
}

function applyRecommendationPreset(key: RecommendationPresetKey): void {
  const preset = recommendationPresets.find(item => item.key === key)
  if (!preset) return
  selectedRecommendationPreset.value = key
  recommendationForm.request_count = preset.request_count
  recommendationForm.active_hours = preset.active_hours
  recommendationForm.input_tokens_per_request = preset.input_tokens_per_request
  recommendationForm.output_tokens_per_request = preset.output_tokens_per_request
  recommendationForm.cache_creation_tokens_per_request = preset.cache_creation_tokens_per_request
  recommendationForm.cache_read_tokens_per_request = preset.cache_read_tokens_per_request
  resetRecommendationResult()
}

function applyRecommendationUsageProfileToForm(profile: AccountShareRecommendationUsageProfile): void {
  recommendationForm.request_count = profile.request_count
  recommendationForm.active_hours = profile.active_hours
  recommendationForm.input_tokens_per_request = profile.input_tokens_per_request
  recommendationForm.output_tokens_per_request = profile.output_tokens_per_request
  recommendationForm.cache_creation_tokens_per_request = profile.cache_creation_tokens_per_request
  recommendationForm.cache_read_tokens_per_request = profile.cache_read_tokens_per_request
}

function buildRecommendationUsageProfileMessage(profile: AccountShareRecommendationUsageProfile): string {
  const prefix = profile.used_model_fallback
    ? '当前模型近3天历史不足，已按全部模型均值填入'
    : '已按近3天历史均值填入'
  const capped = profile.capped ? '，部分数值已按测算上限处理' : ''
  const activeHours = normalizeRecommendationActiveHours(profile.active_hours)
  const requestsPerHour = profile.request_count / activeHours
  return `${prefix}：单次输入 ${formatNumber(profile.input_tokens_per_request)}、输出 ${formatNumber(profile.output_tokens_per_request)}、Cache写入 ${formatNumber(profile.cache_creation_tokens_per_request)}、Cache读取 ${formatNumber(profile.cache_read_tokens_per_request)}；按 ${profile.request_count} 次 / ${formatNumber(activeHours)} 小时（${formatNumber(requestsPerHour)} 次/小时）测算预计额度${capped}`
}

async function applyRecentUsageProfile(): Promise<void> {
  if (recommendationUsageProfileLoading.value || recommendationLoading.value) return
  recommendationUsageProfileMessage.value = ''
  recommendationError.value = ''
  syncRecommendationFormForPlatform()
  recommendationUsageProfileLoading.value = true
  try {
    const profile = await accountShareAPI.getRecommendationUsageProfile({
      platform: activeListingPlatform.value,
      model: recommendationForm.model.trim(),
      days: 3
    })
    if (!profile.has_history) {
      recommendationUsageProfileMessage.value = '近3天暂无历史请求，已保留当前预设'
      return
    }
    selectedRecommendationPreset.value = 'history'
    applyRecommendationUsageProfileToForm(profile)
    resetRecommendationResult({ keepUsageProfileMessage: true })
    recommendationUsageProfileMessage.value = buildRecommendationUsageProfileMessage(profile)
  } catch (error: unknown) {
    recommendationUsageProfileMessage.value = extractApiErrorMessage(error, '近3天均值读取失败')
  } finally {
    recommendationUsageProfileLoading.value = false
  }
}

function validateRecommendationForm(): string {
  if (modeKeysLoading.value) return '账号模式 Key 正在加载，请稍候再测算'
  if (!modeKeysLoaded.value) return '账号模式 Key 尚未加载成功，请刷新后再测算'
  if (recommendationKeyOptions.value.length === 0) return `请先创建一个绑定「${accountModeGroupName(activeListingPlatform.value)}」分组的 API Key`
  const apiKeyID = Number(recommendationForm.api_key_id || 0)
  if (apiKeyID <= 0 || !recommendationKeyOptions.value.some(item => item.id === apiKeyID)) return '请选择账号模式 API Key'
  if (!recommendationForm.model.trim()) return '请选择需要测算的模型'
  const requestCount = Number(recommendationForm.request_count)
  if (!Number.isFinite(requestCount) || requestCount <= 0 || !Number.isInteger(requestCount)) return '请求次数必须是正整数'
  const activeHours = Number(recommendationForm.active_hours)
  if (!Number.isFinite(activeHours) || activeHours <= 0) return '使用时长必须大于 0 小时'
  const tokenFields = [
    recommendationForm.input_tokens_per_request,
    recommendationForm.output_tokens_per_request,
    recommendationForm.cache_creation_tokens_per_request,
    recommendationForm.cache_read_tokens_per_request
  ]
  if (tokenFields.some(value => !Number.isFinite(Number(value)) || Number(value) < 0 || !Number.isInteger(Number(value)))) {
    return '单次 token 必须是非负整数'
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
  recommendationLoading.value = true
  try {
    const result = await accountShareAPI.recommendListings({
      platform: activeListingPlatform.value,
      model: recommendationForm.model.trim(),
      api_key_id: Number(recommendationForm.api_key_id),
      request_count: Number(recommendationForm.request_count),
      active_hours: Number(recommendationForm.active_hours),
      input_tokens_per_request: Number(recommendationForm.input_tokens_per_request),
      output_tokens_per_request: Number(recommendationForm.output_tokens_per_request),
      cache_creation_tokens_per_request: Number(recommendationForm.cache_creation_tokens_per_request),
      cache_read_tokens_per_request: Number(recommendationForm.cache_read_tokens_per_request),
      limit: ACCOUNT_SHARE_RECOMMENDATION_LIMIT
    })
    recommendationResult.value = result
    recommendationPage.value = 1
    const recommendedListings = (result.items || []).map(item => item.listing)
    mergeKnownListings(recommendedListings)
    syncIdleTimeoutControls(recommendedListings)
  } catch (error: unknown) {
    recommendationResult.value = null
    recommendationError.value = extractApiErrorMessage(error, '账号推荐测算失败', accountShareRecommendationErrorMessages)
  } finally {
    recommendationLoading.value = false
  }
}

function useRecommendedListing(candidate: AccountShareRecommendationCandidate): void {
  const listing = candidate.listing
  mergeKnownListings([listing])
  selectedKeyByListing[listing.id] = Number(recommendationForm.api_key_id || 0)
  if (!idleTimeoutByListing[listing.id]) {
    idleTimeoutByListing[listing.id] = DEFAULT_ACCOUNT_SHARE_IDLE_TIMEOUT_MINUTES
  }
  void joinUse(listing)
}

async function loadProxies(): Promise<void> {
  if (proxyLoading.value || proxies.value.length > 0) return

  proxyLoading.value = true
  proxyLoadMessage.value = ''
  try {
    proxies.value = await accountShareAPI.listProxies()
  } catch (error: unknown) {
    proxyLoadMessage.value = `${extractApiErrorMessage(error, '代理列表加载失败')}，可尝试添加自己的代理 IP。`
  } finally {
    proxyLoading.value = false
  }
}

async function startOAuth(): Promise<void> {
  createErrorMessage.value = ''
  const validationError = validateCreateConfig()
  if (validationError) {
    createErrorMessage.value = validationError
    return
  }

  generatingOAuthURL.value = true
  try {
    const result = createPlatform.value === 'anthropic'
      ? await accountShareAPI.generateAnthropicAuthURL({ proxy_id: currentProxyID.value })
      : await accountShareAPI.generateOpenAIAuthURL({ proxy_id: currentProxyID.value })
    authURL.value = result.auth_url
    authSessionID.value = result.session_id
    window.open(result.auth_url, '_blank', 'noopener,noreferrer')
  } catch (error: unknown) {
    createErrorMessage.value = extractApiErrorMessage(error, '生成登录链接失败')
  } finally {
    generatingOAuthURL.value = false
  }
}

async function submitOAuth(): Promise<void> {
  createErrorMessage.value = ''
  const validationError = validateCreateConfig()
  if (validationError) {
    createErrorMessage.value = validationError
    return
  }

  const authCode = (oauthFlowRef.value?.authCode || '').trim()
  const oauthState = (oauthFlowRef.value?.oauthState || '').trim()
  if (!authSessionID.value || !authCode || (createPlatform.value === 'openai' && !oauthState)) {
    createErrorMessage.value = createPlatform.value === 'openai'
      ? '请先生成登录链接，并粘贴包含 code 和 state 的 OpenAI 回调结果'
      : '请先生成登录链接，并粘贴包含 code 的 Anthropic 回调结果'
    return
  }

  creating.value = true
  try {
    const payload = {
      session_id: authSessionID.value,
      code: authCode,
      proxy_id: currentProxyID.value,
      name: createForm.name.trim(),
      concurrency: Number(createForm.concurrency),
      seat_limit: Number(createForm.seat_limit),
      rate_multiplier: Number(createForm.rate_multiplier),
      allowed_models: parseAllowedModels(),
      per_user_concurrency: Number(createForm.per_user_concurrency),
      hourly_rate: Number(createForm.hourly_rate),
      hourly_fee_waiver_minimum: Number(createForm.hourly_fee_waiver_minimum),
      min_balance_required: Number(createForm.min_balance_required)
    }
    if (createPlatform.value === 'anthropic') {
      await accountShareAPI.exchangeAnthropicCode({
        ...payload,
        anthropic_5h_limit_percent: Number(createForm.anthropic_5h_limit_percent),
        anthropic_7d_limit_percent: Number(createForm.anthropic_7d_limit_percent)
      })
    } else {
      await accountShareAPI.exchangeOpenAICode({
        ...payload,
        state: oauthState,
        codex_cli_only: createForm.codex_cli_only,
        codex_5h_limit_percent: Number(createForm.codex_5h_limit_percent),
        codex_7d_limit_percent: Number(createForm.codex_7d_limit_percent)
      })
    }
    resetCreateForm()
    showCreate.value = false
    await loadListings()
  } catch (error: unknown) {
    createErrorMessage.value = extractApiErrorMessage(error, '创建共享账号失败')
  } finally {
    creating.value = false
  }
}

async function joinUse(listing: AccountShareListing): Promise<void> {
  if (joiningId.value === listing.id) return
  errorMessage.value = ''
  if (listingEditLocked(listing)) {
    showActionError('账号配置正在编辑中，暂时不能加入使用。', '无法加入使用')
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
    showActionError(idleTimeoutError, '空闲退出设置有误')
    return
  }
  pendingJoinConfirmation.value = {
    listing,
    apiKeyID,
    idleTimeoutMinutes: normalizeIdleTimeoutMinutes(idleTimeoutValue)
  }
}

function closeJoinConfirmation(): void {
  const listingID = pendingJoinConfirmation.value?.listing.id
  if (listingID && joiningId.value === listingID) return
  pendingJoinConfirmation.value = null
}

async function confirmJoinUse(): Promise<void> {
  const pendingJoin = pendingJoinConfirmation.value
  if (!pendingJoin || joiningId.value === pendingJoin.listing.id) return
  if (listingEditLocked(pendingJoin.listing)) {
    pendingJoinConfirmation.value = null
    showActionError('账号配置正在编辑中，暂时不能加入使用。', '无法加入使用')
    return
  }
  await submitJoinUse(pendingJoin)
}

async function submitJoinUse(pendingJoin: PendingJoinConfirmation): Promise<void> {
  const { listing, apiKeyID, idleTimeoutMinutes } = pendingJoin
  joiningId.value = listing.id
  let joinSucceeded = false
  try {
    const membership = await accountShareAPI.joinListing(listing.id, {
      api_key_id: apiKeyID,
      idle_timeout_minutes: idleTimeoutMinutes
    })
    joinSucceeded = true
    pendingJoinConfirmation.value = null
    const successMessage = membership.status === 'queued'
      ? '预约已成功；下一次使用该 Key 发出 API 请求时会按顺序尝试激活'
      : '加入已成功'
    const refreshed = await loadListings()
    if (refreshed) {
      appStore.showSuccess(successMessage)
    } else {
      const actionLabel = membership.status === 'queued' ? '预约' : '加入'
      appStore.showWarning(`${actionLabel}已成功，但状态刷新失败；记录已经创建，请稍后点击页面顶部“刷新”确认状态。`)
    }
  } catch (error: unknown) {
    pendingJoinConfirmation.value = null
    if (joinSucceeded) {
      appStore.showWarning('预约或加入已成功，但状态刷新时发生异常；记录已经创建，请稍后点击页面顶部“刷新”确认状态。')
    } else {
      showActionError(extractApiErrorMessage(error, '加入使用失败', accountShareJoinErrorMessages), '加入使用失败')
    }
  } finally {
    joiningId.value = null
  }
}

function openEndUseConfirm(listing: AccountShareListing): void {
  const membershipID = Number(listing.queue_membership_id || listing.current_membership_id || 0)
  if (membershipID <= 0 || endingId.value !== null) return
  pendingEndUse.value = {
    membershipID,
    apiKeyID: listing.queue_api_key_id || listing.current_api_key_id,
    apiKeyName: boundApiKeyName(listing),
    status: listing.queue_status || (listing.current_membership_id ? 'active' : ''),
    listing
  }
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
  if (pending && membership && pending.status !== 'queued' && membership.last_request_at) {
    openReviewDialog(pending.listing, membership)
  }
}

async function endUse(pending: PendingEndUseState): Promise<AccountShareMembership | null> {
  const membershipID = pending.membershipID
  errorMessage.value = ''
  endingId.value = membershipID
  let endSucceeded = false
  try {
    const intent = await accountShareAPI.createEndMembershipIntent(membershipID)
    const membership = await accountShareAPI.endMembership(membershipID, intent.token)
    endSucceeded = true
    if (pendingEndUse.value === pending) pendingEndUse.value = null
    const successMessage = pending.status === 'queued' ? '已移出预约' : '已结束使用'
    const refreshed = await loadListings()
    const resolutionRefreshed = !isKeyResolutionMode.value || await loadKeyResolutionState()
    if (refreshed && resolutionRefreshed) {
      appStore.showSuccess(successMessage)
    } else {
      appStore.showWarning(`${successMessage}，但状态刷新失败；请稍后点击页面顶部“刷新”确认状态。`)
    }
    return membership
  } catch (error: unknown) {
    if (endSucceeded) {
      appStore.showWarning('结束操作已成功，但状态刷新时发生异常；请稍后点击页面顶部“刷新”确认状态。')
    } else {
      showActionError(extractApiErrorMessage(error, '结束使用失败', accountShareEndErrorMessages), '结束使用失败')
    }
    return null
  } finally {
    if (endingId.value === membershipID) endingId.value = null
  }
}

function openReviewDialog(listing: AccountShareListing, membership: AccountShareMembership): void {
  pendingReview.value = {
    membershipID: membership.id,
    listing,
    score: null,
    comment: '',
    submitting: false,
    error: ''
  }
}

function closeReviewDialog(): void {
  if (pendingReview.value?.submitting) return
  pendingReview.value = null
}

async function submitReview(): Promise<void> {
  const state = pendingReview.value
  if (!state || state.submitting) return
  if (state.score === null || state.score < 0 || state.score > 10) {
    state.error = '请选择 0-10 分'
    return
  }
  state.submitting = true
  state.error = ''
  try {
    await accountShareAPI.submitReview(state.membershipID, {
      score: state.score,
      comment: state.comment.trim() || undefined
    })
    pendingReview.value = null
    await loadListings()
    appStore.showSuccess(state.comment.trim() ? '评分已提交，评论审核通过后展示' : '评分已提交')
  } catch (error: unknown) {
    state.error = extractApiErrorMessage(error, '提交评分失败', {
      ACCOUNT_SHARE_REVIEW_ALREADY_EXISTS: '该次使用已经评分',
      ACCOUNT_SHARE_REVIEW_NO_USAGE: '该次使用没有实际请求记录，不能评分',
      ACCOUNT_SHARE_COMMENT_REVIEW_UNAVAILABLE: '评论审核未启用或配置不完整，请先删除评论内容或稍后再试',
      ACCOUNT_SHARE_REVIEW_COMMENT_TOO_LONG: '评论最多 1000 个字符',
      ACCOUNT_SHARE_REVIEW_INVALID_SCORE: '评分必须在 0-10 之间'
    })
  } finally {
    if (pendingReview.value) pendingReview.value.submitting = false
  }
}

async function saveIdleTimeout(listing: AccountShareListing): Promise<void> {
  const membershipID = Number(listing.queue_membership_id || listing.current_membership_id || 0)
  if (membershipID <= 0 || savingIdleTimeoutId.value === membershipID) return
  errorMessage.value = ''
  const idleTimeoutValue = idleTimeoutByListing[listing.id] ?? listing.current_idle_timeout_minutes ?? listing.queue_idle_timeout_minutes ?? 0
  const idleTimeoutError = validateIdleTimeoutMinutes(idleTimeoutValue)
  if (idleTimeoutError) {
    showActionError(idleTimeoutError, '空闲退出设置有误')
    return
  }
  savingIdleTimeoutId.value = membershipID
  try {
    await accountShareAPI.updateMembershipIdleTimeout(membershipID, normalizeIdleTimeoutMinutes(idleTimeoutValue))
    await loadListings()
    appStore.showSuccess('空闲退出已保存')
  } catch (error: unknown) {
    showActionError(extractApiErrorMessage(error, '保存空闲自动退出失败'), '保存失败')
  } finally {
    savingIdleTimeoutId.value = null
  }
}

async function updateManagedListingStatus(listing: AccountShareListing, status: AccountShareListingStatus): Promise<void> {
  if (listing.status === status) return
  const ownerRelist = canOwnerRelistListing(listing) && status === 'active'
  errorMessage.value = ''
  managingId.value = listing.id
  try {
    const updated = await accountShareAPI.updateListing(listing.id, { status })
    mergeKnownListings([updated])
    await loadListings()
    appStore.showSuccess(ownerRelist ? '自动测试通过，账号已重新上架' : '账号状态已更新')
  } catch (error: unknown) {
    showActionError(
      extractApiErrorMessage(error, ownerRelist ? '自动测试失败，账号未重新上架' : '更新账号状态失败'),
      ownerRelist ? '重新上架失败' : '更新账号状态失败'
    )
  } finally {
    managingId.value = null
  }
}

function closeTestModal(): void {
  showTestModal.value = false
  testingAccount.value = null
}

function closeStatsModal(): void {
  showStatsModal.value = false
  statsAccount.value = null
}

function closeReAuthModal(): void {
  showReAuthModal.value = false
  reAuthAccount.value = null
}

function closeModelEditDialog(): void {
  if (savingModelsId.value !== null) return
  showModelEditDialog.value = false
  editingModelListing.value = null
  editingAllowedModels.value = []
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

function normalizeEditableProxyID(listing: AccountShareListing): number | null {
  const proxyID = Number(listing.proxy_id ?? listing.proxy?.id ?? 0)
  return Number.isFinite(proxyID) && proxyID > 0 ? proxyID : null
}

function populateEditForm(listing: AccountShareListing): void {
  mergeListingProxyOption(listing)
  Object.assign(editForm, {
    name: listing.account_name?.trim() ? listing.account_name : `${ACCOUNT_NAME_BASE_BY_PLATFORM[listingPlatform(listing)]}${listing.account_id}`,
    proxy_id: normalizeEditableProxyID(listing),
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
    anthropic_7d_limit_percent: anthropic7dLimitPercent(listing)
  } satisfies CreateFormState)
  editAllowedModels.value = Array.isArray(listing.allowed_models) ? [...listing.allowed_models] : []
}

function stopEditSessionRenewal(): void {
  if (editSessionRenewTimer != null) {
    window.clearInterval(editSessionRenewTimer)
    editSessionRenewTimer = null
  }
}

function startEditSessionRenewal(): void {
  stopEditSessionRenewal()
  editSessionRenewTimer = window.setInterval(() => {
    void renewConfigEditSession()
  }, 120_000)
}

async function renewConfigEditSession(): Promise<void> {
  const listing = editingConfigListing.value
  const sessionID = editSessionID.value
  if (!listing || !sessionID) return
  try {
    const updated = await accountShareAPI.beginListingEdit(listing.id, {
      session_id: sessionID,
      force: editForceActive.value
    })
    mergeListingUpdate(updated)
    editSessionID.value = updated.edit_session_id || sessionID
  } catch (error: unknown) {
    stopEditSessionRenewal()
    editErrorMessage.value = extractApiErrorMessage(error, '编辑会话续期失败，请关闭后重新编辑')
  }
}

async function releaseConfigEditSession(showError = false): Promise<boolean> {
  const listing = editingConfigListing.value
  const sessionID = editSessionID.value
  if (!listing || !sessionID) return true
  try {
    const updated = await accountShareAPI.releaseListingEdit(listing.id, sessionID)
    mergeListingUpdate(updated)
    return true
  } catch (error: unknown) {
    if (showError) {
      editErrorMessage.value = extractApiErrorMessage(error, '释放编辑会话失败')
    }
    return false
  }
}

function resetConfigEditState(): void {
  showConfigEditDialog.value = false
  editingConfigListing.value = null
  editAllowedModels.value = []
  editSessionID.value = ''
  editForceActive.value = false
  editErrorMessage.value = ''
  releasingConfigEdit.value = false
  Object.assign(editForm, buildDefaultCreateForm())
}

async function closeConfigEditDialog(): Promise<void> {
  if (savingConfigEdit.value || releasingConfigEdit.value) return
  stopEditSessionRenewal()
  releasingConfigEdit.value = true
  const released = await releaseConfigEditSession(true)
  releasingConfigEdit.value = false
  if (!released) {
    startEditSessionRenewal()
    return
  }
  resetConfigEditState()
}

async function openConfigEditDialog(listing: AccountShareListing, force: boolean): Promise<void> {
  errorMessage.value = ''
  editErrorMessage.value = ''
  managedActionId.value = listing.id
  try {
    await Promise.all([loadProxies(), loadListingNameIndex(false)])
    const updated = await accountShareAPI.beginListingEdit(listing.id, {
      session_id: listing.editing_mine ? listing.edit_session_id : undefined,
      force
    })
    if (!updated.edit_session_id) {
      throw new Error('服务端未返回编辑会话，请刷新后重试')
    }
    mergeListingUpdate(updated)
    editingConfigListing.value = updated
    editSessionID.value = updated.edit_session_id
    editForceActive.value = force
    populateEditForm(updated)
    showConfigEditDialog.value = true
    startEditSessionRenewal()
  } catch (error: unknown) {
    showActionError(extractApiErrorMessage(error, '打开编辑配置失败'), '打开编辑配置失败')
  } finally {
    managedActionId.value = null
  }
}

function requestOpenConfigEdit(listing: AccountShareListing): void {
  if (managedActionId.value === listing.id) return
  if (listingEditLockedByOther(listing)) {
    showActionError(listingEditLockLabel(listing), '暂时不能编辑')
    return
  }
  if (Number(listing.active_seats || 0) > 0) {
    if (authStore.isAdmin) {
      pendingForceEditListing.value = listing
      return
    }
    showActionError(`当前有 ${listing.active_seats}/${listing.seat_limit} 个席位正在使用，全部结束后才能编辑账号配置。`, '暂时不能编辑')
    return
  }
  void openConfigEditDialog(listing, false)
}

function cancelForceEdit(): void {
  pendingForceEditListing.value = null
}

function confirmForceEdit(): void {
  const listing = pendingForceEditListing.value
  pendingForceEditListing.value = null
  if (!listing) return
  void openConfigEditDialog(listing, true)
}

async function saveConfigEdit(): Promise<void> {
  const listing = editingConfigListing.value
  if (!listing || savingConfigEdit.value) return
  editErrorMessage.value = ''
  const validationError = validateEditConfig()
  if (validationError) {
    editErrorMessage.value = validationError
    return
  }

  savingConfigEdit.value = true
  try {
    const payload: UpdateAccountShareListingRequest = {
      name: editForm.name.trim(),
      proxy_id: currentEditProxyID.value,
      concurrency: Number(editForm.concurrency),
      seat_limit: Number(editForm.seat_limit),
      rate_multiplier: Number(editForm.rate_multiplier),
      allowed_models: parseEditAllowedModels(),
      per_user_concurrency: Number(editForm.per_user_concurrency),
      hourly_rate: Number(editForm.hourly_rate),
      hourly_fee_waiver_minimum: Number(editForm.hourly_fee_waiver_minimum),
      min_balance_required: Number(editForm.min_balance_required),
      edit_session_id: editSessionID.value,
      force_active_edit: editForceActive.value
    }
    if (listingPlatform(listing) === 'openai') {
      payload.codex_cli_only = editForm.codex_cli_only
      payload.codex_5h_limit_percent = Number(editForm.codex_5h_limit_percent)
      payload.codex_7d_limit_percent = Number(editForm.codex_7d_limit_percent)
    } else if (listingPlatform(listing) === 'anthropic') {
      payload.anthropic_5h_limit_percent = Number(editForm.anthropic_5h_limit_percent)
      payload.anthropic_7d_limit_percent = Number(editForm.anthropic_7d_limit_percent)
    }
    const updated = await accountShareAPI.updateListing(listing.id, payload)
    stopEditSessionRenewal()
    mergeListingUpdate(updated)
    await loadListings()
    appStore.showSuccess('账号配置已更新')
    resetConfigEditState()
  } catch (error: unknown) {
    editErrorMessage.value = extractApiErrorMessage(error, '保存账号配置失败')
  } finally {
    savingConfigEdit.value = false
  }
}

function openModelEditDialog(listing: AccountShareListing): void {
  errorMessage.value = ''
  editingModelListing.value = listing
  editingAllowedModels.value = [...listing.allowed_models]
  showModelEditDialog.value = true
}

async function saveModelEdit(): Promise<void> {
  const listing = editingModelListing.value
  if (!listing) return
  const nextModels = normalizeAllowedModelList(editingAllowedModels.value)
  if (nextModels.length === 0) {
    showActionError('至少保留一个可用模型。', '模型白名单有误')
    return
  }

  errorMessage.value = ''
  savingModelsId.value = listing.id
  try {
    const updated = await accountShareAPI.updateListing(listing.id, { allowed_models: nextModels })
    mergeKnownListings([updated])
    await loadListings()
    appStore.showSuccess('模型已更新')
    showModelEditDialog.value = false
    editingModelListing.value = null
    editingAllowedModels.value = []
  } catch (error: unknown) {
    showActionError(extractApiErrorMessage(error, '更新模型失败'), '更新模型失败')
  } finally {
    savingModelsId.value = null
  }
}

function copyModelName(model: string): void {
  void copyToClipboard(model, `已复制 ${model}`)
}

async function fetchManagedAccount(listing: AccountShareListing): Promise<Account> {
  return managedAccountScope.value === 'admin'
    ? adminAPI.accounts.getById(listing.account_id)
    : accountsAPI.getById(listing.account_id)
}

function syncOpenManagedAccount(account: Account): void {
  if (testingAccount.value?.id === account.id) testingAccount.value = account
  if (statsAccount.value?.id === account.id) statsAccount.value = account
  if (reAuthAccount.value?.id === account.id) reAuthAccount.value = account
}

async function openManagedAccountModal(listing: AccountShareListing, action: ManagedAccountModalAction): Promise<void> {
  errorMessage.value = ''
  managedActionId.value = listing.id
  try {
    const account = await fetchManagedAccount(listing)
    if (action === 'test') {
      testingAccount.value = account
      showTestModal.value = true
    } else if (action === 'stats') {
      statsAccount.value = account
      showStatsModal.value = true
    } else if (action === 'reauth') {
      if (managedAccountScope.value === 'user') {
        await loadProxies()
      }
      reAuthAccount.value = account
      showReAuthModal.value = true
    }
  } catch (error: unknown) {
    showActionError(extractApiErrorMessage(error, '加载账号详情失败'), '加载账号详情失败')
  } finally {
    managedActionId.value = null
  }
}

async function refreshManagedAccountToken(listing: AccountShareListing): Promise<void> {
  errorMessage.value = ''
  managedActionId.value = listing.id
  try {
    let updated: Account
    let warning = ''
    let message = ''
    if (managedAccountScope.value === 'admin') {
      updated = await adminAPI.accounts.refreshCredentials(listing.account_id)
    } else {
      const result = await accountsAPI.refreshCredentials(listing.account_id)
      updated = result.account
      warning = result.warning || ''
      message = result.message || ''
    }
    syncOpenManagedAccount(updated)
    await loadListings()
    if (warning === 'missing_project_id_temporary') {
      appStore.showWarning(message || 'Token 已刷新，但项目 ID 暂时无法获取，系统会自动重试')
    } else {
      appStore.showSuccess('Token 已刷新')
    }
  } catch (error: unknown) {
    showActionError(extractApiErrorMessage(error, '刷新 Token 失败'), '刷新 Token 失败')
  } finally {
    managedActionId.value = null
  }
}

async function recoverManagedAccountState(listing: AccountShareListing): Promise<void> {
  errorMessage.value = ''
  managedActionId.value = listing.id
  try {
    const updated = managedAccountScope.value === 'admin'
      ? await adminAPI.accounts.recoverState(listing.account_id)
      : await accountsAPI.recoverState(listing.account_id)
    syncOpenManagedAccount(updated)
    await loadListings()
    appStore.showSuccess('账号状态已恢复')
  } catch (error: unknown) {
    showActionError(extractApiErrorMessage(error, '恢复账号状态失败'), '恢复账号状态失败')
  } finally {
    managedActionId.value = null
  }
}

async function handleManagedTestSuccess(accountID: number): Promise<void> {
  await loadListings()
  const listing = listings.value.find(item => item.account_id === accountID)
  if (!listing) return
  try {
    syncOpenManagedAccount(await fetchManagedAccount(listing))
  } catch (error: unknown) {
    showActionError(extractApiErrorMessage(error, '测试成功，但刷新账号详情失败'), '刷新账号详情失败')
  }
}

async function handleManagedAccountReauthorized(): Promise<void> {
  showReAuthModal.value = false
  reAuthAccount.value = null
  await loadListings()
}

watch(searchQuery, () => {
  if (suppressNextSearchRefresh) {
    suppressNextSearchRefresh = false
    return
  }
  clearSearchDebounceTimer()
  searchDebounceTimer = window.setTimeout(() => {
    pagination.page = 1
    persistListingPreferences()
    void loadListings()
  }, 300)
})

watch(modeApiKeys, () => {
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
    recommendationForm.cache_read_tokens_per_request
  ],
  () => {
    resetRecommendationResult()
  }
)

onMounted(async () => {
  document.addEventListener('click', handleFilterPanelDocumentClick)
  document.addEventListener('visibilitychange', handleDocumentVisibilityChange)
  window.addEventListener('focus', handleWindowFocus)
  clockTimer = window.setInterval(() => {
    nowMs.value = Date.now()
  }, 30_000)
  try {
    prepareKeyResolutionMode()
    const initializationTasks: Promise<unknown>[] = [loadListings(), loadModeKeys(), loadProxies(), loadListingNameIndex()]
    if (isKeyResolutionMode.value) initializationTasks.push(loadKeyResolutionState())
    await Promise.all(initializationTasks)
  } catch (error: unknown) {
    errorMessage.value = extractApiErrorMessage(error, '初始化账号广场失败')
  }
})

onBeforeUnmount(() => {
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
  abortMySpendAccountsRequest()
  abortMySpendRequest()
  modeKeysRequestSeq += 1
  keyResolutionRequestSeq += 1
  stopEditSessionRenewal()
  void releaseConfigEditSession()
})
</script>

<style scoped>
.account-share-hero {
  position: relative;
  overflow: hidden;
  border-radius: 0.5rem;
  border: 1px solid rgb(226 232 240);
  background: linear-gradient(180deg, rgb(255 255 255), rgb(248 250 252));
  box-shadow: 0 14px 38px rgb(15 23 42 / 0.07);
}

.account-share-hero::before {
  content: '';
  position: absolute;
  inset: 0 0 auto 0;
  height: 4px;
  background: linear-gradient(90deg, rgb(14 165 233), rgb(16 185 129), rgb(245 158 11));
}

.account-share-hero-head {
  position: relative;
  display: flex;
  flex-direction: column;
  gap: 1rem;
  border-bottom: 1px solid rgb(226 232 240);
  padding: 1.125rem;
}

.hero-icon {
  display: inline-flex;
  height: 2.75rem;
  width: 2.75rem;
  flex-shrink: 0;
  align-items: center;
  justify-content: center;
  border-radius: 0.5rem;
  border: 1px solid rgb(191 219 254);
  background: rgb(239 246 255);
  color: rgb(37 99 235);
  box-shadow: inset 0 1px 0 rgb(255 255 255 / 0.8);
}

.hero-actions {
  display: flex;
  flex-wrap: wrap;
  gap: 0.625rem;
}

.hero-utility-actions {
  display: flex;
  flex-wrap: wrap;
  gap: 0.625rem;
}

.hero-actions .btn-primary,
.hero-actions .btn-secondary {
  min-height: 2.75rem;
}

.account-share-guide-button,
.account-share-spend-button {
  display: inline-flex;
  min-height: 2.75rem;
  flex-shrink: 0;
  align-items: center;
  justify-content: center;
  align-self: flex-start;
  border-radius: 0.5rem;
  border: 1px solid rgb(191 219 254);
  background: rgb(239 246 255);
  padding: 0 0.875rem;
  color: rgb(29 78 216);
  font-size: 0.875rem;
  font-weight: 800;
  transition: border-color 0.16s ease, background-color 0.16s ease, color 0.16s ease, box-shadow 0.16s ease;
}

.account-share-guide-button:hover,
.account-share-spend-button:hover {
  border-color: rgb(96 165 250);
  background: rgb(219 234 254);
  box-shadow: 0 8px 18px rgb(37 99 235 / 0.1);
}

.account-share-spend-button {
  border-color: rgb(167 243 208);
  background: rgb(236 253 245);
  color: rgb(4 120 87);
}

.account-share-spend-button:hover {
  border-color: rgb(52 211 153);
  background: rgb(209 250 229);
  color: rgb(6 95 70);
  box-shadow: 0 8px 18px rgb(16 185 129 / 0.12);
}

.account-share-guide {
  display: grid;
  gap: 1rem;
  color: rgb(51 65 85);
}

.account-share-guide-summary,
.account-share-guide-section,
.account-share-guide-note {
  border-radius: 0.5rem;
  border: 1px solid rgb(226 232 240);
  background: rgb(255 255 255);
}

.account-share-guide-summary {
  padding: 1rem;
  background: linear-gradient(180deg, rgb(248 250 252), rgb(255 255 255));
}

.account-share-guide-summary span {
  display: inline-flex;
  margin-bottom: 0.375rem;
  color: rgb(37 99 235);
  font-size: 0.75rem;
  font-weight: 850;
}

.account-share-guide-summary strong {
  display: block;
  color: rgb(15 23 42);
  font-size: 1rem;
  font-weight: 850;
  line-height: 1.5rem;
}

.account-share-guide-summary p,
.account-share-guide-step p,
.account-share-guide-rule-list p,
.account-share-guide-example p,
.account-share-guide-note p,
.account-share-guide-param-grid dd {
  margin: 0;
  color: rgb(71 85 105);
  font-size: 0.875rem;
  line-height: 1.625rem;
}

.account-share-guide-summary p {
  margin-top: 0.5rem;
}

.account-share-guide-flow {
  display: grid;
  grid-template-columns: 1fr;
  gap: 0.75rem;
}

.account-share-guide-step {
  display: grid;
  gap: 0.375rem;
  border-radius: 0.5rem;
  border: 1px solid rgb(226 232 240);
  background: rgb(248 250 252);
  padding: 0.875rem;
}

.account-share-guide-step > span {
  display: inline-flex;
  height: 1.75rem;
  width: 1.75rem;
  align-items: center;
  justify-content: center;
  border-radius: 0.5rem;
  background: rgb(37 99 235);
  color: white;
  font-size: 0.8125rem;
  font-weight: 900;
}

.account-share-guide-step strong,
.account-share-guide-section h4 {
  margin: 0;
  color: rgb(15 23 42);
  font-weight: 850;
}

.account-share-guide-section {
  display: grid;
  gap: 0.875rem;
  padding: 1rem;
}

.account-share-guide-rule-list {
  display: grid;
  gap: 0.75rem;
}

.account-share-guide-rule-list > div,
.account-share-guide-note {
  display: flex;
  align-items: flex-start;
  gap: 0.625rem;
}

.account-share-guide-rule-list svg,
.account-share-guide-note svg {
  margin-top: 0.25rem;
  flex-shrink: 0;
  color: rgb(37 99 235);
}

.account-share-guide-rule-list strong {
  color: rgb(15 23 42);
  font-weight: 850;
}

.account-share-guide-example {
  display: grid;
  gap: 0.5rem;
  border-radius: 0.5rem;
  background: rgb(248 250 252);
  padding: 0.875rem;
}

.account-share-guide-example .account-share-guide-formula {
  border-radius: 0.5rem;
  border: 1px solid rgb(191 219 254);
  background: rgb(239 246 255);
  padding: 0.5rem 0.625rem;
  color: rgb(29 78 216);
  font-weight: 850;
}

.account-share-guide-param-grid {
  display: grid;
  grid-template-columns: 1fr;
  gap: 0.75rem;
  margin: 0;
}

.account-share-guide-param-grid > div {
  min-width: 0;
  border-radius: 0.5rem;
  background: rgb(248 250 252);
  padding: 0.75rem;
}

.account-share-guide-param-grid dt {
  margin-bottom: 0.25rem;
  color: rgb(15 23 42);
  font-size: 0.8125rem;
  font-weight: 850;
}

.account-share-guide-assistant {
  border-color: rgb(191 219 254);
  background:
    linear-gradient(180deg, rgb(239 246 255 / 0.74), rgb(255 255 255)),
    radial-gradient(circle at 100% 0%, rgb(16 185 129 / 0.12), transparent 34%);
}

.account-share-guide-assistant-head {
  display: flex;
  align-items: flex-start;
  gap: 0.75rem;
}

.account-share-guide-assistant-head > span {
  display: inline-flex;
  height: 2rem;
  width: 2rem;
  flex-shrink: 0;
  align-items: center;
  justify-content: center;
  border-radius: 0.5rem;
  background: rgb(37 99 235);
  color: white;
}

.account-share-guide-assistant-head p {
  margin: 0.25rem 0 0;
  color: rgb(71 85 105);
  font-size: 0.875rem;
  line-height: 1.625rem;
}

.account-share-guide-assistant-grid {
  display: grid;
  grid-template-columns: 1fr;
  gap: 0.75rem;
}

.account-share-guide-assistant-grid > div {
  min-width: 0;
  border-radius: 0.5rem;
  border: 1px solid rgb(219 234 254);
  background: rgb(255 255 255 / 0.88);
  padding: 0.75rem;
}

.account-share-guide-assistant-grid strong {
  display: block;
  color: rgb(15 23 42);
  font-size: 0.8125rem;
  font-weight: 850;
}

.account-share-guide-assistant-grid p {
  margin: 0.25rem 0 0;
  color: rgb(71 85 105);
  font-size: 0.8125rem;
  line-height: 1.5rem;
}

.account-share-guide-note {
  padding: 0.875rem 1rem;
  background: rgb(239 246 255);
}

.account-share-platform-tabs {
  position: relative;
  display: grid;
  grid-template-columns: 1fr;
  gap: 0.5rem;
  border-bottom: 1px solid rgb(226 232 240);
  background: rgb(248 250 252 / 0.82);
  padding: 0.875rem 1.125rem;
}

.account-share-platform-tab {
  display: flex;
  min-height: 3rem;
  min-width: 0;
  flex-direction: column;
  justify-content: center;
  gap: 0.125rem;
  border-radius: 0.5rem;
  border: 1px solid rgb(226 232 240);
  padding: 0.625rem 0.875rem;
  text-align: left;
  transition: border-color 0.16s ease, background-color 0.16s ease, box-shadow 0.16s ease, color 0.16s ease;
}

.account-share-platform-tab span,
.account-share-platform-tab small {
  display: block;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.account-share-platform-tab span {
  font-size: 0.875rem;
  font-weight: 800;
}

.account-share-platform-tab small {
  font-size: 0.75rem;
  font-weight: 700;
}

.account-share-platform-tab-active {
  border-color: rgb(37 99 235);
  background: rgb(239 246 255);
  color: rgb(29 78 216);
  box-shadow: inset 0 0 0 1px rgb(37 99 235 / 0.16), 0 8px 18px rgb(37 99 235 / 0.08);
}

.account-share-platform-tab-active small {
  color: rgb(71 85 105);
}

.account-share-platform-tab-idle {
  background: rgb(255 255 255);
  color: rgb(51 65 85);
}

.account-share-platform-tab-idle small {
  color: rgb(100 116 139);
}

.account-share-platform-tab-idle:hover {
  border-color: rgb(148 163 184);
  background: rgb(255 255 255);
}

.account-share-summary-grid {
  position: relative;
  display: grid;
  grid-template-columns: 1fr;
  gap: 1px;
  background: rgb(226 232 240);
}

.summary-cell {
  display: flex;
  min-height: 5.25rem;
  min-width: 0;
  align-items: center;
  gap: 0.75rem;
  background: rgb(255 255 255 / 0.82);
  padding: 1rem 1.125rem;
}

.summary-cell > div {
  min-width: 0;
}

.summary-cell > div > span {
  display: block;
  font-size: 0.75rem;
  font-weight: 700;
  color: rgb(100 116 139);
}

.summary-cell strong {
  display: block;
  margin-top: 0.125rem;
  font-size: 1.5rem;
  line-height: 2rem;
  font-weight: 800;
  color: rgb(17 24 39);
}

.summary-icon {
  display: inline-flex;
  height: 2.375rem;
  width: 2.375rem;
  flex-shrink: 0;
  align-items: center;
  justify-content: center;
  border-radius: 0.5rem;
}

.summary-icon-blue {
  background: rgb(239 246 255);
  color: rgb(37 99 235);
}

.summary-icon-emerald {
  background: rgb(236 253 245);
  color: rgb(5 150 105);
}

.summary-icon-amber {
  background: rgb(255 247 237);
  color: rgb(217 119 6);
}

.summary-icon-violet {
  background: rgb(245 243 255);
  color: rgb(124 58 237);
}

.recommendation-panel {
  overflow: hidden;
  border-radius: 0.5rem;
  border: 1px solid rgb(226 232 240);
  background: rgb(255 255 255);
  box-shadow: 0 18px 46px rgb(15 23 42 / 0.08);
}

.recommendation-dialog-panel {
  border: 0;
  background: transparent;
  box-shadow: none;
}

.recommendation-head {
  display: grid;
  gap: 0.75rem;
  border-radius: 0.5rem;
  border: 1px solid rgb(226 232 240);
  background: linear-gradient(180deg, rgb(255 255 255), rgb(248 250 252));
  padding: 0.875rem;
}

.recommendation-heading {
  display: grid;
  grid-template-columns: auto minmax(0, 1fr);
  align-items: center;
  gap: 0.75rem;
}

.recommendation-heading-icon {
  display: inline-flex;
  height: 2.5rem;
  width: 2.5rem;
  align-items: center;
  justify-content: center;
  border-radius: 0.5rem;
  background: rgb(15 23 42);
  color: white;
}

.recommendation-head h2 {
  margin: 0;
  color: rgb(17 24 39);
  font-size: 1.0625rem;
  font-weight: 850;
  line-height: 1.5rem;
}

.recommendation-head p {
  margin: 0.125rem 0 0;
  color: rgb(100 116 139);
  font-size: 0.8125rem;
  font-weight: 700;
}

.recommendation-profile-help {
  max-width: 68rem;
  margin: 0;
  color: rgb(71 85 105);
  font-size: 0.75rem;
  font-weight: 650;
  line-height: 1.25rem;
}

.recommendation-preset-row {
  display: flex;
  flex-wrap: wrap;
  gap: 0.5rem;
}

.recommendation-preset {
  min-height: 2.5rem;
  border-radius: 0.5rem;
  border: 1px solid rgb(226 232 240);
  background: rgb(248 250 252);
  padding: 0 0.875rem;
  color: rgb(51 65 85);
  font-size: 0.8125rem;
  font-weight: 800;
  transition: border-color 0.16s ease, background-color 0.16s ease, color 0.16s ease;
}

.recommendation-preset:hover {
  border-color: rgb(148 163 184);
  background: rgb(255 255 255);
}

.recommendation-preset-active {
  border-color: rgb(37 99 235);
  background: rgb(239 246 255);
  color: rgb(29 78 216);
}

.recommendation-profile-button {
  display: inline-flex;
  min-height: 2.5rem;
  align-items: center;
  justify-content: center;
  border-radius: 0.5rem;
  border: 1px solid rgb(191 219 254);
  background: rgb(239 246 255);
  padding: 0 0.875rem;
  color: rgb(29 78 216);
  font-size: 0.8125rem;
  font-weight: 850;
  transition: border-color 0.16s ease, background-color 0.16s ease, color 0.16s ease, opacity 0.16s ease;
}

.recommendation-profile-button:hover:not(:disabled) {
  border-color: rgb(96 165 250);
  background: rgb(219 234 254);
}

.recommendation-profile-button:disabled {
  cursor: not-allowed;
  opacity: 0.68;
}

.recommendation-layout {
  display: grid;
  gap: 1rem;
  padding: 1rem 0 0;
}

.recommendation-form-grid {
  display: grid;
  gap: 0.75rem;
}

.recommendation-action-box {
  display: grid;
  align-content: start;
  gap: 0.75rem;
  border-radius: 0.5rem;
  border: 1px solid rgb(226 232 240);
  background: rgb(248 250 252);
  padding: 0.875rem;
}

.recommendation-profile-message {
  margin: 0;
  border-radius: 0.5rem;
  border: 1px solid rgb(191 219 254);
  background: rgb(239 246 255);
  padding: 0.75rem;
  color: rgb(29 78 216);
  font-size: 0.8125rem;
  font-weight: 750;
  line-height: 1.25rem;
}

.recommendation-error {
  margin: 0;
  border-radius: 0.5rem;
  border: 1px solid rgb(248 113 113 / 0.55);
  background: rgb(254 242 242);
  padding: 0.75rem;
  color: rgb(185 28 28);
  font-size: 0.8125rem;
  font-weight: 700;
  line-height: 1.25rem;
}

.recommendation-summary {
  display: grid;
  gap: 0.25rem;
  border-radius: 0.5rem;
  border: 1px solid rgb(191 219 254);
  background: linear-gradient(180deg, rgb(239 246 255), rgb(240 253 250));
  padding: 0.875rem;
}

.recommendation-summary span,
.recommendation-summary small {
  color: rgb(30 64 175);
  font-size: 0.75rem;
  font-weight: 800;
}

.recommendation-summary strong {
  color: rgb(13 148 136);
  font-size: 1.5rem;
  font-weight: 900;
  line-height: 1.875rem;
}

.recommendation-results {
  display: grid;
  gap: 0.75rem;
  margin-top: 1rem;
  border-radius: 0.5rem;
  border: 1px solid rgb(226 232 240);
  background: rgb(248 250 252);
  padding: 0.875rem;
}

.recommendation-empty {
  border-radius: 0.5rem;
  border: 1px dashed rgb(203 213 225);
  background: rgb(255 255 255);
  padding: 1rem;
  color: rgb(100 116 139);
  font-size: 0.875rem;
  font-weight: 700;
  text-align: center;
}

.recommendation-results-head {
  display: grid;
  gap: 0.75rem;
  border-radius: 0.5rem;
  border: 1px solid rgb(226 232 240);
  background: white;
  padding: 0.75rem;
}

.recommendation-results-head strong {
  display: block;
  color: rgb(15 23 42);
  font-size: 0.9375rem;
  font-weight: 850;
}

.recommendation-results-head span {
  display: block;
  margin-top: 0.125rem;
  color: rgb(100 116 139);
  font-size: 0.75rem;
  font-weight: 750;
}

.recommendation-page-controls {
  display: grid;
  grid-template-columns: 2.25rem minmax(3.5rem, auto) 2.25rem;
  align-items: center;
  justify-content: start;
  gap: 0.375rem;
}

.recommendation-page-controls > span {
  margin: 0;
  text-align: center;
  color: rgb(51 65 85);
  font-size: 0.8125rem;
  font-weight: 850;
}

.recommendation-page-button {
  display: inline-flex;
  min-height: 2.25rem;
  min-width: 2.25rem;
  align-items: center;
  justify-content: center;
  border-radius: 0.5rem;
  border: 1px solid rgb(226 232 240);
  background: rgb(255 255 255);
  color: rgb(51 65 85);
  transition: border-color 0.16s ease, background-color 0.16s ease, color 0.16s ease, opacity 0.16s ease;
}

.recommendation-page-button:hover:not(:disabled) {
  border-color: rgb(37 99 235);
  background: rgb(239 246 255);
  color: rgb(29 78 216);
}

.recommendation-page-button:disabled {
  cursor: not-allowed;
  opacity: 0.42;
}

.recommendation-card {
  display: grid;
  gap: 0.625rem;
  border-radius: 0.5rem;
  border: 1px solid rgb(226 232 240);
  background: rgb(255 255 255);
  padding: 0.75rem;
}

.recommendation-card-head {
  display: grid;
  gap: 0.75rem;
}

.recommendation-title {
  display: flex;
  min-width: 0;
  align-items: center;
  gap: 0.625rem;
}

.recommendation-rank {
  display: inline-flex;
  height: 2rem;
  width: 2rem;
  flex-shrink: 0;
  align-items: center;
  justify-content: center;
  border-radius: 0.5rem;
  background: rgb(15 23 42);
  color: white;
  font-size: 0.8125rem;
  font-weight: 900;
}

.recommendation-title strong,
.recommendation-title small {
  display: block;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.recommendation-title strong {
  color: rgb(17 24 39);
  font-size: 0.9375rem;
  font-weight: 850;
}

.recommendation-title small {
  margin-top: 0.125rem;
  color: rgb(100 116 139);
  font-size: 0.75rem;
  font-weight: 700;
}

.recommendation-total {
  display: grid;
  gap: 0.125rem;
  border-radius: 0.5rem;
  background: rgb(236 253 245);
  padding: 0.5625rem 0.6875rem;
}

.recommendation-total span {
  color: rgb(5 150 105);
  font-size: 0.6875rem;
  font-weight: 800;
}

.recommendation-total strong {
  color: rgb(4 120 87);
  font-size: 1.125rem;
  font-weight: 900;
  line-height: 1.375rem;
}

.recommendation-tag-row,
.recommendation-reasons,
.recommendation-warnings {
  display: flex;
  flex-wrap: wrap;
  gap: 0.375rem;
}

.recommendation-tag-row span {
  border-radius: 999px;
  background: rgb(219 234 254);
  padding: 0.25rem 0.625rem;
  color: rgb(29 78 216);
  font-size: 0.75rem;
  font-weight: 800;
}

.recommendation-score-panel {
  display: grid;
  gap: 0.625rem;
  border-radius: 0.5rem;
  border: 1px solid rgb(203 213 225);
  background: linear-gradient(180deg, rgb(255 255 255), rgb(248 250 252));
  padding: 0.625rem;
}

.recommendation-score-overview {
  display: flex;
  min-width: 0;
  align-items: center;
  justify-content: space-between;
  gap: 0.75rem;
}

.recommendation-score-overview span {
  color: rgb(71 85 105);
  font-size: 0.75rem;
  font-weight: 850;
}

.recommendation-score-overview strong {
  color: rgb(15 23 42);
  font-size: 1rem;
  font-weight: 900;
}

.recommendation-score-grid {
  display: grid;
  grid-template-columns: 1fr;
  gap: 0.5rem;
}

.recommendation-score-item {
  display: grid;
  min-width: 0;
  gap: 0.375rem;
}

.recommendation-score-item > div {
  display: flex;
  min-width: 0;
  align-items: center;
  justify-content: space-between;
  gap: 0.5rem;
}

.recommendation-score-item span {
  color: rgb(100 116 139);
  font-size: 0.6875rem;
  font-weight: 800;
}

.recommendation-score-item strong {
  color: rgb(30 41 59);
  font-size: 0.75rem;
  font-weight: 900;
}

.recommendation-score-bar {
  position: relative;
  display: block;
  height: 0.375rem;
  overflow: hidden;
  border-radius: 999px;
  background: rgb(226 232 240);
}

.recommendation-score-bar::after {
  position: absolute;
  inset: 0 auto 0 0;
  width: var(--score-width, 0%);
  border-radius: inherit;
  background: linear-gradient(90deg, rgb(37 99 235), rgb(20 184 166));
  content: "";
}

.recommendation-metrics {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 0.5rem;
}

.recommendation-metrics > div {
  min-width: 0;
  border-radius: 0.5rem;
  border: 1px solid rgb(226 232 240);
  background: rgb(248 250 252);
  padding: 0.5625rem 0.625rem;
}

.recommendation-metrics span {
  display: block;
  color: rgb(100 116 139);
  font-size: 0.6875rem;
  font-weight: 800;
}

.recommendation-metrics strong {
  display: block;
  margin-top: 0.125rem;
  color: rgb(17 24 39);
  font-size: 0.875rem;
  font-weight: 850;
  overflow-wrap: anywhere;
}

.recommendation-self-use-note {
  display: flex;
  align-items: flex-start;
  gap: 0.5rem;
  border-radius: 0.5rem;
  border: 1px solid rgb(191 219 254);
  background: rgb(239 246 255);
  padding: 0.625rem 0.75rem;
  color: rgb(30 64 175);
  font-size: 0.75rem;
  font-weight: 750;
  line-height: 1.25rem;
}

.recommendation-self-use-note svg {
  margin-top: 0.125rem;
  flex-shrink: 0;
}

.recommendation-reasons span {
  color: rgb(71 85 105);
  font-size: 0.75rem;
  font-weight: 700;
  line-height: 1.125rem;
}

.recommendation-warnings span {
  border-radius: 0.5rem;
  border: 1px solid rgb(251 146 60 / 0.5);
  background: rgb(255 247 237);
  padding: 0.5rem 0.625rem;
  color: rgb(154 52 18);
  font-size: 0.75rem;
  font-weight: 800;
  line-height: 1.125rem;
}

.recommendation-card-actions {
  display: flex;
  flex-direction: column;
  gap: 0.625rem;
  border-top: 1px solid rgb(226 232 240);
  padding-top: 0.625rem;
}

.recommendation-card-actions > span {
  color: rgb(100 116 139);
  font-size: 0.75rem;
  font-weight: 800;
}

.dark .account-share-hero {
  border-color: rgb(63 63 70);
  background: linear-gradient(180deg, rgb(24 24 27), rgb(31 41 55 / 0.72));
  box-shadow: 0 16px 40px rgb(0 0 0 / 0.28);
}

.dark .account-share-hero-head {
  border-color: rgb(63 63 70);
}

.dark .account-share-platform-tabs {
  border-color: rgb(63 63 70);
  background: rgb(24 24 27 / 0.78);
}

.dark .account-share-platform-tab {
  border-color: rgb(63 63 70);
}

.dark .account-share-platform-tab-active {
  border-color: rgb(96 165 250);
  background: rgb(30 64 175 / 0.2);
  color: rgb(191 219 254);
  box-shadow: inset 0 0 0 1px rgb(96 165 250 / 0.18);
}

.dark .account-share-platform-tab-active small {
  color: rgb(203 213 225);
}

.dark .account-share-platform-tab-idle {
  background: rgb(39 39 42 / 0.68);
  color: rgb(226 232 240);
}

.dark .account-share-platform-tab-idle small {
  color: rgb(161 161 170);
}

.dark .account-share-platform-tab-idle:hover {
  border-color: rgb(113 113 122);
  background: rgb(39 39 42);
}

.dark .hero-icon {
  border-color: rgb(59 130 246 / 0.36);
  background: rgb(30 64 175 / 0.2);
  color: rgb(147 197 253);
}

.dark .account-share-guide-button,
.dark .account-share-spend-button {
  border-color: rgb(59 130 246 / 0.38);
  background: rgb(30 64 175 / 0.22);
  color: rgb(191 219 254);
}

.dark .account-share-guide-button:hover,
.dark .account-share-spend-button:hover {
  border-color: rgb(96 165 250 / 0.7);
  background: rgb(30 64 175 / 0.34);
  box-shadow: 0 8px 18px rgb(0 0 0 / 0.2);
}

.dark .account-share-spend-button {
  border-color: rgb(16 185 129 / 0.36);
  background: rgb(6 95 70 / 0.2);
  color: rgb(167 243 208);
}

.dark .account-share-spend-button:hover {
  border-color: rgb(52 211 153 / 0.62);
  background: rgb(6 95 70 / 0.32);
}

.dark .account-share-guide {
  color: rgb(203 213 225);
}

.dark .account-share-guide-summary,
.dark .account-share-guide-section,
.dark .account-share-guide-note {
  border-color: rgb(63 63 70);
  background: rgb(24 24 27 / 0.86);
}

.dark .account-share-guide-summary {
  background: linear-gradient(180deg, rgb(31 41 55 / 0.82), rgb(24 24 27 / 0.86));
}

.dark .account-share-guide-summary span,
.dark .account-share-guide-rule-list svg,
.dark .account-share-guide-note svg {
  color: rgb(147 197 253);
}

.dark .account-share-guide-summary strong,
.dark .account-share-guide-step strong,
.dark .account-share-guide-section h4,
.dark .account-share-guide-rule-list strong,
.dark .account-share-guide-param-grid dt {
  color: rgb(248 250 252);
}

.dark .account-share-guide-summary p,
.dark .account-share-guide-step p,
.dark .account-share-guide-rule-list p,
.dark .account-share-guide-example p,
.dark .account-share-guide-note p,
.dark .account-share-guide-param-grid dd {
  color: rgb(203 213 225);
}

.dark .account-share-guide-step,
.dark .account-share-guide-example,
.dark .account-share-guide-param-grid > div {
  border-color: rgb(63 63 70);
  background: rgb(39 39 42 / 0.72);
}

.dark .account-share-guide-assistant {
  border-color: rgb(59 130 246 / 0.38);
  background:
    linear-gradient(180deg, rgb(30 64 175 / 0.2), rgb(24 24 27 / 0.86)),
    radial-gradient(circle at 100% 0%, rgb(16 185 129 / 0.16), transparent 34%);
}

.dark .account-share-guide-assistant-head > span {
  background: rgb(37 99 235);
  color: white;
}

.dark .account-share-guide-assistant-head p,
.dark .account-share-guide-assistant-grid p {
  color: rgb(203 213 225);
}

.dark .account-share-guide-assistant-grid > div {
  border-color: rgb(59 130 246 / 0.28);
  background: rgb(39 39 42 / 0.62);
}

.dark .account-share-guide-assistant-grid strong {
  color: rgb(248 250 252);
}

.dark .account-share-guide-example .account-share-guide-formula {
  border-color: rgb(59 130 246 / 0.38);
  background: rgb(30 64 175 / 0.24);
  color: rgb(191 219 254);
}

.dark .account-share-guide-note {
  background: rgb(30 64 175 / 0.18);
}

.dark .account-share-summary-grid {
  background: rgb(63 63 70);
}

.dark .summary-cell {
  background: rgb(24 24 27 / 0.78);
}

.dark .summary-cell > div > span {
  color: rgb(161 161 170);
}

.dark .summary-cell strong {
  color: white;
}

.dark .summary-icon-blue {
  background: rgb(37 99 235 / 0.18);
  color: rgb(147 197 253);
}

.dark .summary-icon-emerald {
  background: rgb(5 150 105 / 0.18);
  color: rgb(110 231 183);
}

.dark .summary-icon-amber {
  background: rgb(180 83 9 / 0.18);
  color: rgb(253 186 116);
}

.dark .summary-icon-violet {
  background: rgb(109 40 217 / 0.2);
  color: rgb(196 181 253);
}

.dark .recommendation-panel {
  border-color: rgb(63 63 70);
  background: rgb(24 24 27);
  box-shadow: 0 14px 36px rgb(0 0 0 / 0.26);
}

.dark .recommendation-dialog-panel {
  border: 0;
  background: transparent;
  box-shadow: none;
}

.dark .recommendation-head,
.dark .recommendation-results,
.dark .recommendation-card-actions {
  border-color: rgb(63 63 70);
}

.dark .recommendation-head {
  background: linear-gradient(180deg, rgb(24 24 27), rgb(39 39 42 / 0.78));
}

.dark .recommendation-heading-icon {
  background: rgb(59 130 246);
}

.dark .recommendation-head h2,
.dark .recommendation-results-head strong,
.dark .recommendation-title strong,
.dark .recommendation-score-overview strong,
.dark .recommendation-metrics strong {
  color: white;
}

.dark .recommendation-head p,
.dark .recommendation-results-head span,
.dark .recommendation-page-controls > span,
.dark .recommendation-title small,
.dark .recommendation-score-overview span,
.dark .recommendation-score-item span,
.dark .recommendation-metrics span,
.dark .recommendation-card-actions > span {
  color: rgb(161 161 170);
}

.dark .recommendation-profile-help {
  color: rgb(148 163 184);
}

.dark .recommendation-preset {
  border-color: rgb(63 63 70);
  background: rgb(39 39 42 / 0.72);
  color: rgb(212 212 216);
}

.dark .recommendation-preset:hover {
  border-color: rgb(113 113 122);
  background: rgb(39 39 42);
}

.dark .recommendation-preset-active {
  border-color: rgb(96 165 250);
  background: rgb(30 64 175 / 0.22);
  color: rgb(191 219 254);
}

.dark .recommendation-profile-button {
  border-color: rgb(59 130 246 / 0.42);
  background: rgb(30 64 175 / 0.24);
  color: rgb(191 219 254);
}

.dark .recommendation-profile-button:hover:not(:disabled) {
  border-color: rgb(96 165 250);
  background: rgb(30 64 175 / 0.36);
}

.dark .recommendation-profile-message {
  border-color: rgb(59 130 246 / 0.35);
  background: rgb(30 64 175 / 0.2);
  color: rgb(191 219 254);
}

.dark .recommendation-error {
  border-color: rgb(248 113 113 / 0.45);
  background: rgb(127 29 29 / 0.36);
  color: rgb(254 202 202);
}

.dark .recommendation-summary {
  border-color: rgb(59 130 246 / 0.35);
  background: linear-gradient(180deg, rgb(30 41 59 / 0.9), rgb(20 83 45 / 0.28));
}

.dark .recommendation-summary span,
.dark .recommendation-summary small {
  color: rgb(147 197 253);
}

.dark .recommendation-summary strong {
  color: rgb(94 234 212);
}

.dark .recommendation-results {
  background: rgb(39 39 42 / 0.58);
}

.dark .recommendation-action-box,
.dark .recommendation-results-head {
  border-color: rgb(63 63 70);
  background: rgb(24 24 27);
}

.dark .recommendation-empty,
.dark .recommendation-card {
  border-color: rgb(63 63 70);
  background: rgb(24 24 27);
}

.dark .recommendation-empty,
.dark .recommendation-reasons span {
  color: rgb(161 161 170);
}

.dark .recommendation-rank {
  background: rgb(59 130 246);
}

.dark .recommendation-total {
  background: rgb(6 78 59 / 0.42);
}

.dark .recommendation-total span {
  color: rgb(110 231 183);
}

.dark .recommendation-total strong {
  color: rgb(167 243 208);
}

.dark .recommendation-tag-row span {
  background: rgb(30 64 175 / 0.3);
  color: rgb(191 219 254);
}

.dark .recommendation-score-panel {
  border-color: rgb(63 63 70);
  background: linear-gradient(180deg, rgb(39 39 42 / 0.72), rgb(24 24 27 / 0.86));
}

.dark .recommendation-score-item strong {
  color: rgb(226 232 240);
}

.dark .recommendation-score-bar {
  background: rgb(63 63 70);
}

.dark .recommendation-score-bar::after {
  background: linear-gradient(90deg, rgb(96 165 250), rgb(45 212 191));
}

.dark .recommendation-page-button {
  border-color: rgb(63 63 70);
  background: rgb(39 39 42 / 0.72);
  color: rgb(212 212 216);
}

.dark .recommendation-page-button:hover:not(:disabled) {
  border-color: rgb(96 165 250);
  background: rgb(30 64 175 / 0.24);
  color: rgb(191 219 254);
}

.dark .recommendation-metrics > div {
  border-color: rgb(63 63 70);
  background: rgb(39 39 42 / 0.72);
}

.dark .recommendation-self-use-note {
  border-color: rgb(59 130 246 / 0.35);
  background: rgb(30 64 175 / 0.2);
  color: rgb(191 219 254);
}

.dark .recommendation-warnings span {
  border-color: rgb(251 146 60 / 0.42);
  background: rgb(124 45 18 / 0.36);
  color: rgb(254 215 170);
}

@media (min-width: 640px) {
  .account-share-platform-tabs {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }

  .account-share-guide-param-grid {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }

  .account-share-guide-assistant-grid {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }

  .account-share-summary-grid {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }

  .recommendation-head {
    grid-template-columns: minmax(0, 1fr) auto;
    align-items: center;
    justify-content: space-between;
  }

  .recommendation-profile-help {
    grid-column: 1 / -1;
  }

  .recommendation-preset-row {
    justify-content: flex-end;
  }

  .recommendation-results-head {
    grid-template-columns: minmax(0, 1fr) auto;
    align-items: center;
  }

  .recommendation-page-controls {
    justify-content: end;
  }

  .recommendation-score-grid {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }

  .recommendation-form-grid {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }

  .recommendation-card-head,
  .recommendation-card-actions {
    grid-template-columns: minmax(0, 1fr) auto;
    align-items: center;
  }

  .recommendation-card-actions {
    display: grid;
  }
}

@media (min-width: 1024px) {
  .account-share-hero-head {
    flex-direction: row;
    align-items: center;
    justify-content: space-between;
    padding: 1.125rem 1.25rem;
  }

  .hero-utility-actions {
    align-items: center;
  }

  .account-share-guide-button,
  .account-share-spend-button {
    align-self: center;
  }

  .account-share-guide-flow {
    grid-template-columns: repeat(3, minmax(0, 1fr));
  }

  .account-share-platform-tabs {
    padding: 0.875rem 1.25rem;
  }

  .recommendation-layout {
    grid-template-columns: minmax(0, 1fr) minmax(18rem, 22rem);
    align-items: start;
    padding: 1rem 1.25rem;
  }

  .recommendation-results {
    padding: 1rem 1.25rem;
  }

  .recommendation-metrics {
    grid-template-columns: repeat(5, minmax(0, 1fr));
  }

  .recommendation-score-grid {
    grid-template-columns: repeat(4, minmax(0, 1fr));
  }
}

@media (min-width: 1280px) {
  .account-share-summary-grid {
    grid-template-columns: repeat(4, minmax(0, 1fr));
  }
}

.form-section {
  border-radius: 0.5rem;
  border: 1px solid rgb(229 231 235);
  background: linear-gradient(180deg, rgb(255 255 255), rgb(249 250 251 / 0.55));
  padding: 1rem;
}

.dark .form-section {
  border-color: rgb(63 63 70);
  background: linear-gradient(180deg, rgb(24 24 27), rgb(39 39 42 / 0.35));
}

.edit-context-panel {
  display: flex;
  min-width: 0;
  flex-direction: column;
  gap: 0.75rem;
  border-radius: 0.5rem;
  border: 1px solid rgb(191 219 254);
  background: linear-gradient(180deg, rgb(239 246 255), rgb(248 250 252));
  padding: 0.875rem 1rem;
}

.edit-context-panel strong,
.edit-context-panel small,
.edit-context-eyebrow {
  display: block;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.edit-context-panel strong {
  color: rgb(17 24 39);
  font-weight: 800;
}

.edit-context-panel small,
.edit-context-eyebrow {
  font-size: 0.75rem;
  color: rgb(75 85 99);
}

.edit-force-badge {
  display: inline-flex;
  width: fit-content;
  align-items: center;
  border-radius: 999px;
  background: rgb(254 226 226);
  padding: 0.375rem 0.625rem;
  color: rgb(185 28 28);
  font-size: 0.75rem;
  font-weight: 800;
  white-space: nowrap;
}

.edit-summary-panel {
  height: fit-content;
  border-radius: 0.5rem;
  border: 1px solid rgb(229 231 235);
  background: rgb(249 250 251);
  padding: 0.875rem;
}

@media (min-width: 640px) {
  .edit-context-panel {
    flex-direction: row;
    align-items: center;
    justify-content: space-between;
  }
}

.dark .edit-context-panel {
  border-color: rgb(59 130 246 / 0.35);
  background: linear-gradient(180deg, rgb(30 41 59 / 0.9), rgb(24 24 27 / 0.82));
}

.dark .edit-context-panel strong {
  color: white;
}

.dark .edit-context-panel small,
.dark .edit-context-eyebrow {
  color: rgb(203 213 225);
}

.dark .edit-force-badge {
  background: rgb(127 29 29 / 0.35);
  color: rgb(254 202 202);
}

.dark .edit-summary-panel {
  border-color: rgb(63 63 70);
  background: rgb(24 24 27 / 0.7);
}

.section-heading {
  margin-bottom: 1rem;
  display: flex;
  flex-direction: column;
  gap: 0.25rem;
}

.section-heading span {
  font-size: 0.875rem;
  font-weight: 700;
  color: rgb(17 24 39);
}

.section-heading small {
  font-size: 0.75rem;
  line-height: 1.125rem;
  color: rgb(107 114 128);
}

.dark .section-heading span {
  color: white;
}

.dark .section-heading small {
  color: rgb(161 161 170);
}

.field {
  display: flex;
  min-width: 0;
  flex-direction: column;
  gap: 0.375rem;
  font-size: 0.8125rem;
  font-weight: 600;
  color: rgb(55 65 81);
}

.field small {
  font-size: 0.75rem;
  font-weight: 400;
  line-height: 1rem;
  color: rgb(107 114 128);
}

.dark .field {
  color: rgb(229 231 235);
}

.dark .field small {
  color: rgb(161 161 170);
}

.input {
  width: 100%;
  border-radius: 0.5rem;
  border: 1px solid rgb(209 213 219);
  background: white;
  padding: 0.5rem 0.75rem;
  font-size: 0.875rem;
  color: rgb(17 24 39);
  outline: none;
}

.input:focus {
  border-color: rgb(14 165 233);
  box-shadow: 0 0 0 3px rgb(14 165 233 / 0.14);
}

.input:disabled {
  cursor: not-allowed;
  background: rgb(249 250 251);
  color: rgb(107 114 128);
}

.dark .input {
  border-color: rgb(63 63 70);
  background: rgb(24 24 27);
  color: white;
}

.dark .input:disabled {
  background: rgb(39 39 42);
  color: rgb(161 161 170);
}

.mode-key-readonly {
  display: inline-flex;
  min-height: 2.25rem;
  min-width: 0;
  align-items: center;
  gap: 0.4375rem;
  border-radius: 0.5rem;
  border: 1px solid rgb(191 219 254);
  background: rgb(239 246 255);
  padding: 0.4375rem 0.625rem;
  font-size: 0.8125rem;
  font-weight: 600;
  color: rgb(30 64 175);
}

.mode-key-readonly span {
  min-width: 0;
  overflow-wrap: anywhere;
}

.dark .mode-key-readonly {
  border-color: rgb(59 130 246 / 0.4);
  background: rgb(30 64 175 / 0.16);
  color: rgb(191 219 254);
}

.listing-model-row {
  display: flex;
  min-width: 0;
  flex-wrap: wrap;
  align-items: center;
  gap: 0.3125rem;
  padding-bottom: 0.125rem;
}

.listing-bottom-bar {
  margin-top: 0.5rem;
  display: grid;
  min-width: 0;
  gap: 0.5rem;
  border-radius: 0.5rem;
  border: 1px solid rgb(226 232 240);
  background: rgb(248 250 252 / 0.86);
  padding: 0.5rem;
}

.model-copy-chip {
  border-radius: 0.375rem;
  background: rgb(239 246 255);
  padding: 0.15625rem 0.40625rem;
  font-size: 0.65625rem;
  font-weight: 600;
  line-height: 0.875rem;
  color: rgb(29 78 216);
  transition: background-color 0.15s ease, color 0.15s ease, box-shadow 0.15s ease;
}

.model-copy-chip:hover {
  background: rgb(219 234 254);
  color: rgb(30 64 175);
}

.model-copy-chip:focus-visible {
  outline: none;
  box-shadow: 0 0 0 3px rgb(59 130 246 / 0.22);
}

.dark .model-copy-chip {
  background: rgb(59 130 246 / 0.12);
  color: rgb(191 219 254);
}

.dark .model-copy-chip:hover {
  background: rgb(59 130 246 / 0.22);
  color: white;
}

.dark .listing-bottom-bar {
  border-color: rgb(63 63 70);
  background: rgb(24 24 27 / 0.58);
}

.model-overflow-wrapper {
  position: relative;
  display: inline-flex;
}

.model-overflow-chip {
  border-radius: 0.375rem;
  background: rgb(243 244 246);
  padding: 0.15625rem 0.40625rem;
  font-size: 0.65625rem;
  font-weight: 700;
  line-height: 0.875rem;
  color: rgb(75 85 99);
  transition: background-color 0.15s ease, color 0.15s ease, box-shadow 0.15s ease;
}

.model-overflow-chip:hover,
.model-overflow-chip:focus-visible {
  background: rgb(229 231 235);
  color: rgb(17 24 39);
  outline: none;
  box-shadow: 0 0 0 3px rgb(107 114 128 / 0.16);
}

.model-overflow-popover {
  pointer-events: none;
  visibility: hidden;
  position: absolute;
  bottom: calc(100% + 0.5rem);
  right: 0;
  z-index: 70;
  display: flex;
  width: max-content;
  max-width: min(24rem, calc(100vw - 2rem));
  flex-wrap: wrap;
  gap: 0.375rem;
  border-radius: 0.5rem;
  border: 1px solid rgb(31 41 55);
  background: rgb(17 24 39);
  padding: 0.625rem;
  opacity: 0;
  box-shadow: 0 18px 38px rgb(15 23 42 / 0.22);
  transform: translateY(0.25rem);
  transition: opacity 0.15s ease, transform 0.15s ease, visibility 0.15s ease;
}

.model-overflow-wrapper:hover .model-overflow-popover,
.model-overflow-wrapper:focus-within .model-overflow-popover {
  pointer-events: auto;
  visibility: visible;
  opacity: 1;
  transform: translateY(0);
}

.model-overflow-model {
  max-width: 100%;
  border-radius: 0.375rem;
  background: rgb(255 255 255 / 0.1);
  padding: 0.25rem 0.5rem;
  font-size: 0.75rem;
  font-weight: 700;
  line-height: 1rem;
  color: white;
}

.model-overflow-model:hover,
.model-overflow-model:focus-visible {
  background: rgb(255 255 255 / 0.2);
  outline: none;
}

.dark .model-overflow-chip {
  background: rgb(39 39 42);
  color: rgb(212 212 216);
}

.dark .model-overflow-chip:hover,
.dark .model-overflow-chip:focus-visible {
  background: rgb(63 63 70);
  color: white;
}

.btn-primary,
.btn-secondary {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  border-radius: 0.5rem;
  padding: 0.5rem 0.875rem;
  font-size: 0.875rem;
  font-weight: 600;
  transition: background-color 0.15s ease, border-color 0.15s ease, color 0.15s ease;
}

.btn-primary {
  background: rgb(2 132 199);
  color: white;
}

.btn-primary:hover {
  background: rgb(3 105 161);
}

.btn-secondary {
  border: 1px solid rgb(209 213 219);
  background: white;
  color: rgb(31 41 55);
}

.btn-secondary:hover {
  background: rgb(249 250 251);
}

.btn-danger-soft {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  border-radius: 0.5rem;
  border: 1px solid rgb(254 202 202);
  background: rgb(254 242 242);
  padding: 0.5rem 0.875rem;
  font-size: 0.875rem;
  font-weight: 600;
  color: rgb(185 28 28);
  transition: background-color 0.15s ease, border-color 0.15s ease, color 0.15s ease;
}

.btn-danger-soft:hover {
  border-color: rgb(252 165 165);
  background: rgb(254 226 226);
}

.dark .btn-secondary {
  border-color: rgb(63 63 70);
  background: rgb(39 39 42);
  color: white;
}

.dark .btn-secondary:hover {
  background: rgb(63 63 70);
}

.dark .btn-danger-soft {
  border-color: rgb(127 29 29 / 0.7);
  background: rgb(127 29 29 / 0.2);
  color: rgb(252 165 165);
}

.dark .btn-danger-soft:hover {
  border-color: rgb(239 68 68 / 0.7);
  background: rgb(127 29 29 / 0.35);
}

.btn-primary:disabled,
.btn-secondary:disabled,
.btn-danger-soft:disabled {
  cursor: not-allowed;
  opacity: 0.6;
}

.filter-panel {
  border-radius: 0.5rem;
  border: 1px solid rgb(226 232 240);
  background: rgb(255 255 255 / 0.96);
  padding: 0.75rem;
  box-shadow: 0 12px 32px rgb(15 23 42 / 0.06);
}

.filter-toolbar {
  display: flex;
  flex-direction: column;
  gap: 0.75rem;
}

.filter-primary-row {
  display: flex;
  min-width: 0;
  flex-direction: column;
  gap: 0.625rem;
}

.filter-search {
  display: flex;
  min-height: 2.5rem;
  min-width: 0;
  align-items: center;
  gap: 0.5rem;
  border-radius: 0.5rem;
  border: 1px solid rgb(203 213 225);
  background: white;
  padding: 0 0.75rem;
  color: rgb(100 116 139);
  transition: border-color 0.15s ease, box-shadow 0.15s ease;
}

.filter-search:focus-within {
  border-color: rgb(14 165 233);
  box-shadow: 0 0 0 3px rgb(14 165 233 / 0.14);
}

.filter-search-input {
  min-width: 0;
  width: 100%;
  border: 0;
  background: transparent;
  font-size: 0.875rem;
  color: rgb(17 24 39);
  outline: none;
}

.filter-search-input::placeholder {
  color: rgb(148 163 184);
}

.filter-actions {
  display: flex;
  min-width: 0;
  width: 100%;
  align-items: center;
  gap: 0.25rem;
  overflow-x: auto;
  border-radius: 0.5rem;
  border: 1px solid rgb(226 232 240);
  background: rgb(248 250 252);
  padding: 0.25rem;
  scrollbar-width: thin;
}

.filter-actions::-webkit-scrollbar {
  height: 6px;
}

.filter-actions::-webkit-scrollbar-thumb {
  border-radius: 999px;
  background: rgb(203 213 225);
}

.filter-chip,
.owner-filter-button {
  display: inline-flex;
  min-height: 2.25rem;
  flex: 0 0 auto;
  align-items: center;
  justify-content: center;
  border-radius: 0.5rem;
  padding: 0.4375rem 0.6875rem;
  font-size: 0.8125rem;
  font-weight: 700;
  white-space: nowrap;
  transition: background-color 0.15s ease, border-color 0.15s ease, color 0.15s ease, box-shadow 0.15s ease;
}

.filter-chip {
  border: 1px solid transparent;
}

.filter-chip-idle {
  color: rgb(51 65 85);
}

.filter-chip-idle:hover {
  background: white;
  box-shadow: 0 1px 2px rgb(15 23 42 / 0.08);
}

.filter-chip-active {
  border-color: rgb(15 23 42);
  background: rgb(15 23 42);
  color: white;
  box-shadow: 0 8px 18px rgb(15 23 42 / 0.18);
}

.filter-divider {
  display: none;
  height: 1.5rem;
  width: 1px;
  flex: 0 0 auto;
  background: rgb(203 213 225);
}

.owner-filter-button {
  gap: 0.5rem;
  border: 1px solid rgb(191 219 254);
  background: rgb(239 246 255);
  color: rgb(30 64 175);
}

.owner-filter-button small {
  border-radius: 9999px;
  background: white;
  padding: 0.125rem 0.5rem;
  font-size: 0.6875rem;
  font-weight: 800;
  color: rgb(37 99 235);
}

.owner-filter-button:hover,
.owner-filter-button-active {
  border-color: rgb(37 99 235);
  background: rgb(37 99 235);
  color: white;
  box-shadow: 0 8px 18px rgb(37 99 235 / 0.2);
}

.owner-filter-button:hover small,
.owner-filter-button-active small {
  background: rgb(255 255 255 / 0.18);
  color: white;
}

.filter-body {
  border-radius: 0.5rem;
  border: 1px solid rgb(226 232 240);
  background: rgb(248 250 252);
  padding: 0.75rem;
}

.filter-body-head {
  display: flex;
  flex-direction: column;
  align-items: stretch;
  gap: 0.75rem;
}

.filter-body-title {
  display: flex;
  min-width: 0;
  align-items: center;
  gap: 0.625rem;
}

.filter-body-icon {
  display: inline-flex;
  height: 1.75rem;
  width: 1.75rem;
  flex-shrink: 0;
  align-items: center;
  justify-content: center;
  border-radius: 0.5rem;
  background: rgb(224 242 254);
  color: rgb(2 132 199);
}

.filter-body-title strong,
.filter-body-title small {
  display: block;
}

.filter-body-title strong {
  color: rgb(15 23 42);
  font-size: 0.875rem;
  font-weight: 800;
}

.filter-body-title small {
  margin-top: 0.0625rem;
  color: rgb(100 116 139);
  font-size: 0.75rem;
  line-height: 1rem;
}

.filter-section-label {
  display: block;
  margin-bottom: 0.375rem;
  font-size: 0.75rem;
  font-weight: 800;
  color: rgb(71 85 105);
}

.sort-option-button,
.filter-trigger-button,
.choice-chip,
.filter-menu-option,
.active-filter-chip {
  cursor: pointer;
}

.advanced-filter-grid {
  margin-top: 0.75rem;
  display: grid;
  grid-template-columns: 1fr;
  gap: 0.75rem;
  align-items: start;
}

.filter-section,
.filter-popover-wrap {
  position: relative;
  min-width: 0;
}

.sort-section {
  margin-top: 0.75rem;
  border-radius: 0.5rem;
  border: 1px solid rgb(226 232 240);
  background: white;
  padding: 0.625rem;
}

.sort-section-head {
  display: flex;
  align-items: center;
  justify-content: flex-start;
  gap: 0.75rem;
  margin-bottom: 0.5rem;
}

.sort-section-head .filter-section-label {
  margin-bottom: 0;
}

.sort-button-grid {
  display: flex;
  flex-wrap: wrap;
  gap: 0.375rem;
}

.sort-option-button {
  display: inline-flex;
  min-height: 2.25rem;
  min-width: 0;
  align-items: center;
  justify-content: flex-start;
  gap: 0.375rem;
  border-radius: 0.5rem;
  border: 1px solid rgb(226 232 240);
  background: rgb(248 250 252);
  padding: 0.375rem 0.5625rem;
  font-size: 0.8125rem;
  font-weight: 800;
  color: rgb(51 65 85);
  white-space: nowrap;
  transition: background-color 0.15s ease, border-color 0.15s ease, color 0.15s ease, box-shadow 0.15s ease;
}

.sort-default-button {
  flex: 0 0 auto;
}

.sort-field-button {
  flex: 0 1 auto;
  max-width: 100%;
}

.sort-option-button > svg {
  flex: 0 0 auto;
}

.sort-option-button span {
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
}

.sort-option-button:hover {
  border-color: rgb(147 197 253);
  background: rgb(239 246 255);
  color: rgb(29 78 216);
}

.sort-option-active {
  border-color: rgb(37 99 235);
  background: rgb(37 99 235);
  color: white;
  box-shadow: 0 8px 18px rgb(37 99 235 / 0.18);
}

.sort-option-check {
  margin-left: auto;
  flex: 0 0 auto;
}

.sort-priority-badge {
  display: inline-flex;
  flex: 0 0 auto;
  align-items: center;
  border-radius: 999px;
  background: rgb(226 232 240);
  padding: 0.0625rem 0.3125rem;
  color: rgb(71 85 105);
  font-size: 0.6875rem;
  font-weight: 900;
  line-height: 1rem;
}

.sort-direction-pill {
  display: inline-flex;
  flex: 0 0 auto;
  align-items: center;
  border-radius: 999px;
  border: 1px solid rgb(191 219 254);
  background: rgb(239 246 255);
  padding: 0.0625rem 0.375rem;
  color: rgb(29 78 216);
  font-size: 0.6875rem;
  font-weight: 800;
  line-height: 1rem;
}

.sort-option-active .sort-direction-pill {
  border-color: rgb(255 255 255 / 0.32);
  background: rgb(255 255 255 / 0.16);
  color: white;
}

.sort-option-active .sort-priority-badge {
  background: rgb(255 255 255 / 0.2);
  color: white;
}

.filter-trigger-button {
  display: flex;
  min-height: 2.5rem;
  width: 100%;
  min-width: 0;
  align-items: center;
  gap: 0.5rem;
  border-radius: 0.5rem;
  border: 1px solid rgb(203 213 225);
  background: white;
  padding: 0.5rem 0.625rem;
  font-size: 0.8125rem;
  font-weight: 800;
  color: rgb(31 41 55);
  transition: border-color 0.15s ease, box-shadow 0.15s ease, background-color 0.15s ease, color 0.15s ease;
}

.filter-trigger-button span {
  min-width: 0;
  flex: 1 1 auto;
  overflow: hidden;
  text-align: left;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.filter-trigger-button:hover,
.filter-trigger-active {
  border-color: rgb(14 165 233);
  box-shadow: 0 0 0 3px rgb(14 165 233 / 0.14);
}

.filter-trigger-selected {
  border-color: rgb(59 130 246);
  background: rgb(239 246 255);
  color: rgb(29 78 216);
}

.filter-trigger-chevron {
  flex: 0 0 auto;
}

.filter-popover {
  position: absolute;
  top: calc(100% + 0.5rem);
  right: 0;
  z-index: 90;
  width: min(22rem, calc(100vw - 2rem));
  border-radius: 0.5rem;
  border: 1px solid rgb(226 232 240);
  background: white;
  padding: 0.5rem;
  box-shadow: 0 22px 48px rgb(15 23 42 / 0.18);
}

.seat-popover {
  width: min(17rem, calc(100vw - 2rem));
}

.model-popover {
  width: min(28rem, calc(100vw - 2rem));
}

.seat-chip-grid {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 0.375rem;
}

.choice-chip {
  min-height: 2.25rem;
  border-radius: 0.5rem;
  border: 1px solid rgb(226 232 240);
  background: rgb(248 250 252);
  padding: 0.375rem 0.5rem;
  font-size: 0.8125rem;
  font-weight: 800;
  color: rgb(51 65 85);
  transition: background-color 0.15s ease, border-color 0.15s ease, color 0.15s ease;
}

.choice-chip:hover {
  border-color: rgb(147 197 253);
  background: rgb(239 246 255);
}

.choice-chip-active {
  border-color: rgb(37 99 235);
  background: rgb(37 99 235);
  color: white;
}

.model-filter-options {
  display: grid;
  max-height: 15rem;
  gap: 0.25rem;
  overflow-y: auto;
  padding-right: 0.25rem;
}

.filter-menu-option {
  display: flex;
  min-height: 2.25rem;
  width: 100%;
  align-items: center;
  justify-content: space-between;
  gap: 0.5rem;
  border-radius: 0.5rem;
  padding: 0.375rem 0.5rem;
  font-size: 0.8125rem;
  font-weight: 700;
  color: rgb(55 65 81);
  text-align: left;
  transition: background-color 0.15s ease, color 0.15s ease;
}

.filter-menu-option:hover {
  background: rgb(248 250 252);
}

.filter-menu-option-active {
  background: rgb(239 246 255);
  color: rgb(29 78 216);
}

.filter-menu-option span {
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.model-filter-input-row {
  margin-top: 0.625rem;
  display: grid;
  grid-template-columns: minmax(0, 1fr) auto;
  gap: 0.5rem;
}

.active-filter-row {
  margin-top: 0.75rem;
  display: flex;
  flex-wrap: wrap;
  gap: 0.375rem;
}

.active-filter-chip {
  display: inline-flex;
  min-height: 2rem;
  min-width: 0;
  align-items: center;
  gap: 0.375rem;
  border-radius: 0.5rem;
  border: 1px solid rgb(191 219 254);
  background: rgb(239 246 255);
  padding: 0.3125rem 0.5rem;
  font-size: 0.75rem;
  font-weight: 800;
  color: rgb(29 78 216);
  transition: background-color 0.15s ease, border-color 0.15s ease;
}

.active-filter-chip:hover {
  border-color: rgb(96 165 250);
  background: rgb(219 234 254);
}

.active-filter-chip span {
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.filter-button-row {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 0.5rem;
}

.filter-apply-button,
.filter-reset-button {
  display: inline-flex;
  min-height: 2.25rem;
  align-items: center;
  justify-content: center;
  gap: 0.375rem;
  border-radius: 0.5rem;
  padding: 0.4375rem 0.75rem;
  font-size: 0.8125rem;
  font-weight: 800;
  transition: background-color 0.15s ease, border-color 0.15s ease, color 0.15s ease, box-shadow 0.15s ease;
}

.filter-apply-button {
  background: rgb(2 132 199);
  color: white;
  box-shadow: 0 10px 20px rgb(2 132 199 / 0.18);
}

.filter-apply-button:hover {
  background: rgb(3 105 161);
}

.filter-reset-button {
  border: 1px solid rgb(203 213 225);
  background: white;
  color: rgb(51 65 85);
}

.filter-reset-button:hover {
  border-color: rgb(148 163 184);
  background: rgb(248 250 252);
}

.filter-apply-button:disabled,
.filter-reset-button:disabled {
  cursor: not-allowed;
  opacity: 0.55;
  box-shadow: none;
}

@media (max-width: 639px) {
  .filter-search,
  .filter-chip,
  .owner-filter-button,
  .sort-option-button,
  .filter-trigger-button,
  .filter-apply-button,
  .filter-reset-button {
    min-height: 2.75rem;
  }
}

.dark .filter-panel {
  border-color: rgb(63 63 70);
  background: rgb(24 24 27 / 0.94);
  box-shadow: 0 10px 26px rgb(0 0 0 / 0.22);
}

.dark .filter-search,
.dark .sort-section,
.dark .sort-option-button,
.dark .filter-trigger-button,
.dark .filter-reset-button {
  border-color: rgb(63 63 70);
  background: rgb(24 24 27);
  color: white;
}

.dark .filter-search-input {
  color: white;
}

.dark .filter-actions,
.dark .filter-body {
  border-color: rgb(63 63 70);
  background: rgb(39 39 42 / 0.65);
}

.dark .filter-chip-idle {
  color: rgb(244 244 245);
}

.dark .filter-chip-idle:hover {
  background: rgb(63 63 70);
}

.dark .filter-chip-active {
  border-color: white;
  background: white;
  color: rgb(17 24 39);
}

.dark .filter-divider {
  background: rgb(63 63 70);
}

.dark .owner-filter-button {
  border-color: rgb(59 130 246 / 0.35);
  background: rgb(30 64 175 / 0.16);
  color: rgb(147 197 253);
}

.dark .owner-filter-button small {
  background: rgb(30 41 59 / 0.9);
  color: rgb(191 219 254);
}

.dark .owner-filter-button:hover,
.dark .owner-filter-button-active {
  border-color: rgb(96 165 250);
  background: rgb(37 99 235);
  color: white;
}

.dark .filter-body-title strong,
.dark .filter-section-label {
  color: white;
}

.dark .filter-body-title small {
  color: rgb(161 161 170);
}

.dark .filter-body-icon {
  background: rgb(14 165 233 / 0.16);
  color: rgb(125 211 252);
}

.dark .sort-option-button:hover,
.dark .filter-menu-option:hover {
  background: rgb(63 63 70);
  color: white;
}

.dark .sort-option-active {
  border-color: rgb(96 165 250);
  background: rgb(37 99 235);
  color: white;
}

.dark .filter-trigger-selected,
.dark .filter-menu-option-active,
.dark .active-filter-chip {
  border-color: rgb(59 130 246 / 0.35);
  background: rgb(30 64 175 / 0.2);
  color: rgb(191 219 254);
}

.dark .filter-popover {
  border-color: rgb(63 63 70);
  background: rgb(24 24 27);
}

.dark .filter-menu-option,
.dark .choice-chip,
.dark .sort-option-button {
  color: rgb(229 231 235);
}

.dark .choice-chip {
  border-color: rgb(63 63 70);
  background: rgb(39 39 42);
}

.dark .choice-chip-active {
  border-color: rgb(96 165 250);
  background: rgb(37 99 235);
  color: white;
}

.dark .sort-priority-badge {
  background: rgb(63 63 70);
  color: rgb(212 212 216);
}

.dark .sort-option-active .sort-priority-badge {
  background: rgb(255 255 255 / 0.2);
  color: white;
}

.dark .filter-reset-button:hover {
  border-color: rgb(82 82 91);
  background: rgb(39 39 42);
}

@media (max-width: 640px) {
  .filter-popover {
    left: 0;
    right: auto;
    width: min(100%, calc(100vw - 1.5rem));
  }

  .model-filter-input-row {
    grid-template-columns: 1fr;
  }
}

@media (min-width: 640px) {
  .filter-button-row {
    grid-template-columns: minmax(7rem, 8rem) minmax(7rem, 8rem);
    justify-content: end;
  }
}

@media (min-width: 768px) {
  .advanced-filter-grid {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }

  .filter-actions {
    flex-wrap: wrap;
    overflow: visible;
  }

  .filter-divider {
    display: block;
  }
}

@media (min-width: 1024px) {
  .filter-primary-row {
    flex-direction: row;
    align-items: center;
    justify-content: space-between;
  }

  .filter-search {
    max-width: 28rem;
    flex: 1 1 22rem;
  }

  .filter-actions {
    width: auto;
    justify-content: flex-end;
  }

  .advanced-filter-grid {
    grid-template-columns: repeat(5, minmax(9.5rem, 1fr));
  }

  .filter-body-head {
    flex-direction: row;
    align-items: center;
    justify-content: space-between;
  }
}

@media (min-width: 1536px) {
  .advanced-filter-grid {
    grid-template-columns: repeat(5, minmax(12rem, 1fr));
    align-items: end;
  }
}

.toggle-row {
  display: flex;
  align-items: flex-start;
  gap: 0.75rem;
  border-radius: 0.5rem;
  border: 1px solid rgb(229 231 235);
  padding: 0.75rem;
  color: rgb(55 65 81);
}

.toggle-row input {
  margin-top: 0.125rem;
  height: 1rem;
  width: 1rem;
  border-radius: 0.25rem;
  border-color: rgb(209 213 219);
  color: rgb(2 132 199);
}

.toggle-row span {
  display: flex;
  flex-direction: column;
  gap: 0.125rem;
  font-size: 0.875rem;
}

.toggle-row strong {
  color: rgb(17 24 39);
}

.toggle-row small {
  font-size: 0.75rem;
  color: rgb(107 114 128);
}

.dark .toggle-row {
  border-color: rgb(63 63 70);
  color: rgb(229 231 235);
}

.dark .toggle-row strong {
  color: white;
}

.dark .toggle-row small {
  color: rgb(161 161 170);
}

.model-selector-shell {
  border-radius: 0.5rem;
}

.model-selector-shell :deep(.relative.mb-3) {
  margin-bottom: 0.75rem;
}

.model-selector-shell :deep(.cursor-pointer) {
  min-height: 8.5rem;
  border-color: rgb(209 213 219);
  background: white;
}

.dark .model-selector-shell :deep(.cursor-pointer) {
  border-color: rgb(63 63 70);
  background: rgb(24 24 27);
}

.notice-row {
  display: flex;
  gap: 0.5rem;
  border-radius: 0.5rem;
  border: 1px solid rgb(191 219 254);
  background: rgb(239 246 255);
  padding: 0.75rem;
  font-size: 0.8125rem;
  line-height: 1.25rem;
  color: rgb(30 64 175);
}

.dark .notice-row {
  border-color: rgb(30 64 175 / 0.65);
  background: rgb(30 64 175 / 0.12);
  color: rgb(191 219 254);
}

.proxy-action-option {
  display: flex;
  min-height: 3.75rem;
  align-items: center;
  gap: 0.75rem;
  border-radius: 0.5rem;
  border: 1px solid rgb(229 231 235);
  background: white;
  padding: 0.625rem 0.75rem;
  text-align: left;
  transition: border-color 0.15s ease, background-color 0.15s ease, transform 0.15s ease;
}

.proxy-action-option:hover {
  border-color: rgb(125 211 252);
  background: rgb(240 249 255);
}

.proxy-action-option strong,
.proxy-action-option small {
  display: block;
}

.proxy-action-option strong {
  font-size: 0.8125rem;
  color: rgb(17 24 39);
}

.proxy-action-option small {
  margin-top: 0.125rem;
  font-size: 0.75rem;
  color: rgb(107 114 128);
}

.proxy-action-icon {
  display: inline-flex;
  height: 2.25rem;
  width: 2.25rem;
  flex-shrink: 0;
  align-items: center;
  justify-content: center;
  border-radius: 0.5rem;
}

.dark .proxy-action-option {
  border-color: rgb(63 63 70);
  background: rgb(39 39 42);
}

.dark .proxy-action-option:hover {
  border-color: rgb(14 165 233 / 0.65);
  background: rgb(12 74 110 / 0.18);
}

.dark .proxy-action-option strong {
  color: white;
}

.dark .proxy-action-option small {
  color: rgb(161 161 170);
}

.proxy-dialog-section {
  display: flex;
  min-width: 0;
  flex-direction: column;
  gap: 0.625rem;
}

.proxy-dialog-label {
  font-size: 0.9375rem;
  font-weight: 700;
  color: rgb(17 24 39);
}

.dark .proxy-dialog-label {
  color: white;
}

.proxy-smart-textarea {
  min-height: 7.25rem;
  width: 100%;
  resize: vertical;
  border-radius: 0.5rem;
  border: 1px solid rgb(203 213 225);
  background: white;
  padding: 0.875rem 1rem;
  font-size: 0.9375rem;
  line-height: 1.65;
  color: rgb(17 24 39);
  outline: none;
}

.proxy-smart-textarea:focus {
  border-color: rgb(14 165 233);
  box-shadow: 0 0 0 3px rgb(14 165 233 / 0.14);
}

.dark .proxy-smart-textarea {
  border-color: rgb(63 63 70);
  background: rgb(24 24 27);
  color: white;
}

.proxy-dialog-divider {
  height: 1px;
  background: rgb(203 213 225);
}

.dark .proxy-dialog-divider {
  background: rgb(63 63 70);
}

.proxy-ip-type-grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 0.75rem;
}

.proxy-ip-type-option {
  display: inline-flex;
  min-height: 3.5rem;
  align-items: center;
  gap: 0.625rem;
  border-radius: 0.5rem;
  border: 1px solid rgb(203 213 225);
  background: white;
  padding: 0.75rem 1rem;
  font-size: 1rem;
  font-weight: 700;
  color: rgb(55 65 81);
}

.proxy-ip-type-option-active {
  border-color: rgb(59 130 246);
  color: rgb(37 99 235);
  box-shadow: 0 0 0 3px rgb(59 130 246 / 0.12);
}

.proxy-radio-dot {
  height: 1.125rem;
  width: 1.125rem;
  border-radius: 9999px;
  border: 1px solid rgb(203 213 225);
  background: white;
  box-shadow: inset 0 0 0 0.25rem white;
}

.proxy-ip-type-option-active .proxy-radio-dot {
  border-color: rgb(59 130 246);
  background: rgb(59 130 246);
}

.dark .proxy-ip-type-option {
  border-color: rgb(63 63 70);
  background: rgb(24 24 27);
  color: rgb(229 231 235);
}

.dark .proxy-ip-type-option-active {
  border-color: rgb(96 165 250);
  color: rgb(147 197 253);
}

.proxy-endpoint-row {
  display: grid;
  grid-template-columns: minmax(7.5rem, 10rem) minmax(0, 1fr) auto minmax(6rem, 8rem);
  align-items: center;
  overflow: hidden;
  border-radius: 0.5rem;
  border: 1px solid rgb(203 213 225);
  background: white;
}

.proxy-protocol-select,
.proxy-host-input,
.proxy-port-input {
  min-width: 0;
  border: 0;
  background: transparent;
  padding: 0.875rem 1rem;
  font-size: 0.9375rem;
  color: rgb(17 24 39);
  outline: none;
}

.proxy-protocol-select {
  border-right: 1px solid rgb(229 231 235);
  font-weight: 600;
}

.proxy-colon {
  color: rgb(107 114 128);
}

.dark .proxy-endpoint-row {
  border-color: rgb(63 63 70);
  background: rgb(24 24 27);
}

.dark .proxy-protocol-select,
.dark .proxy-host-input,
.dark .proxy-port-input {
  color: white;
}

.dark .proxy-protocol-select {
  border-color: rgb(63 63 70);
}

@media (max-width: 640px) {
  .proxy-ip-type-grid {
    grid-template-columns: 1fr;
  }

  .proxy-endpoint-row {
    grid-template-columns: 1fr;
  }

  .proxy-protocol-select {
    border-right: 0;
    border-bottom: 1px solid rgb(229 231 235);
  }

  .proxy-colon {
    display: none;
  }
}

.compact-metric {
  border-radius: 0.5rem;
  background: white;
  padding: 0.625rem;
}

.compact-metric span {
  display: block;
  font-size: 0.75rem;
  color: rgb(107 114 128);
}

.compact-metric strong {
  margin-top: 0.125rem;
  display: block;
  color: rgb(17 24 39);
}

.dark .compact-metric {
  background: rgb(39 39 42);
}

.dark .compact-metric span {
  color: rgb(161 161 170);
}

.dark .compact-metric strong {
  color: white;
}

.listing-grid {
  display: grid;
  grid-template-columns: minmax(0, 1fr);
  gap: 0.875rem;
  align-items: start;
}

@media (min-width: 1120px) {
  .listing-grid {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }
}

.listing-card {
  container: account-listing-card / inline-size;
  position: relative;
  display: flex;
  min-width: 0;
  flex-direction: column;
  border-radius: 0.5rem;
  border: 1px solid rgb(226 232 240);
  background:
    linear-gradient(180deg, rgb(255 255 255), rgb(248 250 252 / 0.92)),
    radial-gradient(circle at 14% 0%, rgb(14 165 233 / 0.08), transparent 28%);
  padding: 0.6875rem 0.75rem 0.75rem;
  box-shadow: 0 10px 24px rgb(15 23 42 / 0.05);
  transition: border-color 0.15s ease, box-shadow 0.15s ease, transform 0.15s ease;
}

.listing-card::before {
  content: "";
  position: absolute;
  inset: -1px -1px auto;
  height: 0.1875rem;
  border-radius: 0.5rem 0.5rem 0 0;
  background: linear-gradient(90deg, rgb(56 189 248), rgb(45 212 191), rgb(251 191 36));
}

.listing-card:hover {
  border-color: rgb(186 230 253);
  box-shadow: 0 16px 34px rgb(15 23 42 / 0.09);
  transform: translateY(-1px);
}

.listing-card-head {
  display: flex;
  min-width: 0;
  flex-direction: column;
  gap: 0.5rem;
}

.listing-card-identity {
  min-width: 0;
}

.listing-badge-row {
  display: flex;
  min-width: 0;
  flex-wrap: wrap;
  align-items: center;
  gap: 0.375rem;
}

.listing-title-row {
  margin-top: 0.4375rem;
  display: flex;
  min-width: 0;
  flex-direction: column;
  gap: 0.1875rem;
}

.listing-title {
  color: rgb(17 24 39);
  font-size: 1rem;
  font-weight: 800;
  line-height: 1.3125rem;
  overflow-wrap: anywhere;
}

.listing-owner {
  color: rgb(107 114 128);
  font-size: 0.78125rem;
  font-weight: 600;
  line-height: 1rem;
  overflow-wrap: anywhere;
}

.owner-inline-button {
  margin-left: 0.375rem;
  display: inline-flex;
  min-height: 1.5rem;
  align-items: center;
  gap: 0.25rem;
  border-radius: 0.375rem;
  border: 1px solid rgb(191 219 254);
  background: rgb(239 246 255);
  padding: 0.125rem 0.375rem;
  color: rgb(30 64 175);
  font-size: 0.71875rem;
  font-weight: 800;
  line-height: 1rem;
  vertical-align: baseline;
  transition: border-color 0.15s ease, background-color 0.15s ease, color 0.15s ease;
}

.owner-inline-button:hover {
  border-color: rgb(37 99 235);
  background: rgb(37 99 235);
  color: white;
}

.owner-inline-button svg {
  flex-shrink: 0;
}

.listing-card-state {
  display: flex;
  flex-shrink: 0;
  flex-wrap: wrap;
  align-items: center;
  gap: 0.375rem;
}

.listing-rating-pill {
  display: inline-flex;
  min-height: 1.75rem;
  align-items: center;
  gap: 0.25rem;
  border-radius: 0.5rem;
  border: 1px solid rgb(191 219 254);
  background: rgb(239 246 255 / 0.82);
  padding: 0.1875rem 0.5rem;
  color: rgb(30 64 175);
  font-size: 0.71875rem;
  font-weight: 800;
  line-height: 1rem;
  white-space: nowrap;
}

.listing-rating-pill svg {
  flex-shrink: 0;
  color: rgb(79 70 229);
}

.listing-rating-pill strong {
  color: rgb(17 24 39);
  font-size: 0.78125rem;
  font-weight: 900;
}

.listing-seat-pill {
  display: inline-flex;
  min-height: 1.75rem;
  align-items: center;
  border-radius: 0.5rem;
  background: rgb(239 246 255);
  padding: 0.1875rem 0.5625rem;
  color: rgb(30 64 175);
  font-size: 0.78125rem;
  font-weight: 800;
}

@media (min-width: 768px) {
  .listing-card {
    padding: 0.75rem 0.8125rem 0.8125rem;
  }

  .listing-card-head {
    flex-direction: row;
    align-items: flex-start;
    justify-content: space-between;
  }

  .listing-title-row {
    flex-direction: row;
    flex-wrap: wrap;
    align-items: baseline;
    gap: 0.5rem 1.25rem;
  }

  .listing-card-state {
    justify-content: flex-end;
  }
}

.dark .listing-card {
  border-color: rgb(63 63 70);
  background:
    linear-gradient(180deg, rgb(24 24 27), rgb(39 39 42 / 0.56)),
    radial-gradient(circle at 16% 0%, rgb(14 165 233 / 0.14), transparent 30%);
  box-shadow: 0 14px 32px rgb(0 0 0 / 0.26);
}

.dark .listing-card:hover {
  border-color: rgb(14 165 233 / 0.5);
  box-shadow: 0 20px 42px rgb(0 0 0 / 0.32);
}

.dark .listing-title {
  color: white;
}

.dark .listing-owner {
  color: rgb(161 161 170);
}

.dark .owner-inline-button {
  border-color: rgb(59 130 246 / 0.35);
  background: rgb(59 130 246 / 0.14);
  color: rgb(191 219 254);
}

.dark .owner-inline-button:hover {
  border-color: rgb(96 165 250 / 0.65);
  background: rgb(59 130 246 / 0.42);
  color: white;
}

.dark .listing-rating-pill {
  border-color: rgb(59 130 246 / 0.35);
  background: rgb(30 64 175 / 0.18);
  color: rgb(191 219 254);
}

.dark .listing-rating-pill svg {
  color: rgb(165 180 252);
}

.dark .listing-rating-pill strong {
  color: white;
}

.dark .listing-seat-pill {
  background: rgb(59 130 246 / 0.12);
  color: rgb(191 219 254);
}

.account-level-badge {
  display: inline-flex;
  min-height: 1.5rem;
  align-items: center;
  border-radius: 999px;
  border: 1px solid transparent;
  padding: 0.1875rem 0.5rem;
  font-size: 0.6875rem;
  font-weight: 800;
  line-height: 1rem;
  letter-spacing: 0;
  white-space: nowrap;
}

.account-level-pro {
  border-color: rgb(245 158 11 / 0.55);
  background: linear-gradient(180deg, rgb(254 240 138), rgb(217 119 6));
  color: rgb(69 26 3);
  box-shadow: inset 0 1px 0 rgb(255 255 255 / 0.5), 0 6px 14px rgb(217 119 6 / 0.18);
}

.account-level-team {
  border-color: rgb(20 184 166 / 0.45);
  background: linear-gradient(180deg, rgb(204 251 241), rgb(20 184 166));
  color: rgb(19 78 74);
}

.account-level-k12 {
  border-color: rgb(14 165 233 / 0.45);
  background: linear-gradient(180deg, rgb(224 242 254), rgb(14 165 233));
  color: rgb(12 74 110);
}

.account-level-plus {
  border-color: rgb(99 102 241 / 0.35);
  background: rgb(238 242 255);
  color: rgb(67 56 202);
}

.account-level-free {
  border-color: rgb(34 197 94 / 0.3);
  background: rgb(220 252 231);
  color: rgb(21 128 61);
}

.account-level-unknown {
  border-color: rgb(209 213 219);
  background: rgb(243 244 246);
  color: rgb(75 85 99);
}

.feature-badge {
  display: inline-flex;
  min-height: 1.5rem;
  align-items: center;
  border-radius: 999px;
  padding: 0.25rem 0.5rem;
  font-size: 0.6875rem;
  font-weight: 700;
  line-height: 0.875rem;
  white-space: nowrap;
}

.feature-badge-image {
  background: rgb(236 253 245);
  color: rgb(4 120 87);
}

.feature-badge-client-only {
  background: rgb(239 246 255);
  color: rgb(29 78 216);
}

.feature-badge-waiver {
  background: rgb(255 247 237);
  color: rgb(194 65 12);
}

.listing-health-panel {
  margin-top: 0.625rem;
  display: grid;
  gap: 0.375rem;
  border-radius: 0.5rem;
  border: 1px solid rgb(226 232 240);
  background: rgb(248 250 252 / 0.72);
  padding: 0.4375rem;
}

.listing-health-grid {
  display: grid;
  min-width: 0;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 0.375rem;
  align-items: stretch;
}

.listing-status-stack,
.listing-usage-grid {
  display: contents;
}

@container account-listing-card (min-width: 41rem) {
  .listing-health-grid {
    grid-template-columns: repeat(4, minmax(0, 1fr));
  }
}

.listing-runtime-tile {
  display: grid;
  min-width: 0;
  grid-template-columns: auto minmax(0, 1fr);
  align-items: start;
  gap: 0.4375rem;
  border-radius: 0.5rem;
  background: white;
  padding: 0.5rem;
}

.listing-runtime-tile > svg {
  color: rgb(107 114 128);
}

.listing-runtime-label {
  display: block;
  font-size: 0.6875rem;
  font-weight: 700;
  color: rgb(107 114 128);
}

.listing-runtime-value-row {
  margin-top: 0.1875rem;
  display: flex;
  min-width: 0;
  flex-wrap: wrap;
  align-items: center;
  gap: 0.375rem;
}

.listing-runtime-value-row strong {
  display: block;
  color: rgb(17 24 39);
  font-size: 0.9375rem;
  font-weight: 800;
  line-height: 1.125rem;
  overflow-wrap: anywhere;
}

.listing-runtime-tile p {
  margin-top: 0.125rem;
  font-size: 0.6875rem;
  line-height: 1rem;
  color: rgb(107 114 128);
  overflow-wrap: anywhere;
}

.runtime-badge {
  display: inline-flex;
  flex-shrink: 0;
  align-items: center;
  border-radius: 999px;
  padding: 0.1875rem 0.5rem;
  font-size: 0.6875rem;
  font-weight: 700;
  white-space: nowrap;
}

.runtime-badge-normal {
  background: rgb(209 250 229);
  color: rgb(4 120 87);
}

.runtime-badge-warning {
  background: rgb(254 243 199);
  color: rgb(180 83 9);
}

.runtime-badge-danger {
  background: rgb(254 226 226);
  color: rgb(185 28 28);
}

.runtime-badge-muted {
  background: rgb(243 244 246);
  color: rgb(75 85 99);
}

.usage-window-row {
  display: grid;
  min-width: 0;
  gap: 0.1875rem;
  border-radius: 0.5rem;
  background: white;
  padding: 0.5rem;
}

.usage-window-title {
  display: flex;
  min-width: 0;
  align-items: center;
  gap: 0.375rem;
  font-size: 0.6875rem;
  color: rgb(75 85 99);
}

.usage-window-title span {
  min-width: 0;
  line-height: 1rem;
  overflow-wrap: anywhere;
}

.usage-window-title strong {
  margin-left: auto;
  color: rgb(17 24 39);
  font-weight: 800;
  line-height: 0.9375rem;
  text-align: right;
}

.usage-empty {
  font-size: 0.75rem;
  color: rgb(156 163 175);
}

.capacity-panel {
  display: grid;
  gap: 0.1875rem;
  align-self: stretch;
  align-content: start;
  border-radius: 0.5rem;
  background: white;
  padding: 0.5rem;
  font-size: 0.65625rem;
  color: rgb(75 85 99);
}

.capacity-panel span {
  display: inline-flex;
  min-width: 0;
  align-items: center;
  gap: 0.375rem;
  font-size: 0.6875rem;
  font-weight: 700;
}

.capacity-panel strong {
  color: rgb(17 24 39);
  font-weight: 800;
  overflow-wrap: anywhere;
}

.capacity-track {
  height: 0.25rem;
  overflow: hidden;
  border-radius: 999px;
  background: rgb(229 231 235);
}

.capacity-fill {
  height: 100%;
  border-radius: inherit;
  transition: width 180ms ease;
}

.capacity-fill-normal {
  background: rgb(34 197 94);
}

.capacity-fill-warning {
  background: rgb(245 158 11);
}

.capacity-fill-danger {
  background: rgb(239 68 68);
}

.validity-strip {
  display: flex;
  min-width: 0;
  align-items: center;
  justify-content: space-between;
  gap: 0.625rem;
  border-radius: 0.5rem;
  border: 1px solid rgb(167 243 208);
  background: rgb(236 253 245);
  padding: 0.4375rem 0.5625rem;
  color: rgb(6 95 70);
  font-size: 0.75rem;
  font-weight: 700;
}

.validity-strip span,
.validity-strip strong {
  overflow-wrap: anywhere;
}

.validity-strip strong {
  font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
  font-weight: 600;
  text-align: right;
}

.listing-health-foot {
  display: flex;
  flex-wrap: wrap;
  gap: 0.25rem 0.75rem;
  border-top: 1px solid rgb(226 232 240);
  padding-top: 0.25rem;
  font-size: 0.65625rem;
  color: rgb(107 114 128);
}

.listing-health-foot-empty {
  display: none;
}

.dark .account-level-plus {
  border-color: rgb(129 140 248 / 0.35);
  background: rgb(49 46 129 / 0.4);
  color: rgb(199 210 254);
}

.dark .account-level-k12 {
  border-color: rgb(56 189 248 / 0.35);
  background: rgb(7 89 133 / 0.35);
  color: rgb(186 230 253);
}

.dark .account-level-free {
  border-color: rgb(74 222 128 / 0.25);
  background: rgb(20 83 45 / 0.35);
  color: rgb(187 247 208);
}

.dark .account-level-unknown,
.dark .runtime-badge-muted {
  border-color: rgb(63 63 70);
  background: rgb(39 39 42);
  color: rgb(212 212 216);
}

.dark .feature-badge-image {
  background: rgb(6 95 70 / 0.25);
  color: rgb(167 243 208);
}

.dark .feature-badge-client-only {
  background: rgb(30 64 175 / 0.25);
  color: rgb(191 219 254);
}

.dark .feature-badge-waiver {
  background: rgb(154 52 18 / 0.25);
  color: rgb(253 186 116);
}

.dark .listing-health-panel {
  border-color: rgb(63 63 70);
  background: rgb(24 24 27 / 0.62);
}

.dark .listing-runtime-label,
.dark .listing-runtime-tile p,
.dark .usage-window-title,
.dark .listing-health-foot {
  color: rgb(161 161 170);
}

.dark .listing-health-foot {
  border-color: rgb(63 63 70);
}

.dark .listing-runtime-tile,
.dark .usage-window-row {
  background: rgb(39 39 42 / 0.45);
}

.dark .capacity-panel {
  background: rgb(39 39 42 / 0.45);
  color: rgb(161 161 170);
}

.dark .listing-runtime-value-row strong,
.dark .usage-window-title strong,
.dark .capacity-panel strong {
  color: white;
}

.dark .runtime-badge-normal {
  background: rgb(6 95 70 / 0.25);
  color: rgb(167 243 208);
}

.dark .runtime-badge-warning {
  background: rgb(146 64 14 / 0.25);
  color: rgb(253 230 138);
}

.dark .runtime-badge-danger {
  background: rgb(127 29 29 / 0.25);
  color: rgb(254 202 202);
}

.dark .capacity-track {
  background: rgb(63 63 70);
}

.dark .validity-strip {
  border-color: rgb(16 185 129 / 0.28);
  background: rgb(6 95 70 / 0.18);
  color: rgb(167 243 208);
}

.account-share-membership-panel {
  margin-top: 0.625rem;
  border-radius: 0.5rem;
  border: 1px solid rgb(187 247 208);
  background: linear-gradient(180deg, rgb(240 253 244), rgb(236 253 245 / 0.78));
  padding: 0.625rem;
  color: rgb(22 101 52);
  font-size: 0.875rem;
}

.membership-status-head {
  display: flex;
  min-width: 0;
  align-items: flex-start;
  justify-content: space-between;
  gap: 0.75rem;
}

.membership-status-head > div {
  min-width: 0;
}

.membership-title {
  color: rgb(20 83 45);
  font-size: 0.875rem;
  font-weight: 800;
  line-height: 1.25rem;
  overflow-wrap: anywhere;
}

.membership-subtitle {
  margin-top: 0.125rem;
  color: rgb(21 128 61);
  font-size: 0.75rem;
  font-weight: 600;
  line-height: 1rem;
  overflow-wrap: anywhere;
}

.membership-status-pill {
  display: inline-flex;
  flex-shrink: 0;
  align-items: center;
  border-radius: 0.5rem;
  background: rgb(16 185 129);
  padding: 0.25rem 0.625rem;
  color: white;
  font-size: 0.75rem;
  font-weight: 700;
  white-space: nowrap;
}

.membership-status-pill-queued {
  background: rgb(37 99 235);
}

.membership-status-pill-waiting {
  background: rgb(245 158 11);
  color: rgb(120 53 15);
}

.membership-compact-body {
  margin-top: 0.5rem;
  display: grid;
  gap: 0.5rem;
}

.membership-main {
  display: grid;
  min-width: 0;
  gap: 0.5rem;
}

.membership-detail-grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 0.375rem;
}

.membership-detail-grid div {
  min-width: 0;
  border-radius: 0.5rem;
  border: 1px solid rgb(187 247 208 / 0.72);
  background: rgb(255 255 255 / 0.68);
  padding: 0.4375rem 0.5rem;
}

.membership-detail-grid span,
.idle-timeout-control label,
.idle-timeout-join span {
  display: block;
  color: rgb(5 150 105);
  font-size: 0.75rem;
  font-weight: 600;
}

.membership-detail-grid strong {
  display: block;
  margin-top: 0.125rem;
  color: rgb(20 83 45);
  font-size: 0.8125rem;
  font-weight: 700;
  overflow-wrap: anywhere;
}

.waiver-progress-card {
  display: grid;
  min-width: 0;
  gap: 0.375rem;
  border-radius: 0.5rem;
  border: 1px solid rgb(191 219 254);
  background: rgb(239 246 255 / 0.82);
  padding: 0.5rem;
  color: rgb(30 64 175);
}

.waiver-progress-top,
.waiver-progress-foot {
  display: flex;
  min-width: 0;
  align-items: center;
  justify-content: space-between;
  gap: 0.5rem;
}

.waiver-progress-top > div {
  min-width: 0;
}

.waiver-progress-top span,
.waiver-progress-foot {
  font-size: 0.71875rem;
  font-weight: 700;
  line-height: 1rem;
}

.waiver-progress-top strong {
  display: block;
  margin-top: 0.0625rem;
  color: rgb(15 23 42);
  font-size: 0.875rem;
  font-weight: 900;
  line-height: 1.125rem;
}

.waiver-progress-badge {
  flex-shrink: 0;
  border-radius: 0.5rem;
  background: rgb(219 234 254);
  padding: 0.1875rem 0.5rem;
  color: rgb(29 78 216);
  white-space: nowrap;
}

.waiver-progress-track {
  height: 0.5rem;
  overflow: hidden;
  border-radius: 999px;
  background: rgb(191 219 254 / 0.7);
}

.waiver-progress-track span {
  display: block;
  height: 100%;
  border-radius: inherit;
  background: linear-gradient(90deg, rgb(59 130 246), rgb(14 165 233));
  transition: width 0.2s ease;
}

.waiver-progress-foot {
  flex-wrap: wrap;
  color: rgb(29 78 216);
}

.waiver-progress-close {
  border-color: rgb(253 230 138);
  background: rgb(255 251 235 / 0.9);
  color: rgb(146 64 14);
}

.waiver-progress-close .waiver-progress-track {
  background: rgb(253 230 138 / 0.75);
}

.waiver-progress-close .waiver-progress-track span {
  background: linear-gradient(90deg, rgb(245 158 11), rgb(234 179 8));
}

.waiver-progress-close .waiver-progress-badge {
  background: rgb(254 243 199);
  color: rgb(146 64 14);
}

.waiver-progress-close .waiver-progress-foot {
  color: rgb(146 64 14);
}

.waiver-progress-met {
  border-color: rgb(134 239 172);
  background: rgb(220 252 231 / 0.9);
  color: rgb(22 101 52);
}

.waiver-progress-met .waiver-progress-track {
  background: rgb(187 247 208);
}

.waiver-progress-met .waiver-progress-track span {
  background: linear-gradient(90deg, rgb(34 197 94), rgb(16 185 129));
}

.waiver-progress-met .waiver-progress-badge {
  background: rgb(187 247 208);
  color: rgb(21 128 61);
}

.waiver-progress-met .waiver-progress-foot {
  color: rgb(22 101 52);
}

.membership-controls {
  display: grid;
  min-width: 0;
  gap: 0.4375rem;
}

.idle-timeout-control {
  display: grid;
  gap: 0.25rem;
}

.idle-timeout-row {
  display: grid;
  grid-template-columns: minmax(4.5rem, 1fr) auto auto;
  align-items: center;
  gap: 0.375rem;
}

.idle-timeout-input-row {
  display: flex;
  min-width: 0;
  align-items: center;
  gap: 0.375rem;
}

.idle-timeout-row .input,
.idle-timeout-join .input {
  min-width: 0;
}

.idle-timeout-row span {
  color: rgb(6 95 70);
  font-size: 0.8125rem;
  font-weight: 600;
  white-space: nowrap;
}

.idle-timeout-join {
  display: grid;
  min-width: 0;
  gap: 0.125rem;
}

.idle-timeout-join > span {
  font-size: 0.6875rem;
  line-height: 0.875rem;
}

.idle-timeout-join-unit {
  flex-shrink: 0;
  color: rgb(6 95 70);
  font-size: 0.75rem;
  font-weight: 600;
  white-space: nowrap;
}

.membership-action-row {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 0.375rem;
}

.membership-action-row .btn-secondary,
.membership-end-button {
  min-width: 0;
  justify-content: center;
  padding-inline: 0.625rem;
  white-space: nowrap;
}

.membership-end-button {
  display: inline-flex;
  min-height: 2.25rem;
  align-items: center;
  justify-content: center;
  border-radius: 0.5rem;
  background: rgb(4 120 87);
  padding: 0.4375rem 0.75rem;
  color: white;
  font-size: 0.8125rem;
  font-weight: 800;
  transition: background-color 0.15s ease, opacity 0.15s ease;
}

.membership-end-button:hover {
  background: rgb(6 95 70);
}

.membership-end-button:disabled {
  cursor: not-allowed;
  opacity: 0.6;
}

.listing-join-section {
  margin-top: 0.5rem;
  display: grid;
  gap: 0.375rem;
}

.listing-bottom-bar .listing-join-section {
  margin-top: 0;
}

.edit-lock-strip {
  display: flex;
  min-width: 0;
  align-items: flex-start;
  gap: 0.5rem;
  border-radius: 0.5rem;
  border: 1px solid rgb(253 230 138);
  background: rgb(255 251 235);
  padding: 0.5rem 0.625rem;
  color: rgb(146 64 14);
  font-size: 0.75rem;
  font-weight: 700;
  line-height: 1.125rem;
}

.edit-lock-strip span {
  min-width: 0;
}

.listing-action-row {
  display: grid;
  gap: 0.375rem;
}

@container account-listing-card (min-width: 38rem) {
  .listing-action-row {
    grid-template-columns: minmax(8.75rem, 10rem) minmax(0, 1fr) auto;
    align-items: center;
  }
}

.listing-action-row .btn-primary {
  min-width: 6rem;
}

.listing-timeout-row {
  display: grid;
  min-width: 0;
  gap: 0.375rem;
}

@container account-listing-card (min-width: 38rem) {
  .listing-timeout-row {
    grid-template-columns: auto minmax(0, 1fr);
    align-items: center;
  }
}

.idle-timeout-join-inline {
  gap: 0.375rem;
}

@container account-listing-card (min-width: 38rem) {
  .idle-timeout-join-inline {
    grid-template-columns: auto minmax(0, 1fr);
    align-items: center;
  }
}

.idle-timeout-join-inline > span {
  white-space: nowrap;
}

.idle-timeout-inline-note {
  display: flex;
  min-width: 0;
  align-items: flex-start;
  gap: 0.375rem;
  border-radius: 0.5rem;
  border: 1px solid rgb(191 219 254);
  background: rgb(239 246 255 / 0.82);
  padding: 0.4375rem 0.5625rem;
  color: rgb(29 78 216);
  font-size: 0.71875rem;
  line-height: 1.0625rem;
}

.idle-timeout-inline-note svg {
  margin-top: 0.0625rem;
  flex-shrink: 0;
}

.idle-timeout-reminder,
.idle-timeout-hint,
.join-usage-reminder {
  display: flex;
  min-width: 0;
  align-items: flex-start;
  gap: 0.4375rem;
  border-radius: 0.5rem;
  border: 1px solid rgb(191 219 254);
  background: rgb(239 246 255 / 0.82);
  padding: 0.5rem 0.625rem;
  color: rgb(29 78 216);
  font-size: 0.75rem;
  line-height: 1.125rem;
}

.membership-controls .idle-timeout-hint {
  overflow: hidden;
  padding: 0;
  border: 0;
  background: transparent;
  color: rgb(5 150 105);
  font-size: 0.71875rem;
  font-weight: 700;
  line-height: 1rem;
  text-overflow: ellipsis;
  white-space: nowrap;
}

@container account-listing-card (min-width: 34rem) {
  .membership-detail-grid {
    grid-template-columns: repeat(3, minmax(0, 1fr));
  }
}

@container account-listing-card (min-width: 44rem) {
  .membership-compact-body {
    grid-template-columns: minmax(0, 1fr) minmax(13.5rem, 15rem);
    align-items: start;
  }

  .membership-detail-grid {
    grid-template-columns: repeat(4, minmax(0, 1fr));
  }
}

@container account-listing-card (min-width: 52rem) {
  .membership-main {
    grid-template-columns: minmax(0, 0.92fr) minmax(14rem, 1fr);
    align-items: stretch;
  }

  .membership-detail-grid {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }
}

.idle-timeout-reminder svg,
.idle-timeout-hint svg,
.join-usage-reminder svg {
  flex-shrink: 0;
}

.dark .account-share-membership-panel {
  border-color: rgb(16 185 129 / 0.28);
  background: linear-gradient(180deg, rgb(6 95 70 / 0.18), rgb(20 83 45 / 0.12));
  color: rgb(167 243 208);
}

.dark .edit-lock-strip {
  border-color: rgb(245 158 11 / 0.3);
  background: rgb(146 64 14 / 0.2);
  color: rgb(253 230 138);
}

.dark .membership-status-pill {
  background: rgb(5 150 105);
}

.dark .membership-status-pill-queued {
  background: rgb(37 99 235);
}

.dark .membership-status-pill-waiting {
  background: rgb(146 64 14);
  color: rgb(253 230 138);
}

.dark .membership-title {
  color: rgb(209 250 229);
}

.dark .membership-subtitle {
  color: rgb(110 231 183);
}

.dark .membership-detail-grid div {
  border-color: rgb(16 185 129 / 0.22);
  background: rgb(24 24 27 / 0.52);
}

.dark .membership-detail-grid span,
.dark .idle-timeout-control label,
.dark .idle-timeout-join span {
  color: rgb(110 231 183);
}

.dark .membership-detail-grid strong,
.dark .idle-timeout-row span,
.dark .idle-timeout-join-unit {
  color: rgb(209 250 229);
}

.dark .idle-timeout-reminder,
.dark .idle-timeout-hint,
.dark .join-usage-reminder {
  border-color: rgb(37 99 235 / 0.4);
  background: rgb(30 64 175 / 0.16);
  color: rgb(191 219 254);
}

.dark .waiver-progress-card {
  border-color: rgb(59 130 246 / 0.35);
  background: rgb(30 64 175 / 0.16);
  color: rgb(191 219 254);
}

.dark .waiver-progress-top strong {
  color: white;
}

.dark .waiver-progress-badge {
  background: rgb(59 130 246 / 0.2);
  color: rgb(191 219 254);
}

.dark .waiver-progress-track {
  background: rgb(30 64 175 / 0.5);
}

.dark .waiver-progress-foot {
  color: rgb(191 219 254);
}

.dark .waiver-progress-close {
  border-color: rgb(245 158 11 / 0.32);
  background: rgb(146 64 14 / 0.18);
  color: rgb(253 230 138);
}

.dark .waiver-progress-close .waiver-progress-badge {
  background: rgb(245 158 11 / 0.18);
  color: rgb(253 230 138);
}

.dark .waiver-progress-close .waiver-progress-foot {
  color: rgb(253 230 138);
}

.dark .waiver-progress-met {
  border-color: rgb(34 197 94 / 0.35);
  background: rgb(20 83 45 / 0.3);
  color: rgb(187 247 208);
}

.dark .waiver-progress-met .waiver-progress-badge {
  background: rgb(34 197 94 / 0.18);
  color: rgb(187 247 208);
}

.dark .waiver-progress-met .waiver-progress-foot {
  color: rgb(187 247 208);
}

.dark .membership-end-button {
  background: rgb(5 150 105);
}

.dark .membership-end-button:hover {
  background: rgb(4 120 87);
}

.dark .membership-controls .idle-timeout-hint {
  color: rgb(110 231 183);
}

.dark .idle-timeout-inline-note {
  border-color: rgb(37 99 235 / 0.4);
  background: rgb(30 64 175 / 0.16);
  color: rgb(191 219 254);
}

.my-spend-panel {
  display: grid;
  gap: 1rem;
  color: rgb(51 65 85);
}

.my-spend-account-picker {
  display: grid;
  gap: 0.75rem;
  border-radius: 0.5rem;
  border: 1px solid rgb(226 232 240);
  background: rgb(248 250 252);
  padding: 0.875rem;
}

.my-spend-account-picker-head {
  display: flex;
  flex-wrap: wrap;
  align-items: flex-start;
  justify-content: space-between;
  gap: 0.75rem;
}

.my-spend-account-picker-head > div {
  display: grid;
  min-width: 0;
  gap: 0.1875rem;
}

.my-spend-account-picker-head span {
  color: rgb(15 23 42);
  font-size: 0.875rem;
  font-weight: 850;
}

.my-spend-account-picker-head strong {
  color: rgb(37 99 235);
  font-size: 0.8125rem;
  font-weight: 800;
}

.my-spend-account-picker-head small {
  color: rgb(100 116 139);
  font-size: 0.75rem;
  line-height: 1.125rem;
}

.my-spend-account-grid {
  display: grid;
  max-height: 18rem;
  grid-template-columns: repeat(auto-fit, minmax(13.5rem, 1fr));
  gap: 0.5rem;
  overflow-y: auto;
  padding-right: 0.125rem;
}

.my-spend-account-option {
  display: grid;
  min-width: 0;
  gap: 0.375rem;
  border-radius: 0.5rem;
  border: 1px solid rgb(226 232 240);
  background: white;
  padding: 0.75rem;
  text-align: left;
  transition: border-color 0.16s ease, background-color 0.16s ease, box-shadow 0.16s ease, transform 0.16s ease;
}

.my-spend-account-option:hover {
  border-color: rgb(147 197 253);
  background: rgb(239 246 255);
  box-shadow: 0 8px 18px rgb(15 23 42 / 0.08);
}

.my-spend-account-option.active {
  border-color: rgb(37 99 235);
  background: linear-gradient(180deg, rgb(239 246 255), rgb(240 253 250));
  box-shadow: inset 3px 0 0 rgb(37 99 235), 0 10px 22px rgb(37 99 235 / 0.12);
}

.my-spend-account-option-top,
.my-spend-account-option-foot {
  display: flex;
  min-width: 0;
  flex-wrap: wrap;
  align-items: center;
  gap: 0.375rem;
}

.my-spend-account-option-top > span:not(.feature-badge),
.my-spend-account-option-foot span {
  color: rgb(100 116 139);
  font-size: 0.6875rem;
  font-weight: 800;
}

.my-spend-account-option strong {
  min-width: 0;
  color: rgb(15 23 42);
  font-size: 0.875rem;
  font-weight: 850;
  overflow-wrap: anywhere;
}

.my-spend-account-option small {
  color: rgb(71 85 105);
  font-size: 0.75rem;
  line-height: 1.125rem;
  overflow-wrap: anywhere;
}

.my-spend-context {
  display: grid;
  grid-template-columns: auto minmax(0, 1fr);
  align-items: flex-start;
  gap: 0.75rem;
  border-radius: 0.5rem;
  border: 1px solid rgb(191 219 254);
  background: linear-gradient(180deg, rgb(239 246 255), rgb(240 253 250));
  padding: 0.875rem;
}

.my-spend-context-icon {
  display: inline-flex;
  height: 2.25rem;
  width: 2.25rem;
  align-items: center;
  justify-content: center;
  border-radius: 0.5rem;
  background: rgb(37 99 235);
  color: white;
}

.my-spend-eyebrow {
  display: block;
  color: rgb(29 78 216);
  font-size: 0.75rem;
  font-weight: 800;
}

.my-spend-context strong {
  display: block;
  margin-top: 0.1875rem;
  color: rgb(15 23 42);
  font-size: 1rem;
  font-weight: 850;
  overflow-wrap: anywhere;
}

.my-spend-context small {
  display: block;
  margin-top: 0.25rem;
  color: rgb(71 85 105);
  font-size: 0.8125rem;
  line-height: 1.25rem;
  overflow-wrap: anywhere;
}

.my-spend-toolbar {
  display: flex;
  flex-direction: column;
  gap: 0.625rem;
}

.my-spend-range-tabs {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 0.25rem;
  border-radius: 0.5rem;
  border: 1px solid rgb(226 232 240);
  background: rgb(248 250 252);
  padding: 0.25rem;
}

.my-spend-range-tabs button {
  min-width: 0;
  border-radius: 0.375rem;
  padding: 0.5rem 0.625rem;
  color: rgb(71 85 105);
  font-size: 0.8125rem;
  font-weight: 800;
  transition: background-color 0.16s ease, color 0.16s ease, box-shadow 0.16s ease;
}

.my-spend-range-tabs button:hover {
  background: white;
  color: rgb(37 99 235);
}

.my-spend-range-tabs button.active {
  background: rgb(37 99 235);
  color: white;
  box-shadow: 0 8px 16px rgb(37 99 235 / 0.18);
}

.my-spend-loading,
.my-spend-empty {
  border-radius: 0.5rem;
  border: 1px dashed rgb(203 213 225);
  background: rgb(248 250 252);
  padding: 1rem;
  text-align: center;
  color: rgb(100 116 139);
  font-size: 0.875rem;
}

.my-spend-window {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(13rem, 1fr));
  gap: 0.5rem;
}

.my-spend-window > div,
.my-spend-detail-grid > div,
.my-spend-hourly-panel > div {
  min-width: 0;
  border-radius: 0.5rem;
  border: 1px solid rgb(226 232 240);
  background: white;
  padding: 0.75rem;
}

.my-spend-window span,
.my-spend-detail-grid span,
.my-spend-hourly-panel span {
  display: block;
  color: rgb(100 116 139);
  font-size: 0.75rem;
  font-weight: 700;
}

.my-spend-window strong,
.my-spend-detail-grid strong,
.my-spend-hourly-panel strong {
  display: block;
  margin-top: 0.25rem;
  color: rgb(15 23 42);
  font-size: 0.875rem;
  font-weight: 850;
  overflow-wrap: anywhere;
}

.my-spend-detail-grid small {
  display: block;
  margin-top: 0.1875rem;
  color: rgb(100 116 139);
  font-size: 0.75rem;
  font-weight: 700;
  line-height: 1rem;
}

.my-spend-metric-grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(10rem, 1fr));
  gap: 0.625rem;
}

.my-spend-metric {
  display: grid;
  min-width: 0;
  gap: 0.1875rem;
  border-radius: 0.5rem;
  border: 1px solid rgb(226 232 240);
  background: white;
  padding: 0.875rem;
  box-shadow: inset 3px 0 0 rgb(148 163 184);
}

.my-spend-metric span {
  display: flex;
  min-width: 0;
  align-items: center;
  gap: 0.375rem;
  color: rgb(100 116 139);
  font-size: 0.75rem;
  font-weight: 800;
}

.my-spend-metric strong {
  color: rgb(15 23 42);
  font-size: 1.25rem;
  font-weight: 900;
  line-height: 1.5rem;
  overflow-wrap: anywhere;
}

.my-spend-metric small {
  color: rgb(100 116 139);
  font-size: 0.75rem;
  line-height: 1.0625rem;
  overflow-wrap: anywhere;
}

.my-spend-metric-total {
  border-color: rgb(59 130 246 / 0.3);
  background: rgb(239 246 255);
  box-shadow: inset 3px 0 0 rgb(37 99 235);
}

.my-spend-metric-request {
  border-color: rgb(20 184 166 / 0.32);
  background: rgb(240 253 250);
  box-shadow: inset 3px 0 0 rgb(13 148 136);
}

.my-spend-metric-hourly {
  border-color: rgb(245 158 11 / 0.32);
  background: rgb(255 251 235);
  box-shadow: inset 3px 0 0 rgb(217 119 6);
}

.my-spend-detail-grid,
.my-spend-hourly-panel {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(10rem, 1fr));
  gap: 0.5rem;
}

.my-spend-breakdown {
  display: grid;
  gap: 0.625rem;
}

.my-spend-section-head {
  display: flex;
  min-width: 0;
  justify-content: space-between;
  gap: 0.75rem;
}

.my-spend-section-head strong {
  display: block;
  color: rgb(15 23 42);
  font-size: 0.9375rem;
  font-weight: 850;
}

.my-spend-section-head small {
  display: block;
  margin-top: 0.1875rem;
  color: rgb(100 116 139);
  font-size: 0.75rem;
}

.my-spend-table-wrap {
  overflow-x: auto;
  border-radius: 0.5rem;
  border: 1px solid rgb(226 232 240);
}

.my-spend-table {
  width: 100%;
  min-width: 38rem;
  border-collapse: collapse;
  background: white;
  font-size: 0.8125rem;
}

.my-spend-table th,
.my-spend-table td {
  border-bottom: 1px solid rgb(226 232 240);
  padding: 0.625rem 0.75rem;
  text-align: left;
  white-space: nowrap;
}

.my-spend-table th {
  background: rgb(248 250 252);
  color: rgb(71 85 105);
  font-size: 0.75rem;
  font-weight: 800;
}

.my-spend-table td {
  color: rgb(15 23 42);
  font-weight: 650;
}

.my-spend-table tr:last-child td {
  border-bottom: 0;
}

.dark .my-spend-panel {
  color: rgb(212 212 216);
}

.dark .my-spend-account-picker,
.dark .my-spend-account-option {
  border-color: rgb(63 63 70);
  background: rgb(39 39 42 / 0.72);
}

.dark .my-spend-account-picker-head span,
.dark .my-spend-account-option strong {
  color: white;
}

.dark .my-spend-account-picker-head strong {
  color: rgb(147 197 253);
}

.dark .my-spend-account-picker-head small,
.dark .my-spend-account-option-top > span:not(.feature-badge),
.dark .my-spend-account-option-foot span,
.dark .my-spend-account-option small {
  color: rgb(161 161 170);
}

.dark .my-spend-account-option:hover {
  border-color: rgb(96 165 250 / 0.56);
  background: rgb(30 64 175 / 0.18);
  box-shadow: 0 8px 18px rgb(0 0 0 / 0.24);
}

.dark .my-spend-account-option.active {
  border-color: rgb(96 165 250 / 0.72);
  background: linear-gradient(180deg, rgb(30 64 175 / 0.24), rgb(20 83 45 / 0.2));
  box-shadow: inset 3px 0 0 rgb(96 165 250), 0 10px 22px rgb(0 0 0 / 0.22);
}

.dark .my-spend-context {
  border-color: rgb(96 165 250 / 0.32);
  background: linear-gradient(180deg, rgb(30 41 59 / 0.82), rgb(20 83 45 / 0.24));
}

.dark .my-spend-eyebrow {
  color: rgb(147 197 253);
}

.dark .my-spend-context strong,
.dark .my-spend-window strong,
.dark .my-spend-detail-grid strong,
.dark .my-spend-hourly-panel strong,
.dark .my-spend-section-head strong,
.dark .my-spend-metric strong,
.dark .my-spend-table td {
  color: white;
}

.dark .my-spend-context small,
.dark .my-spend-window span,
.dark .my-spend-detail-grid span,
.dark .my-spend-detail-grid small,
.dark .my-spend-hourly-panel span,
.dark .my-spend-section-head small,
.dark .my-spend-metric span,
.dark .my-spend-metric small {
  color: rgb(161 161 170);
}

.dark .my-spend-range-tabs,
.dark .my-spend-window > div,
.dark .my-spend-detail-grid > div,
.dark .my-spend-hourly-panel > div,
.dark .my-spend-metric,
.dark .my-spend-table {
  border-color: rgb(63 63 70);
  background: rgb(39 39 42 / 0.72);
}

.dark .my-spend-range-tabs button {
  color: rgb(212 212 216);
}

.dark .my-spend-range-tabs button:hover {
  background: rgb(63 63 70);
  color: rgb(147 197 253);
}

.dark .my-spend-range-tabs button.active {
  background: rgb(37 99 235);
  color: white;
}

.dark .my-spend-loading,
.dark .my-spend-empty {
  border-color: rgb(63 63 70);
  background: rgb(39 39 42 / 0.65);
  color: rgb(161 161 170);
}

.dark .my-spend-metric-total {
  border-color: rgb(96 165 250 / 0.34);
  background: rgb(30 64 175 / 0.18);
}

.dark .my-spend-metric-request {
  border-color: rgb(45 212 191 / 0.3);
  background: rgb(20 83 45 / 0.2);
}

.dark .my-spend-metric-hourly {
  border-color: rgb(245 158 11 / 0.3);
  background: rgb(146 64 14 / 0.18);
}

.dark .my-spend-table-wrap {
  border-color: rgb(63 63 70);
}

.dark .my-spend-table th {
  border-color: rgb(63 63 70);
  background: rgb(24 24 27);
  color: rgb(212 212 216);
}

.dark .my-spend-table td {
  border-color: rgb(63 63 70);
}

@media (min-width: 640px) {
  .my-spend-toolbar {
    flex-direction: row;
    align-items: center;
    justify-content: space-between;
  }

  .my-spend-range-tabs {
    width: min(100%, 24rem);
  }
}

@media (max-width: 640px) {
  .hero-utility-actions,
  .hero-actions {
    width: 100%;
  }

  .hero-utility-actions > button,
  .hero-actions > button {
    flex: 1 1 9rem;
  }

  .my-spend-account-picker-head .btn-secondary {
    width: 100%;
  }
}

.join-confirmation {
  display: grid;
  gap: 0.875rem;
}

.join-confirmation-head {
  display: grid;
  grid-template-columns: auto minmax(0, 1fr);
  gap: 0.75rem;
  align-items: flex-start;
  border-radius: 0.5rem;
  border: 1px solid rgb(191 219 254);
  background: linear-gradient(135deg, rgb(239 246 255), rgb(240 253 250));
  padding: 0.875rem;
}

.join-confirmation-head strong {
  display: block;
  color: rgb(15 23 42);
  font-size: 0.9375rem;
  font-weight: 800;
}

.join-confirmation-head span:not(.join-confirmation-icon) {
  display: block;
  margin-top: 0.25rem;
  color: rgb(71 85 105);
  font-size: 0.8125rem;
  line-height: 1.35rem;
}

.join-confirmation-icon {
  display: inline-flex;
  height: 2.25rem;
  width: 2.25rem;
  align-items: center;
  justify-content: center;
  border-radius: 0.5rem;
  background: rgb(37 99 235);
  color: white;
}

.join-confirmation-head-danger {
  border-color: rgb(248 113 113 / 0.5);
  background: linear-gradient(135deg, rgb(254 242 242), rgb(255 247 237));
}

.join-confirmation-head-danger .join-confirmation-icon {
  background: rgb(220 38 38);
}

.join-warning-list {
  display: grid;
  gap: 0.5rem;
}

.join-warning-item {
  display: flex;
  align-items: flex-start;
  gap: 0.5rem;
  border-radius: 0.5rem;
  border: 1px solid rgb(248 113 113 / 0.45);
  background: rgb(254 242 242);
  padding: 0.625rem 0.75rem;
  color: rgb(185 28 28);
  font-size: 0.8125rem;
  font-weight: 700;
  line-height: 1.25rem;
}

.join-confirmation-grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 0.5rem;
}

@media (min-width: 768px) {
  .join-confirmation-grid {
    grid-template-columns: repeat(5, minmax(0, 1fr));
  }
}

.join-confirmation-field {
  display: grid;
  min-width: 0;
  gap: 0.1875rem;
  border-radius: 0.5rem;
  border: 1px solid rgb(226 232 240);
  background: rgb(248 250 252);
  padding: 0.625rem 0.6875rem;
}

.join-confirmation-field span {
  color: rgb(100 116 139);
  font-size: 0.71875rem;
  font-weight: 700;
}

.join-confirmation-field strong {
  min-width: 0;
  color: rgb(15 23 42);
  font-size: 0.875rem;
  font-weight: 900;
  overflow-wrap: anywhere;
}

.join-price-danger {
  border-color: rgb(248 113 113 / 0.55);
  background: rgb(254 242 242);
  box-shadow: inset 3px 0 0 rgb(220 38 38);
}

.join-price-danger span,
.join-price-danger strong {
  color: rgb(185 28 28);
}

.join-model-confirmation {
  display: grid;
  gap: 0.5rem;
  border-radius: 0.5rem;
  border: 1px solid rgb(226 232 240);
  background: rgb(255 255 255);
  padding: 0.75rem;
}

.join-model-confirmation > span {
  color: rgb(100 116 139);
  font-size: 0.75rem;
  font-weight: 800;
}

.join-model-confirmation > div {
  display: flex;
  flex-wrap: wrap;
  gap: 0.375rem;
}

.join-model-more {
  display: inline-flex;
  align-items: center;
  border-radius: 0.375rem;
  background: rgb(241 245 249);
  padding: 0.25rem 0.5rem;
  color: rgb(71 85 105);
  font-size: 0.75rem;
  font-weight: 800;
}

.dark .join-confirmation-head {
  border-color: rgb(59 130 246 / 0.38);
  background: linear-gradient(135deg, rgb(30 41 59), rgb(20 83 45 / 0.35));
}

.dark .join-confirmation-head strong {
  color: white;
}

.dark .join-confirmation-head span:not(.join-confirmation-icon) {
  color: rgb(203 213 225);
}

.dark .join-confirmation-head-danger {
  border-color: rgb(248 113 113 / 0.55);
  background: linear-gradient(135deg, rgb(69 10 10 / 0.76), rgb(67 20 7 / 0.58));
}

.dark .join-warning-item {
  border-color: rgb(248 113 113 / 0.45);
  background: rgb(127 29 29 / 0.42);
  color: rgb(254 202 202);
}

.dark .join-confirmation-field,
.dark .join-model-confirmation {
  border-color: rgb(63 63 70);
  background: rgb(39 39 42 / 0.72);
}

.dark .join-confirmation-field span,
.dark .join-model-confirmation > span {
  color: rgb(161 161 170);
}

.dark .join-confirmation-field strong {
  color: white;
}

.dark .join-price-danger {
  border-color: rgb(248 113 113 / 0.55);
  background: rgb(127 29 29 / 0.38);
}

.dark .join-price-danger span,
.dark .join-price-danger strong {
  color: rgb(252 165 165);
}

.dark .join-model-more {
  background: rgb(63 63 70);
  color: rgb(212 212 216);
}

@media (max-width: 640px) {
  .membership-status-head {
    flex-direction: column;
  }

  .idle-timeout-row {
    grid-template-columns: minmax(0, 1fr) auto;
  }

  .idle-timeout-row button {
    grid-column: 1 / -1;
  }
}

.listing-metric-grid {
  margin-top: 0.5rem;
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 0.3125rem;
  font-size: 0.8125rem;
}

@container account-listing-card (min-width: 26rem) {
  .listing-metric-grid {
    grid-template-columns: repeat(3, minmax(0, 1fr));
  }
}

@container account-listing-card (min-width: 34rem) {
  .listing-metric-grid {
    grid-template-columns: repeat(4, minmax(0, 1fr));
  }
}

@container account-listing-card (min-width: 38rem) {
  .listing-metric-grid {
    grid-template-columns: repeat(7, minmax(0, 1fr));
  }

  .metric {
    min-height: 2.875rem;
    gap: 0;
    padding: 0.3125rem 0.34375rem;
  }

  .metric span {
    font-size: 0.625rem;
    line-height: 0.8125rem;
  }

  .metric strong,
  .metric-billing strong {
    font-size: 0.78125rem;
    line-height: 0.9375rem;
  }
}

.metric {
  display: grid;
  min-width: 0;
  align-content: start;
  min-height: 3.25rem;
  gap: 0.0625rem;
  border-radius: 0.5rem;
  border: 1px solid rgb(226 232 240);
  background: rgb(255 255 255 / 0.86);
  padding: 0.375rem 0.4375rem;
}

.metric span {
  min-width: 0;
  font-size: 0.65625rem;
  line-height: 0.875rem;
  color: rgb(107 114 128);
}

.metric-label {
  display: flex;
  align-items: center;
  gap: 0.25rem;
  font-weight: 700;
}

.metric-label svg {
  flex-shrink: 0;
}

.metric strong {
  display: block;
  min-width: 0;
  color: rgb(17 24 39);
  font-size: 0.84375rem;
  font-weight: 800;
  line-height: 1rem;
  overflow-wrap: anywhere;
}

.metric-billing {
  border-color: rgb(59 130 246 / 0.28);
  background: linear-gradient(180deg, rgb(239 246 255 / 0.96), rgb(240 253 250 / 0.9));
  box-shadow: inset 3px 0 0 rgb(37 99 235 / 0.86);
}

.metric-billing span {
  color: rgb(29 78 216);
  font-weight: 800;
}

.metric-billing strong {
  color: rgb(13 148 136);
  font-size: 0.84375rem;
  font-weight: 900;
}

.metric-price-danger {
  border-color: rgb(248 113 113 / 0.62);
  background: linear-gradient(180deg, rgb(254 242 242), rgb(255 247 237));
  box-shadow: inset 3px 0 0 rgb(220 38 38);
}

.metric-price-danger span,
.metric-price-danger strong {
  color: rgb(185 28 28);
}

.dark .metric {
  border-color: rgb(63 63 70);
  background: rgb(39 39 42 / 0.7);
}

.dark .metric span {
  color: rgb(161 161 170);
}

.dark .metric strong {
  color: white;
}

.dark .metric-billing {
  border-color: rgb(96 165 250 / 0.34);
  background: linear-gradient(180deg, rgb(30 41 59 / 0.86), rgb(20 83 45 / 0.26));
  box-shadow: inset 3px 0 0 rgb(96 165 250 / 0.9);
}

.dark .metric-billing span {
  color: rgb(147 197 253);
}

.dark .metric-billing strong {
  color: rgb(94 234 212);
}

.dark .metric-price-danger {
  border-color: rgb(248 113 113 / 0.56);
  background: linear-gradient(180deg, rgb(127 29 29 / 0.48), rgb(67 20 7 / 0.36));
  box-shadow: inset 3px 0 0 rgb(248 113 113 / 0.88);
}

.dark .metric-price-danger span,
.dark .metric-price-danger strong {
  color: rgb(252 165 165);
}

.key-resolution-panel {
  display: grid;
  min-width: 0;
  gap: 1rem;
  border: 1px solid rgb(245 158 11 / 0.58);
  border-radius: 0.625rem;
  background: rgb(255 251 235);
  padding: 1rem;
  box-shadow: inset 0.25rem 0 0 rgb(217 119 6);
}

.key-resolution-main {
  display: grid;
  min-width: 0;
  grid-template-columns: auto minmax(0, 1fr);
  align-items: flex-start;
  gap: 0.75rem;
}

.key-resolution-icon {
  display: inline-flex;
  width: 2.75rem;
  height: 2.75rem;
  flex-shrink: 0;
  align-items: center;
  justify-content: center;
  border-radius: 0.5rem;
  background: rgb(217 119 6);
  color: white;
}

.key-resolution-copy {
  min-width: 0;
}

.key-resolution-eyebrow {
  display: block;
  color: rgb(146 64 14);
  font-size: 0.75rem;
  font-weight: 850;
  letter-spacing: 0.04em;
}

.key-resolution-copy h2 {
  margin: 0.1875rem 0 0;
  color: rgb(69 26 3);
  font-size: 1rem;
  font-weight: 900;
  line-height: 1.375rem;
  overflow-wrap: anywhere;
}

.key-resolution-copy p {
  max-width: 70ch;
  margin: 0.375rem 0 0;
  color: rgb(120 53 15);
  font-size: 0.875rem;
  line-height: 1.375rem;
}

.key-resolution-counts {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 0.5rem;
}

.key-resolution-counts > div {
  display: grid;
  min-width: 0;
  gap: 0.125rem;
  border: 1px solid rgb(245 158 11 / 0.42);
  border-radius: 0.5rem;
  background: rgb(255 255 255 / 0.86);
  padding: 0.625rem 0.75rem;
}

.key-resolution-counts span {
  color: rgb(120 53 15);
  font-size: 0.75rem;
  font-weight: 750;
}

.key-resolution-counts strong {
  color: rgb(69 26 3);
  font-size: 1.25rem;
  font-weight: 900;
  line-height: 1.5rem;
}

.key-resolution-actions {
  display: grid;
  grid-template-columns: 1fr;
  gap: 0.5rem;
}

.key-resolution-refresh-button,
.key-resolution-return-button {
  display: inline-flex;
  min-width: 0;
  min-height: 2.75rem;
  cursor: pointer;
  align-items: center;
  justify-content: center;
  gap: 0.5rem;
  border-radius: 0.5rem;
  padding: 0.625rem 0.875rem;
  font-size: 0.875rem;
  font-weight: 850;
  line-height: 1.25rem;
  transition: border-color 180ms ease, background-color 180ms ease, color 180ms ease;
}

.key-resolution-refresh-button {
  border: 1px solid rgb(217 119 6);
  background: rgb(255 255 255);
  color: rgb(146 64 14);
}

.key-resolution-refresh-button:hover:not(:disabled) {
  background: rgb(254 243 199);
}

.key-resolution-refresh-button:disabled {
  cursor: not-allowed;
  opacity: 0.68;
}

.key-resolution-return-button {
  border: 1px solid rgb(30 64 175);
  background: rgb(30 64 175);
  color: white;
}

.key-resolution-return-button:hover {
  border-color: rgb(30 58 138);
  background: rgb(30 58 138);
}

.key-resolution-refresh-button:focus-visible,
.key-resolution-return-button:focus-visible {
  outline: 3px solid rgb(59 130 246 / 0.42);
  outline-offset: 2px;
}

.key-resolution-panel-loading {
  border-color: rgb(96 165 250 / 0.72);
  background: rgb(239 246 255);
  box-shadow: inset 0.25rem 0 0 rgb(37 99 235);
}

.key-resolution-panel-loading .key-resolution-icon {
  background: rgb(37 99 235);
}

.key-resolution-panel-error {
  border-color: rgb(248 113 113 / 0.72);
  background: rgb(254 242 242);
  box-shadow: inset 0.25rem 0 0 rgb(220 38 38);
}

.key-resolution-panel-error .key-resolution-icon {
  background: rgb(220 38 38);
}

.key-resolution-panel-error .key-resolution-eyebrow,
.key-resolution-panel-error .key-resolution-copy p,
.key-resolution-panel-error .key-resolution-counts span {
  color: rgb(153 27 27);
}

.key-resolution-panel-error .key-resolution-copy h2,
.key-resolution-panel-error .key-resolution-counts strong {
  color: rgb(69 10 10);
}

.key-resolution-panel-clear {
  border-color: rgb(52 211 153 / 0.72);
  background: rgb(236 253 245);
  box-shadow: inset 0.25rem 0 0 rgb(5 150 105);
}

.key-resolution-panel-clear .key-resolution-icon {
  background: rgb(5 150 105);
}

.key-resolution-panel-clear .key-resolution-eyebrow,
.key-resolution-panel-clear .key-resolution-copy p,
.key-resolution-panel-clear .key-resolution-counts span {
  color: rgb(6 95 70);
}

.key-resolution-panel-clear .key-resolution-copy h2,
.key-resolution-panel-clear .key-resolution-counts strong {
  color: rgb(6 78 59);
}

.key-resolution-listing-card {
  border-color: rgb(245 158 11 / 0.72);
  box-shadow: inset 0.25rem 0 0 rgb(217 119 6), 0 0.75rem 1.75rem rgb(120 53 15 / 0.1);
}

.dark .key-resolution-panel {
  border-color: rgb(245 158 11 / 0.48);
  background: rgb(69 26 3 / 0.42);
}

.dark .key-resolution-copy h2,
.dark .key-resolution-counts strong {
  color: rgb(255 247 237);
}

.dark .key-resolution-eyebrow,
.dark .key-resolution-copy p,
.dark .key-resolution-counts span {
  color: rgb(253 230 138);
}

.dark .key-resolution-counts > div {
  border-color: rgb(245 158 11 / 0.34);
  background: rgb(41 37 36 / 0.82);
}

.dark .key-resolution-refresh-button {
  border-color: rgb(245 158 11 / 0.68);
  background: rgb(41 37 36);
  color: rgb(253 230 138);
}

.dark .key-resolution-refresh-button:hover:not(:disabled) {
  background: rgb(69 26 3);
}

.dark .key-resolution-panel-loading {
  border-color: rgb(96 165 250 / 0.5);
  background: rgb(30 58 138 / 0.24);
}

.dark .key-resolution-panel-error {
  border-color: rgb(248 113 113 / 0.52);
  background: rgb(127 29 29 / 0.3);
}

.dark .key-resolution-panel-clear {
  border-color: rgb(52 211 153 / 0.48);
  background: rgb(6 78 59 / 0.3);
}

.dark .key-resolution-panel-error .key-resolution-eyebrow,
.dark .key-resolution-panel-error .key-resolution-copy p,
.dark .key-resolution-panel-error .key-resolution-counts span {
  color: rgb(254 202 202);
}

.dark .key-resolution-panel-clear .key-resolution-eyebrow,
.dark .key-resolution-panel-clear .key-resolution-copy p,
.dark .key-resolution-panel-clear .key-resolution-counts span {
  color: rgb(167 243 208);
}

.dark .key-resolution-listing-card {
  border-color: rgb(245 158 11 / 0.64);
  box-shadow: inset 0.25rem 0 0 rgb(245 158 11), 0 0.75rem 1.75rem rgb(0 0 0 / 0.28);
}

@media (min-width: 768px) {
  .key-resolution-panel {
    grid-template-columns: minmax(0, 1fr) auto;
    align-items: center;
  }

  .key-resolution-counts {
    min-width: 15rem;
  }

  .key-resolution-actions {
    grid-column: 1 / -1;
    grid-template-columns: repeat(2, auto);
    justify-content: flex-end;
  }
}

@media (min-width: 1024px) {
  .key-resolution-panel {
    grid-template-columns: minmax(0, 1fr) auto auto;
  }

  .key-resolution-actions {
    grid-column: auto;
  }
}

@media (prefers-reduced-motion: reduce) {
  .key-resolution-refresh-button,
  .key-resolution-return-button {
    transition: none;
  }
}
</style>
