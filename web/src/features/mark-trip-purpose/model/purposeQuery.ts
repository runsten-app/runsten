import type { LocationQuery } from 'vue-router'
import { purposes, type PurposeFilter } from '@/entities/trip'

// purposeQuery picks the purpose of the trips listed out of a route's query: one of the
// purposes, or none; anything else is no filter.
export function purposeQuery(query: LocationQuery): PurposeFilter | undefined {
  const v = query.purpose
  if (typeof v !== 'string') return undefined
  return v === 'none' || (purposes as readonly string[]).includes(v)
    ? (v as PurposeFilter)
    : undefined
}
