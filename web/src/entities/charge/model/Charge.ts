import type { components } from '@/shared/api'

export type Charge = components['schemas']['Charge']
export type ChargePage = components['schemas']['PageCharge']
export type ChargeType = NonNullable<Charge['type']>
export type Cost = components['schemas']['Cost']
export type EnteredCost = components['schemas']['EnteredCost']
export type OrphanCost = components['schemas']['Orphan']
