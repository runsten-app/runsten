import { flushPromises } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'
import { LoginPage } from '@/pages/login'
import session from '@fixtures/session.json'
import { mountWith, testRouter } from '@test/utils'

const { signIn } = vi.hoisted(() => ({ signIn: vi.fn() }))
vi.mock('@/entities/session', async (original) => ({
  ...(await original<typeof import('@/entities/session')>()),
  signIn,
}))

describe('LoginPage', () => {
  it.each([
    ['/login?next=/vehicles/v1', '/vehicles/v1'],
    ['/login?next=https://evil.example/', '/'],
    ['/login', '/'],
  ])('goes on from %s to %s', async (from, to) => {
    signIn.mockResolvedValue(session)
    const router = testRouter([{ path: '/vehicles/:v', component: { render: () => null } }])
    await router.push(from)
    const { wrapper } = mountWith(LoginPage, { router })
    await wrapper.find('input[name="username"]').setValue('admin')
    await wrapper.find('input[name="password"]').setValue('a long enough password')
    await wrapper.find('form').trigger('submit')
    await flushPromises()
    await vi.waitFor(() => expect(router.currentRoute.value.fullPath).toBe(to))
  })
})
