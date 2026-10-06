// nextPath is where to go after signing in: the page that asked for it (?next=), if it
// is a path of the app; never another site ("//evil.example", "https://…").
export function nextPath(raw: unknown): string {
  return typeof raw === 'string' &&
    raw.startsWith('/') &&
    !raw.startsWith('//') &&
    !raw.startsWith('/\\')
    ? raw
    : '/'
}
