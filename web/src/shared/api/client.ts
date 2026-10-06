import createClient, { type Middleware } from 'openapi-fetch'
import { ApiError, type ErrorCode } from './errors'
import type { paths } from './schema'

// Relative to the document (its <base href>): runsten-web serves the app and relays
// /api/ at the same origin, under the reverse proxy's prefix if any.
const baseUrl = new URL('api/v1', document.baseURI).href

type Listener = () => void
const unauthorized = new Set<Listener>()

// onUnauthorized registers a listener for a 401 on any call but those of /session, whose
// callers handle it (the sign-in form, the route guard): the session ended (expired,
// signed out elsewhere). shared knows no router: the app decides.
export function onUnauthorized(l: Listener): () => void {
  unauthorized.add(l)
  return () => unauthorized.delete(l)
}

const notify401: Middleware = {
  onResponse({ request, response }) {
    const session = new URL(request.url).pathname.endsWith('/api/v1/session')
    if (response.status === 401 && !session) unauthorized.forEach((l) => l())
  },
}

export const api = createClient<paths>({
  baseUrl,
  // Looked up at each call, so that tests can replace it.
  fetch: (req) => globalThis.fetch(req),
})
api.use(notify401)

// downloadPath is where the browser downloads a file of the API, relative to the
// document like every call: a link with download, not a fetch, so that the browser
// saves the file as it comes. Undefined values of the query are left out.
export function downloadPath(path: string, query: Record<string, string | undefined> = {}): string {
  const q = new URLSearchParams()
  for (const [k, v] of Object.entries(query)) if (v !== undefined) q.set(k, v)
  const search = q.toString()
  return `api/v1${path}${search ? `?${search}` : ''}`
}

type Result<T> = { data?: T; error?: unknown; response: Response }

// unwrap returns the data of a call, or throws an ApiError.
export async function unwrap<T>(call: Promise<Result<T>>): Promise<T> {
  let r: Result<T>
  try {
    r = await call
  } catch (e) {
    throw new ApiError(0, 'network', e instanceof Error ? e.message : String(e))
  }
  if (r.response.ok) return r.data as T
  const { status, headers } = r.response
  const body = (r.error as { error?: { code?: ErrorCode; message?: string } } | undefined)?.error
  const retry = Number(headers.get('Retry-After'))
  throw new ApiError(
    status,
    body?.code ?? (status >= 502 && status <= 504 ? 'unavailable' : 'internal'),
    body?.message ?? `HTTP ${status}`,
    Number.isInteger(retry) && retry > 0 ? retry : undefined,
  )
}
