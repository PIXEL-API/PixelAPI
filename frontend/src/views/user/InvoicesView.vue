<template>
  <AppLayout>
    <div class="space-y-5">
      <div
        class="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between"
      >
        <div>
          <h1 class="text-2xl font-semibold text-gray-900 dark:text-white">
            {{ t('invoices.title') }}
          </h1>
          <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">
            {{ t('invoices.subtitle') }}
          </p>
        </div>
        <button
          class="btn btn-secondary"
          type="button"
          @click="reloadAll"
          :disabled="loading"
        >
          {{ t('invoices.refresh') }}
        </button>
      </div>

      <div class="grid gap-5 xl:grid-cols-[minmax(0,420px)_minmax(0,1fr)]">
        <section class="card p-5">
          <div class="mb-4 flex items-center justify-between">
            <h2 class="text-lg font-semibold text-gray-900 dark:text-white">
              {{ t('invoices.profiles') }}
            </h2>
            <button
              class="btn btn-sm btn-secondary"
              type="button"
              @click="resetForm"
            >
              {{ t('invoices.newProfile') }}
            </button>
          </div>

          <div class="space-y-4">
            <div>
              <label class="input-label">{{ t('invoices.type') }}</label>
              <select v-model="form.invoice_type" class="input">
                <option value="personal_normal">{{ t('invoices.typePersonal') }}</option>
                <option value="enterprise_normal">{{ t('invoices.typeEntNormal') }}</option>
                <option value="enterprise_special">{{ t('invoices.typeEntSpecial') }}</option>
              </select>
            </div>

            <div>
              <label class="input-label">{{
                form.invoice_type === "personal_normal"
                  ? t('invoices.personalName')
                  : t('invoices.entName')
              }}</label>
              <input v-model.trim="form.title_name" class="input" type="text" />
            </div>

            <div v-if="isEnterprise">
              <label class="input-label">{{ t('invoices.taxId') }}</label>
              <input v-model.trim="form.tax_id" class="input" type="text" />
            </div>

            <template v-if="isSpecial">
              <div>
                <label class="input-label">{{ t('invoices.registeredAddress') }}</label>
                <input
                  v-model.trim="form.registered_address"
                  class="input"
                  type="text"
                />
              </div>
              <div>
                <label class="input-label">{{ t('invoices.registeredPhone') }}</label>
                <input
                  v-model.trim="form.registered_phone"
                  class="input"
                  type="text"
                />
              </div>
              <div>
                <label class="input-label">{{ t('invoices.bankName') }}</label>
                <input
                  v-model.trim="form.bank_name"
                  class="input"
                  type="text"
                />
              </div>
              <div>
                <label class="input-label">{{ t('invoices.bankAccount') }}</label>
                <input
                  v-model.trim="form.bank_account"
                  class="input"
                  type="text"
                />
              </div>
            </template>

            <div>
              <label class="input-label">{{ t('invoices.receiptEmail') }}</label>
              <input
                v-model.trim="form.recipient_email"
                class="input"
                type="email"
              />
            </div>

            <div>
              <label class="input-label">{{ t('invoices.receiptPhone') }}</label>
              <input
                v-model.trim="form.recipient_phone"
                class="input"
                type="text"
              />
            </div>

            <div>
              <label class="input-label">{{ t('invoices.remark') }}</label>
              <textarea
                v-model.trim="form.remark"
                class="input min-h-20"
                :placeholder="t('invoices.batchRemarkPlaceholder')"
              ></textarea>
            </div>

            <label
              class="flex items-center gap-2 text-sm text-gray-700 dark:text-gray-300"
            >
              <input
                v-model="form.is_default"
                type="checkbox"
                class="rounded border-gray-300 text-primary-600"
              />
              {{ t('invoices.setDefault') }}
            </label>

            <div class="flex gap-2">
              <button
                class="btn btn-primary flex-1"
                type="button"
                @click="saveProfile"
                :disabled="savingProfile"
              >
                {{ editingProfileId ? t('invoices.saveProfile') : t('invoices.addProfile') }}
              </button>
              <button
                class="btn btn-secondary flex-1"
                type="button"
                @click="submitInvoice"
                :disabled="submitting || selectedRefs.length === 0"
              >
                {{ t('invoices.submit') }}
              </button>
            </div>
          </div>
        </section>

        <section class="space-y-5">
          <div class="card overflow-hidden">
            <div
              class="border-b border-gray-100 px-5 py-4 dark:border-dark-700"
            >
              <h2 class="text-lg font-semibold text-gray-900 dark:text-white">
                {{ t('invoices.availableSources') }}
              </h2>
              <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">
                {{ t('invoices.selectedSummary', { length: selectedRefs.length, selectedAmount: formatMoney(selectedAmount) }) }}
              </p>
            </div>
            <div class="overflow-x-auto">
              <table
                class="min-w-full divide-y divide-gray-100 text-sm dark:divide-dark-700"
              >
                <thead
                  class="bg-gray-50 text-left text-xs uppercase text-gray-500 dark:bg-dark-800 dark:text-gray-400"
                >
                  <tr>
                    <th class="w-12 px-4 py-3"></th>
                    <th class="px-4 py-3">{{ t('invoices.colSource') }}</th>
                    <th class="px-4 py-3">{{ t('invoices.colId') }}</th>
                    <th class="px-4 py-3 text-right">{{ t('invoices.colAvailable') }}</th>
                    <th class="px-4 py-3">{{ t('invoices.colTime') }}</th>
                  </tr>
                </thead>
                <tbody class="divide-y divide-gray-100 dark:divide-dark-700">
                  <tr
                    v-for="source in sources"
                    :key="sourceKey(source)"
                    class="text-gray-700 dark:text-gray-300"
                  >
                    <td class="px-4 py-3">
                      <input
                        v-model="selectedSourceKeys"
                        :value="sourceKey(source)"
                        type="checkbox"
                        class="rounded border-gray-300 text-primary-600"
                      />
                    </td>
                    <td class="px-4 py-3">{{ source.source_label }}</td>
                    <td class="px-4 py-3 font-mono text-xs">
                      {{ source.source_no }}
                    </td>
                    <td
                      class="px-4 py-3 text-right font-medium text-gray-900 dark:text-white"
                    >
                      {{ formatMoney(source.invoice_amount) }}
                    </td>
                    <td class="px-4 py-3">
                      {{ formatDateTime(source.occurred_at) }}
                    </td>
                  </tr>
                  <tr v-if="!sources.length">
                    <td
                      colspan="5"
                      class="px-4 py-8 text-center text-sm text-gray-500 dark:text-gray-400"
                    >
                      {{ t('invoices.noSources') }}
                    </td>
                  </tr>
                </tbody>
              </table>
            </div>
          </div>

          <div class="card overflow-hidden">
            <div
              class="border-b border-gray-100 px-5 py-4 dark:border-dark-700"
            >
              <h2 class="text-lg font-semibold text-gray-900 dark:text-white">
                {{ t('invoices.savedProfiles') }}
              </h2>
            </div>
            <div class="divide-y divide-gray-100 dark:divide-dark-700">
              <div
                v-for="profile in profiles"
                :key="profile.id"
                class="flex flex-col gap-3 px-5 py-4 md:flex-row md:items-center md:justify-between"
              >
                <div class="min-w-0">
                  <div class="flex items-center gap-2">
                    <span class="font-medium text-gray-900 dark:text-white">{{
                      profile.title_name
                    }}</span>
                    <span
                      v-if="profile.is_default"
                      class="rounded bg-primary-50 px-2 py-0.5 text-xs text-primary-600 dark:bg-primary-900/30 dark:text-primary-300"
                      >{{ t('invoices.defaultProfile') }}</span
                    >
                  </div>
                  <p
                    class="mt-1 truncate text-sm text-gray-500 dark:text-gray-400"
                  >
                    {{ invoiceTypeLabel(profile.invoice_type) }} ·
                    {{ profile.recipient_email }}
                  </p>
                </div>
                <div class="flex shrink-0 gap-2">
                  <button
                    class="btn btn-sm btn-secondary"
                    type="button"
                    @click="applyProfile(profile)"
                  >
                    {{ t('invoices.applyProfile') }}
                  </button>
                  <button
                    class="btn btn-sm btn-secondary"
                    type="button"
                    @click="editProfile(profile)"
                  >
                    {{ t('common.edit') }}
                  </button>
                  <button
                    v-if="!profile.is_default"
                    class="btn btn-sm btn-secondary"
                    type="button"
                    @click="setDefault(profile.id)"
                  >
                    {{ t('invoices.defaultProfile') }}
                  </button>
                  <button
                    class="btn btn-sm btn-danger"
                    type="button"
                    @click="deleteProfile(profile.id)"
                  >
                    {{ t('common.delete') }}
                  </button>
                </div>
              </div>
              <div
                v-if="!profiles.length"
                class="px-5 py-8 text-center text-sm text-gray-500 dark:text-gray-400"
              >
                {{ t('invoices.noProfiles') }}
              </div>
            </div>
          </div>
        </section>
      </div>

      <section class="card overflow-hidden">
        <div class="border-b border-gray-100 px-5 py-4 dark:border-dark-700">
          <h2 class="text-lg font-semibold text-gray-900 dark:text-white">
            {{ t('invoices.requests') }}
          </h2>
        </div>
        <div class="overflow-x-auto">
          <table
            class="min-w-full divide-y divide-gray-100 text-sm dark:divide-dark-700"
          >
            <thead
              class="bg-gray-50 text-left text-xs uppercase text-gray-500 dark:bg-dark-800 dark:text-gray-400"
            >
              <tr>
                <th class="px-4 py-3">{{ t('invoices.colRequestNo') }}</th>
                <th class="px-4 py-3">{{ t('invoices.colTitle') }}</th>
                <th class="px-4 py-3">{{ t('invoices.colType') }}</th>
                <th class="px-4 py-3 text-right">{{ t('invoices.colAmount') }}</th>
                <th class="px-4 py-3">{{ t('invoices.colStatus') }}</th>
                <th class="px-4 py-3">{{ t('invoices.colCreatedAt') }}</th>
                <th class="px-4 py-3 text-right">{{ t('invoices.colActions') }}</th>
              </tr>
            </thead>
            <tbody class="divide-y divide-gray-100 dark:divide-dark-700">
              <tr
                v-for="request in requests"
                :key="request.id"
                class="text-gray-700 dark:text-gray-300"
              >
                <td class="px-4 py-3 font-mono text-xs">
                  {{ request.request_no }}
                </td>
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
                <td class="px-4 py-3 text-right">
                  <button
                    v-if="request.status === 'pending'"
                    class="btn btn-sm btn-secondary"
                    type="button"
                    @click="cancelRequest(request.id)"
                  >
                    {{ t('common.cancel') }}
                  </button>
                  <span
                    v-else-if="request.status === 'rejected'"
                    class="inline-block max-w-64 whitespace-normal break-words text-red-600 dark:text-red-400"
                  >
                    {{ request.rejected_reason || "-" }}
                  </span>
                  <span v-else>-</span>
                </td>
              </tr>
              <tr v-if="!requests.length">
                <td
                  colspan="7"
                  class="px-4 py-8 text-center text-sm text-gray-500 dark:text-gray-400"
                >
                  {{ t('invoices.noRequests') }}
                </td>
              </tr>
            </tbody>
          </table>
        </div>
      </section>
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref } from "vue";
import AppLayout from "@/components/layout/AppLayout.vue";
import invoicesAPI from "@/api/invoices";
import { useAppStore } from "@/stores/app";
import type {
  InvoiceEligibleSource,
  InvoiceProfile,
  InvoiceProfileInput,
  InvoiceRequest,
  InvoiceRequestInput,
  InvoiceType,
} from "@/types";
import { extractApiErrorMessage } from "@/utils/apiError";
import { formatCurrency, formatDateTime } from "@/utils/format";
import { useI18n } from 'vue-i18n'

const { t } = useI18n()

const appStore = useAppStore();
const loading = ref(false);
const savingProfile = ref(false);
const submitting = ref(false);
const editingProfileId = ref<number | null>(null);
const profiles = ref<InvoiceProfile[]>([]);
const sources = ref<InvoiceEligibleSource[]>([]);
const requests = ref<InvoiceRequest[]>([]);
const selectedSourceKeys = ref<string[]>([]);

const form = reactive<InvoiceProfileInput>({
  invoice_type: "personal_normal",
  title_name: "",
  tax_id: "",
  registered_address: "",
  registered_phone: "",
  bank_name: "",
  bank_account: "",
  recipient_email: "",
  recipient_phone: "",
  remark: "",
  is_default: false,
});

const isEnterprise = computed(() => form.invoice_type !== "personal_normal");
const isSpecial = computed(() => form.invoice_type === "enterprise_special");
const selectedRefs = computed(() =>
  sources.value
    .filter((source) => selectedSourceKeys.value.includes(sourceKey(source)))
    .map((source) => ({
      source_type: source.source_type,
      source_id: source.source_id,
    })),
);
const selectedAmount = computed(() =>
  sources.value
    .filter((source) => selectedSourceKeys.value.includes(sourceKey(source)))
    .reduce((sum, source) => sum + source.invoice_amount, 0),
);

onMounted(() => {
  void reloadAll();
});

async function reloadAll(): Promise<void> {
  loading.value = true;
  try {
    const [profilesRes, sourcesRes, requestsRes] = await Promise.all([
      invoicesAPI.listProfiles(),
      invoicesAPI.listEligibleSources({ page: 1, page_size: 100 }),
      invoicesAPI.listRequests({ page: 1, page_size: 50 }),
    ]);
    profiles.value = profilesRes.data;
    sources.value = sourcesRes.data.items;
    requests.value = requestsRes.data.items;
    selectedSourceKeys.value = selectedSourceKeys.value.filter((key) =>
      sources.value.some((source) => sourceKey(source) === key),
    );
    const defaultProfile = profiles.value.find((profile) => profile.is_default);
    if (!editingProfileId.value && defaultProfile && !form.title_name) {
      applyProfile(defaultProfile);
    }
  } catch (error) {
    appStore.showError(extractApiErrorMessage(error, t('invoices.loadFailed')));
  } finally {
    loading.value = false;
  }
}

async function saveProfile(): Promise<void> {
  savingProfile.value = true;
  try {
    const payload = profilePayload();
    if (editingProfileId.value) {
      await invoicesAPI.updateProfile(editingProfileId.value, payload);
      appStore.showSuccess(t('invoices.profileUpdated'));
    } else {
      await invoicesAPI.createProfile(payload);
      appStore.showSuccess(t('invoices.profileAdded'));
    }
    resetForm();
    await reloadAll();
  } catch (error) {
    appStore.showError(extractApiErrorMessage(error, t('invoices.profileSaveFailed')));
  } finally {
    savingProfile.value = false;
  }
}

async function submitInvoice(): Promise<void> {
  if (selectedRefs.value.length === 0) {
    appStore.showError(t('invoices.selectSourceRequired'));
    return;
  }
  submitting.value = true;
  try {
    const invoiceFields = profilePayload();
    const payload: InvoiceRequestInput = {
      invoice_type: invoiceFields.invoice_type,
      title_name: invoiceFields.title_name,
      tax_id: invoiceFields.tax_id,
      registered_address: invoiceFields.registered_address,
      registered_phone: invoiceFields.registered_phone,
      bank_name: invoiceFields.bank_name,
      bank_account: invoiceFields.bank_account,
      recipient_email: invoiceFields.recipient_email,
      recipient_phone: invoiceFields.recipient_phone,
      remark: invoiceFields.remark,
      source_refs: selectedRefs.value,
    };
    await invoicesAPI.createRequest(payload);
    appStore.showSuccess(t('invoices.submitSuccess'));
    selectedSourceKeys.value = [];
    await reloadAll();
  } catch (error) {
    appStore.showError(extractApiErrorMessage(error, t('invoices.submitFailed')));
  } finally {
    submitting.value = false;
  }
}

async function setDefault(id: number): Promise<void> {
  try {
    await invoicesAPI.setDefaultProfile(id);
    appStore.showSuccess(t('invoices.defaultUpdated'));
    await reloadAll();
  } catch (error) {
    appStore.showError(extractApiErrorMessage(error, t('invoices.defaultFailed')));
  }
}

async function deleteProfile(id: number): Promise<void> {
  try {
    await invoicesAPI.deleteProfile(id);
    appStore.showSuccess(t('invoices.profileDeleted'));
    if (editingProfileId.value === id) resetForm();
    await reloadAll();
  } catch (error) {
    appStore.showError(extractApiErrorMessage(error, t('invoices.profileDeleteFailed')));
  }
}

async function cancelRequest(id: number): Promise<void> {
  try {
    await invoicesAPI.cancelRequest(id);
    appStore.showSuccess(t('invoices.cancelSuccess'));
    await reloadAll();
  } catch (error) {
    appStore.showError(extractApiErrorMessage(error, t('invoices.cancelFailed')));
  }
}

function applyProfile(profile: InvoiceProfile): void {
  form.invoice_type = profile.invoice_type;
  form.title_name = profile.title_name;
  form.tax_id = profile.tax_id;
  form.registered_address = profile.registered_address;
  form.registered_phone = profile.registered_phone;
  form.bank_name = profile.bank_name;
  form.bank_account = profile.bank_account;
  form.recipient_email = profile.recipient_email;
  form.recipient_phone = profile.recipient_phone;
  form.remark = profile.remark;
  form.is_default = profile.is_default;
}

function editProfile(profile: InvoiceProfile): void {
  editingProfileId.value = profile.id;
  applyProfile(profile);
}

function resetForm(): void {
  editingProfileId.value = null;
  form.invoice_type = "personal_normal";
  form.title_name = "";
  form.tax_id = "";
  form.registered_address = "";
  form.registered_phone = "";
  form.bank_name = "";
  form.bank_account = "";
  form.recipient_email = "";
  form.recipient_phone = "";
  form.remark = "";
  form.is_default = false;
}

function profilePayload(): InvoiceProfileInput {
  return {
    invoice_type: form.invoice_type,
    title_name: form.title_name.trim(),
    tax_id: isEnterprise.value ? (form.tax_id || "").trim() : "",
    registered_address: isSpecial.value
      ? (form.registered_address || "").trim()
      : "",
    registered_phone: isSpecial.value
      ? (form.registered_phone || "").trim()
      : "",
    bank_name: isSpecial.value ? (form.bank_name || "").trim() : "",
    bank_account: isSpecial.value ? (form.bank_account || "").trim() : "",
    recipient_email: form.recipient_email.trim(),
    recipient_phone: (form.recipient_phone || "").trim(),
    remark: (form.remark || "").trim(),
    is_default: form.is_default,
  };
}

function sourceKey(source: InvoiceEligibleSource): string {
  return `${source.source_type}:${source.source_id}`;
}

function invoiceTypeLabel(type: InvoiceType): string {
  if (type === "enterprise_special") return t('invoices.typeEntSpecial');
  if (type === "enterprise_normal") return t('invoices.typeEntNormal');
  return t('invoices.typePersonal');
}

function statusLabel(status: string): string {
  const labels: Record<string, string> = {
    pending: t('invoices.statusPending'),
    issued: t('invoices.statusIssued'),
    rejected: t('invoices.statusRejected'),
    cancelled: t('invoices.statusCancelled'),
  };
  return labels[status] || status;
}

function formatMoney(value: number): string {
  return formatCurrency(value || 0, "CNY");
}
</script>
