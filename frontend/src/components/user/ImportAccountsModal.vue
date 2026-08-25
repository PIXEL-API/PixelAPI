<template>
  <CredentialImportModal
    v-if="!requiresOAuthLogin"
    :show="show"
    :title="t('userAccounts.importTitle')"
    width="extra-wide"
    :hint="importHintText"
    :warning="importWarningText"
    :text-hint="importTextHint"
    :text-placeholder="importTextPlaceholder"
    :show-openai-format-examples="
      selectedPlatform === 'openai' && selectedOpenAIAuthMode === 'oauth'
    "
    :file-accept="importFileAccept"
    :allowed-extensions="importAllowedExtensions"
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
      <OpenAIAuthModeSelector
        v-if="selectedPlatform === 'openai'"
        :selected-mode="selectedOpenAIAuthMode"
        @select="selectOpenAIAuthMode"
      />
      <AccountLevelSelector
        v-if="
          selectedPlatform === 'grok' ||
          (selectedPlatform === 'openai' && selectedOpenAIAuthMode !== 'agent_identity')
        "
        :platform="selectedPlatform"
        :selected-level="selectedAccountLevel"
        @select="selectAccountLevel"
      />
      <div
        v-if="selectedPlatform === 'openai' && selectedOpenAIAuthMode === 'agent_identity'"
        class="rounded-lg border border-primary-200 bg-primary-50 p-3 text-sm text-primary-800 dark:border-primary-500/30 dark:bg-primary-500/10 dark:text-primary-200"
        data-testid="agent-identity-import-notice"
      >
        <p class="font-medium">{{ t('userAccounts.importAgentIdentityNoticeTitle') }}</p>
        <ul class="mt-2 list-disc space-y-1 pl-5">
          <li>{{ t('userAccounts.importAgentIdentityTeamIsolation') }}</li>
          <li>{{ t('userAccounts.importAgentIdentityPrivate') }}</li>
          <li>{{ t('userAccounts.importAgentIdentityNoOAuthExpiry') }}</li>
        </ul>
      </div>
      <div
        v-else-if="selectedPlatform"
        class="rounded-lg border border-gray-200 bg-gray-50 p-3 text-xs text-gray-600 dark:border-dark-700 dark:bg-dark-800 dark:text-dark-300"
      >
        {{ selectedPlatformHint }}
      </div>
      <div
        v-if="selectedPlatform === 'openai' && selectedOpenAIAuthMode === 'oauth'"
        class="flex justify-end"
      >
        <button
          type="button"
          class="text-sm font-medium text-primary-600 hover:text-primary-700 dark:text-primary-400"
          @click="switchToOAuthLogin"
        >
          {{ t('userAccounts.importSwitchToOAuthLogin') }}
        </button>
      </div>
      <div v-if="requiresCredentialImportProxy" class="space-y-2">
        <div class="flex flex-wrap items-center justify-between gap-3">
          <label class="input-label mb-0">{{ t('userAccounts.importProxy') }}</label>
          <button
            type="button"
            class="text-xs font-medium text-primary-600 hover:text-primary-700 disabled:cursor-not-allowed disabled:opacity-60 dark:text-primary-400"
            :disabled="proxyLoading"
            @click="loadProxies(true)"
          >
            {{ proxyLoading ? t('common.loading') : t('common.refresh') }}
          </button>
        </div>
        <ProxySelector
          v-model="selectedProxyId"
          :proxies="proxies"
          :allow-empty="false"
          :can-test="false"
          disable-full
          hide-endpoint
        />
        <p class="input-hint">
          {{ proxyHelperText }}
        </p>
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
      <OpenAIAuthModeSelector
        v-if="selectedPlatform === 'openai'"
        :selected-mode="selectedOpenAIAuthMode"
        @select="selectOpenAIAuthMode"
      />
      <AccountLevelSelector
        v-if="selectedPlatform === 'openai' && selectedOpenAIAuthMode === 'oauth'"
        :platform="selectedPlatform"
        :selected-level="selectedAccountLevel"
        @select="selectAccountLevel"
      />

      <div class="flex justify-end">
        <button
          type="button"
          class="text-sm font-medium text-primary-600 hover:text-primary-700 dark:text-primary-400"
          @click="switchToCredentialImport"
        >
          {{ t('userAccounts.importSwitchToCredential') }}
        </button>
      </div>

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

      <section
        class="border-t border-gray-200 pt-4 dark:border-dark-600"
        data-testid="oauth-import-model-whitelist"
      >
        <label class="input-label">{{ t('admin.accounts.modelWhitelist') }}</label>
        <p class="mb-3 text-xs leading-5 text-gray-500 dark:text-gray-400">
          {{ t('admin.accounts.userModelWhitelistHint') }}
        </p>
        <ModelWhitelistSelector
          v-model="oauthAllowedModels"
          platform="openai"
          :allowed-options="oauthModelOptions ?? []"
          :allow-custom="false"
        />
        <p v-if="oauthModelOptionsLoading" class="mt-2 text-xs text-gray-500 dark:text-gray-400">
          {{ t('admin.accounts.userModelOptionsLoading') }}
        </p>
        <p
          v-else-if="oauthModelOptionsLoadError"
          class="mt-2 text-xs text-red-600 dark:text-red-400"
          role="alert"
        >
          {{ t('admin.accounts.userModelOptionsLoadFailed', { message: oauthModelOptionsLoadError }) }}
        </p>
        <p
          v-else-if="oauthModelOptions !== null && oauthModelOptions.length === 0"
          class="mt-2 text-xs text-amber-600 dark:text-amber-400"
        >
          {{ t('admin.accounts.userModelOptionsEmpty') }}
        </p>
        <p
          v-else-if="oauthAllowedModels.length === 0"
          class="mt-2 text-xs text-amber-600 dark:text-amber-400"
        >
          {{ t('admin.accounts.userModelSelectionRequired') }}
        </p>
        <p v-else class="mt-2 text-xs text-gray-500 dark:text-gray-400">
          {{ t('admin.accounts.selectedModels', { count: oauthAllowedModels.length }) }}
        </p>
      </section>

      <div>
        <div class="mb-2 flex items-center justify-between gap-3">
          <label class="input-label mb-0">{{ t('userAccounts.importProxy') }}</label>
          <button
            type="button"
            class="text-xs font-medium text-primary-600 hover:text-primary-700 disabled:cursor-not-allowed disabled:opacity-60 dark:text-primary-400"
            :disabled="proxyLoading"
            @click="loadProxies(true)"
          >
            {{ proxyLoading ? t('common.loading') : t('common.refresh') }}
          </button>
        </div>
        <ProxySelector
          v-model="selectedProxyId"
          :proxies="proxies"
          :allow-empty="false"
          :can-test="false"
          disable-full
          hide-endpoint
        />
        <p class="input-hint">
          {{ proxyHelperText }}
        </p>
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
import ModelWhitelistSelector from '@/components/account/ModelWhitelistSelector.vue'
import OAuthAuthorizationFlow from '@/components/account/OAuthAuthorizationFlow.vue'
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
import { isProxyAccountFull, normalizeProxyAccountCount, normalizeProxyMaxAccounts } from '@/utils/proxyCapacity'
import { openAIAccountLevelLabel, selectableOpenAIAccountLevels } from '@/utils/openaiAccountLevels'
import { GROK_ACCOUNT_LEVEL_OPTIONS } from '@/utils/grokAccountLevels'
import type { ImportCredentialContentsRequest, ImportCredentialContentsResponse } from '@/api/accounts'
import type { AccountLevel, AccountPlatform, Proxy } from '@/types'

type SelectableImportLevel = Exclude<AccountLevel, 'unknown'>
type ImportPlatform = AccountPlatform
type OpenAIImportAuthMode = 'oauth' | 'personal_access_token' | 'agent_identity'

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


const selectedPlatform = ref<ImportPlatform | ''>('')
const selectedOpenAIAuthMode = ref<OpenAIImportAuthMode>('oauth')
const selectedAccountLevel = ref<SelectableImportLevel | ''>('')
const selectedProxyId = ref<number | null>(null)
const selectedImportFlow = ref<'credential' | 'oauth_login'>('credential')
const proxies = ref<Proxy[]>([])
const proxyLoading = ref(false)
const proxyLoadMessage = ref('')
const oauthAccountName = ref('')
const oauthSubmitting = ref(false)
const oauthFlowRef = ref<InstanceType<typeof OAuthAuthorizationFlow> | null>(null)
const oauthAllowedModels = ref<string[]>([])
const oauthModelOptions = ref<string[] | null>(null)
const oauthModelOptionsLoading = ref(false)
const oauthModelOptionsLoadError = ref('')
let oauthModelOptionsRequestVersion = 0

const oauthModelSelectionValid = computed(() => {
  if (
    oauthModelOptionsLoading.value ||
    oauthModelOptions.value === null ||
    oauthModelOptionsLoadError.value ||
    oauthAllowedModels.value.length === 0
  ) {
    return false
  }
  const allowedSet = new Set(oauthModelOptions.value)
  return oauthAllowedModels.value.every(model => allowedSet.has(model))
})

async function loadOAuthModelOptions(): Promise<void> {
  const requestVersion = ++oauthModelOptionsRequestVersion
  oauthModelOptionsLoadError.value = ''

  if (!props.show || !requiresOAuthLogin.value || selectedPlatform.value !== 'openai') {
    oauthModelOptions.value = null
    oauthModelOptionsLoading.value = false
    oauthAllowedModels.value = []
    return
  }

  oauthModelOptions.value = null
  oauthModelOptionsLoading.value = true
  oauthAllowedModels.value = []
  try {
    const result = await accountsAPI.getModelOptions('openai')
    if (requestVersion !== oauthModelOptionsRequestVersion) return
    const models = Array.from(
      new Set((result.models || []).map(model => model.trim()).filter(Boolean))
    )
    oauthModelOptions.value = models
    oauthAllowedModels.value = [...models]
  } catch (error: unknown) {
    if (requestVersion !== oauthModelOptionsRequestVersion) return
    oauthModelOptions.value = []
    oauthModelOptionsLoadError.value = extractApiErrorMessage(
      error,
      t('admin.accounts.userModelOptionsLoadFailedDefault')
    )
  } finally {
    if (requestVersion === oauthModelOptionsRequestVersion) {
      oauthModelOptionsLoading.value = false
    }
  }
}

function validateOAuthModelSelection(): boolean {
  if (oauthModelOptionsLoading.value || oauthModelOptions.value === null) {
    appStore.showError(t('admin.accounts.userModelOptionsNotReady'))
    return false
  }
  if (oauthModelOptionsLoadError.value) {
    appStore.showError(
      t('admin.accounts.userModelOptionsLoadFailed', { message: oauthModelOptionsLoadError.value })
    )
    return false
  }
  if (oauthAllowedModels.value.length === 0) {
    appStore.showError(t('admin.accounts.userModelSelectionRequired'))
    return false
  }
  const allowedSet = new Set(oauthModelOptions.value)
  const invalidModels = oauthAllowedModels.value.filter(model => !allowedSet.has(model))
  if (invalidModels.length > 0) {
    appStore.showError(
      t('admin.accounts.userModelSelectionInvalid', { models: invalidModels.join(', ') })
    )
    return false
  }
  return true
}

const importLimit = computed(() => {
  const configured = Number(appStore.cachedPublicSettings?.user_account_import_limit)
  return Number.isFinite(configured) && configured > 0
    ? Math.floor(configured)
    : PERSONAL_ACCOUNT_IMPORT_LIMIT
})

const openAIAccountLevelConfigs = computed(() => appStore.cachedPublicSettings?.openai_account_levels)
const isAgentIdentityImport = computed(() =>
  selectedPlatform.value === 'openai' && selectedOpenAIAuthMode.value === 'agent_identity'
)
const isPersonalAccessTokenImport = computed(() =>
  selectedPlatform.value === 'openai' && selectedOpenAIAuthMode.value === 'personal_access_token'
)

// OAuth 登录流程只在用户显式切换时进入，不再因 pro 等级自动触发。
const requiresOAuthLogin = computed(() => selectedImportFlow.value === 'oauth_login')

const requiresPersonalAccessTokenProxy = computed(() =>
  isPersonalAccessTokenImport.value &&
  selectableOpenAIAccountLevels(openAIAccountLevelConfigs.value)
    .some(level => level.key === selectedAccountLevel.value && level.requires_proxy_login)
)

// OpenAI OAuth 凭证导入时，pro 等需要代理登录的等级要求绑定代理。
const requiresOpenAIOAuthCredentialProxy = computed(() =>
  selectedPlatform.value === 'openai' &&
  selectedOpenAIAuthMode.value === 'oauth' &&
  selectableOpenAIAccountLevels(openAIAccountLevelConfigs.value)
    .some(level => level.key === selectedAccountLevel.value && level.requires_proxy_login)
)

const requiresCredentialImportProxy = computed(() =>
  selectedPlatform.value === 'anthropic' ||
  selectedPlatform.value === 'gemini' ||
  selectedPlatform.value === 'antigravity' ||
  selectedPlatform.value === 'grok' ||
  requiresPersonalAccessTokenProxy.value ||
  requiresOpenAIOAuthCredentialProxy.value
)

const canSubmitCredentialImport = computed(() => {
  if (!selectedPlatform.value) return false
  if (selectedPlatform.value === 'grok' && !selectedAccountLevel.value) return false
  if (selectedPlatform.value === 'openai') {
    if (isAgentIdentityImport.value) return true
    if (!selectedAccountLevel.value) return false
    if (requiresCredentialImportProxy.value) {
      return Boolean(selectedProxyId.value && !selectedProxyCapacityMessage.value)
    }
    return true
  }
  if (requiresCredentialImportProxy.value) {
    return Boolean(selectedProxyId.value && !selectedProxyCapacityMessage.value)
  }
  return true
})

const importHintText = computed(() =>
  isAgentIdentityImport.value
    ? t('userAccounts.importHintAgentIdentity')
    : isPersonalAccessTokenImport.value
      ? t('userAccounts.importHintPersonalAccessToken')
    : t('userAccounts.importHint')
)

const importWarningText = computed(() => {
  if (isAgentIdentityImport.value) {
    return t('userAccounts.importWarningAgentIdentity', { max: importLimit.value })
  }
  if (isPersonalAccessTokenImport.value) {
    return t('userAccounts.importWarningPersonalAccessToken', { max: importLimit.value })
  }
  switch (selectedPlatform.value) {
    case 'openai':
      return t('userAccounts.importWarningOpenAI', { max: importLimit.value })
    case 'anthropic':
      return t('userAccounts.importWarningClaude', { max: importLimit.value })
    case 'gemini':
      return t('userAccounts.importWarningGemini', { max: importLimit.value })
    case 'antigravity':
      return t('userAccounts.importWarningAntigravity', { max: importLimit.value })
    case 'grok':
      return t('userAccounts.importWarningGrok', { max: importLimit.value })
    case 'opencode':
      return t('userAccounts.importWarningOpencode', { max: importLimit.value })
    default:
      return t('userAccounts.importWarningChoosePlatform', { max: importLimit.value })
  }
})

const importTextHint = computed(() => {
  if (isAgentIdentityImport.value) {
    return t('userAccounts.importTextHintAgentIdentity')
  }
  if (isPersonalAccessTokenImport.value) {
    return t('userAccounts.importTextHintPersonalAccessToken')
  }
  switch (selectedPlatform.value) {
    case 'openai':
      return t('userAccounts.importTextHintOpenAI')
    case 'anthropic':
      return t('userAccounts.importTextHintClaude')
    case 'gemini':
      return t('userAccounts.importTextHintGemini')
    case 'antigravity':
      return t('userAccounts.importTextHintAntigravity')
    case 'grok':
      return t('userAccounts.importTextHintGrok')
    case 'opencode':
      return t('userAccounts.importTextHintOpencode')
    default:
      return t('userAccounts.importTextHintChoosePlatform')
  }
})

const importTextPlaceholder = computed(() => {
  if (isAgentIdentityImport.value) return t('userAccounts.importTextPlaceholderAgentIdentity')
  if (isPersonalAccessTokenImport.value) return t('userAccounts.importTextPlaceholderPersonalAccessToken')
  if (selectedPlatform.value === 'opencode') return t('userAccounts.importTextPlaceholderOpencode')
  return t('userAccounts.importTextPlaceholder')
})

const importFileAccept = computed(() =>
  isAgentIdentityImport.value || isPersonalAccessTokenImport.value
    ? 'application/json,.json'
    : 'application/json,text/plain,.json,.txt'
)

const importAllowedExtensions = computed(() =>
  isAgentIdentityImport.value || isPersonalAccessTokenImport.value
    ? ['.json']
    : ['.json', '.txt']
)

const selectedPlatformHint = computed(() => {
  switch (selectedPlatform.value) {
    case 'anthropic':
      return t('userAccounts.importPlatformHintClaude')
    case 'gemini':
      return t('userAccounts.importPlatformHintGemini')
    case 'antigravity':
      return t('userAccounts.importPlatformHintAntigravity')
    case 'grok':
      return t('userAccounts.importPlatformHintGrok')
    case 'opencode':
      return t('userAccounts.importPlatformHintOpencode')
    default:
      return ''
  }
})

const selectedProxy = computed(() => {
  const proxyId = selectedProxyId.value
  if (!proxyId) return null
  return proxies.value.find(proxy => proxy.id === proxyId) || null
})

const selectedProxyCapacityMessage = computed(() => {
  const proxy = selectedProxy.value
  if (!isProxyAccountFull(proxy)) return ''
  const count = normalizeProxyAccountCount(proxy)
  const max = normalizeProxyMaxAccounts(proxy)
  return t('admin.proxies.accountUsageFullSelectOther', { count, max })
})

const proxyHelperText = computed(() => {
  if (proxyLoading.value) return t('userAccounts.importProxyLoading')
  if (proxyLoadMessage.value) return proxyLoadMessage.value
  if (selectedProxyCapacityMessage.value) return selectedProxyCapacityMessage.value
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
    !selectedProxyCapacityMessage.value &&
    openaiOAuth.sessionId.value &&
    String(authCode).trim() &&
    String(oauthState).trim() &&
    oauthModelSelectionValid.value
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
      { value: 'antigravity', label: 'Antigravity', desc: t('userAccounts.importPlatformAntigravity') },
      { value: 'grok', label: 'Grok', desc: t('userAccounts.importPlatformGrok') },
      { value: 'opencode', label: 'OpenCode', desc: t('userAccounts.importPlatformOpencode') }
    ]
    return () => h('div', { class: 'space-y-2' }, [
      h('label', { class: 'input-label' }, t('userAccounts.importPlatform')),
      h('div', { class: 'grid grid-cols-[repeat(auto-fit,minmax(7.5rem,1fr))] gap-2' }, options.map(option =>
        h(
          'button',
          {
            type: 'button',
            'aria-pressed': props.selectedPlatform === option.value,
            class: [
              'flex min-h-[76px] cursor-pointer flex-col justify-center rounded-lg border px-3 py-2 text-left transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary-500/50 focus-visible:ring-offset-2 dark:focus-visible:ring-offset-dark-800',
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

const OpenAIAuthModeSelector = defineComponent({
  name: 'UserImportOpenAIAuthModeSelector',
  props: {
    selectedMode: {
      type: String,
      default: 'oauth'
    }
  },
  emits: ['select'],
  setup(props, { emit }) {
    const options: Array<{ value: OpenAIImportAuthMode; label: string; desc: string }> = [
      {
        value: 'oauth',
        label: t('userAccounts.importAuthModeOAuth'),
        desc: t('userAccounts.importAuthModeOAuthDesc')
      },
      {
        value: 'personal_access_token',
        label: t('userAccounts.importAuthModePersonalAccessToken'),
        desc: t('userAccounts.importAuthModePersonalAccessTokenDesc')
      },
      {
        value: 'agent_identity',
        label: t('userAccounts.importAuthModeAgentIdentity'),
        desc: t('userAccounts.importAuthModeAgentIdentityDesc')
      }
    ]
    return () => h('fieldset', { class: 'space-y-2' }, [
      h('legend', { class: 'input-label' }, t('userAccounts.importAuthMode')),
      h('div', { class: 'grid grid-cols-1 gap-2 sm:grid-cols-3' }, options.map(option => {
        const descriptionId = `openai-import-auth-mode-${option.value}-description`
        return h('label', { class: 'block cursor-pointer' }, [
          h('input', {
            type: 'radio',
            name: 'openai-import-auth-mode',
            value: option.value,
            checked: props.selectedMode === option.value,
            class: 'peer sr-only',
            'aria-describedby': descriptionId,
            onChange: () => emit('select', option.value)
          }),
          h('span', {
            class: [
              'flex min-h-[76px] flex-col justify-center rounded-lg border px-3 py-2 text-left transition-colors peer-focus-visible:outline-none peer-focus-visible:ring-2 peer-focus-visible:ring-primary-500/50 peer-focus-visible:ring-offset-2 dark:peer-focus-visible:ring-offset-dark-800',
              props.selectedMode === option.value
                ? 'border-primary-400 bg-primary-50 text-primary-700 dark:border-primary-500 dark:bg-primary-900/30 dark:text-primary-300'
                : 'border-gray-200 bg-white text-gray-700 hover:bg-gray-50 dark:border-dark-700 dark:bg-dark-800 dark:text-dark-200 dark:hover:bg-dark-700'
            ]
          }, [
            h('span', { class: 'text-sm font-semibold' }, option.label),
            h('span', { id: descriptionId, class: 'mt-1 text-sm text-gray-500 dark:text-dark-400' }, option.desc)
          ])
        ])
      }))
    ])
  }
})

const AccountLevelSelector = defineComponent({
  name: 'UserImportAccountLevelSelector',
  props: {
    platform: {
      type: String,
      required: true
    },
    selectedLevel: {
      type: String,
      default: ''
    }
  },
  emits: ['select'],
  setup(props, { emit }) {
    const appStore = useAppStore()
    const options = computed<Array<{ value: SelectableImportLevel; label: string; desc: string }>>(() => {
      if (props.platform === 'grok') {
        return GROK_ACCOUNT_LEVEL_OPTIONS.map(option => ({
          value: option.value,
          label: option.label,
          desc: t(`admin.accounts.grokAccountLevel.${option.value}Description`)
        }))
      }
      return selectableOpenAIAccountLevels(appStore.cachedPublicSettings?.openai_account_levels).map(level => ({
        value: level.key as SelectableImportLevel,
        label: level.label,
        desc: level.requires_proxy_login
          ? t('userAccounts.importLevelRequiresProxy', { level: level.label })
          : t('userAccounts.importLevelDirect', { level: level.label })
      }))
    })
    return () => h('div', { class: 'space-y-2' }, [
      h('div', { class: 'flex items-center justify-between gap-3' }, [
        h(
          'label',
          { class: 'input-label mb-0' },
          props.platform === 'grok'
            ? t('userAccounts.importGrokAccountLevel')
            : t('userAccounts.importAccountLevel')
        ),
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
      h('div', { class: props.platform === 'grok' ? 'grid grid-cols-1 gap-2 sm:grid-cols-2' : 'grid gap-2 sm:grid-cols-4' }, options.value.map(option =>
        h(
          'button',
          {
            type: 'button',
            'aria-pressed': props.selectedLevel === option.value,
            class: [
              'flex min-h-[76px] cursor-pointer flex-col justify-center rounded-lg border px-3 py-2 text-left transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary-500/50 focus-visible:ring-offset-2 dark:focus-visible:ring-offset-dark-800',
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
      h(
        'p',
        { class: 'input-hint' },
        props.platform === 'grok'
          ? t('userAccounts.importGrokAccountLevelHint')
          : t('userAccounts.importAccountLevelHint')
      )
    ])
  }
})

watch(
  () => selectedPlatform.value,
  () => {
    selectedOpenAIAuthMode.value = 'oauth'
    selectedAccountLevel.value = ''
    selectedProxyId.value = null
    oauthAccountName.value = ''
    proxyLoadMessage.value = ''
    openaiOAuth.error.value = ''
    openaiOAuth.resetState()
    oauthFlowRef.value?.reset()
    if (requiresCredentialImportProxy.value) {
      loadProxies()
    }
  }
)

watch(
  () => selectedAccountLevel.value,
  () => {
    proxyLoadMessage.value = ''
    openaiOAuth.error.value = ''
    openaiOAuth.resetState()
    oauthFlowRef.value?.reset()
    if (requiresCredentialImportProxy.value || requiresOAuthLogin.value) {
      loadProxies()
    } else {
      selectedProxyId.value = null
      oauthAccountName.value = ''
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

watch(
  [() => props.show, requiresOAuthLogin, () => selectedPlatform.value],
  () => {
    void loadOAuthModelOptions()
  },
  { immediate: true }
)

function selectPlatform(platform: ImportPlatform): void {
  selectedPlatform.value = platform
  selectedImportFlow.value = 'credential'
}

function selectOpenAIAuthMode(mode: OpenAIImportAuthMode): void {
  if (selectedOpenAIAuthMode.value === mode) return
  selectedOpenAIAuthMode.value = mode
  selectedImportFlow.value = 'credential'
  selectedAccountLevel.value = ''
  selectedProxyId.value = null
  oauthAccountName.value = ''
  oauthAllowedModels.value = []
  oauthModelOptions.value = null
  oauthModelOptionsLoading.value = false
  oauthModelOptionsLoadError.value = ''
  oauthModelOptionsRequestVersion++
  proxyLoadMessage.value = ''
  openaiOAuth.error.value = ''
  openaiOAuth.resetState()
  oauthFlowRef.value?.reset()
}

function selectAccountLevel(level: SelectableImportLevel | ''): void {
  selectedAccountLevel.value = level
}

function switchToOAuthLogin(): void {
  selectedImportFlow.value = 'oauth_login'
  openaiOAuth.error.value = ''
  openaiOAuth.resetState()
  oauthFlowRef.value?.reset()
  loadProxies(true)
}

function switchToCredentialImport(): void {
  selectedImportFlow.value = 'credential'
}

function resetOAuthImportState(): void {
  selectedPlatform.value = ''
  selectedOpenAIAuthMode.value = 'oauth'
  selectedAccountLevel.value = ''
  selectedProxyId.value = null
  oauthAccountName.value = ''
  proxyLoadMessage.value = ''
  openaiOAuth.resetState()
  oauthFlowRef.value?.reset()
}

function isValidJSONDocument(content: string): boolean {
  try {
    const parsed: unknown = JSON.parse(content)
    return parsed !== null && typeof parsed === 'object'
  } catch {
    return false
  }
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
  if ((isAgentIdentityImport.value || isPersonalAccessTokenImport.value) && !contents.every(isValidJSONDocument)) {
    const message = isPersonalAccessTokenImport.value
      ? t('userAccounts.importPersonalAccessTokenJSONRequired')
      : t('userAccounts.importAgentIdentityJSONRequired')
    appStore.showError(message)
    return Promise.reject(new Error(message))
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
    request.openai_auth_mode = selectedOpenAIAuthMode.value
    if (isAgentIdentityImport.value) {
      return accountsAPI.importCredentialContents(request)
    }
    const accountLevel = selectedAccountLevel.value
    if (!accountLevel) {
      appStore.showError(t('userAccounts.importAccountLevelRequired'))
      return Promise.reject(new Error(t('userAccounts.importAccountLevelRequired')))
    }
    request.account_level = accountLevel
  } else if (selectedPlatform.value === 'grok') {
    const accountLevel = selectedAccountLevel.value
    if (!accountLevel) {
      appStore.showError(t('userAccounts.importGrokAccountLevelRequired'))
      return Promise.reject(new Error(t('userAccounts.importGrokAccountLevelRequired')))
    }
    request.account_level = accountLevel
  }
  if (requiresCredentialImportProxy.value) {
    if (!selectedProxyId.value) {
      appStore.showError(t('userAccounts.importProxyRequired'))
      return Promise.reject(new Error(t('userAccounts.importProxyRequired')))
    }
    if (selectedProxyCapacityMessage.value) {
      appStore.showError(selectedProxyCapacityMessage.value)
      return Promise.reject(new Error(selectedProxyCapacityMessage.value))
    }
    request.proxy_id = selectedProxyId.value
  }
  return accountsAPI.importCredentialContents(request)
}

// 用户只能选择平台代理：按当前平台/等级拉取可选代理。scope 变化用不同缓存键，
// 保证切换平台或等级后（watcher 会重新调用 loadProxies）能取到对应的代理集合。
//
// 用请求序号而不是「有请求在飞就返回」来去重：快速连续切换平台/等级时，
// 后一次请求必须能覆盖前一次，否则列表会停在上一个平台上（而 scope 键还记成了旧值）。
let lastProxyScopeKey = ''
let proxyRequestSeq = 0
async function loadProxies(force = false): Promise<void> {
  const scope = {
    platform: selectedPlatform.value || '',
    account_level: selectedAccountLevel.value || ''
  }
  const scopeKey = `${scope.platform}|${scope.account_level}`
  if (!force && scopeKey === lastProxyScopeKey && proxies.value.length > 0) {
    // 同上：命中缓存时作废在飞的旧 scope 请求，并收掉它已经不会再清的 loading 标志。
    proxyRequestSeq++
    proxyLoading.value = false
    return
  }
  const seq = ++proxyRequestSeq
  proxyLoading.value = true
  proxyLoadMessage.value = ''
  try {
    const list = await accountShareAPI.listProxies(scope)
    if (seq !== proxyRequestSeq) return
    proxies.value = list
    lastProxyScopeKey = scopeKey
    // 换了范围之后，之前选中的代理可能已经不在列表里，必须丢弃：
    // 后端会按同样的 scope 校验，越界的 proxy_id 只会在提交时才报错。
    if (selectedProxyId.value && !list.some(proxy => proxy.id === selectedProxyId.value)) {
      selectedProxyId.value = null
    }
  } catch (error: unknown) {
    if (seq !== proxyRequestSeq) return
    proxyLoadMessage.value = extractApiErrorMessage(error, t('userAccounts.importProxyLoadFailed'))
  } finally {
    if (seq === proxyRequestSeq) {
      proxyLoading.value = false
    }
  }
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
  if (selectedProxyCapacityMessage.value) {
    appStore.showError(selectedProxyCapacityMessage.value)
    return
  }
  if (!validateOAuthModelSelection()) {
    return
  }
  await openaiOAuth.generateAuthUrl(selectedProxyId.value, { accountLevel: selectedAccountLevel.value })
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
  if (selectedProxyCapacityMessage.value) {
    appStore.showError(selectedProxyCapacityMessage.value)
    return
  }
  if (!validateOAuthModelSelection()) {
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
      selectedProxyId.value,
      selectedAccountLevel.value
    )
    if (!tokenInfo) return

    const templated = applyPersonalAccountTemplate(
      'openai',
      {
        ...openaiOAuth.buildCredentials(tokenInfo),
        model_mapping: Object.fromEntries(
          oauthAllowedModels.value.map(model => [model, model])
        )
      },
      openaiOAuth.buildExtraInfo(tokenInfo)
    )
    await accountsAPI.create({
      name: oauthAccountName.value || tokenInfo.email || `${openAIAccountLevelLabel(selectedAccountLevel.value, openAIAccountLevelConfigs.value)} OpenAI`,
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
