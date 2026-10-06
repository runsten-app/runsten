import { describe, expect, it } from 'vitest'
import type { Place } from '@/entities/place'
import type { Limits } from '@/entities/settings'
import { draftOf, newWindow, type PlaceDraft } from '@/features/edit-place/model/draft'
import { field, validate } from '@/features/edit-place/model/validate'
import place from '@fixtures/place.json'
import settings from '@fixtures/settings.json'

// check validates within the limits the API gives.
const check = (d: PlaceDraft, limits: Limits = settings.limits) => validate(d, limits)

const written = Object.fromEntries(Object.entries(place).filter(([k]) => k !== 'id')) as Omit<
  typeof place,
  'id'
>
// The body lists the months in order: the reference writes the winter's from November.
const fields = {
  ...written,
  tariff: written.tariff.map((v) => ({
    ...v,
    windows: v.windows.map((w) => ({ ...w, months: [...w.months].sort((a, b) => a - b) })),
  })),
}

// valid is a draft of the reference place, changed by edit.
function valid(edit: (d: PlaceDraft) => void = () => {}) {
  const d = draftOf(place as Place)
  edit(d)
  return d
}
const issues = (edit: (d: PlaceDraft) => void) =>
  check(valid(edit)).issues.map((i) => [i.field, i.rule, i.params ?? null])

describe('validate', () => {
  it('gives back the body of a valid draft', () => {
    expect(check(valid())).toEqual({ issues: [], fields })
  })

  it('reads numbers with a comma, trims the name, and sends blanks as null', () => {
    const { fields: body } = check(
      valid((d) => {
        d.name = '  Home  '
        d.lat = '45,764'
        d.lon = ' 4,8357'
        d.radius = '150,5'
        d.maxPower = ''
        d.efficiency = '0,8'
        const v = d.tariff[0]
        if (v) v.price = '0,25'
      }),
    )
    expect(body).toMatchObject({
      name: 'Home',
      position: { lat: 45.764, lon: 4.8357 },
      radius_m: 150.5,
      max_power_kw: null,
      efficiency: 0.8,
    })
    expect(body?.tariff[0]?.price_per_kwh).toBe(0.25)
  })

  it('sends the days in the order of the week, the months in order, [] for all year', () => {
    const { fields: body } = check(
      valid((d) => {
        const w = d.tariff[1]?.windows[0]
        if (!w) return
        w.days = ['sun', 'mon', 'wed']
        w.months = [12, 1, 11]
        const all = d.tariff[1]?.windows[1]
        if (all) all.allYear = true
      }),
    )
    expect(body?.tariff[1]?.windows[0]).toMatchObject({
      days: ['mon', 'wed', 'sun'],
      months: [1, 11, 12],
    })
    expect(body?.tariff[1]?.windows[1]?.months).toEqual([])
  })

  it.each<[string, (d: PlaceDraft) => void, [string, string, Record<string, number> | null][]]>([
    ['a blank name', (d) => (d.name = '  '), [[field.name, 'required', null]]],
    ['a long name', (d) => (d.name = 'é'.repeat(61)), [[field.name, 'tooLong', { max: 60 }]]],
    [
      'no position',
      (d) => {
        d.lat = ''
        d.lon = ''
      },
      [
        [field.lat, 'required', null],
        [field.lon, 'required', null],
      ],
    ],
    ['a word for a latitude', (d) => (d.lat = 'north'), [[field.lat, 'notNumber', null]]],
    [
      'a latitude past a pole',
      (d) => (d.lat = '90.1'),
      [[field.lat, 'range', { min: -90, max: 90 }]],
    ],
    [
      'a longitude out of range',
      (d) => (d.lon = '-180,5'),
      [[field.lon, 'range', { min: -180, max: 180 }]],
    ],
    ['a small radius', (d) => (d.radius = '19'), [[field.radius, 'range', { min: 20, max: 1000 }]]],
    [
      'a large radius',
      (d) => (d.radius = '1001'),
      [[field.radius, 'range', { min: 20, max: 1000 }]],
    ],
    ['no radius', (d) => (d.radius = ''), [[field.radius, 'required', null]]],
    [
      'a power of 0',
      (d) => (d.maxPower = '0'),
      [[field.maxPower, 'aboveZero', { min: 0, max: 400 }]],
    ],
    [
      'too much power',
      (d) => (d.maxPower = '401'),
      [[field.maxPower, 'aboveZero', { min: 0, max: 400 }]],
    ],
    [
      'an efficiency above 1',
      (d) => (d.efficiency = '88'),
      [[field.efficiency, 'aboveZero', { min: 0, max: 1 }]],
    ],
    ['no time zone', (d) => (d.timeZone = ''), [[field.timeZone, 'required', null]]],
    ['no version', (d) => (d.tariff = []), [[field.tariff, 'noVersion', null]]],
    [
      'too many versions',
      (d) => {
        const v = d.tariff[0]
        if (v)
          d.tariff = Array.from({ length: 51 }, (_, i) => ({
            ...v,
            validFrom: `2026-01-01`.replace(
              '01-01',
              `${String(1 + Math.floor(i / 28)).padStart(2, '0')}-${String(1 + (i % 28)).padStart(2, '0')}`,
            ),
          }))
      },
      [[field.tariff, 'tooMany', { max: 50 }]],
    ],
    [
      'no date',
      (d) => d.tariff[0] && (d.tariff[0].validFrom = ''),
      [[field.validFrom(0), 'required', null]],
    ],
    [
      'a wrong date',
      (d) => d.tariff[0] && (d.tariff[0].validFrom = '2026-02-30'),
      [[field.validFrom(0), 'notDate', null]],
    ],
    [
      'two versions on the same day',
      (d) => d.tariff[1] && (d.tariff[1].validFrom = '2026-02-01'),
      [[field.validFrom(1), 'sameDay', null]],
    ],
    [
      'no base price',
      (d) => d.tariff[0] && (d.tariff[0].price = ''),
      [[field.price(0), 'required', null]],
    ],
    [
      'a negative price',
      (d) => d.tariff[0] && (d.tariff[0].price = '-0.1'),
      [[field.price(0), 'range', { min: 0, max: 10000 }]],
    ],
    [
      'a price above the cap',
      (d) => d.tariff[0] && (d.tariff[0].price = '10000.01'),
      [[field.price(0), 'range', { min: 0, max: 10000 }]],
    ],
    [
      'six decimals',
      (d) => d.tariff[0] && (d.tariff[0].price = '0.123456'),
      [[field.price(0), 'decimals', { max: 5 }]],
    ],
    [
      'too many windows',
      (d) =>
        d.tariff[0] &&
        (d.tariff[0].windows = Array.from({ length: 25 }, () => ({
          ...newWindow(),
          from: '22:00',
          to: '06:00',
          price: '0.1',
        }))),
      [[field.addWindow(0), 'tooMany', { max: 24 }]],
    ],
    [
      'a window without a day',
      (d) => d.tariff[0]?.windows[0] && (d.tariff[0].windows[0].days = []),
      [[field.days(0, 0), 'noDay', null]],
    ],
    [
      'a window without its times',
      (d) => {
        const w = d.tariff[1]?.windows[2]
        if (w) {
          w.from = ''
          w.to = '25:00'
        }
      },
      [
        [field.windowFrom(1, 2), 'required', null],
        [field.windowTo(1, 2), 'notTime', null],
      ],
    ],
    [
      'a window without a month',
      (d) => d.tariff[1]?.windows[0] && (d.tariff[1].windows[0].months = []),
      [[field.months(1, 0), 'noMonth', null]],
    ],
    [
      'a window without a price',
      (d) => d.tariff[1]?.windows[1] && (d.tariff[1].windows[1].price = 'free'),
      [[field.windowPrice(1, 1), 'notNumber', null]],
    ],
  ])('refuses %s', (_, edit, want) => {
    expect(issues(edit)).toEqual(want)
    expect(check(valid(edit)).fields).toBeNull()
  })

  it('accepts the bounds', () => {
    const d = valid((d) => {
      d.lat = '-90'
      d.lon = '180'
      d.radius = '20'
      d.maxPower = '400'
      d.efficiency = '1'
      if (d.tariff[0]) d.tariff[0].price = '10000'
      if (d.tariff[1]) d.tariff[1].price = '0.12345'
    })
    expect(check(d).issues).toEqual([])
  })

  it('checks the limits it is given, not a copy of them', () => {
    const narrow: Limits = {
      ...settings.limits,
      place_name_chars: 3,
      radius_m: { min: 200, max: 300, default: 250 },
      max_power_kw_max: 7,
      price_per_kwh: { max: 1, decimals: 4 },
    }
    const issues = check(
      valid((d) => {
        d.maxPower = '11'
        if (d.tariff[0]) d.tariff[0].price = '0.21501'
      }),
      narrow,
    ).issues.map((i) => [i.field, i.rule, i.params ?? null])
    expect(issues).toEqual([
      [field.name, 'tooLong', { max: 3 }],
      [field.radius, 'range', { min: 200, max: 300 }],
      [field.maxPower, 'aboveZero', { min: 0, max: 7 }],
      [field.price(0), 'decimals', { max: 4 }],
    ])
  })

  it('names the version and window of each issue', () => {
    const [issue] = check(
      valid((d) => d.tariff[1]?.windows[2] && (d.tariff[1].windows[2].price = '')),
    ).issues
    expect(issue?.where).toEqual({ version: 1, window: 2 })
  })
})
