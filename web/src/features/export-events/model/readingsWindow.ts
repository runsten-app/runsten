// maxReadingsMs is the longest period of the readings' CSV file, as the API bounds it
// (maxReadingsWindow): 31 days, one of them 25 hours long. Change it with the API.
export const maxReadingsMs = (31 * 24 + 1) * 3_600_000

// readingsPeriod is the period of the readings' file, or undefined when the period is
// open at an end or longer than the API takes.
export function readingsPeriod(
  from?: string,
  to?: string,
): { from: string; to: string } | undefined {
  if (!from || !to) return undefined
  return Date.parse(to) - Date.parse(from) <= maxReadingsMs ? { from, to } : undefined
}
