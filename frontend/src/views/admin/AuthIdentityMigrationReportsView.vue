<template>
  <AppLayout>
    <div class="space-y-6">
      <section class="grid gap-4 md:grid-cols-3">
        <div class="card p-5">
          <p class="text-sm font-medium text-gray-500 dark:text-dark-400">
            {{ copy.total }}
          </p>
          <p data-test="summary-total" class="mt-2 text-3xl font-semibold text-gray-900 dark:text-gray-100">
            {{ summary.total }}
          </p>
        </div>
        <div class="card p-5">
          <p class="text-sm font-medium text-gray-500 dark:text-dark-400">
            {{ copy.open }}
          </p>
          <p data-test="summary-open" class="mt-2 text-3xl font-semibold text-amber-600 dark:text-amber-400">
            {{ summary.open_total }}
          </p>
        </div>
        <div class="card p-5">
          <p class="text-sm font-medium text-gray-500 dark:text-dark-400">
            {{ copy.resolved }}
          </p>
          <p data-test="summary-resolved" class="mt-2 text-3xl font-semibold text-emerald-600 dark:text-emerald-400">
            {{ summary.resolved_total }}
          </p>
        </div>
      </section>

      <TablePageLayout>
        <template #actions>
          <div class="flex flex-wrap items-center justify-between gap-3">
            <div>
              <h1 class="text-2xl font-semibold text-gray-900 dark:text-gray-100">
                {{ copy.title }}
              </h1>
              <p class="mt-1 text-sm text-gray-500 dark:text-dark-400">
                {{ copy.subtitle }}
              </p>
            </div>
            <button type="button" class="btn btn-secondary" :disabled="loading || resolving" @click="refreshAll">
              <Icon name="refresh" size="md" :class="loading || summaryLoading ? 'animate-spin' : ''" />
            </button>
          </div>
        </template>

        <template #filters>
          <div class="flex flex-wrap items-center gap-3">
            <div class="w-full sm:w-80">
              <label class="input-label" for="report-type-filter">{{ copy.reportType }}</label>
              <select
                id="report-type-filter"
                v-model="filters.reportType"
                data-test="report-type-filter"
                class="input"
                @change="handleReportTypeChange"
              >
                <option value="">{{ copy.allReportTypes }}</option>
                <option
                  v-for="option in reportTypeOptions"
                  :key="option.value"
                  :value="option.value"
                >
                  {{ option.label }}
                </option>
              </select>
            </div>
          </div>
        </template>

        <template #table>
          <DataTable :columns="columns" :data="reports" :loading="loading">
            <template #cell-status="{ row }">
              <span :class="['badge', row.resolved_at ? 'badge-success' : 'badge-warning']">
                {{ row.resolved_at ? copy.resolvedBadge : copy.openBadge }}
              </span>
            </template>

            <template #cell-report_type="{ value }">
              <span class="font-mono text-xs text-gray-600 dark:text-dark-300">{{ value }}</span>
            </template>

            <template #cell-report_key="{ value }">
              <span class="font-medium text-gray-900 dark:text-gray-100">{{ value }}</span>
            </template>

            <template #cell-details_preview="{ row }">
              <div class="flex flex-wrap gap-2">
                <span
                  v-for="entry in getDetailHighlights(row.details)"
                  :key="entry.key"
                  class="rounded-full bg-gray-100 px-2 py-1 text-xs text-gray-600 dark:bg-dark-700 dark:text-dark-200"
                >
                  {{ entry.key }}: {{ entry.value }}
                </span>
              </div>
            </template>

            <template #cell-created_at="{ value }">
              <span class="text-sm text-gray-500 dark:text-dark-400">{{ formatDateTime(value) }}</span>
            </template>

            <template #cell-resolved_at="{ value }">
              <span class="text-sm text-gray-500 dark:text-dark-400">
                {{ value ? formatDateTime(value) : copy.notResolved }}
              </span>
            </template>

            <template #cell-actions="{ row }">
              <button
                type="button"
                class="btn btn-secondary btn-sm"
                :data-test="`select-report-${row.id}`"
                @click="selectReport(row)"
              >
                {{ copy.viewDetails }}
              </button>
            </template>
          </DataTable>
        </template>

        <template #pagination>
          <Pagination
            v-if="pagination.total > 0"
            :page="pagination.page"
            :page-size="pagination.pageSize"
            :total="pagination.total"
            @update:page="handlePageChange"
            @update:pageSize="handlePageSizeChange"
          />
        </template>
      </TablePageLayout>

      <section class="grid gap-6 xl:grid-cols-[minmax(0,1.25fr)_minmax(0,1fr)]">
        <div class="card p-6">
          <div class="flex items-start justify-between gap-4">
            <div>
              <h2 class="text-lg font-semibold text-gray-900 dark:text-gray-100">
                {{ copy.detailTitle }}
              </h2>
              <p class="mt-1 text-sm text-gray-500 dark:text-dark-400">
                {{ selectedReport ? selectedReport.report_key : copy.selectPrompt }}
              </p>
            </div>
            <span
              v-if="selectedReport"
              :class="['badge', selectedReport.resolved_at ? 'badge-success' : 'badge-warning']"
            >
              {{ selectedReport.resolved_at ? copy.resolvedBadge : copy.openBadge }}
            </span>
          </div>

          <div v-if="selectedReport" class="mt-6 space-y-5">
            <dl class="grid gap-4 sm:grid-cols-2">
              <div>
                <dt class="text-xs font-medium uppercase tracking-wide text-gray-500 dark:text-dark-400">{{ copy.reportType }}</dt>
                <dd class="mt-1 break-all font-mono text-sm text-gray-900 dark:text-gray-100">{{ selectedReport.report_type }}</dd>
              </div>
              <div>
                <dt class="text-xs font-medium uppercase tracking-wide text-gray-500 dark:text-dark-400">{{ copy.reportKey }}</dt>
                <dd class="mt-1 break-all text-sm text-gray-900 dark:text-gray-100">{{ selectedReport.report_key }}</dd>
              </div>
              <div>
                <dt class="text-xs font-medium uppercase tracking-wide text-gray-500 dark:text-dark-400">{{ copy.createdAt }}</dt>
                <dd class="mt-1 text-sm text-gray-900 dark:text-gray-100">{{ formatDateTime(selectedReport.created_at) }}</dd>
              </div>
              <div>
                <dt class="text-xs font-medium uppercase tracking-wide text-gray-500 dark:text-dark-400">{{ copy.resolvedAt }}</dt>
                <dd class="mt-1 text-sm text-gray-900 dark:text-gray-100">
                  {{ selectedReport.resolved_at ? formatDateTime(selectedReport.resolved_at) : copy.notResolved }}
                </dd>
              </div>
              <div>
                <dt class="text-xs font-medium uppercase tracking-wide text-gray-500 dark:text-dark-400">{{ copy.resolvedBy }}</dt>
                <dd class="mt-1 text-sm text-gray-900 dark:text-gray-100">{{ selectedReport.resolved_by_user_id ?? '-' }}</dd>
              </div>
              <div>
                <dt class="text-xs font-medium uppercase tracking-wide text-gray-500 dark:text-dark-400">{{ copy.resolutionNote }}</dt>
                <dd class="mt-1 whitespace-pre-wrap text-sm text-gray-900 dark:text-gray-100">
                  {{ selectedReport.resolution_note || copy.emptyResolutionNote }}
                </dd>
              </div>
            </dl>

            <div>
              <h3 class="text-sm font-medium text-gray-700 dark:text-dark-300">{{ copy.keyFields }}</h3>
              <div class="mt-3 flex flex-wrap gap-2">
                <span
                  v-for="entry in getDetailHighlights(selectedReport.details)"
                  :key="entry.key"
                  class="rounded-full bg-gray-100 px-2 py-1 text-xs text-gray-600 dark:bg-dark-700 dark:text-dark-200"
                >
                  {{ entry.key }}: {{ entry.value }}
                </span>
              </div>
            </div>

            <div>
              <h3 class="text-sm font-medium text-gray-700 dark:text-dark-300">{{ copy.rawDetails }}</h3>
              <pre class="mt-3 max-h-96 overflow-auto rounded-xl bg-gray-950 p-4 text-xs text-gray-100">{{ formatDetailsJson(selectedReport.details) }}</pre>
            </div>
          </div>

          <div v-else class="mt-6 rounded-2xl border border-dashed border-gray-300 p-8 text-center text-sm text-gray-500 dark:border-dark-600 dark:text-dark-400">
            {{ copy.selectPrompt }}
          </div>
        </div>

        <div class="card p-6">
          <h2 class="text-lg font-semibold text-gray-900 dark:text-gray-100">
            {{ copy.resolveTitle }}
          </h2>
          <p class="mt-1 text-sm text-gray-500 dark:text-dark-400">
            {{ copy.resolveSubtitle }}
          </p>

          <div class="mt-6 space-y-4">
            <div>
              <label class="input-label" for="resolution-note">{{ copy.resolutionNote }}</label>
              <textarea
                id="resolution-note"
                v-model="resolutionNote"
                data-test="resolution-note"
                class="input min-h-40"
                :disabled="!selectedReport || Boolean(selectedReport.resolved_at) || resolving"
                :placeholder="copy.resolvePlaceholder"
              ></textarea>
            </div>

            <button
              type="button"
              class="btn btn-primary w-full"
              data-test="resolve-submit"
              :disabled="!canResolve"
              @click="submitResolve"
            >
              {{ resolving ? copy.resolving : copy.resolveAction }}
            </button>
          </div>

          <div class="mt-8 border-t border-gray-200 pt-6 dark:border-dark-700">
            <h3 class="text-base font-semibold text-gray-900 dark:text-gray-100">
              {{ copy.remediationTitle }}
            </h3>
            <p class="mt-1 text-sm text-gray-500 dark:text-dark-400">
              {{ copy.remediationSubtitle }}
            </p>

            <div class="mt-6 space-y-4">
              <div>
                <label class="input-label" for="remediation-user-id">{{ copy.remediationUserID }}</label>
                <input
                  id="remediation-user-id"
                  v-model="remediation.userID"
                  data-test="remediation-user-id"
                  class="input"
                  :disabled="!selectedReport || Boolean(selectedReport.resolved_at) || binding"
                  inputmode="numeric"
                />
              </div>

              <div>
                <label class="input-label" for="remediation-provider-type">{{ copy.remediationProviderType }}</label>
                <input
                  id="remediation-provider-type"
                  v-model="remediation.providerType"
                  data-test="remediation-provider-type"
                  class="input"
                  :disabled="!selectedReport || Boolean(selectedReport.resolved_at) || binding"
                />
              </div>

              <div>
                <label class="input-label" for="remediation-provider-key">{{ copy.remediationProviderKey }}</label>
                <input
                  id="remediation-provider-key"
                  v-model="remediation.providerKey"
                  data-test="remediation-provider-key"
                  class="input"
                  :disabled="!selectedReport || Boolean(selectedReport.resolved_at) || binding"
                />
              </div>

              <div>
                <label class="input-label" for="remediation-provider-subject">{{ copy.remediationProviderSubject }}</label>
                <input
                  id="remediation-provider-subject"
                  v-model="remediation.providerSubject"
                  data-test="remediation-provider-subject"
                  class="input"
                  :disabled="!selectedReport || Boolean(selectedReport.resolved_at) || binding"
                />
              </div>

              <div>
                <label class="input-label" for="remediation-issuer">{{ copy.remediationIssuer }}</label>
                <input
                  id="remediation-issuer"
                  v-model="remediation.issuer"
                  data-test="remediation-issuer"
                  class="input"
                  :disabled="!selectedReport || Boolean(selectedReport.resolved_at) || binding"
                />
              </div>

              <button
                type="button"
                class="btn btn-secondary w-full"
                data-test="remediation-submit"
                :disabled="!canBindRemediation"
                @click="submitRemediationBinding"
              >
                {{ binding ? copy.remediationSubmitting : copy.remediationAction }}
              </button>
            </div>
          </div>
        </div>
      </section>
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { adminAPI } from '@/api/admin'
import type {
  AdminBindAuthIdentityRequest,
  AuthIdentityMigrationReport,
  AuthIdentityMigrationReportSummary,
} from '@/api/admin/users'
import type { Column } from '@/components/common/types'
import AppLayout from '@/components/layout/AppLayout.vue'
import TablePageLayout from '@/components/layout/TablePageLayout.vue'
import DataTable from '@/components/common/DataTable.vue'
import Pagination from '@/components/common/Pagination.vue'
import Icon from '@/components/icons/Icon.vue'
import { useAppStore } from '@/stores/app'
import { formatDateTime } from '@/utils/format'

const { locale } = useI18n()
const appStore = useAppStore()

const isZh = computed(() => locale.value.toLowerCase().startsWith('zh'))
const text = (zh: string, en: string) => (isZh.value ? zh : en)

const copy = computed(() => ({
  title: text('Auth Identity Migration Reports', 'Auth Identity Migration Reports'),
  subtitle: text('处理 auth identity 迁移过程中需要人工收口的异常记录。', 'Review and resolve auth identity migration records that require manual follow-up.'),
  total: text('总报告数', 'Total reports'),
  open: text('待处理', 'Open'),
  resolved: text('已解决', 'Resolved'),
  reportType: text('报告类型', 'Report type'),
  allReportTypes: text('全部类型', 'All report types'),
  resolvedBadge: text('已解决', 'Resolved'),
  openBadge: text('待处理', 'Open'),
  notResolved: text('未解决', 'Not resolved'),
  viewDetails: text('查看', 'View'),
  detailTitle: text('报告详情', 'Report details'),
  selectPrompt: text('从列表中选择一条报告以查看详情和处理意见。', 'Select a report from the list to inspect details and submit a resolution note.'),
  reportKey: text('报告键', 'Report key'),
  createdAt: text('创建时间', 'Created at'),
  resolvedAt: text('解决时间', 'Resolved at'),
  resolvedBy: text('处理人 ID', 'Resolved by'),
  resolutionNote: text('处理备注', 'Resolution note'),
  emptyResolutionNote: text('暂无处理备注', 'No resolution note'),
  keyFields: text('关键字段', 'Key fields'),
  rawDetails: text('原始详情', 'Raw details'),
  resolveTitle: text('提交处理结果', 'Submit resolution'),
  resolveSubtitle: text('填写运营备注后提交 resolve，后端会记录处理人和处理时间。', 'Submit an operational note to resolve the selected report. The backend will record the resolver and timestamp.'),
  resolvePlaceholder: text('填写本次处理动作、用户沟通结果或后续追踪信息。', 'Describe the action taken, user communication, or follow-up context.'),
  resolveAction: text('提交 Resolve', 'Submit resolve'),
  resolving: text('提交中...', 'Submitting...'),
  remediationTitle: text('修复绑定', 'Remediation binding'),
  remediationSubtitle: text('可直接把迁移报告中的身份信息绑定到指定用户；已识别字段会自动预填。', 'Bind the migrated identity directly to a user. Recognized fields are prefilled automatically.'),
  remediationUserID: text('目标用户 ID', 'Target user ID'),
  remediationProviderType: text('Provider Type', 'Provider type'),
  remediationProviderKey: text('Provider Key', 'Provider key'),
  remediationProviderSubject: text('Provider Subject', 'Provider subject'),
  remediationIssuer: text('Issuer', 'Issuer'),
  remediationAction: text('提交绑定修复', 'Submit remediation binding'),
  remediationSubmitting: text('提交中...', 'Submitting...'),
}))

const summary = ref<AuthIdentityMigrationReportSummary>({
  total: 0,
  open_total: 0,
  resolved_total: 0,
  by_type: {},
})
const reports = ref<AuthIdentityMigrationReport[]>([])
const selectedReport = ref<AuthIdentityMigrationReport | null>(null)
const resolutionNote = ref('')
const loading = ref(false)
const summaryLoading = ref(false)
const resolving = ref(false)
const binding = ref(false)

const filters = reactive({
  reportType: '',
})

const pagination = reactive({
  page: 1,
  pageSize: 20,
  total: 0,
})
const knownReportTypes = ref<string[]>([])
const remediation = reactive({
  userID: '',
  providerType: '',
  providerKey: '',
  providerSubject: '',
  issuer: '',
})

const columns: Column[] = [
  { key: 'status', label: text('状态', 'Status') },
  { key: 'report_type', label: text('报告类型', 'Report type') },
  { key: 'report_key', label: text('报告键', 'Report key') },
  { key: 'details_preview', label: text('关键字段', 'Key fields') },
  { key: 'created_at', label: text('创建时间', 'Created at') },
  { key: 'resolved_at', label: text('解决时间', 'Resolved at') },
  { key: 'actions', label: text('操作', 'Actions') },
]

const reportTypeOptions = computed(() =>
  knownReportTypes.value
    .slice()
    .sort((left, right) => left.localeCompare(right))
    .map((value) => {
      const count = summary.value.by_type[value]
      return {
        value,
        label: count === undefined ? value : `${value} (${count})`,
      }
    })
)

const canResolve = computed(() =>
  Boolean(
    selectedReport.value &&
    !selectedReport.value.resolved_at &&
    resolutionNote.value.trim() &&
    !resolving.value
  )
)

const canBindRemediation = computed(() =>
  Boolean(
    selectedReport.value &&
    !selectedReport.value.resolved_at &&
    remediation.userID.trim() &&
    remediation.providerType.trim() &&
    remediation.providerKey.trim() &&
    remediation.providerSubject.trim() &&
    !binding.value
  )
)

const mergeKnownReportTypes = (...values: Array<string | null | undefined>) => {
  const merged = new Set(knownReportTypes.value)
  for (const value of values) {
    const normalized = value?.trim()
    if (normalized) {
      merged.add(normalized)
    }
  }
  knownReportTypes.value = Array.from(merged)
}

const loadSummary = async () => {
  summaryLoading.value = true
  try {
    summary.value = await adminAPI.users.getAuthIdentityMigrationReportSummary()
    mergeKnownReportTypes(...Object.keys(summary.value.by_type))
  } catch (error) {
    console.error('Failed to load auth identity migration report summary:', error)
    appStore.showError(text('加载 migration reports 汇总失败', 'Failed to load migration report summary'))
  } finally {
    summaryLoading.value = false
  }
}

const loadReports = async () => {
  loading.value = true
  try {
    const response = await adminAPI.users.listAuthIdentityMigrationReports({
      page: pagination.page,
      pageSize: pagination.pageSize,
      reportType: filters.reportType,
    })

    reports.value = response.items
    pagination.total = response.total
    mergeKnownReportTypes(filters.reportType, ...response.items.map((report) => report.report_type))

    if (selectedReport.value) {
      const refreshed = response.items.find((report) => report.id === selectedReport.value?.id) ?? null
      selectedReport.value = refreshed
      applyRemediationDefaults(refreshed)
      resolutionNote.value = refreshed?.resolved_at
        ? refreshed.resolution_note ?? ''
        : resolutionNote.value
    }
  } catch (error) {
    console.error('Failed to load auth identity migration reports:', error)
    appStore.showError(text('加载 migration reports 列表失败', 'Failed to load migration reports'))
  } finally {
    loading.value = false
  }
}

const refreshAll = async () => {
  await Promise.all([loadSummary(), loadReports()])
}

const handleReportTypeChange = async () => {
  pagination.page = 1
  await loadReports()
}

const handlePageChange = async (page: number) => {
  pagination.page = page
  await loadReports()
}

const handlePageSizeChange = async (pageSize: number) => {
  pagination.page = 1
  pagination.pageSize = pageSize
  await loadReports()
}

const selectReport = (report: AuthIdentityMigrationReport) => {
  selectedReport.value = report
  resolutionNote.value = report.resolution_note ?? ''
  applyRemediationDefaults(report)
}

const formatDetailsJson = (details: Record<string, unknown>) => JSON.stringify(details ?? {}, null, 2)

const isDisplayableValue = (value: unknown) =>
  ['string', 'number', 'boolean'].includes(typeof value)

const getDetailHighlights = (details: Record<string, unknown>) => {
  const preferredKeys = [
    'user_id',
    'legacy_email',
    'provider_key',
    'provider_subject',
    'email',
    'subject',
  ]

  const entries = preferredKeys
    .filter((key) => key in details && isDisplayableValue(details[key]))
    .map((key) => ({ key, value: String(details[key]) }))

  if (entries.length > 0) {
    return entries
  }

  return Object.entries(details)
    .filter(([, value]) => isDisplayableValue(value))
    .slice(0, 4)
    .map(([key, value]) => ({ key, value: String(value) }))
}

const stringDetailValue = (details: Record<string, unknown>, key: string) => {
  const value = details[key]
  return typeof value === 'string' ? value.trim() : ''
}

const numericDetailValue = (details: Record<string, unknown>, key: string) => {
  const value = details[key]
  if (typeof value === 'number' && Number.isFinite(value)) {
    return String(Math.trunc(value))
  }
  if (typeof value === 'string' && value.trim()) {
    return value.trim()
  }
  return ''
}

const inferProviderTypeFromReport = (report: AuthIdentityMigrationReport) => {
  const explicit = stringDetailValue(report.details, 'provider_type')
  if (explicit) {
    return explicit
  }
  if (report.report_type.includes('oidc')) {
    return 'oidc'
  }
  if (report.report_type.includes('wechat')) {
    return 'wechat'
  }
  if (report.report_type.includes('linuxdo')) {
    return 'linuxdo'
  }
  return ''
}

const inferProviderKeyFromReport = (report: AuthIdentityMigrationReport, providerType: string) => {
  const explicit = stringDetailValue(report.details, 'provider_key')
  if (explicit) {
    return explicit
  }
  if (providerType === 'wechat') {
    return 'wechat-main'
  }
  return ''
}

const inferProviderSubjectFromReport = (report: AuthIdentityMigrationReport) =>
  stringDetailValue(report.details, 'provider_subject')
  || stringDetailValue(report.details, 'subject')
  || stringDetailValue(report.details, 'unionid')

const applyRemediationDefaults = (report: AuthIdentityMigrationReport | null) => {
  remediation.userID = report ? numericDetailValue(report.details, 'user_id') : ''
  remediation.providerType = report ? inferProviderTypeFromReport(report) : ''
  remediation.providerKey = report ? inferProviderKeyFromReport(report, remediation.providerType) : ''
  remediation.providerSubject = report ? inferProviderSubjectFromReport(report) : ''
  remediation.issuer = report ? stringDetailValue(report.details, 'issuer') : ''
}

const submitResolve = async () => {
  if (!selectedReport.value) {
    appStore.showError(text('请先选择一条报告', 'Select a report first'))
    return
  }

  const note = resolutionNote.value.trim()
  if (!note) {
    appStore.showError(text('请填写处理备注', 'Enter a resolution note'))
    return
  }

  resolving.value = true
  try {
    const updated = await adminAPI.users.resolveAuthIdentityMigrationReport(selectedReport.value.id, note)
    selectedReport.value = updated
    resolutionNote.value = updated.resolution_note ?? ''
    appStore.showSuccess(text('处理结果已提交', 'Resolution submitted'))
    await refreshAll()
  } catch (error) {
    console.error('Failed to resolve auth identity migration report:', error)
    appStore.showError(text('提交 resolve 失败', 'Failed to resolve report'))
  } finally {
    resolving.value = false
  }
}

const submitRemediationBinding = async () => {
  if (!selectedReport.value) {
    appStore.showError(text('请先选择一条报告', 'Select a report first'))
    return
  }

  const userID = Number.parseInt(remediation.userID.trim(), 10)
  if (!Number.isFinite(userID) || userID <= 0) {
    appStore.showError(text('请输入有效的目标用户 ID', 'Enter a valid target user ID'))
    return
  }

  const payload: AdminBindAuthIdentityRequest = {
    provider_type: remediation.providerType.trim(),
    provider_key: remediation.providerKey.trim(),
    provider_subject: remediation.providerSubject.trim(),
    issuer: remediation.issuer.trim() || undefined,
    metadata: {},
  }

  binding.value = true
  try {
    await adminAPI.users.bindUserAuthIdentity(userID, payload)
    appStore.showSuccess(text('修复绑定已提交', 'Remediation binding submitted'))
  } catch (error) {
    console.error('Failed to submit auth identity remediation binding:', error)
    appStore.showError(text('提交修复绑定失败', 'Failed to submit remediation binding'))
  } finally {
    binding.value = false
  }
}

onMounted(async () => {
  await refreshAll()
})
</script>
