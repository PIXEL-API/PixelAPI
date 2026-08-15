import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { describe, expect, it } from 'vitest'

const source = readFileSync(
  resolve(process.cwd(), 'src/components/account/OAuthAuthorizationFlow.vue'),
  'utf8'
)

describe('OAuthAuthorizationFlow Grok password form', () => {
  it('keeps email and password in separate accessible inputs', () => {
    expect(source).toContain('for="grok-password-email"')
    expect(source).toContain('id="grok-password-email"')
    expect(source).toContain('type="email"')
    expect(source).toContain('autocomplete="username"')
    expect(source).toContain('for="grok-password-value"')
    expect(source).toContain('id="grok-password-value"')
    expect(source).toContain('type="password"')
    expect(source).toContain('autocomplete="current-password"')
  })

  it('uses touch-friendly controls and focuses the form when selected', () => {
    expect(source).toContain('class="input min-h-11 w-full"')
    expect(source).toContain('class="btn btn-primary mt-4 min-h-11 w-full"')
    expect(source).toContain("newVal === 'email_password'")
    expect(source).toContain('passwordEmailInputRef.value?.focus()')
  })

  it('clears the password on reset and emits it only for the one-shot request', () => {
    expect(source).toContain("'authorize-password': [credentials: { email: string; password: string }]")
    expect(source).toContain("emit('authorize-password', { email, password: passwordInput.value })")
    expect(source).toContain("passwordInput.value = ''")
  })
})
