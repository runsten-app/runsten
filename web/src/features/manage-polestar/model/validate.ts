import type { PolestarCredentialUpdate } from '@/entities/connection'

// The API's bounds of a credential's values (PUT /connection/polestar): change them with
// it. They only catch a paste gone wrong: the Data Portal checks the credential itself.
export const credentialLimits = {
  client_id: { min: 4, max: 256 },
  client_secret: { min: 4, max: 512 },
  account_id: { min: 4, max: 256 },
} as const

export type Field = keyof typeof credentialLimits
export const fields: readonly Field[] = ['client_id', 'client_secret', 'account_id']

// fieldId is the element that shows a value's issue: the error summary leads there.
export const fieldId = (f: Field) => `polestar-${f.replace('_', '-')}`

export type Rule = 'required' | 'spaces' | 'characters' | 'tooShort' | 'tooLong'
export type Issue = { field: Field; rule: Rule; params?: Record<string, number> }
export type Draft = Record<Field, string>
export type Checked = { issues: Issue[]; body: PolestarCredentialUpdate | null }

function check(field: Field, value: string): Issue | null {
  const { min, max } = credentialLimits[field]
  if (!value) return { field, rule: 'required' }
  if (/\s/.test(value)) return { field, rule: 'spaces' }
  if (!/^[!-~]+$/.test(value)) return { field, rule: 'characters' }
  if (value.length < min) return { field, rule: 'tooShort', params: { min } }
  if (value.length > max) return { field, rule: 'tooLong', params: { max } }
  return null
}

// validate checks the three values as pasted, with the API's rules (printable ASCII, no
// space, within credentialLimits), and builds the body to send: the spaces a copy adds
// around a value are dropped, those within it are an error.
export function validate(draft: Draft): Checked {
  const body = {
    client_id: draft.client_id.trim(),
    client_secret: draft.client_secret.trim(),
    account_id: draft.account_id.trim(),
  }
  const issues = fields.map((f) => check(f, body[f])).filter((i): i is Issue => i !== null)
  return { issues, body: issues.length ? null : body }
}
