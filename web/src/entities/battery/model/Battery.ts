import type { components } from '@/shared/api'

// The estimated capacity of a vehicle's battery, as the API computes it from its
// charges: an alias of each generated schema, never a type written by hand.
export type Battery = components['schemas']['Battery']
export type CapacityEstimate = components['schemas']['CapacityEstimate']
export type CapacityMonth = components['schemas']['CapacityMonth']
export type CapacityQuartiles = components['schemas']['CapacityQuartiles']
export type CapacityReference = components['schemas']['CapacityReference']
export type CapacityChange = components['schemas']['CapacityChange']
export type CapacityExcluded = components['schemas']['CapacityExcluded']
export type RangeAtFull = components['schemas']['RangeAtFull']
// The sources of an estimate and of a reference capacity, kept apart as the API gives
// them: the two estimates of one charge are two points, never averaged.
export type EstimateSource = CapacityEstimate['source']
export type CapacitySource = CapacityReference['source']
