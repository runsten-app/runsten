import type { components } from '@/shared/api'

export type Stats = components['schemas']['Stats']
export type PeriodStats = components['schemas']['PeriodStats']
export type Bucket = NonNullable<Stats['bucket']>
export type Parked = components['schemas']['Parked']
export type ParkedStop = components['schemas']['ParkedStop']
