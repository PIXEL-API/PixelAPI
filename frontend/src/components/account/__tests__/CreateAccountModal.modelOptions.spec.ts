import { beforeEach, describe, expect, it, vi } from 'vitest'
import { defineComponent } from 'vue'
import { flushPromises, mount } from '@vue/test-utils'

const {
  createAdminAccountMock,
  getPricedModelOptionsMock,
  getUserModelOptionsMock,
  validateOpenAIRefreshTokenMock
} = vi.hoisted(() => ({
  createAdminAccountMock: vi.fn(),
  getPricedModelOptionsMock: vi.fn(),
  getUserModelOptionsMock: vi.fn(),
  validateOpenAIRefreshTokenMock: vi.fn()
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({
    cachedPublicSettings: undefined,
    showError: vi.fn(),
    showSuccess: vi.fn(),
    showWarning: vi.fn(),
    showInfo: vi.fn()
  })
}))

vi.mock('@/stores/auth', () => ({
  useAuthStore: () => ({ isSimpleMode: true })
}))

vi.mock('@/stores/adminSettings', () => ({
  useAdminSettingsStore: () => ({
    webSearchGlobalEnabled: false,
    openAIAccountLevels: undefined,
    ensureWebSearchEmulation: vi.fn().mockResolvedValue(undefined)
  })
}))

vi.mock('@/composables/useQuotaNotifyState', async () => {
  const { reactive, ref } = await vi.importActual<typeof import('vue')>('vue')
  const threshold = () => ({ enabled: false, threshold: null, thresholdType: 'percent' })
  return {
    useQuotaNotifyState: () => ({
      globalEnabled: ref(false),
      state: reactive({ daily: threshold(), weekly: threshold(), total: threshold() }),
      loadGlobalState: vi.fn().mockResolvedValue(undefined),
      writeToExtra: vi.fn()
    })
  }
})

vi.mock('@/api/admin', () => ({
  adminAPI: {
    accounts: {
      create: createAdminAccountMock
    },
    channels: {
      getPricedModelOptions: getPricedModelOptionsMock
    },
    tlsFingerprintProfiles: {
      list: vi.fn().mockResolvedValue([])
    },
    grok: {
      getCapabilities: vi.fn().mockResolvedValue({ password_auth_enabled: false })
    }
  }
}))

vi.mock('@/api/accounts', () => ({
  accountsAPI: {
    getModelOptions: getUserModelOptionsMock,
    create: vi.fn()
  }
}))

vi.mock('@/api/admin/accounts', () => ({
  getAntigravityDefaultModelMapping: vi.fn().mockResolvedValue({})
}))

vi.mock('@/composables/useOpenAIOAuth', async () => {
  const { ref } = await vi.importActual<typeof import('vue')>('vue')
  return {
    useOpenAIOAuth: () => ({
      authUrl: ref(''),
      sessionId: ref(''),
      oauthState: ref(''),
      loading: ref(false),
      error: ref(''),
      resetState: vi.fn(),
      generateAuthUrl: vi.fn(),
      exchangeAuthCode: vi.fn(),
      validateRefreshToken: validateOpenAIRefreshTokenMock,
      buildCredentials: (tokenInfo: Record<string, unknown>) => ({
        refresh_token: tokenInfo.refresh_token
      }),
      buildExtraInfo: vi.fn()
    })
  }
})

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({ t: (key: string) => key })
  }
})

import CreateAccountModal from '../CreateAccountModal.vue'

const BaseDialogStub = defineComponent({
  name: 'BaseDialog',
  props: { show: Boolean },
  template: '<div v-if="show"><slot /><slot name="footer" /></div>'
})

const ModelWhitelistSelectorStub = defineComponent({
  name: 'ModelWhitelistSelector',
  props: {
    modelValue: { type: Array, default: () => [] },
    allowedOptions: { type: Array, default: () => [] }
  },
  emits: ['update:modelValue'],
  template: `
    <div
      data-testid="model-whitelist-selector"
      :data-model-value="modelValue.join(',')"
      :data-allowed-options="allowedOptions.join(',')"
    />
  `
})

const OAuthAuthorizationFlowStub = defineComponent({
  name: 'OAuthAuthorizationFlow',
  emits: ['validate-refresh-token'],
  template: `
    <button
      type="button"
      data-testid="import-openai-rt"
      @click="$emit('validate-refresh-token', 'refresh-token')"
    >
      import
    </button>
  `
})

function mountModal(accountScope: 'admin' | 'user' = 'admin') {
  return mount(CreateAccountModal, {
    props: {
      show: false,
      proxies: [],
      groups: [],
      accountScope,
      initialPlatform: 'openai',
      lockPlatform: true
    },
    global: {
      stubs: {
        BaseDialog: BaseDialogStub,
        ConfirmDialog: true,
        Select: true,
        Icon: true,
        ProxySelector: true,
        GroupSelector: true,
        ModelWhitelistSelector: ModelWhitelistSelectorStub,
        QuotaLimitCard: true,
        GrokBaseUrlPresets: true,
        HeaderOverrideEditor: true,
        OAuthAuthorizationFlow: OAuthAuthorizationFlowStub
      }
    }
  })
}

describe('CreateAccountModal priced model options', () => {
  beforeEach(() => {
    createAdminAccountMock.mockReset()
    createAdminAccountMock.mockResolvedValue({ id: 1 })
    getPricedModelOptionsMock.mockReset()
    getPricedModelOptionsMock.mockResolvedValue({
      models: [' gpt-5.4 ', 'gpt-5.4', '', 'gpt-5.5']
    })
    getUserModelOptionsMock.mockReset()
    getUserModelOptionsMock.mockResolvedValue({ models: ['user-gpt-5.4'] })
    validateOpenAIRefreshTokenMock.mockReset()
    validateOpenAIRefreshTokenMock.mockResolvedValue({
      refresh_token: 'validated-refresh-token',
      email: 'owner@example.com'
    })
  })

  it('管理员新增账号默认全选当前平台的渠道定价模型并集', async () => {
    const wrapper = mountModal()
    await wrapper.setProps({ show: true })
    await flushPromises()

    expect(getPricedModelOptionsMock).toHaveBeenCalledWith(['openai'])
    const selector = wrapper.get('[data-testid="model-whitelist-selector"]')
    expect(selector.attributes('data-allowed-options')).toBe('gpt-5.4,gpt-5.5')
    expect(selector.attributes('data-model-value')).toBe('gpt-5.4,gpt-5.5')
  })

  it('用户新增账号继续使用个人账号模型目录接口并默认全选', async () => {
    const wrapper = mountModal('user')
    await wrapper.setProps({ show: true })
    await flushPromises()

    expect(getUserModelOptionsMock).toHaveBeenCalledWith('openai')
    expect(getPricedModelOptionsMock).not.toHaveBeenCalled()
    const selector = wrapper.get('[data-testid="model-whitelist-selector"]')
    expect(selector.attributes('data-model-value')).toBe('user-gpt-5.4')
  })

  it('渠道定价目录加载失败时保持空候选，不回退前端静态模型', async () => {
    getPricedModelOptionsMock.mockRejectedValue(new Error('pricing unavailable'))

    const wrapper = mountModal()
    await wrapper.setProps({ show: true })
    await flushPromises()

    const selector = wrapper.get('[data-testid="model-whitelist-selector"]')
    expect(selector.attributes('data-allowed-options')).toBe('')
    expect(selector.attributes('data-model-value')).toBe('')
    expect(wrapper.get('[role="alert"]').text()).toContain(
      'admin.accounts.pricedModelsLoadFailed'
    )
  })

  it('RT 导入等待最新目录完成后才创建，并携带默认模型映射', async () => {
    const wrapper = mountModal()
    await wrapper.setProps({ show: true })
    await flushPromises()

    await wrapper.get('[data-tour="account-form-name"]').setValue('OpenAI RT')
    await wrapper.get('form#create-account-form').trigger('submit.prevent')
    await flushPromises()
    expect(wrapper.find('[data-testid="import-openai-rt"]').exists()).toBe(true)

    let resolveLatestOptions!: (value: { models: string[] }) => void
    getPricedModelOptionsMock.mockImplementationOnce(
      () => new Promise(resolve => {
        resolveLatestOptions = resolve
      })
    )

    await wrapper.get('[data-testid="import-openai-rt"]').trigger('click')
    await flushPromises()
    expect(validateOpenAIRefreshTokenMock).not.toHaveBeenCalled()
    expect(createAdminAccountMock).not.toHaveBeenCalled()

    resolveLatestOptions({ models: ['gpt-5.4', 'gpt-5.5'] })
    await flushPromises()

    expect(validateOpenAIRefreshTokenMock).toHaveBeenCalledWith(
      'refresh-token',
      null,
      undefined
    )
    expect(createAdminAccountMock).toHaveBeenCalledWith(
      expect.objectContaining({
        platform: 'openai',
        credentials: expect.objectContaining({
          model_mapping: {
            'gpt-5.4': 'gpt-5.4',
            'gpt-5.5': 'gpt-5.5'
          }
        })
      })
    )
  })
})
