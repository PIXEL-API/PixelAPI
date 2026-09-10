import { onScopeDispose, watch, type Ref } from 'vue'

interface RoomGridCapacityOptions {
  viewport: Ref<HTMLElement | null>
  grid: Ref<HTMLElement | null>
  enabled: Readonly<Ref<boolean>>
  onCapacityChange: (size: number) => void
}

const MEASUREMENT_DELAY_MS = 120
const MAX_PAGE_SIZE = 1000

export function useRoomGridCapacity({ viewport, grid, enabled, onCapacityChange }: RoomGridCapacityOptions): void {
  if (typeof window === 'undefined' || typeof ResizeObserver === 'undefined') return

  let timer: number | null = null
  let disposed = false
  let heightBaseline = ''
  let maximumCardHeight = 0
  let lastMeasurement = ''
  let lastCapacity = 0

  function cancelMeasurement(): void {
    if (timer !== null) window.clearTimeout(timer)
    timer = null
  }

  function measure(): void {
    timer = null
    const viewportElement = viewport.value
    const gridElement = grid.value
    if (disposed || !enabled.value || !viewportElement || !gridElement) return

    const width = viewportElement.clientWidth
    const height = viewportElement.clientHeight
    const gridWidth = gridElement.clientWidth
    if (width <= 0 || height <= 0 || gridWidth <= 0 || gridElement.clientHeight <= 0) return

    const style = window.getComputedStyle(gridElement)
    if (style.display === 'none' || style.visibility === 'hidden' || style.visibility === 'collapse') return
    // 浏览器将 repeat()/minmax() 展开为像素轨道；不推测尚未完成布局的 CSS 表达式。
    const tracks = style.gridTemplateColumns.replace(/\[[^\]]*\]/g, '').trim().split(/\s+/)
    if (tracks.length === 0 || tracks.some(track => !/^\d+(?:\.\d+)?px$/.test(track))) return
    const columns = tracks.filter(track => Number.parseFloat(track) > 0).length
    if (columns === 0) return

    let measuredCardHeight = 0
    for (const card of gridElement.querySelectorAll<HTMLElement>('.room-preview-card')) {
      measuredCardHeight = Math.max(measuredCardHeight, Math.ceil(card.getBoundingClientRect().height))
    }
    if (measuredCardHeight <= 0) return

    const fontSize = Number.parseFloat(style.fontSize)
    const rootFontSize = Number.parseFloat(window.getComputedStyle(document.documentElement).fontSize)
    if (!Number.isFinite(fontSize) || fontSize <= 0 || !Number.isFinite(rootFontSize) || rootFontSize <= 0) return
    const nextBaseline = `${width}:${gridWidth}:${fontSize}:${rootFontSize}`
    if (nextBaseline !== heightBaseline) {
      heightBaseline = nextBaseline
      maximumCardHeight = measuredCardHeight
    } else {
      // 同一宽度与字体尺度保留已见最高卡片，避免短页导致容量来回跳变。
      maximumCardHeight = Math.max(maximumCardHeight, measuredCardHeight)
    }

    const rowGap = Math.max(0, Number.parseFloat(style.rowGap) || 0)
    const measurement = `${nextBaseline}:${height}:${columns}:${rowGap}:${maximumCardHeight}`
    if (measurement === lastMeasurement) return
    lastMeasurement = measurement

    const rows = Math.max(1, Math.floor((height + rowGap) / (maximumCardHeight + rowGap)))
    const capacity = Math.min(MAX_PAGE_SIZE, Math.max(1, columns * rows))
    if (capacity === lastCapacity) return
    lastCapacity = capacity
    onCapacityChange(capacity)
  }

  function scheduleMeasurement(): void {
    cancelMeasurement()
    if (disposed || !enabled.value) return
    timer = window.setTimeout(measure, MEASUREMENT_DELAY_MS)
  }

  const observer = new ResizeObserver(scheduleMeasurement)
  watch([viewport, grid, enabled], ([viewportElement, gridElement, isEnabled]) => {
    observer.disconnect()
    cancelMeasurement()
    if (!isEnabled) return
    if (viewportElement) observer.observe(viewportElement)
    if (gridElement) observer.observe(gridElement)
    scheduleMeasurement()
  }, { immediate: true, flush: 'post' })

  window.addEventListener('resize', scheduleMeasurement)
  onScopeDispose(() => {
    disposed = true
    observer.disconnect()
    cancelMeasurement()
    window.removeEventListener('resize', scheduleMeasurement)
  })
}
