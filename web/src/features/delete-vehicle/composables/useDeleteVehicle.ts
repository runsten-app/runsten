import { useMutation, useQueryClient } from '@tanstack/vue-query'
import { toValue, type MaybeRefOrGetter } from 'vue'
import { useRouter } from 'vue-router'
import { deleteVehicle, vehicleKeys, type Vehicle } from '@/entities/vehicle'

// useDeleteVehicle deletes a vehicle with its history, then goes home. The list drops it
// first, or the home page would lead back to it; its queries go once its pages are left,
// or they would read it again; the rest of the account (its connection, its notices) is
// read again.
export function useDeleteVehicle(vehicle: MaybeRefOrGetter<string>) {
  const queryClient = useQueryClient()
  const router = useRouter()
  const { mutateAsync, isPending, error, reset } = useMutation({
    mutationFn: () => deleteVehicle(toValue(vehicle)),
    onSuccess: async () => {
      const id = toValue(vehicle)
      queryClient.setQueryData<Vehicle[]>(vehicleKeys.list(), (vs) =>
        vs?.filter((v) => v.id !== id),
      )
      await router.replace({ name: 'home' })
      queryClient.removeQueries({ predicate: (q) => q.queryKey.includes(id) })
      await queryClient.invalidateQueries()
    },
  })
  return { deleteVehicle: mutateAsync, isPending, error, reset }
}
