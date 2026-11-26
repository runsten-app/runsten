---
description: "Read a Ford with Runsten: FordConnect Query, the application you create on Ford's developer portal, its redirect address, the sign-in to renew every year, what is read and its limits."
---

# Your Ford

Runsten reads a Ford through **FordConnect Query**: the read-only access to their car's data that Ford opens to its owners on its developer portal, free of charge. You create an **application of your own** there, give its client ID and its secret to Runsten, then sign in on Ford's own page, where you choose the car and consent. Runsten never asks for the password of your Ford account. A self-hosted instance needs nothing else from Ford.

::: warning Beta, and not checked on a real car
Ford support is in beta. It was built from Ford's specification of the API and from the free clients that already read it (evcc, the Home Assistant integration for FordConnect Query), against a simulator: **no real car has been read yet**. What Ford does not publish is assumed until then — how often a car sends its state, how many calls Ford accepts, the units and the words of what a car sends, how the car's model is named — and may differ from what Runsten expects. What the Connection page tells you, and the [troubleshooting](#when-it-is-not-read) below, are the first things to look at.

The source is **off by default**: the administrator of an instance turns it on with the flag `source.ford` ([Feature flags](../self-hosting/flags.md)). Until it is on, the Ford tab of the Connection page shows only for an account that already has a Ford connection or car: it says Ford is turned off, and a connection already made stays shown, and can be removed.
:::

Runsten is not affiliated with Ford. It only reads: FordConnect Query offers no command, and Runsten sends none.

## An application for your personal use

Ford's developer terms grant an application created on the portal for **a personal use**: it may not be distributed, nor serve other people, without a separate agreement with Ford. Yours reads your own car, with your own consent: create it yourself, and do not share its client ID nor its secret.

## What you need

- A Ford set up in the Ford app, its modem activated, in **your Ford account**: the sign-in lists the cars of that account.
- An instance of Runsten with the flag `source.ford` on.
- **One Ford per Runsten account** for now: a sign-in reads one car, the one you choose on Ford's page, and an account holds one Ford connection.

Runsten was written against Ford's **European** portal. Ford has one for the United Kingdom and one for North America too, which nobody has tried with Runsten.

Only the **electric** Fords have their figures in Runsten's catalog: the Puma Gen-E, the Explorer, the Capri, the Mustang Mach-E, the E-Transit, the E-Transit Custom, the E-Tourneo Custom, the E-Transit Courier and the E-Tourneo Courier ([Vehicle models](./vehicle-models.md)). A plug-in hybrid is read, but has no variant yet: its trips and charges have no energy.

## The redirect address

Ford sends your browser back to an address after the sign-in, and it must be declared in your application beforehand. The **Ford** tab of the Connection page (in the menu under your name) shows the address to declare: **copy it from there**. It is the one Runsten will ask Ford with, and Ford refuses a sign-in whose address is not, to the letter, one the application declares.

Ford accepts an address in **https**, or in http on **localhost** only. So the address depends on the one you open Runsten at:

- **In https**: the address is the instance's own, followed by `/auth/ford/callback`, as `https://runsten.example.org/auth/ford/callback`. Ford sends you back to Runsten, which finishes by itself.
- **In http**, on a local network for instance (`http://192.168.1.20:8082`): Ford would refuse that address. The one to declare is `http://localhost/auth/ford/callback`. Nothing answers there: after the sign-in, your browser shows a page that does not load, and you paste its address back into Runsten ([below](#the-address-pasted-back)).

On an instance you host, the address Runsten is opened at is its `RUNSTEN_APP_URL`: an https one behind a [reverse proxy](../self-hosting/reverse-proxy.md), and Ford comes back to it; so does it in http on the machine itself (`http://127.0.0.1:8082`). Without one, the address is pasted back.

## Create your application

You do this once, on a computer, on [Ford's developer portal](https://developer.ford.com/developer-eu).

1. Create a developer account, **with the same email address as your Ford account**, the one of the Ford app that holds your car: owners report that Ford links the two by it. Ford asks for a second factor at sign-in.
2. Register an application. On the European portal, tick the box of the EU Data Act's data request, then **Continue**.
3. Tick **Yes - This app is for personal use**. The VAT fields under it are for a company's application.
4. Fill in the names, the description and the purpose in plain words, such as `Runsten`, `Logbook of my car` and `Reading the trips and charges of my own car`. **Letters without accents, digits and spaces only**: the portal fails on an apostrophe or an accented letter, in these fields as in the name of your developer profile.
5. Under **Which device does your application service?**, tick every category.
6. Under **Allowed Redirect URI(s)**, paste the redirect address the Connection page shows, without a slash at its end.
7. **Continue**, tick **All Available Data Categories**, then submit.
8. Copy the application's **client ID** and its **secret**. Both stay on the portal's dashboard, where the secret can be generated anew: Runsten must then be given the new one.

Ford takes **up to two hours** to make a new application ready, sometimes longer. Until then its sign-in page shows an error ("something went wrong"): try again later, there is nothing to change.

## Connect it in Runsten

On the **Connection** page, in the **Ford** tab:

1. Paste the client ID and the secret, then **Sign in to Ford**.
2. On Ford's page, sign in with your Ford account, **choose the car** and accept. Your password stays between you and Ford.
3. Ford sends you back to Runsten, which says your Ford is connected.

Runsten then exchanges Ford's code with your application's secret, reads which car the sign-in covers, and adds it to your account, within the number of vehicles it may have. The collector reads it from its next pass. A sign-in must be finished within **15 minutes**; after that, start again.

The client ID, the secret and the token Ford gives are stored encrypted, and only ever sent to Ford. Nothing of them is shown again but the client ID's last four characters, to find the application on the portal, when you connected, and the day the sign-in ends.

### The address pasted back

On an instance Ford cannot send you back to, the second step ends differently:

1. After you accept, Ford sends your browser to an address starting with `http://localhost/auth/ford/callback`. The browser says the page cannot be reached: that is expected.
2. Copy **the whole address** from the browser's address bar.
3. Back in Runsten's tab, paste it under **Address Ford sent you to**, then **Connect**.

The address holds a code that works once, for a few minutes. If Runsten says it expired or was already used, select **Start again**.

## Once a year

Ford asks for a new sign-in **a year after yours**. The Connection page shows the day: **Sign in again before**. From **two weeks before**, the Ford tab and the menu under your name carry a dot, and the tab says to sign in again. **Sign in again** runs the same sign-in with a client ID and a secret you give again; your application stays as it is on the portal.

Once the year is over, Ford refuses Runsten's access and the car is no longer read, until you sign in again. Nothing already read is lost.

## What is read, and how often

Runsten reads, in one call, the battery level and the range, whether the car charges — and how: AC or DC, the current and the voltage, and their product as the power, the time left, the charge's target — whether the cable is connected, the ignition, the odometer, the position, the outside temperature, the doors, the boot, the bonnet, the windows and the central lock, and the tyres' pressures and warnings, which show on the [Health](./health.md) page. Once a day, it reads what the car is: its model, its model year, the name you gave it and its colour. What a model does not send is simply missing from its pages: the users of other clients report that an Explorer and a Capri send fewer values than a Puma Gen-E or a Mustang Mach-E, and no charge target. The responses Runsten kept are in the [readings' CSV file](./exporting.md), without Ford's identifiers of the car and of its modem in your account.

- **Every minute** while the car drives or charges, **every 5 minutes** while it is parked.
- Reading does not wake the car: Ford returns the last values the car sent, each dated by the car. Reading more often than the car sends gives nothing more.
- Ford publishes **no limit of calls**. Other clients report that two calls close together are refused: Runsten never makes two calls for a car within 20 seconds, and makes **2,000 calls a day at most**. When Ford limits the calls all the same, Runsten waits as long as Ford says, or 5 minutes, by itself.
- That limit is shared with **every other application** that reads the car with the same sign-in to Ford, such as evcc or Home Assistant. To keep the car in Home Assistant, you can let Runsten **republish the car to your MQTT broker** rather than have Home Assistant read it too ([Home Assistant and MQTT](./mqtt.md)).

**The battery's capacity is not read.** Ford's API tells one, but not what it counts: the gross capacity, the usable one, or what the pack holds today. Runsten counts a trip's and a charge's energy from the battery levels on the net capacity of the car's variant, from its catalog. Several variants of a model often fit: **choose yours** in the vehicle's settings. Until you do, its trips and charges have no energy ([Vehicle models](./vehicle-models.md)).

## When it is not read

The vehicle's pages warn when nothing new is read of it; the Connection page says why:

- **Ford refused the connection**: you withdrew the consent, its year is over, or the application's secret changed on the portal. Select **Sign in again**: what was read stays.
- **Not signed in to Ford**: the connection was removed. Sign in again.
- **The sign-in reads another car**: a sign-in reads one car, the one chosen on Ford's page, and the last one chose another. Runsten keeps nothing of the other car under this one's name. Sign in again and choose this car.
- **Paused by Ford**: Ford limited the calls. Another application may read the car too. Reading resumes by itself.
- **Daily calls to Ford reached**: Runsten's own 2,000 calls of the day are used up. Reading resumes by itself.
- **Ford unreachable**, or its sign-in failing: that is never taken for a refusal, and the connection stays as it is. Runsten reads again by itself.
- **Nothing for a day** on a car that was driven: check the car's connected services in the Ford app, and that the car has had mobile coverage.

When you connect, a message may tell why nothing was connected:

| Message | What to do |
|---|---|
| Ford's page shows an error before the sign-in | A new application takes up to two hours to be ready. Otherwise the redirect address declared at Ford is not, to the letter, the one the Connection page shows, or the client ID is mistyped. |
| The sign-in was not completed on Ford's page | Start again, and accept on Ford's page. |
| This sign-in expired, was already used, or Ford refused its code | Start again, and finish within 15 minutes. |
| This address holds no code | Copy the whole address of the page Ford sent you to, from `http://` to its end. |
| Ford refused the client ID or the secret | Check them on Ford's developer portal, then start again. |
| This sign-in reads no car | Add your car to your Ford account, in the Ford app, then start again. |
| Your account would have more vehicles than it may | Nothing is connected. |
| Ford did not answer | Nothing was stored. Start again in a moment. |
| Too many sign-ins are waiting on this instance | Try again in a few minutes. |
| Ford is not offered to your account | The administrator left `source.ford` off ([Feature flags](../self-hosting/flags.md)). |

## Removing it

**Remove the connection** erases the client ID, the secret and the token Runsten keeps. Your Ford stays, with its history, and is no longer read until you sign in again. The application stays on Ford's portal, and Ford's consent in your Ford account: remove them there if you no longer use them.
