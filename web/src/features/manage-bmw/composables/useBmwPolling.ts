import { useQueryClient } from '@tanstack/vue-query'
import { onScopeDispose, ref, shallowRef } from 'vue'
import { connectionKeys, pollBmwToken, type BmwDevice } from '@/entities/connection'
import { ApiError, isApiError } from '@/shared/api'
import { afterConnectionChange } from '../model/invalidate'

// idle: no code is waited for; waiting: the owner has the code, Runsten asks BMW whether
// they approved it; connected: they did, the connection is stored; failed: a terminal
// answer (error), or the code expired.
export type BmwPhase = 'idle' | 'waiting' | 'connected' | 'failed'

// useBmwPolling asks POST /connection/bmw/token until the owner approved the code on BMW's
// page: no sooner than the interval BMW gave, then as the API's Retry-After says
// (authorization_pending). BMW not answering (502) or Runsten unreachable does not end
// the wait, the code may well be approved: it is told (unreachable) and asked again, until
// the code expires. Any other answer ends it. cancel stops asking; leaving the page does
// too. A connection stored after cancel (a request already in flight) still reaches the
// cache, but not the phase.
export function useBmwPolling() {
  const queryClient = useQueryClient()
  const phase = ref<BmwPhase>('idle')
  const error = shallowRef<ApiError | null>(null)
  const unreachable = ref(false)
  let timer: ReturnType<typeof setTimeout> | undefined
  let run = 0
  // disposed: the page was left, perhaps while the code was being asked for; no wait
  // starts after it, which would spend a later code of the same account.
  let disposed = false

  function stop() {
    run++
    clearTimeout(timer)
    timer = undefined
  }

  function fail(e: ApiError) {
    phase.value = 'failed'
    error.value = e
  }

  async function ask(id: number, device: BmwDevice) {
    if (id !== run) return
    if (Date.parse(device.expires_at) <= Date.now()) {
      return fail(new ApiError(400, 'authorization_expired', 'the code expired'))
    }
    let delay = device.interval
    try {
      const connection = await pollBmwToken()
      queryClient.setQueryData(connectionKeys.current(), connection)
      await afterConnectionChange(queryClient)
      if (id === run) phase.value = 'connected'
      return
    } catch (e) {
      if (id !== run) return
      if (isApiError(e, 'authorization_pending')) {
        unreachable.value = false
        delay = e.retryAfter ?? delay
      } else if (
        isApiError(e, 'unavailable') ||
        isApiError(e, 'network') ||
        isApiError(e, 'source_unavailable')
      ) {
        unreachable.value = true
      } else {
        return fail(e instanceof ApiError ? e : new ApiError(500, 'internal', String(e)))
      }
    }
    timer = setTimeout(() => void ask(id, device), Math.max(delay, 1) * 1000)
  }

  // begin waits for the owner's approval of the device's code.
  function begin(device: BmwDevice) {
    if (disposed) return
    stop()
    phase.value = 'waiting'
    error.value = null
    unreachable.value = false
    const id = run
    timer = setTimeout(() => void ask(id, device), Math.max(device.interval, 1) * 1000)
  }

  function cancel() {
    stop()
    phase.value = 'idle'
    error.value = null
    unreachable.value = false
  }

  onScopeDispose(() => {
    disposed = true
    stop()
  })
  return { phase, error, unreachable, begin, cancel }
}
