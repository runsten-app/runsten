import { useQuery } from '@tanstack/vue-query'
import { computed, toValue, type MaybeRefOrGetter } from 'vue'
import { chargeKeys, fetchCharges, type OrphanCost } from '@/entities/charge'
import { candidatePeriod } from '../model/candidates'

// useCandidateCharges reads the charges an orphaned cost may be attached to, while
// enabled (its dialog is open): the first page of those around its window, newest first.
export function useCandidateCharges(
  orphan: MaybeRefOrGetter<OrphanCost | null>,
  enabled: MaybeRefOrGetter<boolean>,
) {
  const q = computed(() => {
    const o = toValue(orphan)
    return o ? { vehicle: o.vehicle_id, ...candidatePeriod(o) } : { vehicle: '', from: '', to: '' }
  })
  const { data, isPending, error } = useQuery({
    queryKey: computed(() => chargeKeys.candidates(q.value.vehicle, q.value.from, q.value.to)),
    queryFn: async () =>
      (await fetchCharges(q.value.vehicle, { from: q.value.from, to: q.value.to })).items,
    enabled: computed(() => toValue(enabled) && !!toValue(orphan)),
  })
  return { charges: data, isPending, error }
}
