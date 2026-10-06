import { checkView, expect, signIn, test, user } from './support'

// What the collector writes of each vehicle, as the stack's own collector runs it: the
// state it is in depends on the simulator's progress, the structure does not.
test('Connection: the Volvo ID and how each vehicle is read, and the way back', async ({
  page,
}, testInfo) => {
  await signIn(page)
  await page.goto('/')
  await expect(page).toHaveURL(/\/vehicles\/[^/]+$/)
  const vehicle = page.url()
  await page.getByRole('button', { name: `Account: ${user}` }).click()
  await page.locator('a.connection').click()
  await expect(page).toHaveURL(/\/connection\?vehicle=/)
  await expect(page.getByRole('heading', { level: 1 })).toHaveText('Connection')

  const volvo = page.getByRole('region', { name: 'Volvo ID' })
  await expect(volvo.getByText('Connected', { exact: true })).toBeVisible()
  await expect(volvo.getByRole('link', { name: 'Reconnect the Volvo ID' })).toHaveAttribute(
    'href',
    'auth/volvo/start',
  )
  const card = page.locator('.vehicle-collection').first()
  await expect(card.getByRole('heading', { level: 3 })).toBeVisible()
  // compose-test connects the Volvo ID before the browser tests: the collector passed.
  await expect(
    card.locator('.fact', { hasText: 'Latest pass of the collector' }),
  ).not.toContainText('Never')
  await checkView(page, testInfo, 'connection')

  // The instance has its key (sim.env): it reads every vehicle, there is none to give.
  await expect(page.getByRole('region', { name: 'Volvo key' })).toHaveCount(0)

  await page.getByRole('link', { name: 'Back to the vehicle' }).click()
  await expect(page).toHaveURL(vehicle)
})
