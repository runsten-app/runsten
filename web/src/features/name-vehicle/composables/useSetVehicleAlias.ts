import { useMutation, useQueryClient } from '@tanstack/vue-query'
import { setVehicleAlias, vehicleKeys, type AliasChoice } from '@/entities/vehicle'

// useSetVehicleAlias writes the name the user gives a vehicle. It names the vehicle
// wherever the vehicle shows: the list (the menu, the garage, the settings) and its details.
export function useSetVehicleAlias() {
  const queryClient = useQueryClient()
  const { mutateAsync, isPending, error, reset } = useMutation({
    mutationFn: (p: { vehicle: string; alias: AliasChoice }) => setVehicleAlias(p.vehicle, p.alias),
    onSuccess: async (vehicle) => {
      queryClient.setQueryData(vehicleKeys.detail(vehicle.id), vehicle)
      // The detail too: of two saves that overlap, the last response is not always the
      // last write.
      await Promise.all(
        [vehicleKeys.list(), vehicleKeys.detail(vehicle.id)].map((queryKey) =>
          queryClient.invalidateQueries({ queryKey }),
        ),
      )
    },
  })
  return { setVehicleAlias: mutateAsync, isPending, error, reset }
}
