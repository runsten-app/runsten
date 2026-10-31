import type { Health, Tyre, TyreWarning } from './Health'

// The pressures come in kPa; they are shown in bar, as the cars' screens show them in
// Europe (100 kPa = 1 bar).
export function bar(kPa: number): number {
  return kPa / 100
}

// The order of the wheels on the screen: front first, left first, as the API gives them.
export const wheels = ['front_left', 'front_right', 'rear_left', 'rear_right'] as const

// recommendedFor is the pressure the vehicle recommends at the wheel's axle; null when
// it does not tell.
export function recommendedFor(h: Health, t: Tyre): number | null {
  const tyres = h.tyres
  if (!tyres) return null
  return t.wheel.startsWith('front') ? tyres.recommended_front_kpa : tyres.recommended_rear_kpa
}

// hasPressures tells whether the vehicle reports a tyre's pressure: a Volvo reports a
// warning only.
export function hasPressures(h: Health): boolean {
  return !!h.tyres?.wheels.some((w) => w.pressure_kpa !== null)
}

// tyreTrouble is the tyres whose monitoring warns, worst first; [] when none does.
const severity: Record<TyreWarning, number> = { ok: 0, high: 1, fault: 2, low: 3, very_low: 4 }
export function tyreTrouble(h: Health): Tyre[] {
  return (h.tyres?.wheels ?? [])
    .filter((w) => w.warning && w.warning !== 'ok')
    .sort((a, b) => severity[b.warning ?? 'ok'] - severity[a.warning ?? 'ok'])
}

// tyresFine tells whether the four tyres are known and none warns: fewer known are
// never fine, whatever they say.
export function tyresFine(h: Health): boolean {
  const ws = h.tyres?.wheels ?? []
  return wheels.every((w) => ws.find((x) => x.wheel === w)?.warning === 'ok')
}

// activeWarnings are the checks that warn, in the API's order: the fluids and the
// battery first, then the lights.
export function activeWarnings(h: Health) {
  return h.checks.filter((c) => c.warning)
}

// reportedFine tells whether nothing warns in what the vehicle reports, something of it
// reported: the tyres it reports all known, the state of the service it reports known.
// Never more: a check reported with an unknown state is left out, and which checks a
// vehicle reports depends on its model, so no health is known to be complete.
export function reportedFine(h: Health): boolean {
  const reported = !!h.tyres || !!h.service || h.checks.length > 0
  return (
    reported &&
    !needsAttention(h) &&
    (!h.tyres || tyresFine(h)) &&
    (!h.service || h.service.state !== null)
  )
}

// needsAttention tells whether anything of the health warns: a tyre, the service due or
// overdue, a check.
export function needsAttention(h: Health): boolean {
  const s = h.service?.state
  return tyreTrouble(h).length > 0 || s === 'due' || s === 'overdue' || activeWarnings(h).length > 0
}
