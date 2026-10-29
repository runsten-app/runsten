import type { components } from '@/shared/api'

export type Charge = components['schemas']['Charge']
export type ChargePage = components['schemas']['PageCharge']
// A charge not over yet: not an event of the lists, read apart.
export type ChargeInProgress = components['schemas']['ChargeInProgress']
export type ChargeType = NonNullable<Charge['type']>
export type Cost = components['schemas']['Cost']
export type EnteredCost = components['schemas']['EnteredCost']
export type OrphanCost = components['schemas']['Orphan']
