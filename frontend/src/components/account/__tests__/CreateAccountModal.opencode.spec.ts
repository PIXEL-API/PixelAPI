import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { describe, expect, it } from 'vitest'

const source = readFileSync(
  resolve(process.cwd(), 'src/components/account/CreateAccountModal.vue'),
  'utf8'
)

describe('CreateAccountModal Opencode boundary', () => {
  it('offers an opencode platform option', () => {
    expect(source).toContain('form.platform = \'opencode\'')
  })

  it('renders only an API key field for opencode, locking the endpoint', () => {
    expect(source).toContain('v-if="form.platform === \'opencode\'"')
    expect(source).toContain('opencode-go-api-key...')
    expect(source).toContain('t(\'admin.accounts.opencode.apiKeyHint\')')
    // opencode 不计入通用 apikey 块（该块含 base_url 输入框），避免暴露 base_url。
    expect(source).toContain("form.platform !== 'opencode'")
  })

  it('submits api_key only, base_url locked by backend', () => {
    // 前端不传 base_url：后端 GetOpencodeBaseURL 锁定官方地址，且凭证校验禁止 base_url 字段。
    expect(source).toContain('api_key: apiKeyValue.value.trim()')
    expect(source).not.toContain('base_url: OPENCODE_DEFAULT_BASE_URL')
  })

  it('submits opencode as an apikey account and requires a name', () => {
    expect(source).toContain('createAccountAndFinish(\'opencode\', \'apikey\', credentials)')
    expect(source).toContain('if (!form.name.trim())')
  })

  it('allows user-scope creation for opencode without an OAuth flow', () => {
    // 仅 opencode 豁免用户端强制 OAuth 的拦截。
    expect(source).toContain("form.platform !== 'opencode'")
  })
})
