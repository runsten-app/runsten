import { api, downloadPath, unwrap } from '@/shared/api'
import type { ModelChoice, State, VariantOption, Vehicle } from '../model/Vehicle'

export async function fetchVehicles(): Promise<Vehicle[]> {
  return (await unwrap(api.GET('/vehicles'))).items
}

export function fetchVehicle(vehicle: string): Promise<Vehicle> {
  return unwrap(api.GET('/vehicles/{vehicle}', { params: { path: { vehicle } } }))
}

export function fetchVehicleState(vehicle: string): Promise<State> {
  return unwrap(api.GET('/vehicles/{vehicle}/state', { params: { path: { vehicle } } }))
}

export async function fetchVariants(vehicle: string): Promise<VariantOption[]> {
  return (await unwrap(api.GET('/vehicles/{vehicle}/variants', { params: { path: { vehicle } } })))
    .items
}

export function setVehicleModel(vehicle: string, choice: ModelChoice): Promise<Vehicle> {
  return unwrap(
    api.PUT('/vehicles/{vehicle}/model', { params: { path: { vehicle } }, body: choice }),
  )
}

// deleteVehicle deletes the vehicle with its whole history; its connection stays.
export async function deleteVehicle(vehicle: string): Promise<void> {
  await unwrap(api.DELETE('/vehicles/{vehicle}', { params: { path: { vehicle } } }))
}

// vehiclePicturePath is where the browser loads the vehicle's picture: the API relays it,
// so that the browser calls no third party.
export function vehiclePicturePath(vehicle: string): string {
  return downloadPath(`/vehicles/${encodeURIComponent(vehicle)}/picture`)
}
