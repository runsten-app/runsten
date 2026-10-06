import { flushPromises } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { sessionKeys } from '@/entities/session'
import { UserMenu } from '@/features/auth'
import session from '@fixtures/session.json'
import { mountWith, testQueryClient, testRouter } from '@test/utils'

const { signOut } = vi.hoisted(() => ({ signOut: vi.fn() }))
vi.mock('@/entities/session', async (original) => ({
  ...(await original<typeof import('@/entities/session')>()),
  signOut,
}))

const display = vi.hoisted(() => ({ xs: false }))
vi.mock('vuetify', async (original) => {
  const v = await original<typeof import('vuetify')>()
  const { ref } = await import('vue')
  return { ...v, useDisplay: () => ({ ...v.useDisplay(), xs: ref(display.xs) }) }
})

beforeEach(() => {
  display.xs = false
  signOut.mockReset()
})

async function opened() {
  const queryClient = testQueryClient()
  queryClient.setQueryData(sessionKeys.current(), session)
  const m = mountWith(UserMenu, { queryClient, attachTo: document.body })
  await m.router.push('/')
  const button = m.wrapper.find('button.user-menu')
  await button.trigger('click')
  await flushPromises()
  return { ...m, button }
}

describe('UserMenu', () => {
  it('names the user, and leads to their preferences', async () => {
    const { wrapper, button } = await opened()
    expect(button.text()).toBe('admin')
    expect(button.attributes('aria-label')).toBe('Account: admin')
    const preferences = [...document.querySelectorAll<HTMLAnchorElement>('a.preferences')].at(-1)
    expect(preferences?.textContent?.trim()).toBe('Preferences')
    expect(preferences?.getAttribute('href')).toBe('/preferences')
    wrapper.unmount()
  })

  it('is an icon with the same name on a phone', async () => {
    display.xs = true
    const { wrapper, button } = await opened()
    expect(button.text()).toBe('')
    expect(button.attributes('aria-label')).toBe('Account: admin')
    wrapper.unmount()
  })

  it.each([
    ['signed out', () => signOut.mockResolvedValue(undefined)],
    ['the session had ended already', () => signOut.mockRejectedValue(new Error('401'))],
  ])('signs out, clears the cache and goes to the sign-in: %s', async (_, arrange) => {
    arrange()
    const { wrapper, router, queryClient } = await opened()
    // The menu's own item: an earlier test's overlay may still be leaving.
    const items = document.querySelectorAll<HTMLElement>('.sign-out')
    items[items.length - 1]?.click()
    await vi.waitFor(() => expect(router.currentRoute.value.name).toBe('login'))
    expect(queryClient.getQueryData(sessionKeys.current())).toBeUndefined()
    wrapper.unmount()
  })

  it('ends the sign-out on the page named signedOut, when the instance has one', async () => {
    signOut.mockResolvedValue(undefined)
    const queryClient = testQueryClient()
    queryClient.setQueryData(sessionKeys.current(), session)
    const router = testRouter([
      { path: '/signed-out', name: 'signedOut', component: { render: () => null } },
    ])
    const { wrapper } = mountWith(UserMenu, { queryClient, router, attachTo: document.body })
    await router.push('/')
    await wrapper.find('button.user-menu').trigger('click')
    await flushPromises()
    const items = document.querySelectorAll<HTMLElement>('.sign-out')
    items[items.length - 1]?.click()
    await vi.waitFor(() => expect(router.currentRoute.value.name).toBe('signedOut'))
    wrapper.unmount()
  })
})
