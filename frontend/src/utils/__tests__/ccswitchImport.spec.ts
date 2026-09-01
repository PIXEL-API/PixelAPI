import { describe, expect, it } from 'vitest'
import {
  buildCcSwitchImportDeeplink,
  GROK_CC_SWITCH_MODEL,
  OPENAI_CC_SWITCH_CODEX_MODEL,
  OPENAI_CC_SWITCH_REASONING_EFFORT,
  resolveCcSwitchImportConfig
} from '../ccswitchImport'

function decodeBase64Utf8(value: string): string {
  const binary = atob(value)
  const bytes = Uint8Array.from(binary, (character) => character.charCodeAt(0))
  return new TextDecoder().decode(bytes)
}

describe('resolveCcSwitchImportConfig', () => {
  it.each([
    ['https://gateway.example.com', 'https://gateway.example.com/v1'],
    ['https://gateway.example.com/', 'https://gateway.example.com/v1'],
    ['https://gateway.example.com/v1', 'https://gateway.example.com/v1'],
    ['https://gateway.example.com/v1/', 'https://gateway.example.com/v1'],
    ['https://gateway.example.com/v1/v1', 'https://gateway.example.com/v1']
  ])('normalizes Grok endpoint %s to exactly one /v1', (baseUrl, endpoint) => {
    expect(resolveCcSwitchImportConfig('grok', 'claude', baseUrl)).toEqual({
      app: 'grokbuild',
      endpoint,
      model: GROK_CC_SWITCH_MODEL
    })
  })
})

describe('buildCcSwitchImportDeeplink', () => {
  it('uses the recommended GPT-5.6 migration defaults', () => {
    expect(OPENAI_CC_SWITCH_CODEX_MODEL).toBe('gpt-5.6-terra')
    expect(OPENAI_CC_SWITCH_REASONING_EFFORT).toBe('medium')
  })

  it('generates an OpenAI Codex import from the shared model configuration', () => {
    const apiKey = 'sk-test'
    const deeplink = buildCcSwitchImportDeeplink({
      baseUrl: 'https://gateway.example.com/v1',
      platform: 'openai',
      clientType: 'claude',
      providerName: 'Pixel API',
      apiKey,
      usageScript: 'return { usage: 1 }'
    })

    const searchParams = new URL(deeplink).searchParams
    expect(searchParams.get('app')).toBe('codex')
    expect(searchParams.get('model')).toBe(OPENAI_CC_SWITCH_CODEX_MODEL)

    const encodedConfig = searchParams.get('config')
    expect(encodedConfig).not.toBeNull()

    const importedConfig = JSON.parse(decodeBase64Utf8(encodedConfig!)) as {
      auth: Record<string, string>
      config: string
    }

    expect(importedConfig.auth).toEqual({ OPENAI_API_KEY: apiKey })
    expect(importedConfig.config).toContain(
      `model = ${JSON.stringify(OPENAI_CC_SWITCH_CODEX_MODEL)}`
    )
    expect(importedConfig.config).toContain(
      `model_reasoning_effort = ${JSON.stringify(OPENAI_CC_SWITCH_REASONING_EFFORT)}`
    )
    expect(importedConfig.config).toContain('wire_api = "responses"')
    expect(importedConfig.config).toContain('requires_openai_auth = true')
    expect(importedConfig.config).not.toContain('disable_response_storage')
    expect(importedConfig.config).not.toContain('env_key')
  })
})
