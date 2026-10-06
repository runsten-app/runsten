import { flushPromises } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { useShortcuts } from '@/features/keyboard-shortcuts'
import { enableShortcuts } from '@/features/keyboard-shortcuts/model/enabled'
import { holdDelay } from '@/features/keyboard-shortcuts/composables/useShortcuts'
import { testRouter, withSetup } from '@test/utils'

const sections = [
  { name: 'vehicle', params: { vehicle: 'v1' } },
  { name: 'battery', params: { vehicle: 'v1' } },
]

// Every host is unmounted after its test: one left listening would take the keys first.
let unmount: (() => void) | undefined

async function setup() {
  const router = testRouter()
  await router.push('/vehicles/v1')
  const mounted = withSetup(() => useShortcuts(sections), { router })
  unmount = () => mounted.wrapper.unmount()
  return { ...mounted, router }
}

function press(
  key: string,
  code: string,
  init: KeyboardEventInit = {},
  target: EventTarget = window,
) {
  const e = new KeyboardEvent('keydown', { key, code, bubbles: true, cancelable: true, ...init })
  target.dispatchEvent(e)
  return e
}

beforeEach(() => {
  enableShortcuts(true)
})
afterEach(() => {
  unmount?.()
  unmount = undefined
  vi.useRealTimers()
  localStorage.clear()
  document.body.innerHTML = ''
})

describe('useShortcuts', () => {
  it('leads to a section by its digit, and to the settings', async () => {
    const { router } = await setup()
    const e = press('2', 'Digit2')
    await vi.waitFor(() => expect(router.currentRoute.value.name).toBe('battery'))
    expect(e.defaultPrevented).toBe(true)

    // No third section here: nothing happens.
    press('3', 'Digit3')
    await flushPromises()
    expect(router.currentRoute.value.name).toBe('battery')

    press(',', 'Comma')
    await vi.waitFor(() => expect(router.currentRoute.value.name).toBe('settings'))
  })

  it('opens the list', async () => {
    const { result } = await setup()
    expect(result.help.value).toBe(false)
    press('?', 'Slash', { shiftKey: true })
    expect(result.help.value).toBe(true)
  })

  it('leaves the keys typed in a field to it', async () => {
    const { router } = await setup()
    const input = document.body.appendChild(document.createElement('input'))
    const e = press('2', 'Digit2', {}, input)
    await flushPromises()
    expect(e.defaultPrevented).toBe(false)
    expect(router.currentRoute.value.name).toBe('vehicle')
  })

  it('does nothing under an open dialog', async () => {
    const { router } = await setup()
    document.body.insertAdjacentHTML('beforeend', '<div class="v-overlay v-overlay--active"></div>')
    press('2', 'Digit2')
    await flushPromises()
    expect(router.currentRoute.value.name).toBe('vehicle')
  })

  it('does nothing once turned off', async () => {
    const { router, result } = await setup()
    enableShortcuts(false)
    expect(localStorage.getItem('runsten.shortcuts')).toBe('off')
    press('2', 'Digit2')
    press('?', 'Slash', { shiftKey: true })
    await flushPromises()
    expect(router.currentRoute.value.name).toBe('vehicle')
    expect(result.help.value).toBe(false)
  })

  it('shows the keys while Shift is held alone', async () => {
    vi.useFakeTimers()
    const { result } = await setup()
    press('Shift', 'ShiftLeft', { shiftKey: true })
    vi.advanceTimersByTime(holdDelay - 1)
    expect(result.hints.value).toBe(false)
    vi.advanceTimersByTime(1)
    expect(result.hints.value).toBe(true)
    window.dispatchEvent(new KeyboardEvent('keyup', { key: 'Shift', code: 'ShiftLeft' }))
    expect(result.hints.value).toBe(false)

    // Shift+Tab never shows them.
    press('Shift', 'ShiftLeft', { shiftKey: true })
    press('Tab', 'Tab', { shiftKey: true })
    vi.advanceTimersByTime(holdDelay)
    expect(result.hints.value).toBe(false)
  })

  it('stops listening once unmounted', async () => {
    const { router } = await setup()
    const push = vi.spyOn(router, 'push')
    unmount?.()
    unmount = undefined
    const e = press('2', 'Digit2')
    expect(e.defaultPrevented).toBe(false)
    expect(push).not.toHaveBeenCalled()
  })
})
