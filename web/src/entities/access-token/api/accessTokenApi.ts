import { api, unwrap } from '@/shared/api'
import type { AccessToken, CreatedAccessToken, NewAccessToken } from '../model/AccessToken'

export async function fetchAccessTokens(): Promise<AccessToken[]> {
  return (await unwrap(api.GET('/tokens'))).items
}

export function createAccessToken(body: NewAccessToken): Promise<CreatedAccessToken> {
  return unwrap(api.POST('/tokens', { body }))
}

export async function revokeAccessToken(token: string): Promise<void> {
  await unwrap(api.DELETE('/tokens/{token}', { params: { path: { token } } }))
}
