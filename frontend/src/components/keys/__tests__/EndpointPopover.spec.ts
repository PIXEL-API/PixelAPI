import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { nextTick } from 'vue'

const copyToClipboard = vi.fn().mockResolvedValue(true)
const initialInnerWidth = window.innerWidth
const initialInnerHeight = window.innerHeight

const messages: Record<string, string> = {
  'keys.endpoints.title': 'API 端点',
  'keys.endpoints.default': '默认',
  'keys.endpoints.copied': '已复制',
  'keys.endpoints.copiedHint': '已复制到剪贴板',
  'keys.endpoints.clickToCopy': '点击可复制此端点',
  'keys.endpoints.speedTest': '测速',
}

vi.mock('vue-i18n', () => ({
  useI18n: () => ({
    t: (key: string) => messages[key] ?? key,
  }),
}))

vi.mock('@/composables/useClipboard', () => ({
  useClipboard: () => ({
    copyToClipboard,
  }),
}))

import EndpointPopover from '../EndpointPopover.vue'

describe('EndpointPopover', () => {
  beforeEach(() => {
    copyToClipboard.mockReset()
    copyToClipboard.mockResolvedValue(true)
  })

  afterEach(() => {
    vi.useRealTimers()
    Object.defineProperty(window, 'innerWidth', {
      configurable: true,
      value: initialInnerWidth,
    })
    Object.defineProperty(window, 'innerHeight', {
      configurable: true,
      value: initialInnerHeight,
    })
  })

  it('将说明提示渲染到 URL 上方而不是旧的 title 图标上', () => {
    const wrapper = mount(EndpointPopover, {
      props: {
        apiBaseUrl: 'https://default.example.com/v1',
        customEndpoints: [
          {
            name: '备用线路',
            endpoint: 'https://backup.example.com/v1',
            description: '自定义说明',
          },
        ],
      },
    })

    expect(wrapper.text()).toContain('自定义说明')
    expect(wrapper.text()).toContain('点击可复制此端点')
    expect(wrapper.get('[data-testid="endpoint-url"]').attributes('title')).toBeUndefined()
    expect(wrapper.find('[title="自定义说明"]').exists()).toBe(false)
    expect(wrapper.findAll('[role="tooltip"]')).toHaveLength(2)

    wrapper.unmount()
  })

  it('只保留原生按钮作为键盘复制入口', async () => {
    const wrapper = mount(EndpointPopover, {
      props: {
        apiBaseUrl: 'https://default.example.com/v1',
        customEndpoints: [],
      },
    })

    const endpointText = wrapper.get('[data-testid="endpoint-url"]')
    expect(endpointText.attributes('role')).toBeUndefined()
    expect(endpointText.attributes('tabindex')).toBeUndefined()

    await endpointText.trigger('keydown', { key: 'Enter' })
    expect(copyToClipboard).not.toHaveBeenCalled()

    const copyButton = wrapper.get('[data-testid="endpoint-copy-button"]')
    expect(copyButton.element.tagName).toBe('BUTTON')
    const tooltipContent = wrapper.get('[data-testid="endpoint-tooltip-content"]')
    expect(copyButton.attributes('aria-describedby')).toBe(tooltipContent.attributes('id'))

    const copyRegion = wrapper.get('[data-testid="endpoint-copy-region"]')
    const speedTestLink = wrapper.get('a[aria-label="测速"]')
    expect(copyRegion.element.contains(speedTestLink.element)).toBe(false)
    expect(speedTestLink.attributes('title')).toBe('测速')
    const copyRegionRect = vi.spyOn(copyRegion.element, 'getBoundingClientRect')
    await speedTestLink.trigger('focusin')
    expect(copyRegionRect).not.toHaveBeenCalled()

    await copyButton.trigger('click')
    await flushPromises()

    expect(copyToClipboard).toHaveBeenCalledWith('https://default.example.com/v1', '已复制')
    expect(wrapper.text()).toContain('已复制到剪贴板')
    expect(wrapper.find('button[aria-label="已复制到剪贴板"]').exists()).toBe(true)
    wrapper.unmount()
  })

  it('keeps short tooltip content out of the tab order', async () => {
    const wrapper = mount(EndpointPopover, {
      props: {
        apiBaseUrl: 'https://default.example.com/v1',
        customEndpoints: [],
      },
    })
    const copyRegion = wrapper.get('[data-testid="endpoint-copy-region"]')
    const tooltipContent = wrapper.get('[data-testid="endpoint-tooltip-content"]')
    vi.spyOn(copyRegion.element, 'getBoundingClientRect').mockReturnValue({
      x: 16,
      y: 300,
      left: 16,
      top: 300,
      right: 359,
      bottom: 344,
      width: 343,
      height: 44,
      toJSON: () => ({}),
    })
    Object.defineProperty(tooltipContent.element, 'scrollHeight', {
      configurable: true,
      value: 80,
    })

    await copyRegion.trigger('focusin')
    await nextTick()

    expect(tooltipContent.attributes('role')).toBe('tooltip')
    expect(tooltipContent.attributes('tabindex')).toBeUndefined()
    expect(tooltipContent.attributes('aria-label')).toBeUndefined()

    wrapper.unmount()
  })

  it('exposes only overflowing content as a named scroll region after the copy button', async () => {
    Object.defineProperty(window, 'innerWidth', { configurable: true, value: 375 })
    Object.defineProperty(window, 'innerHeight', { configurable: true, value: 240 })

    const wrapper = mount(EndpointPopover, {
      attachTo: document.body,
      props: {
        apiBaseUrl: '',
        customEndpoints: [
          {
            name: '备用线路',
            endpoint: 'https://backup.example.com/v1',
            description: '较长的端点说明'.repeat(30),
          },
        ],
      },
    })
    const copyRegion = wrapper.get('[data-testid="endpoint-copy-region"]')
    const copyButton = wrapper.get('[data-testid="endpoint-copy-button"]')
    const tooltip = wrapper.get('[data-testid="endpoint-tooltip"]')
    const tooltipContent = wrapper.get('[data-testid="endpoint-tooltip-content"]')
    const tooltipDescription = wrapper.get('[data-testid="endpoint-tooltip-description"]')
    const speedTestLink = wrapper.get('a[aria-label="测速"]')
    vi.spyOn(copyRegion.element, 'getBoundingClientRect').mockReturnValue({
      x: 16,
      y: 24,
      left: 16,
      top: 24,
      right: 359,
      bottom: 68,
      width: 343,
      height: 44,
      toJSON: () => ({}),
    })
    Object.defineProperty(tooltipContent.element, 'scrollHeight', {
      configurable: true,
      value: 300,
    })

    copyButton.element.focus()
    await nextTick()

    expect(tooltipContent.attributes('role')).toBe('region')
    expect(tooltipContent.attributes('aria-label')).toBe('备用线路')
    expect(tooltipContent.attributes('tabindex')).toBe('0')
    expect(tooltipContent.classes()).toContain('overflow-y-auto')
    expect(copyButton.attributes('aria-describedby')).toBe(tooltipDescription.attributes('id'))
    expect(copyButton.attributes('aria-describedby')).not.toBe(tooltipContent.attributes('id'))
    expect(copyButton.element.compareDocumentPosition(tooltipContent.element)
      & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy()
    expect(tooltipContent.element.compareDocumentPosition(speedTestLink.element)
      & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy()

    tooltipContent.element.focus()
    await nextTick()

    expect(document.activeElement).toBe(tooltipContent.element)
    expect(copyRegion.element.contains(document.activeElement)).toBe(true)
    expect(tooltip.classes()).toContain('group-focus-within/copy:opacity-100')

    const mountedElement = wrapper.element
    wrapper.unmount()
    mountedElement.remove()
  })

  it('keeps long names and complete endpoint URLs inside a fluid wrapping chip', () => {
    const endpoint = `https://api.example.com/${'very-long-segment/'.repeat(12)}v1`
    const wrapper = mount(EndpointPopover, {
      props: {
        apiBaseUrl: '',
        customEndpoints: [
          {
            name: '这是一个非常长且不应撑破容器的自定义端点名称'.repeat(4),
            endpoint,
            description: '说明'.repeat(80),
          },
        ],
      },
    })

    const chip = wrapper.get('.endpoint-chip')
    const name = wrapper.get('[data-testid="endpoint-name"]')
    const url = wrapper.get('[data-testid="endpoint-url"]')

    expect(chip.classes()).toEqual(expect.arrayContaining(['w-full', 'max-w-full', 'flex-wrap']))
    expect(name.classes()).toEqual(expect.arrayContaining(['min-w-0', 'break-words']))
    expect(url.text()).toBe(endpoint)
    expect(url.classes()).toEqual(expect.arrayContaining(['min-w-0', 'flex-1', 'break-all']))
    expect(url.classes()).not.toContain('truncate')

    wrapper.unmount()
  })

  it('clamps tooltip width and horizontal position to the viewport edge', async () => {
    Object.defineProperty(window, 'innerWidth', { configurable: true, value: 320 })

    const wrapper = mount(EndpointPopover, {
      props: {
        apiBaseUrl: 'https://default.example.com/v1',
        customEndpoints: [],
      },
    })
    const region = wrapper.get('[data-testid="endpoint-copy-region"]')
    const tooltip = wrapper.get('[data-testid="endpoint-tooltip"]')
    expect(tooltip.attributes('style')).toContain('width: 100%')
    vi.spyOn(region.element, 'getBoundingClientRect').mockReturnValue({
      x: 280,
      y: 100,
      left: 280,
      top: 100,
      right: 320,
      bottom: 144,
      width: 40,
      height: 44,
      toJSON: () => ({}),
    })

    await region.trigger('mouseenter')
    await nextTick()

    expect(tooltip.attributes('style')).toContain('left: -264px')
    expect(tooltip.attributes('style')).toContain('width: 288px')

    wrapper.unmount()
  })

  it('places a tall tooltip below a top-edge anchor and limits it to the viewport', async () => {
    Object.defineProperty(window, 'innerWidth', { configurable: true, value: 375 })
    Object.defineProperty(window, 'innerHeight', { configurable: true, value: 240 })

    const wrapper = mount(EndpointPopover, {
      attachTo: document.body,
      props: {
        apiBaseUrl: '',
        customEndpoints: [
          {
            name: '备用线路',
            endpoint: 'https://backup.example.com/v1',
            description: '较长的端点说明'.repeat(30),
          },
        ],
      },
    })
    const region = wrapper.get('[data-testid="endpoint-copy-region"]')
    const anchorRect = vi.spyOn(region.element, 'getBoundingClientRect').mockReturnValue({
      x: 16,
      y: 24,
      left: 16,
      top: 24,
      right: 359,
      bottom: 68,
      width: 343,
      height: 44,
      toJSON: () => ({}),
    })
    const tooltipContent = wrapper.get('[data-testid="endpoint-tooltip-content"]')
    Object.defineProperty(tooltipContent.element, 'scrollHeight', {
      configurable: true,
      value: 300,
    })

    await region.trigger('mouseenter')
    await nextTick()

    const tooltip = wrapper.get('[data-testid="endpoint-tooltip"]')
    expect(tooltip.classes()).toEqual(expect.arrayContaining(['top-full', 'mt-2']))
    expect(tooltip.classes()).not.toContain('bottom-full')
    expect(tooltipContent.attributes('style')).toContain('max-height: 148px')
    expect(wrapper.get('[data-testid="endpoint-tooltip"] > .pointer-events-none').classes())
      .toContain('bottom-full')

    anchorRect.mockReturnValue({
      x: 16,
      y: 180,
      left: 16,
      top: 180,
      right: 359,
      bottom: 224,
      width: 343,
      height: 44,
      toJSON: () => ({}),
    })
    window.dispatchEvent(new Event('scroll'))
    await nextTick()

    expect(tooltip.classes()).toEqual(expect.arrayContaining(['bottom-full', 'mb-2']))
    expect(tooltipContent.attributes('style')).toContain('max-height: 156px')

    const mountedElement = wrapper.element
    wrapper.unmount()
    mountedElement.remove()
  })

  it('does not let an older timer clear feedback for a newer endpoint copy', async () => {
    vi.useFakeTimers()
    const wrapper = mount(EndpointPopover, {
      props: {
        apiBaseUrl: 'https://default.example.com/v1',
        customEndpoints: [
          {
            name: '备用线路',
            endpoint: 'https://backup.example.com/v1',
            description: '',
          },
        ],
      },
    })
    const copyButtons = wrapper.findAll('[data-testid="endpoint-copy-button"]')

    await copyButtons[0].trigger('click')
    await flushPromises()
    vi.advanceTimersByTime(900)

    await copyButtons[1].trigger('click')
    await flushPromises()
    expect(copyButtons[1].attributes('aria-label')).toBe('已复制到剪贴板')

    vi.advanceTimersByTime(900)
    await nextTick()
    expect(copyButtons[1].attributes('aria-label')).toBe('已复制到剪贴板')

    vi.advanceTimersByTime(900)
    await nextTick()
    expect(copyButtons[1].attributes('aria-label')).toBe('点击可复制此端点')

    wrapper.unmount()
  })

  it('clears pending copy feedback timers when unmounted', async () => {
    vi.useFakeTimers()
    const wrapper = mount(EndpointPopover, {
      props: {
        apiBaseUrl: 'https://default.example.com/v1',
        customEndpoints: [],
      },
    })

    await wrapper.get('[data-testid="endpoint-copy-button"]').trigger('click')
    await flushPromises()
    expect(vi.getTimerCount()).toBe(1)

    wrapper.unmount()
    expect(vi.getTimerCount()).toBe(0)
  })
})
