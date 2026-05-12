<template>
  <AppLayout>
    <TablePageLayout>
      <template #filters>
        <div class="flex flex-wrap items-center gap-3">
          <div class="relative w-full sm:w-72">
            <Icon name="search" size="md" class="absolute left-3 top-1/2 -translate-y-1/2 text-gray-400" />
            <input v-model="search" type="text" class="input min-h-[44px] pl-10" placeholder="搜索名称、ID 或 URL" @input="handleSearch" />
          </div>
          <select v-model="status" class="input min-h-[44px] w-full sm:w-44" @change="reload">
            <option value="">全部状态</option>
            <option value="pending">待激活</option>
            <option value="active">运行中</option>
            <option value="maintenance">维护中</option>
            <option value="unhealthy">异常</option>
            <option value="disabled">已停用</option>
          </select>
          <div class="ml-auto flex flex-wrap gap-2">
            <button class="btn btn-secondary" :disabled="loading" @click="reload">
              <Icon name="refresh" size="md" :class="loading ? 'animate-spin' : ''" />
            </button>
            <button class="btn btn-primary" @click="openCreate">
              <Icon name="plus" size="md" class="mr-2" />
              新建子站
            </button>
          </div>
        </div>
      </template>

      <template #table>
        <DataTable :columns="columns" :data="subsites" :loading="loading">
          <template #cell-name="{ row }">
            <div class="min-w-0">
              <div class="truncate font-medium text-gray-900 dark:text-white">{{ row.name }}</div>
              <code class="text-xs text-gray-500">{{ row.subsite_id }}</code>
            </div>
          </template>

          <template #cell-status="{ value }">
            <span :class="['badge', statusClass(value)]">{{ statusLabel(value) }}</span>
          </template>

          <template #cell-public_url="{ value }">
            <div class="flex min-w-0 items-center gap-2">
              <a v-if="value" :href="value" target="_blank" rel="noreferrer" class="truncate text-sm text-primary-600 hover:underline dark:text-primary-300">
                {{ value }}
              </a>
              <span v-else class="text-sm text-gray-400">-</span>
              <button v-if="value" class="rounded p-1 text-gray-400 hover:text-primary-600 dark:hover:text-primary-300" title="复制 URL" @click.stop="copy(value)">
                <Icon name="copy" size="sm" />
              </button>
            </div>
          </template>

          <template #cell-health_score="{ row }">
            <div class="flex flex-col gap-1">
              <span class="text-sm font-medium text-gray-900 dark:text-white">{{ row.health_score }}%</span>
              <span class="text-xs text-gray-500">{{ formatDate(row.last_heartbeat_at) }}</span>
            </div>
          </template>

          <template #cell-limits="{ row }">
            <div class="text-sm text-gray-700 dark:text-gray-200">
              QPS {{ row.max_qps || '-' }} / 并发 {{ row.max_concurrency || '-' }}
            </div>
          </template>

          <template #cell-actions="{ row }">
            <div class="flex flex-wrap justify-end gap-1.5">
              <button class="btn btn-sm btn-secondary" @click="openLeases(row)">租约</button>
              <button class="btn btn-sm btn-secondary" @click="copyClientConfig(row)">复制配置</button>
              <button v-if="row.status === 'pending' || row.status === 'unhealthy'" class="btn btn-sm btn-secondary" @click="openResetSecret(row)">修复</button>
              <button class="btn btn-sm btn-secondary" @click="openEdit(row)">编辑</button>
              <button v-if="row.status === 'pending' || row.status === 'unhealthy'" class="btn btn-sm btn-primary" @click="changeStatus(row, 'activate')">激活</button>
              <button v-if="row.status === 'active'" class="btn btn-sm btn-secondary" @click="changeStatus(row, 'pause')">暂停</button>
              <button v-if="row.status === 'maintenance'" class="btn btn-sm btn-primary" @click="changeStatus(row, 'resume')">恢复</button>
            </div>
          </template>
        </DataTable>
      </template>

      <template #pagination>
        <Pagination
          v-if="pagination.total > 0"
          :page="pagination.page"
          :total="pagination.total"
          :page-size="pagination.page_size"
          @update:page="handlePageChange"
          @update:pageSize="handlePageSizeChange"
        />
      </template>
    </TablePageLayout>

    <BaseDialog :show="showForm" :title="editing ? '编辑子站' : '新建子站'" width="wide" @close="closeForm">
      <form class="space-y-4" @submit.prevent="saveSubsite">
        <div class="grid gap-4 md:grid-cols-2">
          <label class="form-field">
            <span class="form-label">名称</span>
            <input v-model.trim="form.name" class="input" required />
          </label>
          <label class="form-field">
            <span class="form-label">子站 ID</span>
            <input v-model.trim="form.subsite_id" class="input" :disabled="!!editing" placeholder="留空自动生成" />
          </label>
          <label class="form-field md:col-span-2">
            <span class="form-label">Public URL</span>
            <input v-model.trim="form.public_url" class="input" placeholder="https://edge.example.com" />
          </label>
          <label class="form-field">
            <span class="form-label">区域</span>
            <input v-model.trim="form.region" class="input" placeholder="us-east-1" />
          </label>
          <label class="form-field">
            <span class="form-label">版本</span>
            <input v-model.trim="form.version" class="input" placeholder="subsite-agent" />
          </label>
          <label class="form-field">
            <span class="form-label">最大 QPS</span>
            <input v-model.number="form.max_qps" type="number" min="0" class="input" />
          </label>
          <label class="form-field">
            <span class="form-label">最大并发</span>
            <input v-model.number="form.max_concurrency" type="number" min="0" class="input" />
          </label>
        </div>
        <label class="form-field">
          <span class="form-label">能力标签</span>
          <input v-model.trim="capabilitiesText" class="input" placeholder="openai,claude,gemini,images,websocket" />
        </label>
        <div class="flex justify-end gap-2 pt-2">
          <button type="button" class="btn btn-secondary" @click="closeForm">取消</button>
          <button type="submit" class="btn btn-primary" :disabled="saving">{{ saving ? '保存中...' : '保存' }}</button>
        </div>
      </form>
    </BaseDialog>

    <BaseDialog :show="!!createdSecret" title="子站密钥" width="normal" @close="createdSecret = ''">
      <div class="space-y-4">
        <p class="text-sm text-gray-600 dark:text-gray-300">密钥只显示一次，请写入子站镜像服务的环境变量。</p>
        <div class="flex items-center gap-2 rounded border border-gray-200 bg-gray-50 p-3 dark:border-dark-500 dark:bg-dark-700">
          <code class="min-w-0 flex-1 break-all text-sm">{{ createdSecret }}</code>
          <button class="btn btn-secondary" @click="copy(createdSecret)">复制</button>
        </div>
      </div>
    </BaseDialog>

    <BaseDialog :show="!!resetTarget" title="修复子站连通" width="normal" @close="closeResetSecret">
      <div class="space-y-4">
        <div class="rounded border border-amber-200 bg-amber-50 p-4 text-sm text-amber-900 dark:border-amber-600/40 dark:bg-amber-900/20 dark:text-amber-100">
          这会重置 {{ resetTarget?.name }} 的主站认证密钥。旧子站密钥会立即失效，必须把新密钥写入子站环境变量并重启子站后，心跳才会恢复。
        </div>
        <div v-if="resetSecretResult" class="space-y-3">
          <div class="flex items-center gap-2 rounded border border-gray-200 bg-gray-50 p-3 dark:border-dark-500 dark:bg-dark-700">
            <code class="min-w-0 flex-1 break-all text-sm">{{ resetSecretResult.secret }}</code>
            <button class="btn btn-secondary" @click="copy(resetSecretResult.secret)">复制</button>
          </div>
          <div class="rounded border border-gray-200 bg-gray-50 p-3 dark:border-dark-500 dark:bg-dark-700">
            <pre class="whitespace-pre-wrap break-all text-xs text-gray-700 dark:text-gray-200">{{ resetEnvText }}</pre>
          </div>
          <p class="text-xs text-gray-500 dark:text-gray-400">请确认 SUBSITE_MASTER_URL 是子站能够访问到的主站根地址，不要带 /api/v1。</p>
          <div class="flex justify-end gap-2">
            <button class="btn btn-secondary" @click="copy(resetEnvText)">复制环境变量</button>
            <button class="btn btn-primary" @click="closeResetSecret">完成</button>
          </div>
        </div>
        <div v-else class="flex justify-end gap-2">
          <button class="btn btn-secondary" :disabled="resettingSecret" @click="closeResetSecret">取消</button>
          <button class="btn btn-danger" :disabled="resettingSecret" @click="resetSubsiteSecret">
            {{ resettingSecret ? '修复中...' : '重置密钥' }}
          </button>
        </div>
      </div>
    </BaseDialog>

    <BaseDialog :show="!!leaseSubsite" :title="leaseSubsite ? `租约 - ${leaseSubsite.name}` : '租约'" width="full" @close="closeLeases">
      <div class="grid gap-6 xl:grid-cols-[minmax(0,1fr)_24rem]">
        <div class="min-w-0">
          <DataTable :columns="leaseColumns" :data="leases" :loading="leasesLoading">
            <template #header-select>
              <input
                type="checkbox"
                class="h-4 w-4 rounded border-gray-300 text-primary-600"
                :checked="allVisibleLeasesSelected"
                :disabled="leases.length === 0"
                @change="toggleAllVisibleLeases"
              />
            </template>
            <template #cell-select="{ row }">
              <input
                type="checkbox"
                class="h-4 w-4 rounded border-gray-300 text-primary-600"
                :checked="selectedLeaseIDs.has(row.lease_id)"
                @change="toggleLeaseSelection(row)"
              />
            </template>
            <template #cell-group="{ row }">
              <div class="min-w-0">
                <div class="truncate text-sm font-medium text-gray-900 dark:text-white">{{ leaseGroupLabel(row) }}</div>
                <div class="text-xs text-gray-500">{{ leaseGroupMeta(row) }}</div>
              </div>
            </template>
            <template #cell-account="{ row }">
              <div class="min-w-0">
                <div class="truncate text-sm font-medium text-gray-900 dark:text-white">{{ leaseAccountLabel(row) }}</div>
                <div class="text-xs text-gray-500">{{ row.platform || '-' }}</div>
              </div>
            </template>
            <template #cell-status="{ value }">
              <span :class="['badge', leaseStatusClass(value)]">{{ leaseStatusLabel(value) }}</span>
            </template>
            <template #cell-usage="{ row }">
              <div class="text-sm text-gray-700 dark:text-gray-200">
                请求 {{ row.used_requests }} / {{ row.max_requests || '-' }}
                <br />
                Token {{ row.used_tokens }} / {{ row.max_tokens || '-' }}
              </div>
            </template>
            <template #cell-expires_at="{ value }">
              <span class="text-sm text-gray-600 dark:text-gray-300">{{ formatDate(value) }}</span>
            </template>
            <template #cell-actions="{ row }">
              <div class="flex flex-wrap justify-end gap-1.5">
                <button class="btn btn-sm btn-secondary" @click="renewLease(row)">续租 24h</button>
                <button v-if="row.status === 'active' || row.status === 'renewing'" class="btn btn-sm btn-secondary" @click="drainLease(row)">排空</button>
                <button v-if="row.status !== 'released'" class="btn btn-sm btn-danger" @click="releaseLease(row)">释放</button>
                <button class="btn btn-sm btn-danger" @click="deleteLease(row)">删除</button>
              </div>
            </template>
          </DataTable>
          <Pagination
            v-if="leasePagination.total > 0"
            :page="leasePagination.page"
            :total="leasePagination.total"
            :page-size="leasePagination.page_size"
            :page-size-options="[10, 20]"
            class="mt-3"
            @update:page="handleLeasePageChange"
            @update:pageSize="handleLeasePageSizeChange"
          />
        </div>
        <div class="space-y-4">
        <form class="space-y-4 rounded-lg border border-gray-200 p-4 dark:border-dark-500" @submit.prevent="createLease">
          <div>
            <h3 class="text-sm font-semibold text-gray-900 dark:text-white">新增租约</h3>
            <p v-if="leaseAccountsLoading" class="mt-1 text-xs text-gray-500 dark:text-gray-400">正在按分组加载主站账号...</p>
          </div>
          <label class="form-field">
            <span class="form-label">分组</span>
            <select v-model.number="leaseForm.group_id" class="input" required @change="handleLeaseGroupChange">
              <option :value="0" disabled>选择分组</option>
              <option v-for="group in leaseGroupOptions" :key="group.id" :value="group.id">{{ group.name }}</option>
            </select>
          </label>
          <label class="form-field">
            <span class="form-label">账号</span>
            <input
              v-model.trim="leaseAccountSearch"
              class="input"
              :disabled="leaseForm.group_id <= 0"
              placeholder="输入账号 ID、名称或平台搜索"
            />
            <select v-if="filteredAccountOptions.length > 0" v-model.number="leaseForm.account_id" class="input" required :disabled="leaseForm.group_id <= 0">
              <option :value="0" disabled>选择账号</option>
              <option v-for="account in filteredAccountOptions" :key="account.id" :value="account.id">{{ account.label }}</option>
            </select>
            <div v-else class="rounded border border-dashed border-gray-300 px-3 py-2 text-sm text-gray-500 dark:border-dark-500 dark:text-gray-400">
              {{ leaseAccountEmptyText }}
            </div>
            <p v-if="leaseBulkSummary" class="text-xs text-gray-500 dark:text-gray-400">{{ leaseBulkSummary }}</p>
          </label>
          <label class="form-field">
            <span class="form-label">TTL 小时</span>
            <input v-model.number="leaseForm.ttl_hours" type="number" min="1" class="input" required />
          </label>
          <label class="form-field">
            <span class="form-label">最大请求数</span>
            <input v-model.number="leaseForm.max_requests" type="number" min="0" class="input" />
          </label>
          <label class="form-field">
            <span class="form-label">最大 Token</span>
            <input v-model.number="leaseForm.max_tokens" type="number" min="0" class="input" />
          </label>
          <label class="form-field">
            <span class="form-label">随机数量</span>
            <input
              v-model.number="leaseRandomCount"
              type="number"
              min="1"
              :max="bulkLeaseAccounts.length || 1"
              class="input"
              :disabled="leaseForm.group_id <= 0"
            />
          </label>
          <div class="grid gap-2 sm:grid-cols-2 xl:grid-cols-1 2xl:grid-cols-2">
            <button type="submit" class="btn btn-primary w-full" :disabled="leaseSaving || leaseBulkSaving || leaseForm.account_id <= 0">
              {{ leaseSaving ? '创建中...' : '创建租约' }}
            </button>
            <button
              type="button"
              class="btn btn-secondary w-full"
              :disabled="leaseSaving || leaseBulkSaving || leaseAccountsLoading || bulkLeaseAccounts.length === 0"
              @click="createAllVisibleLeases"
            >
              {{ leaseBulkSaving ? `添加中 ${leaseBulkProgress.done}/${leaseBulkProgress.total}` : `添加全部 ${bulkLeaseAccounts.length} 个账号` }}
            </button>
            <button
              type="button"
              class="btn btn-secondary w-full"
              :disabled="leaseSaving || leaseBulkSaving || leaseAccountsLoading || bulkLeaseAccounts.length === 0"
              @click="createRandomLeases(10)"
            >
              随机 {{ randomTenCount }} 个
            </button>
            <button
              type="button"
              class="btn btn-secondary w-full"
              :disabled="leaseSaving || leaseBulkSaving || leaseAccountsLoading || bulkLeaseAccounts.length === 0"
              @click="createRandomLeases(20)"
            >
              随机 {{ randomTwentyCount }} 个
            </button>
            <button
              type="button"
              class="btn btn-secondary w-full sm:col-span-2 xl:col-span-1 2xl:col-span-2"
              :disabled="leaseSaving || leaseBulkSaving || leaseAccountsLoading || bulkLeaseAccounts.length === 0 || normalizedLeaseRandomCount <= 0"
              @click="createRandomLeases(normalizedLeaseRandomCount)"
            >
              随机自定义 {{ normalizedLeaseRandomCount }} 个
            </button>
          </div>
        </form>
        <form class="space-y-4 rounded-lg border border-gray-200 p-4 dark:border-dark-500" @submit.prevent="updateSelectedLeaseLimits">
          <div>
            <h3 class="text-sm font-semibold text-gray-900 dark:text-white">批量更改设置</h3>
            <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">已选择 {{ selectedLeaseIDs.size }} 个租约</p>
          </div>
          <label class="form-field">
            <span class="form-label">最大并发</span>
            <input v-model.number="leaseBulkEditForm.max_concurrency" type="number" min="0" class="input" />
          </label>
          <label class="form-field">
            <span class="form-label">最大请求数</span>
            <input v-model.number="leaseBulkEditForm.max_requests" type="number" min="0" class="input" />
          </label>
          <label class="form-field">
            <span class="form-label">最大 Token</span>
            <input v-model.number="leaseBulkEditForm.max_tokens" type="number" min="0" class="input" />
          </label>
          <button type="submit" class="btn btn-primary w-full" :disabled="leaseBulkUpdating || selectedLeaseIDs.size === 0">
            {{ leaseBulkUpdating ? `保存中 ${leaseBulkProgress.done}/${leaseBulkProgress.total}` : '保存所选租约设置' }}
          </button>
          <div class="grid gap-2 sm:grid-cols-3 xl:grid-cols-1 2xl:grid-cols-3">
            <button type="button" class="btn btn-secondary w-full" :disabled="leaseBulkUpdating || selectedLeaseIDs.size === 0" @click="renewSelectedLeases">
              批量续租
            </button>
            <button type="button" class="btn btn-secondary w-full" :disabled="leaseBulkUpdating || selectedLeaseIDs.size === 0" @click="releaseSelectedLeases">
              批量释放
            </button>
            <button type="button" class="btn btn-danger w-full" :disabled="leaseBulkUpdating || selectedLeaseIDs.size === 0" @click="deleteSelectedLeases">
              批量删除
            </button>
          </div>
        </form>
        </div>
      </div>
    </BaseDialog>
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { adminAPI } from '@/api/admin'
import type { AccountLease, ResetSubsiteSecretResult, Subsite } from '@/api/admin'
import type { Account, AdminGroup } from '@/types'
import AppLayout from '@/components/layout/AppLayout.vue'
import TablePageLayout from '@/components/layout/TablePageLayout.vue'
import DataTable from '@/components/common/DataTable.vue'
import Pagination from '@/components/common/Pagination.vue'
import BaseDialog from '@/components/common/BaseDialog.vue'
import Icon from '@/components/icons/Icon.vue'
import type { Column } from '@/components/common/types'
import { useClipboard } from '@/composables/useClipboard'

const subsites = ref<Subsite[]>([])
const leases = ref<AccountLease[]>([])
const leaseAccounts = ref<Account[]>([])
const leaseGroups = ref<AdminGroup[]>([])
const activeLeaseAccountIds = ref<Set<number>>(new Set())
const loading = ref(false)
const leasesLoading = ref(false)
const saving = ref(false)
const leaseSaving = ref(false)
const leaseBulkSaving = ref(false)
const leaseBulkUpdating = ref(false)
const leaseAccountsLoading = ref(false)
const resettingSecret = ref(false)
const search = ref('')
const status = ref('')
const leaseAccountSearch = ref('')
const leaseBulkSummary = ref('')
const leaseRandomCount = ref(10)
const searchTimer = ref<number | undefined>()
const showForm = ref(false)
const editing = ref<Subsite | null>(null)
const createdSecret = ref('')
const leaseSubsite = ref<Subsite | null>(null)
const resetTarget = ref<Subsite | null>(null)
const resetSecretResult = ref<ResetSubsiteSecretResult | null>(null)
const capabilitiesText = ref('openai,claude,gemini,images,websocket')
const { copyToClipboard } = useClipboard()

const resetEnvText = computed(() => {
  if (!resetTarget.value || !resetSecretResult.value) return ''
  return [
    `SUBSITE_ID=${resetTarget.value.subsite_id}`,
    `SUBSITE_PUBLIC_URL=${resetTarget.value.public_url}`,
    `SUBSITE_MASTER_URL=${window.location.origin}`,
    `SUBSITE_MASTER_SECRET=${resetSecretResult.value.secret}`
  ].join('\n')
})

const pagination = reactive({ page: 1, page_size: 20, total: 0 })
const leasePagination = reactive({ page: 1, page_size: 10, total: 0 })
const form = reactive({
  subsite_id: '',
  name: '',
  public_url: '',
  region: '',
  version: '',
  max_qps: 0,
  max_concurrency: 0
})
const leaseForm = reactive({
  group_id: 0,
  account_id: 0,
  ttl_hours: 24,
  max_requests: 0,
  max_tokens: 0
})
const leaseBulkEditForm = reactive({
  max_concurrency: 0,
  max_requests: 0,
  max_tokens: 0
})
const selectedLeaseIDs = ref<Set<string>>(new Set())

const leaseGroupOptions = computed(() => leaseGroups.value
  .filter((group) => group.status === 'active')
  .map((group) => ({
    id: group.id,
    name: group.name
  })))

const filteredLeaseAccounts = computed(() => {
  if (leaseForm.group_id <= 0) return []
  const query = leaseAccountSearch.value.trim().toLowerCase()
  return leaseAccounts.value.filter((account) => {
    if (activeLeaseAccountIDs.value.has(account.id)) return false
    if (!query) return true
    return accountSearchText(account).includes(query)
  })
})

const filteredAccountOptions = computed(() => filteredLeaseAccounts.value.map((account) => ({
  id: account.id,
  label: accountOptionLabel(account)
})))

const activeLeaseAccountIDs = computed(() => new Set(
  activeLeaseAccountIds.value
))

const bulkLeaseAccounts = computed(() => filteredLeaseAccounts.value)
const allVisibleLeasesSelected = computed(() => leases.value.length > 0 && leases.value.every((lease) => selectedLeaseIDs.value.has(lease.lease_id)))
const randomTenCount = computed(() => Math.min(10, bulkLeaseAccounts.value.length))
const randomTwentyCount = computed(() => Math.min(20, bulkLeaseAccounts.value.length))
const normalizedLeaseRandomCount = computed(() => Math.min(
  Math.max(Number.isFinite(leaseRandomCount.value) ? Math.floor(leaseRandomCount.value) : 0, 0),
  bulkLeaseAccounts.value.length
))

const leaseBulkProgress = reactive({
  done: 0,
  total: 0
})

const leaseAccountEmptyText = computed(() => {
  if (leaseForm.group_id <= 0) return '请先选择分组'
  if (leaseAccountSearch.value.trim()) return '没有匹配当前搜索的可用账号'
  return '当前分组下没有可用活跃账号'
})

const columns = computed<Column[]>(() => [
  { key: 'name', label: '子站' },
  { key: 'status', label: '状态' },
  { key: 'public_url', label: '入口 URL' },
  { key: 'region', label: '区域' },
  { key: 'health_score', label: '健康' },
  { key: 'limits', label: '限制' },
  { key: 'actions', label: '操作', class: 'text-right' }
])

const leaseColumns = computed<Column[]>(() => [
  { key: 'select', label: '' },
  { key: 'lease_id', label: '租约 ID' },
  { key: 'group', label: '分组' },
  { key: 'account', label: '账号' },
  { key: 'status', label: '状态' },
  { key: 'usage', label: '用量' },
  { key: 'expires_at', label: '过期时间' },
  { key: 'actions', label: '操作', class: 'text-right' }
])

async function reload(): Promise<void> {
  loading.value = true
  try {
    const result = await adminAPI.subsites.list(pagination.page, pagination.page_size, {
      search: search.value || undefined,
      status: status.value || undefined
    })
    subsites.value = result.items
    pagination.total = result.total
  } finally {
    loading.value = false
  }
}

function handleSearch(): void {
  window.clearTimeout(searchTimer.value)
  searchTimer.value = window.setTimeout(() => {
    pagination.page = 1
    void reload()
  }, 300)
}

function handlePageChange(page: number): void {
  pagination.page = page
  void reload()
}

function handlePageSizeChange(pageSize: number): void {
  pagination.page_size = pageSize
  pagination.page = 1
  void reload()
}

function openCreate(): void {
  editing.value = null
  Object.assign(form, { subsite_id: '', name: '', public_url: '', region: '', version: '', max_qps: 0, max_concurrency: 0 })
  capabilitiesText.value = 'openai,claude,gemini,images,websocket'
  showForm.value = true
}

function openEdit(row: Subsite): void {
  editing.value = row
  Object.assign(form, {
    subsite_id: row.subsite_id,
    name: row.name,
    public_url: row.public_url,
    region: row.region,
    version: row.version,
    max_qps: row.max_qps,
    max_concurrency: row.max_concurrency
  })
  capabilitiesText.value = (row.capabilities || []).join(',')
  showForm.value = true
}

function closeForm(): void {
  showForm.value = false
  editing.value = null
}

async function saveSubsite(): Promise<void> {
  saving.value = true
  const payload = {
    ...form,
    capabilities: capabilitiesText.value.split(',').map((item) => item.trim()).filter(Boolean)
  }
  try {
    if (editing.value) {
      await adminAPI.subsites.update(editing.value.subsite_id, payload)
    } else {
      const result = await adminAPI.subsites.create(payload)
      createdSecret.value = result.secret
    }
    closeForm()
    await reload()
  } finally {
    saving.value = false
  }
}

async function changeStatus(row: Subsite, action: 'activate' | 'pause' | 'resume'): Promise<void> {
  if (action === 'activate') await adminAPI.subsites.activate(row.subsite_id)
  if (action === 'pause') await adminAPI.subsites.pause(row.subsite_id)
  if (action === 'resume') await adminAPI.subsites.resume(row.subsite_id)
  await reload()
}

function openResetSecret(row: Subsite): void {
  resetTarget.value = row
  resetSecretResult.value = null
}

function closeResetSecret(): void {
  if (resettingSecret.value) return
  resetTarget.value = null
  resetSecretResult.value = null
}

async function resetSubsiteSecret(): Promise<void> {
  if (!resetTarget.value) return
  resettingSecret.value = true
  try {
    resetSecretResult.value = await adminAPI.subsites.resetSecret(resetTarget.value.subsite_id)
    await reload()
  } finally {
    resettingSecret.value = false
  }
}

async function openLeases(row: Subsite): Promise<void> {
  leaseSubsite.value = row
  resetLeaseForm()
  leasePagination.page = 1
  await Promise.all([loadLeases(), loadLeaseGroups()])
}

function closeLeases(): void {
  leaseSubsite.value = null
  leases.value = []
  leaseAccounts.value = []
  selectedLeaseIDs.value = new Set()
  activeLeaseAccountIds.value = new Set()
  leasePagination.page = 1
  leasePagination.total = 0
  resetLeaseForm()
}

async function loadLeases(): Promise<void> {
  if (!leaseSubsite.value) return
  leasesLoading.value = true
  try {
    const result = await adminAPI.subsites.listLeases(
      leaseSubsite.value.subsite_id,
      leasePagination.page,
      leasePagination.page_size
    )
    leases.value = result.items
    leasePagination.total = result.total
    selectedLeaseIDs.value = new Set([...selectedLeaseIDs.value].filter((leaseID) => result.items.some((item) => item.lease_id === leaseID)))
    if (result.items.length === 0 && result.total > 0 && leasePagination.page > 1) {
      leasePagination.page = Math.max(1, Math.ceil(result.total / leasePagination.page_size))
      await loadLeases()
      return
    }
    await refreshActiveLeaseAccountIds()
  } finally {
    leasesLoading.value = false
  }
}

async function refreshActiveLeaseAccountIds(): Promise<void> {
  if (!leaseSubsite.value) return
  const accountIds = await adminAPI.subsites.listLeaseActiveAccountIds(leaseSubsite.value.subsite_id)
  activeLeaseAccountIds.value = new Set(accountIds)
}

function handleLeasePageChange(page: number): void {
  leasePagination.page = page
  void loadLeases()
}

function handleLeasePageSizeChange(pageSize: number): void {
  leasePagination.page_size = pageSize
  leasePagination.page = 1
  void loadLeases()
}

async function loadLeaseAccounts(): Promise<void> {
  if (leaseForm.group_id <= 0) {
    leaseAccounts.value = []
    return
  }
  leaseAccountsLoading.value = true
  try {
    const pageSize = 200
    const items: Account[] = []
    let page = 1
    let total = 0
    do {
      const result = await adminAPI.accounts.list(page, pageSize, {
        status: 'active',
        group: String(leaseForm.group_id),
        search: '',
        sort_by: 'id',
        sort_order: 'desc'
      })
      items.push(...result.items)
      total = result.total
      page += 1
    } while (items.length < total)
    leaseAccounts.value = items
  } finally {
    leaseAccountsLoading.value = false
  }
}

async function loadLeaseGroups(): Promise<void> {
  leaseGroups.value = await adminAPI.groups.getAll(undefined, 'public')
}

function handleLeaseGroupChange(): void {
  leaseForm.account_id = 0
  leaseAccountSearch.value = ''
  leaseAccounts.value = []
  leaseBulkSummary.value = ''
  void loadLeaseAccounts()
}

function resetLeaseForm(): void {
  Object.assign(leaseForm, {
    group_id: 0,
    account_id: 0,
    ttl_hours: 24,
    max_requests: 0,
    max_tokens: 0
  })
  leaseAccountSearch.value = ''
  leaseBulkSummary.value = ''
  leaseBulkProgress.done = 0
  leaseBulkProgress.total = 0
  leaseRandomCount.value = 10
  resetLeaseBulkEditForm()
}

function resetLeaseBulkEditForm(): void {
  Object.assign(leaseBulkEditForm, {
    max_concurrency: 0,
    max_requests: 0,
    max_tokens: 0
  })
}

function toggleLeaseSelection(row: AccountLease): void {
  const next = new Set(selectedLeaseIDs.value)
  if (next.has(row.lease_id)) {
    next.delete(row.lease_id)
  } else {
    next.add(row.lease_id)
  }
  selectedLeaseIDs.value = next
}

function toggleAllVisibleLeases(): void {
  if (allVisibleLeasesSelected.value) {
    selectedLeaseIDs.value = new Set()
    return
  }
  selectedLeaseIDs.value = new Set(leases.value.map((lease) => lease.lease_id))
}

async function createLease(): Promise<void> {
  if (!leaseSubsite.value) return
  leaseSaving.value = true
  try {
    await adminAPI.subsites.createLease(leaseSubsite.value.subsite_id, buildLeasePayload(leaseForm.account_id))
    resetLeaseForm()
    await loadLeases()
  } finally {
    leaseSaving.value = false
  }
}

async function updateSelectedLeaseLimits(): Promise<void> {
  if (!leaseSubsite.value || selectedLeaseIDs.value.size === 0) return
  const leaseIDs = [...selectedLeaseIDs.value]
  const confirmed = window.confirm(`确认更新 ${leaseIDs.length} 个租约的请求数和最大 Token 设置？`)
  if (!confirmed) return

  leaseBulkUpdating.value = true
  leaseBulkSummary.value = ''
  leaseBulkProgress.done = 0
  leaseBulkProgress.total = leaseIDs.length
  let success = 0
  let failed = 0
  try {
    for (const leaseID of leaseIDs) {
      try {
        await adminAPI.subsites.updateLease(leaseSubsite.value.subsite_id, leaseID, {
          max_concurrency: normalizeNonNegativeNumber(leaseBulkEditForm.max_concurrency),
          max_requests: normalizeNonNegativeNumber(leaseBulkEditForm.max_requests),
          max_tokens: normalizeNonNegativeNumber(leaseBulkEditForm.max_tokens)
        })
        success += 1
      } catch {
        failed += 1
      } finally {
        leaseBulkProgress.done += 1
      }
    }
    leaseBulkSummary.value = `批量保存完成：成功 ${success} 个，失败 ${failed} 个`
    selectedLeaseIDs.value = new Set()
    await loadLeases()
  } finally {
    leaseBulkUpdating.value = false
  }
}

async function renewSelectedLeases(): Promise<void> {
  await runSelectedLeaseAction('续租 24 小时', async (leaseID) => {
    if (!leaseSubsite.value) return
    await adminAPI.subsites.renewLease(leaseSubsite.value.subsite_id, leaseID, { ttl_seconds: 24 * 3600 })
  })
}

async function releaseSelectedLeases(): Promise<void> {
  await runSelectedLeaseAction('释放', async (leaseID) => {
    if (!leaseSubsite.value) return
    await adminAPI.subsites.releaseLease(leaseSubsite.value.subsite_id, leaseID)
  })
}

async function deleteSelectedLeases(): Promise<void> {
  await runSelectedLeaseAction('删除', async (leaseID) => {
    if (!leaseSubsite.value) return
    await adminAPI.subsites.deleteLease(leaseSubsite.value.subsite_id, leaseID)
  })
}

async function runSelectedLeaseAction(label: string, action: (leaseID: string) => Promise<void>): Promise<void> {
  if (!leaseSubsite.value || selectedLeaseIDs.value.size === 0) return
  const leaseIDs = [...selectedLeaseIDs.value]
  const confirmed = window.confirm(`确认${label} ${leaseIDs.length} 个租约？`)
  if (!confirmed) return

  leaseBulkUpdating.value = true
  leaseBulkSummary.value = ''
  leaseBulkProgress.done = 0
  leaseBulkProgress.total = leaseIDs.length
  let success = 0
  let failed = 0
  try {
    for (const leaseID of leaseIDs) {
      try {
        await action(leaseID)
        success += 1
      } catch {
        failed += 1
      } finally {
        leaseBulkProgress.done += 1
      }
    }
    leaseBulkSummary.value = `批量${label}完成：成功 ${success} 个，失败 ${failed} 个`
    selectedLeaseIDs.value = new Set()
    await loadLeases()
  } finally {
    leaseBulkUpdating.value = false
  }
}

async function createAllVisibleLeases(): Promise<void> {
  if (!leaseSubsite.value || bulkLeaseAccounts.value.length === 0) return
  const accounts = [...bulkLeaseAccounts.value]
  await createBulkLeases(accounts, `确认给当前筛选出的 ${accounts.length} 个账号全部创建租约？`)
}

async function createRandomLeases(count: number): Promise<void> {
  if (!leaseSubsite.value || bulkLeaseAccounts.value.length === 0) return
  const normalizedCount = Math.min(Math.max(Math.floor(count), 0), bulkLeaseAccounts.value.length)
  if (normalizedCount <= 0) return
  const accounts = shuffleAccounts(bulkLeaseAccounts.value).slice(0, normalizedCount)
  await createBulkLeases(accounts, `确认随机选择 ${accounts.length} 个可用账号创建租约？`)
}

async function createBulkLeases(accounts: Account[], confirmMessage: string): Promise<void> {
  if (!leaseSubsite.value || accounts.length === 0) return
  const confirmed = window.confirm(confirmMessage)
  if (!confirmed) return

  leaseBulkSaving.value = true
  leaseBulkSummary.value = ''
  leaseBulkProgress.done = 0
  leaseBulkProgress.total = accounts.length
  let success = 0
  let failed = 0
  try {
    for (const account of accounts) {
      try {
        await adminAPI.subsites.createLease(leaseSubsite.value.subsite_id, buildLeasePayload(account.id))
        success += 1
      } catch {
        failed += 1
      } finally {
        leaseBulkProgress.done += 1
      }
    }
    leaseBulkSummary.value = `批量添加完成：成功 ${success} 个，失败 ${failed} 个`
    leaseForm.account_id = 0
    leasePagination.page = 1
    await loadLeases()
  } finally {
    leaseBulkSaving.value = false
  }
}

function shuffleAccounts(accounts: Account[]): Account[] {
  const shuffled = [...accounts]
  for (let index = shuffled.length - 1; index > 0; index -= 1) {
    const swapIndex = Math.floor(Math.random() * (index + 1))
    const current = shuffled[index]
    shuffled[index] = shuffled[swapIndex]
    shuffled[swapIndex] = current
  }
  return shuffled
}

function buildLeasePayload(accountID: number) {
  return {
    group_id: leaseForm.group_id,
    account_id: accountID,
    ttl_seconds: Math.max(1, leaseForm.ttl_hours) * 3600,
    max_requests: leaseForm.max_requests || 0,
    max_tokens: leaseForm.max_tokens || 0
  }
}

function normalizeNonNegativeNumber(value: number): number {
  if (!Number.isFinite(value)) return 0
  return Math.max(0, Math.floor(value))
}

function accountOptionLabel(account: Account): string {
  const parts = [`#${account.id}`, account.name || account.platform || 'account']
  if (account.account_level && account.account_level !== 'unknown') parts.push(account.account_level)
  if (!account.schedulable) parts.push('不可调度')
  return parts.join(' · ')
}

function accountSearchText(account: Account): string {
  return [
    account.id,
    account.name,
    account.platform,
    account.account_level,
    account.type,
    account.status
  ].filter((item) => item !== undefined && item !== null).join(' ').toLowerCase()
}

async function renewLease(row: AccountLease): Promise<void> {
  if (!leaseSubsite.value) return
  await adminAPI.subsites.renewLease(leaseSubsite.value.subsite_id, row.lease_id, { ttl_seconds: 24 * 3600 })
  await loadLeases()
}

async function drainLease(row: AccountLease): Promise<void> {
  if (!leaseSubsite.value) return
  await adminAPI.subsites.drainLease(leaseSubsite.value.subsite_id, row.lease_id)
  await loadLeases()
}

async function releaseLease(row: AccountLease): Promise<void> {
  if (!leaseSubsite.value) return
  await adminAPI.subsites.releaseLease(leaseSubsite.value.subsite_id, row.lease_id)
  selectedLeaseIDs.value = new Set([...selectedLeaseIDs.value].filter((leaseID) => leaseID !== row.lease_id))
  await loadLeases()
}

async function deleteLease(row: AccountLease): Promise<void> {
  if (!leaseSubsite.value) return
  const confirmed = window.confirm(`确认删除租约 ${row.lease_id}？该操作不可撤销。`)
  if (!confirmed) return
  await adminAPI.subsites.deleteLease(leaseSubsite.value.subsite_id, row.lease_id)
  selectedLeaseIDs.value = new Set([...selectedLeaseIDs.value].filter((leaseID) => leaseID !== row.lease_id))
  await loadLeases()
}

async function copy(value: string): Promise<void> {
  await copyToClipboard(value)
}

async function copyClientConfig(row: Subsite): Promise<void> {
  const config = {
    base_url: row.public_url,
    endpoints: [
      '/v1/messages',
      '/v1/responses',
      '/v1/chat/completions',
      '/v1/images/generations',
      '/v1beta/models/*'
    ]
  }
  await copyToClipboard(JSON.stringify(config, null, 2))
}

function statusLabel(value: string): string {
  return ({ pending: '待激活', active: '运行中', maintenance: '维护中', unhealthy: '异常', disabled: '已停用' } as Record<string, string>)[value] || value
}

function statusClass(value: string): string {
  return ({ active: 'badge-success', pending: 'badge-warning', maintenance: 'badge-warning', unhealthy: 'badge-danger', disabled: 'badge-gray' } as Record<string, string>)[value] || 'badge-gray'
}

function leaseStatusLabel(value: string): string {
  return ({ active: '可用', renewing: '续租中', draining: '排空中', released: '已释放', expired: '已过期', revoked: '已撤销' } as Record<string, string>)[value] || value
}

function leaseStatusClass(value: string): string {
  return ({ active: 'badge-success', renewing: 'badge-primary', draining: 'badge-warning', released: 'badge-gray', expired: 'badge-gray', revoked: 'badge-danger' } as Record<string, string>)[value] || 'badge-gray'
}

function leaseGroupLabel(row: AccountLease): string {
  return row.group_name || (row.group_id ? `#${row.group_id}` : '-')
}

function leaseGroupMeta(row: AccountLease): string {
  return row.group_id ? `Group ID: ${row.group_id}` : '未绑定分组'
}

function leaseAccountLabel(row: AccountLease): string {
  return row.account_name || `#${row.account_id}`
}

function formatDate(value?: string): string {
  if (!value) return '-'
  return new Date(value).toLocaleString()
}

onMounted(() => {
  void reload()
})
</script>
