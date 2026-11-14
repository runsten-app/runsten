import type { RenaultSignInBody } from '@/entities/connection'

// The API's bounds of a sign-in (POST /connection/renault/sign-in): change them with it.
// They only catch a typing gone wrong: Renault checks the rest.
export const limits = { email: 254, password: 256 } as const

// The fields: the error summary leads there.
export const emailId = 'renault-email'
export const passwordId = 'renault-password'
export const codeId = 'renault-code'

export type Rule = 'required' | 'email' | 'tooLong' | 'codeFormat'
export type Issue = { field: string; rule: Rule; params?: Record<string, number> }

// validateSignIn checks the email and the password, as the API does, and builds the body
// to send (the language is the form's). The password is taken as typed: a space may be
// part of it.
export function validateSignIn(
  email: string,
  password: string,
): { issues: Issue[]; body: Pick<RenaultSignInBody, 'email' | 'password'> | null } {
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

// validateCode checks the one-time code: 4 to 8 digits, as Renault's email shows it. The
// spaces a copy adds, around it or between its digits, are dropped.
export function validateCode(draft: string): { issues: Issue[]; code: string | null } {
  const code = draft.replace(/\s+/g, '')
  if (!code) return { issues: [{ field: codeId, rule: 'required' }], code: null }
  if (!/^[0-9]{4,8}$/.test(code))
    return { issues: [{ field: codeId, rule: 'codeFormat' }], code: null }
  return { issues: [], code }
}
