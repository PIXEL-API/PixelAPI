import { afterEach, describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import { nextTick } from 'vue'
import HelpTooltip from '@/components/common/HelpTooltip.vue'

const DEFAULT_VIEWPORT_WIDTH = window.innerWidth
const DEFAULT_VIEWPORT_HEIGHT = window.innerHeight
const DEFAULT_SCROLL_X = window.scrollX
const DEFAULT_SCROLL_Y = window.scrollY

function setViewport(width: number, height: number): void {
  Object.defineProperties(window, {
    innerWidth: { configurable: true, value: width },
    innerHeight: { configurable: true, value: height },
  })
}

function setScrollPosition(x: number, y: number): void {
  Object.defineProperties(window, {
    scrollX: { configurable: true, value: x },
    scrollY: { configurable: true, value: y },
  })
}

function createRect(
  left: number,
  top: number,
  width: number,
  height: number,
): DOMRect {
  return {
    bottom: top + height,
    height,
    left,
    right: left + width,
    top,
    width,
    x: left,
    y: top,
    toJSON: () => ({}),
  }
}

function getTooltipElement(): HTMLDivElement {
  const tooltip = document.body.querySelector('[role="tooltip"]')
  if (!(tooltip instanceof HTMLDivElement)) {
    throw new Error('tooltip element not found')
  }
  return tooltip
}

describe('HelpTooltip', () => {
  afterEach(() => {
    setViewport(DEFAULT_VIEWPORT_WIDTH, DEFAULT_VIEWPORT_HEIGHT)
    setScrollPosition(DEFAULT_SCROLL_X, DEFAULT_SCROLL_Y)
    vi.restoreAllMocks()
    document.body.innerHTML = ''
  })

  it('keeps the existing hover interaction by default', async () => {
    const wrapper = mount(HelpTooltip, {
      attachTo: document.body,
      props: {
        content: 'hover details',
      },
    })

    const trigger = wrapper.get('.group')
    const tooltip = getTooltipElement()

    expect(tooltip.style.display).toBe('none')

    await trigger.trigger('mouseenter')
    await nextTick()
    expect(tooltip.style.display).not.toBe('none')

    await trigger.trigger('mouseleave')
    await nextTick()
    expect(tooltip.style.display).toBe('none')

    wrapper.unmount()
  })

  it('supports click-to-toggle details and closes on outside click', async () => {
    const wrapper = mount(HelpTooltip, {
      attachTo: document.body,
      props: {
        content: 'click details',
        trigger: 'click',
      },
    })

    const trigger = wrapper.get('.group')
    const tooltip = getTooltipElement()

    expect(tooltip.style.display).toBe('none')

    await trigger.trigger('click')
    await nextTick()
    expect(tooltip.style.display).not.toBe('none')
    expect(tooltip.textContent).toContain('click details')

    const closeButton = tooltip.querySelector('button[aria-label="Close"]')
    if (!(closeButton instanceof HTMLButtonElement)) {
      throw new Error('close button not found')
    }
    closeButton.click()
    await nextTick()
    expect(tooltip.style.display).toBe('none')

    await trigger.trigger('click')
    await nextTick()
    expect(tooltip.style.display).not.toBe('none')

    document.body.dispatchEvent(new MouseEvent('click', { bubbles: true }))
    await nextTick()
    expect(tooltip.style.display).toBe('none')

    wrapper.unmount()
  })

  it('keeps the tooltip inside the horizontal viewport near the right edge', async () => {
    setViewport(1000, 700)
    setScrollPosition(240, 900)
    const wrapper = mount(HelpTooltip, {
      attachTo: document.body,
      props: {
        content: 'viewport-safe details',
        widthClass: 'w-80',
      },
    })

    const trigger = wrapper.get('.group')
    const tooltip = getTooltipElement()
    vi.spyOn(trigger.element, 'getBoundingClientRect').mockReturnValue(createRect(970, 400, 16, 16))
    vi.spyOn(tooltip, 'getBoundingClientRect').mockReturnValue(createRect(0, 0, 320, 200))

    await trigger.trigger('mouseenter')
    await nextTick()

    expect(tooltip.style.left).toBe('664px')
    expect(tooltip.style.top).toBe('192px')
    expect(tooltip.dataset.placement).toBe('top')

    const arrow = tooltip.querySelector('.rotate-45')
    expect(arrow).toBeInstanceOf(HTMLDivElement)
    expect((arrow as HTMLDivElement).style.left).toBe('308px')

    wrapper.unmount()
  })

  it('places the tooltip below the trigger when there is not enough room above', async () => {
    setViewport(375, 600)
    const wrapper = mount(HelpTooltip, {
      attachTo: document.body,
      props: {
        content: 'details below the trigger',
        widthClass: 'w-80',
      },
    })

    const trigger = wrapper.get('.group')
    const tooltip = getTooltipElement()
    vi.spyOn(trigger.element, 'getBoundingClientRect').mockReturnValue(createRect(20, 40, 16, 16))
    vi.spyOn(tooltip, 'getBoundingClientRect').mockReturnValue(createRect(0, 0, 320, 180))

    await trigger.trigger('mouseenter')
    await nextTick()

    expect(tooltip.style.left).toBe('16px')
    expect(tooltip.style.top).toBe('64px')
    expect(tooltip.dataset.placement).toBe('bottom')

    const arrow = tooltip.querySelector('.rotate-45')
    expect(arrow?.classList.contains('-top-1')).toBe(true)

    wrapper.unmount()
  })

  it('recalculates the position when the viewport changes', async () => {
    setViewport(1000, 700)
    const wrapper = mount(HelpTooltip, {
      attachTo: document.body,
      props: {
        content: 'responsive details',
        widthClass: 'w-80',
      },
    })

    const trigger = wrapper.get('.group')
    const tooltip = getTooltipElement()
    vi.spyOn(trigger.element, 'getBoundingClientRect').mockReturnValue(createRect(850, 400, 16, 16))
    vi.spyOn(tooltip, 'getBoundingClientRect').mockReturnValue(createRect(0, 0, 320, 200))

    await trigger.trigger('mouseenter')
    await nextTick()
    expect(tooltip.style.left).toBe('664px')

    setViewport(800, 700)
    window.dispatchEvent(new Event('resize'))
    await nextTick()
    expect(tooltip.style.left).toBe('464px')

    wrapper.unmount()
  })
})
