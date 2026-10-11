import { useMutation, useQuery, useQueryClient } from '@tanstack/vue-query'
import { computed, toValue, watch, type MaybeRefOrGetter } from 'vue'
import {
  fetchAximoteVehicles,
  fetchImportJob,
  importKeys,
  isActive,
  startAximoteImport,
} from '@/entities/vehicle-import'
import { afterImportChange } from './invalidate'

// pollInterval is how often an active import is read again, in milliseconds.
export const pollInterval = 2000

// useImportJob reads the vehicle's latest import, again every pollInterval while it is
// active. Once it ends, what it imported is read again everywhere.
export function useImportJob(vehicle: MaybeRefOrGetter<string>) {
  const queryClient = useQueryClient()
  const { data, error } = useQuery({
    queryKey: computed(() => importKeys.job(toValue(vehicle))),
    queryFn: () => fetchImportJob(toValue(vehicle)),
    refetchInterval: (query) => (isActive(query.state.data) ? pollInterval : false),
  })
  watch(data, (job, before) => {
    if (job && before?.id === job.id && isActive(before) && !isActive(job))
      void afterImportChange(queryClient, toValue(vehicle))
  })
  return { job: data, error }
}

// useAximoteVehicles asks Aximote for the vehicles of the token's account.
export function useAximoteVehicles(vehicle: MaybeRefOrGetter<string>) {
  const { mutateAsync, data, isPending, error, reset } = useMutation({
    mutationFn: (token: string) => fetchAximoteVehicles(toValue(vehicle), token),
  })
  return { listVehicles: mutateAsync, vehicles: data, isPending, error, reset }
}

// useStartImport imports an Aximote vehicle's history; the job then shows its progress.
export function useStartImport(vehicle: MaybeRefOrGetter<string>) {
  const queryClient = useQueryClient()
  const { mutateAsync, isPending, error, reset } = useMutation({
    mutationFn: (p: { token: string; aximoteVehicle: string }) =>
      startAximoteImport(toValue(vehicle), p.token, p.aximoteVehicle),
    onSuccess: (job) => queryClient.setQueryData(importKeys.job(toValue(vehicle)), job),
  })
  return { startImport: mutateAsync, isPending, error, reset }
}
