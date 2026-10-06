import { flushPromises } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { sessionKeys } from '@/entities/session'
import { SignInForm } from '@/features/auth'
import { ApiError } from '@/shared/api'
import { i18n } from '@/shared/i18n'
import session from '@fixtures/session.json'
import { mountWith } from '@test/utils'

const { signIn } = vi.hoisted(() => ({ signIn: vi.fn() }))
vi.mock('@/entities/session', async (original) => ({
  ...(await original<typeof import('@/entities/session')>()),
  signIn,
}))

async function submit(error?: ApiError) {
  signIn.mockReset()
  if (error) signIn.mockRejectedValue(error)
  else signIn.mockResolvedValue(session)
  const signedIn = vi.fn()
  const m = mountWith(SignInForm, { props: { onSignedIn: signedIn } })
  m.queryClient.setQueryData(['vehicles'], 'from an earlier session')
  await m.wrapper.find('input[name="username"]').setValue('admin')
  await m.wrapper.find('input[name="password"]').setValue('a long enough password')
  await m.wrapper.find('form').trigger('submit')
  await flushPromises()
  return { ...m, signedIn }
}

beforeEach(() => {
  i18n.global.locale.value = 'en'
})

describe('SignInForm', () => {
  it('focuses the username once, so that Tab leads on to the password', async () => {
    const { wrapper } = mountWith(SignInForm, { attachTo: document.body })
    const username = wrapper.find('input[name="username"]').element
    expect(document.activeElement).toBe(username)
    expect(username.hasAttribute('autofocus')).toBe(false)
    const password = wrapper.find<HTMLInputElement>('input[name="password"]').element
    password.focus()
    await new Promise((resolve) => setTimeout(resolve, 100))
    expect(document.activeElement).toBe(password)
    wrapper.unmount()
  })

  it('signs in, caches the session and drops the rest', async () => {
    const { signedIn, queryClient, wrapper } = await submit()
    expect(signIn).toHaveBeenCalledWith('admin', 'a long enough password')
    expect(signedIn).toHaveBeenCalledOnce()
    expect(queryClient.getQueryData(sessionKeys.current())).toEqual(session)
    expect(queryClient.getQueryData(['vehicles'])).toBeUndefined()
    expect(wrapper.find('.v-alert').exists()).toBe(false)
  })

  it.each([
    [new ApiError(401, 'invalid_credentials', ''), 'Wrong username or password.'],
    [new ApiError(429, 'too_many_attempts', '', 120), 'Try again in 2 min.'],
    [new ApiError(429, 'too_many_attempts', ''), 'Try again in 1 min.'],
    [new ApiError(502, 'unavailable', ''), 'Runsten cannot be reached.'],
    [new ApiError(0, 'network', ''), 'Runsten cannot be reached.'],
    [new ApiError(500, 'internal', ''), 'Something went wrong.'],
    [new Error('bug'), 'Something went wrong.'],
  ])('explains %o', async (error, text) => {
    const { signedIn, wrapper } = await submit(error as ApiError)
    expect(signedIn).not.toHaveBeenCalled()
    expect(wrapper.find('.v-alert').text()).toContain(text)
  })

  it('speaks the chosen language', async () => {
    i18n.global.locale.value = 'sv'
    const { wrapper } = await submit(new ApiError(401, 'invalid_credentials', ''))
    expect(wrapper.find('.v-alert').text()).toBe('Fel användarnamn eller lösenord.')
  })
})
