const decimal = /^[+-]?(\d+([.,]\d*)?|[.,]\d+)$/

// parseDecimal reads a number typed with a decimal point or a comma, whatever the
// language: "0,2142" and "0.2142" alike. Anything else, blank included, is null.
export function parseDecimal(s: string): number | null {
  const t = s.trim()
  if (!decimal.test(t)) return null
  return Number(t.replace(',', '.'))
}

// decimalPlaces counts the decimals of a number as typed, trailing zeros left out:
// "0.21420" has 4, as the API counts on the shortest writing of the number.
export function decimalPlaces(s: string): number {
  const fraction = s.trim().split(/[.,]/)[1] ?? ''
  return fraction.replace(/0+$/, '').length
}
