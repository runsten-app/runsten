import { computed } from 'vue'
import { useRoute, useRouter } from 'vue-router'

// The views of the statistics: what a reader looks for, each with a few charts.
export type StatsView = 'driving' | 'charging' | 'costs'
export const statsViews: StatsView[] = ['driving', 'charging', 'costs']

// useStatsView is the view of the statistics page: the URL's when it names one, else the
// driving. Kept in the URL with the period and the split, so that coming back from an
// interval's events finds it again.
export function useStatsView() {
  const route = useRoute()
  const router = useRouter()
  const view = computed<StatsView>(() => {
    const asked = route.query.view
    return typeof asked === 'string' && statsViews.includes(asked as StatsView)
      ? (asked as StatsView)
      : 'driving'
  })
  function setView(v: StatsView) {
    if (!statsViews.includes(v)) return
    return router.replace({ query: { ...route.query, view: v } })
  }
  return { view, setView }
}
