import type { components } from '@/shared/api'

// The energy states a vehicle gave over a window, as the API reads them: an alias of each
// generated schema, never a type written by hand.
export type Series = components['schemas']['Series']
export type SeriesRun = components['schemas']['SeriesRun']
export type SeriesReading = components['schemas']['SeriesReading']
