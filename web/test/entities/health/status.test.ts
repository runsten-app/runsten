import { describe, expect, it } from 'vitest'
import {
  activeWarnings,
  bar,
  hasPressures,
  needsAttention,
  recommendedFor,
  tyresFine,
  tyreTrouble,
  type Health,
} from '@/entities/health'
import health from '@fixtures/health.json'
import unknown from '@fixtures/health-unknown.json'

const h = health as Health
const none = unknown as Health

describe('health status', () => {
  it('shows the pressures in bar', () => {
    expect(bar(248)).toBe(2.48)
  })

  it('takes the recommended pressure of the wheel’s axle', () => {
    const [frontLeft, , rearLeft] = h.tyres?.wheels ?? []
    expect(frontLeft && recommendedFor(h, frontLeft)).toBe(280)
    expect(rearLeft && recommendedFor(h, rearLeft)).toBe(290)
    expect(frontLeft && recommendedFor(none, frontLeft)).toBeNull()
  })

  it('tells whether the vehicle gives its pressures', () => {
    expect(hasPressures(h)).toBe(true)
    expect(hasPressures(none)).toBe(false)
    const warningsOnly: Health = {
      ...h,
      tyres: {
        wheels: (h.tyres?.wheels ?? []).map((w) => ({ ...w, pressure_kpa: null })),
        recommended_front_kpa: null,
        recommended_rear_kpa: null,
      },
    }
    expect(hasPressures(warningsOnly)).toBe(false)
  })

  it('lists the tyres in trouble, the worst first, and the active warnings', () => {
    expect(tyreTrouble(h).map((w) => [w.wheel, w.warning])).toEqual([
      ['rear_left', 'low'],
      ['rear_right', 'fault'],
    ])
    expect(activeWarnings(h).map((c) => c.check)).toEqual(['washer_fluid'])
    expect(tyreTrouble(none)).toEqual([])
  })

  it('is fine only when the four tyres are known and none warns', () => {
    const all = (warning: 'ok' | null): Health => ({
      ...h,
      tyres: {
        wheels: (h.tyres?.wheels ?? []).map((w) => ({ ...w, warning })),
        recommended_front_kpa: null,
        recommended_rear_kpa: null,
      },
    })
    expect(tyresFine(all('ok'))).toBe(true)
    expect(tyresFine(all(null))).toBe(false)
    expect(tyresFine(h)).toBe(false)
    const three = all('ok')
    three.tyres?.wheels.pop()
    expect(tyresFine(three)).toBe(false)
    expect(tyresFine(none)).toBe(false)
  })

  it('needs attention for a tyre, a service due or a warning', () => {
    expect(needsAttention(h)).toBe(true)
    expect(needsAttention(none)).toBe(false)
    const fine: Health = {
      ...h,
      tyres: null,
      checks: h.checks.map((c) => ({ ...c, warning: false })),
    }
    expect(needsAttention(fine)).toBe(false)
    const due = (state: 'ok' | 'soon' | 'due' | 'overdue'): Health => ({
      ...fine,
      service: {
        ...(h.service ?? { days: null, months: null, distance_km: null, reported_at: null }),
        state,
      },
    })
    expect([
      needsAttention(due('soon')),
      needsAttention(due('due')),
      needsAttention(due('overdue')),
    ]).toEqual([false, true, true])
  })
})
