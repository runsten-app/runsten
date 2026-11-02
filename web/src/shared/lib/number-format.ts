// The units Runsten shows. Intl formats kilometers, speeds, percents and degrees itself; it has no watt,
// watt-hour, ampere, volt nor pressure unit, so W, kW, kWh, kWh/100 km, A, V and bar are a
// formatted number and a symbol.
export type Unit =
  | 'km'
  | 'kmh'
  | 'percent'
  | 'W'
  | 'kW'
  | 'kWh'
  | 'kWhPer100km'
  | 'bar'
  | 'celsius'
  | 'ampere'
  | 'volt'

const intlUnits: Partial<Record<Unit, string>> = {
  km: 'kilometer',
  kmh: 'kilometer-per-hour',
  percent: 'percent',
  celsius: 'celsius',
}
const symbols: Partial<Record<Unit, string>> = {
  kWhPer100km: 'kWh/100\u00a0km',
  ampere: 'A',
  volt: 'V',
}
const fractionDigits: Record<Unit, number> = {
  km: 0,
  kmh: 0,
  percent: 0,
  // The power a parked car loses, tens of watts.
  W: 0,
  kW: 1,
  kWh: 1,
  kWhPer100km: 1,
  // A tyre's pressure, measured to the kPa.
  bar: 2,
  // A Tesla tells its temperatures to the half degree.
  celsius: 1,
  // A Polestar tells its current to the tenth of an ampere.
  ampere: 1,
  volt: 0,
}

// formatQuantity formats a value in its unit, in the locale, with at most digits
// decimals (by default, those of the unit); null stays null (unknown), never 0.
export function formatQuantity(
  value: number | null,
  unit: Unit,
  locale: string,
  digits = fractionDigits[unit],
): string | null {
  if (value === null) return null
  const options = { maximumFractionDigits: digits }
  const unitName = intlUnits[unit]
  if (unitName)
    return new Intl.NumberFormat(locale, { ...options, style: 'unit', unit: unitName }).format(
      value,
    )
  // A no-break space, as Intl puts between a number and its unit.
  return `${new Intl.NumberFormat(locale, options).format(value)}\u00a0${symbols[unit] ?? unit}`
}

// formatQuantityRange formats bounds in their unit as Intl writes a range ("44–52 km/h"),
// one value when both round alike: never formatRange on equal bounds ("~44 km/h").
export function formatQuantityRange(min: number, max: number, unit: Unit, locale: string): string {
  const options = { maximumFractionDigits: fractionDigits[unit] }
  const unitName = intlUnits[unit]
  const f = new Intl.NumberFormat(
    locale,
    unitName ? { ...options, style: 'unit', unit: unitName } : options,
  )
  const text = f.format(min) === f.format(max) ? f.format(min) : f.formatRange(min, max)
  return unitName ? text : `${text}\u00a0${symbols[unit] ?? unit}`
}

// kilowatts converts a power in W, as the API gives it, to kW, as it is shown.
export function kilowatts(watts: number): number {
  return watts / 1000
}

// formatSignedPercent shows a whole percent with its sign: "+3 %" or the locale's minus.
// Never clamped, never rounded further: a positive deviation tells a bias of the method
// as much as a negative one tells a loss.
export function formatSignedPercent(value: number, locale: string): string {
  const f = new Intl.NumberFormat(locale, { style: 'unit', unit: 'percent' })
  return value < 0 ? f.format(value) : `+${f.format(value)}`
}
