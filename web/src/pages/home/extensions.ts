import type { Component, Ref } from 'vue'

// The makes of the empty home's tiles.
export type HomeMake =
  'volvo' | 'polestar' | 'tesla' | 'bmw' | 'mini' | 'hyundai' | 'kia' | 'genesis' | 'skoda'

// extensionChoices adds none: the public build offers the makes it reads, and nothing for
// a car it does not. A downstream build replaces this file with its own components, shown
// in order under the makes' tiles of an account without a vehicle, each deciding whether
// it has anything to show.
export const extensionChoices: Component[] = []

// extensionMakeMentions is none: the makes' tiles say the make, and Beta for a make read
// in beta. A downstream build sets a composable the page calls once, giving by make a
// short text shown on its tile under the make, in place of Beta.
export const extensionMakeMentions:
  (() => Readonly<Ref<Partial<Record<HomeMake, string>>>>) | null = null
