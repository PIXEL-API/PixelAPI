import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { describe, expect, it } from 'vitest'

const source = readFileSync(
  resolve(process.cwd(), 'src/components/account/CreateAccountModal.vue'),
  'utf8'
)

describe('CreateAccountModal Grok administrator boundary', () => {
  it('offers API Key and administrator upstream configuration only outside user scope', () => {
    expect(source).toContain('data-testid="grok-api-key-type"')
    expect(source).toContain('v-if="!isUserScope"')
    expect(source).toContain('data-testid="grok-admin-upstream-config"')
    expect(source).toContain(
      'v-if="!isUserScope && form.platform === \'grok\' && accountCategory === \'oauth-based\'"'
    )
  })

  it('strips sensitive Grok upstream fields in user scope before submission', () => {
    expect(source).toContain('delete credentials.base_url')
    expect(source).toContain('delete credentials.header_override_enabled')
    expect(source).toContain('delete credentials.header_overrides')
  })

  it('uses the official xAI API endpoint as the Grok API Key default', () => {
    expect(source).toContain("newPlatform === 'grok'")
    expect(source).toContain("? 'https://api.x.ai/v1'")
  })
})
