// The API's bounds of an application key (PUT /connection/api-key): change them with it.
// They only catch a paste gone wrong: Volvo checks the key itself.
export const keyLimits = { min: 16, max: 128 }

// field is the element that shows the key's issue: the error summary leads there.
export const field = 'api-key'

export type Rule = 'required' | 'spaces' | 'characters' | 'tooShort' | 'tooLong'
export type Issue = { field: string; rule: Rule; params?: Record<string, number> }
export type Checked = { issues: Issue[]; key: string | null }

// validate checks a key as pasted, with the API's rules (printable ASCII, no space,
// within keyLimits), and gives the key to send: the spaces a copy adds around it are
// dropped, those within it are an error.
export function validate(draft: string): Checked {
  const key = draft.trim()
  let rule: Rule | null = null
  let params: Record<string, number> | undefined
  if (!key) rule = 'required'
  else if (/\s/.test(key)) rule = 'spaces'
  else if (!/^[!-~]+$/.test(key)) rule = 'characters'
  else if (key.length < keyLimits.min) [rule, params] = ['tooShort', { min: keyLimits.min }]
  else if (key.length > keyLimits.max) [rule, params] = ['tooLong', { max: keyLimits.max }]
  if (rule) return { issues: [{ field, rule, ...(params && { params }) }], key: null }
  return { issues: [], key }
}
