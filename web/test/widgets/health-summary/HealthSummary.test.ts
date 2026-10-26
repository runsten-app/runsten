import { flushPromises } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { HealthSummary } from '@/widgets/health-summary'
import health from '@fixtures/health.json'
import unknown from '@fixtures/health-unknown.json'
import { mountWith } from '@test/utils'

const mocks = vi.hoisted(() => ({ fetchHealth: vi.fn() }))
vi.mock('@/entities/health', async (original) => ({
  ...(await original<typeof import('@/entities/health')>()),
  ...mocks,
}))

beforeEach(() => {
  mocks.fetchHealth.mockReset()
})

async function mounted() {
  const { wrapper } = mountWith(HealthSummary, { props: { vehicle: 'v1' } })
  await flushPromises()
  return wrapper
}

describe('HealthSummary', () => {
  it('tells what needs attention in three lines, and leads to the health', async () => {
    mocks.fetchHealth.mockResolvedValue(health)
    const w = await mounted()
    expect(w.find('h2').text()).toContain('Health')
    expect(w.find('.status-pill').text()).toBe('Needs attention')
    expect(w.findAll('li').map((li) => li.text())).toEqual([
      'Tyres: Rear left · Low',
      'Service: Soon',
      'Warnings: Washer fluid',
    ])
    expect(w.find('a').attributes('href')).toBe('/vehicles/v1/health')
  })

  it('says nothing to report when all is fine', async () => {
    mocks.fetchHealth.mockResolvedValue({
      ...health,
      tyres: { ...health.tyres, wheels: health.tyres.wheels.map((x) => ({ ...x, warning: 'ok' })) },
      service: { ...health.service, state: 'ok' },
      checks: health.checks.map((c) => ({ ...c, warning: false })),
    })
    const w = await mounted()
    expect(w.find('.status-pill').text()).toBe('Nothing to report')
    expect(w.findAll('li').map((li) => li.text())).toEqual([
      'Tyres: OK',
      'Service: Not due',
      'Warnings: none',
    ])
  })

  it('never says the tyres are fine when one is unknown', async () => {
    mocks.fetchHealth.mockResolvedValue({
      ...health,
      tyres: {
        ...health.tyres,
        wheels: health.tyres.wheels.map((x, i) => ({ ...x, warning: i === 3 ? null : 'ok' })),
      },
    })
    const w = await mounted()
    expect(w.findAll('li').map((li) => li.text())).toEqual([
      'Service: Soon',
      'Warnings: Washer fluid',
    ])
  })

  it('is hidden before the vehicle told anything, and when the health cannot be read', async () => {
    mocks.fetchHealth.mockResolvedValue(unknown)
    expect((await mounted()).find('.health-summary').exists()).toBe(false)
    mocks.fetchHealth.mockRejectedValue(new Error('down'))
    expect((await mounted()).find('.health-summary').exists()).toBe(false)
  })
})
