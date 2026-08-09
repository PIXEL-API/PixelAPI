<template>
  <AppLayout>
    <div class="space-y-5">
      <div
        class="flex flex-col gap-3 lg:flex-row lg:items-center lg:justify-between"
      >
        <div>
          <h1 class="text-2xl font-semibold text-gray-900 dark:text-white">
            发票管理
          </h1>
          <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">
            处理用户提交的发票申请
          </p>
        </div>
        <div class="flex w-full flex-wrap gap-2 lg:w-auto">
          <select
            v-model="filters.status"
            class="input w-full sm:w-36"
            @change="handleFilterChange"
          >
            <option value="">全部状态</option>
            <option value="pending">待处理</option>
            <option value="issued">已开票</option>
            <option value="rejected">已驳回</option>
            <option value="cancelled">已取消</option>
          </select>
          <input
            v-model.trim="filters.keyword"
            class="input w-full sm:w-56"
            type="search"
            placeholder="申请号 / 用户 / 抬头"
            @keyup.enter="handleSearch"
          />
          <button
            class="btn btn-secondary min-h-11 flex-1 sm:flex-none"
            type="button"
            @click="handleSearch"
            :disabled="loading"
          >
            查询
          </button>
          <button
            class="btn btn-primary min-h-11 flex-1 sm:flex-none"
            type="button"
            @click="exportSelected"
            :disabled="exporting || selectedCount === 0"
          >
            {{ exporting ? "正在导出" : `批量导出（${selectedCount}）` }}
          </button>
        </div>
      </div>

      <div class="grid gap-5 xl:grid-cols-[minmax(0,1fr)_420px]">
        <section class="card overflow-hidden">
          <div
            v-if="selectedCount > 0"
            class="flex flex-col gap-2 border-b border-gray-100 px-4 py-3 text-sm sm:flex-row sm:items-center sm:justify-between dark:border-dark-700"
          >
            <span class="text-gray-600 dark:text-gray-300">
              已选择 {{ selectedCount }} 条待处理申请
            </span>
            <button
              class="btn btn-sm btn-secondary min-h-11 self-start sm:self-auto"
              type="button"
              @click="clearSelection"
            >
              清除选择
            </button>
          </div>
          <div class="overflow-x-auto">
            <table
              class="min-w-full divide-y divide-gray-100 text-sm dark:divide-dark-700"
            >
              <thead
                class="bg-gray-50 text-left text-xs uppercase text-gray-500 dark:bg-dark-800 dark:text-gray-400"
              >
                <tr>
                  <th class="w-14 px-2 py-1 text-center">
                    <label
                      class="inline-flex min-h-11 min-w-11 cursor-pointer items-center justify-center"
                    >
                      <input
                        type="checkbox"
                        class="rounded border-gray-300 text-primary-600"
                        :checked="allVisibleSelected"
                        :indeterminate="someVisibleSelected"
                        :disabled="selectableRequests.length === 0"
                        @change="toggleVisibleSelection"
                      />
                      <span class="sr-only">选择当前待处理申请</span>
                    </label>
                  </th>
                  <th class="px-4 py-3">申请号</th>
                  <th class="px-4 py-3">用户</th>
                  <th class="px-4 py-3">抬头</th>
                  <th class="px-4 py-3">类型</th>
                  <th class="px-4 py-3 text-right">金额</th>
                  <th class="px-4 py-3">状态</th>
                  <th class="px-4 py-3">申请时间</th>
                </tr>
              </thead>
              <tbody class="divide-y divide-gray-100 dark:divide-dark-700">
                <tr
                  v-for="request in requests"
                  :key="request.id"
                  class="cursor-pointer text-gray-700 hover:bg-gray-50 dark:text-gray-300 dark:hover:bg-dark-800"
                  :class="{
                    'bg-primary-50/70 dark:bg-primary-900/20':
                      selected?.id === request.id,
                  }"
                  @click="selectRequest(request)"
                >
                  <td class="px-2 py-1 text-center" @click.stop>
                    <label
                      v-if="request.status === 'pending'"
                      class="inline-flex min-h-11 min-w-11 cursor-pointer items-center justify-center"
                    >
                      <input
                        type="checkbox"
                        class="rounded border-gray-300 text-primary-600"
                        :checked="isSelected(request.id)"
                        @change="toggleSelection(request.id)"
                      />
                      <span class="sr-only">选择 {{ request.request_no }}</span>
                    </label>
                    <span v-else class="text-gray-300 dark:text-dark-600">-</span>
                  </td>
                  <td class="px-4 py-3 font-mono text-xs">
                    {{ request.request_no }}
                  </td>
                  <td class="px-4 py-3">{{ request.user_email }}</td>
                  <td class="px-4 py-3">{{ request.title_name }}</td>
                  <td class="px-4 py-3">
                    {{ invoiceTypeLabel(request.invoice_type) }}
                  </td>
                  <td
                    class="px-4 py-3 text-right font-medium text-gray-900 dark:text-white"
                  >
                    {{ formatMoney(request.amount) }}
                  </td>
                  <td class="px-4 py-3">{{ statusLabel(request.status) }}</td>
                  <td class="px-4 py-3">
                    {{ formatDateTime(request.created_at) }}
                  </td>
                </tr>
                <tr v-if="!requests.length">
                  <td
                    colspan="8"
                    class="px-4 py-8 text-center text-sm text-gray-500 dark:text-gray-400"
                  >
                    暂无发票申请
                  </td>
                </tr>
              </tbody>
            </table>
          </div>
          <Pagination
            v-if="pagination.total > 0"
            class="invoice-pagination"
            :page="pagination.page"
            :page-size="pagination.page_size"
            :total="pagination.total"
            @update:page="handlePageChange"
            @update:pageSize="handlePageSizeChange"
          />
        </section>

        <aside class="card p-5">
          <template v-if="selected">
            <div class="mb-4 flex items-start justify-between gap-3">
              <div>
                <h2 class="text-lg font-semibold text-gray-900 dark:text-white">
                  {{ selected.title_name }}
                </h2>
                <p
                  class="mt-1 font-mono text-xs text-gray-500 dark:text-gray-400"
                >
                  {{ selected.request_no }}
                </p>
              </div>
              <span
                class="rounded bg-gray-100 px-2 py-1 text-xs text-gray-600 dark:bg-dark-800 dark:text-gray-300"
              >
                {{ statusLabel(selected.status) }}
              </span>
            </div>

            <dl class="grid gap-3 text-sm">
              <div class="flex justify-between gap-4">
                <dt class="text-gray-500 dark:text-gray-400">类型</dt>
                <dd class="text-right text-gray-900 dark:text-white">
                  {{ invoiceTypeLabel(selected.invoice_type) }}
                </dd>
              </div>
              <div class="flex justify-between gap-4">
                <dt class="text-gray-500 dark:text-gray-400">税号</dt>
                <dd class="text-right text-gray-900 dark:text-white">
                  {{ selected.tax_id || "-" }}
                </dd>
              </div>
              <div class="flex justify-between gap-4">
                <dt class="text-gray-500 dark:text-gray-400">邮箱</dt>
                <dd class="text-right text-gray-900 dark:text-white">
                  {{ selected.recipient_email }}
                </dd>
              </div>
              <div class="flex justify-between gap-4">
                <dt class="text-gray-500 dark:text-gray-400">金额</dt>
                <dd
                  class="text-right font-semibold text-gray-900 dark:text-white"
                >
                  {{ formatMoney(selected.amount) }}
                </dd>
              </div>
              <div class="flex justify-between gap-4">
                <dt class="text-gray-500 dark:text-gray-400">发票备注</dt>
                <dd
                  class="max-w-[70%] whitespace-pre-wrap break-words text-right text-gray-900 dark:text-white"
                >
                  {{ selected.remark || "-" }}
                </dd>
              </div>
            </dl>

            <div
              v-if="selected.invoice_type === 'enterprise_special'"
              class="mt-5 rounded border border-gray-100 p-3 text-sm dark:border-dark-700"
            >
              <div class="grid gap-2">
                <p>
                  <span class="text-gray-500 dark:text-gray-400"
                    >注册地址：</span
                  >{{ selected.registered_address }}
                </p>
                <p>
                  <span class="text-gray-500 dark:text-gray-400"
                    >注册电话：</span
                  >{{ selected.registered_phone }}
                </p>
                <p>
                  <span class="text-gray-500 dark:text-gray-400">开户行：</span
                  >{{ selected.bank_name }}
                </p>
                <p>
                  <span class="text-gray-500 dark:text-gray-400"
                    >银行账号：</span
                  >{{ selected.bank_account }}
                </p>
              </div>
            </div>

            <div class="mt-5">
              <h3
                class="mb-2 text-sm font-semibold text-gray-900 dark:text-white"
              >
                来源明细
              </h3>
              <div class="space-y-2">
                <div
                  v-for="item in selected.items || []"
                  :key="item.id"
                  class="rounded border border-gray-100 p-3 text-sm dark:border-dark-700"
                >
                  <div class="flex justify-between gap-3">
                    <span class="font-medium text-gray-900 dark:text-white">{{
                      item.source_label
                    }}</span>
                    <span class="font-semibold text-gray-900 dark:text-white">{{
                      formatMoney(item.invoice_amount)
                    }}</span>
                  </div>
                  <p
                    class="mt-1 font-mono text-xs text-gray-500 dark:text-gray-400"
                  >
                    {{ item.source_no }}
                  </p>
                </div>
              </div>
            </div>

            <div v-if="selected.status === 'pending'" class="mt-5 space-y-4">
              <div>
                <label class="input-label">处理备注</label>
                <textarea
                  v-model.trim="issueForm.admin_note"
                  class="input min-h-20"
                  placeholder="可选，仅管理员可见"
                ></textarea>
              </div>
              <div class="flex gap-2">
                <button
                  class="btn btn-primary flex-1"
                  type="button"
                  @click="issueSelected"
                  :disabled="processing"
                >
                  标记已开票
                </button>
                <button
                  class="btn btn-danger flex-1"
                  type="button"
                  @click="openRejectDialog"
                  :disabled="processing"
                >
                  驳回
                </button>
              </div>
            </div>

            <div
              v-else
              class="mt-5 rounded border border-gray-100 p-3 text-sm dark:border-dark-700"
            >
              <p v-if="selected.status === 'issued'">
                该申请已标记为已开票。
              </p>
              <p v-if="selected.rejected_reason">
                <span class="text-gray-500 dark:text-gray-400">驳回原因：</span
                >{{ selected.rejected_reason }}
              </p>
              <p v-if="selected.admin_note">
                <span class="text-gray-500 dark:text-gray-400">处理备注：</span
                >{{ selected.admin_note }}
              </p>
            </div>
          </template>
          <div
            v-else
            class="py-12 text-center text-sm text-gray-500 dark:text-gray-400"
          >
            请选择一条发票申请
          </div>
        </aside>
      </div>

      <BaseDialog
        :show="!!rejectTarget"
        title="驳回发票申请"
        width="narrow"
        :close-disabled="processing"
        @close="closeRejectDialog"
      >
        <div v-if="rejectTarget" class="space-y-4">
          <p class="text-sm text-gray-600 dark:text-gray-300">
            正在驳回申请 {{ rejectTarget.request_no }}。驳回后会释放对应开票来源。
          </p>
          <div>
            <label class="input-label">驳回原因（用户可见）</label>
            <textarea
              v-model="rejectReason"
              class="input mt-1.5 min-h-24"
              placeholder="请填写具体驳回原因"
              required
            ></textarea>
            <p class="mt-1.5 text-xs text-gray-500 dark:text-gray-400">
              此原因会直接显示在用户的开票申请中。
            </p>
          </div>
          <div class="flex justify-end gap-2">
            <button
              class="btn btn-secondary min-h-11"
              type="button"
              :disabled="processing"
              @click="closeRejectDialog"
            >
              取消
            </button>
            <button
              class="btn btn-danger min-h-11"
              type="button"
              :disabled="processing || !rejectReason.trim()"
              @click="rejectSelected"
            >
              {{ processing ? "处理中" : "确认驳回" }}
            </button>
          </div>
        </div>
      </BaseDialog>
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref } from "vue";
import { saveAs } from "file-saver";
import type { CellObject } from "xlsx";
import AppLayout from "@/components/layout/AppLayout.vue";
import BaseDialog from "@/components/common/BaseDialog.vue";
import Pagination from "@/components/common/Pagination.vue";
import adminInvoicesAPI from "@/api/admin/invoices";
import { useAppStore } from "@/stores/app";
import type { InvoiceRequest, InvoiceType } from "@/types";
import { getPersistedPageSize } from "@/composables/usePersistedPageSize";
import { useTableSelection } from "@/composables/useTableSelection";
import { extractApiErrorMessage } from "@/utils/apiError";
import { formatCurrency, formatDateTime } from "@/utils/format";

const invoiceExportHeaders = [
  "序号",
  "发票类型",
  "公司名称",
  "税号",
  "开票 名称",
  "总金额",
  "邮箱号",
  "开户行",
  "银行账号",
  "发票备注",
] as const;
const invoiceExportItemName = "信息服务费";

const appStore = useAppStore();
const loading = ref(false);
const processing = ref(false);
const exporting = ref(false);
const requests = ref<InvoiceRequest[]>([]);
let loadSequence = 0;
const selected = ref<InvoiceRequest | null>(null);
const rejectTarget = ref<InvoiceRequest | null>(null);
const rejectReason = ref("");
const filters = reactive({
  status: "",
  keyword: "",
});
const pagination = reactive({
  page: 1,
  page_size: getPersistedPageSize(50),
  total: 0,
});
const issueForm = reactive({
  admin_note: "",
});
const selectableRequests = computed(() =>
  requests.value.filter((request) => request.status === "pending"),
);
const {
  selectedCount,
  allVisibleSelected,
  isSelected,
  toggle: toggleSelection,
  clear: clearSelection,
  toggleVisible,
} = useTableSelection({
  rows: selectableRequests,
  getId: (request) => request.id,
});
const someVisibleSelected = computed(
  () =>
    !allVisibleSelected.value &&
    selectableRequests.value.some((request) => isSelected(request.id)),
);
const selectedRequests = computed(() =>
  requests.value.filter(
    (request) => request.status === "pending" && isSelected(request.id),
  ),
);

onMounted(() => {
  void loadRequests();
});

async function loadRequests(): Promise<void> {
  const currentLoadSequence = ++loadSequence;
  loading.value = true;
  try {
    let { data } = await fetchRequestsPage();
    if (currentLoadSequence !== loadSequence) return;

    if (data.pages > 0 && pagination.page > data.pages) {
      pagination.page = data.pages;
      ({ data } = await fetchRequestsPage());
      if (currentLoadSequence !== loadSequence) return;
    }

    requests.value = data.items;
    pagination.total = data.total;
    pagination.page = data.pages === 0 ? 1 : data.page;
    pagination.page_size = data.page_size;
    clearSelection();
    if (selected.value) {
      selected.value =
        requests.value.find((item) => item.id === selected.value?.id) || null;
    }
  } catch (error) {
    if (currentLoadSequence !== loadSequence) return;
    appStore.showError(extractApiErrorMessage(error, "发票申请加载失败"));
  } finally {
    if (currentLoadSequence === loadSequence) {
      loading.value = false;
    }
  }
}

function fetchRequestsPage() {
  return adminInvoicesAPI.list({
    page: pagination.page,
    page_size: pagination.page_size,
    status: filters.status || undefined,
    keyword: filters.keyword || undefined,
  });
}

function handleFilterChange(): void {
  pagination.page = 1;
  clearSelection();
  void loadRequests();
}

function handleSearch(): void {
  pagination.page = 1;
  clearSelection();
  void loadRequests();
}

function handlePageChange(page: number): void {
  pagination.page = page;
  clearSelection();
  void loadRequests();
}

function handlePageSizeChange(pageSize: number): void {
  pagination.page = 1;
  pagination.page_size = pageSize;
  clearSelection();
  void loadRequests();
}

async function selectRequest(request: InvoiceRequest): Promise<void> {
  try {
    const { data } = await adminInvoicesAPI.get(request.id);
    selected.value = data;
    issueForm.admin_note = data.admin_note || "";
  } catch (error) {
    appStore.showError(extractApiErrorMessage(error, "发票详情加载失败"));
  }
}

async function issueSelected(): Promise<void> {
  if (!selected.value) return;
  processing.value = true;
  try {
    const { data } = await adminInvoicesAPI.issue(selected.value.id, {
      ...issueForm,
    });
    selected.value = data;
    appStore.showSuccess("发票状态已更新");
    await loadRequests();
  } catch (error) {
    appStore.showError(extractApiErrorMessage(error, "开票处理失败"));
  } finally {
    processing.value = false;
  }
}

async function rejectSelected(): Promise<void> {
  const target = rejectTarget.value;
  const reason = rejectReason.value.trim();
  if (!target) return;
  if (!reason) {
    appStore.showError("请填写驳回原因");
    return;
  }
  processing.value = true;
  try {
    const { data } = await adminInvoicesAPI.reject(target.id, {
      reason,
      admin_note: issueForm.admin_note,
    });
    if (selected.value?.id === target.id) selected.value = data;
    rejectTarget.value = null;
    rejectReason.value = "";
    appStore.showSuccess("发票申请已驳回");
    await loadRequests();
  } catch (error) {
    appStore.showError(extractApiErrorMessage(error, "驳回发票失败"));
  } finally {
    processing.value = false;
  }
}

function openRejectDialog(): void {
  if (!selected.value || selected.value.status !== "pending") return;
  rejectReason.value = "";
  rejectTarget.value = selected.value;
}

function closeRejectDialog(): void {
  if (processing.value) return;
  rejectTarget.value = null;
  rejectReason.value = "";
}

function toggleVisibleSelection(event: Event): void {
  const target = event.target;
  if (!(target instanceof HTMLInputElement)) return;
  toggleVisible(target.checked);
}

function textCell(value: string): CellObject {
  return { t: "s", v: value };
}

function numberCell(value: number): CellObject {
  return { t: "n", v: value };
}

function exportFileName(now: Date = new Date()): string {
  const pad = (value: number) => String(value).padStart(2, "0");
  const date = `${now.getFullYear()}${pad(now.getMonth() + 1)}${pad(now.getDate())}`;
  const time = `${pad(now.getHours())}${pad(now.getMinutes())}${pad(now.getSeconds())}`;
  return `批量开票-${date}-${time}.xls`;
}

async function exportSelected(): Promise<void> {
  if (exporting.value) return;
  const exportItems = selectedRequests.value;
  if (exportItems.length === 0) {
    appStore.showError("请选择待处理的发票申请");
    return;
  }

  exporting.value = true;
  try {
    const XLSX = await import("xlsx");
    const rows: CellObject[][] = [
      invoiceExportHeaders.map((header) => textCell(header)),
      ...exportItems.map((request, index) => [
        numberCell(index + 1),
        textCell(
          request.invoice_type === "enterprise_special" ? "专票" : "普票",
        ),
        textCell(request.title_name),
        textCell(request.tax_id),
        textCell(invoiceExportItemName),
        numberCell(request.amount),
        textCell(request.recipient_email),
        textCell(request.bank_name),
        textCell(request.bank_account),
        textCell(request.remark),
      ]),
    ];
    const worksheet = XLSX.utils.aoa_to_sheet(rows);
    worksheet["!cols"] = [
      { wch: 8 },
      { wch: 12 },
      { wch: 38 },
      { wch: 24 },
      { wch: 20 },
      { wch: 14 },
      { wch: 28 },
      { wch: 28 },
      { wch: 26 },
      { wch: 40 },
    ];

    const workbook = XLSX.utils.book_new();
    XLSX.utils.book_append_sheet(workbook, worksheet, "Sheet1");
    const output = XLSX.write(workbook, {
      type: "array",
      bookType: "biff8",
      bookSST: true,
    });
    saveAs(
      new Blob([output], { type: "application/vnd.ms-excel" }),
      exportFileName(),
    );
    appStore.showSuccess(`已导出 ${exportItems.length} 条发票申请`);
  } catch (error) {
    const message = error instanceof Error ? error.message : "批量导出失败";
    appStore.showError(message);
  } finally {
    exporting.value = false;
  }
}

function invoiceTypeLabel(type: InvoiceType): string {
  if (type === "enterprise_special") return "企业专票";
  if (type === "enterprise_normal") return "企业普票";
  return "个人普票";
}

function statusLabel(status: string): string {
  const labels: Record<string, string> = {
    pending: "待处理",
    issued: "已开票",
    rejected: "已驳回",
    cancelled: "已取消",
  };
  return labels[status] || status;
}

function formatMoney(value: number): string {
  return formatCurrency(value || 0, "CNY");
}
</script>

<style scoped>
.invoice-pagination :deep(button),
.invoice-pagination :deep(.select-trigger) {
  @apply min-h-11;
}
</style>
