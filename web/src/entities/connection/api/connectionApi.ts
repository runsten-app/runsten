import { api, unwrap } from '@/shared/api'
import type {
  BmwDevice,
  ConnectionSettings,
  PleosBrand,
  PleosCredentialUpdate,
  PolestarCredentialUpdate,
  PolestarIdAuthorization,
  SkodaCredentialUpdate,
} from '../model/Connection'

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

export function startPolestarIdSignIn(): Promise<PolestarIdAuthorization> {
  return unwrap(api.POST('/connection/polestar-id/authorization'))
}

export function setPolestarIdCode(state: string, callback: string): Promise<ConnectionSettings> {
  return unwrap(api.PUT('/connection/polestar-id/code', { body: { state, callback } }))
}

export function setPolestarIdPassword(
  email: string,
  password: string,
): Promise<ConnectionSettings> {
  return unwrap(api.PUT('/connection/polestar-id/password', { body: { email, password } }))
}

export async function deletePolestarId(): Promise<void> {
  await unwrap(api.DELETE('/connection/polestar-id'))
}

export function setPleosCredential(
  brand: PleosBrand,
  body: PleosCredentialUpdate,
): Promise<ConnectionSettings> {
  return unwrap(api.PUT('/connection/pleos/{brand}', { params: { path: { brand } }, body }))
}

export async function deletePleosCredential(brand: PleosBrand): Promise<void> {
  await unwrap(api.DELETE('/connection/pleos/{brand}', { params: { path: { brand } } }))
}

export function setSkodaCredential(body: SkodaCredentialUpdate): Promise<ConnectionSettings> {
  return unwrap(api.PUT('/connection/skoda', { body }))
}

export async function deleteSkodaCredential(): Promise<void> {
  await unwrap(api.DELETE('/connection/skoda'))
}

export function startBmwAuthorization(clientId: string): Promise<BmwDevice> {
  return unwrap(api.POST('/connection/bmw/authorization', { body: { client_id: clientId } }))
}

export function pollBmwToken(): Promise<ConnectionSettings> {
  return unwrap(api.POST('/connection/bmw/token'))
}

export async function deleteBmw(): Promise<void> {
  await unwrap(api.DELETE('/connection/bmw'))
}
