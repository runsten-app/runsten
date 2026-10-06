import { api, unwrap } from '@/shared/api'
import type { ConnectionSettings } from '../model/Connection'

export function fetchConnection(): Promise<ConnectionSettings> {
  return unwrap(api.GET('/connection'))
}

export function setApiKey(key: string): Promise<ConnectionSettings> {
  return unwrap(api.PUT('/connection/api-key', { body: { key } }))
}

export async function deleteApiKey(): Promise<void> {
  await unwrap(api.DELETE('/connection/api-key'))
}
