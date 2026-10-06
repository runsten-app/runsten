import { checkView, exclusive, expect, signIn, test } from './support'

// The account's MQTT broker, set in the settings, read back without its password,
// changed and removed. No broker listens: the collector's connection is not checked here.
test('Settings: an MQTT broker is set, changed and removed', async ({ page }, testInfo) => {
  await signIn(page)
  await exclusive('mqtt', async () => {
    await page.request.delete('/api/v1/mqtt')
    await page.goto('/settings')
    const section = page.locator('section', { has: page.getByRole('heading', { name: 'MQTT' }) })

    // A wrong URL: told before sending, in a summary that leads to the field.
    await section.getByLabel('Broker URL').fill('homeassistant.local')
    await section.getByRole('button', { name: 'Save the broker' }).click()
    const summary = section.locator('.error-summary')
    await expect(summary).toBeFocused()
    await checkView(page, testInfo, 'mqtt-errors')
    await summary.getByRole('link').click()
    await expect(section.getByLabel('Broker URL')).toBeFocused()

    await section.getByLabel('Broker URL').fill('mqtt://mosquitto.invalid:1883')
    await section.getByLabel('Username').fill('runsten')
    await section.getByLabel('Password', { exact: true }).fill('e2e-secret')
    await section.getByRole('button', { name: 'Save the broker' }).click()
    await expect(section.getByRole('status')).toHaveText(
      'Broker saved: Runsten connects to it within a minute.',
    )
    await expect(section).toContainText('mqtt://mosquitto.invalid:1883')
    await expect(section).not.toContainText('e2e-secret')
    await checkView(page, testInfo, 'mqtt-broker')

    // Changed: the password kept for the same host.
    await section.getByRole('button', { name: 'Change the broker' }).click()
    await expect(section.getByLabel('Broker URL')).toBeFocused()
    await section.getByLabel('Publish the position').uncheck()
    await section.getByRole('button', { name: 'Save the broker' }).click()
    await expect(section.getByRole('status')).toHaveText(
      'Broker saved: Runsten connects to it within a minute.',
    )
    const broker = await (await page.request.get('/api/v1/mqtt')).json()
    expect(broker.broker).toMatchObject({ password_set: true, publish_location: false })

    await section.getByRole('button', { name: 'Remove the broker' }).click()
    const dialog = page.getByRole('dialog')
    await checkView(page, testInfo, 'mqtt-remove')
    await dialog.getByRole('button', { name: 'Remove' }).click()
    await expect(section.getByRole('status')).toHaveText('Broker removed.')
    await expect(section.getByLabel('Broker URL')).toBeFocused()
  })
})
