import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

import { describe, expect, it } from 'vitest'

const componentPath = resolve(dirname(fileURLToPath(import.meta.url)), '../AppHeader.vue')
const componentSource = readFileSync(componentPath, 'utf8')

describe('AppHeader responsive text constraints', () => {
  it('lets the page title shrink without wrapping into a vertical column', () => {
    expect(componentSource).toContain('flex min-w-0 flex-1 items-center')
    expect(componentSource).toContain('truncate text-base font-semibold')
    expect(componentSource).toContain('truncate text-xs text-content-subtle')
  })

  it('bounds and truncates long user names at tablet and desktop widths', () => {
    expect(componentSource).toContain('hidden min-w-0 max-w-32 text-left md:block xl:max-w-48')
    expect(componentSource).toContain('truncate text-sm font-medium text-content')
    expect(componentSource).toContain('header-user-button min-w-0')
  })
})
