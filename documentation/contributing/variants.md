---
description: "Add or correct a vehicle variant in the catalog of Runsten: its fields, sources and recognition rules."
---

# Contributing a variant

The catalog of models is [`internal/catalog/variants.yaml`](https://github.com/runsten-app/runsten/blob/main/internal/catalog/variants.yaml): a missing or wrong variant is a pull request on it. Its header documents every field.

## An entry

One entry per variant, a powertrain and battery over a range of model years:

- `id`: lowercase words joined by hyphens, as `ex30-er-2024`;
- `brand`, `family` exactly as the API's `descriptions.model` returns it, `name`, `years` (`[from, to]`, or `[from]` while on sale);
- `gross_kwh`, `net_kwh` when a source settles it, `chemistry`;
- `ac_max_kw`, `ac_option_kw`, `dc_max_kw`, `motor_codes`.

## Sources

Every figure needs a source: an https URL, its `level`, `manufacturer` or `secondary` (press, Wikipedia; ev-database as a cross-check only, never copied), and `for`, the figures it backs. The interface tells "from the manufacturer" from "from public sources" by these levels. Where sources disagree, a maximum power keeps the highest value, a capacity the manufacturer's, and a comment says so.

## Recognition

- `api_kwh`: the values of `batteryCapacityKWH` seen reported for the variant, each with a link to where (an issue, a forum post). A value that differs from the gross capacity is recognized only through them.
- `ambiguous_with`: two variants no reading tells apart must name each other; the user chooses between them.

## Rules

- Never remove an entry, nor change its `id`: users' choices refer to it. Correct its figures instead.
- No real VIN, anywhere: motor codes are two characters.
- `go test ./internal/catalog/...` validates the file, as Runsten does at startup. The tests that list variants change in the same pull request, and a case in `TestMatch` shows the reading that recognizes a new entry.

A corrected net capacity, or a vehicle now recognized, changes the energies of the events worked out after the update, and of the past ones after a [rebuild](../self-hosting/backup.md#rebuild).
