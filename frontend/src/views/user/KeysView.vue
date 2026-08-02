<template>
  <AppLayout>
    <TablePageLayout class="data-page keys-page">
      <template #filters>
        <div class="data-filter-card flex flex-col gap-3">
          <div class="data-filter-row">
            <SearchInput
              v-model="filterSearch"
              :placeholder="t('keys.searchPlaceholder')"
              class="w-full sm:w-64"
              @search="onFilterChange"
            />
            <Select
              :model-value="filterGroupId"
              class="w-full sm:w-40"
              :options="groupFilterOptions"
              :aria-label="t('keys.groupFilterLabel')"
              @update:model-value="onGroupFilterChange"
            />
            <Select
              :model-value="filterStatus"
              class="w-full sm:w-40"
              :options="statusFilterOptions"
              :aria-label="t('keys.statusFilterLabel')"
              @update:model-value="onStatusFilterChange"
            />
          </div>
          <EndpointPopover
            v-if="publicSettings?.api_base_url || (publicSettings?.custom_endpoints?.length ?? 0) > 0"
            :api-base-url="publicSettings?.api_base_url || ''"
            :custom-endpoints="publicSettings?.custom_endpoints || []"
          />
        </div>
      </template>

      <template #actions>
        <div class="data-page-actions">
          <div ref="columnDropdownRef" class="relative">
            <button
              id="api-key-column-settings-trigger"
              ref="columnSettingsTriggerRef"
              type="button"
              class="btn btn-secondary min-w-11 px-2 md:px-3"
              :title="t('keys.columnSettings')"
              :aria-expanded="showColumnDropdown"
              :aria-controls="showColumnDropdown ? COLUMN_SETTINGS_MENU_ID : undefined"
              aria-haspopup="menu"
              @click="toggleColumnSettings"
            >
              <svg class="h-4 w-4 md:mr-1.5" fill="none" stroke="currentColor" viewBox="0 0 24 24" stroke-width="1.5">
                <path stroke-linecap="round" stroke-linejoin="round" d="M9 4.5v15m6-15v15m-10.875 0h15.75c.621 0 1.125-.504 1.125-1.125V5.625c0-.621-.504-1.125-1.125-1.125H4.125C3.504 4.5 3 5.004 3 5.625v12.75c0 .621.504 1.125 1.125 1.125z" />
              </svg>
              <span class="hidden md:inline">{{ t('keys.columnSettings') }}</span>
            </button>
            <div
              v-if="showColumnDropdown"
              :id="COLUMN_SETTINGS_MENU_ID"
              class="data-popover-panel absolute left-0 right-auto z-50 mt-2 w-52 origin-top-left p-2 sm:left-auto sm:right-0 sm:origin-top-right"
              role="menu"
              :aria-label="t('keys.columnSettings')"
              @keydown="handleColumnSettingsKeydown"
              @focusout="handleColumnSettingsFocusout"
            >
              <div class="max-h-80 overflow-y-auto">
                <button
                  v-for="(column, index) in toggleableColumns"
                  :key="column.key"
                  type="button"
                  class="data-popover-option"
                  role="menuitemcheckbox"
                  :aria-checked="isColumnVisible(column.key)"
                  :tabindex="columnMenuFocusIndex === index ? 0 : -1"
                  data-column-menu-item
                  @focus="columnMenuFocusIndex = index"
                  @click="toggleColumn(column.key)"
                >
                  <span>{{ column.label }}</span>
                  <Icon v-if="isColumnVisible(column.key)" name="check" size="sm" class="text-brand" />
                </button>
              </div>
            </div>
          </div>
          <button
            @click="refreshKeyPageData"
            :disabled="loading"
            class="btn btn-secondary btn-icon"
            :title="t('common.refresh')"
            :aria-label="t('common.refresh')"
          >
            <Icon name="refresh" size="md" :class="loading ? 'animate-spin' : ''" />
          </button>
          <button
            @click="showCreateModal = true"
            class="btn btn-primary min-w-0 flex-1 sm:flex-none"
            data-tour="keys-create-btn"
          >
            <Icon name="plus" size="md" />
            {{ t('keys.createKey') }}
          </button>
        </div>
      </template>

      <template #table>
        <DataTable
          :columns="columns"
          :data="apiKeys"
          :loading="loading"
          :server-side-sort="true"
          default-sort-key="created_at"
          default-sort-order="desc"
          @sort="handleSort"
        >
          <template #cell-id="{ value }">
            <span class="font-mono text-xs text-content-subtle">#{{ value }}</span>
          </template>

          <template #cell-key="{ value, row }">
            <div class="flex min-w-0 items-center justify-end gap-1.5 md:justify-start">
              <code class="code text-xs">
                {{ maskApiKey(value) }}
              </code>
              <button
                type="button"
                @click="copyToClipboard(value, row.id)"
                class="data-inline-icon-button"
                :class="
                  copiedKeyId === row.id
                    ? 'text-positive'
                    : ''
                "
                :title="copiedKeyId === row.id ? t('keys.copied') : t('keys.copyToClipboard')"
                :aria-label="copiedKeyId === row.id ? t('keys.copied') : t('keys.copyToClipboard')"
              >
                <Icon
                  v-if="copiedKeyId === row.id"
                  name="check"
                  size="sm"
                  :stroke-width="2"
                />
                <Icon v-else name="clipboard" size="sm" />
              </button>
            </div>
          </template>

          <template #cell-name="{ value, row }">
            <div class="flex min-w-0 items-center justify-end gap-1.5 md:justify-start">
              <span class="inline-block max-w-64 whitespace-normal break-words font-medium text-content">{{ value }}</span>
              <Icon
                v-if="row.ip_whitelist?.length > 0 || row.ip_blacklist?.length > 0"
                name="shield"
                size="sm"
                class="text-brand"
                :title="t('keys.ipRestrictionEnabled')"
              />
            </div>
          </template>

          <template #cell-group="{ row }">
            <div class="group/dropdown relative">
              <button
                type="button"
                :ref="(el) => setGroupButtonRef(row.id, el)"
                @click="openGroupSelector(row)"
                class="data-cell-trigger -mx-2 -my-1 max-w-full"
                :title="t('keys.clickToChangeGroup')"
                :aria-expanded="groupSelectorKeyId === row.id"
                :aria-controls="groupSelectorKeyId === row.id ? getGroupSelectorPopoverId(row.id) : undefined"
                aria-haspopup="dialog"
              >
                <GroupBadge
                  v-if="row.group"
                  :name="row.group.name"
                  :platform="row.group.platform"
                  :subscription-type="row.group.subscription_type"
                  :rate-multiplier="row.group.rate_multiplier"
                  :user-rate-multiplier="userGroupRates[row.group.id]"
                  :effective-rate-multiplier="effectiveRateForGroup(row.group)"
                  :rate-multiplier-source="effectiveRateSourceForGroup(row.group)"
                />
                <span v-else class="text-sm text-content-subtle">{{
                  t('keys.noGroup')
                }}</span>
                <span class="hidden text-xs text-content-subtle xl:inline">{{ t('keys.selectGroup') }}</span>
                <svg
                  class="h-3.5 w-3.5 flex-shrink-0 text-content-subtle opacity-60 transition-opacity group-hover/dropdown:opacity-100"
                  fill="none"
                  stroke="currentColor"
                  viewBox="0 0 24 24"
                  stroke-width="2"
                >
                  <path
                    stroke-linecap="round"
                    stroke-linejoin="round"
                    d="M8.25 15L12 18.75 15.75 15m-7.5-6L12 5.25 15.75 9"
                  />
                </svg>
              </button>
            </div>
          </template>

          <template #cell-usage="{ row }">
            <div class="text-sm">
              <div class="flex items-center gap-1.5">
                <span class="text-content-subtle">{{ t('keys.today') }}:</span>
                <span v-if="usageStatsLoading" class="inline-block h-3 w-14 animate-pulse rounded bg-surface-hover"></span>
                <span v-else-if="usageStatsError" class="text-danger">{{ t('common.error') }}</span>
                <span v-else-if="usageStats[row.id]" class="font-medium text-content">
                  ${{ usageStats[row.id].today_actual_cost.toFixed(4) }}
                </span>
                <span v-else class="text-content-subtle">-</span>
              </div>
              <div class="mt-0.5 flex items-center gap-1.5">
                <span class="text-content-subtle">{{ t('keys.total') }}:</span>
                <span v-if="usageStatsLoading" class="inline-block h-3 w-14 animate-pulse rounded bg-surface-hover"></span>
                <span v-else-if="usageStatsError" class="text-danger">{{ t('common.error') }}</span>
                <span v-else-if="usageStats[row.id]" class="font-medium text-content">
                  ${{ usageStats[row.id].total_actual_cost.toFixed(4) }}
                </span>
                <span v-else class="text-content-subtle">-</span>
              </div>
              <!-- Quota progress (if quota is set) -->
              <div v-if="row.quota > 0" class="mt-1.5">
                <div class="flex items-center gap-1.5">
                  <span class="text-gray-500 dark:text-gray-400">{{ t('keys.quota') }}:</span>
                  <span :class="[
                    'font-medium',
                    row.quota_used >= row.quota ? 'text-red-500' :
                    row.quota_used >= row.quota * 0.8 ? 'text-yellow-500' :
                    'text-gray-900 dark:text-white'
                  ]">
                    ${{ row.quota_used?.toFixed(2) || '0.00' }} / ${{ row.quota?.toFixed(2) }}
                  </span>
                </div>
                <div class="mt-1 h-1.5 w-full overflow-hidden rounded-full bg-gray-200 dark:bg-dark-600">
                  <div
                    :class="[
                      'h-full rounded-full transition-all',
                      row.quota_used >= row.quota ? 'bg-red-500' :
                      row.quota_used >= row.quota * 0.8 ? 'bg-yellow-500' :
                      'bg-primary-500'
                    ]"
                    :style="{ width: Math.min((row.quota_used / row.quota) * 100, 100) + '%' }"
                  />
                </div>
              </div>
            </div>
          </template>

          <template #cell-rate_limit="{ row }">
            <div v-if="row.rate_limit_5h > 0 || row.rate_limit_1d > 0 || row.rate_limit_7d > 0" class="space-y-1.5 min-w-[140px]">
              <!-- 5h window -->
              <div v-if="row.rate_limit_5h > 0">
                <div class="flex items-center justify-between text-xs">
                  <span class="text-gray-500 dark:text-gray-400">5h</span>
                  <span :class="[
                    'font-medium tabular-nums',
                    row.usage_5h >= row.rate_limit_5h ? 'text-red-500' :
                    row.usage_5h >= row.rate_limit_5h * 0.8 ? 'text-yellow-500' :
                    'text-gray-700 dark:text-gray-300'
                  ]">
                    ${{ row.usage_5h?.toFixed(2) || '0.00' }}/${{ row.rate_limit_5h?.toFixed(2) }}
                  </span>
                </div>
                <div class="h-1 w-full overflow-hidden rounded-full bg-gray-200 dark:bg-dark-600">
                  <div
                    :class="[
                      'h-full rounded-full transition-all',
                      row.usage_5h >= row.rate_limit_5h ? 'bg-red-500' :
                      row.usage_5h >= row.rate_limit_5h * 0.8 ? 'bg-yellow-500' :
                      'bg-emerald-500'
                    ]"
                    :style="{ width: Math.min((row.usage_5h / row.rate_limit_5h) * 100, 100) + '%' }"
                  />
                </div>
                <div v-if="row.reset_5h_at && formatResetTime(row.reset_5h_at)" class="text-[10px] text-gray-400 dark:text-gray-500 tabular-nums">
                  ⟳ {{ formatResetTime(row.reset_5h_at) }}
                </div>
              </div>
              <!-- 1d window -->
              <div v-if="row.rate_limit_1d > 0">
                <div class="flex items-center justify-between text-xs">
                  <span class="text-gray-500 dark:text-gray-400">1d</span>
                  <span :class="[
                    'font-medium tabular-nums',
                    row.usage_1d >= row.rate_limit_1d ? 'text-red-500' :
                    row.usage_1d >= row.rate_limit_1d * 0.8 ? 'text-yellow-500' :
                    'text-gray-700 dark:text-gray-300'
                  ]">
                    ${{ row.usage_1d?.toFixed(2) || '0.00' }}/${{ row.rate_limit_1d?.toFixed(2) }}
                  </span>
                </div>
                <div class="h-1 w-full overflow-hidden rounded-full bg-gray-200 dark:bg-dark-600">
                  <div
                    :class="[
                      'h-full rounded-full transition-all',
                      row.usage_1d >= row.rate_limit_1d ? 'bg-red-500' :
                      row.usage_1d >= row.rate_limit_1d * 0.8 ? 'bg-yellow-500' :
                      'bg-emerald-500'
                    ]"
                    :style="{ width: Math.min((row.usage_1d / row.rate_limit_1d) * 100, 100) + '%' }"
                  />
                </div>
                <div v-if="row.reset_1d_at && formatResetTime(row.reset_1d_at)" class="text-[10px] text-gray-400 dark:text-gray-500 tabular-nums">
                  ⟳ {{ formatResetTime(row.reset_1d_at) }}
                </div>
              </div>
              <!-- 7d window -->
              <div v-if="row.rate_limit_7d > 0">
                <div class="flex items-center justify-between text-xs">
                  <span class="text-gray-500 dark:text-gray-400">7d</span>
                  <span :class="[
                    'font-medium tabular-nums',
                    row.usage_7d >= row.rate_limit_7d ? 'text-red-500' :
                    row.usage_7d >= row.rate_limit_7d * 0.8 ? 'text-yellow-500' :
                    'text-gray-700 dark:text-gray-300'
                  ]">
                    ${{ row.usage_7d?.toFixed(2) || '0.00' }}/${{ row.rate_limit_7d?.toFixed(2) }}
                  </span>
                </div>
                <div class="h-1 w-full overflow-hidden rounded-full bg-gray-200 dark:bg-dark-600">
                  <div
                    :class="[
                      'h-full rounded-full transition-all',
                      row.usage_7d >= row.rate_limit_7d ? 'bg-red-500' :
                      row.usage_7d >= row.rate_limit_7d * 0.8 ? 'bg-yellow-500' :
                      'bg-emerald-500'
                    ]"
                    :style="{ width: Math.min((row.usage_7d / row.rate_limit_7d) * 100, 100) + '%' }"
                  />
                </div>
                <div v-if="row.reset_7d_at && formatResetTime(row.reset_7d_at)" class="text-[10px] text-gray-400 dark:text-gray-500 tabular-nums">
                  ⟳ {{ formatResetTime(row.reset_7d_at) }}
                </div>
              </div>
              <!-- Reset button -->
              <button
                v-if="row.usage_5h > 0 || row.usage_1d > 0 || row.usage_7d > 0"
                type="button"
                @click.stop="confirmResetRateLimitFromTable(row)"
                class="mt-0.5 inline-flex min-h-11 items-center gap-1 rounded-lg px-3 py-1 text-xs text-content-subtle transition-colors hover:bg-surface-hover hover:text-brand"
                :title="t('keys.resetRateLimitUsage')"
              >
                <Icon name="refresh" size="xs" />
                {{ t('keys.resetUsage') }}
              </button>
            </div>
            <span v-else class="text-sm text-gray-400 dark:text-dark-500">-</span>
          </template>

          <template #cell-current_concurrency="{ row }">
            <span
              :class="[
                'inline-flex min-w-8 items-center justify-center rounded-full px-2 py-0.5 text-xs font-semibold tabular-nums',
                (row.current_concurrency ?? 0) > 0
                  ? 'bg-emerald-100 text-emerald-700 dark:bg-emerald-900/30 dark:text-emerald-300'
                  : 'bg-gray-100 text-gray-500 dark:bg-dark-700 dark:text-dark-300'
              ]"
            >
              {{ row.current_concurrency ?? 0 }}
            </span>
          </template>

          <template #cell-expires_at="{ value }">
            <span v-if="value" :class="[
              'text-sm',
              new Date(value) < new Date() ? 'text-red-500 dark:text-red-400' : 'text-gray-500 dark:text-dark-400'
            ]">
              {{ formatDateTime(value) }}
            </span>
            <span v-else class="text-sm text-gray-400 dark:text-dark-500">{{ t('keys.noExpiration') }}</span>
          </template>

          <template #cell-status="{ value }">
            <span :class="[
              'badge',
              value === 'active' ? 'badge-success' :
              value === 'quota_exhausted' ? 'badge-warning' :
              value === 'expired' ? 'badge-danger' :
              'badge-gray'
            ]">
              {{ t('keys.status.' + value) }}
            </span>
          </template>

		  <template #cell-last_used_at="{ value }">
			<span v-if="value" class="text-sm text-gray-500 dark:text-dark-400">
			  {{ formatDateTime(value) }}
			</span>
			<span v-else class="text-sm text-gray-400 dark:text-dark-500">-</span>
		  </template>

		  <template #cell-last_used_ip="{ value }">
			<span v-if="value" class="text-sm text-gray-500 dark:text-dark-400">
			  {{ value }}
			</span>
			<span v-else class="text-sm text-gray-400 dark:text-dark-500">-</span>
		  </template>

          <template #cell-created_at="{ value }">
            <span class="text-sm text-gray-500 dark:text-dark-400">{{ formatDateTime(value) }}</span>
          </template>

          <template #cell-actions="{ row }">
            <div class="data-table-actions">
              <!-- Use Key Button -->
              <button
                type="button"
                @click="openUseKeyModal(row)"
                class="data-table-action text-content-muted hover:bg-brand-soft hover:text-brand"
              >
                <Icon name="terminal" size="sm" />
                <span class="text-xs">{{ t('keys.useKey') }}</span>
              </button>
              <!-- Import to CC Switch Button -->
              <button
                v-if="!publicSettings?.hide_ccs_import_button"
                type="button"
                @click="importToCcswitch(row)"
                class="data-table-action text-content-muted hover:bg-brand-soft hover:text-brand"
              >
                <Icon name="upload" size="sm" />
                <span class="text-xs">{{ t('keys.importToCcSwitch') }}</span>
              </button>
              <!-- Open Image Playground Button -->
              <button
                type="button"
                @click="openImagePlayground(row)"
                class="data-table-action text-content-muted hover:bg-brand-soft hover:text-brand"
              >
                <Icon name="sparkles" size="sm" />
                <span class="text-xs">{{ t('keys.openImagePlayground') }}</span>
              </button>
              <!-- Toggle Status Button -->
              <button
                type="button"
                @click="toggleKeyStatus(row)"
                :class="[
                  'data-table-action',
                  row.status === 'active'
                    ? 'text-content-muted hover:bg-amber-50 hover:text-warning dark:hover:bg-amber-950/30'
                    : 'text-content-muted hover:bg-emerald-50 hover:text-positive dark:hover:bg-emerald-950/30'
                ]"
              >
                <Icon v-if="row.status === 'active'" name="ban" size="sm" />
                <Icon v-else name="checkCircle" size="sm" />
                <span class="text-xs">{{ row.status === 'active' ? t('keys.disable') : t('keys.enable') }}</span>
              </button>
              <!-- Edit Button -->
              <button
                type="button"
                @click="editKey(row)"
                class="data-table-action text-content-muted hover:bg-surface-hover hover:text-brand"
              >
                <Icon name="edit" size="sm" />
                <span class="text-xs">{{ t('common.edit') }}</span>
              </button>
              <!-- Delete Button -->
              <button
                type="button"
                @click="confirmDelete(row)"
                class="data-table-action text-content-muted hover:bg-red-50 hover:text-danger dark:hover:bg-red-950/30"
              >
                <Icon name="trash" size="sm" />
                <span class="text-xs">{{ t('common.delete') }}</span>
              </button>
            </div>
          </template>

          <template #empty>
            <EmptyState
              :title="t('keys.noKeysYet')"
              :description="t('keys.createFirstKey')"
              :action-text="t('keys.createKey')"
              @action="showCreateModal = true"
            />
          </template>
        </DataTable>
      </template>

      <template #pagination>
        <Pagination
          v-if="pagination.total > 0"
          class="data-pagination"
          :page="pagination.page"
          :total="pagination.total"
          :page-size="pagination.page_size"
          @update:page="handlePageChange"
          @update:pageSize="handlePageSizeChange"
        />
      </template>
    </TablePageLayout>

    <!-- Create/Edit Modal -->
    <BaseDialog
      :show="showCreateModal || showEditModal"
      :title="showEditModal ? t('keys.editKey') : t('keys.createKey')"
      width="wide"
      @close="closeModals"
    >
      <form id="key-form" @submit.prevent="handleSubmit" class="space-y-5">
        <div>
          <label for="key-name" class="input-label">{{ t('keys.nameLabel') }}</label>
          <input
            id="key-name"
            v-model="formData.name"
            type="text"
            required
            class="input"
            :placeholder="t('keys.namePlaceholder')"
            data-tour="key-form-name"
          />
        </div>

        <div>
          <span id="key-group-label" class="input-label">{{ t('keys.groupLabel') }}</span>
          <Select
            v-model="formData.group_id"
            :options="groupOptions"
            :placeholder="t('keys.selectGroup')"
            :searchable="true"
            :search-placeholder="t('keys.searchGroup')"
            aria-labelledby="key-group-label"
            data-tour="key-form-group"
          >
            <template #selected="{ option }">
              <div v-if="option" class="flex min-w-0 flex-wrap items-center gap-1.5">
                <GroupApiKeyBadge
                  :type="(option as unknown as GroupOption).apiKeyBadgeType"
                  :text="(option as unknown as GroupOption).apiKeyBadgeText"
                  :scope="(option as unknown as GroupOption).scope"
                />
                <GroupBadge
                  :name="(option as unknown as GroupOption).label"
                  :platform="(option as unknown as GroupOption).platform"
                  :scope="(option as unknown as GroupOption).scope"
                  :subscription-type="(option as unknown as GroupOption).subscriptionType"
                  :rate-multiplier="(option as unknown as GroupOption).rate"
                  :user-rate-multiplier="(option as unknown as GroupOption).userRate"
                  :effective-rate-multiplier="(option as unknown as GroupOption).effectiveRate"
                  :rate-multiplier-source="(option as unknown as GroupOption).rateSource"
                />
              </div>
              <span v-else class="text-gray-400">{{ t('keys.selectGroup') }}</span>
            </template>
            <template #option="{ option, selected }">
              <GroupOptionItem
                :name="(option as unknown as GroupOption).label"
                :platform="(option as unknown as GroupOption).platform"
                :scope="(option as unknown as GroupOption).scope"
                :subscription-type="(option as unknown as GroupOption).subscriptionType"
                :rate-multiplier="(option as unknown as GroupOption).rate"
                :user-rate-multiplier="(option as unknown as GroupOption).userRate"
                :effective-rate-multiplier="(option as unknown as GroupOption).effectiveRate"
                :rate-multiplier-source="(option as unknown as GroupOption).rateSource"
                :description="(option as unknown as GroupOption).description"
                :api-key-badge-type="(option as unknown as GroupOption).apiKeyBadgeType"
                :api-key-badge-text="(option as unknown as GroupOption).apiKeyBadgeText"
                :selected="selected"
              />
            </template>
          </Select>
        </div>

        <div class="space-y-4 rounded-xl border border-gray-200 bg-gray-50/60 p-4 dark:border-dark-700 dark:bg-dark-900/20 sm:p-5">
          <div class="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
            <div class="min-w-0">
              <label for="key-group-routes-switch" class="text-sm font-medium text-gray-700 dark:text-dark-200">
                {{ t('keys.groupRoutes.title') }}
              </label>
              <p id="key-group-routes-description" class="mt-1 max-w-2xl text-xs leading-5 text-gray-500 dark:text-dark-400">
                {{ t('keys.groupRoutes.description') }}
                <span class="block">{{ t('keys.groupRoutes.platformLocked') }}</span>
              </p>
            </div>
            <button
              id="key-group-routes-switch"
              type="button"
              class="data-form-switch"
              role="switch"
              :aria-checked="formData.enable_group_routes"
              aria-describedby="key-group-routes-description"
              :title="t('keys.groupRoutes.title')"
              @click="toggleGroupRoutes"
            >
              <span
                aria-hidden="true"
                :class="[
                  'relative inline-flex h-6 w-11 items-center rounded-full transition-colors',
                  formData.enable_group_routes ? 'bg-primary-600' : 'bg-gray-200 dark:bg-dark-600'
                ]"
              >
                <span
                  :class="[
                    'inline-block h-4 w-4 transform rounded-full bg-white transition-transform',
                    formData.enable_group_routes ? 'translate-x-6' : 'translate-x-1'
                  ]"
                />
              </span>
            </button>
          </div>

          <div
            v-if="formData.enable_group_routes"
            class="space-y-3"
            :aria-label="t('keys.groupRoutes.title')"
          >
            <div
              v-for="(route, index) in formData.group_routes"
              :key="index"
              class="space-y-3 rounded-xl border border-gray-200 bg-white p-3 shadow-sm dark:border-dark-700 dark:bg-dark-800/70 sm:p-4"
            >
              <div class="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
                <div class="flex min-w-0 items-center gap-2">
                  <span class="inline-flex h-6 min-w-6 items-center justify-center rounded-full bg-primary-50 px-2 text-xs font-medium text-primary-700 dark:bg-primary-900/30 dark:text-primary-300">
                    {{ index + 1 }}
                  </span>
                  <span class="text-sm font-medium text-gray-700 dark:text-dark-200">
                    {{ t('keys.groupRoutes.configuration') }}
                  </span>
                </div>
                <div class="flex items-center gap-2">
                  <label
                    :for="`key-group-route-enabled-${index}`"
                    class="inline-flex min-h-11 items-center gap-2 rounded-lg border border-gray-200 bg-gray-50 px-3 text-sm text-gray-600 dark:border-dark-600 dark:bg-dark-700/60 dark:text-dark-300"
                  >
                    <input
                      :id="`key-group-route-enabled-${index}`"
                      v-model="route.enabled"
                      type="checkbox"
                      class="rounded border-gray-300 text-primary-600 focus:ring-primary-500"
                    />
                    {{ t('keys.groupRoutes.enabled') }}
                  </label>
                  <button
                    type="button"
                    class="btn btn-secondary min-h-11 min-w-11 px-3"
                    :disabled="formData.group_routes.length <= 1"
                    :title="t('keys.groupRoutes.removeRoute', { index: index + 1 })"
                    :aria-label="t('keys.groupRoutes.removeRoute', { index: index + 1 })"
                    @click="removeGroupRoute(index)"
                  >
                    <Icon name="trash" size="sm" />
                  </button>
                </div>
              </div>

              <div class="grid gap-3 md:grid-cols-2 lg:grid-cols-[minmax(16rem,1fr)_8rem_9rem]">
                <div class="md:col-span-2 lg:col-span-1">
                  <span
                    :id="`key-group-route-group-label-${index}`"
                    class="mb-1 block text-xs text-gray-500 dark:text-dark-400"
                  >
                    {{ t('keys.groupRoutes.group') }}
                  </span>
                  <Select
                    v-model="route.group_id"
                    :options="routeGroupOptions"
                    :searchable="true"
                    :search-placeholder="t('keys.searchGroup')"
                    :aria-labelledby="`key-group-route-group-label-${index}`"
                  >
                    <template #selected="{ option }">
                      <div v-if="option" class="flex min-w-0 flex-wrap items-center gap-1.5">
                        <GroupApiKeyBadge
                          :type="(option as unknown as GroupOption).apiKeyBadgeType"
                          :text="(option as unknown as GroupOption).apiKeyBadgeText"
                          :scope="(option as unknown as GroupOption).scope"
                        />
                        <GroupBadge
                          :name="(option as unknown as GroupOption).label"
                          :platform="(option as unknown as GroupOption).platform"
                          :scope="(option as unknown as GroupOption).scope"
                          :subscription-type="(option as unknown as GroupOption).subscriptionType"
                          :rate-multiplier="(option as unknown as GroupOption).rate"
                          :user-rate-multiplier="(option as unknown as GroupOption).userRate"
                          :effective-rate-multiplier="(option as unknown as GroupOption).effectiveRate"
                          :rate-multiplier-source="(option as unknown as GroupOption).rateSource"
                        />
                      </div>
                      <span v-else class="text-gray-400">{{ t('keys.selectGroup') }}</span>
                    </template>
                    <template #option="{ option, selected }">
                      <GroupOptionItem
                        :name="(option as unknown as GroupOption).label"
                        :platform="(option as unknown as GroupOption).platform"
                        :scope="(option as unknown as GroupOption).scope"
                        :subscription-type="(option as unknown as GroupOption).subscriptionType"
                        :rate-multiplier="(option as unknown as GroupOption).rate"
                        :user-rate-multiplier="(option as unknown as GroupOption).userRate"
                        :effective-rate-multiplier="(option as unknown as GroupOption).effectiveRate"
                        :rate-multiplier-source="(option as unknown as GroupOption).rateSource"
                        :description="(option as unknown as GroupOption).description"
                        :api-key-badge-type="(option as unknown as GroupOption).apiKeyBadgeType"
                        :api-key-badge-text="(option as unknown as GroupOption).apiKeyBadgeText"
                        :selected="selected"
                      />
                    </template>
                  </Select>
                </div>
                <div>
                  <label :for="`key-group-route-priority-${index}`" class="mb-1 block text-xs text-gray-500 dark:text-dark-400">
                    {{ t('keys.groupRoutes.priority') }}
                  </label>
                  <input
                    :id="`key-group-route-priority-${index}`"
                    v-model.number="route.priority"
                    type="number"
                    min="1"
                    step="1"
                    class="input"
                  />
                </div>
                <div>
                  <label :for="`key-group-route-cooldown-${index}`" class="mb-1 block text-xs text-gray-500 dark:text-dark-400">
                    {{ t('keys.groupRoutes.cooldownSeconds') }}
                  </label>
                  <input
                    :id="`key-group-route-cooldown-${index}`"
                    v-model.number="route.cooldown_seconds"
                    type="number"
                    min="0"
                    step="1"
                    class="input"
                  />
                </div>
              </div>
            </div>
            <button type="button" class="btn btn-secondary w-full sm:w-auto" @click="addGroupRoute">
              <Icon name="plus" size="sm" class="mr-2" />
              {{ t('keys.groupRoutes.addRoute') }}
            </button>
          </div>
        </div>

        <!-- Custom Key Section (only for create) -->
        <div v-if="!showEditModal" class="space-y-3">
          <div class="flex items-center justify-between">
            <label id="key-custom-key-label" for="key-custom-key-switch" class="input-label mb-0">
              {{ t('keys.customKeyLabel') }}
            </label>
            <button
              id="key-custom-key-switch"
              type="button"
              class="data-form-switch"
              role="switch"
              :aria-checked="formData.use_custom_key"
              :aria-label="t('keys.customKeyLabel')"
              :title="t('keys.customKeyLabel')"
              @click="toggleCustomKey"
            >
              <span
                aria-hidden="true"
                :class="[
                  'relative inline-flex h-5 w-9 rounded-full border-2 border-transparent transition-colors duration-200 ease-in-out',
                  formData.use_custom_key ? 'bg-primary-600' : 'bg-gray-200 dark:bg-dark-600'
                ]"
              >
                <span
                  :class="[
                    'pointer-events-none inline-block h-4 w-4 transform rounded-full bg-white shadow ring-0 transition duration-200 ease-in-out',
                    formData.use_custom_key ? 'translate-x-4' : 'translate-x-0'
                  ]"
                />
              </span>
            </button>
          </div>
          <div v-if="formData.use_custom_key">
            <input
              id="key-custom-key-value"
              ref="customKeyInputRef"
              v-model="formData.custom_key"
              type="text"
              maxlength="128"
              class="input font-mono"
              :placeholder="t('keys.customKeyPlaceholder')"
              :class="{ 'border-red-500 dark:border-red-500': customKeyError }"
              aria-labelledby="key-custom-key-label"
              aria-required="true"
              :aria-describedby="customKeyError ? 'key-custom-key-error' : 'key-custom-key-hint'"
              :aria-invalid="Boolean(customKeyError)"
              @blur="customKeyTouched = true"
            />
            <p v-if="customKeyError" id="key-custom-key-error" class="mt-1 text-sm text-red-500" role="alert">
              {{ customKeyError }}
            </p>
            <p v-else id="key-custom-key-hint" class="input-hint">{{ t('keys.customKeyHint') }}</p>
          </div>
        </div>

        <div v-if="showEditModal">
          <span id="key-status-label" class="input-label">{{ t('keys.statusLabel') }}</span>
          <Select
            v-model="formData.status"
            :options="statusOptions"
            :placeholder="t('keys.selectStatus')"
            aria-labelledby="key-status-label"
          />
        </div>

        <!-- IP Restriction Section -->
        <div class="space-y-3">
          <div class="flex items-center justify-between">
            <label for="key-ip-restriction-switch" class="input-label mb-0">{{ t('keys.ipRestriction') }}</label>
            <button
              id="key-ip-restriction-switch"
              type="button"
              class="data-form-switch"
              role="switch"
              :aria-checked="formData.enable_ip_restriction"
              :aria-label="t('keys.ipRestriction')"
              :title="t('keys.ipRestriction')"
              @click="formData.enable_ip_restriction = !formData.enable_ip_restriction"
            >
              <span
                aria-hidden="true"
                :class="[
                  'relative inline-flex h-5 w-9 rounded-full border-2 border-transparent transition-colors duration-200 ease-in-out',
                  formData.enable_ip_restriction ? 'bg-primary-600' : 'bg-gray-200 dark:bg-dark-600'
                ]"
              >
                <span
                  :class="[
                    'pointer-events-none inline-block h-4 w-4 transform rounded-full bg-white shadow ring-0 transition duration-200 ease-in-out',
                    formData.enable_ip_restriction ? 'translate-x-4' : 'translate-x-0'
                  ]"
                />
              </span>
            </button>
          </div>

          <div v-if="formData.enable_ip_restriction" class="space-y-4 pt-2">
            <div>
              <label for="key-ip-whitelist" class="input-label">{{ t('keys.ipWhitelist') }}</label>
              <textarea
                id="key-ip-whitelist"
                v-model="formData.ip_whitelist"
                rows="3"
                class="input font-mono text-sm"
                :placeholder="t('keys.ipWhitelistPlaceholder')"
                aria-describedby="key-ip-whitelist-hint"
              />
              <p id="key-ip-whitelist-hint" class="input-hint">{{ t('keys.ipWhitelistHint') }}</p>
            </div>

            <div>
              <label for="key-ip-blacklist" class="input-label">{{ t('keys.ipBlacklist') }}</label>
              <textarea
                id="key-ip-blacklist"
                v-model="formData.ip_blacklist"
                rows="3"
                class="input font-mono text-sm"
                :placeholder="t('keys.ipBlacklistPlaceholder')"
                aria-describedby="key-ip-blacklist-hint"
              />
              <p id="key-ip-blacklist-hint" class="input-hint">{{ t('keys.ipBlacklistHint') }}</p>
            </div>
          </div>
        </div>

        <!-- Quota Limit Section -->
        <div class="space-y-3">
          <label for="key-quota-limit" class="input-label">{{ t('keys.quotaLimit') }}</label>
          <!-- Switch commented out - always show input, 0 = unlimited
          <div class="flex items-center justify-between">
            <label class="input-label mb-0">{{ t('keys.quotaLimit') }}</label>
            <button
              type="button"
              @click="formData.enable_quota = !formData.enable_quota"
              :class="[
                'relative inline-flex h-5 w-9 flex-shrink-0 cursor-pointer rounded-full border-2 border-transparent transition-colors duration-200 ease-in-out focus:outline-none',
                formData.enable_quota ? 'bg-primary-600' : 'bg-gray-200 dark:bg-dark-600'
              ]"
            >
              <span
                :class="[
                  'pointer-events-none inline-block h-4 w-4 transform rounded-full bg-white shadow ring-0 transition duration-200 ease-in-out',
                  formData.enable_quota ? 'translate-x-4' : 'translate-x-0'
                ]"
              />
            </button>
          </div>
          -->

          <div class="space-y-4">
            <div>
              <div class="relative">
                <span class="absolute left-3 top-1/2 -translate-y-1/2 text-gray-500">$</span>
                <input
                  id="key-quota-limit"
                  v-model.number="formData.quota"
                  type="number"
                  step="0.01"
                  min="0"
                  class="input pl-7"
                  :placeholder="t('keys.quotaAmountPlaceholder')"
                  aria-describedby="key-quota-limit-hint"
                />
              </div>
              <p id="key-quota-limit-hint" class="input-hint">{{ t('keys.quotaAmountHint') }}</p>
            </div>

            <!-- Quota used display (only in edit mode) -->
            <div v-if="showEditModal && selectedKey && selectedKey.quota > 0">
              <label class="input-label">{{ t('keys.quotaUsed') }}</label>
              <div class="flex items-center gap-2">
                <div class="flex-1 rounded-lg bg-gray-100 px-3 py-2 dark:bg-dark-700">
                  <span class="font-medium text-gray-900 dark:text-white">
                    ${{ selectedKey.quota_used?.toFixed(4) || '0.0000' }}
                  </span>
                  <span class="mx-2 text-gray-400">/</span>
                  <span class="text-gray-500 dark:text-gray-400">
                    ${{ selectedKey.quota?.toFixed(2) || '0.00' }}
                  </span>
                </div>
                <button
                  type="button"
                  @click="confirmResetQuota"
                  class="btn btn-secondary min-h-11 text-sm"
                  :title="t('keys.resetQuotaUsed')"
                >
                  {{ t('keys.reset') }}
                </button>
              </div>
            </div>
          </div>
        </div>

        <!-- Rate Limit Section -->
        <div class="space-y-3">
          <div class="flex items-center justify-between">
            <label for="key-rate-limit-switch" class="input-label mb-0">{{ t('keys.rateLimitSection') }}</label>
            <button
              id="key-rate-limit-switch"
              type="button"
              class="data-form-switch"
              role="switch"
              :aria-checked="formData.enable_rate_limit"
              :aria-label="t('keys.rateLimitSection')"
              :title="t('keys.rateLimitSection')"
              @click="formData.enable_rate_limit = !formData.enable_rate_limit"
            >
              <span
                aria-hidden="true"
                :class="[
                  'relative inline-flex h-5 w-9 rounded-full border-2 border-transparent transition-colors duration-200 ease-in-out',
                  formData.enable_rate_limit ? 'bg-primary-600' : 'bg-gray-200 dark:bg-dark-600'
                ]"
              >
                <span
                  :class="[
                    'pointer-events-none inline-block h-4 w-4 transform rounded-full bg-white shadow ring-0 transition duration-200 ease-in-out',
                    formData.enable_rate_limit ? 'translate-x-4' : 'translate-x-0'
                  ]"
                />
              </span>
            </button>
          </div>

          <div v-if="formData.enable_rate_limit" class="space-y-4 pt-2">
            <p id="key-rate-limit-hint" class="input-hint -mt-2">{{ t('keys.rateLimitHint') }}</p>
            <!-- 5-Hour Limit -->
            <div>
              <label for="key-rate-limit-5h" class="input-label">{{ t('keys.rateLimit5h') }}</label>
              <div class="relative">
                <span class="absolute left-3 top-1/2 -translate-y-1/2 text-gray-500">$</span>
                <input
                  id="key-rate-limit-5h"
                  v-model.number="formData.rate_limit_5h"
                  type="number"
                  step="0.01"
                  min="0"
                  class="input pl-7"
                  :placeholder="'0'"
                  aria-describedby="key-rate-limit-hint"
                />
              </div>
              <!-- Usage info (edit mode only) -->
              <div v-if="showEditModal && selectedKey && selectedKey.rate_limit_5h > 0" class="mt-2">
                <div class="flex items-center gap-2">
                  <div class="flex-1 rounded-lg bg-gray-100 px-3 py-2 dark:bg-dark-700 text-sm">
                    <span :class="[
                      'font-medium',
                      selectedKey.usage_5h >= selectedKey.rate_limit_5h ? 'text-red-500' :
                      selectedKey.usage_5h >= selectedKey.rate_limit_5h * 0.8 ? 'text-yellow-500' :
                      'text-gray-900 dark:text-white'
                    ]">
                      ${{ selectedKey.usage_5h?.toFixed(4) || '0.0000' }}
                    </span>
                    <span class="mx-2 text-gray-400">/</span>
                    <span class="text-gray-500 dark:text-gray-400">
                      ${{ selectedKey.rate_limit_5h?.toFixed(2) || '0.00' }}
                    </span>
                  </div>
                </div>
                <div class="mt-1 h-1.5 w-full overflow-hidden rounded-full bg-gray-200 dark:bg-dark-600">
                  <div
                    :class="[
                      'h-full rounded-full transition-all',
                      selectedKey.usage_5h >= selectedKey.rate_limit_5h ? 'bg-red-500' :
                      selectedKey.usage_5h >= selectedKey.rate_limit_5h * 0.8 ? 'bg-yellow-500' :
                      'bg-green-500'
                    ]"
                    :style="{ width: Math.min((selectedKey.usage_5h / selectedKey.rate_limit_5h) * 100, 100) + '%' }"
                  />
                </div>
              </div>
            </div>

            <!-- Daily Limit -->
            <div>
              <label for="key-rate-limit-1d" class="input-label">{{ t('keys.rateLimit1d') }}</label>
              <div class="relative">
                <span class="absolute left-3 top-1/2 -translate-y-1/2 text-gray-500">$</span>
                <input
                  id="key-rate-limit-1d"
                  v-model.number="formData.rate_limit_1d"
                  type="number"
                  step="0.01"
                  min="0"
                  class="input pl-7"
                  :placeholder="'0'"
                  aria-describedby="key-rate-limit-hint"
                />
              </div>
              <!-- Usage info (edit mode only) -->
              <div v-if="showEditModal && selectedKey && selectedKey.rate_limit_1d > 0" class="mt-2">
                <div class="flex items-center gap-2">
                  <div class="flex-1 rounded-lg bg-gray-100 px-3 py-2 dark:bg-dark-700 text-sm">
                    <span :class="[
                      'font-medium',
                      selectedKey.usage_1d >= selectedKey.rate_limit_1d ? 'text-red-500' :
                      selectedKey.usage_1d >= selectedKey.rate_limit_1d * 0.8 ? 'text-yellow-500' :
                      'text-gray-900 dark:text-white'
                    ]">
                      ${{ selectedKey.usage_1d?.toFixed(4) || '0.0000' }}
                    </span>
                    <span class="mx-2 text-gray-400">/</span>
                    <span class="text-gray-500 dark:text-gray-400">
                      ${{ selectedKey.rate_limit_1d?.toFixed(2) || '0.00' }}
                    </span>
                  </div>
                </div>
                <div class="mt-1 h-1.5 w-full overflow-hidden rounded-full bg-gray-200 dark:bg-dark-600">
                  <div
                    :class="[
                      'h-full rounded-full transition-all',
                      selectedKey.usage_1d >= selectedKey.rate_limit_1d ? 'bg-red-500' :
                      selectedKey.usage_1d >= selectedKey.rate_limit_1d * 0.8 ? 'bg-yellow-500' :
                      'bg-green-500'
                    ]"
                    :style="{ width: Math.min((selectedKey.usage_1d / selectedKey.rate_limit_1d) * 100, 100) + '%' }"
                  />
                </div>
              </div>
            </div>

            <!-- 7-Day Limit -->
            <div>
              <label for="key-rate-limit-7d" class="input-label">{{ t('keys.rateLimit7d') }}</label>
              <div class="relative">
                <span class="absolute left-3 top-1/2 -translate-y-1/2 text-gray-500">$</span>
                <input
                  id="key-rate-limit-7d"
                  v-model.number="formData.rate_limit_7d"
                  type="number"
                  step="0.01"
                  min="0"
                  class="input pl-7"
                  :placeholder="'0'"
                  aria-describedby="key-rate-limit-hint"
                />
              </div>
              <!-- Usage info (edit mode only) -->
              <div v-if="showEditModal && selectedKey && selectedKey.rate_limit_7d > 0" class="mt-2">
                <div class="flex items-center gap-2">
                  <div class="flex-1 rounded-lg bg-gray-100 px-3 py-2 dark:bg-dark-700 text-sm">
                    <span :class="[
                      'font-medium',
                      selectedKey.usage_7d >= selectedKey.rate_limit_7d ? 'text-red-500' :
                      selectedKey.usage_7d >= selectedKey.rate_limit_7d * 0.8 ? 'text-yellow-500' :
                      'text-gray-900 dark:text-white'
                    ]">
                      ${{ selectedKey.usage_7d?.toFixed(4) || '0.0000' }}
                    </span>
                    <span class="mx-2 text-gray-400">/</span>
                    <span class="text-gray-500 dark:text-gray-400">
                      ${{ selectedKey.rate_limit_7d?.toFixed(2) || '0.00' }}
                    </span>
                  </div>
                </div>
                <div class="mt-1 h-1.5 w-full overflow-hidden rounded-full bg-gray-200 dark:bg-dark-600">
                  <div
                    :class="[
                      'h-full rounded-full transition-all',
                      selectedKey.usage_7d >= selectedKey.rate_limit_7d ? 'bg-red-500' :
                      selectedKey.usage_7d >= selectedKey.rate_limit_7d * 0.8 ? 'bg-yellow-500' :
                      'bg-green-500'
                    ]"
                    :style="{ width: Math.min((selectedKey.usage_7d / selectedKey.rate_limit_7d) * 100, 100) + '%' }"
                  />
                </div>
              </div>
            </div>

            <!-- Reset Rate Limit button (edit mode only) -->
            <div v-if="showEditModal && selectedKey && (selectedKey.rate_limit_5h > 0 || selectedKey.rate_limit_1d > 0 || selectedKey.rate_limit_7d > 0)">
              <button
                type="button"
                @click="confirmResetRateLimit"
                class="btn btn-secondary min-h-11 text-sm"
              >
                {{ t('keys.resetRateLimitUsage') }}
              </button>
            </div>
          </div>
        </div>

        <!-- Expiration Section -->
        <div class="space-y-3">
          <div class="flex items-center justify-between">
            <label for="key-expiration-switch" class="input-label mb-0">{{ t('keys.expiration') }}</label>
            <button
              id="key-expiration-switch"
              type="button"
              class="data-form-switch"
              role="switch"
              :aria-checked="formData.enable_expiration"
              :aria-label="t('keys.expiration')"
              :title="t('keys.expiration')"
              @click="toggleExpiration"
            >
              <span
                aria-hidden="true"
                :class="[
                  'relative inline-flex h-5 w-9 rounded-full border-2 border-transparent transition-colors duration-200 ease-in-out',
                  formData.enable_expiration ? 'bg-primary-600' : 'bg-gray-200 dark:bg-dark-600'
                ]"
              >
                <span
                  :class="[
                    'pointer-events-none inline-block h-4 w-4 transform rounded-full bg-white shadow ring-0 transition duration-200 ease-in-out',
                    formData.enable_expiration ? 'translate-x-4' : 'translate-x-0'
                  ]"
                />
              </span>
            </button>
          </div>

          <div v-if="formData.enable_expiration" class="space-y-4 pt-2">
            <!-- Quick select buttons (for both create and edit mode) -->
            <div class="flex flex-wrap gap-2" role="group" :aria-label="t('keys.expiration')">
              <button
                v-for="days in ['7', '30', '90']"
                :key="days"
                type="button"
                :aria-pressed="formData.expiration_preset === days"
                @click="setExpirationDays(parseInt(days))"
                :class="[
                  'min-h-11 rounded-lg px-3 py-1.5 text-sm transition-colors',
                  formData.expiration_preset === days
                    ? 'bg-primary-100 text-primary-700 dark:bg-primary-900/30 dark:text-primary-400'
                    : 'bg-gray-100 text-gray-600 hover:bg-gray-200 dark:bg-dark-700 dark:text-gray-400 dark:hover:bg-dark-600'
                ]"
              >
                {{ showEditModal ? t('keys.extendDays', { days }) : t('keys.expiresInDays', { days }) }}
              </button>
              <button
                type="button"
                :aria-pressed="formData.expiration_preset === 'custom'"
                @click="formData.expiration_preset = 'custom'"
                :class="[
                  'min-h-11 rounded-lg px-3 py-1.5 text-sm transition-colors',
                  formData.expiration_preset === 'custom'
                    ? 'bg-primary-100 text-primary-700 dark:bg-primary-900/30 dark:text-primary-400'
                    : 'bg-gray-100 text-gray-600 hover:bg-gray-200 dark:bg-dark-700 dark:text-gray-400 dark:hover:bg-dark-600'
                ]"
              >
                {{ t('keys.customDate') }}
              </button>
            </div>

            <!-- Date picker (always show for precise adjustment) -->
            <div>
              <label for="key-expiration-date" class="input-label">{{ t('keys.expirationDate') }}</label>
              <input
                id="key-expiration-date"
                v-model="formData.expiration_date"
                type="datetime-local"
                class="input"
                aria-describedby="key-expiration-date-hint"
                @input="formData.expiration_preset = 'custom'"
              />
              <p id="key-expiration-date-hint" class="input-hint">{{ t('keys.expirationDateHint') }}</p>
            </div>

            <!-- Current expiration display (only in edit mode) -->
            <div v-if="showEditModal && selectedKey?.expires_at" class="text-sm">
              <span class="text-gray-500 dark:text-gray-400">{{ t('keys.currentExpiration') }}: </span>
              <span class="font-medium text-gray-900 dark:text-white">
                {{ formatDateTime(selectedKey.expires_at) }}
              </span>
            </div>
          </div>
        </div>
      </form>
      <template #footer>
        <div class="flex justify-end gap-3">
          <button @click="closeModals" type="button" class="btn btn-secondary">
            {{ t('common.cancel') }}
          </button>
          <button
            form="key-form"
            type="submit"
            :disabled="submitting"
            class="btn btn-primary"
            data-tour="key-form-submit"
          >
            <svg
              v-if="submitting"
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
            {{
              submitting
                ? t('keys.saving')
                : showEditModal
                  ? t('common.update')
                  : t('common.create')
            }}
          </button>
        </div>
      </template>
    </BaseDialog>

    <!-- Delete Confirmation Dialog -->
    <ConfirmDialog
      :show="showDeleteDialog"
      :title="t('keys.deleteKey')"
      :message="t('keys.deleteConfirmMessage', { name: selectedKey?.name })"
      :confirm-text="t('common.delete')"
      :cancel-text="t('common.cancel')"
      :danger="true"
      @confirm="handleDelete"
      @cancel="showDeleteDialog = false"
    />

    <ApiKeyAccountShareConflictDialog
      :show="accountShareConflict.show"
      :action="accountShareConflict.action"
      :key-name="accountShareConflict.key?.name || ''"
      :active-count="accountShareConflict.activeCount"
      :queued-count="accountShareConflict.queuedCount"
      :ending-count="accountShareConflict.endingCount"
      :navigating="accountShareConflictNavigating"
      @close="closeAccountShareConflict"
      @resolve="navigateToAccountShareResolution"
    />

    <!-- Reset Quota Confirmation Dialog -->
    <ConfirmDialog
      :show="showResetQuotaDialog"
      :title="t('keys.resetQuotaTitle')"
      :message="t('keys.resetQuotaConfirmMessage', { name: selectedKey?.name, used: selectedKey?.quota_used?.toFixed(4) })"
      :confirm-text="t('keys.reset')"
      :cancel-text="t('common.cancel')"
      :danger="true"
      @confirm="resetQuotaUsed"
      @cancel="showResetQuotaDialog = false"
    />

    <!-- Reset Rate Limit Confirmation Dialog -->
    <ConfirmDialog
      :show="showResetRateLimitDialog"
      :title="t('keys.resetRateLimitTitle')"
      :message="t('keys.resetRateLimitConfirmMessage', { name: selectedKey?.name })"
      :confirm-text="t('keys.reset')"
      :cancel-text="t('common.cancel')"
      :danger="true"
      @confirm="resetRateLimitUsage"
      @cancel="showResetRateLimitDialog = false"
    />

    <!-- Use Key Modal -->
    <UseKeyModal
      :show="showUseKeyModal"
      :api-key="selectedKey?.key || ''"
      :base-url="publicSettings?.api_base_url || ''"
      :platform="selectedKey?.group?.platform || null"
      :allow-messages-dispatch="selectedKey?.group?.allow_messages_dispatch || false"
      @close="closeUseKeyModal"
    />

    <BaseDialog
      :show="showImagePlaygroundModelDialog"
      :title="t('keys.imagePlaygroundModelDialog.title')"
      width="narrow"
      @close="closeImagePlaygroundModelDialog"
    >
      <form
        id="image-playground-model-form"
        class="space-y-4"
        @submit.prevent="confirmOpenImagePlayground"
      >
        <p class="text-sm leading-6 text-gray-600 dark:text-gray-400">
          {{ t('keys.imagePlaygroundModelDialog.description') }}
        </p>
        <div>
          <label for="image-playground-model" class="input-label">
            {{ t('keys.imagePlaygroundModelDialog.modelLabel') }}
          </label>
          <input
            id="image-playground-model"
            v-model="imagePlaygroundModel"
            type="text"
            required
            autocomplete="off"
            class="input min-h-11 w-full"
            :placeholder="t('keys.imagePlaygroundModelDialog.modelPlaceholder')"
          />
        </div>
      </form>
      <template #footer>
        <div class="flex w-full flex-col-reverse gap-2 sm:flex-row sm:justify-end">
          <button
            type="button"
            class="btn btn-secondary min-h-11 w-full sm:w-auto"
            @click="closeImagePlaygroundModelDialog"
          >
            {{ t('common.cancel') }}
          </button>
          <button
            type="submit"
            form="image-playground-model-form"
            class="btn btn-primary min-h-11 w-full sm:w-auto"
            :disabled="!canOpenImagePlayground"
          >
            {{ t('keys.imagePlaygroundModelDialog.open') }}
          </button>
        </div>
      </template>
    </BaseDialog>

    <!-- CCS Client Selection Dialog for Antigravity -->
    <BaseDialog
      :show="showCcsClientSelect"
      :title="t('keys.ccsClientSelect.title')"
      width="narrow"
      @close="closeCcsClientSelect"
    >
      <div class="space-y-4">
        <p class="text-sm text-gray-600 dark:text-gray-400">
          {{ t('keys.ccsClientSelect.description') }}
	        </p>
	        <div class="grid grid-cols-2 gap-3">
	          <button
	            @click="handleCcsClientSelect('claude')"
	            class="flex flex-col items-center gap-2 p-4 rounded-xl border-2 border-gray-200 dark:border-dark-600 hover:border-primary-500 dark:hover:border-primary-500 hover:bg-primary-50 dark:hover:bg-primary-900/20 transition-all"
	          >
	            <Icon name="terminal" size="xl" class="text-gray-600 dark:text-gray-400" />
	            <span class="font-medium text-gray-900 dark:text-white">{{
	              t('keys.ccsClientSelect.claudeCode')
	            }}</span>
	            <span class="text-xs text-gray-500 dark:text-gray-400">{{
	              t('keys.ccsClientSelect.claudeCodeDesc')
	            }}</span>
	          </button>
	          <button
	            @click="handleCcsClientSelect('gemini')"
	            class="flex flex-col items-center gap-2 p-4 rounded-xl border-2 border-gray-200 dark:border-dark-600 hover:border-primary-500 dark:hover:border-primary-500 hover:bg-primary-50 dark:hover:bg-primary-900/20 transition-all"
	          >
	            <Icon name="sparkles" size="xl" class="text-gray-600 dark:text-gray-400" />
	            <span class="font-medium text-gray-900 dark:text-white">{{
	              t('keys.ccsClientSelect.geminiCli')
	            }}</span>
	            <span class="text-xs text-gray-500 dark:text-gray-400">{{
	              t('keys.ccsClientSelect.geminiCliDesc')
	            }}</span>
	          </button>
	        </div>
	      </div>
      <template #footer>
        <div class="flex justify-end">
          <button @click="closeCcsClientSelect" class="btn btn-secondary">
            {{ t('common.cancel') }}
          </button>
        </div>
      </template>
    </BaseDialog>

    <!-- Group Selector Dropdown (Teleported to body to avoid overflow clipping) -->
    <Teleport to="body">
      <div
        v-if="groupSelectorKeyId !== null && dropdownPosition"
        :id="getGroupSelectorPopoverId(groupSelectorKeyId)"
        ref="dropdownRef"
        :data-ui-skin="uiSkin"
        class="data-teleport-panel animate-in fade-in slide-in-from-top-2 fixed z-[100000020] flex max-h-[calc(100dvh-2rem)] min-w-0 w-[calc(100vw-2rem)] max-w-[380px] flex-col duration-200"
        style="pointer-events: auto !important;"
        role="dialog"
        :aria-label="t('keys.groupSelectorLabel')"
        :style="{
          top: dropdownPosition.top + 'px',
          left: dropdownPosition.left + 'px'
        }"
        @click.stop
        @keydown="handleGroupSelectorKeydown"
      >
        <!-- Search box -->
        <div class="flex-shrink-0 border-b border-line p-2">
          <div class="relative">
            <svg class="absolute left-2.5 top-1/2 h-4 w-4 -translate-y-1/2 text-content-subtle" fill="none" stroke="currentColor" viewBox="0 0 24 24" stroke-width="2">
              <path stroke-linecap="round" stroke-linejoin="round" d="M21 21l-6-6m2-5a7 7 0 11-14 0 7 7 0 0114 0z" />
            </svg>
            <input
              :id="getGroupSelectorSearchId(groupSelectorKeyId)"
              ref="groupSearchInputRef"
              v-model="groupSearchQuery"
              type="text"
              class="input min-h-11 py-1.5 pl-8 pr-3"
              :placeholder="t('keys.searchGroup')"
              :aria-label="t('keys.searchGroup')"
              :aria-controls="getGroupSelectorListId(groupSelectorKeyId)"
              autocomplete="off"
              @click.stop
            />
          </div>
        </div>
        <!-- Group list -->
        <div
          :id="getGroupSelectorListId(groupSelectorKeyId)"
          class="min-h-0 flex-1 overflow-y-auto p-1.5"
          role="group"
          :aria-label="t('keys.groupLabel')"
        >
          <button
            v-for="option in filteredGroupOptions"
            :key="option.value ?? 'null'"
            type="button"
            :aria-pressed="
              selectedKeyForGroup?.group_id === option.value ||
              (!selectedKeyForGroup?.group_id && option.value === null)
            "
            @click="changeGroup(selectedKeyForGroup!, option.value)"
            :class="[
              'data-teleport-option',
              selectedKeyForGroup?.group_id === option.value ||
              (!selectedKeyForGroup?.group_id && option.value === null)
                ? 'bg-brand-soft'
                : ''
            ]"
            :title="option.description || undefined"
          >
            <GroupOptionItem
              :name="option.label"
              :platform="option.platform"
              :scope="option.scope"
              :subscription-type="option.subscriptionType"
              :rate-multiplier="option.rate"
              :user-rate-multiplier="option.userRate"
              :effective-rate-multiplier="option.effectiveRate"
              :rate-multiplier-source="option.rateSource"
              :description="option.description"
              :api-key-badge-type="option.apiKeyBadgeType"
              :api-key-badge-text="option.apiKeyBadgeText"
              :selected="
                selectedKeyForGroup?.group_id === option.value ||
                (!selectedKeyForGroup?.group_id && option.value === null)
              "
            />
          </button>
          <!-- Empty state when search has no results -->
          <div v-if="filteredGroupOptions.length === 0" class="py-4 text-center text-sm text-content-subtle">
            {{ t('keys.noGroupFound') }}
          </div>
        </div>
      </div>
    </Teleport>
  </AppLayout>
</template>

<script setup lang="ts">
	import { ref, reactive, computed, nextTick, onMounted, onUnmounted, type ComponentPublicInstance } from 'vue'
	import { useI18n } from 'vue-i18n'
	import { useRouter } from 'vue-router'
	import { useAppStore } from '@/stores/app'
	import { useOnboardingStore } from '@/stores/onboarding'
	import { useClipboard } from '@/composables/useClipboard'
import { getPersistedPageSize } from '@/composables/usePersistedPageSize'
import { useUiSkin } from '@/composables/useUiSkin'

const { t } = useI18n()
const uiSkin = useUiSkin()
const router = useRouter()
import { keysAPI, authAPI, usageAPI, userGroupsAPI, accountShareAPI } from '@/api'
import AppLayout from '@/components/layout/AppLayout.vue'
import TablePageLayout from '@/components/layout/TablePageLayout.vue'
	import DataTable from '@/components/common/DataTable.vue'
	import Pagination from '@/components/common/Pagination.vue'
	import BaseDialog from '@/components/common/BaseDialog.vue'
	import ConfirmDialog from '@/components/common/ConfirmDialog.vue'
	import EmptyState from '@/components/common/EmptyState.vue'
	import Select, { type SelectOption } from '@/components/common/Select.vue'
	import SearchInput from '@/components/common/SearchInput.vue'
	import Icon from '@/components/icons/Icon.vue'
	import UseKeyModal from '@/components/keys/UseKeyModal.vue'
	import ApiKeyAccountShareConflictDialog from '@/components/keys/ApiKeyAccountShareConflictDialog.vue'
	import EndpointPopover from '@/components/keys/EndpointPopover.vue'
	import GroupBadge from '@/components/common/GroupBadge.vue'
	import GroupApiKeyBadge from '@/components/common/GroupApiKeyBadge.vue'
	import GroupOptionItem from '@/components/common/GroupOptionItem.vue'
	import type {
	  ApiKey,
	  ApiKeyGroupBadgeType,
	  ApiKeyGroupRoute,
	  Group,
	  PublicSettings,
	  SubscriptionType,
	  GroupPlatform,
	  GroupScope
	} from '@/types'
import type { Column } from '@/components/common/types'
import type { BatchApiKeyUsageStats } from '@/api/usage'
import type { AccountShareAPIKeyBindingStatus } from '@/api/accountShare'
import { formatDateTime } from '@/utils/format'
import { maskApiKey } from '@/utils/maskApiKey'
import { buildCcSwitchImportDeeplink } from '@/utils/ccswitchImport'
import { buildImagePlaygroundImportUrl } from '@/utils/imagePlaygroundImport'
import { extractApiErrorCode, extractApiErrorMessage } from '@/utils/apiError'
import {
  calculateExpirationPresetDate,
  resolveExpirationPresetBase
} from '@/utils/apiKeyExpiration'

type AccountShareBlockedAction = 'delete' | 'change_group'

interface AccountShareConflictState {
  show: boolean
  action: AccountShareBlockedAction
  key: ApiKey | null
  activeCount: number | null
  queuedCount: number | null
  endingCount: number | null
}

// Helper to format date for datetime-local input
const formatDateTimeLocal = (isoDate: string): string => {
  const date = new Date(isoDate)
  const pad = (n: number) => n.toString().padStart(2, '0')
  return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())}T${pad(date.getHours())}:${pad(date.getMinutes())}`
}

interface GroupOption extends SelectOption {
  value: number
  label: string
  description: string | null
  rate: number
  userRate: number | null
  effectiveRate: number | null
  rateSource: string | null
  subscriptionType: SubscriptionType
  platform: GroupPlatform
  scope?: GroupScope
  apiKeyBadgeType: ApiKeyGroupBadgeType
  apiKeyBadgeText: string
}

interface ApiKeyGroupRouteForm {
  group_id: number | null
  priority: number
  weight: number
  enabled: boolean
  cooldown_seconds: number
}

const defaultGroupRoute = (groupId: number | null = null): ApiKeyGroupRouteForm => ({
  group_id: groupId,
  priority: 100,
  weight: 1,
  enabled: true,
  cooldown_seconds: 30
})

const appStore = useAppStore()
const onboardingStore = useOnboardingStore()
const { copyToClipboard: clipboardCopy } = useClipboard()

const allColumns = computed<Column[]>(() => [
  { key: 'name', label: t('common.name'), sortable: true },
  { key: 'id', label: t('keys.id'), sortable: true },
  { key: 'key', label: t('keys.apiKey'), sortable: false },
  { key: 'group', label: t('keys.group'), sortable: false },
  { key: 'usage', label: t('keys.usage'), sortable: false },
  { key: 'rate_limit', label: t('keys.rateLimitColumn'), sortable: false },
  { key: 'current_concurrency', label: t('keys.currentConcurrency'), sortable: false },
  { key: 'expires_at', label: t('keys.expiresAt'), sortable: true },
  { key: 'status', label: t('common.status'), sortable: true },
  { key: 'last_used_at', label: t('keys.lastUsedAt'), sortable: true },
  { key: 'last_used_ip', label: t('keys.lastUsedIP'), sortable: false },
  { key: 'created_at', label: t('keys.created'), sortable: true },
  { key: 'actions', label: t('common.actions'), sortable: false }
])

const ALWAYS_VISIBLE_COLUMNS = new Set(['name', 'actions'])
const DEFAULT_HIDDEN_COLUMNS = ['id', 'last_used_ip']
const COLUMN_SETTINGS_MENU_ID = 'api-key-column-settings-menu'
const HIDDEN_COLUMNS_KEY = 'api-key-hidden-columns'
const COLUMN_SETTINGS_VERSION_KEY = 'api-key-column-settings-version'
const COLUMN_SETTINGS_VERSION = 4
const VERSION_NEW_HIDDEN_COLUMNS: Record<number, string[]> = {
  3: ['id'],
  4: ['last_used_ip']
}

const hiddenColumns = reactive<Set<string>>(new Set())
const showColumnDropdown = ref(false)
const columnDropdownRef = ref<HTMLElement | null>(null)
const columnSettingsTriggerRef = ref<HTMLButtonElement | null>(null)
const columnMenuFocusIndex = ref(0)

const toggleableColumns = computed(() =>
  allColumns.value.filter((column) => !ALWAYS_VISIBLE_COLUMNS.has(column.key))
)

const columns = computed(() =>
  allColumns.value.filter((column) =>
    ALWAYS_VISIBLE_COLUMNS.has(column.key) || !hiddenColumns.has(column.key)
  )
)

const getValidHiddenColumnKeys = () =>
  new Set(toggleableColumns.value.map((column) => column.key))

const saveColumnsToStorage = () => {
  try {
    const validKeys = getValidHiddenColumnKeys()
    const keys = [...hiddenColumns].filter((key) => validKeys.has(key))
    localStorage.setItem(HIDDEN_COLUMNS_KEY, JSON.stringify(keys))
    localStorage.setItem(COLUMN_SETTINGS_VERSION_KEY, String(COLUMN_SETTINGS_VERSION))
  } catch (error) {
    console.error('Failed to save API key column settings:', error)
  }
}

const loadSavedColumns = () => {
  hiddenColumns.clear()
  try {
    const validKeys = getValidHiddenColumnKeys()
    const saved = localStorage.getItem(HIDDEN_COLUMNS_KEY)
    if (!saved) {
      DEFAULT_HIDDEN_COLUMNS.forEach((key) => {
        if (validKeys.has(key)) hiddenColumns.add(key)
      })
      saveColumnsToStorage()
      return
    }

    const parsed = JSON.parse(saved)
    if (Array.isArray(parsed)) {
      parsed
        .filter((key): key is string => typeof key === 'string' && validKeys.has(key))
        .forEach((key) => hiddenColumns.add(key))
    }

    const rawVersion = Number(localStorage.getItem(COLUMN_SETTINGS_VERSION_KEY) ?? '1')
    const storedVersion = Number.isInteger(rawVersion) && rawVersion >= 1 ? rawVersion : 1
    if (storedVersion < COLUMN_SETTINGS_VERSION) {
      for (let version = storedVersion + 1; version <= COLUMN_SETTINGS_VERSION; version += 1) {
        for (const key of VERSION_NEW_HIDDEN_COLUMNS[version] ?? []) {
          if (validKeys.has(key)) hiddenColumns.add(key)
        }
      }
      saveColumnsToStorage()
    }
  } catch (error) {
    console.error('Failed to load API key column settings:', error)
    DEFAULT_HIDDEN_COLUMNS.forEach((key) => hiddenColumns.add(key))
  }
}

const toggleColumn = (key: string) => {
  if (hiddenColumns.has(key)) hiddenColumns.delete(key)
  else hiddenColumns.add(key)
  saveColumnsToStorage()
}

const isColumnVisible = (key: string) => !hiddenColumns.has(key)

const getColumnMenuItems = (): HTMLButtonElement[] => {
  if (!columnDropdownRef.value) return []
  return Array.from(
    columnDropdownRef.value.querySelectorAll<HTMLButtonElement>('[data-column-menu-item]')
  )
}

const focusColumnMenuItem = (index: number) => {
  const items = getColumnMenuItems()
  if (items.length === 0) return
  const normalizedIndex = (index + items.length) % items.length
  columnMenuFocusIndex.value = normalizedIndex
  items[normalizedIndex].focus()
}

const closeColumnSettings = (restoreFocus: boolean) => {
  if (!showColumnDropdown.value) return
  showColumnDropdown.value = false
  if (restoreFocus) {
    void nextTick(() => columnSettingsTriggerRef.value?.focus())
  }
}

const toggleColumnSettings = () => {
  if (showColumnDropdown.value) {
    closeColumnSettings(false)
    return
  }
  closeGroupSelectorPopover(false)
  showColumnDropdown.value = true
  columnMenuFocusIndex.value = 0
  void nextTick(() => focusColumnMenuItem(0))
}

const handleColumnSettingsKeydown = (event: KeyboardEvent) => {
  if (event.key === 'Escape') {
    event.preventDefault()
    event.stopPropagation()
    closeColumnSettings(true)
    return
  }

  const items = getColumnMenuItems()
  if (items.length === 0) return
  const activeIndex = items.findIndex((item) => item === document.activeElement)
  const currentIndex = activeIndex >= 0 ? activeIndex : columnMenuFocusIndex.value
  let nextIndex: number | null = null

  if (event.key === 'ArrowDown') nextIndex = currentIndex + 1
  else if (event.key === 'ArrowUp') nextIndex = currentIndex - 1
  else if (event.key === 'Home') nextIndex = 0
  else if (event.key === 'End') nextIndex = items.length - 1

  if (nextIndex !== null) {
    event.preventDefault()
    focusColumnMenuItem(nextIndex)
  }
}

const handleColumnSettingsFocusout = () => {
  void nextTick(() => {
    const activeElement = document.activeElement
    if (
      showColumnDropdown.value &&
      activeElement instanceof Node &&
      !columnDropdownRef.value?.contains(activeElement)
    ) {
      closeColumnSettings(false)
    }
  })
}

const apiKeys = ref<ApiKey[]>([])
const groups = ref<Group[]>([])
const loading = ref(false)
const submitting = ref(false)
const now = ref(new Date())
let resetTimer: ReturnType<typeof setInterval> | null = null
const usageStats = ref<Record<string, BatchApiKeyUsageStats>>({})
const usageStatsLoading = ref(false)
const usageStatsError = ref(false)
let usageStatsRequestSequence = 0
const userGroupRates = ref<Record<number, number>>({})

const pagination = ref({
  page: 1,
  page_size: getPersistedPageSize(),
  total: 0,
  pages: 0
})
const sortState = ref({
  sort_by: 'created_at',
  sort_order: 'desc' as 'asc' | 'desc'
})

// Filter state
const filterSearch = ref('')
const filterStatus = ref('')
const filterGroupId = ref<string | number>('')

const showCreateModal = ref(false)
const showEditModal = ref(false)
const editExpirationPresetBase = ref<Date | null>(null)
const customKeyInputRef = ref<HTMLInputElement | null>(null)
const customKeyTouched = ref(false)
const customKeySubmitAttempted = ref(false)
const showDeleteDialog = ref(false)
const showResetQuotaDialog = ref(false)
const showResetRateLimitDialog = ref(false)
const showUseKeyModal = ref(false)
const showCcsClientSelect = ref(false)
const pendingCcsRow = ref<ApiKey | null>(null)
const showImagePlaygroundModelDialog = ref(false)
const pendingImagePlaygroundRow = ref<ApiKey | null>(null)
const IMAGE_PLAYGROUND_DEFAULT_MODEL = 'gpt-image-2'
const imagePlaygroundModel = ref('')
const canOpenImagePlayground = computed(() => imagePlaygroundModel.value.trim().length > 0)
const selectedKey = ref<ApiKey | null>(null)
const copiedKeyId = ref<number | null>(null)
const groupSelectorKeyId = ref<number | null>(null)
const publicSettings = ref<PublicSettings | null>(null)
const dropdownRef = ref<HTMLElement | null>(null)
const groupSearchInputRef = ref<HTMLInputElement | null>(null)
const dropdownPosition = ref<{ top: number; left: number } | null>(null)
const groupButtonRefs = ref<Map<number, HTMLElement>>(new Map())
const accountShareBindingChecks = new Map<number, Promise<AccountShareAPIKeyBindingStatus>>()
const accountShareConflictNavigating = ref(false)
const accountShareConflict = ref<AccountShareConflictState>({
  show: false,
  action: 'change_group',
  key: null,
  activeCount: null,
  queuedCount: null,
  endingCount: null
})
let abortController: AbortController | null = null
let copiedKeyResetTimer: ReturnType<typeof setTimeout> | null = null
const ccSwitchDetectionTimers = new Set<ReturnType<typeof setTimeout>>()

const getGroupSelectorPopoverId = (keyId: number) => `api-key-group-selector-${keyId}`
const getGroupSelectorSearchId = (keyId: number) => `api-key-group-selector-search-${keyId}`
const getGroupSelectorListId = (keyId: number) => `api-key-group-selector-list-${keyId}`

const isAbortError = (error: unknown) => {
  if (!error || typeof error !== 'object') return false
  const { name, code } = error as { name?: string; code?: string }
  return name === 'AbortError' || code === 'ERR_CANCELED'
}

const loadApiKeyUsageStats = async (keyIds: number[], signal: AbortSignal) => {
  const requestSequence = ++usageStatsRequestSequence
  usageStatsLoading.value = keyIds.length > 0
  usageStatsError.value = false
  usageStats.value = {}
  if (keyIds.length === 0) return

  try {
    const response = await usageAPI.getDashboardApiKeysUsage(keyIds, { signal })
    if (signal.aborted || requestSequence !== usageStatsRequestSequence) return
    usageStats.value = response.stats
  } catch (error) {
    if (signal.aborted || requestSequence !== usageStatsRequestSequence || isAbortError(error)) return
    usageStatsError.value = true
    console.error('Failed to load usage stats:', error)
  } finally {
    if (requestSequence === usageStatsRequestSequence) {
      usageStatsLoading.value = false
    }
  }
}

// Get the currently selected key for group change
const selectedKeyForGroup = computed(() => {
  if (groupSelectorKeyId.value === null) return null
  return apiKeys.value.find((k) => k.id === groupSelectorKeyId.value) || null
})

const setGroupButtonRef = (keyId: number, el: Element | ComponentPublicInstance | null) => {
  if (el instanceof HTMLElement) {
    groupButtonRefs.value.set(keyId, el)
  } else {
    groupButtonRefs.value.delete(keyId)
  }
}

const formData = ref({
  name: '',
  group_id: null as number | null,
  status: 'active' as 'active' | 'inactive',
  use_custom_key: false,
  custom_key: '',
  enable_ip_restriction: false,
  ip_whitelist: '',
  ip_blacklist: '',
  // Quota settings (empty = unlimited)
  enable_quota: false,
  quota: null as number | null,
  // Rate limit settings
  enable_rate_limit: false,
  rate_limit_5h: null as number | null,
  rate_limit_1d: null as number | null,
  rate_limit_7d: null as number | null,
  enable_group_routes: false,
  group_routes: [defaultGroupRoute()] as ApiKeyGroupRouteForm[],
  enable_expiration: false,
  expiration_preset: '30' as '7' | '30' | '90' | 'custom',
  expiration_date: ''
})

// 自定义Key验证
const customKeyError = computed(() => {
  if (!formData.value.use_custom_key) {
    return ''
  }
  const key = formData.value.custom_key
  if (!key.trim()) {
    return customKeyTouched.value || customKeySubmitAttempted.value
      ? t('keys.customKeyRequired')
      : ''
  }
  if (key.length < 16) {
    return t('keys.customKeyTooShort')
  }
  if (key.length > 128) {
    return t('keys.customKeyTooLong')
  }
  // 检查字符：只允许字母、数字、下划线、连字符
  if (!/^[a-zA-Z0-9_-]+$/.test(key)) {
    return t('keys.customKeyInvalidChars')
  }
  return ''
})

const statusOptions = computed(() => [
  { value: 'active', label: t('common.active') },
  { value: 'inactive', label: t('common.inactive') }
])

// Filter dropdown options
const groupFilterOptions = computed(() => [
  { value: '', label: t('keys.allGroups') },
  { value: 0, label: t('keys.noGroup') },
  ...groups.value.map((g) => ({ value: g.id, label: g.name }))
])

const effectiveRateByGroupId = computed(() => {
  const result: Record<number, { multiplier: number | null; source: string | null }> = {}
  groups.value.forEach((group) => {
    const fallbackUserRate = userGroupRates.value[group.id] ?? null
    result[group.id] = {
      multiplier: group.effective_rate_multiplier ?? fallbackUserRate,
      source: group.effective_rate_multiplier_source ?? (fallbackUserRate != null ? 'user_group' : null)
    }
  })
  return result
})

const effectiveRateForGroup = (group: Group): number | null => {
  return group.effective_rate_multiplier ??
    effectiveRateByGroupId.value[group.id]?.multiplier ??
    userGroupRates.value[group.id] ??
    null
}

const effectiveRateSourceForGroup = (group: Group): string | null => {
  return group.effective_rate_multiplier_source ??
    effectiveRateByGroupId.value[group.id]?.source ??
    (userGroupRates.value[group.id] != null ? 'user_group' : null)
}

const statusFilterOptions = computed(() => [
  { value: '', label: t('keys.allStatus') },
  { value: 'active', label: t('keys.status.active') },
  { value: 'inactive', label: t('keys.status.inactive') },
  { value: 'quota_exhausted', label: t('keys.status.quota_exhausted') },
  { value: 'expired', label: t('keys.status.expired') }
])

const onFilterChange = () => {
  pagination.value.page = 1
  loadApiKeys()
}

const onGroupFilterChange = (value: string | number | boolean | null) => {
  filterGroupId.value = value as string | number
  onFilterChange()
}

const onStatusFilterChange = (value: string | number | boolean | null) => {
  filterStatus.value = value as string
  onFilterChange()
}

// Convert groups to Select options format with rate multiplier and subscription type
const groupOptions = computed<GroupOption[]>(() =>
  groups.value.map((group) => {
    const fallbackUserRate = userGroupRates.value[group.id] ?? null
    return {
      value: group.id,
      label: group.name,
      description: group.description,
      rate: group.rate_multiplier,
      userRate: fallbackUserRate,
      effectiveRate: group.effective_rate_multiplier ?? fallbackUserRate,
      rateSource: group.effective_rate_multiplier_source ?? (fallbackUserRate != null ? 'user_group' : 'group_default'),
      subscriptionType: group.subscription_type,
      platform: group.platform,
      scope: group.scope,
      apiKeyBadgeType: group.api_key_badge_type,
      apiKeyBadgeText: group.api_key_badge_text
    }
  })
)

// 平台隔离：同一把 Key 的多条路由必须落在同一平台。第一条选定分组的平台即锁定整条链，
// 后续下拉只列同平台分组。已选中的分组始终保留在选项里，否则存量的跨平台 Key 打开
// 编辑框会看到空白下拉。
const routeLockedPlatform = computed<string | null>(() => {
  for (const route of formData.value.group_routes) {
    if (route.group_id === null) continue
    const option = groupOptions.value.find((opt) => opt.value === route.group_id)
    if (option?.platform) return option.platform
  }
  return null
})

const routeGroupOptions = computed<GroupOption[]>(() => {
  const platform = routeLockedPlatform.value
  if (!platform) return groupOptions.value
  const selectedIds = new Set(
    formData.value.group_routes
      .map((route) => route.group_id)
      .filter((id): id is number => id !== null)
  )
  return groupOptions.value.filter(
    (opt) => opt.platform === platform || selectedIds.has(opt.value)
  )
})

const createRoutesFromKey = (key: ApiKey): ApiKeyGroupRouteForm[] => {
  if (key.group_routes && key.group_routes.length > 0) {
    return key.group_routes.map((route) => ({
      group_id: route.group_id,
      priority: route.priority,
      weight: route.weight,
      enabled: route.enabled === false ? false : true,
      cooldown_seconds: route.cooldown_seconds
    }))
  }
  return [defaultGroupRoute(key.group_id)]
}

const toggleGroupRoutes = () => {
  formData.value.enable_group_routes = !formData.value.enable_group_routes
  if (
    formData.value.enable_group_routes &&
    (formData.value.group_routes.length === 0 || formData.value.group_routes.every((route) => route.group_id === null))
  ) {
    formData.value.group_routes = [defaultGroupRoute(formData.value.group_id)]
  }
}

const addGroupRoute = () => {
  formData.value.group_routes.push(defaultGroupRoute())
}

const removeGroupRoute = (index: number) => {
  if (formData.value.group_routes.length <= 1) return
  formData.value.group_routes.splice(index, 1)
}

const normalizeGroupRoutes = (): ApiKeyGroupRoute[] | null => {
  if (!formData.value.enable_group_routes) {
    if (formData.value.group_id === null) return []
    return [{
      group_id: formData.value.group_id,
      priority: 100,
      weight: 1,
      enabled: true,
      cooldown_seconds: 30
    }]
  }

  const routes: ApiKeyGroupRoute[] = []
  const seenGroupIds = new Set<number>()
  for (const route of formData.value.group_routes) {
    if (route.group_id === null) {
      appStore.showError(t('keys.groupRoutes.validation.groupRequired'))
      return null
    }
    if (seenGroupIds.has(route.group_id)) {
      appStore.showError(t('keys.groupRoutes.validation.duplicateGroup'))
      return null
    }
    if (!Number.isInteger(route.priority) || route.priority < 1) {
      appStore.showError(t('keys.groupRoutes.validation.priority'))
      return null
    }
    if (!Number.isInteger(route.cooldown_seconds) || route.cooldown_seconds < 0) {
      appStore.showError(t('keys.groupRoutes.validation.cooldownSeconds'))
      return null
    }
    seenGroupIds.add(route.group_id)
    routes.push({
      group_id: route.group_id,
      priority: route.priority,
      weight: route.weight,
      enabled: route.enabled === true,
      cooldown_seconds: route.cooldown_seconds
    })
  }

  return routes
}

const resolvePrimaryGroupId = (routes: ApiKeyGroupRoute[] | null): number | null => {
  if (!routes || routes.length === 0) return formData.value.group_id
  const enabledRoutes = routes.filter((route) => route.enabled)
  if (enabledRoutes.length === 0) return formData.value.group_id
  return enabledRoutes.reduce((best, route) => (
    route.priority < best.priority ? route : best
  )).group_id
}

const canonicalGroupRouteSignatures = (
  groupId: number | null,
  routes?: ApiKeyGroupRoute[]
): string[] => {
  const canonicalRoutes = routes && routes.length > 0
    ? routes
    : groupId === null
      ? []
      : [{
          group_id: groupId,
          priority: 100,
          weight: 1,
          enabled: true,
          cooldown_seconds: 30
        }]
  return canonicalRoutes
    .map((route) => [
      route.group_id,
      route.priority,
      route.weight,
      route.enabled,
      route.cooldown_seconds
    ].join(':'))
    .sort()
}

const groupRoutingChanged = (
  key: ApiKey,
  nextGroupId: number | null,
  nextRoutes: ApiKeyGroupRoute[]
): boolean => (
  key.group_id !== nextGroupId ||
  canonicalGroupRouteSignatures(key.group_id, key.group_routes).join('|') !==
    canonicalGroupRouteSignatures(nextGroupId, nextRoutes).join('|')
)

// Group dropdown search
const groupSearchQuery = ref('')
const filteredGroupOptions = computed(() => {
  const query = groupSearchQuery.value.trim().toLowerCase()
  if (!query) return groupOptions.value
  return groupOptions.value.filter((opt) => {
    return opt.label.toLowerCase().includes(query) ||
      (opt.description && opt.description.toLowerCase().includes(query))
  })
})

const copyToClipboard = async (text: string, keyId: number) => {
  const success = await clipboardCopy(text, t('keys.copied'))
  if (success) {
    if (copiedKeyResetTimer !== null) clearTimeout(copiedKeyResetTimer)
    copiedKeyId.value = keyId
    const timer = setTimeout(() => {
      if (copiedKeyResetTimer !== timer) return
      copiedKeyId.value = null
      copiedKeyResetTimer = null
    }, 800)
    copiedKeyResetTimer = timer
  }
}

const loadApiKeys = async () => {
  abortController?.abort()
  usageStatsRequestSequence += 1
  usageStats.value = {}
  usageStatsLoading.value = false
  usageStatsError.value = false
  const controller = new AbortController()
  abortController = controller
  const { signal } = controller
  loading.value = true
  try {
    // Build filters
    const filters: {
      search?: string
      status?: string
      group_id?: number | string
      sort_by?: string
      sort_order?: 'asc' | 'desc'
    } = {}
    if (filterSearch.value) filters.search = filterSearch.value
    if (filterStatus.value) filters.status = filterStatus.value
    if (filterGroupId.value !== '') filters.group_id = filterGroupId.value
    filters.sort_by = sortState.value.sort_by
    filters.sort_order = sortState.value.sort_order

    const response = await keysAPI.list(pagination.value.page, pagination.value.page_size, filters, {
      signal
    })
    if (signal.aborted) return
    apiKeys.value = response.items
    pagination.value.total = response.total
    pagination.value.pages = response.pages

    void loadApiKeyUsageStats(response.items.map((key) => key.id), signal)
  } catch (error) {
    if (isAbortError(error)) {
      return
    }
    appStore.showError(t('keys.failedToLoad'))
  } finally {
    if (abortController === controller) {
      loading.value = false
    }
  }
}

const loadGroups = async () => {
  try {
    groups.value = await userGroupsAPI.getAvailable()
  } catch (error) {
    console.error('Failed to load groups:', error)
  }
}

const loadUserGroupRates = async () => {
  try {
    userGroupRates.value = await userGroupsAPI.getUserGroupRates()
  } catch (error) {
    console.error('Failed to load user group rates:', error)
  }
}

const loadPublicSettings = async () => {
  try {
    publicSettings.value = await authAPI.getPublicSettings()
  } catch (error) {
    console.error('Failed to load public settings:', error)
  }
}

const openUseKeyModal = (key: ApiKey) => {
  selectedKey.value = key
  showUseKeyModal.value = true
}

const closeUseKeyModal = () => {
  showUseKeyModal.value = false
  selectedKey.value = null
}

const handlePageChange = (page: number) => {
  pagination.value.page = page
  loadApiKeys()
}

const handlePageSizeChange = (pageSize: number) => {
  pagination.value.page_size = pageSize
  pagination.value.page = 1
  loadApiKeys()
}

const refreshKeyPageData = async () => {
  await Promise.all([loadApiKeys(), loadGroups(), loadUserGroupRates()])
}

const handleSort = (key: string, order: 'asc' | 'desc') => {
  sortState.value.sort_by = key
  sortState.value.sort_order = order
  pagination.value.page = 1
  loadApiKeys()
}

const editKey = (key: ApiKey) => {
  selectedKey.value = key
  editExpirationPresetBase.value = resolveExpirationPresetBase(key.expires_at, new Date())
  const hasIPRestriction = (key.ip_whitelist?.length > 0) || (key.ip_blacklist?.length > 0)
  const hasExpiration = !!key.expires_at
  const groupRoutes = createRoutesFromKey(key)
  formData.value = {
    name: key.name,
    group_id: key.group_id,
    status: key.status === 'quota_exhausted' || key.status === 'expired' ? 'inactive' : key.status,
    use_custom_key: false,
    custom_key: '',
    enable_ip_restriction: hasIPRestriction,
    ip_whitelist: (key.ip_whitelist || []).join('\n'),
    ip_blacklist: (key.ip_blacklist || []).join('\n'),
    enable_quota: key.quota > 0,
    quota: key.quota > 0 ? key.quota : null,
    enable_rate_limit: (key.rate_limit_5h > 0) || (key.rate_limit_1d > 0) || (key.rate_limit_7d > 0),
    rate_limit_5h: key.rate_limit_5h || null,
    rate_limit_1d: key.rate_limit_1d || null,
    rate_limit_7d: key.rate_limit_7d || null,
    enable_group_routes: (key.group_routes?.length ?? 0) > 1,
    group_routes: groupRoutes,
    enable_expiration: hasExpiration,
    expiration_preset: 'custom',
    expiration_date: key.expires_at ? formatDateTimeLocal(key.expires_at) : ''
  }
  showEditModal.value = true
}

const toggleKeyStatus = async (key: ApiKey) => {
  const newStatus = key.status === 'active' ? 'inactive' : 'active'
  try {
    await keysAPI.toggleStatus(key.id, newStatus)
    appStore.showSuccess(
      newStatus === 'active' ? t('keys.keyEnabledSuccess') : t('keys.keyDisabledSuccess')
    )
    loadApiKeys()
  } catch (error) {
    appStore.showError(t('keys.failedToUpdateStatus'))
  }
}

const closeGroupSelectorPopover = (restoreFocus: boolean) => {
  const keyId = groupSelectorKeyId.value
  if (keyId === null) return
  groupSelectorKeyId.value = null
  dropdownPosition.value = null
  groupSearchQuery.value = ''
  if (restoreFocus) {
    void nextTick(() => groupButtonRefs.value.get(keyId)?.focus())
  }
}

const updateGroupSelectorPosition = (): boolean => {
  const keyId = groupSelectorKeyId.value
  if (keyId === null) return false
  const buttonEl = groupButtonRefs.value.get(keyId)
  if (!buttonEl?.isConnected) return false

  const rect = buttonEl.getBoundingClientRect()
  const viewportPadding = 16
  const maxDropdownHeight = Math.max(0, window.innerHeight - viewportPadding * 2)
  const dropdownHeight = Math.min(dropdownRef.value?.offsetHeight || 400, maxDropdownHeight)
  const spaceBelow = window.innerHeight - rect.bottom - viewportPadding
  const spaceAbove = rect.top - viewportPadding
  const dropdownWidth = Math.min(380, Math.max(0, window.innerWidth - viewportPadding * 2))
  const maxLeft = Math.max(viewportPadding, window.innerWidth - dropdownWidth - viewportPadding)
  const clampedLeft = Math.min(Math.max(rect.left, viewportPadding), maxLeft)
  const preferredTop = spaceBelow >= dropdownHeight || spaceBelow >= spaceAbove
    ? rect.bottom + 4
    : rect.top - dropdownHeight - 4
  const maxTop = Math.max(viewportPadding, window.innerHeight - dropdownHeight - viewportPadding)
  const clampedTop = Math.min(Math.max(preferredTop, viewportPadding), maxTop)

  dropdownPosition.value = {
    top: clampedTop,
    left: clampedLeft
  }
  return true
}

const openGroupSelector = async (key: ApiKey) => {
  if (groupSelectorKeyId.value === key.id) {
    closeGroupSelectorPopover(false)
    return
  }
  closeColumnSettings(false)
  if (!groupButtonRefs.value.get(key.id)) {
    throw new Error(`Group selector trigger is not mounted for API key ${key.id}`)
  }

  groupSelectorKeyId.value = key.id
  groupSearchQuery.value = ''
  if (!updateGroupSelectorPosition()) {
    closeGroupSelectorPopover(false)
    return
  }

  await nextTick()
  if (groupSelectorKeyId.value !== key.id) return
  if (!updateGroupSelectorPosition()) {
    closeGroupSelectorPopover(false)
    return
  }
  const searchInput = groupSearchInputRef.value
  if (!searchInput) {
    closeGroupSelectorPopover(false)
    throw new Error(`Group selector search is not mounted for API key ${key.id}`)
  }
  searchInput.focus()
}

const handleGroupSelectorKeydown = (event: KeyboardEvent) => {
  if (event.key !== 'Escape') return
  event.preventDefault()
  event.stopPropagation()
  closeGroupSelectorPopover(true)
}

const handleGroupSelectorFocusIn = (event: FocusEvent) => {
  const keyId = groupSelectorKeyId.value
  const target = event.target
  if (keyId === null || !(target instanceof Node)) return

  const trigger = groupButtonRefs.value.get(keyId)
  if (dropdownRef.value?.contains(target) || trigger?.contains(target)) return

  closeGroupSelectorPopover(false)
}

const handleGroupSelectorViewportChange = () => {
  if (groupSelectorKeyId.value === null) return
  if (!updateGroupSelectorPosition()) closeGroupSelectorPopover(false)
}

const loadAccountShareBindingStatus = (keyId: number): Promise<AccountShareAPIKeyBindingStatus> => {
  const activeRequest = accountShareBindingChecks.get(keyId)
  if (activeRequest) return activeRequest

  const request = accountShareAPI.getAPIKeyBindingStatus(keyId)
  accountShareBindingChecks.set(keyId, request)
  void request.then(
    () => accountShareBindingChecks.delete(keyId),
    () => accountShareBindingChecks.delete(keyId)
  )
  return request
}

const showAccountShareConflict = (
  key: ApiKey,
  action: AccountShareBlockedAction,
  status: AccountShareAPIKeyBindingStatus | null
) => {
  accountShareConflict.value = {
    show: true,
    action,
    key,
    activeCount: status?.active_count ?? null,
    queuedCount: status?.queued_count ?? null,
    endingCount: status?.ending_count ?? null
  }
}

const checkAccountShareBindings = async (
  key: ApiKey,
  action: AccountShareBlockedAction
): Promise<boolean> => {
  try {
    const status = await loadAccountShareBindingStatus(key.id)
    if (status.blocking_count === 0) return true
    showAccountShareConflict(key, action, status)
    return false
  } catch (error: unknown) {
    appStore.showError(extractApiErrorMessage(error, t('keys.accountShareConflict.checkFailed')))
    return false
  }
}

const showAccountShareConflictFromGuard = async (
  key: ApiKey,
  action: AccountShareBlockedAction
) => {
  try {
    const status = await loadAccountShareBindingStatus(key.id)
    if (status.blocking_count === 0) {
      appStore.showSuccess(t('keys.accountShareConflict.resolvedDuringRetry'))
      return
    }
    showAccountShareConflict(key, action, status)
  } catch {
    showAccountShareConflict(key, action, null)
  }
}

const isAccountShareBindingConflict = (error: unknown) => (
  extractApiErrorCode(error) === 'API_KEY_ACCOUNT_SHARE_BINDING_EXISTS'
)

const closeAccountShareConflict = () => {
  if (accountShareConflictNavigating.value) return
  accountShareConflict.value.show = false
}

const navigateToAccountShareResolution = async () => {
  const { key, action } = accountShareConflict.value
  if (!key || accountShareConflictNavigating.value) return

  accountShareConflictNavigating.value = true
  try {
    await router.push({
      name: 'AccountShare',
      query: {
        mode: 'resolve-key-binding',
        api_key_id: String(key.id),
        api_key_name: key.name,
        blocked_action: action,
        return_to: '/keys'
      }
    })
    accountShareConflict.value.show = false
  } catch (error: unknown) {
    appStore.showError(extractApiErrorMessage(error, t('keys.accountShareConflict.navigationFailed')))
  } finally {
    accountShareConflictNavigating.value = false
  }
}

const changeGroup = async (key: ApiKey, newGroupId: number | null) => {
  closeGroupSelectorPopover(true)
  if (key.group_id === newGroupId) return

  if (!(await checkAccountShareBindings(key, 'change_group'))) return

  try {
    await keysAPI.update(key.id, {
      group_id: newGroupId,
      group_routes: newGroupId === null ? [] : [{
        group_id: newGroupId,
        priority: 100,
        weight: 1,
        enabled: true,
        cooldown_seconds: 30
      }]
    })
    appStore.showSuccess(t('keys.groupChangedSuccess'))
    loadApiKeys()
  } catch (error: unknown) {
    if (isAccountShareBindingConflict(error)) {
      await showAccountShareConflictFromGuard(key, 'change_group')
      return
    }
    appStore.showError(extractApiErrorMessage(error, t('keys.failedToChangeGroup')))
  }
}

const closeGroupSelector = (event: MouseEvent) => {
  const target = event.target
  if (!(target instanceof Node)) return
  // Check if click is inside the dropdown or the trigger button
  if (
    !(target instanceof Element && target.closest('.group\\/dropdown')) &&
    !dropdownRef.value?.contains(target)
  ) {
    closeGroupSelectorPopover(false)
  }
}

const confirmDelete = async (key: ApiKey) => {
  if (!(await checkAccountShareBindings(key, 'delete'))) return
  selectedKey.value = key
  showDeleteDialog.value = true
}

const handleSubmit = async () => {
  // Validate the optional custom key before the remaining form so submit feedback
  // is always attached to and focused on the field that needs correction.
  if (!showEditModal.value && formData.value.use_custom_key) {
    customKeySubmitAttempted.value = true
    if (customKeyError.value) {
      appStore.showError(customKeyError.value)
      await nextTick()
      customKeyInputRef.value?.focus()
      return
    }
  }

  const groupRoutes = normalizeGroupRoutes()
  if (groupRoutes === null) {
    return
  }
  const primaryGroupId = resolvePrimaryGroupId(groupRoutes)

  // Validate group_id is required
  if (primaryGroupId === null) {
    appStore.showError(t('keys.groupRequired'))
    return
  }

  // Parse IP lists only if IP restriction is enabled
  const parseIPList = (text: string): string[] =>
    text.split('\n').map(ip => ip.trim()).filter(ip => ip.length > 0)
  const ipWhitelist = formData.value.enable_ip_restriction ? parseIPList(formData.value.ip_whitelist) : []
  const ipBlacklist = formData.value.enable_ip_restriction ? parseIPList(formData.value.ip_blacklist) : []

  // Calculate quota value (null/empty/0 = unlimited, stored as 0)
  const quota = formData.value.quota && formData.value.quota > 0 ? formData.value.quota : 0

  // Calculate expiration
  let expiresAt: string | null | undefined
  if (formData.value.enable_expiration && formData.value.expiration_date) {
    expiresAt = new Date(formData.value.expiration_date).toISOString()
  } else if (showEditModal.value) {
    // Edit mode: if expiration disabled or date cleared, send empty string to clear
    expiresAt = ''
  }

  // Calculate rate limit values (send 0 when toggle is off)
  const rateLimitData = formData.value.enable_rate_limit ? {
    rate_limit_5h: formData.value.rate_limit_5h && formData.value.rate_limit_5h > 0 ? formData.value.rate_limit_5h : 0,
    rate_limit_1d: formData.value.rate_limit_1d && formData.value.rate_limit_1d > 0 ? formData.value.rate_limit_1d : 0,
    rate_limit_7d: formData.value.rate_limit_7d && formData.value.rate_limit_7d > 0 ? formData.value.rate_limit_7d : 0,
  } : { rate_limit_5h: 0, rate_limit_1d: 0, rate_limit_7d: 0 }

  const editingKey = showEditModal.value ? selectedKey.value : null
  const changesGroupRouting = Boolean(
    editingKey && groupRoutingChanged(editingKey, primaryGroupId, groupRoutes)
  )
  if (
    editingKey &&
    changesGroupRouting &&
    !(await checkAccountShareBindings(editingKey, 'change_group'))
  ) {
    return
  }

  submitting.value = true
  try {
    if (showEditModal.value && selectedKey.value) {
      await keysAPI.update(selectedKey.value.id, {
        name: formData.value.name,
        group_id: primaryGroupId,
        group_routes: groupRoutes,
        status: formData.value.status,
        ip_whitelist: ipWhitelist,
        ip_blacklist: ipBlacklist,
        quota: quota,
        expires_at: expiresAt,
        rate_limit_5h: rateLimitData.rate_limit_5h,
        rate_limit_1d: rateLimitData.rate_limit_1d,
        rate_limit_7d: rateLimitData.rate_limit_7d,
      })
      appStore.showSuccess(t('keys.keyUpdatedSuccess'))
    } else {
      const customKey = formData.value.use_custom_key ? formData.value.custom_key : undefined
      await keysAPI.create(
        formData.value.name,
        primaryGroupId,
        customKey,
        ipWhitelist,
        ipBlacklist,
        quota,
        undefined,
        rateLimitData,
        groupRoutes,
        expiresAt ?? undefined
      )
      appStore.showSuccess(t('keys.keyCreatedSuccess'))
      // Only advance tour if active, on submit step, and creation succeeded
      if (onboardingStore.isCurrentStep('[data-tour="key-form-submit"]')) {
        onboardingStore.nextStep(500)
      }
    }
    closeModals()
    loadApiKeys()
  } catch (error: any) {
    if (
      changesGroupRouting &&
      showEditModal.value &&
      selectedKey.value &&
      isAccountShareBindingConflict(error)
    ) {
      showEditModal.value = false
      await showAccountShareConflictFromGuard(selectedKey.value, 'change_group')
      return
    }
    const errorMsg = error.response?.data?.detail || t('keys.failedToSave')
    appStore.showError(errorMsg)
    // Don't advance tour on error
  } finally {
    submitting.value = false
  }
}

/**
 * 处理删除 API Key 的操作
 * 优化：错误处理改进，优先显示后端返回的具体错误消息（如权限不足等），
 * 若后端未返回消息则显示默认的国际化文本
 */
const handleDelete = async () => {
  if (!selectedKey.value) return

  try {
    await keysAPI.delete(selectedKey.value.id)
    appStore.showSuccess(t('keys.keyDeletedSuccess'))
    showDeleteDialog.value = false
    loadApiKeys()
  } catch (error: unknown) {
    if (isAccountShareBindingConflict(error)) {
      showDeleteDialog.value = false
      await showAccountShareConflictFromGuard(selectedKey.value, 'delete')
      return
    }
    const errorMsg = extractApiErrorMessage(error, t('keys.failedToDelete'))
    appStore.showError(errorMsg)
  }
}

const closeModals = () => {
  showCreateModal.value = false
  showEditModal.value = false
  editExpirationPresetBase.value = null
  customKeyTouched.value = false
  customKeySubmitAttempted.value = false
  selectedKey.value = null
  formData.value = {
    name: '',
    group_id: null,
    status: 'active',
    use_custom_key: false,
    custom_key: '',
    enable_ip_restriction: false,
    ip_whitelist: '',
    ip_blacklist: '',
    enable_quota: false,
    quota: null,
    enable_rate_limit: false,
    rate_limit_5h: null,
    rate_limit_1d: null,
    rate_limit_7d: null,
    enable_group_routes: false,
    group_routes: [defaultGroupRoute()],
    enable_expiration: false,
    expiration_preset: '30',
    expiration_date: ''
  }
}

const toggleCustomKey = () => {
  formData.value.use_custom_key = !formData.value.use_custom_key
  if (!formData.value.use_custom_key) {
    customKeyTouched.value = false
    customKeySubmitAttempted.value = false
  }
}

// Show reset quota confirmation dialog
const confirmResetQuota = () => {
  showResetQuotaDialog.value = true
}

// Set expiration date based on quick select days
const setExpirationDays = (days: number) => {
  let presetBase: Date
  if (showEditModal.value) {
    if (editExpirationPresetBase.value === null) {
      throw new Error('Edit expiration preset base is not initialized')
    }
    presetBase = editExpirationPresetBase.value
  } else {
    presetBase = new Date()
  }

  const expDate = calculateExpirationPresetDate(presetBase, days)
  formData.value.expiration_preset = days.toString() as '7' | '30' | '90'
  formData.value.expiration_date = formatDateTimeLocal(expDate.toISOString())
}

const toggleExpiration = () => {
  const enableExpiration = !formData.value.enable_expiration
  formData.value.enable_expiration = enableExpiration
  if (enableExpiration && !formData.value.expiration_date) {
    setExpirationDays(30)
  }
}

// Reset quota used for an API key
const resetQuotaUsed = async () => {
  if (!selectedKey.value) return
  showResetQuotaDialog.value = false
  try {
    await keysAPI.update(selectedKey.value.id, { reset_quota: true })
    appStore.showSuccess(t('keys.quotaResetSuccess'))
    // Update local state
    if (selectedKey.value) {
      selectedKey.value.quota_used = 0
    }
  } catch (error: any) {
    const errorMsg = error.response?.data?.detail || t('keys.failedToResetQuota')
    appStore.showError(errorMsg)
  }
}

// Show reset rate limit confirmation dialog (from edit modal)
const confirmResetRateLimit = () => {
  showResetRateLimitDialog.value = true
}

// Show reset rate limit confirmation dialog (from table row)
const confirmResetRateLimitFromTable = (row: ApiKey) => {
  selectedKey.value = row
  showResetRateLimitDialog.value = true
}

// Reset rate limit usage for an API key
const resetRateLimitUsage = async () => {
  if (!selectedKey.value) return
  showResetRateLimitDialog.value = false
  try {
    await keysAPI.update(selectedKey.value.id, { reset_rate_limit_usage: true })
    appStore.showSuccess(t('keys.rateLimitResetSuccess'))
    // Refresh key data
    await loadApiKeys()
    // Update the editing key with fresh data
    const refreshedKey = apiKeys.value.find(k => k.id === selectedKey.value!.id)
    if (refreshedKey) {
      selectedKey.value = refreshedKey
    }
  } catch (error: any) {
    const errorMsg = error.response?.data?.detail || t('keys.failedToResetRateLimit')
    appStore.showError(errorMsg)
  }
}

const importToCcswitch = (row: ApiKey) => {
  const platform = row.group?.platform || 'anthropic'

  // For antigravity platform, show client selection dialog
  if (platform === 'antigravity') {
    pendingCcsRow.value = row
    showCcsClientSelect.value = true
    return
  }

  // For other platforms, execute directly
  executeCcsImport(row, platform === 'gemini' ? 'gemini' : 'claude')
}

const executeCcsImport = (row: ApiKey, clientType: 'claude' | 'gemini') => {
  const baseUrl = publicSettings.value?.api_base_url || window.location.origin
  const platform = row.group?.platform || 'anthropic'

  const usageScript = `({
    request: {
      url: "{{baseUrl}}/v1/usage",
      method: "GET",
      headers: { "Authorization": "Bearer {{apiKey}}" }
    },
    extractor: function(response) {
      const remaining = response?.remaining ?? response?.quota?.remaining ?? response?.balance;
      const unit = response?.unit ?? response?.quota?.unit ?? "USD";
      return {
        isValid: response?.is_active ?? response?.isValid ?? true,
        remaining,
        unit
      };
    }
  })`
  const providerName = (publicSettings.value?.site_name || 'sub2api').trim() || 'sub2api'

  const deeplink = buildCcSwitchImportDeeplink({
    baseUrl,
    platform,
    clientType,
    providerName,
    apiKey: row.key,
    usageScript
  })

  try {
    window.open(deeplink, '_self')

    // Check if the protocol handler worked by detecting if we're still focused
    const detectionTimer = setTimeout(() => {
      ccSwitchDetectionTimers.delete(detectionTimer)
      if (document.hasFocus()) {
        // Still focused means the protocol handler likely failed
        appStore.showError(t('keys.ccSwitchNotInstalled'))
      }
    }, 100)
    ccSwitchDetectionTimers.add(detectionTimer)
  } catch (error) {
    appStore.showError(t('keys.ccSwitchNotInstalled'))
  }
}

const openImagePlayground = (row: ApiKey) => {
  pendingImagePlaygroundRow.value = row
  imagePlaygroundModel.value = IMAGE_PLAYGROUND_DEFAULT_MODEL
  showImagePlaygroundModelDialog.value = true
}

const closeImagePlaygroundModelDialog = () => {
  showImagePlaygroundModelDialog.value = false
  pendingImagePlaygroundRow.value = null
  imagePlaygroundModel.value = ''
}

const confirmOpenImagePlayground = () => {
  const row = pendingImagePlaygroundRow.value
  const model = imagePlaygroundModel.value.trim()
  if (!row || !model) return

  try {
    const baseUrl = publicSettings.value?.api_base_url || window.location.origin
    const siteName = (publicSettings.value?.site_name || 'Pixel API').trim() || 'Pixel API'
    const url = buildImagePlaygroundImportUrl({
      apiBaseUrl: baseUrl,
      apiKey: row.key,
      keyId: row.id,
      keyName: row.name,
      model,
      sourceName: siteName
    })

    window.open(url, '_blank', 'noopener,noreferrer')
    closeImagePlaygroundModelDialog()
  } catch {
    appStore.showError(t('keys.openImagePlaygroundFailed'))
  }
}

const handleCcsClientSelect = (clientType: 'claude' | 'gemini') => {
  if (pendingCcsRow.value) {
    executeCcsImport(pendingCcsRow.value, clientType)
  }
  showCcsClientSelect.value = false
  pendingCcsRow.value = null
}

const closeCcsClientSelect = () => {
  showCcsClientSelect.value = false
  pendingCcsRow.value = null
}

function formatResetTime(resetAt: string | null): string {
  if (!resetAt) return ''
  const diff = new Date(resetAt).getTime() - now.value.getTime()
  if (diff <= 0) return t('keys.resetNow')
  const days = Math.floor(diff / 86400000)
  const hours = Math.floor((diff % 86400000) / 3600000)
  const mins = Math.floor((diff % 3600000) / 60000)
  if (days > 0) return `${days}d ${hours}h`
  if (hours > 0) return `${hours}h ${mins}m`
  return `${mins}m`
}

const handleColumnSettingsClickOutside = (event: MouseEvent) => {
  const target = event.target
  if (
    target instanceof Node &&
    columnDropdownRef.value &&
    !columnDropdownRef.value.contains(target)
  ) {
    closeColumnSettings(false)
  }
}

onMounted(() => {
  loadSavedColumns()
  loadApiKeys()
  loadGroups()
  loadUserGroupRates()
  loadPublicSettings()
  document.addEventListener('click', closeGroupSelector)
  document.addEventListener('click', handleColumnSettingsClickOutside)
  document.addEventListener('focusin', handleGroupSelectorFocusIn)
  window.addEventListener('resize', handleGroupSelectorViewportChange)
  window.addEventListener('scroll', handleGroupSelectorViewportChange, true)
  resetTimer = setInterval(() => { now.value = new Date() }, 60000)
})

onUnmounted(() => {
  document.removeEventListener('click', closeGroupSelector)
  document.removeEventListener('click', handleColumnSettingsClickOutside)
  document.removeEventListener('focusin', handleGroupSelectorFocusIn)
  window.removeEventListener('resize', handleGroupSelectorViewportChange)
  window.removeEventListener('scroll', handleGroupSelectorViewportChange, true)
  if (resetTimer !== null) {
    clearInterval(resetTimer)
    resetTimer = null
  }
  if (copiedKeyResetTimer !== null) {
    clearTimeout(copiedKeyResetTimer)
    copiedKeyResetTimer = null
  }
  ccSwitchDetectionTimers.forEach((timer) => clearTimeout(timer))
  ccSwitchDetectionTimers.clear()
  usageStatsRequestSequence += 1
  abortController?.abort()
  abortController = null
})
</script>
