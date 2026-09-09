<template>
  <BaseDialog :show="show" :title="dialogTitle" width="wide" :close-disabled="saving" @close="close">
    <div v-if="view === 'list'" class="space-y-4">
      <div class="space-y-1 text-sm text-gray-600 dark:text-gray-400">
        <p>{{ t('userAccounts.proxyManagerDescription') }}</p>
        <p>{{ t('userAccounts.proxyManagerDeleteRule') }}</p>
      </div>
      <div class="flex flex-wrap justify-end gap-2">
        <button type="button" class="btn btn-secondary min-h-11" :disabled="loading || saving" @click="loadProxies">
          {{ t('common.refresh') }}
        </button>
        <button type="button" class="btn btn-primary min-h-11" :disabled="saving" @click="openForm()">
          {{ t('userAccounts.proxyDialogTitle') }}
        </button>
      </div>
      <p v-if="loading" class="py-8 text-center text-sm text-gray-500" role="status">{{ t('common.loading') }}</p>
      <p v-else-if="loadError" class="rounded-lg bg-red-50 p-4 text-sm text-red-700 dark:bg-red-900/20 dark:text-red-300" role="alert">{{ loadError }}</p>
      <p v-else-if="proxies.length === 0" class="py-8 text-center text-sm text-gray-500">{{ t('userAccounts.proxyManagerEmpty') }}</p>
      <ul v-else class="space-y-3">
        <li v-for="proxy in proxies" :key="proxy.id" class="rounded-xl border border-gray-200 p-4 dark:border-dark-600">
          <div class="flex flex-col justify-between gap-3 sm:flex-row sm:items-start">
            <div class="min-w-0 space-y-1">
              <div class="flex flex-wrap items-center gap-2">
                <span class="break-all font-medium text-gray-900 dark:text-gray-100">{{ proxy.name }}</span>
                <span :class="['badge', proxy.status === 'active' ? 'badge-success' : 'badge-gray']">
                  {{ t(`admin.accounts.status.${proxy.status}`) }}
                </span>
              </div>
              <p class="break-all text-sm text-gray-500 dark:text-gray-400">{{ proxy.protocol }}://{{ proxy.host }}:{{ proxy.port }}</p>
              <p class="text-sm text-gray-500 dark:text-gray-400">{{ proxyUsage(proxy) }}</p>
            </div>
            <div class="flex shrink-0 flex-wrap gap-2">
              <button type="button" class="btn btn-secondary min-h-11" :disabled="saving" @click="openForm(proxy)">{{ t('common.edit') }}</button>
              <button type="button" class="btn btn-secondary min-h-11" :disabled="saving" @click="toggleStatus(proxy)">
                {{ proxy.status === 'active' ? t('userAccounts.disable') : t('userAccounts.enable') }}
              </button>
              <button
                type="button"
                class="btn btn-danger min-h-11"
                :disabled="saving || proxy.account_count > 0"
                :title="proxy.account_count > 0 ? t('userAccounts.proxyDeleteBlocked') : undefined"
                @click="openDelete(proxy)"
              >{{ t('common.delete') }}</button>
            </div>
          </div>
        </li>
      </ul>
    </div>

    <form v-else-if="view === 'form'" id="user-proxy-form" class="space-y-4" @submit.prevent="saveProxy">
      <fieldset :disabled="saving" class="space-y-4">
        <div>
          <label for="user-proxy-name" class="input-label">{{ t('userAccounts.proxyName') }}</label>
          <input id="user-proxy-name" v-model="form.name" class="input min-h-11" required maxlength="100" :placeholder="t('userAccounts.proxyNamePlaceholder')" />
        </div>
        <div class="grid grid-cols-1 gap-4 sm:grid-cols-3">
          <div>
            <label for="user-proxy-protocol" class="input-label">{{ t('admin.proxies.columns.protocol') }}</label>
            <select id="user-proxy-protocol" v-model="form.protocol" class="input min-h-11" required>
              <option v-for="protocol in protocols" :key="protocol" :value="protocol">{{ protocol.toUpperCase() }}</option>
            </select>
          </div>
          <div>
            <label for="user-proxy-host" class="input-label">{{ t('userAccounts.proxyHost') }}</label>
            <input id="user-proxy-host" v-model="form.host" class="input min-h-11" required maxlength="255" autocomplete="off" />
          </div>
          <div>
            <label for="user-proxy-port" class="input-label">{{ t('userAccounts.proxyPort') }}</label>
            <input id="user-proxy-port" v-model.number="form.port" type="number" class="input min-h-11" required min="1" max="65535" step="1" />
          </div>
        </div>
        <div class="grid grid-cols-1 gap-4 sm:grid-cols-2">
          <div>
            <label for="user-proxy-username" class="input-label">{{ t('admin.proxies.columns.usernameLabel') }}</label>
            <input id="user-proxy-username" v-model="form.username" class="input min-h-11" autocomplete="off" />
          </div>
          <div>
            <label for="user-proxy-password" class="input-label">{{ t('admin.proxies.columns.passwordLabel') }}</label>
            <input
              id="user-proxy-password"
              v-model="form.password"
              type="password"
              class="input min-h-11"
              :disabled="clearPassword"
              autocomplete="new-password"
              :placeholder="editingProxy ? t('userAccounts.proxyPasswordKeepPlaceholder') : t('userAccounts.proxyPasswordPlaceholder')"
            />
            <template v-if="editingProxy">
              <p class="input-hint">{{ t('userAccounts.proxyPasswordKeepHint') }}</p>
              <label class="flex min-h-11 cursor-pointer items-center gap-2 text-sm text-gray-700 dark:text-gray-300">
                <input v-model="clearPassword" type="checkbox" class="rounded border-gray-300 text-primary-600" />
                {{ t('userAccounts.proxyClearPassword') }}
              </label>
            </template>
          </div>
        </div>
        <div>
          <label for="user-proxy-max-accounts" class="input-label">{{ t('admin.proxies.maxAccounts') }}</label>
          <input id="user-proxy-max-accounts" v-model.number="form.max_accounts" type="number" class="input min-h-11" required min="0" step="1" />
          <p class="input-hint">{{ t('admin.proxies.maxAccountsHint') }}</p>
        </div>
        <p v-if="editingProxy" class="text-sm text-gray-500 dark:text-gray-400">{{ t('userAccounts.proxyManagerDescription') }}</p>
      </fieldset>
    </form>

    <p v-else-if="deletingProxy" class="text-sm text-gray-700 dark:text-gray-300">{{ t('userAccounts.proxyDeleteConfirm', { name: deletingProxy.name }) }}</p>

    <template #footer>
      <div class="flex flex-wrap justify-end gap-3">
        <button type="button" class="btn btn-secondary min-h-11" :disabled="saving" @click="view === 'list' ? close() : showList()">
          {{ view === 'list' ? t('common.close') : t('common.cancel') }}
        </button>
        <button v-if="view === 'form'" type="submit" form="user-proxy-form" class="btn btn-primary min-h-11" :disabled="saving">
          {{ saving ? t('common.saving') : t('common.save') }}
        </button>
        <button v-if="view === 'delete'" type="button" class="btn btn-danger min-h-11" :disabled="saving" @click="deleteProxy">
          {{ saving ? t('common.loading') : t('common.delete') }}
        </button>
      </div>
    </template>
  </BaseDialog>
</template>

<script setup lang="ts">
import { computed, reactive, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { accountShareAPI } from '@/api'
import type { CreateUserProxyRequest, UserManagedProxy } from '@/api/accountShare'
import BaseDialog from '@/components/common/BaseDialog.vue'
import { useAppStore } from '@/stores/app'
import { extractApiErrorMessage } from '@/utils/apiError'
import type { ProxyProtocol } from '@/types'

const props = defineProps<{ show: boolean }>()
const emit = defineEmits<{ close: []; changed: [] }>()
const { t } = useI18n()
const appStore = useAppStore()
const protocols: ProxyProtocol[] = ['http', 'https', 'socks5', 'socks5h']
const view = ref<'list' | 'form' | 'delete'>('list')
const proxies = ref<UserManagedProxy[]>([])
const loading = ref(false)
const saving = ref(false)
const loadError = ref('')
const editingProxy = ref<UserManagedProxy | null>(null)
const deletingProxy = ref<UserManagedProxy | null>(null)
const clearPassword = ref(false)
const form = reactive({ name: '', protocol: 'http' as ProxyProtocol, host: '', port: 8080, username: '', password: '', max_accounts: 0 })
let loadSequence = 0

const dialogTitle = computed(() => t(view.value === 'form'
  ? editingProxy.value ? 'userAccounts.proxyEditTitle' : 'userAccounts.proxyDialogTitle'
  : view.value === 'delete' ? 'userAccounts.proxyDeleteTitle' : 'userAccounts.proxyManagerTitle'))

function proxyUsage(proxy: UserManagedProxy): string {
  return proxy.max_accounts > 0
    ? t('admin.proxies.accountUsageLimited', { count: proxy.account_count, max: proxy.max_accounts })
    : t('admin.proxies.accountUsageUnlimited', { count: proxy.account_count })
}

async function loadProxies(): Promise<void> {
  const sequence = ++loadSequence
  loading.value = true
  loadError.value = ''
  try {
    const result = await accountShareAPI.listMyProxies()
    if (sequence === loadSequence) proxies.value = result
  } catch (error) {
    if (sequence !== loadSequence) return
    proxies.value = []
    loadError.value = extractApiErrorMessage(error, t('userAccounts.importProxyLoadFailed'))
  } finally {
    if (sequence === loadSequence) loading.value = false
  }
}

function showList(): void {
  view.value = 'list'
  editingProxy.value = null
  deletingProxy.value = null
  form.password = ''
  clearPassword.value = false
}

function openForm(proxy?: UserManagedProxy): void {
  editingProxy.value = proxy || null
  Object.assign(form, {
    name: proxy?.name || '', protocol: proxy?.protocol || 'http', host: proxy?.host || '',
    port: proxy?.port || 8080, username: proxy?.username || '', password: '', max_accounts: proxy?.max_accounts || 0
  })
  clearPassword.value = false
  view.value = 'form'
}

function openDelete(proxy: UserManagedProxy): void {
  if (proxy.account_count > 0) return
  deletingProxy.value = proxy
  view.value = 'delete'
}

async function saveProxy(): Promise<void> {
  if (saving.value) return
  const name = form.name.trim()
  const host = form.host.trim()
  const validationError = !name ? 'userAccounts.proxyNameRequired'
    : !host ? 'userAccounts.proxyHostRequired'
      : /\s/.test(host) ? 'userAccounts.proxyHostNoSpaces'
        : !Number.isInteger(form.port) || form.port < 1 || form.port > 65535 ? 'userAccounts.proxyPortInvalid'
          : !Number.isInteger(form.max_accounts) || form.max_accounts < 0 ? 'admin.proxies.maxAccountsInvalid'
            : ''
  if (validationError) {
    appStore.showError(t(validationError))
    return
  }
  const payload: CreateUserProxyRequest = {
    name, protocol: form.protocol, host, port: form.port, username: form.username, max_accounts: form.max_accounts
  }
  // 编辑时不发送空密码，只有明确勾选清除才发送空字符串。
  if (!editingProxy.value || form.password || clearPassword.value) payload.password = clearPassword.value ? '' : form.password
  saving.value = true
  try {
    if (editingProxy.value) {
      await accountShareAPI.updateProxy(editingProxy.value.id, payload)
      appStore.showSuccess(t('userAccounts.proxyUpdatedSuccess'))
    } else {
      await accountShareAPI.createProxy(payload)
      appStore.showSuccess(t('userAccounts.proxyAddedSuccess'))
    }
    emit('changed')
    showList()
    await loadProxies()
  } catch (error) {
    appStore.showError(extractApiErrorMessage(error, t(editingProxy.value ? 'userAccounts.proxyUpdateFailed' : 'userAccounts.proxyCreateFailed')))
  } finally {
    saving.value = false
  }
}

async function toggleStatus(proxy: UserManagedProxy): Promise<void> {
  if (saving.value) return
  saving.value = true
  try {
    await accountShareAPI.updateProxy(proxy.id, { status: proxy.status === 'active' ? 'inactive' : 'active' })
    appStore.showSuccess(t('userAccounts.proxyUpdatedSuccess'))
    emit('changed')
    await loadProxies()
  } catch (error) {
    appStore.showError(extractApiErrorMessage(error, t('userAccounts.proxyUpdateFailed')))
  } finally {
    saving.value = false
  }
}

async function deleteProxy(): Promise<void> {
  if (!deletingProxy.value || saving.value) return
  saving.value = true
  try {
    await accountShareAPI.deleteProxy(deletingProxy.value.id)
    appStore.showSuccess(t('userAccounts.proxyDeletedSuccess'))
    emit('changed')
    showList()
    await loadProxies()
  } catch (error) {
    appStore.showError(extractApiErrorMessage(error, t('userAccounts.proxyDeleteFailed')))
  } finally {
    saving.value = false
  }
}

function close(): void {
  if (!saving.value) emit('close')
}

watch(() => props.show, (show) => {
  showList()
  if (show) void loadProxies()
  else {
    loadSequence++
    proxies.value = []
    loading.value = false
  }
}, { immediate: true })
</script>
