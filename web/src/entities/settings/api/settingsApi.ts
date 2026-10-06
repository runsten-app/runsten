import { api, unwrap } from '@/shared/api'
import type { CurrencyCode, Settings } from '../model/Settings'

export function fetchSettings(): Promise<Settings> {
  return unwrap(api.GET('/settings'))
}

export function setCurrency(currency: CurrencyCode): Promise<Settings> {
  return unwrap(api.PUT('/settings', { body: { currency } }))
}
