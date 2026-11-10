import { api, unwrap } from '@/shared/api'
import type { Session } from '../model/Session'
import type { AccountExport } from '../model/AccountExport'

export function fetchSession(): Promise<Session> {
  return unwrap(api.GET('/session'))
}

export function signIn(username: string, password: string): Promise<Session> {
  return unwrap(api.POST('/session', { body: { username, password } }))
}

export async function signOut(): Promise<void> {
  await unwrap(api.DELETE('/session'))
}

// exportZipPath is where the browser downloads the account's export as the ZIP
// archive the export worker built: a link, not a fetch, so that the browser saves the
// file as it comes; held once built.
export const exportZipPath = 'api/v1/account/export.zip'

// fetchAccountExport reads the account's export; null when it has none.
export async function fetchAccountExport(): Promise<AccountExport | null> {
  return (await unwrap(api.GET('/account/export-job'))).export
}

// startAccountExport builds the account's export as a ZIP archive, in the background.
export async function startAccountExport(): Promise<AccountExport> {
  return unwrap(api.POST('/account/export-job'))
}
