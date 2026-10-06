import { flushPromises, type VueWrapper } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { AccessToken, CreatedAccessToken } from '@/entities/access-token'
import { AccessTokens } from '@/features/manage-access-tokens'
import { ApiError } from '@/shared/api'
import { i18n } from '@/shared/i18n'
import { mountWith } from '@test/utils'

const api = vi.hoisted(() => ({
  fetchAccessTokens: vi.fn(),
  createAccessToken: vi.fn(),
  revokeAccessToken: vi.fn(),
}))
vi.mock('@/entities/access-token', async (original) => ({
  ...(await original<typeof import('@/entities/access-token')>()),
  ...api,
}))

const secret = 'rst_' + 'a'.repeat(43)
const homeAssistant: AccessToken = {
  id: '7a0e3c1e-0000-4000-8000-000000000001',
  name: 'Home Assistant',
  created_at: '2026-09-01T08:00:00Z',
  last_used_at: '2026-10-03T07:00:00Z',
  expires_at: null,
}
const oldScript: AccessToken = {
  id: '7a0e3c1e-0000-4000-8000-000000000002',
  name: 'Old script',
  created_at: '2026-06-01T08:00:00Z',
  last_used_at: null,
  expires_at: '2026-08-30T08:00:00Z',
}
const created: CreatedAccessToken = {
  token: secret,
  id: '7a0e3c1e-0000-4000-8000-000000000003',
  name: 'Claude Code',
  created_at: '2026-10-03T07:05:00Z',
  last_used_at: null,
  expires_at: '2027-01-01T07:05:00Z',
}
let wrapper: VueWrapper | undefined

beforeEach(() => {
  for (const f of Object.values(api)) f.mockReset()
  i18n.global.locale.value = 'en'
  vi.useFakeTimers({ now: Date.parse('2026-10-03T07:05:00Z'), toFake: ['Date'] })
})
afterEach(() => {
  wrapper?.unmount()
  wrapper = undefined
  vi.useRealTimers()
})

async function mounted(tokens: AccessToken[] = [homeAssistant, oldScript]) {
  api.fetchAccessTokens.mockResolvedValue(tokens)
  wrapper = mountWith(AccessTokens, { attachTo: document.body }).wrapper
  await flushPromises()
  return wrapper
}
const status = (w: VueWrapper) => w.find('[role="status"]').text()

describe('AccessTokens', () => {
  it('lists the tokens, when they were used and when they end', async () => {
    const w = await mounted()
    const items = w.findAll('.token')
    expect(items).toHaveLength(2)
    expect(items[0]?.text()).toContain('Home Assistant')
    expect(items[0]?.text()).toContain('never expires')
    expect(items[1]?.text()).toContain('last used never')
    expect(items[1]?.find('.ends').text()).toMatch(/^expired /)
    expect(items[1]?.find('.ends').classes()).toContain('text-error')
    expect(w.text()).not.toContain('rst_')
  })

  it('says when there is none, or when they cannot be loaded', async () => {
    let w = await mounted([])
    expect(w.find('.none').text()).toBe('No access token yet.')
    w.unmount()
    api.fetchAccessTokens.mockRejectedValue(new ApiError(500, 'internal', 'boom'))
    w = mountWith(AccessTokens).wrapper
    wrapper = w
    await flushPromises()
    expect(w.text()).toContain('The access tokens could not be loaded.')
  })

  it('issues a token, shows its secret once, and copies it', async () => {
    const w = await mounted()
    await w.find('.new').trigger('click')
    expect(document.activeElement?.id).toBe('access-token-name')
    await w.find('#access-token-name').setValue('  Claude Code ')
    api.createAccessToken.mockResolvedValue(created)
    api.fetchAccessTokens.mockResolvedValue([created, homeAssistant, oldScript])
    await w.find('form').trigger('submit')
    await flushPromises()
    expect(api.createAccessToken).toHaveBeenCalledWith({ name: 'Claude Code', expiry: '90d' })
    expect(w.find('form').exists()).toBe(false)
    expect(w.find('.created').text()).toContain('Token “Claude Code” created')
    expect(w.find<HTMLInputElement>('#access-token-secret').element.value).toBe(secret)
    expect(w.find('.example').text()).toBe(`Authorization: Bearer ${secret}`)

    const writeText = vi.fn().mockResolvedValue(undefined)
    Object.defineProperty(navigator, 'clipboard', { value: { writeText }, configurable: true })
    await w.find('.copy').trigger('click')
    await flushPromises()
    expect(writeText).toHaveBeenCalledWith(secret)
    expect(status(w)).toBe('Access token copied.')

    await w.find('.done').trigger('click')
    await flushPromises()
    expect(w.find('.created').exists()).toBe(false)
    expect(w.text()).not.toContain(secret)
    expect(document.activeElement?.id).toBe('access-token-new')
  })

  it('says how to copy by hand without the clipboard', async () => {
    const w = await mounted()
    await w.find('.new').trigger('click')
    await w.find('#access-token-name').setValue('Claude Code')
    api.createAccessToken.mockResolvedValue(created)
    await w.find('form').trigger('submit')
    await flushPromises()
    const writeText = vi.fn().mockRejectedValue(new Error('denied'))
    Object.defineProperty(navigator, 'clipboard', { value: { writeText }, configurable: true })
    await w.find('.copy').trigger('click')
    await flushPromises()
    expect(status(w)).toContain('copy it by hand')
  })

  it('requires a name, and tells a failure', async () => {
    const w = await mounted()
    await w.find('.new').trigger('click')
    await w.find('#access-token-name').setValue('   ')
    await w.find('form').trigger('submit')
    await flushPromises()
    expect(api.createAccessToken).not.toHaveBeenCalled()
    expect(w.find('.error-summary').text()).toContain('Give the token a name.')

    await w.find('#access-token-name').setValue('x'.repeat(65))
    await w.find('form').trigger('submit')
    await flushPromises()
    expect(w.find('.error-summary').text()).toContain('At most 64 characters.')

    await w.find('#access-token-name').setValue('Script')
    api.createAccessToken.mockRejectedValue(new ApiError(502, 'unavailable', 'down'))
    await w.find('form').trigger('submit')
    await flushPromises()
    expect(w.find('.error-summary').text()).toContain('Runsten cannot be reached.')
    expect(w.find('form').exists()).toBe(true)

    await w.find('.cancel').trigger('click')
    await flushPromises()
    expect(w.find('form').exists()).toBe(false)
    expect(document.activeElement?.id).toBe('access-token-new')
  })

  it('revokes a token after a confirmation', async () => {
    const w = await mounted()
    await w.findAll('.revoke')[0]?.trigger('click')
    await flushPromises()
    expect(document.body.textContent).toContain('Revoke “Home Assistant”?')
    api.revokeAccessToken.mockRejectedValueOnce(new ApiError(500, 'internal', 'boom'))
    document.querySelector<HTMLButtonElement>('.confirm-revoke')?.click()
    await flushPromises()
    expect(document.body.textContent).toContain('The token could not be revoked.')

    api.revokeAccessToken.mockResolvedValue(undefined)
    api.fetchAccessTokens.mockResolvedValue([oldScript])
    document.querySelector<HTMLButtonElement>('.confirm-revoke')?.click()
    await flushPromises()
    expect(api.revokeAccessToken).toHaveBeenLastCalledWith(homeAssistant.id)
    expect(status(w)).toBe('Token Home Assistant revoked.')
    expect(w.findAll('.token')).toHaveLength(1)
  })
})
