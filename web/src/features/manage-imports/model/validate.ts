// An Aximote personal access token, as its API states it (POST /vehicles/{id}/import-job):
// axm_pat_ then 26 letters and digits.
export const tokenPattern = /^axm_pat_[0-9A-Za-z]{26}$/

// The fields the error summary leads to.
export const fields = { token: 'aximote-token', vehicle: 'aximote-vehicle' } as const

export type Rule = 'required' | 'format' | 'vehicle'
export type Issue = { field: string; rule: Rule }

// validateToken checks a token as pasted; the spaces a copy adds around it are dropped.
export function validateToken(draft: string): { issues: Issue[]; token: string | null } {
  const token = draft.trim()
  if (!token) return { issues: [{ field: fields.token, rule: 'required' }], token: null }
  if (!tokenPattern.test(token))
    return { issues: [{ field: fields.token, rule: 'format' }], token: null }
  return { issues: [], token }
}

// validateImport checks the token and the Aximote vehicle chosen, and gives what to send.
export function validateImport(
  draft: string,
  vehicle: string | null,
): { issues: Issue[]; body: { token: string; aximoteVehicle: string } | null } {
  const { issues, token } = validateToken(draft)
  if (!vehicle) issues.push({ field: fields.vehicle, rule: 'vehicle' })
  if (!token || !vehicle) return { issues, body: null }
  return { issues, body: { token, aximoteVehicle: vehicle } }
}
