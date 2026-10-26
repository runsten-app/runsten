import { flushPromises } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { healthKeys } from '@/entities/health'
import { useHealth, useHealthText, useTyres } from '@/features/view-health'
import health from '@fixtures/health.json'
import tyres from '@fixtures/tyres.json'
import { withSetup } from '@test/utils'

const mocks = vi.hoisted(() => ({ fetchHealth: vi.fn(), fetchTyres: vi.fn() }))
vi.mock('@/entities/health', async (original) => ({
  ...(await original<typeof import('@/entities/health')>()),
  ...mocks,
}))

beforeEach(() => {
  Object.values(mocks).forEach((m) => m.mockReset())
})

const vehicle = 'v1'

describe('useHealth', () => {
  it('reads the latest health', async () => {
    mocks.fetchHealth.mockResolvedValue(health)
    const { result, queryClient } = withSetup(() => useHealth(vehicle))
    await flushPromises()
    expect(mocks.fetchHealth).toHaveBeenCalledWith(vehicle)
    expect(result.health.value).toEqual(health)
    expect(queryClient.getQueryData(healthKeys.current(vehicle))).toEqual(health)
  })
})

describe('useTyres', () => {
  it('reads the pressures of a window', async () => {
    mocks.fetchTyres.mockResolvedValue(tyres)
    const window = { from: '2026-09-28T12:00:00Z', to: '2026-09-28T23:00:00Z' }
    const { result, queryClient } = withSetup(() => useTyres(vehicle, window))
    await flushPromises()
    expect(mocks.fetchTyres).toHaveBeenCalledWith(vehicle, window)
    expect(result.tyres.value).toEqual(tyres)
    expect(queryClient.getQueryData(healthKeys.tyres(vehicle, window))).toEqual(tyres)
  })
})

describe('useHealthText', () => {
  it('names every wheel, warning, state and check', () => {
    const { result } = withSetup(() => useHealthText())
    expect(result.wheels.value.rear_left).toBe('Rear left')
    expect(result.tyreWarnings.value.very_low).toBe('Very low')
    expect(result.serviceStates.value.overdue).toBe('Overdue')
    expect(result.checks.value.low_voltage_battery).toBe('12 V battery')
    expect(Object.values(result.checks.value).every((s) => !s.startsWith('health.'))).toBe(true)
  })
})
