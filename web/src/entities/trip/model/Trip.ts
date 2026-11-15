import type { components } from '@/shared/api'

export type Trip = components['schemas']['Trip']
export type TripPage = components['schemas']['PageTrip']
// TripTrack is the path of an imported trip, a GeoJSON LineString: longitude, latitude.
export type TripTrack = components['schemas']['Track']
// TripMap is where the vehicle went over a period: the trips' ends gathered into spots,
// and the links between them.
export type TripMap = components['schemas']['TripMap']

// Purpose is what a trip was made for. Commute stands apart from business: each country
// counts the way to work its own way.
export type Purpose = NonNullable<Trip['purpose']>
export const purposes: readonly Purpose[] = ['business', 'commute', 'personal']
// PurposeFilter keeps the trips of a purpose, or those without one.
export type PurposeFilter = Purpose | 'none'
// Mark is what a write marks trips with: a purpose, none by hand, or back to the rules.
export type Mark = Purpose | 'none' | 'rule'
