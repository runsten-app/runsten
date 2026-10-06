import { api, unwrap } from '@/shared/api'
import type { Session } from '../model/Session'

export function fetchSession(): Promise<Session> {
  return unwrap(api.GET('/session'))
}

export function signIn(username: string, password: string): Promise<Session> {
  return unwrap(api.POST('/session', { body: { username, password } }))
}

export async function signOut(): Promise<void> {
  await unwrap(api.DELETE('/session'))
}

// exportPath is where the browser downloads the account's export, relative to the
// document like every call: a link, not a fetch, so that the browser saves the file
// as it comes.
export const exportPath = 'api/v1/account/export'
