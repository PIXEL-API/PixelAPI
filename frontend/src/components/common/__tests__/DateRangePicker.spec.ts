import { afterEach, describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import { nextTick, ref } from 'vue'

import DateRangePicker from '../DateRangePicker.vue'

const messages: Record<string, string> = {
  'dates.today': 'Today',
  'dates.yesterday': 'Yesterday',
  'dates.last24Hours': 'Last 24 Hours',
  'dates.last7Days': 'Last 7 Days',
  'dates.last14Days': 'Last 14 Days',
  'dates.last30Days': 'Last 30 Days',
  'dates.thisMonth': 'This Month',
  'dates.lastMonth': 'Last Month',
  'dates.startDate': 'Start Date',
  'dates.endDate': 'End Date',
  'dates.apply': 'Apply',
  'dates.selectDateRange': 'Select date range'
}

vi.mock('vue-i18n', () => ({
  useI18n: () => ({
    t: (key: string) => messages[key] ?? key,
    locale: ref('en')
  })
}))

const formatLocalDate = (date: Date): string => {
  const year = date.getFullYear()
  const month = String(date.getMonth() + 1).padStart(2, '0')
  const day = String(date.getDate()).padStart(2, '0')
  return `${year}-${month}-${day}`
}

const createRect = (
  left: number,
  top: number,
  width: number,
  height: number
): DOMRect => ({
  x: left,
  y: top,
  left,
  top,
  width,
  height,
  right: left + width,
  bottom: top + height,
  toJSON: () => ({})
}) as DOMRect

describe('DateRangePicker', () => {
  afterEach(() => {
    vi.useRealTimers()
    vi.unstubAllGlobals()
    vi.restoreAllMocks()
    document.body.innerHTML = ''
  })

  it('does not infer a rolling 24-hour preset from date-only props', async () => {
    // 钉在月中：本用例刻意构造「昨天→今天」这种与 24 小时窗口同形的日期对。
    // 若使用真实时钟，每月 2 号「昨天」恰为月初，日期对同时命中「本月至今」，
    // 组件会推断出 thisMonth 预设，断言随日历假红。只伪造 Date，不动定时器。
    vi.useFakeTimers({ toFake: ['Date'] })
    vi.setSystemTime(new Date('2026-06-17T12:00:00'))
    const now = new Date()
    const yesterday = new Date(now.getTime() - 24 * 60 * 60 * 1000)

    const wrapper = mount(DateRangePicker, {
      props: {
        startDate: formatLocalDate(yesterday),
        endDate: formatLocalDate(now)
      },
      global: {
        stubs: {
          Icon: true
        }
      }
    })

    expect(wrapper.get('.date-picker-trigger').text()).not.toContain('Last 24 Hours')

    await wrapper.get('.date-picker-trigger').trigger('click')
    await wrapper.get('.date-picker-apply').trigger('click')
    expect(wrapper.emitted('change')?.[0]?.[0]).toEqual({
      startDate: formatLocalDate(yesterday),
      endDate: formatLocalDate(now),
      preset: null
    })
    wrapper.unmount()
  })

  it('emits range updates with last24Hours preset when applied', async () => {
    const now = new Date()
    const today = formatLocalDate(now)

    const wrapper = mount(DateRangePicker, {
      props: {
        startDate: today,
        endDate: today
      },
      global: {
        stubs: {
          Icon: true
        }
      }
    })

    await wrapper.find('.date-picker-trigger').trigger('click')
    const presetButton = wrapper.findAll('.date-picker-preset').find((node) =>
      node.text().includes('Last 24 Hours')
    )
    expect(presetButton).toBeDefined()

    await presetButton!.trigger('click')
    await wrapper.find('.date-picker-apply').trigger('click')

    const nowAfterClick = new Date()
    const yesterdayAfterClick = new Date(nowAfterClick.getTime() - 24 * 60 * 60 * 1000)
    const expectedStart = formatLocalDate(yesterdayAfterClick)
    const expectedEnd = formatLocalDate(nowAfterClick)

    expect(wrapper.emitted('update:startDate')?.[0]).toEqual([expectedStart])
    expect(wrapper.emitted('update:endDate')?.[0]).toEqual([expectedEnd])
    expect(wrapper.emitted('change')?.[0]).toEqual([
      {
        startDate: expectedStart,
        endDate: expectedEnd,
        preset: 'last24Hours'
      }
    ])

    await wrapper.setProps({ startDate: expectedStart, endDate: expectedEnd })
    await nextTick()
    expect(wrapper.get('.date-picker-trigger').text()).toContain('Last 24 Hours')

    await wrapper.get('.date-picker-trigger').trigger('click')
    await wrapper.get('.date-picker-apply').trigger('click')
    expect(wrapper.emitted('change')?.[1]?.[0]).toEqual({
      startDate: expectedStart,
      endDate: expectedEnd,
      preset: 'last24Hours'
    })
    wrapper.unmount()
  })

  it('connects popup and input labels, then returns focus to the trigger on Escape', async () => {
    const today = formatLocalDate(new Date())
    const wrapper = mount(DateRangePicker, {
      attachTo: document.body,
      props: {
        startDate: today,
        endDate: today
      },
      global: {
        stubs: {
          Icon: true
        }
      }
    })
    const trigger = wrapper.get<HTMLButtonElement>('.date-picker-trigger')

    await trigger.trigger('click')

    const popup = wrapper.get<HTMLElement>('.date-picker-dropdown')
    expect(trigger.attributes('aria-expanded')).toBe('true')
    expect(trigger.attributes('aria-haspopup')).toBe('dialog')
    expect(trigger.attributes('aria-controls')).toBe(popup.attributes('id'))
    expect(popup.attributes('aria-labelledby')).toBe(trigger.attributes('id'))

    const labels = wrapper.findAll<HTMLLabelElement>('.date-picker-label')
    const inputs = wrapper.findAll<HTMLInputElement>('.date-picker-input')
    expect(labels[0].attributes('for')).toBe(inputs[0].attributes('id'))
    expect(labels[1].attributes('for')).toBe(inputs[1].attributes('id'))

    const preset = wrapper.get<HTMLButtonElement>('.date-picker-preset')
    expect(preset.attributes('aria-pressed')).toBe('true')
    preset.element.focus()
    await preset.trigger('keydown', { key: 'Escape' })

    expect(trigger.attributes('aria-expanded')).toBe('false')
    expect(wrapper.find('.date-picker-dropdown').exists()).toBe(false)
    expect(document.activeElement).toBe(trigger.element)

    wrapper.unmount()
  })

  it('discards an unapplied preset when focus leaves the picker', async () => {
    const today = formatLocalDate(new Date())
    const wrapper = mount(DateRangePicker, {
      attachTo: document.body,
      props: { startDate: today, endDate: today },
      global: { stubs: { Icon: true } }
    })
    const outsideButton = document.createElement('button')
    document.body.appendChild(outsideButton)

    await wrapper.get('.date-picker-trigger').trigger('click')
    const lastSevenDays = wrapper.findAll('.date-picker-preset').find((button) =>
      button.text().includes('Last 7 Days')
    )
    expect(lastSevenDays).toBeDefined()
    await lastSevenDays!.trigger('click')
    expect(wrapper.get('.date-picker-trigger').text()).toContain('Last 7 Days')

    lastSevenDays!.element.focus()
    outsideButton.focus()
    await nextTick()

    expect(wrapper.find('.date-picker-dropdown').exists()).toBe(false)
    expect(wrapper.get('.date-picker-trigger').text()).toContain('Today')
    expect(wrapper.emitted('change')).toBeUndefined()
    wrapper.unmount()
  })

  it('closes on an outside click without stealing focus from the clicked control', async () => {
    const today = formatLocalDate(new Date())
    const wrapper = mount(DateRangePicker, {
      attachTo: document.body,
      props: { startDate: today, endDate: today },
      global: { stubs: { Icon: true } }
    })
    const outsideButton = document.createElement('button')
    document.body.appendChild(outsideButton)

    await wrapper.get('.date-picker-trigger').trigger('click')
    outsideButton.focus()
    outsideButton.click()
    await nextTick()

    expect(wrapper.find('.date-picker-dropdown').exists()).toBe(false)
    expect(document.activeElement).toBe(outsideButton)
    wrapper.unmount()
  })

  it.each([320, 375])(
    'clamps and flips the dropdown within a %dpx viewport on scroll and resize',
    async (viewportWidth) => {
      vi.stubGlobal('innerWidth', viewportWidth)
      vi.stubGlobal('innerHeight', 400)
      const addEventListenerSpy = vi.spyOn(window, 'addEventListener')
      const removeEventListenerSpy = vi.spyOn(window, 'removeEventListener')
      let triggerRect = createRect(viewportWidth - 60, 300, 50, 40)
      const today = formatLocalDate(new Date())
      const wrapper = mount(DateRangePicker, {
        attachTo: document.body,
        props: { startDate: today, endDate: today },
        global: { stubs: { Icon: true } }
      })
      vi.spyOn(wrapper.element, 'getBoundingClientRect').mockImplementation(() => triggerRect)

      await wrapper.get('.date-picker-trigger').trigger('click')
      await nextTick()
      const dropdown = wrapper.get<HTMLElement>('.date-picker-dropdown').element
      Object.defineProperties(dropdown, {
        offsetWidth: { configurable: true, value: viewportWidth - 32 },
        offsetHeight: { configurable: true, value: 300 },
        scrollHeight: { configurable: true, value: 300 }
      })

      window.dispatchEvent(new Event('resize'))
      await nextTick()
      await nextTick()

      expect(dropdown.style.left).toBe(`${16 - triggerRect.left}px`)
      expect(dropdown.style.maxWidth).toBe('calc(100vw - 2rem)')
      expect(dropdown.style.maxHeight).toBe('280px')
      expect(dropdown.style.bottom).toBe('44px')
      expect(dropdown.style.top).toBe('')

      vi.stubGlobal('innerHeight', 600)
      triggerRect = createRect(20, 20, 50, 40)
      window.dispatchEvent(new Event('scroll'))
      await nextTick()
      await nextTick()

      expect(dropdown.style.left).toBe('-4px')
      expect(dropdown.style.maxHeight).toBe('520px')
      expect(dropdown.style.top).toBe('44px')
      expect(dropdown.style.bottom).toBe('')

      const scrollRegistration = addEventListenerSpy.mock.calls.find(
        ([eventName, , options]) => eventName === 'scroll' &&
          typeof options === 'object' && options?.capture === true
      )
      expect(scrollRegistration).toBeDefined()

      wrapper.unmount()
      expect(removeEventListenerSpy).toHaveBeenCalledWith(
        'scroll',
        scrollRegistration?.[1],
        { capture: true }
      )
      expect(removeEventListenerSpy).toHaveBeenCalledWith(
        'resize',
        expect.any(Function)
      )
    }
  )
})
