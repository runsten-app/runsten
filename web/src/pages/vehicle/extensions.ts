import type { Component } from 'vue'

// extensionVehicleActions adds none: the public build's vehicle page shows what the
// vehicle reports, and sends it nothing. A downstream build replaces this file with its
// own components, such as controls that send the vehicle a command, each given the
// vehicle's ID (its `vehicle` prop) and shown in order between the state and the recent
// summary; each decides whether it has anything to show for that vehicle.
export const extensionVehicleActions: Component[] = []
