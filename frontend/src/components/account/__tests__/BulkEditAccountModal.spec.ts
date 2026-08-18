import { describe, expect, it, vi, beforeEach } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import BulkEditAccountModal from '../BulkEditAccountModal.vue'
import ModelWhitelistSelector from '../ModelWhitelistSelector.vue'
import { adminAPI } from '@/api/admin'
import { accountsAPI } from '@/api/accounts'
import { createPinia, setActivePinia } from 'pinia'

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({
    showError: vi.fn(),
    showSuccess: vi.fn(),
    showInfo: vi.fn()
  })
}))

vi.mock('@/stores/auth', () => ({
  useAuthStore: () => ({
    refreshUser: vi.fn().mockResolvedValue(undefined)
  })
}))

vi.mock('@/api/admin', () => ({
  adminAPI: {
    accounts: {
      bulkUpdate: vi.fn(),
      checkMixedChannelRisk: vi.fn()
    }
  }
}))

vi.mock('@/api/accounts', () => ({
  accountsAPI: {
    bulkUpdate: vi.fn(),
    convertExternalPlacementBatch: vi.fn()
  }
}))

vi.mock('@/api/admin/accounts', () => ({
  getAntigravityDefaultModelMapping: vi.fn()
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

function makeGroup(overrides: Record<string, unknown>) {
  return {
    id: 1,
    name: 'group',
    description: null,
    platform: 'openai',
    rate_multiplier: 1,
    rpm_limit: 0,
    is_exclusive: false,
    status: 'active',
    owner_user_id: null,
    scope: 'user_private',
    subscription_type: 'standard',
    daily_limit_usd: null,
    weekly_limit_usd: null,
    monthly_limit_usd: null,
    image_price_1k: null,
    image_price_2k: null,
    image_price_4k: null,
    claude_code_only: false,
    fallback_group_id: null,
    fallback_group_id_on_invalid_request: null,
    allow_messages_dispatch: false,
    require_oauth_only: false,
    require_privacy_set: false,
    created_at: '',
    updated_at: '',
    model_routing: null,
    model_routing_enabled: false,
    mcp_xml_inject: false,
    supported_model_scopes: [],
    account_count: 0,
    active_account_count: 0,
    rate_limited_account_count: 0,
    sort_order: 0,
    ...overrides
  }
}

function mountModal(extraProps: Record<string, unknown> = {}, extraStubs: Record<string, unknown> = {}) {
  return mount(BulkEditAccountModal, {
    props: {
      show: true,
      accountIds: [1, 2],
      selectedPlatforms: ['antigravity'],
      selectedTypes: ['apikey'],
      proxies: [],
      groups: [],
      ...extraProps
    } as any,
    global: {
      stubs: {
        BaseDialog: { template: '<div><slot /><slot name="footer" /></div>' },
        ConfirmDialog: true,
        Select: {
          props: ['modelValue', 'options'],
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
        },
        ProxySelector: true,
        GroupSelector: true,
        Icon: true,
        ...extraStubs
      }
    }
  })
}

describe('BulkEditAccountModal', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    vi.mocked(adminAPI.accounts.bulkUpdate).mockReset()
    vi.mocked(adminAPI.accounts.checkMixedChannelRisk).mockReset()
    vi.mocked(accountsAPI.bulkUpdate).mockReset()
    vi.mocked(accountsAPI.convertExternalPlacementBatch).mockReset()

    vi.mocked(adminAPI.accounts.bulkUpdate).mockResolvedValue({
      success: 2,
      failed: 0,
      results: []
    } as any)
    vi.mocked(adminAPI.accounts.checkMixedChannelRisk).mockResolvedValue({
      has_risk: false
    } as any)
    vi.mocked(accountsAPI.bulkUpdate).mockResolvedValue({
      success: 2,
      failed: 0,
      results: []
    } as any)
    vi.mocked(accountsAPI.convertExternalPlacementBatch).mockResolvedValue({
      success: 2,
      failed: 0,
      success_ids: [1, 2],
      failed_ids: [],
      results: [
        { account_id: 1, success: true },
        { account_id: 2, success: true }
      ]
    } as any)
  })

  it('antigravity 白名单包含 Gemini 图片模型且过滤掉普通 GPT 模型', async () => {
    const wrapper = mountModal()
    const selector = wrapper.findComponent(ModelWhitelistSelector)
    expect(selector.exists()).toBe(true)

    await selector.find('div.cursor-pointer').trigger('click')

    expect(wrapper.text()).toContain('gemini-3.1-flash-image')
    expect(wrapper.text()).toContain('gemini-2.5-flash-image')
    expect(wrapper.text()).not.toContain('gpt-5.3-codex')
  })

  it('antigravity 映射预设包含图片映射并过滤 OpenAI 预设', async () => {
    const wrapper = mountModal()

    const mappingTab = wrapper.findAll('button').find((btn) => btn.text().includes('admin.accounts.modelMapping'))
    expect(mappingTab).toBeTruthy()
    await mappingTab!.trigger('click')

    expect(wrapper.text()).toContain('Flash-Image')
    expect(wrapper.text()).toContain('Pro-Image')
    expect(wrapper.text()).not.toContain('GPT-5.3 Codex Spark')
  })

  it('仅勾选模型限制且白名单留空时，应提交空 model_mapping 以支持所有模型', async () => {
    const wrapper = mountModal({
      selectedPlatforms: ['anthropic'],
      selectedTypes: ['apikey']
    })

    await wrapper.get('#bulk-edit-model-restriction-enabled').setValue(true)
    await wrapper.get('#bulk-edit-account-form').trigger('submit.prevent')
    await flushPromises()

    expect(adminAPI.accounts.bulkUpdate).toHaveBeenCalledTimes(1)
    expect(adminAPI.accounts.bulkUpdate).toHaveBeenCalledWith([1, 2], {
      credentials: {
        model_mapping: {}
      }
    })
  })

  it('个人账号侧 OAuth 账号清空白名单应提交空 model_mapping 而不是被清洗掉', async () => {
    const wrapper = mountModal({
      accountScope: 'user',
      selectedPlatforms: ['openai'],
      selectedTypes: ['oauth']
    })

    await wrapper.get('#bulk-edit-model-restriction-enabled').setValue(true)
    await wrapper.get('#bulk-edit-account-form').trigger('submit.prevent')
    await flushPromises()

    expect(accountsAPI.bulkUpdate).toHaveBeenCalledTimes(1)
    expect(accountsAPI.bulkUpdate).toHaveBeenCalledWith([1, 2], {
      credentials: {
        model_mapping: {}
      }
    })
  })

  it('OpenAI 账号批量编辑可开启自动透传', async () => {
    const wrapper = mountModal({
      selectedPlatforms: ['openai'],
      selectedTypes: ['oauth']
    })

    await wrapper.get('#bulk-edit-openai-passthrough-enabled').setValue(true)
    await wrapper.get('#bulk-edit-openai-passthrough-toggle').trigger('click')
    await wrapper.get('#bulk-edit-account-form').trigger('submit.prevent')
    await flushPromises()

    expect(adminAPI.accounts.bulkUpdate).toHaveBeenCalledTimes(1)
    expect(adminAPI.accounts.bulkUpdate).toHaveBeenCalledWith([1, 2], {
      extra: {
        openai_passthrough: true
      }
    })
  })

  it('OpenAI 管理员批量编辑可提交账号等级', async () => {
    const wrapper = mountModal({
      selectedPlatforms: ['openai'],
      selectedTypes: ['oauth']
    })

    await wrapper.get('#bulk-edit-account-level-enabled').setValue(true)
    await wrapper.get('#bulk-edit-account-level').setValue('plus')
    await wrapper.get('#bulk-edit-account-form').trigger('submit.prevent')
    await flushPromises()

    expect(adminAPI.accounts.bulkUpdate).toHaveBeenCalledTimes(1)
    expect(adminAPI.accounts.bulkUpdate).toHaveBeenCalledWith([1, 2], {
      account_level: 'plus'
    })
  })

  it('非 OpenAI 账号批量编辑不显示账号等级入口', () => {
    const wrapper = mountModal({
      selectedPlatforms: ['anthropic'],
      selectedTypes: ['oauth']
    })

    expect(wrapper.find('#bulk-edit-account-level-enabled').exists()).toBe(false)
  })

  it('OpenAI OAuth 批量编辑应提交 OAuth 专属 WS mode 字段', async () => {
    const wrapper = mountModal({
      selectedPlatforms: ['openai'],
      selectedTypes: ['oauth']
    })

    await wrapper.get('#bulk-edit-openai-ws-mode-enabled').setValue(true)
    await wrapper.get('[data-testid="bulk-edit-openai-ws-mode-select"]').setValue('passthrough')
    await wrapper.get('#bulk-edit-account-form').trigger('submit.prevent')
    await flushPromises()

    expect(adminAPI.accounts.bulkUpdate).toHaveBeenCalledTimes(1)
    expect(adminAPI.accounts.bulkUpdate).toHaveBeenCalledWith([1, 2], {
      extra: {
        openai_oauth_responses_websockets_v2_mode: 'passthrough',
        openai_oauth_responses_websockets_v2_enabled: true
      }
    })
  })

  it('OpenAI API Key 批量编辑不显示 WS mode 入口', () => {
    const wrapper = mountModal({
      selectedPlatforms: ['openai'],
      selectedTypes: ['apikey']
    })

    expect(wrapper.find('#bulk-edit-openai-ws-mode-enabled').exists()).toBe(false)
  })

  it('OpenAI OAuth 批量编辑应提交 codex_cli_only 字段', async () => {
    const wrapper = mountModal({
      selectedPlatforms: ['openai'],
      selectedTypes: ['oauth']
    })

    await wrapper.get('#bulk-edit-openai-codex-cli-only-enabled').setValue(true)
    await wrapper.get('#bulk-edit-openai-codex-cli-only-toggle').trigger('click')
    await wrapper.get('#bulk-edit-account-form').trigger('submit.prevent')
    await flushPromises()

    expect(adminAPI.accounts.bulkUpdate).toHaveBeenCalledTimes(1)
    expect(adminAPI.accounts.bulkUpdate).toHaveBeenCalledWith([1, 2], {
      extra: {
        codex_cli_only: true
      }
    })
  })

  it('OpenAI API Key 批量编辑应提交 API Key 专属 WS mode 字段', async () => {
    const wrapper = mountModal({
      selectedPlatforms: ['openai'],
      selectedTypes: ['apikey']
    })

    await wrapper.get('#bulk-edit-openai-apikey-ws-mode-enabled').setValue(true)
    await wrapper.get('[data-testid="bulk-edit-openai-apikey-ws-mode-select"]').setValue('ctx_pool')
    await wrapper.get('#bulk-edit-account-form').trigger('submit.prevent')
    await flushPromises()

    expect(adminAPI.accounts.bulkUpdate).toHaveBeenCalledTimes(1)
    expect(adminAPI.accounts.bulkUpdate).toHaveBeenCalledWith([1, 2], {
      extra: {
        openai_apikey_responses_websockets_v2_mode: 'ctx_pool',
        openai_apikey_responses_websockets_v2_enabled: true
      }
    })
  })

  it('OpenAI 账号批量编辑可关闭自动透传', async () => {
    const wrapper = mountModal({
      selectedPlatforms: ['openai'],
      selectedTypes: ['apikey']
    })

    await wrapper.get('#bulk-edit-openai-passthrough-enabled').setValue(true)
    await wrapper.get('#bulk-edit-account-form').trigger('submit.prevent')
    await flushPromises()

    expect(adminAPI.accounts.bulkUpdate).toHaveBeenCalledTimes(1)
    expect(adminAPI.accounts.bulkUpdate).toHaveBeenCalledWith([1, 2], {
      extra: {
        openai_passthrough: false,
        openai_oauth_passthrough: false
      }
    })
  })

  it('开启 OpenAI 自动透传时不再同时提交模型限制', async () => {
    const wrapper = mountModal({
      selectedPlatforms: ['openai'],
      selectedTypes: ['oauth']
    })

    await wrapper.get('#bulk-edit-openai-passthrough-enabled').setValue(true)
    await wrapper.get('#bulk-edit-openai-passthrough-toggle').trigger('click')
    await wrapper.get('#bulk-edit-model-restriction-enabled').setValue(true)
    await wrapper.get('#bulk-edit-account-form').trigger('submit.prevent')
    await flushPromises()

    expect(adminAPI.accounts.bulkUpdate).toHaveBeenCalledTimes(1)
    expect(adminAPI.accounts.bulkUpdate).toHaveBeenCalledWith([1, 2], {
      extra: {
        openai_passthrough: true
      }
    })
    expect(wrapper.text()).toContain('admin.accounts.openai.modelRestrictionDisabledByPassthrough')
  })

  it('filtered-results 模式下应提交 filters 而不是 account_ids', async () => {
    const wrapper = mountModal({
      accountIds: [],
      target: {
        mode: 'filtered',
        filters: {
          platform: 'openai',
          type: 'oauth',
          status: 'active',
          group: '12',
          search: 'bulk-target',
          privacy_mode: 'training_set_cf_blocked'
        },
        previewCount: 5,
        selectedPlatforms: ['openai'],
        selectedTypes: ['oauth']
      }
    })

    await wrapper.get('#bulk-edit-status-enabled').setValue(true)
    await wrapper.get('#bulk-edit-account-form').trigger('submit.prevent')
    await flushPromises()

    expect(adminAPI.accounts.bulkUpdate).toHaveBeenCalledTimes(1)
    expect(adminAPI.accounts.bulkUpdate).toHaveBeenCalledWith({
      filters: {
        platform: 'openai',
        type: 'oauth',
        status: 'active',
        group: '12',
        search: 'bulk-target',
        privacy_mode: 'training_set_cf_blocked'
      },
      status: 'active'
    })
  })

  it('用户作用域不再展示旧共享模式和分组入口', async () => {
    const wrapper = mountModal({
      accountScope: 'user',
      selectedPlatforms: ['openai'],
      selectedTypes: ['apikey'],
      groups: [
        makeGroup({ id: 1, name: 'private-u9-openai', platform: 'openai' }),
        makeGroup({ id: 2, name: 'private-u9-anthropic', platform: 'anthropic' }),
        makeGroup({ id: 3, name: 'private-u9-gemini', platform: 'gemini' }),
        makeGroup({ id: 4, name: 'Codex OAuth Only', platform: 'openai', require_oauth_only: true })
      ]
    }, {
      GroupSelector: {
        props: ['groups'],
        template: `
          <div>
            <span v-for="group in groups" :key="group.id" class="group-option">{{ group.name }}</span>
          </div>
        `
      }
    })

    expect(wrapper.find('#bulk-edit-share-mode-enabled').exists()).toBe(false)
    expect(wrapper.find('#bulk-edit-groups-enabled').exists()).toBe(false)
    expect(wrapper.text()).not.toContain('private-u9-openai')
  })

  it('用户作用域可仅批量设置三个模式之一而不提交普通字段更新', async () => {
    vi.spyOn(globalThis.crypto, 'randomUUID').mockReturnValue(
      '22222222-2222-4222-8222-222222222222'
    )
    const wrapper = mountModal({
      accountScope: 'user',
      ownerUserId: 9,
      selectedPlatforms: ['openai'],
      selectedTypes: ['oauth'],
      selectedAccountLevels: ['plus']
    })

    expect(wrapper.find('[data-testid="placement-target-private"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="placement-target-public_pool"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="placement-target-room"]').exists()).toBe(true)

    await wrapper.get('#bulk-edit-external-placement-enabled').setValue(true)
    await wrapper.get('[data-testid="placement-target-room"]').setValue()
    await wrapper.get('#bulk-edit-account-form').trigger('submit.prevent')
    await flushPromises()

    expect(accountsAPI.bulkUpdate).not.toHaveBeenCalled()
    expect(accountsAPI.convertExternalPlacementBatch).toHaveBeenCalledWith({
      account_ids: [1, 2],
      target: 'room',
      idempotency_key: 'batch-placement-22222222-2222-4222-8222-222222222222'
    })
    expect(wrapper.emitted('updated')).toHaveLength(1)
  })

  it('同平台账号等级不一致时仍可设置平台账号模式且不选择房间', async () => {
    const wrapper = mountModal({
      accountScope: 'user',
      ownerUserId: 9,
      selectedPlatforms: ['openai'],
      selectedTypes: ['oauth'],
      selectedAccountLevels: ['plus', 'team']
    })

    await wrapper.get('#bulk-edit-external-placement-enabled').setValue(true)

    expect(
      (wrapper.get('[data-testid="placement-target-room"]').element as HTMLInputElement).disabled
    ).toBe(false)
    expect(wrapper.find('[data-testid="refresh-compatible-rooms"]').exists()).toBe(false)
  })

  it('普通字段已保存但模式全部失败时仍通知父级刷新列表', async () => {
    vi.spyOn(globalThis.crypto, 'randomUUID').mockReturnValue(
      '33333333-3333-4333-8333-333333333333'
    )
    vi.mocked(accountsAPI.bulkUpdate).mockResolvedValueOnce({
      success: 2,
      failed: 0,
      success_ids: [1, 2],
      failed_ids: [],
      results: [
        { account_id: 1, success: true },
        { account_id: 2, success: true }
      ]
    } as any)
    vi.mocked(accountsAPI.convertExternalPlacementBatch).mockResolvedValueOnce({
      success: 0,
      failed: 2,
      success_ids: [],
      failed_ids: [1, 2],
      results: [
        { account_id: 1, success: false, error: 'busy', reason: 'ACCOUNT_EXTERNAL_PLACEMENT_BUSY' },
        { account_id: 2, success: false, error: 'busy', reason: 'ACCOUNT_EXTERNAL_PLACEMENT_BUSY' }
      ]
    } as any)
    const wrapper = mountModal({
      accountScope: 'user',
      ownerUserId: 9,
      selectedPlatforms: ['openai'],
      selectedTypes: ['oauth'],
      selectedAccountLevels: ['plus']
    })

    await wrapper.get('#bulk-edit-concurrency-enabled').setValue(true)
    await wrapper.get('#bulk-edit-external-placement-enabled').setValue(true)
    await wrapper.get('[data-testid="placement-target-public_pool"]').setValue()
    await wrapper.get('#bulk-edit-account-form').trigger('submit.prevent')
    await flushPromises()

    expect(accountsAPI.bulkUpdate).toHaveBeenCalledTimes(1)
    expect(accountsAPI.convertExternalPlacementBatch).toHaveBeenCalledTimes(1)
    expect(wrapper.emitted('updated')).toHaveLength(1)
  })

  it('批量模式切换部分失败时展示失败账号明细', async () => {
    vi.spyOn(globalThis.crypto, 'randomUUID').mockReturnValue(
      '44444444-4444-4444-8444-444444444444'
    )
    vi.mocked(accountsAPI.bulkUpdate).mockResolvedValueOnce({
      success: 2,
      failed: 0,
      success_ids: [1, 2],
      failed_ids: [],
      results: [
        { account_id: 1, success: true },
        { account_id: 2, success: true }
      ]
    } as any)
    vi.mocked(accountsAPI.convertExternalPlacementBatch).mockResolvedValueOnce({
      success: 1,
      failed: 1,
      success_ids: [1],
      failed_ids: [2],
      results: [
        { account_id: 1, success: true },
        { account_id: 2, success: false, error: 'public account validation failed', reason: 'OWNED_ACCOUNT_PUBLIC_VALIDATION_FAILED', message: 'public account validation failed' }
      ]
    } as any)
    const wrapper = mountModal({
      accountScope: 'user',
      ownerUserId: 9,
      selectedPlatforms: ['openai'],
      selectedTypes: ['oauth'],
      selectedAccountLevels: ['plus']
    })

    await wrapper.get('#bulk-edit-external-placement-enabled').setValue(true)
    await wrapper.get('[data-testid="placement-target-public_pool"]').setValue()
    await wrapper.get('#bulk-edit-account-form').trigger('submit.prevent')
    await flushPromises()

    expect(accountsAPI.convertExternalPlacementBatch).toHaveBeenCalledTimes(1)
    const errorArea = wrapper.get('[data-testid="bulk-placement-submit-error"]')
    expect(errorArea.text()).toContain('#2')
    // i18n mock 的 t 直接返回 key，extractI18nErrorMessage 在无映射时退回后端 message。
    expect(errorArea.text()).toContain('public account validation failed')
    const details = wrapper.get('[data-testid="bulk-placement-failed-details"]')
    expect(details.text()).toContain('#2')
    // 部分失败时弹窗保持打开，失败明细才不会被关闭清空。
    expect(wrapper.emitted('close')).toBeUndefined()
  })

  it('用户作用域提交普通字段时调用用户接口且不包含 share_mode', async () => {
    const wrapper = mountModal({
      accountScope: 'user',
      selectedPlatforms: ['openai'],
      selectedTypes: ['oauth'],
      groups: [
        makeGroup({ id: 1, name: 'private-u9-openai', platform: 'openai' }),
        makeGroup({ id: 2, name: 'private-u9-anthropic', platform: 'anthropic' })
      ]
    }, {
      GroupSelector: {
        props: ['groups'],
        emits: ['update:modelValue'],
        template: `
          <div>
            <button
              v-for="group in groups"
              :key="group.id"
              type="button"
              class="group-option"
              @click="$emit('update:modelValue', [group.id])"
            >
              {{ group.name }}
            </button>
          </div>
        `
      }
    })

    await wrapper.get('#bulk-edit-concurrency-enabled').setValue(true)
    await wrapper.get('#bulk-edit-concurrency').setValue(8)
    await wrapper.get('#bulk-edit-account-form').trigger('submit.prevent')
    await flushPromises()

    expect(accountsAPI.bulkUpdate).toHaveBeenCalledWith([1, 2], {
      concurrency: 8
    })
    expect(vi.mocked(accountsAPI.bulkUpdate).mock.calls[0]?.[1]).not.toHaveProperty('share_mode')
    expect(adminAPI.accounts.bulkUpdate).not.toHaveBeenCalled()
  })

  it('用户作用域普通批量更新支持后台任务响应', async () => {
    vi.mocked(accountsAPI.bulkUpdate).mockResolvedValueOnce({
      async: true,
      task: {
        id: 77,
        scope: 'user',
        operation: 'user_bulk_update',
        status: 'pending',
        total: 2,
        processed: 0,
        success: 0,
        failed: 0,
        created_by: 9,
      },
      success: 0,
      failed: 0,
      results: []
    } as any)
    const wrapper = mountModal({
      accountScope: 'user',
      selectedPlatforms: ['openai'],
      selectedTypes: ['oauth']
    })

    await wrapper.get('#bulk-edit-concurrency-enabled').setValue(true)
    await wrapper.get('#bulk-edit-concurrency').setValue(8)
    await wrapper.get('#bulk-edit-account-form').trigger('submit.prevent')
    await flushPromises()

    expect(accountsAPI.bulkUpdate).toHaveBeenCalledWith([1, 2], {
      concurrency: 8
    })
    expect(vi.mocked(accountsAPI.bulkUpdate).mock.calls[0]?.[1]).not.toHaveProperty('share_mode')
    expect(wrapper.emitted('updated')?.[0]).toEqual([
      expect.objectContaining({
        async: true,
        task: expect.objectContaining({ id: 77, operation: 'user_bulk_update' })
      })
    ])
  })

  it('用户作用域批量编辑不暴露 Grok 上游和请求头覆写入口', () => {
    const wrapper = mountModal({
      accountScope: 'user',
      selectedPlatforms: ['grok'],
      selectedTypes: ['oauth']
    })

    expect(wrapper.find('[data-testid="bulk-grok-header-override"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="grok-base-url-preset"]').exists()).toBe(false)
  })
})
