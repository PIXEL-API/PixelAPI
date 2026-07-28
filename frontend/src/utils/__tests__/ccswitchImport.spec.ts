import { describe, expect, it } from 'vitest'
import {
  GROK_CC_SWITCH_MODEL,
  resolveCcSwitchImportConfig
} from '../ccswitchImport'

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
