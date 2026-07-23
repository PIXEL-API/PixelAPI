import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { nextTick } from 'vue'

import en from '@/i18n/locales/en'
import zh from '@/i18n/locales/zh'
import KeysView from '../KeysView.vue'

const {
  getDashboardApiKeysUsage,
  getPublicSettings,
  getAvailableGroups,
  getUserGroupRates,
  listKeys,
  createKey,
  showError,
} = vi.hoisted(() => ({
  getDashboardApiKeysUsage: vi.fn(),
  getPublicSettings: vi.fn(),
  getAvailableGroups: vi.fn(),
  getUserGroupRates: vi.fn(),
  listKeys: vi.fn(),
  createKey: vi.fn(),
  showError: vi.fn(),
}))

vi.mock('@/api', () => ({
  keysAPI: {
    list: listKeys,
    update: vi.fn(),
    create: createKey,
    delete: vi.fn(),
    toggleStatus: vi.fn(),
  },
  authAPI: { getPublicSettings },
  usageAPI: { getDashboardApiKeysUsage },
  userGroupsAPI: {
    getAvailable: getAvailableGroups,
    getUserGroupRates,
  },
  accountShareAPI: { listMembershipQueue: vi.fn() },
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({
    showError,
    showSuccess: vi.fn(),
  }),
}))

vi.mock('@/stores/onboarding', () => ({
  useOnboardingStore: () => ({
    isCurrentStep: vi.fn(() => false),
    nextStep: vi.fn(),
  }),
}))

vi.mock('@/composables/useClipboard', () => ({
  useClipboard: () => ({ copyToClipboard: vi.fn(async () => true) }),
}))

vi.mock('vue-router', () => ({
  useRouter: () => ({ push: vi.fn() }),
}))

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({ t: (key: string) => key }),
  }
})

const group = {
  id: 9,
  name: 'Primary group',
  description: 'Primary test group',
  platform: 'openai',
  scope: 'private',
  subscription_type: 'standard',
  rate_multiplier: 1,
  effective_rate_multiplier: 1,
  effective_rate_multiplier_source: 'group_default',
}

const apiKey = {
  id: 7,
  name: 'Test key',
  key: 'sk-test',
  group_id: group.id,
  group,
}

const AppLayoutStub = { template: '<div><slot /></div>' }
const TablePageLayoutStub = {
  template: `
    <div>
      <slot name="actions" />
      <slot name="filters" />
      <slot name="table" />
      <slot name="pagination" />
    </div>
  `,
}
const DataTableStub = {
  props: ['data'],
  template: `
    <div v-if="data.length">
      <slot name="cell-name" :value="data[0].name" :row="data[0]" />
      <slot name="cell-key" :value="data[0].key" :row="data[0]" />
      <slot name="cell-group" :row="data[0]" />
      <slot name="cell-actions" :row="data[0]" />
    </div>
  `,
}
const BaseDialogStub = {
  props: ['show'],
  template: '<div v-if="show"><slot /><slot name="footer" /></div>',
}

const wrappers: VueWrapper[] = []
let triggerBottom = 40

const mountKeysView = () => {
  const wrapper = mount(KeysView, {
    attachTo: document.body,
    global: {
      stubs: {
        AppLayout: AppLayoutStub,
        TablePageLayout: TablePageLayoutStub,
        DataTable: DataTableStub,
        BaseDialog: BaseDialogStub,
        SearchInput: true,
        Select: true,
        Pagination: true,
        EndpointPopover: true,
        EmptyState: true,
        ConfirmDialog: true,
        UseKeyModal: true,
        ApiKeyAccountShareConflictDialog: true,
        GroupBadge: true,
        GroupOptionItem: true,
        Icon: true,
      },
    },
  })
  wrappers.push(wrapper)
  return wrapper
}

describe('user KeysView accessibility interactions', () => {
  beforeEach(() => {
    triggerBottom = 40
    listKeys.mockReset().mockResolvedValue({
      items: [apiKey],
      total: 1,
      pages: 1,
    })
    getDashboardApiKeysUsage.mockReset().mockResolvedValue({ stats: {} })
    getPublicSettings.mockReset().mockResolvedValue({})
    getAvailableGroups.mockReset().mockResolvedValue([group])
    getUserGroupRates.mockReset().mockResolvedValue({})
    createKey.mockReset().mockResolvedValue(apiKey)
    showError.mockReset()
    window.localStorage.clear()

    Object.defineProperty(window, 'innerHeight', { configurable: true, value: 800 })
    Object.defineProperty(window, 'innerWidth', { configurable: true, value: 1200 })
    vi.spyOn(HTMLElement.prototype, 'getBoundingClientRect').mockImplementation(() => ({
      x: 24,
      y: triggerBottom - 20,
      top: triggerBottom - 20,
      left: 24,
      right: 224,
      bottom: triggerBottom,
      width: 200,
      height: 20,
      toJSON: () => ({}),
    } as DOMRect))
  })

  afterEach(() => {
    wrappers.splice(0).forEach((wrapper) => {
      const element = wrapper.element
      wrapper.unmount()
      element.remove()
    })
    vi.restoreAllMocks()
    document.querySelectorAll('[data-test-outside]').forEach((element) => element.remove())
  })

  it('uses a roving column menu, restores focus after Escape, and preserves outside focus', async () => {
    const wrapper = mountKeysView()
    await flushPromises()
    const trigger = wrapper.get<HTMLButtonElement>('#api-key-column-settings-trigger')

    expect(trigger.classes()).toContain('min-w-11')
    expect(trigger.attributes('aria-expanded')).toBe('false')
    await trigger.trigger('click')
    await nextTick()

    const menu = wrapper.get('#api-key-column-settings-menu')
    expect(menu.classes()).toEqual(
      expect.arrayContaining(['left-0', 'right-auto', 'sm:left-auto', 'sm:right-0'])
    )
    const items = menu.findAll<HTMLButtonElement>('[data-column-menu-item]')
    expect(trigger.attributes('aria-controls')).toBe('api-key-column-settings-menu')
    expect(document.activeElement).toBe(items[0].element)
    expect(items[0].attributes('role')).toBe('menuitemcheckbox')

    await items[0].trigger('keydown', { key: 'ArrowDown' })
    expect(document.activeElement).toBe(items[1].element)
    await items[1].trigger('keydown', { key: 'Escape' })
    await nextTick()
    expect(wrapper.find('#api-key-column-settings-menu').exists()).toBe(false)
    expect(document.activeElement).toBe(trigger.element)

    await trigger.trigger('click')
    await nextTick()
    const outside = document.createElement('button')
    outside.dataset.testOutside = 'true'
    document.body.appendChild(outside)
    outside.focus()
    outside.click()
    await nextTick()
    expect(wrapper.find('#api-key-column-settings-menu').exists()).toBe(false)
    expect(document.activeElement).toBe(outside)
  })

  it('focuses and clamps the teleported group selector and restores its trigger focus', async () => {
    const wrapper = mountKeysView()
    await flushPromises()
    const trigger = wrapper.get<HTMLButtonElement>('[aria-haspopup="dialog"]')

    await wrapper.get('#api-key-column-settings-trigger').trigger('click')
    await nextTick()
    expect(wrapper.find('#api-key-column-settings-menu').exists()).toBe(true)
    await trigger.trigger('click')
    await nextTick()
    const dialogId = 'api-key-group-selector-7'
    const dialog = document.getElementById(dialogId)
    const search = document.getElementById('api-key-group-selector-search-7') as HTMLInputElement

    expect(trigger.attributes('aria-expanded')).toBe('true')
    expect(trigger.attributes('aria-controls')).toBe(dialogId)
    expect(wrapper.find('#api-key-column-settings-menu').exists()).toBe(false)
    expect(dialog?.getAttribute('role')).toBe('dialog')
    expect(dialog?.style.top).toBe('44px')
    expect(search.getAttribute('aria-label')).toBe('keys.searchGroup')
    expect(search.classList.contains('min-h-11')).toBe(true)
    expect(document.activeElement).toBe(search)

    triggerBottom = 120
    window.dispatchEvent(new Event('scroll'))
    await nextTick()
    expect(dialog?.style.top).toBe('124px')

    Object.defineProperty(window, 'innerHeight', { configurable: true, value: 300 })
    triggerBottom = 160
    window.dispatchEvent(new Event('resize'))
    await nextTick()
    expect(dialog?.style.top).toBe('16px')
    expect(dialog?.classList.contains('max-h-[calc(100dvh-2rem)]')).toBe(true)
    expect(dialog?.querySelector('[role="group"]')?.classList.contains('flex-1')).toBe(true)

    search.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }))
    await nextTick()
    expect(document.getElementById(dialogId)).toBeNull()
    expect(trigger.attributes('aria-expanded')).toBe('false')
    expect(document.activeElement).toBe(trigger.element)

    await trigger.trigger('click')
    await nextTick()
    const option = document.querySelector<HTMLButtonElement>(`#${dialogId} .data-teleport-option`)
    expect(option).not.toBeNull()
    option!.focus()
    option!.click()
    await nextTick()
    expect(document.getElementById(dialogId)).toBeNull()
    expect(document.activeElement).toBe(trigger.element)

    await trigger.trigger('click')
    await nextTick()
    const outside = document.createElement('button')
    outside.dataset.testOutside = 'true'
    document.body.appendChild(outside)
    outside.focus()
    await nextTick()
    expect(document.getElementById(dialogId)).toBeNull()
    expect(document.activeElement).toBe(outside)
  })

  it('gives filters and form selects distinct programmatic labels', async () => {
    const wrapper = mountKeysView()
    await flushPromises()

    expect(wrapper.get('select-stub[aria-label="keys.groupFilterLabel"]').exists()).toBe(true)
    expect(wrapper.get('select-stub[aria-label="keys.statusFilterLabel"]').exists()).toBe(true)

    await wrapper.get('[data-tour="keys-create-btn"]').trigger('click')
    await nextTick()
    expect(wrapper.get('#key-group-label').exists()).toBe(true)
    expect(
      wrapper.get('select-stub[data-tour="key-form-group"]').attributes('arialabelledby')
    ).toBe('key-group-label')

    await wrapper.get('#key-group-routes-switch').trigger('click')
    await nextTick()
    expect(wrapper.get('#key-group-route-group-label-0').exists()).toBe(true)
    expect(
      wrapper.get('select-stub[arialabelledby="key-group-route-group-label-0"]').exists()
    ).toBe(true)

    const cancel = wrapper.findAll('button').find((button) => button.text() === 'common.cancel')
    expect(cancel).toBeDefined()
    await cancel!.trigger('click')
    const edit = wrapper.findAll('button').find((button) => button.text() === 'common.edit')
    expect(edit).toBeDefined()
    await edit!.trigger('click')
    await nextTick()
    expect(wrapper.get('#key-status-label').exists()).toBe(true)
    expect(wrapper.get('select-stub[arialabelledby="key-status-label"]').exists()).toBe(true)
  })

  it('constrains long API key names without forcing the table wider', async () => {
    const wrapper = mountKeysView()
    await flushPromises()

    const name = wrapper.get('span.font-medium.text-content')
    expect(name.classes()).toEqual(
      expect.arrayContaining(['inline-block', 'max-w-64', 'whitespace-normal', 'break-words'])
    )
  })

  it('associates critical native fields with labels, hints, and validation errors', async () => {
    const wrapper = mountKeysView()
    await flushPromises()
    await wrapper.get('[data-tour="keys-create-btn"]').trigger('click')
    await nextTick()

    expect(wrapper.get('label[for="key-name"]').exists()).toBe(true)
    expect(wrapper.get('#key-quota-limit').attributes('aria-describedby')).toBe('key-quota-limit-hint')

    await wrapper.get('#key-custom-key-switch').trigger('click')
    const customKey = wrapper.get<HTMLInputElement>('#key-custom-key-value')
    expect(customKey.attributes('maxlength')).toBe('128')
    await customKey.setValue('short')
    expect(customKey.attributes('aria-invalid')).toBe('true')
    expect(customKey.attributes('aria-describedby')).toBe('key-custom-key-error')
    expect(wrapper.get('#key-custom-key-error').attributes('role')).toBe('alert')

    await customKey.setValue('a'.repeat(128))
    expect(wrapper.find('#key-custom-key-error').exists()).toBe(false)
    await customKey.setValue('a'.repeat(129))
    expect(wrapper.get('#key-custom-key-error').text()).toBe('keys.customKeyTooLong')
    expect(customKey.attributes('aria-invalid')).toBe('true')

    await customKey.setValue('')
    expect(wrapper.find('#key-custom-key-error').exists()).toBe(false)
    await customKey.trigger('blur')
    expect(wrapper.get('#key-custom-key-error').text()).toBe('keys.customKeyRequired')
    expect(customKey.attributes('aria-invalid')).toBe('true')
    expect(customKey.attributes('aria-describedby')).toBe('key-custom-key-error')

    await wrapper.get('#key-name').trigger('focus')
    await wrapper.get('#key-form').trigger('submit')
    await nextTick()
    expect(showError).toHaveBeenCalledWith('keys.customKeyRequired')
    expect(document.activeElement).toBe(customKey.element)

    await wrapper.get('#key-expiration-switch').trigger('click')
    const expirationDate = wrapper.get<HTMLInputElement>('#key-expiration-date')
    expect(expirationDate.attributes('aria-describedby')).toBe('key-expiration-date-hint')
    expect(expirationDate.element.value).toMatch(/^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}$/)
    const expirationPresets = wrapper.findAll('button[aria-pressed]')
    expect(expirationPresets).toHaveLength(4)
    expect(expirationPresets[1].attributes('aria-pressed')).toBe('true')
    expirationPresets.forEach((button) => {
      expect(button.classes()).toContain('min-h-11')
    })
  })

  it('marks manually edited expiration as custom and creates with the exact ISO timestamp', async () => {
    const wrapper = mountKeysView()
    await flushPromises()
    await wrapper.get('[data-tour="keys-create-btn"]').trigger('click')
    await nextTick()

    await wrapper.get<HTMLInputElement>('#key-name').setValue('Precise expiration key')
    wrapper.getComponent('[data-tour="key-form-group"]').vm.$emit('update:modelValue', group.id)
    await wrapper.get('#key-expiration-switch').trigger('click')

    const expirationDate = wrapper.get<HTMLInputElement>('#key-expiration-date')
    const localExpiration = '2099-03-04T05:06'
    const presetButtons = wrapper.findAll<HTMLButtonElement>('button[aria-pressed]')
    const activePreset = presetButtons.find((button) => button.attributes('aria-pressed') === 'true')
    const customPreset = presetButtons.find((button) => button.text() === 'keys.customDate')
    expect(activePreset?.text()).toBe('keys.expiresInDays')

    await expirationDate.setValue(localExpiration)

    expect(activePreset?.attributes('aria-pressed')).toBe('false')
    expect(customPreset?.attributes('aria-pressed')).toBe('true')

    await wrapper.get('#key-form').trigger('submit')
    await flushPromises()

    expect(createKey).toHaveBeenCalledTimes(1)
    const createArguments = createKey.mock.calls[0]
    expect(createArguments[6]).toBeUndefined()
    expect(createArguments[9]).toBe(new Date(localExpiration).toISOString())
  })

  it('removes viewport listeners and clears active timers on unmount', async () => {
    const removeEventListener = vi.spyOn(window, 'removeEventListener')
    const clearIntervalSpy = vi.spyOn(globalThis, 'clearInterval')
    const clearTimeoutSpy = vi.spyOn(globalThis, 'clearTimeout')
    const wrapper = mountKeysView()
    await flushPromises()

    await wrapper.get('button[aria-label="keys.copyToClipboard"]').trigger('click')
    await flushPromises()
    const element = wrapper.element
    wrappers.splice(wrappers.indexOf(wrapper), 1)
    wrapper.unmount()
    element.remove()

    expect(removeEventListener).toHaveBeenCalledWith('resize', expect.any(Function))
    expect(removeEventListener).toHaveBeenCalledWith('scroll', expect.any(Function), true)
    expect(clearIntervalSpy).toHaveBeenCalled()
    expect(clearTimeoutSpy).toHaveBeenCalled()
  })
})

describe('KeysView group-route translations', () => {
  it('keeps complete Chinese and English labels and validation messages', () => {
    const expectedKeys = [
      'title',
      'description',
      'configuration',
      'enabled',
      'group',
      'priority',
      'weight',
      'cooldownSeconds',
      'addRoute',
      'removeRoute',
    ] as const

    for (const locale of [zh, en]) {
      expect(locale.keys.lastUsedIP).toBeTruthy()
      expect(locale.keys.customKeyTooLong).toBeTruthy()
      expect(locale.keys.groupFilterLabel).toBeTruthy()
      expect(locale.keys.statusFilterLabel).toBeTruthy()
      for (const key of expectedKeys) {
        expect(locale.keys.groupRoutes[key]).toBeTruthy()
      }
      expect(Object.values(locale.keys.groupRoutes.validation)).toHaveLength(5)
      expect(Object.values(locale.keys.groupRoutes.validation).every(Boolean)).toBe(true)
    }
  })
})
