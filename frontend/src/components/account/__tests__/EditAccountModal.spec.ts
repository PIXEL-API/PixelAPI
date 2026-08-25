import { beforeEach, describe, expect, it, vi } from 'vitest'
import { defineComponent } from 'vue'
import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'

const {
  updateAccountMock,
  updateUserAccountMock,
  getUserAccountMock,
  getUserModelOptionsMock,
  convertPlacementMock,
  convertAdminPlacementMock,
  checkMixedChannelRiskMock
} = vi.hoisted(() => ({
  updateAccountMock: vi.fn(),
  updateUserAccountMock: vi.fn(),
  getUserAccountMock: vi.fn(),
  getUserModelOptionsMock: vi.fn(),
  convertPlacementMock: vi.fn(),
  convertAdminPlacementMock: vi.fn(),
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
      convertExternalPlacement: convertAdminPlacementMock,
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
    getById: getUserAccountMock,
    getModelOptions: getUserModelOptionsMock
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
    },
    allowedOptions: {
      type: Array,
      default: undefined
    },
    allowCustom: {
      type: Boolean,
      default: true
    }
  },
  emits: ['update:modelValue'],
  template: `
    <div
      data-testid="model-whitelist-selector"
      :data-allow-custom="String(allowCustom)"
      :data-allowed-options="Array.isArray(allowedOptions) ? allowedOptions.join(',') : ''"
    >
      <button
        type="button"
        data-testid="rewrite-to-snapshot"
        @click="$emit('update:modelValue', ['gpt-5.2-2025-12-11'])"
      >
        rewrite
      </button>
      <button
        type="button"
        data-testid="clear-model-whitelist"
        @click="$emit('update:modelValue', [])"
      >
        clear
      </button>
      <button
        type="button"
        data-testid="set-invalid-model-whitelist"
        @click="$emit('update:modelValue', ['outside-priced-union'])"
      >
        invalid
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
    },
    hideEndpoint: {
      type: Boolean,
      default: false
    }
  },
  emits: ['update:modelValue'],
  template: `
    <div data-testid="proxy-selector" :data-allow-empty="String(allowEmpty)" :data-hide-endpoint="String(hideEndpoint)" :data-value="String(modelValue ?? '')">
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
  const mountedAccount = extraProps.accountScope === 'user' && account.credentials?.model_mapping == null
    ? {
        ...account,
        credentials: {
          ...(account.credentials || {}),
          model_mapping: { 'gpt-5.2': 'gpt-5.2' }
        }
      }
    : account
  return mount(EditAccountModal, {
    props: {
      show: true,
      account: mountedAccount,
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
    convertAdminPlacementMock.mockReset()
    getUserAccountMock.mockReset()
    getUserModelOptionsMock.mockReset()
    getUserModelOptionsMock.mockResolvedValue({
      models: ['gpt-5.2', 'gpt-5.2-2025-12-11', 'custom-model']
    })
  })

  // 投放守卫：管理端命中"必须先转出投放"时，给出一键转私有并重放本次编辑的通路。
  // 此前管理员遇到这类字段只能去找房主，功能上是死路。
  it('管理端遇到投放硬锁字段时可一键转私有并重放，且重放不带分组', async () => {
    const account = buildAccount()
    account.owner_user_id = 5112
    account.group_ids = [7, 9]
    account.external_placement = { target: 'public_pool', state: 'active', version: 3 }

    updateAccountMock.mockReset()
    updateAccountMock.mockRejectedValueOnce({
      status: 400,
      reason: 'OWNED_ACCOUNT_PLACEMENT_CONVERSION_REQUIRED',
      message: 'conversion required',
      metadata: {
        required_action: 'convert_external_placement',
        changed_fields: 'account_level',
        placement_target: 'public_pool'
      }
    })
    updateAccountMock.mockResolvedValueOnce(account)
    convertAdminPlacementMock.mockResolvedValue({
      account_id: account.id,
      current: { target: 'private', state: 'active', version: 4 }
    })

    const wrapper = mountModal(account)
    await flushPromises()
    await wrapper.get('form#edit-account-form').trigger('submit.prevent')
    await flushPromises()

    // 弹窗里有多个 ConfirmDialog（混合渠道警告 / 投放转换），只有当前展示的那个才是目标。
    const convertDialog = wrapper
      .findAllComponents({ name: 'ConfirmDialog' })
      .find((dialog) => dialog.props('show') === true)
    expect(convertDialog).toBeTruthy()
    convertDialog!.vm.$emit('confirm')
    await flushPromises()

    expect(convertAdminPlacementMock).toHaveBeenCalledTimes(1)
    expect(convertAdminPlacementMock.mock.calls[0]?.[0]).toBe(account.id)
    expect(convertAdminPlacementMock.mock.calls[0]?.[1]).toMatchObject({ target: 'private' })

    expect(updateAccountMock).toHaveBeenCalledTimes(2)
    // 转换事务已经把分组重置成所有者私有分组，重放若原样带回投放期的分组会把它冲掉。
    expect(updateAccountMock.mock.calls[1]?.[1]).not.toHaveProperty('group_ids')
  })

  // 可以改但影响在用消费者的字段：要求填写理由并二次确认，然后带着强制标记重放。
  it('管理端遇到需强制确认的字段时，填写理由后带强制标记重放', async () => {
    const account = buildAccount()
    account.owner_user_id = 5112
    account.external_placement = { target: 'public_pool', state: 'active', version: 3 }

    updateAccountMock.mockReset()
    updateAccountMock.mockRejectedValueOnce({
      status: 409,
      reason: 'ACCOUNT_MUTATION_FORCE_REQUIRED',
      message: 'force required',
      metadata: { missing: 'force_active_edit', changed_fields: 'credentials' }
    })
    updateAccountMock.mockResolvedValueOnce(account)

    const wrapper = mountModal(account)
    await flushPromises()
    await wrapper.get('form#edit-account-form').trigger('submit.prevent')
    await flushPromises()

    const reasonInput = wrapper.get('#placement-force-reason')
    await reasonInput.setValue('上游账号被封，更换凭证')
    await flushPromises()

    const confirmButton = wrapper
      .findAll('button')
      .find((button) => button.text() === 'admin.accounts.placementGuard.forceConfirm')
    expect(confirmButton).toBeTruthy()
    await confirmButton!.trigger('click')
    await flushPromises()

    expect(updateAccountMock).toHaveBeenCalledTimes(2)
    expect(updateAccountMock.mock.calls[1]?.[1]).toMatchObject({
      force_active_edit: true,
      confirmed: true,
      reason: '上游账号被封，更换凭证'
    })
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

  it('keeps account level read-only and submits an editable identity whitelist for user-scoped account edits', async () => {
    const account = buildAccount()
    account.type = 'oauth'
    account.credentials = {
      access_token: 'oauth-token',
      model_mapping: {
        'custom-model': 'custom-model'
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
    await flushPromises()
    expect(wrapper.find('[data-testid="openai-plan-type-override"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="user-model-whitelist-section"]').exists()).toBe(true)
    expect(wrapper.get('[data-testid="model-whitelist-selector"]').attributes('data-allow-custom')).toBe('false')
    expect(wrapper.get('[data-testid="model-whitelist-value"]').text()).toBe('custom-model')
    await wrapper.get('[data-testid="rewrite-to-snapshot"]').trigger('click')
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
      'gpt-5.2-2025-12-11': 'gpt-5.2-2025-12-11'
    })
    expect(payload?.extra?.openai_compact_mode).toBe('force_off')
    expect(payload?.extra?.openai_passthrough).toBe(true)
    expect(payload?.extra?.codex_5h_limit_percent).toBe(60)
  })

  it('blocks user-scoped updates when the whitelist is empty or outside the priced-model union', async () => {
    const account = buildAccount()
    account.type = 'oauth'
    account.credentials = {
      access_token: 'oauth-token',
      model_mapping: { 'gpt-5.2': 'gpt-5.2' }
    }
    updateUserAccountMock.mockReset()
    updateUserAccountMock.mockResolvedValue(account)

    const wrapper = mountModal(account, { accountScope: 'user' })
    await flushPromises()

    await wrapper.get('[data-testid="clear-model-whitelist"]').trigger('click')
    await wrapper.get('form#edit-account-form').trigger('submit.prevent')
    expect(updateUserAccountMock).not.toHaveBeenCalled()

    await wrapper.get('[data-testid="set-invalid-model-whitelist"]').trigger('click')
    await wrapper.get('form#edit-account-form').trigger('submit.prevent')
    expect(updateUserAccountMock).not.toHaveBeenCalled()
  })

  it('blocks user-scoped updates when priced-model options fail to load', async () => {
    const account = buildAccount()
    account.type = 'oauth'
    account.credentials = {
      access_token: 'oauth-token',
      model_mapping: { 'gpt-5.2': 'gpt-5.2' }
    }
    getUserModelOptionsMock.mockRejectedValueOnce(new Error('network error'))
    updateUserAccountMock.mockReset()

    const wrapper = mountModal(account, { accountScope: 'user' })
    await flushPromises()
    await wrapper.get('form#edit-account-form').trigger('submit.prevent')

    expect(updateUserAccountMock).not.toHaveBeenCalled()
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

    await flushPromises()
    await wrapper.get('form#edit-account-form').trigger('submit.prevent')
    await flushPromises()

    expect(updateUserAccountMock).toHaveBeenCalledTimes(1)
    const payload = updateUserAccountMock.mock.calls[0]?.[1]
    expect(payload?.credentials).not.toHaveProperty('base_url')
    expect(payload?.credentials).not.toHaveProperty('header_override_enabled')
    expect(payload?.credentials).not.toHaveProperty('header_overrides')
    expect(payload?.extra?.grok_client_tool_cache_enabled).toBeUndefined()
  })

  function buildRoomAttachedAccount() {
    const account = buildAccount()
    account.type = 'oauth'
    account.credentials = { access_token: 'oauth-token' }
    account.share_mode = 'private'
    // 后端 HasRoomAccount 用的就是这张表的状态，前端以此判断「还挂在房间上」
    account.account_share_mode_listing_id = 31
    account.external_placement = { target: 'room', state: 'active', version: 3 }
    return account
  }

  it('账号仍挂在广场房间时禁用仅本人/公共号池并说明原因', async () => {
    const account = buildRoomAttachedAccount()
    updateUserAccountMock.mockReset()
    updateUserAccountMock.mockResolvedValue(account)

    const wrapper = mountModal(account, { accountScope: 'user', ownerUserId: 9 })

    expect(
      wrapper.get('[data-testid="placement-target-private"]').attributes('disabled')
    ).toBeDefined()
    expect(
      wrapper.get('[data-testid="placement-target-public_pool"]').attributes('disabled')
    ).toBeDefined()
    // 当前生效的模式不能被禁用，否则等于一个都选不中
    expect(
      wrapper.get('[data-testid="placement-target-room"]').attributes('disabled')
    ).toBeUndefined()
    expect(wrapper.get('[data-testid="placement-disabled-reason-private"]').text()).toBe(
      'userAccounts.externalPlacement.roomAttachedDisabledHint'
    )
  })

  it('房间账号点不动被禁用的模式，保存其它字段也不会触发注定失败的转换', async () => {
    const account = buildRoomAttachedAccount()
    updateUserAccountMock.mockReset()
    updateUserAccountMock.mockResolvedValue(account)
    convertPlacementMock.mockReset()

    const wrapper = mountModal(account, { accountScope: 'user', ownerUserId: 9 })

    // 被禁用的 radio 不会响应点击，模式停在 room
    await wrapper.get('[data-testid="placement-target-private"]').setValue()
    await wrapper.get('form#edit-account-form').trigger('submit.prevent')
    await flushPromises()

    expect(updateUserAccountMock).toHaveBeenCalledTimes(1)
    // 修复前这里会调用转换并被后端以 ErrAccountShareRoomAccountAttached 拒绝，
    // 用户看到的是「账号其他设置已保存，但模式切换失败」
    expect(convertPlacementMock).not.toHaveBeenCalled()
    expect(wrapper.find('[data-testid="edit-placement-save-error"]').exists()).toBe(false)
  })

  it('已退房但仍处于平台账号模式的账号可以切回仅本人', async () => {
    const account = buildAccount()
    account.type = 'oauth'
    account.credentials = { access_token: 'oauth-token' }
    account.share_mode = 'private'
    // 退房后 account_share_room_accounts 里没有记录了（listing id 为空），
    // 但 DetachRoomAccountsAtomic 不会把 placement.target 写回 private
    account.external_placement = { target: 'room', state: 'active', version: 4 }

    const wrapper = mountModal(account, { accountScope: 'user', ownerUserId: 9 })

    expect(
      wrapper.get('[data-testid="placement-target-private"]').attributes('disabled')
    ).toBeUndefined()
    expect(
      wrapper.get('[data-testid="placement-target-public_pool"]').attributes('disabled')
    ).toBeUndefined()
  })

  it('用户侧默认隐藏平台代理的 host:port，管理端仍可见', async () => {
    const account = buildAccount()
    account.type = 'oauth'
    account.credentials = { access_token: 'oauth-token' }
    account.proxy_id = 7
    const proxies = [
      { id: 7, name: 'p', protocol: 'http', host: 'proxy.example.com', port: 8080, status: 'active', max_accounts: 0 }
    ]

    const userWrapper = mountModal(account, { accountScope: 'user', allowProxy: true, proxies })
    expect(
      userWrapper.get('[data-testid="proxy-selector"]').attributes('data-hide-endpoint')
    ).toBe('true')

    const adminWrapper = mountModal(account, { allowProxy: true, proxies })
    expect(
      adminWrapper.get('[data-testid="proxy-selector"]').attributes('data-hide-endpoint')
    ).toBe('false')
  })

  it('管理端不传 allowProxy / allowBillingRate 时仍应显示代理与倍率字段', async () => {
    const account = buildAccount()
    account.proxy_id = 7
    const proxies = [
      { id: 7, name: 'p', protocol: 'http', host: 'proxy.example.com', port: 8080, status: 'active', max_accounts: 0 }
    ]

    // 与 views/admin/AccountsView.vue 的调用方式一致：只给 show/account/proxies/groups。
    // 修复前这两个可选 boolean prop 会被 Vue 转成 false，两个字段一起消失。
    const wrapper = mountModal(account, { proxies })

    expect(wrapper.find('[data-testid="proxy-selector"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="rate-multiplier"]').exists()).toBe(true)
  })

  function buildUnschedulableAccount() {
    const account = buildAccount()
    account.type = 'oauth'
    account.credentials = { access_token: 'oauth-token' }
    account.share_mode = 'private'
    // 列表里显示成「暂停」的那一档：状态正常但调度开关关着。
    // 后端 isOwnedAccountPublicShareApprovable → IsSchedulable 第一条就是 !a.Schedulable。
    account.status = 'active'
    account.schedulable = false
    return account
  }

  it('账号暂停调度时禁用公共号池并说明原因，仅本人/平台账号模式不受影响', async () => {
    const account = buildUnschedulableAccount()

    const wrapper = mountModal(account, { accountScope: 'user', ownerUserId: 9 })

    expect(
      wrapper.get('[data-testid="placement-target-public_pool"]').attributes('disabled')
    ).toBeDefined()
    expect(wrapper.get('[data-testid="placement-disabled-reason-public_pool"]').text()).toBe(
      'userAccounts.externalPlacement.publicPoolUnschedulableHint'
    )
    // 后端 room 分支只校验账号等级与模式分组，不看 schedulable；private 更不需要
    expect(
      wrapper.get('[data-testid="placement-target-private"]').attributes('disabled')
    ).toBeUndefined()
    expect(
      wrapper.get('[data-testid="placement-target-room"]').attributes('disabled')
    ).toBeUndefined()
  })

  it('暂停调度的账号保存其它字段不会触发注定失败的公共号池转换', async () => {
    const account = buildUnschedulableAccount()
    updateUserAccountMock.mockReset()
    updateUserAccountMock.mockResolvedValue(account)
    convertPlacementMock.mockReset()

    const wrapper = mountModal(account, { accountScope: 'user', ownerUserId: 9 })

    await wrapper.get('[data-testid="placement-target-public_pool"]').setValue()
    await wrapper.get('form#edit-account-form').trigger('submit.prevent')
    await flushPromises()

    expect(updateUserAccountMock).toHaveBeenCalledTimes(1)
    // 修复前这里会调用转换并被后端以 OWNED_ACCOUNT_PUBLIC_VALIDATION_FAILED 拒绝，
    // 用户看到的是「账号其他设置已保存，但模式切换失败：public account validation failed」
    expect(convertPlacementMock).not.toHaveBeenCalled()
    expect(wrapper.find('[data-testid="edit-placement-save-error"]').exists()).toBe(false)
  })

  it('房间占用与暂停调度同时成立时，公共号池按更根本的房间原因提示', async () => {
    const account = buildRoomAttachedAccount()
    account.schedulable = false

    const wrapper = mountModal(account, { accountScope: 'user', ownerUserId: 9 })

    // 不退房什么模式都切不了，先说房间的事，别让用户先去开调度开关白折腾一轮
    expect(wrapper.get('[data-testid="placement-disabled-reason-public_pool"]').text()).toBe(
      'userAccounts.externalPlacement.roomAttachedDisabledHint'
    )
  })

  it('调度正常的账号不会被误拦在公共号池外', async () => {
    const account = buildUnschedulableAccount()
    account.schedulable = true

    const wrapper = mountModal(account, { accountScope: 'user', ownerUserId: 9 })

    expect(
      wrapper.get('[data-testid="placement-target-public_pool"]').attributes('disabled')
    ).toBeUndefined()
    expect(wrapper.find('[data-testid="placement-disabled-reason-public_pool"]').exists()).toBe(false)
  })

  it('未挂房间的账号仍可自由切换模式', async () => {
    const account = buildAccount()
    account.type = 'oauth'
    account.credentials = { access_token: 'oauth-token' }
    account.share_mode = 'private'

    const wrapper = mountModal(account, { accountScope: 'user', ownerUserId: 9 })

    expect(
      wrapper.get('[data-testid="placement-target-private"]').attributes('disabled')
    ).toBeUndefined()
    expect(
      wrapper.get('[data-testid="placement-target-public_pool"]').attributes('disabled')
    ).toBeUndefined()
    expect(wrapper.find('[data-testid="placement-disabled-reason-public_pool"]').exists()).toBe(false)
  })
})
