import { afterEach, describe, expect, it, vi } from 'vitest'
import { fetchStats, statsKeys } from '@/entities/stats'
import stats from '@fixtures/stats.json'
import { apiError, json, stubFetch } from '@test/http'

afterEach(() => vi.unstubAllGlobals())

const vehicle = 'v1'

describe('stats API', () => {
  it('reads the statistics, with only the parameters given', async () => {
    const requests = stubFetch({ [`GET /vehicles/${vehicle}/stats`]: () => json(stats) })
    expect(await fetchStats(vehicle, { tz: 'Europe/Paris' })).toEqual(stats)
    expect(Object.fromEntries(new URL(requests[0]?.url ?? '').searchParams)).toEqual({
      tz: 'Europe/Paris',
    })
    await fetchStats(vehicle, { from: 'a', to: 'b', tz: 'UTC', bucket: 'day' })
    expect(Object.fromEntries(new URL(requests[1]?.url ?? '').searchParams)).toEqual({
      from: 'a',
      to: 'b',
      tz: 'UTC',
      bucket: 'day',
    })
  })

  it('fails with the code of the contract', async () => {
    stubFetch({ [`GET /vehicles/${vehicle}/stats`]: () => apiError(400, 'invalid_parameter') })
    await expect(fetchStats(vehicle, { tz: 'Mars' })).rejects.toMatchObject({
      code: 'invalid_parameter',
    })
  })

  it('has one key per query, under the vehicle', () => {
    expect(statsKeys.period(vehicle, { tz: 'UTC' })).toEqual([
      'stats',
      vehicle,
      null,
      null,
      'UTC',
      null,
    ])
    const key = statsKeys.period(vehicle, { from: 'a', to: 'b', tz: 'UTC', bucket: 'week' })
    expect(key).toEqual(['stats', vehicle, 'a', 'b', 'UTC', 'week'])
    expect(key.slice(0, 2)).toEqual(statsKeys.all(vehicle))
  })
})
