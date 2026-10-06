import { checkView, expect, signIn, test, vehicleId } from './support'

// A token issued in the settings reads the API alone, without the session, only reads,
// and stops at once when revoked.
test('Settings: an access token reads the API, never writes, until revoked', async ({
  page,
  playwright,
}, testInfo) => {
  const name = `E2E ${testInfo.project.name} ${Date.now()}`
  await signIn(page)
  const car = await vehicleId(page)
  await page.goto('/settings')
  const section = page.locator('section', {
    has: page.getByRole('heading', { name: 'API access' }),
  })
  await section.getByRole('button', { name: 'New access token' }).click()
  await expect(page.getByLabel('Name', { exact: true })).toBeFocused()
  await page.getByLabel('Name', { exact: true }).fill(name)
  await section.getByRole('button', { name: 'Create the token' }).click()

  const secretField = page.getByLabel('Access token', { exact: true })
  await expect(secretField).toBeFocused()
  const secret = await secretField.inputValue()
  expect(secret).toMatch(/^rst_[A-Za-z0-9_-]{43}$/)
  await checkView(page, testInfo, 'access-token-created')
  await section.getByRole('button', { name: 'I have copied it' }).click()
  await expect(section.locator('.token', { hasText: name })).toBeVisible()

  // Another context: no cookie, the token alone.
  const program = await playwright.request.newContext({
    baseURL: testInfo.project.use.baseURL,
    extraHTTPHeaders: { Authorization: `Bearer ${secret}` },
  })
  try {
    const vehicles = await program.get('/api/v1/vehicles')
    expect(vehicles.status()).toBe(200)
    expect(JSON.stringify(await vehicles.json())).toContain(car)
    expect((await program.get(`/api/v1/vehicles/${car}/trips`)).status()).toBe(200)
    const write = await program.put('/api/v1/settings', { data: { currency: 'EUR' } })
    expect(write.status()).toBe(403)
    expect((await write.json()).error.code).toBe('insufficient_scope')
    expect((await program.get('/api/v1/tokens')).status()).toBe(403)

    await section
      .locator('.token', { hasText: name })
      .getByRole('button', { name: `Revoke the token ${name}` })
      .click()
    await page.getByRole('dialog').getByRole('button', { name: 'Revoke' }).click()
    await expect(section.getByRole('status')).toHaveText(`Token ${name} revoked.`)
    expect((await program.get('/api/v1/vehicles')).status()).toBe(401)
  } finally {
    await program.dispose()
  }
})
