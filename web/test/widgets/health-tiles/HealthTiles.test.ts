import { describe, expect, it, vi } from 'vitest'
import type { Health } from '@/entities/health'
import { HealthTiles } from '@/widgets/health-tiles'
import health from '@fixtures/health.json'
import { mountWith } from '@test/utils'

const h = health as Health
const plain = (s: string) => s.replace(/\s+/g, ' ').trim()

function mounted(props: Health) {
  vi.useFakeTimers({ toFake: ['Date'] })
  vi.setSystemTime(new Date('2026-09-28T19:30:00Z'))
  const { wrapper } = mountWith(HealthTiles, { props: { health: props } })
  vi.useRealTimers()
  return wrapper
}

describe('HealthTiles', () => {
  it('shows each wheel against its recommended pressure, the service and the warnings', () => {
    const w = mounted(h)
    const wheels = w.findAll('[data-test="tyres"] .wheel')
    const parts = (i: number) => wheels[i]?.findAll('dt, dd').map((x) => plain(x.text()))
    expect(parts(0)).toEqual(['Front left', '2.82 bar', 'OK', 'Recommended at the front 2.8 bar'])
    expect(parts(2)).toEqual(['Rear left', '2.48 bar', 'Low', 'Recommended at the rear 2.9 bar'])
    // No pressure: its warning only.
    expect(parts(3)).toEqual(['Rear right', 'Fault', 'Recommended at the rear 2.9 bar'])
    const service = plain(w.find('[data-test="service"]').text())
    expect(service).toContain('Soon')
    expect(service).toContain('21 days')
    expect(service).toContain('1,200 km')
    expect(w.findAll('[data-test="warnings"] li').map((li) => li.text())).toEqual(['Washer fluid'])
  })

  it('says what the vehicle does not report, never shows it fine', () => {
    const w = mounted({ ...h, tyres: null, service: null, checks: [] })
    expect(w.find('[data-test="tyres"]').text()).toContain('does not report its tyres')
    // Neither the service nor the warnings: one sentence, not two empty parts.
    expect(w.find('[data-test="upkeep-none"]').text()).toBe(
      'The vehicle tells neither its next service nor its warnings.',
    )
    expect(w.find('[data-test="service"]').exists()).toBe(false)
    expect(w.findAll('.status-pill')).toHaveLength(0)
  })

  it('says which of the service and the warnings the vehicle does not report', () => {
    const w = mounted({ ...h, service: null })
    expect(w.find('.upkeep h2').text()).toBe('Service and warnings')
    expect(w.find('[data-test="service"]').text()).toContain('does not tell when its next service')
    expect(w.findAll('[data-test="warnings"] li').map((li) => li.text())).toEqual(['Washer fluid'])
    expect(
      plain(
        mounted({ ...h, checks: [] })
          .find('[data-test="warnings"]')
          .text(),
      ),
    ).toContain('reports no check')
  })

  it('tells a Volvo’s tyres by their warnings, the months to its service, and no warning counted', () => {
    const w = mounted({
      ...h,
      tyres: {
        wheels: (h.tyres?.wheels ?? []).map((x) => ({ ...x, pressure_kpa: null, warning: 'ok' })),
        recommended_front_kpa: null,
        recommended_rear_kpa: null,
      },
      service: { state: 'ok', days: null, months: 3, distance_km: null, reported_at: null },
      checks: h.checks.map((c) => ({ ...c, warning: false })),
    })
    expect(w.find('[data-test="tyres"]').text()).toContain('a warning per tyre, not its pressure')
    expect(w.find('[data-test="tyres"] .status-pill').text()).toBe('Nothing to report')
    expect(w.find('[data-test="tyres"] .figure').exists()).toBe(false)
    const service = plain(w.find('[data-test="service"]').text())
    expect(service).toContain('3 months')
    expect(service).toContain('Unknown')
    expect(w.find('[data-test="warnings"]').text()).toContain(
      'No warning, out of 4 checks reported.',
    )
  })
})
