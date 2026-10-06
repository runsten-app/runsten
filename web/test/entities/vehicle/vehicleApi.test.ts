import { afterEach, describe, expect, it, vi } from 'vitest'
import {
  fetchVariants,
  fetchVehicle,
  fetchVehicles,
  fetchVehicleState,
  setVehicleModel,
  vehicleKeys,
} from '@/entities/vehicle'
import state from '@fixtures/state.json'
import variants from '@fixtures/variants.json'
import chosen from '@fixtures/vehicle-model.json'
import reauth from '@fixtures/vehicle-reauth-required.json'
import vehicles from '@fixtures/vehicles.json'
import { apiError, json, stubFetch } from '@test/http'

afterEach(() => vi.unstubAllGlobals())

const id = reauth.id

describe('vehicle API', () => {
  it('lists the vehicles, reads one and its state', async () => {
    stubFetch({
      'GET /vehicles': () => json(vehicles),
      [`GET /vehicles/${id}`]: () => json(reauth),
      [`GET /vehicles/${id}/state`]: () => json(state),
    })
    expect(await fetchVehicles()).toEqual(vehicles.items)
    expect(await fetchVehicle(id)).toEqual(reauth)
    expect(await fetchVehicleState(id)).toEqual(state)
  })

  it('lists the variants a vehicle may be, and writes the choice', async () => {
    const requests = stubFetch({
      [`GET /vehicles/${chosen.id}/variants`]: () => json(variants),
      [`PUT /vehicles/${chosen.id}/model`]: () => json(chosen),
    })
    expect(await fetchVariants(chosen.id)).toEqual(variants.items)
    const choice = { variant_id: 'ex30-er-2024', ac_max_kw: 11 }
    expect(await setVehicleModel(chosen.id, choice)).toEqual(chosen)
    expect(await requests[1]?.json()).toEqual(choice)
  })

  it('fails with the code of the contract', async () => {
    stubFetch({ 'GET /vehicles/nope/state': () => apiError(404, 'not_found') })
    await expect(fetchVehicleState('nope')).rejects.toMatchObject({
      status: 404,
      code: 'not_found',
    })
  })

  it('has one key per query', () => {
    expect(vehicleKeys.list()).toEqual(['vehicles'])
    expect(vehicleKeys.detail(id)).toEqual(['vehicle', id])
    expect(vehicleKeys.state(id)).toEqual(['state', id])
    expect(vehicleKeys.variants(id)).toEqual(['variants', id])
  })
})
