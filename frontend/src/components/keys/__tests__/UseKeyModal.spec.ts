import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { nextTick } from 'vue'

const clipboardCopy = vi.hoisted(() => vi.fn())

vi.mock('vue-i18n', () => ({
  useI18n: () => ({
    t: (key: string) => key
  })
}))

vi.mock('@/composables/useClipboard', () => ({
  useClipboard: () => ({
    copyToClipboard: clipboardCopy
  })
}))

import UseKeyModal from '../UseKeyModal.vue'

function mountUseKeyModal(
  attachTo?: HTMLElement,
  props: Partial<{
    show: boolean
    apiKey: string
    baseUrl: string
    platform: 'openai' | 'grok' | 'opencode'
    allowMessagesDispatch: boolean
  }> = {}
) {
  return mount(UseKeyModal, {
    ...(attachTo ? { attachTo } : {}),
    props: {
      show: true,
      apiKey: 'sk-test',
      baseUrl: 'https://example.com/v1',
      platform: 'openai',
      ...props
    },
    global: {
      stubs: {
        BaseDialog: {
          template: '<div><slot /><slot name="footer" /></div>'
        },
        Icon: {
          template: '<span />'
        }
      }
    }
  })
}

describe('UseKeyModal', () => {
  beforeEach(() => {
    clipboardCopy.mockReset()
    clipboardCopy.mockResolvedValue(true)
  })

  afterEach(() => {
    vi.useRealTimers()
    document.body.replaceChildren()
  })

  it('provides automatic roving focus and complete ARIA relationships for both tablists', async () => {
    const wrapper = mountUseKeyModal(document.body)
    const tablists = wrapper.findAll('[role="tablist"]')
    expect(tablists).toHaveLength(2)
    expect(tablists[0].attributes('aria-label')).toBe('keys.useKeyModal.clientTabsLabel')
    expect(tablists[1].attributes('aria-label')).toBe('keys.useKeyModal.shellTabsLabel')

    const clientTabs = tablists[0].findAll('[role="tab"]')
    expect(clientTabs[0].attributes('aria-selected')).toBe('true')
    expect(clientTabs[0].attributes('tabindex')).toBe('0')
    expect(clientTabs[1].attributes('tabindex')).toBe('-1')

    const clientPanel = wrapper.get(`#${clientTabs[0].attributes('aria-controls')}`)
    expect(clientPanel.attributes('role')).toBe('tabpanel')
    expect(clientPanel.attributes('aria-labelledby')).toBe(clientTabs[0].attributes('id'))

    clientTabs[0].element.focus()
    await clientTabs[0].trigger('keydown', { key: 'ArrowRight' })
    await nextTick()

    expect(clientTabs[1].attributes('aria-selected')).toBe('true')
    expect(clientTabs[1].attributes('tabindex')).toBe('0')
    expect(clientTabs[0].attributes('tabindex')).toBe('-1')
    expect(document.activeElement).toBe(clientTabs[1].element)

    await clientTabs[1].trigger('keydown', { key: 'End' })
    await nextTick()
    const lastClientTab = clientTabs[clientTabs.length - 1]
    expect(lastClientTab.attributes('aria-selected')).toBe('true')

    await lastClientTab.trigger('keydown', { key: 'Home' })
    await nextTick()
    expect(clientTabs[0].attributes('aria-selected')).toBe('true')

    const shellTablist = wrapper.findAll('[role="tablist"]')[1]
    const shellTabs = shellTablist.findAll('[role="tab"]')
    const shellPanel = wrapper.get(`#${shellTabs[0].attributes('aria-controls')}`)
    expect(shellPanel.attributes('role')).toBe('tabpanel')
    expect(shellPanel.attributes('aria-labelledby')).toBe(shellTabs[0].attributes('id'))

    shellTabs[0].element.focus()
    await shellTabs[0].trigger('keydown', { key: 'ArrowLeft' })
    await nextTick()
    const lastShellTab = shellTabs[shellTabs.length - 1]
    expect(lastShellTab.attributes('aria-selected')).toBe('true')
    expect(document.activeElement).toBe(lastShellTab.element)

    wrapper.unmount()
  })

  it('uses roving tabindex and arrow, Home, and End keys for Codex authentication mode', async () => {
    const wrapper = mountUseKeyModal(document.body)
    const radios = wrapper.findAll('[role="radio"]')
    expect(radios).toHaveLength(2)
    expect(radios[0].attributes('aria-checked')).toBe('true')
    expect(radios[0].attributes('tabindex')).toBe('0')
    expect(radios[1].attributes('tabindex')).toBe('-1')

    const radiogroup = wrapper.get('[role="radiogroup"]')
    expect(radiogroup.attributes('aria-describedby')).toBeTruthy()
    expect(wrapper.get(`#${radiogroup.attributes('aria-describedby')}`).exists()).toBe(true)

    radios[0].element.focus()
    await radios[0].trigger('keydown', { key: 'ArrowRight' })
    await nextTick()
    expect(radios[1].attributes('aria-checked')).toBe('true')
    expect(radios[1].attributes('tabindex')).toBe('0')
    expect(document.activeElement).toBe(radios[1].element)

    await radios[1].trigger('keydown', { key: 'Home' })
    await nextTick()
    expect(radios[0].attributes('aria-checked')).toBe('true')

    await radios[0].trigger('keydown', { key: 'End' })
    await nextTick()
    expect(radios[1].attributes('aria-checked')).toBe('true')

    wrapper.unmount()
  })

  it('does not let an older copy timer clear feedback for a newer code block', async () => {
    vi.useFakeTimers()
    const wrapper = mountUseKeyModal()
    const copyButtons = wrapper.findAll('[data-testid="copy-code-block"]')
    expect(copyButtons.length).toBeGreaterThanOrEqual(2)

    await copyButtons[0].trigger('click')
    await flushPromises()
    expect(copyButtons[0].text()).toContain('keys.useKeyModal.copied')

    vi.advanceTimersByTime(1000)
    await copyButtons[1].trigger('click')
    await flushPromises()
    expect(copyButtons[1].text()).toContain('keys.useKeyModal.copied')

    vi.advanceTimersByTime(1000)
    await nextTick()
    expect(copyButtons[1].text()).toContain('keys.useKeyModal.copied')

    vi.advanceTimersByTime(1000)
    await nextTick()
    expect(copyButtons[1].text()).toContain('keys.useKeyModal.copy')
    expect(copyButtons[1].text()).not.toContain('keys.useKeyModal.copied')

    wrapper.unmount()
  })

  it('clears copied feedback and its timer when generated content changes', async () => {
    vi.useFakeTimers()
    const wrapper = mountUseKeyModal()
    const copyButton = wrapper.get('[data-testid="copy-code-block"]')

    await copyButton.trigger('click')
    await flushPromises()
    expect(copyButton.text()).toContain('keys.useKeyModal.copied')
    expect(vi.getTimerCount()).toBe(1)

    await wrapper.get('[data-testid="codex-auth-mode-api-key"]').trigger('click')
    await nextTick()

    expect(wrapper.find('pre code').text()).toContain('requires_openai_auth = false')
    expect(copyButton.text()).toContain('keys.useKeyModal.copy')
    expect(copyButton.text()).not.toContain('keys.useKeyModal.copied')
    expect(vi.getTimerCount()).toBe(0)

    wrapper.unmount()
  })

  it('ignores a deferred copy result after generated content changes', async () => {
    vi.useFakeTimers()
    let resolveCopy: (success: boolean) => void = () => undefined
    clipboardCopy.mockImplementationOnce(() => new Promise<boolean>((resolve) => {
      resolveCopy = resolve
    }))

    const wrapper = mountUseKeyModal()
    const copyButton = wrapper.get('[data-testid="copy-code-block"]')
    const legacyContent = wrapper.find('pre code').text()

    await copyButton.trigger('click')
    expect(clipboardCopy).toHaveBeenCalledWith(legacyContent, 'keys.copied')

    await wrapper.get('[data-testid="codex-auth-mode-api-key"]').trigger('click')
    await nextTick()
    resolveCopy(true)
    await flushPromises()

    expect(wrapper.find('pre code').text()).toContain('requires_openai_auth = false')
    expect(copyButton.text()).toContain('keys.useKeyModal.copy')
    expect(copyButton.text()).not.toContain('keys.useKeyModal.copied')
    expect(vi.getTimerCount()).toBe(0)

    wrapper.unmount()
  })

  it('clears pending copy feedback timers when unmounted', async () => {
    vi.useFakeTimers()
    const wrapper = mountUseKeyModal()

    await wrapper.get('[data-testid="copy-code-block"]').trigger('click')
    await flushPromises()
    expect(vi.getTimerCount()).toBe(1)

    wrapper.unmount()
    expect(vi.getTimerCount()).toBe(0)
  })

  it('keeps legacy Codex authentication as the default', () => {
    const wrapper = mountUseKeyModal()

    const codeBlock = wrapper.find('pre code')
    expect(codeBlock.exists()).toBe(true)
    expect(codeBlock.text()).toContain('model = "gpt-5.5"')
    expect(codeBlock.text()).toContain('review_model = "gpt-5.5"')
    expect(codeBlock.text()).toContain('[model_providers.OpenAI]')
    expect(codeBlock.text()).not.toContain('[model_providers.codex_local_access]')
    expect(codeBlock.text()).toContain('base_url = "https://example.com/v1"')
    expect(codeBlock.text()).toContain('wire_api = "responses"')
    expect(codeBlock.text()).toContain('requires_openai_auth = true')
    expect(codeBlock.text()).not.toContain('x-openai-actor-authorization')
    expect(wrapper.get('[data-testid="codex-auth-mode-legacy"]').attributes('aria-checked')).toBe('true')
  })

  it('renders image executor headers only in Codex API Key Mode', async () => {
    const wrapper = mountUseKeyModal()

    await wrapper.get('[data-testid="codex-auth-mode-api-key"]').trigger('click')
    await nextTick()

    const codeBlock = wrapper.find('pre code')
    expect(codeBlock.text()).toContain('requires_openai_auth = false')
    expect(codeBlock.text()).toContain(
      'http_headers = { "x-openai-actor-authorization" = "local-image-extension" }'
    )
    expect(wrapper.get('[data-testid="codex-auth-mode-api-key"]').attributes('aria-checked')).toBe('true')
  })

  it('renders WebSocket Codex CLI config without local access provider alias', async () => {
    const wrapper = mountUseKeyModal()

    const wsTab = wrapper.findAll('button').find((button) =>
      button.text().includes('keys.useKeyModal.cliTabs.codexCliWs')
    )

    expect(wsTab).toBeDefined()
    await wsTab!.trigger('click')
    await nextTick()

    const codeBlock = wrapper.find('pre code')
    expect(codeBlock.exists()).toBe(true)
    expect(codeBlock.text()).toContain('[model_providers.OpenAI]')
    expect(codeBlock.text()).not.toContain('[model_providers.codex_local_access]')
    expect(codeBlock.text()).toContain('supports_websockets = true')
    expect(codeBlock.text()).toContain('[features]')
    expect(codeBlock.text()).toContain('responses_websockets_v2 = true')
  })

  it('renders GPT-5.4 mini entry in OpenCode config', async () => {
    const wrapper = mountUseKeyModal()

    const opencodeTab = wrapper.findAll('button').find((button) =>
      button.text().includes('keys.useKeyModal.cliTabs.opencode')
    )

    expect(opencodeTab).toBeDefined()
    await opencodeTab!.trigger('click')
    await nextTick()

    const codeBlock = wrapper.find('pre code')
    expect(codeBlock.exists()).toBe(true)
    expect(codeBlock.text()).toContain('"name": "GPT-5.4 Mini"')
    expect(codeBlock.text()).not.toContain('"name": "GPT-5.4 Nano"')
  })

  it('renders protocol-aware OpenCode Go config with bare model slugs', () => {
    const wrapper = mountUseKeyModal(undefined, {
      platform: 'opencode',
      baseUrl: 'https://example.com/v1'
    })

    const parsed = JSON.parse(wrapper.find('pre code').text())
    expect(Object.keys(parsed.provider)).toEqual(['opencode-go'])

    const provider = parsed.provider['opencode-go']
    expect(provider.name).toBe('OpenCode Go')
    expect(provider.npm).toBe('@ai-sdk/openai-compatible')
    expect(provider.options).toEqual({
      baseURL: 'https://example.com/v1',
      apiKey: 'sk-test'
    })
    expect(provider.options.baseURL).not.toContain('/zen/go/v1')

    const messagesModels = [
      'minimax-m3',
      'minimax-m2.7',
      'minimax-m2.5',
      'qwen3.7-max',
      'qwen3.8-max',
      'qwen3.8-flash',
      'qwen3.7-plus',
      'qwen3.6-plus'
    ]
    const responsesModels = [
      'gpt-5.6-luna',
      'grok-4.5',
      'grok-4.6',
      'muse-spark-1.2-contributor'
    ]

    for (const modelId of messagesModels) {
      expect(provider.models[modelId]).toEqual({
        provider: { npm: '@ai-sdk/anthropic' }
      })
    }
    for (const modelId of responsesModels) {
      expect(provider.models[modelId]).toEqual({
        provider: { npm: '@ai-sdk/openai' }
      })
    }

    expect(Object.keys(provider.models)).toEqual([...messagesModels, ...responsesModels])
    expect(Object.keys(provider.models).every((modelId) => !modelId.startsWith('opencode-go/'))).toBe(true)
    expect(provider.models['deepseek-v4-pro']).toBeUndefined()
  })

  it('resets Codex authentication mode when the modal reopens', async () => {
    const wrapper = mountUseKeyModal()

    await wrapper.get('[data-testid="codex-auth-mode-api-key"]').trigger('click')
    await wrapper.setProps({ show: false })
    await wrapper.setProps({ show: true })
    await nextTick()

    expect(wrapper.get('[data-testid="codex-auth-mode-legacy"]').attributes('aria-checked')).toBe('true')
    expect(wrapper.find('pre code').text()).toContain('requires_openai_auth = true')
  })

  it('falls back to the default client tab when Claude dispatch permission is revoked', async () => {
    const wrapper = mountUseKeyModal(undefined, { allowMessagesDispatch: true })
    const clientTablist = wrapper.get('[role="tablist"]')
    const claudeTab = clientTablist.findAll('[role="tab"]')
      .find((tab) => tab.text().includes('keys.useKeyModal.cliTabs.claudeCode'))

    expect(claudeTab).toBeDefined()
    await claudeTab!.trigger('click')
    await wrapper.setProps({ allowMessagesDispatch: false })
    await nextTick()

    const clientTabs = clientTablist.findAll('[role="tab"]')
    const selectedTab = clientTabs.find((tab) => tab.attributes('aria-selected') === 'true')
    const clientPanel = wrapper.get(`#${clientTabs[0].attributes('aria-controls')}`)

    expect(clientTabs.some((tab) => tab.attributes('id') === claudeTab!.attributes('id'))).toBe(false)
    expect(selectedTab?.attributes('id')).toBe(clientTabs[0].attributes('id'))
    expect(clientPanel.attributes('aria-labelledby')).toBe(selectedTab?.attributes('id'))
    expect(clientPanel.attributes('aria-labelledby')).not.toBe(claudeTab!.attributes('id'))
  })

  it('renders Grok Build and OpenCode setup for Grok groups', async () => {
    const wrapper = mount(UseKeyModal, {
      props: {
        show: true,
        apiKey: 'sk-grok-test',
        baseUrl: 'https://example.com/v1',
        platform: 'grok'
      },
      global: {
        stubs: {
          BaseDialog: {
            template: '<div><slot /><slot name="footer" /></div>'
          },
          Icon: {
            template: '<span />'
          }
        }
      }
    })

    const grokConfig = wrapper.findAll('pre code')
      .map((code) => code.text())
      .find((content) => content.includes('[model."grok"]'))
    expect(grokConfig).toContain('model = "grok-4.5"')
    expect(grokConfig).toContain('base_url = "https://example.com/v1"')
    expect(grokConfig).toContain('api_key = "sk-grok-test"')

    const opencodeTab = wrapper.findAll('button').find((button) =>
      button.text().includes('keys.useKeyModal.cliTabs.opencode')
    )
    expect(opencodeTab).toBeDefined()
    await opencodeTab!.trigger('click')
    await nextTick()

    const parsed = JSON.parse(wrapper.find('pre code').text())
    expect(parsed.provider.grok.name).toBe('Grok')
    expect(parsed.provider.grok.npm).toBe('@ai-sdk/openai')
    expect(parsed.provider.grok.models['grok-4.5']).toBeDefined()
    expect(parsed.provider.grok.models['grok-build-0.1']).toBeDefined()
    expect(parsed.provider.grok.models['gpt-5.6']).toBeUndefined()
  })
})
