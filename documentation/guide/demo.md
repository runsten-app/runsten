---
description: "Try Runsten without a car: a demo vehicle the instance simulates, with four weeks of trips and charges, and readings that keep coming. How to turn it on, connect it and remove it."
---

# The demo vehicle

A vehicle that needs **no car and no maker's account**: your instance simulates it, in its own process, with four weeks of history written at once and readings that go on coming. Use it to try Runsten before you connect a car, to show it (a talk, screenshots), or to develop an application against a real instance, its API and its pages, with data in them.

It is **off by default**. Turn it on with the flag `source.demo` ([Feature flags](../self-hosting/flags.md)):

```sh
RUNSTEN_FLAGS=source.demo=true
```

Then restart the stack. Until it is on, the Demo tab of the Connection page shows only for an account that already has a demo vehicle.

## Connect it

On the **Connection** page (in the menu under your name), in the **Demo** tab, click **Connect the demo vehicle**. It takes a few seconds: Runsten writes the vehicle's last four weeks, then derives its trips and charges from them, so that the pages are full as soon as it is connected. The collector then reads the vehicle as it reads any other, every minute while it drives or charges and every ten minutes while it is parked.

Programs connect it with `PUT /api/v1/connection/demo`, without a body, and disconnect it with `DELETE /api/v1/connection/demo` ([API](../reference/api.md)). It counts as one of your account's vehicles.

## What it simulates

One typical week, repeated: five commutes of 14 km each way, a stop at the shop on Wednesday and at the gym on Thursday, a long Saturday of 330 km with a fast charge on arriving, a visit on Sunday. The vehicle is charged at home on a 11 kW wall box each evening but Friday's, up to 80 %. It is a Volvo EX30 Single Motor Extended Range of 2024, which the catalog recognizes, in and around Ghent, in Belgium, with places that are not anyone's.

The week starts and ends in the same state, plugged in at home and charged to 80 %, from a fixed Monday long ago: the vehicle's state at any instant is the same whatever restarted since, so that its history and its readings agree, on every instance. The outside temperature follows the season and the hour. Nothing more is simulated: the consumption is the same on every road, and a charge lasts as long as its energy needs, at the power of the step (a fast charge goes down in three steps).

The vehicle is plugged in as it arrives, so that the collector, which reads it every minute while it charges, sees each charge from its start: the energy measured from the charging power matches the battery's, to within a few percent, and a trip's consumption is about 17.5 kWh per 100 km.

## Disconnect it

**Disconnect** in the same tab deletes the demo vehicle with its trips and charges, which no other source could bring back. Connecting it again starts over, with the four weeks before that moment.
