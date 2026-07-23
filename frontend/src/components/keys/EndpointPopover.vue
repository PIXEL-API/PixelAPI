<script lang="ts">
let endpointPopoverInstanceCounter = 0
</script>

<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, type CSSProperties } from 'vue'
import { useI18n } from 'vue-i18n'
import { useClipboard } from '@/composables/useClipboard'
import type { CustomEndpoint } from '@/types'

const props = defineProps<{
  apiBaseUrl: string
  customEndpoints: CustomEndpoint[]
}>()

const { t } = useI18n()
const { copyToClipboard } = useClipboard()
const copiedEndpoint = ref<string | null>(null)

let copiedResetTimer: number | undefined
let copyRequestSequence = 0

type TooltipSide = 'above' | 'below'

interface TooltipPlacement {
  left: number
  width: number
  arrowLeft: number
  maxHeight: number
  isOverflowing: boolean
  side: TooltipSide
}

const tooltipPlacements = ref<Record<number, TooltipPlacement>>({})
const tooltipContentElements = new Map<number, HTMLElement>()
let activeTooltipAnchor: { index: number; element: HTMLElement } | null = null
const componentId = `endpoint-popover-${++endpointPopoverInstanceCounter}`
const endpointTooltipId = (index: number) => `${componentId}-tooltip-${index}`
const endpointTooltipDescriptionId = (index: number) => `${componentId}-tooltip-description-${index}`

const VIEWPORT_PADDING = 16
const TOOLTIP_GAP = 8
const MAX_TOOLTIP_WIDTH = 384
const FALLBACK_TOOLTIP_HEIGHT = 80

function clearCopiedResetTimer(): void {
  if (copiedResetTimer === undefined) return
  window.clearTimeout(copiedResetTimer)
  copiedResetTimer = undefined
}

const allEndpoints = computed(() => {
  const items: Array<{ name: string; endpoint: string; description: string; isDefault: boolean }> = []
  if (props.apiBaseUrl) {
    items.push({
      name: t('keys.endpoints.title'),
      endpoint: props.apiBaseUrl,
      description: '',
      isDefault: true,
    })
  }
  for (const ep of props.customEndpoints) {
    items.push({ ...ep, isDefault: false })
  }
  return items
})

async function copy(url: string) {
  const requestSequence = ++copyRequestSequence
  const success = await copyToClipboard(url, t('keys.endpoints.copied'))
  if (!success || requestSequence !== copyRequestSequence) return

  clearCopiedResetTimer()
  copiedEndpoint.value = url
  void nextTick(refreshActiveTooltipPlacement)
  const timer = window.setTimeout(() => {
    if (copiedResetTimer === timer) {
      copiedResetTimer = undefined
    }
    if (copiedEndpoint.value === url) {
      copiedEndpoint.value = null
      void nextTick(refreshActiveTooltipPlacement)
    }
  }, 1800)
  copiedResetTimer = timer
}

function tooltipHint(endpoint: string): string {
  return copiedEndpoint.value === endpoint
    ? t('keys.endpoints.copiedHint')
    : t('keys.endpoints.clickToCopy')
}

function speedTestUrl(endpoint: string): string {
  return `https://www.tcptest.cn/http/${encodeURIComponent(endpoint)}`
}

function setTooltipContentElement(index: number, element: unknown): void {
  if (element instanceof HTMLElement) {
    tooltipContentElements.set(index, element)
  } else {
    tooltipContentElements.delete(index)
  }
}

function getNaturalTooltipHeight(index: number): number {
  const element = tooltipContentElements.get(index)
  if (!element) return FALLBACK_TOOLTIP_HEIGHT

  const measuredHeight = Math.max(
    element.scrollHeight,
    element.getBoundingClientRect().height
  )
  return measuredHeight > 0 ? measuredHeight : FALLBACK_TOOLTIP_HEIGHT
}

function calculateTooltipPlacement(
  anchorRect: DOMRect,
  naturalTooltipHeight: number,
  viewportWidth: number,
  viewportHeight: number
): TooltipPlacement {
  const tooltipWidth = Math.max(
    1,
    Math.min(MAX_TOOLTIP_WIDTH, viewportWidth - VIEWPORT_PADDING * 2)
  )
  const centeredViewportLeft = anchorRect.left + anchorRect.width / 2 - tooltipWidth / 2
  const maximumViewportLeft = Math.max(
    VIEWPORT_PADDING,
    viewportWidth - tooltipWidth - VIEWPORT_PADDING
  )
  const viewportLeft = Math.min(
    Math.max(centeredViewportLeft, VIEWPORT_PADDING),
    maximumViewportLeft
  )
  const relativeLeft = viewportLeft - anchorRect.left
  const arrowPadding = 12
  const arrowLeft = Math.min(
    Math.max(anchorRect.width / 2 - relativeLeft, arrowPadding),
    Math.max(arrowPadding, tooltipWidth - arrowPadding)
  )

  const availableAbove = Math.max(
    1,
    anchorRect.top - VIEWPORT_PADDING - TOOLTIP_GAP
  )
  const availableBelow = Math.max(
    1,
    viewportHeight - anchorRect.bottom - VIEWPORT_PADDING - TOOLTIP_GAP
  )
  const fitsAbove = naturalTooltipHeight <= availableAbove
  const fitsBelow = naturalTooltipHeight <= availableBelow
  const side: TooltipSide = fitsAbove || (!fitsBelow && availableAbove >= availableBelow)
    ? 'above'
    : 'below'
  const maxHeight = Math.floor(side === 'above' ? availableAbove : availableBelow)

  return {
    left: relativeLeft,
    width: tooltipWidth,
    arrowLeft,
    maxHeight,
    isOverflowing: naturalTooltipHeight > maxHeight,
    side
  }
}

function updateTooltipPlacement(index: number, anchor: HTMLElement): void {
  const placement = calculateTooltipPlacement(
    anchor.getBoundingClientRect(),
    getNaturalTooltipHeight(index),
    Math.max(1, window.innerWidth),
    Math.max(1, window.innerHeight)
  )

  tooltipPlacements.value = {
    ...tooltipPlacements.value,
    [index]: placement
  }
}

function positionTooltip(index: number, event: MouseEvent | FocusEvent): void {
  const anchor = event.currentTarget
  if (!(anchor instanceof HTMLElement)) {
    throw new TypeError('Endpoint tooltip anchor must be an HTMLElement')
  }

  activeTooltipAnchor = { index, element: anchor }
  updateTooltipPlacement(index, anchor)
}

function tooltipStyle(index: number): CSSProperties {
  const placement = tooltipPlacements.value[index]
  if (!placement) {
    return {
      left: '0',
      width: '100%'
    }
  }
  return {
    left: `${placement.left}px`,
    width: `${placement.width}px`
  }
}

function tooltipArrowStyle(index: number): CSSProperties {
  const placement = tooltipPlacements.value[index]
  return {
    left: placement ? `${placement.arrowLeft}px` : '50%'
  }
}

function tooltipContentStyle(index: number): CSSProperties {
  const placement = tooltipPlacements.value[index]
  return placement ? { maxHeight: `${placement.maxHeight}px` } : {}
}

function isTooltipScrollable(index: number): boolean {
  return tooltipPlacements.value[index]?.isOverflowing === true
}

function tooltipPositionClass(index: number): string {
  return tooltipPlacements.value[index]?.side === 'below'
    ? 'top-full mt-2'
    : 'bottom-full mb-2'
}

function tooltipArrowPositionClass(index: number): string {
  return tooltipPlacements.value[index]?.side === 'below'
    ? 'bottom-full translate-y-1/2'
    : 'top-full -translate-y-1/2'
}

function refreshActiveTooltipPlacement(): void {
  if (!activeTooltipAnchor) return
  if (!activeTooltipAnchor.element.isConnected) {
    activeTooltipAnchor = null
    return
  }
  updateTooltipPlacement(activeTooltipAnchor.index, activeTooltipAnchor.element)
}

onMounted(() => {
  window.addEventListener('resize', refreshActiveTooltipPlacement)
  window.addEventListener('scroll', refreshActiveTooltipPlacement, {
    capture: true,
    passive: true
  })
})

onBeforeUnmount(() => {
  copyRequestSequence += 1
  clearCopiedResetTimer()
  window.removeEventListener('resize', refreshActiveTooltipPlacement)
  window.removeEventListener('scroll', refreshActiveTooltipPlacement, true)
  activeTooltipAnchor = null
  tooltipContentElements.clear()
})
</script>

<template>
  <div v-if="allEndpoints.length > 0" class="flex min-w-0 flex-wrap gap-2">
    <div
      v-for="(item, index) in allEndpoints"
      :key="`${item.endpoint}:${index}`"
      class="endpoint-chip flex w-full max-w-full min-w-0 flex-wrap items-center gap-1.5 rounded-control border border-line bg-surface px-2.5 py-1.5 text-xs transition-colors hover:border-line-strong sm:w-auto"
    >
      <span
        data-testid="endpoint-name"
        class="min-w-0 max-w-full break-words font-medium text-content-muted [overflow-wrap:anywhere]"
      >
        {{ item.name }}
      </span>
      <span
        v-if="item.isDefault"
        class="flex-shrink-0 rounded bg-brand-soft px-1 py-px text-[10px] font-medium leading-tight text-brand"
      >{{ t('keys.endpoints.default') }}</span>

      <span class="hidden flex-shrink-0 text-line-strong sm:inline">|</span>

      <div
        data-testid="endpoint-actions"
        class="flex w-full min-w-0 items-start gap-1 sm:w-auto sm:flex-1"
      >
        <div
          data-testid="endpoint-copy-region"
          class="group/copy relative flex min-w-0 flex-1 items-start gap-1"
          @mouseenter="positionTooltip(index, $event)"
          @focusin="positionTooltip(index, $event)"
        >
          <code
            data-testid="endpoint-url"
            class="min-w-0 flex-1 select-all whitespace-normal break-all py-1 font-mono leading-5 text-content-muted [overflow-wrap:anywhere]"
          >{{ item.endpoint }}</code>

          <button
            type="button"
            data-testid="endpoint-copy-button"
            class="endpoint-action inline-flex min-h-11 min-w-11 flex-shrink-0 items-center justify-center rounded-lg transition-colors"
            :class="copiedEndpoint === item.endpoint
              ? 'text-positive'
              : 'text-content-subtle hover:bg-surface-hover hover:text-brand'"
            :aria-label="tooltipHint(item.endpoint)"
            :aria-describedby="isTooltipScrollable(index)
              ? endpointTooltipDescriptionId(index)
              : endpointTooltipId(index)"
            @click="copy(item.endpoint)"
          >
            <svg v-if="copiedEndpoint === item.endpoint" class="h-3 w-3" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2.2">
              <path stroke-linecap="round" stroke-linejoin="round" d="M5 13l4 4L19 7" />
            </svg>
            <svg v-else class="h-3 w-3" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2">
              <path stroke-linecap="round" stroke-linejoin="round" d="M8 16H6a2 2 0 01-2-2V6a2 2 0 012-2h8a2 2 0 012 2v2m-6 12h8a2 2 0 002-2v-8a2 2 0 00-2-2h-8a2 2 0 00-2 2v8a2 2 0 002 2z" />
            </svg>
          </button>

          <div
            data-testid="endpoint-tooltip"
            class="pointer-events-none absolute z-20 max-w-none translate-y-1 text-left opacity-0 transition-all duration-150 group-hover/copy:pointer-events-auto group-hover/copy:translate-y-0 group-hover/copy:opacity-100 group-focus-within/copy:pointer-events-auto group-focus-within/copy:translate-y-0 group-focus-within/copy:opacity-100"
            :class="tooltipPositionClass(index)"
            :style="tooltipStyle(index)"
          >
            <div
              :id="endpointTooltipId(index)"
              :ref="(element) => setTooltipContentElement(index, element)"
              data-testid="endpoint-tooltip-content"
              :role="isTooltipScrollable(index) ? 'region' : 'tooltip'"
              :aria-label="isTooltipScrollable(index) ? item.name : undefined"
              :tabindex="isTooltipScrollable(index) ? 0 : undefined"
              class="overflow-y-auto rounded-panel border border-line bg-surface-elevated px-3 py-2.5 shadow-elevated focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary-500/60"
              :style="tooltipContentStyle(index)"
            >
              <div
                :id="endpointTooltipDescriptionId(index)"
                data-testid="endpoint-tooltip-description"
              >
                <p
                  v-if="item.description"
                  class="break-words text-xs leading-5 text-content-muted [overflow-wrap:anywhere]"
                >
                  {{ item.description }}
                </p>
                <p
                  class="flex items-center gap-1.5 text-[11px] leading-4 text-brand"
                  :class="item.description ? 'mt-1.5' : ''"
                >
                  <span class="h-1.5 w-1.5 rounded-full bg-brand"></span>
                  {{ tooltipHint(item.endpoint) }}
                </p>
              </div>
            </div>
            <div
              class="pointer-events-none absolute h-3 w-3 -translate-x-1/2 rotate-45 border border-line bg-surface-elevated"
              :class="tooltipArrowPositionClass(index)"
              :style="tooltipArrowStyle(index)"
            ></div>
          </div>
        </div>

        <a
          :href="speedTestUrl(item.endpoint)"
          target="_blank"
          rel="noopener noreferrer"
          class="endpoint-action inline-flex min-h-11 min-w-11 flex-shrink-0 items-center justify-center rounded-lg text-content-subtle transition-colors hover:bg-surface-hover hover:text-warning"
          :title="t('keys.endpoints.speedTest')"
          :aria-label="t('keys.endpoints.speedTest')"
        >
          <svg class="h-3 w-3" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2">
            <path stroke-linecap="round" stroke-linejoin="round" d="M13 10V3L4 14h7v7l9-11h-7z" />
          </svg>
        </a>
      </div>
    </div>
  </div>
</template>
