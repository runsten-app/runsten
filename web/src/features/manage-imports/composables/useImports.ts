import { useMutation, useQuery, useQueryClient } from '@tanstack/vue-query'
import { computed, toValue, type MaybeRefOrGetter } from 'vue'
import { batteryKeys } from '@/entities/battery'
import { chargeKeys } from '@/entities/charge'
import { statsKeys } from '@/entities/stats'
import { tripKeys } from '@/entities/trip'
import { deleteImport, fetchImports, importKeys } from '@/entities/vehicle-import'

// useImports lists a vehicle's imports, by source.
export function useImports(vehicle: MaybeRefOrGetter<string>) {
  const { data, isPending, error } = useQuery({
    queryKey: computed(() => importKeys.list(toValue(vehicle))),
    queryFn: () => fetchImports(toValue(vehicle)),
  })
  return { imports: data, isPending, error }
}

// useDeleteImport deletes one source's events of a vehicle: they leave its lists, its
// details and every statistic.
export function useDeleteImport(vehicle: MaybeRefOrGetter<string>) {
  const queryClient = useQueryClient()
  const { mutateAsync, isPending, error, reset } = useMutation({
    mutationFn: (source: string) => deleteImport(toValue(vehicle), source),
    onSuccess: () => {
      const v = toValue(vehicle)
      return Promise.all(
        [
          importKeys.list(v),
          tripKeys.lists(v),
          tripKeys.details(),
          chargeKeys.all(),
          chargeKeys.details(),
          statsKeys.every(),
          batteryKeys.every(),
        ].map((queryKey) => queryClient.invalidateQueries({ queryKey })),
      )
    },
  })
  return { deleteImport: mutateAsync, isPending, error, reset }
}
