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
        <div class="flex flex-wrap gap-2">
          <select
            v-model="filters.status"
            class="input w-36"
            @change="loadRequests"
          >
            <option value="">全部状态</option>
            <option value="pending">待处理</option>
            <option value="issued">已开票</option>
            <option value="rejected">已驳回</option>
            <option value="cancelled">已取消</option>
          </select>
          <input
            v-model.trim="filters.keyword"
            class="input w-56"
            type="search"
            placeholder="申请号 / 用户 / 抬头"
            @keyup.enter="loadRequests"
          />
          <button
            class="btn btn-secondary"
            type="button"
            @click="loadRequests"
            :disabled="loading"
          >
            查询
          </button>
        </div>
      </div>

      <div class="grid gap-5 xl:grid-cols-[minmax(0,1fr)_420px]">
        <section class="card overflow-hidden">
          <div class="overflow-x-auto">
            <table
              class="min-w-full divide-y divide-gray-100 text-sm dark:divide-dark-700"
            >
              <thead
                class="bg-gray-50 text-left text-xs uppercase text-gray-500 dark:bg-dark-800 dark:text-gray-400"
              >
                <tr>
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
                    colspan="7"
                    class="px-4 py-8 text-center text-sm text-gray-500 dark:text-gray-400"
                  >
                    暂无发票申请
                  </td>
                </tr>
              </tbody>
            </table>
          </div>
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
                <label class="input-label">发票号码</label>
                <input
                  v-model.trim="issueForm.invoice_number"
                  class="input"
                  type="text"
                />
              </div>
              <div>
                <label class="input-label">发票代码</label>
                <input
                  v-model.trim="issueForm.invoice_code"
                  class="input"
                  type="text"
                />
              </div>
              <div>
                <label class="input-label">发票文件链接</label>
                <input
                  v-model.trim="issueForm.invoice_file_url"
                  class="input"
                  type="url"
                />
              </div>
              <div>
                <label class="input-label">文件名称</label>
                <input
                  v-model.trim="issueForm.invoice_file_name"
                  class="input"
                  type="text"
                />
              </div>
              <div>
                <label class="input-label">处理备注</label>
                <textarea
                  v-model.trim="issueForm.admin_note"
                  class="input min-h-20"
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
                  @click="rejectSelected"
                  :disabled="processing"
                >
                  驳回
                </button>
              </div>
              <textarea
                v-model.trim="rejectReason"
                class="input min-h-20"
                placeholder="驳回原因"
              ></textarea>
            </div>

            <div
              v-else
              class="mt-5 rounded border border-gray-100 p-3 text-sm dark:border-dark-700"
            >
              <p v-if="selected.invoice_number">
                <span class="text-gray-500 dark:text-gray-400">发票号码：</span
                >{{ selected.invoice_number }}
              </p>
              <p v-if="selected.invoice_code">
                <span class="text-gray-500 dark:text-gray-400">发票代码：</span
                >{{ selected.invoice_code }}
              </p>
              <p v-if="selected.rejected_reason">
                <span class="text-gray-500 dark:text-gray-400">驳回原因：</span
                >{{ selected.rejected_reason }}
              </p>
              <a
                v-if="selected.invoice_file_url"
                class="text-primary-600 hover:underline dark:text-primary-400"
                :href="selected.invoice_file_url"
                target="_blank"
                rel="noreferrer"
              >
                打开发票文件
              </a>
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
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import { onMounted, reactive, ref } from "vue";
import AppLayout from "@/components/layout/AppLayout.vue";
import adminInvoicesAPI from "@/api/admin/invoices";
import { useAppStore } from "@/stores/app";
import type { InvoiceRequest, InvoiceType } from "@/types";
import { extractApiErrorMessage } from "@/utils/apiError";
import { formatCurrency, formatDateTime } from "@/utils/format";

const appStore = useAppStore();
const loading = ref(false);
const processing = ref(false);
const requests = ref<InvoiceRequest[]>([]);
const selected = ref<InvoiceRequest | null>(null);
const rejectReason = ref("");
const filters = reactive({
  status: "",
  keyword: "",
});
const issueForm = reactive({
  invoice_number: "",
  invoice_code: "",
  invoice_file_url: "",
  invoice_file_name: "",
  admin_note: "",
});

onMounted(() => {
  void loadRequests();
});

async function loadRequests(): Promise<void> {
  loading.value = true;
  try {
    const { data } = await adminInvoicesAPI.list({
      page: 1,
      page_size: 50,
      status: filters.status || undefined,
      keyword: filters.keyword || undefined,
    });
    requests.value = data.items;
    if (selected.value) {
      selected.value =
        requests.value.find((item) => item.id === selected.value?.id) || null;
    }
  } catch (error) {
    appStore.showError(extractApiErrorMessage(error, "发票申请加载失败"));
  } finally {
    loading.value = false;
  }
}

async function selectRequest(request: InvoiceRequest): Promise<void> {
  try {
    const { data } = await adminInvoicesAPI.get(request.id);
    selected.value = data;
    issueForm.invoice_number = data.invoice_number || "";
    issueForm.invoice_code = data.invoice_code || "";
    issueForm.invoice_file_url = data.invoice_file_url || "";
    issueForm.invoice_file_name = data.invoice_file_name || "";
    issueForm.admin_note = data.admin_note || "";
    rejectReason.value = data.rejected_reason || "";
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
  if (!selected.value) return;
  processing.value = true;
  try {
    const { data } = await adminInvoicesAPI.reject(selected.value.id, {
      reason: rejectReason.value,
      admin_note: issueForm.admin_note,
    });
    selected.value = data;
    appStore.showSuccess("发票申请已驳回");
    await loadRequests();
  } catch (error) {
    appStore.showError(extractApiErrorMessage(error, "驳回发票失败"));
  } finally {
    processing.value = false;
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
