import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { describe, expect, it } from 'vitest'

const source = readFileSync(
  resolve(process.cwd(), 'src/components/admin/account/ReAuthAccountModal.vue'),
  'utf8'
)

describe('ReAuthAccountModal Grok secure reauthorization', () => {
  it('shows password login only when the backend capability is enabled', () => {
    expect(source).toContain(':show-email-password-option="isGrok && grokOAuth.passwordAuthEnabled.value"')
    expect(source).toContain('await grokOAuth.loadCapabilities()')
    expect(source).toContain('@authorize-password="handleGrokAuthorizePassword"')
  })

  it('supports SSO and refresh-token reauthorization without creating another account', () => {
    expect(source).toContain('@import-sso="handleGrokImportSSO"')
    expect(source).toContain('@validate-refresh-token="handleGrokValidateRefreshToken"')
    expect(source).toContain('applyGrokReauthorization')
    expect(source).not.toContain('createFromSSO')
  })

  it('removes historical raw password and SSO fields while preserving account settings', () => {
    expect(source).toContain("new Set(['password', 'sso', 'sso-rw', 'sso_token'])")
    expect(source).toContain('mergeSafeGrokAccountRecord')
  })
})
