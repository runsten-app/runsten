import type { Component } from 'vue'

// extensionSleepActions adds none: the public build lets a vehicle asleep sleep, and
// sends it nothing. A downstream build replaces this file with its own components, such
// as a button that wakes the vehicle up, each given the vehicle's ID (its `vehicle` prop)
// and whether it is `asleep` or `offline` (its `sleep` prop), and shown in order at the
// end of the state's headline that says it; each decides whether it has anything to show.
export const extensionSleepActions: Component[] = []
