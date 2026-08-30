import { defineComponent } from 'vue'
import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'

const {
  createAccountMock,
  exchangeAuthCodeMock,
  getModelOptionsMock,
  importCredentialContentsMock,
  listProxiesMock,
  resetOAuthStateMock,
  showErrorMock,
  showSuccessMock,
  showWarningMock
} = vi.hoisted(() => ({
  createAccountMock: vi.fn(),
  exchangeAuthCodeMock: vi.fn(),
  getModelOptionsMock: vi.fn(),
  importCredentialContentsMock: vi.fn(),
  listProxiesMock: vi.fn(),
  resetOAuthStateMock: vi.fn(),
  showErrorMock: vi.fn(),
  showSuccessMock: vi.fn(),
  showWarningMock: vi.fn()
}))

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string, params?: Record<string, unknown>) =>
        params ? `${key}:${JSON.stringify(params)}` : key
    })
  }
})

vi.mock('@/api', () => ({
  accountsAPI: {
    create: createAccountMock,
    getModelOptions: getModelOptionsMock,
    importCredentialContents: importCredentialContentsMock
  },
  accountShareAPI: {
    listProxies: listProxiesMock
  }
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({
    cachedPublicSettings: {},
    showError: showErrorMock,
    showSuccess: showSuccessMock,
    showWarning: showWarningMock
  })
}))

vi.mock('@/composables/useOpenAIOAuth', async () => {
  const { ref } = await vi.importActual<typeof import('vue')>('vue')
  return {
    useOpenAIOAuth: () => ({
      authUrl: ref(''),
      sessionId: ref('oauth-session'),
      loading: ref(false),
      error: ref(''),
      oauthState: ref('oauth-state'),
      resetState: resetOAuthStateMock,
      generateAuthUrl: vi.fn(),
      exchangeAuthCode: exchangeAuthCodeMock,
      buildCredentials: vi.fn(() => ({ access_token: 'oauth-access-token' })),
      buildExtraInfo: vi.fn(() => ({}))
    })
  }
})

import CredentialImportModal from '@/components/account/CredentialImportModal.vue'
import ImportAccountsModal from '@/components/user/ImportAccountsModal.vue'
import en from '@/i18n/locales/en'
import zh from '@/i18n/locales/zh'

const BaseDialogStub = defineComponent({
  name: 'BaseDialog',
  props: {
    show: {
      type: Boolean,
      default: false
    }
  },
  template: '<div v-if="show"><slot /><slot name="footer" /></div>'
})

const CredentialImportModalStub = defineComponent({
  name: 'CredentialImportModal',
  props: {
    show: Boolean,
    importer: {
      type: Function,
      required: true
    },
    fileAccept: String,
    allowedExtensions: Array,
    textPlaceholder: String,
    submitDisabled: Boolean
  },
  template: '<div v-if="show"><slot name="controls" /></div>'
})

const ModelWhitelistSelectorStub = defineComponent({
  name: 'ModelWhitelistSelector',
  props: {
    modelValue: { type: Array, default: () => [] },
    allowedOptions: { type: Array, default: () => [] },
    allowCustom: { type: Boolean, default: true }
  },
  emits: ['update:modelValue'],
  template: `
    <div data-testid="import-model-selector" :data-allow-custom="String(allowCustom)">
      <span data-testid="import-selected-models">{{ modelValue.join(',') }}</span>
      <button type="button" data-testid="clear-import-models" @click="$emit('update:modelValue', [])">clear</button>
    </div>
  `
})

const ProxySelectorStub = defineComponent({
  name: 'ProxySelector',
  props: {
    modelValue: { type: Number, default: null },
    proxies: { type: Array, default: () => [] }
  },
  emits: ['update:modelValue'],
  template: '<button type="button" data-testid="select-import-proxy" @click="$emit(\'update:modelValue\', 9)">proxy</button>'
})

const OAuthAuthorizationFlowStub = defineComponent({
  name: 'OAuthAuthorizationFlow',
  setup(_props, { expose }) {
    expose({
      authCode: 'oauth-auth-code',
      oauthState: 'oauth-state',
      reset: vi.fn()
    })
    return () => null
  }
})

const basicStubs = {
  BaseDialog: BaseDialogStub,
  CredentialImportModal: CredentialImportModalStub,
  ProxySelector: true,
  OAuthAuthorizationFlow: true,
  Icon: true
}

function findButtonByText(wrapper: ReturnType<typeof mount>, text: string) {
  const button = wrapper.findAll('button').find(item => item.text().includes(text))
  expect(button, `button text "${text}"`).toBeTruthy()
  return button!
}

async function selectOpenAI(wrapper: ReturnType<typeof mount>): Promise<void> {
  await findButtonByText(wrapper, 'OpenAI').trigger('click')
  await wrapper.vm.$nextTick()
}

describe('ImportAccountsModal Agent Identity', () => {
  beforeEach(() => {
    createAccountMock.mockReset()
    createAccountMock.mockResolvedValue({ id: 1 })
    exchangeAuthCodeMock.mockReset()
    exchangeAuthCodeMock.mockResolvedValue({ email: 'owner@example.com' })
    getModelOptionsMock.mockReset()
    getModelOptionsMock.mockResolvedValue({ models: ['gpt-5.2', 'gpt-5.2-2025-12-11'] })
    importCredentialContentsMock.mockReset()
    importCredentialContentsMock.mockResolvedValue({
      total: 1,
      created: 1,
      updated: 0,
      failed: 0,
      errors: []
    })
    resetOAuthStateMock.mockReset()
    listProxiesMock.mockReset()
    listProxiesMock.mockResolvedValue([])
    showErrorMock.mockReset()
    showSuccessMock.mockReset()
    showWarningMock.mockReset()
  })

  it('shows the Free fallback copy and preserves the server-side account-level detection request', async () => {
    const wrapper = mount(ImportAccountsModal, {
      props: { show: true },
      global: { stubs: basicStubs }
    })

    await selectOpenAI(wrapper)

    const oauthRadio = wrapper.get('input[value="oauth"]')
    expect((oauthRadio.element as HTMLInputElement).checked).toBe(true)
    expect(wrapper.text()).toContain('userAccounts.importAccountLevel')

    const freeFallbackButton = findButtonByText(wrapper, 'userAccounts.importLevelFreeLabel')
    expect(
      freeFallbackButton.findAll('span').some(item => item.text() === 'userAccounts.importLevelFree')
    ).toBe(true)
    expect(zh.userAccounts.importLevelFreeLabel).toBe('自动识别 / Free 兜底')
    expect(zh.userAccounts.importLevelFree).toContain('保存服务端识别到的真实等级')
    expect(zh.userAccounts.importLevelFree).toContain('无精确池时才落入 Free 池')
    expect(zh.userAccounts.importAccountLevelHint).toContain('其他显式等级仍严格匹配')
    expect(zh.userAccounts.importAccountLevelHint).toContain('例如 Pro')
    expect(en.userAccounts.importLevelFreeLabel).toBe('Auto-detect / Free fallback')
    expect(en.userAccounts.importLevelFree).toContain('actual level detected by the server')
    expect(en.userAccounts.importLevelFree).toContain('only when no exact pool is available')
    expect(en.userAccounts.importAccountLevelHint).toContain('other explicit levels still require an exact match')
    expect(en.userAccounts.importAccountLevelHint).toContain('for example, Pro')

    await freeFallbackButton.trigger('click')
    const importer = wrapper.getComponent(CredentialImportModalStub).props('importer') as (contents: string[]) => Promise<unknown>
    await importer(['refresh-token'])

    expect(importCredentialContentsMock).toHaveBeenCalledWith(expect.objectContaining({
      contents: ['refresh-token'],
      platform: 'openai',
      openai_auth_mode: 'oauth',
      account_level: 'free',
      share_mode: 'private'
    }))
  })

  it('imports Codex PAT export JSON with an explicit auth mode and account level', async () => {
    const wrapper = mount(ImportAccountsModal, {
      props: { show: true },
      global: { stubs: basicStubs }
    })

    await selectOpenAI(wrapper)
    await wrapper.get('input[value="personal_access_token"]').setValue()
    await wrapper.vm.$nextTick()

    expect(wrapper.text()).toContain('userAccounts.importAccountLevel')
    await findButtonByText(wrapper, 'Team').trigger('click')
    const importModal = wrapper.getComponent(CredentialImportModalStub)
    expect(importModal.props('fileAccept')).toBe('application/json,.json')
    expect(importModal.props('allowedExtensions')).toEqual(['.json'])
    expect(importModal.props('textPlaceholder')).toBe('userAccounts.importTextPlaceholderPersonalAccessToken')

    const importer = importModal.props('importer') as (contents: string[]) => Promise<unknown>
    await importer(['{"accounts":[{"platform":"openai","type":"oauth","credentials":{"access_token":"at-test-token","auth_mode":"personalAccessToken"}}]}'])

    expect(importCredentialContentsMock).toHaveBeenCalledWith(expect.objectContaining({
      platform: 'openai',
      openai_auth_mode: 'personal_access_token',
      account_level: 'team',
      share_mode: 'private'
    }))
  })

  it('requires a proxy for PAT levels configured for proxy login', async () => {
    const wrapper = mount(ImportAccountsModal, {
      props: { show: true },
      global: { stubs: basicStubs }
    })

    await selectOpenAI(wrapper)
    await wrapper.get('input[value="personal_access_token"]').setValue()
    await findButtonByText(wrapper, 'Pro').trigger('click')
    await wrapper.vm.$nextTick()

    expect(wrapper.findComponent({ name: 'ProxySelector' }).exists()).toBe(true)
    expect(wrapper.getComponent(CredentialImportModalStub).props('submitDisabled')).toBe(true)
  })

  it('allows OAuth credential import for a proxy-login level without forcing account login', async () => {
    const wrapper = mount(ImportAccountsModal, {
      props: { show: true },
      global: { stubs: basicStubs }
    })

    await selectOpenAI(wrapper)
    await findButtonByText(wrapper, 'Pro').trigger('click')
    await wrapper.vm.$nextTick()

    // Pro 不再强制切换到 OAuth 登录，而是停留在凭证导入并要求选择代理。
    expect(wrapper.findComponent({ name: 'OAuthAuthorizationFlow' }).exists()).toBe(false)
    expect(wrapper.findComponent({ name: 'ProxySelector' }).exists()).toBe(true)
    expect(wrapper.getComponent(CredentialImportModalStub).props('submitDisabled')).toBe(true)
    expect(wrapper.text()).toContain('userAccounts.importSwitchToOAuthLogin')
  })

  it('submits the selected priced-model identity whitelist through OAuth account login import', async () => {
    listProxiesMock.mockResolvedValueOnce([
      { id: 9, name: 'proxy', status: 'active', account_count: 0, max_accounts: 10 }
    ])
    const wrapper = mount(ImportAccountsModal, {
      props: { show: true },
      global: {
        stubs: {
          ...basicStubs,
          ModelWhitelistSelector: ModelWhitelistSelectorStub,
          ProxySelector: ProxySelectorStub,
          OAuthAuthorizationFlow: OAuthAuthorizationFlowStub
        }
      }
    })

    await selectOpenAI(wrapper)
    await findButtonByText(wrapper, 'Free').trigger('click')
    await findButtonByText(wrapper, 'userAccounts.importSwitchToOAuthLogin').trigger('click')
    await flushPromises()

    expect(getModelOptionsMock).toHaveBeenCalledWith('openai')
    const selector = wrapper.getComponent(ModelWhitelistSelectorStub)
    expect(selector.props('allowedOptions')).toEqual(['gpt-5.2', 'gpt-5.2-2025-12-11'])
    expect(selector.props('allowCustom')).toBe(false)
    expect(wrapper.get('[data-testid="import-selected-models"]').text()).toBe(
      'gpt-5.2,gpt-5.2-2025-12-11'
    )

    await wrapper.get('[data-testid="select-import-proxy"]').trigger('click')
    const modelOptionCallsBeforeSubmit = getModelOptionsMock.mock.calls.length
    const resetCallsBeforeSubmit = resetOAuthStateMock.mock.calls.length
    await wrapper.get('#user-import-openai-oauth-form').trigger('submit.prevent')
    await flushPromises()

    expect(getModelOptionsMock.mock.calls.length).toBeGreaterThan(modelOptionCallsBeforeSubmit)
    expect(resetOAuthStateMock.mock.calls.length).toBeGreaterThan(resetCallsBeforeSubmit)
    expect(createAccountMock).toHaveBeenCalledWith(expect.objectContaining({
      platform: 'openai',
      type: 'oauth',
      credentials: expect.objectContaining({
        access_token: 'oauth-access-token',
        model_mapping: {
          'gpt-5.2': 'gpt-5.2',
          'gpt-5.2-2025-12-11': 'gpt-5.2-2025-12-11'
        }
      })
    }))
  })

  it('blocks OAuth exchange when the priced-model whitelist changes during authorization', async () => {
    listProxiesMock.mockResolvedValueOnce([
      { id: 9, name: 'proxy', status: 'active', account_count: 0, max_accounts: 10 }
    ])
    const wrapper = mount(ImportAccountsModal, {
      props: { show: true },
      global: {
        stubs: {
          ...basicStubs,
          ModelWhitelistSelector: ModelWhitelistSelectorStub,
          ProxySelector: ProxySelectorStub,
          OAuthAuthorizationFlow: OAuthAuthorizationFlowStub
        }
      }
    })

    await selectOpenAI(wrapper)
    await findButtonByText(wrapper, 'Free').trigger('click')
    await findButtonByText(wrapper, 'userAccounts.importSwitchToOAuthLogin').trigger('click')
    await flushPromises()
    await wrapper.get('[data-testid="select-import-proxy"]').trigger('click')

    const modelOptionCallsBeforeSubmit = getModelOptionsMock.mock.calls.length
    // 禁止把已经失效的选择静默裁剪后继续消费一次性 code。
    getModelOptionsMock.mockResolvedValueOnce({ models: ['gpt-5.2'] })
    await wrapper.get('#user-import-openai-oauth-form').trigger('submit.prevent')
    await flushPromises()

    expect(getModelOptionsMock.mock.calls.length).toBeGreaterThan(modelOptionCallsBeforeSubmit)
    expect(exchangeAuthCodeMock).not.toHaveBeenCalled()
    expect(createAccountMock).not.toHaveBeenCalled()
    expect(showErrorMock).toHaveBeenCalledWith(expect.stringContaining('gpt-5.2-2025-12-11'))
  })

  it('blocks OAuth account login import when the model whitelist is empty', async () => {
    listProxiesMock.mockResolvedValueOnce([
      { id: 9, name: 'proxy', status: 'active', account_count: 0, max_accounts: 10 }
    ])
    const wrapper = mount(ImportAccountsModal, {
      props: { show: true },
      global: {
        stubs: {
          ...basicStubs,
          ModelWhitelistSelector: ModelWhitelistSelectorStub,
          ProxySelector: ProxySelectorStub,
          OAuthAuthorizationFlow: OAuthAuthorizationFlowStub
        }
      }
    })

    await selectOpenAI(wrapper)
    await findButtonByText(wrapper, 'Free').trigger('click')
    await findButtonByText(wrapper, 'userAccounts.importSwitchToOAuthLogin').trigger('click')
    await flushPromises()
    await wrapper.get('[data-testid="select-import-proxy"]').trigger('click')
    await wrapper.get('[data-testid="clear-import-models"]').trigger('click')
    await wrapper.get('#user-import-openai-oauth-form').trigger('submit.prevent')
    await flushPromises()

    expect(exchangeAuthCodeMock).not.toHaveBeenCalled()
    expect(createAccountMock).not.toHaveBeenCalled()
    expect(showErrorMock).toHaveBeenCalledWith('admin.accounts.userModelSelectionRequired')
  })

  it('imports Agent Identity as private by default without account level or OAuth flow', async () => {
    const wrapper = mount(ImportAccountsModal, {
      props: { show: true },
      global: { stubs: basicStubs }
    })

    await selectOpenAI(wrapper)
    await wrapper.get('input[value="agent_identity"]').setValue()
    await wrapper.vm.$nextTick()

    expect((wrapper.get('input[value="agent_identity"]').element as HTMLInputElement).checked).toBe(true)
    const notice = wrapper.get('[data-testid="agent-identity-import-notice"]')
    expect(notice.text()).toContain('userAccounts.importAgentIdentityPrivate')
    expect(wrapper.text()).not.toContain('userAccounts.importAccountLevel')
    expect(wrapper.findComponent({ name: 'OAuthAuthorizationFlow' }).exists()).toBe(false)

    const importModal = wrapper.getComponent(CredentialImportModalStub)
    expect(importModal.props('fileAccept')).toBe('application/json,.json')
    expect(importModal.props('allowedExtensions')).toEqual(['.json'])
    expect(importModal.props('textPlaceholder')).toBe('userAccounts.importTextPlaceholderAgentIdentity')

    const importer = importModal.props('importer') as (contents: string[]) => Promise<unknown>
    await importer(['{"identity":"value"}'])

    const request = importCredentialContentsMock.mock.calls[0]?.[0]
    expect(request).toMatchObject({
      platform: 'openai',
      openai_auth_mode: 'agent_identity',
      share_mode: 'private'
    })
    expect(request).not.toHaveProperty('account_level')
    expect(request).not.toHaveProperty('proxy_id')
  })

  it('explains the private default and validated public transition in both locales', () => {
    expect(zh.userAccounts.importHintAgentIdentity).toContain('默认为')
    expect(zh.userAccounts.importAgentIdentityPrivate).toContain('切换为公共')
    expect(zh.userAccounts.importAgentIdentityPrivate).toContain('校验')

    expect(en.userAccounts.importHintAgentIdentity).toContain('by default')
    expect(en.userAccounts.importAgentIdentityPrivate).toContain('switch it to public')
    expect(en.userAccounts.importAgentIdentityPrivate).toContain('validation')
  })

  it('rejects non-JSON Agent Identity content before calling the API', async () => {
    const wrapper = mount(ImportAccountsModal, {
      props: { show: true },
      global: { stubs: basicStubs }
    })

    await selectOpenAI(wrapper)
    await wrapper.get('input[value="agent_identity"]').setValue()

    const importer = wrapper.getComponent(CredentialImportModalStub).props('importer') as (contents: string[]) => Promise<unknown>
    await expect(importer(['not-json'])).rejects.toThrow('userAccounts.importAgentIdentityJSONRequired')

    expect(importCredentialContentsMock).not.toHaveBeenCalled()
    expect(showErrorMock).toHaveBeenCalledWith('userAccounts.importAgentIdentityJSONRequired')
  })

  it('resets Agent Identity mode when switching platform or reopening the dialog', async () => {
    const wrapper = mount(ImportAccountsModal, {
      props: { show: true },
      global: { stubs: basicStubs }
    })

    await selectOpenAI(wrapper)
    await wrapper.get('input[value="agent_identity"]').setValue()
    await findButtonByText(wrapper, 'Claude').trigger('click')
    await selectOpenAI(wrapper)

    expect((wrapper.get('input[value="oauth"]').element as HTMLInputElement).checked).toBe(true)

    await wrapper.get('input[value="agent_identity"]').setValue()
    await wrapper.setProps({ show: false })
    await wrapper.setProps({ show: true })

    expect(wrapper.find('input[name="openai-import-auth-mode"]').exists()).toBe(false)
  })
})

describe('CredentialImportModal updated results', () => {
  it('shows the optional updated count returned by the importer', async () => {
    const wrapper = mount(CredentialImportModal, {
      props: {
        show: true,
        title: 'Import',
        hint: 'Hint',
        warning: 'Warning',
        importer: vi.fn().mockResolvedValue({
          total: 1,
          created: 0,
          updated: 1,
          failed: 0,
          errors: []
        })
      },
      global: {
        stubs: {
          BaseDialog: BaseDialogStub,
          Icon: true
        }
      }
    })

    await wrapper.get('textarea').setValue('{"identity":"value"}')
    await wrapper.get('form').trigger('submit')
    await flushPromises()

    expect(wrapper.text()).toContain('"updated":1')
    expect(wrapper.emitted('imported')).toEqual([[{ close: true }]])
  })
})
