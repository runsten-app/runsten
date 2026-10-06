import AxeBuilder from '@axe-core/playwright'
import { test as base, expect, type Page, type TestInfo } from '@playwright/test'
import { mkdir, rm, stat } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import { join } from 'node:path'

// test is Playwright's, with every page recording the violations of the Content
// Security Policy of runsten-web: checkView fails on any. A library that got around the
// CSP (a charts library writing inline styles) is caught on every view.
export const test = base.extend({
  page: async ({ page }, use) => {
    await page.addInitScript(() => {
      document.addEventListener('securitypolicyviolation', (e) => {
        const w = window as unknown as { cspViolations?: string[] }
        ;(w.cspViolations ??= []).push(`${e.violatedDirective} ${e.blockedURI}`)
      })
    })
    await use(page)
  },
})
export { expect }

// The development user that task compose-test and task compose-sim create: development
// only, like sim.env.
export const user = 'admin'
export const password = 'runsten-dev-password'

// signIn opens a session through the API: the pages under test are behind it, the form
// has its own tests.
export async function signIn(page: Page) {
  const res = await page.request.post('/api/v1/session', { data: { username: user, password } })
  expect(res.status(), 'sign in').toBe(200)
}

// vehicleId is the stack's only vehicle, the simulator's.
export async function vehicleId(page: Page): Promise<string> {
  const res = await page.request.get('/api/v1/vehicles')
  expect(res.ok(), 'GET /vehicles').toBe(true)
  const body = (await res.json()) as { items: { id: string }[] }
  const id = body.items[0]?.id
  expect(id, 'a vehicle').toBeTruthy()
  return String(id)
}

// checkView asserts that the page has no WCAG 2.1 A or AA violation axe can detect, nor
// any violation of the CSP, and keeps a screenshot of it in web/e2e-results/screenshots/.
export async function checkView(page: Page, testInfo: TestInfo, view: string) {
  await settled(page)
  await expectAccessible(page)
  const violations = await page.evaluate(
    () => (window as unknown as { cspViolations?: string[] }).cspViolations ?? [],
  )
  expect(violations, 'Content Security Policy violations').toEqual([])
  await page.screenshot({
    path: `e2e-results/screenshots/${testInfo.project.name}/${view}.png`,
    fullPage: true,
  })
}

// settled waits for the transitions under way: axe would measure the contrast of a
// message still fading in (Vuetify's field errors), or of a dialog opening. A loop (a
// progress bar) never ends, and is not waited for. Nothing may run for 100 ms: a
// transition may start a frame or two after what triggered it.
async function settled(page: Page) {
  await page.evaluate(() => delete (window as unknown as { idleSince?: number }).idleSince)
  await page.waitForFunction(() => {
    const w = window as unknown as { idleSince?: number }
    const idle = document
      .getAnimations()
      .every((a) => a.playState !== 'running' || a.effect?.getTiming().iterations === Infinity)
    if (!idle) {
      w.idleSince = undefined
      return false
    }
    w.idleSince ??= performance.now()
    return performance.now() - w.idleSince >= 100
  })
}

export async function expectAccessible(page: Page) {
  const { violations } = await new AxeBuilder({ page })
    .withTags(['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa'])
    .analyze()
  // The rule, the elements and why: a readable failure.
  const found = violations.map((v) => ({
    rule: v.id,
    help: v.help,
    nodes: v.nodes.map((n) => ({ target: n.target.join(' '), why: n.failureSummary })),
  }))
  expect(found).toEqual([])
}

// tabTo presses Tab until target has the focus: the keyboard alone, however many
// controls come before it.
export async function tabTo(page: Page, target: ReturnType<Page['locator']>, max = 40) {
  for (let i = 0; i < max; i++) {
    await page.keyboard.press('Tab')
    if (await target.evaluate((el) => el === document.activeElement).catch(() => false)) return
  }
  throw new Error(`not reached with ${max} presses of Tab`)
}

// A day before today, in the browser's time zone (UTC): every event of the stack is after it.
export function yesterday(): string {
  return new Date(Date.now() - 86_400_000).toISOString().slice(0, 10)
}

// exclusive runs fn while no other worker runs one of the same name. The four projects run
// at once against one stack, and some of its state is the account's, not a project's: the
// entered cost of the one DC charge, and the place that holds it (costs, and settings,
// whose place is at the vehicle's position). A directory is the lock (mkdir is atomic), in the
// temporary directory the workers share; one left by a killed run is taken after 3 min.
export async function exclusive<T>(name: string, fn: () => Promise<T>): Promise<T> {
  const lock = join(tmpdir(), `runsten-e2e-${name}.lock`)
  for (;;) {
    try {
      await mkdir(lock)
      break
    } catch (e) {
      if ((e as NodeJS.ErrnoException).code !== 'EEXIST') throw e
      const since = await stat(lock).then(
        (s) => Date.now() - s.mtimeMs,
        () => 0,
      )
      if (since > 180_000) await rm(lock, { recursive: true, force: true })
      await new Promise((r) => setTimeout(r, 250))
    }
  }
  try {
    return await fn()
  } finally {
    await rm(lock, { recursive: true, force: true })
  }
}

// setEuro sets the account's currency to the euro, as every spec that needs one does:
// setting the same one again is accepted, a change is not once a price exists.
export async function setEuro(page: Page) {
  const res = await page.request.put('/api/v1/settings', { data: { currency: 'EUR' } })
  expect(res.status(), 'PUT /settings').toBe(200)
}
