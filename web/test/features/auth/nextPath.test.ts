import { describe, expect, it } from 'vitest'
import { nextPath } from '@/features/auth'

describe('nextPath', () => {
  it.each([
    ['/vehicles/v1/trips?from=2026-09-01', '/vehicles/v1/trips?from=2026-09-01'],
    ['/', '/'],
    [undefined, '/'],
    [['/a', '/b'], '/'],
    ['https://evil.example/', '/'],
    ['//evil.example/', '/'],
    ['/\\evil.example', '/'],
    ['vehicles', '/'],
  ])('%s → %s', (raw, want) => expect(nextPath(raw)).toBe(want))
})
