import { computed } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import type { PurposeFilter } from '@/entities/trip'
import { purposeQuery } from '../model/purposeQuery'

// usePurposeFilter is the purpose of the trips listed, kept in the URL's query beside the
// period, as the place of the charges is.
export function usePurposeFilter() {
  const route = useRoute()
  const router = useRouter()
  const purpose = computed(() => purposeQuery(route.query))

  // Replace, as the period does: going back leaves the list.
  function setPurpose(p: PurposeFilter | undefined) {
    return router.replace({ query: { ...route.query, purpose: p || undefined } })
  }
  return { purpose, setPurpose }
}
