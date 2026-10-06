import { onBeforeUnmount, onMounted, ref, toValue, type MaybeRefOrGetter } from 'vue'
import { useRouter, type RouteLocationRaw } from 'vue-router'
import { shortcutsEnabled } from '../model/enabled'
import { keyAction, typing } from '../model/keys'

// Shift held alone this long shows the keys on the page: long enough that Shift+Tab, a
// capital or "?" never flashes them.
export const holdDelay = 400

// An open dialog or menu has the keyboard: a digit must not change the page under it.
// Tooltips do not.
const overlayOpen = () => document.querySelector('.v-overlay--active:not(.v-tooltip)') !== null

// useShortcuts listens to the keyboard while the shell is mounted: the digits lead to the
// sections, in their order, "," to the settings, "?" opens the list of them; Shift held
// alone shows the keys where they lead.
export function useShortcuts(sections: MaybeRefOrGetter<RouteLocationRaw[]>) {
  const router = useRouter()
  const help = ref(false)
  const hints = ref(false)
  let timer: ReturnType<typeof setTimeout> | undefined

  function hide() {
    clearTimeout(timer)
    timer = undefined
    hints.value = false
  }

  function onKeydown(e: KeyboardEvent) {
    if (!shortcutsEnabled.value) return
    const blocked = e.defaultPrevented || typing(e.target) || overlayOpen()
    if (e.key === 'Shift') {
      if (!e.repeat && !blocked) timer = setTimeout(() => (hints.value = true), holdDelay)
      return
    }
    hide()
    const action = blocked ? undefined : keyAction(e)
    if (!action) return
    e.preventDefault()
    if (action.kind === 'help') help.value = true
    else if (action.kind === 'settings') void router.push({ name: 'settings' })
    else {
      const to = toValue(sections)[action.index]
      if (to) void router.push(to)
    }
  }

  function onKeyup(e: KeyboardEvent) {
    if (e.key === 'Shift') hide()
  }

  onMounted(() => {
    window.addEventListener('keydown', onKeydown)
    window.addEventListener('keyup', onKeyup)
    window.addEventListener('blur', hide)
  })
  onBeforeUnmount(() => {
    hide()
    window.removeEventListener('keydown', onKeydown)
    window.removeEventListener('keyup', onKeyup)
    window.removeEventListener('blur', hide)
  })
  return { help, hints }
}
