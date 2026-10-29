import { readonly, ref } from 'vue'

// The vehicle whose pages were seen last, so that the account's pages (settings,
// connection, preferences) keep it in the bar: the way back to it. In memory only: a
// reload falls back to the first vehicle, and nothing of it is stored.
const last = ref<string>()

export const rememberedVehicle = readonly(last)

export function rememberVehicle(vehicle: string) {
  last.value = vehicle
}
