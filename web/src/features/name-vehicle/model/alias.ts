import type { AliasChoice } from '@/entities/vehicle'

// maxAlias is the API's limit of an alias, in characters (api.MaxAlias).
export const maxAlias = 40

export type AliasIssue = 'tooLong' | 'control'

// aliasField is the ID of a vehicle's alias field, which the error summary leads to.
export const aliasField = (vehicle: string) => `vehicle-${vehicle}-alias`

// validate checks the alias typed as the API does, and builds the body: an empty field
// removes the alias.
export function validate(text: string): { body: AliasChoice | null; issue: AliasIssue | null } {
  const alias = text.trim()
  // eslint-disable-next-line no-control-regex
  if (/[\u0000-\u001f\u007f-\u009f]/.test(alias)) return { body: null, issue: 'control' }
  if ([...alias].length > maxAlias) return { body: null, issue: 'tooLong' }
  return { body: { alias: alias === '' ? null : alias }, issue: null }
}
