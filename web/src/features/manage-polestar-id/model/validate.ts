// The API's bounds of a connection of the Polestar ID (PUT /connection/polestar-id/…):
// change them with it. They only catch a paste or a typing gone wrong: the Polestar ID
// checks the rest.
export const limits = { callback: 4096, email: 320, password: 1024 } as const

// The fields: the error summary leads there.
export const callbackId = 'polestar-id-callback'
export const emailId = 'polestar-id-email'
export const passwordId = 'polestar-id-password'
// The button that starts a sign-in on Polestar's page, before its address is pasted.
export const startId = 'polestar-id-start'

export type Rule = 'required' | 'noCode' | 'tooLong' | 'email' | 'start'
export type Issue = { field: string; rule: Rule; params?: Record<string, number> }

// validateCallback checks the address pasted back after the sign-in: polestar.com's
// sign-in-callback with its code, or the code alone. The spaces a copy adds around it are
// dropped. started: a sign-in was started on Polestar's page, whose state goes with it.
export function validateCallback(
  draft: string,
  started: boolean,
): { issues: Issue[]; callback: string | null } {
  const callback = draft.trim()
  const field = callbackId
  const issues: Issue[] = started ? [] : [{ field: startId, rule: 'start' }]
  let issue: Issue | null = null
  if (!callback) issue = { field, rule: 'required' }
  else if (callback.length > limits.callback)
    issue = { field, rule: 'tooLong', params: { max: limits.callback } }
  else if (!hasCode(callback)) issue = { field, rule: 'noCode' }
  if (issue) issues.push(issue)
  return { issues, callback: issues.length ? null : callback }
}

function hasCode(s: string): boolean {
  if (!/[?=/]/.test(s)) return !/\s/.test(s)
  try {
    return !!new URL(s).searchParams.get('code')
  } catch {
    return false
  }
}

// validatePassword checks the email and the password, as the API does. The password is
// taken as typed: a space may be part of it.
export function validatePassword(
  email: string,
  password: string,
): { issues: Issue[]; body: { email: string; password: string } | null } {
  const e = email.trim()
  const issues: Issue[] = []
  if (!e) issues.push({ field: emailId, rule: 'required' })
  else if (e.length > limits.email)
    issues.push({ field: emailId, rule: 'tooLong', params: { max: limits.email } })
  else if (!/^[^\s@]+@[^\s@]+$/.test(e)) issues.push({ field: emailId, rule: 'email' })
  if (!password) issues.push({ field: passwordId, rule: 'required' })
  else if (password.length > limits.password)
    issues.push({ field: passwordId, rule: 'tooLong', params: { max: limits.password } })
  return { issues, body: issues.length ? null : { email: e, password } }
}
