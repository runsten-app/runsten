// FigureParts is a formatted quantity cut into its number and its unit, the unit with the
// space before it: shown apart (the unit smaller), they read the same as the whole.
export type FigureParts = { value: string; unit: string }

// A number as Intl writes it (grouping by spaces, points or commas, a sign), then a unit
// with no figure of its own but a "per 100": "80 %", "13 119 km", "20.7 kWh/100 km",
// "0.4 % per day". Anything else stays whole: an amount ("€10.78"), a duration of two
// units ("1 h 20 min"), a range, a word ("Unknown").
const pattern =
  /^([+\-\u2212]?\d(?:[\d.,\u00a0\u202f ]*\d)?)([\u00a0\u202f ]?[^\d\s][^\d]*(?:100[\u00a0\u202f ]?km)?)$/u

// figureParts cuts a formatted quantity; null when it is not one number and its unit.
export function figureParts(text: string): FigureParts | null {
  const m = pattern.exec(text)
  if (!m?.[1] || !m[2]) return null
  return { value: m[1], unit: m[2] }
}
