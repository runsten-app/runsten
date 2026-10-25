---
description: "Read a Polestar with Runsten: create your credential on Polestar's Data Portal (EU Data Act), the five domains to grant, the 90 days, and what is still in beta."
---

# Your Polestar

Runsten reads a Polestar through **Polestar's Data Portal**: the access to their car's data that the EU Data Act gives its owners. On the portal, you create a credential of your own, and give its three values to Runsten. Runsten never asks for your Polestar ID or its password. A self-hosted instance needs no Volvo application to read a Polestar.

::: warning Beta
Polestar support is in beta. It was built against a simulator of the Data Portal, from its specification and responses of real cars, and has not yet been checked on a car of our own. What is not known yet is **how often a car sends its readings while it drives**. If it sends them often, trips are found as for a Volvo; if it sends them rarely, trips are only found from the odometer, as reconstructed trips, without their route. Tell us how yours behaves.
:::

## Create your credential

1. Sign in with your Polestar ID on [Polestar's Data Portal](https://data-portal.polestar.com/).
2. In its API section, create a credential: any name will do.
3. Grant it the five domains Runsten reads:
   - **battery** (state of charge, range, charging),
   - **odometer**,
   - **location**,
   - **availability** (whether the car is driving),
   - **target state of charge** (the charge limit).

   The others (doors, health, climate, timers) are not needed: Runsten does not read them.
4. If the portal asks which cars the credential reads, choose yours.
5. On the credential's page, copy the three values Runsten needs, as the portal labels them:
   - **App client ID**,
   - **Client secret** (the portal shows it once only),
   - **Expected x-client-id header**.

   The page also shows the M2M token endpoint and the credential's expiry: Runsten does not need them.

## Give it to Runsten

On the **Connection** page (in the menu under your name), under **Polestar**, paste the three values, then **Save the credential**. Runsten checks it first: it asks Polestar for a token and lists the cars the credential reads.

- A credential Polestar refuses is not saved: check its three values, and that it has not expired.
- A credential that reads no car is not saved either: check on the portal that it reads yours.
- Otherwise the cars it reads are added to your account, and read from the collector's next pass.

The credential is stored encrypted and is only ever sent to Polestar. It is never shown again: the Connection page shows the last four characters of its client ID, when it was given and when it expires.

## Choose its variant

The Data Portal gives the car's VIN, not its model: Runsten reads the model year from the VIN, but not whether it is a Polestar 2, 3 or 4. Several variants fit almost every model year: in the vehicle's settings (the button by its name), choose yours among the Polestar ones, those of its model year first ([Vehicle models](./vehicle-models.md)). The variant sets its model, and the net battery capacity its energies rest on: without it, the trips and charges have no energy, since the Data Portal does not report the battery's capacity.

## Every 90 days

A credential of the Data Portal reads for **90 days**, then expires: create a new one on the portal, and give it in place of the old one with **Replace the credential**. Nothing already read is lost. The Connection page reminds you two weeks before, counted from the day you gave the credential.

## How it is read

- Parked, Runsten reads the battery and whether the car is driving every 10 minutes, and the odometer, the position and the charge limit every hour. While the car drives or charges, it reads all but the charge limit every minute. When a trip or a charge ends, it reads the odometer and the position at once.
- The data is the last the car sent. A parked car sends it when it chooses, and the values may be hours old however recently Runsten read them: the vehicle's page tells when the car last measured them, once that is more than half an hour ago.
- Polestar allows 10,000 calls a day per credential (according to other owners: Polestar does not document it). That is more than a car that would drive and charge all day long needs, and it is your credential's alone.

## When it is not read

The vehicle's pages warn when nothing new is read of it; the Connection page says why:

- **Polestar refused the credential**: it expired, or was deleted on the portal. Create a new one and give it.
- **No Polestar credential given**: it was removed. Give a new one.
- **The vehicle is no longer shared with the credential**: the portal no longer lets the credential read the car. Let it read the car again on the portal, then give the credential again: the car is read again.
- **Read in part**: the credential was not granted one of the five domains, and what it gives is not read. Runsten tries it again a day later; create a credential with the five domains to read it all.
- **Paused by Polestar** or **Polestar quota reached**: reading resumes by itself.

## Removing the credential

**Remove the credential** erases it from Runsten. Your Polestar cars stay, with their history, and are no longer read until you give another. Delete it on the portal too, if you no longer use it.
