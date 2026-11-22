import { readonly, ref } from 'vue'

// The event (a trip, a charge) last opened from its list. Its row and the header of its
// page share one view-transition name (the class vt-event), so that the row grows into
// the header and shrinks back into the row: only that event's, since a name shared by
// two elements cancels the transition.
const opened = ref<string>()

export const openedEvent = readonly(opened)

export function openEvent(id: string) {
  opened.value = id
}
