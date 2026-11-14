---
description: "The web interface of Runsten: the supported cars, the app on a phone's home screen, the state of the car, the connection, your Volvo key, settings and preferences, privacy and accessibility."
---

# The web interface

The web interface works on a phone as well as a desktop, in English, French, German, Italian, Dutch, Norwegian, Spanish and Swedish, light or dark. Each vehicle has five tabs: **State**, **[Health](./health.md)**, **Trips**, **Charges** and **Stats**. The [battery](./battery.md)'s estimated capacity is part of its health. An account with several vehicles opens on the **[Garage](./garage.md)**, which compares them.

Runsten reads your car from its manufacturer:

- **Volvo**, through the Volvo Cars API: a Volvo ID, and on some instances a key of your own ([Your Volvo key](#your-volvo-key)).
- **Polestar**, through Polestar's Data Portal: a credential of your own ([Your Polestar](./polestar.md)); outside the EU and the EEA, on a self-hosted instance, through the Polestar app's API with your Polestar ID (beta, [Your Polestar, with the Polestar ID](./polestar-id.md)).
- **Tesla**, through the Owner API of the Tesla app: a refresh token of your Tesla account ([Your Tesla](./tesla.md)).
- **BMW and Mini**, through BMW CarData: a client ID of your own, approved with a code on BMW's page ([Your BMW or Mini](./bmw.md)).
- **Hyundai, Kia and Genesis** (beta), through the Pleos Vehicle Data API: a key of your own per brand, created in the Pleos Playground ([Your Hyundai, Kia or Genesis](./hyundai-kia.md)).
- **Škoda** (beta), through the MyŠkoda Public API: a key of your own, created in the MyŠkoda app, and the VIN of each car ([Your Škoda](./skoda.md)).
- **Renault, Dacia and Alpine** (beta, unofficial), through the My Renault app's API: the email and password of your account, with a one-time code for some ([Your Renault, Dacia or Alpine](./renault.md)).

<Screen view="state" alt="The State tab: charge level, range, odometer and last position." />

## Installing it on a phone

Runsten installs on a phone like an app: its own icon, full screen, without the browser's bars.

- **iPhone**: open your instance in Safari, select **Share**, then **Add to Home Screen**.
- **Android**: open it in Chrome, open the menu, then **Add to home screen** (or **Install app**).

On an iPhone, the app takes Safari's session as it is added, then keeps its own: signing out of one leaves the other signed in. Android installs it only from an `https` address ([Reverse proxy](../self-hosting/reverse-proxy.md)).

## State

The car as last read: its state of charge and range, whether it is charging, its odometer and its last position. The page opens on what the car is doing: parked, driving or charging, then what runs, each said in a few words with an icon: the charge's power, the climate (heating or cooling to its setpoint, or preconditioning), the battery heated, Sentry Mode; and when its values were last read. The tiles below keep the details, and each says in a green pill in its title what of it runs: the battery its heater, the vehicle its climate, the doors and windows Sentry Mode. The charge's pill says what the charge does or has done: charging or done, in green; scheduled or discharging, plain; failed, in red; none while it is idle or its status unknown. A car that sleeps (a Tesla) and is asleep, or out of reach, says so there too, with since when, the line tinted. Asleep, Runsten lets it sleep, and its values are those it had when it fell asleep; out of reach, Runsten reads it again as soon as Tesla can reach it. The battery comes first, across the page; then the charge, beside the doors and windows: while a charge runs (or discharges, or fails), its power, large, with, as far as the car tells them, the current, voltage and phases under it, when the car expects it to end (full, or at its target, as the car says), and the current limit set; otherwise the charge's type and whether the cable is connected, the pill telling a charge done or one scheduled. The vehicle's tile gives the odometer, beside the last position on a map, and the **Activity** ends the page: the state of charge of the last day or week over what the car was doing (driving, charging, parked, asleep). The totals of a longer period, and the distance of each day, are on the [Statistics](./statistics.md) page. Each value tells when it was last checked and since when it holds, and is marked as old when it is. When the car's own latest measure is more than half an hour old (a parked Polestar sends its readings when it chooses), the page tells when it was.

Beside the charge, the **Doors and windows** tile says whether the car is locked and what of it is open: "Locked · All closed", or the doors, lids and windows open or ajar, named; Sentry Mode, when a Tesla watches, in its title. It shows only for a car that tells it, with when it was checked; a Polestar often sends its doors hours after it measured them, and the tile then tells when it did. A door or window the car does not tell is left out, never taken as closed: a car that tells its doors but not its windows says its doors closed, not all. Under them, the tile shows the **tyres**, laid out as the wheels: a line each, its pressure in bar where the car gives it (a Tesla, a Polestar), its warning otherwise (a Volvo); a tyre that warns is colored, its warning beside it, and a pill says that they need attention, nothing being said of tyres that are fine; the tile is then named **Doors, windows and tyres**, or **Tyres** for a car that tells its tyres but not its doors. The [Health](./health.md) page keeps their details: the recommended pressures, and the pressures over time. With [MQTT](./mqtt.md#home-assistant-entities), Home Assistant gets each of them, and a **Left open** entity that turns on once the car has stood parked and open for 10 minutes.

A warning shows on every page of the vehicle when nothing new is read of it, with what to do ([Connection](#connection)).

## Connection

The **Connection** page (in the menu under your name) tells whether Runsten reads your vehicles, and if not, why: the Volvo ID and when it was authorized, your Polestar credential ([Your Polestar](./polestar.md)), your Tesla refresh token ([Your Tesla](./tesla.md)), and for each vehicle its latest reading and the next one due. When nothing new is read of a vehicle, its pages say why:

- **Re-authentication required**: a Volvo authorization lasts 6 months at most. Select **Reconnect the Volvo ID**: nothing already read is lost.
- **No Volvo key given**, or **Volvo refused the key**: on an instance without a Volvo key of its own, each account gives its own ([Your Volvo key](#your-volvo-key)). A key regenerated, or whose application was deleted on the developer portal, is refused: give the new one, the Volvo ID stays connected.
- **Quota reached**: the day's calls of the Volvo key are used up. Reading resumes by itself when the quota is renewed; the trips and charges missed meanwhile are found afterwards, marked as reconstructed.
- **Paused by Volvo**: Volvo limited the number of calls. Reading resumes by itself.
- **The market of the vehicle is not served**: the Volvo Cars API does not serve the market the car was sold in, and refuses every call about it. Runsten marks the car as not supported and no longer reads it; neither you nor the instance can change that. Should Volvo serve its market one day, connect the Volvo ID again: Runsten tries the car again.
- **Polestar refused the credential**, **no Polestar credential given**, **the vehicle is no longer shared with the credential**, or **read in part**: a Polestar's credential expired, was removed, no longer reads the car, or lacks one of the five domains ([When it is not read](./polestar.md#when-it-is-not-read)).
- **The Polestar ID refused the sign-in**, **no Polestar ID connected**, or **the car is no longer in the Polestar ID**: a Polestar read with the Polestar ID must be connected again ([When it is not read](./polestar-id.md#when-it-is-not-read)).
- **Tesla refused the refresh token**, **no Tesla refresh token given**, or **the vehicle is no longer in the Tesla account**: a Tesla's token was revoked, removed, or the car left the account ([When it is not read](./tesla.md#when-it-is-not-read)).
- **BMW refused the connection**, **no BMW connected**, or **the BMW account is no longer the car's main user**: a BMW's connection must be made again ([When it is not read](./bmw.md#when-it-is-not-read)); and **stream held by another application**: Home Assistant's CarData integration or evcc holds BMW's only stream ([One application on the stream](./bmw.md#one-application-on-the-stream)).
- **Pleos refused the key**, **no Pleos key given**, or **the car no longer shares its data with the key**: a Hyundai's, Kia's or Genesis's key must be given again ([When it is not read](./hyundai-kia.md#when-it-is-not-read)); and **key shared with another application**: only one application reads with a Pleos key at a time ([One key, one application](./hyundai-kia.md#one-key-one-application)).
- **Škoda refused the key**, **no MyŠkoda key given**, or **the key no longer covers this car**: a Škoda's key expired, was removed, or no longer reads the car ([When it is not read](./skoda.md#when-it-is-not-read)).
- **Renault refused the sign-in**, or **Renault refused the email or the password**: the sign-in of a Renault, a Dacia or an Alpine must be made again ([When it is not read](./renault.md#when-it-is-not-read)).
- **The collector seems stopped**: nothing is read on the instance's side. Whoever runs the instance looks into it ([Troubleshooting](../self-hosting/troubleshooting.md#nothing-new-is-read)).

### Your Volvo key

Volvo counts its daily quota of calls per application key. An instance set up with a Volvo application of its own reads every vehicle with it: there is nothing to give. Otherwise the Connection page asks for your key first, then for your Volvo ID:

1. Sign in with your Volvo ID on the [Volvo Cars developer portal](https://developer.volvocars.com/).
2. Under **Your API applications**, create an application: any name will do. Do not publish it: it needs no redirect URI, no terms and no review.
3. Copy its **primary VCC API key**, and paste it on the Connection page. It is never shown again, only its last four characters.
4. Then **Connect a Volvo ID**, and log in with the Volvo ID of your car.

The key's quota is yours alone: 10,000 calls a day for each Volvo API, about seven times what a car driven ten hours a day needs. **Replace the key** on the Connection page after regenerating it on the portal: the Volvo ID stays connected.

## Settings and preferences

The bar names the vehicle shown, on every page. Its menu lists your other vehicles, each opened on the same tab, with the same period, and leads to the vehicle's settings; with several vehicles, **All vehicles** comes first and leads to the [Garage](./garage.md), which the bar then names. The menu under your name holds the account's settings, the Connection page, your preferences and the keyboard shortcuts (press **?** to list them).

- **Settings** (in the menu under your name) are the account's, in four areas: **Costs**, the currency, the places and their tariffs, and the costs left without a charge ([Places, tariffs and costs](./costs.md)); **Vehicles**, a way to each vehicle's own settings; **Integrations**, the access tokens and MQTT ([Home Assistant and MQTT](./mqtt.md)); **Account**, the download of your data. Under **Account**, **About Runsten** shows the release the instance runs, and a newer one when the instance checks ([Updating](../self-hosting/updating.md#knowing-a-release-is-out)).
- **A vehicle's settings** (the menu of its name in the bar), in two parts: **Vehicle**, its model and onboard charger ([Vehicle models](./vehicle-models.md)) and, at the bottom, **Delete this vehicle**, for a car sold or no longer followed: its readings, trips, charges, imported history and the costs entered for them are deleted for good, after a confirmation. Download your data in the settings first to keep a copy. Its connection stays and keeps reading your other vehicles; a new connection that lists the car again adds it as a new vehicle, without that history. **Imported history**: what was imported from other apps ([Importing your history](./importing.md)).
- **Preferences** (the menu under your name, which also signs out): the language, the appearance, automatic, light or dark, the clock, automatic, 24-hour or 12-hour, whether the keyboard shortcuts are on, and, where the instance shows maps, whether to show them, kept in the browser.

## Privacy

No map is loaded unless the instance names a tile server ([Maps](../self-hosting/maps.md)): positions are then coordinates with a link to OpenStreetMap, and no map service learns where the car is unless you click it. With maps, the tile server sees your IP address and the areas shown; you can hide them in your preferences. The browser keeps only the session cookie, the chosen language, the chosen appearance, the chosen clock, whether the keyboard shortcuts are on and whether the maps are shown.

## Accessibility

Every view is checked with axe, on a phone and a desktop, light and dark, with no WCAG 2.1 A or AA violation. Each chart is described in a sentence, and its figures are in a table for screen readers.
