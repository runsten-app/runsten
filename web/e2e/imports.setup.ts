import { expect, test } from '@playwright/test'
import { aximoteToken, password, user } from './support'

// The history the browser tests read, imported from the simulator's Aximote through the
// API as the import screen does it, before every project (task compose-test has
// imported it already, task e2e not yet): two trips and a charge a month before the
// vehicle's first reading. Importing again replaces the same events.
test('Imports: the history of the simulator’s Aximote', async ({ request }) => {
  test.setTimeout(60_000)
  const session = await request.post('/api/v1/session', { data: { username: user, password } })
  expect(session.status(), 'sign in').toBe(200)
  const vehicles = (await (await request.get('/api/v1/vehicles')).json()) as {
    items: { id: string }[]
  }
  const vehicle = vehicles.items[0]?.id
  expect(vehicle, 'a vehicle').toBeTruthy()
  const started = await request.post(`/api/v1/vehicles/${vehicle}/import-job`, {
    data: { source: 'aximote', token: aximoteToken, vehicle: 'veh-ex30' },
  })
  // 409: one is under way already.
  expect([202, 409], 'POST /import-job').toContain(started.status())
  await expect
    .poll(
      async () => {
        const res = await request.get(`/api/v1/vehicles/${vehicle}/import-job`)
        return ((await res.json()) as { job: { status: string } | null }).job?.status
      },
      { timeout: 30_000 },
    )
    .toBe('done')
})
