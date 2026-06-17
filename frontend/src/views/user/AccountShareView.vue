<template>
  <AppLayout>
    <div class="mx-auto flex w-full max-w-[1680px] flex-col gap-5">
      <div class="lg:hidden">
        <h1 class="text-2xl font-semibold text-gray-950 dark:text-white">账号广场</h1>
        <p class="mt-1 text-sm text-gray-500 dark:text-dark-300">选择共享账号后，账号模式分组下的 API Key 将只调度当前绑定账号。</p>
      </div>

      <section class="account-share-hero">
        <div class="account-share-hero-head">
          <div class="flex min-w-0 items-start gap-3">
            <div class="hero-icon">
              <Icon name="users" size="lg" />
            </div>
            <div class="min-w-0">
              <h2 class="text-base font-semibold text-gray-950 dark:text-white">共享账号池</h2>
              <p class="mt-1 max-w-3xl text-sm leading-6 text-gray-500 dark:text-dark-300">
                OpenAI OAuth 账号会按账号模式上架，消费者加入后只绑定到自己的账号模式 API Key。
              </p>
            </div>
          </div>
          <div class="hero-actions">
            <button class="btn-secondary h-10" type="button" :disabled="loading" @click="loadListings">
              <Icon name="refresh" size="sm" class="mr-2" :class="{ 'animate-spin': loading }" />
              刷新
            </button>
            <button class="btn-primary h-10" type="button" @click="toggleCreatePanel">
              <Icon :name="showCreate ? 'chevronUp' : 'plus'" size="sm" class="mr-2" />
              {{ showCreate ? '收起新增' : '新增共享账号' }}
            </button>
          </div>
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
              <span>本页正在使用</span>
              <strong>{{ activeMembershipCount }}</strong>
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

      <section v-if="showCreate" class="overflow-hidden rounded-lg border border-gray-200 bg-white shadow-sm dark:border-dark-700 dark:bg-dark-900">
        <div class="flex flex-col gap-3 border-b border-gray-100 p-4 dark:border-dark-800 lg:flex-row lg:items-center lg:justify-between">
          <div>
            <h2 class="text-base font-semibold text-gray-950 dark:text-white">新增共享账号</h2>
            <p class="mt-1 text-sm text-gray-500 dark:text-dark-300">复用 OpenAI OAuth 授权流程，提交后自动创建账号并发布到账号广场。</p>
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
                <label class="field">
                  <span>账号名称</span>
                  <input v-model="createForm.name" class="input" placeholder="OpenAI共享账号" />
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
                <small>后端会强制账号模式、ctx_pool 和 Compact 配置，前端只提交可变策略。</small>
              </div>
              <div class="grid gap-4 xl:grid-cols-[minmax(0,1fr)_320px]">
                <div class="field">
                  <span>模型白名单</span>
                  <div class="model-selector-shell">
                    <ModelWhitelistSelector v-model="allowedModels" platform="openai" />
                  </div>
                  <small>复用“我的账号”新增账号的模型选择器，可搜索、多选并添加自定义模型。</small>
                </div>

                <div class="grid gap-3 sm:grid-cols-2 lg:grid-cols-1">
                  <label class="field">
                    <span>Codex 5h 保护 %</span>
                    <input v-model.number="createForm.codex_5h_limit_percent" class="input" type="number" min="1" max="100" step="1" />
                  </label>
                  <label class="field">
                    <span>Codex 7d 保护 %</span>
                    <input v-model.number="createForm.codex_7d_limit_percent" class="input" type="number" min="1" max="100" step="1" />
                  </label>
                </div>
              </div>

              <div v-if="concurrencyNotice" class="notice-row mt-3">
                <Icon name="infoCircle" size="sm" class="mt-0.5 flex-shrink-0" />
                <span>{{ concurrencyNotice }}</span>
              </div>

              <label class="toggle-row mt-3">
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
                platform="openai"
                :show-project-id="false"
                @generate-url="startOAuth"
              />

              <button class="btn-primary h-11 w-full" type="button" :disabled="creating || !canSubmitOAuth" @click="submitOAuth">
                <svg v-if="creating" class="-ml-1 mr-2 h-4 w-4 animate-spin" fill="none" viewBox="0 0 24 24">
                  <circle class="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="4"></circle>
                  <path class="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"></path>
                </svg>
                <Icon v-else name="checkCircle" size="sm" class="mr-2" />
                {{ creating ? '创建中...' : '完成 OAuth 并上架' }}
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

      <section class="filter-panel">
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
                  <strong>筛选条件</strong>
                  <small>{{ activeResultFilterCount > 0 ? `已启用 ${activeResultFilterCount} 项` : '默认展示全部可见账号' }}</small>
                </div>
              </div>
            </div>

            <div class="advanced-filter-grid" aria-label="账号广场高级筛选">
              <label class="filter-field">
                <span>状态</span>
                <select v-model="listingFilters.status" class="input h-11">
                  <option v-for="option in listingStatusFilterOptions" :key="option.value" :value="option.value">
                    {{ option.label }}
                  </option>
                </select>
              </label>

              <label class="filter-field">
                <span>账号等级</span>
                <select v-model="listingFilters.accountLevel" class="input h-11">
                  <option v-for="option in accountLevelFilterOptions" :key="option.value" :value="option.value">
                    {{ option.label }}
                  </option>
                </select>
              </label>

              <label class="filter-field filter-range-field">
                <span>单用户并发数</span>
                <div class="range-inputs">
                  <input v-model.trim="listingFilters.perUserConcurrency.min" class="input h-11" type="number" min="0" step="1" placeholder="最低" />
                  <span>至</span>
                  <input v-model.trim="listingFilters.perUserConcurrency.max" class="input h-11" type="number" min="0" step="1" placeholder="最高" />
                </div>
              </label>

              <label class="filter-field filter-range-field">
                <span>最低余额</span>
                <div class="range-inputs">
                  <input v-model.trim="listingFilters.minBalance.min" class="input h-11" type="number" min="0" step="0.01" placeholder="最低" />
                  <span>至</span>
                  <input v-model.trim="listingFilters.minBalance.max" class="input h-11" type="number" min="0" step="0.01" placeholder="最高" />
                </div>
              </label>

              <label class="filter-field filter-range-field">
                <span>小时费</span>
                <div class="range-inputs">
                  <input v-model.trim="listingFilters.hourlyRate.min" class="input h-11" type="number" min="0" step="0.0001" placeholder="最低" />
                  <span>至</span>
                  <input v-model.trim="listingFilters.hourlyRate.max" class="input h-11" type="number" min="0" step="0.0001" placeholder="最高" />
                </div>
              </label>

              <label class="filter-field filter-range-field">
                <span>免小时费低消</span>
                <div class="range-inputs">
                  <input v-model.trim="listingFilters.hourlyFeeWaiver.min" class="input h-11" type="number" min="0" step="0.0001" placeholder="最低" />
                  <span>至</span>
                  <input v-model.trim="listingFilters.hourlyFeeWaiver.max" class="input h-11" type="number" min="0" step="0.0001" placeholder="最高" />
                </div>
              </label>

              <div class="filter-field model-filter-field">
                <span>可用模型</span>
                <details class="model-filter-menu">
                  <summary>
                    <Icon name="filter" size="sm" />
                    <span>{{ modelFilterSummary }}</span>
                  </summary>
                  <div class="model-filter-panel">
                    <div class="model-filter-options">
                      <label v-for="model in modelFilterOptions" :key="model" class="model-filter-option">
                        <input
                          type="checkbox"
                          :checked="listingFilters.models.includes(model)"
                          @change="toggleModelFilter(model)"
                        />
                        <span>{{ model }}</span>
                      </label>
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
                    <div v-if="listingFilters.models.length > 0" class="selected-model-filters">
                      <button
                        v-for="model in listingFilters.models"
                        :key="model"
                        type="button"
                        class="selected-model-filter"
                        @click="removeModelFilter(model)"
                      >
                        <span>{{ model }}</span>
                        <Icon name="x" size="xs" />
                      </button>
                    </div>
                  </div>
                </details>
              </div>
            </div>

            <div class="filter-button-row">
              <button class="filter-apply-button" type="button" :disabled="loading" @click="applyListingFilters">
                <Icon name="filter" size="sm" class="mr-2" />
                <span>应用筛选</span>
              </button>
              <button class="filter-reset-button" type="button" :disabled="loading || !hasResultFilters" @click="resetListingFilters">
                <Icon name="x" size="sm" class="mr-2" />
                <span>重置</span>
              </button>
            </div>
          </div>
        </div>
      </section>

      <div v-if="errorMessage" class="rounded-lg border border-red-200 bg-red-50 p-3 text-sm text-red-700 dark:border-red-500/30 dark:bg-red-500/10 dark:text-red-200">
        {{ errorMessage }}
      </div>

      <div v-if="loading" class="rounded-lg border border-gray-200 bg-white p-8 text-center text-sm text-gray-500 shadow-sm dark:border-dark-700 dark:bg-dark-900 dark:text-dark-300">
        正在加载账号广场...
      </div>

      <section v-else-if="displayedListings.length > 0" class="grid gap-4 xl:grid-cols-2">
        <article
          v-for="listing in displayedListings"
          :key="listing.id"
          class="listing-card"
        >
          <div class="flex items-start justify-between gap-3">
            <div class="min-w-0">
              <div class="mb-2 flex flex-wrap items-center gap-1.5">
                <span :class="accountLevelBadgeClass(listing)">
                  {{ accountLevelBadgeLabel(listing) }}
                </span>
                <span v-if="supportsImageGeneration(listing)" class="feature-badge feature-badge-image">支持生图</span>
                <span
                  v-if="listing.hourly_fee_waiver_minimum > 0"
                  class="feature-badge feature-badge-waiver"
                  :title="`每小时消费满 ${formatNumber(listing.hourly_fee_waiver_minimum)} 免小时费`"
                >
                  满低消免小时费
                </span>
              </div>
              <h2 class="listing-title">{{ listing.account_name || `共享账号 #${listing.id}` }}</h2>
              <p class="listing-owner">号主：{{ listing.owner_username || `用户 ${listing.owner_user_id}` }}</p>
            </div>
            <div class="flex flex-shrink-0 flex-col items-end gap-1">
              <span :class="statusBadgeClass(listing.status)">
                {{ statusLabel(listing.status) }}
              </span>
              <span class="rounded-full bg-primary-50 px-2.5 py-1 text-xs font-semibold text-primary-700 dark:bg-primary-500/10 dark:text-primary-200">
                {{ listing.active_seats }}/{{ listing.seat_limit }}
              </span>
            </div>
          </div>

          <div class="listing-health-panel">
            <div class="listing-status-grid">
              <div class="listing-runtime-row">
                <div class="min-w-0">
                  <span class="listing-runtime-label">账号状态</span>
                  <strong>{{ runtimeInsight(listing).label }}</strong>
                  <p v-if="runtimeInsight(listing).detail">{{ runtimeInsight(listing).detail }}</p>
                </div>
                <span :class="runtimeInsightClass(runtimeInsight(listing).tone)">
                  {{ runtimeInsight(listing).badge }}
                </span>
              </div>

              <div class="capacity-panel">
                <div class="flex items-center justify-between gap-3">
                  <span>实时容量</span>
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

            <div class="usage-window-list">
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

            <div v-if="validityInfo(listing)" class="validity-strip">
              <div class="flex min-w-0 items-center gap-2">
                <Icon name="calendar" size="sm" />
                <span>{{ validityInfo(listing)?.label }}</span>
              </div>
              <strong>{{ validityInfo(listing)?.expiresAtLabel }}</strong>
            </div>

            <div class="listing-health-foot">
              <span v-if="listing.codex_usage_updated_at">用量更新：{{ formatDate(listing.codex_usage_updated_at) }}</span>
              <span v-if="listing.codex_quota_protection_reset_at">保护解除：{{ formatRelativeUntil(listing.codex_quota_protection_reset_at) }}</span>
              <span v-if="listing.rate_limit_reset_at">限流解除：{{ formatRelativeUntil(listing.rate_limit_reset_at) }}</span>
            </div>
          </div>

          <div class="listing-metric-grid">
            <div class="metric metric-billing" :class="{ 'metric-price-danger': isRateMultiplierExpensive(listing) }"><span>倍率</span><strong>{{ formatNumber(listing.rate_multiplier) }}x</strong></div>
            <div class="metric metric-billing"><span>最低余额</span><strong>{{ formatNumber(listing.min_balance_required) }}</strong></div>
            <div class="metric"><span>账号并发</span><strong>{{ listing.account_concurrency }}</strong></div>
            <div class="metric"><span>单用户并发</span><strong>{{ listing.per_user_concurrency }}</strong></div>
            <div class="metric metric-billing" :class="{ 'metric-price-danger': isHourlyRateExpensive(listing) }"><span>小时费</span><strong>{{ formatNumber(listing.hourly_rate) }}</strong></div>
            <div class="metric metric-billing"><span>免小时费低消</span><strong>{{ hourlyFeeWaiverLabel(listing.hourly_fee_waiver_minimum) }}</strong></div>
            <div class="metric"><span>Codex保护</span><strong>{{ listing.codex_5h_limit_percent }}% / {{ listing.codex_7d_limit_percent }}%</strong></div>
          </div>

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

          <div v-if="isManagementView" class="mt-3 rounded-lg border border-gray-200 bg-gray-50 p-3 text-sm dark:border-dark-700 dark:bg-dark-800/60">
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
          <div v-else-if="listing.current_membership_id" class="account-share-membership-panel">
            <div class="membership-status-head">
              <div>
                <div class="font-medium">正在使用，绑定 Key #{{ listing.current_api_key_id }}</div>
                <div class="mt-1 text-xs opacity-80">{{ idleTimeoutSummary(listing) }}</div>
              </div>
              <span class="membership-status-pill">已加入</span>
            </div>
            <div class="membership-detail-grid">
              <div v-if="listing.current_joined_at">
                <span>加入时间</span>
                <strong>{{ formatDate(listing.current_joined_at) }}</strong>
              </div>
              <div v-if="listing.current_last_request_at">
                <span>最近请求</span>
                <strong>{{ formatDate(listing.current_last_request_at) }}</strong>
              </div>
              <div v-if="listing.current_paid_until">
                <span>下次预付</span>
                <strong>{{ formatCountdownUntil(listing.current_paid_until) }}</strong>
              </div>
              <div v-if="listing.current_billed_until">
                <span>已结算到</span>
                <strong>{{ formatDate(listing.current_billed_until) }}</strong>
              </div>
            </div>
            <div class="idle-timeout-control">
              <label :for="`idle-timeout-current-${listing.id}`">空闲自动退出</label>
              <div class="idle-timeout-row">
                <input
                  :id="`idle-timeout-current-${listing.id}`"
                  v-model.number="idleTimeoutByListing[listing.id]"
                  class="input h-10"
                  type="number"
                  min="0"
                  :max="ACCOUNT_SHARE_IDLE_TIMEOUT_MAX_MINUTES"
                  step="1"
                />
                <span>分钟</span>
                <button
                  class="btn-secondary h-10"
                  type="button"
                  :disabled="savingIdleTimeoutId === listing.current_membership_id"
                  @click="saveIdleTimeout(listing)"
                >
                  保存
                </button>
              </div>
            </div>
            <button class="mt-3 h-9 rounded-lg bg-emerald-700 px-3 text-sm font-medium text-white hover:bg-emerald-800 disabled:cursor-not-allowed disabled:opacity-60" type="button" :disabled="endingId === listing.current_membership_id" @click="openEndUseConfirm(listing)">
              结束使用
            </button>
          </div>
          <div v-else class="listing-join-section">
            <div v-if="listingEditLocked(listing)" class="edit-lock-strip">
              <Icon name="exclamationCircle" size="sm" />
              <span>账号配置正在编辑中，暂时不能加入使用，避免使用修改前的旧配置。</span>
            </div>
            <div class="listing-action-row">
              <div v-if="singleModeApiKey" class="mode-key-readonly">
                <Icon name="key" size="sm" />
                <span>{{ modeKeyLabel(singleModeApiKey) }}</span>
              </div>
              <select v-else v-model.number="selectedKeyByListing[listing.id]" class="input h-10" :disabled="modeKeysLoading || !modeKeysLoaded">
                <option :value="0">{{ modeApiKeyPlaceholder }}</option>
                <option v-for="key in modeApiKeys" :key="key.id" :value="key.id">{{ key.name || `Key #${key.id}` }}</option>
              </select>
              <label class="idle-timeout-join">
                <span>空闲退出</span>
                <input
                  v-model.number="idleTimeoutByListing[listing.id]"
                  class="input h-10"
                  type="number"
                  min="0"
                  :max="ACCOUNT_SHARE_IDLE_TIMEOUT_MAX_MINUTES"
                  step="1"
                />
              </label>
              <button class="btn-primary h-10" type="button" :disabled="listingEditLocked(listing) || modeKeysLoading || joiningId === listing.id" @click="joinUse(listing)">
                {{ joiningId === listing.id ? '加入中' : (modeKeysLoading ? '加载 Key 中' : '加入使用') }}
              </button>
            </div>
          </div>
        </article>
      </section>

      <div v-else class="rounded-lg border border-dashed border-gray-300 bg-white p-8 text-center text-sm text-gray-500 dark:border-dark-700 dark:bg-dark-900 dark:text-dark-300">
        {{ pagination.total === 0 ? (hasResultFilters ? '没有匹配的共享账号。' : (isManagementView ? '暂无可管理账号。' : '当前分类暂无账号。')) : '当前页暂无账号。' }}
      </div>

      <Pagination
        v-if="!loading && pagination.total > pagination.page_size"
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
      <ModelWhitelistSelector v-model="editingAllowedModels" platform="openai" />

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
      title="确认加入共享账号"
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
            <span>加入后该 API Key 会绑定到这个账号，请确认价格、并发和模型限制后再继续。</span>
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
            <span>账号等级</span>
            <strong>{{ accountLevelBadgeLabel(pendingJoinListing) }}</strong>
          </div>
          <div class="join-confirmation-field" :class="{ 'join-price-danger': isRateMultiplierExpensive(pendingJoinListing) }">
            <span>倍率</span>
            <strong>{{ formatNumber(pendingJoinListing.rate_multiplier) }}x</strong>
          </div>
          <div class="join-confirmation-field" :class="{ 'join-price-danger': isHourlyRateExpensive(pendingJoinListing) }">
            <span>小时费</span>
            <strong>{{ formatNumber(pendingJoinListing.hourly_rate) }}</strong>
          </div>
          <div class="join-confirmation-field">
            <span>免小时费低消</span>
            <strong>{{ hourlyFeeWaiverLabel(pendingJoinListing.hourly_fee_waiver_minimum) }}</strong>
          </div>
          <div class="join-confirmation-field">
            <span>最低余额</span>
            <strong>{{ formatNumber(pendingJoinListing.min_balance_required) }}</strong>
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
          <div class="join-confirmation-field">
            <span>Codex保护</span>
            <strong>{{ pendingJoinListing.codex_5h_limit_percent }}% / {{ pendingJoinListing.codex_7d_limit_percent }}%</strong>
          </div>
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
          确认加入
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
                  <input v-model="editForm.name" class="input" placeholder="OpenAI共享账号" />
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
                    <ModelWhitelistSelector v-model="editAllowedModels" platform="openai" />
                  </div>
                </div>

                <div class="grid gap-3 sm:grid-cols-2 xl:grid-cols-1">
                  <label class="field">
                    <span>Codex 5h 保护 %</span>
                    <input v-model.number="editForm.codex_5h_limit_percent" class="input" type="number" min="1" max="100" step="1" />
                  </label>
                  <label class="field">
                    <span>Codex 7d 保护 %</span>
                    <input v-model.number="editForm.codex_7d_limit_percent" class="input" type="number" min="1" max="100" step="1" />
                  </label>
                </div>
              </div>

              <div v-if="editConcurrencyNotice" class="notice-row mt-3">
                <Icon name="infoCircle" size="sm" class="mt-0.5 flex-shrink-0" />
                <span>{{ editConcurrencyNotice }}</span>
              </div>

              <label class="toggle-row mt-3">
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

    <ConfirmDialog
      :show="pendingEndUse !== null"
      title="确认结束使用"
      :message="endUseConfirmMessage"
      confirm-text="结束使用"
      cancel-text="取消"
      danger
      @confirm="confirmEndUse"
      @cancel="cancelEndUse"
    />

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
import { useRouter } from 'vue-router'
import { accountShareAPI, type AccountShareListing, type AccountShareListingFilters, type AccountShareListingStatus, type AccountShareListingTab } from '@/api/accountShare'
import { accountsAPI, adminAPI, keysAPI, userGroupsAPI } from '@/api'
import type { Account, AccountLevel, AccountUsageStatsResponse, ApiKey, Group, Proxy, ProxyProtocol, UsageProgress } from '@/types'
import { useAppStore } from '@/stores/app'
import { useAuthStore } from '@/stores/auth'
import { useClipboard } from '@/composables/useClipboard'
import { extractApiErrorMessage } from '@/utils/apiError'
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
  seatLimit?: number
}

type ListingStatusFilterValue = AccountShareListingStatus | 'available' | 'all' | ''
type AccountLevelFilterValue = AccountLevel | 'all' | ''

interface TextRangeFilter {
  min: string | number
  max: string | number
}

interface ListingFilterState {
  status: ListingStatusFilterValue
  accountLevel: AccountLevelFilterValue
  perUserConcurrency: TextRangeFilter
  minBalance: TextRangeFilter
  hourlyRate: TextRangeFilter
  hourlyFeeWaiver: TextRangeFilter
  models: string[]
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

interface PendingJoinConfirmation {
  listing: AccountShareListing
  apiKeyID: number
  idleTimeoutMinutes: number
}

const DEFAULT_ACCOUNT_CONCURRENCY = 20
const DEFAULT_PER_USER_CONCURRENCY = 5
const DEFAULT_HOURLY_RATE = 0.2
const PLUS_EXPENSIVE_RATE_MULTIPLIER = 0.15
const PRO_EXPENSIVE_RATE_MULTIPLIER = 0.25
const EXPENSIVE_HOURLY_RATE = 2
const MAX_ACCOUNT_CONCURRENCY = 50
const ACCOUNT_SHARE_MIN_SEATS = 2
const ACCOUNT_SHARE_MAX_SEATS = 12
const ACCOUNT_NAME_BASE = 'OpenAI共享账号'
const PROXY_PURCHASE_URL = 'https://www.seekproxy.com/user/reg?invite_id=105978'
const DEFAULT_ACCOUNT_SHARE_ALLOWED_MODELS = ['gpt-5.5', 'gpt-5.4', 'gpt-5.4-mini', 'codex-auto-review']
const ACCOUNT_SHARE_PAGE_SIZE = 10
const MODEL_PREVIEW_LIMIT = 5
const ACCOUNT_SHARE_IDLE_TIMEOUT_MAX_MINUTES = 10080

const appStore = useAppStore()
const authStore = useAuthStore()
const router = useRouter()
const { copyToClipboard } = useClipboard()
const seatOptions = Array.from({ length: ACCOUNT_SHARE_MAX_SEATS - ACCOUNT_SHARE_MIN_SEATS + 1 }, (_, index) => index + ACCOUNT_SHARE_MIN_SEATS)
const filters: FilterOption[] = [
  { key: 'using', label: '正在使用', tab: 'using' },
  { key: 'history', label: '历史使用', tab: 'history' },
  { key: 'all', label: '全部', tab: 'all' },
  ...seatOptions.map((seat): FilterOption => ({ key: `seat-${seat}`, label: `${seat}人`, tab: 'all', seatLimit: seat }))
]
const ownerFilter: FilterOption = { key: 'mine', label: '我的账号', tab: 'mine' }
const listingStatusFilterOptions: Array<{ value: ListingStatusFilterValue; label: string }> = [
  { value: '', label: '默认状态' },
  { value: 'available', label: '可用账号' },
  { value: 'active', label: '已上架' },
  { value: 'paused', label: '已暂停' },
  { value: 'disabled', label: '已下架' },
  { value: 'all', label: '全部状态' }
]
const accountLevelFilterOptions: Array<{ value: AccountLevelFilterValue; label: string }> = [
  { value: 'all', label: '全部等级' },
  { value: 'free', label: 'Free' },
  { value: 'plus', label: 'Plus' },
  { value: 'pro', label: 'Pro' },
  { value: 'team', label: 'Team' },
  { value: 'unknown', label: 'UNKNOWN' }
]
const accountShareJoinErrorMessages: Record<string, string> = {
  ACCOUNT_SHARE_ACCOUNT_UNAVAILABLE: '该共享账号当前不可加入，请换一个账号或稍后再试',
  ACCOUNT_SHARE_ALREADY_USING: '你当前已有正在使用的共享账号，请先结束后再加入新的账号',
  ACCOUNT_SHARE_API_KEY_ALREADY_BOUND: '当前账号模式 Key 已绑定其他共享账号，请先结束原使用记录',
  ACCOUNT_SHARE_API_KEY_MUST_USE_MODE_GROUP: '请选择绑定「OpenAI账号模式」分组的 API Key',
  ACCOUNT_SHARE_LISTING_NOT_FOUND: '该共享账号不存在或已下架，请刷新账号广场后再试',
  ACCOUNT_SHARE_LISTING_NOT_ACTIVE: '该共享账号当前未上架，暂时不能加入',
  ACCOUNT_SHARE_LISTING_FULL: '该共享账号席位已满，请换一个账号',
  ACCOUNT_SHARE_BALANCE_BELOW_MINIMUM: '余额低于该账号最低要求，暂时不能加入',
  ACCOUNT_SHARE_MODE_GROUP_UNAVAILABLE: '账号模式分组尚未配置，请联系管理员处理',
  ACCOUNT_SHARE_MODE_GROUP_UNBOUND: '当前账号模式分组未绑定共享账号，请先在账号广场加入一个账号',
  ACCOUNT_SHARE_MODE_INVALID_IDLE_TIMEOUT: '空闲自动退出时间必须在 0-10080 分钟之间',
  ACCOUNT_SHARE_MODE_PREPAY_INSUFFICIENT: '余额不足以预付本次使用，请充值后再试',
  ACCOUNT_SHARE_PER_USER_CONCURRENCY_EXCEEDED: '该共享账号当前单用户并发已达到上限，请稍后再试',
  ACCOUNT_SHARE_OWNER_CANNOT_JOIN: '不能加入自己上架的共享账号',
  ACCOUNT_SHARE_LISTING_EDITING: '账号配置正在编辑中，暂时不能加入使用',
  API_KEY_NOT_FOUND: '该 API Key 不存在或已被删除，请重新选择',
  INSUFFICIENT_PERMISSIONS: '你没有权限使用这个 API Key，请重新选择自己的账号模式 Key',
  SERVICE_UNAVAILABLE: '账号广场服务暂时不可用，请稍后再试',
  USER_NOT_FOUND: '当前用户状态异常，请重新登录后再试'
}

const activeFilter = ref<FilterOption>(filters[2])
const listings = ref<AccountShareListing[]>([])
const pagination = reactive({
  page: 1,
  page_size: ACCOUNT_SHARE_PAGE_SIZE,
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
const authURL = ref('')
const authSessionID = ref('')
const creating = ref(false)
const generatingOAuthURL = ref(false)
const joiningId = ref<number | null>(null)
const pendingJoinConfirmation = ref<PendingJoinConfirmation | null>(null)
const endingId = ref<number | null>(null)
const pendingEndUse = ref<{ membershipID: number; apiKeyID?: number } | null>(null)
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
const availableGroups = ref<Group[]>([])
const modeApiKeys = ref<ApiKey[]>([])
const modeKeysLoading = ref(false)
const modeKeysLoaded = ref(false)
const proxies = ref<Proxy[]>([])
const knownListings = ref<AccountShareListing[]>([])
const proxyLoading = ref(false)
const proxyLoadMessage = ref('')
const searchQuery = ref('')
const modelFilterInput = ref('')
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

const listingFilters = reactive<ListingFilterState>({
  status: '',
  accountLevel: 'all',
  perUserConcurrency: { min: '', max: '' },
  minBalance: { min: '', max: '' },
  hourlyRate: { min: '', max: '' },
  hourlyFeeWaiver: { min: '', max: '' },
  models: []
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
    name: suggestedAccountName(),
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
    codex_7d_limit_percent: 100
  }
}

const createForm = reactive<CreateFormState>(buildDefaultCreateForm())
const editForm = reactive<CreateFormState>(buildDefaultCreateForm())
const allowedModels = ref<string[]>([...DEFAULT_ACCOUNT_SHARE_ALLOWED_MODELS])

function isOpenAIAccountModeGroup(group: Group): boolean {
  return group.platform === 'openai' && (group.name === 'OpenAI账号模式' || group.name.includes('账号模式'))
}

const modeGroup = computed(() => availableGroups.value.find(isOpenAIAccountModeGroup))
const singleModeApiKey = computed(() => modeApiKeys.value.length === 1 ? modeApiKeys.value[0] : null)
const modeApiKeyPlaceholder = computed(() => {
  if (modeKeysLoading.value) return '正在加载账号模式 API Key'
  if (!modeKeysLoaded.value) return '账号模式 API Key 未加载'
  return '选择账号模式 API Key'
})
const pendingJoinListing = computed(() => pendingJoinConfirmation.value?.listing ?? null)
const pendingJoinApiKeyLabel = computed(() => {
  const apiKeyID = pendingJoinConfirmation.value?.apiKeyID
  if (!apiKeyID) return '-'
  const key = modeApiKeys.value.find(item => item.id === apiKeyID)
  return key ? modeKeyLabel(key) : `Key #${apiKeyID}`
})
const pendingJoinIdleTimeoutLabel = computed(() => formatIdleTimeoutSetting(pendingJoinConfirmation.value?.idleTimeoutMinutes ?? 0))
const pendingJoinPriceWarnings = computed(() => {
  const listing = pendingJoinListing.value
  if (!listing) return []
  const warnings: string[] = []
  if (isRateMultiplierExpensive(listing)) {
    warnings.push(`${accountLevelBadgeLabel(listing)} 账号倍率 ${formatNumber(listing.rate_multiplier)}x 偏高，后续请求消耗会明显增加。`)
  }
  if (isHourlyRateExpensive(listing)) {
    warnings.push(`小时费 ${formatNumber(listing.hourly_rate)} 偏高，空闲或长时间使用时费用压力较大。`)
  }
  return warnings
})
const endUseConfirmMessage = computed(() => {
  const apiKeyLabel = pendingEndUse.value?.apiKeyID ? ` Key #${pendingEndUse.value.apiKeyID}` : '当前 Key'
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
const activeMembershipCount = computed(() => listings.value.filter(listing => listing.current_membership_id).length)
const isManagementView = computed(() => activeFilter.value.key === ownerFilter.key)
const activeAdvancedFilterCount = computed(() => {
  let count = 0
  if (listingFilters.status !== '') count += 1
  if (listingFilters.accountLevel !== 'all') count += 1
  if (listingFilters.perUserConcurrency.min !== '' || listingFilters.perUserConcurrency.max !== '') count += 1
  if (listingFilters.minBalance.min !== '' || listingFilters.minBalance.max !== '') count += 1
  if (listingFilters.hourlyRate.min !== '' || listingFilters.hourlyRate.max !== '') count += 1
  if (listingFilters.hourlyFeeWaiver.min !== '' || listingFilters.hourlyFeeWaiver.max !== '') count += 1
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

const displayedListings = computed(() => listings.value)
const modelFilterOptions = computed(() => {
  const models = new Set<string>([...DEFAULT_ACCOUNT_SHARE_ALLOWED_MODELS, ...listingFilters.models])
  for (const listing of knownListings.value) {
    for (const model of listing.allowed_models) {
      const value = model.trim()
      if (value) models.add(value)
    }
  }
  for (const listing of listings.value) {
    for (const model of listing.allowed_models) {
      const value = model.trim()
      if (value) models.add(value)
    }
  }
  return Array.from(models).sort((a, b) => a.localeCompare(b))
})
const modelFilterSummary = computed(() => {
  if (listingFilters.models.length === 0) return '全部模型'
  if (listingFilters.models.length === 1) return listingFilters.models[0]
  return `已选 ${listingFilters.models.length} 个模型`
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

function suggestedAccountName(): string {
  for (let index = 1; index <= 999; index += 1) {
    const candidate = index === 1 ? ACCOUNT_NAME_BASE : `${ACCOUNT_NAME_BASE}${index}`
    if (!hasKnownAccountName(candidate)) return candidate
  }
  return `${ACCOUNT_NAME_BASE}${Date.now()}`
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

function addModelFilterFromInput(): void {
  addModelFilter(modelFilterInput.value)
  modelFilterInput.value = ''
}

function parseFilterNumber(raw: unknown): number | undefined {
  if (raw === undefined || raw === null) return undefined
  if (typeof raw === 'number') return Number.isFinite(raw) ? raw : Number.NaN
  if (typeof raw !== 'string') return Number.NaN
  const value = raw.trim()
  if (!value) return undefined
  const parsed = Number(value)
  return Number.isFinite(parsed) ? parsed : Number.NaN
}

function validateRangeFilter(range: TextRangeFilter, label: string, integerOnly = false): string {
  const min = parseFilterNumber(range.min)
  const max = parseFilterNumber(range.max)
  if (Number.isNaN(min) || Number.isNaN(max)) return `${label}必须是有效数字`
  if ((min !== undefined && min < 0) || (max !== undefined && max < 0)) return `${label}不能小于 0`
  if (integerOnly && ((min !== undefined && !Number.isInteger(min)) || (max !== undefined && !Number.isInteger(max)))) {
    return `${label}必须是整数`
  }
  if (min !== undefined && max !== undefined && min > max) return `${label}的最低值不能大于最高值`
  return ''
}

function validateListingFilters(): string {
  return validateRangeFilter(listingFilters.perUserConcurrency, '单用户并发数范围', true) ||
    validateRangeFilter(listingFilters.minBalance, '最低余额范围') ||
    validateRangeFilter(listingFilters.hourlyRate, '小时费范围') ||
    validateRangeFilter(listingFilters.hourlyFeeWaiver, '免小时费低消范围')
}

function appendRangeFilters(target: AccountShareListingFilters, range: TextRangeFilter, minKey: keyof AccountShareListingFilters, maxKey: keyof AccountShareListingFilters): void {
  const min = parseFilterNumber(range.min)
  const max = parseFilterNumber(range.max)
  if (min !== undefined && !Number.isNaN(min)) {
    target[minKey] = min as never
  }
  if (max !== undefined && !Number.isNaN(max)) {
    target[maxKey] = max as never
  }
}

function buildListingFilters(): AccountShareListingFilters {
  const result: AccountShareListingFilters = {
    tab: activeFilter.value.tab
  }
  if (activeFilter.value.seatLimit) result.seat_limit = activeFilter.value.seatLimit
  const search = searchQuery.value.trim()
  if (search) result.search = search
  if (listingFilters.status === 'available') {
    result.status = 'active'
    result.available_only = true
  } else if (listingFilters.status !== '') {
    result.status = listingFilters.status
  }
  if (listingFilters.accountLevel !== 'all') result.account_level = listingFilters.accountLevel
  if (listingFilters.models.length > 0) result.models = normalizeAllowedModelList(listingFilters.models)
  appendRangeFilters(result, listingFilters.perUserConcurrency, 'per_user_concurrency_min', 'per_user_concurrency_max')
  appendRangeFilters(result, listingFilters.minBalance, 'min_balance_required_min', 'min_balance_required_max')
  appendRangeFilters(result, listingFilters.hourlyRate, 'hourly_rate_min', 'hourly_rate_max')
  appendRangeFilters(result, listingFilters.hourlyFeeWaiver, 'hourly_fee_waiver_min', 'hourly_fee_waiver_max')
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
  return maybeCanceled.code === 'ERR_CANCELED' || maybeCanceled.name === 'CanceledError'
}

function applyListingFilters(): void {
  clearSearchDebounceTimer()
  const validationError = validateListingFilters()
  if (validationError) {
    errorMessage.value = validationError
    return
  }
  pagination.page = 1
  void loadListings()
}

function resetListingFilters(): void {
  listingFilters.status = ''
  listingFilters.accountLevel = 'all'
  listingFilters.perUserConcurrency.min = ''
  listingFilters.perUserConcurrency.max = ''
  listingFilters.minBalance.min = ''
  listingFilters.minBalance.max = ''
  listingFilters.hourlyRate.min = ''
  listingFilters.hourlyRate.max = ''
  listingFilters.hourlyFeeWaiver.min = ''
  listingFilters.hourlyFeeWaiver.max = ''
  listingFilters.models = []
  modelFilterInput.value = ''
  if (searchQuery.value !== '') {
    suppressNextSearchRefresh = true
    searchQuery.value = ''
  }
  clearSearchDebounceTimer()
  pagination.page = 1
  void loadListings()
}

function handlePageChange(page: number): void {
  clearSearchDebounceTimer()
  pagination.page = page
  void loadListings()
}

function handlePageSizeChange(): void {
  clearSearchDebounceTimer()
  pagination.page_size = ACCOUNT_SHARE_PAGE_SIZE
  pagination.page = 1
  void loadListings()
}

function formatNumber(value: number): string {
  return Number(value || 0).toFixed(4).replace(/\.?0+$/, '')
}

function hourlyFeeWaiverLabel(value?: number | null): string {
  const amount = Number(value || 0)
  if (!Number.isFinite(amount) || amount <= 0) return '未开启'
  return `${formatNumber(amount)}/小时`
}

function formatIdleTimeoutSetting(minutes: number): string {
  const normalized = normalizeIdleTimeoutMinutes(minutes)
  if (normalized <= 0) return '未开启'
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

function accountLevelLabel(level?: AccountLevel | string): string {
  switch ((level || '').toLowerCase()) {
    case 'free':
      return 'Free'
    case 'plus':
      return 'Plus'
    case 'pro':
      return 'Pro'
    case 'team':
      return 'Team'
    default:
      return 'UNKNOWN'
  }
}

function normalizePlanToken(planType?: string | null): string {
  return (planType || '').trim().toLowerCase().replace(/[\s_-]+/g, '')
}

function officialPlanLabel(planType?: string | null): string {
  const raw = (planType || '').trim()
  if (!raw) return ''
  const token = normalizePlanToken(raw)
  if (token === 'free' || token === 'chatgptfree') return 'Free'
  if (token === 'plus' || token === 'chatgptplus' || token.startsWith('plus')) return 'Plus'
  if (token === 'team' || token === 'chatgptteam') return 'Team'
  if (token === 'pro' || token === 'chatgptpro') return 'Pro'
  const proMatch = token.match(/^(?:chatgpt)?pro(\d+)x?$/)
  if (proMatch?.[1]) return `Pro${proMatch[1]}x`
  if (token.startsWith('pro') || token.startsWith('chatgptpro')) {
    const multiplier = token.match(/(\d+)x?/)
    return multiplier?.[1] ? `Pro${multiplier[1]}x` : 'Pro'
  }
  return raw
}

function accountLevelTone(listing: AccountShareListing): string {
  const level = (listing.account_level || '').toLowerCase()
  if (level && level !== 'unknown') return level
  const planToken = normalizePlanToken(listing.account_plan_type)
  if (planToken.includes('team')) return 'team'
  if (planToken.includes('pro')) return 'pro'
  if (planToken.includes('plus')) return 'plus'
  if (planToken.includes('free')) return 'free'
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
  if (listing.codex_quota_protection_reason) {
    const windowLabel = listing.codex_quota_protection_reason === '7d' ? '7天' : '5小时'
    return {
      label: `${windowLabel}保护中`,
      detail: listing.codex_quota_protection_reset_at ? `预计 ${formatRelativeUntil(listing.codex_quota_protection_reset_at)} 后解除` : '',
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

function canOwnerRelistListing(listing: AccountShareListing): boolean {
  const currentUserID = Number(authStore.user?.id || 0)
  return !authStore.isAdmin &&
    listing.status !== 'active' &&
    currentUserID > 0 &&
    listing.owner_user_id === currentUserID
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

function selectedModeApiKeyID(listing: AccountShareListing): number {
  return singleModeApiKey.value?.id || Number(selectedKeyByListing[listing.id] || 0)
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

function showModeApiKeyRequiredDialog(): void {
  if (modeKeysLoading.value) {
    showActionError('账号模式 API Key 正在加载，请稍候再加入使用。', '正在加载')
    return
  }
  if (!modeKeysLoaded.value) {
    showActionError('账号模式 API Key 尚未加载成功，请刷新账号广场后再加入使用。', '无法加入使用')
    return
  }
  if (!modeGroup.value) {
    showActionError('当前账号没有可用的「OpenAI账号模式」分组，请联系管理员开通后再加入。', '无法加入使用')
    return
  }
  if (modeApiKeys.value.length === 0) {
    showActionError(
      '你还没有账号模式 API Key，请先到「API 密钥」页面创建一个绑定「OpenAI账号模式」分组的 Key。',
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
  if (!Number.isFinite(parsed) || parsed < 0 || !Number.isInteger(parsed)) return '空闲自动退出时间必须是非负整数分钟'
  if (parsed > ACCOUNT_SHARE_IDLE_TIMEOUT_MAX_MINUTES) return '空闲自动退出时间不能超过 10080 分钟'
  return ''
}

function syncIdleTimeoutControls(items: AccountShareListing[]): void {
  for (const listing of items) {
    if (listing.current_membership_id && typeof listing.current_idle_timeout_minutes === 'number') {
      idleTimeoutByListing[listing.id] = normalizeIdleTimeoutMinutes(listing.current_idle_timeout_minutes)
      continue
    }
    if (idleTimeoutByListing[listing.id] === undefined) {
      idleTimeoutByListing[listing.id] = 0
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

function setFilter(filter: FilterOption): void {
  clearSearchDebounceTimer()
  activeFilter.value = filter
  pagination.page = 1
  void loadListings()
}

function toggleCreatePanel(): void {
  showCreate.value = !showCreate.value
  if (showCreate.value) {
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
  allowedModels.value = [...DEFAULT_ACCOUNT_SHARE_ALLOWED_MODELS]
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
  if (!Number.isFinite(Number(createForm.codex_5h_limit_percent)) || Number(createForm.codex_5h_limit_percent) < 1 || Number(createForm.codex_5h_limit_percent) > 100) return 'Codex 5h 保护必须在 1-100 之间'
  if (!Number.isFinite(Number(createForm.codex_7d_limit_percent)) || Number(createForm.codex_7d_limit_percent) < 1 || Number(createForm.codex_7d_limit_percent) > 100) return 'Codex 7d 保护必须在 1-100 之间'
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
  if (!Number.isFinite(Number(editForm.codex_5h_limit_percent)) || Number(editForm.codex_5h_limit_percent) < 1 || Number(editForm.codex_5h_limit_percent) > 100) return 'Codex 5h 保护必须在 1-100 之间'
  if (!Number.isFinite(Number(editForm.codex_7d_limit_percent)) || Number(editForm.codex_7d_limit_percent) < 1 || Number(editForm.codex_7d_limit_percent) > 100) return 'Codex 7d 保护必须在 1-100 之间'
  if (parseEditAllowedModels().length === 0) return '至少填写一个模型白名单'
  if (!editSessionID.value) return '编辑会话已失效，请关闭后重新编辑'
  return ''
}

async function loadListings(): Promise<void> {
  const validationError = validateListingFilters()
  if (validationError) {
    abortActiveListingsRequest()
    loading.value = false
    errorMessage.value = validationError
    return
  }
  abortActiveListingsRequest()
  const requestSeq = ++listingsRequestSeq
  const controller = new AbortController()
  listingsRequestController = controller
  loading.value = true
  errorMessage.value = ''
  try {
    const result = await accountShareAPI.listListings(pagination.page, pagination.page_size, buildListingFilters(), {
      signal: controller.signal
    })
    if (controller.signal.aborted || requestSeq !== listingsRequestSeq) return
    const realListings = (result.items || []).map(normalizeListingForMerge)
    pagination.total = result.total || 0
    pagination.page = result.page || pagination.page
    pagination.page_size = result.page_size || ACCOUNT_SHARE_PAGE_SIZE
    pagination.pages = result.pages || 1
    listings.value = realListings
    syncIdleTimeoutControls(realListings)
    mergeKnownListings(realListings)
  } catch (error: unknown) {
    if (controller.signal.aborted || requestSeq !== listingsRequestSeq || isCanceledRequest(error)) return
    listings.value = []
    pagination.total = 0
    pagination.pages = 1
    errorMessage.value = extractApiErrorMessage(error, '加载账号广场失败')
  } finally {
    if (requestSeq === listingsRequestSeq) {
      if (listingsRequestController === controller) listingsRequestController = null
      loading.value = false
    }
  }
}

function normalizeListingForMerge(listing: AccountShareListing): AccountShareListing {
  const next = { ...listing }
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

async function loadModeKeys(): Promise<void> {
  modeKeysLoading.value = true
  modeKeysLoaded.value = false
  modeApiKeys.value = []
  try {
    const groups = await userGroupsAPI.getAvailable()
    availableGroups.value = groups
    const accountModeGroup = groups.find(isOpenAIAccountModeGroup)
    if (!accountModeGroup) {
      modeKeysLoaded.value = true
      return
    }
    const result = await keysAPI.list(1, 100, { group_id: accountModeGroup.id })
    modeApiKeys.value = result.items || []
    modeKeysLoaded.value = true
  } finally {
    modeKeysLoading.value = false
  }
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
    const result = await accountShareAPI.generateOpenAIAuthURL({ proxy_id: currentProxyID.value })
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
  if (!authSessionID.value || !authCode || !oauthState) {
    createErrorMessage.value = '请先生成登录链接，并粘贴包含 code 和 state 的 OpenAI 回调结果'
    return
  }

  creating.value = true
  try {
    await accountShareAPI.exchangeOpenAICode({
      session_id: authSessionID.value,
      code: authCode,
      state: oauthState,
      proxy_id: currentProxyID.value,
      name: createForm.name.trim(),
      concurrency: Number(createForm.concurrency),
      seat_limit: Number(createForm.seat_limit),
      rate_multiplier: Number(createForm.rate_multiplier),
      allowed_models: parseAllowedModels(),
      per_user_concurrency: Number(createForm.per_user_concurrency),
      hourly_rate: Number(createForm.hourly_rate),
      hourly_fee_waiver_minimum: Number(createForm.hourly_fee_waiver_minimum),
      min_balance_required: Number(createForm.min_balance_required),
      codex_cli_only: createForm.codex_cli_only,
      codex_5h_limit_percent: Number(createForm.codex_5h_limit_percent),
      codex_7d_limit_percent: Number(createForm.codex_7d_limit_percent)
    })
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
  if (modeKeysLoading.value || !modeKeysLoaded.value) {
    showModeApiKeyRequiredDialog()
    return
  }
  const apiKeyID = selectedModeApiKeyID(listing)
  if (!apiKeyID) {
    showModeApiKeyRequiredDialog()
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
  try {
    await accountShareAPI.joinListing(listing.id, {
      api_key_id: apiKeyID,
      idle_timeout_minutes: idleTimeoutMinutes
    })
    pendingJoinConfirmation.value = null
    await loadListings()
    appStore.showSuccess('已加入使用')
  } catch (error: unknown) {
    pendingJoinConfirmation.value = null
    showActionError(extractApiErrorMessage(error, '加入使用失败', accountShareJoinErrorMessages), '加入使用失败')
  } finally {
    joiningId.value = null
  }
}

function openEndUseConfirm(listing: AccountShareListing): void {
  const membershipID = Number(listing.current_membership_id || 0)
  if (membershipID <= 0 || endingId.value === membershipID) return
  pendingEndUse.value = {
    membershipID,
    apiKeyID: listing.current_api_key_id
  }
}

function cancelEndUse(): void {
  pendingEndUse.value = null
}

async function confirmEndUse(): Promise<void> {
  const membershipID = pendingEndUse.value?.membershipID
  if (!membershipID || endingId.value === membershipID) return
  await endUse(membershipID)
  pendingEndUse.value = null
}

async function endUse(membershipID: number): Promise<void> {
  errorMessage.value = ''
  endingId.value = membershipID
  try {
    const intent = await accountShareAPI.createEndMembershipIntent(membershipID)
    await accountShareAPI.endMembership(membershipID, intent.token)
    await loadListings()
    appStore.showSuccess('已结束使用')
  } catch (error: unknown) {
    showActionError(extractApiErrorMessage(error, '结束使用失败'), '结束使用失败')
  } finally {
    endingId.value = null
  }
}

async function saveIdleTimeout(listing: AccountShareListing): Promise<void> {
  const membershipID = Number(listing.current_membership_id || 0)
  if (membershipID <= 0 || savingIdleTimeoutId.value === membershipID) return
  errorMessage.value = ''
  const idleTimeoutValue = idleTimeoutByListing[listing.id] ?? listing.current_idle_timeout_minutes ?? 0
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
    name: listing.account_name?.trim() ? listing.account_name : `${ACCOUNT_NAME_BASE}${listing.account_id}`,
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
    codex_7d_limit_percent: normalizeEditableNumber(listing.codex_7d_limit_percent, 100)
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
    const updated = await accountShareAPI.updateListing(listing.id, {
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
      codex_cli_only: editForm.codex_cli_only,
      codex_5h_limit_percent: Number(editForm.codex_5h_limit_percent),
      codex_7d_limit_percent: Number(editForm.codex_7d_limit_percent),
      edit_session_id: editSessionID.value,
      force_active_edit: editForceActive.value
    })
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
    void loadListings()
  }, 300)
})

onMounted(async () => {
  clockTimer = window.setInterval(() => {
    nowMs.value = Date.now()
  }, 30_000)
  try {
    await Promise.all([loadListings(), loadModeKeys(), loadProxies(), loadListingNameIndex()])
  } catch (error: unknown) {
    errorMessage.value = extractApiErrorMessage(error, '初始化账号广场失败')
  }
})

onBeforeUnmount(() => {
  if (clockTimer != null) {
    window.clearInterval(clockTimer)
    clockTimer = null
  }
  clearSearchDebounceTimer()
  abortActiveListingsRequest()
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

.hero-actions .btn-primary,
.hero-actions .btn-secondary {
  min-height: 2.75rem;
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

.dark .account-share-hero {
  border-color: rgb(63 63 70);
  background: linear-gradient(180deg, rgb(24 24 27), rgb(31 41 55 / 0.72));
  box-shadow: 0 16px 40px rgb(0 0 0 / 0.28);
}

.dark .account-share-hero-head {
  border-color: rgb(63 63 70);
}

.dark .hero-icon {
  border-color: rgb(59 130 246 / 0.36);
  background: rgb(30 64 175 / 0.2);
  color: rgb(147 197 253);
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

@media (min-width: 640px) {
  .account-share-summary-grid {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }
}

@media (min-width: 1024px) {
  .account-share-hero-head {
    flex-direction: row;
    align-items: center;
    justify-content: space-between;
    padding: 1.125rem 1.25rem;
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
  min-height: 2.5rem;
  min-width: 0;
  align-items: center;
  gap: 0.5rem;
  border-radius: 0.5rem;
  border: 1px solid rgb(191 219 254);
  background: rgb(239 246 255);
  padding: 0.5rem 0.75rem;
  font-size: 0.875rem;
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
  margin-top: 0.625rem;
  display: flex;
  flex-wrap: wrap;
  gap: 0.375rem;
}

.model-copy-chip {
  border-radius: 0.375rem;
  background: rgb(239 246 255);
  padding: 0.1875rem 0.4375rem;
  font-size: 0.6875rem;
  font-weight: 600;
  line-height: 0.9375rem;
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

.model-overflow-wrapper {
  position: relative;
  display: inline-flex;
}

.model-overflow-chip {
  border-radius: 0.375rem;
  background: rgb(243 244 246);
  padding: 0.1875rem 0.4375rem;
  font-size: 0.6875rem;
  font-weight: 700;
  line-height: 0.9375rem;
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
  left: 0;
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
  background: rgb(255 255 255 / 0.92);
  padding: 0.625rem;
  box-shadow: 0 8px 22px rgb(15 23 42 / 0.05);
}

.filter-toolbar {
  display: flex;
  flex-direction: column;
  gap: 0.625rem;
}

.filter-primary-row {
  display: flex;
  min-width: 0;
  flex-direction: column;
  gap: 0.625rem;
}

.filter-search {
  display: flex;
  min-height: 2.375rem;
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
  background: linear-gradient(180deg, rgb(248 250 252), rgb(255 255 255));
  padding: 0.75rem;
}

.filter-body-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
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

.advanced-filter-grid {
  margin-top: 0.75rem;
  display: grid;
  grid-template-columns: 1fr;
  gap: 0.625rem;
}

.filter-field {
  display: flex;
  min-width: 0;
  flex-direction: column;
  gap: 0.375rem;
}

.filter-field > span {
  font-size: 0.75rem;
  font-weight: 800;
  color: rgb(71 85 105);
}

.range-inputs {
  display: grid;
  grid-template-columns: minmax(0, 1fr) auto minmax(0, 1fr);
  align-items: center;
  gap: 0.5rem;
}

.range-inputs span {
  font-size: 0.75rem;
  font-weight: 800;
  color: rgb(100 116 139);
}

.model-filter-field {
  position: relative;
}

.model-filter-menu {
  position: relative;
}

.model-filter-menu summary {
  display: flex;
  min-height: 2.375rem;
  min-width: 0;
  cursor: pointer;
  list-style: none;
  align-items: center;
  gap: 0.5rem;
  border-radius: 0.5rem;
  border: 1px solid rgb(203 213 225);
  background: white;
  padding: 0.4375rem 0.625rem;
  font-size: 0.8125rem;
  font-weight: 800;
  color: rgb(31 41 55);
  transition: border-color 0.15s ease, box-shadow 0.15s ease;
}

.model-filter-menu summary span {
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.model-filter-menu summary::-webkit-details-marker {
  display: none;
}

.model-filter-menu[open] summary {
  border-color: rgb(14 165 233);
  box-shadow: 0 0 0 3px rgb(14 165 233 / 0.14);
}

.model-filter-panel {
  position: absolute;
  top: calc(100% + 0.5rem);
  right: 0;
  z-index: 80;
  width: min(26rem, calc(100vw - 2rem));
  border-radius: 0.5rem;
  border: 1px solid rgb(226 232 240);
  background: white;
  padding: 0.625rem;
  box-shadow: 0 22px 48px rgb(15 23 42 / 0.18);
}

.model-filter-options {
  display: grid;
  max-height: 12rem;
  gap: 0.25rem;
  overflow-y: auto;
  padding-right: 0.25rem;
}

.model-filter-option {
  display: flex;
  min-height: 2.25rem;
  cursor: pointer;
  align-items: center;
  gap: 0.5rem;
  border-radius: 0.5rem;
  padding: 0.375rem 0.5rem;
  font-size: 0.8125rem;
  font-weight: 700;
  color: rgb(55 65 81);
}

.model-filter-option:hover {
  background: rgb(248 250 252);
}

.model-filter-option input {
  height: 1rem;
  width: 1rem;
  accent-color: rgb(2 132 199);
}

.model-filter-option span {
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

.selected-model-filters {
  margin-top: 0.625rem;
  display: flex;
  flex-wrap: wrap;
  gap: 0.375rem;
}

.selected-model-filter {
  display: inline-flex;
  min-width: 0;
  align-items: center;
  gap: 0.375rem;
  border-radius: 0.5rem;
  background: rgb(239 246 255);
  padding: 0.25rem 0.5rem;
  font-size: 0.75rem;
  font-weight: 700;
  color: rgb(29 78 216);
}

.selected-model-filter span {
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.filter-button-row {
  margin-top: 0.75rem;
  display: grid;
  grid-template-columns: 1fr;
  gap: 0.625rem;
}

.filter-apply-button,
.filter-reset-button {
  display: inline-flex;
  min-height: 2.375rem;
  align-items: center;
  justify-content: center;
  border-radius: 0.5rem;
  padding: 0.5rem 0.875rem;
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
  .model-filter-menu summary,
  .filter-apply-button,
  .filter-reset-button {
    min-height: 2.75rem;
  }
}

.dark .filter-panel {
  border-color: rgb(63 63 70);
  background: rgb(24 24 27 / 0.92);
  box-shadow: 0 10px 26px rgb(0 0 0 / 0.22);
}

.dark .filter-search,
.dark .model-filter-menu summary,
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
.dark .filter-field > span {
  color: white;
}

.dark .filter-body-title small,
.dark .range-inputs span {
  color: rgb(161 161 170);
}

.dark .filter-body-icon {
  background: rgb(14 165 233 / 0.16);
  color: rgb(125 211 252);
}

.dark .model-filter-panel {
  border-color: rgb(63 63 70);
  background: rgb(24 24 27);
}

.dark .model-filter-option {
  color: rgb(229 231 235);
}

.dark .model-filter-option:hover {
  background: rgb(39 39 42);
}

.dark .selected-model-filter {
  background: rgb(59 130 246 / 0.12);
  color: rgb(191 219 254);
}

.dark .filter-reset-button:hover {
  border-color: rgb(82 82 91);
  background: rgb(39 39 42);
}

@media (max-width: 640px) {
  .model-filter-panel {
    left: 0;
    right: auto;
    width: min(100%, calc(100vw - 2rem));
  }

  .model-filter-input-row {
    grid-template-columns: 1fr;
  }
}

@media (min-width: 640px) {
  .filter-button-row {
    grid-template-columns: minmax(9rem, 11rem) minmax(7rem, 9rem);
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
    grid-template-columns: repeat(3, minmax(0, 1fr));
  }
}

@media (min-width: 1536px) {
  .advanced-filter-grid {
    grid-template-columns: minmax(8.75rem, 0.72fr) minmax(8.75rem, 0.72fr) repeat(4, minmax(12rem, 1fr)) minmax(13rem, 0.9fr);
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

.listing-card {
  display: flex;
  min-width: 0;
  flex-direction: column;
  border-radius: 0.5rem;
  border: 1px solid rgb(226 232 240);
  background: linear-gradient(180deg, rgb(255 255 255), rgb(248 250 252 / 0.72));
  padding: 1rem;
  box-shadow: 0 12px 28px rgb(15 23 42 / 0.06);
  transition: border-color 0.15s ease, box-shadow 0.15s ease, transform 0.15s ease;
}

.listing-card:hover {
  border-color: rgb(186 230 253);
  box-shadow: 0 18px 38px rgb(15 23 42 / 0.1);
  transform: translateY(-1px);
}

.listing-title {
  color: rgb(17 24 39);
  font-size: 1rem;
  font-weight: 700;
  line-height: 1.375rem;
  overflow-wrap: anywhere;
}

.listing-owner {
  margin-top: 0.25rem;
  color: rgb(107 114 128);
  font-size: 0.75rem;
  line-height: 1.125rem;
  overflow-wrap: anywhere;
}

.dark .listing-card {
  border-color: rgb(63 63 70);
  background: linear-gradient(180deg, rgb(24 24 27), rgb(39 39 42 / 0.48));
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

.feature-badge-waiver {
  background: rgb(255 247 237);
  color: rgb(194 65 12);
}

.listing-health-panel {
  margin-top: 0.875rem;
  display: grid;
  gap: 0.75rem;
  border-top: 1px solid rgb(226 232 240);
  padding-top: 0.875rem;
}

.listing-status-grid {
  display: grid;
  min-width: 0;
  gap: 0.625rem;
}

@media (min-width: 640px) {
  .listing-status-grid {
    grid-template-columns: minmax(0, 1fr) minmax(8.5rem, 10rem);
    align-items: center;
  }
}

.listing-runtime-row {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 0.75rem;
}

.listing-runtime-label {
  display: block;
  font-size: 0.75rem;
  color: rgb(107 114 128);
}

.listing-runtime-row strong {
  display: block;
  margin-top: 0.125rem;
  color: rgb(17 24 39);
  font-size: 0.9375rem;
}

.listing-runtime-row p {
  margin-top: 0.125rem;
  font-size: 0.75rem;
  line-height: 1.125rem;
  color: rgb(107 114 128);
  overflow-wrap: anywhere;
}

.runtime-badge {
  display: inline-flex;
  flex-shrink: 0;
  align-items: center;
  border-radius: 999px;
  padding: 0.25rem 0.625rem;
  font-size: 0.75rem;
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

.usage-window-list {
  display: grid;
  gap: 0.375rem;
}

@media (min-width: 640px) {
  .usage-window-list {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }
}

.usage-window-row {
  display: grid;
  min-width: 0;
  gap: 0.25rem;
  border-radius: 0.5rem;
  background: rgb(248 250 252 / 0.8);
  padding: 0.375rem 0.5rem;
}

.usage-window-title {
  display: flex;
  min-width: 0;
  align-items: center;
  gap: 0.375rem;
  font-size: 0.75rem;
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
  line-height: 1rem;
  text-align: right;
}

.usage-empty {
  font-size: 0.75rem;
  color: rgb(156 163 175);
}

.capacity-panel {
  display: grid;
  gap: 0.25rem;
  align-self: stretch;
  border-radius: 0.5rem;
  border: 1px solid rgb(226 232 240);
  background: white;
  padding: 0.4375rem 0.5625rem;
  font-size: 0.6875rem;
  color: rgb(75 85 99);
}

.capacity-panel strong {
  color: rgb(17 24 39);
  font-weight: 800;
  overflow-wrap: anywhere;
}

.capacity-track {
  height: 0.3125rem;
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
  gap: 0.75rem;
  border-radius: 0.5rem;
  border: 1px solid rgb(167 243 208);
  background: rgb(236 253 245);
  padding: 0.5rem 0.625rem;
  color: rgb(6 95 70);
  font-size: 0.8125rem;
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
  gap: 0.25rem 0.625rem;
  font-size: 0.6875rem;
  color: rgb(107 114 128);
}

.dark .account-level-plus {
  border-color: rgb(129 140 248 / 0.35);
  background: rgb(49 46 129 / 0.4);
  color: rgb(199 210 254);
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

.dark .feature-badge-waiver {
  background: rgb(154 52 18 / 0.25);
  color: rgb(253 186 116);
}

.dark .listing-health-panel {
  border-color: rgb(63 63 70);
}

.dark .listing-runtime-label,
.dark .listing-runtime-row p,
.dark .usage-window-title,
.dark .listing-health-foot {
  color: rgb(161 161 170);
}

.dark .usage-window-row {
  background: rgb(39 39 42 / 0.45);
}

.dark .capacity-panel {
  border-color: rgb(63 63 70);
  background: rgb(24 24 27 / 0.78);
  color: rgb(161 161 170);
}

.dark .listing-runtime-row strong,
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
  margin-top: 1rem;
  border-radius: 0.5rem;
  border: 1px solid rgb(167 243 208);
  background: rgb(236 253 245);
  padding: 0.75rem;
  color: rgb(6 95 70);
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

.membership-status-pill {
  display: inline-flex;
  flex-shrink: 0;
  align-items: center;
  border-radius: 999px;
  background: rgb(16 185 129);
  padding: 0.25rem 0.625rem;
  color: white;
  font-size: 0.75rem;
  font-weight: 700;
  white-space: nowrap;
}

.membership-detail-grid {
  margin-top: 0.75rem;
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(8.5rem, 1fr));
  gap: 0.5rem 0.75rem;
}

.membership-detail-grid div {
  min-width: 0;
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
  color: rgb(6 78 59);
  font-weight: 700;
  overflow-wrap: anywhere;
}

.idle-timeout-control {
  margin-top: 0.75rem;
  display: grid;
  gap: 0.375rem;
}

.idle-timeout-row {
  display: grid;
  grid-template-columns: minmax(5rem, 8rem) auto auto;
  align-items: center;
  gap: 0.5rem;
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
  gap: 0.25rem;
}

.listing-join-section {
  margin-top: 0.75rem;
  display: grid;
  gap: 0.5rem;
}

.edit-lock-strip {
  display: flex;
  min-width: 0;
  align-items: flex-start;
  gap: 0.5rem;
  border-radius: 0.5rem;
  border: 1px solid rgb(253 230 138);
  background: rgb(255 251 235);
  padding: 0.625rem 0.75rem;
  color: rgb(146 64 14);
  font-size: 0.8125rem;
  font-weight: 700;
  line-height: 1.25rem;
}

.edit-lock-strip span {
  min-width: 0;
}

.listing-action-row {
  display: grid;
  gap: 0.5rem;
}

@media (min-width: 640px) {
  .listing-action-row {
    grid-template-columns: minmax(0, 1fr) minmax(8.25rem, 10rem) auto;
    align-items: end;
  }
}

.dark .account-share-membership-panel {
  border-color: rgb(16 185 129 / 0.28);
  background: rgb(6 95 70 / 0.18);
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

.dark .membership-detail-grid span,
.dark .idle-timeout-control label,
.dark .idle-timeout-join span {
  color: rgb(110 231 183);
}

.dark .membership-detail-grid strong,
.dark .idle-timeout-row span {
  color: rgb(209 250 229);
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
  margin-top: 0.75rem;
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 0.375rem;
  font-size: 0.8125rem;
}

@media (min-width: 640px) {
  .listing-metric-grid {
    grid-template-columns: repeat(4, minmax(0, 1fr));
  }
}

.metric {
  display: grid;
  min-width: 0;
  align-content: start;
  gap: 0.125rem;
  border-radius: 0.5rem;
  border: 1px solid rgb(226 232 240);
  background: rgb(255 255 255 / 0.86);
  padding: 0.4375rem 0.5625rem;
}

.metric span {
  min-width: 0;
  font-size: 0.6875rem;
  line-height: 0.9375rem;
  color: rgb(107 114 128);
}

.metric strong {
  display: block;
  min-width: 0;
  color: rgb(17 24 39);
  font-size: 0.8125rem;
  font-weight: 800;
  line-height: 1.0625rem;
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
  font-size: 0.875rem;
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
</style>
