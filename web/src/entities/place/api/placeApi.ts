import { api, unwrap } from '@/shared/api'
import type { Place, PlaceFields, UnpricedCharges } from '../model/Place'

export async function fetchPlaces(): Promise<Place[]> {
  return (await unwrap(api.GET('/places'))).items
}

export function fetchPlace(place: string): Promise<Place> {
  return unwrap(api.GET('/places/{place}', { params: { path: { place } } }))
}

export function createPlace(fields: PlaceFields): Promise<Place> {
  return unwrap(api.POST('/places', { body: fields }))
}

export function replacePlace(place: string, fields: PlaceFields): Promise<Place> {
  return unwrap(api.PUT('/places/{place}', { params: { path: { place } }, body: fields }))
}

export async function deletePlace(place: string): Promise<void> {
  await unwrap(api.DELETE('/places/{place}', { params: { path: { place } } }))
}

export function fetchUnpricedCharges(place: string): Promise<UnpricedCharges> {
  return unwrap(api.GET('/places/{place}/unpriced', { params: { path: { place } } }))
}
