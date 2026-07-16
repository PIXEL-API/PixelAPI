import { mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'
import PricingEntryCard from '../PricingEntryCard.vue'
import {
  MAX_LONG_CONTEXT_INPUT_TOKEN_THRESHOLD,
  apiLongContextPricingToForm,
  createPricingFormEntry,
  formLongContextPricingToAPI,
  validateLongContextPricing,
  type IntervalFormEntry,
  type PricingFormEntry,
} from '../types'

vi.mock('@/api/admin/channels', () => ({
  default: {
    getModelDefaultPricing: vi.fn(),
  },
}))

vi.mock('vue-i18n', () => ({
  useI18n: () => ({
    t: (key: string, arg?: unknown, fallback?: string) => {
      if (typeof arg === 'string') return arg
      return fallback || key
    },
  }),
}))

function createInterval(): IntervalFormEntry {
  return {
    min_tokens: 0,
    max_tokens: 100_000,
    tier_label: '',
    input_price: 1,
    output_price: 2,
    cache_write_price: null,
    cache_read_price: null,
    per_request_price: null,
    sort_order: 0,
  }
}

function mountCard(
  entry: PricingFormEntry,
  showLongContextPricing = true,
  platform = 'openai',
) {
  return mount(PricingEntryCard, {
    props: {
      entry,
      platform,
      showLongContextPricing,
    },
    global: {
      stubs: {
        Icon: true,
        IntervalRow: true,
        ModelTagInput: true,
        Select: true,
      },
    },
  })
}

describe('channel long-context pricing form helpers', () => {
  it('creates OpenAI main pricing entries as explicitly disabled by default', () => {
    const entry = createPricingFormEntry({ longContextPricingEnabled: false })

    expect(entry.long_context_pricing_enabled).toBe(false)
    expect(entry.long_context_input_token_threshold).toBeNull()
  })

  it('keeps account-stats entries unset and preserves unset API responses', () => {
    const entry = createPricingFormEntry()
    const inherited = apiLongContextPricingToForm({
      long_context_pricing_enabled: null,
      long_context_input_token_threshold: null,
    })

    expect(entry.long_context_pricing_enabled).toBeNull()
    expect(entry.long_context_input_token_threshold).toBeNull()
    expect(inherited).toEqual({
      long_context_pricing_enabled: null,
      long_context_input_token_threshold: null,
    })
  })

  it('serializes an explicit disable without retaining a stale threshold', () => {
    expect(formLongContextPricingToAPI({
      long_context_pricing_enabled: false,
      long_context_input_token_threshold: 300_000,
    })).toEqual({
      long_context_pricing_enabled: false,
      long_context_input_token_threshold: null,
    })
  })

  it('rejects missing, overflowing, and interval-conflicting explicit settings', () => {
    const entry = createPricingFormEntry({ longContextPricingEnabled: true })
    expect(validateLongContextPricing(entry)).toBe('threshold_required')

    entry.long_context_input_token_threshold = MAX_LONG_CONTEXT_INPUT_TOKEN_THRESHOLD + 1
    expect(validateLongContextPricing(entry)).toBe('threshold_required')

    entry.long_context_input_token_threshold = '300000'
    entry.intervals = [createInterval()]
    expect(validateLongContextPricing(entry)).toBe('interval_conflict')

    entry.intervals = []
    expect(validateLongContextPricing(entry)).toBeNull()
  })
})

describe('PricingEntryCard long-context controls', () => {
  it('does not render controls for the account-stats reuse', () => {
    const wrapper = mountCard(createPricingFormEntry(), false)

    expect(wrapper.find('[data-testid="long-context-pricing"]').exists()).toBe(false)
  })

  it('does not render OpenAI controls on another platform', () => {
    const wrapper = mountCard(createPricingFormEntry(), true, 'anthropic')

    expect(wrapper.find('[data-testid="long-context-pricing"]').exists()).toBe(false)
  })

  it('shows default-off without materializing null and makes threshold edits explicit', async () => {
    const wrapper = mountCard(createPricingFormEntry())

    expect(wrapper.text()).toContain('默认关闭（未显式开启）')
    expect(wrapper.emitted('update')).toBeUndefined()

    await wrapper.get('[data-testid="long-context-threshold"]').setValue('300000')
    const update = wrapper.emitted<Parameters<(entry: PricingFormEntry) => void>>('update')?.at(-1)
    expect(update?.[0]).toMatchObject({
      long_context_pricing_enabled: true,
      long_context_input_token_threshold: '300000',
    })
  })

  it('keeps custom intervals in control and prevents the default-off policy from being enabled', async () => {
    const entry = createPricingFormEntry()
    entry.intervals = [createInterval()]
    const wrapper = mountCard(entry)
    const toggle = wrapper.get<HTMLButtonElement>('[data-testid="long-context-toggle"]')

    expect(wrapper.text()).toContain('自定义上下文区间已接管')
    expect(toggle.element.disabled).toBe(true)
    expect(wrapper.emitted('update')).toBeUndefined()
  })

  it('allows a conflicting explicit policy to be turned off without deleting intervals', async () => {
    const entry = createPricingFormEntry({ longContextPricingEnabled: true })
    entry.long_context_input_token_threshold = 300_000
    entry.intervals = [createInterval()]
    const wrapper = mountCard(entry)
    const toggle = wrapper.get<HTMLButtonElement>('[data-testid="long-context-toggle"]')

    expect(toggle.element.disabled).toBe(false)
    await toggle.trigger('click')

    const update = wrapper.emitted<Parameters<(entry: PricingFormEntry) => void>>('update')?.at(-1)
    expect(update?.[0]).toMatchObject({
      long_context_pricing_enabled: false,
      long_context_input_token_threshold: null,
    })
  })

  it('disables adding custom intervals while the explicit policy is enabled', () => {
    const entry = createPricingFormEntry({ longContextPricingEnabled: true })
    entry.long_context_input_token_threshold = 300_000
    const wrapper = mountCard(entry)

    expect(wrapper.get<HTMLButtonElement>('[data-testid="add-token-interval"]').element.disabled).toBe(true)
    expect(wrapper.text()).toContain('关闭后才能添加自定义区间')
  })

  it('clears long-context fields when switching away from token billing', async () => {
    const entry = createPricingFormEntry({ longContextPricingEnabled: true })
    entry.long_context_input_token_threshold = 300_000
    const wrapper = mountCard(entry)

    wrapper.findComponent({ name: 'Select' }).vm.$emit('update:modelValue', 'per_request')
    const update = wrapper.emitted<Parameters<(entry: PricingFormEntry) => void>>('update')?.at(-1)
    expect(update?.[0]).toMatchObject({
      billing_mode: 'per_request',
      long_context_pricing_enabled: null,
      long_context_input_token_threshold: null,
    })
  })
})
