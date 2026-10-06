const minute = 60_000
const day = 24 * 60 * minute

// The steps a time axis is graduated by, in minutes: those a clock reads.
const steps = [5, 10, 15, 30, 60, 120, 180, 360, 720]

// timeTicks graduates [from, to] (milliseconds) with at most max ticks, on round times of
// the reader's day ("18:30") or, beyond twelve hours a step, on their midnights. Local
// arithmetic, never a sum of milliseconds: a day of a change of time lasts 23 or 25
// hours, and the ticks stay on the clock's round times across it.
export function timeTicks(from: number, to: number, max = 8): { ticks: number[]; daily: boolean } {
  const span = to - from
  const step = steps.find((s) => span / (s * minute) <= max)
  const d = new Date(from)
  d.setHours(0, 0, 0, 0)
  const ticks: number[] = []
  if (step === undefined) {
    const every = Math.max(1, Math.ceil(span / day / max))
    if (d.getTime() < from) d.setDate(d.getDate() + 1)
    for (; d.getTime() <= to; d.setDate(d.getDate() + every)) ticks.push(d.getTime())
    return { ticks, daily: true }
  }
  // From the midnight before from, to the first round time at or after it.
  while (d.getTime() < from) d.setMinutes(d.getMinutes() + step)
  for (; d.getTime() <= to; d.setMinutes(d.getMinutes() + step)) ticks.push(d.getTime())
  return { ticks, daily: false }
}
