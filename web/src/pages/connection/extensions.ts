import type { Component } from 'vue'
import type { Brand } from './model/brands'

// extensionBrandSections replaces none of the brands' sections: the public build connects
// each brand its own way (Volvo ID, Polestar's credentials, Tesla's refresh token). A build
// that connects a brand otherwise (a sign-in with the manufacturer) sets the brand's
// component here: shown in that tab in place of the core's sections, above its vehicles,
// it reads what it needs itself, and its headings are h2s. The tab is shown while it is
// set, whatever the brand's flag. A sign-in that comes back to connection?<brand>=<outcome>
// (connected, no_vehicle, too_many_vehicles) has the page tell its outcome.
export const extensionBrandSections: Partial<Record<Brand, Component>> = {}
