<template>
  <div
    class="rounded-lg border border-primary-200 bg-primary-50/40 p-4 dark:border-primary-500/30 dark:bg-primary-500/10"
    data-testid="user-proxy-create-panel"
  >
    <div class="mb-4 flex items-start justify-between gap-3">
      <div>
        <h4 class="text-sm font-semibold text-gray-900 dark:text-white">
          {{ t('userAccounts.proxyDialogTitle') }}
        </h4>
        <p class="mt-1 text-xs text-gray-500 dark:text-dark-300">
          {{ t('userAccounts.proxyActionAddDesc') }}
        </p>
      </div>
      <button
        type="button"
        class="rounded-md p-1.5 text-gray-400 hover:bg-white hover:text-gray-600 disabled:cursor-not-allowed disabled:opacity-60 dark:hover:bg-dark-800 dark:hover:text-dark-200"
        :disabled="savingProxy"
        :aria-label="t('common.cancel')"
        @click="emit('cancel')"
      >
        <Icon name="x" size="sm" />
      </button>
    </div>

    <div class="space-y-5">
      <div>
        <label class="input-label">{{ t('userAccounts.proxySmartLabel') }}</label>
        <textarea
          v-model="proxySmartInput"
          class="input min-h-[116px] resize-y leading-6"
          rows="4"
          :placeholder="t('userAccounts.proxySmartPlaceholder')"
          data-testid="user-proxy-smart-input"
          @blur="applySmartProxyInput(false)"
        />
        <div class="mt-2 flex flex-wrap items-center gap-2">
          <button type="button" class="btn btn-secondary h-9" @click="applySmartProxyInput(true)">
            <Icon name="sync" size="sm" class="mr-2" />
            {{ t('userAccounts.proxySmartApply') }}
          </button>
          <span class="text-xs text-gray-500 dark:text-dark-300">{{ t('userAccounts.proxySmartHint') }}</span>
        </div>
      </div>

      <div class="border-t border-gray-200 dark:border-dark-700"></div>

      <label class="block">
        <span class="input-label">{{ t('userAccounts.proxyName') }}</span>
        <input
          v-model.trim="proxyForm.name"
          class="input"
          maxlength="100"
          :placeholder="t('userAccounts.proxyNamePlaceholder')"
          data-testid="user-proxy-name-input"
        />
        <small class="mt-1 block text-xs text-gray-500 dark:text-dark-300">{{ t('userAccounts.proxyNameHint') }}</small>
      </label>

      <div>
        <label class="input-label">{{ t('userAccounts.proxyIpType') }}</label>
        <div class="grid gap-3 sm:grid-cols-2">
          <button
            type="button"
            :class="[
              'flex min-h-[52px] items-center gap-2 rounded-lg border px-4 text-sm font-semibold transition-colors',
              proxyForm.ip_type === 'ipv4'
                ? 'border-primary-500 text-primary-600 ring-2 ring-primary-500/10 dark:text-primary-300'
                : 'border-gray-200 text-gray-700 hover:bg-gray-50 dark:border-dark-700 dark:text-dark-200 dark:hover:bg-dark-800'
            ]"
            @click="proxyForm.ip_type = 'ipv4'"
          >
            <span
              class="h-4 w-4 rounded-full border"
              :class="proxyForm.ip_type === 'ipv4' ? 'border-primary-500 bg-primary-500 shadow-[inset_0_0_0_4px_white]' : 'border-gray-300 bg-white dark:border-dark-500 dark:bg-dark-800'"
            ></span>
            IPV4
          </button>
          <button
            type="button"
            :class="[
              'flex min-h-[52px] items-center gap-2 rounded-lg border px-4 text-sm font-semibold transition-colors',
              proxyForm.ip_type === 'ipv6'
                ? 'border-primary-500 text-primary-600 ring-2 ring-primary-500/10 dark:text-primary-300'
                : 'border-gray-200 text-gray-700 hover:bg-gray-50 dark:border-dark-700 dark:text-dark-200 dark:hover:bg-dark-800'
            ]"
            @click="proxyForm.ip_type = 'ipv6'"
          >
            <span
              class="h-4 w-4 rounded-full border"
              :class="proxyForm.ip_type === 'ipv6' ? 'border-primary-500 bg-primary-500 shadow-[inset_0_0_0_4px_white]' : 'border-gray-300 bg-white dark:border-dark-500 dark:bg-dark-800'"
            ></span>
            IPV6
          </button>
        </div>
      </div>

      <div>
        <label class="input-label">{{ t('userAccounts.proxyEndpoint') }}</label>
        <div class="grid gap-3 md:grid-cols-[150px_minmax(0,1fr)_110px]">
          <select v-model="proxyForm.protocol" class="input">
            <option value="socks5">SOCKS5</option>
            <option value="socks5h">SOCKS5H</option>
            <option value="http">HTTP</option>
            <option value="https">HTTPS</option>
          </select>
          <input
            v-model.trim="proxyForm.host"
            class="input"
            :placeholder="t('userAccounts.proxyHost')"
            data-testid="user-proxy-host-input"
          />
          <input
            v-model.number="proxyForm.port"
            class="input"
            type="number"
            min="1"
            max="65535"
            :placeholder="t('userAccounts.proxyPort')"
            data-testid="user-proxy-port-input"
          />
        </div>
      </div>

      <div class="grid gap-4 md:grid-cols-2">
        <label class="block">
          <span class="input-label">{{ t('userAccounts.proxyUsername') }}</span>
          <input
            v-model.trim="proxyForm.username"
            class="input"
            :placeholder="t('userAccounts.proxyUsernamePlaceholder')"
          />
        </label>
        <label class="block">
          <span class="input-label">{{ t('userAccounts.proxyPassword') }}</span>
          <input
            v-model.trim="proxyForm.password"
            class="input"
            type="password"
            :placeholder="t('userAccounts.proxyPasswordPlaceholder')"
          />
        </label>
      </div>

      <div
        v-if="proxyDialogError"
        class="flex gap-2 rounded-lg border border-red-200 bg-red-50 p-3 text-sm text-red-700 dark:border-red-900/60 dark:bg-red-900/20 dark:text-red-300"
      >
        <Icon name="exclamationCircle" size="sm" class="mt-0.5 flex-shrink-0" />
        <span>{{ proxyDialogError }}</span>
      </div>

      <div class="flex flex-wrap justify-end gap-3 border-t border-gray-200 pt-4 dark:border-dark-700">
        <button type="button" class="btn btn-secondary" :disabled="savingProxy" @click="emit('cancel')">
          {{ t('common.cancel') }}
        </button>
        <button
          type="button"
          class="btn btn-primary"
          :disabled="savingProxy"
          data-testid="user-proxy-save-button"
          @click="saveUserProxy"
        >
          <svg v-if="savingProxy" class="-ml-1 mr-2 h-4 w-4 animate-spin" fill="none" viewBox="0 0 24 24">
            <circle class="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="4"></circle>
            <path class="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"></path>
          </svg>
          <Icon v-else name="checkCircle" size="sm" class="mr-2" />
          {{ t('userAccounts.proxySaveAndUse') }}
        </button>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { accountShareAPI } from '@/api'
import Icon from '@/components/icons/Icon.vue'
import { useAppStore } from '@/stores/app'
import { extractApiErrorMessage } from '@/utils/apiError'
import type { Proxy, ProxyProtocol } from '@/types'

type UserProxyFormState = {
  ip_type: 'ipv4' | 'ipv6'
  name: string
  protocol: ProxyProtocol
  host: string
  port: number | null
  username: string
  password: string
}

const emit = defineEmits<{
  created: [proxy: Proxy]
  cancel: []
}>()

const { t } = useI18n()
const appStore = useAppStore()

const savingProxy = ref(false)
const proxyDialogError = ref('')
const proxySmartInput = ref('')

const proxyForm = reactive<UserProxyFormState>({
  ip_type: 'ipv4',
  name: '',
  protocol: 'socks5',
  host: '',
  port: null,
  username: '',
  password: ''
})

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
  return `${t('userAccounts.proxyDefaultName')} ${host}:${port}`
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
    proxyForm.ip_type = parsed.hostname.includes(':') ? 'ipv6' : 'ipv4'
    updateProxyNameFromParsedInput(parsed.hostname, port, remark)
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
    proxyDialogError.value = t('userAccounts.proxyInvalidFormat')
  }
}

function validateUserProxyForm(): string {
  if (!['http', 'https', 'socks5', 'socks5h'].includes(proxyForm.protocol)) return t('userAccounts.proxyProtocolRequired')
  if (!proxyForm.host.trim()) return t('userAccounts.proxyHostRequired')
  if (/\s/.test(proxyForm.host)) return t('userAccounts.proxyHostNoSpaces')
  const port = Number(proxyForm.port || 0)
  if (!Number.isInteger(port) || port < 1 || port > 65535) return t('userAccounts.proxyPortInvalid')
  return ''
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
    appStore.showSuccess(t('userAccounts.proxyCreatedSuccess'))
    emit('created', created)
  } catch (error: unknown) {
    proxyDialogError.value = extractApiErrorMessage(error, t('userAccounts.proxyCreateFailed'))
  } finally {
    savingProxy.value = false
  }
}
</script>
