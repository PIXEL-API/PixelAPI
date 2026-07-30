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
            <button
              v-if="authStore.isAdmin"
              class="account-share-admin-quota-button"
              type="button"
              data-testid="open-account-share-quotas"
              @click="openAdminQuotaDialog"
            >
              <Icon name="cog" size="sm" class="mr-2" />
              房间配额
            </button>
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
            <button class="btn-secondary min-h-11" type="button" :disabled="currentViewLoading || isAnyModeKeysLoading || selfUseSettingsLoading" @click="refreshPageData">
              <Icon name="refresh" size="sm" class="mr-2" :class="{ 'animate-spin': currentViewLoading || isAnyModeKeysLoading || selfUseSettingsLoading }" />
              刷新
            </button>
            <button
              class="btn-primary min-h-11"
              type="button"
              :disabled="capabilitiesLoading || capabilities?.can_create_room === false"
              :title="createRoomCapabilityHint"
              @click="openCreateDialog"
            >
              <Icon name="plus" size="sm" class="mr-2" />
              创建房间
            </button>
            <button class="btn-secondary min-h-11" type="button" @click="openRecommendationDialog">
              <Icon name="sparkles" size="sm" class="mr-2" />
              选号助手
            </button>
          </div>
        </div>

        <div
          v-if="capabilities || capabilitiesError"
          class="account-share-capability-strip"
          :class="{ 'account-share-capability-strip-blocked': capabilities?.can_create_room === false }"
          aria-live="polite"
        >
          <template v-if="capabilities">
            <span>
              <strong>{{ capabilities.live_rooms.used }}/{{ capabilities.live_rooms.limit }}</strong>
              未删除房间
            </span>
            <span>
              <strong>{{ capabilities.room_creates_24_hours.used }}/{{ capabilities.room_creates_24_hours.limit }}</strong>
              24 小时创建
            </span>
            <span>
              <strong>{{ capabilities.owner_room_accounts.used }}/{{ capabilities.owner_room_accounts.limit }}</strong>
              房间账号
            </span>
            <small v-if="capabilities.capability_blockers.length > 0">
              {{ capabilityBlockerMessage(capabilities.capability_blockers[0]) }}
            </small>
            <small v-else>
              每个房间最多 {{ capabilities.max_accounts_per_room }} 个账号，成员上限可设为
              {{ capabilities.seat_limit_minimum }}～{{ capabilities.seat_limit_maximum }} 人。
            </small>
          </template>
          <small v-else>{{ capabilitiesError }}</small>
        </div>

        <div v-if="!isMembershipHistoryView" class="account-share-platform-tabs" role="tablist" aria-label="账号模式平台">
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

        <div v-if="!isMembershipHistoryView && !isArchiveView" class="account-share-summary-grid">
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

        <div class="key-resolution-counts grid grid-cols-1 gap-2 sm:grid-cols-3" aria-label="待处理关联数量">
          <div>
            <span>正在使用</span>
            <strong>{{ (keyResolutionLoading && !keyResolutionLoaded) || keyResolutionError ? '—' : keyResolutionActiveCount }}</strong>
          </div>
          <div>
            <span>预约中</span>
            <strong>{{ (keyResolutionLoading && !keyResolutionLoaded) || keyResolutionError ? '—' : keyResolutionQueuedCount }}</strong>
          </div>
          <div>
            <span>退出/结算中</span>
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
            <strong>选择房间 → 绑定 Key → 预约/激活 → 使用计费 → 退出结算 → 历史留档。</strong>
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
              <strong>使用计费</strong>
              <p>请求费按实际用量结算，小时费按激活分钟预扣；单个低消核销窗口最长 1 小时。</p>
            </div>
            <div class="account-share-guide-step">
              <span>4</span>
              <strong>退出结算</strong>
              <p>主动退出或达到空闲时限后释放席位并完成核销；窗口消费达标则退回对应小时费。</p>
            </div>
            <div class="account-share-guide-step">
              <span>5</span>
              <strong>历史留档</strong>
              <p>每次加入/使用记录按 membership 独立留档，房间删除后仍可核对当时条款与消费。</p>
            </div>
          </div>

          <div class="account-share-guide-section">
            <h4>房间与席位规则</h4>
            <div class="account-share-guide-rule-list">
              <div>
                <Icon name="users" size="sm" />
                <p><strong>成员上限由房主设置</strong>，最低 1 人、最高 30 人；不按房间账号数量或账号并发推导。</p>
              </div>
              <div>
                <Icon name="user" size="sm" />
                <p><strong>房主自用不占消费者名额</strong>；成员上限只约束同时使用房间的消费者人数。</p>
              </div>
              <div>
                <Icon name="database" size="sm" />
                <p><strong>账号数量使用独立配额</strong>，账号请求并发只决定请求处理能力，与房间成员上限相互独立。</p>
              </div>
            </div>
          </div>

          <div class="account-share-guide-section">
            <h4>角色与权限边界</h4>
            <dl class="account-share-guide-param-grid">
              <div>
                <dt>消费者</dt>
                <dd>可加入、预约、退出，并查看自己的 membership 消费与结算历史。</dd>
              </div>
              <div>
                <dt>房主</dt>
                <dd>可管理自己的房间及普通参数；存在活跃使用、待结算或冲突操作时不能变更受保护配置。</dd>
              </div>
              <div>
                <dt>管理员</dt>
                <dd>保留最高参数修改和强制处理能力；管理员最高处理权限不向房主开放。</dd>
              </div>
            </dl>
          </div>

          <div class="account-share-guide-section">
            <h4>删除房间与历史快照</h4>
            <div class="account-share-guide-rule-list">
              <div>
                <Icon name="shield" size="sm" />
                <p><strong>删除前必须完成清场。</strong>存在活跃、排队或退出中成员，以及进行中请求、待结算、有效编辑会话或其他冲突操作时，不能删除房间。</p>
              </div>
              <div>
                <Icon name="database" size="sm" />
                <p><strong>删除房间采用软删除。</strong>房间转为只读归档，历史订单、membership、账单与审计记录不会被物理清除。</p>
              </div>
              <div>
                <Icon name="clock" size="sm" />
                <p><strong>历史展示删除时保存的精确房间条款快照。</strong>旧数据缺少快照或快照损坏时会明确显示“不可恢复”，不会拿当前房间参数冒充历史。</p>
              </div>
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
                <dt>配置并发/运行时请求能力</dt>
                <dd>账号级请求处理能力的配置值，不代表实时空闲容量，也不决定成员上限。</dd>
              </div>
              <div>
                <dt>房间成员上限</dt>
                <dd>由房主在 1～30 人范围内设置，仅约束消费者席位，房主自用不占用。</dd>
              </div>
              <div>
                <dt>房间账号数量</dt>
                <dd>受独立账号配额约束，不随成员上限或账号请求并发自动变化。</dd>
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
              近3天均值按你在当前平台的全部 API Key 汇总，不按所选 Key 单独统计；所选 Key 仅用于确定测算计费分组和后续加入房间。历史 Cache 读取无法可靠拆分文本和图片，两项均保留手工填写值。
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
              <span>单次文本输入 Token</span>
              <input v-model.number="recommendationForm.input_tokens_per_request" class="input h-10" type="number" min="0" step="1" />
            </label>
            <label class="field">
              <span>单次文本输出 Token</span>
              <input v-model.number="recommendationForm.output_tokens_per_request" class="input h-10" type="number" min="0" step="1" />
            </label>
            <label class="field">
              <span>单次 Cache 写入</span>
              <input v-model.number="recommendationForm.cache_creation_tokens_per_request" class="input h-10" type="number" min="0" step="1" />
            </label>
            <label class="field">
              <span>单次文本 Cache 读取</span>
              <input v-model.number="recommendationForm.cache_read_tokens_per_request" class="input h-10" type="number" min="0" step="1" />
            </label>
            <label class="field">
              <span>单次图片输入 Token</span>
              <input v-model.number="recommendationForm.image_input_tokens_per_request" class="input h-10" type="number" min="0" step="1" />
            </label>
            <label class="field">
              <span>单次图片输出 Token</span>
              <input v-model.number="recommendationForm.image_output_tokens_per_request" class="input h-10" type="number" min="0" step="1" />
            </label>
            <label class="field">
              <span>单次图片 Cache 读取</span>
              <input v-model.number="recommendationForm.image_cache_read_tokens_per_request" class="input h-10" type="number" min="0" step="1" />
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
              <div v-if="candidate.estimate.owner_self_use && selfUseSettingsError" class="recommendation-warnings">
                <span>{{ selfUseSettingsError }}</span>
              </div>

              <div class="recommendation-card-actions">
                <span>消费者名额 {{ candidate.listing.active_seats }}/{{ candidate.listing.seat_limit }} · 配置并发 {{ candidate.listing.account_concurrency }}</span>
                <button
                  class="btn-primary h-10"
                  type="button"
                  :disabled="preparingJoinId !== null || joiningId !== null || selfUseJoinUnavailable(candidate.listing)"
                  :title="selfUseJoinUnavailable(candidate.listing) ? selfUseSettingsError : undefined"
                  @click="useRecommendedListing(candidate)"
                >
                  <Icon name="login" size="sm" class="mr-2" />
                  {{ preparingJoinId === candidate.listing.id ? '准备确认中' : '加入使用' }}
                </button>
              </div>
            </article>
          </template>
        </div>
        </section>
      </BaseDialog>

      <CreateRoomDialog
        :show="showCreate"
        :busy="creating || generatingOAuthURL"
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
            房间 {{ capabilities.live_rooms.used }}/{{ capabilities.live_rooms.limit }}
          </span>
          <span>
            24 小时创建 {{ capabilities.room_creates_24_hours.used }}/{{ capabilities.room_creates_24_hours.limit }}
          </span>
          <span>
            房间账号 {{ capabilities.owner_room_accounts.used }}/{{ capabilities.owner_room_accounts.limit }}
          </span>
          <strong v-if="!capabilities.can_create_room">
            {{ capabilities.capability_blockers[0]?.message || '当前暂不能创建房间' }}
          </strong>
        </div>

        <div class="create-room-source-stage">
          <div class="create-room-stage-heading">
            <span class="create-room-stage-index">1</span>
            <div>
              <strong>选择账号来源</strong>
              <small>已有账号可直接创建；只有尚未登录的账号才需要 OAuth。</small>
            </div>
          </div>
          <div class="create-room-source-grid">
            <button
              type="button"
              class="create-source-option"
              :class="createSourceMode === 'existing' && 'create-source-option-active'"
              :disabled="creating"
              @click="selectCreateSourceMode('existing')"
            >
              <span class="create-source-icon"><Icon name="database" size="sm" /></span>
              <span>
                <strong>选择已有账号</strong>
                <small>推荐：保留账号 ID、凭证和代理，直接创建房间。</small>
              </span>
            </button>
            <button
              type="button"
              class="create-source-option"
              :class="createSourceMode === 'oauth' && 'create-source-option-active'"
              :disabled="creating"
              @click="selectCreateSourceMode('oauth')"
            >
              <span class="create-source-icon"><Icon name="login" size="sm" /></span>
              <span>
                <strong>登录新账号</strong>
                <small>仅在账号尚未登录时，使用 OAuth 创建新账号和房间。</small>
              </span>
            </button>
          </div>

          <div v-if="createSourceMode === 'existing'" class="create-room-account-picker">
            <div class="flex flex-col gap-3 md:flex-row md:items-end">
              <label class="field min-w-0 flex-1">
                <span>已有自有账号</span>
                <select
                  v-model.number="selectedOwnedAccountID"
                  class="input"
                  :disabled="ownedAccountsLoading || creating"
                >
                  <option :value="0">
                    {{ ownedAccountsLoading ? '正在加载账号...' : '请选择未外投的健康账号' }}
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
                刷新账号
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
                  <span>房间规则与计费</span>
                <small>{{ createSourceMode === 'existing' ? '房间会沿用所选账号的凭证、代理和配置并发。' : '新账号需要代理、成员上限和配置并发，授权前请先确认。' }}</small>
                </div>
              </div>
              <div class="create-room-field-grid">
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
                  <span>房间名称</span>
                  <input v-model="createForm.name" class="input" :placeholder="ACCOUNT_NAME_BASE_BY_PLATFORM[createPlatform]" />
                  <small :class="accountNameValidationMessage ? 'text-red-600 dark:text-red-300' : ''">
                    {{ accountNameValidationMessage || '同一号主下房间名称必须唯一，且不能包含空格、换行或制表符。' }}
                  </small>
                </label>

                <div v-if="createSourceMode === 'oauth'" class="field md:col-span-2 2xl:col-span-2">
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
                  <span>成员上限（1～30）</span>
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
                  <small>{{ ACCOUNT_SHARE_MEMBER_LIMIT_HELP }}</small>
                </label>

                <label v-if="createSourceMode === 'oauth'" class="field">
                  <span>配置并发/运行时请求能力</span>
                  <input v-model.number="createForm.concurrency" class="input" type="number" min="1" :max="MAX_ACCOUNT_CONCURRENCY" step="1" />
                  <small :class="concurrencyValidationMessage ? 'text-red-600 dark:text-red-300' : ''">
                    {{ concurrencyValidationMessage || `账号级请求能力配置为 1-${MAX_ACCOUNT_CONCURRENCY}，不决定成员上限。` }}
                  </small>
                </label>
                <div v-else class="field">
                  <span>配置并发/运行时请求能力</span>
                  <div class="input flex items-center bg-gray-50 text-gray-700 dark:bg-dark-800 dark:text-dark-200">
                    {{ selectedOwnedAccount?.concurrency ?? '—' }}
                  </div>
                  <small>沿用已有账号的运行时请求能力配置，不在创建房间时修改，也不决定成员上限。</small>
                </div>

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

            <div class="form-section create-room-stage-card">
              <div class="section-heading create-room-stage-heading">
                <span class="create-room-stage-index">3</span>
                <div>
                  <span>模型与额度保护</span>
                <small>{{ createPlatform === 'openai' ? '后端会强制账号模式、ctx_pool 和 Compact 配置，前端只提交可变策略。' : 'Anthropic 账号模式提交 OAuth 凭证、代理、模型白名单和 Claude 额度保护。' }}</small>
                </div>
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
                  <small>关闭后会允许更多客户端加入该账号房间。</small>
                </span>
              </label>
            </div>
          </div>

          <div class="create-room-submit-stage">
            <div class="create-room-submit-content">
              <div class="create-room-stage-heading">
                <span class="create-room-stage-index">4</span>
                <div>
                  <strong>{{ createSourceMode === 'oauth' ? '授权并创建' : '确认创建' }}</strong>
                  <small>{{ createSourceMode === 'oauth' ? '完成 OAuth 授权后，系统将创建账号并上架房间。' : '系统会保留所选账号的凭证、代理和账号 ID。' }}</small>
                </div>
              </div>
              <OAuthAuthorizationFlow
                v-if="createSourceMode === 'oauth'"
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

              <button
                v-if="createSourceMode === 'oauth'"
                class="btn-primary create-room-submit-button"
                type="button"
                :disabled="creating || !canSubmitOAuth"
                @click="submitOAuth"
              >
                <svg v-if="creating" class="-ml-1 mr-2 h-4 w-4 animate-spin" fill="none" viewBox="0 0 24 24">
                  <circle class="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="4"></circle>
                  <path class="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"></path>
                </svg>
                <Icon v-else name="checkCircle" size="sm" class="mr-2" />
                {{ creating ? '创建中...' : `完成 ${platformLabel(createPlatform)} OAuth 并上架` }}
              </button>
              <button
                v-else
                class="btn-primary create-room-submit-button"
                type="button"
                :disabled="creating || !canCreateRoomFromOwnedAccount"
                @click="createRoomFromOwnedAccount"
              >
                <Icon name="plus" size="sm" class="mr-2" :class="{ 'animate-pulse': creating }" />
                {{ creating ? '创建房间中...' : '使用已有账号创建房间' }}
              </button>
            </div>
          </div>
        </div>
      </CreateRoomDialog>

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

      <section ref="filterPanelRef" class="filter-panel" @keydown.esc="handleFilterPopoverEscape">
        <div class="filter-toolbar">
          <div class="filter-primary-row">
            <label v-if="!isMembershipHistoryView" class="filter-search">
              <Icon name="search" size="sm" />
              <input v-model.trim="searchQuery" class="filter-search-input" placeholder="搜索账号、号主或模型" />
            </label>
            <div
              v-else
              class="flex min-h-11 min-w-0 flex-1 items-center gap-2 rounded-xl border border-sky-200 bg-sky-50 px-3 text-sm leading-6 text-sky-800 dark:border-sky-900/60 dark:bg-sky-950/30 dark:text-sky-200"
            >
              <Icon name="clock" size="sm" class="flex-none" />
              <span>按每次加入独立展示，包含所有平台；搜索和实时状态筛选不适用于不可变历史。</span>
            </div>
            <div class="filter-actions" aria-label="账号广场分类">
              <button
                type="button"
                class="owner-filter-button"
                :class="activeFilter.key === ownerFilter.key && 'owner-filter-button-active'"
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

          <div
            v-if="isArchiveView"
            class="flex min-h-11 items-center gap-2 border-t border-slate-200 px-4 py-3 text-sm leading-6 text-slate-600 dark:border-dark-700 dark:text-dark-300"
            data-testid="archive-readonly-notice"
          >
            <Icon name="document" size="sm" class="flex-none" />
            <span>归档仅展示已删除房间的不可变快照；实时状态、运行时用量和管理操作不会在这里显示。</span>
          </div>
          <div v-else-if="!isMembershipHistoryView" class="filter-body">
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
                  aria-label="状态选项"
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
                <span class="filter-section-label">账号等级</span>
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
                  aria-label="账号等级选项"
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
                <span class="filter-section-label">账号席位</span>
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
                  aria-label="账号席位选项"
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
                      {{ seat }}人
                    </button>
                  </div>
                </div>
              </div>

              <div class="filter-popover-wrap">
                <span class="filter-section-label">标签</span>
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
                  aria-label="标签选项"
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
                <span class="filter-section-label">可用模型</span>
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
                  aria-label="可用模型选项"
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

      <MembershipHistoryPanel
        v-if="isMembershipHistoryView"
        :items="membershipHistoryEntries"
        :loading="membershipHistoryLoading"
        :error-message="membershipHistoryError"
        :page="membershipHistoryPagination.page"
        :page-size="membershipHistoryPagination.page_size"
        :total="membershipHistoryPagination.total"
        @reload="loadMembershipHistory"
        @update:page="handleMembershipHistoryPageChange"
        @review="openHistoryReviewDialog"
      />

      <template v-else>
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
                  <h2 class="text-lg font-semibold text-gray-950 dark:text-white">房间 ID：#{{ listing.id }}</h2>
                  <span class="rounded-full bg-gray-200 px-3 py-1 text-xs font-medium text-gray-700 dark:bg-dark-700 dark:text-dark-200">
                    已删除
                  </span>
                </div>
                <div class="space-y-2 text-sm text-gray-600 dark:text-dark-200">
                  <p>最后使用：{{ listing.last_used_at ? formatDate(listing.last_used_at) : '时间不可恢复' }}</p>
                  <p class="leading-6 text-amber-800 dark:text-amber-200">
                    该记录生成于历史快照功能上线前，迁移前的房间详情与使用条款不可恢复。
                  </p>
                </div>
              </div>

              <template v-else>
                <header class="flex flex-col gap-4 sm:flex-row sm:items-start sm:justify-between">
                  <div class="min-w-0">
                    <div class="flex flex-wrap items-center gap-2">
                      <span class="feature-badge">{{ platformLabel(listingPlatform(listing)) }}</span>
                      <span v-if="isOpenAIListing(listing)" :class="accountLevelBadgeClass(listing)">
                        {{ accountLevelBadgeLabel(listing) }}
                      </span>
                      <span class="inline-flex min-h-7 items-center rounded-full bg-slate-200 px-3 text-xs font-semibold text-slate-700 dark:bg-dark-700 dark:text-dark-200">
                        已删除
                      </span>
                    </div>
                    <h2 class="mt-3 break-words text-lg font-semibold text-slate-950 dark:text-white">
                      {{ listing.room_name || `房间 #${listing.id}` }}
                    </h2>
                    <p class="mt-1 break-words text-sm leading-6 text-slate-600 dark:text-dark-300">
                      号主：{{ listing.owner_username || `用户 ${listing.owner_user_id}` }} · 房间 #{{ listing.id }}
                    </p>
                  </div>
                  <span class="inline-flex min-h-11 flex-none items-center justify-center rounded-xl bg-slate-100 px-4 text-sm font-semibold text-slate-700 dark:bg-dark-800 dark:text-dark-200">
                    只读历史快照
                  </span>
                </header>

                <div
                  v-if="isBackfilledHistorySnapshot(listing)"
                  class="rounded-lg border border-amber-200 bg-amber-50 px-4 py-3 text-sm leading-6 text-amber-800 dark:border-amber-800 dark:bg-amber-950/20 dark:text-amber-200"
                  data-testid="backfilled-history-notice"
                >
                  这条记录由当前或最终房间信息回填，不是删除当时保存的精确快照。
                </div>

                <div
                  class="grid gap-3 sm:grid-cols-2 lg:grid-cols-3"
                  data-testid="archive-terms-snapshot"
                >
                  <div class="rounded-xl bg-slate-50 p-3 dark:bg-dark-800">
                    <span class="text-xs font-medium text-slate-500 dark:text-dark-400">历史成员上限</span>
                    <strong class="mt-1 block text-sm text-slate-950 dark:text-white">{{ listing.seat_limit }} 人</strong>
                  </div>
                  <div class="rounded-xl bg-slate-50 p-3 dark:bg-dark-800">
                    <span class="text-xs font-medium text-slate-500 dark:text-dark-400">历史单用户并发</span>
                    <strong class="mt-1 block text-sm text-slate-950 dark:text-white">{{ listing.per_user_concurrency }}</strong>
                  </div>
                  <div class="rounded-xl bg-slate-50 p-3 dark:bg-dark-800">
                    <span class="text-xs font-medium text-slate-500 dark:text-dark-400">历史费率倍率</span>
                    <strong class="mt-1 block text-sm text-slate-950 dark:text-white">{{ formatNumber(listing.rate_multiplier) }}x</strong>
                  </div>
                  <div class="rounded-xl bg-slate-50 p-3 dark:bg-dark-800">
                    <span class="text-xs font-medium text-slate-500 dark:text-dark-400">历史小时费</span>
                    <strong class="mt-1 block text-sm text-slate-950 dark:text-white">{{ formatNumber(listing.hourly_rate) }}</strong>
                  </div>
                  <div class="rounded-xl bg-slate-50 p-3 dark:bg-dark-800">
                    <span class="text-xs font-medium text-slate-500 dark:text-dark-400">历史免小时费低消</span>
                    <strong class="mt-1 block text-sm text-slate-950 dark:text-white">{{ hourlyFeeWaiverLabel(listing.hourly_fee_waiver_minimum) }}</strong>
                  </div>
                  <div class="rounded-xl bg-slate-50 p-3 dark:bg-dark-800">
                    <span class="text-xs font-medium text-slate-500 dark:text-dark-400">历史最低余额</span>
                    <strong class="mt-1 block text-sm text-slate-950 dark:text-white">{{ formatNumber(listing.min_balance_required) }}</strong>
                  </div>
                </div>

                <div class="rounded-xl border border-slate-200 bg-slate-50/70 p-3 dark:border-dark-700 dark:bg-dark-800/70">
                  <span class="text-xs font-medium text-slate-500 dark:text-dark-400">历史允许模型</span>
                  <div class="mt-2 flex flex-wrap gap-2">
                    <span
                      v-for="model in listing.allowed_models"
                      :key="model"
                      class="max-w-full break-all rounded-lg bg-white px-2.5 py-1 text-xs text-slate-700 ring-1 ring-slate-200 dark:bg-dark-900 dark:text-dark-200 dark:ring-dark-600"
                    >
                      {{ model }}
                    </span>
                    <span v-if="listing.allowed_models.length === 0" class="text-sm text-slate-500 dark:text-dark-400">
                      未记录
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
                <h2 class="text-lg font-semibold text-gray-950 dark:text-white">房间 ID：#{{ listing.id }}</h2>
                <span class="rounded-full bg-gray-200 px-3 py-1 text-xs font-medium text-gray-700 dark:bg-dark-700 dark:text-dark-200">
                  {{ listing.deleted ? '已删除' : '历史记录' }}
                </span>
              </div>
              <div class="space-y-2 text-sm text-gray-600 dark:text-dark-200">
                <p>最后使用：{{ listing.last_used_at ? formatDate(listing.last_used_at) : '时间不可恢复' }}</p>
                <p class="leading-6 text-amber-800 dark:text-amber-200">
                  该记录生成于历史快照功能上线前，迁移前的房间详情与使用条款不可恢复。
                </p>
              </div>
            </div>
          </template>
          <template v-else>
          <div
            v-if="isBackfilledHistorySnapshot(listing)"
            class="mb-4 rounded-lg border border-amber-200 bg-amber-50 px-4 py-3 text-sm leading-6 text-amber-800 dark:border-amber-800 dark:bg-amber-950/20 dark:text-amber-200"
            data-testid="backfilled-history-notice"
          >
            这条历史记录由当前或最终房间信息回填，不是本次使用当时保存的精确快照。
          </div>
          <div class="listing-card-head">
            <div class="listing-card-main-row">
              <div class="listing-card-identity">
                <div class="listing-title-row">
                  <div class="listing-title-line">
                    <h2 class="listing-title">{{ listingDisplayName(listing) }}</h2>
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
                  </div>
                  <span class="listing-owner">
                    号主：{{ listing.owner_username || `用户 ${listing.owner_user_id}` }}
                    <button
                      type="button"
                      class="owner-inline-button"
                      :title="isOwnListing(listing) ? '管理房间账号' : ownerDialogButtonTitle(listing)"
                      @click="isOwnListing(listing) ? openRoomAccountsDialog(listing) : openOwnerDialog(listing)"
                    >
                      <Icon :name="isOwnListing(listing) ? 'database' : 'eye'" size="xs" />
                      <span>{{ isOwnListing(listing) ? '管理账号' : '查看号主' }}</span>
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
                <span :class="listingStatusBadgeClass(listing)">
                  {{ listingStatusLabel(listing) }}
                </span>
                <div class="listing-member-limit">
                  <span class="listing-seat-pill">
                    席位 {{ listing.active_seats }}/{{ listing.seat_limit }}
                  </span>
                </div>
              </div>
            </div>
          </div>

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
                    <span>{{ listing.deleted || listing.status !== 'active' ? '并发状态' : '可用并发' }}</span>
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
                    <span>5H 综合已用</span>
                    <strong>{{ roomWindowUtilizationLabel(listing.quota_summary.window_5h) }}</strong>
                  </div>
                  <div
                    v-if="roomWindowUtilization(listing.quota_summary.window_5h) !== null"
                    class="combined-availability-track"
                    role="progressbar"
                    aria-label="房间 5H 综合已用量"
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
                    <span>7D 综合已用</span>
                    <strong>{{ roomWindowUtilizationLabel(listing.quota_summary.window_7d) }}</strong>
                  </div>
                  <div
                    v-if="roomWindowUtilization(listing.quota_summary.window_7d) !== null"
                    class="combined-availability-track"
                    role="progressbar"
                    aria-label="房间 7D 综合已用量"
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
              <span class="metric-label"><Icon name="users" size="xs" />可调度总并发</span>
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
              <div v-if="isListingMembershipEnding(listing)" class="edit-lock-strip">
                <Icon name="refresh" size="sm" class="animate-spin" />
                <span>退出结算处理中，结算完成后才能重新加入或排队。</span>
              </div>
              <div v-if="isOwnListing(listing) && selfUseSettingsError" class="edit-lock-strip">
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
                      <small>账号模式 Key</small>
                    </span>
                    <Icon v-if="selected" name="check" size="sm" class="text-primary-500" />
                  </template>
                </Select>
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
                <button
                  class="btn-primary h-9"
                  type="button"
                  :disabled="isListingMembershipEnding(listing) || listingEditLocked(listing) || modeKeysLoading || preparingJoinId !== null || joiningId !== null || selfUseJoinUnavailable(listing)"
                  :title="isListingMembershipEnding(listing) ? '退出结算处理中' : (selfUseJoinUnavailable(listing) ? selfUseSettingsError : undefined)"
                  @click="joinUse(listing)"
                >
                  {{ isListingMembershipEnding(listing) ? '退出结算处理中' : (preparingJoinId === listing.id ? '准备确认中' : (joiningId === listing.id ? (isOwnListing(listing) ? '绑定中' : '加入中') : (modeKeysLoading ? '加载 Key 中' : (isOwnListing(listing) ? (selfUseSettingsLoading ? '加载自用配置' : (selfUseSettingsError ? '自用配置不可用' : '使用自己的账号')) : '加入使用')))) }}
                </button>
              </div>
            </div>

          </div>

          <template v-if="isManagementView">
            <div class="mt-3 rounded-lg border border-gray-200 bg-gray-50 p-3 text-sm dark:border-dark-700 dark:bg-dark-800/60">
              <div class="flex flex-col gap-1 text-gray-600 dark:text-dark-200">
                <span>房间 ID：#{{ listing.id }}</span>
                <span>房间账号：{{ roomAggregateAccountCountLabel(listing) }}</span>
                <span>更新：{{ formatDate(listing.updated_at) }}</span>
              </div>
              <div v-if="!listing.deleted" class="listing-management-actions">
                <button
                  type="button"
                  class="btn-secondary listing-management-action"
                  @click="openRoomAccountsDialog(listing)"
                >
                  <Icon name="database" size="xs" />
                  查看房间账号
                </button>
                <button
                  type="button"
                  class="btn-secondary listing-management-action"
                  :disabled="managedActionId === listing.id"
                  :title="listingEditLockedByOther(listing) ? listingEditLockLabel(listing) : ''"
                  @click="requestOpenConfigEdit(listing)"
                >
                  <Icon name="edit" size="xs" />
                  编辑配置
                </button>
                <button
                  v-if="(isOwnListing(listing) || authStore.isAdmin) && capabilities?.lifecycle_enabled !== false"
                  type="button"
                  class="btn-secondary listing-management-action"
                  data-testid="room-lifecycle-entry"
                  @click="openRoomLifecycleDialog(listing)"
                >
                  <Icon name="cog" size="xs" />
                  房间管理
                </button>
              </div>
              <div
                v-else
                class="mt-3 rounded-lg border border-gray-200 bg-white px-3 py-2 text-sm text-gray-500 dark:border-dark-700 dark:bg-dark-900 dark:text-dark-300"
              >
                {{ deletedHistorySnapshotMessage(listing) }}
              </div>
              <div v-if="listingEditLocked(listing)" class="edit-lock-strip mt-3">
                <Icon name="exclamationCircle" size="sm" />
                <span>{{ listingEditLockLabel(listing) }}</span>
              </div>
            </div>
          </template>
          <div
            v-if="listing.queue_membership_id || listing.current_membership_id"
            class="account-share-membership-panel"
            :class="{ 'account-share-membership-panel-ending': isListingMembershipEnding(listing) }"
          >
            <div class="membership-status-head">
              <div>
                <div class="membership-title">
                  {{ membershipPanelTitle(listing) }}，绑定 {{ boundApiKeyDisplayName(listing) }}
                </div>
                <div class="membership-subtitle">
                  {{ membershipPanelSubtitle(listing) }}
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
                  <div v-if="!isListingMembershipEnding(listing) && waiverProgressVisible(listing)">
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
                  <div v-if="listing.current_billed_until && (isListingMembershipEnding(listing) || !waiverProgressVisible(listing))">
                    <span>已结算到</span>
                    <strong>{{ formatDate(listing.current_billed_until) }}</strong>
                  </div>
                  <div v-if="listing.queue_dispatch_cooldown_until">
                    <span>失败冷却</span>
                    <strong>{{ formatRelativeUntil(listing.queue_dispatch_cooldown_until) }}</strong>
                  </div>
                </div>

                <div
                  v-if="!isListingMembershipEnding(listing) && waiverProgressVisible(listing)"
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
                    <strong>{{ queueStatusLabel(listing) }}</strong>
                    <span>{{ membershipPanelSubtitle(listing) }}</span>
                  </div>
                </div>
                <template v-else>
                <div class="idle-timeout-control">
                  <label :for="`idle-timeout-current-${listing.id}`">空闲退出</label>
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
                    <span>分钟</span>
                    <button
                      class="btn-secondary min-h-11"
                      type="button"
                      :disabled="savingIdleTimeoutId === Number(listing.queue_membership_id || listing.current_membership_id || 0)"
                      @click="saveIdleTimeout(listing)"
                    >
                      保存
                    </button>
                  </div>
                </div>
                <div class="membership-action-row">
                  <button
                    class="btn-secondary min-h-11"
                    type="button"
                    :disabled="!canMoveQueueItem(listing, -1)"
                    @click="moveQueueItem(listing, -1)"
                  >
                    <Icon name="chevronUp" size="xs" class="mr-2" />
                    上移
                  </button>
                  <button
                    class="btn-secondary min-h-11"
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
                    :disabled="endingId !== null || isListingMembershipEnding(listing)"
                    @click="handleEndUseClick(listing)"
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
                </template>
              </div>
            </div>
          </div>
          </template>
        </article>
      </section>

      <div v-else class="rounded-lg border border-dashed border-gray-300 bg-white p-8 text-center text-sm text-gray-500 dark:border-dark-700 dark:bg-dark-900 dark:text-dark-300">
        {{ isKeyResolutionMode ? (keyResolutionError ? '关联房间详情暂时无法加载，请在上方刷新状态后重试。' : '当前 API Key 没有需要处理的关联房间。') : (pagination.total === 0 ? (hasResultFilters ? '没有匹配的账号房间。' : (isArchiveView ? '暂无已删除房间。' : (isManagementView ? '暂无可管理房间。' : '当前分类暂无房间。'))) : '当前页暂无房间。') }}
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
      </template>
    </div>

    <BaseDialog
      :show="pendingJoinConfirmation !== null"
      :title="pendingJoinIsOwnerSelfUse ? '确认使用自己的账号' : '确认加入账号房间'"
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
            <strong>{{ pendingJoinTerms.room_name || `房间 #${pendingJoinIntent.listing_id}` }}</strong>
            <span>{{ pendingJoinIsOwnerSelfUse ? `这是你自己的房间。绑定后按全局自用倍率 ${ownerSelfUseRateMultiplierLabel} 计算请求费用，不收小时费，也不占用消费者名额。` : '以下内容来自服务端刚刚签发的条款快照；确认后，该 API Key 会按这份快照加入房间。' }}</span>
          </div>
        </div>

        <div v-if="pendingJoinPriceWarnings.length > 0" class="join-warning-list">
          <div v-for="warning in pendingJoinPriceWarnings" :key="warning" class="join-warning-item">
            <Icon name="exclamationCircle" size="sm" />
            <span>{{ warning }}</span>
          </div>
        </div>

        <div class="join-confirmation-grid">
          <div class="join-confirmation-field">
            <span>条款版本</span>
            <strong>v{{ pendingJoinTerms.row_version }} · rev {{ pendingJoinTerms.listing_revision_id || 0 }}</strong>
          </div>
          <div class="join-confirmation-field">
            <span>房间状态</span>
            <strong>{{ pendingJoinTerms.status === 'active' ? '可加入' : pendingJoinTerms.status }}</strong>
          </div>
          <div class="join-confirmation-field">
            <span>消费者名额</span>
            <strong>{{ pendingJoinTerms.seat_limit }}</strong>
          </div>
          <div class="join-confirmation-field" :class="{ 'join-price-danger': !pendingJoinIsOwnerSelfUse && pendingJoinTerms.rate_multiplier > 1 }">
            <span>{{ pendingJoinIsOwnerSelfUse ? '自用请求倍率' : '倍率' }}</span>
            <strong>{{ pendingJoinIsOwnerSelfUse ? ownerSelfUseRateMultiplierLabel : `${formatNumber(pendingJoinTerms.rate_multiplier)}x` }}</strong>
          </div>
          <div v-if="pendingJoinIsOwnerSelfUse" class="join-confirmation-field">
            <span>公开条款倍率</span>
            <strong>{{ formatNumber(pendingJoinTerms.rate_multiplier) }}x</strong>
          </div>
          <div class="join-confirmation-field" :class="{ 'join-price-danger': !pendingJoinIsOwnerSelfUse && pendingJoinTerms.hourly_rate > EXPENSIVE_HOURLY_RATE }">
            <span>小时费</span>
            <strong>{{ pendingJoinIsOwnerSelfUse ? '不收取' : formatNumber(pendingJoinTerms.hourly_rate) }}</strong>
          </div>
          <div class="join-confirmation-field">
            <span>免小时费低消</span>
            <strong>{{ pendingJoinIsOwnerSelfUse ? '不适用' : hourlyFeeWaiverLabel(pendingJoinTerms.hourly_fee_waiver_minimum) }}</strong>
          </div>
          <div class="join-confirmation-field">
            <span>最低余额</span>
            <strong>{{ pendingJoinIsOwnerSelfUse ? '不校验' : formatNumber(pendingJoinTerms.min_balance_required) }}</strong>
          </div>
          <div class="join-confirmation-field">
            <span>单用户并发</span>
            <strong>{{ pendingJoinTerms.per_user_concurrency }}</strong>
          </div>
          <div class="join-confirmation-field">
            <span>绑定 Key</span>
            <strong>{{ pendingJoinApiKeyLabel }}</strong>
          </div>
          <div class="join-confirmation-field">
            <span>空闲退出</span>
            <strong>{{ pendingJoinIdleTimeoutLabel }}</strong>
          </div>
          <div v-if="pendingJoinHasOpenAIProtection" class="join-confirmation-field">
            <span>Codex保护</span>
            <strong>{{ pendingJoinTerms.codex_5h_limit_percent }}% / {{ pendingJoinTerms.codex_7d_limit_percent }}%</strong>
          </div>
          <div v-if="pendingJoinHasAnthropicProtection" class="join-confirmation-field">
            <span>Claude保护</span>
            <strong>{{ pendingJoinTerms.anthropic_5h_limit_percent || 0 }}% / {{ pendingJoinTerms.anthropic_7d_limit_percent || 0 }}%</strong>
          </div>
        </div>

        <label
          class="join-queue-consent"
          :class="{ 'join-queue-consent-required': pendingJoinIntent.queue_may_be_required && !pendingJoinIntent.accept_queue }"
        >
          <input
            type="checkbox"
            :checked="pendingJoinIntent.accept_queue"
            :disabled="joinDialogBusy"
            data-testid="join-accept-queue"
            @change="updatePendingJoinQueueAcceptance"
          />
          <span>
            <strong>席位不足时，我同意进入预约队列</strong>
            <small v-if="pendingJoinIntent.queue_may_be_required">当前状态可能需要预约；必须明确勾选后才能继续，预约会在该 Key 下按顺序等待。</small>
            <small v-else>当前预计可直接加入；若提交瞬间席位已满，未勾选时系统不会擅自把你加入预约队列。</small>
          </span>
        </label>

        <div v-if="refreshingJoinIntent" class="join-intent-state" data-testid="join-intent-refreshing">
          <Icon name="refresh" size="sm" class="animate-spin" />
          <span>正在按你的排队选择重新签发确认条款...</span>
        </div>
        <div v-else-if="joinIntentError" class="join-intent-state join-intent-state-error" data-testid="join-intent-error">
          <Icon name="exclamationCircle" size="sm" />
          <span>{{ joinIntentError }}</span>
        </div>

        <div class="join-usage-reminder">
          <Icon name="infoCircle" size="sm" />
          <span>{{ pendingJoinIsOwnerSelfUse ? `确认使用后，连续空闲达到 ${pendingJoinIdleTimeoutLabel} 会自动解除绑定；自用期间不产生小时费和号主收益。` : `确认令牌有效至 ${formatDate(pendingJoinIntent.expires_at)}。若进入预约，下一次使用该 Key 发出 API 请求时会按顺序尝试激活；连续空闲达到 ${pendingJoinIdleTimeoutLabel} 会自动退出并停止占位。` }}</span>
        </div>

        <div class="join-model-confirmation">
          <span>可用模型</span>
          <div>
            <button
              v-for="model in pendingJoinVisibleModels"
              :key="model"
              type="button"
              class="model-copy-chip"
              :title="`复制 ${model}`"
              @click="copyModelName(model)"
            >
              {{ model }}
            </button>
            <span v-if="pendingJoinHiddenModelCount > 0" class="join-model-more">+{{ pendingJoinHiddenModelCount }}</span>
          </div>
        </div>
      </div>

      <template #footer>
        <button type="button" class="btn-secondary min-h-11" :disabled="joinDialogBusy" @click="closeJoinConfirmation">取消</button>
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
          {{ refreshingJoinIntent ? '更新条款中' : (joiningId !== null ? '提交中' : (pendingJoinIsOwnerSelfUse ? '确认使用' : '确认加入')) }}
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
            <button type="button" class="btn-secondary min-h-11" :disabled="mySpendAccountsLoading" @click="loadMySpendAccountOptions()">
              <Icon name="refresh" size="xs" class="mr-2" :class="{ 'animate-spin': mySpendAccountsLoading }" />
              刷新账号
            </button>
          </div>

          <div v-if="mySpendAccountsError" class="notice-row border-red-200 bg-red-50 text-red-700 dark:border-red-900/60 dark:bg-red-900/20 dark:text-red-300">
            <Icon name="exclamationCircle" size="sm" class="mt-0.5 flex-shrink-0" />
            <span>{{ mySpendAccountsError }}</span>
          </div>

          <div class="my-spend-range-tabs my-spend-source-tabs" role="tablist" aria-label="消费账号记录类型">
            <button
              type="button"
              role="tab"
              :class="{ active: mySpendPickerSource === 'using' }"
              :aria-selected="mySpendPickerSource === 'using'"
              :disabled="mySpendAccountsLoading"
              @click="setMySpendPickerSource('using')"
            >
              使用/预约 {{ mySpendUsingPagination.total }}
            </button>
            <button
              type="button"
              role="tab"
              :class="{ active: mySpendPickerSource === 'history' }"
              :aria-selected="mySpendPickerSource === 'history'"
              :disabled="mySpendAccountsLoading"
              @click="setMySpendPickerSource('history')"
            >
              消费历史 {{ mySpendHistoryPagination.total }}
            </button>
          </div>

          <div v-if="mySpendAccountsLoading && mySpendAccountOptions.length === 0" class="my-spend-loading">
            正在加载使用过的账号...
          </div>
          <div v-else-if="!mySpendAccountsLoading && mySpendAccountOptions.length === 0" class="my-spend-empty">
            {{ mySpendPickerSource === 'using'
              ? '暂无正在使用或预约中的记录。'
              : '暂无已结束的消费历史。每次使用都会在结束后单独保留。' }}
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
                  <span>记录 #{{ option.membershipID }}</span>
                  <span>{{ mySpendAccountStatusLabel(option) }}</span>
                </span>
              </button>
            </div>
            <Pagination
              v-if="mySpendActivePickerPagination.total > mySpendActivePickerPagination.pageSize"
              class="overflow-hidden rounded-xl border border-slate-200 shadow-sm dark:border-dark-700"
              :page="mySpendActivePickerPagination.page"
              :total="mySpendActivePickerPagination.total"
              :page-size="mySpendActivePickerPagination.pageSize"
              :show-page-size-selector="false"
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
              {{ platformLabel(mySpendSelectedOption.platform) }} · 房间 #{{ mySpendSelectedOption.listingID }}
              <template v-if="mySpendSelectedOption.roomDeleted"> · 已删除</template>
            </span>
            <strong>{{ mySpendAccountDisplayName(mySpendSelectedOption) }}</strong>
            <small>
              号主：{{ mySpendSelectedOption.ownerUsername || `用户 ${mySpendSelectedOption.ownerUserID}` }}
              · 使用记录 #{{ mySpendSelectedOption.membershipID }}
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
              :disabled="mySpendHistorySelection && option.value !== 'current_membership'"
              :title="mySpendHistorySelection && option.value !== 'current_membership' ? '历史记录仅按选中的这一次使用精确统计' : undefined"
              role="tab"
              @click="setMySpendRange(option.value)"
            >
              {{ option.label }}
            </button>
          </div>
          <button type="button" class="btn-secondary min-h-11" :disabled="mySpendLoading || !mySpendSelectedOption" @click="loadMySpendSummary">
            <Icon name="refresh" size="xs" class="mr-2" />
            刷新
          </button>
        </div>

        <div
          v-if="mySpendHistorySelection"
          class="notice-row border-sky-200 bg-sky-50 text-sky-800 dark:border-sky-900/60 dark:bg-sky-950/30 dark:text-sky-200"
        >
          <Icon name="infoCircle" size="sm" class="mt-0.5 flex-shrink-0" />
          <span>历史记录按选中的 membership 精确统计，不会合并同一房间的其他使用记录。</span>
        </div>

        <div
          v-if="!mySpendLoading && !mySpendSelectedOption && (mySpendUsingPagination.total + mySpendHistoryPagination.total) > 0"
          class="my-spend-empty"
        >
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

    <RoomAccountsDialog
      :show="roomAccountsListing !== null"
      :listing="roomAccountsListing"
      :proxies="proxies"
      @close="closeRoomAccountsDialog"
      @changed="handleRoomAccountsChanged"
    />

    <BaseDialog
      :show="showConfigEditDialog"
      title="编辑房间配置"
      width="extra-wide"
      :close-disabled="savingConfigEdit || releasingConfigEdit || pendingDraftDiscardTarget === 'config'"
      @close="closeConfigEditDialog"
    >
      <div class="space-y-5">
        <div v-if="editingConfigListing" class="edit-context-panel">
          <div class="min-w-0">
            <span class="edit-context-eyebrow">房间 #{{ editingConfigListing.id }}</span>
            <strong>{{ listingDisplayName(editingConfigListing) }}</strong>
            <small>
              消费者名额 {{ editingConfigListing.active_seats }} / {{ editingConfigListing.seat_limit }}
              <template v-if="editingConfigListing.editing_expires_at">
                · 编辑锁 {{ formatCountdownUntil(editingConfigListing.editing_expires_at) }}到期
              </template>
            </small>
          </div>
          <span v-if="editForceActive" class="edit-force-badge">管理员强制编辑</span>
        </div>

        <div
          v-if="editForceActive"
          class="notice-row border-amber-200 bg-amber-50 text-amber-800 dark:border-amber-900/60 dark:bg-amber-900/20 dark:text-amber-200"
        >
          <Icon name="exclamationTriangle" size="sm" class="mt-0.5 flex-shrink-0" />
          <span>管理员强制编辑已确认；保存时将使用下方“本次修改原因”写入审计记录。</span>
        </div>
        <div
          v-else-if="editConsumerProtected"
          class="notice-row border-blue-200 bg-blue-50 text-blue-800 dark:border-blue-900/60 dark:bg-blue-900/20 dark:text-blue-200"
        >
          <Icon name="exclamationCircle" size="sm" class="mt-0.5 flex-shrink-0" />
          <span>房间正在被使用：只允许降费、提高单用户并发、增加模型，或在保留现有席位与预约的前提下减少席位。</span>
        </div>

        <div v-if="editErrorMessage" class="notice-row border-red-200 bg-red-50 text-red-700 dark:border-red-900/60 dark:bg-red-900/20 dark:text-red-300">
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
              刷新房间并重新编辑
            </button>
          </div>
        </div>

        <div class="grid gap-5 xl:grid-cols-[minmax(0,1fr)_320px]">
          <div class="space-y-5">
            <div class="form-section">
              <div class="section-heading">
                <span>基础配置</span>
                <small>这里只修改房间级策略；成员账号的代理和配置并发请在“我的账号”中单独管理。</small>
              </div>
              <div class="grid gap-3 md:grid-cols-2 xl:grid-cols-4">
                <label class="field">
                  <span>房间名称</span>
                  <input v-model="editForm.name" class="input" :placeholder="ACCOUNT_NAME_BASE_BY_PLATFORM[listingPlatform(editingConfigListing)]" />
                  <small :class="editAccountNameValidationMessage ? 'text-red-600 dark:text-red-300' : ''">
                    {{ editAccountNameValidationMessage || '名称必须唯一，且不能包含空格、换行或制表符。' }}
                  </small>
                </label>

                <label class="field">
                  <span>成员上限（1～30）</span>
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
                  <small>{{ ACCOUNT_SHARE_MEMBER_LIMIT_HELP }}</small>
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
                <small>模型白名单与其他房间条款统一提交，共用同一个版本校验和审计记录。</small>
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
                  <small>关闭后会允许更多客户端加入该账号房间。</small>
                </span>
              </label>
            </div>

            <div class="form-section">
              <div class="section-heading">
                <span>修改原因</span>
                <small>每次房间配置变更都会生成审计记录，请写明本次调整目的。</small>
              </div>
              <label class="field">
                <span>本次修改原因</span>
                <textarea
                  v-model="editReason"
                  class="input min-h-24"
                  maxlength="1000"
                  placeholder="例如：根据近期使用情况调整单用户并发和小时费"
                  data-testid="room-config-update-reason"
                ></textarea>
                <small :class="!editReason.trim() ? 'text-amber-700 dark:text-amber-300' : ''">
                  {{ editReason.trim().length }}/1000 · 必填，保存后不可从审计记录中移除。
                </small>
              </label>
            </div>
          </div>

          <aside class="edit-summary-panel">
            <span class="text-xs font-semibold text-gray-500 dark:text-dark-300">保存摘要</span>
            <div class="mt-3 grid gap-2">
              <div class="compact-metric">
                <span>模型</span>
                <strong>{{ editAllowedModels.length }}</strong>
              </div>
              <div class="compact-metric">
                <span>可调度账号</span>
                <strong>{{ editingConfigListing ? roomEligibleAccountCount(editingConfigListing) : 0 }}/{{ editingConfigListing ? roomAttachedAccountCount(editingConfigListing) : 0 }}</strong>
              </div>
              <div class="compact-metric">
                <span>成员上限（1～30）</span>
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
        <button type="button" class="btn-secondary" :disabled="savingConfigEdit || releasingConfigEdit" @click="() => closeConfigEditDialog()">取消</button>
        <button
          type="button"
          class="btn-primary"
          :disabled="savingConfigEdit || releasingConfigEdit || editVersionConflict || !editReason.trim() || editAllowedModels.length === 0 || Boolean(editPerUserConcurrencyValidationMessage)"
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
            <span class="text-xs font-semibold uppercase tracking-wide text-gray-500 dark:text-dark-300">{{ pendingReview.platformLabel }}</span>
            <strong class="text-base text-gray-900 dark:text-dark-50">{{ pendingReview.roomName }}</strong>
            <span class="text-sm text-gray-500 dark:text-dark-300">号主：{{ pendingReview.ownerName }}</span>
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
          <div
            v-if="ownerDialog.listingsError"
            class="flex flex-wrap items-center justify-between gap-3 rounded-lg border border-red-200 bg-red-50 p-3 text-sm text-red-700 dark:border-red-500/30 dark:bg-red-500/10 dark:text-red-200"
          >
            <span>{{ ownerDialog.listingsError }}</span>
            <button type="button" class="btn-secondary h-9" :disabled="ownerDialog.loadingListings" @click="loadOwnerListings()">
              重试账号
            </button>
          </div>
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
                <span :class="listingStatusBadgeClass(item)">{{ listingStatusLabel(item) }}</span>
              </div>
              <div class="mt-3 grid grid-cols-3 gap-2 text-xs text-gray-600 dark:text-dark-300">
                <span>消费者名额 {{ item.active_seats }}/{{ item.seat_limit }}</span>
                <span>倍率 {{ formatNumber(item.rate_multiplier) }}x</span>
                <span>小时费 {{ formatNumber(item.hourly_rate) }}</span>
              </div>
            </button>
            <div class="col-span-full flex flex-wrap items-center justify-between gap-3 text-xs text-gray-500 dark:text-dark-300">
              <span>已显示 {{ ownerDialog.listings.length }}/{{ ownerDialog.listingsTotal }}</span>
              <button
                v-if="ownerDialog.listingsPage < ownerDialog.listingsPages"
                type="button"
                class="btn-secondary h-9"
                :disabled="ownerDialog.loadingListings"
                @click="loadMoreOwnerListings"
              >
                继续加载账号
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
              重试评论
            </button>
          </div>
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
                <span>{{ review.platform ? platformLabel(review.platform) : '账号房间' }}</span>
                <span>来自 匿名用户</span>
              </div>
            </article>
            <div class="flex flex-wrap items-center justify-between gap-3 text-xs text-gray-500 dark:text-dark-300">
              <span>已显示 {{ ownerDialog.reviews.length }}/{{ ownerDialog.reviewsTotal }}</span>
              <button
                v-if="ownerDialog.reviewsPage < ownerDialog.reviewsPages"
                type="button"
                class="btn-secondary h-9"
                :disabled="ownerDialog.loadingReviews"
                @click="loadMoreOwnerReviews"
              >
                继续加载评论
              </button>
            </div>
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

    <BaseDialog
      :show="roomLifecycleListing !== null"
      :title="roomLifecycleListing ? `${listingDisplayName(roomLifecycleListing)} · 房间管理` : '房间管理'"
      width="normal"
      :close-disabled="roomLifecycleCommandBusy"
      @close="closeRoomLifecycleDialog"
    >
      <div class="room-lifecycle-dialog" data-testid="room-lifecycle-dialog">
        <div
          v-if="roomLifecycleLoading"
          class="room-lifecycle-state-message"
          data-testid="room-lifecycle-loading"
        >
          <Icon name="refresh" size="sm" class="animate-spin" />
          <span>正在读取房间的最新状态...</span>
        </div>

        <div
          v-if="roomLifecycleError"
          class="room-lifecycle-alert room-lifecycle-alert-danger"
          role="alert"
          data-testid="room-lifecycle-error"
        >
          <Icon name="exclamationCircle" size="sm" class="mt-0.5 flex-shrink-0" />
          <div class="min-w-0">
            <strong>操作没有完成</strong>
            <p>{{ roomLifecycleError }}</p>
            <code v-if="roomLifecycleErrorCode">{{ roomLifecycleErrorCode }}</code>
          </div>
        </div>

        <div
          v-if="roomLifecycleDeleted"
          class="room-lifecycle-alert room-lifecycle-alert-success"
          data-testid="room-lifecycle-deleted"
        >
          <Icon name="checkCircle" size="sm" class="mt-0.5 flex-shrink-0" />
          <div>
            <strong>房间已软删除</strong>
            <p>房间不会再出现在可用列表中，历史消费、结算和评价记录仍会保留。</p>
          </div>
        </div>

        <template v-else-if="roomLifecycleState">
          <section class="room-lifecycle-overview">
            <div class="room-lifecycle-overview-head">
              <div>
                <span class="room-lifecycle-eyebrow">当前状态</span>
                <div class="mt-1 flex flex-wrap items-center gap-2">
                  <strong class="text-base text-gray-950 dark:text-white">
                    {{ roomLifecycleStatusLabel(roomLifecycleState.lifecycle_status) }}
                  </strong>
                  <span :class="roomLifecycleStatusBadgeClass(roomLifecycleState.lifecycle_status)">
                    {{ roomLifecycleHealthLabel(roomLifecycleState.health_state) }}
                  </span>
                </div>
              </div>
              <span class="room-lifecycle-version">版本 {{ roomLifecycleState.row_version }}</span>
            </div>
            <p v-if="roomLifecycleState.status_reason" class="room-lifecycle-status-reason">
              {{ roomLifecycleState.status_reason }}
            </p>
            <div class="room-lifecycle-metrics">
              <div>
                <span>消费者席位</span>
                <strong>{{ roomLifecycleState.active_seats }}/{{ roomLifecycleState.seat_limit }}</strong>
              </div>
              <div>
                <span>排队成员</span>
                <strong>{{ roomLifecycleState.queued_membership_count }}</strong>
              </div>
              <div>
                <span>房间账号</span>
                <strong>{{ roomLifecycleState.room_account_count }}</strong>
              </div>
              <div>
                <span>进行中请求</span>
                <strong>{{ roomLifecycleState.in_flight_concurrency }}</strong>
              </div>
            </div>
          </section>

          <section
            v-if="roomLifecycleOperation"
            class="room-lifecycle-operation"
            data-testid="room-lifecycle-operation"
          >
            <div class="flex min-w-0 items-start gap-3">
              <Icon
                :name="roomLifecycleOperationTerminal ? (roomLifecycleOperation.status === 'succeeded' ? 'checkCircle' : 'exclamationCircle') : 'refresh'"
                size="sm"
                class="mt-0.5 flex-shrink-0"
                :class="{ 'animate-spin': roomLifecyclePolling }"
              />
              <div class="min-w-0">
                <strong>{{ roomLifecycleOperationLabel(roomLifecycleOperation) }}</strong>
                <p>{{ roomLifecycleOperationStatusDescription(roomLifecycleOperation) }}</p>
                <code>{{ roomLifecycleOperation.id }}</code>
              </div>
            </div>
          </section>

          <template v-if="!roomLifecycleHasPendingOperation">
            <section v-if="roomLifecycleAction === null" class="space-y-3">
              <div>
                <span class="room-lifecycle-eyebrow">可用操作</span>
                <p class="mt-1 text-sm leading-6 text-gray-500 dark:text-dark-300">
                  下架后停止新增用户，现有消费者与预约保持不变；需要恢复招募时可重新上架。
                </p>
              </div>
              <div class="room-lifecycle-action-grid">
                <button
                  v-if="roomLifecycleActionAllowed('drain')"
                  type="button"
                  class="room-lifecycle-action-card"
                  data-testid="room-lifecycle-action-drain"
                  @click="selectRoomLifecycleAction('drain')"
                >
                  <Icon name="clock" size="sm" />
                  <span>
                    <strong>下架房间</strong>
                    <small>停止新增，已有用户继续使用</small>
                  </span>
                </button>
                <button
                  v-if="roomLifecycleActionAllowed('activate')"
                  type="button"
                  class="room-lifecycle-action-card"
                  data-testid="room-lifecycle-action-activate"
                  @click="selectRoomLifecycleAction('activate')"
                >
                  <Icon name="play" size="sm" />
                  <span>
                    <strong>重新上架</strong>
                    <small>完成账号连通性校验后重新开放</small>
                  </span>
                </button>
                <button
                  v-if="roomLifecycleActionAllowed('suspend')"
                  type="button"
                  class="room-lifecycle-action-card"
                  data-testid="room-lifecycle-action-suspend"
                  @click="selectRoomLifecycleAction('suspend')"
                >
                  <Icon name="ban" size="sm" />
                  <span>
                    <strong>紧急停用</strong>
                    <small>仅管理员用于异常处置</small>
                  </span>
                </button>
                <button
                  type="button"
                  class="room-lifecycle-action-card room-lifecycle-action-card-danger"
                  data-testid="room-lifecycle-action-delete"
                  @click="selectRoomLifecycleAction('delete')"
                >
                  <Icon name="trash" size="sm" />
                  <span>
                    <strong>删除房间</strong>
                    <small>先检查使用、结算与运行时阻塞项</small>
                  </span>
                </button>
              </div>
              <p
                v-if="!roomLifecycleHasStateChangeAction"
                class="room-lifecycle-muted-note"
              >
                当前没有可执行的状态变更；你仍可检查删除条件，或刷新状态。
              </p>
            </section>

            <section
              v-else-if="roomLifecycleAction !== 'delete'"
              class="room-lifecycle-confirm-panel"
              data-testid="room-lifecycle-confirm"
            >
              <span class="room-lifecycle-eyebrow">确认操作</span>
              <h4>{{ roomLifecycleActionTitle(roomLifecycleAction) }}</h4>
              <p>{{ roomLifecycleActionDescription(roomLifecycleAction) }}</p>
              <div class="room-lifecycle-alert room-lifecycle-alert-warning">
                <Icon name="infoCircle" size="sm" class="mt-0.5 flex-shrink-0" />
                <p>{{ roomLifecycleActionImpact(roomLifecycleAction) }}</p>
              </div>
              <label v-if="authStore.isAdmin" class="field">
                <span>管理员操作原因</span>
                <textarea
                  v-model="roomLifecycleReason"
                  class="input min-h-24"
                  maxlength="500"
                  placeholder="请说明本次生命周期变更原因"
                  data-testid="room-lifecycle-reason"
                ></textarea>
                <small>原因会写入房间事件与审计记录。</small>
              </label>
            </section>

            <section
              v-else
              class="room-lifecycle-confirm-panel"
              data-testid="room-delete-confirm"
            >
              <span class="room-lifecycle-eyebrow">删除校验</span>
              <h4>软删除房间</h4>
              <p>系统会先检查使用中成员、请求、结算、编辑会话和其他房间操作，全部清零后才签发两分钟有效的确认令牌。</p>

              <label v-if="authStore.isAdmin" class="field">
                <span>管理员删除原因</span>
                <textarea
                  v-model="roomLifecycleReason"
                  class="input min-h-24"
                  maxlength="500"
                  placeholder="请说明为什么需要删除此房间"
                  data-testid="room-delete-reason"
                ></textarea>
                <small>必须填写原因后才能检查删除条件，原因会写入审计记录。</small>
              </label>

              <button
                v-if="authStore.isAdmin && !roomDeleteIntent"
                type="button"
                class="btn btn-secondary min-h-11"
                :disabled="roomDeleteIntentLoading || !roomLifecycleReason.trim()"
                data-testid="room-delete-intent-submit"
                @click="loadRoomDeleteIntent"
              >
                <Icon name="search" size="sm" />
                检查删除条件
              </button>

              <div
                v-if="roomDeleteIntentLoading"
                class="room-lifecycle-state-message"
                data-testid="room-delete-intent-loading"
              >
                <Icon name="refresh" size="sm" class="animate-spin" />
                <span>正在检查删除条件...</span>
              </div>

              <template v-else-if="roomDeleteIntent">
                <div
                  :class="[
                    'room-lifecycle-alert',
                    roomDeleteIntent.can_delete
                      ? 'room-lifecycle-alert-warning'
                      : 'room-lifecycle-alert-danger'
                  ]"
                  data-testid="room-delete-intent-result"
                >
                  <Icon
                    :name="roomDeleteIntent.can_delete ? 'exclamationTriangle' : 'exclamationCircle'"
                    size="sm"
                    class="mt-0.5 flex-shrink-0"
                  />
                  <div>
                    <strong>{{ roomDeleteIntent.can_delete ? '删除条件已满足' : '暂时不能删除' }}</strong>
                    <p>{{ roomDeleteIntent.history_notice }}</p>
                  </div>
                </div>

                <ul
                  v-if="roomLifecycleBlockerItems.length > 0"
                  class="room-lifecycle-blocker-list"
                  data-testid="room-delete-blockers"
                >
                  <li v-for="item in roomLifecycleBlockerItems" :key="item.key">
                    <span>{{ item.label }}</span>
                    <strong>{{ item.value }}</strong>
                  </li>
                </ul>

                <label v-if="roomDeleteIntent.can_delete" class="field">
                  <span>输入房间名确认</span>
                  <input
                    v-model="roomDeleteNameConfirmation"
                    class="input min-h-11"
                    type="text"
                    autocomplete="off"
                    :placeholder="roomDeleteIntent.room_name"
                    data-testid="room-delete-name-input"
                  />
                  <small>请完整输入“{{ roomDeleteIntent.room_name }}”。确认令牌将在 {{ formatRoomDeleteIntentExpiry(roomDeleteIntent.expires_at) }} 失效。</small>
                </label>
              </template>
            </section>
          </template>
        </template>
      </div>

      <template #footer>
        <div class="room-lifecycle-footer">
          <button
            v-if="roomLifecycleAction !== null && !roomLifecycleHasPendingOperation && !roomLifecycleDeleted"
            type="button"
            class="btn btn-secondary min-h-11"
            :disabled="roomLifecycleCommandBusy"
            @click="resetRoomLifecycleAction"
          >
            返回
          </button>
          <button
            v-else
            type="button"
            class="btn btn-secondary min-h-11"
            :disabled="roomLifecycleCommandBusy"
            @click="closeRoomLifecycleDialog"
          >
            关闭
          </button>
          <button
            v-if="roomLifecycleHasPendingOperation && !roomLifecycleDeleted"
            type="button"
            class="btn btn-secondary min-h-11"
            :disabled="roomLifecyclePolling"
            data-testid="room-operation-refresh"
            @click="pollRoomLifecycleOperationNow"
          >
            <Icon name="refresh" size="sm" :class="{ 'animate-spin': roomLifecyclePolling }" />
            {{ roomLifecyclePolling ? '自动查询中' : '继续查询' }}
          </button>
          <button
            v-else-if="roomLifecycleAction === null && !roomLifecycleDeleted"
            type="button"
            class="btn btn-secondary min-h-11"
            :disabled="roomLifecycleLoading"
            @click="refreshRoomLifecycleState"
          >
            <Icon name="refresh" size="sm" :class="{ 'animate-spin': roomLifecycleLoading }" />
            刷新状态
          </button>
          <button
            v-else-if="roomLifecycleAction === 'delete' && roomDeleteIntent && (!roomDeleteIntent.can_delete || roomDeleteIntentExpired)"
            type="button"
            class="btn btn-secondary min-h-11"
            :disabled="roomLifecycleCommandBusy"
            @click="loadRoomDeleteIntent"
          >
            {{ roomDeleteIntentExpired ? '重新获取确认' : '重新检查' }}
          </button>
          <button
            v-else-if="roomLifecycleAction !== null && !roomLifecycleDeleted"
            type="button"
            :class="roomLifecycleAction === 'delete' ? 'btn btn-danger min-h-11' : 'btn btn-primary min-h-11'"
            :disabled="!canSubmitRoomLifecycleAction"
            data-testid="room-lifecycle-submit"
            @click="submitRoomLifecycleAction"
          >
            <Icon
              :name="roomLifecycleAction === 'delete' ? 'trash' : 'checkCircle'"
              size="sm"
              :class="{ 'animate-pulse': roomLifecycleSubmitting }"
            />
            {{ roomLifecycleSubmitting ? '提交中...' : roomLifecycleSubmitLabel }}
          </button>
        </div>
      </template>
    </BaseDialog>

    <BaseDialog
      :show="pendingForceEditListing !== null"
      title="管理员强制编辑房间"
      width="narrow"
      @close="cancelForceEdit"
    >
      <div class="space-y-4">
        <div class="notice-row border-amber-200 bg-amber-50 text-amber-800 dark:border-amber-900/60 dark:bg-amber-900/20 dark:text-amber-200">
          <Icon name="exclamationTriangle" size="sm" class="mt-0.5 flex-shrink-0" />
          <span>{{ forceEditConfirmMessage }}</span>
        </div>
        <label class="field">
          <span>强制修改原因</span>
          <textarea
            v-model="forceEditReason"
            class="input min-h-24"
            maxlength="500"
            placeholder="请说明为什么不能先暂停并排空房间"
            data-testid="force-edit-reason"
          ></textarea>
          <small>原因会随新 revision 写入审计记录，不能为空。</small>
        </label>
        <label class="toggle-row">
          <input v-model="forceEditConfirmed" type="checkbox" data-testid="force-edit-confirmed" />
          <span>
            <strong>我确认需要绕过房主的暂停编辑限制</strong>
            <small>已有 active、queued 或 ending membership 继续使用旧 revision；只有修改后新加入的 membership 使用新 revision。</small>
          </span>
        </label>
      </div>

      <template #footer>
        <button type="button" class="btn-secondary min-h-11" @click="cancelForceEdit">取消</button>
        <button
          type="button"
          class="btn-danger min-h-11"
          :disabled="!forceEditReason.trim() || !forceEditConfirmed"
          data-testid="confirm-force-edit"
          @click="confirmForceEdit"
        >
          继续强制编辑
        </button>
      </template>
    </BaseDialog>

    <ConfirmDialog
      :show="pendingDraftDiscardTarget !== null"
      title="放弃未保存的修改？"
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
import { computed, nextTick, onBeforeUnmount, onMounted, reactive, ref, watch } from 'vue'
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
  type AccountShareRecommendationCandidate,
  type AccountShareRecommendationRequest,
  type AccountShareRecommendationResult,
  type AccountShareRecommendationScoreBreakdown,
  type AccountShareRecommendationUsageProfile,
  type AccountShareReview,
  type AccountShareRoomBlockers,
  type AccountShareRoomDeleteIntent,
  type AccountShareRoomHealthState,
  type AccountShareRoomLifecycleAction,
  type AccountShareRoomLifecycleStatus,
  type AccountShareRoomManagementState,
  type AccountShareRoomOperation,
  type AccountShareRoomQuotaWindow,
  type CreateAccountShareRoomRequest,
  type UpdateAccountShareListingRequest
} from '@/api/accountShare'
import { accountsAPI, keysAPI } from '@/api'
import type { Account, AccountLevel, ApiKey, Proxy, ProxyProtocol } from '@/types'
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
import Select, { type SelectOption } from '@/components/common/Select.vue'
import Icon from '@/components/icons/Icon.vue'
import ModelWhitelistSelector from '@/components/account/ModelWhitelistSelector.vue'
import OAuthAuthorizationFlow from '@/components/account/OAuthAuthorizationFlow.vue'
import ProxySelector from '@/components/common/ProxySelector.vue'
import Pagination from '@/components/common/Pagination.vue'
import CreateRoomDialog from '@/components/account-share/CreateRoomDialog.vue'
import RoomAccountsDialog from '@/components/account-share/RoomAccountsDialog.vue'
import AccountShareQuotaAdminDialog from '@/components/account-share/AccountShareQuotaAdminDialog.vue'
import MembershipHistoryPanel from '@/components/account-share/MembershipHistoryPanel.vue'
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
}

type DraftDiscardTarget = 'create' | 'config'

interface CreateDraftSnapshot {
  sourceMode: 'existing' | 'oauth'
  platform: AccountSharePlatform
  selectedOwnedAccountID: number
  form: CreateFormState
  allowedModels: string[]
  authURL: string
  authSessionID: string
  authCode: string
  oauthState: string
}

interface ConfigDraftSnapshot {
  form: CreateFormState
  allowedModels: string[]
  reason: string
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

interface RecommendationScoreItem {
  key: 'cost' | 'stable' | 'available' | 'risk'
  label: string
  value: number
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
  apiKeyID?: number
  apiKeyName?: string
  membership: AccountShareMembership
  listingSnapshot: AccountShareListing
}

interface QueueSnapshotLoadResult {
  snapshots: Record<number, AccountShareMembership[]>
  failedApiKeyIDs: number[]
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
  queueRank?: number
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
}

interface MySpendAccountOptionPagination {
  page: number
  pageSize: number
  total: number
  pages: number
}

type OwnerDialogTab = 'listings' | 'reviews'

interface RoomLifecycleBlockerItem {
  key: keyof AccountShareRoomBlockers
  label: string
  value: string
}

const DEFAULT_ACCOUNT_CONCURRENCY = 20
const DEFAULT_PER_USER_CONCURRENCY = 5
const DEFAULT_HOURLY_RATE = 0.2
const DEFAULT_ACCOUNT_SHARE_IDLE_TIMEOUT_MINUTES = 10
const MY_SPEND_ACCOUNT_PAGE_SIZE = 12
const PLUS_EXPENSIVE_RATE_MULTIPLIER = 0.15
const PRO_EXPENSIVE_RATE_MULTIPLIER = 0.25
const EXPENSIVE_HOURLY_RATE = 2
const MAX_ACCOUNT_CONCURRENCY = 50
const MAX_PER_USER_CONCURRENCY = 50
const ACCOUNT_SHARE_MIN_SEATS = 1
const ACCOUNT_SHARE_MAX_SEATS = 30
const ACCOUNT_SHARE_MEMBER_LIMIT_HELP = '由房主设置，与账号数量/账号并发无推导关系；房主自用不占消费者名额'
const ROOM_LIFECYCLE_OPERATION_POLL_INTERVAL_MS = 1500
const ACCOUNT_SHARE_TRANSIENT_STATUS_REFRESH_INTERVAL_MS = 8_000
const ROOM_LIFECYCLE_TERMINAL_OPERATION_STATUSES = new Set([
  'succeeded',
  'failed',
  'cancelled'
])
const ROOM_LIFECYCLE_ERROR_MESSAGES: Record<string, string> = {
  ACCOUNT_SHARE_ROOM_VERSION_CONFLICT: '房间状态刚刚发生变化，请刷新后重新确认本次操作。',
  ACCOUNT_SHARE_ROOM_OPERATION_CONFLICT: '房间已有一个生命周期操作正在执行，请等待它结束后再试。',
  ACCOUNT_SHARE_ROOM_INVALID_TRANSITION: '当前房间状态不允许执行该操作，请刷新后查看最新状态。',
  ACCOUNT_SHARE_ROOM_DELETE_BLOCKED: '房间仍有使用、请求或结算阻塞项，暂时不能删除。',
  ACCOUNT_SHARE_ROOM_REVIEW_IDENTITY_MISSING: '房间存在可评价的历史记录，但账号邮箱身份尚未固化。请先刷新或重新授权房间账号后再删除。',
  ACCOUNT_SHARE_ROOM_DELETION_TOKEN_INVALID: '删除确认已失效或房间状态已变化，请重新检查删除条件。',
  ACCOUNT_SHARE_ROOM_DELETED: '房间已经删除，无需重复操作。',
  ACCOUNT_SHARE_LISTING_EDITING: '房间配置仍在编辑中，请先关闭编辑窗口或等待编辑会话失效。',
  ACCOUNT_SHARE_ROOM_VALIDATION_FAILED: '房间恢复校验未通过，请检查房间账号状态后重试。',
  ACCOUNT_SHARE_RUNTIME_DEPENDENCY_UNAVAILABLE: '运行时状态暂时不可用，为保护历史与结算安全，当前操作已停止。'
}
const PROXY_PURCHASE_URL = 'https://www.seekproxy.com/user/reg?invite_id=105978'
const ACCOUNT_SHARE_PLATFORM_OPTIONS: Array<{ value: AccountSharePlatform; label: string }> = [
  { value: 'openai', label: 'OpenAI' },
  { value: 'anthropic', label: 'Anthropic' }
]
const ACCOUNT_NAME_BASE_BY_PLATFORM: Record<AccountSharePlatform, string> = {
  openai: 'OpenAI房间',
  anthropic: 'Anthropic房间'
}
const ACCOUNT_MODE_GROUP_NAME_BY_PLATFORM: Record<AccountSharePlatform, string> = {
  openai: 'OpenAI账号模式',
  anthropic: 'Anthropic账号模式'
}
const DEFAULT_ACCOUNT_SHARE_ALLOWED_MODELS_BY_PLATFORM: Record<AccountSharePlatform, string[]> = {
  openai: ['gpt-5.5', 'gpt-5.4', 'gpt-5.4-mini', 'codex-auto-review'],
  anthropic: ['claude-sonnet-4-6', 'claude-opus-5', 'claude-opus-4-8', 'claude-opus-4-7', 'claude-fable-5', 'claude-opus-4-6', 'claude-haiku-4-5']
}
const ACCOUNT_SHARE_RECOMMENDATION_LIMIT = 10
const ACCOUNT_SHARE_RECOMMENDATION_PAGE_SIZE = 5
const OWNER_LISTINGS_PAGE_SIZE = 24
const OWNER_REVIEWS_PAGE_SIZE = 20
const recommendationPresets: RecommendationPreset[] = [
  {
    key: 'light',
    label: '轻量',
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
    label: '均衡',
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
    label: '重度',
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
  return value === null ? '配置不可用' : `${formatNumber(value)}x`
})
const seatOptions = Array.from({ length: ACCOUNT_SHARE_MAX_SEATS - ACCOUNT_SHARE_MIN_SEATS + 1 }, (_, index) => index + ACCOUNT_SHARE_MIN_SEATS)
const reviewScoreOptions = Array.from({ length: 11 }, (_, score) => score)
const filters: FilterOption[] = [
  { key: 'using', label: '使用/预约', tab: 'using' },
  { key: 'history', label: '消费记录', tab: 'history' },
  { key: 'archive', label: '已删除房间', tab: 'archive' },
  { key: 'all', label: '全部', tab: 'all' }
]
const ownerFilter: FilterOption = { key: 'mine', label: '我的账号', tab: 'mine' }
const listingSortFieldOptions: ListingSortFieldOption[] = [
  { sortBy: 'account_concurrency', label: '配置并发', ascLabel: '从小到大', descLabel: '从大到小' },
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
  { value: 'suspended', label: '管理员暂停' },
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
  ACCOUNT_SHARE_ACCOUNT_UNAVAILABLE: '该账号房间当前不可加入，请换一个房间或稍后再试',
  ACCOUNT_SHARE_ALREADY_USING: '你当前已有正在使用的账号房间，请先结束后再加入新的房间',
  ACCOUNT_SHARE_API_KEY_ALREADY_BOUND: '当前账号模式 Key 已绑定其他账号房间，请先结束原使用记录',
  ACCOUNT_SHARE_QUEUE_FULL: '当前账号模式 Key 的预约列表已满，最多只能保留 5 个账号',
  ACCOUNT_SHARE_QUEUE_INVALID: '预约列表顺序无效，请刷新后重试',
  ACCOUNT_SHARE_API_KEY_MUST_USE_MODE_GROUP: '请选择绑定对应平台账号模式分组的 API Key',
  ACCOUNT_SHARE_LISTING_NOT_FOUND: '该账号房间不存在或已下架，请刷新账号广场后再试',
  ACCOUNT_SHARE_LISTING_NOT_ACTIVE: '该账号房间当前未上架，暂时不能加入',
  ACCOUNT_SHARE_LISTING_FULL: '该账号房间成员已满，请换一个房间',
  ACCOUNT_SHARE_BALANCE_BELOW_MINIMUM: '余额低于该账号最低要求，暂时不能加入',
  ACCOUNT_SHARE_MODE_GROUP_UNAVAILABLE: '账号模式分组尚未配置，请联系管理员处理',
  ACCOUNT_SHARE_MODE_GROUP_UNBOUND: '当前账号模式分组未绑定账号房间，请先在账号广场加入一个房间',
  ACCOUNT_SHARE_MODE_INVALID_IDLE_TIMEOUT: '空闲自动退出时间必须在 1-10080 分钟之间',
  ACCOUNT_SHARE_MODE_PREPAY_INSUFFICIENT: '余额不足以预付本次使用，请充值后再试',
  ACCOUNT_SHARE_PER_USER_CONCURRENCY_EXCEEDED: '该账号房间当前单用户并发已达到上限，请稍后再试',
  ACCOUNT_SHARE_OWNER_CANNOT_JOIN: '不能以消费者身份加入自己管理的账号房间',
  ACCOUNT_SHARE_LISTING_EDITING: '账号配置正在编辑中，暂时不能加入使用',
  ACCOUNT_SHARE_JOIN_INTENT_REQUIRED: '请先获取并确认最新加入条款',
  ACCOUNT_SHARE_JOIN_INTENT_INVALID: '加入确认已失效，请重新确认最新条款',
  ACCOUNT_SHARE_JOIN_INTENT_CONSUMED: '这份加入确认已经使用过，请重新确认',
  ACCOUNT_SHARE_JOIN_TERMS_CHANGED: '房间条款已变化，请重新确认最新条款',
  ACCOUNT_SHARE_QUEUE_CONFIRMATION_REQUIRED: '当前需要进入预约队列，请明确同意排队后重试',
  ACCOUNT_SHARE_MEMBERSHIP_ENDING: '退出结算处理中，结算完成后才能重新加入或排队',
  API_KEY_NOT_FOUND: '该 API Key 不存在或已被删除，请重新选择',
  INSUFFICIENT_PERMISSIONS: '你没有权限使用这个 API Key，请重新选择自己的账号模式 Key',
  SERVICE_UNAVAILABLE: '账号广场服务暂时不可用，请稍后再试',
  USER_NOT_FOUND: '当前用户状态异常，请重新登录后再试'
}
const accountShareRoomCreateErrorMessages: Record<string, string> = {
  ACCOUNT_SHARE_ROOM_LIMIT_EXCEEDED: '未删除房间数量已达到当前配额上限，请删除不再使用的空房间或联系管理员调整配额',
  ACCOUNT_SHARE_ROOM_CREATE_RATE_EXCEEDED: '最近 24 小时创建房间次数已达到上限，请在配额窗口恢复后再试',
  ACCOUNT_SHARE_ROOM_ACCOUNT_LIMIT_EXCEEDED: '该房间的账号数量已达到上限，请先移出不再使用的账号',
  ACCOUNT_SHARE_OWNER_ROOM_ACCOUNT_LIMIT_EXCEEDED: '你管理的房间账号总数已达到上限，请先整理现有房间账号',
  ACCOUNT_SHARE_ROOM_OWNER_MISMATCH: '所选账号不属于当前房主，请刷新账号列表后重新选择',
  ACCOUNT_SHARE_ROOM_PLATFORM_MISMATCH: '所选账号与房间平台不一致，请选择同平台账号',
  ACCOUNT_SHARE_ROOM_LEVEL_MISMATCH: '所选账号等级与房间要求不一致，请选择相同等级账号',
  ACCOUNT_SHARE_ROOM_UNKNOWN_LEVEL: '所选账号等级尚未识别，请先完成账号检测后再创建房间',
  ACCOUNT_SHARE_ROOM_MODE_REQUIRED: '所选账号尚未处于可创建房间的账号模式，请先完成账号配置',
  ACCOUNT_SHARE_ROOM_ACCOUNT_CONFLICT: '所选账号已加入其他房间或正在切换归属，请刷新后重新选择',
  ACCOUNT_SHARE_QUOTA_HISTORICAL_GROWTH_BLOCKED: '当前用量超过新配额，历史保留状态下只能收缩，不能继续创建或增加房间账号',
  ACCOUNT_SHARE_QUOTA_GRANDFATHER_GROWTH_BLOCKED: '当前处于历史保留配额，只能减少现有用量；请先整理房间或联系管理员调整配额'
}
const accountShareCapabilityBlockerMessages: Record<string, string> = {
  ACCOUNT_SHARE_ROOM_LIMIT_EXCEEDED: '未删除房间数量已达到配额上限',
  ACCOUNT_SHARE_ROOM_CREATE_RATE_EXCEEDED: '最近 24 小时创建房间次数已达到配额上限',
  ACCOUNT_SHARE_ROOM_ACCOUNT_LIMIT_EXCEEDED: '单个房间账号数量已达到配额上限',
  ACCOUNT_SHARE_OWNER_ROOM_ACCOUNT_LIMIT_EXCEEDED: '房主管理的房间账号总数已达到配额上限',
  ACCOUNT_SHARE_QUOTA_HISTORICAL_GROWTH_BLOCKED: '当前用量超过新配额，历史保留状态下只能收缩',
  ACCOUNT_SHARE_QUOTA_GRANDFATHER_GROWTH_BLOCKED: '当前处于历史保留配额，只能减少现有用量'
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
  return [ownerFilter, ...filters].find(option => option.tab === tab)
    || filters.find(option => option.tab === 'all')
    || filters[0]
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
const queueMembershipsByApiKey = ref<Record<number, AccountShareMembership[]>>({})
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
  pages: 1
})
const membershipHistoryPagination = reactive({
  page: 1,
  page_size: initialListingPreferences.pageSize,
  total: 0,
  pages: 1
})
const loading = ref(false)
const errorMessage = ref('')
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
  title: '操作失败',
  message: '',
  action: null
})
const createErrorMessage = ref('')
const showCreate = ref(false)
const createPlatform = ref<AccountSharePlatform>('openai')
const createSourceMode = ref<'existing' | 'oauth'>('existing')
const ownedAccounts = ref<Account[]>([])
const ownedAccountsLoading = ref(false)
const ownedAccountsError = ref('')
const selectedOwnedAccountID = ref(0)
let ownedAccountsRequestVersion = 0
let ownedAccountsRequestController: AbortController | null = null
let ownedAccountsLoadedPlatform: AccountSharePlatform | null = null
let pendingCreateRoomIntentSignature = ''
let pendingCreateRoomIdempotencyKey = ''
const oauthExchangeIntent: StableIdempotencyIntent = { signature: '', key: '' }
const reviewSubmitIntent: StableIdempotencyIntent = { signature: '', key: '' }
const beginEditIntent: StableIdempotencyIntent = { signature: '', key: '' }
const releaseEditIntent: StableIdempotencyIntent = { signature: '', key: '' }
const updateListingIntent: StableIdempotencyIntent = { signature: '', key: '' }
const authURL = ref('')
const authSessionID = ref('')
const creating = ref(false)
const generatingOAuthURL = ref(false)
const preparingJoinId = ref<number | null>(null)
const joiningId = ref<number | null>(null)
const refreshingJoinIntent = ref(false)
const joinIntentError = ref('')
const pendingJoinConfirmation = ref<PendingJoinConfirmation | null>(null)
const endingId = ref<number | null>(null)
const pendingEndUse = ref<PendingEndUseState | null>(null)
const pendingMembershipEnds = ref<Record<number, PendingMembershipEnd>>({})
const pendingReview = ref<ReviewDialogState | null>(null)
const roomAccountsListing = ref<AccountShareListing | null>(null)
const roomLifecycleListing = ref<AccountShareListing | null>(null)
const roomLifecycleState = ref<AccountShareRoomManagementState | null>(null)
const roomLifecycleAction = ref<AccountShareRoomLifecycleAction | null>(null)
const roomLifecycleOperation = ref<AccountShareRoomOperation | null>(null)
const roomDeleteIntent = ref<AccountShareRoomDeleteIntent | null>(null)
const roomDeleteNameConfirmation = ref('')
const roomLifecycleReason = ref('')
const roomLifecycleLoading = ref(false)
const roomDeleteIntentLoading = ref(false)
const roomLifecycleSubmitting = ref(false)
const roomLifecyclePolling = ref(false)
const roomLifecycleDeleted = ref(false)
const roomLifecycleError = ref('')
const roomLifecycleErrorCode = ref('')
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
  listingsPages: 1,
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
})
const mySpendHistoryPagination = reactive<MySpendAccountOptionPagination>({
  page: 1,
  pageSize: MY_SPEND_ACCOUNT_PAGE_SIZE,
  total: 0,
  pages: 1,
})
const mySpendAccountsLoading = ref(false)
const mySpendAccountsError = ref('')
const mySpendRange = ref<AccountShareMySpendRange>('current_membership')
const mySpendSummary = ref<AccountShareMySpendSummary | null>(null)
const mySpendLoading = ref(false)
const mySpendError = ref('')
const reorderingQueueId = ref<number | null>(null)
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
const editSessionID = ref('')
const editForceActive = ref(false)
const editConsumerProtected = ref(false)
const editReason = ref('')
const editErrorMessage = ref('')
const editVersionConflict = ref(false)
const savingConfigEdit = ref(false)
const releasingConfigEdit = ref(false)
let editSessionGeneration = 0
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
const oauthFlowRef = ref<OAuthFlowInstance | null>(null)
const showProxyDialog = ref(false)
const savingProxy = ref(false)
const proxyDialogError = ref('')
const proxySmartInput = ref('')
const nowMs = ref(Date.now())
let clockTimer: number | null = null
let searchDebounceTimer: number | null = null
let editSessionRenewTimer: number | null = null
let suppressNextSearchRefresh = false
let listingsRequestController: AbortController | null = null
let listingsRequestSeq = 0
let membershipHistoryRequestController: AbortController | null = null
let membershipHistoryRequestSeq = 0
let roomLifecycleStateController: AbortController | null = null
let roomLifecycleOperationController: AbortController | null = null
let roomLifecycleStateRequestSeq = 0
let roomLifecycleOperationPollSeq = 0
let roomLifecycleOperationPollTimer: number | null = null
let roomLifecycleIdempotencySignature = ''
let roomLifecycleIdempotencyKey = ''
let mySpendAccountsRequestController: AbortController | null = null
let mySpendAccountsRequestSeq = 0
let mySpendRequestController: AbortController | null = null
let mySpendRequestSeq = 0
let recommendationRequestController: AbortController | null = null
let recommendationRequestSeq = 0
let recommendationUsageProfileController: AbortController | null = null
let recommendationUsageProfileRequestSeq = 0
let editSessionRenewController: AbortController | null = null
let ownerListingsRequestController: AbortController | null = null
let ownerReviewsRequestController: AbortController | null = null
let ownerDialogRequestSeq = 0
let modeKeysRequestSeq = 0
let keyResolutionRequestSeq = 0
let membershipEndOperationRequestSeq = 0
const membershipEndOperationControllers = new Map<number, AbortController>()
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
    anthropic_7d_limit_percent: 100
  }
}

const createForm = reactive<CreateFormState>(buildDefaultCreateForm())
const editForm = reactive<CreateFormState>(buildDefaultCreateForm())
const allowedModels = ref<string[]>(defaultAllowedModelsForPlatform(createPlatform.value))

function createDraftSnapshot(): CreateDraftSnapshot {
  return {
    sourceMode: createSourceMode.value,
    platform: createPlatform.value,
    selectedOwnedAccountID: selectedOwnedAccountID.value,
    form: { ...createForm },
    allowedModels: [...allowedModels.value],
    authURL: authURL.value,
    authSessionID: authSessionID.value,
    authCode: (oauthFlowRef.value?.authCode || '').trim(),
    oauthState: (oauthFlowRef.value?.oauthState || '').trim()
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
    && !snapshotsMatch(configDraftBaseline.value, configDraftSnapshot())
  )
}

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
  const prefix = listing.deleted || listing.status !== 'active' ? '健康账号' : '可调度账号'
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
      detail: `房间生命周期为“${lifecycle}”，当前不可新加入；挂载账号健康度仅反映账号状态，不代表房间已开放。`,
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
      detail: `房间生命周期为“${lifecycle}”，但当前没有挂载账号。`,
      badge: '无账号',
      tone: 'danger'
    }
  }
  if (eligible === 0) {
    return {
      detail: `房间生命周期为“${lifecycle}”，但当前没有可路由账号。`,
      badge: '不可用',
      tone: 'danger'
    }
  }
  if (eligible < attached) {
    return {
      detail: `房间生命周期为“${lifecycle}”；部分挂载账号当前不具备路由资格。`,
      badge: '部分可用',
      tone: 'warning'
    }
  }
  return {
    detail: `房间生命周期为“${lifecycle}”；全部挂载账号当前具备路由资格。`,
    badge: '可用',
    tone: 'normal'
  }
}

function roomAvailableConcurrencyLabel(listing: AccountShareListing): string {
  if (listing.deleted || listing.status !== 'active') return '不可新加入'
  if (listing.runtime_load_known !== true) return '运行时未知'
  const total = Number(listing.account_concurrency)
  const used = Number(listing.current_concurrency)
  if (!Number.isFinite(total) || !Number.isFinite(used)) return '运行时未知'
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
  return utilization === null ? '暂无数据' : `${formatNumber(utilization)}%`
}

function roomWindowUtilizationBarClass(window?: AccountShareRoomQuotaWindow): string {
  const utilization = roomWindowUtilization(window)
  if (utilization === null || utilization < 80) return 'combined-availability-fill combined-availability-fill-normal'
  if (utilization < 100) return 'combined-availability-fill combined-availability-fill-warning'
  return 'combined-availability-fill combined-availability-fill-danger'
}

function platformLabel(platform: string): string {
  return ACCOUNT_SHARE_PLATFORM_OPTIONS.find(item => item.value === platform)?.label || platform
}

function accountModeGroupName(platform: AccountSharePlatform): string {
  return ACCOUNT_MODE_GROUP_NAME_BY_PLATFORM[platform]
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
  ACCOUNT_SHARE_PLATFORM_OPTIONS.some(option => modeKeysLoadingForPlatform(option.value))
)

function modeApiKeyPlaceholderForListing(listing: AccountShareListing): string {
  const platform = listingPlatform(listing)
  if (modeKeysLoadingForPlatform(platform)) return '正在加载账号模式 API Key'
  if (!modeKeysLoadedForPlatform(platform)) return '账号模式 API Key 未加载'
  return `选择${accountModeGroupName(listingPlatform(listing))} Key`
}
const pendingJoinIntent = computed(() => pendingJoinConfirmation.value?.intent ?? null)
const pendingJoinTerms = computed(() => pendingJoinIntent.value?.terms ?? null)
const pendingJoinIsOwnerSelfUse = computed(() => pendingJoinConfirmation.value?.ownerSelfUse === true)
const pendingJoinPlatform = computed(() => pendingJoinConfirmation.value?.platform ?? 'openai')
const pendingJoinApiKeyLabel = computed(() => pendingJoinConfirmation.value?.apiKeyLabel || '-')
const pendingJoinIdleTimeoutLabel = computed(() => formatIdleTimeoutSetting(pendingJoinConfirmation.value?.idleTimeoutMinutes ?? 0))
const joinDialogBusy = computed(() => refreshingJoinIntent.value || joiningId.value !== null)
const pendingJoinExpired = computed(() => {
  const expiresAt = pendingJoinIntent.value?.expires_at
  if (!expiresAt) return true
  const expiresAtMs = Date.parse(expiresAt)
  return !Number.isFinite(expiresAtMs) || expiresAtMs <= nowMs.value
})
const pendingJoinCanSubmit = computed(() => {
  const intent = pendingJoinIntent.value
  if (!intent || joinDialogBusy.value || pendingJoinExpired.value) return false
  return !intent.queue_may_be_required || intent.accept_queue
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
    warnings.push(`本次确认条款中的倍率为 ${formatNumber(terms.rate_multiplier)}x，后续请求消耗会按此倍率计算。`)
  }
  if (Number(terms.hourly_rate || 0) > EXPENSIVE_HOURLY_RATE) {
    warnings.push(`本次确认条款中的小时费为 ${formatNumber(terms.hourly_rate)}，空闲或长时间使用时请留意费用。`)
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

const currentProxyID = computed(() => {
  const proxyID = Number(createForm.proxy_id || 0)
  return Number.isFinite(proxyID) && proxyID > 0 ? proxyID : 0
})

const eligibleOwnedAccounts = computed(() => (
  ownedAccounts.value
    .filter((account) => {
      if (account.platform.trim().toLowerCase() !== createPlatform.value) return false
      if (account.status !== 'active' || !account.schedulable) return false
      if (!Number.isFinite(Number(account.concurrency)) || Number(account.concurrency) <= 0) return false
      if (!account.account_level || account.account_level.trim().toLowerCase() === 'unknown') return false
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
  if (ownedAccountsLoading.value) return '正在读取我的账号...'
  if (ownedAccountsError.value) return ownedAccountsError.value
  if (selectedOwnedAccount.value) {
    return selectedOwnedAccount.value.external_placement?.target === 'public_pool'
      || (!selectedOwnedAccount.value.external_placement && selectedOwnedAccount.value.share_mode === 'public')
      ? `账号 #${selectedOwnedAccount.value.id} 当前在公共号池；创建时会原子切换到新房间，并保留凭证、代理和私有自用能力。`
      : `将保留账号 #${selectedOwnedAccount.value.id} 的凭证、代理和私有自用能力。`
  }
  if (eligibleOwnedAccounts.value.length === 0) {
    return '没有可创建房间的账号；账号需健康、等级已确认且尚未加入其他房间。'
  }
  return `可选 ${eligibleOwnedAccounts.value.length} 个账号。`
})

const selectedCreateProxy = computed(() => findProxyByID(currentProxyID.value))

const createProxyCapacityValidationMessage = computed(() =>
  proxyCapacityValidationMessage(selectedCreateProxy.value)
)

const parsedAllowedModelCount = computed(() => allowedModels.value.length)
const availableSeatCount = computed(() => listings.value.reduce((total, listing) => {
  if (listing.deleted || listing.status !== 'active') return total
  return total + Math.max(0, listing.seat_limit - listing.active_seats)
}, 0))
const activeSeatCount = computed(() => listings.value.reduce((total, listing) => total + Math.max(0, Number(listing.active_seats || 0)), 0))
const createRoomCapabilityHint = computed(() => {
  if (capabilitiesLoading.value) return '正在读取房间配额'
  const blocker = capabilities.value?.capability_blockers[0]
  if (blocker) return capabilityBlockerMessage(blocker)
  if (capabilitiesError.value) return capabilitiesError.value
  return '创建一个由你管理的账号共享房间'
})
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
  if (!Number.isFinite(concurrency) || concurrency < 1) return '配置并发必须大于 0'
  if (!Number.isInteger(concurrency)) return '配置并发必须是整数'
  if (concurrency > MAX_ACCOUNT_CONCURRENCY) return `配置并发不能超过 ${MAX_ACCOUNT_CONCURRENCY}`
  return ''
})
const editConcurrencyValidationMessage = computed(() => {
  const concurrency = Number(editForm.concurrency)
  if (!Number.isFinite(concurrency) || concurrency < 1) return '配置并发必须大于 0'
  if (!Number.isInteger(concurrency)) return '配置并发必须是整数'
  if (concurrency > MAX_ACCOUNT_CONCURRENCY) return `配置并发不能超过 ${MAX_ACCOUNT_CONCURRENCY}`
  return ''
})
const perUserConcurrencyValidationMessage = computed(() =>
  validatePerUserConcurrencyValue(createForm.per_user_concurrency)
)
const editPerUserConcurrencyValidationMessage = computed(() =>
  validatePerUserConcurrencyValue(editForm.per_user_concurrency)
)
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
const canSubmitOAuth = computed(() =>
  authSessionID.value &&
  currentProxyID.value > 0 &&
  parsedAllowedModelCount.value > 0 &&
  !accountNameValidationMessage.value &&
  !concurrencyValidationMessage.value &&
  !perUserConcurrencyValidationMessage.value
)
const canCreateRoomFromOwnedAccount = computed(() =>
  selectedOwnedAccount.value !== null
  && parsedAllowedModelCount.value > 0
  && !accountNameValidationMessage.value
  && !concurrencyValidationMessage.value
  && !perUserConcurrencyValidationMessage.value
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
const draftDiscardMessage = computed(() => (
  pendingDraftDiscardTarget.value === 'config'
    ? '当前房间配置尚未保存。确认放弃后会释放编辑锁，本次修改不会提交。'
    : '当前创建信息或 OAuth 进度尚未提交。确认放弃后会恢复为打开窗口时的状态。'
))

function roomEditBlockerLabels(state: AccountShareRoomManagementState): string[] {
  const blockers = state.blockers
  const labels: string[] = []
  if (blockers.active_membership_count > 0) labels.push(`使用中 ${blockers.active_membership_count}`)
  if (blockers.queued_membership_count > 0) labels.push(`排队 ${blockers.queued_membership_count}`)
  if (blockers.ending_membership_count > 0) labels.push(`结束中 ${blockers.ending_membership_count}`)
  if (blockers.in_flight_request_count > 0) labels.push(`进行中请求 ${blockers.in_flight_request_count}`)
  if (blockers.pending_billing_intent_count > 0) labels.push(`待处理计费 ${blockers.pending_billing_intent_count}`)
  if (blockers.synchronous_billing_pending_count > 0) labels.push(`同步结算中 ${blockers.synchronous_billing_pending_count}`)
  if (blockers.valid_edit_session) labels.push('存在有效编辑锁')
  if (blockers.conflicting_operation || state.pending_operation_id) labels.push('存在生命周期操作')
  return labels
}

function roomRequiresForceEdit(
  listing: AccountShareListing,
  state: AccountShareRoomManagementState
): boolean {
  const blockers = state.blockers
  return !['active', 'paused'].includes(state.lifecycle_status)
    || blockers.active_membership_count > 0
    || blockers.queued_membership_count > 0
    || blockers.ending_membership_count > 0
    || blockers.in_flight_request_count > 0
    || blockers.pending_billing_intent_count > 0
    || blockers.synchronous_billing_pending_count > 0
    || blockers.conflicting_operation
    || Boolean(state.pending_operation_id)
    || (blockers.valid_edit_session && !listing.editing_mine)
}

const forceEditConfirmMessage = computed(() => {
  const listing = pendingForceEditListing.value
  if (!listing) return ''
  const state = pendingForceEditManagementState.value
  const status = state?.lifecycle_status || listing.status
  const activeSeats = state?.active_seats ?? listing.active_seats
  const seatLimit = state?.seat_limit ?? listing.seat_limit
  const blockers = state ? roomEditBlockerLabels(state) : []
  const blockerText = blockers.length > 0 ? ` 当前阻塞项：${blockers.join('、')}。` : ''
  return `房间当前状态为“${statusLabel(status)}”，消费者席位 ${activeSeats}/${seatLimit}。${blockerText}强制编辑会生成新的条款 revision；已有 membership 继续按其历史快照结算。`
})
const roomLifecycleCommandBusy = computed(() =>
  roomLifecycleSubmitting.value || roomDeleteIntentLoading.value
)
const roomLifecycleOperationTerminal = computed(() => {
  const status = roomLifecycleOperation.value?.status
  return Boolean(status && ROOM_LIFECYCLE_TERMINAL_OPERATION_STATUSES.has(status))
})
const roomLifecycleHasPendingOperation = computed(() => {
  if (roomLifecycleOperation.value) return !roomLifecycleOperationTerminal.value
  return Boolean(roomLifecycleState.value?.pending_operation_id)
})
const roomLifecycleHasStateChangeAction = computed(() => {
  const allowedActions = roomLifecycleState.value?.allowed_actions ?? []
  return allowedActions.some(action => action === 'drain' || action === 'activate' || action === 'suspend')
})
const roomDeleteIntentExpired = computed(() => {
  const expiresAt = normalizeDateInput(roomDeleteIntent.value?.expires_at)
  return Boolean(expiresAt && expiresAt.getTime() <= nowMs.value)
})
const roomLifecycleBlockerItems = computed<RoomLifecycleBlockerItem[]>(() => {
  const blockers = roomDeleteIntent.value?.blockers
  if (!blockers) return []

  const items: RoomLifecycleBlockerItem[] = []
  const appendCount = (
    key: keyof AccountShareRoomBlockers,
    label: string,
    value: number
  ) => {
    if (value > 0) items.push({ key, label, value: String(value) })
  }
  appendCount('active_membership_count', '正在使用的成员', blockers.active_membership_count)
  appendCount('queued_membership_count', '排队中的成员', blockers.queued_membership_count)
  appendCount('ending_membership_count', '正在退出或结算的成员', blockers.ending_membership_count)
  appendCount('in_flight_request_count', '进行中的请求', blockers.in_flight_request_count)
  appendCount('pending_billing_intent_count', '待处理计费意图', blockers.pending_billing_intent_count)
  appendCount(
    'synchronous_billing_pending_count',
    '同步结算任务',
    blockers.synchronous_billing_pending_count
  )
  if (blockers.valid_edit_session) {
    items.push({ key: 'valid_edit_session', label: '房间编辑会话', value: '仍在占用' })
  }
  if (blockers.conflicting_operation) {
    items.push({
      key: 'conflicting_operation',
      label: '其他生命周期操作',
      value: blockers.conflicting_operation_id || '正在执行'
    })
  }
  if (blockers.runtime_dependency_unavailable) {
    items.push({
      key: 'runtime_dependency_unavailable',
      label: '运行时状态',
      value: '暂时无法确认'
    })
  }
  return items
})
const canSubmitRoomLifecycleAction = computed(() => {
  const action = roomLifecycleAction.value
  const state = roomLifecycleState.value
  if (
    !action ||
    !state ||
    roomLifecycleCommandBusy.value ||
    roomLifecycleHasPendingOperation.value
  ) {
    return false
  }
  if (authStore.isAdmin && !roomLifecycleReason.value.trim()) return false
  if (action !== 'delete') return state.allowed_actions.includes(action)
  const intent = roomDeleteIntent.value
  return Boolean(
    intent?.can_delete &&
    intent.token &&
    !roomDeleteIntentExpired.value &&
    roomDeleteNameConfirmation.value === intent.room_name
  )
})
const roomLifecycleSubmitLabel = computed(() => {
  switch (roomLifecycleAction.value) {
    case 'drain':
      return '确认下架'
    case 'activate':
      return '确认重新上架'
    case 'suspend':
      return '确认紧急停用'
    case 'delete':
      return roomDeleteIntentExpired.value ? '确认已过期' : '确认软删除'
    default:
      return '确认操作'
  }
})

const isKeyResolutionMode = computed(() => routeQueryString(route.query.mode) === 'resolve-key-binding')
const keyResolutionApiKeyID = computed(() => {
  const value = Number(routeQueryString(route.query.api_key_id))
  return Number.isSafeInteger(value) && value > 0 ? value : 0
})
const keyResolutionApiKeyName = computed(() => routeQueryString(route.query.api_key_name).trim())
const keyResolutionKeyLabel = computed(() => keyResolutionApiKeyName.value || (keyResolutionApiKeyID.value > 0 ? `API Key #${keyResolutionApiKeyID.value}` : '指定 API Key'))
const keyResolutionActiveCount = computed(() => keyResolutionBindingStatus.value?.active_count ?? 0)
const keyResolutionQueuedCount = computed(() => keyResolutionBindingStatus.value?.queued_count ?? 0)
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
  if (keyResolutionLoading.value) return `正在核对 ${keyResolutionKeyLabel.value} 的使用与预约记录，请稍候。`
  if (keyResolutionError.value) return keyResolutionError.value
  if (keyResolutionAllClear.value) return '可以返回 API Key 管理重新执行删除或更换分组；系统不会自动继续原操作。'
  return '请在下方关联账号中结束使用、移出预约，并等待退出结算完成。全部处理完成后，状态会自动重新核对。'
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
    return '加载中'
  }
  if (mySpendUsingPagination.total === 0 && mySpendHistoryPagination.total === 0) return '暂无记录'
  return `使用/预约 ${mySpendUsingPagination.total} 条 · 历史 ${mySpendHistoryPagination.total} 条`
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
  if (selectedOwnerID.value > 0) {
    chips.push({
      key: `owner:${selectedOwnerID.value}`,
      label: `号主：${selectedOwnerDisplayName.value || `用户 #${selectedOwnerID.value}`}`,
      remove: () => {
        selectedOwnerID.value = 0
        selectedOwnerDisplayName.value = ''
      }
    })
  }
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
  const baseName = ACCOUNT_NAME_BASE_BY_PLATFORM[platform]
  const ownerUserID = Number(authStore.user?.id || 0)
  for (let index = 1; index <= 999; index += 1) {
    const candidate = index === 1 ? baseName : `${baseName}${index}`
    if (!hasKnownAccountName(candidate, ownerUserID)) return candidate
  }
  return `${baseName}${Date.now()}`
}

function validateAccountName(name: string, excludeAccountID?: number, ownerUserID = 0): string {
  const value = name.trim()
  if (!value) return '请填写房间名称'
  if (/\s/.test(name)) return '房间名称不能包含空格、换行或制表符'
  if (Array.from(value).length > 100) return '房间名称不能超过 100 个字符'
  if (hasKnownAccountName(value, ownerUserID, excludeAccountID)) return '房间名称已存在，请换一个名称'
  return ''
}

function buildPerUserConcurrencyLimitTip(maxPerUser: number): string {
  return `每个用户最多可设置 ${maxPerUser} 个并发请求；账号忙时仍受运行时请求能力限制，不影响成员上限。`
}

function validatePerUserConcurrencyValue(value: unknown): string {
  const perUserConcurrency = Number(value)
  if (!Number.isFinite(perUserConcurrency) || perUserConcurrency < 1) return '单用户最高并发必须大于 0'
  if (!Number.isInteger(perUserConcurrency)) return '单用户最高并发必须是整数'
  if (perUserConcurrency > MAX_PER_USER_CONCURRENCY) {
    return `单用户最高并发不能超过 ${MAX_PER_USER_CONCURRENCY}`
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

function abortMembershipHistoryRequest(): void {
  membershipHistoryRequestSeq += 1
  membershipHistoryRequestController?.abort()
  membershipHistoryRequestController = null
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
  return `这是你自己上架的账号，推荐测算按自用规则执行：${formatNumber(candidate.estimate.effective_rate_multiplier)}x、不收小时费、不校验最低余额；公开参数 ${formatNumber(listing.rate_multiplier)}x / 小时费 ${formatNumber(listing.hourly_rate)} / 低消 ${hourlyFeeWaiverLabel(listing.hourly_fee_waiver_minimum)} 仍用于其他用户。`
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
  if (listing.room_name) return listing.room_name
  if ((isOwnListing(listing) || authStore.isAdmin) && listing.account_name) {
    return listing.account_name
  }
  return `房间 #${listing.id}`
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
        ? '当前展示的是删除时保存的精确房间条款快照'
        : '当前展示的是本次历史使用时保存的精确条款快照'
    case 'backfilled_current':
      return context === 'archive'
        ? '当前展示的是由删除前最终房间信息回填的历史内容，不是删除时保存的精确快照'
        : '当前展示的是由当前或最终房间信息回填的历史内容，不是使用当时的精确快照'
    case 'unknown':
      return '该记录生成于历史快照功能上线前，迁移前信息不可恢复'
    default:
      return '当前展示历史记录，但服务端未标注快照精度'
  }
}

function deletedHistorySnapshotMessage(listing: AccountShareListing): string {
  return `该房间已删除；${historySnapshotDescription(listing, 'archive')}。不能再加入、编辑或管理账号。`
}

function isOwnListing(listing: AccountShareListing): boolean {
  const currentUserID = Number(authStore.user?.id || 0)
  return currentUserID > 0 && listing.owner_user_id === currentUserID
}

function selfUseJoinUnavailable(listing: AccountShareListing): boolean {
  return isOwnListing(listing) && (selfUseSettingsLoading.value || ownerSelfUseRateMultiplier.value === null)
}

function canShowListingJoinSection(listing: AccountShareListing): boolean {
  return !listing.deleted
    && !listing.queue_membership_id
    && !listing.current_membership_id
    && (!isManagementView.value || isOwnListing(listing))
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
    case 'validating':
      return '恢复校验中'
    case 'draining':
      return '安全排空中'
    case 'suspended':
      return '管理员暂停'
    case 'disabled':
      return '已下架'
    default:
      return status
  }
}

function listingStatusLabel(listing: AccountShareListing): string {
  return listing.deleted ? '已删除' : statusLabel(listing.status)
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

function pendingMembershipEndForListing(
  listing: AccountShareListing
): PendingMembershipEnd | null {
  return pendingMembershipEnds.value[listing.id] || null
}

function isListingMembershipEnding(listing: AccountShareListing): boolean {
  return listing.queue_status === 'ending'
    || pendingMembershipEndForListing(listing) !== null
}

function membershipPanelTitle(listing: AccountShareListing): string {
  if (isListingMembershipEnding(listing)) return '正在退出并结算'
  return listing.current_membership_id ? '正在使用' : '预约队列'
}

function membershipPanelSubtitle(listing: AccountShareListing): string {
  if (!isListingMembershipEnding(listing)) {
    return listing.current_membership_id
      ? idleTimeoutSummary(listing)
      : queueIdleTimeoutSummary(listing)
  }
  const pending = pendingMembershipEndForListing(listing)
  if (pending?.operationStatus === 'needs_attention') {
    return '后台仍在核对未完成计费；结算完成前不会开放评价'
  }
  if (
    pending?.operationStatus === 'failed'
    || pending?.operationStatus === 'cancelled'
  ) {
    return pending.operationError || '退出处理未完成，请联系管理员核对计费状态'
  }
  if (!pending?.operationID) {
    return '退出已受理，但缺少进度标识；请刷新状态并联系管理员'
  }
  return '退出请求已受理，正在等待请求释放并完成最终结算'
}

function queueStatusLabel(listing: AccountShareListing): string {
  if (isListingMembershipEnding(listing)) {
    const pending = pendingMembershipEndForListing(listing)
    if (pending?.operationStatus === 'needs_attention') return '结算待处理'
    if (pending?.operationStatus === 'failed' || pending?.operationStatus === 'cancelled') {
      return '退出处理失败'
    }
    return '退出/结算中'
  }
  if (listing.queue_status === 'active' || listing.current_membership_id) return '当前使用'
  if (isFuture(listing.queue_dispatch_cooldown_until)) return `冷却中，${formatRelativeUntil(listing.queue_dispatch_cooldown_until)} 后重试`
  return `预约第 ${listing.queue_rank || '-'} 位`
}

function queueStatusPillClass(listing: AccountShareListing): string {
  if (isListingMembershipEnding(listing)) {
    const pending = pendingMembershipEndForListing(listing)
    if (pending?.operationStatus === 'failed' || pending?.operationStatus === 'cancelled') {
      return 'membership-status-pill membership-status-pill-error'
    }
    return 'membership-status-pill membership-status-pill-ending'
  }
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
  return summary.listing.account_name || `账号房间 #${summary.listing.id}`
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

function mySpendAccountOptionKey(listingID: number, source: MySpendAccountOptionSource, membershipID: number): string {
  return `${source}:${listingID}:${membershipID}`
}

function mySpendAccountSourceLabel(source: MySpendAccountOptionSource): string {
  return source === 'using' ? '使用/预约' : '消费历史'
}

function mySpendAccountStatusLabel(option: MySpendAccountOption): string {
  if (option.status === 'active') return '正在使用'
  if (option.status === 'queued') return option.queueRank ? `预约第 ${option.queueRank} 位` : '预约中'
  if (option.status === 'ending') return '结算中'
  if (option.status === 'ended') return '已结束'
  return option.status || '可统计'
}

function mySpendAccountDisplayName(option: MySpendAccountOption): string {
  return option.roomName || option.accountName || `房间 #${option.listingID}`
}

function mySpendAccountUsagePeriod(option: MySpendAccountOption): string {
  if (option.source === 'history') {
    const joinedAt = option.joinedAt ? formatDate(option.joinedAt) : '时间未记录'
    const endedAt = option.endedAt ? formatDate(option.endedAt) : '尚未记录结束时间'
    return `${joinedAt} 至 ${endedAt}`
  }
  if (option.joinedAt) {
    const lastRequest = option.lastRequestAt ? ` · 最近请求 ${formatDate(option.lastRequestAt)}` : ''
    return `加入 ${formatDate(option.joinedAt)}${lastRequest}`
  }
  if (option.queueRank) return `预约第 ${option.queueRank} 位`
  return `使用记录 #${option.membershipID}`
}

function mySpendAccountOptionTitle(option: MySpendAccountOption): string {
  return `${mySpendAccountDisplayName(option)} · ${mySpendAccountSourceLabel(option.source)} · 记录 #${option.membershipID}`
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
    : (normalized.queue_membership_id ? (normalized.queue_status || 'queued') : 'ended')
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
    queueRank: normalized.queue_rank,
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
    pages: Math.max(1, Number(result.pages || Math.ceil(total / normalizedPageSize) || 1)),
  }
}

function applyMySpendAccountOptionPage(source: MySpendAccountOptionSource, result: MySpendAccountOptionPage): void {
  const pagination = source === 'using' ? mySpendUsingPagination : mySpendHistoryPagination
  pagination.page = result.page
  pagination.pageSize = result.pageSize
  pagination.total = result.total
  pagination.pages = result.pages
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

function setMySpendPickerSource(source: MySpendAccountOptionSource): void {
  mySpendPickerSource.value = source
}

async function handleMySpendAccountPageChange(page: number): Promise<void> {
  const source = mySpendPickerSource.value
  const pagination = source === 'using' ? mySpendUsingPagination : mySpendHistoryPagination
  const normalizedPage = Math.min(Math.max(1, Number(page || 1)), Math.max(1, pagination.pages))
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
    mySpendAccountsError.value = extractApiErrorMessage(error, '加载账号记录分页失败', {
      USER_NOT_FOUND: '当前用户状态异常，请重新登录后再试'
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
  })
  Object.assign(mySpendHistoryPagination, {
    page: 1,
    pageSize: MY_SPEND_ACCOUNT_PAGE_SIZE,
    total: 0,
    pages: 1,
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
    || memberships.find(item => item.status === 'queued')
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
  const nextPending = { ...pendingMembershipEnds.value }

  for (const [listingID, pending] of Object.entries(nextPending)) {
    if (Number(pending.apiKeyID || 0) === apiKeyID && !endingByListingID.has(Number(listingID))) {
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
    keyResolutionError.value = '处置链接缺少有效的 API Key ID，请返回 API Key 管理后重新进入。'
    return false
  }

  try {
    const bindingStatus = await accountShareAPI.getAPIKeyBindingStatus(apiKeyID)
    if (requestSeq !== keyResolutionRequestSeq || apiKeyID !== keyResolutionApiKeyID.value) return false
    if (bindingStatus.api_key_id !== apiKeyID) {
      throw new Error('关联状态返回了不匹配的 API Key ID，无法安全展示处置入口。')
    }
    const memberships = bindingStatus.memberships

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
    syncKeyResolutionEndingMemberships(apiKeyID, memberships, exactListings)
    keyResolutionBindingStatus.value = bindingStatus
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
    scheduleTransientStatusRefresh()
    return true
  } catch (error: unknown) {
    if (requestSeq !== keyResolutionRequestSeq) return false
    keyResolutionMemberships.value = []
    keyResolutionListings.value = []
    keyResolutionBindingStatus.value = null
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
      selfUseSettingsError.value = '全局自用抽成配置加载失败，暂时不能使用自己的房间账号，请刷新后重试。'
    }
  } catch (error: unknown) {
    selfUseSettingsError.value = extractApiErrorMessage(error, '全局自用抽成配置加载失败，暂时不能使用自己的房间账号，请刷新后重试。')
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
    capabilitiesError.value = extractApiErrorMessage(error, '房间配额暂时无法读取，请稍后刷新')
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
    listing.current_membership_id
    || listing.queue_membership_id
    || listing.status === 'validating'
  )) || hasPollablePendingMembershipEnd()
}

function pendingMembershipEndIsPollable(pending: PendingMembershipEnd): boolean {
  return Boolean(
    pending.operationID
    && !ROOM_LIFECYCLE_TERMINAL_OPERATION_STATUSES.has(pending.operationStatus)
  )
}

function hasPollablePendingMembershipEnd(): boolean {
  return Object.values(pendingMembershipEnds.value).some(pendingMembershipEndIsPollable)
}

function hasVisibleTransientStatus(): boolean {
  if (isMembershipHistoryView.value || isArchiveView.value) return false
  return visibleValidatingListingIDs.value.size > 0
    || hasPollablePendingMembershipEnd()
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
  const refreshed = await loadListings()
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
  return accountShareCapabilityBlockerMessages[blocker.code]
    || blocker.message?.trim()
    || '当前房间配额不足，请稍后重试或联系管理员'
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
    actionErrorDialog.title = '暂时不能创建房间'
    actionErrorDialog.message = capabilityBlockerMessage(blocker)
    actionErrorDialog.action = null
    actionErrorDialog.show = true
    return
  }
  showCreate.value = true
  if (createPlatform.value !== activeListingPlatform.value) {
    selectCreatePlatform(activeListingPlatform.value)
  } else if (createSourceMode.value === 'existing') {
    void loadOwnedAccounts()
  } else {
    void loadProxies()
  }
  void loadListingNameIndex()
  captureCreateDraftBaseline()
  void nextTick(() => {
    if (showCreate.value && !createDraftHasChanges()) captureCreateDraftBaseline()
  })
}

function closeCreateDialog(): void {
  if (creating.value || generatingOAuthURL.value) return
  if (createDraftHasChanges()) {
    pendingDraftDiscardTarget.value = 'create'
    return
  }
  abortOwnedAccountsRequest()
  showCreate.value = false
  createDraftBaseline.value = null
}

function resetOAuthState(): void {
  authURL.value = ''
  authSessionID.value = ''
  clearStableIdempotencyIntent(oauthExchangeIntent)
  oauthFlowRef.value?.reset()
}

function resetCreateForm(): void {
  Object.assign(createForm, buildDefaultCreateForm())
  allowedModels.value = defaultAllowedModelsForPlatform(createPlatform.value)
  createErrorMessage.value = ''
  clearPendingCreateRoomIdempotencyKey()
  resetOAuthState()
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
  createSourceMode.value = snapshot.sourceMode
  createPlatform.value = snapshot.platform
  selectedOwnedAccountID.value = snapshot.selectedOwnedAccountID
  Object.assign(createForm, snapshot.form)
  allowedModels.value = [...snapshot.allowedModels]
  oauthFlowRef.value?.reset()
  authURL.value = snapshot.authURL
  authSessionID.value = snapshot.authSessionID
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
  if (createPlatform.value === platform || creating.value || generatingOAuthURL.value) return
  const proxyID = createForm.proxy_id
  abortOwnedAccountsRequest()
  createPlatform.value = platform
  Object.assign(createForm, buildDefaultCreateForm(), { proxy_id: proxyID })
  allowedModels.value = defaultAllowedModelsForPlatform(platform)
  createErrorMessage.value = ''
  selectedOwnedAccountID.value = 0
  ownedAccounts.value = []
  ownedAccountsError.value = ''
  ownedAccountsLoadedPlatform = null
  clearPendingCreateRoomIdempotencyKey()
  resetOAuthState()
  if (createSourceMode.value === 'existing') {
    void loadOwnedAccounts(true)
  } else {
    void loadProxies()
  }
}

function selectCreateSourceMode(mode: 'existing' | 'oauth'): void {
  if (creating.value || generatingOAuthURL.value || createSourceMode.value === mode) return
  createSourceMode.value = mode
  createErrorMessage.value = ''
  clearPendingCreateRoomIdempotencyKey()
  if (mode === 'existing') {
    resetOAuthState()
    void loadOwnedAccounts()
  } else {
    void loadProxies()
  }
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

function openAddProxyDialog(close?: () => void): void {
  close?.()
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
    createForm.proxy_id = created.id
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
  const accountNameError = validateAccountName(
    createForm.name,
    undefined,
    Number(authStore.user?.id || 0)
  )
  if (accountNameError) return accountNameError
  if (createSourceMode.value === 'existing') {
    if (!selectedOwnedAccount.value) return '请选择一个可创建房间的自有账号'
  } else {
    if (currentProxyID.value <= 0) return '请选择代理 IP，或先添加自己的代理 IP'
    if (createProxyCapacityValidationMessage.value) return createProxyCapacityValidationMessage.value
  }
  if (!seatOptions.includes(Number(createForm.seat_limit))) return `成员上限必须在 ${ACCOUNT_SHARE_MIN_SEATS}-${ACCOUNT_SHARE_MAX_SEATS} 人之间`
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
  const accountNameError = validateAccountName(
    editForm.name,
    editingConfigListing.value?.id,
    Number(editingConfigListing.value?.owner_user_id || 0)
  )
  if (accountNameError) return accountNameError
  if (!seatOptions.includes(Number(editForm.seat_limit))) return `成员上限必须在 ${ACCOUNT_SHARE_MIN_SEATS}-${ACCOUNT_SHARE_MAX_SEATS} 人之间`
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
  if (
    !Number.isSafeInteger(Number(editingConfigListing.value?.row_version))
    || Number(editingConfigListing.value?.row_version) <= 0
  ) {
    return '房间版本无效，请关闭后刷新房间再编辑'
  }
  if (!editReason.value.trim()) return '请填写本次房间配置修改原因'
  if (editForceActive.value && !authStore.isAdmin) return '管理员身份已失效，请关闭窗口后重新进入'
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
      operationStatus: 'pending',
      operationError: operationID
        ? ''
        : '退出已受理，但服务端没有返回进度标识。请刷新状态并联系管理员。',
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
      operationError: operation.error_message || ''
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
    membershipHistoryError.value = formatAccountShareLoadError(error, '加载完整消费记录失败')
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

async function loadListings(): Promise<boolean> {
  abortActiveListingsRequest()
  const requestSeq = ++listingsRequestSeq
  const controller = new AbortController()
  const requestTab = activeFilter.value.tab
  listingsRequestController = controller
  let queueSnapshotRefreshStarted = false
  loading.value = true
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
    pagination.page = result.page || pagination.page
    pagination.page_size = result.page_size || ACCOUNT_SHARE_PAGE_SIZE
    pagination.pages = result.pages || 1
    listings.value = realListings
    if (requestTab === 'archive') {
      visibleValidatingListingIDs.value = new Set()
      unavailableQueueSnapshotApiKeyIDs.value = new Set()
      visibleQueueSnapshotWarning.value = ''
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
    unavailableQueueSnapshotApiKeyIDs.value = new Set(queueApiKeyIDsForListings(realListings))
    visibleQueueSnapshotWarning.value = ''
    lastMembershipStatusRefreshAt = Date.now()
    scheduleTransientStatusRefresh()
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
    scheduleTransientStatusRefresh()
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

function removeKnownListing(listingID: number): void {
  knownListings.value = knownListings.value.filter((listing) => listing.id !== listingID)
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
  ownerDialog.listingsPages = 1
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
  ownerDialog.listingsPages = 1
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
  selectedOwnerDisplayName.value = ownerDialog.ownerUsername || `用户 #${ownerUserID}`
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
    ownerDialog.listingsPages = Math.max(ownerDialog.listingsPage, result.pages || 1)
    ownerDialog.listingsTotal = Math.max(ownerDialog.listings.length, result.total || 0)
    ownerDialog.listingsError = ''
  } catch (error: unknown) {
    if (controller.signal.aborted || requestSeq !== ownerDialogRequestSeq || isCanceledRequest(error)) return
    ownerDialog.listingsError = extractApiErrorMessage(error, '加载号主账号失败')
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
    ownerDialog.reviewsError = extractApiErrorMessage(error, '加载号主评论失败')
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
    ? '当前模型近3天历史不足，已按全部模型均值填入'
    : '已按近3天历史均值填入'
  const capped = profile.capped ? '，部分数值已按测算上限处理' : ''
  const activeHours = normalizeRecommendationActiveHours(profile.active_hours)
  const requestsPerHour = profile.request_count / activeHours
  return `${prefix}：单次文本输入 ${formatNumber(profile.input_tokens_per_request)}、文本输出 ${formatNumber(profile.output_tokens_per_request)}、Cache写入 ${formatNumber(profile.cache_creation_tokens_per_request)}、历史总Cache读取 ${formatNumber(profile.cache_read_tokens_per_request)}（未自动填入）、图片输入 ${formatNumber(profile.image_input_tokens_per_request)}、图片输出 ${formatNumber(profile.image_output_tokens_per_request)}；文本/图片Cache读取因无法可靠拆分，均保留手工值。按 ${profile.request_count} 次 / ${formatNumber(activeHours)} 小时（${formatNumber(requestsPerHour)} 次/小时）测算预计额度${capped}`
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
      recommendationUsageProfileMessage.value = '近3天暂无历史请求，已保留当前预设'
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
    recommendationUsageProfileMessage.value = extractApiErrorMessage(error, '近3天均值读取失败')
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
    recommendationForm.cache_read_tokens_per_request,
    recommendationForm.image_input_tokens_per_request,
    recommendationForm.image_output_tokens_per_request,
    recommendationForm.image_cache_read_tokens_per_request
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
    recommendationError.value = extractApiErrorMessage(error, '账号推荐测算失败', accountShareRecommendationErrorMessages)
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
    recommendationError.value = '推荐结果已失效，请重新测算'
    return
  }
  const currentApiKeyID = Number(recommendationForm.api_key_id || 0)
  if (currentApiKeyID !== requestSnapshot.api_key_id) {
    recommendationError.value = 'API Key 已改变，请重新测算后再使用推荐结果'
    return
  }
  if (!recommendationKeyOptions.value.some(item => item.id === requestSnapshot.api_key_id)) {
    recommendationError.value = '生成推荐时使用的 API Key 已不可用，请重新测算'
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
    ownedAccountsError.value = extractApiErrorMessage(error, '加载自有账号失败，请重试')
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
    anthropic_7d_limit_percent: Number(createForm.anthropic_7d_limit_percent)
  }
}

function clearPendingCreateRoomIdempotencyKey(): void {
  pendingCreateRoomIntentSignature = ''
  pendingCreateRoomIdempotencyKey = ''
}

function createSecureRequestID(): string {
  const requestID = globalThis.crypto?.randomUUID?.()
  if (!requestID) {
    throw new Error('当前浏览器无法生成安全的幂等键，请升级浏览器后重试。')
  }
  return requestID
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
    createErrorMessage.value = '所选账号状态已变化，请刷新账号列表后重试'
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
        ? '房间已创建，账号已从公共号池切换到新房间'
        : '房间已创建'
    )
    resetCreateForm()
    showCreate.value = false
    createDraftBaseline.value = null
    await Promise.all([loadOwnedAccounts(true), loadListings(), loadCapabilities()])
  } catch (error: unknown) {
    createErrorMessage.value = extractApiErrorMessage(
      error,
      '创建房间失败',
      accountShareRoomCreateErrorMessages
    )
  } finally {
    creating.value = false
  }
}

function openRoomAccountsDialog(listing: AccountShareListing): void {
  if (listing.deleted) {
    showActionError('已删除房间不再提供账号管理，历史条款快照仍可查看。', '房间已删除')
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

  const actionLabel = payload.operation === 'add' ? '加入房间' : '退出房间'
  if (payload.failed > 0) {
    appStore.showWarning(
      `${actionLabel}部分完成：成功 ${payload.success} 个，失败 ${payload.failed} 个；失败原因已显示在房间账号窗口中。`
    )
  } else {
    appStore.showSuccess(
      payload.operation === 'add'
        ? `已有 ${payload.success} 个账号成功加入房间`
        : `已有 ${payload.success} 个账号成功退出房间，账号仍保持原平台账号模式`
    )
  }

  const [refreshed] = await Promise.all([loadListings(), loadCapabilities()])
  if (!refreshed) {
    appStore.showWarning('账号变更已生效，但账号广场计数刷新失败；请稍后点击页面顶部“刷新”确认。')
    return
  }

  const refreshedListing = listings.value.find((listing) => listing.id === listingID)
  if (refreshedListing) {
    roomAccountsListing.value = refreshedListing
  } else {
    roomAccountsListing.value = null
  }
}

function roomLifecycleStatusLabel(status: AccountShareRoomLifecycleStatus): string {
  switch (status) {
    case 'active':
      return '开放使用'
    case 'paused':
      return '已暂停'
    case 'validating':
      return '恢复校验中'
    case 'draining':
      return '安全排空中'
    case 'suspended':
      return '管理员暂停'
  }
}

function roomLifecycleStatusBadgeClass(status: AccountShareRoomLifecycleStatus): string {
  const base = 'rounded-full px-2.5 py-1 text-xs font-semibold'
  switch (status) {
    case 'active':
      return `${base} bg-emerald-50 text-emerald-700 dark:bg-emerald-500/10 dark:text-emerald-200`
    case 'validating':
    case 'draining':
      return `${base} bg-blue-50 text-blue-700 dark:bg-blue-500/10 dark:text-blue-200`
    case 'paused':
      return `${base} bg-amber-50 text-amber-700 dark:bg-amber-500/10 dark:text-amber-200`
    case 'suspended':
      return `${base} bg-gray-100 text-gray-600 dark:bg-dark-700 dark:text-dark-200`
  }
}

function roomLifecycleHealthLabel(healthState: AccountShareRoomHealthState): string {
  switch (healthState) {
    case 'healthy':
      return '健康'
    case 'degraded':
      return '部分可用'
    case 'unavailable':
      return '不可用'
  }
}

function roomLifecycleActionAllowed(action: AccountShareRoomLifecycleAction): boolean {
  return roomLifecycleState.value?.allowed_actions.includes(action) === true
}

function roomLifecycleActionTitle(action: Exclude<AccountShareRoomLifecycleAction, 'delete'>): string {
  switch (action) {
    case 'drain':
      return '下架房间'
    case 'activate':
      return '重新上架'
    case 'suspend':
      return '紧急停用房间'
  }
}

function roomLifecycleActionDescription(action: Exclude<AccountShareRoomLifecycleAction, 'delete'>): string {
  switch (action) {
    case 'drain':
      return '房间将停止接收新成员；现有消费者继续正常使用，已有预约不会被取消。'
    case 'activate':
      return '系统会校验房间主账号的连通性和可用状态；只有校验通过才会重新开放。'
    case 'suspend':
      return '管理员将因异常立即停用房间，恢复前不会再分配给消费用户。'
  }
}

function roomLifecycleActionImpact(action: Exclude<AccountShareRoomLifecycleAction, 'delete'>): string {
  switch (action) {
    case 'drain':
      return '下架不会中断现有消费者，也不会删除房间或历史记录。'
    case 'activate':
      return '恢复校验失败时房间仍保持暂停，并展示失败原因，不会带病开放。'
    case 'suspend':
      return '紧急停用不会删除房间或历史记录，操作原因会被审计。'
  }
}

function roomLifecycleOperationLabel(operation: AccountShareRoomOperation): string {
  const actionLabel = operation.action === 'delete_room' ? '软删除房间' : '旧版排空任务'
  switch (operation.status) {
    case 'succeeded':
      return `${actionLabel}已完成`
    case 'failed':
      return `${actionLabel}失败`
    case 'cancelled':
      return `${actionLabel}已取消`
    case 'needs_attention':
      return `${actionLabel}需要处理阻塞项`
    case 'running':
      return `${actionLabel}执行中`
    case 'pending':
      return `${actionLabel}等待执行`
  }
}

function roomLifecycleOperationStatusDescription(operation: AccountShareRoomOperation): string {
  if (operation.error_message) return operation.error_message
  switch (operation.status) {
    case 'succeeded':
      return '服务端已完成全部状态与历史快照写入。'
    case 'failed':
      return '操作未完成，请根据错误信息处理后重新打开房间状态。'
    case 'cancelled':
      return '操作已经取消，房间未按本次请求继续变更。'
    case 'needs_attention':
      return '仍有运行时或结算阻塞项，系统会继续等待。'
    case 'running':
      return '正在等待请求、成员与结算安全收口。'
    case 'pending':
      return '操作已受理，正在等待后台处理。'
  }
}

function formatRoomDeleteIntentExpiry(value?: string): string {
  const expiresAt = normalizeDateInput(value)
  if (!expiresAt) return '令牌过期时'
  return expiresAt.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit', second: '2-digit' })
}

function clearRoomLifecycleError(): void {
  roomLifecycleError.value = ''
  roomLifecycleErrorCode.value = ''
}

function setRoomLifecycleError(error: unknown, fallback: string): void {
  roomLifecycleErrorCode.value = extractApiErrorCode(error) || ''
  roomLifecycleError.value = extractApiErrorMessage(
    error,
    fallback,
    ROOM_LIFECYCLE_ERROR_MESSAGES
  )
}

function clearRoomLifecycleIdempotencyKey(): void {
  roomLifecycleIdempotencySignature = ''
  roomLifecycleIdempotencyKey = ''
}

function getRoomLifecycleIdempotencyKey(
  listingID: number,
  action: AccountShareRoomLifecycleAction,
  expectedVersion: number,
  token = ''
): string {
  const signature = JSON.stringify({ listingID, action, expectedVersion, token })
  if (
    roomLifecycleIdempotencyKey &&
    roomLifecycleIdempotencySignature === signature
  ) {
    return roomLifecycleIdempotencyKey
  }
  roomLifecycleIdempotencySignature = signature
  roomLifecycleIdempotencyKey = `account-share-room-${listingID}-${action}-${createSecureRequestID()}`
  return roomLifecycleIdempotencyKey
}

function stopRoomLifecycleOperationPolling(): void {
  roomLifecycleOperationPollSeq += 1
  if (roomLifecycleOperationPollTimer !== null) {
    window.clearTimeout(roomLifecycleOperationPollTimer)
    roomLifecycleOperationPollTimer = null
  }
  roomLifecycleOperationController?.abort()
  roomLifecycleOperationController = null
  roomLifecyclePolling.value = false
}

function resetRoomLifecycleAction(): void {
  if (roomLifecycleCommandBusy.value || roomLifecycleHasPendingOperation.value) return
  roomLifecycleAction.value = null
  roomDeleteIntent.value = null
  roomDeleteNameConfirmation.value = ''
  roomLifecycleReason.value = ''
  clearRoomLifecycleIdempotencyKey()
  clearRoomLifecycleError()
}

function openRoomLifecycleDialog(listing: AccountShareListing): void {
  if (listing.deleted || (!authStore.isAdmin && !isOwnListing(listing))) return
  stopRoomLifecycleOperationPolling()
  roomLifecycleStateController?.abort()
  roomLifecycleStateController = null
  roomLifecycleListing.value = listing
  roomLifecycleState.value = null
  roomLifecycleOperation.value = null
  roomLifecycleAction.value = null
  roomDeleteIntent.value = null
  roomDeleteNameConfirmation.value = ''
  roomLifecycleReason.value = ''
  roomLifecycleDeleted.value = false
  roomLifecycleLoading.value = false
  roomDeleteIntentLoading.value = false
  roomLifecycleSubmitting.value = false
  clearRoomLifecycleIdempotencyKey()
  clearRoomLifecycleError()
  void refreshRoomLifecycleState()
}

function closeRoomLifecycleDialog(): void {
  if (roomLifecycleCommandBusy.value) return
  roomLifecycleStateRequestSeq += 1
  roomLifecycleStateController?.abort()
  roomLifecycleStateController = null
  stopRoomLifecycleOperationPolling()
  roomLifecycleListing.value = null
  roomLifecycleState.value = null
  roomLifecycleOperation.value = null
  roomLifecycleAction.value = null
  roomDeleteIntent.value = null
  roomDeleteNameConfirmation.value = ''
  roomLifecycleReason.value = ''
  roomLifecycleDeleted.value = false
  roomLifecycleLoading.value = false
  clearRoomLifecycleIdempotencyKey()
  clearRoomLifecycleError()
}

async function refreshRoomLifecycleState(): Promise<void> {
  const listing = roomLifecycleListing.value
  if (!listing) return

  stopRoomLifecycleOperationPolling()
  roomLifecycleStateController?.abort()
  const controller = new AbortController()
  roomLifecycleStateController = controller
  const requestSeq = ++roomLifecycleStateRequestSeq
  roomLifecycleLoading.value = true
  roomLifecycleAction.value = null
  roomLifecycleOperation.value = null
  roomDeleteIntent.value = null
  roomDeleteNameConfirmation.value = ''
  roomLifecycleReason.value = ''
  clearRoomLifecycleIdempotencyKey()
  clearRoomLifecycleError()
  try {
    const state = await accountShareAPI.getRoomManagementState(listing.id, {
      signal: controller.signal
    })
    if (
      requestSeq !== roomLifecycleStateRequestSeq ||
      roomLifecycleListing.value?.id !== listing.id
    ) {
      return
    }
    roomLifecycleState.value = state
    if (state.pending_operation_id) {
      startRoomLifecycleOperationPolling(state.pending_operation_id)
    }
  } catch (error: unknown) {
    if (
      requestSeq !== roomLifecycleStateRequestSeq ||
      isCanceledRequest(error)
    ) {
      return
    }
    setRoomLifecycleError(error, '读取房间生命周期状态失败，请稍后重试。')
  } finally {
    if (requestSeq === roomLifecycleStateRequestSeq) {
      roomLifecycleLoading.value = false
      if (roomLifecycleStateController === controller) {
        roomLifecycleStateController = null
      }
    }
  }
}

function selectRoomLifecycleAction(action: AccountShareRoomLifecycleAction): void {
  if (
    roomLifecycleCommandBusy.value ||
    roomLifecycleHasPendingOperation.value ||
    (action !== 'delete' && !roomLifecycleActionAllowed(action))
  ) {
    return
  }
  roomLifecycleAction.value = action
  roomDeleteIntent.value = null
  roomDeleteNameConfirmation.value = ''
  roomLifecycleReason.value = ''
  clearRoomLifecycleIdempotencyKey()
  clearRoomLifecycleError()
  if (action === 'delete' && !authStore.isAdmin) {
    void loadRoomDeleteIntent()
  }
}

async function loadRoomDeleteIntent(): Promise<void> {
  const listing = roomLifecycleListing.value
  const state = roomLifecycleState.value
  if (!listing || !state || roomLifecycleAction.value !== 'delete') return
  if (roomDeleteIntentLoading.value || roomLifecycleSubmitting.value) return
  const reason = roomLifecycleReason.value.trim()
  if (authStore.isAdmin && !reason) {
    roomLifecycleErrorCode.value = 'ACCOUNT_SHARE_ROOM_REASON_REQUIRED'
    roomLifecycleError.value = '管理员必须填写删除原因后再检查删除条件。'
    return
  }

  roomDeleteIntentLoading.value = true
  roomDeleteIntent.value = null
  roomDeleteNameConfirmation.value = ''
  clearRoomLifecycleIdempotencyKey()
  clearRoomLifecycleError()
  try {
    const intent = await accountShareAPI.createRoomDeleteIntent(listing.id, {
      expected_version: state.row_version,
      ...(authStore.isAdmin ? { reason } : {})
    })
    if (
      roomLifecycleListing.value?.id !== listing.id ||
      roomLifecycleAction.value !== 'delete' ||
      roomLifecycleState.value?.row_version !== state.row_version
    ) {
      return
    }
    roomDeleteIntent.value = intent
  } catch (error: unknown) {
    if (!roomLifecycleListing.value) return
    setRoomLifecycleError(error, '检查房间删除条件失败，请稍后重试。')
  } finally {
    roomDeleteIntentLoading.value = false
  }
}

async function submitRoomLifecycleAction(): Promise<void> {
  const listing = roomLifecycleListing.value
  const state = roomLifecycleState.value
  const action = roomLifecycleAction.value
  if (
    !listing ||
    !state ||
    !action ||
    roomLifecycleSubmitting.value
  ) {
    return
  }
  if (action === 'delete') {
    const expiresAt = normalizeDateInput(roomDeleteIntent.value?.expires_at)
    if (expiresAt && expiresAt.getTime() <= Date.now()) {
      nowMs.value = Date.now()
      roomLifecycleErrorCode.value = 'ACCOUNT_SHARE_ROOM_DELETION_TOKEN_INVALID'
      roomLifecycleError.value = '删除确认已经过期，请重新获取确认后再提交。'
      return
    }
  }
  if (!canSubmitRoomLifecycleAction.value) return

  roomLifecycleSubmitting.value = true
  clearRoomLifecycleError()
  try {
    if (action === 'delete') {
      const intent = roomDeleteIntent.value
      if (!intent?.token) return
      const operation = await accountShareAPI.deleteRoom(
        listing.id,
        {
          expected_version: intent.row_version,
          room_name: roomDeleteNameConfirmation.value,
          token: intent.token,
          confirmed: true,
          ...(authStore.isAdmin ? { reason: roomLifecycleReason.value.trim() } : {})
        },
        getRoomLifecycleIdempotencyKey(
          listing.id,
          action,
          intent.row_version,
          intent.token
        )
      )
      if (roomLifecycleListing.value?.id !== listing.id) return
      roomLifecycleOperation.value = operation
      clearRoomLifecycleIdempotencyKey()
      if (ROOM_LIFECYCLE_TERMINAL_OPERATION_STATUSES.has(operation.status)) {
        await handleRoomLifecycleTerminalOperation(operation)
      } else {
        startRoomLifecycleOperationPolling(operation.id)
        appStore.showSuccess('软删除请求已受理，正在安全收口房间数据')
      }
      return
    }

    const payload = {
      expected_version: state.row_version,
      confirmed: true,
      ...(authStore.isAdmin ? { reason: roomLifecycleReason.value.trim() } : {})
    }
    const idempotencyKey = getRoomLifecycleIdempotencyKey(
      listing.id,
      action,
      state.row_version
    )
    const updatedState = action === 'drain'
      ? await accountShareAPI.drainRoom(listing.id, payload, idempotencyKey)
      : action === 'activate'
        ? await accountShareAPI.activateRoom(listing.id, payload, idempotencyKey)
        : await accountShareAPI.suspendRoom(listing.id, payload, idempotencyKey)

    if (roomLifecycleListing.value?.id !== listing.id) return
    roomLifecycleState.value = updatedState
    roomLifecycleAction.value = null
    clearRoomLifecycleIdempotencyKey()
    await loadListings()
    const refreshedListing = listings.value.find(item => item.id === listing.id)
    if (refreshedListing) roomLifecycleListing.value = refreshedListing
    if (updatedState.pending_operation_id) {
      startRoomLifecycleOperationPolling(updatedState.pending_operation_id)
      appStore.showSuccess('旧版排空任务正在收口')
    } else {
      appStore.showSuccess(
        action === 'activate'
          ? '房间已重新上架'
          : action === 'drain'
            ? '房间已下架，现有用户不受影响'
            : '房间已紧急停用'
      )
    }
  } catch (error: unknown) {
    if (!roomLifecycleListing.value) return
    setRoomLifecycleError(error, '房间生命周期操作失败，请稍后重试。')
  } finally {
    roomLifecycleSubmitting.value = false
  }
}

function startRoomLifecycleOperationPolling(operationID: string): void {
  const normalizedOperationID = operationID.trim()
  if (!normalizedOperationID || !roomLifecycleListing.value) return
  stopRoomLifecycleOperationPolling()
  const pollSeq = roomLifecycleOperationPollSeq
  roomLifecyclePolling.value = true
  void pollRoomLifecycleOperation(normalizedOperationID, pollSeq)
}

function pollRoomLifecycleOperationNow(): void {
  if (roomLifecyclePolling.value) return
  const operationID = roomLifecycleOperation.value?.id ||
    roomLifecycleState.value?.pending_operation_id ||
    ''
  if (!operationID) {
    void refreshRoomLifecycleState()
    return
  }
  clearRoomLifecycleError()
  startRoomLifecycleOperationPolling(operationID)
}

async function pollRoomLifecycleOperation(
  operationID: string,
  pollSeq: number
): Promise<void> {
  if (
    pollSeq !== roomLifecycleOperationPollSeq ||
    !roomLifecycleListing.value
  ) {
    return
  }

  roomLifecycleOperationController?.abort()
  const controller = new AbortController()
  roomLifecycleOperationController = controller
  try {
    const operation = await accountShareAPI.getRoomOperation(operationID, {
      signal: controller.signal
    })
    if (
      pollSeq !== roomLifecycleOperationPollSeq ||
      !roomLifecycleListing.value
    ) {
      return
    }
    roomLifecycleOperation.value = operation
    if (ROOM_LIFECYCLE_TERMINAL_OPERATION_STATUSES.has(operation.status)) {
      roomLifecyclePolling.value = false
      roomLifecycleOperationController = null
      await handleRoomLifecycleTerminalOperation(operation)
      return
    }
    roomLifecycleOperationPollTimer = window.setTimeout(() => {
      roomLifecycleOperationPollTimer = null
      void pollRoomLifecycleOperation(operationID, pollSeq)
    }, ROOM_LIFECYCLE_OPERATION_POLL_INTERVAL_MS)
  } catch (error: unknown) {
    if (
      pollSeq !== roomLifecycleOperationPollSeq ||
      isCanceledRequest(error)
    ) {
      return
    }
    roomLifecyclePolling.value = false
    setRoomLifecycleError(error, '查询房间操作进度失败；你可以点击“继续查询”重试。')
  } finally {
    if (roomLifecycleOperationController === controller) {
      roomLifecycleOperationController = null
    }
  }
}

async function handleRoomLifecycleTerminalOperation(
  operation: AccountShareRoomOperation
): Promise<void> {
  if (operation.status !== 'succeeded') {
    roomLifecycleErrorCode.value = operation.error_code || operation.status
    roomLifecycleError.value = operation.error_message ||
      (operation.status === 'cancelled'
        ? '房间操作已取消，当前房间没有按本次请求继续变更。'
        : '房间操作执行失败，请处理错误后重新打开房间状态。')
    return
  }

  clearRoomLifecycleError()
  if (operation.action === 'delete_room') {
    removeKnownListing(operation.listing_id)
    roomLifecycleDeleted.value = true
    roomLifecycleAction.value = null
    roomDeleteIntent.value = null
    roomDeleteNameConfirmation.value = ''
    roomLifecycleReason.value = ''
    await Promise.all([loadListings(), loadCapabilities()])
    appStore.showSuccess('房间已软删除，历史消费、结算和评价记录继续保留')
    return
  }

  appStore.showSuccess('房间已完成排空并暂停')
  await Promise.all([loadListings(), refreshRoomLifecycleState()])
  const refreshedListing = listings.value.find(item => item.id === operation.listing_id)
  if (refreshedListing) roomLifecycleListing.value = refreshedListing
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
      const exchangePayload = {
        ...payload,
        anthropic_5h_limit_percent: Number(createForm.anthropic_5h_limit_percent),
        anthropic_7d_limit_percent: Number(createForm.anthropic_7d_limit_percent)
      }
      const idempotencyKey = getStableIdempotencyKey(
        oauthExchangeIntent,
        'account-share-oauth-anthropic',
        { platform: 'anthropic', payload: exchangePayload }
      )
      await accountShareAPI.exchangeAnthropicCode(exchangePayload, idempotencyKey)
    } else {
      const exchangePayload = {
        ...payload,
        state: oauthState,
        codex_cli_only: createForm.codex_cli_only,
        codex_5h_limit_percent: Number(createForm.codex_5h_limit_percent),
        codex_7d_limit_percent: Number(createForm.codex_7d_limit_percent)
      }
      const idempotencyKey = getStableIdempotencyKey(
        oauthExchangeIntent,
        'account-share-oauth-openai',
        { platform: 'openai', payload: exchangePayload }
      )
      await accountShareAPI.exchangeOpenAICode(exchangePayload, idempotencyKey)
    }
    clearStableIdempotencyIntent(oauthExchangeIntent)
    resetCreateForm()
    showCreate.value = false
    createDraftBaseline.value = null
    await loadListings()
  } catch (error: unknown) {
    createErrorMessage.value = extractApiErrorMessage(
      error,
      '创建账号房间失败',
      accountShareRoomCreateErrorMessages
    )
  } finally {
    creating.value = false
  }
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
    showActionError('退出结算处理中，结算完成后才能重新加入或排队。', '暂时无法加入')
    return
  }
  if (isOwnListing(listing) && ownerSelfUseRateMultiplier.value === null) {
    showActionError(
      selfUseSettingsError.value || '全局自用抽成配置尚未加载，暂时不能使用自己的房间账号，请刷新后重试。',
      '自用配置不可用'
    )
    return
  }
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

  const idleTimeoutMinutes = normalizeIdleTimeoutMinutes(idleTimeoutValue)
  const key = modeApiKeysForListing(listing).find(item => item.id === apiKeyID)
  preparingJoinId.value = listing.id
  joinIntentError.value = ''
  try {
    const intent = await requestJoinIntent(
      listing.id,
      apiKeyID,
      idleTimeoutMinutes,
      false
    )
    pendingJoinConfirmation.value = {
      listingID: listing.id,
      ownerSelfUse: isOwnListing(listing),
      platform,
      apiKeyID,
      apiKeyLabel: key ? modeKeyLabel(key) : `Key #${apiKeyID}`,
      idleTimeoutMinutes,
      intent
    }
  } catch (error: unknown) {
    showActionError(
      extractApiErrorMessage(error, '获取最新加入条款失败，请稍后重试', accountShareJoinErrorMessages),
      '暂时无法确认加入'
    )
  } finally {
    if (preparingJoinId.value === listing.id) preparingJoinId.value = null
  }
}

async function requestJoinIntent(
  listingID: number,
  apiKeyID: number,
  idleTimeoutMinutes: number,
  acceptQueue: boolean
): Promise<AccountShareJoinIntent> {
  const intent = await accountShareAPI.createJoinIntent(listingID, {
    api_key_id: apiKeyID,
    idle_timeout_minutes: idleTimeoutMinutes,
    accept_queue: acceptQueue
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
    intent?.accept_queue !== acceptQueue ||
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
    throw new Error('服务端返回的加入确认条款不完整或已经失效，请刷新后重试')
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
}

async function updatePendingJoinQueueAcceptance(event: Event): Promise<void> {
  const checkbox = event.target as HTMLInputElement | null
  const pendingJoin = pendingJoinConfirmation.value
  if (!checkbox || !pendingJoin || joinDialogBusy.value) {
    if (checkbox && pendingJoin) checkbox.checked = pendingJoin.intent.accept_queue
    return
  }

  const acceptQueue = checkbox.checked
  if (acceptQueue === pendingJoin.intent.accept_queue) return
  refreshingJoinIntent.value = true
  joinIntentError.value = ''
  try {
    const intent = await requestJoinIntent(
      pendingJoin.listingID,
      pendingJoin.apiKeyID,
      pendingJoin.idleTimeoutMinutes,
      acceptQueue
    )
    if (pendingJoinConfirmation.value !== pendingJoin) return
    pendingJoinConfirmation.value = {
      ...pendingJoin,
      intent
    }
  } catch (error: unknown) {
    checkbox.checked = pendingJoin.intent.accept_queue
    joinIntentError.value = extractApiErrorMessage(
      error,
      '更新排队选择失败，原确认条款未改变，请重试'
    )
  } finally {
    refreshingJoinIntent.value = false
  }
}

async function confirmJoinUse(): Promise<void> {
  const pendingJoin = pendingJoinConfirmation.value
  if (!pendingJoin || joinDialogBusy.value) return
  if (Date.parse(pendingJoin.intent.expires_at) <= Date.now()) {
    await invalidateJoinConfirmation('加入确认已过期，房间状态已刷新，请重新点击加入并确认最新条款。')
    return
  }
  if (pendingJoin.intent.queue_may_be_required && !pendingJoin.intent.accept_queue) {
    joinIntentError.value = '当前状态可能需要预约，请先明确勾选“同意进入预约队列”。'
    return
  }
  await submitJoinUse(pendingJoin)
}

async function invalidateJoinConfirmation(message: string): Promise<void> {
  pendingJoinConfirmation.value = null
  joinIntentError.value = ''
  const refreshed = await loadListings()
  showActionError(
    refreshed ? message : `${message} 列表刷新失败，请先点击页面顶部“刷新”。`,
    '请重新确认加入'
  )
}

async function submitJoinUse(pendingJoin: PendingJoinConfirmation): Promise<void> {
  const { listingID, apiKeyID, idleTimeoutMinutes, intent } = pendingJoin
  joiningId.value = listingID
  let joinSucceeded = false
  try {
    const membership = await accountShareAPI.joinListing(listingID, {
      api_key_id: apiKeyID,
      idle_timeout_minutes: idleTimeoutMinutes,
      intent_token: intent.token,
      expected_version: intent.expected_version,
      expected_revision_id: intent.expected_revision_id,
      accept_queue: intent.accept_queue
    })
    joinSucceeded = true
    pendingJoinConfirmation.value = null
    joinIntentError.value = ''
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
    if (joinSucceeded) {
      pendingJoinConfirmation.value = null
      appStore.showWarning('预约或加入已成功，但状态刷新时发生异常；记录已经创建，请稍后点击页面顶部“刷新”确认状态。')
    } else {
      const errorCode = extractApiErrorCode(error)
      if (
        errorCode === 'ACCOUNT_SHARE_JOIN_INTENT_INVALID' ||
        errorCode === 'ACCOUNT_SHARE_JOIN_INTENT_CONSUMED' ||
        errorCode === 'ACCOUNT_SHARE_JOIN_TERMS_CHANGED' ||
        errorCode === 'ACCOUNT_SHARE_QUEUE_CONFIRMATION_REQUIRED'
      ) {
        const message = errorCode === 'ACCOUNT_SHARE_QUEUE_CONFIRMATION_REQUIRED'
          ? '房间席位刚刚发生变化，现在需要进入预约队列。请重新点击加入，并明确勾选同意排队。'
          : '房间条款或确认令牌已经变化，旧确认已关闭。请重新点击加入并确认最新条款。'
        await invalidateJoinConfirmation(message)
      } else {
        pendingJoinConfirmation.value = null
        joinIntentError.value = ''
        showActionError(extractApiErrorMessage(error, '加入使用失败', accountShareJoinErrorMessages), '加入使用失败')
      }
    }
  } finally {
    joiningId.value = null
  }
}

function handleEndUseClick(listing: AccountShareListing): void {
  const membershipID = Number(listing.queue_membership_id || listing.current_membership_id || 0)
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
  if (pending.status === 'queued') {
    void endUse(pending)
    return
  }
  pendingEndUse.value = pending
}

async function pollPendingMembershipEndOperations(): Promise<PendingMembershipEnd[]> {
  const requestSeq = membershipEndOperationRequestSeq
  const entries = Object.values(pendingMembershipEnds.value)
    .filter(pendingMembershipEndIsPollable)
  if (entries.length === 0) return []

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
            ...current,
            operationStatus: 'failed',
            operationError: '退出操作返回了不完整的完成状态，请联系管理员核对。'
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
            operationError: extractApiErrorMessage(
              error,
              '查询退出结算进度失败，系统会继续重试。',
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

  const completed = results.filter(
    (item): item is PendingMembershipEnd => item !== null
  )
  if (completed.length > 0) {
    appStore.showSuccess(
      completed.length === 1
        ? '退出与结算已完成'
        : `${completed.length} 个退出与结算任务已完成`
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
    && pending.status !== 'queued'
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
    const intent = await accountShareAPI.createEndMembershipIntent(membershipID)
    const responseMembership = await accountShareAPI.endMembership(membershipID, intent.token)
    const membership = responseMembership.status === 'ending' && !responseMembership.ending_operation_id
      ? { ...responseMembership, ending_operation_id: intent.operation_id }
      : responseMembership
    endSucceeded = true
    if (membership.status === 'ending') {
      setPendingMembershipEnd(pending, membership)
    } else if (membership.status === 'ended') {
      removePendingMembershipEnd(pending.listing.id)
    }
    if (pendingEndUse.value === pending) pendingEndUse.value = null
    const successMessage = pending.status === 'queued'
      ? '已移出预约'
      : membership.status === 'ending'
        ? '退出请求已受理，正在释放请求并完成结算'
        : '已结束使用'
    const refreshed = await loadListings()
    const resolutionRefreshed = !isKeyResolutionMode.value || await loadKeyResolutionState()
    if (refreshed && resolutionRefreshed) {
      if (membership.status === 'ending' && !membership.ending_operation_id) {
        appStore.showWarning(`${successMessage}，但进度标识缺失；请刷新状态并联系管理员。`)
      } else {
        appStore.showSuccess(successMessage)
      }
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
  clearStableIdempotencyIntent(reviewSubmitIntent)
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
  const normalizedPlatform = entry.platform.trim().toLowerCase()
  pendingReview.value = {
    membershipID: entry.membership_id,
    platformLabel: normalizedPlatform === 'openai' || normalizedPlatform === 'anthropic'
      ? platformLabel(normalizedPlatform)
      : (entry.platform.trim() || '未知平台'),
    roomName: entry.room_name.trim() || `房间 #${entry.listing_id}`,
    ownerName: entry.owner_username?.trim() || (entry.owner_user_id > 0 ? `用户 #${entry.owner_user_id}` : '历史号主'),
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
    state.error = '请选择 0-10 分'
    return
  }
  state.submitting = true
  state.error = ''
  try {
    const payload = {
      score: state.score,
      comment: state.comment.trim() || undefined
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
    name: listing.room_name?.trim() ? listing.room_name : `${ACCOUNT_NAME_BASE_BY_PLATFORM[listingPlatform(listing)]}${listing.id}`,
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
    anthropic_7d_limit_percent: anthropic7dLimitPercent(listing)
  } satisfies CreateFormState)
  editAllowedModels.value = Array.isArray(listing.allowed_models) ? [...listing.allowed_models] : []
}

function stopEditSessionRenewal(): void {
  if (editSessionRenewTimer != null) {
    window.clearInterval(editSessionRenewTimer)
    editSessionRenewTimer = null
  }
  editSessionRenewController?.abort()
  editSessionRenewController = null
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
  const generation = editSessionGeneration
  editSessionRenewController?.abort()
  const controller = new AbortController()
  editSessionRenewController = controller
  try {
    const payload = {
      session_id: sessionID,
      ...(editForceActive.value && authStore.isAdmin ? { force: true } : {})
    }
    const updated = await accountShareAPI.beginListingEdit(
      listing.id,
      payload,
      `account-share-edit-renew-${listing.id}-${createSecureRequestID()}`,
      { signal: controller.signal }
    )
    if (
      generation !== editSessionGeneration
      || editingConfigListing.value?.id !== listing.id
      || editSessionID.value !== sessionID
    ) {
      return
    }
    mergeListingUpdate(updated)
    editSessionID.value = updated.edit_session_id || sessionID
  } catch (error: unknown) {
    if (
      controller.signal.aborted
      || generation !== editSessionGeneration
      || editingConfigListing.value?.id !== listing.id
      || editSessionID.value !== sessionID
      || isCanceledRequest(error)
    ) {
      return
    }
    stopEditSessionRenewal()
    editErrorMessage.value = extractApiErrorMessage(error, '编辑会话续期失败，请关闭后重新编辑')
  } finally {
    if (editSessionRenewController === controller) {
      editSessionRenewController = null
    }
  }
}

async function releaseConfigEditSession(showError = false): Promise<boolean> {
  const listing = editingConfigListing.value
  const sessionID = editSessionID.value
  if (!listing || !sessionID) return true
  try {
    const idempotencyKey = getStableIdempotencyKey(
      releaseEditIntent,
      `account-share-edit-release-${listing.id}`,
      { listingID: listing.id, sessionID }
    )
    const updated = await accountShareAPI.releaseListingEdit(listing.id, sessionID, idempotencyKey)
    clearStableIdempotencyIntent(releaseEditIntent)
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
  editSessionGeneration += 1
  clearStableIdempotencyIntent(beginEditIntent)
  clearStableIdempotencyIntent(releaseEditIntent)
  clearStableIdempotencyIntent(updateListingIntent)
  showConfigEditDialog.value = false
  editingConfigListing.value = null
  editAllowedModels.value = []
  editSessionID.value = ''
  editForceActive.value = false
  editConsumerProtected.value = false
  editReason.value = ''
  editErrorMessage.value = ''
  editVersionConflict.value = false
  releasingConfigEdit.value = false
  configDraftBaseline.value = null
  Object.assign(editForm, buildDefaultCreateForm())
}

async function closeConfigEditDialog(discardConfirmed = false): Promise<void> {
  if (savingConfigEdit.value || releasingConfigEdit.value) return
  if (!discardConfirmed && configDraftHasChanges()) {
    pendingDraftDiscardTarget.value = 'config'
    return
  }
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

function isForceEditRequiredError(error: unknown): boolean {
  const code = extractApiErrorCode(error)
  return code === 'ACCOUNT_SHARE_ROOM_UPDATE_REQUIRES_PAUSED'
    || code === 'ACCOUNT_SHARE_LISTING_IN_USE'
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

function ownerEditBlockedMessage(state: AccountShareRoomManagementState | null): string {
  const blockers = state ? roomEditBlockerLabels(state) : []
  const detail = blockers.length > 0 ? ` 当前阻塞项：${blockers.join('、')}。` : ''
  return `房主只能编辑没有正在使用、排队、结束中、请求中、结算中或其他编辑会话的上架或已暂停房间。${detail}请先处理阻塞项后重试。`
}

async function openConfigEditDialog(
  listing: AccountShareListing,
  force: boolean,
  forceReason = ''
): Promise<void> {
  if (force && !authStore.isAdmin) {
    showActionError('只有管理员可以强制编辑房间配置。', '无权强制编辑')
    return
  }
  if (listing.deleted) {
    showActionError('已删除房间只能查看历史快照，不能再编辑配置。', '房间已删除')
    return
  }
  errorMessage.value = ''
  editErrorMessage.value = ''
  editVersionConflict.value = false
  managedActionId.value = listing.id
  try {
    await loadListingNameIndex(false)
    const editSessionPayload = {
      session_id: listing.editing_mine ? listing.edit_session_id : undefined,
      ...(force ? { force: true } : {})
    }
    const idempotencyKey = getStableIdempotencyKey(
      beginEditIntent,
      `account-share-edit-begin-${listing.id}`,
      { listingID: listing.id, payload: editSessionPayload }
    )
    const updated = await accountShareAPI.beginListingEdit(
      listing.id,
      editSessionPayload,
      idempotencyKey
    )
    clearStableIdempotencyIntent(beginEditIntent)
    if (!updated.edit_session_id) {
      throw new Error('服务端未返回编辑会话，请刷新后重试')
    }
    if (!Number.isSafeInteger(Number(updated.row_version)) || Number(updated.row_version) <= 0) {
      throw new Error('服务端未返回有效的房间版本，请刷新后重试')
    }
    mergeListingUpdate(updated)
    editingConfigListing.value = updated
    editSessionID.value = updated.edit_session_id
    editForceActive.value = force
    editReason.value = force ? forceReason.trim() : ''
    populateEditForm(updated)
    editSessionGeneration += 1
    captureConfigDraftBaseline()
    showConfigEditDialog.value = true
    startEditSessionRenewal()
  } catch (error: unknown) {
    if (!force && isForceEditRequiredError(error)) {
      if (authStore.isAdmin) {
        prepareForceEdit(listing)
      } else {
        showActionError(ownerEditBlockedMessage(null), '房间暂时不能编辑')
      }
      return
    }
    showActionError(extractApiErrorMessage(error, '打开编辑配置失败'), '打开编辑配置失败')
  } finally {
    managedActionId.value = null
  }
}

async function openConsumerProtectedEditDialog(listing: AccountShareListing): Promise<void> {
  await loadListingNameIndex(false)
  editingConfigListing.value = listing
  editSessionID.value = ''
  editForceActive.value = false
  editConsumerProtected.value = true
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
    showActionError('已删除房间只能查看历史快照，不能再编辑配置。', '房间已删除')
    return
  }
  if (listingEditLockedByOther(listing)) {
    showActionError(listingEditLockLabel(listing), '暂时不能编辑')
    return
  }

  managedActionId.value = listing.id
  try {
    const state = await accountShareAPI.getRoomManagementState(listing.id)
    if (state.listing_id !== listing.id) {
      throw new Error('服务端返回了不匹配的房间管理状态，请刷新后重试')
    }
    if (state.blockers.runtime_dependency_unavailable) {
      showActionError(
        '当前无法确认房间内是否仍有运行中请求，请等待运行时状态恢复后再编辑。',
        '暂时不能安全编辑'
      )
      return
    }
    const currentListing: AccountShareListing = {
      ...listing,
      row_version: state.row_version,
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
    showActionError(extractApiErrorMessage(error, '读取房间实时状态失败，请稍后重试'), '打开编辑配置失败')
  } finally {
    if (managedActionId.value === listing.id) managedActionId.value = null
  }
}

function cancelForceEdit(): void {
  clearStableIdempotencyIntent(beginEditIntent)
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
    if (!editConsumerProtected.value) {
      payload.edit_session_id = editSessionID.value
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
    stopEditSessionRenewal()
    mergeListingUpdate(updated)
    await loadListings()
    appStore.showSuccess('房间配置已更新')
    resetConfigEditState()
  } catch (error: unknown) {
    if (extractApiErrorCode(error) === 'ACCOUNT_SHARE_ROOM_VERSION_CONFLICT') {
      editVersionConflict.value = true
      editErrorMessage.value = '房间配置已被更新，请刷新后重新编辑'
      stopEditSessionRenewal()
    } else {
      editErrorMessage.value = extractApiErrorMessage(error, '保存房间配置失败', {
        ACCOUNT_SHARE_ROOM_UPDATE_REASON_REQUIRED: '请填写本次房间配置修改原因',
        ACCOUNT_SHARE_ROOM_FORCE_REASON_REQUIRED: '管理员强制修改原因不能为空',
        ACCOUNT_SHARE_ROOM_FORCE_CONFIRMATION_REQUIRED: '管理员强制修改必须完成明确确认',
        ACCOUNT_SHARE_CONSUMER_PROTECTION_VIOLATION: '当前房间已有消费者，只能降低费用、提高单用户并发、增加模型，或在不影响现有席位的前提下减少席位'
      })
    }
  } finally {
    savingConfigEdit.value = false
  }
}

async function reloadConfigEditAfterConflict(): Promise<void> {
  const listingID = editingConfigListing.value?.id
  if (!listingID || savingConfigEdit.value || releasingConfigEdit.value) return
  stopEditSessionRenewal()
  releasingConfigEdit.value = true
  const released = await releaseConfigEditSession(false)
  releasingConfigEdit.value = false
  if (!released) {
    editErrorMessage.value = '释放旧编辑会话失败，请稍后重试'
    return
  }
  resetConfigEditState()
  await loadListings()
  const refreshed = listings.value.find((item) => item.id === listingID)
    || knownListings.value.find((item) => item.id === listingID)
  if (!refreshed) {
    showActionError('房间已不存在或当前列表无法访问，请刷新页面后重试。', '无法重新编辑')
    return
  }
  requestOpenConfigEdit(refreshed)
}

function copyModelName(model: string): void {
  void copyToClipboard(model, `已复制 ${model}`)
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
    abortRecommendationAsyncRequests()
    resetRecommendationResult()
  },
  { flush: 'sync' }
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
    const initializationTasks: Promise<unknown>[] = [
      loadCurrentView(),
      loadModeKeys(),
      loadProxies(),
      loadListingNameIndex(),
      loadSelfUseCommissionRate(),
      loadCapabilities()
    ]
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
  abortMembershipHistoryRequest()
  abortMySpendAccountsRequest()
  abortMySpendRequest()
  abortRecommendationAsyncRequests()
  abortOwnerDialogRequests()
  roomLifecycleStateRequestSeq += 1
  roomLifecycleStateController?.abort()
  roomLifecycleStateController = null
  stopRoomLifecycleOperationPolling()
  roomLifecycleListing.value = null
  roomLifecycleState.value = null
  roomLifecycleOperation.value = null
  modeKeysRequestSeq += 1
  keyResolutionRequestSeq += 1
  membershipEndOperationRequestSeq += 1
  for (const controller of membershipEndOperationControllers.values()) {
    controller.abort()
  }
  membershipEndOperationControllers.clear()
  stopEditSessionRenewal()
  void releaseConfigEditSession()
})
</script>

<style scoped>
.room-lifecycle-dialog {
  display: grid;
  min-width: 0;
  gap: 1rem;
}

.room-lifecycle-state-message,
.room-lifecycle-alert,
.room-lifecycle-operation {
  display: flex;
  min-width: 0;
  gap: 0.75rem;
  border: 1px solid rgb(226 232 240);
  border-radius: 0.75rem;
  padding: 0.875rem;
  font-size: 0.875rem;
  line-height: 1.5;
}

.room-lifecycle-state-message {
  align-items: center;
  color: rgb(71 85 105);
  background: rgb(248 250 252);
}

.room-lifecycle-alert strong,
.room-lifecycle-operation strong {
  display: block;
  color: rgb(15 23 42);
}

.room-lifecycle-alert p,
.room-lifecycle-operation p {
  margin-top: 0.25rem;
}

.room-lifecycle-alert code,
.room-lifecycle-operation code {
  display: block;
  margin-top: 0.375rem;
  overflow-wrap: anywhere;
  color: currentColor;
  font-size: 0.75rem;
}

.room-lifecycle-alert-danger {
  border-color: rgb(254 202 202);
  color: rgb(185 28 28);
  background: rgb(254 242 242);
}

.room-lifecycle-alert-warning {
  border-color: rgb(253 230 138);
  color: rgb(146 64 14);
  background: rgb(255 251 235);
}

.room-lifecycle-alert-success {
  border-color: rgb(167 243 208);
  color: rgb(4 120 87);
  background: rgb(236 253 245);
}

.room-lifecycle-overview,
.room-lifecycle-confirm-panel {
  min-width: 0;
  border: 1px solid rgb(226 232 240);
  border-radius: 0.875rem;
  padding: 1rem;
  background: rgb(255 255 255);
}

.room-lifecycle-overview-head {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 0.75rem;
}

.room-lifecycle-eyebrow {
  display: block;
  color: rgb(100 116 139);
  font-size: 0.75rem;
  font-weight: 700;
  letter-spacing: 0.06em;
  text-transform: uppercase;
}

.room-lifecycle-version {
  flex-shrink: 0;
  border-radius: 9999px;
  padding: 0.3rem 0.625rem;
  color: rgb(71 85 105);
  background: rgb(241 245 249);
  font-size: 0.75rem;
  font-weight: 600;
}

.room-lifecycle-status-reason {
  margin-top: 0.75rem;
  color: rgb(71 85 105);
  font-size: 0.875rem;
  line-height: 1.5;
}

.room-lifecycle-metrics {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 0.625rem;
  margin-top: 1rem;
}

.room-lifecycle-metrics > div {
  min-width: 0;
  border-radius: 0.625rem;
  padding: 0.75rem;
  background: rgb(248 250 252);
}

.room-lifecycle-metrics span,
.room-lifecycle-metrics strong {
  display: block;
}

.room-lifecycle-metrics span {
  color: rgb(100 116 139);
  font-size: 0.75rem;
}

.room-lifecycle-metrics strong {
  margin-top: 0.25rem;
  color: rgb(15 23 42);
  font-size: 1rem;
}

.room-lifecycle-operation {
  color: rgb(30 64 175);
  background: rgb(239 246 255);
  border-color: rgb(191 219 254);
}

.room-lifecycle-action-grid {
  display: grid;
  grid-template-columns: minmax(0, 1fr);
  gap: 0.625rem;
}

.room-lifecycle-action-card {
  display: flex;
  min-height: 3.25rem;
  min-width: 0;
  align-items: flex-start;
  gap: 0.75rem;
  border: 1px solid rgb(226 232 240);
  border-radius: 0.75rem;
  padding: 0.875rem;
  color: rgb(51 65 85);
  background: rgb(255 255 255);
  text-align: left;
  transition: border-color 160ms ease, background-color 160ms ease, transform 160ms ease;
}

.room-lifecycle-action-card:hover {
  border-color: rgb(129 140 248);
  background: rgb(248 250 252);
  transform: translateY(-1px);
}

.room-lifecycle-action-card:focus-visible {
  outline: 2px solid rgb(99 102 241 / 0.55);
  outline-offset: 2px;
}

.room-lifecycle-action-card > span {
  min-width: 0;
}

.room-lifecycle-action-card strong,
.room-lifecycle-action-card small {
  display: block;
}

.room-lifecycle-action-card strong {
  color: rgb(15 23 42);
  font-size: 0.875rem;
}

.room-lifecycle-action-card small {
  margin-top: 0.2rem;
  color: rgb(100 116 139);
  font-size: 0.75rem;
  line-height: 1.45;
}

.room-lifecycle-action-card-danger {
  border-color: rgb(254 202 202);
  color: rgb(220 38 38);
}

.room-lifecycle-action-card-danger:hover {
  border-color: rgb(248 113 113);
  background: rgb(254 242 242);
}

.room-lifecycle-confirm-panel h4 {
  margin-top: 0.375rem;
  color: rgb(15 23 42);
  font-size: 1rem;
  font-weight: 700;
}

.room-lifecycle-confirm-panel > p {
  margin-top: 0.5rem;
  color: rgb(71 85 105);
  font-size: 0.875rem;
  line-height: 1.6;
}

.room-lifecycle-confirm-panel > .room-lifecycle-alert,
.room-lifecycle-confirm-panel > .field,
.room-lifecycle-confirm-panel > .room-lifecycle-state-message {
  margin-top: 1rem;
}

.room-lifecycle-blocker-list {
  display: grid;
  gap: 0.5rem;
  margin-top: 0.875rem;
}

.room-lifecycle-blocker-list li {
  display: flex;
  min-width: 0;
  align-items: center;
  justify-content: space-between;
  gap: 0.75rem;
  border-radius: 0.625rem;
  padding: 0.625rem 0.75rem;
  color: rgb(71 85 105);
  background: rgb(248 250 252);
  font-size: 0.875rem;
}

.room-lifecycle-blocker-list strong {
  overflow-wrap: anywhere;
  color: rgb(185 28 28);
  text-align: right;
}

.room-lifecycle-muted-note {
  color: rgb(100 116 139);
  font-size: 0.875rem;
  line-height: 1.5;
}

.room-lifecycle-footer {
  display: grid;
  width: 100%;
  grid-template-columns: minmax(0, 1fr);
  gap: 0.625rem;
}

.room-lifecycle-footer > button {
  width: 100%;
  min-width: 0;
  white-space: nowrap;
}

.dark .room-lifecycle-state-message,
.dark .room-lifecycle-overview,
.dark .room-lifecycle-confirm-panel,
.dark .room-lifecycle-action-card {
  border-color: rgb(51 65 85);
  background: rgb(15 23 42);
}

.dark .room-lifecycle-state-message,
.dark .room-lifecycle-status-reason,
.dark .room-lifecycle-confirm-panel > p,
.dark .room-lifecycle-action-card,
.dark .room-lifecycle-action-card small,
.dark .room-lifecycle-muted-note {
  color: rgb(148 163 184);
}

.dark .room-lifecycle-alert strong,
.dark .room-lifecycle-operation strong,
.dark .room-lifecycle-metrics strong,
.dark .room-lifecycle-action-card strong,
.dark .room-lifecycle-confirm-panel h4 {
  color: rgb(248 250 252);
}

.dark .room-lifecycle-version,
.dark .room-lifecycle-metrics > div,
.dark .room-lifecycle-blocker-list li {
  color: rgb(148 163 184);
  background: rgb(30 41 59);
}

.dark .room-lifecycle-alert-danger {
  border-color: rgb(127 29 29);
  color: rgb(254 202 202);
  background: rgb(127 29 29 / 0.25);
}

.dark .room-lifecycle-alert-warning {
  border-color: rgb(120 53 15);
  color: rgb(253 230 138);
  background: rgb(120 53 15 / 0.24);
}

.dark .room-lifecycle-alert-success {
  border-color: rgb(6 78 59);
  color: rgb(167 243 208);
  background: rgb(6 78 59 / 0.28);
}

.dark .room-lifecycle-operation {
  border-color: rgb(30 64 175);
  color: rgb(191 219 254);
  background: rgb(30 58 138 / 0.25);
}

.dark .room-lifecycle-action-card:hover {
  border-color: rgb(99 102 241);
  background: rgb(30 41 59);
}

.dark .room-lifecycle-action-card-danger {
  border-color: rgb(127 29 29);
  color: rgb(248 113 113);
}

.dark .room-lifecycle-action-card-danger:hover {
  border-color: rgb(239 68 68);
  background: rgb(127 29 29 / 0.22);
}

@media (min-width: 640px) {
  .room-lifecycle-metrics,
  .room-lifecycle-action-grid {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }

  .room-lifecycle-footer {
    display: flex;
    justify-content: flex-end;
  }

  .room-lifecycle-footer > button {
    width: auto;
    min-width: 6.5rem;
    flex: 0 0 auto;
  }
}

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

.account-share-capability-strip {
  position: relative;
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 0.5rem 1rem;
  border-bottom: 1px solid rgb(226 232 240);
  background: rgb(248 250 252 / 0.88);
  padding: 0.75rem 1.125rem;
  color: rgb(71 85 105);
  font-size: 0.75rem;
}

.account-share-capability-strip > span {
  display: inline-flex;
  align-items: baseline;
  gap: 0.25rem;
  white-space: nowrap;
}

.account-share-capability-strip strong {
  color: rgb(15 23 42);
  font-size: 0.875rem;
}

.account-share-capability-strip small {
  flex: 1 1 18rem;
  min-width: 0;
  color: rgb(100 116 139);
}

.account-share-capability-strip-blocked {
  border-bottom-color: rgb(254 202 202);
  background: rgb(254 242 242 / 0.92);
}

.account-share-capability-strip-blocked small {
  color: rgb(185 28 28);
  font-weight: 700;
}

.create-capability-summary {
  display: flex;
  flex-wrap: wrap;
  gap: 0.5rem 1rem;
  border-bottom: 1px solid rgb(186 230 253);
  background: rgb(240 249 255);
  padding: 0.75rem 1rem;
  color: rgb(3 105 161);
  font-size: 0.75rem;
}

.create-capability-summary strong {
  flex-basis: 100%;
  color: rgb(185 28 28);
}

.create-capability-summary-blocked {
  border-bottom-color: rgb(254 202 202);
  background: rgb(254 242 242);
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

.account-share-admin-quota-button,
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

.account-share-admin-quota-button:hover,
.account-share-guide-button:hover,
.account-share-spend-button:hover {
  border-color: rgb(96 165 250);
  background: rgb(219 234 254);
  box-shadow: 0 8px 18px rgb(37 99 235 / 0.1);
}

.account-share-admin-quota-button {
  border-color: rgb(196 181 253);
  background: rgb(245 243 255);
  color: rgb(109 40 217);
}

.account-share-admin-quota-button:hover {
  border-color: rgb(139 92 246);
  background: rgb(237 233 254);
  color: rgb(91 33 182);
  box-shadow: 0 8px 18px rgb(124 58 237 / 0.12);
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

.dark .account-share-capability-strip {
  border-bottom-color: rgb(51 65 85);
  background: rgb(15 23 42 / 0.72);
  color: rgb(148 163 184);
}

.dark .account-share-capability-strip strong {
  color: rgb(241 245 249);
}

.dark .account-share-capability-strip-blocked {
  border-bottom-color: rgb(127 29 29);
  background: rgb(69 10 10 / 0.3);
}

.dark .account-share-capability-strip-blocked small {
  color: rgb(252 165 165);
}

.dark .create-capability-summary {
  border-bottom-color: rgb(12 74 110);
  background: rgb(8 47 73 / 0.45);
  color: rgb(125 211 252);
}

.dark .create-capability-summary-blocked {
  border-bottom-color: rgb(127 29 29);
  background: rgb(69 10 10 / 0.32);
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

.dark .account-share-admin-quota-button,
.dark .account-share-guide-button,
.dark .account-share-spend-button {
  border-color: rgb(59 130 246 / 0.38);
  background: rgb(30 64 175 / 0.22);
  color: rgb(191 219 254);
}

.dark .account-share-admin-quota-button:hover,
.dark .account-share-guide-button:hover,
.dark .account-share-spend-button:hover {
  border-color: rgb(96 165 250 / 0.7);
  background: rgb(30 64 175 / 0.34);
  box-shadow: 0 8px 18px rgb(0 0 0 / 0.2);
}

.dark .account-share-admin-quota-button {
  border-color: rgb(139 92 246 / 0.42);
  background: rgb(76 29 149 / 0.22);
  color: rgb(221 214 254);
}

.dark .account-share-admin-quota-button:hover {
  border-color: rgb(167 139 250 / 0.7);
  background: rgb(76 29 149 / 0.36);
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

  .account-share-admin-quota-button,
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

.mode-key-select {
  min-width: 0;
}

.mode-key-select :deep(.select-trigger) {
  min-height: 2.75rem;
  border-radius: 0.625rem;
  border-color: rgb(191 219 254);
  background: linear-gradient(180deg, rgb(255 255 255), rgb(248 250 252));
  padding: 0.5rem 0.625rem;
  box-shadow: 0 1px 2px rgb(15 23 42 / 0.04);
}

.mode-key-select :deep(.select-trigger:hover) {
  border-color: rgb(96 165 250);
}

.mode-key-select-value {
  display: flex;
  min-width: 0;
  align-items: center;
  gap: 0.4375rem;
  color: rgb(30 64 175);
  font-size: 0.8125rem;
  font-weight: 700;
}

.mode-key-select-value span {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.mode-key-option-icon {
  display: inline-flex;
  width: 1.75rem;
  height: 1.75rem;
  flex: 0 0 auto;
  align-items: center;
  justify-content: center;
  border-radius: 0.5rem;
  background: rgb(239 246 255);
  color: rgb(37 99 235);
}

.mode-key-option-copy {
  display: flex;
  min-width: 0;
  flex: 1;
  flex-direction: column;
  gap: 0.0625rem;
  text-align: left;
}

.mode-key-option-copy strong,
.mode-key-option-copy small {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.mode-key-option-copy strong {
  color: rgb(30 41 59);
  font-size: 0.8125rem;
  font-weight: 700;
}

.mode-key-option-copy small {
  color: rgb(100 116 139);
  font-size: 0.6875rem;
  line-height: 1rem;
}

.dark .mode-key-select :deep(.select-trigger) {
  border-color: rgb(59 130 246 / 0.4);
  background: linear-gradient(180deg, rgb(30 41 59), rgb(15 23 42));
}

.dark .mode-key-select-value {
  color: rgb(191 219 254);
}

.dark .mode-key-option-icon {
  background: rgb(30 64 175 / 0.24);
  color: rgb(147 197 253);
}

.dark .mode-key-option-copy strong {
  color: rgb(241 245 249);
}

.dark .mode-key-option-copy small {
  color: rgb(148 163 184);
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
  display: inline-flex;
  min-width: 2.75rem;
  min-height: 2.75rem;
  align-items: center;
  justify-content: center;
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
  min-height: 2.75rem;
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
  min-height: 2.75rem;
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

  .filter-body-head {
    flex-direction: row;
    align-items: center;
    justify-content: space-between;
  }
}

@media (min-width: 1280px) {
  .advanced-filter-grid {
    grid-template-columns: repeat(5, minmax(0, 1fr));
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

.create-room-source-stage,
.create-room-form-flow,
.create-room-submit-stage {
  padding: 1rem;
}

.create-room-source-stage {
  border-bottom: 1px solid rgb(226 232 240);
  background: rgb(255 255 255);
}

.create-room-stage-heading {
  display: flex;
  min-width: 0;
  align-items: flex-start;
  gap: 0.75rem;
}

.create-room-stage-heading > div {
  min-width: 0;
}

.create-room-stage-heading strong,
.create-room-stage-heading > div > span,
.create-room-stage-heading small {
  display: block;
}

.create-room-stage-heading strong,
.create-room-stage-heading > div > span {
  color: rgb(15 23 42);
  font-size: 0.9375rem;
  font-weight: 700;
  line-height: 1.4;
}

.create-room-stage-heading small {
  margin-top: 0.1875rem;
  color: rgb(100 116 139);
  font-size: 0.8125rem;
  line-height: 1.45;
}

.create-room-stage-index {
  display: inline-flex;
  width: 1.75rem;
  height: 1.75rem;
  flex: 0 0 1.75rem;
  align-items: center;
  justify-content: center;
  border-radius: 9999px;
  background: rgb(239 246 255);
  color: rgb(37 99 235);
  font-size: 0.75rem;
  font-weight: 800;
}

.create-room-source-grid {
  display: grid;
  grid-template-columns: 1fr;
  gap: 0.625rem;
  margin-top: 1rem;
}

.create-room-account-picker {
  margin-top: 0.75rem;
  border: 1px solid rgb(191 219 254);
  border-radius: 0.75rem;
  background: rgb(239 246 255 / 0.65);
  padding: 0.875rem;
}

.create-room-workspace {
  width: 100%;
}

.create-room-form-flow {
  display: grid;
  gap: 0.875rem;
}

.create-room-stage-card.form-section {
  margin: 0;
  border-radius: 0.875rem;
  background: rgb(255 255 255);
  padding: 1rem;
  box-shadow: 0 1px 2px rgb(15 23 42 / 0.03);
}

.create-room-stage-card .section-heading {
  margin-bottom: 1rem;
}

.create-room-field-grid {
  display: grid;
  grid-template-columns: minmax(0, 1fr);
  gap: 1rem;
}

.create-room-submit-stage {
  border-top: 1px solid rgb(226 232 240);
  background: rgb(255 255 255);
}

.create-room-submit-content {
  display: grid;
  gap: 1rem;
}

.create-room-submit-button {
  min-height: 3rem;
  width: 100%;
}

.dark .create-room-source-stage,
.dark .create-room-submit-stage,
.dark .create-room-stage-card.form-section {
  border-color: rgb(63 63 70);
  background: rgb(24 24 27);
}

.dark .create-room-account-picker {
  border-color: rgb(30 64 175);
  background: rgb(30 64 175 / 0.14);
}

.dark .create-room-stage-heading strong,
.dark .create-room-stage-heading > div > span {
  color: rgb(244 244 245);
}

.dark .create-room-stage-heading small {
  color: rgb(161 161 170);
}

.dark .create-room-stage-index {
  background: rgb(30 64 175 / 0.3);
  color: rgb(147 197 253);
}

@media (min-width: 640px) {
  .create-room-source-stage,
  .create-room-form-flow,
  .create-room-submit-stage {
    padding: 1.25rem 1.5rem;
  }

  .create-room-source-grid,
  .create-room-field-grid {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }

  .create-room-stage-card.form-section {
    padding: 1.25rem;
  }
}

@media (min-width: 1024px) {
  .create-room-field-grid {
    grid-template-columns: repeat(3, minmax(0, 1fr));
  }

  .create-room-submit-content {
    grid-template-columns: minmax(0, 1fr) minmax(18rem, 24rem);
    align-items: center;
  }

  .create-room-submit-content > :not(.create-room-stage-heading):not(.create-room-submit-button) {
    grid-column: 1 / -1;
  }
}

.create-source-option {
  display: flex;
  min-height: 5rem;
  min-width: 0;
  align-items: center;
  gap: 0.75rem;
  border-radius: 0.75rem;
  border: 1px solid rgb(226 232 240);
  background: rgb(255 255 255);
  padding: 0.875rem;
  text-align: left;
  transition: border-color 0.15s ease, background-color 0.15s ease, box-shadow 0.15s ease;
}

.create-source-option:hover {
  border-color: rgb(147 197 253);
  background: rgb(248 250 252);
}

.create-source-option:disabled {
  cursor: not-allowed;
  opacity: 0.6;
}

.create-source-option:focus-visible {
  outline: 3px solid rgb(147 197 253 / 0.75);
  outline-offset: 2px;
}

.create-source-option strong,
.create-source-option small {
  display: block;
}

.create-source-option strong {
  color: rgb(15 23 42);
  font-size: 0.875rem;
}

.create-source-option small {
  margin-top: 0.1875rem;
  color: rgb(100 116 139);
  font-size: 0.75rem;
  line-height: 1.125rem;
}

.create-source-option-active {
  border-color: rgb(37 99 235);
  background: rgb(239 246 255);
  box-shadow: inset 0 0 0 1px rgb(37 99 235 / 0.12);
}

.create-source-icon {
  display: inline-flex;
  height: 2.5rem;
  width: 2.5rem;
  flex-shrink: 0;
  align-items: center;
  justify-content: center;
  border-radius: 0.625rem;
  background: rgb(241 245 249);
  color: rgb(51 65 85);
}

.create-source-option-active .create-source-icon {
  background: rgb(37 99 235);
  color: white;
}

.dark .create-source-option {
  border-color: rgb(63 63 70);
  background: rgb(39 39 42);
}

.dark .create-source-option:hover {
  border-color: rgb(59 130 246);
  background: rgb(39 39 42 / 0.76);
}

.dark .create-source-option-active {
  border-color: rgb(96 165 250);
  background: rgb(30 64 175 / 0.2);
}

.dark .create-source-option strong {
  color: white;
}

.dark .create-source-option small {
  color: rgb(161 161 170);
}

.dark .create-source-icon {
  background: rgb(63 63 70);
  color: rgb(212 212 216);
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
  border: 1px solid rgb(226 232 240);
  border-radius: 0.625rem;
  background: linear-gradient(135deg, rgb(248 250 252 / 0.96), rgb(255 255 255 / 0.92));
  padding: 0.625rem 0.6875rem;
}

.listing-card-main-row {
  display: flex;
  min-width: 0;
  flex-direction: column;
  gap: 0.5rem;
}

.listing-card-identity {
  display: flex;
  min-width: 0;
  flex: 1;
  align-items: center;
}

.listing-badge-row {
  display: flex;
  min-width: 0;
  flex-wrap: wrap;
  align-items: center;
  gap: 0.3125rem;
}

.listing-title-row {
  display: flex;
  min-width: 0;
  flex-direction: column;
  gap: 0.4375rem;
}

.listing-title-line {
  display: flex;
  min-width: 0;
  flex-wrap: wrap;
  align-items: center;
  gap: 0.375rem 0.625rem;
}

.listing-title {
  color: rgb(17 24 39);
  font-size: 1rem;
  font-weight: 800;
  line-height: 1.3125rem;
  overflow-wrap: anywhere;
}

.listing-owner {
  display: flex;
  min-width: 0;
  flex-wrap: wrap;
  align-items: center;
  gap: 0.375rem;
  color: rgb(107 114 128);
  font-size: 0.78125rem;
  font-weight: 600;
  line-height: 1rem;
  overflow-wrap: anywhere;
}

.owner-inline-button {
  display: inline-flex;
  min-height: 2rem;
  align-items: center;
  justify-content: center;
  gap: 0.25rem;
  border-radius: 9999px;
  border: 1px solid rgb(191 219 254);
  background: rgb(239 246 255);
  padding: 0.25rem 0.5625rem;
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

.listing-member-limit {
  min-width: 0;
  flex: 0 0 auto;
}

@media (min-width: 768px) {
  .listing-card {
    padding: 0.75rem 0.8125rem 0.8125rem;
  }
}

@container account-listing-card (min-width: 38rem) {
  .listing-card-main-row {
    flex-direction: row;
    align-items: center;
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

.dark .listing-card-head {
  border-color: rgb(63 63 70);
  background: linear-gradient(135deg, rgb(39 39 42 / 0.72), rgb(24 24 27 / 0.9));
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

.listing-status-stack {
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

.listing-runtime-summary {
  align-items: center;
}

.listing-runtime-summary-content {
  display: flex;
  min-width: 0;
  flex-wrap: wrap;
  align-items: center;
  gap: 0.375rem;
  color: rgb(75 85 99);
  font-size: 0.75rem;
  line-height: 1rem;
}

.listing-runtime-summary-content > strong {
  color: rgb(17 24 39);
  font-size: 0.8125rem;
  font-weight: 800;
}

.listing-runtime-summary-divider {
  width: 1px;
  height: 0.875rem;
  background: rgb(209 213 219);
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

.listing-combined-availability {
  display: contents;
}

.availability-progress-row {
  display: grid;
  min-width: 0;
  grid-template-columns: auto minmax(2.5rem, 1fr) auto;
  align-items: center;
  gap: 0.4375rem;
  border-radius: 0.5rem;
  background: white;
  padding: 0.5rem;
}

.combined-availability-head {
  display: contents;
  font-size: 0.75rem;
  color: rgb(75 85 99);
}

.combined-availability-head span {
  grid-column: 1;
  white-space: nowrap;
}

.combined-availability-head strong {
  grid-column: 3;
  color: rgb(17 24 39);
  font-size: 0.875rem;
  font-weight: 800;
}

.combined-availability-track {
  grid-column: 2;
  grid-row: 1;
  height: 0.4375rem;
  overflow: hidden;
  border-radius: 999px;
  background: rgb(229 231 235);
}

.combined-availability-fill {
  display: block;
  height: 100%;
  border-radius: inherit;
  transition: width 300ms ease;
}

.combined-availability-fill-normal {
  background: rgb(16 185 129);
}

.combined-availability-fill-warning {
  background: rgb(245 158 11);
}

.combined-availability-fill-danger {
  background: rgb(239 68 68);
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

.capacity-panel p {
  margin: 0;
  color: rgb(107 114 128);
  font-size: 0.6875rem;
  line-height: 1rem;
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
.dark .listing-runtime-tile p {
  color: rgb(161 161 170);
}

.dark .listing-runtime-tile,
.dark .availability-progress-row {
  background: rgb(39 39 42 / 0.45);
}

.dark .combined-availability-head {
  color: rgb(161 161 170);
}

.dark .combined-availability-track {
  background: rgb(63 63 70);
}

.dark .capacity-panel {
  background: rgb(39 39 42 / 0.45);
  color: rgb(161 161 170);
}

.dark .capacity-panel p {
  color: rgb(161 161 170);
}

.dark .listing-runtime-value-row strong,
.dark .combined-availability-head strong,
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

.membership-status-pill-ending {
  background: rgb(245 158 11);
  color: rgb(120 53 15);
}

.membership-status-pill-error {
  background: rgb(220 38 38);
  color: white;
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

.membership-ending-state {
  display: flex;
  min-width: 0;
  min-height: 4.5rem;
  align-items: flex-start;
  gap: 0.75rem;
  border: 1px solid rgb(253 230 138);
  border-radius: 0.875rem;
  background: rgb(255 251 235);
  padding: 0.875rem;
  color: rgb(146 64 14);
}

.membership-ending-state > svg {
  margin-top: 0.125rem;
  flex-shrink: 0;
}

.membership-ending-state strong,
.membership-ending-state span {
  display: block;
}

.membership-ending-state strong {
  font-size: 0.8125rem;
}

.membership-ending-state span {
  margin-top: 0.25rem;
  font-size: 0.75rem;
  line-height: 1.25rem;
}

.account-share-membership-panel-ending {
  border-color: rgb(253 230 138);
  background: linear-gradient(180deg, rgb(255 251 235), rgb(255 247 237));
}

.account-share-membership-panel-ending .membership-title,
.account-share-membership-panel-ending .membership-subtitle {
  color: rgb(146 64 14);
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

.listing-management-actions {
  margin-top: 0.75rem;
  display: grid;
  grid-template-columns: minmax(0, 1fr);
  gap: 0.5rem;
}

.listing-management-action {
  display: inline-flex;
  min-height: 2.5rem;
  width: 100%;
  align-items: center;
  justify-content: center;
  gap: 0.4375rem;
  border-radius: 0.5rem;
  padding: 0.5rem 0.75rem;
  font-size: 0.75rem;
  font-weight: 700;
  line-height: 1rem;
  white-space: nowrap;
}

.listing-management-action svg {
  flex: 0 0 auto;
}

@container account-listing-card (min-width: 28rem) {
  .listing-management-actions {
    display: flex;
    flex-wrap: wrap;
  }

  .listing-management-action {
    width: auto;
  }
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

.dark .membership-status-pill-ending {
  background: rgb(146 64 14);
  color: rgb(253 230 138);
}

.dark .membership-status-pill-error {
  background: rgb(153 27 27);
  color: rgb(254 202 202);
}

.dark .account-share-membership-panel-ending {
  border-color: rgb(180 83 9 / 0.5);
  background: linear-gradient(180deg, rgb(120 53 15 / 0.22), rgb(69 26 3 / 0.18));
  color: rgb(253 230 138);
}

.dark .account-share-membership-panel-ending .membership-title,
.dark .account-share-membership-panel-ending .membership-subtitle {
  color: rgb(253 230 138);
}

.dark .membership-ending-state {
  border-color: rgb(120 53 15);
  background: rgb(69 26 3 / 0.28);
  color: rgb(253 186 116);
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

.my-spend-account-option:disabled {
  cursor: wait;
  opacity: 0.68;
}

.my-spend-account-option:disabled:hover {
  border-color: rgb(226 232 240);
  background: white;
  box-shadow: none;
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
  min-height: 2.75rem;
  border-radius: 0.375rem;
  padding: 0.5rem 0.625rem;
  color: rgb(71 85 105);
  font-size: 0.8125rem;
  font-weight: 800;
  transition: background-color 0.16s ease, color 0.16s ease, box-shadow 0.16s ease;
}

.my-spend-source-tabs {
  grid-template-columns: repeat(2, minmax(0, 1fr));
}

.my-spend-range-tabs button:hover {
  background: white;
  color: rgb(37 99 235);
}

.my-spend-range-tabs button:disabled {
  cursor: not-allowed;
  opacity: 0.5;
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

.dark .my-spend-account-option:disabled:hover {
  border-color: rgb(63 63 70);
  background: rgb(39 39 42 / 0.72);
  box-shadow: none;
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

.join-queue-consent {
  display: grid;
  min-height: 3.5rem;
  cursor: pointer;
  grid-template-columns: 1.25rem minmax(0, 1fr);
  align-items: flex-start;
  gap: 0.75rem;
  border: 1px solid rgb(191 219 254);
  border-radius: 0.625rem;
  background: rgb(239 246 255);
  padding: 0.875rem;
  color: rgb(30 64 175);
}

.join-queue-consent input {
  width: 1.125rem;
  height: 1.125rem;
  margin-top: 0.125rem;
  accent-color: rgb(37 99 235);
}

.join-queue-consent span,
.join-queue-consent strong,
.join-queue-consent small {
  display: block;
}

.join-queue-consent strong {
  font-size: 0.875rem;
  font-weight: 850;
  line-height: 1.25rem;
}

.join-queue-consent small {
  margin-top: 0.25rem;
  color: rgb(71 85 105);
  font-size: 0.75rem;
  font-weight: 650;
  line-height: 1.25rem;
}

.join-queue-consent-required {
  border-color: rgb(251 191 36);
  background: rgb(255 251 235);
  color: rgb(146 64 14);
  box-shadow: inset 3px 0 0 rgb(245 158 11);
}

.join-intent-state {
  display: flex;
  min-height: 2.75rem;
  align-items: center;
  gap: 0.625rem;
  border-radius: 0.5rem;
  border: 1px solid rgb(191 219 254);
  background: rgb(239 246 255);
  padding: 0.625rem 0.75rem;
  color: rgb(30 64 175);
  font-size: 0.8125rem;
  font-weight: 700;
  line-height: 1.25rem;
}

.join-intent-state-error {
  border-color: rgb(248 113 113 / 0.5);
  background: rgb(254 242 242);
  color: rgb(185 28 28);
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

.dark .join-queue-consent {
  border-color: rgb(59 130 246 / 0.42);
  background: rgb(30 58 138 / 0.22);
  color: rgb(191 219 254);
}

.dark .join-queue-consent small {
  color: rgb(203 213 225);
}

.dark .join-queue-consent-required {
  border-color: rgb(245 158 11 / 0.58);
  background: rgb(120 53 15 / 0.3);
  color: rgb(253 230 138);
}

.dark .join-intent-state {
  border-color: rgb(59 130 246 / 0.42);
  background: rgb(30 58 138 / 0.22);
  color: rgb(191 219 254);
}

.dark .join-intent-state-error {
  border-color: rgb(248 113 113 / 0.45);
  background: rgb(127 29 29 / 0.36);
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
  min-width: 0;
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
