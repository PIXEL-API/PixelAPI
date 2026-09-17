/**
 * Vitest 测试环境设置
 * 提供全局 mock 和测试工具
 */
import { config } from '@vue/test-utils'
import { vi } from 'vitest'

// Mock requestIdleCallback (Safari < 15 不支持)
if (typeof globalThis.requestIdleCallback === 'undefined') {
  globalThis.requestIdleCallback = ((callback: IdleRequestCallback) => {
    return window.setTimeout(() => callback({ didTimeout: false, timeRemaining: () => 50 }), 1)
  }) as unknown as typeof requestIdleCallback
}

if (typeof globalThis.cancelIdleCallback === 'undefined') {
  globalThis.cancelIdleCallback = ((id: number) => {
    window.clearTimeout(id)
  }) as unknown as typeof cancelIdleCallback
}

// Mock IntersectionObserver
class MockIntersectionObserver {
  observe = vi.fn()
  disconnect = vi.fn()
  unobserve = vi.fn()
}

globalThis.IntersectionObserver = MockIntersectionObserver as unknown as typeof IntersectionObserver

// Mock ResizeObserver
class MockResizeObserver {
  observe = vi.fn()
  disconnect = vi.fn()
  unobserve = vi.fn()
}

globalThis.ResizeObserver = MockResizeObserver as unknown as typeof ResizeObserver

// Vue Test Utils 全局配置
config.global.stubs = {
  // 可以在这里添加全局 stub
}

// Teleport 目标：BaseDialog/ModalShell 等弹层组件统一挂载到 #dialog-root。
// 生产环境由 index.html 提供；测试环境缺失会导致 Teleport 目标为 null 并引发更新崩溃。
if (typeof document !== 'undefined' && !document.getElementById('dialog-root')) {
  const dialogRoot = document.createElement('div')
  dialogRoot.id = 'dialog-root'
  dialogRoot.className = 'notranslate'
  dialogRoot.translate = false
  document.body.appendChild(dialogRoot)
}

// 设置全局测试超时
vi.setConfig({ testTimeout: 10000 })
