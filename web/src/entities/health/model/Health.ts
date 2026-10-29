import type { components } from '@/shared/api'

// The health of a vehicle as the API reads it: an alias of each generated schema, never a
// type written by hand.
export type Health = components['schemas']['Health']
export type Tyres = components['schemas']['Tyres']
export type Tyre = components['schemas']['Tyre']
export type Service = components['schemas']['Service']
export type Check = components['schemas']['Check']
export type TyreSeries = components['schemas']['TyreSeries']
export type TyreRun = components['schemas']['TyreRun']
export type TyreReading = components['schemas']['TyreReading']

export type Wheel = Tyre['wheel']
export type TyreWarning = NonNullable<Tyre['warning']>
export type ServiceState = NonNullable<Service['state']>
export type CheckName = Check['check']

// SoftwareVersion is a version of the vehicle's software, from when it was first read.
export type SoftwareVersion = components['schemas']['SoftwareVersion']
export type SoftwareVersionPage = components['schemas']['PageSoftwareVersion']
