import type { components } from '@/shared/api'

// ConnectionSettings is the account's connection to Volvo: whether a Volvo ID is
// connected, and the application key its vehicles are read with. The key itself never
// comes back from the API: only its last four characters.
export type ConnectionSettings = components['schemas']['ConnectionSettings']
export type ApiKey = components['schemas']['ApiKey']
