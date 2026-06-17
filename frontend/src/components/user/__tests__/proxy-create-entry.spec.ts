import { defineComponent } from 'vue'
import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'

const {
  listProxiesMock,
  createProxyMock,
  showErrorMock,
  showSuccessMock,
  generateAuthUrlMock,
  resetOAuthStateMock
} = vi.hoisted(() => ({
  listProxiesMock: vi.fn(),
  createProxyMock: vi.fn(),
  showErrorMock: vi.fn(),
  showSuccessMock: vi.fn(),
  generateAuthUrlMock: vi.fn(),
  resetOAuthStateMock: vi.fn()
}))

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string) => key
    })
  }
})

vi.mock('@/api', () => ({
  accountsAPI: {
    create: vi.fn(),
    importCredentialContents: vi.fn()
  },
  accountShareAPI: {
    listProxies: listProxiesMock,
    createProxy: createProxyMock
  }
}))

vi.mock('@/api/accounts', () => ({
  accountsAPI: {
    create: vi.fn()
  }
}))

vi.mock('@/api/admin', () => ({
  adminAPI: {
    accounts: {
      checkMixedChannelRisk: vi.fn().mockResolvedValue({ has_risk: false })
    },
    settings: {
      getSettings: vi.fn().mockResolvedValue({}),
      getWebSearchEmulationConfig: vi.fn().mockResolvedValue({ enabled: false, providers: [] })
    },
    tlsFingerprintProfiles: {
      list: vi.fn().mockResolvedValue([])
    }
  }
}))

vi.mock('@/api/admin/accounts', () => ({
  getAntigravityDefaultModelMapping: vi.fn().mockResolvedValue([])
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({
    cachedPublicSettings: {},
    showError: showErrorMock,
    showSuccess: showSuccessMock,
    showInfo: vi.fn()
  })
}))

vi.mock('@/stores/auth', () => ({
  useAuthStore: () => ({
    isSimpleMode: true,
    user: { load_factor_credits_balance: 100 },
    refreshUser: vi.fn().mockResolvedValue(undefined)
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
      generateAuthUrl: generateAuthUrlMock,
      exchangeAuthCode: vi.fn(),
      buildCredentials: vi.fn(() => ({})),
      buildExtraInfo: vi.fn(() => ({}))
    })
  }
})

vi.mock('@/composables/useGeminiOAuth', async () => {
  const { ref } = await vi.importActual<typeof import('vue')>('vue')
  return {
    useGeminiOAuth: () => ({
      authUrl: ref(''),
      sessionId: ref(''),
      loading: ref(false),
      error: ref(''),
      resetState: vi.fn(),
      generateAuthUrl: vi.fn()
    })
  }
})

vi.mock('@/composables/useAntigravityOAuth', async () => {
  const { ref } = await vi.importActual<typeof import('vue')>('vue')
  return {
    useAntigravityOAuth: () => ({
      authUrl: ref(''),
      sessionId: ref(''),
      loading: ref(false),
      error: ref(''),
      resetState: vi.fn(),
      generateAuthUrl: vi.fn()
    })
  }
})

vi.mock('@/composables/useAccountOAuth', async () => {
  const { ref } = await vi.importActual<typeof import('vue')>('vue')
  return {
    useAccountOAuth: () => ({
      authUrl: ref(''),
      sessionId: ref(''),
      loading: ref(false),
      error: ref(''),
      resetState: vi.fn(),
      generateAuthUrl: vi.fn()
    })
  }
})

import CreateAccountModal from '@/components/account/CreateAccountModal.vue'
import ImportAccountsModal from '@/components/user/ImportAccountsModal.vue'
import UserProxyQuickCreatePanel from '@/components/user/UserProxyQuickCreatePanel.vue'

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
    show: {
      type: Boolean,
      default: false
    }
  },
  template: '<div v-if="show"><slot name="controls" /><slot /></div>'
})

const ProxySelectorStub = defineComponent({
  name: 'ProxySelector',
  props: {
    modelValue: {
      type: [Number, null],
      default: null
    },
    proxies: {
      type: Array,
      default: () => []
    }
  },
  emits: ['update:modelValue'],
  template: '<div data-testid="proxy-selector">{{ proxies.length }}</div>'
})

const OAuthAuthorizationFlowStub = defineComponent({
  name: 'OAuthAuthorizationFlow',
  template: '<div data-testid="oauth-flow"></div>'
})

const basicStubs = {
  BaseDialog: BaseDialogStub,
  CredentialImportModal: CredentialImportModalStub,
  ProxySelector: ProxySelectorStub,
  OAuthAuthorizationFlow: OAuthAuthorizationFlowStub,
  ConfirmDialog: BaseDialogStub,
  Select: true,
  GroupSelector: true,
  ModelWhitelistSelector: true,
  QuotaLimitCard: true
}

function findButtonByText(wrapper: ReturnType<typeof mount>, text: string) {
  const button = wrapper.findAll('button').find(item => item.text().includes(text))
  expect(button, `button text "${text}"`).toBeTruthy()
  return button!
}

describe('user proxy create entry buttons', () => {
  beforeEach(() => {
    listProxiesMock.mockResolvedValue([])
    createProxyMock.mockResolvedValue({
      id: 123,
      name: 'proxy-123',
      protocol: 'socks5',
      host: '127.0.0.1',
      port: 1080,
      username: null,
      status: 'active',
      max_accounts: 10,
      created_at: '2026-01-01T00:00:00Z',
      updated_at: '2026-01-01T00:00:00Z'
    })
    showErrorMock.mockReset()
    showSuccessMock.mockReset()
    generateAuthUrlMock.mockReset()
    resetOAuthStateMock.mockReset()
  })

  it('opens the inline proxy create panel from OpenAI Pro import', async () => {
    const wrapper = mount(ImportAccountsModal, {
      props: {
        show: true
      },
      global: {
        stubs: basicStubs
      }
    })

    await findButtonByText(wrapper, 'OpenAI').trigger('click')
    await wrapper.vm.$nextTick()
    await findButtonByText(wrapper, 'admin.accounts.accountLevel.pro').trigger('click')
    await flushPromises()

    const openPanelButton = wrapper.find('[data-testid="import-open-user-proxy-panel"]')
    expect(openPanelButton.exists()).toBe(true)

    await openPanelButton.trigger('click')
    await wrapper.vm.$nextTick()

    expect(wrapper.find('[data-testid="user-proxy-create-panel"]').exists()).toBe(true)
  })

  it('opens the inline proxy create panel from user OpenAI Pro account creation', async () => {
    const wrapper = mount(CreateAccountModal, {
      props: {
        show: true,
        proxies: [],
        groups: [],
        accountScope: 'user',
        allowProxy: true,
        allowBillingRate: false
      },
      global: {
        stubs: basicStubs
      }
    })

    await findButtonByText(wrapper, 'OpenAI').trigger('click')
    await wrapper.vm.$nextTick()
    await findButtonByText(wrapper, 'admin.accounts.accountLevel.pro').trigger('click')
    await wrapper.vm.$nextTick()

    const openPanelButton = wrapper.find('[data-testid="create-open-user-proxy-panel"]')
    expect(openPanelButton.exists()).toBe(true)

    await openPanelButton.trigger('click')
    await wrapper.vm.$nextTick()

    expect(wrapper.find('[data-testid="user-proxy-create-panel"]').exists()).toBe(true)
  })

  it('creates a custom proxy from the inline panel and emits the created proxy', async () => {
    const wrapper = mount(UserProxyQuickCreatePanel)

    await wrapper.find('[data-testid="user-proxy-smart-input"]').setValue('127.0.0.1:1080:user:pass')
    await wrapper.find('[data-testid="user-proxy-save-button"]').trigger('click')
    await flushPromises()

    expect(createProxyMock).toHaveBeenCalledWith({
      name: 'userAccounts.proxyDefaultName 127.0.0.1:1080',
      protocol: 'socks5',
      host: '127.0.0.1',
      port: 1080,
      username: 'user',
      password: 'pass'
    })
    expect(wrapper.emitted('created')?.[0]?.[0]).toMatchObject({ id: 123 })
  })
})
