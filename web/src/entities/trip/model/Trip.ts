import type { components } from '@/shared/api'

export type Trip = components['schemas']['Trip']
export type TripPage = components['schemas']['PageTrip']
// TripTrack is the path of an imported trip, a GeoJSON LineString: longitude, latitude.
export type TripTrack = components['schemas']['Track']
