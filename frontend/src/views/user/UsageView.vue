<template>
  <AppLayout>
    <TablePageLayout>
      <template #actions>
        <div v-if="activeTab === 'requests'" class="grid grid-cols-2 gap-4 lg:grid-cols-4">
          <!-- Total Requests -->
          <div class="card p-4">
          <div class="flex items-center gap-3">
            <div class="rounded-lg bg-blue-100 p-2 dark:bg-blue-900/30">
              <Icon name="document" size="md" class="text-blue-600 dark:text-blue-400" />
            </div>
            <div>
              <p class="text-xs font-medium text-gray-500 dark:text-gray-400">
                {{ t('usage.totalRequests') }}
              </p>
              <p class="text-xl font-bold text-gray-900 dark:text-white">
                {{ usageStats?.total_requests?.toLocaleString() || '0' }}
              </p>
              <p class="text-xs text-gray-500 dark:text-gray-400">
                {{ t('usage.inSelectedRange') }}
              </p>
            </div>
          </div>
        </div>

        <!-- Total Tokens -->
        <div class="card p-4">
          <div class="flex items-center gap-3">
            <div class="rounded-lg bg-amber-100 p-2 dark:bg-amber-900/30">
              <Icon name="cube" size="md" class="text-amber-600 dark:text-amber-400" />
            </div>
            <div>
              <p class="text-xs font-medium text-gray-500 dark:text-gray-400">
                {{ t('usage.totalTokens') }}
              </p>
              <p class="text-xl font-bold text-gray-900 dark:text-white">
                {{ formatTokens(usageStats?.total_tokens || 0) }}
              </p>
              <p class="text-xs text-gray-500 dark:text-gray-400">
                {{ t('usage.in') }}: {{ formatTokens(usageStats?.total_input_tokens || 0) }} /
                {{ t('usage.out') }}: {{ formatTokens(usageStats?.total_output_tokens || 0) }}
              </p>
              <p class="text-xs text-gray-500 dark:text-gray-400">
                {{ t('admin.usage.cacheReadTokens') }}: {{ formatTokens(usageStats?.total_cache_read_tokens || 0) }} /
                {{ t('usage.cacheHitRate') }}: {{ formatCacheHitRate(usageStats?.total_input_tokens, usageStats?.total_cache_read_tokens) }}
              </p>
            </div>
          </div>
        </div>

        <!-- Total Cost -->
        <div class="card p-4">
          <div class="flex items-center gap-3">
            <div class="rounded-lg bg-green-100 p-2 dark:bg-green-900/30">
              <Icon name="dollar" size="md" class="text-green-600 dark:text-green-400" />
            </div>
            <div class="min-w-0 flex-1">
              <p class="text-xs font-medium text-gray-500 dark:text-gray-400">
                {{ t('usage.totalCost') }}
              </p>
              <p class="text-xl font-bold text-green-600 dark:text-green-400">
                ${{ (usageStats?.total_actual_cost || 0).toFixed(4) }}
              </p>
              <p class="text-xs text-gray-500 dark:text-gray-400">
                {{ t('usage.actualCost') }} /
                <span class="line-through">${{ (usageStats?.total_cost || 0).toFixed(4) }}</span>
                {{ t('usage.standardCost') }}
              </p>
            </div>
          </div>
        </div>

        <!-- Average Duration -->
        <div class="card p-4">
          <div class="flex items-center gap-3">
            <div class="rounded-lg bg-purple-100 p-2 dark:bg-purple-900/30">
              <Icon name="clock" size="md" class="text-purple-600 dark:text-purple-400" />
            </div>
            <div>
              <p class="text-xs font-medium text-gray-500 dark:text-gray-400">
                {{ t('usage.avgDuration') }}
              </p>
              <p class="text-xl font-bold text-gray-900 dark:text-white">
                {{ formatDuration(usageStats?.average_duration_ms || 0) }}
              </p>
              <p class="text-xs text-gray-500 dark:text-gray-400">{{ t('usage.perRequest') }}</p>
            </div>
          </div>
        </div>
        </div>
      </template>

      <template #filters>
        <div class="card">
          <div class="border-b border-gray-200 p-2 dark:border-dark-700 sm:p-3">
            <div class="grid grid-cols-2 gap-1 rounded-lg bg-gray-100 p-1 dark:bg-dark-800 sm:inline-grid sm:w-auto">
              <button
                type="button"
                class="min-h-11 rounded-md px-4 py-2 text-sm font-medium transition-colors"
                :class="activeTab === 'requests'
                  ? 'bg-white text-gray-900 shadow-sm dark:bg-dark-700 dark:text-white'
                  : 'text-gray-600 hover:text-gray-900 dark:text-dark-300 dark:hover:text-white'"
                @click="switchUsageTab('requests')"
              >
                {{ t('usage.tabs.requests') }}
              </button>
              <button
                type="button"
                class="min-h-11 rounded-md px-4 py-2 text-sm font-medium transition-colors"
                :class="activeTab === 'balanceLedger'
                  ? 'bg-white text-gray-900 shadow-sm dark:bg-dark-700 dark:text-white'
                  : 'text-gray-600 hover:text-gray-900 dark:text-dark-300 dark:hover:text-white'"
                @click="switchUsageTab('balanceLedger')"
              >
                {{ t('usage.tabs.balanceLedger') }}
              </button>
            </div>
          </div>
          <div class="px-6 py-4">
          <div v-if="activeTab === 'requests'" class="flex flex-wrap items-end gap-4">
            <!-- API Key Filter -->
            <div class="min-w-[180px]">
              <label class="input-label">{{ t('usage.apiKeyFilter') }}</label>
              <Select
                v-model="filters.api_key_id"
                :options="apiKeyOptions"
                :placeholder="t('usage.allApiKeys')"
                @change="applyFilters"
              />
            </div>

            <!-- Date Range Filter -->
            <div>
              <label class="input-label">{{ t('usage.timeRange') }}</label>
              <DateRangePicker
                v-model:start-date="startDate"
                v-model:end-date="endDate"
                @change="onDateRangeChange"
              />
            </div>

            <!-- Actions -->
            <div class="ml-auto flex items-center gap-3">
              <button @click="applyFilters" :disabled="loading" class="btn btn-secondary">
                {{ t('common.refresh') }}
              </button>
              <button @click="resetFilters" class="btn btn-secondary">
                {{ t('common.reset') }}
              </button>
              <button @click="exportToCSV" :disabled="exporting" class="btn btn-primary">
                <svg
                  v-if="exporting"
                  class="-ml-1 mr-2 h-4 w-4 animate-spin"
                  fill="none"
                  viewBox="0 0 24 24"
                >
                  <circle
                    class="opacity-25"
                    cx="12"
                    cy="12"
                    r="10"
                    stroke="currentColor"
                    stroke-width="4"
                  ></circle>
                  <path
                    class="opacity-75"
                    fill="currentColor"
                    d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"
                  ></path>
                </svg>
                {{ exporting ? t('usage.exporting') : t('usage.exportCsv') }}
              </button>
            </div>
          </div>
          <div v-else class="flex flex-wrap items-end gap-4">
            <div>
              <label class="input-label">{{ t('usage.timeRange') }}</label>
              <DateRangePicker
                v-model:start-date="startDate"
                v-model:end-date="endDate"
                @change="onDateRangeChange"
              />
            </div>
            <div class="min-w-[160px]">
              <label class="input-label">{{ t('usage.balanceLedger.direction') }}</label>
              <Select
                v-model="ledgerFilters.direction"
                :options="ledgerDirectionOptions"
                @change="applyFilters"
              />
            </div>
            <div class="min-w-[220px]">
              <label class="input-label">{{ t('usage.balanceLedger.reason') }}</label>
              <Select
                v-model="ledgerFilters.reason"
                :options="ledgerReasonOptions"
                @change="applyFilters"
              />
            </div>
            <div class="ml-auto flex items-center gap-3">
              <button @click="applyFilters" :disabled="ledgerLoading" class="btn btn-secondary">
                {{ t('common.refresh') }}
              </button>
              <button @click="resetFilters" class="btn btn-secondary">
                {{ t('common.reset') }}
              </button>
            </div>
          </div>
        </div>
        </div>
      </template>

      <template #table>
        <DataTable
          v-if="activeTab === 'requests'"
          :columns="columns"
          :data="usageLogs"
          :loading="loading"
          :server-side-sort="true"
          default-sort-key="created_at"
          default-sort-order="desc"
          @sort="handleSort"
        >
          <template #cell-api_key="{ row }">
            <span class="text-sm text-gray-900 dark:text-white">{{
              row.api_key?.name || '-'
            }}</span>
          </template>

          <template #cell-model="{ value }">
            <span class="font-medium text-gray-900 dark:text-white">{{ value }}</span>
          </template>

          <template #cell-reasoning_effort="{ row }">
            <span class="text-sm text-gray-900 dark:text-white">
              {{ formatReasoningEffort(row.reasoning_effort) }}
            </span>
          </template>

          <template #cell-endpoint="{ row }">
            <span class="text-sm text-gray-600 dark:text-gray-300 block max-w-[320px] whitespace-normal break-all">
              {{ formatUsageEndpoints(row) }}
            </span>
          </template>

          <template #cell-stream="{ row }">
            <span
              class="inline-flex items-center rounded px-2 py-0.5 text-xs font-medium"
              :class="getRequestTypeBadgeClass(row)"
            >
              {{ getRequestTypeLabel(row) }}
            </span>
          </template>

          <template #cell-billing_mode="{ row }">
            <span class="inline-flex items-center rounded px-1.5 py-0.5 text-xs font-medium"
                  :class="getBillingModeBadgeClass(row.billing_mode)">
              {{ getBillingModeLabel(row.billing_mode, t) }}
            </span>
          </template>

          <template #cell-payment_source="{ row }">
            <div class="flex flex-col gap-1 text-xs">
              <span :class="paymentSourceClass(row)">{{ paymentSourceLabel(row) }}</span>
              <span v-if="walletDeductionText(row)" class="text-gray-500 dark:text-gray-400">{{ walletDeductionText(row) }}</span>
            </div>
          </template>

          <template #cell-tokens="{ row }">
            <!-- 图片生成请求（仅按次计费时显示图片格式） -->
            <div v-if="row.image_count > 0 && row.billing_mode === 'image'" class="flex items-center gap-1.5">
              <svg
                class="h-4 w-4 text-indigo-500"
                fill="none"
                stroke="currentColor"
                viewBox="0 0 24 24"
              >
                <path
                  stroke-linecap="round"
                  stroke-linejoin="round"
                  stroke-width="2"
                  d="M4 16l4.586-4.586a2 2 0 012.828 0L16 16m-2-2l1.586-1.586a2 2 0 012.828 0L20 14m-6-6h.01M6 20h12a2 2 0 002-2V6a2 2 0 00-2-2H6a2 2 0 00-2 2v12a2 2 0 002 2z"
                />
              </svg>
              <span class="font-medium text-gray-900 dark:text-white">{{ row.image_count }}{{ $t('usage.imageUnit') }}</span>
              <span class="text-gray-400">({{ row.image_size || '2K' }})</span>
            </div>
            <!-- Token 请求 -->
            <div v-else class="flex items-center gap-1.5">
              <div class="space-y-1.5 text-sm">
                <!-- Input / Output Tokens -->
                <div class="flex items-center gap-2">
                  <!-- Input -->
                  <div class="inline-flex items-center gap-1">
                    <Icon name="arrowDown" size="sm" class="text-emerald-500" />
                    <span class="font-medium text-gray-900 dark:text-white">{{
                      row.input_tokens.toLocaleString()
                    }}</span>
                  </div>
                  <!-- Output -->
                  <div class="inline-flex items-center gap-1">
                    <Icon name="arrowUp" size="sm" class="text-violet-500" />
                    <span class="font-medium text-gray-900 dark:text-white">{{
                      row.output_tokens.toLocaleString()
                    }}</span>
                  </div>
                </div>
                <!-- Cache Tokens (Read + Write) -->
                <div
                  v-if="row.cache_read_tokens > 0 || row.cache_creation_tokens > 0"
                  class="flex items-center gap-2"
                >
                  <!-- Cache Read -->
                  <div v-if="row.cache_read_tokens > 0" class="inline-flex items-center gap-1">
                    <Icon name="inbox" size="sm" class="text-sky-500" />
                    <span class="font-medium text-sky-600 dark:text-sky-400">{{
                      formatCacheTokens(row.cache_read_tokens)
                    }}</span>
                  </div>
                  <!-- Cache Write -->
                  <div v-if="row.cache_creation_tokens > 0" class="inline-flex items-center gap-1">
                    <Icon name="edit" size="sm" class="text-amber-500" />
                    <span class="font-medium text-amber-600 dark:text-amber-400">{{
                      formatCacheTokens(row.cache_creation_tokens)
                    }}</span>
                    <span v-if="row.cache_creation_1h_tokens > 0" class="inline-flex items-center rounded px-1 py-px text-[10px] font-medium leading-tight bg-orange-100 text-orange-600 ring-1 ring-inset ring-orange-200 dark:bg-orange-500/20 dark:text-orange-400 dark:ring-orange-500/30">1h</span>
                    <span v-if="row.cache_ttl_overridden" :title="t('usage.cacheTtlOverriddenHint')" class="inline-flex items-center rounded px-1 py-px text-[10px] font-medium leading-tight bg-rose-100 text-rose-600 ring-1 ring-inset ring-rose-200 dark:bg-rose-500/20 dark:text-rose-400 dark:ring-rose-500/30 cursor-help">R</span>
                  </div>
                </div>
              </div>
              <!-- Token Detail Tooltip -->
              <div
                class="group relative"
                @mouseenter="showTokenTooltip($event, row)"
                @mouseleave="hideTokenTooltip"
              >
                <div
                  class="flex h-4 w-4 cursor-help items-center justify-center rounded-full bg-gray-100 transition-colors group-hover:bg-blue-100 dark:bg-gray-700 dark:group-hover:bg-blue-900/50"
                >
                  <Icon
                    name="infoCircle"
                    size="xs"
                    class="text-gray-400 group-hover:text-blue-500 dark:text-gray-500 dark:group-hover:text-blue-400"
                  />
                </div>
              </div>
            </div>
          </template>

          <template #cell-cost="{ row }">
            <div class="flex items-start gap-1.5 text-sm">
              <div class="flex flex-col">
                <span class="font-medium text-green-600 dark:text-green-400">
                  ${{ row.actual_cost.toFixed(6) }}
                </span>
                <span v-if="walletDeductionText(row)" class="text-xs text-gray-500 dark:text-gray-400">{{ walletDeductionText(row) }}</span>
              </div>
              <!-- Cost Detail Tooltip -->
              <div
                class="group relative"
                @mouseenter="showTooltip($event, row)"
                @mouseleave="hideTooltip"
              >
                <div
                  class="flex h-4 w-4 cursor-help items-center justify-center rounded-full bg-gray-100 transition-colors group-hover:bg-blue-100 dark:bg-gray-700 dark:group-hover:bg-blue-900/50"
                >
                  <Icon
                    name="infoCircle"
                    size="xs"
                    class="text-gray-400 group-hover:text-blue-500 dark:text-gray-500 dark:group-hover:text-blue-400"
                  />
                </div>
              </div>
            </div>
          </template>

          <template #cell-first_token="{ row }">
            <span
              v-if="row.first_token_ms != null"
              class="text-sm text-gray-600 dark:text-gray-400"
            >
              {{ formatDuration(row.first_token_ms) }}
            </span>
            <span v-else class="text-sm text-gray-400 dark:text-gray-500">-</span>
          </template>

          <template #cell-duration="{ row }">
            <span class="text-sm text-gray-600 dark:text-gray-400">{{
              formatDuration(row.duration_ms)
            }}</span>
          </template>

          <template #cell-created_at="{ value }">
            <span class="text-sm text-gray-600 dark:text-gray-400">{{
              formatDateTime(value)
            }}</span>
          </template>

          <template #cell-user_agent="{ row }">
            <span v-if="row.user_agent" class="text-sm text-gray-600 dark:text-gray-400 block max-w-[320px] whitespace-normal break-all" :title="row.user_agent">{{ formatUserAgent(row.user_agent) }}</span>
            <span v-else class="text-sm text-gray-400 dark:text-gray-500">-</span>
          </template>

          <template #empty>
            <EmptyState :message="t('usage.noRecords')" />
          </template>
        </DataTable>
        <DataTable
          v-else
          :columns="ledgerColumns"
          :data="balanceLedgerRows"
          :loading="ledgerLoading"
          :server-side-sort="true"
          default-sort-key="created_at"
          default-sort-order="desc"
          :estimate-row-height="76"
          :overscan="8"
          row-key="id"
          @sort="handleLedgerSort"
        >
          <template #cell-reason="{ row }">
            <div class="flex flex-col gap-1">
              <span class="font-medium text-gray-900 dark:text-white">{{ row.reasonLabel }}</span>
              <span class="text-xs text-gray-500 dark:text-gray-400">{{ row.reason }}</span>
            </div>
          </template>

          <template #cell-amount="{ row }">
            <span class="font-semibold" :class="row.direction === 'credit' ? 'text-emerald-600 dark:text-emerald-400' : 'text-rose-600 dark:text-rose-400'">
              {{ row.amountLabel }}
            </span>
          </template>

          <template #cell-balance_after="{ row }">
            <span class="font-mono text-sm text-gray-700 dark:text-gray-300">{{ row.balanceAfterLabel }}</span>
          </template>

          <template #cell-remark="{ row }">
            <span class="block max-w-[520px] whitespace-normal text-sm text-gray-600 dark:text-gray-300">
              {{ row.remarkText }}
            </span>
          </template>

          <template #cell-created_at="{ value }">
            <span class="text-sm text-gray-600 dark:text-gray-400">{{
              formatDateTime(value)
            }}</span>
          </template>

          <template #empty>
            <EmptyState :message="t('usage.balanceLedger.noRecords')" />
          </template>
        </DataTable>
      </template>

      <template #pagination>
        <Pagination
          v-if="activeTab === 'requests' && pagination.total > 0"
          :page="pagination.page"
          :total="pagination.total"
          :page-size="pagination.page_size"
          @update:page="handlePageChange"
          @update:pageSize="handlePageSizeChange"
        />
        <Pagination
          v-else-if="activeTab === 'balanceLedger' && ledgerPagination.total > 0"
          :page="ledgerPagination.page"
          :total="ledgerPagination.total"
          :page-size="ledgerPagination.page_size"
          @update:page="handleLedgerPageChange"
          @update:pageSize="handleLedgerPageSizeChange"
        />
      </template>
    </TablePageLayout>
  </AppLayout>

  <!-- Token Tooltip Portal -->
  <Teleport to="body">
    <div
      v-if="tokenTooltipVisible"
      class="fixed z-[9999] pointer-events-none -translate-y-1/2"
      :style="{
        left: tokenTooltipPosition.x + 'px',
        top: tokenTooltipPosition.y + 'px'
      }"
    >
      <div
        class="whitespace-nowrap rounded-lg border border-gray-700 bg-gray-900 px-3 py-2.5 text-xs text-white shadow-xl dark:border-gray-600 dark:bg-gray-800"
      >
        <div class="space-y-1.5">
          <!-- Token Breakdown -->
          <div>
            <div class="text-xs font-semibold text-gray-300 mb-1">{{ t('usage.tokenDetails') }}</div>
            <div v-if="tokenTooltipData && tokenTooltipData.input_tokens > 0" class="flex items-center justify-between gap-4">
              <span class="text-gray-400">{{ t('admin.usage.inputTokens') }}</span>
              <span class="font-medium text-white">{{ tokenTooltipData.input_tokens.toLocaleString() }}</span>
            </div>
            <div v-if="tokenTooltipData && tokenTooltipData.output_tokens > 0" class="flex items-center justify-between gap-4">
              <span class="text-gray-400">{{ t('admin.usage.outputTokens') }}</span>
              <span class="font-medium text-white">{{ tokenTooltipData.output_tokens.toLocaleString() }}</span>
            </div>
            <div v-if="tokenTooltipData && tokenTooltipData.cache_creation_tokens > 0">
              <!-- 有 5m/1h 明细时，展开显示 -->
              <template v-if="tokenTooltipData.cache_creation_5m_tokens > 0 || tokenTooltipData.cache_creation_1h_tokens > 0">
                <div v-if="tokenTooltipData.cache_creation_5m_tokens > 0" class="flex items-center justify-between gap-4">
                  <span class="text-gray-400 flex items-center gap-1.5">
                    {{ t('admin.usage.cacheCreation5mTokens') }}
                    <span class="inline-flex items-center rounded px-1 py-px text-[10px] font-medium leading-tight bg-amber-500/20 text-amber-400 ring-1 ring-inset ring-amber-500/30">5m</span>
                  </span>
                  <span class="font-medium text-white">{{ tokenTooltipData.cache_creation_5m_tokens.toLocaleString() }}</span>
                </div>
                <div v-if="tokenTooltipData.cache_creation_1h_tokens > 0" class="flex items-center justify-between gap-4">
                  <span class="text-gray-400 flex items-center gap-1.5">
                    {{ t('admin.usage.cacheCreation1hTokens') }}
                    <span class="inline-flex items-center rounded px-1 py-px text-[10px] font-medium leading-tight bg-orange-500/20 text-orange-400 ring-1 ring-inset ring-orange-500/30">1h</span>
                  </span>
                  <span class="font-medium text-white">{{ tokenTooltipData.cache_creation_1h_tokens.toLocaleString() }}</span>
                </div>
              </template>
              <!-- 无明细时，只显示聚合值 -->
              <div v-else class="flex items-center justify-between gap-4">
                <span class="text-gray-400">{{ t('admin.usage.cacheCreationTokens') }}</span>
                <span class="font-medium text-white">{{ tokenTooltipData.cache_creation_tokens.toLocaleString() }}</span>
              </div>
            </div>
            <div v-if="tokenTooltipData && tokenTooltipData.cache_ttl_overridden" class="flex items-center justify-between gap-4">
              <span class="text-gray-400 flex items-center gap-1.5">
                {{ t('usage.cacheTtlOverriddenLabel') }}
                <span class="inline-flex items-center rounded px-1 py-px text-[10px] font-medium leading-tight bg-rose-500/20 text-rose-400 ring-1 ring-inset ring-rose-500/30">R-{{ tokenTooltipData.cache_creation_1h_tokens > 0 ? '5m' : '1H' }}</span>
              </span>
              <span class="font-medium text-rose-400">{{ tokenTooltipData.cache_creation_1h_tokens > 0 ? t('usage.cacheTtlOverridden1h') : t('usage.cacheTtlOverridden5m') }}</span>
            </div>
            <div v-if="tokenTooltipData && tokenTooltipData.cache_read_tokens > 0" class="flex items-center justify-between gap-4">
              <span class="text-gray-400">{{ t('admin.usage.cacheReadTokens') }}</span>
              <span class="font-medium text-white">{{ tokenTooltipData.cache_read_tokens.toLocaleString() }}</span>
            </div>
            <div v-if="tokenTooltipData" class="flex items-center justify-between gap-4">
              <span class="text-gray-400">{{ t('usage.cacheHitRate') }}</span>
              <span class="font-medium text-cyan-300">{{ formatCacheHitRate(tokenTooltipData.input_tokens, tokenTooltipData.cache_read_tokens) }}</span>
            </div>
          </div>
          <!-- Total -->
          <div class="flex items-center justify-between gap-6 border-t border-gray-700 pt-1.5">
            <span class="text-gray-400">{{ t('usage.totalTokens') }}</span>
            <span class="font-semibold text-blue-400">{{ ((tokenTooltipData?.input_tokens || 0) + (tokenTooltipData?.output_tokens || 0) + (tokenTooltipData?.cache_creation_tokens || 0) + (tokenTooltipData?.cache_read_tokens || 0)).toLocaleString() }}</span>
          </div>
        </div>
        <!-- Tooltip Arrow (left side) -->
        <div
          class="absolute right-full top-1/2 h-0 w-0 -translate-y-1/2 border-b-[6px] border-r-[6px] border-t-[6px] border-b-transparent border-r-gray-900 border-t-transparent dark:border-r-gray-800"
        ></div>
      </div>
    </div>
  </Teleport>

  <!-- Tooltip Portal -->
  <Teleport to="body">
    <div
      v-if="tooltipVisible"
      class="fixed z-[9999] pointer-events-none -translate-y-1/2"
      :style="{
        left: tooltipPosition.x + 'px',
        top: tooltipPosition.y + 'px'
      }"
    >
      <div
        class="whitespace-nowrap rounded-lg border border-gray-700 bg-gray-900 px-3 py-2.5 text-xs text-white shadow-xl dark:border-gray-600 dark:bg-gray-800"
      >
        <div class="space-y-1.5">
          <!-- Cost Breakdown -->
          <div class="mb-2 border-b border-gray-700 pb-1.5">
            <div class="text-xs font-semibold text-gray-300 mb-1">{{ t('usage.costDetails') }}</div>
            <div v-if="tooltipData && tooltipData.input_cost > 0" class="flex items-center justify-between gap-4">
              <span class="text-gray-400">{{ t('admin.usage.inputCost') }}</span>
              <span class="font-medium text-white">${{ tooltipData.input_cost.toFixed(6) }}</span>
            </div>
            <div v-if="tooltipData && tooltipData.output_cost > 0" class="flex items-center justify-between gap-4">
              <span class="text-gray-400">{{ t('admin.usage.outputCost') }}</span>
              <span class="font-medium text-white">${{ tooltipData.output_cost.toFixed(6) }}</span>
            </div>
            <!-- Token billing: show unit prices per 1M tokens -->
            <template v-if="!tooltipData?.billing_mode || tooltipData.billing_mode === 'token'">
              <div v-if="tooltipData && tooltipData.input_tokens > 0" class="flex items-center justify-between gap-4">
                <span class="text-gray-400">{{ t('usage.inputTokenPrice') }}</span>
                <span class="font-medium text-sky-300">{{ formatTokenPricePerMillion(tooltipData.input_cost, tooltipData.input_tokens) }} {{ t('usage.perMillionTokens') }}</span>
              </div>
              <div v-if="tooltipData && tooltipData.output_tokens > 0" class="flex items-center justify-between gap-4">
                <span class="text-gray-400">{{ t('usage.outputTokenPrice') }}</span>
                <span class="font-medium text-violet-300">{{ formatTokenPricePerMillion(tooltipData.output_cost, tooltipData.output_tokens) }} {{ t('usage.perMillionTokens') }}</span>
              </div>
            </template>
            <!-- Per-request / image billing: show unit price -->
            <div v-else class="flex items-center justify-between gap-4">
              <span class="text-gray-400">{{ tooltipData.billing_mode === 'image' ? t('usage.imageUnitPrice') : t('usage.unitPrice') }}</span>
              <span class="font-medium text-sky-300">${{ tooltipData.total_cost?.toFixed(6) || '0.000000' }}</span>
            </div>
            <div v-if="tooltipData && tooltipData.cache_creation_cost > 0" class="flex items-center justify-between gap-4">
              <span class="text-gray-400">{{ t('admin.usage.cacheCreationCost') }}</span>
              <span class="font-medium text-white">${{ tooltipData.cache_creation_cost.toFixed(6) }}</span>
            </div>
            <div v-if="tooltipData && tooltipData.cache_read_cost > 0" class="flex items-center justify-between gap-4">
              <span class="text-gray-400">{{ t('admin.usage.cacheReadCost') }}</span>
              <span class="font-medium text-white">${{ tooltipData.cache_read_cost.toFixed(6) }}</span>
            </div>
          </div>
          <!-- Rate and Summary -->
          <div class="flex items-center justify-between gap-6">
            <span class="text-gray-400">{{ t('usage.serviceTier') }}</span>
            <span class="font-semibold text-cyan-300">{{ getUsageServiceTierLabel(tooltipData?.service_tier, t) }}</span>
          </div>
          <div class="flex items-center justify-between gap-6">
            <span class="text-gray-400">{{ t('usage.rate') }}</span>
            <span class="font-semibold text-blue-400"
              >{{ formatMultiplier(tooltipData?.rate_multiplier || 1) }}x</span
            >
          </div>
          <div class="flex items-center justify-between gap-6">
            <span class="text-gray-400">{{ t('usage.original') }}</span>
            <span class="font-medium text-white">${{ tooltipData?.total_cost.toFixed(6) }}</span>
          </div>
          <div class="flex items-center justify-between gap-6 border-t border-gray-700 pt-1.5">
            <span class="text-gray-400">{{ t('usage.billed') }}</span>
            <span class="font-semibold text-green-400"
              >${{ tooltipData?.actual_cost.toFixed(6) }}</span
            >
          </div>
        </div>
        <!-- Tooltip Arrow (left side) -->
        <div
          class="absolute right-full top-1/2 h-0 w-0 -translate-y-1/2 border-b-[6px] border-r-[6px] border-t-[6px] border-b-transparent border-r-gray-900 border-t-transparent dark:border-r-gray-800"
        ></div>
      </div>
    </div>
  </Teleport>
</template>

<script setup lang="ts">
import { ref, computed, reactive, onMounted, onUnmounted } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAppStore } from '@/stores/app'
import { usageAPI, keysAPI } from '@/api'
import AppLayout from '@/components/layout/AppLayout.vue'
import TablePageLayout from '@/components/layout/TablePageLayout.vue'
import DataTable from '@/components/common/DataTable.vue'
import Pagination from '@/components/common/Pagination.vue'
import EmptyState from '@/components/common/EmptyState.vue'
import Select from '@/components/common/Select.vue'
import DateRangePicker from '@/components/common/DateRangePicker.vue'
import Icon from '@/components/icons/Icon.vue'
import type {
  ApiKey,
  BalanceLedgerDirection,
  UsageLog,
  UsageQueryParams,
  UsageStatsResponse,
  UserBalanceLedgerEntry,
  UserBalanceLedgerQueryParams
} from '@/types'
import type { Column } from '@/components/common/types'
import { formatDateTime, formatReasoningEffort } from '@/utils/format'
import { getPersistedPageSize } from '@/composables/usePersistedPageSize'
import { formatCacheHitRate, formatCacheTokens, formatMultiplier } from '@/utils/formatters'
import { formatTokenPricePerMillion } from '@/utils/usagePricing'
import { getUsageServiceTierLabel } from '@/utils/usageServiceTier'
import { resolveUsageRequestType } from '@/utils/usageRequestType'
import { getBillingModeLabel, getBillingModeBadgeClass } from '@/utils/billingMode'

const { t } = useI18n()
const appStore = useAppStore()

let abortController: AbortController | null = null
let ledgerAbortController: AbortController | null = null
let ledgerPrefetchTimer: number | null = null

type UsageTab = 'requests' | 'balanceLedger'
type BalanceLedgerTableRow = UserBalanceLedgerEntry & {
  reasonLabel: string
  amountLabel: string
  balanceAfterLabel: string
  remarkText: string
}

const activeTab = ref<UsageTab>('requests')

// Tooltip state
const tooltipVisible = ref(false)
const tooltipPosition = ref({ x: 0, y: 0 })
const tooltipData = ref<UsageLog | null>(null)

// Token tooltip state
const tokenTooltipVisible = ref(false)
const tokenTooltipPosition = ref({ x: 0, y: 0 })
const tokenTooltipData = ref<UsageLog | null>(null)

// Usage stats from API
const usageStats = ref<UsageStatsResponse | null>(null)

const columns = computed<Column[]>(() => [
  { key: 'api_key', label: t('usage.apiKeyFilter'), sortable: false },
  { key: 'model', label: t('usage.model'), sortable: true },
  { key: 'reasoning_effort', label: t('usage.reasoningEffort'), sortable: false },
  { key: 'endpoint', label: t('usage.endpoint'), sortable: false },
  { key: 'stream', label: t('usage.type'), sortable: false },
  { key: 'billing_mode', label: t('admin.usage.billingMode'), sortable: false },
  { key: 'payment_source', label: t('usage.paymentSource'), sortable: false },
  { key: 'tokens', label: t('usage.tokens'), sortable: false },
  { key: 'cost', label: t('usage.cost'), sortable: false },
  { key: 'first_token', label: t('usage.firstToken'), sortable: false },
  { key: 'duration', label: t('usage.duration'), sortable: false },
  { key: 'created_at', label: t('usage.time'), sortable: true },
  { key: 'user_agent', label: t('usage.userAgent'), sortable: false }
])

const ledgerColumns = computed<Column[]>(() => [
  { key: 'reason', label: t('usage.balanceLedger.reason'), sortable: false },
  { key: 'amount', label: t('usage.balanceLedger.amount'), sortable: false },
  { key: 'balance_after', label: t('usage.balanceLedger.balanceAfter'), sortable: false },
  { key: 'remark', label: t('usage.balanceLedger.remark'), sortable: false },
  { key: 'created_at', label: t('usage.time'), sortable: true }
])

const usageLogs = ref<UsageLog[]>([])
const balanceLedger = ref<UserBalanceLedgerEntry[]>([])
const apiKeys = ref<ApiKey[]>([])
const loading = ref(false)
const ledgerLoading = ref(false)
const ledgerLoaded = ref(false)
const exporting = ref(false)

const apiKeyOptions = computed(() => {
  return [
    { value: null, label: t('usage.allApiKeys') },
    ...apiKeys.value.map((key) => ({
      value: key.id,
      label: key.name
    }))
  ]
})

const ledgerDirectionOptions = computed(() => [
  { value: '', label: t('usage.balanceLedger.allDirections') },
  { value: 'debit', label: t('usage.balanceLedger.debit') },
  { value: 'credit', label: t('usage.balanceLedger.credit') }
])

const ledgerReasonOptions = computed(() => [
  { value: '', label: t('usage.balanceLedger.allReasons') },
  { value: 'usage_charge', label: t('usage.balanceLedger.reasons.usage_charge') },
  { value: 'private_group_commission', label: t('usage.balanceLedger.reasons.private_group_commission') },
  { value: 'account_share_mode_seat_prepay', label: t('usage.balanceLedger.reasons.account_share_mode_seat_prepay') },
  { value: 'account_share_mode_seat_refund', label: t('usage.balanceLedger.reasons.account_share_mode_seat_refund') },
  { value: 'account_share_mode_seat_waiver_refund', label: t('usage.balanceLedger.reasons.account_share_mode_seat_waiver_refund') },
  { value: 'account_share_mode_income', label: t('usage.balanceLedger.reasons.account_share_mode_income') },
  { value: 'account_share_income', label: t('usage.balanceLedger.reasons.account_share_income') },
  { value: 'invite_share_income', label: t('usage.balanceLedger.reasons.invite_share_income') },
  { value: 'redeem_code', label: t('usage.balanceLedger.reasons.redeem_code') },
  { value: 'admin_adjustment', label: t('usage.balanceLedger.reasons.admin_adjustment') }
])

const ledgerReasonLabelMap = computed<Record<string, string>>(() => ({
  usage_charge: t('usage.balanceLedger.reasons.usage_charge'),
  private_group_commission: t('usage.balanceLedger.reasons.private_group_commission'),
  account_share_mode_seat_prepay: t('usage.balanceLedger.reasons.account_share_mode_seat_prepay'),
  account_share_mode_seat_refund: t('usage.balanceLedger.reasons.account_share_mode_seat_refund'),
  account_share_mode_seat_waiver_refund: t('usage.balanceLedger.reasons.account_share_mode_seat_waiver_refund'),
  account_share_mode_income: t('usage.balanceLedger.reasons.account_share_mode_income'),
  account_share_income: t('usage.balanceLedger.reasons.account_share_income'),
  invite_share_income: t('usage.balanceLedger.reasons.invite_share_income'),
  redeem_code: t('usage.balanceLedger.reasons.redeem_code'),
  admin_adjustment: t('usage.balanceLedger.reasons.admin_adjustment')
}))

const epsilon = 1e-9

function formatPointsAmount(value?: number | null) {
  const amount = Number(value || 0)
  if (!Number.isFinite(amount) || Math.abs(amount) <= epsilon) return '0'
  return amount.toFixed(10).replace(/\.?0+$/, '') || '0'
}

function billingWalletType(row: UsageLog) {
  if (row.billing_wallet_type) return row.billing_wallet_type
  if (row.billing_type === 1) return 'subscription'
  const points = Number(row.points_deducted || 0)
  const balance = Number(row.balance_deducted || 0)
  if (points > epsilon && balance > epsilon) return 'mixed'
  if (points > epsilon) return 'points'
  if (balance > epsilon) return 'balance'
  return 'none'
}

function paymentSourceLabel(row: UsageLog) {
  switch (billingWalletType(row)) {
    case 'subscription':
      return t('usage.paymentSources.subscription')
    case 'points':
      return t('usage.paymentSources.points')
    case 'balance':
      return t('usage.paymentSources.balance')
    case 'mixed':
      return t('usage.paymentSources.mixed')
    default:
      return t('usage.paymentSources.none')
  }
}

function paymentSourceClass(row: UsageLog) {
  switch (billingWalletType(row)) {
    case 'subscription':
      return 'font-medium text-purple-600 dark:text-purple-300'
    case 'points':
      return 'font-medium text-amber-600 dark:text-amber-300'
    case 'balance':
      return 'font-medium text-green-600 dark:text-green-300'
    case 'mixed':
      return 'font-medium text-blue-600 dark:text-blue-300'
    default:
      return 'font-medium text-gray-500 dark:text-gray-400'
  }
}

function walletDeductionText(row: UsageLog) {
  const points = Number(row.points_deducted || 0)
  const balance = Number(row.balance_deducted || 0)
  const parts: string[] = []
  if (points > epsilon) parts.push(`${formatPointsAmount(points)} ${t('common.points')}`)
  if (balance > epsilon) parts.push(`$${balance.toFixed(6)}`)
  return parts.join(' + ')
}

// Helper function to format date in local timezone
const formatLocalDate = (date: Date): string => {
  return `${date.getFullYear()}-${String(date.getMonth() + 1).padStart(2, '0')}-${String(date.getDate()).padStart(2, '0')}`
}

// Initialize date range immediately
const now = new Date()
const weekAgo = new Date(now)
weekAgo.setDate(weekAgo.getDate() - 6)

// Date range state
const startDate = ref(formatLocalDate(weekAgo))
const endDate = ref(formatLocalDate(now))

const filters = ref<UsageQueryParams>({
  api_key_id: undefined,
  start_date: undefined,
  end_date: undefined
})

const ledgerFilters = ref<{
  direction: '' | BalanceLedgerDirection
  reason: string
}>({
  direction: '',
  reason: ''
})

// Initialize filters with date range
filters.value.start_date = startDate.value
filters.value.end_date = endDate.value

// Handle date range change from DateRangePicker
const onDateRangeChange = (range: {
  startDate: string
  endDate: string
  preset: string | null
}) => {
  filters.value.start_date = range.startDate
  filters.value.end_date = range.endDate
  applyFilters()
}

const pagination = reactive({
  page: 1,
  page_size: getPersistedPageSize(),
  total: 0,
  pages: 0
})
const ledgerPagination = reactive({
  page: 1,
  page_size: getPersistedPageSize(),
  total: 0,
  pages: 0
})
const sortState = reactive({
  sort_by: 'created_at',
  sort_order: 'desc' as 'asc' | 'desc'
})
const ledgerSortState = reactive({
  sort_order: 'desc' as 'asc' | 'desc'
})

const formatDuration = (ms: number): string => {
  if (ms < 1000) return `${ms.toFixed(0)}ms`
  return `${(ms / 1000).toFixed(2)}s`
}

const formatUserAgent = (ua: string): string => {
  return ua
}

const getRequestTypeLabel = (log: UsageLog): string => {
  const requestType = resolveUsageRequestType(log)
  if (requestType === 'ws_v2') return t('usage.ws')
  if (requestType === 'stream') return t('usage.stream')
  if (requestType === 'sync') return t('usage.sync')
  return t('usage.unknown')
}

const getRequestTypeBadgeClass = (log: UsageLog): string => {
  const requestType = resolveUsageRequestType(log)
  if (requestType === 'ws_v2') return 'bg-violet-100 text-violet-800 dark:bg-violet-900 dark:text-violet-200'
  if (requestType === 'stream') return 'bg-blue-100 text-blue-800 dark:bg-blue-900 dark:text-blue-200'
  if (requestType === 'sync') return 'bg-gray-100 text-gray-800 dark:bg-gray-700 dark:text-gray-200'
  return 'bg-amber-100 text-amber-800 dark:bg-amber-900 dark:text-amber-200'
}


const getRequestTypeExportText = (log: UsageLog): string => {
  const requestType = resolveUsageRequestType(log)
  if (requestType === 'ws_v2') return 'WS'
  if (requestType === 'stream') return 'Stream'
  if (requestType === 'sync') return 'Sync'
  return 'Unknown'
}

const formatUsageEndpoints = (log: UsageLog): string => {
  const inbound = log.inbound_endpoint?.trim()
  return inbound || '-'
}

const formatTokens = (value: number): string => {
  if (value >= 1_000_000_000) {
    return `${(value / 1_000_000_000).toFixed(2)}B`
  } else if (value >= 1_000_000) {
    return `${(value / 1_000_000).toFixed(2)}M`
  } else if (value >= 1_000) {
    return `${(value / 1_000).toFixed(2)}K`
  }
  return value.toLocaleString()
}

type UsageTableQueryParams = UsageQueryParams & {
  sort_by?: string
  sort_order?: 'asc' | 'desc'
}

const buildUsageQueryParams = (page: number, pageSize: number): UsageTableQueryParams => ({
  page,
  page_size: pageSize,
  ...filters.value,
  sort_by: sortState.sort_by,
  sort_order: sortState.sort_order
})

const buildBalanceLedgerQueryParams = (page: number, pageSize: number): UserBalanceLedgerQueryParams => ({
  page,
  page_size: pageSize,
  direction: ledgerFilters.value.direction,
  reason: ledgerFilters.value.reason || undefined,
  start_date: filters.value.start_date || startDate.value,
  end_date: filters.value.end_date || endDate.value,
  sort_order: ledgerSortState.sort_order
})

const loadBalanceLedger = async () => {
  if (ledgerAbortController) {
    ledgerAbortController.abort()
  }
  const currentAbortController = new AbortController()
  ledgerAbortController = currentAbortController
  const { signal } = currentAbortController
  ledgerLoading.value = true
  try {
    const response = await usageAPI.queryBalanceLedger(
      buildBalanceLedgerQueryParams(ledgerPagination.page, ledgerPagination.page_size),
      { signal }
    )
    if (signal.aborted) {
      return
    }
    balanceLedger.value = response.items
    ledgerPagination.total = response.total
    ledgerPagination.pages = response.pages
    ledgerLoaded.value = true
  } catch (error) {
    if (signal.aborted) {
      return
    }
    const abortError = error as { name?: string; code?: string }
    if (abortError?.name === 'AbortError' || abortError?.code === 'ERR_CANCELED') {
      return
    }
    appStore.showError(t('usage.balanceLedger.failedToLoad'))
  } finally {
    if (ledgerAbortController === currentAbortController) {
      ledgerLoading.value = false
    }
  }
}

const loadUsageLogs = async () => {
  if (abortController) {
    abortController.abort()
  }
  const currentAbortController = new AbortController()
  abortController = currentAbortController
  const { signal } = currentAbortController
  loading.value = true
  try {
    const response = await usageAPI.query(
      buildUsageQueryParams(pagination.page, pagination.page_size),
      { signal }
    )
    if (signal.aborted) {
      return
    }
    usageLogs.value = response.items
    pagination.total = response.total
    pagination.pages = response.pages
  } catch (error) {
    if (signal.aborted) {
      return
    }
    const abortError = error as { name?: string; code?: string }
    if (abortError?.name === 'AbortError' || abortError?.code === 'ERR_CANCELED') {
      return
    }
    appStore.showError(t('usage.failedToLoad'))
  } finally {
    if (abortController === currentAbortController) {
      loading.value = false
    }
  }
}

const switchUsageTab = (tab: UsageTab) => {
  if (activeTab.value === tab) return
  activeTab.value = tab
  if (tab === 'balanceLedger') {
    if (!ledgerLoaded.value && !ledgerLoading.value) {
      ledgerPagination.page = 1
      loadBalanceLedger()
    }
    return
  }
  pagination.page = 1
  loadUsageLogs()
  loadUsageStats()
}

const abortBalanceLedgerRequest = () => {
  if (ledgerAbortController) {
    ledgerAbortController.abort()
  }
}

const clearBalanceLedgerPrefetch = () => {
  if (typeof window === 'undefined' || ledgerPrefetchTimer === null) return
  window.clearTimeout(ledgerPrefetchTimer)
  ledgerPrefetchTimer = null
}

const invalidateBalanceLedgerCache = () => {
  ledgerLoaded.value = false
  clearBalanceLedgerPrefetch()
  abortBalanceLedgerRequest()
  ledgerLoading.value = false
}

const scheduleBalanceLedgerPrefetch = () => {
  if (typeof window === 'undefined') return
  if (ledgerLoaded.value || ledgerLoading.value || ledgerPrefetchTimer !== null) return
  ledgerPrefetchTimer = window.setTimeout(() => {
    ledgerPrefetchTimer = null
    if (activeTab.value !== 'requests' || ledgerLoaded.value || ledgerLoading.value) return
    loadBalanceLedger()
  }, 400)
}

const loadApiKeys = async () => {
  try {
    const response = await keysAPI.list(1, 100)
    apiKeys.value = response.items
  } catch (error) {
    console.error('Failed to load API keys:', error)
  }
}

const loadUsageStats = async () => {
  try {
    const apiKeyId = filters.value.api_key_id ? Number(filters.value.api_key_id) : undefined
    const stats = await usageAPI.getStatsByDateRange(
      filters.value.start_date || startDate.value,
      filters.value.end_date || endDate.value,
      apiKeyId
    )
    usageStats.value = stats
  } catch (error) {
    console.error('Failed to load usage stats:', error)
  }
}

const applyFilters = () => {
  if (activeTab.value === 'balanceLedger') {
    ledgerPagination.page = 1
    loadBalanceLedger()
    return
  }
  invalidateBalanceLedgerCache()
  pagination.page = 1
  loadUsageLogs()
  loadUsageStats()
}

const resetFilters = () => {
  filters.value = {
    api_key_id: undefined,
    start_date: undefined,
    end_date: undefined
  }
  // Reset date range to default (last 7 days)
  const now = new Date()
  const weekAgo = new Date(now)
  weekAgo.setDate(weekAgo.getDate() - 6)
  startDate.value = formatLocalDate(weekAgo)
  endDate.value = formatLocalDate(now)
  filters.value.start_date = startDate.value
  filters.value.end_date = endDate.value
  ledgerFilters.value = {
    direction: '',
    reason: ''
  }
  pagination.page = 1
  ledgerPagination.page = 1
  invalidateBalanceLedgerCache()
  if (activeTab.value === 'balanceLedger') {
    loadBalanceLedger()
    return
  }
  loadUsageLogs()
  loadUsageStats()
}

const handlePageChange = (page: number) => {
  pagination.page = page
  loadUsageLogs()
}

const handlePageSizeChange = (pageSize: number) => {
  pagination.page_size = pageSize
  pagination.page = 1
  loadUsageLogs()
}

const handleSort = (key: string, order: 'asc' | 'desc') => {
  sortState.sort_by = key
  sortState.sort_order = order
  pagination.page = 1
  loadUsageLogs()
}

const handleLedgerPageChange = (page: number) => {
  ledgerPagination.page = page
  loadBalanceLedger()
}

const handleLedgerPageSizeChange = (pageSize: number) => {
  ledgerPagination.page_size = pageSize
  ledgerPagination.page = 1
  loadBalanceLedger()
}

const handleLedgerSort = (_key: string, order: 'asc' | 'desc') => {
  ledgerSortState.sort_order = order
  ledgerPagination.page = 1
  loadBalanceLedger()
}

const getLedgerReasonLabel = (reason: string): string => {
  return ledgerReasonLabelMap.value[reason] || reason || t('usage.unknown')
}

const normalizeLedgerDecimal = (value?: string | number | null, digits = 10): string => {
  if (value === undefined || value === null || value === '') return '0'
  const raw = String(value).trim()
  const match = raw.match(/^(-?)(\d+)(?:\.(\d+))?$/)
  if (!match) {
    const amount = Number(value)
    if (!Number.isFinite(amount)) return '0'
    return amount.toFixed(digits).replace(/\.?0+$/, '') || '0'
  }
  const sign = match[1]
  const integer = match[2].replace(/^0+(?=\d)/, '') || '0'
  const fraction = (match[3] || '').slice(0, digits).replace(/0+$/, '')
  const normalized = fraction ? `${integer}.${fraction}` : integer
  if (normalized === '0') return '0'
  return `${sign}${normalized}`
}

const absoluteLedgerDecimal = (value?: string | number | null): string => {
  return normalizeLedgerDecimal(value).replace(/^-/, '')
}

const formatLedgerCurrency = (value?: string | number | null, digits = 10): string => {
  const normalized = normalizeLedgerDecimal(value, digits)
  return normalized.startsWith('-')
    ? `-$${normalized.slice(1)}`
    : `$${normalized}`
}

const formatOptionalLedgerCurrency = (value?: number | null, digits = 10): string => {
  return value == null ? '' : formatLedgerCurrency(value, digits)
}

const formatLedgerAmount = (row: UserBalanceLedgerEntry): string => {
  const sign = row.direction === 'credit' ? '+' : '-'
  return `${sign}${formatLedgerCurrency(absoluteLedgerDecimal(row.amount))}`
}

const metadataValue = (row: UserBalanceLedgerEntry, key: string): unknown => {
  return row.metadata ? row.metadata[key] : undefined
}

const metadataString = (row: UserBalanceLedgerEntry, key: string): string => {
  const value = metadataValue(row, key)
  if (value === undefined || value === null) return ''
  return String(value)
}

const metadataNumber = (row: UserBalanceLedgerEntry, key: string): number | null => {
  const value = Number(metadataValue(row, key))
  return Number.isFinite(value) ? value : null
}

const formatMetadataDateTime = (row: UserBalanceLedgerEntry, key: string): string => {
  const value = metadataString(row, key)
  return value ? formatDateTime(value) : ''
}

const formatLedgerDuration = (milliseconds?: number | null): string => {
  const ms = Number(milliseconds || 0)
  if (!Number.isFinite(ms) || ms <= 0) return ''
  const minutes = ms / 60000
  if (minutes < 1) return `${Math.round(ms / 1000)}s`
  if (minutes < 60) return `${Number(minutes.toFixed(1))}m`
  const hours = minutes / 60
  if (hours < 24) return `${Number(hours.toFixed(2))}h`
  const days = hours / 24
  return `${Number(days.toFixed(2))}d`
}

const ledgerPart = (labelKey: string, value: string | number | null | undefined): string => {
  if (value === undefined || value === null || value === '') return ''
  return `${t(labelKey)}: ${value}`
}

const compactLedgerParts = (parts: string[]): string => {
  const clean = parts.filter(Boolean)
  return clean.length > 0 ? clean.join(' · ') : t('usage.balanceLedger.noRemark')
}

const getLedgerStageLabel = (stage: string): string => {
  if (stage === 'renew') return t('usage.balanceLedger.stages.renew')
  if (stage === 'join') return t('usage.balanceLedger.stages.join')
  return stage
}

const getLedgerRemark = (row: UserBalanceLedgerEntry): string => {
  const duration = formatLedgerDuration(metadataNumber(row, 'duration_ms'))
  const rate = metadataNumber(row, 'hourly_rate')
  const periodStarted = metadataString(row, 'period_started')
  const periodEnded = metadataString(row, 'period_ended')
  const period = periodStarted && periodEnded
    ? `${formatDateTime(periodStarted)} - ${formatDateTime(periodEnded)}`
    : ''

  switch (row.reason) {
    case 'usage_charge':
      return compactLedgerParts([
        ledgerPart('usage.balanceLedger.labels.requestId', metadataString(row, 'request_id')),
        ledgerPart('usage.balanceLedger.labels.apiKey', metadataString(row, 'api_key_id')),
        ledgerPart('usage.balanceLedger.labels.account', metadataString(row, 'account_id'))
      ])
    case 'private_group_commission':
      return compactLedgerParts([
        ledgerPart('usage.balanceLedger.labels.group', metadataString(row, 'group_id')),
        ledgerPart('usage.balanceLedger.labels.baseCost', formatOptionalLedgerCurrency(metadataNumber(row, 'base_cost'))),
        ledgerPart('usage.balanceLedger.labels.requestId', metadataString(row, 'request_id'))
      ])
    case 'account_share_mode_seat_prepay':
      return compactLedgerParts([
        ledgerPart('usage.balanceLedger.labels.stage', getLedgerStageLabel(metadataString(row, 'prepay_stage'))),
        ledgerPart('usage.balanceLedger.labels.hourlyRate', rate == null ? '' : formatLedgerCurrency(rate, 4)),
        ledgerPart('usage.balanceLedger.labels.duration', duration),
        ledgerPart('usage.balanceLedger.labels.paidUntil', formatMetadataDateTime(row, 'paid_until')),
        ledgerPart('usage.balanceLedger.labels.account', metadataString(row, 'account_id')),
        ledgerPart('usage.balanceLedger.labels.membership', metadataString(row, 'membership_id'))
      ])
    case 'account_share_mode_seat_refund':
      return compactLedgerParts([
        ledgerPart('usage.balanceLedger.labels.hourlyRate', rate == null ? '' : formatLedgerCurrency(rate, 4)),
        ledgerPart('usage.balanceLedger.labels.duration', duration),
        ledgerPart('usage.balanceLedger.labels.refundUntil', formatMetadataDateTime(row, 'refund_until')),
        ledgerPart('usage.balanceLedger.labels.account', metadataString(row, 'account_id'))
      ])
    case 'account_share_mode_seat_waiver_refund':
      return compactLedgerParts([
        ledgerPart('usage.balanceLedger.labels.period', period),
        ledgerPart('usage.balanceLedger.labels.duration', duration),
        ledgerPart('usage.balanceLedger.labels.waiverRequired', formatOptionalLedgerCurrency(metadataNumber(row, 'waiver_required'))),
        ledgerPart('usage.balanceLedger.labels.waiverUsage', formatOptionalLedgerCurrency(metadataNumber(row, 'waiver_usage'))),
        ledgerPart('usage.balanceLedger.labels.account', metadataString(row, 'account_id'))
      ])
    case 'account_share_mode_income':
      return compactLedgerParts([
        ledgerPart('usage.balanceLedger.labels.consumer', metadataString(row, 'consumer_user_id')),
        ledgerPart('usage.balanceLedger.labels.period', period),
        ledgerPart('usage.balanceLedger.labels.settlement', metadataString(row, 'settlement_id')),
        ledgerPart('usage.balanceLedger.labels.account', metadataString(row, 'account_id')),
        ledgerPart('usage.balanceLedger.labels.requestId', metadataString(row, 'request_id'))
      ])
    case 'account_share_income':
    case 'invite_share_income':
      return compactLedgerParts([
        ledgerPart('usage.balanceLedger.labels.consumer', metadataString(row, 'consumer_user_id')),
        ledgerPart('usage.balanceLedger.labels.apiKey', metadataString(row, 'api_key_id')),
        ledgerPart('usage.balanceLedger.labels.account', metadataString(row, 'account_id')),
        ledgerPart('usage.balanceLedger.labels.requestId', metadataString(row, 'request_id'))
      ])
    case 'redeem_code':
      return compactLedgerParts([
        ledgerPart('usage.balanceLedger.labels.code', metadataString(row, 'code')),
        ledgerPart('usage.balanceLedger.labels.reference', row.ref_id || '')
      ])
    case 'admin_adjustment':
      return compactLedgerParts([
        ledgerPart('usage.balanceLedger.labels.notes', metadataString(row, 'notes')),
        ledgerPart('usage.balanceLedger.labels.operation', metadataString(row, 'operation')),
        ledgerPart('usage.balanceLedger.labels.code', metadataString(row, 'code')),
        ledgerPart('usage.balanceLedger.labels.reference', row.ref_id || '')
      ])
    default:
      return compactLedgerParts([
        ledgerPart('usage.balanceLedger.labels.reference', row.ref_id || ''),
        ledgerPart('usage.balanceLedger.labels.referenceType', row.ref_type)
      ])
  }
}

const balanceLedgerRows = computed<BalanceLedgerTableRow[]>(() =>
  balanceLedger.value.map((row) => ({
    ...row,
    reasonLabel: getLedgerReasonLabel(row.reason),
    amountLabel: formatLedgerAmount(row),
    balanceAfterLabel: formatLedgerCurrency(row.balance_after),
    remarkText: getLedgerRemark(row)
  }))
)

/**
 * Escape CSV value to prevent injection and handle special characters
 */
const escapeCSVValue = (value: unknown): string => {
  if (value == null) return ''

  const str = String(value)
  const escaped = str.replace(/"/g, '""')

  // Prevent formula injection by prefixing dangerous characters with single quote
  if (/^[=+\-@\t\r]/.test(str)) {
    return `"\'${escaped}"`
  }

  // Escape values containing comma, quote, or newline
  if (/[,"\n\r]/.test(str)) {
    return `"${escaped}"`
  }

  return str
}

const exportToCSV = async () => {
  if (pagination.total === 0) {
    appStore.showWarning(t('usage.noDataToExport'))
    return
  }

  exporting.value = true
  appStore.showInfo(t('usage.preparingExport'))

  try {
    const allLogs: UsageLog[] = []
    const pageSize = 100 // Use a larger page size for export to reduce requests
    const totalRequests = Math.ceil(pagination.total / pageSize)

    for (let page = 1; page <= totalRequests; page++) {
      const response = await usageAPI.query(buildUsageQueryParams(page, pageSize))
      allLogs.push(...response.items)
    }

    if (allLogs.length === 0) {
      appStore.showWarning(t('usage.noDataToExport'))
      return
    }

    const headers = [
      'Time',
      'API Key Name',
      'Model',
      'Reasoning Effort',
      'Inbound Endpoint',
      'Type',
      'Billing Mode',
      'Payment Source',
      'Points Deducted',
      'Balance Deducted',
      'Input Tokens',
      'Output Tokens',
      'Cache Read Tokens',
      'Cache Creation Tokens',
      'Cache Hit Rate',
      'Rate Multiplier',
      'Billed Cost',
      'Original Cost',
      'First Token (ms)',
      'Duration (ms)'
    ]
    const rows = allLogs.map((log) =>
      [
        log.created_at,
        log.api_key?.name || '',
        log.model,
        formatReasoningEffort(log.reasoning_effort),
        log.inbound_endpoint || '',
        getRequestTypeExportText(log),
        getBillingModeLabel(log.billing_mode, t),
        paymentSourceLabel(log),
        formatPointsAmount(log.points_deducted),
        Number(log.balance_deducted || 0).toFixed(8),
        log.input_tokens,
        log.output_tokens,
        log.cache_read_tokens,
        log.cache_creation_tokens,
        formatCacheHitRate(log.input_tokens, log.cache_read_tokens),
        log.rate_multiplier,
        log.actual_cost.toFixed(8),
        log.total_cost.toFixed(8),
        log.first_token_ms ?? '',
        log.duration_ms
      ].map(escapeCSVValue)
    )

    const csvContent = [
      headers.map(escapeCSVValue).join(','),
      ...rows.map((row) => row.join(','))
    ].join('\n')

    const blob = new Blob([csvContent], { type: 'text/csv;charset=utf-8;' })
    const url = window.URL.createObjectURL(blob)
    const link = document.createElement('a')
    link.href = url
    link.download = `usage_${filters.value.start_date}_to_${filters.value.end_date}.csv`
    link.click()
    window.URL.revokeObjectURL(url)

    appStore.showSuccess(t('usage.exportSuccess'))
  } catch (error) {
    appStore.showError(t('usage.exportFailed'))
    console.error('CSV Export failed:', error)
  } finally {
    exporting.value = false
  }
}

// Tooltip functions
const showTooltip = (event: MouseEvent, row: UsageLog) => {
  const target = event.currentTarget as HTMLElement
  const rect = target.getBoundingClientRect()

  tooltipData.value = row
  // Position to the right of the icon, vertically centered
  tooltipPosition.value.x = rect.right + 8
  tooltipPosition.value.y = rect.top + rect.height / 2
  tooltipVisible.value = true
}

const hideTooltip = () => {
  tooltipVisible.value = false
  tooltipData.value = null
}

// Token tooltip functions
const showTokenTooltip = (event: MouseEvent, row: UsageLog) => {
  const target = event.currentTarget as HTMLElement
  const rect = target.getBoundingClientRect()

  tokenTooltipData.value = row
  tokenTooltipPosition.value.x = rect.right + 8
  tokenTooltipPosition.value.y = rect.top + rect.height / 2
  tokenTooltipVisible.value = true
}

const hideTokenTooltip = () => {
  tokenTooltipVisible.value = false
  tokenTooltipData.value = null
}

onMounted(() => {
  loadApiKeys()
  loadUsageLogs()
  loadUsageStats()
  scheduleBalanceLedgerPrefetch()
})

onUnmounted(() => {
  clearBalanceLedgerPrefetch()
  if (abortController) {
    abortController.abort()
  }
  abortBalanceLedgerRequest()
})
</script>
