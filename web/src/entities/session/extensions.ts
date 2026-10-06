import type { RouteLocationRaw } from 'vue-router'

// limitsPage is none: the public build limits no account, and has no page that lifts a
// limit. An instance that limits its accounts (AccountLimits) replaces this file with
// the page where they are lifted, which every limit then leads to: the history's, a
// function left out, a vehicle left unread, too many vehicles.
export const limitsPage: RouteLocationRaw | null = null
