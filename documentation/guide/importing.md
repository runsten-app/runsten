---
description: "Import your Volvo's trips and charges from before Runsten read it: from Aximote, with a personal access token, or from the files of the Volvo Data Portal (EU Data Act)."
---

# Importing your history

Runsten reads your car from the day you connect it. What came before may be in another app, or at Volvo Cars: Runsten imports it from **Aximote**, with a personal access token of its Pro offer, and from the files of the **Volvo Data Portal**, which Volvo Cars gives an owner in the EU who asks for their car's data.

## From Aximote

The import from Aximote is in **beta**: it follows Aximote's documentation, and has not yet been tried on many real accounts. If a trip or a charge looks wrong, tell us; importing again after a fix replaces what was imported.

1. In the Aximote app, create a personal access token (it starts with `axm_pat_`).
2. In Runsten, open the vehicle's settings (the button by its name) and, under **Imported history**, paste it in **Aximote token**, then **Find my vehicles**.
3. Runsten lists the vehicles of your Aximote account. The one with this vehicle's VIN is chosen; when none has it, choose the one whose history this is: Runsten never guesses.
4. **Import**. The import goes on in the background: you can leave the page. Its progress, then its report, show in the same place.

Runsten keeps the token encrypted while the import runs and erases it as soon as it ends, whether it succeeded or not. It is never shown again, and never leaves Runsten for anything but Aximote. Revoking it in Aximote afterwards is harmless.

### What is imported

- **Trips**: their exact times, distance, energy used and places, named as Aximote names them ("Lyon, France"). Neither state of charge nor odometer: Aximote gives none.
- **Charges**: the charging session's times, the energy charged, the average power, the cost Aximote gives, and the state of charge from the battery's energy at both ends, when Aximote gives it. Never AC or DC: Aximote does not tell, and Runsten does not guess it from the power.
- **Tracks** of the trips, when Aximote gives them: the path driven. A track is fine-grained location data: Runsten keeps it with the trip, reads it only to draw it on the trip's map, and deletes it with the import.

Only what started before Runsten's first reading of the vehicle is imported: it never duplicates what Runsten saw. The report tells how many events were imported, how many were left because they start after that first reading, and how many were unfinished or out of bounds (a trip still under way, a charge in progress). Fuel stops are left without being counted.

Importing again replaces what was imported, event by event: nothing is counted twice. Deleting the import, in the same place, removes every event imported from Aximote; what Runsten read stays.

### When it fails

- **Aximote refused the token**: it was mistyped, revoked, or the Pro offer ended. Create another one.
- **Aximote kept failing**: try again later. When Aximote asks to wait, the import waits, then resumes where it stopped; a restart of Runsten resumes it too.

## From the Volvo Data Portal

In the EU, the Data Act lets the user of a connected car ask its maker for the data it collects. Volvo Cars answers on its [Data Portal](https://data.volvocars.com/), which Runsten reads the files of. This import is in **beta** too: it follows the files an owner published, and has not yet been tried on many.

1. On the Data Portal, sign in with your Volvo ID, choose the car, and ask for the **trips** and the **waypoints**, over the period you want. The portal takes several hours to prepare them.
2. Download the two CSV files it gives.
3. In Runsten, open the vehicle's settings and, under **Imported history**, choose them in **Import from the Volvo Data Portal**: the trips file, required, and the waypoints file, optional. Then **Import**.

The import runs at once, and its report shows in the same place. Files of 32 MB at most together: a year of waypoints is a few.

### What is imported

- **Trips**: their exact times, the positions at both ends, the odometer at both ends and the distance (rounded to the kilometer by the portal), and the energy used, when the portal gives it (a trip of 0 kWh over a distance has none). No place names: Runsten names the positions as it names those it reads. No state of charge: the portal gives none.
- **Tracks**: the waypoints, about one a minute while driving, with a precise position. Those of a trip, from its start to its end, make its track. A track is fine-grained location data: Runsten keeps it with the trip, reads it only to draw it on the trip's map, and deletes it with the import.
- **No charges**: the portal gives none.

What the portal gives may be shorter than the period asked for: an owner found the last month missing. Fuel, regeneration and heading are given as 0 for an electric car: Runsten leaves them out.

As for Aximote, only what started before Runsten's first reading is imported; the report tells how many trips were imported, how many were left because they start after that reading, and how many rows were unreadable or out of bounds (a trip that ends before it starts). Importing the same files again replaces the trips, one by one, by their start time. Deleting the import removes every trip imported from the portal, with its track.

A file that is not the portal's (another app's export, the waypoints given as the trips) is refused, and nothing is imported.

See [Trips and charges](./trips-and-charges.md) for how imported events show.
