<script setup lang="ts">
import { nextTick, onBeforeUnmount, onMounted, ref, useTemplateRef } from 'vue'

type TooltipPlacement = 'top' | 'bottom'

const VIEWPORT_PADDING = 16
const TOOLTIP_GAP = 8
const ARROW_EDGE_PADDING = 12

const props = withDefaults(defineProps<{
  content?: string
  trigger?: 'hover' | 'click'
  widthClass?: string
}>(), {
  trigger: 'hover',
  widthClass: 'w-64',
})

const show = ref(false)
const triggerRef = useTemplateRef<HTMLElement>('trigger')
const tooltipRef = useTemplateRef<HTMLElement>('tooltip')
const tooltipStyle = ref({ top: '0px', left: '0px' })
const arrowStyle = ref({ left: '50%' })
const placement = ref<TooltipPlacement>('top')

function clamp(value: number, min: number, max: number): number {
  if (max < min) return min
  return Math.min(Math.max(value, min), max)
}

function openTooltip() {
  show.value = true
  nextTick(updatePosition)
}

function closeTooltip() {
  show.value = false
}

function onEnter() {
  if (props.trigger !== 'hover') return
  openTooltip()
}

function onLeave() {
  if (props.trigger !== 'hover') return
  closeTooltip()
}

function onClick(event: MouseEvent) {
  if (props.trigger !== 'click') return
  event.stopPropagation()
  if (show.value) {
    closeTooltip()
    return
  }
  openTooltip()
}

function onDocumentClick(event: MouseEvent) {
  if (props.trigger !== 'click' || !show.value) return
  const target = event.target as Node | null
  if (!target) return
  if (triggerRef.value?.contains(target) || tooltipRef.value?.contains(target)) return
  closeTooltip()
}

function onDocumentKeydown(event: KeyboardEvent) {
  if (props.trigger !== 'click') return
  if (event.key === 'Escape') {
    closeTooltip()
  }
}

function onViewportChange() {
  if (!show.value) return
  updatePosition()
}

function updatePosition() {
  const trigger = triggerRef.value
  const tooltip = tooltipRef.value
  if (!trigger || !tooltip) return

  const triggerRect = trigger.getBoundingClientRect()
  const tooltipRect = tooltip.getBoundingClientRect()
  const viewportWidth = window.innerWidth
  const viewportHeight = window.innerHeight
  const tooltipWidth = Math.min(
    tooltipRect.width,
    Math.max(viewportWidth - VIEWPORT_PADDING * 2, 0),
  )
  const triggerCenter = triggerRect.left + triggerRect.width / 2
  const left = clamp(
    triggerCenter - tooltipWidth / 2,
    VIEWPORT_PADDING,
    viewportWidth - VIEWPORT_PADDING - tooltipWidth,
  )

  const preferredTop = triggerRect.top - TOOLTIP_GAP - tooltipRect.height
  const preferredBottom = triggerRect.bottom + TOOLTIP_GAP
  const fitsAbove = preferredTop >= VIEWPORT_PADDING
  const fitsBelow = preferredBottom + tooltipRect.height <= viewportHeight - VIEWPORT_PADDING
  const availableAbove = triggerRect.top - TOOLTIP_GAP - VIEWPORT_PADDING
  const availableBelow = viewportHeight - VIEWPORT_PADDING - preferredBottom

  let top: number
  if (fitsAbove || (!fitsBelow && availableAbove >= availableBelow)) {
    placement.value = 'top'
    top = clamp(
      preferredTop,
      VIEWPORT_PADDING,
      viewportHeight - VIEWPORT_PADDING - tooltipRect.height,
    )
  } else {
    placement.value = 'bottom'
    top = clamp(
      preferredBottom,
      VIEWPORT_PADDING,
      viewportHeight - VIEWPORT_PADDING - tooltipRect.height,
    )
  }

  tooltipStyle.value = {
    top: `${top}px`,
    left: `${left}px`,
  }
  arrowStyle.value = {
    left: `${clamp(
      triggerCenter - left,
      ARROW_EDGE_PADDING,
      tooltipWidth - ARROW_EDGE_PADDING,
    )}px`,
  }
}

onMounted(() => {
  document.addEventListener('click', onDocumentClick, true)
  document.addEventListener('keydown', onDocumentKeydown)
  window.addEventListener('resize', onViewportChange)
  window.addEventListener('scroll', onViewportChange, true)
})

onBeforeUnmount(() => {
  document.removeEventListener('click', onDocumentClick, true)
  document.removeEventListener('keydown', onDocumentKeydown)
  window.removeEventListener('resize', onViewportChange)
  window.removeEventListener('scroll', onViewportChange, true)
})
</script>

<template>
  <div
    ref="trigger"
    class="group relative ml-1 inline-flex items-center align-middle"
    @mouseenter="onEnter"
    @mouseleave="onLeave"
    @click="onClick"
  >
    <!-- Trigger Icon -->
    <slot name="trigger">
      <svg
        class="h-4 w-4 cursor-help text-gray-400 transition-colors hover:text-primary-600 dark:text-gray-500 dark:hover:text-primary-400"
        fill="none"
        viewBox="0 0 24 24"
        stroke="currentColor"
        stroke-width="2"
      >
        <path
          stroke-linecap="round"
          stroke-linejoin="round"
          d="M13 16h-1v-4h-1m1-4h.01M21 12a9 9 0 11-18 0 9 9 0 0118 0z"
        />
      </svg>
    </slot>

    <!-- Teleport to body to escape modal overflow clipping -->
    <Teleport to="body">
      <div
        ref="tooltip"
        v-show="show"
        role="tooltip"
        :data-placement="placement"
        :class="[
          'fixed z-[99999] max-w-[calc(100vw-2rem)] rounded-lg bg-gray-900 p-3 text-xs leading-relaxed text-white shadow-xl ring-1 ring-white/10 dark:bg-gray-800',
          props.widthClass,
        ]"
        :style="tooltipStyle"
      >
        <button
          v-if="props.trigger === 'click'"
          type="button"
          class="absolute right-1.5 top-1.5 rounded p-1 text-gray-300 transition-colors hover:bg-white/10 hover:text-white"
          aria-label="Close"
          @click.stop="closeTooltip"
        >
          <svg class="h-3.5 w-3.5" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2">
            <path stroke-linecap="round" stroke-linejoin="round" d="M6 18L18 6M6 6l12 12" />
          </svg>
        </button>
        <slot>{{ content }}</slot>
        <div
          class="absolute h-2 w-2 -translate-x-1/2 rotate-45 bg-gray-900 dark:bg-gray-800"
          :class="placement === 'top' ? '-bottom-1' : '-top-1'"
          :style="arrowStyle"
        ></div>
      </div>
    </Teleport>
  </div>
</template>
