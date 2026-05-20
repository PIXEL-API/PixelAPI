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
  it('includes codex_local_access provider alias in Codex CLI config', () => {
    const wrapper = mountUseKeyModal()

    const codeBlock = wrapper.find('pre code')
    expect(codeBlock.exists()).toBe(true)
    expect(codeBlock.text()).toContain('[model_providers.OpenAI]')
    expect(codeBlock.text()).toContain('[model_providers.codex_local_access]')
    expect(codeBlock.text()).toContain('base_url = "https://example.com/v1"')
    expect(codeBlock.text()).toContain('wire_api = "responses"')
  })

  it('keeps codex_local_access provider alias in WebSocket Codex CLI config', async () => {
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
    expect(codeBlock.text()).toContain('[model_providers.codex_local_access]')
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
})
