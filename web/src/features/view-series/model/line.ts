import type { SeriesReading, SeriesRun } from '@/entities/series'

// A point of a line over time: x in milliseconds, y null where nothing is known.
export type TimePoint = { x: number; y: number | null }

// seriesLine is one value of the readings as a line: every reading a point, and a hole
// between two runs, where nothing was read, so that the line breaks there rather than
// join them. A reading that lacks the value is a hole too.
export function seriesLine(
  runs: SeriesRun[],
  value: (r: SeriesReading) => number | null,
): TimePoint[] {
  const out: TimePoint[] = []
  runs.forEach((run, i) => {
    const [first] = run.readings
    if (i > 0 && first) out.push({ x: Date.parse(first.at) - 1, y: null })
    for (const r of run.readings) out.push({ x: Date.parse(r.at), y: value(r) })
  })
  return out
}

// splitRuns breaks the runs at an instant: the readings before it and those from it on
// fall in two runs, so that no line joins them. A charge's curve breaks so at its first
// reading charging: the reading before is the vehicle not yet charging, and a line from
// it would draw a ramp that was never read.
export function splitRuns(runs: SeriesRun[], at: string): SeriesRun[] {
  const cut = Date.parse(at)
  return runs.flatMap((run) => {
    const i = run.readings.findIndex((r) => Date.parse(r.at) >= cut)
    if (i <= 0) return [run]
    return [{ readings: run.readings.slice(0, i) }, { readings: run.readings.slice(i) }]
  })
}

// readingsOf lists the readings of every run, oldest first.
export function readingsOf(runs: SeriesRun[]): SeriesReading[] {
  return runs.flatMap((r) => r.readings)
}
