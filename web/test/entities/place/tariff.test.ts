import { describe, expect, it } from 'vitest'
import { currentVersion, upcomingVersion, type TariffVersion } from '@/entities/place'
import place from '@fixtures/place.json'

const tariff = place.tariff as TariffVersion[]
const version = (valid_from: string): TariffVersion => ({
  valid_from,
  price_per_kwh: 0.2,
  windows: [],
})

describe('tariff versions', () => {
  it.each([
    ['2026-01-31', null, '2026-02-01'],
    ['2026-02-01', '2026-02-01', '2026-08-01'],
    ['2026-07-31', '2026-02-01', '2026-08-01'],
    ['2026-08-01', '2026-08-01', null],
    ['2027-01-01', '2026-08-01', null],
  ])('on %s, the current is %s and the next %s', (day, current, next) => {
    expect(currentVersion(tariff, day)?.valid_from ?? null).toBe(current)
    expect(upcomingVersion(tariff, day)?.valid_from ?? null).toBe(next)
  })

  it('do not depend on the order written', () => {
    const t = [version('2026-08-01'), version('2026-01-01'), version('2026-05-01')]
    expect(currentVersion(t, '2026-06-01')?.valid_from).toBe('2026-05-01')
    expect(upcomingVersion(t, '2025-06-01')?.valid_from).toBe('2026-01-01')
  })
})
