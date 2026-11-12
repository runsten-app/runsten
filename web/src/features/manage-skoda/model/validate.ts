import type { SkodaCredentialUpdate } from '@/entities/connection'

// The API's bounds of a key and of its VINs (PUT /connection/skoda): change them with it.
// They only catch a paste gone wrong: Škoda checks the key on each VIN itself.
export const keyLimits = { min: 8, max: 512 } as const
export const maxVins = 5

export type Field = 'api_key' | 'vins'
export const fields: readonly Field[] = ['api_key', 'vins']

// fieldId is the element that shows a value's issue: the error summary leads there.
export const fieldId = (f: Field) => `skoda-${f.replace('_', '-')}`

export type Rule =
  | 'required'
  | 'spaces'
  | 'characters'
  | 'tooShort'
  | 'tooLong'
  | 'vinRequired'
  | 'vinInvalid'
  | 'vinTwice'
  | 'tooManyVins'
export type Issue = { field: Field; rule: Rule; params?: Record<string, string | number> }
export type Draft = { api_key: string; vins: string }
export type Checked = { issues: Issue[]; body: SkodaCredentialUpdate | null }

// A VIN: 17 letters and digits, without I, O nor Q (ISO 3779).
const vinPattern = /^[A-HJ-NPR-Z0-9]{17}$/

// vinsOf reads the VINs as typed or pasted, one a line or separated by commas or spaces,
// in capitals: the papers and the app write them so.
export const vinsOf = (text: string): string[] =>
  text
    .split(/[\s,;]+/)
    .map((v) => v.trim().toUpperCase())
    .filter((v) => v !== '')

function checkKey(value: string): Issue | null {
  if (!value) return { field: 'api_key', rule: 'required' }
  if (/\s/.test(value)) return { field: 'api_key', rule: 'spaces' }
  if (!/^[!-~]+$/.test(value)) return { field: 'api_key', rule: 'characters' }
  if (value.length < keyLimits.min)
    return { field: 'api_key', rule: 'tooShort', params: { min: keyLimits.min } }
  if (value.length > keyLimits.max)
    return { field: 'api_key', rule: 'tooLong', params: { max: keyLimits.max } }
  return null
}

function checkVins(vins: string[]): Issue | null {
  if (!vins.length) return { field: 'vins', rule: 'vinRequired' }
  const bad = vins.find((v) => !vinPattern.test(v))
  if (bad) return { field: 'vins', rule: 'vinInvalid', params: { vin: bad } }
  const twice = vins.find((v, i) => vins.indexOf(v) !== i)
  if (twice) return { field: 'vins', rule: 'vinTwice', params: { vin: twice } }
  if (vins.length > maxVins) return { field: 'vins', rule: 'tooManyVins', params: { max: maxVins } }
  return null
}

// validate checks the key and the VINs as pasted, with the API's rules, and builds the
// body to send: the spaces a copy adds around the key are dropped, those within it are an
// error.
export function validate(draft: Draft): Checked {
  const body = { api_key: draft.api_key.trim(), vins: vinsOf(draft.vins) }
  const issues = [checkKey(body.api_key), checkVins(body.vins)].filter(
    (i): i is Issue => i !== null,
  )
  return { issues, body: issues.length ? null : body }
}
