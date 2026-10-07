---
description: "The web interface of Runsten: the state of the car, the connection and your Volvo key, settings and preferences, privacy and accessibility."
---

# The web interface

The web interface works on a phone as well as a desktop, in English, French and Swedish, light or dark. Each vehicle has five tabs: **State**, **Trips**, **Charges**, **Stats** and **Battery**.

<Screen view="state" alt="The State tab: charge level, range, odometer and last position." />

## State

The car as last read: its state of charge and range, whether it is charging, its odometer and its last position. Each value tells when it was last checked and since when it holds, and is marked as old when it is.

A warning shows on every page of the vehicle when nothing new is read of it, with what to do ([Connection](#connection)).

## Connection

The **Connection** page (in the menu under your name) tells whether Runsten reads your vehicles, and if not, why: the Volvo ID and when it was authorized, and for each vehicle its latest reading and the next one due. When nothing new is read of a vehicle, its pages say why:

- **Re-authentication required**: a Volvo authorization lasts 6 months at most. Select **Reconnect the Volvo ID**: nothing already read is lost.
- **No Volvo key given**, or **Volvo refused the key**: on an instance without a Volvo key of its own, each account gives its own ([Your Volvo key](#your-volvo-key)). A key regenerated, or whose application was deleted on the developer portal, is refused: give the new one, the Volvo ID stays connected.
- **Quota reached**: the day's calls of the Volvo key are used up. Reading resumes by itself when the quota is renewed; the trips and charges missed meanwhile are found afterwards, marked as reconstructed.
- **Paused by Volvo**: Volvo limited the number of calls. Reading resumes by itself.
- **The collector seems stopped**: nothing is read on the instance's side. Whoever runs the instance looks into it ([Troubleshooting](../self-hosting/troubleshooting.md#nothing-new-is-read)).

### Your Volvo key

Volvo counts its daily quota of calls per application key. An instance set up with a Volvo application of its own reads every vehicle with it: there is nothing to give. Otherwise the Connection page asks for your key first, then for your Volvo ID:

1. Sign in with your Volvo ID on the [Volvo Cars developer portal](https://developer.volvocars.com/).
2. Under **Your API applications**, create an application: any name will do. Do not publish it: it needs no redirect URI, no terms and no review.
3. Copy its **primary VCC API key**, and paste it on the Connection page. It is never shown again, only its last four characters.
4. Then **Connect a Volvo ID**, and log in with the Volvo ID of your car.

The key's quota is yours alone: 10,000 calls a day for each Volvo API, about seven times what a car driven ten hours a day needs. **Replace the key** on the Connection page after regenerating it on the portal: the Volvo ID stays connected.

## Settings and preferences

- **Settings** (the cog in the bar) are the account's: the currency, the places and their tariffs, and the costs left without a charge ([Places, tariffs and costs](./costs.md)).
- **A vehicle's settings** (the button by its name): its model and onboard charger ([Vehicle models](./vehicle-models.md)).
- **Preferences** (the menu under your name, which also signs out): the language and the appearance, automatic, light or dark, kept in the browser.

## Privacy

Positions are coordinates with a link to OpenStreetMap, opened only on a click: no map is loaded, so no map service learns where the car is. The browser keeps only the session cookie, the chosen language and the chosen appearance.

## Accessibility

Every view is checked with axe, on a phone and a desktop, light and dark, with no WCAG 2.1 A or AA violation. Each chart is described in a sentence, and its figures are in a table for screen readers.
