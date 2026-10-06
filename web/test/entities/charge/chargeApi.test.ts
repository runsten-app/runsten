import { afterEach, describe, expect, it, vi } from 'vitest'
import { chargeKeys, fetchCharge, fetchCharges } from '@/entities/charge'
import charge from '@fixtures/charge.json'
import charges from '@fixtures/charges.json'
import { apiError, json, stubFetch } from '@test/http'

afterEach(() => vi.unstubAllGlobals())

const vehicle = 'v1'

describe('charge API', () => {
  it('lists a page of charges in a period', async () => {
    const requests = stubFetch({ [`GET /vehicles/${vehicle}/charges`]: () => json(charges) })
    const to = '2026-09-29T00:00:00.000Z'
    expect(await fetchCharges(vehicle, { to })).toEqual(charges)
    expect(Object.fromEntries(new URL(requests[0]?.url ?? '').searchParams)).toEqual({ to })
  })

  it('reads a charge, its ID encoded in the path', async () => {
    const requests = stubFetch({
      [`GET /vehicles/${vehicle}/charges/${encodeURIComponent(charge.id)}`]: () => json(charge),
    })
    expect(await fetchCharge(vehicle, charge.id)).toEqual(charge)
    expect(requests[0]?.url).toContain(`/charges/${encodeURIComponent(charge.id)}`)
  })

  it('fails with the code of the contract', async () => {
    stubFetch({ [`GET /vehicles/${vehicle}/charges`]: () => apiError(400, 'invalid_parameter') })
    await expect(fetchCharges(vehicle, { from: 'x' })).rejects.toMatchObject({
      status: 400,
      code: 'invalid_parameter',
    })
  })

  it('has one key per query, the lists under one prefix', () => {
    expect(chargeKeys.list(vehicle, 'a')).toEqual(['charges', vehicle, 'a', null])
    expect(chargeKeys.lists(vehicle)).toEqual(['charges', vehicle])
    expect(chargeKeys.detail(vehicle, charge.id)).toEqual(['charge', vehicle, charge.id])
  })
})
