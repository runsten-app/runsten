import type { Component } from 'vue'

// costsUnavailable is null: where the account's offer leaves out the costs, the
// statistics' costs view says so, and no more. The hosted offer's build replaces this
// file with a component of its own, shown there instead of the message.
export const costsUnavailable: Component | null = null
