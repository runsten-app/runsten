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

// readingsOf lists the readings of every run, oldest first.
export function readingsOf(runs: SeriesRun[]): SeriesReading[] {
  return runs.flatMap((r) => r.readings)
}
