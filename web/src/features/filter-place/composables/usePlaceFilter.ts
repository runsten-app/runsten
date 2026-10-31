import { computed } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { placeQuery, type ChargePlace } from '../model/placeQuery'

// usePlaceFilter is the place of the charges listed, kept in the URL's query beside the
// period: a filtered link can be shared, and the statistics lead to it.
export function usePlaceFilter() {
  const route = useRoute()
  const router = useRouter()
  const place = computed(() => placeQuery(route.query).place)

  // Replace, as the period does: going back leaves the list.
  function setPlace(p: ChargePlace | undefined) {
    return router.replace({ query: { ...route.query, place: p || undefined } })
  }
  return { place, setPlace }
}
