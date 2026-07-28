import { describe, it, expect } from 'vitest'
import {
  applyInterceptWarmup,
  applyPlanType,
  buildHeaderOverridesObject,
  isCustomGrokBaseUrl,
  parseHeaderOverridesJson,
  readPlanType,
  serializeHeaderOverrideRows,
  splitHeaderOverridesObject,
  validateHeaderOverrideRows
} from '../credentialsBuilder'

describe('applyInterceptWarmup', () => {
  it('create + enabled=true: should set intercept_warmup_requests to true', () => {
    const creds: Record<string, unknown> = { access_token: 'tok' }
    applyInterceptWarmup(creds, true, 'create')
    expect(creds.intercept_warmup_requests).toBe(true)
  })

  it('create + enabled=false: should not add the field', () => {
    const creds: Record<string, unknown> = { access_token: 'tok' }
    applyInterceptWarmup(creds, false, 'create')
    expect('intercept_warmup_requests' in creds).toBe(false)
  })

  it('edit + enabled=true: should set intercept_warmup_requests to true', () => {
    const creds: Record<string, unknown> = { api_key: 'sk' }
    applyInterceptWarmup(creds, true, 'edit')
    expect(creds.intercept_warmup_requests).toBe(true)
  })

  it('edit + enabled=false + field exists: should delete the field', () => {
    const creds: Record<string, unknown> = { api_key: 'sk', intercept_warmup_requests: true }
    applyInterceptWarmup(creds, false, 'edit')
    expect('intercept_warmup_requests' in creds).toBe(false)
  })

  it('edit + enabled=false + field absent: should not throw', () => {
    const creds: Record<string, unknown> = { api_key: 'sk' }
    applyInterceptWarmup(creds, false, 'edit')
    expect('intercept_warmup_requests' in creds).toBe(false)
  })

  it('should not affect other fields', () => {
    const creds: Record<string, unknown> = {
      api_key: 'sk',
      base_url: 'url',
      intercept_warmup_requests: true
    }
    applyInterceptWarmup(creds, false, 'edit')
    expect(creds.api_key).toBe('sk')
    expect(creds.base_url).toBe('url')
    expect('intercept_warmup_requests' in creds).toBe(false)
  })
})

describe('OpenAI plan_type override helpers', () => {
  it('reads only trimmed string values', () => {
    expect(readPlanType({ plan_type: '  self_serve_business  ' })).toBe('self_serve_business')
    expect(readPlanType({ plan_type: 42 })).toBe('')
    expect(readPlanType(undefined)).toBe('')
  })

  it('writes arbitrary non-empty values without changing other credentials', () => {
    const credentials: Record<string, unknown> = {
      access_token: 'token',
      model_mapping: { source: 'target' }
    }

    applyPlanType(credentials, '  team_custom  ')

    expect(credentials).toEqual({
      access_token: 'token',
      model_mapping: { source: 'target' },
      plan_type: 'team_custom'
    })
  })

  it('removes only plan_type when the override is cleared', () => {
    const credentials: Record<string, unknown> = {
      access_token: 'token',
      plan_type: 'plus'
    }

    applyPlanType(credentials, '   ')

    expect(credentials).toEqual({ access_token: 'token' })
  })
})

describe('Grok header override helpers', () => {
  it('normalizes header names and round-trips JSON', () => {
    const rows = [
      { name: ' User-Agent ', value: ' grok-build ' },
      { name: 'X-Grok-Client-Version', value: '1.2.3' }
    ]

    expect(buildHeaderOverridesObject(rows)).toEqual({
      'user-agent': 'grok-build',
      'x-grok-client-version': '1.2.3'
    })
    expect(splitHeaderOverridesObject(buildHeaderOverridesObject(rows))).toEqual([
      { name: 'user-agent', value: 'grok-build' },
      { name: 'x-grok-client-version', value: '1.2.3' }
    ])
    expect(parseHeaderOverridesJson(serializeHeaderOverrideRows(rows))).toEqual([
      { name: 'User-Agent', value: 'grok-build' },
      { name: 'X-Grok-Client-Version', value: '1.2.3' }
    ])
  })

  it('rejects blocked, duplicate and malformed header rows', () => {
    expect(validateHeaderOverrideRows([{ name: 'Authorization', value: 'secret' }])).toBe(
      'blockedName'
    )
    expect(
      validateHeaderOverrideRows([
        { name: 'X-Trace', value: 'one' },
        { name: 'x-trace', value: 'two' }
      ])
    ).toBe('duplicateName')
    expect(validateHeaderOverrideRows([{ name: 'bad name', value: 'value' }])).toBe(
      'invalidName'
    )
    expect(validateHeaderOverrideRows([{ name: 'x-trace', value: 'line\nbreak' }])).toBe(
      'invalidValue'
    )
  })

  it('rejects invalid JSON shapes and accepts flat primitive values', () => {
    expect(parseHeaderOverridesJson('[]')).toBeNull()
    expect(parseHeaderOverridesJson('{"x-test":{"nested":true}}')).toBeNull()
    expect(parseHeaderOverridesJson('{"x-number":42,"x-enabled":true}')).toEqual([
      { name: 'x-enabled', value: 'true' },
      { name: 'x-number', value: '42' }
    ])
  })
})

describe('isCustomGrokBaseUrl', () => {
  it('only treats the default Grok CLI gateway host as non-custom', () => {
    expect(isCustomGrokBaseUrl('https://cli-chat-proxy.grok.com/v1')).toBe(false)
    expect(isCustomGrokBaseUrl('HTTPS://CLI-CHAT-PROXY.GROK.COM:443/')).toBe(false)
    expect(isCustomGrokBaseUrl('https://api.x.ai/v1')).toBe(true)
    expect(isCustomGrokBaseUrl('https://relay.example.com/v1')).toBe(true)
  })

  it('rejects empty and malformed values', () => {
    expect(isCustomGrokBaseUrl('')).toBe(false)
    expect(isCustomGrokBaseUrl('not a url')).toBe(false)
    expect(isCustomGrokBaseUrl(undefined)).toBe(false)
  })
})
