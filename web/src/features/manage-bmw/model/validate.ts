// The API's bounds of a client ID (POST /connection/bmw/authorization): change them with
// it. They only catch a paste gone wrong: BMW checks the client ID itself.
export const clientIdLimits = { min: 8, max: 128 } as const

// clientIdId is the field of the client ID: the error summary leads there.
export const clientIdId = 'bmw-client-id'

export type Rule = 'required' | 'spaces' | 'characters' | 'tooShort' | 'tooLong'
export type Issue = { rule: Rule; params?: Record<string, number> }
export type Checked = { issue: Issue | null; clientId: string | null }

// validate checks the client ID as pasted, with the API's rules (printable ASCII, no space,
// within clientIdLimits): the spaces a copy adds around it are dropped, those within it
// are an error.
export function validate(draft: string): Checked {
  const clientId = draft.trim()
  const { min, max } = clientIdLimits
  let issue: Issue | null = null
  if (!clientId) issue = { rule: 'required' }
  else if (/\s/.test(clientId)) issue = { rule: 'spaces' }
  else if (!/^[!-~]+$/.test(clientId)) issue = { rule: 'characters' }
  else if (clientId.length < min) issue = { rule: 'tooShort', params: { min } }
  else if (clientId.length > max) issue = { rule: 'tooLong', params: { max } }
  return { issue, clientId: issue ? null : clientId }
}
