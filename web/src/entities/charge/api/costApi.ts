import { api, unwrap } from '@/shared/api'
import type { Charge, EnteredCost, OrphanCost } from '../model/Charge'

// setChargeCost enters what was paid for a charge: it replaces its tariff's cost.
export function setChargeCost(vehicle: string, id: string, cost: EnteredCost): Promise<Charge> {
  return unwrap(
    api.PUT('/vehicles/{vehicle}/charges/{id}/cost', {
      params: { path: { vehicle, id } },
      body: cost,
    }),
  )
}

export async function deleteChargeCost(vehicle: string, id: string): Promise<void> {
  await unwrap(
    api.DELETE('/vehicles/{vehicle}/charges/{id}/cost', { params: { path: { vehicle, id } } }),
  )
}

// fetchOrphanCosts lists the entered costs no charge takes any more, newest first.
export async function fetchOrphanCosts(): Promise<OrphanCost[]> {
  return (await unwrap(api.GET('/charge-costs/orphans'))).items
}

export function attachOrphanCost(id: string, vehicle: string, charge: string): Promise<Charge> {
  return unwrap(
    api.PUT('/charge-costs/orphans/{id}/charge', {
      params: { path: { id } },
      body: { vehicle, charge },
    }),
  )
}

export async function deleteOrphanCost(id: string): Promise<void> {
  await unwrap(api.DELETE('/charge-costs/orphans/{id}', { params: { path: { id } } }))
}
