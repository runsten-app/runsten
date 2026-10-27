// The API's bounds of a refresh token (PUT /connection/tesla): change them with it. They
// only catch a paste gone wrong: Tesla checks the token itself.
export const tokenLimits = { min: 16, max: 8192 } as const

// tokenId is the field of the token: the error summary leads there.
export const tokenId = 'tesla-refresh-token'

export type Rule = 'required' | 'spaces' | 'characters' | 'tooShort' | 'tooLong'
export type Issue = { rule: Rule; params?: Record<string, number> }
export type Checked = { issue: Issue | null; token: string | null }

// validate checks the token as pasted, with the API's rules (printable ASCII, no space,
// within tokenLimits): the spaces a copy adds around it are dropped, those within it are
// an error.
export function validate(draft: string): Checked {
  const token = draft.trim()
  const { min, max } = tokenLimits
  let issue: Issue | null = null
  if (!token) issue = { rule: 'required' }
  else if (/\s/.test(token)) issue = { rule: 'spaces' }
  else if (!/^[!-~]+$/.test(token)) issue = { rule: 'characters' }
  else if (token.length < min) issue = { rule: 'tooShort', params: { min } }
  else if (token.length > max) issue = { rule: 'tooLong', params: { max } }
  return { issue, token: issue ? null : token }
}
