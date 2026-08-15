import { ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAppStore } from '@/stores/app'
import { adminAPI } from '@/api/admin'
import { accountsAPI } from '@/api/accounts'
import type { GrokTokenInfo } from '@/api/admin/grok'
import type { AccountApiScope } from '@/composables/useAccountOAuth'
import type { AccountLevel } from '@/types'
import { extractApiErrorMessage, extractI18nErrorMessage } from '@/utils/apiError'

export function useGrokOAuth(scope: AccountApiScope = 'admin') {
  const appStore = useAppStore()
  const { t } = useI18n()

  const authUrl = ref('')
  const sessionId = ref('')
  const state = ref('')
  const loading = ref(false)
  const error = ref('')
  const passwordAuthEnabled = ref(false)

  const resetState = () => {
    authUrl.value = ''
    sessionId.value = ''
    state.value = ''
    loading.value = false
    error.value = ''
  }

  // Capability discovery is fail-closed: an unavailable/old backend must not
  // make the password form appear. Password auth is administrator-only.
  const loadCapabilities = async (): Promise<boolean> => {
    passwordAuthEnabled.value = false
    if (scope !== 'admin') return false
    try {
      const capabilities = await adminAPI.grok.getCapabilities()
      passwordAuthEnabled.value = capabilities.password_auth_enabled === true
    } catch {
      passwordAuthEnabled.value = false
    }
    return passwordAuthEnabled.value
  }

  const generateAuthUrl = async (
    proxyId: number | null | undefined,
    options?: { accountLevel?: AccountLevel }
  ): Promise<boolean> => {
    loading.value = true
    authUrl.value = ''
    sessionId.value = ''
    state.value = ''
    error.value = ''

    try {
      const payload: Record<string, unknown> = {}
      if (proxyId) payload.proxy_id = proxyId
      if (options?.accountLevel) payload.account_level = options.accountLevel

      const response =
        scope === 'user'
          ? await accountsAPI.generateGrokOAuthUrl(payload)
          : await adminAPI.grok.generateAuthUrl(payload)
      authUrl.value = response.auth_url
      sessionId.value = response.session_id
      state.value = response.state
      return true
    } catch (err: unknown) {
      error.value = extractApiErrorMessage(err, t('admin.accounts.oauth.grok.failedToGenerateUrl'))
      appStore.showError(error.value)
      return false
    } finally {
      loading.value = false
    }
  }

  const exchangeAuthCode = async (params: {
    code: string
    sessionId: string
    state: string
    proxyId?: number | null
    accountLevel?: AccountLevel
  }): Promise<GrokTokenInfo | null> => {
    const code = params.code?.trim()
    if (!code || !params.sessionId || !params.state) {
      error.value = t('admin.accounts.oauth.grok.missingExchangeParams')
      return null
    }

    loading.value = true
    error.value = ''

    try {
      const payload: Record<string, unknown> = {
        session_id: params.sessionId,
        state: params.state,
        code
      }
      if (params.proxyId) payload.proxy_id = params.proxyId
      if (params.accountLevel) payload.account_level = params.accountLevel

      return scope === 'user'
        ? await accountsAPI.exchangeGrokOAuthCode(payload as any)
        : await adminAPI.grok.exchangeCode(payload as any)
    } catch (err: unknown) {
      error.value = extractI18nErrorMessage(
        err,
        t,
        'admin.accounts.oauth.grok.errors',
        t('admin.accounts.oauth.grok.failedToExchangeCode')
      )
      appStore.showError(error.value)
      return null
    } finally {
      loading.value = false
    }
  }

  const validateRefreshToken = async (
    refreshToken: string,
    proxyId?: number | null
  ): Promise<GrokTokenInfo | null> => {
    if (!refreshToken.trim()) {
      error.value = t('admin.accounts.oauth.grok.pleaseEnterRefreshToken')
      return null
    }

    loading.value = true
    error.value = ''

    try {
      return scope === 'user'
        ? await accountsAPI.refreshGrokToken(refreshToken.trim(), proxyId)
        : await adminAPI.grok.refreshGrokToken(refreshToken.trim(), proxyId)
    } catch (err: unknown) {
      error.value = extractI18nErrorMessage(
        err,
        t,
        'admin.accounts.oauth.grok.errors',
        t('admin.accounts.oauth.grok.failedToValidateRT')
      )
      return null
    } finally {
      loading.value = false
    }
  }

  const validateSSOToken = async (
    ssoToken: string,
    proxyId?: number | null
  ): Promise<GrokTokenInfo | null> => {
    if (scope !== 'admin') return null
    const normalized = ssoToken.trim()
    if (!normalized) {
      error.value = t('admin.accounts.oauth.grok.pleaseEnterSSOToken')
      return null
    }
    loading.value = true
    error.value = ''
    try {
      return await adminAPI.grok.validateSSOToken(normalized, proxyId)
    } catch (err: unknown) {
      error.value = extractI18nErrorMessage(
        err,
        t,
        'admin.accounts.oauth.grok.errors',
        t('admin.accounts.oauth.grok.failedToValidateSSO')
      )
      appStore.showError(error.value)
      return null
    } finally {
      loading.value = false
    }
  }

  const authorizePassword = async (
    email: string,
    password: string,
    proxyId?: number | null
  ): Promise<GrokTokenInfo | null> => {
    if (scope !== 'admin' || !passwordAuthEnabled.value) {
      error.value = t('admin.accounts.oauth.grok.passwordAuthDisabled')
      return null
    }
    const normalizedEmail = email.trim()
    if (!normalizedEmail || !password) {
      error.value = t('admin.accounts.oauth.grok.emailPasswordRequired')
      return null
    }
    loading.value = true
    error.value = ''
    try {
      return await adminAPI.grok.authorizePassword(normalizedEmail, password, proxyId)
    } catch (err: unknown) {
      error.value = extractI18nErrorMessage(
        err,
        t,
        'admin.accounts.oauth.grok.errors',
        t('admin.accounts.oauth.grok.failedToAuthorizePassword')
      )
      appStore.showError(error.value)
      return null
    } finally {
      loading.value = false
    }
  }

  const buildCredentials = (tokenInfo: GrokTokenInfo): Record<string, unknown> => {
    const credentials: Record<string, unknown> = {
      access_token: tokenInfo.access_token,
      token_type: tokenInfo.token_type,
      expires_at: tokenInfo.expires_at,
      client_id: tokenInfo.client_id,
      scope: tokenInfo.scope,
      email: tokenInfo.email,
      sub: tokenInfo.sub,
      team_id: tokenInfo.team_id,
      subscription_tier: tokenInfo.subscription_tier,
      entitlement_status: tokenInfo.entitlement_status
    }
    if (tokenInfo.refresh_token) credentials.refresh_token = tokenInfo.refresh_token
    if (tokenInfo.id_token) credentials.id_token = tokenInfo.id_token
    return Object.fromEntries(Object.entries(credentials).filter(([, value]) => value !== undefined && value !== ''))
  }

  const buildExtraInfo = (tokenInfo: GrokTokenInfo): Record<string, unknown> => {
    const extra: Record<string, unknown> = {}
    if (tokenInfo.email) extra.email = tokenInfo.email
    if (tokenInfo.subscription_tier) extra.subscription_tier = tokenInfo.subscription_tier
    if (tokenInfo.entitlement_status) extra.entitlement_status = tokenInfo.entitlement_status
    return extra
  }

  return {
    authUrl,
    sessionId,
    state,
    loading,
    error,
    passwordAuthEnabled,
    resetState,
    loadCapabilities,
    generateAuthUrl,
    exchangeAuthCode,
    validateRefreshToken,
    validateSSOToken,
    authorizePassword,
    buildCredentials,
    buildExtraInfo
  }
}
