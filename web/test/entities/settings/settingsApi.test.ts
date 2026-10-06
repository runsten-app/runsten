import { afterEach, describe, expect, it, vi } from 'vitest'
import { fetchSettings, setCurrency, settingsKeys } from '@/entities/settings'
import settings from '@fixtures/settings.json'
import unset from '@fixtures/settings-unset.json'
import { apiError, json, stubFetch } from '@test/http'

afterEach(() => vi.unstubAllGlobals())

describe('settings API', () => {
  it('reads the settings, a currency or none', async () => {
    stubFetch({ 'GET /settings': () => json(unset) })
    expect(await fetchSettings()).toEqual(unset)
  })

  it('sets the currency', async () => {
    const requests = stubFetch({ 'PUT /settings': () => json(settings) })
    expect(await setCurrency('EUR')).toEqual(settings)
    expect(requests[0]?.headers.get('Content-Type')).toBe('application/json')
    expect(await requests[0]?.json()).toEqual({ currency: 'EUR' })
  })

  it('fails with the code of the contract', async () => {
    stubFetch({ 'PUT /settings': () => apiError(409, 'currency_in_use') })
    await expect(setCurrency('SEK')).rejects.toMatchObject({
      status: 409,
      code: 'currency_in_use',
    })
  })

  it('has one key', () => {
    expect(settingsKeys.current()).toEqual(['settings'])
  })
})
