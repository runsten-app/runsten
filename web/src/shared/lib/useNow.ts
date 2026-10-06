import { onScopeDispose, ref } from 'vue'

// useNow is the current time in ms, updated every interval, so that ages ("3 min ago")
// and staleness move on without a new response.
export function useNow(interval = 30_000) {
  const now = ref(Date.now())
  const timer = setInterval(() => (now.value = Date.now()), interval)
  onScopeDispose(() => clearInterval(timer))
  return now
}
