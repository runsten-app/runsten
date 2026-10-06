import { api, unwrap } from '@/shared/api'
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
