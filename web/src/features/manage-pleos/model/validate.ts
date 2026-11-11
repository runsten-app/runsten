import type { PleosBrand, PleosCredentialUpdate } from '@/entities/connection'

// The API's bounds of a key's values (PUT /connection/pleos/{brand}): change them with
// it. They only catch a paste gone wrong: the Pleos Playground checks the key itself.
export const credentialLimits = {
  client_id: { min: 4, max: 256 },
  client_secret: { min: 4, max: 512 },
} as const

export type Field = keyof typeof credentialLimits
export const fields: readonly Field[] = ['client_id', 'client_secret']

// fieldId is the element that shows a value's issue: the error summary leads there.
// Each brand has its own form, so its own fields.
export const fieldId = (brand: PleosBrand, f: Field) => `pleos-${brand}-${f.replace('_', '-')}`

export type Rule = 'required' | 'spaces' | 'characters' | 'tooShort' | 'tooLong'
export type Issue = { field: Field; rule: Rule; params?: Record<string, number> }
export type Draft = Record<Field, string>
export type Checked = { issues: Issue[]; body: PleosCredentialUpdate | null }

function check(field: Field, value: string): Issue | null {
  const { min, max } = credentialLimits[field]
  if (!value) return { field, rule: 'required' }
  if (/\s/.test(value)) return { field, rule: 'spaces' }
  if (!/^[!-~]+$/.test(value)) return { field, rule: 'characters' }
  if (value.length < min) return { field, rule: 'tooShort', params: { min } }
  if (value.length > max) return { field, rule: 'tooLong', params: { max } }
  return null
}

// validate checks the two values as pasted, with the API's rules (printable ASCII, no
// space, within credentialLimits), and builds the body to send: the spaces a copy adds
// around a value are dropped, those within it are an error.
export function validate(draft: Draft): Checked {
  const body = {
    client_id: draft.client_id.trim(),
    client_secret: draft.client_secret.trim(),
  }
  const issues = fields.map((f) => check(f, body[f])).filter((i): i is Issue => i !== null)
  return { issues, body: issues.length ? null : body }
}
