import type { components } from '@/shared/api'

export type Stats = components['schemas']['Stats']
export type PeriodStats = components['schemas']['PeriodStats']
export type Bucket = NonNullable<Stats['bucket']>
