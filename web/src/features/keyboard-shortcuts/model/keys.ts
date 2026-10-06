// What a key pressed asks for. Digits go by their place on the keyboard (code), not by
// the character they type: on an AZERTY keyboard the row types "&é\"'(" unshifted and the
// digits with Shift, and both lead to the same section. "?" and "," go by the character:
// they move from one layout to another.
export type Action = { kind: 'section'; index: number } | { kind: 'settings' } | { kind: 'help' }

type Key = Pick<
  KeyboardEvent,
  'key' | 'code' | 'ctrlKey' | 'metaKey' | 'altKey' | 'repeat' | 'isComposing'
>

// keyAction is undefined for any other key, and for any chord: those are the browser's
// and the system's (AltGr included, which is Ctrl+Alt on Windows).
export function keyAction(e: Key): Action | undefined {
  if (e.ctrlKey || e.metaKey || e.altKey || e.repeat || e.isComposing) return undefined
  if (e.key === '?') return { kind: 'help' }
  if (e.key === ',') return { kind: 'settings' }
  const digit = /^Digit([1-9])$/.exec(e.code)?.[1] ?? /^Numpad([1-9])$/.exec(e.code)?.[1]
  // A numpad without Num Lock moves the cursor instead.
  if (digit && (e.code.startsWith('Digit') || e.key === digit)) {
    return { kind: 'section', index: Number(digit) - 1 }
  }
  return undefined
}

// typing tells a key pressed in a field: it is the field's.
export function typing(target: EventTarget | null): boolean {
  if (!(target instanceof HTMLElement)) return false
  return (
    target.isContentEditable ||
    target.closest('input, textarea, select, [contenteditable="true"]') !== null
  )
}
