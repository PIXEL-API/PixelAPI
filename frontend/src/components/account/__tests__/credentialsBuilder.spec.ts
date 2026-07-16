import { describe, it, expect } from 'vitest'
import { applyInterceptWarmup, applyPlanType, readPlanType } from '../credentialsBuilder'

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
