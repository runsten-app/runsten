import { weekdays, type PlaceFields } from '@/entities/place'
import type { Limits } from '@/entities/settings'
import { decimalPlaces, isCalendarDay, parseDecimal } from '@/shared/lib'
import type { PlaceDraft, WindowDraft } from './draft'

// Field names every issue by the element that shows it: the error summary leads there.
export const field = {
  name: 'place-name',
  lat: 'place-lat',
  lon: 'place-lon',
  radius: 'place-radius',
  withoutPosition: 'place-without-position',
  maxPower: 'place-max-power',
  efficiency: 'place-efficiency',
  timeZone: 'place-time-zone',
  tariff: 'place-tariff',
  validFrom: (v: number) => `place-v${v}-from`,
  price: (v: number) => `place-v${v}-price`,
  addWindow: (v: number) => `place-v${v}-add-window`,
  days: (v: number, w: number) => `place-v${v}-w${w}-days`,
  windowFrom: (v: number, w: number) => `place-v${v}-w${w}-from`,
  windowTo: (v: number, w: number) => `place-v${v}-w${w}-to`,
  months: (v: number, w: number) => `place-v${v}-w${w}-months`,
  windowPrice: (v: number, w: number) => `place-v${v}-w${w}-price`,
}

export type Rule =
  | 'required'
  | 'notNumber'
  | 'range'
  | 'aboveZero'
  | 'tooLong'
  | 'decimals'
  | 'notDate'
  | 'notTime'
  | 'sameDay'
  | 'noVersion'
  | 'tooMany'
  | 'noDay'
  | 'noMonth'

// Issue is a rule a field breaks; where says which version and window it is in, for
// the summary.
export type Issue = {
  field: string
  rule: Rule
  params?: Record<string, number>
  where?: { version: number; window?: number }
}

export type Checked = { issues: Issue[]; fields: PlaceFields | null }

const time = /^([01]\d|2[0-3]):[0-5]\d$/

// validate checks a draft with the API's rules (PlaceFields, within its limits), and gives
// the body to send when it breaks none.
export function validate(d: PlaceDraft, limits: Limits): Checked {
  const issues: Issue[] = []
  const add = (i: Issue) => issues.push(i)

  // number reads a required or optional field within [min, max] (above min when
  // aboveMin), with at most decimals when given.
  function number(
    s: string,
    id: string,
    o: { min: number; max: number; optional?: boolean; aboveMin?: boolean; decimals?: number },
    where?: Issue['where'],
  ): number | null {
    if (!s.trim()) {
      if (!o.optional) add({ field: id, rule: 'required', where })
      return null
    }
    const n = parseDecimal(s)
    if (n === null) add({ field: id, rule: 'notNumber', where })
    else if (o.aboveMin ? n <= o.min || n > o.max : n < o.min || n > o.max)
      add({
        field: id,
        rule: o.aboveMin ? 'aboveZero' : 'range',
        params: { min: o.min, max: o.max },
        where,
      })
    else if (o.decimals !== undefined && decimalPlaces(s) > o.decimals)
      add({ field: id, rule: 'decimals', params: { max: o.decimals }, where })
    return n
  }
  const price = { min: 0, max: limits.price_per_kwh.max, decimals: limits.price_per_kwh.decimals }

  const name = d.name.trim()
  if (!name) add({ field: field.name, rule: 'required' })
  // In characters, as the API counts them, not UTF-16 units.
  else if ([...name].length > limits.place_name_chars)
    add({ field: field.name, rule: 'tooLong', params: { max: limits.place_name_chars } })
  const lat = number(d.lat, field.lat, { min: -90, max: 90 })
  const lon = number(d.lon, field.lon, { min: -180, max: 180 })
  const radius = number(d.radius, field.radius, limits.radius_m)
  const maxPower = number(d.maxPower, field.maxPower, {
    min: 0,
    max: limits.max_power_kw_max,
    optional: true,
    aboveMin: true,
  })
  const efficiency = number(d.efficiency, field.efficiency, {
    min: 0,
    max: 1,
    optional: true,
    aboveMin: true,
  })
  if (!d.timeZone) add({ field: field.timeZone, rule: 'required' })

  if (d.tariff.length === 0) add({ field: field.tariff, rule: 'noVersion' })
  else if (d.tariff.length > limits.versions_per_place)
    add({ field: field.tariff, rule: 'tooMany', params: { max: limits.versions_per_place } })
  const tariff = d.tariff.map((v, i) => {
    const where = { version: i }
    if (!v.validFrom) add({ field: field.validFrom(i), rule: 'required', where })
    else if (!isCalendarDay(v.validFrom)) add({ field: field.validFrom(i), rule: 'notDate', where })
    else if (d.tariff.findIndex((o) => o.validFrom === v.validFrom) < i)
      add({ field: field.validFrom(i), rule: 'sameDay', where })
    const base = number(v.price, field.price(i), price, where)
    if (v.windows.length > limits.windows_per_version)
      add({
        field: field.addWindow(i),
        rule: 'tooMany',
        params: { max: limits.windows_per_version },
        where,
      })
    return {
      valid_from: v.validFrom,
      price_per_kwh: base ?? 0,
      windows: v.windows.map((w, j) => checkWindow(w, i, j)),
    }
  })

  function checkWindow(w: WindowDraft, v: number, j: number) {
    const where = { version: v, window: j }
    if (w.days.length === 0) add({ field: field.days(v, j), rule: 'noDay', where })
    for (const [value, id] of [
      [w.from, field.windowFrom(v, j)],
      [w.to, field.windowTo(v, j)],
    ] as const) {
      if (!value) add({ field: id, rule: 'required', where })
      else if (!time.test(value)) add({ field: id, rule: 'notTime', where })
    }
    if (!w.allYear && w.months.length === 0)
      add({ field: field.months(v, j), rule: 'noMonth', where })
    const p = number(w.price, field.windowPrice(v, j), price, where)
    return {
      days: weekdays.filter((day) => w.days.includes(day)),
      from: w.from,
      to: w.to,
      months: w.allYear ? [] : [...w.months].sort((a, b) => a - b),
      price_per_kwh: p ?? 0,
    }
  }

  if (issues.length) return { issues, fields: null }
  return {
    issues,
    fields: {
      name,
      position: { lat: lat ?? 0, lon: lon ?? 0 },
      radius_m: radius ?? 0,
      time_zone: d.timeZone,
      without_position: d.withoutPosition,
      max_power_kw: maxPower,
      efficiency,
      tariff,
    },
  }
}
