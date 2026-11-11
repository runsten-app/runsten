import { computed } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { compareViews, garagePeriods, type CompareView, type GaragePeriod } from '../model/garage'

// useGarageView is what the Garage shows: its period (?period=, the last 30 days by
// default) and the view of its comparison (?compare=, the consumption by default). Kept in
// the URL, so that coming back from a vehicle finds them again.
export function useGarageView() {
  const route = useRoute()
  const router = useRouter()
  const period = computed<GaragePeriod>(() => {
    const asked = route.query.period
    return garagePeriods.find((p) => p === asked) ?? '30d'
  })
  const view = computed<CompareView>(() => {
    const asked = route.query.compare
    return compareViews.find((v) => v === asked) ?? 'consumption'
  })
  function setPeriod(p: GaragePeriod) {
    if (!garagePeriods.includes(p)) return
    return router.replace({ query: { ...route.query, period: p } })
  }
  function setView(v: CompareView) {
    if (!compareViews.includes(v)) return
    return router.replace({ query: { ...route.query, compare: v } })
  }
  return { period, view, setPeriod, setView }
}
