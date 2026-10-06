import type { TariffVersion } from './Place'

// currentVersion is the version of a tariff that holds on day (YYYY-MM-DD, in the place's
// time zone): the latest one from that day or before. null when every version is yet to
// come.
export function currentVersion(tariff: readonly TariffVersion[], day: string) {
  let current: TariffVersion | null = null
  for (const v of tariff)
    if (v.valid_from <= day && (!current || v.valid_from > current.valid_from)) current = v
  return current
}

// upcomingVersion is the first version to come after day, if any.
export function upcomingVersion(tariff: readonly TariffVersion[], day: string) {
  let next: TariffVersion | null = null
  for (const v of tariff)
    if (v.valid_from > day && (!next || v.valid_from < next.valid_from)) next = v
  return next
}
