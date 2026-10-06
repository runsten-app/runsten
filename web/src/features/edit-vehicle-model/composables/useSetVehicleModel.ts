import { useMutation, useQueryClient } from '@tanstack/vue-query'
import { chargeKeys } from '@/entities/charge'
import { statsKeys } from '@/entities/stats'
import { setVehicleModel, vehicleKeys, type ModelChoice } from '@/entities/vehicle'

// useSetVehicleModel writes the user's variant and charger. The model shows wherever the
// vehicle does: the list (the selector, the settings) and its details (the header). The
// charger bounds the costs of AC charges at once: the charges and statistics too. The
// energies follow a new variant only once the collector has derived them again.
export function useSetVehicleModel() {
  const queryClient = useQueryClient()
  const { mutateAsync, isPending, error, reset } = useMutation({
    mutationFn: (p: { vehicle: string; choice: ModelChoice }) =>
      setVehicleModel(p.vehicle, p.choice),
    onSuccess: async (vehicle) => {
      queryClient.setQueryData(vehicleKeys.detail(vehicle.id), vehicle)
      await Promise.all(
        [vehicleKeys.list(), chargeKeys.all(), chargeKeys.details(), statsKeys.every()].map(
          (queryKey) => queryClient.invalidateQueries({ queryKey }),
        ),
      )
    },
  })
  return { setVehicleModel: mutateAsync, isPending, error, reset }
}
