import { computed } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { parsePeriod } from '@/shared/lib'
import { periodQuery, type PeriodQuery } from '../model/periodQuery'

// usePeriod is the period of the current list, kept in the URL's query: a filtered link
// can be shared, and survives a reload.
export function usePeriod() {
  const route = useRoute()
  const router = useRouter()
  const days = computed(() => periodQuery(route.query))
  const period = computed(() => parsePeriod(days.value.from, days.value.to))

  // Replace, not push: going back leaves the list rather than undoing each date typed.
  function setPeriod(p: PeriodQuery) {
    const query = { ...route.query, from: p.from || undefined, to: p.to || undefined }
    return router.replace({ query })
  }
  return { days, period, setPeriod }
}
