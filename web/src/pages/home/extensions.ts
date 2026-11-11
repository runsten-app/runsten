import type { Component, Ref } from 'vue'

// The makes of the empty home's buttons.
export type HomeMake = 'volvo' | 'polestar' | 'tesla' | 'bmw' | 'pleos'

// extensionChoices adds none: the public build offers the makes it reads, and nothing for
// a car it does not. A downstream build replaces this file with its own components, shown
// in order after the makes' buttons of an account without a vehicle, in the same row, each
// deciding whether it has anything to show.
export const extensionChoices: Component[] = []

// extensionMakeMentions is none: the makes' buttons say the make, and no more. A
// downstream build sets a composable the page calls once, giving by make a short text
// shown on its button after the make; a make without one shows none.
export const extensionMakeMentions:
  (() => Readonly<Ref<Partial<Record<HomeMake, string>>>>) | null = null
