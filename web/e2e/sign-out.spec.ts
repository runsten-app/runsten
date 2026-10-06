import { expect, signIn, test, user, vehicleId } from './support'

test('signs out; a page behind the session then leads to the sign-in', async ({ page }) => {
  // Its own session: the other tests keep theirs.
  await signIn(page)
  const trips = `/vehicles/${await vehicleId(page)}/trips`
  await page.goto(trips)
  await page.getByRole('button', { name: `Account: ${user}` }).click()
  await page.locator('.sign-out').click()
  await expect(page).toHaveURL(/\/login(\?|$)/)

  await page.goto(trips)
  await expect(page).toHaveURL(/\/login\?next=/)
  expect(new URL(page.url()).searchParams.get('next')).toBe(trips)
  await expect(page.getByRole('button', { name: 'Sign in' })).toBeVisible()
})
