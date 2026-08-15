import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { describe, expect, it } from 'vitest'

const flowSource = readFileSync(
  resolve(process.cwd(), 'src/components/account/OAuthAuthorizationFlow.vue'),
  'utf8'
)
const modalSource = readFileSync(
  resolve(process.cwd(), 'src/components/account/CreateAccountModal.vue'),
  'utf8'
)

describe('admin Codex import entry points', () => {
  it('adds dedicated Codex Session and Agent Identity choices without replacing existing methods', () => {
    for (const method of [
      'manual',
      'refresh_token',
      'mobile_refresh_token',
      'access_token',
      'codex_session',
      'agent_identity',
      'codex_pat',
      'sso_cookie',
      'email_password'
    ]) {
      expect(flowSource).toContain(`value="${method}"`)
    }
    expect(flowSource).toContain("'import-codex-session': [content: string]")
  })

  it('keeps both new choices touch-friendly and admin/OpenAI-only', () => {
    expect(flowSource).toContain('v-if="showCodexSessionImportOption"')
    expect(flowSource).toContain('v-if="showAgentIdentityOption"')
    expect(flowSource).toContain('class="flex min-h-11 cursor-pointer items-center gap-2 py-2"')
    expect(flowSource).toContain('class="btn btn-primary min-h-11 w-full"')
    expect(modalSource).toContain(
      ':show-codex-session-import-option="!isUserScope && form.platform === \'openai\'"'
    )
    expect(modalSource).toContain(
      ':show-agent-identity-option="!isUserScope && form.platform === \'openai\'"'
    )
  })

  it('uses the dedicated API with upstream upsert enabled', () => {
    expect(modalSource).toContain('adminAPI.accounts.importCodexSession({')
    expect(modalSource).toContain('update_existing: true')
    expect(modalSource).toContain("oauthFlowRef.value?.inputMethod === 'agent_identity'")
  })
})
