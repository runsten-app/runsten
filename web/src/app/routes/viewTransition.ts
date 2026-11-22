import { nextTick } from 'vue'
import type { RouteLocationNormalized, Router } from 'vue-router'

// withViewTransitions runs each navigation from one page to another in a view
// transition: the browser takes a picture of the page, the router changes it, and the
// two are animated into one another by the rules of app/styles/base.css. A navigation
// that keeps the page (a period or a filter in the query, a new place taking its ID, the
// next trip) stays in place, as does the first page. Without the API, or with reduced
// motion, the page changes at once.
// samePage: the page shown stays the same component, its data alone changing.
function samePage(to: RouteLocationNormalized, from: RouteLocationNormalized) {
  return to.matched.at(-1)?.components?.default === from.matched.at(-1)?.components?.default
}

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
      samePage(to, from)
    ) {
      return
    }
    done()
    // The guard returns once the old page is pictured; the transition then waits for the
    // new one, rendered after the navigation (afterEach), or gives up after a second.
    return new Promise<void>((pictured) => {
      const transition = document.startViewTransition(
        () =>
          new Promise<void>((rendered) => {
            finish = rendered
            setTimeout(() => {
              if (finish === rendered) done()
            }, 1000)
            pictured()
          }),
      )
      // A transition skipped (the tab hidden, another one started) rejects ready; the
      // navigation does not wait for it.
      transition.ready.catch(() => {})
    })
  })
  router.afterEach(async () => {
    await nextTick()
    done()
  })
  router.onError(done)
}
