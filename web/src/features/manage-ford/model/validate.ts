// The API's bounds of a Ford sign-in (POST /connection/ford/authorization, PUT
// /connection/ford/code): change them with it. They only catch a paste gone wrong: Ford
// checks the rest.
export const limits = { clientId: [8, 128], clientSecret: [8, 512], callback: 8192 } as const

// The fields: the error summary leads there.
export const clientIdId = 'ford-client-id'
export const clientSecretId = 'ford-client-secret'
export const callbackId = 'ford-callback'

export type Rule = 'required' | 'tooShort' | 'tooLong' | 'characters' | 'noCode'
export type Issue = { field: string; rule: Rule; params?: Record<string, number> }

// A client ID and a secret are printable ASCII without spaces, as the API asks.
const printable = /^[!-~]+$/

function check(field: string, value: string, [min, max]: readonly [number, number]): Issue | null {
  if (!value) return { field, rule: 'required' }
  if (!printable.test(value)) return { field, rule: 'characters' }
  if (value.length < min) return { field, rule: 'tooShort', params: { min } }
  if (value.length > max) return { field, rule: 'tooLong', params: { max } }
  return null
}

// validateApplication checks the client ID and the secret of the owner's application. The
// spaces a copy adds around them are dropped.
export function validateApplication(
  clientId: string,
  clientSecret: string,
): { issues: Issue[]; app: { clientId: string; clientSecret: string } | null } {
  const app = { clientId: clientId.trim(), clientSecret: clientSecret.trim() }
  const issues = [
    check(clientIdId, app.clientId, limits.clientId),
    check(clientSecretId, app.clientSecret, limits.clientSecret),
  ].filter((i): i is Issue => i !== null)
  return { issues, app: issues.length ? null : app }
}

// validateCallback checks the address pasted back after the sign-in: the one Ford sent
// the browser to, with its code, or the code alone. The spaces a copy adds around it are
// dropped.
export function validateCallback(draft: string): { issues: Issue[]; callback: string | null } {
  const callback = draft.trim()
  const field = callbackId
  let issue: Issue | null = null
  if (!callback) issue = { field, rule: 'required' }
  else if (callback.length > limits.callback)
    issue = { field, rule: 'tooLong', params: { max: limits.callback } }
  else if (!hasCode(callback)) issue = { field, rule: 'noCode' }
  return { issues: issue ? [issue] : [], callback: issue ? null : callback }
}

function hasCode(s: string): boolean {
  if (!/[?=/&]/.test(s)) return !/\s/.test(s)
  try {
    return !!new URL(s).searchParams.get('code')
  } catch {
    return false
  }
}
