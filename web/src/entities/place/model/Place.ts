import type { components } from '@/shared/api'

export type Place = components['schemas']['Place']
export type PlaceFields = components['schemas']['PlaceFields']
export type TariffVersion = components['schemas']['TariffVersion']
export type PriceWindow = components['schemas']['PriceWindow']
export type UnpricedCharges = components['schemas']['UnpricedCharges']
export type Weekday = PriceWindow['days'][number]

// In the order of the week, Monday first as ISO 8601 has it.
export const weekdays: readonly Weekday[] = ['mon', 'tue', 'wed', 'thu', 'fri', 'sat', 'sun']
