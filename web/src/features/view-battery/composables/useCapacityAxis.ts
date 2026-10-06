import { computed } from 'vue'
import { useRoute, useRouter } from 'vue-router'

export type CapacityAxis = 'date' | 'odometer'
export const capacityAxes: CapacityAxis[] = ['date', 'odometer']

// useCapacityAxis is what the capacity chart is plotted against: the URL's axis when
// asked for, the date by default. Unlike the statistics' split, no length chooses: the
// whole history reads as well by time as by mileage.
export function useCapacityAxis() {
  const route = useRoute()
  const router = useRouter()
  const axis = computed<CapacityAxis>(() => {
    const asked = route.query.axis
    return typeof asked === 'string' && capacityAxes.includes(asked as CapacityAxis)
      ? (asked as CapacityAxis)
      : 'date'
  })
  function setAxis(a: CapacityAxis) {
    if (!capacityAxes.includes(a)) return
    return router.replace({ query: { ...route.query, axis: a } })
  }
  return { axis, setAxis }
}
