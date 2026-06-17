import { describe, expect, it } from 'vitest'
import en from '@/i18n/locales/en'
import zh from '@/i18n/locales/zh'

describe('user account proxy i18n messages', () => {
  it.each([
    ['zh', zh.userAccounts.proxySmartPlaceholder],
    ['en', en.userAccounts.proxySmartPlaceholder]
  ] as const)('escapes literal at-signs in proxy smart placeholder for %s', (_locale, placeholder) => {
    expect(placeholder).toContain('@')
    expect(placeholder).toContain("{'@'}")
    expect(placeholder).not.toMatch(/[^']@[^']/)
  })
})
