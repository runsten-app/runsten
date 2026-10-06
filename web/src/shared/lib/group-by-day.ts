import { dayKey } from './event-time'

// day is the key of the day (YYYY-MM-DD), at a time within it to name it by.
export type DayGroup<T> = { day: string; at: string; items: T[] }

// groupByDay splits events, newest first, by the day each started on (start.after, in the
// reader's time zone), keeping their order.
export function groupByDay<T extends { start: { after: string } }>(
  items: readonly T[],
  timeZone?: string,
): DayGroup<T>[] {
  const groups: DayGroup<T>[] = []
  for (const item of items) {
    const day = dayKey(item.start.after, timeZone)
    const last = groups.at(-1)
    if (last?.day === day) last.items.push(item)
    else groups.push({ day, at: item.start.after, items: [item] })
  }
  return groups
}
