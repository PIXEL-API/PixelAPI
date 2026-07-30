import { beforeEach, describe, expect, it, vi } from 'vitest'
import { defineComponent } from 'vue'
import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'

const {
  updateAccountMock,
  updateUserAccountMock,
  getUserAccountMock,
  convertPlacementMock,
  checkMixedChannelRiskMock
} = vi.hoisted(() => ({
  updateAccountMock: vi.fn(),
  updateUserAccountMock: vi.fn(),
  getUserAccountMock: vi.fn(),
  convertPlacementMock: vi.fn(),
  checkMixedChannelRiskMock: vi.fn()
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({
    showError: vi.fn(),
    showSuccess: vi.fn(),
    showInfo: vi.fn()
  })
}))

vi.mock('@/stores/auth', () => ({
  useAuthStore: () => ({
    isSimpleMode: true,
    user: {
      load_factor_credits_balance: 100
    },
    refreshUser: vi.fn().mockResolvedValue(undefined)
  })
}))

vi.mock('@/api/admin', () => ({
  adminAPI: {
    accounts: {
      update: updateAccountMock,
      checkMixedChannelRisk: checkMixedChannelRiskMock
    },
    settings: {
      getWebSearchEmulationConfig: vi.fn().mockResolvedValue({ enabled: false, providers: [] }),
      getSettings: vi.fn().mockResolvedValue({})
    },
    tlsFingerprintProfiles: {
      list: vi.fn().mockResolvedValue([])
    }
  }
}))

vi.mock('@/api/admin/accounts', () => ({
  getAntigravityDefaultModelMapping: vi.fn()
}))

vi.mock('@/api/accounts', () => ({
  accountsAPI: {
    update: updateUserAccountMock,
    getById: getUserAccountMock
  }
}))

vi.mock('@/api/accountShare', () => ({
  accountShareAPI: {
    convertAccountExternalPlacement: convertPlacementMock
  }
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

import EditAccountModal from '../EditAccountModal.vue'

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

const ModelWhitelistSelectorStub = defineComponent({
  name: 'ModelWhitelistSelector',
  props: {
    modelValue: {
      type: Array,
      default: () => []
    }
  },
  emits: ['update:modelValue'],
  template: `
    <div>
      <button
        type="button"
        data-testid="rewrite-to-snapshot"
        @click="$emit('update:modelValue', ['gpt-5.2-2025-12-11'])"
      >
        rewrite
      </button>
      <span data-testid="model-whitelist-value">
        {{ Array.isArray(modelValue) ? modelValue.join(',') : '' }}
      </span>
    </div>
  `
})

const SelectStub = defineComponent({
  name: 'SelectStub',
  props: {
    modelValue: {
      type: [String, Number, Boolean, null],
      default: ''
    },
    options: {
      type: Array,
      default: () => []
    }
  },
  emits: ['update:modelValue'],
  template: `
    <select
      v-bind="$attrs"
      :value="modelValue"
      @change="$emit('update:modelValue', $event.target.value)"
    >
      <option v-for="option in options" :key="option.value" :value="option.value">
        {{ option.label }}
      </option>
    </select>
  `
})

const ProxySelectorStub = defineComponent({
  name: 'ProxySelectorStub',
  props: {
    modelValue: {
      type: [Number, null],
      default: null
    },
    allowEmpty: {
      type: Boolean,
      default: true
    }
  },
  emits: ['update:modelValue'],
  template: `
    <div data-testid="proxy-selector" :data-allow-empty="String(allowEmpty)" :data-value="String(modelValue ?? '')">
      <button type="button" data-testid="select-proxy-9" @click="$emit('update:modelValue', 9)">replace</button>
      <button v-if="allowEmpty" type="button" data-testid="clear-proxy" @click="$emit('update:modelValue', null)">clear</button>
    </div>
  `
})

function buildAccount() {
  return {
    id: 1,
    name: 'OpenAI Key',
    notes: '',
    platform: 'openai',
    type: 'apikey',
    credentials: {
      api_key: 'sk-test',
      base_url: 'https://api.openai.com',
      model_mapping: {
        'gpt-5.2': 'gpt-5.2'
      }
    },
    credentials_status: {
      has_api_key: true
    },
    extra: {},
    proxy_id: null,
    concurrency: 1,
    priority: 1,
    rate_multiplier: 1,
    account_level: 'plus',
    status: 'active',
    group_ids: [],
    expires_at: null,
    auto_pause_on_expired: false
  } as any
}

function mountModal(account = buildAccount(), extraProps: Record<string, unknown> = {}) {
  return mount(EditAccountModal, {
    props: {
      show: true,
      account,
      proxies: [],
      groups: [],
      ...extraProps
    },
    global: {
      stubs: {
        BaseDialog: BaseDialogStub,
        Select: SelectStub,
        Icon: true,
        ProxySelector: ProxySelectorStub,
        GroupSelector: true,
        ModelWhitelistSelector: ModelWhitelistSelectorStub
      }
    }
  })
}

describe('EditAccountModal', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    convertPlacementMock.mockReset()
    getUserAccountMock.mockReset()
  })

  it('用户可选择平台账号模式且无需选择具体房间', async () => {
    const account = buildAccount()
    account.type = 'oauth'
    account.credentials = { access_token: 'oauth-token' }
    account.share_mode = 'private'
    updateUserAccountMock.mockReset()
    updateUserAccountMock.mockResolvedValue(account)
    convertPlacementMock.mockResolvedValue({
      account_id: account.id,
      previous: { target: 'private', state: 'active', version: 1 },
      current: { target: 'room', state: 'active', version: 2 },
      unchanged: false
    })
    getUserAccountMock.mockResolvedValue({
      ...account,
      share_mode: 'private',
      external_placement: { target: 'room', state: 'active', version: 2 }
    })
    vi.spyOn(globalThis.crypto, 'randomUUID').mockReturnValue(
      '11111111-1111-4111-8111-111111111111'
    )

    const wrapper = mountModal(account, {
      accountScope: 'user',
      ownerUserId: 9
    })

    expect(wrapper.find('[data-testid="placement-target-private"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="placement-target-public_pool"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="placement-target-room"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="refresh-compatible-rooms"]').exists()).toBe(false)

    await wrapper.get('[data-testid="placement-target-room"]').setValue()
    await wrapper.get('form#edit-account-form').trigger('submit.prevent')
    await flushPromises()

    expect(updateUserAccountMock).toHaveBeenCalledTimes(1)
    expect(convertPlacementMock).toHaveBeenCalledWith(account.id, {
      target: 'room',
      idempotency_key: 'account-placement-1-11111111-1111-4111-8111-111111111111'
    })
    expect(getUserAccountMock).toHaveBeenCalledWith(account.id)
    expect(wrapper.emitted('updated')).toHaveLength(1)
  })

  it('普通设置已保存但模式切换失败时通知父级刷新账号', async () => {
    const account = buildAccount()
    account.type = 'oauth'
    account.credentials = { access_token: 'oauth-token' }
    account.share_mode = 'private'
    updateUserAccountMock.mockReset()
    updateUserAccountMock.mockResolvedValue(account)
    convertPlacementMock.mockRejectedValue({ message: '账号正在使用中' })
    vi.spyOn(globalThis.crypto, 'randomUUID').mockReturnValue(
      '44444444-4444-4444-8444-444444444444'
    )
    const wrapper = mountModal(account, {
      accountScope: 'user',
      ownerUserId: 9
    })

    await wrapper.get('[data-testid="placement-target-public_pool"]').setValue()
    await wrapper.get('form#edit-account-form').trigger('submit.prevent')
    await flushPromises()

    expect(updateUserAccountMock).toHaveBeenCalledTimes(1)
    expect(convertPlacementMock).toHaveBeenCalledTimes(1)
    expect(wrapper.emitted('updated')).toHaveLength(1)
    expect(wrapper.find('[data-testid="edit-placement-save-error"]').exists()).toBe(true)
  })

  it('reopening the same account rehydrates the OpenAI whitelist from props', async () => {
    const account = buildAccount()
    updateAccountMock.mockReset()
    checkMixedChannelRiskMock.mockReset()
    checkMixedChannelRiskMock.mockResolvedValue({ has_risk: false })
    updateAccountMock.mockResolvedValue(account)

    const wrapper = mountModal(account)

    expect(wrapper.get('[data-testid="model-whitelist-value"]').text()).toBe('gpt-5.2')

    await wrapper.get('[data-testid="rewrite-to-snapshot"]').trigger('click')
    expect(wrapper.get('[data-testid="model-whitelist-value"]').text()).toBe('gpt-5.2-2025-12-11')

    await wrapper.setProps({ show: false })
    await wrapper.setProps({ show: true })

    expect(wrapper.get('[data-testid="model-whitelist-value"]').text()).toBe('gpt-5.2')

    await wrapper.get('form#edit-account-form').trigger('submit.prevent')

    expect(updateAccountMock).toHaveBeenCalledTimes(1)
    expect(updateAccountMock.mock.calls[0]?.[1]?.credentials?.model_mapping).toEqual({
      'gpt-5.2': 'gpt-5.2'
    })
    expect(updateAccountMock.mock.calls[0]?.[1]?.account_level).toBe('plus')
  })

  it('submits an arbitrary OpenAI OAuth subscription tier override', async () => {
    const account = buildAccount()
    account.type = 'oauth'
    account.credentials = {
      access_token: 'oauth-token',
      plan_type: 'plus',
      model_mapping: {
        'custom-model': 'custom-target'
      }
    }
    updateAccountMock.mockReset()
    checkMixedChannelRiskMock.mockReset()
    checkMixedChannelRiskMock.mockResolvedValue({ has_risk: false })
    updateAccountMock.mockResolvedValue(account)

    const wrapper = mountModal(account)
    const planTypeInput = wrapper.get('[data-testid="openai-plan-type-override"]')

    expect((planTypeInput.element as HTMLInputElement).value).toBe('plus')
    await planTypeInput.setValue('self_serve_business')
    await wrapper.get('form#edit-account-form').trigger('submit.prevent')

    expect(updateAccountMock).toHaveBeenCalledTimes(1)
    expect(updateAccountMock.mock.calls[0]?.[1]?.credentials).toMatchObject({
      access_token: 'oauth-token',
      plan_type: 'self_serve_business',
      model_mapping: {
        'custom-model': 'custom-target'
      }
    })
  })

  it('removes the OpenAI OAuth subscription tier override when cleared', async () => {
    const account = buildAccount()
    account.type = 'oauth'
    account.credentials = {
      access_token: 'oauth-token',
      plan_type: 'plus'
    }
    updateAccountMock.mockReset()
    checkMixedChannelRiskMock.mockReset()
    checkMixedChannelRiskMock.mockResolvedValue({ has_risk: false })
    updateAccountMock.mockResolvedValue(account)

    const wrapper = mountModal(account)
    await wrapper.get('[data-testid="openai-plan-type-override"]').setValue('')
    await wrapper.get('form#edit-account-form').trigger('submit.prevent')

    const credentials = updateAccountMock.mock.calls[0]?.[1]?.credentials
    expect(credentials?.access_token).toBe('oauth-token')
    expect(credentials).not.toHaveProperty('plan_type')
  })

  it('does not show the subscription tier override for OpenAI API Key accounts', () => {
    const wrapper = mountModal()

    expect(wrapper.find('[data-testid="openai-plan-type-override"]').exists()).toBe(false)
  })

  it('keeps account level read-only for user-scoped account edits', async () => {
    const account = buildAccount()
    account.type = 'oauth'
    account.credentials = {
      access_token: 'oauth-token',
      model_mapping: {
        'custom-model': 'custom-target'
      }
    }
    account.extra = {
      openai_compact_mode: 'force_off',
      openai_passthrough: true,
      codex_5h_limit_percent: 60
    }
    account.concurrency = 9
    account.load_factor = 10
    account.load_factor_paid_ceiling = 10
    updateAccountMock.mockReset()
    updateUserAccountMock.mockReset()
    checkMixedChannelRiskMock.mockReset()
    checkMixedChannelRiskMock.mockResolvedValue({ has_risk: false })
    updateUserAccountMock.mockResolvedValue(account)

    const wrapper = mountModal(account)
    await wrapper.setProps({ accountScope: 'user' })
    expect(wrapper.find('[data-testid="openai-plan-type-override"]').exists()).toBe(false)
    const concurrencyInput = wrapper.get('input[type="number"][min="1"][max="30"]')
    await concurrencyInput.setValue('')
    expect((concurrencyInput.element as HTMLInputElement).value).toBe('')
    await concurrencyInput.setValue('25')
    await wrapper.get('form#edit-account-form').trigger('submit.prevent')

    expect(updateUserAccountMock).toHaveBeenCalledTimes(1)
    const payload = updateUserAccountMock.mock.calls[0]?.[1]
    expect(payload).not.toHaveProperty('account_level')
    expect(payload?.concurrency).toBe(25)
    expect(payload?.load_factor).toBe(10)
    expect(payload).not.toHaveProperty('priority')
    expect(payload).not.toHaveProperty('auto_pause_on_expired')
    expect(payload?.credentials?.model_mapping).toEqual({
      'custom-model': 'custom-target'
    })
    expect(payload?.extra?.openai_compact_mode).toBe('force_off')
    expect(payload?.extra?.openai_passthrough).toBe(true)
    expect(payload?.extra?.codex_5h_limit_percent).toBe(60)
  })

  it('submits OpenAI compact mode and compact-only model mapping', async () => {
    const account = buildAccount()
    account.extra = {
      openai_compact_mode: 'force_on'
    }
    account.credentials = {
      ...account.credentials,
      compact_model_mapping: {
        'gpt-5.4': 'gpt-5.4-openai-compact'
      }
    }
    updateAccountMock.mockReset()
    checkMixedChannelRiskMock.mockReset()
    checkMixedChannelRiskMock.mockResolvedValue({ has_risk: false })
    updateAccountMock.mockResolvedValue(account)

    const wrapper = mountModal(account)

    await wrapper.get('form#edit-account-form').trigger('submit.prevent')

    expect(updateAccountMock).toHaveBeenCalledTimes(1)
    expect(updateAccountMock.mock.calls[0]?.[1]?.extra?.openai_compact_mode).toBe('force_on')
    expect(updateAccountMock.mock.calls[0]?.[1]?.credentials?.compact_model_mapping).toEqual({
      'gpt-5.4': 'gpt-5.4-openai-compact'
    })
  })

  it('allows a required-proxy user account to replace but not clear its proxy', async () => {
    const account = buildAccount()
    account.platform = 'anthropic'
    account.type = 'oauth'
    account.account_level = 'unknown'
    account.proxy_id = 7
    account.credentials = { access_token: 'oauth-token' }
    updateUserAccountMock.mockReset()
    updateUserAccountMock.mockResolvedValue(account)

    const wrapper = mountModal(account, {
      accountScope: 'user',
      allowProxy: true,
      proxies: [
        { id: 7, name: 'old', protocol: 'http', host: 'old.example.com', port: 8080, status: 'active', max_accounts: 0 },
        { id: 9, name: 'new', protocol: 'http', host: 'new.example.com', port: 8080, status: 'active', max_accounts: 0 }
      ]
    })

    expect(wrapper.get('[data-testid="proxy-selector"]').attributes('data-allow-empty')).toBe('false')
    expect(wrapper.find('[data-testid="clear-proxy"]').exists()).toBe(false)
    await wrapper.get('[data-testid="select-proxy-9"]').trigger('click')
    await wrapper.get('form#edit-account-form').trigger('submit.prevent')

    expect(updateUserAccountMock).toHaveBeenCalledTimes(1)
    expect(updateUserAccountMock.mock.calls[0]?.[1]?.proxy_id).toBe(9)
  })

  it('allows an optional-proxy user account to clear its proxy', async () => {
    const account = buildAccount()
    account.type = 'oauth'
    account.proxy_id = 7
    account.credentials = { access_token: 'oauth-token', plan_type: 'plus' }
    updateUserAccountMock.mockReset()
    updateUserAccountMock.mockResolvedValue(account)

    const wrapper = mountModal(account, {
      accountScope: 'user',
      allowProxy: true,
      proxies: [
        { id: 7, name: 'optional', protocol: 'http', host: 'proxy.example.com', port: 8080, status: 'active', max_accounts: 0 }
      ]
    })

    expect(wrapper.get('[data-testid="proxy-selector"]').attributes('data-allow-empty')).toBe('true')
    expect(wrapper.find('[data-testid="openai-plan-type-override"]').exists()).toBe(false)
    await wrapper.get('[data-testid="clear-proxy"]').trigger('click')
    await wrapper.get('form#edit-account-form').trigger('submit.prevent')

    expect(updateUserAccountMock).toHaveBeenCalledTimes(1)
    const payload = updateUserAccountMock.mock.calls[0]?.[1]
    expect(payload?.proxy_id).toBe(0)
    expect(payload?.credentials?.plan_type).toBe('plus')
  })

  it('loads and preserves Grok OAuth upstream config for administrators', async () => {
    const account = buildAccount()
    account.name = 'Grok OAuth'
    account.platform = 'grok'
    account.type = 'oauth'
    account.account_level = 'unknown'
    account.credentials = {
      access_token: 'grok-token',
      refresh_token: 'grok-refresh',
      base_url: 'https://relay.example.com/v1',
      header_override_enabled: true,
      header_overrides: {
        'user-agent': 'grok-build',
        'x-grok-client-version': '1.2.3'
      }
    }
    account.extra = {
      grok_client_tool_cache_enabled: false,
      custom_setting: 'preserved'
    }
    updateAccountMock.mockReset()
    checkMixedChannelRiskMock.mockReset()
    checkMixedChannelRiskMock.mockResolvedValue({ has_risk: false })
    updateAccountMock.mockResolvedValue(account)

    const wrapper = mountModal(account)

    expect(wrapper.get('[data-testid="grok-custom-base-url-input"]').element).toHaveProperty(
      'value',
      'https://relay.example.com/v1'
    )
    expect(wrapper.get('[data-testid="grok-client-tool-cache-toggle"]').attributes('aria-checked')).toBe(
      'false'
    )
    expect(wrapper.find('[data-testid="grok-header-override"]').exists()).toBe(true)

    await wrapper.get('form#edit-account-form').trigger('submit.prevent')
    await flushPromises()

    expect(updateAccountMock).toHaveBeenCalledTimes(1)
    const payload = updateAccountMock.mock.calls[0]?.[1]
    expect(payload?.credentials).toMatchObject({
      base_url: 'https://relay.example.com/v1',
      header_override_enabled: true,
      header_overrides: {
        'user-agent': 'grok-build',
        'x-grok-client-version': '1.2.3'
      }
    })
    expect(payload?.extra).toMatchObject({
      grok_client_tool_cache_enabled: false,
      custom_setting: 'preserved'
    })
  })

  it('does not expose or submit Grok administrator upstream controls in user scope', async () => {
    const account = buildAccount()
    account.platform = 'grok'
    account.type = 'oauth'
    account.account_level = 'unknown'
    account.proxy_id = 7
    account.credentials = {
      access_token: 'grok-token',
      refresh_token: 'grok-refresh',
      base_url: 'https://relay.example.com/v1',
      header_override_enabled: true,
      header_overrides: {
        'user-agent': 'grok-build'
      }
    }
    account.extra = {
      grok_client_tool_cache_enabled: true
    }
    updateUserAccountMock.mockReset()
    updateUserAccountMock.mockResolvedValue(account)

    const wrapper = mountModal(account, { accountScope: 'user' })

    expect(wrapper.find('[data-testid="grok-custom-base-url"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="grok-header-override"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="grok-client-tool-cache"]').exists()).toBe(false)

    await wrapper.get('form#edit-account-form').trigger('submit.prevent')
    await flushPromises()

    expect(updateUserAccountMock).toHaveBeenCalledTimes(1)
    const payload = updateUserAccountMock.mock.calls[0]?.[1]
    expect(payload?.credentials).not.toHaveProperty('base_url')
    expect(payload?.credentials).not.toHaveProperty('header_override_enabled')
    expect(payload?.credentials).not.toHaveProperty('header_overrides')
    expect(payload?.extra?.grok_client_tool_cache_enabled).toBeUndefined()
  })
})
