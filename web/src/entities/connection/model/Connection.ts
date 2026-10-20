import type { components } from '@/shared/api'

// ConnectionSettings is the account's connections: whether a Volvo ID is connected, the
// application key its vehicles are read with, and the credential of Polestar's Data
// Portal. Neither the key nor the credential ever comes back from the API: only their
// last four characters.
export type ConnectionSettings = components['schemas']['ConnectionSettings']
export type ApiKey = components['schemas']['ApiKey']
// PolestarCredential is what the user is shown again of their Polestar credential;
// PolestarCredentialUpdate is a new one, its three values.
export type PolestarCredential = components['schemas']['PolestarCredential']
export type PolestarCredentialUpdate = components['schemas']['PolestarCredentialUpdate']
