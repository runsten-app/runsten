import type { PeriodStats, Stats } from '@/entities/stats'
import { lastDays, parsePeriod, shortcutDays, type Days } from '@/shared/lib'

// The periods of the Garage: the last 30 days, or the last 12 months by month (this one
// and the eleven before), as the statistics' shortcut has them.
export type GaragePeriod = '30d' | '12m'
export const garagePeriods: readonly GaragePeriod[] = ['30d', '12m']

// The views of the comparison: what tells two vehicles apart.
export type CompareView = 'consumption' | 'cost' | 'charging' | 'battery'
export const compareViews: readonly CompareView[] = ['consumption', 'cost', 'charging', 'battery']

// periodDays are the days of a period on the day today.
export function periodDays(p: GaragePeriod, today: Date): Days {
  return p === '30d' ? lastDays(30, today) : shortcutDays('last12Months', today)
}

// instants are the API's bounds of days: from the start of the first to the start of the
// day after the last.
export function instants(days: Days): { from?: string; to?: string } {
  const p = parsePeriod(days.from, days.to)
  return p.valid ? { from: p.from, to: p.to } : {}
}

export type Amounts = { min_minor: number; max_minor: number }
type Trips = PeriodStats['trips']
type Charges = PeriodStats['charges']

// costKnown tells whether a cost is known for some charge of the period: none of them, or
// all of them unknown, and it is no amount.
export function costKnown(c: Pick<Charges, 'count' | 'cost_unknown'>): boolean {
  return c.count === 0 || c.cost_unknown < c.count
}

// perHundred is what 100 km cost a vehicle over a period or an interval, as its
// statistics give it: its trips' energy at the price of the kWh of the 30 days before
// each, over their distance. Unknown without a trip of known cost and distance, and for
// a plug-in hybrid, whose trips' energy is the battery's alone.
export function perHundred(trips: Pick<Trips, 'cost_per_100km'>): Amounts | null {
  return trips.cost_per_100km
}

// accountPerHundred is what 100 km cost the account: its trips' costs over their
// distance. Unknown as soon as a vehicle that drove has a trip of unknown cost or
// distance, or no cost per 100 km (a plug-in hybrid): a part left out would bend the
// ratio, not just lower it. The lowest rounded down and the highest up, as the API
// rounds the costs; a cost known exactly rounds to the nearest.
function accountPerHundred(periods: PeriodStats[]): Amounts | null {
  const drove = periods.map((p) => p.trips).filter((t) => t.count > 0)
  const whole = (t: Trips) =>
    t.cost_unknown === 0 && t.distance_unknown === 0 && t.cost_per_100km !== null
  if (!drove.length || !drove.every(whole)) return null
  const km = drove.reduce((a, t) => a + t.distance_km, 0)
  const min = drove.reduce((a, t) => a + t.cost.min_minor, 0)
  const max = drove.reduce((a, t) => a + t.cost.max_minor, 0)
  if (km <= 0) return null
  if (min === max) {
    const v = Math.round((min * 100) / km)
    return { min_minor: v, max_minor: v }
  }
  return { min_minor: Math.floor((min * 100) / km), max_minor: Math.ceil((max * 100) / km) }
}

// AccountTotals are the vehicles' periods added up: what the account drove and paid.
export type AccountTotals = {
  distanceKm: number
  distanceUnknown: number
  charges: number
  cost: Amounts
  costUnknown: number
  // No vehicle's cost is known: no amount to show.
  costKnown: boolean
  perHundred: Amounts | null
}

export function accountTotals(periods: PeriodStats[]): AccountTotals {
  const sum = (v: (p: PeriodStats) => number) => periods.reduce((a, p) => a + v(p), 0)
  const trips = {
    distance_km: sum((p) => p.trips.distance_km),
    distance_unknown: sum((p) => p.trips.distance_unknown),
  }
  const charges = {
    count: sum((p) => p.charges.count),
    cost: {
      min_minor: sum((p) => p.charges.cost.min_minor),
      max_minor: sum((p) => p.charges.cost.max_minor),
    },
    cost_unknown: sum((p) => p.charges.cost_unknown),
  }
  return {
    distanceKm: trips.distance_km,
    distanceUnknown: trips.distance_unknown,
    charges: charges.count,
    cost: charges.cost,
    costUnknown: charges.cost_unknown,
    costKnown: periods.some((p) => p.charges.count > 0 && costKnown(p.charges)),
    perHundred: accountPerHundred(periods),
  }
}

// ChargeSplit is the energy charged, in kWh, by where: at the account's places, on AC
// outside them, on DC outside them, and unknown (no position and no place, or no type).
export type ChargeSplit = { places: number; ac: number; dc: number; unknown: number }

export function chargeSplit(s: Pick<Stats, 'charges_by_place'>): ChargeSplit {
  const c = s.charges_by_place
  return {
    places: c.places.reduce((a, p) => a + p.energy_soc_kwh, 0),
    ac: c.outside.ac.energy_soc_kwh,
    dc: c.outside.dc.energy_soc_kwh,
    unknown: c.outside.unknown.energy_soc_kwh + c.no_position.energy_soc_kwh,
  }
}

// shares are the parts of a split in percent of its whole, null without energy.
export function shares(s: ChargeSplit): ChargeSplit | null {
  const total = s.places + s.ac + s.dc + s.unknown
  if (total <= 0) return null
  const pct = (v: number) => (v * 100) / total
  return { places: pct(s.places), ac: pct(s.ac), dc: pct(s.dc), unknown: pct(s.unknown) }
}

// TemperatureAxis is the bands of outside temperature shared by every vehicle, coldest
// first, by their lower bound in °C: each vehicle's own run from its coldest to its
// warmest, and the comparison needs one axis across them.
export function temperatureAxis(lists: { min_c: number }[][]): number[] {
  const all = lists.flat().map((b) => b.min_c)
  if (!all.length) return []
  const lo = Math.min(...all)
  const hi = Math.max(...all)
  const step = 5
  return Array.from({ length: Math.round((hi - lo) / step) + 1 }, (_, i) => lo + i * step)
}

// rankedConsumption is the consumption of a period when every trip's distance and
// energy are known: an estimate over part of the trips is shown, never ranked.
export function rankedConsumption(
  trips: Pick<Trips, 'consumption_kwh_per_100km' | 'distance_unknown' | 'energy_unknown'>,
): number | null {
  return trips.distance_unknown === 0 && trips.energy_unknown === 0
    ? trips.consumption_kwh_per_100km
    : null
}

// best is the lowest known value among several, when there are two at least to compare:
// the one a tile marks. Several equal to it are all the best.
export function best(values: (number | null)[]): number | null {
  const known = values.filter((v): v is number => v !== null)
  return known.length >= 2 ? Math.min(...known) : null
}
