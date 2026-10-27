import { api, unwrap } from '@/shared/api'
import type { ConnectionSettings, PolestarCredentialUpdate } from '../model/Connection'

export function fetchConnection(): Promise<ConnectionSettings> {
  return unwrap(api.GET('/connection'))
}

export function setApiKey(key: string): Promise<ConnectionSettings> {
  return unwrap(api.PUT('/connection/api-key', { body: { key } }))
}

export async function deleteApiKey(): Promise<void> {
  await unwrap(api.DELETE('/connection/api-key'))
}

export function setPolestarCredential(body: PolestarCredentialUpdate): Promise<ConnectionSettings> {
  return unwrap(api.PUT('/connection/polestar', { body }))
}

export async function deletePolestarCredential(): Promise<void> {
  await unwrap(api.DELETE('/connection/polestar'))
}

export function setTeslaToken(refreshToken: string): Promise<ConnectionSettings> {
  return unwrap(api.PUT('/connection/tesla', { body: { refresh_token: refreshToken } }))
}

export async function deleteTeslaToken(): Promise<void> {
  await unwrap(api.DELETE('/connection/tesla'))
}
