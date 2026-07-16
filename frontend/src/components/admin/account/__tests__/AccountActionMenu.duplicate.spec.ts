import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import AccountActionMenu from '../AccountActionMenu.vue'
import type { Account } from '@/types'

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({ t: (key: string) => key })
  }
})

function makeAccount(overrides: Partial<Account>): Account {
  return {
    id: 1,
    name: 'test-account',
    platform: 'anthropic',
    account_level: 'unknown',
    type: 'apikey',
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
    session_window_status: null,
    ...overrides
  }
}

const position = { top: 100, left: 100 }
const bodyText = () => document.body.textContent ?? ''

describe('AccountActionMenu account duplication', () => {
	it('hides OAuth maintenance and privacy actions for Agent Identity', () => {
		const wrapper = mount(AccountActionMenu, {
			props: {
				show: true,
				account: makeAccount({
					platform: 'openai',
					type: 'oauth',
					credentials: { auth_mode: 'agentIdentity' }
				}),
				position
			},
			attachTo: document.body
		})

		expect(bodyText()).not.toContain('admin.accounts.reAuthorize')
		expect(bodyText()).not.toContain('admin.accounts.refreshToken')
		expect(bodyText()).not.toContain('admin.accounts.setPrivacy')
		wrapper.unmount()
	})

  it.each(['apikey', 'upstream', 'bedrock', 'service_account'] as const)('shows duplication for %s accounts', (type) => {
    const wrapper = mount(AccountActionMenu, {
      props: { show: true, account: makeAccount({ type }), position },
      attachTo: document.body
    })
    expect(bodyText()).toContain('admin.accounts.duplicateAccount')
    wrapper.unmount()
  })

  it.each([
    { type: 'oauth' as const },
    { type: 'setup-token' as const },
    { type: 'apikey' as const, owner_user_id: 7 },
    { type: 'apikey' as const, share_mode: 'public' },
    { type: 'apikey' as const, share_policy_id: 8 },
    { type: 'apikey' as const, account_share_mode_listing_id: 9 }
  ])('hides duplication for rotating or shared accounts %#', (overrides) => {
    const wrapper = mount(AccountActionMenu, {
      props: { show: true, account: makeAccount(overrides), position },
      attachTo: document.body
    })
    expect(bodyText()).not.toContain('admin.accounts.duplicateAccount')
    wrapper.unmount()
  })

  it('emits the selected account', async () => {
    const account = makeAccount({})
    const wrapper = mount(AccountActionMenu, {
      props: { show: true, account, position },
      attachTo: document.body
    })
    const button = Array.from(document.body.querySelectorAll('button')).find((item) =>
      item.textContent?.includes('admin.accounts.duplicateAccount')
    )
    expect(button).toBeDefined()
    button!.click()
    await wrapper.vm.$nextTick()
    expect(wrapper.emitted('duplicate')?.[0]?.[0]).toMatchObject({ id: account.id })
    wrapper.unmount()
  })
})
