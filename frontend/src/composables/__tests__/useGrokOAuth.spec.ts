import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { describe, expect, it } from 'vitest'

const composableSource = readFileSync(
  resolve(process.cwd(), 'src/composables/useGrokOAuth.ts'),
  'utf8'
)
const apiSource = readFileSync(resolve(process.cwd(), 'src/api/admin/grok.ts'), 'utf8')

describe('useGrokOAuth password and SSO boundaries', () => {
  it('discovers password capability fail-closed and only for admin scope', () => {
    expect(composableSource).toContain("if (scope !== 'admin') return false")
    expect(composableSource).toContain('passwordAuthEnabled.value = false')
    expect(composableSource).toContain('capabilities.password_auth_enabled === true')
  })

  it('passes email and password as separate fields and never builds them into credentials', () => {
    expect(apiSource).toContain('const payload: Record<string, unknown> = { email, password }')
    expect(composableSource).toContain('access_token: tokenInfo.access_token')
    expect(composableSource).not.toContain('password: tokenInfo.password')
    expect(composableSource).not.toContain('sso_token: tokenInfo.sso_token')
  })

  it('uses bounded authorization endpoints for SSO and password', () => {
    expect(apiSource).toContain("'/admin/grok/oauth/sso-token'")
    expect(apiSource).toContain("'/admin/grok/oauth/password'")
    expect(apiSource).toContain('GROK_AUTHORIZATION_TIMEOUT_MS')
  })
})
