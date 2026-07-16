import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import { nextTick } from 'vue'

vi.mock('vue-i18n', () => ({
  useI18n: () => ({
    t: (key: string) => key
  })
}))

vi.mock('@/composables/useClipboard', () => ({
  useClipboard: () => ({
    copyToClipboard: vi.fn().mockResolvedValue(true)
  })
}))

import UseKeyModal from '../UseKeyModal.vue'

function mountUseKeyModal() {
  return mount(UseKeyModal, {
    props: {
      show: true,
      apiKey: 'sk-test',
      baseUrl: 'https://example.com/v1',
      platform: 'openai'
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

  it('resets Codex authentication mode when the modal reopens', async () => {
    const wrapper = mountUseKeyModal()

    await wrapper.get('[data-testid="codex-auth-mode-api-key"]').trigger('click')
    await wrapper.setProps({ show: false })
    await wrapper.setProps({ show: true })
    await nextTick()

    expect(wrapper.get('[data-testid="codex-auth-mode-legacy"]').attributes('aria-checked')).toBe('true')
    expect(wrapper.find('pre code').text()).toContain('requires_openai_auth = true')
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
