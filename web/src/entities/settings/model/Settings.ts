import type { components } from '@/shared/api'

export type Settings = components['schemas']['Settings']
export type Currency = components['schemas']['Currency']
export type CurrencyCode = Currency['code']
// The API's limits of the writes, for a form to tell what is wrong before sending: the
// API checks them again.
export type Limits = components['schemas']['Limits']
export type EnteredCostLimits = components['schemas']['EnteredCostLimits']
// The efficiencies the API applies to a place without its own: shown to the user, who
// may replace them.
export type DefaultEfficiency = components['schemas']['DefaultEfficiency']
