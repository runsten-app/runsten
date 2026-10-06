import type { RouteRecordRaw } from 'vue-router'
import type { ExtensionGuard } from './guard'

// extensionRoutes adds nothing: the public build is complete on its own. The hosted
// offer's build replaces this file with its pages' routes, which its own extensions link
// to. One named signedOut, public, is where the sign-out ends instead of the sign-in page.
export const extensionRoutes: RouteRecordRaw[] = []

// extensionGuards check none: every signed-in user opens every page. The hosted build's
// may send them to a page of its own first, in order.
export const extensionGuards: ExtensionGuard[] = []
