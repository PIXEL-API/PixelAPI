import { describe, expect, it } from 'vitest'

import router from '@/router'
import { resolveUiSkin } from '@/composables/useUiSkin'

describe('bounded UI skin routing', () => {
  it('defaults unknown and unspecified values to the legacy skin', () => {
    expect(resolveUiSkin(undefined)).toBe('legacy')
    expect(resolveUiSkin('legacy')).toBe('legacy')
    expect(resolveUiSkin('future-skin')).toBe('legacy')
    expect(resolveUiSkin('v2')).toBe('v2')
  })

  it('opts in only the migrated user routes', () => {
    const v2Paths = router
      .getRoutes()
      .filter((route) => route.meta.uiSkin === 'v2')
      .map((route) => route.path)
      .sort()

    expect(v2Paths).toEqual(['/activities', '/admin/activities', '/dashboard', '/keys', '/usage'])
    expect(router.resolve('/profile').meta.uiSkin).toBeUndefined()
    expect(router.resolve('/admin/usage').meta.uiSkin).toBeUndefined()
    expect(router.resolve('/admin/activities').meta.uiSkin).toBe('v2')
    expect(router.resolve('/key-usage').meta.uiSkin).toBeUndefined()
    expect(router.resolve('/login').meta.uiSkin).toBeUndefined()
  })
})
