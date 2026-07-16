<template>
  <BaseDialog
    :show="show"
    :title="t('userAccounts.proxyManagerTitle')"
    width="wide"
    @close="requestClose"
  >
    <div class="space-y-5">
      <div class="flex flex-col gap-3 rounded-xl border border-gray-200 bg-gray-50 p-4 dark:border-dark-700 dark:bg-dark-900/50 sm:flex-row sm:items-center sm:justify-between">
        <div>
          <p class="font-medium text-gray-900 dark:text-white">{{ t('userAccounts.proxyManagerDescription') }}</p>
          <p class="mt-1 text-sm text-gray-500 dark:text-dark-300">{{ t('userAccounts.proxyManagerDeleteRule') }}</p>
        </div>
        <button type="button" class="btn btn-primary min-h-[44px]" @click="startCreate">
          <Icon name="plus" size="sm" class="mr-2" />
          {{ t('userAccounts.proxyActionAddTitle') }}
        </button>
      </div>

      <UserProxyQuickCreatePanel
        v-if="creating"
        @created="handleCreated"
        @cancel="creating = false"
      />

      <form
        v-if="editingProxy"
        class="space-y-4 rounded-xl border border-primary-200 bg-primary-50/40 p-4 dark:border-primary-500/30 dark:bg-primary-500/10"
        @submit.prevent="saveEdit"
      >
        <div class="flex items-center justify-between gap-3">
          <div>
            <h4 class="font-semibold text-gray-900 dark:text-white">{{ t('userAccounts.proxyEditTitle') }}</h4>
            <p class="mt-1 text-xs text-gray-500 dark:text-dark-300">{{ t('userAccounts.proxyPasswordKeepHint') }}</p>
          </div>
          <button type="button" class="min-h-[44px] min-w-[44px] rounded-lg p-2 text-gray-400 hover:bg-white dark:hover:bg-dark-800" :aria-label="t('common.cancel')" @click="cancelEdit">
            <Icon name="x" size="sm" />
          </button>
        </div>

        <div class="grid gap-4 sm:grid-cols-2">
          <label class="block sm:col-span-2">
            <span class="input-label">{{ t('userAccounts.proxyName') }}</span>
            <input v-model.trim="editForm.name" class="input" maxlength="100" required />
          </label>
          <label class="block">
            <span class="input-label">{{ t('userAccounts.proxyIpType') }}</span>
            <select v-model="editForm.protocol" class="input">
              <option value="socks5">SOCKS5</option>
              <option value="socks5h">SOCKS5H</option>
              <option value="http">HTTP</option>
              <option value="https">HTTPS</option>
            </select>
          </label>
          <label class="block">
            <span class="input-label">{{ t('userAccounts.proxyHost') }}</span>
            <input v-model.trim="editForm.host" class="input" required />
          </label>
          <label class="block">
            <span class="input-label">{{ t('userAccounts.proxyPort') }}</span>
            <input v-model.number="editForm.port" class="input" type="number" min="1" max="65535" required />
          </label>
          <label class="block">
            <span class="input-label">{{ t('userAccounts.proxyUsername') }}</span>
            <input v-model.trim="editForm.username" class="input" />
          </label>
          <label class="block sm:col-span-2">
            <span class="input-label">{{ t('userAccounts.proxyPassword') }}</span>
            <input v-model="editForm.password" class="input" type="password" :disabled="clearPassword" :placeholder="t('userAccounts.proxyPasswordKeepPlaceholder')" />
          </label>
          <label class="flex min-h-[44px] cursor-pointer items-center gap-3 rounded-lg border border-gray-200 bg-white px-3 py-2 text-sm text-gray-700 dark:border-dark-700 dark:bg-dark-800 dark:text-dark-200 sm:col-span-2">
            <input v-model="clearPassword" type="checkbox" class="h-4 w-4 rounded border-gray-300 text-primary-600 focus:ring-primary-500" />
            <span>{{ t('userAccounts.proxyClearPassword') }}</span>
          </label>
        </div>

        <div class="flex flex-wrap justify-end gap-3 border-t border-primary-200 pt-4 dark:border-primary-500/30">
          <button type="button" class="btn btn-secondary" :disabled="saving" @click="cancelEdit">{{ t('common.cancel') }}</button>
          <button type="submit" class="btn btn-primary" :disabled="saving">
            <Icon name="check" size="sm" class="mr-2" />
            {{ saving ? t('common.saving') : t('common.save') }}
          </button>
        </div>
      </form>

      <div v-if="loading" class="flex min-h-40 items-center justify-center text-gray-500 dark:text-dark-300">
        <Icon name="refresh" size="md" class="mr-2 animate-spin" />
        {{ t('userAccounts.importProxyLoading') }}
      </div>

      <div v-else-if="ownedProxies.length === 0" class="rounded-xl border border-dashed border-gray-300 px-6 py-12 text-center dark:border-dark-600">
        <Icon name="server" size="lg" class="mx-auto text-gray-400" />
        <p class="mt-3 font-medium text-gray-700 dark:text-dark-200">{{ t('userAccounts.proxyManagerEmpty') }}</p>
        <p class="mt-1 text-sm text-gray-500 dark:text-dark-400">{{ t('userAccounts.proxyActionAddDesc') }}</p>
      </div>

      <div v-else class="grid gap-3 sm:grid-cols-2">
        <article
          v-for="proxy in ownedProxies"
          :key="proxy.id"
          class="flex min-w-0 flex-col gap-4 rounded-xl border border-gray-200 bg-white p-4 dark:border-dark-700 dark:bg-dark-800"
        >
          <div class="min-w-0">
            <div class="flex items-start justify-between gap-3">
              <div class="min-w-0">
                <h4 class="truncate font-semibold text-gray-900 dark:text-white">{{ proxy.name }}</h4>
                <p class="mt-1 truncate text-sm text-gray-500 dark:text-dark-300">{{ proxy.protocol }}://{{ proxy.host }}:{{ proxy.port }}</p>
              </div>
              <span class="flex-shrink-0 rounded-full bg-gray-100 px-2 py-1 text-xs font-medium text-gray-600 dark:bg-dark-700 dark:text-dark-200">
                {{ t('userAccounts.proxyAccountCount', { count: proxy.account_count || 0 }) }}
              </span>
            </div>
            <p v-if="proxy.username" class="mt-2 truncate text-xs text-gray-500 dark:text-dark-400">
              {{ t('userAccounts.proxyUsername') }}：{{ proxy.username }}
            </p>
          </div>

          <div class="mt-auto grid grid-cols-2 gap-2">
            <button type="button" class="btn btn-secondary min-h-[44px]" :disabled="saving || deleting" @click="startEdit(proxy)">
              <Icon name="edit" size="sm" class="mr-2" />
              {{ t('common.edit') }}
            </button>
            <button
              type="button"
              class="btn btn-danger min-h-[44px]"
              :disabled="saving || deleting || (proxy.account_count || 0) > 0"
              :title="(proxy.account_count || 0) > 0 ? t('userAccounts.proxyDeleteBlocked') : t('common.delete')"
              @click="requestDelete(proxy)"
            >
              <Icon name="trash" size="sm" class="mr-2" />
              {{ t('common.delete') }}
            </button>
          </div>
        </article>
      </div>
    </div>
  </BaseDialog>

  <ConfirmDialog
    :show="Boolean(deletingProxy)"
    :title="t('userAccounts.proxyDeleteTitle')"
    :message="t('userAccounts.proxyDeleteConfirm', { name: deletingProxy?.name || '' })"
    :confirm-text="t('common.delete')"
    :cancel-text="t('common.cancel')"
    danger
    @confirm="confirmDelete"
    @cancel="deletingProxy = null"
  />
</template>

<script setup lang="ts">
import { computed, reactive, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { accountShareAPI } from '@/api'
import { useAppStore } from '@/stores/app'
import { extractApiErrorMessage } from '@/utils/apiError'
import BaseDialog from '@/components/common/BaseDialog.vue'
import ConfirmDialog from '@/components/common/ConfirmDialog.vue'
import Icon from '@/components/icons/Icon.vue'
import UserProxyQuickCreatePanel from '@/components/user/UserProxyQuickCreatePanel.vue'
import type { Proxy, ProxyProtocol } from '@/types'

const props = defineProps<{
  show: boolean
  proxies: Proxy[]
  loading?: boolean
}>()

const emit = defineEmits<{
  close: []
  changed: []
}>()

const { t } = useI18n()
const appStore = useAppStore()
const creating = ref(false)
const saving = ref(false)
const deleting = ref(false)
const editingProxy = ref<Proxy | null>(null)
const deletingProxy = ref<Proxy | null>(null)
const clearPassword = ref(false)
const editForm = reactive({
  name: '',
  protocol: 'socks5' as ProxyProtocol,
  host: '',
  port: 1080,
  username: '',
  password: ''
})

const ownedProxies = computed(() => props.proxies.filter(proxy => Number(proxy.owner_user_id || 0) > 0))

watch(() => props.show, show => {
  if (!show) resetTransientState()
})

function resetTransientState(): void {
  creating.value = false
  editingProxy.value = null
  deletingProxy.value = null
  editForm.password = ''
  clearPassword.value = false
}

function requestClose(): void {
  if (saving.value || deleting.value) return
  emit('close')
}

function startCreate(): void {
  cancelEdit()
  creating.value = true
}

function handleCreated(): void {
  creating.value = false
  emit('changed')
}

function startEdit(proxy: Proxy): void {
  creating.value = false
  editingProxy.value = proxy
  Object.assign(editForm, {
    name: proxy.name,
    protocol: proxy.protocol,
    host: proxy.host,
    port: proxy.port,
    username: proxy.username || '',
    password: ''
  })
  clearPassword.value = false
}

function cancelEdit(): void {
  editingProxy.value = null
  editForm.password = ''
  clearPassword.value = false
}

async function saveEdit(): Promise<void> {
  if (!editingProxy.value || saving.value) return
  if (!editForm.host.trim() || !Number.isInteger(editForm.port) || editForm.port < 1 || editForm.port > 65535) {
    appStore.showError(t('userAccounts.proxyInvalidFormat'))
    return
  }
  saving.value = true
  try {
    await accountShareAPI.updateProxy(editingProxy.value.id, {
      name: editForm.name.trim(),
      protocol: editForm.protocol,
      host: editForm.host.trim(),
      port: editForm.port,
      username: editForm.username.trim(),
      ...(clearPassword.value ? { password: '' } : editForm.password ? { password: editForm.password } : {})
    })
    appStore.showSuccess(t('userAccounts.proxyUpdatedSuccess'))
    cancelEdit()
    emit('changed')
  } catch (error) {
    appStore.showError(extractApiErrorMessage(error, t('userAccounts.proxyUpdateFailed')))
  } finally {
    saving.value = false
  }
}

function requestDelete(proxy: Proxy): void {
  if ((proxy.account_count || 0) > 0) {
    appStore.showError(t('userAccounts.proxyDeleteBlocked'))
    return
  }
  deletingProxy.value = proxy
}

async function confirmDelete(): Promise<void> {
  if (!deletingProxy.value || deleting.value) return
  deleting.value = true
  try {
    await accountShareAPI.deleteProxy(deletingProxy.value.id)
    appStore.showSuccess(t('userAccounts.proxyDeletedSuccess'))
    deletingProxy.value = null
    emit('changed')
  } catch (error) {
    appStore.showError(extractApiErrorMessage(error, t('userAccounts.proxyDeleteFailed')))
  } finally {
    deleting.value = false
  }
}
</script>
