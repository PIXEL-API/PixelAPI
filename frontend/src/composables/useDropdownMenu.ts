import { nextTick, onBeforeUnmount, onMounted, ref, type Ref } from 'vue'

interface UseDropdownMenuOptions {
  /** 菜单打开状态 */
  open: Ref<boolean>
  /** 包裹触发器与菜单的容器（用于 click-outside 与菜单项查询） */
  container: Ref<HTMLElement | null>
  /** 触发器按钮（关闭时恢复焦点） */
  trigger: Ref<HTMLElement | null>
  /** 菜单项选择器，默认 [data-menu-item] */
  itemSelector?: string
}

/**
 * WAI-ARIA menu 键盘交互：ArrowUp/Down/Home/End 导航、Esc 关闭并恢复焦点、
 * Tab 关闭并让焦点自然移出、点击外部关闭、触发器 ArrowDown/ArrowUp 打开菜单。
 * 菜单项统一使用 tabindex="-1"（菜单不进入 Tab 序列，与 APG menu 模式一致）。
 */
export function useDropdownMenu(options: UseDropdownMenuOptions) {
  const { open, container, trigger } = options
  const itemSelector = options.itemSelector ?? '[data-menu-item]'

  const focusIndex = ref(0)

  const getItems = (): HTMLElement[] => {
    if (!container.value) return []
    return Array.from(
      container.value.querySelectorAll<HTMLElement>(itemSelector)
    ).filter((el) => !el.matches(':disabled'))
  }

  const focusItem = (index: number) => {
    const items = getItems()
    if (items.length === 0) return
    const normalizedIndex = (index + items.length) % items.length
    focusIndex.value = normalizedIndex
    items[normalizedIndex].focus()
  }

  const openMenu = (initialIndex = 0, moveFocus = true) => {
    if (open.value) {
      if (moveFocus) focusItem(initialIndex)
      return
    }
    open.value = true
    focusIndex.value = initialIndex
    if (moveFocus) {
      void nextTick(() => focusItem(initialIndex))
    }
  }

  const closeMenu = (restoreFocus = true) => {
    if (!open.value) return
    open.value = false
    if (restoreFocus) {
      void nextTick(() => trigger.value?.focus())
    }
  }

  /** 鼠标点击切换：默认不把焦点移入菜单（键盘走 onTriggerKeydown） */
  const toggleMenu = (initialIndex = 0, moveFocus = false) => {
    if (open.value) closeMenu(false)
    else openMenu(initialIndex, moveFocus)
  }

  /** 绑定到触发器按钮：ArrowDown/ArrowUp 打开并定位首/末项 */
  const onTriggerKeydown = (event: KeyboardEvent) => {
    if (event.key === 'ArrowDown') {
      event.preventDefault()
      openMenu(0)
    } else if (event.key === 'ArrowUp') {
      event.preventDefault()
      openMenu(-1)
    } else if (event.key === 'Escape' && open.value) {
      event.preventDefault()
      event.stopPropagation()
      closeMenu(true)
    }
  }

  /** 绑定到菜单容器 */
  const onMenuKeydown = (event: KeyboardEvent) => {
    if (event.key === 'Escape') {
      event.preventDefault()
      event.stopPropagation()
      closeMenu(true)
      return
    }

    // Tab：关闭菜单，焦点自然移出（不 preventDefault）
    if (event.key === 'Tab') {
      closeMenu(false)
      return
    }

    const items = getItems()
    if (items.length === 0) return
    const activeIndex = items.findIndex((item) => item === document.activeElement)
    const currentIndex = activeIndex >= 0 ? activeIndex : focusIndex.value
    let nextIndex: number | null = null

    if (event.key === 'ArrowDown') nextIndex = currentIndex + 1
    else if (event.key === 'ArrowUp') nextIndex = currentIndex - 1
    else if (event.key === 'Home') nextIndex = 0
    else if (event.key === 'End') nextIndex = items.length - 1

    if (nextIndex !== null) {
      event.preventDefault()
      focusItem(nextIndex)
    }
  }

  /** 焦点移出容器（含触发器）时关闭 */
  const onMenuFocusout = () => {
    void nextTick(() => {
      const activeElement = document.activeElement
      if (
        open.value &&
        activeElement instanceof Node &&
        !container.value?.contains(activeElement)
      ) {
        closeMenu(false)
      }
    })
  }

  const onDocumentClick = (event: MouseEvent) => {
    if (container.value && !container.value.contains(event.target as Node)) {
      closeMenu(false)
    }
  }

  onMounted(() => {
    document.addEventListener('click', onDocumentClick)
  })

  onBeforeUnmount(() => {
    document.removeEventListener('click', onDocumentClick)
  })

  return {
    focusIndex,
    openMenu,
    closeMenu,
    toggleMenu,
    onTriggerKeydown,
    onMenuKeydown,
    onMenuFocusout
  }
}
