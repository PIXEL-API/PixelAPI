import { computed, onBeforeUnmount, onMounted, reactive, shallowRef } from 'vue'

const usageDetailCompactBreakpoint = 1024

export function useUsageDetailTooltip<T>() {
  const visible = shallowRef(false)
  const position = reactive({ x: 0, y: 0 })
  const data = shallowRef<T | null>(null)
  const trigger = shallowRef<HTMLElement | null>(null)
  const isCompact = shallowRef(false)
  const isClickPinned = shallowRef(false)
  const style = computed(() =>
    isCompact.value
      ? { left: '0.5rem', right: '0.5rem', bottom: '0.5rem' }
      : { left: `${position.x}px`, top: `${position.y}px` }
  )

  const hide = () => {
    visible.value = false
    data.value = null
    trigger.value = null
    isClickPinned.value = false
  }

  const show = (event: Event, value: T) => {
    const currentTarget = event.currentTarget
    if (!(currentTarget instanceof HTMLElement)) return

    const rect = currentTarget.getBoundingClientRect()
    if (trigger.value !== currentTarget || data.value !== value) {
      isClickPinned.value = false
    }
    isCompact.value = window.innerWidth < usageDetailCompactBreakpoint
    trigger.value = currentTarget
    data.value = value
    position.x = rect.right + 8
    position.y = rect.top + rect.height / 2
    visible.value = true
  }

  const toggle = (event: MouseEvent, value: T) => {
    const currentTarget = event.currentTarget
    if (!(currentTarget instanceof HTMLElement)) return

    if (visible.value && data.value === value && isClickPinned.value) {
      hide()
      return
    }

    show(event, value)
    isClickPinned.value = true
  }

  const hideOnPointerLeave = (event: MouseEvent) => {
    if (event.currentTarget === document.activeElement || isClickPinned.value) return
    hide()
  }

  const hideOnEscape = (event: KeyboardEvent) => {
    hide()
    if (event.currentTarget instanceof HTMLElement) {
      event.currentTarget.blur()
    }
  }

  const isActive = (value: T) => visible.value && data.value === value

  const handleDocumentPointerDown = (event: PointerEvent) => {
    if (!visible.value) return
    const eventTarget = event.target
    if (eventTarget instanceof Node && trigger.value?.contains(eventTarget)) return
    hide()
  }

  onMounted(() => {
    document.addEventListener('pointerdown', handleDocumentPointerDown, true)
    window.addEventListener('resize', hide)
    window.addEventListener('scroll', hide, true)
  })

  onBeforeUnmount(() => {
    document.removeEventListener('pointerdown', handleDocumentPointerDown, true)
    window.removeEventListener('resize', hide)
    window.removeEventListener('scroll', hide, true)
  })

  return {
    visible,
    position,
    isCompact,
    style,
    data,
    show,
    toggle,
    hide,
    hideOnPointerLeave,
    hideOnEscape,
    isActive
  }
}
