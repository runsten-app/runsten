import { afterEach, describe, expect, it, vi } from 'vitest'
import {
  attachOrphanCost,
  chargeKeys,
  deleteChargeCost,
  deleteOrphanCost,
  fetchOrphanCosts,
  setChargeCost,
} from '@/entities/charge'
import entered from '@fixtures/charge-cost.json'
import charge from '@fixtures/charge.json'
import orphans from '@fixtures/orphans.json'
import { apiError, json, stubFetch } from '@test/http'

afterEach(() => vi.unstubAllGlobals())

const vehicle = 'v1'
const path = `/vehicles/${vehicle}/charges/${encodeURIComponent(entered.id)}/cost`
const orphan = orphans.items[0]?.id ?? ''

describe('cost API', () => {
  it('enters the cost of a charge, and gets the charge back', async () => {
    const requests = stubFetch({ [`PUT ${path}`]: () => json(entered) })
    const cost = { amount_minor: 850, energy_kwh: 20.1, note: 'Borne du parking' }
    expect(await setChargeCost(vehicle, entered.id, cost)).toEqual(entered)
    expect(requests[0]?.headers.get('Content-Type')).toBe('application/json')
    expect(await requests[0]?.json()).toEqual(cost)
  })

  it('deletes the entered cost of a charge', async () => {
    const requests = stubFetch({ [`DELETE ${path}`]: () => new Response(null, { status: 204 }) })
    await deleteChargeCost(vehicle, entered.id)
    expect(requests).toHaveLength(1)
  })

  it('lists the orphaned costs, attaches one to a charge, and deletes one', async () => {
    const requests = stubFetch({
      'GET /charge-costs/orphans': () => json(orphans),
      [`PUT /charge-costs/orphans/${orphan}/charge`]: () => json(charge),
      [`DELETE /charge-costs/orphans/${orphan}`]: () => new Response(null, { status: 204 }),
    })
    expect(await fetchOrphanCosts()).toEqual(orphans.items)
    expect(await attachOrphanCost(orphan, vehicle, charge.id)).toEqual(charge)
    expect(await requests[1]?.json()).toEqual({ vehicle, charge: charge.id })
    await deleteOrphanCost(orphan)
    expect(requests).toHaveLength(3)
  })

  it('fails with the code of the contract', async () => {
    stubFetch({
      [`PUT /charge-costs/orphans/${orphan}/charge`]: () => apiError(409, 'charge_has_cost'),
    })
    await expect(attachOrphanCost(orphan, vehicle, charge.id)).rejects.toMatchObject({
      status: 409,
      code: 'charge_has_cost',
    })
  })

  // Apart from the lists, which the details read as pages; under no prefix of theirs.
  it('keys the orphans and the candidates apart from the lists', () => {
    expect(chargeKeys.orphans()).toEqual(['charge-costs', 'orphans'])
    expect(chargeKeys.candidates(vehicle, 'a', 'b')).toEqual([
      'charge-candidates',
      vehicle,
      'a',
      'b',
    ])
    expect(chargeKeys.candidates(vehicle, 'a', 'b').slice(0, 1)).toEqual(chargeKeys.allCandidates())
  })
})
