import type { TyreReading, TyreRun } from '@/entities/health'

// A point of a line over time: x in milliseconds, y null where nothing is known.
export type TimePoint = { x: number; y: number | null }

// tyreLine is one value of the readings as a line, as the energy states' series: every
// reading a point, and a hole between two runs, where nothing was read, so that the line
// breaks there. A reading that lacks the value is a hole too.
export function tyreLine(runs: TyreRun[], value: (r: TyreReading) => number | null): TimePoint[] {
  const out: TimePoint[] = []
  runs.forEach((run, i) => {
    const [first] = run.readings
    if (i > 0 && first) out.push({ x: Date.parse(first.at) - 1, y: null })
    for (const r of run.readings) out.push({ x: Date.parse(r.at), y: value(r) })
  })
  return out
}
