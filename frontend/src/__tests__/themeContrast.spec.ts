import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

import { describe, expect, it } from 'vitest'

const stylePath = resolve(dirname(fileURLToPath(import.meta.url)), '../style.css')
const styleSource = readFileSync(stylePath, 'utf8')

const readRule = (selector: string): string => {
  const escapedSelector = selector.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')
  const declarations = styleSource.match(new RegExp(`${escapedSelector}\\s*\\{([^}]*)\\}`))?.[1]
  if (!declarations) throw new Error(`Missing CSS rule: ${selector}`)
  return declarations
}

const readLightThemeColor = (token: string): [number, number, number] => {
  const rootBlock = styleSource.match(/:root\s*\{([\s\S]*?)\n\s*\}/)?.[1]
  const match = rootBlock?.match(new RegExp(`--ui-${token}:\\s*(\\d+)\\s+(\\d+)\\s+(\\d+);`))

  if (!match) {
    throw new Error(`Missing light theme color token: --ui-${token}`)
  }

  return [Number(match[1]), Number(match[2]), Number(match[3])]
}

const relativeLuminance = ([red, green, blue]: [number, number, number]): number => {
  const [r, g, b] = [red, green, blue].map((channel) => {
    const normalized = channel / 255
    return normalized <= 0.04045
      ? normalized / 12.92
      : ((normalized + 0.055) / 1.055) ** 2.4
  })

  return 0.2126 * r + 0.7152 * g + 0.0722 * b
}

const contrastRatio = (
  foreground: [number, number, number],
  background: [number, number, number]
): number => {
  const foregroundLuminance = relativeLuminance(foreground)
  const backgroundLuminance = relativeLuminance(background)
  const lighter = Math.max(foregroundLuminance, backgroundLuminance)
  const darker = Math.min(foregroundLuminance, backgroundLuminance)

  return (lighter + 0.05) / (darker + 0.05)
}

describe('light theme semantic text colors', () => {
  it.each(['positive', 'warning', 'danger'] as const)(
    'keeps %s text at WCAG AA contrast on the primary surface',
    (token) => {
      expect(contrastRatio(readLightThemeColor(token), readLightThemeColor('surface'))).toBeGreaterThanOrEqual(4.5)
    }
  )
})

describe('bounded v2 primitives', () => {
  it('keeps legacy defaults and applies the new primitives only inside the v2 marker', () => {
    expect(readRule('body')).toContain(
      '@apply bg-gray-50 text-gray-900 dark:bg-dark-950 dark:text-gray-100;'
    )
    expect(readRule('.btn')).toContain('@apply rounded-xl px-4 py-2.5 text-sm font-medium;')
    expect(readRule('.btn')).not.toContain('rounded-control')
    expect(readRule("[data-ui-skin='v2'] .btn")).toContain(
      '@apply min-h-10 rounded-control px-4 py-2 text-sm font-semibold;'
    )
  })
})
