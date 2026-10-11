import { useMutation, useQuery, useQueryClient } from '@tanstack/vue-query'
import { computed, toValue, type MaybeRefOrGetter } from 'vue'
import { deleteImport, fetchImports, importKeys } from '@/entities/vehicle-import'
import { afterImportChange } from './invalidate'

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
    onSuccess: () => afterImportChange(queryClient, toValue(vehicle)),
  })
  return { deleteImport: mutateAsync, isPending, error, reset }
}
