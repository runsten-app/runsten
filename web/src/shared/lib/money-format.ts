import type { CurrencyUnit } from './price-format'

// Amounts are the bounds of a cost, in minor units of the account's currency (cents), as
// the API gives them: min rounded down, max up. Equal when the cost is a single amount.
export type Amounts = { min_minor: number; max_minor: number }

// minorDigits is the digits of a currency's minor unit, as Intl knows them: the API's
// list of currencies agrees with it (tested), and a charge's cost names its currency by
// its code alone.
export function minorDigits(code: string): number {
  const f = new Intl.NumberFormat('en', { style: 'currency', currency: code })
  return f.resolvedOptions().maximumFractionDigits ?? 2
}

export function currencyUnit(code: string): CurrencyUnit {
  return { code, minor_digits: minorDigits(code) }
}

// majorUnits is the only arithmetic on an amount shown: the API has already rounded it,
// and Intl gets exactly the currency's digits, so nothing is rounded again.
export function majorUnits(minor: number, digits: number): number {
  return minor / 10 ** digits
}

function currencyFormat(c: CurrencyUnit, locale: string): Intl.NumberFormat {
  return new Intl.NumberFormat(locale, {
    style: 'currency',
    currency: c.code,
    minimumFractionDigits: c.minor_digits,
    maximumFractionDigits: c.minor_digits,
  })
}

// formatMajor shows an amount already in major units, such as a value of a chart.
export function formatMajor(major: number, c: CurrencyUnit, locale: string): string {
  return currencyFormat(c, locale).format(major)
}

// formatAmount shows an amount in minor units ("€5.24", "5,24 €"); 0 is "€0.00".
export function formatAmount(minor: number, c: CurrencyUnit, locale: string): string {
  return formatMajor(majorUnits(minor, c.minor_digits), c, locale)
}

// formatAmounts shows the bounds of a cost: one amount when they are equal, else a range
// as the language writes it ("€5.24 – €5.79", "5,24–5,79 €"). min and max are each bound
// on its own, for the words a screen reader is given instead of a dash.
export function formatAmounts(
  a: Amounts,
  c: CurrencyUnit,
  locale: string,
): { text: string; min: string; max: string } {
  const f = currencyFormat(c, locale)
  const [lo, hi] = [
    majorUnits(a.min_minor, c.minor_digits),
    majorUnits(a.max_minor, c.minor_digits),
  ]
  const [min, max] = [f.format(lo), f.format(hi)]
  // formatRange writes equal bounds as an approximation ("~€5.24"): an exact one here.
  return { text: lo === hi ? min : f.formatRange(lo, hi), min, max }
}

// parseAmount reads an amount typed in major units, with a decimal point or a comma
// ("12,34", "12.3", "12"), as an integer of minor units, without floating point. null if
// it is not a plain positive number, or has more decimals than the currency.
export function parseAmount(s: string, digits: number): number | null {
  const m = /^(\d+)(?:[.,](\d*))?$/.exec(s.trim())
  if (!m) return null
  const fraction = (m[2] ?? '').replace(/0+$/, '')
  if (fraction.length > digits) return null
  return Number(m[1]) * 10 ** digits + Number(fraction.padEnd(digits, '0') || '0')
}

// amountInput writes an amount of minor units as a field shows it, with a decimal point:
// the fields read either.
export function amountInput(minor: number, digits: number): string {
  if (!digits) return String(minor)
  const s = String(minor).padStart(digits + 1, '0')
  return `${s.slice(0, -digits)}.${s.slice(-digits)}`
}
