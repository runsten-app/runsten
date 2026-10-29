---
description: "The web interface of Runsten: the supported cars, the state of the car, the connection, your Volvo key, settings and preferences, privacy and accessibility."
---

# The web interface

The web interface works on a phone as well as a desktop, in English, French and Swedish, light or dark. Each vehicle has five tabs: **State**, **[Health](./health.md)**, **Trips**, **Charges** and **Stats**. The [battery](./battery.md)'s estimated capacity is part of its health.

Runsten reads your car from its manufacturer:

- **Volvo**, through the Volvo Cars API: a Volvo ID, and on some instances a key of your own ([Your Volvo key](#your-volvo-key)).
- **Polestar** (beta), through Polestar's Data Portal: a credential of your own ([Your Polestar](./polestar.md)).
- **Tesla** (beta), through the Owner API of the Tesla app: a refresh token of your Tesla account ([Your Tesla](./tesla.md)).

<Screen view="state" alt="The State tab: charge level, range, odometer and last position." />

## State

The car as last read: its state of charge and range, whether it is charging, its odometer and its last position. Each value tells when it was last checked and since when it holds, and is marked as old when it is. When the car's own latest measure is more than half an hour old (a parked Polestar sends its readings when it chooses), the page tells when it was.

A warning shows on every page of the vehicle when nothing new is read of it, with what to do ([Connection](#connection)).

## Connection

The **Connection** page (in the menu under your name) tells whether Runsten reads your vehicles, and if not, why: the Volvo ID and when it was authorized, your Polestar credential ([Your Polestar](./polestar.md)), your Tesla refresh token ([Your Tesla](./tesla.md)), and for each vehicle its latest reading and the next one due. When nothing new is read of a vehicle, its pages say why:

- **Re-authentication required**: a Volvo authorization lasts 6 months at most. Select **Reconnect the Volvo ID**: nothing already read is lost.
- **No Volvo key given**, or **Volvo refused the key**: on an instance without a Volvo key of its own, each account gives its own ([Your Volvo key](#your-volvo-key)). A key regenerated, or whose application was deleted on the developer portal, is refused: give the new one, the Volvo ID stays connected.
- **Quota reached**: the day's calls of the Volvo key are used up. Reading resumes by itself when the quota is renewed; the trips and charges missed meanwhile are found afterwards, marked as reconstructed.
- **Paused by Volvo**: Volvo limited the number of calls. Reading resumes by itself.
- **The market of the vehicle is not served**: the Volvo Cars API does not serve the market the car was sold in, and refuses every call about it. Runsten marks the car as not supported and no longer reads it; neither you nor the instance can change that. Should Volvo serve its market one day, connect the Volvo ID again: Runsten tries the car again.
- **Polestar refused the credential**, **no Polestar credential given**, **the vehicle is no longer shared with the credential**, or **read in part**: a Polestar's credential expired, was removed, no longer reads the car, or lacks one of the five domains ([When it is not read](./polestar.md#when-it-is-not-read)).
- **Tesla refused the refresh token**, **no Tesla refresh token given**, or **the vehicle is no longer in the Tesla account**: a Tesla's token was revoked, removed, or the car left the account ([When it is not read](./tesla.md#when-it-is-not-read)).
- **The collector seems stopped**: nothing is read on the instance's side. Whoever runs the instance looks into it ([Troubleshooting](../self-hosting/troubleshooting.md#nothing-new-is-read)).

### Your Volvo key

Volvo counts its daily quota of calls per application key. An instance set up with a Volvo application of its own reads every vehicle with it: there is nothing to give. Otherwise the Connection page asks for your key first, then for your Volvo ID:

1. Sign in with your Volvo ID on the [Volvo Cars developer portal](https://developer.volvocars.com/).
2. Under **Your API applications**, create an application: any name will do. Do not publish it: it needs no redirect URI, no terms and no review.
3. Copy its **primary VCC API key**, and paste it on the Connection page. It is never shown again, only its last four characters.
4. Then **Connect a Volvo ID**, and log in with the Volvo ID of your car.

The key's quota is yours alone: 10,000 calls a day for each Volvo API, about seven times what a car driven ten hours a day needs. **Replace the key** on the Connection page after regenerating it on the portal: the Volvo ID stays connected.

## Settings and preferences

- **Settings** (the cog in the bar) are the account's: the currency, the places and their tariffs, and the costs left without a charge ([Places, tariffs and costs](./costs.md)). At the bottom, **About Runsten** shows the release the instance runs, and a newer one when the instance checks ([Updating](../self-hosting/updating.md#knowing-a-release-is-out)).
- **A vehicle's settings** (the button by its name): its model and onboard charger ([Vehicle models](./vehicle-models.md)), its imported history ([Importing your history](./importing.md)), and, at the bottom, **Delete this vehicle**, for a car sold or no longer followed: its readings, trips, charges, imported history and the costs entered for them are deleted for good, after a confirmation. Download your data in the settings first to keep a copy. Its connection stays and keeps reading your other vehicles; a new connection that lists the car again adds it as a new vehicle, without that history.
- **Preferences** (the menu under your name, which also signs out): the language, the appearance, automatic, light or dark, the clock, automatic, 24-hour or 12-hour, whether the keyboard shortcuts are on, and, where the instance shows maps, whether to show them, kept in the browser.

## Privacy

No map is loaded unless the instance names a tile server ([Maps](../self-hosting/maps.md)): positions are then coordinates with a link to OpenStreetMap, and no map service learns where the car is unless you click it. With maps, the tile server sees your IP address and the areas shown; you can hide them in your preferences. The browser keeps only the session cookie, the chosen language, the chosen appearance, the chosen clock, whether the keyboard shortcuts are on and whether the maps are shown.

## Accessibility

Every view is checked with axe, on a phone and a desktop, light and dark, with no WCAG 2.1 A or AA violation. Each chart is described in a sentence, and its figures are in a table for screen readers.
