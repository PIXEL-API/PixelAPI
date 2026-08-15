import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { describe, expect, it } from 'vitest'

const source = readFileSync(
  resolve(process.cwd(), 'src/components/account/CreateAccountModal.vue'),
  'utf8'
)
const userImportSource = readFileSync(
  resolve(process.cwd(), 'src/components/user/ImportAccountsModal.vue'),
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

  it('requires Free or Heavy and submits the selected Grok account level', () => {
    expect(source).toContain("GROK_ACCOUNT_LEVEL_OPTIONS")
    expect(source).toContain("form.platform === 'grok'")
    expect(source).toContain("platform === 'openai' || platform === 'grok' ? form.account_level : 'unknown'")
  })

  it('requires the Grok account level for credential imports', () => {
    expect(userImportSource).toContain("selectedPlatform === 'grok'")
    expect(userImportSource).toContain("request.account_level = accountLevel")
    expect(userImportSource).toContain("importGrokAccountLevelRequired")
  })

  it('shows password authorization only for admin Grok OAuth when capability is enabled', () => {
    expect(source).toContain(
      ':show-email-password-option="!isUserScope && form.platform === \'grok\' && grokOAuth.passwordAuthEnabled.value"'
    )
    expect(source).toContain('await grokOAuth.loadCapabilities()')
    expect(source).toContain('@authorize-password="handleGrokAuthorizePassword"')
    expect(source).toContain('grokOAuth.buildCredentials(tokenInfo)')
  })
})
