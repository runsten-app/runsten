import type { components } from '@/shared/api'

// AccessToken is a personal access token of the user: a program reads the API with it,
// without a session. Its secret only comes back once, when it is issued
// (CreatedAccessToken).
export type AccessToken = components['schemas']['AccessToken']
export type CreatedAccessToken = components['schemas']['CreatedAccessToken']
export type NewAccessToken = components['schemas']['NewAccessToken']
export type Expiry = NewAccessToken['expiry']

export const accessTokenKeys = {
  list: () => ['accessTokens'] as const,
}
