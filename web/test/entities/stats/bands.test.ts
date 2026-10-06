import { describe, expect, it } from 'vitest'
import { bandOf } from '@/entities/stats'
import stats from '@fixtures/stats.json'

const bands = stats.trips_by_distance.bands

describe('bandOf', () => {
  it('finds the band of a distance: at least its min, less than its max', () => {
    expect(bandOf(bands, 0)?.min_km).toBe(0)
    expect(bandOf(bands, 4.9)?.max_km).toBe(5)
    expect(bandOf(bands, 5)?.min_km).toBe(5)
    expect(bandOf(bands, 32)?.min_km).toBe(20)
    // The last band has no upper limit.
    expect(bandOf(bands, 100)?.min_km).toBe(100)
    expect(bandOf(bands, 2000)?.max_km).toBeNull()
  })
})
