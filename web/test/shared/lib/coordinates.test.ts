import { describe, expect, it } from 'vitest'
import { formatCoordinates, openStreetMapUrl } from '@/shared/lib'

describe('coordinates', () => {
  it.each([
    [{ lat: 45.764, lon: 4.8357 }, '45.76400, 4.83570'],
    [{ lat: -33.8567844, lon: 151.2152967 }, '-33.85678, 151.21530'],
  ])('%o → %s', (p, want) => expect(formatCoordinates(p)).toBe(want))

  it('links to OpenStreetMap at the position', () => {
    expect(openStreetMapUrl({ lat: 45.764, lon: 4.8357 })).toBe(
      'https://www.openstreetmap.org/?mlat=45.76400&mlon=4.83570#map=16/45.76400/4.83570',
    )
  })
})
