import { describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import { defineComponent, nextTick, ref } from 'vue'
import CNProviderSettings from '../CNProviderSettings.vue'

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

  it('preserves a user-entered base URL when defaults change', async () => {
    const wrapper = mount(CNProviderSettings, {
      props: { platform: 'qwen', modelValue: paygChat() }
    })
    const [modeSelect, protocolSelect] = wrapper.findAll('select')

    await wrapper.get('input[type="url"]').setValue('https://relay.example.test/qwen')
    await modeSelect.setValue('coding')
    await protocolSelect.setValue('anthropic')

    expect(latestModelValue(wrapper).base_url).toBe('https://relay.example.test/qwen')
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
