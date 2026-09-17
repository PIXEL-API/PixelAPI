import { describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import { defineComponent, nextTick, ref } from 'vue'
import CNProviderSettings from '../CNProviderSettings.vue'

vi.mock('vue-i18n', async (importOriginal) => {
  const actual = await importOriginal<typeof import('vue-i18n')>()
  const { default: zh } = await import('@/i18n/locales/zh')
  const resolve = (key: string): unknown =>
    key.split('.').reduce<unknown>((o, k) => (o && typeof o === 'object' ? (o as Record<string, unknown>)[k] : undefined), zh)
  const t = (key: string, params?: Record<string, unknown>): string => {
    const v = resolve(key)
    if (typeof v !== 'string') return key
    if (!params) return v
    return Object.entries(params).reduce((s, [k, val]) => s.replaceAll(`{${k}}`, String(val)), v)
  }
  return {
    ...actual,
    useI18n: () => ({ t }),
  }
})

const paygChat = () => ({
  mode: 'payg' as const,
  protocol: 'chat_completions' as const,
  base_url: '',
  api_base_urls: {}
})

function latestModelValue(wrapper: ReturnType<typeof mount>) {
  const updates = wrapper.emitted('update:modelValue') || []
  return updates.at(-1)?.[0] as {
    mode: string
    protocol: string
    base_url: string
    api_base_urls: Record<string, string>
  }
}

describe('CNProviderSettings Qwen', () => {
  it('uses the requested account mode labels and locks official URLs', async () => {
    const wrapper = mount(CNProviderSettings, {
      props: { platform: 'qwen', modelValue: paygChat(), allowCustomBaseUrl: false }
    })

    expect(wrapper.find('option[value="payg"]').text()).toBe('API key')
    expect(wrapper.find('option[value="coding"]').text()).toBe('Coding Plan')
    expect(wrapper.findAll('input[type="url"]')).toHaveLength(0)
    expect(wrapper.find('code').text()).toBe('https://dashscope.aliyuncs.com/compatible-mode/v1')

    await wrapper.find('select').setValue('coding')
    expect(wrapper.find('code').text()).toBe('https://coding.dashscope.aliyuncs.com/v1')
  })

  it('updates official defaults across mode, protocol, and adaptive switches', async () => {
    const wrapper = mount(CNProviderSettings, {
      props: { platform: 'qwen', modelValue: paygChat() }
    })
    const [modeSelect, protocolSelect] = wrapper.findAll('select')

    await modeSelect.setValue('coding')
    expect(latestModelValue(wrapper)).toMatchObject({
      mode: 'coding',
      protocol: 'chat_completions',
      base_url: 'https://coding.dashscope.aliyuncs.com/v1'
    })

    await protocolSelect.setValue('anthropic')
    expect(latestModelValue(wrapper)).toMatchObject({
      protocol: 'anthropic',
      base_url: 'https://coding.dashscope.aliyuncs.com/apps/anthropic'
    })

    await protocolSelect.setValue('adaptive')
    expect(latestModelValue(wrapper)).toMatchObject({
      protocol: 'adaptive',
      api_base_urls: {
        chat_completions: 'https://coding.dashscope.aliyuncs.com/v1',
        anthropic: 'https://coding.dashscope.aliyuncs.com/apps/anthropic'
      }
    })
    expect(wrapper.findAll('option').map((option) => option.attributes('value'))).not.toContain('responses')
  })

  it('locks the base URL to the official endpoint', async () => {
    const wrapper = mount(CNProviderSettings, {
      props: { platform: 'qwen', modelValue: paygChat() }
    })
    const [modeSelect, protocolSelect] = wrapper.findAll('select')

    await modeSelect.setValue('coding')
    await protocolSelect.setValue('anthropic')

    expect(wrapper.findAll('input[type="url"]')).toHaveLength(0)
    expect(latestModelValue(wrapper).base_url).toBe('https://coding.dashscope.aliyuncs.com/apps/anthropic')
  })

  it('normalizes stored Responses protocol because Qwen has no native Responses endpoint', async () => {
    const wrapper = mount(CNProviderSettings, {
      props: {
        platform: 'qwen',
        modelValue: {
          ...paygChat(),
          protocol: 'responses' as const
        }
      }
    })

    await nextTick()
    expect(latestModelValue(wrapper).protocol).toBe('chat_completions')
    expect(wrapper.findAll('option').map((option) => option.attributes('value'))).not.toContain('responses')
  })

  it('does not recursively update when a parent writes v-model updates back', async () => {
    const Harness = defineComponent({
      components: { CNProviderSettings },
      setup() {
        const model = ref(paygChat())
        return { model }
      },
      template: '<CNProviderSettings v-model="model" platform="qwen" />'
    })
    const wrapper = mount(Harness)
    const modeSelect = wrapper.find('select')

    await modeSelect.setValue('coding')
    await nextTick()

    expect((wrapper.vm as unknown as { model: ReturnType<typeof paygChat> }).model).toMatchObject({
      mode: 'coding',
      base_url: 'https://coding.dashscope.aliyuncs.com/v1'
    })
  })
})
