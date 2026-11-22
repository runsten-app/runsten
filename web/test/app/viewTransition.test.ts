import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createMemoryHistory, createRouter } from 'vue-router'
import { withViewTransitions } from '@/app/routes/viewTransition'

const home = { template: '<div />' }
const trips = { template: '<div />' }
const trip = { template: '<div />' }

function router() {
  const r = createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/', component: home },
      { path: '/trips', component: trips },
      { path: '/trips/:id', component: trip },
    ],
  })
  withViewTransitions(r)
  return r
}

// startViewTransition as the browser runs it: the update called at once, its promise
// awaited before the transition finishes.
const start = vi.fn((update: () => Promise<void>) => {
  const finished = update()
  return { finished, ready: finished, updateCallbackDone: finished, skipTransition() {} }
})
let reduced = false

beforeEach(() => {
  start.mockClear()
  reduced = false
  Object.assign(document, { startViewTransition: start })
  vi.stubGlobal('matchMedia', (q: string) => ({ matches: reduced && q.includes('reduce') }))
})
afterEach(() => {
  Reflect.deleteProperty(document, 'startViewTransition')
  vi.unstubAllGlobals()
})

describe('withViewTransitions', () => {
  it('runs a change of page in a transition, which ends once it is rendered', async () => {
    const r = router()
    await r.push('/trips')
    expect(start).not.toHaveBeenCalled() // the first page
    await r.push('/trips/1')
    expect(start).toHaveBeenCalledOnce()
    expect(r.currentRoute.value.path).toBe('/trips/1')
    await expect(start.mock.results[0]?.value.finished).resolves.toBeUndefined()
  })

  it('leaves in place a navigation that keeps the page: the query, another trip', async () => {
    const r = router()
    await r.push('/trips')
    await r.push('/trips?from=2026-11-01')
    expect(start).not.toHaveBeenCalled()
    await r.push('/trips/1')
    start.mockClear()
    await r.push('/trips/2')
    expect(start).not.toHaveBeenCalled()
    expect(r.currentRoute.value.path).toBe('/trips/2')
  })

  it('changes the page at once with reduced motion, or without the API', async () => {
    reduced = true
    const r = router()
    await r.push('/')
    await r.push('/trips')
    expect(start).not.toHaveBeenCalled()

    reduced = false
    Reflect.deleteProperty(document, 'startViewTransition')
    await r.push('/trips/1')
    expect(r.currentRoute.value.path).toBe('/trips/1')
  })
})
