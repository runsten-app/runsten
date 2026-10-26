import { api, unwrap } from '@/shared/api'
import type { Instance } from '../model/Instance'

export function fetchInstance(): Promise<Instance> {
  return unwrap(api.GET('/instance'))
}
