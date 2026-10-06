import { vi } from 'vitest'

export type Handler = (req: Request) => Response | Promise<Response>

export function json(body: unknown, status = 200, headers: Record<string, string> = {}): Response {
  return new Response(body === undefined ? null : JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json', ...headers },
  })
}

export function apiError(status: number, code: string, headers: Record<string, string> = {}) {
  return json({ error: { code, message: code.replaceAll('_', ' ') } }, status, headers)
}

// stubFetch answers the API calls with the handler of "METHOD /path" (the path after
// /api/v1), and records the requests.
export function stubFetch(handlers: Record<string, Handler>) {
  const requests: Request[] = []
  vi.stubGlobal(
    'fetch',
    vi.fn(async (req: Request) => {
      requests.push(req.clone())
      const path = new URL(req.url).pathname.replace(/^.*\/api\/v1/, '')
      const h = handlers[`${req.method} ${path}`]
      if (!h) throw new Error(`unexpected ${req.method} ${path}`)
      return h(req)
    }),
  )
  return requests
}
