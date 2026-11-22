import { nextTick } from 'vue'
import type { Router } from 'vue-router'

// withViewTransitions runs each navigation from one page to another in a view
// transition: the browser takes a picture of the page, the router changes it, and the
// two are animated into one another by the rules of app/styles/base.css. A change of the
// query alone (a period, a filter) stays in place, as does the first page. Without the
// API, or with reduced motion, the page changes at once.
export function withViewTransitions(router: Router) {
  let finish: (() => void) | undefined

  function done() {
    finish?.()
    finish = undefined
  }

  router.beforeResolve((to, from) => {
    if (
      typeof document.startViewTransition !== 'function' ||
      window.matchMedia?.('(prefers-reduced-motion: reduce)').matches ||
      !from.matched.length ||
      to.path === from.path
    ) {
      return
    }
    done()
    // The guard returns once the old page is pictured; the transition then waits for the
    // new one, rendered after the navigation (afterEach), or gives up after a second.
    return new Promise<void>((pictured) => {
      document.startViewTransition(
        () =>
          new Promise<void>((rendered) => {
            finish = rendered
            setTimeout(() => {
              if (finish === rendered) done()
            }, 1000)
            pictured()
          }),
      )
    })
  })
  router.afterEach(async () => {
    await nextTick()
    done()
  })
  router.onError(done)
}
