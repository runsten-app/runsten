import { afterEach, expect, it, vi } from 'vitest'

// The extension's preparation, held until the test lets it end.
const { prepared } = vi.hoisted(() => {
  let end = () => {}
  const done = new Promise<void>((resolve) => (end = resolve))
  return { prepared: { done, end, called: false } }
})
vi.mock('@/app/extensions', () => ({
  beforeStart: () => {
    prepared.called = true
    return prepared.done
  },
}))

afterEach(() => {
  vi.unstubAllGlobals()
  document.body.innerHTML = ''
})

it('awaits the extension before reading the API and mounting the app', async () => {
  const fetch = vi.fn(async () =>
    Response.json({ error: { code: 'unauthorized' } }, { status: 401 }),
  )
  vi.stubGlobal('fetch', fetch)
  document.body.innerHTML = '<div id="app"></div>'

  const started = import('@/app/main')
  // Once main runs (its imports loaded), leave the router time to read the session.
  await vi.waitFor(() => expect(prepared.called).toBe(true), { timeout: 5000 })
  await new Promise((r) => setTimeout(r, 50))
  expect(fetch).not.toHaveBeenCalled()
  expect(document.querySelector('#app')?.childElementCount).toBe(0)

  prepared.end()
  await started
  await vi.waitFor(() => expect(fetch).toHaveBeenCalled())
  expect(document.querySelector('#app')?.childElementCount).toBeGreaterThan(0)
})
