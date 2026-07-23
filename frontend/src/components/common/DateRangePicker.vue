<template>
  <div
    ref="containerRef"
    class="relative"
    @keydown="handleKeydown"
  >
    <button
      :id="triggerId"
      ref="triggerRef"
      type="button"
      @click="toggle"
      :aria-expanded="isOpen"
      aria-haspopup="dialog"
      :aria-controls="dropdownId"
      :class="['date-picker-trigger', isOpen && 'date-picker-trigger-open']"
    >
      <span class="date-picker-icon">
        <Icon name="calendar" size="sm" />
      </span>
      <span class="date-picker-value">
        {{ displayValue }}
      </span>
      <span class="date-picker-chevron">
        <Icon
          name="chevronDown"
          size="sm"
          :class="['transition-transform duration-200', isOpen && 'rotate-180']"
        />
      </span>
    </button>

    <Transition name="date-picker-dropdown">
      <div
        v-if="isOpen"
        :id="dropdownId"
        ref="dropdownRef"
        class="date-picker-dropdown"
        role="dialog"
        :aria-labelledby="triggerId"
        :data-placement="dropdownPosition"
        :style="dropdownStyle"
      >
        <!-- Quick presets -->
        <div class="date-picker-presets">
          <button
            v-for="preset in presets"
            :key="preset.value"
            type="button"
            @click="selectPreset(preset)"
            :aria-pressed="isPresetActive(preset)"
            :class="['date-picker-preset', isPresetActive(preset) && 'date-picker-preset-active']"
          >
            {{ t(preset.labelKey) }}
          </button>
        </div>

        <div class="date-picker-divider"></div>

        <!-- Custom date range inputs -->
        <div class="date-picker-custom">
          <div class="date-picker-field">
            <label :for="startInputId" class="date-picker-label">{{ t('dates.startDate') }}</label>
            <input
              :id="startInputId"
              type="date"
              v-model="localStartDate"
              :max="localEndDate || tomorrow"
              class="date-picker-input"
              @change="onDateChange"
            />
          </div>
          <div class="date-picker-separator">
            <Icon name="arrowRight" size="sm" class="text-gray-400" />
          </div>
          <div class="date-picker-field">
            <label :for="endInputId" class="date-picker-label">{{ t('dates.endDate') }}</label>
            <input
              :id="endInputId"
              type="date"
              v-model="localEndDate"
              :min="localStartDate"
              :max="tomorrow"
              class="date-picker-input"
              @change="onDateChange"
            />
          </div>
        </div>

        <!-- Apply button -->
        <div class="date-picker-actions">
          <button type="button" @click="apply" class="date-picker-apply">
            {{ t('dates.apply') }}
          </button>
        </div>
      </div>
    </Transition>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, watch, onMounted, onUnmounted, nextTick } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '@/components/icons/Icon.vue'

interface DatePreset {
  labelKey: string
  value: string
  getRange: () => { start: string; end: string }
}

interface Props {
  startDate: string
  endDate: string
}

interface Emits {
  (e: 'update:startDate', value: string): void
  (e: 'update:endDate', value: string): void
  (e: 'change', range: { startDate: string; endDate: string; preset: string | null }): void
}

const props = defineProps<Props>()
const emit = defineEmits<Emits>()

const { t, locale } = useI18n()

const instanceId = `date-range-${Math.random().toString(36).substring(2, 9)}`
const triggerId = `${instanceId}-trigger`
const dropdownId = `${instanceId}-dialog`
const startInputId = `${instanceId}-start`
const endInputId = `${instanceId}-end`
const VIEWPORT_PADDING = 16
const DROPDOWN_GAP = 4
const DEFAULT_DROPDOWN_WIDTH = 320
const DEFAULT_DROPDOWN_HEIGHT = 300
const isOpen = ref(false)
const containerRef = ref<HTMLElement | null>(null)
const triggerRef = ref<HTMLButtonElement | null>(null)
const dropdownRef = ref<HTMLElement | null>(null)
const dropdownPosition = ref<'bottom' | 'top'>('bottom')
const dropdownGeometry = ref({
  left: 0,
  maxHeight: DEFAULT_DROPDOWN_HEIGHT,
  offset: 0
})
const localStartDate = ref(props.startDate)
const localEndDate = ref(props.endDate)
const activePreset = ref<string | null>(null)
const committedSelection = ref<{
  startDate: string
  endDate: string
  preset: string | null
} | null>(null)

const dropdownStyle = computed(() => {
  const geometry = dropdownGeometry.value
  const style: Record<string, string> = {
    left: `${geometry.left}px`,
    maxWidth: 'calc(100vw - 2rem)',
    maxHeight: `${geometry.maxHeight}px`
  }

  if (dropdownPosition.value === 'top') {
    style.bottom = `${geometry.offset}px`
  } else {
    style.top = `${geometry.offset}px`
  }
  return style
})

const today = computed(() => {
  // Use local timezone to avoid UTC timezone issues
  const now = new Date()
  const year = now.getFullYear()
  const month = String(now.getMonth() + 1).padStart(2, '0')
  const day = String(now.getDate()).padStart(2, '0')
  return `${year}-${month}-${day}`
})

// Tomorrow's date - used for max date to handle timezone differences
// When user is in a timezone behind the server, "today" on server might be "tomorrow" locally
const tomorrow = computed(() => {
  const d = new Date()
  d.setDate(d.getDate() + 1)
  return formatDateToString(d)
})

// Helper function to format date to YYYY-MM-DD using local timezone
const formatDateToString = (date: Date): string => {
  const year = date.getFullYear()
  const month = String(date.getMonth() + 1).padStart(2, '0')
  const day = String(date.getDate()).padStart(2, '0')
  return `${year}-${month}-${day}`
}

const presets: DatePreset[] = [
  {
    labelKey: 'dates.today',
    value: 'today',
    getRange: () => {
      const t = today.value
      return { start: t, end: t }
    }
  },
  {
    labelKey: 'dates.yesterday',
    value: 'yesterday',
    getRange: () => {
      const d = new Date()
      d.setDate(d.getDate() - 1)
      const yesterday = formatDateToString(d)
      return { start: yesterday, end: yesterday }
    }
  },
  {
    labelKey: 'dates.last24Hours',
    value: 'last24Hours',
    getRange: () => {
      const end = new Date()
      const start = new Date(end.getTime() - 24 * 60 * 60 * 1000)
      return {
        start: formatDateToString(start),
        end: formatDateToString(end)
      }
    }
  },
  {
    labelKey: 'dates.last7Days',
    value: '7days',
    getRange: () => {
      const end = today.value
      const d = new Date()
      d.setDate(d.getDate() - 6)
      const start = formatDateToString(d)
      return { start, end }
    }
  },
  {
    labelKey: 'dates.last14Days',
    value: '14days',
    getRange: () => {
      const end = today.value
      const d = new Date()
      d.setDate(d.getDate() - 13)
      const start = formatDateToString(d)
      return { start, end }
    }
  },
  {
    labelKey: 'dates.last30Days',
    value: '30days',
    getRange: () => {
      const end = today.value
      const d = new Date()
      d.setDate(d.getDate() - 29)
      const start = formatDateToString(d)
      return { start, end }
    }
  },
  {
    labelKey: 'dates.thisMonth',
    value: 'thisMonth',
    getRange: () => {
      const now = new Date()
      const start = formatDateToString(new Date(now.getFullYear(), now.getMonth(), 1))
      return { start, end: today.value }
    }
  },
  {
    labelKey: 'dates.lastMonth',
    value: 'lastMonth',
    getRange: () => {
      const now = new Date()
      const start = formatDateToString(new Date(now.getFullYear(), now.getMonth() - 1, 1))
      const end = formatDateToString(new Date(now.getFullYear(), now.getMonth(), 0))
      return { start, end }
    }
  }
]

const displayValue = computed(() => {
  if (activePreset.value) {
    const preset = presets.find((p) => p.value === activePreset.value)
    if (preset) return t(preset.labelKey)
  }

  if (localStartDate.value && localEndDate.value) {
    if (localStartDate.value === localEndDate.value) {
      return formatDate(localStartDate.value)
    }
    return `${formatDate(localStartDate.value)} - ${formatDate(localEndDate.value)}`
  }

  return t('dates.selectDateRange')
})

const formatDate = (dateStr: string): string => {
  const date = new Date(dateStr + 'T00:00:00')
  const dateLocale = locale.value === 'zh' ? 'zh-CN' : 'en-US'
  return date.toLocaleDateString(dateLocale, { month: 'short', day: 'numeric' })
}

const isPresetActive = (preset: DatePreset): boolean => {
  return activePreset.value === preset.value
}

const selectPreset = (preset: DatePreset) => {
  const range = preset.getRange()
  localStartDate.value = range.start
  localEndDate.value = range.end
  activePreset.value = preset.value
}

const onDateChange = () => {
  // Check if current dates match any preset
  activePreset.value = null
  for (const preset of presets) {
    // A rolling 24-hour window cannot be reconstructed from date-only fields.
    // Only an explicit click may select this preset; otherwise yesterday→today
    // must remain a calendar range.
    if (preset.value === 'last24Hours') continue
    const range = preset.getRange()
    if (range.start === localStartDate.value && range.end === localEndDate.value) {
      activePreset.value = preset.value
      break
    }
  }
}

const syncDraftFromProps = () => {
  localStartDate.value = props.startDate
  localEndDate.value = props.endDate

  const committed = committedSelection.value
  if (
    committed &&
    committed.startDate === props.startDate &&
    committed.endDate === props.endDate
  ) {
    activePreset.value = committed.preset
    return
  }

  onDateChange()
  committedSelection.value = {
    startDate: props.startDate,
    endDate: props.endDate,
    preset: activePreset.value
  }
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
  const dropdownWidth = Math.min(measuredWidth, availableWidth)
  const viewportLeft = clamp(
    rect.left,
    VIEWPORT_PADDING,
    window.innerWidth - VIEWPORT_PADDING - dropdownWidth
  )
  const spaceBelow = Math.max(
    0,
    window.innerHeight - rect.bottom - DROPDOWN_GAP - VIEWPORT_PADDING
  )
  const spaceAbove = Math.max(0, rect.top - DROPDOWN_GAP - VIEWPORT_PADDING)
  const position = spaceBelow < measuredHeight && spaceAbove > spaceBelow ? 'top' : 'bottom'

  dropdownPosition.value = position
  dropdownGeometry.value = {
    left: viewportLeft - rect.left,
    maxHeight: position === 'top' ? spaceAbove : spaceBelow,
    offset: rect.height + DROPDOWN_GAP
  }
}

const calculateDropdownPosition = () => {
  if (!containerRef.value || !isOpen.value) return
  const rect = containerRef.value.getBoundingClientRect()
  const fallbackWidth = Math.min(
    DEFAULT_DROPDOWN_WIDTH,
    Math.max(0, window.innerWidth - VIEWPORT_PADDING * 2)
  )
  const measuredWidth = dropdownRef.value?.offsetWidth || fallbackWidth
  const measuredHeight = dropdownRef.value?.scrollHeight ||
    dropdownRef.value?.offsetHeight ||
    DEFAULT_DROPDOWN_HEIGHT
  updateDropdownGeometry(rect, measuredWidth, measuredHeight)

  nextTick(() => {
    if (!dropdownRef.value || !containerRef.value || !isOpen.value) return
    const currentRect = containerRef.value.getBoundingClientRect()
    const dropdownWidth = dropdownRef.value.offsetWidth || measuredWidth
    const dropdownHeight = dropdownRef.value.scrollHeight ||
      dropdownRef.value.offsetHeight ||
      measuredHeight
    updateDropdownGeometry(currentRect, dropdownWidth, dropdownHeight)
  })
}

const toggle = () => {
  if (isOpen.value) {
    closePicker(false)
    return
  }

  syncDraftFromProps()
  isOpen.value = true
}

const closePicker = (restoreFocus = false, discardDraft = true) => {
  if (!isOpen.value) return
  isOpen.value = false
  if (discardDraft) syncDraftFromProps()
  if (restoreFocus) triggerRef.value?.focus()
}

const handleKeydown = (event: KeyboardEvent) => {
  if (event.key !== 'Escape' || !isOpen.value) return
  event.preventDefault()
  event.stopPropagation()
  closePicker(true)
}

const handleDocumentFocusIn = (event: FocusEvent) => {
  const target = event.target
  if (
    isOpen.value &&
    target instanceof Node &&
    containerRef.value &&
    !containerRef.value.contains(target)
  ) {
    closePicker(false)
  }
}

const apply = () => {
  committedSelection.value = {
    startDate: localStartDate.value,
    endDate: localEndDate.value,
    preset: activePreset.value
  }
  emit('update:startDate', localStartDate.value)
  emit('update:endDate', localEndDate.value)
  emit('change', {
    startDate: localStartDate.value,
    endDate: localEndDate.value,
    preset: activePreset.value
  })
  closePicker(true, false)
}

const handleClickOutside = (event: MouseEvent) => {
  if (containerRef.value && !containerRef.value.contains(event.target as Node)) {
    closePicker(false)
  }
}

// Sync local state with externally committed values without erasing an explicitly
// applied rolling preset when its date-only display values are echoed via v-model.
watch(
  [() => props.startDate, () => props.endDate],
  syncDraftFromProps
)

watch(isOpen, (open) => {
  if (open) {
    calculateDropdownPosition()
    window.addEventListener('scroll', calculateDropdownPosition, {
      capture: true,
      passive: true
    })
    window.addEventListener('resize', calculateDropdownPosition)
  } else {
    window.removeEventListener('scroll', calculateDropdownPosition, { capture: true })
    window.removeEventListener('resize', calculateDropdownPosition)
  }
})

onMounted(() => {
  document.addEventListener('click', handleClickOutside)
  document.addEventListener('focusin', handleDocumentFocusIn)
  syncDraftFromProps()
})

onUnmounted(() => {
  document.removeEventListener('click', handleClickOutside)
  document.removeEventListener('focusin', handleDocumentFocusIn)
  window.removeEventListener('scroll', calculateDropdownPosition, { capture: true })
  window.removeEventListener('resize', calculateDropdownPosition)
})
</script>

<style scoped>
.date-picker-trigger {
  @apply flex min-h-11 items-center gap-2;
  @apply rounded-lg px-3 py-2 text-sm;
  @apply bg-white dark:bg-dark-800;
  @apply border border-gray-200 dark:border-dark-600;
  @apply text-gray-700 dark:text-gray-300;
  @apply transition-all duration-200;
  @apply focus:border-primary-500 focus:outline-none focus:ring-2 focus:ring-primary-500/30;
  @apply hover:border-gray-300 dark:hover:border-dark-500;
  @apply cursor-pointer;
}

.date-picker-trigger-open {
  @apply border-primary-500 ring-2 ring-primary-500/30;
}

.date-picker-icon {
  @apply text-gray-400 dark:text-dark-400;
}

.date-picker-value {
  @apply font-medium;
}

.date-picker-chevron {
  @apply text-gray-400 dark:text-dark-400;
}

.date-picker-dropdown {
  @apply absolute z-[100];
  @apply bg-white dark:bg-dark-800;
  @apply rounded-xl;
  @apply border border-gray-200 dark:border-dark-700;
  @apply shadow-lg shadow-black/10 dark:shadow-black/30;
  @apply overflow-x-hidden overflow-y-auto;
  width: 20rem;
  max-width: calc(100vw - 2rem);
}

.date-picker-presets {
  @apply grid grid-cols-2 gap-1 p-2;
}

.date-picker-preset {
  @apply min-h-11 rounded-md px-3 py-1.5 text-xs font-medium;
  @apply text-gray-600 dark:text-gray-400;
  @apply hover:bg-gray-100 dark:hover:bg-dark-700;
  @apply transition-colors duration-150;
}

.date-picker-preset-active {
  @apply bg-primary-100 dark:bg-primary-900/30;
  @apply text-primary-700 dark:text-primary-300;
}

.date-picker-divider {
  @apply border-t border-gray-100 dark:border-dark-700;
}

.date-picker-custom {
  @apply flex items-end gap-2 p-3;
}

.date-picker-field {
  @apply min-w-0 flex-1;
}

.date-picker-label {
  @apply mb-1 block text-xs font-medium text-gray-500 dark:text-gray-400;
}

.date-picker-input {
  @apply min-h-11 min-w-0 w-full rounded-md px-2 py-1.5 text-sm;
  @apply bg-gray-50 dark:bg-dark-700;
  @apply border border-gray-200 dark:border-dark-600;
  @apply text-gray-900 dark:text-gray-100;
  @apply focus:border-primary-500 focus:outline-none focus:ring-2 focus:ring-primary-500/30;
}

.date-picker-input::-webkit-calendar-picker-indicator {
  @apply cursor-pointer opacity-60 hover:opacity-100;
  filter: invert(0.5);
}

.dark .date-picker-input::-webkit-calendar-picker-indicator {
  filter: invert(0.7);
}

.date-picker-separator {
  @apply flex items-center justify-center pb-1;
}

.date-picker-actions {
  @apply flex justify-end p-2 pt-0;
}

.date-picker-apply {
  @apply min-h-11 rounded-lg px-4 py-1.5 text-sm font-medium;
  @apply bg-primary-600 text-white;
  @apply hover:bg-primary-700;
  @apply transition-colors duration-150;
}

/* Dropdown animation */
.date-picker-dropdown-enter-active,
.date-picker-dropdown-leave-active {
  transition: all 0.2s ease;
}

.date-picker-dropdown-enter-from,
.date-picker-dropdown-leave-to {
  opacity: 0;
  transform: translateY(-8px);
}
</style>
