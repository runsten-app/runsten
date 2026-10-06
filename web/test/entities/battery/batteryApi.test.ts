import { afterEach, describe, expect, it, vi } from 'vitest'
import { batteryKeys, getBattery } from '@/entities/battery'
import battery from '@fixtures/battery.json'
import { apiError, json, stubFetch } from '@test/http'

afterEach(() => vi.unstubAllGlobals())

const vehicle = 'v1'

describe('battery API', () => {
  it('reads the estimated capacity, with only the time zone given', async () => {
    const requests = stubFetch({ [`GET /vehicles/${vehicle}/battery`]: () => json(battery) })
    expect(await getBattery(vehicle, { tz: 'Europe/Paris' })).toEqual(battery)
    expect(Object.fromEntries(new URL(requests[0]?.url ?? '').searchParams)).toEqual({
      tz: 'Europe/Paris',
    })
  })

  it('fails with the code of the contract', async () => {
    stubFetch({ [`GET /vehicles/${vehicle}/battery`]: () => apiError(400, 'invalid_parameter') })
    await expect(getBattery(vehicle, { tz: 'Mars' })).rejects.toMatchObject({
      code: 'invalid_parameter',
    })
  })

  it('has one key per query, under the vehicle', () => {
    expect(batteryKeys.trend(vehicle, { tz: 'UTC' })).toEqual(['battery', vehicle, 'UTC'])
    expect(batteryKeys.trend(vehicle, { tz: 'Europe/Paris' }).slice(0, 2)).toEqual(
      batteryKeys.all(vehicle),
    )
    expect(batteryKeys.all(vehicle).slice(0, 1)).toEqual(batteryKeys.every())
  })
})
