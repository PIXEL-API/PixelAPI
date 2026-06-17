<template>
  <CredentialImportModal
    v-if="!requiresOAuthLogin"
    :show="show"
    :title="t('userAccounts.importTitle')"
    :hint="t('userAccounts.importHint')"
    :warning="importWarningText"
    :text-hint="importTextHint"
    form-id="user-import-accounts-form"
    :submit-disabled="!canSubmitCredentialImport"
    :importer="importPersonalCredentials"
    @close="$emit('close')"
    @imported="$emit('imported', $event)"
  >
    <template #controls>
      <PlatformSelector
        :selected-platform="selectedPlatform"
        @select="selectPlatform"
      />
      <AccountLevelSelector
        v-if="selectedPlatform === 'openai'"
        :selected-level="selectedAccountLevel"
        @select="selectAccountLevel"
      />
      <div
        v-else-if="selectedPlatform"
        class="rounded-lg border border-gray-200 bg-gray-50 p-3 text-xs text-gray-600 dark:border-dark-700 dark:bg-dark-800 dark:text-dark-300"
      >
        {{ selectedPlatformHint }}
      </div>
    </template>
  </CredentialImportModal>

  <BaseDialog
    v-else
    :show="show"
    :title="t('userAccounts.importTitle')"
    width="wide"
    close-on-click-outside
    @close="handleClose"
  >
    <form id="user-import-openai-oauth-form" class="space-y-5" @submit.prevent="submitOAuthImport">
      <PlatformSelector
        :selected-platform="selectedPlatform"
        @select="selectPlatform"
      />
      <AccountLevelSelector
        v-if="selectedPlatform === 'openai'"
        :selected-level="selectedAccountLevel"
        @select="selectAccountLevel"
      />

      <div class="rounded-lg border border-amber-200 bg-amber-50 p-3 text-xs text-amber-800 dark:border-amber-800 dark:bg-amber-900/20 dark:text-amber-300">
        {{ t('userAccounts.importOAuthOnlyHint') }}
      </div>

      <div>
        <label class="input-label">{{ t('admin.accounts.accountName') }}</label>
        <input
          v-model.trim="oauthAccountName"
          type="text"
          class="input"
          :placeholder="t('userAccounts.importOAuthNamePlaceholder')"
        />
      </div>

      <div>
        <div class="mb-2 flex items-center justify-between gap-3">
          <label class="input-label mb-0">{{ t('userAccounts.importProxy') }}</label>
          <div class="flex flex-wrap items-center justify-end gap-2">
            <button
              type="button"
              class="text-xs font-medium text-primary-600 hover:text-primary-700 disabled:cursor-not-allowed disabled:opacity-60 dark:text-primary-400"
              :disabled="proxyLoading"
              @click="loadProxies(true)"
            >
              {{ proxyLoading ? t('common.loading') : t('common.refresh') }}
            </button>
            <button
              type="button"
              class="inline-flex items-center gap-1 rounded-md border border-gray-200 bg-white px-2.5 py-1.5 text-xs font-medium text-gray-700 hover:border-sky-300 hover:bg-sky-50 dark:border-dark-700 dark:bg-dark-800 dark:text-dark-200 dark:hover:border-sky-500/70 dark:hover:bg-sky-900/20"
              @click="openProxyPurchase()"
            >
              <Icon name="externalLink" size="xs" />
              {{ t('userAccounts.proxyActionBuyTitle') }}
            </button>
            <button
              type="button"
              class="inline-flex items-center gap-1 rounded-md border border-primary-200 bg-primary-50 px-2.5 py-1.5 text-xs font-medium text-primary-700 hover:border-primary-300 hover:bg-primary-100 dark:border-primary-500/30 dark:bg-primary-500/10 dark:text-primary-300 dark:hover:bg-primary-500/20"
              :aria-expanded="showProxyDialog"
              data-testid="import-open-user-proxy-panel"
              @click="openAddProxyDialog()"
            >
              <Icon name="plus" size="xs" />
              {{ t('userAccounts.proxyActionAddTitle') }}
            </button>
          </div>
        </div>
        <ProxySelector
          v-model="selectedProxyId"
          :proxies="proxies"
          :allow-empty="false"
          :can-test="false"
          hide-endpoint
        />
        <p class="input-hint">
          {{ proxyHelperText }}
        </p>
        <UserProxyQuickCreatePanel
          v-if="showProxyDialog"
          class="mt-4"
          @created="handleProxyCreated"
          @cancel="closeProxyDialog"
        />
      </div>

      <OAuthAuthorizationFlow
        ref="oauthFlowRef"
        add-method="oauth"
        :auth-url="openaiOAuth.authUrl.value"
        :session-id="openaiOAuth.sessionId.value"
        :loading="openaiOAuth.loading.value"
        :error="openaiOAuth.error.value"
        :show-help="false"
        :show-proxy-warning="false"
        :show-cookie-option="false"
        :show-refresh-token-option="false"
        :show-mobile-refresh-token-option="false"
        :show-session-token-option="false"
        :show-access-token-option="false"
        platform="openai"
        @generate-url="generateOAuthUrl"
      />
    </form>

    <template #footer>
      <div class="flex justify-end gap-3">
        <button class="btn btn-secondary" type="button" :disabled="oauthSubmitting" @click="handleClose">
          {{ t('common.cancel') }}
        </button>
        <button
          class="btn btn-primary"
          type="submit"
          form="user-import-openai-oauth-form"
          :disabled="oauthSubmitting || !canSubmitOAuthImport"
        >
          <Icon v-if="!oauthSubmitting" name="check" size="sm" class="mr-2" />
          {{ oauthSubmitting ? t('admin.accounts.oauth.verifying') : t('admin.accounts.oauth.completeAuth') }}
        </button>
      </div>
    </template>
  </BaseDialog>

</template>

<script setup lang="ts">
import { computed, defineComponent, h, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { accountsAPI, accountShareAPI } from '@/api'
import BaseDialog from '@/components/common/BaseDialog.vue'
import ProxySelector from '@/components/common/ProxySelector.vue'
import CredentialImportModal from '@/components/account/CredentialImportModal.vue'
import OAuthAuthorizationFlow from '@/components/account/OAuthAuthorizationFlow.vue'
import UserProxyQuickCreatePanel from '@/components/user/UserProxyQuickCreatePanel.vue'
import Icon from '@/components/icons/Icon.vue'
import {
  PERSONAL_ACCOUNT_DEFAULT_AUTO_PAUSE_ON_EXPIRED,
  PERSONAL_ACCOUNT_DEFAULT_CONCURRENCY,
  PERSONAL_ACCOUNT_IMPORT_LIMIT,
  PERSONAL_ACCOUNT_DEFAULT_PRIORITY,
  applyPersonalAccountTemplate
} from '@/components/account/personalAccountTemplate'
import { useAppStore } from '@/stores/app'
import { useOpenAIOAuth } from '@/composables/useOpenAIOAuth'
import { extractApiErrorMessage } from '@/utils/apiError'
import type { ImportCredentialContentsRequest, ImportCredentialContentsResponse } from '@/api/accounts'
import type { AccountLevel, AccountPlatform, Proxy } from '@/types'

type SelectableOpenAILevel = Exclude<AccountLevel, 'unknown'>
type ImportPlatform = AccountPlatform

interface Props {
  show: boolean
}

interface Emits {
  (e: 'close'): void
  (e: 'imported', payload?: { close: boolean }): void
}

const props = defineProps<Props>()
const emit = defineEmits<Emits>()

const { t } = useI18n()
const appStore = useAppStore()
const openaiOAuth = useOpenAIOAuth('user')

const PROXY_PURCHASE_URL = 'https://www.seekproxy.com/user/reg?invite_id=105978'

const selectedPlatform = ref<ImportPlatform | ''>('')
const selectedAccountLevel = ref<SelectableOpenAILevel | ''>('')
const selectedProxyId = ref<number | null>(null)
const proxies = ref<Proxy[]>([])
const proxyLoading = ref(false)
const proxyLoadMessage = ref('')
const oauthAccountName = ref('')
const oauthSubmitting = ref(false)
const oauthFlowRef = ref<InstanceType<typeof OAuthAuthorizationFlow> | null>(null)
const showProxyDialog = ref(false)

const importLimit = computed(() => {
  const configured = Number(appStore.cachedPublicSettings?.user_account_import_limit)
  return Number.isFinite(configured) && configured > 0
    ? Math.floor(configured)
    : PERSONAL_ACCOUNT_IMPORT_LIMIT
})

const requiresOAuthLogin = computed(() => selectedPlatform.value === 'openai' && selectedAccountLevel.value === 'pro')

const canSubmitCredentialImport = computed(() => {
  if (!selectedPlatform.value) return false
  if (selectedPlatform.value === 'openai') {
    return Boolean(selectedAccountLevel.value && selectedAccountLevel.value !== 'pro')
  }
  return true
})

const importWarningText = computed(() => {
  switch (selectedPlatform.value) {
    case 'openai':
      return t('userAccounts.importWarningOpenAI', { max: importLimit.value })
    case 'anthropic':
      return t('userAccounts.importWarningClaude', { max: importLimit.value })
    case 'gemini':
      return t('userAccounts.importWarningGemini', { max: importLimit.value })
    case 'antigravity':
      return t('userAccounts.importWarningAntigravity', { max: importLimit.value })
    default:
      return t('userAccounts.importWarningChoosePlatform', { max: importLimit.value })
  }
})

const importTextHint = computed(() => {
  switch (selectedPlatform.value) {
    case 'openai':
      return t('userAccounts.importTextHintOpenAI')
    case 'anthropic':
      return t('userAccounts.importTextHintClaude')
    case 'gemini':
      return t('userAccounts.importTextHintGemini')
    case 'antigravity':
      return t('userAccounts.importTextHintAntigravity')
    default:
      return t('userAccounts.importTextHintChoosePlatform')
  }
})

const selectedPlatformHint = computed(() => {
  switch (selectedPlatform.value) {
    case 'anthropic':
      return t('userAccounts.importPlatformHintClaude')
    case 'gemini':
      return t('userAccounts.importPlatformHintGemini')
    case 'antigravity':
      return t('userAccounts.importPlatformHintAntigravity')
    default:
      return ''
  }
})

const proxyHelperText = computed(() => {
  if (proxyLoading.value) return t('userAccounts.importProxyLoading')
  if (proxyLoadMessage.value) return proxyLoadMessage.value
  if (proxies.value.length > 0) return t('userAccounts.importProxyHint')
  return t('userAccounts.importProxyEmpty')
})

const canSubmitOAuthImport = computed(() => {
  const authCode = oauthFlowRef.value?.authCode || ''
  const oauthState = oauthFlowRef.value?.oauthState || openaiOAuth.oauthState.value || ''
  return Boolean(
    selectedPlatform.value === 'openai' &&
    selectedAccountLevel.value &&
    selectedProxyId.value &&
    openaiOAuth.sessionId.value &&
    String(authCode).trim() &&
    String(oauthState).trim()
  )
})

const PlatformSelector = defineComponent({
  name: 'UserImportPlatformSelector',
  props: {
    selectedPlatform: {
      type: String,
      default: ''
    }
  },
  emits: ['select'],
  setup(props, { emit }) {
    const options: Array<{ value: ImportPlatform; label: string; desc: string }> = [
      { value: 'anthropic', label: 'Claude', desc: t('userAccounts.importPlatformClaude') },
      { value: 'openai', label: 'OpenAI', desc: t('userAccounts.importPlatformOpenAI') },
      { value: 'gemini', label: 'Gemini', desc: t('userAccounts.importPlatformGemini') },
      { value: 'antigravity', label: 'Antigravity', desc: t('userAccounts.importPlatformAntigravity') }
    ]
    return () => h('div', { class: 'space-y-2' }, [
      h('label', { class: 'input-label' }, t('userAccounts.importPlatform')),
      h('div', { class: 'grid gap-2 sm:grid-cols-2 lg:grid-cols-4' }, options.map(option =>
        h(
          'button',
          {
            type: 'button',
            class: [
              'flex min-h-[76px] flex-col justify-center rounded-lg border px-3 py-2 text-left transition-colors',
              props.selectedPlatform === option.value
                ? 'border-primary-400 bg-primary-50 text-primary-700 dark:border-primary-500 dark:bg-primary-900/30 dark:text-primary-300'
                : 'border-gray-200 bg-white text-gray-700 hover:bg-gray-50 dark:border-dark-700 dark:bg-dark-800 dark:text-dark-200 dark:hover:bg-dark-700'
            ],
            onClick: () => emit('select', option.value)
          },
          [
            h('span', { class: 'text-sm font-semibold' }, option.label),
            h('span', { class: 'mt-1 text-xs text-gray-500 dark:text-dark-400' }, option.desc)
          ]
        )
      ))
    ])
  }
})

const AccountLevelSelector = defineComponent({
  name: 'UserImportAccountLevelSelector',
  props: {
    selectedLevel: {
      type: String,
      default: ''
    }
  },
  emits: ['select'],
  setup(props, { emit }) {
    const options: Array<{ value: SelectableOpenAILevel; label: string; desc: string }> = [
      { value: 'free', label: t('admin.accounts.accountLevel.free'), desc: t('userAccounts.importLevelFree') },
      { value: 'plus', label: t('admin.accounts.accountLevel.plus'), desc: t('userAccounts.importLevelPlus') },
      { value: 'pro', label: t('admin.accounts.accountLevel.pro'), desc: t('userAccounts.importLevelPro') },
      { value: 'team', label: t('admin.accounts.accountLevel.team'), desc: t('userAccounts.importLevelTeam') }
    ]
    return () => h('div', { class: 'space-y-2' }, [
      h('div', { class: 'flex items-center justify-between gap-3' }, [
        h('label', { class: 'input-label mb-0' }, t('userAccounts.importAccountLevel')),
        props.selectedLevel
          ? h(
              'button',
              {
                type: 'button',
                class: 'text-xs font-medium text-gray-500 hover:text-gray-700 dark:text-dark-400 dark:hover:text-dark-200',
                onClick: () => emit('select', '')
              },
              t('common.clear')
            )
          : null
      ]),
      h('div', { class: 'grid gap-2 sm:grid-cols-4' }, options.map(option =>
        h(
          'button',
          {
            type: 'button',
            class: [
              'flex min-h-[76px] flex-col justify-center rounded-lg border px-3 py-2 text-left transition-colors',
              props.selectedLevel === option.value
                ? 'border-primary-400 bg-primary-50 text-primary-700 dark:border-primary-500 dark:bg-primary-900/30 dark:text-primary-300'
                : 'border-gray-200 bg-white text-gray-700 hover:bg-gray-50 dark:border-dark-700 dark:bg-dark-800 dark:text-dark-200 dark:hover:bg-dark-700'
            ],
            onClick: () => emit('select', option.value)
          },
          [
            h('span', { class: 'text-sm font-semibold' }, option.label),
            h('span', { class: 'mt-1 text-xs text-gray-500 dark:text-dark-400' }, option.desc)
          ]
        )
      )),
      h('p', { class: 'input-hint' }, t('userAccounts.importAccountLevelHint'))
    ])
  }
})

watch(
  () => selectedPlatform.value,
  (platform) => {
    if (platform !== 'openai') {
      selectedAccountLevel.value = ''
    }
    selectedProxyId.value = null
    oauthAccountName.value = ''
    proxyLoadMessage.value = ''
    showProxyDialog.value = false
    openaiOAuth.error.value = ''
    openaiOAuth.resetState()
    oauthFlowRef.value?.reset()
  }
)

watch(
  () => selectedAccountLevel.value,
  (level) => {
    proxyLoadMessage.value = ''
    openaiOAuth.error.value = ''
    openaiOAuth.resetState()
    oauthFlowRef.value?.reset()
    if (selectedPlatform.value === 'openai' && level === 'pro') {
      loadProxies()
    } else {
      selectedProxyId.value = null
      oauthAccountName.value = ''
      showProxyDialog.value = false
    }
  }
)

watch(
  () => selectedProxyId.value,
  () => {
    openaiOAuth.error.value = ''
    openaiOAuth.resetState()
    oauthFlowRef.value?.reset()
  }
)

watch(
  () => openaiOAuth.authUrl.value,
  (authUrl) => {
    if (authUrl) {
      window.open(authUrl, '_blank', 'noopener,noreferrer')
    }
  }
)

watch(
  () => openaiOAuth.sessionId.value,
  () => {
    oauthFlowRef.value?.reset()
  }
)

function selectPlatform(platform: ImportPlatform): void {
  selectedPlatform.value = platform
}

function selectAccountLevel(level: SelectableOpenAILevel | ''): void {
  selectedAccountLevel.value = level
}

function resetOAuthImportState(): void {
  selectedPlatform.value = ''
  selectedAccountLevel.value = ''
  selectedProxyId.value = null
  oauthAccountName.value = ''
  proxyLoadMessage.value = ''
  showProxyDialog.value = false
  openaiOAuth.resetState()
  oauthFlowRef.value?.reset()
}

watch(
  () => props.show,
  (open) => {
    if (!open) {
      resetOAuthImportState()
    }
  }
)

function importPersonalCredentials(contents: string[]): Promise<ImportCredentialContentsResponse> {
  if (!selectedPlatform.value) {
    appStore.showError(t('userAccounts.importPlatformRequired'))
    return Promise.reject(new Error(t('userAccounts.importPlatformRequired')))
  }
  const request: ImportCredentialContentsRequest = {
    contents,
    platform: selectedPlatform.value,
    share_mode: 'private',
    concurrency: PERSONAL_ACCOUNT_DEFAULT_CONCURRENCY,
    priority: PERSONAL_ACCOUNT_DEFAULT_PRIORITY,
    group_ids: [],
    auto_pause_on_expired: PERSONAL_ACCOUNT_DEFAULT_AUTO_PAUSE_ON_EXPIRED
  }
  if (selectedPlatform.value === 'openai') {
    const accountLevel = selectedAccountLevel.value
    if (!accountLevel) {
      appStore.showError(t('userAccounts.importAccountLevelRequired'))
      return Promise.reject(new Error(t('userAccounts.importAccountLevelRequired')))
    }
    request.account_level = accountLevel
  }
  return accountsAPI.importCredentialContents(request)
}

async function loadProxies(force = false): Promise<void> {
  if (proxyLoading.value || (!force && proxies.value.length > 0)) return
  proxyLoading.value = true
  proxyLoadMessage.value = ''
  try {
    proxies.value = await accountShareAPI.listProxies()
  } catch (error: unknown) {
    proxyLoadMessage.value = extractApiErrorMessage(error, t('userAccounts.importProxyLoadFailed'))
  } finally {
    proxyLoading.value = false
  }
}

function openProxyPurchase(close?: () => void): void {
  close?.()
  window.open(PROXY_PURCHASE_URL, '_blank', 'noopener,noreferrer')
}

function openAddProxyDialog(close?: () => void): void {
  close?.()
  showProxyDialog.value = true
}

function closeProxyDialog(): void {
  showProxyDialog.value = false
}

function upsertProxy(proxy: Proxy): void {
  const index = proxies.value.findIndex(item => item.id === proxy.id)
  if (index >= 0) {
    proxies.value[index] = { ...proxies.value[index], ...proxy }
    return
  }
  proxies.value = [proxy, ...proxies.value]
}

function handleProxyCreated(proxy: Proxy): void {
  upsertProxy(proxy)
  selectedProxyId.value = proxy.id
  proxyLoadMessage.value = ''
  showProxyDialog.value = false
}

async function generateOAuthUrl(): Promise<void> {
  if (selectedPlatform.value !== 'openai') {
    appStore.showError(t('userAccounts.importPlatformRequired'))
    return
  }
  if (!selectedAccountLevel.value) {
    appStore.showError(t('userAccounts.importAccountLevelRequired'))
    return
  }
  if (!selectedProxyId.value) {
    appStore.showError(t('userAccounts.importProxyRequired'))
    return
  }
  await openaiOAuth.generateAuthUrl(selectedProxyId.value)
}

function handleClose(): void {
  if (oauthSubmitting.value) return
  emit('close')
}

async function submitOAuthImport(): Promise<void> {
  if (selectedPlatform.value !== 'openai') {
    appStore.showError(t('userAccounts.importPlatformRequired'))
    return
  }
  if (!selectedAccountLevel.value) {
    appStore.showError(t('userAccounts.importAccountLevelRequired'))
    return
  }
  if (!selectedProxyId.value) {
    appStore.showError(t('userAccounts.importProxyRequired'))
    return
  }
  const authCode = String(oauthFlowRef.value?.authCode || '').trim()
  const oauthState = String(oauthFlowRef.value?.oauthState || openaiOAuth.oauthState.value || '').trim()
  if (!authCode || !oauthState || !openaiOAuth.sessionId.value) {
    appStore.showError(t('userAccounts.importOAuthCallbackRequired'))
    return
  }

  oauthSubmitting.value = true
  try {
    const tokenInfo = await openaiOAuth.exchangeAuthCode(
      authCode,
      openaiOAuth.sessionId.value,
      oauthState,
      selectedProxyId.value
    )
    if (!tokenInfo) return

    const templated = applyPersonalAccountTemplate(
      'openai',
      openaiOAuth.buildCredentials(tokenInfo),
      openaiOAuth.buildExtraInfo(tokenInfo)
    )
    await accountsAPI.create({
      name: oauthAccountName.value || tokenInfo.email || `${t('admin.accounts.accountLevel.' + selectedAccountLevel.value)} OpenAI`,
      platform: 'openai',
      account_level: selectedAccountLevel.value,
      type: 'oauth',
      credentials: templated.credentials,
      extra: templated.extra,
      proxy_id: selectedProxyId.value,
      share_mode: 'private',
      concurrency: PERSONAL_ACCOUNT_DEFAULT_CONCURRENCY,
      priority: PERSONAL_ACCOUNT_DEFAULT_PRIORITY,
      group_ids: [],
      auto_pause_on_expired: PERSONAL_ACCOUNT_DEFAULT_AUTO_PAUSE_ON_EXPIRED
    })
    appStore.showSuccess(t('userAccounts.accountCreatedSuccess'))
    emit('imported', { close: true })
  } catch (error: unknown) {
    appStore.showError(extractApiErrorMessage(error, t('userAccounts.importFailed')))
  } finally {
    oauthSubmitting.value = false
  }
}
</script>
