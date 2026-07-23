<template>
  <div ref="containerRef" class="relative" :data-ui-skin="uiSkin">
    <button
      :id="triggerId"
      ref="triggerRef"
      type="button"
      @click="toggle"
      :disabled="disabled"
      :role="searchable ? undefined : 'combobox'"
      :aria-expanded="isOpen"
      aria-haspopup="listbox"
      :aria-controls="listboxId"
      :aria-activedescendant="!searchable ? activeDescendantId : undefined"
      :aria-label="triggerAriaLabel"
      :aria-labelledby="ariaLabelledby"
      :class="[
        'select-trigger',
        isOpen && 'select-trigger-open',
        error && 'select-trigger-error',
        disabled && 'select-trigger-disabled'
      ]"
      @keydown="onTriggerKeyDown"
    >
      <span class="select-value">
        <slot name="selected" :option="selectedOption">
          {{ selectedLabel }}
        </slot>
      </span>
      <span class="select-icon">
        <Icon
          name="chevronDown"
          size="md"
          :class="['transition-transform duration-200', isOpen && 'rotate-180']"
        />
      </span>
    </button>

    <!-- Teleport dropdown to body to escape stacking context -->
    <Teleport to="body">
      <Transition name="select-dropdown">
        <div
          v-if="isOpen"
          ref="dropdownRef"
          class="select-dropdown-portal"
          :data-ui-skin="uiSkin"
          :data-dialog-focus-owner-id="triggerId"
          :class="[instanceId]"
          :style="dropdownStyle"
          @click.stop
          @mousedown.stop
          @keydown="onDropdownKeyDown"
        >
          <!-- Search input -->
          <div v-if="searchable" class="select-search">
            <Icon name="search" size="sm" class="text-gray-400" />
            <input
              ref="searchInputRef"
              v-model="searchQuery"
              type="text"
              role="combobox"
              autocomplete="off"
              aria-autocomplete="list"
              :aria-expanded="true"
              :aria-controls="listboxId"
              :aria-activedescendant="activeDescendantId"
              :aria-label="searchAriaLabel"
              :aria-labelledby="ariaLabelledby"
              :placeholder="searchPlaceholderText"
              class="select-search-input"
              @click.stop
            />
          </div>

          <!-- Options list -->
          <div
            :id="listboxId"
            ref="optionsListRef"
            class="select-options"
            role="listbox"
            :aria-labelledby="triggerId"
          >
            <div
              v-for="(option, index) in filteredOptions"
              :key="`${typeof getOptionValue(option)}:${String(getOptionValue(option) ?? '')}`"
              :id="getOptionId(index)"
              :role="isGroupHeaderOption(option) ? 'presentation' : 'option'"
              :aria-selected="isGroupHeaderOption(option) ? undefined : isSelected(option)"
              :aria-disabled="!isGroupHeaderOption(option) && isOptionDisabled(option) ? true : undefined"
              @click.stop="isOptionFocusable(option) && selectOption(option)"
              @mouseenter="handleOptionMouseEnter(option, index)"
              :class="[
                'select-option',
                isGroupHeaderOption(option) && 'select-option-group',
                isSelected(option) && 'select-option-selected',
                isOptionDisabled(option) && !isGroupHeaderOption(option) && 'select-option-disabled',
                focusedIndex === index && !isGroupHeaderOption(option) && 'select-option-focused'
              ]"
            >
              <slot name="option" :option="option" :selected="isSelected(option)">
                <Icon
                  v-if="option._creatable"
                  name="search"
                  size="sm"
                  class="flex-shrink-0 text-gray-400"
                />
                <span class="select-option-label" :class="option._creatable && 'italic text-gray-500 dark:text-dark-300'">{{ getOptionLabel(option) }}</span>
                <Icon
                  v-if="isSelected(option)"
                  name="check"
                  size="sm"
                  class="text-primary-500"
                  :stroke-width="2"
                />
              </slot>
            </div>

            <!-- Empty state -->
            <div v-if="filteredOptions.length === 0" class="select-empty">
              {{ emptyTextDisplay }}
            </div>
          </div>
        </div>
      </Transition>
    </Teleport>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, watch, onMounted, onUnmounted, nextTick } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '@/components/icons/Icon.vue'
import { useUiSkin } from '@/composables/useUiSkin'

const { t } = useI18n()
const uiSkin = useUiSkin()

// Instance ID for unique click-outside detection
const instanceId = `select-${Math.random().toString(36).substring(2, 9)}`
const triggerId = `${instanceId}-trigger`
const listboxId = `${instanceId}-listbox`
const VIEWPORT_PADDING = 16
const DROPDOWN_GAP = 4
const MIN_DROPDOWN_WIDTH = 200
const DEFAULT_DROPDOWN_HEIGHT = 240

export interface SelectOption {
  value: string | number | boolean | null
  label: string
  disabled?: boolean
  [key: string]: unknown
}

interface Props {
  modelValue: string | number | boolean | null | undefined
  options: SelectOption[] | Array<Record<string, unknown>>
  placeholder?: string
  disabled?: boolean
  error?: boolean
  searchable?: boolean
  searchPlaceholder?: string
  ariaLabel?: string
  ariaLabelledby?: string
  emptyText?: string
  valueKey?: string
  labelKey?: string
  creatable?: boolean
  creatablePrefix?: string
}

interface Emits {
  (e: 'update:modelValue', value: string | number | boolean | null): void
  (e: 'change', value: string | number | boolean | null, option: SelectOption | null): void
}

const props = withDefaults(defineProps<Props>(), {
  disabled: false,
  error: false,
  searchable: false,
  creatable: false,
  creatablePrefix: '',
  valueKey: 'value',
  labelKey: 'label'
})

const emit = defineEmits<Emits>()

const isOpen = ref(false)
const searchQuery = ref('')
const focusedIndex = ref(-1)
const containerRef = ref<HTMLElement | null>(null)
const triggerRef = ref<HTMLButtonElement | null>(null)
const searchInputRef = ref<HTMLInputElement | null>(null)
const dropdownRef = ref<HTMLElement | null>(null)
const optionsListRef = ref<HTMLElement | null>(null)
const dropdownPosition = ref<'bottom' | 'top'>('bottom')
const triggerRect = ref<DOMRect | null>(null)
const dropdownGeometry = ref({
  left: VIEWPORT_PADDING,
  minWidth: MIN_DROPDOWN_WIDTH,
  maxHeight: DEFAULT_DROPDOWN_HEIGHT,
  offset: 0
})

// i18n placeholders
const placeholderText = computed(() => props.placeholder ?? t('common.selectOption'))
const searchPlaceholderText = computed(() => props.searchPlaceholder ?? t('common.searchPlaceholder'))
const triggerAriaLabel = computed(() =>
  props.ariaLabelledby ? undefined : (props.ariaLabel ?? placeholderText.value)
)
const searchAriaLabel = computed(() =>
  props.ariaLabelledby ? undefined : (props.ariaLabel ?? searchPlaceholderText.value)
)
const emptyTextDisplay = computed(() => props.emptyText ?? t('common.noOptionsFound'))

// Computed style for teleported dropdown
const dropdownStyle = computed(() => {
  if (!triggerRect.value) return {}

  const geometry = dropdownGeometry.value
  const style: Record<string, string> = {
    position: 'fixed',
    left: `${geometry.left}px`,
    minWidth: `${geometry.minWidth}px`,
    maxWidth: 'calc(100vw - 2rem)',
    maxHeight: `${geometry.maxHeight}px`,
    zIndex: '100000020'
  }

  if (dropdownPosition.value === 'top') {
    style.bottom = `${geometry.offset}px`
  } else {
    style.top = `${geometry.offset}px`
  }

  return style
})

const getOptionValue = (option: any): any => {
  if (typeof option === 'object' && option !== null) {
    return option[props.valueKey]
  }
  return option
}

const getOptionLabel = (option: any): string => {
  if (typeof option === 'object' && option !== null) {
    return String(option[props.labelKey] ?? '')
  }
  return String(option ?? '')
}

const isOptionDisabled = (option: any): boolean => {
  if (typeof option === 'object' && option !== null) {
    return !!option.disabled
  }
  return false
}

const isGroupHeaderOption = (option: any): boolean => {
  if (typeof option === 'object' && option !== null) {
    return option.kind === 'group'
  }
  return false
}

const isOptionFocusable = (option: any): boolean => {
  return !isOptionDisabled(option) && !isGroupHeaderOption(option)
}

const selectedOption = computed(() => {
  return props.options.find((opt) => getOptionValue(opt) === props.modelValue) || null
})

const selectedLabel = computed(() => {
  if (selectedOption.value) {
    return getOptionLabel(selectedOption.value)
  }
  // In creatable mode, show the raw value if no matching option
  if (props.creatable && props.modelValue) {
    return String(props.modelValue)
  }
  return placeholderText.value
})

const filteredOptions = computed(() => {
  let opts = props.options as any[]
  if (props.searchable && searchQuery.value) {
    const query = searchQuery.value.toLowerCase()
    opts = opts.filter((opt) => {
      // Match label
      if (getOptionLabel(opt).toLowerCase().includes(query)) return true
      // Also match description if present
      if (opt.description && String(opt.description).toLowerCase().includes(query)) return true
      return false
    })
    // In creatable mode, always prepend a fuzzy search option
    if (props.creatable && searchQuery.value.trim()) {
      const trimmed = searchQuery.value.trim()
      const prefix = props.creatablePrefix || t('common.search')
      opts = [{ [props.valueKey]: trimmed, [props.labelKey]: `${prefix} "${trimmed}"`, _creatable: true }, ...opts]
    }
  }
  return opts
})

const isSelected = (option: any): boolean => {
  return getOptionValue(option) === props.modelValue
}

const getOptionId = (index: number): string => `${instanceId}-option-${index}`

const activeDescendantId = computed(() => {
  if (!isOpen.value || focusedIndex.value < 0) return undefined
  const option = filteredOptions.value[focusedIndex.value]
  return option && isOptionFocusable(option) ? getOptionId(focusedIndex.value) : undefined
})

const findNextEnabledIndex = (startIndex: number): number => {
  const opts = filteredOptions.value
  if (opts.length === 0) return -1
  for (let offset = 0; offset < opts.length; offset++) {
    const idx = (startIndex + offset) % opts.length
    if (isOptionFocusable(opts[idx])) return idx
  }
  return -1
}

const findPrevEnabledIndex = (startIndex: number): number => {
  const opts = filteredOptions.value
  if (opts.length === 0) return -1
  for (let offset = 0; offset < opts.length; offset++) {
    const idx = (startIndex - offset + opts.length) % opts.length
    if (isOptionFocusable(opts[idx])) return idx
  }
  return -1
}

const handleOptionMouseEnter = (option: any, index: number) => {
  if (!isOptionFocusable(option)) return
  focusedIndex.value = index
}

const resetFocusedIndex = () => {
  if (filteredOptions.value.length === 0) {
    focusedIndex.value = -1
    return
  }

  const selectedIndex = filteredOptions.value.findIndex(
    (option) => isSelected(option) && isOptionFocusable(option)
  )
  focusedIndex.value = selectedIndex >= 0 ? selectedIndex : findNextEnabledIndex(0)
}

const clamp = (value: number, min: number, max: number): number => {
  return Math.min(Math.max(value, min), Math.max(min, max))
}

const updateDropdownGeometry = (
  rect: DOMRect,
  measuredWidth: number,
  measuredHeight: number
) => {
  const availableWidth = Math.max(0, window.innerWidth - VIEWPORT_PADDING * 2)
  const minWidth = Math.min(Math.max(rect.width, MIN_DROPDOWN_WIDTH), availableWidth)
  const dropdownWidth = Math.min(Math.max(measuredWidth, minWidth), availableWidth)
  const maxLeft = window.innerWidth - VIEWPORT_PADDING - dropdownWidth
  const left = clamp(rect.left, VIEWPORT_PADDING, maxLeft)
  const spaceBelow = Math.max(
    0,
    window.innerHeight - rect.bottom - DROPDOWN_GAP - VIEWPORT_PADDING
  )
  const spaceAbove = Math.max(0, rect.top - DROPDOWN_GAP - VIEWPORT_PADDING)
  const position = spaceBelow < measuredHeight && spaceAbove > spaceBelow ? 'top' : 'bottom'
  const maxHeight = position === 'top' ? spaceAbove : spaceBelow
  const offset = position === 'top'
    ? window.innerHeight - rect.top + DROPDOWN_GAP
    : rect.bottom + DROPDOWN_GAP

  dropdownPosition.value = position
  dropdownGeometry.value = { left, minWidth, maxHeight, offset }
}

const calculateDropdownPosition = () => {
  if (!containerRef.value) return
  const rect = containerRef.value.getBoundingClientRect()
  triggerRect.value = rect
  const fallbackWidth = Math.max(rect.width, MIN_DROPDOWN_WIDTH)
  const measuredWidth = dropdownRef.value?.offsetWidth || fallbackWidth
  const measuredHeight = dropdownRef.value?.scrollHeight ||
    dropdownRef.value?.offsetHeight ||
    DEFAULT_DROPDOWN_HEIGHT
  updateDropdownGeometry(rect, measuredWidth, measuredHeight)

  nextTick(() => {
    if (!dropdownRef.value || !triggerRect.value) return
    const dropdownWidth = dropdownRef.value.offsetWidth || measuredWidth
    const dropdownHeight = dropdownRef.value.scrollHeight ||
      dropdownRef.value.offsetHeight ||
      measuredHeight
    updateDropdownGeometry(triggerRect.value, dropdownWidth, dropdownHeight)
  })
}

const toggle = () => {
  if (props.disabled) return
  isOpen.value = !isOpen.value
}

watch(isOpen, (open) => {
  if (open) {
    calculateDropdownPosition()
    resetFocusedIndex()

    if (props.searchable) {
      nextTick(() => searchInputRef.value?.focus())
    }
    window.addEventListener('scroll', calculateDropdownPosition, { capture: true, passive: true })
    window.addEventListener('resize', calculateDropdownPosition)
  } else {
    searchQuery.value = ''
    focusedIndex.value = -1
    window.removeEventListener('scroll', calculateDropdownPosition, { capture: true })
    window.removeEventListener('resize', calculateDropdownPosition)
  }
})

watch(filteredOptions, () => {
  if (isOpen.value) resetFocusedIndex()
})

const closeDropdown = (restoreFocus = false) => {
  isOpen.value = false
  if (restoreFocus) {
    nextTick(() => triggerRef.value?.focus())
  }
}

const selectOption = (option: any) => {
  if (!isOptionFocusable(option)) return
  const value = getOptionValue(option) ?? null
  emit('update:modelValue', value)
  emit('change', value, option)
  closeDropdown(true)
}

// Keyboards
const onTriggerKeyDown = (event: KeyboardEvent) => {
  switch (event.key) {
    case 'ArrowDown':
    case 'ArrowUp':
      event.preventDefault()
      if (!isOpen.value) {
        isOpen.value = true
        return
      }
      focusedIndex.value = event.key === 'ArrowDown'
        ? findNextEnabledIndex(focusedIndex.value + 1)
        : findPrevEnabledIndex(focusedIndex.value - 1)
      if (focusedIndex.value >= 0) scrollToFocused()
      break
    case 'Home':
    case 'End':
      if (!isOpen.value) return
      event.preventDefault()
      focusedIndex.value = event.key === 'Home'
        ? findNextEnabledIndex(0)
        : findPrevEnabledIndex(filteredOptions.value.length - 1)
      if (focusedIndex.value >= 0) scrollToFocused()
      break
    case 'Enter':
    case ' ':
      event.preventDefault()
      if (!isOpen.value) {
        isOpen.value = true
        return
      }
      if (!props.searchable && focusedIndex.value >= 0) {
        selectOption(filteredOptions.value[focusedIndex.value])
      }
      break
    case 'Escape':
      if (!isOpen.value) return
      event.preventDefault()
      event.stopPropagation()
      closeDropdown(true)
      break
    case 'Tab':
      if (isOpen.value) closeDropdown(false)
      break
  }
}

const onDropdownKeyDown = (e: KeyboardEvent) => {
  const preserveSearchInputEditing = props.searchable && e.target === searchInputRef.value

  switch (e.key) {
    case 'ArrowDown':
      e.preventDefault()
      focusedIndex.value = findNextEnabledIndex(focusedIndex.value + 1)
      if (focusedIndex.value >= 0) scrollToFocused()
      break
    case 'ArrowUp':
      e.preventDefault()
      focusedIndex.value = findPrevEnabledIndex(focusedIndex.value - 1)
      if (focusedIndex.value >= 0) scrollToFocused()
      break
    case 'Home':
      if (preserveSearchInputEditing) return
      e.preventDefault()
      focusedIndex.value = findNextEnabledIndex(0)
      if (focusedIndex.value >= 0) scrollToFocused()
      break
    case 'End':
      if (preserveSearchInputEditing) return
      e.preventDefault()
      focusedIndex.value = findPrevEnabledIndex(filteredOptions.value.length - 1)
      if (focusedIndex.value >= 0) scrollToFocused()
      break
    case 'Enter':
      e.preventDefault()
      if (focusedIndex.value >= 0 && focusedIndex.value < filteredOptions.value.length) {
        const opt = filteredOptions.value[focusedIndex.value]
        if (isOptionFocusable(opt)) selectOption(opt)
      }
      break
    case 'Escape':
      e.preventDefault()
      e.stopPropagation()
      closeDropdown(true)
      break
    case 'Tab': {
      if (triggerRef.value?.closest('[role="dialog"][aria-modal="true"]')) {
        closeDropdown(false)
        break
      }

      const focusTarget = getAdjacentTriggerFocusTarget(e.shiftKey)
      if (focusTarget) {
        e.preventDefault()
        closeDropdown(false)
        nextTick(() => {
          if (focusTarget.isConnected && focusTarget.tabIndex >= 0) focusTarget.focus()
        })
      } else {
        closeDropdown(false)
      }
      break
    }
  }
}

const focusableSelector = [
  'a[href]',
  'area[href]',
  'button:not([disabled])',
  'input:not([disabled]):not([type="hidden"])',
  'select:not([disabled])',
  'textarea:not([disabled])',
  'iframe',
  'object',
  'embed',
  '[contenteditable="true"]',
  '[tabindex]:not([tabindex="-1"])'
].join(',')

const isAvailableFocusTarget = (element: HTMLElement): boolean => {
  if (!element.isConnected || element.tabIndex < 0 || dropdownRef.value?.contains(element)) {
    return false
  }
  if (element.matches(':disabled') || element.closest('[hidden], [inert], [aria-hidden="true"]')) {
    return false
  }

  let current: HTMLElement | null = element
  while (current) {
    const style = window.getComputedStyle(current)
    if (style.display === 'none' || style.visibility === 'hidden') return false
    current = current.parentElement
  }
  return true
}

const getAdjacentTriggerFocusTarget = (backward: boolean): HTMLElement | null => {
  const trigger = triggerRef.value
  if (!trigger) return null
  const focusableElements = Array.from(
    document.querySelectorAll<HTMLElement>(focusableSelector)
  ).filter(isAvailableFocusTarget)
  const triggerIndex = focusableElements.indexOf(trigger)
  if (triggerIndex < 0) return null
  return focusableElements[triggerIndex + (backward ? -1 : 1)] ?? null
}

const scrollToFocused = () => {
  nextTick(() => {
    const list = optionsListRef.value
    if (!list) return
    const focusedEl = list.children[focusedIndex.value] as HTMLElement
    if (!focusedEl) return

    if (focusedEl.offsetTop < list.scrollTop) {
      list.scrollTop = focusedEl.offsetTop
    } else if (focusedEl.offsetTop + focusedEl.offsetHeight > list.scrollTop + list.offsetHeight) {
      list.scrollTop = focusedEl.offsetTop + focusedEl.offsetHeight - list.offsetHeight
    }
  })
}

const handleClickOutside = (event: MouseEvent) => {
  const target = event.target
  if (!(target instanceof Element)) return
  // Check if click is inside THIS specific instance's dropdown or trigger
  const isInDropdown = !!target.closest(`.${instanceId}`)
  const isInTrigger = containerRef.value?.contains(target)

  if (!isInDropdown && !isInTrigger && isOpen.value) {
    closeDropdown(false)
  }
}

onMounted(() => {
  document.addEventListener('click', handleClickOutside)
})

onUnmounted(() => {
  document.removeEventListener('click', handleClickOutside)
  window.removeEventListener('scroll', calculateDropdownPosition, { capture: true })
  window.removeEventListener('resize', calculateDropdownPosition)
})
</script>

<style scoped>
.select-trigger {
  @apply flex w-full items-center justify-between gap-2;
  @apply rounded-xl px-4 py-2.5 text-sm;
  @apply bg-white dark:bg-dark-800;
  @apply border border-gray-200 dark:border-dark-600;
  @apply text-gray-900 dark:text-gray-100;
  @apply transition-all duration-200;
  @apply focus:border-primary-500 focus:outline-none focus:ring-2 focus:ring-primary-500/30;
  @apply hover:border-gray-300 dark:hover:border-dark-500;
  @apply cursor-pointer;
}

.select-trigger-open {
  @apply border-primary-500 ring-2 ring-primary-500/30;
}

.select-trigger-error {
  @apply border-red-500 focus:border-red-500 focus:ring-red-500/30;
}

.select-trigger-disabled {
  @apply cursor-not-allowed bg-gray-100 opacity-60 dark:bg-dark-900;
}

.select-value {
  @apply flex-1 truncate text-left;
}

.select-icon {
  @apply flex-shrink-0 text-gray-400 dark:text-dark-400;
}
</style>

<style>
.select-dropdown-portal {
  @apply flex w-max flex-col;
  @apply bg-white dark:bg-dark-800;
  @apply rounded-xl;
  @apply border border-gray-200 dark:border-dark-700;
  @apply shadow-lg shadow-black/10 dark:shadow-black/30;
  @apply overflow-hidden;
  pointer-events: auto !important;
}

.select-dropdown-portal .select-search {
  @apply flex flex-shrink-0 items-center gap-2 px-3 py-2;
  @apply border-b border-gray-100 dark:border-dark-700;
}

.select-dropdown-portal .select-search-input {
  @apply min-w-0 flex-1 bg-transparent text-sm;
  @apply text-gray-900 dark:text-gray-100;
  @apply placeholder:text-gray-400 dark:placeholder:text-dark-400;
  @apply focus:outline-none;
}

.select-dropdown-portal .select-options {
  @apply min-h-0 max-h-60 overflow-y-auto py-1 outline-none;
}

.select-dropdown-portal .select-option {
  @apply flex items-center justify-between gap-2;
  @apply px-4 py-2.5 text-sm;
  @apply text-gray-700 dark:text-gray-300;
  @apply cursor-pointer transition-colors duration-150;
  @apply hover:bg-gray-50 dark:hover:bg-dark-700;
  pointer-events: auto !important;
}

.select-dropdown-portal .select-option-selected {
  @apply bg-primary-50 dark:bg-primary-900/20;
  @apply text-primary-700 dark:text-primary-300;
}

.select-dropdown-portal .select-option-focused {
  @apply bg-gray-100 dark:bg-dark-700;
}

.select-dropdown-portal .select-option-disabled {
  @apply cursor-not-allowed opacity-40;
}

.select-dropdown-portal .select-option-group {
  @apply cursor-default select-none;
  @apply bg-gray-50 dark:bg-dark-900;
  @apply text-[11px] font-bold uppercase tracking-wider;
  @apply text-gray-500 dark:text-gray-400;
}

.select-dropdown-portal .select-option-group:hover {
  @apply bg-gray-50 dark:bg-dark-900;
}

.select-dropdown-portal .select-option-label {
  @apply flex-1 min-w-0 truncate text-left;
}

.select-dropdown-portal .select-empty {
  @apply px-4 py-8 text-center text-sm;
  @apply text-gray-500 dark:text-dark-400;
}

/* The portal is outside the page skin boundary, so its v2 appearance must travel with it. */
.select-dropdown-portal[data-ui-skin='v2'] {
  border-color: rgb(var(--ui-border));
  border-radius: 0.875rem;
  background-color: rgb(var(--ui-surface-elevated));
  color: rgb(var(--ui-text));
  box-shadow: var(--ui-shadow-elevated);
}

.select-dropdown-portal[data-ui-skin='v2'] .select-search {
  border-color: rgb(var(--ui-border));
}

.select-dropdown-portal[data-ui-skin='v2'] .select-search-input {
  color: rgb(var(--ui-text));
}

.select-dropdown-portal[data-ui-skin='v2'] .select-option {
  min-height: 2.5rem;
  color: rgb(var(--ui-text-muted));
}

.select-dropdown-portal[data-ui-skin='v2'] .select-option:hover,
.select-dropdown-portal[data-ui-skin='v2'] .select-option-focused {
  background-color: rgb(var(--ui-surface-hover));
  color: rgb(var(--ui-text));
}

.select-dropdown-portal[data-ui-skin='v2'] .select-option-selected {
  background-color: rgb(var(--ui-brand-soft));
  color: rgb(var(--ui-brand));
}

.select-dropdown-portal[data-ui-skin='v2'] .select-option-group,
.select-dropdown-portal[data-ui-skin='v2'] .select-option-group:hover {
  background-color: rgb(var(--ui-surface-subtle));
  color: rgb(var(--ui-text-subtle));
}

.select-dropdown-portal[data-ui-skin='v2'] .select-empty {
  color: rgb(var(--ui-text-subtle));
}

@media (pointer: coarse), (any-pointer: coarse) {
  .select-dropdown-portal .select-search {
    min-height: 44px;
    padding-block: 0;
  }

  .select-dropdown-portal .select-search-input {
    min-height: 44px;
  }

  .select-dropdown-portal .select-option:not(.select-option-group) {
    min-height: 44px;
  }
}

.select-dropdown-enter-active,
.select-dropdown-leave-active {
  transition: all 0.2s ease;
}

.select-dropdown-enter-from,
.select-dropdown-leave-to {
  opacity: 0;
  transform: translateY(-8px);
}
</style>
