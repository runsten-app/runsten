import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { intlLocale, isLocale } from '@/shared/i18n'
import {
  formatCalendarDay,
  formatCalendarMonth,
  formatCalendarRange,
  monthName,
  weekdayName,
} from './calendar-names'
import { formatCoordinates, type Position } from './coordinates'
import { formatAge, formatBucket, formatDateTime } from './date-format'
import {
  dayKey,
  formatBounds,
  formatDay,
  formatDuration,
  formatTime,
  formatTotalDuration,
  honestDuration,
  totalDurationBounds,
  type Bounds,
} from './event-time'
import {
  currencyUnit,
  formatAmount,
  formatAmounts,
  formatMajor,
  type Amounts,
} from './money-format'
import { formatQuantity, formatSignedPercent, type Unit } from './number-format'
import { currencyName, currencySymbol, formatPricePerKwh, type CurrencyUnit } from './price-format'
import type { Bucket } from './stats-period'

// Spoken is a text, and the words a screen reader is given for it when they differ: a
// range of amounts is read "from €5.24 to €5.79", not "5.24 dash 5.79".
export type Spoken = { text: string; spoken: string }

// ChargeCost is what a charge's cost needs to be shown: the API's, its currency a code.
type ChargeCost = Amounts & { currency: string }

// useFormat binds the formats to the chosen language, and shows a missing value as
// "Unknown": never 0, never blank.
export function useFormat() {
  const { t, locale } = useI18n()
  const intl = computed(() =>
    isLocale(locale.value) ? intlLocale(locale.value, navigator.languages) : locale.value,
  )
  const unknown = () => t('common.unknown')
  // amounts shows the bounds of a cost; unknown without them or without a currency.
  function amounts(a: Amounts | null, c: CurrencyUnit | null): Spoken {
    if (!a || !c) return { text: unknown(), spoken: unknown() }
    const { text, min, max } = formatAmounts(a, c, intl.value)
    return { text, spoken: min === max ? text : t('cost.range', { min, max }) }
  }
  return {
    unknown,
    quantity: (v: number | null | undefined, unit: Unit, digits?: number) =>
      formatQuantity(v ?? null, unit, intl.value, digits) ?? unknown(),
    age: (iso: string, now: number) => formatAge(iso, now, intl.value),
    dateTime: (iso: string, now: number) => formatDateTime(iso, now, intl.value),
    coordinates: (p: Position | null | undefined) => (p ? formatCoordinates(p) : unknown()),
    // change shows a value before and after ("62% → 54.5%"), each unknown on its own.
    change: (from: number | null, to: number | null, unit: Unit) =>
      from === null && to === null
        ? unknown()
        : `${formatQuantity(from, unit, intl.value) ?? unknown()} → ${formatQuantity(to, unit, intl.value) ?? unknown()}`,
    day: (iso: string) => formatDay(iso, intl.value),
    dayKey: (iso: string) => dayKey(iso),
    bounds: (b: Bounds, day?: string) => formatBounds(b, intl.value, day),
    time: (iso: string, day?: string) => formatTime(iso, intl.value, day),
    duration: (start: Bounds, end: Bounds) =>
      formatDuration(honestDuration(start, end), intl.value),
    // totalDuration shows a sum of durations, in seconds as the API gives it.
    totalDuration: (s: { min: number; max: number }) =>
      formatTotalDuration({ min: s.min * 1000, max: s.max * 1000 }, intl.value),
    // totalDurationBounds shows the same sum as its two bounds, null when they are one.
    totalDurationBounds: (s: { min: number; max: number }) => {
      const b = totalDurationBounds({ min: s.min * 1000, max: s.max * 1000 })
      const one = (ms: number) => formatTotalDuration({ min: ms, max: ms }, intl.value)
      return b && { min: one(b.min), max: one(b.max) }
    },
    bucket: (iso: string, b: Bucket) => formatBucket(iso, b, intl.value),
    // calendarDay shows a day of the calendar (YYYY-MM-DD), such as a tariff's valid_from.
    calendarDay: (day: string) => formatCalendarDay(day, intl.value),
    // calendarMonth names the month of a day; calendarRange shows two days as one range.
    calendarMonth: (day: string) => formatCalendarMonth(day, intl.value),
    calendarRange: (from: string, to: string) => formatCalendarRange(from, to, intl.value),
    // number shows a count or an index, with at most digits decimals (the cycles).
    number: (v: number, digits?: number) =>
      new Intl.NumberFormat(
        intl.value,
        digits === undefined ? undefined : { maximumFractionDigits: digits },
      ).format(v),
    // signedPercent shows a whole percent with its sign, never clamped: a bias of the
    // method shows as one.
    signedPercent: (v: number | null) =>
      v === null ? unknown() : formatSignedPercent(v, intl.value),
    price: (v: number, c: CurrencyUnit | null) => formatPricePerKwh(v, c, intl.value),
    // amount shows an amount in minor units; majorAmount one in major units (a chart's).
    amount: (minor: number, c: CurrencyUnit) => formatAmount(minor, c, intl.value),
    majorAmount: (major: number, c: CurrencyUnit) => formatMajor(major, c, intl.value),
    amounts,
    // chargeCost shows the cost of a charge. Exactly 0 is free (an entered 0, or a tariff
    // at 0 over the whole charge): a true zero, not an unknown. Sums stay amounts.
    chargeCost: (c: ChargeCost | null): Spoken => {
      if (c && c.max_minor === 0) return { text: t('cost.free'), spoken: t('cost.free') }
      return amounts(c, c && currencyUnit(c.currency))
    },
    currencyName: (code: string) => currencyName(code, intl.value),
    currencySymbol: (code: string) => currencySymbol(code, intl.value),
    // weekday names a day of the week, 0 for Monday; month a month, 1 to 12.
    weekday: (i: number, style?: 'long' | 'short') => weekdayName(i, intl.value, style),
    month: (m: number, style?: 'long' | 'short') => monthName(m, intl.value, style),
  }
}
