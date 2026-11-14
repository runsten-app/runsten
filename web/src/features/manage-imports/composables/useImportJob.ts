import { useMutation, useQuery, useQueryClient } from '@tanstack/vue-query'
import { computed, ref, toValue, watch, type MaybeRefOrGetter } from 'vue'
import { fetchSettings, settingsKeys } from '@/entities/settings'
import {
  fetchAximoteVehicles,
  fetchImportJob,
  importAAOSLogging,
  importKeys,
  importTeslaMate,
  importVolvoDataPortal,
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

// useImportVolvoDataPortal imports the Volvo Data Portal's files; once written, what they
// imported is read again everywhere.
export function useImportVolvoDataPortal(vehicle: MaybeRefOrGetter<string>) {
  const queryClient = useQueryClient()
  const { mutateAsync, isPending, error, reset } = useMutation({
    mutationFn: (files: File[]) => importVolvoDataPortal(toValue(vehicle), files),
    onSuccess: () => afterImportChange(queryClient, toValue(vehicle)),
  })
  return { importFiles: mutateAsync, isPending, error, reset }
}

// useImportAAOSLogging imports AAOS Logging's export; once written, what it imported is
// read again everywhere.
export function useImportAAOSLogging(vehicle: MaybeRefOrGetter<string>) {
  const queryClient = useQueryClient()
  const { mutateAsync, isPending, error, reset } = useMutation({
    mutationFn: (exported: File) => importAAOSLogging(toValue(vehicle), exported),
    onSuccess: () => afterImportChange(queryClient, toValue(vehicle)),
  })
  return { importExport: mutateAsync, isPending, error, reset }
}

// useImportTeslaMate imports a TeslaMate backup; once written, what it imported is read
// again everywhere. phase tells whether the backup is being compressed or sent.
export function useImportTeslaMate(vehicle: MaybeRefOrGetter<string>) {
  const queryClient = useQueryClient()
  const phase = ref<'compressing' | 'sending' | null>(null)
  const { mutateAsync, isPending, error, reset } = useMutation({
    mutationFn: (p: { backup: File; currency?: string }) => {
      phase.value = 'compressing'
      return importTeslaMate(toValue(vehicle), p.backup, p.currency, () => {
        phase.value = 'sending'
      })
    },
    onSuccess: () => afterImportChange(queryClient, toValue(vehicle)),
    onSettled: () => {
      phase.value = null
    },
  })
  return { importBackup: mutateAsync, isPending, phase, error, reset }
}

// useAccountCurrency is the account's currency, null without one: TeslaMate's costs are
// imported in it, when the user says they are.
export function useAccountCurrency() {
  const { data } = useQuery({ queryKey: settingsKeys.current(), queryFn: fetchSettings })
  return computed(() => data.value?.currency?.code ?? null)
}
