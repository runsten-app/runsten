import { flushPromises, type VueWrapper } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { Battery } from '@/entities/battery'
import { ApiError } from '@/shared/api'
import { BatteryTiles } from '@/widgets/battery-tiles'
import apiCapacity from '@fixtures/battery-api-capacity.json'
import battery from '@fixtures/battery.json'
import empty from '@fixtures/battery-empty.json'
import { mountWith } from '@test/utils'

const { getBattery } = vi.hoisted(() => ({ getBattery: vi.fn() }))
vi.mock('@/entities/battery', async (original) => ({
  ...(await original<typeof import('@/entities/battery')>()),
  getBattery,
}))

beforeEach(() => {
  getBattery.mockReset()
})

const plain = (s = '') => s.replace(/\s+/g, ' ').trim()
let wrapper: VueWrapper | undefined
afterEach(() => {
  wrapper?.unmount()
  wrapper = undefined
})

async function tiles(body?: unknown) {
  // Without a body: what the mock was set to (a rejection, most often).
  if (body !== undefined) getBattery.mockResolvedValue(body)
  wrapper = mountWith(BatteryTiles, { props: { vehicle: 'v1' }, attachTo: document.body }).wrapper
  await flushPromises()
  return wrapper
}

// tile is the label, the value and the notes of a tile.
function tile(w: VueWrapper, key: string) {
  const t = w.find(`.tile.${key}`)
  return {
    label: t.find('dt').text(),
    value: plain(t.find('.value').text()),
    notes: t.findAll('.note').map((n) => plain(n.text())),
  }
}

describe('BatteryTiles', () => {
  it('tells the median with its quartiles, the reference and the evolution', async () => {
    const w = await tiles(battery)
    // 73.5 kWh over the 75 of the reference: the gauge, and no deviation tile, which
    // would say the same figure.
    expect(tile(w, 'capacity')).toEqual({
      label: 'Remaining capacity',
      value: '≈ 98%',
      notes: ['73.5 kWh estimated', 'between 73 kWh and 73.8 kWh'],
    })
    const meter = w.find('.tile.capacity [role="meter"]')
    expect(meter.attributes('aria-label')).toBe('Remaining capacity')
    expect(meter.attributes('aria-valuenow')).toBe('98')
    expect(meter.attributes('aria-valuetext')).toBe('About 98%, between 97% and 98%')
    expect(w.find('.tile.deviation').exists()).toBe(false)
    expect(tile(w, 'reference')).toEqual({
      label: 'Reference capacity',
      value: '75 kWh',
      notes: ["From the model's data sheet"],
    })
    expect(tile(w, 'change')).toEqual({
      label: 'Evolution',
      value: '-2%',
      notes: ['since 5 Aug 2025'],
    })
    expect(tile(w, 'cycles')).toEqual({
      label: 'Equivalent cycles',
      value: '14.4',
      notes: ["Full charges' worth of energy"],
    })
    // A charge can give two estimates (its power, its receipt): the tile counts charges.
    expect(tile(w, 'kept')).toEqual({
      label: 'Charges used',
      value: '35',
      notes: ['7 charges not used'],
    })
  })

  it('opens the reasons of the filters in a dialog, each with its count', async () => {
    const w = await tiles(battery)
    await w.find('.why').trigger('click')
    await flushPromises()
    const dialog = document.querySelector('[role="dialog"]')
    expect(dialog?.getAttribute('aria-modal')).toBe('true')
    expect(
      document.getElementById(dialog?.getAttribute('aria-labelledby') ?? '')?.textContent?.trim(),
    ).toBe('Why some charges give no estimate')
    expect(
      [...document.querySelectorAll('[role="dialog"] li')].map((li) => plain(li.textContent ?? '')),
    ).toEqual([
      '1 Not seen charging',
      "1 The car doesn't report its charging power",
      '1 Charged above 95 %',
      '1 Too small a charge (under 20 %)',
      '1 Readings too far apart',
      '1 Charging too slowly',
      '1 Result too far off',
    ])
    // The focus goes back to the button that opened the dialog.
    const close = [...document.querySelectorAll('[role="dialog"] button')].find(
      (b) => b.textContent?.trim() === 'Close',
    ) as HTMLElement | undefined
    close?.click()
    await flushPromises()
    expect(document.activeElement).toBe(w.find('.why').element)
  })

  it('says when there are not enough charges, and what the evolution waits for', async () => {
    const w = await tiles(empty)
    expect(tile(w, 'capacity')).toEqual({
      label: 'Estimated capacity',
      value: 'Not enough charges yet',
      // Four estimates kept: one more is enough.
      notes: ['1 more usable charge needed'],
    })
    expect(w.find('[role="meter"]').exists()).toBe(false)
    expect(tile(w, 'change')).toEqual({
      label: 'Evolution',
      value: 'In 12 months of data',
      notes: [],
    })
    expect(tile(w, 'kept').notes).toEqual(['0 charges not used'])
  })

  it('counts the usable charges still needed, not every charge', async () => {
    const w = await tiles({
      ...empty,
      estimates: [],
      months: [],
      excluded: { ...empty.excluded, implausible: 12 },
    })
    expect(tile(w, 'capacity').notes).toEqual(['5 more usable charges needed'])
    expect(tile(w, 'kept')).toMatchObject({ value: '0', notes: ['12 charges not used'] })
  })

  it('says where the reference comes from when the catalog does not know the variant', async () => {
    const w = await tiles(apiCapacity)
    expect(tile(w, 'reference')).toEqual({
      label: 'Reference capacity',
      value: '80 kWh',
      notes: ['As reported by the car'],
    })
    expect(tile(w, 'change').value).toBe('In 12 months of data')
    // 75 kWh estimated over the 80 the car reports.
    expect(tile(w, 'capacity').value).toBe('≈ 94%')
  })

  it('draws the gauge clamped to its scale, the figure under it exact', async () => {
    const b = structuredClone(battery) as Battery
    if (b.current) b.current = { ...b.current, capacity_kwh: 78, q1_kwh: 77, q3_kwh: 79 }
    const w = await tiles(b)
    // 104 % of the reference: the arc is full, the text and the value text say 104.
    expect(tile(w, 'capacity').value).toBe('≈ 104%')
    const meter = w.find('.tile.capacity [role="meter"]')
    expect(meter.attributes('aria-valuenow')).toBe('100')
    expect(meter.attributes('aria-valuetext')).toBe('About 104%, between 103% and 105%')
    expect(meter.find('.fill').attributes('stroke-dasharray')).toBe('100 100')
  })

  it('says unknown without a reference: no gauge, no cycles', async () => {
    const b = structuredClone(battery) as Battery
    b.reference = null
    b.deviation_pct = null
    b.cycles = null
    const w = await tiles(b)
    expect(tile(w, 'reference').value).toBe('Unknown')
    expect(tile(w, 'reference').notes).toEqual([])
    // Nothing to compare with: the estimate alone, without a gauge.
    expect(tile(w, 'capacity')).toEqual({
      label: 'Estimated capacity',
      value: '73.5 kWh',
      notes: ['between 73 kWh and 73.8 kWh'],
    })
    expect(w.find('[role="meter"]').exists()).toBe(false)
    expect(tile(w, 'cycles').value).toBe('Unknown')
  })

  it('counts one charge not used as one', async () => {
    const b = structuredClone(battery) as Battery
    b.excluded = {
      reconstructed: 1,
      no_power: 0,
      high_soc: 0,
      span_soc: 0,
      power_gap: 0,
      low_power: 0,
      implausible: 0,
    }
    const w = await tiles(b)
    expect(tile(w, 'kept').notes).toEqual(['1 charge not used'])
  })

  it.each([
    [new ApiError(404, 'not_found', ''), 'This vehicle does not exist.'],
    [new ApiError(400, 'invalid_parameter', ''), 'more months of history'],
    [new ApiError(500, 'internal', ''), 'The battery could not be loaded'],
  ])('tells a failure: %s', async (err, text) => {
    getBattery.mockRejectedValue(err)
    const w = await tiles()
    expect(w.find('.v-alert').text()).toContain(text)
    expect(w.find('dl').exists()).toBe(false)
  })
})
