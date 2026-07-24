import { mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'
import UserAccountActionMenu from '../UserAccountActionMenu.vue'
import type { Account } from '@/types'

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({ t: (key: string) => key })
  }
})

function makeAgentIdentityAccount(): Account {
  return {
    id: 1,
    name: 'agent-identity',
    platform: 'openai',
    account_level: 'unknown',
    type: 'oauth',
    credentials: { auth_mode: 'agentIdentity' },
    proxy_id: null,
    concurrency: 3,
    priority: 50,
    status: 'active',
    error_message: null,
    error_since: null,
    last_used_at: null,
    expires_at: null,
    auto_pause_on_expired: false,
    created_at: '2026-01-01T00:00:00Z',
    updated_at: '2026-01-01T00:00:00Z',
    schedulable: true,
    rate_limited_at: null,
    rate_limit_reset_at: null,
    overload_until: null,
    temp_unschedulable_until: null,
    temp_unschedulable_reason: null,
    session_window_start: null,
    session_window_end: null,
    session_window_status: null
  }
}

describe('UserAccountActionMenu Agent Identity', () => {
  it('hides credential actions but keeps the external placement conversion entry', async () => {
    const wrapper = mount(UserAccountActionMenu, {
      props: {
        show: true,
        account: makeAgentIdentityAccount(),
        position: { top: 100, left: 100 }
      },
      attachTo: document.body
    })

    const text = document.body.textContent ?? ''
    expect(text).not.toContain('admin.accounts.reAuthorize')
    expect(text).not.toContain('admin.accounts.refreshToken')
    expect(text).not.toContain('admin.accounts.setPrivacy')
    expect(text).not.toContain('userAccounts.verifyPlus')
    expect(text).toContain('userAccounts.externalPlacement.action')

    const placementButton = Array.from(document.body.querySelectorAll('button')).find(button =>
      button.textContent?.includes('userAccounts.externalPlacement.action')
    )
    expect(placementButton).toBeDefined()
    placementButton?.click()
    expect(wrapper.emitted('external-placement')?.[0]).toEqual([
      expect.objectContaining({ id: 1 })
    ])
    wrapper.unmount()
  })
})
