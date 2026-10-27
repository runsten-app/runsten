import type { components } from '@/shared/api'

// ConnectionSettings is the account's connections: whether a Volvo ID is connected, the
// application key its vehicles are read with, the credential of Polestar's Data Portal
// and the refresh token of Tesla's Owner API. Neither the key, the credential nor the
// token ever comes back from the API: only their last four characters.
export type ConnectionSettings = components['schemas']['ConnectionSettings']
export type ApiKey = components['schemas']['ApiKey']
// PolestarCredential is what the user is shown again of their Polestar credential;
// PolestarCredentialUpdate is a new one, its three values.
export type PolestarCredential = components['schemas']['PolestarCredential']
export type PolestarCredentialUpdate = components['schemas']['PolestarCredentialUpdate']
// TeslaToken is what the user is shown again of their Tesla refresh token.
export type TeslaToken = components['schemas']['TeslaToken']
