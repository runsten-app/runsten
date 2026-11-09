import { useMutation, useQuery, useQueryClient } from '@tanstack/vue-query'
import {
  accountExportKeys,
  fetchAccountExport,
  isExportActive,
  startAccountExport,
} from '@/entities/session'

// pollInterval is how often an active export is read again, in milliseconds.
export const pollInterval = 2000

// useAccountExport reads the account's export, again every pollInterval while it is
// active, and starts another: only then the worker builds the archive.
export function useAccountExport() {
  const queryClient = useQueryClient()
  const { data: accountExport, error } = useQuery({
    queryKey: accountExportKeys.job(),
    queryFn: fetchAccountExport,
    refetchInterval: (query) => (isExportActive(query.state.data) ? pollInterval : false),
  })
  const {
    mutateAsync,
    isPending,
    error: startError,
    reset,
  } = useMutation({
    mutationFn: startAccountExport,
    onSuccess: (job) => queryClient.setQueryData(accountExportKeys.job(), job),
  })
  return { accountExport, error, start: mutateAsync, isPending, startError, reset }
}
