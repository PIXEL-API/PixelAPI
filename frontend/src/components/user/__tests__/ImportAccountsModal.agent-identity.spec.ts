import { defineComponent } from 'vue'
import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'

const {
  importCredentialContentsMock,
  resetOAuthStateMock,
  showErrorMock,
  showSuccessMock,
  showWarningMock
} = vi.hoisted(() => ({
  importCredentialContentsMock: vi.fn(),
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
    create: vi.fn(),
    importCredentialContents: importCredentialContentsMock
  },
  accountShareAPI: {
    listProxies: vi.fn().mockResolvedValue([])
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
      sessionId: ref(''),
      loading: ref(false),
      error: ref(''),
      oauthState: ref(''),
      resetState: resetOAuthStateMock,
      generateAuthUrl: vi.fn(),
      exchangeAuthCode: vi.fn(),
      buildCredentials: vi.fn(() => ({})),
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
    importCredentialContentsMock.mockReset()
    importCredentialContentsMock.mockResolvedValue({
      total: 1,
      created: 1,
      updated: 0,
      failed: 0,
      errors: []
    })
    resetOAuthStateMock.mockReset()
    showErrorMock.mockReset()
    showSuccessMock.mockReset()
    showWarningMock.mockReset()
  })

  it('defaults OpenAI imports to OAuth and preserves the account-level request', async () => {
    const wrapper = mount(ImportAccountsModal, {
      props: { show: true },
      global: { stubs: basicStubs }
    })

    await selectOpenAI(wrapper)

    const oauthRadio = wrapper.get('input[value="oauth"]')
    expect((oauthRadio.element as HTMLInputElement).checked).toBe(true)
    expect(wrapper.text()).toContain('userAccounts.importAccountLevel')

    await findButtonByText(wrapper, 'Free').trigger('click')
    const importer = wrapper.getComponent(CredentialImportModalStub).props('importer') as (contents: string[]) => Promise<unknown>
    await importer(['refresh-token'])

    expect(importCredentialContentsMock).toHaveBeenCalledWith(expect.objectContaining({
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
