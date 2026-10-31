import type { LocationQuery } from 'vue-router'

// ChargePlace is a group of the statistics' charges by place, as the API's place
// parameter names it: a place's ID, outside every place, or without a position.
export type ChargePlace = string

const pattern =
  /^(outside|no_position|[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12})$/

// placeQuery picks the place of the charges out of a route's query, to carry it with the
// period; one the API would refuse is none. An ID is lowercased, as the API gives them.
export function placeQuery(query: LocationQuery): { place?: ChargePlace } {
  const p = query.place
  return typeof p === 'string' && pattern.test(p) ? { place: p.toLowerCase() } : {}
}
