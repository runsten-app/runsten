import { describe, expect, it } from 'vitest'
import { keyAction, typing } from '@/features/keyboard-shortcuts/model/keys'

const key = (key: string, code: string, more: Partial<KeyboardEvent> = {}) =>
  keyAction(new KeyboardEvent('keydown', { key, code, ...more }))

describe('keyAction', () => {
  it.each([
    ['a digit', key('2', 'Digit2'), { kind: 'section', index: 1 }],
    // AZERTY: the row types "é" unshifted, "2" with Shift; both lead to the same section.
    ['a digit of AZERTY, unshifted', key('é', 'Digit2'), { kind: 'section', index: 1 }],
    ['a digit, shifted', key('2', 'Digit2', { shiftKey: true }), { kind: 'section', index: 1 }],
    ['the numpad', key('5', 'Numpad5'), { kind: 'section', index: 4 }],
    ['the numpad without Num Lock', key('Clear', 'Numpad5'), undefined],
    ['zero', key('0', 'Digit0'), undefined],
    ['the help', key('?', 'Slash', { shiftKey: true }), { kind: 'help' }],
    ['the help on AZERTY', key('?', 'KeyM', { shiftKey: true }), { kind: 'help' }],
    ['the settings', key(',', 'Comma'), { kind: 'settings' }],
    ['a chord', key('2', 'Digit2', { ctrlKey: true }), undefined],
    ['a chord with Command', key(',', 'Comma', { metaKey: true }), undefined],
    ['AltGr', key('?', 'Slash', { altKey: true, ctrlKey: true }), undefined],
    ['a repeat', key('1', 'Digit1', { repeat: true }), undefined],
    ['a letter', key('b', 'KeyB'), undefined],
  ])('%s', (_, got, want) => {
    expect(got).toEqual(want)
  })
})

describe('typing', () => {
  it('tells a field from the rest', () => {
    document.body.innerHTML =
      '<input id="i"><textarea id="t"></textarea><div contenteditable="true"><b id="c">x</b></div><button id="b"></button>'
    const at = (id: string) => document.getElementById(id)
    expect(['i', 't', 'c', 'b'].map((id) => typing(at(id)))).toEqual([true, true, true, false])
    expect(typing(window)).toBe(false)
    document.body.innerHTML = ''
  })
})
