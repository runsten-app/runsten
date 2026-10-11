---
description: "Import your Volvo's trips and charges from before Runsten read it, from Aximote, with a personal access token."
---

# Importing your history

Runsten reads your car from the day you connect it. What came before may be in another app: Runsten imports it from **Aximote**, with a personal access token of its Pro offer.

## From Aximote

The import from Aximote is in **beta**: it follows Aximote's documentation, and has not yet been tried on many real accounts. If a trip or a charge looks wrong, tell us; importing again after a fix replaces what was imported.

1. In the Aximote app, create a personal access token (it starts with `axm_pat_`).
2. In Runsten, open the vehicle's settings (the button by its name) and, under **Imported history**, paste it in **Aximote token**, then **Find my vehicles**.
3. Runsten lists the vehicles of your Aximote account. The one with this vehicle's VIN is chosen; when none has it, choose the one whose history this is: Runsten never guesses.
4. **Import**. The import goes on in the background: you can leave the page. Its progress, then its report, show in the same place.

Runsten keeps the token encrypted while the import runs and erases it as soon as it ends, whether it succeeded or not. It is never shown again, and never leaves Runsten for anything but Aximote. Revoking it in Aximote afterwards is harmless.

## What is imported

- **Trips**: their exact times, distance, energy used and places, named as Aximote names them ("Lyon, France"). Neither state of charge nor odometer: Aximote gives none.
- **Charges**: the charging session's times, the energy charged, the average power, the cost Aximote gives, and the state of charge from the battery's energy at both ends, when Aximote gives it. Never AC or DC: Aximote does not tell, and Runsten does not guess it from the power.
- **Tracks** of the trips, when Aximote gives them.

Only what started before Runsten's first reading of the vehicle is imported: it never duplicates what Runsten saw. The report tells how many events were imported, how many were left because they start after that first reading, and how many were unfinished or out of bounds (a trip still under way, a charge in progress). Fuel stops are left without being counted.

Importing again replaces what was imported, event by event: nothing is counted twice. Deleting the import, in the same place, removes every event imported from Aximote; what Runsten read stays.

## When it fails

- **Aximote refused the token**: it was mistyped, revoked, or the Pro offer ended. Create another one.
- **Aximote kept failing**: try again later. When Aximote asks to wait, the import waits, then resumes where it stopped; a restart of Runsten resumes it too.

See [Trips and charges](./trips-and-charges.md) for how imported events show.
