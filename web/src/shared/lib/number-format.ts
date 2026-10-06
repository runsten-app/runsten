// The units Runsten shows. Intl formats kilometers and percents itself; it has no watt or
// watt-hour unit, so kW, kWh and kWh/100 km are a formatted number and a symbol.
export type Unit = 'km' | 'percent' | 'kW' | 'kWh' | 'kWhPer100km'

const intlUnits: Partial<Record<Unit, string>> = { km: 'kilometer', percent: 'percent' }
const symbols: Partial<Record<Unit, string>> = { kWhPer100km: 'kWh/100\u00a0km' }
const fractionDigits: Record<Unit, number> = {
  km: 0,
  percent: 0,
  kW: 1,
  kWh: 1,
  kWhPer100km: 1,
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
