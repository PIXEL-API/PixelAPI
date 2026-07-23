import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

import { describe, expect, it } from 'vitest'

const componentPath = resolve(dirname(fileURLToPath(import.meta.url)), '../TablePageLayout.vue')
const componentSource = readFileSync(componentPath, 'utf8')

describe('TablePageLayout responsive behavior', () => {
  it('uses CSS media queries instead of a JavaScript resize breakpoint', () => {
    expect(componentSource).not.toContain('mobile-mode')
    expect(componentSource).not.toContain("addEventListener('resize'")
    expect(componentSource).toContain('@media (max-width: 1023px)')
  })

  it('only constrains the page height on sufficiently tall desktop viewports', () => {
    expect(componentSource).toContain('@media (min-width: 1024px) and (min-height: 720px)')
    expect(componentSource).toContain('height: calc(100dvh - 64px - 4rem);')
    expect(componentSource).toContain('@media (min-width: 1024px) and (max-height: 719px)')
    expect(componentSource).not.toContain('max-height: min(60dvh, 36rem);')
  })
})
