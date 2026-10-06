import { weekdays, type Place, type Weekday } from '@/entities/place'
import { addDays, formatCoordinate, isCalendarDay } from '@/shared/lib'

// A draft is the form of a place as typed: numbers stay text until they are checked
// (a comma is a decimal separator too), and a window says "all year" apart from its
// months.
export type WindowDraft = {
  key: number
  days: Weekday[]
  from: string
  to: string
  allYear: boolean
  months: number[]
  price: string
}

export type VersionDraft = {
  key: number
  validFrom: string
  price: string
  windows: WindowDraft[]
}

export type PlaceDraft = {
  name: string
  lat: string
  lon: string
  radius: string
  withoutPosition: boolean
  maxPower: string
  efficiency: string
  timeZone: string
  tariff: VersionDraft[]
}

// Keys only tell the cards and windows apart for Vue, across additions and moves.
let lastKey = 0
const key = () => ++lastKey

export function draftOf(place: Place): PlaceDraft {
  return {
    name: place.name,
    lat: formatCoordinate(place.position.lat),
    lon: formatCoordinate(place.position.lon),
    radius: String(place.radius_m),
    withoutPosition: place.without_position,
    maxPower: place.max_power_kw === null ? '' : String(place.max_power_kw),
    efficiency: place.efficiency === null ? '' : String(place.efficiency),
    timeZone: place.time_zone,
    tariff: place.tariff.map((v) => ({
      key: key(),
      validFrom: v.valid_from,
      price: String(v.price_per_kwh),
      windows: v.windows.map((w) => ({
        key: key(),
        days: [...w.days],
        from: w.from,
        to: w.to,
        allYear: w.months.length === 0,
        months: [...w.months],
        price: String(w.price_per_kwh),
      })),
    })),
  }
}

// emptyDraft is a new place: at position when given (from a charge, a vehicle), in the
// reader's time zone, of the radius the API offers, with one version of its tariff from
// today.
export function emptyDraft(o: {
  timeZone: string
  today: string
  radiusM: number
  lat?: number
  lon?: number
}) {
  const draft: PlaceDraft = {
    name: '',
    lat: o.lat === undefined ? '' : formatCoordinate(o.lat),
    lon: o.lon === undefined ? '' : formatCoordinate(o.lon),
    radius: String(o.radiusM),
    withoutPosition: false,
    maxPower: '',
    efficiency: '',
    timeZone: o.timeZone,
    tariff: [{ key: key(), validFrom: o.today, price: '', windows: [] }],
  }
  return draft
}

// newWindow opens every day, all year: the common case is a daily off-peak time.
export function newWindow(): WindowDraft {
  return { key: key(), days: [...weekdays], from: '', to: '', allYear: true, months: [], price: '' }
}

// nextVersion is a new price from today, or the day after the latest version if that is
// later: a copy of the latest, where the user changes what changed.
export function nextVersion(tariff: readonly VersionDraft[], today: string): VersionDraft {
  const latest = tariff.reduce<VersionDraft | undefined>(
    (l, v) => (!l || v.validFrom > l.validFrom ? v : l),
    undefined,
  )
  if (!latest) return { key: key(), validFrom: today, price: '', windows: [] }
  const after = latest.validFrom ? addDays(latest.validFrom, 1) : today
  return {
    key: key(),
    validFrom: after > today ? after : today,
    price: latest.price,
    windows: latest.windows.map((w) => ({
      ...w,
      key: key(),
      days: [...w.days],
      months: [...w.months],
    })),
  }
}

// firstVersionAfter is the index of the tariff's first version when it starts after day,
// the version to start on day for the charges before it to get a price; -1 when the
// tariff starts on day or before, or has no day written yet.
export function firstVersionAfter(tariff: readonly VersionDraft[], day: string): number {
  let first = -1
  tariff.forEach((v, i) => {
    const earliest = tariff[first]?.validFrom
    if (isCalendarDay(v.validFrom) && (earliest === undefined || v.validFrom < earliest)) first = i
  })
  return first >= 0 && (tariff[first]?.validFrom ?? '') > day ? first : -1
}

// daysPresets are the shortcuts of a window's days.
export const dayPresets = {
  everyDay: weekdays,
  weekdays: weekdays.slice(0, 5),
  weekend: weekdays.slice(5),
} as const satisfies Record<string, readonly Weekday[]>
export type DayPreset = keyof typeof dayPresets

// crossesMidnight tells how a window ends: the next day, 24 hours later, or the same day.
export function windowSpan(w: Pick<WindowDraft, 'from' | 'to'>) {
  if (!w.from || !w.to) return null
  if (w.to === w.from) return 'allDay'
  return w.to < w.from ? 'overnight' : null
}

// move puts the item at i at j, in place.
export function move<T>(items: T[], i: number, j: number) {
  const [item] = items.splice(i, 1)
  if (item !== undefined) items.splice(j, 0, item)
}
