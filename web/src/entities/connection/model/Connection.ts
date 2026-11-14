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
// PolestarId is what the user is shown again of their Polestar ID: its mode, never the
// token, the email nor the password; PolestarIdAuthorization a sign-in started on
// Polestar's page, its address and state.
export type PolestarId = components['schemas']['PolestarID']
export type PolestarIdAuthorization = components['schemas']['PolestarIDAuthorization']
// Bmw is what the user is shown again of their BMW CarData connection: the last four
// characters of the client ID they created, and when; BmwDevice is BMW's device flow
// started with it: the code to enter on BMW's page, and when to ask whether it was.
export type Bmw = components['schemas']['Bmw']
export type BmwDevice = components['schemas']['BmwDevice']
// PleosKeys is what the user is shown again of the account's key of each brand of the
// Pleos Vehicle Data API; PleosKey of one brand's, PleosCredentialUpdate a new one,
// its two values.
export type PleosKeys = components['schemas']['PleosKeys']
export type PleosKey = components['schemas']['PleosKey']
export type PleosCredentialUpdate = components['schemas']['PleosCredentialUpdate']
// PleosBrand is the brand of a key, in the API's own words.
export type PleosBrand = keyof PleosKeys
export const pleosBrands: PleosBrand[] = ['hyundai', 'kia', 'genesis']
// SkodaKey is what the user is shown again of the account's key of the MyŠkoda Public
// API: its last four characters, when it was given and when it expires; SkodaCredentialUpdate
// a new one, with the VIN of each car it reads (the API lists none).
export type SkodaKey = components['schemas']['SkodaKey']
export type SkodaCredentialUpdate = components['schemas']['SkodaCredentialUpdate']
// Renault is what the user is shown again of the account's sign-in to My Renault (the
// same account as MyDacia and MyAlpine): the last four characters of its person id, when
// the owner signed in, and whether Renault refused it since; never the password nor the
// token. RenaultSignIn is a sign-in's answer: the connection, or the address the one-time
// code was sent to, which RenaultSignInBody and the code complete.
export type Renault = components['schemas']['Renault']
export type RenaultSignIn = components['schemas']['RenaultSignIn']
export type RenaultSignInBody = components['schemas']['RenaultSignInBody']
