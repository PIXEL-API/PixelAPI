import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

import { describe, expect, it } from 'vitest'

const componentPath = resolve(dirname(fileURLToPath(import.meta.url)), '../AppHeader.vue')
const componentSource = readFileSync(componentPath, 'utf8')
const announcementBellPath = resolve(
  dirname(fileURLToPath(import.meta.url)),
  '../../common/AnnouncementBell.vue'
)
const announcementBellSource = readFileSync(announcementBellPath, 'utf8')
const localeSwitcherPath = resolve(
  dirname(fileURLToPath(import.meta.url)),
  '../../common/LocaleSwitcher.vue'
)
const localeSwitcherSource = readFileSync(localeSwitcherPath, 'utf8')

describe('AppHeader responsive text constraints', () => {
  it('renders the one-click invite link beside announcements', () => {
    expect(componentSource).toContain('<AnnouncementBell v-if="user" />')
    expect(componentSource).toContain('<HeaderInviteLink v-if="user" />')
  })

  it('keeps the mobile menu and primary actions from shrinking or overlapping', () => {
    expect(componentSource).toContain(
      'flex flex-none items-center gap-2 lg:min-w-0 lg:flex-1 lg:gap-4'
    )
    expect(componentSource).toContain(
      'flex min-w-0 flex-1 flex-nowrap items-center justify-end gap-1 sm:flex-none sm:gap-2'
    )
    expect(componentSource).toContain('header-icon-button flex-none lg:hidden')
    expect(announcementBellSource).toContain('h-11 w-11 flex-none')
    expect(localeSwitcherSource).toContain('min-h-11 min-w-11 flex-none')
  })

  it('moves secondary mobile actions out of the crowded header row', () => {
    expect(componentSource).toContain('class="header-action hidden sm:inline-flex"')
    expect(componentSource).toContain('class="hidden flex-none sm:block"')
    expect(componentSource).toContain('class="dropdown-item sm:hidden"')
  })

  it('lets the page title shrink without wrapping into a vertical column', () => {
    expect(componentSource).toContain('lg:min-w-0 lg:flex-1')
    expect(componentSource).toContain('truncate text-base font-semibold')
    expect(componentSource).toContain('truncate text-xs text-content-subtle')
  })

  it('only expands long user names when the desktop header has enough room', () => {
    expect(componentSource).toContain('hidden min-w-0 max-w-48 text-left xl:block')
    expect(componentSource).toContain('truncate text-sm font-medium text-content')
    expect(componentSource).toContain('header-user-button min-w-0')
    expect(componentSource).toContain(
      'class="hidden flex-shrink-0 text-gray-400 xl:block"'
    )
  })
})
