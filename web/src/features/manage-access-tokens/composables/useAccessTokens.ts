import { useMutation, useQuery, useQueryClient } from '@tanstack/vue-query'
import {
  type NewAccessToken,
  accessTokenKeys,
  createAccessToken,
  fetchAccessTokens,
  revokeAccessToken,
} from '@/entities/access-token'

// useAccessTokens lists the user's access tokens, expired ones included.
export function useAccessTokens() {
  const { data, isPending, error } = useQuery({
    queryKey: accessTokenKeys.list(),
    queryFn: fetchAccessTokens,
  })
  return { tokens: data, isPending, error }
}

// useCreateAccessToken issues a token: its secret is in the answer only.
export function useCreateAccessToken() {
  const queryClient = useQueryClient()
  const { mutateAsync, isPending, error, reset } = useMutation({
    mutationFn: (body: NewAccessToken) => createAccessToken(body),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: accessTokenKeys.list() }),
  })
  return { createAccessToken: mutateAsync, isPending, error, reset }
}

// useRevokeAccessToken revokes a token: it no longer reads, at once.
export function useRevokeAccessToken() {
  const queryClient = useQueryClient()
  const { mutateAsync, isPending, error, reset } = useMutation({
    mutationFn: (id: string) => revokeAccessToken(id),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: accessTokenKeys.list() }),
  })
  return { revokeAccessToken: mutateAsync, isPending, error, reset }
}
