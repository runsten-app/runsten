import { api, unwrap } from '@/shared/api'
import type { Battery } from '../model/Battery'

// BatteryQuery is the IANA time zone in which the months of the history are split.
export type BatteryQuery = { tz: string }

export function getBattery(vehicle: string, query: BatteryQuery): Promise<Battery> {
  return unwrap(api.GET('/vehicles/{vehicle}/battery', { params: { path: { vehicle }, query } }))
}
