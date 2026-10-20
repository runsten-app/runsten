# Polestar Data Portal M2M API: recorded responses

Responses of the M2M API (`https://pc-api.polestar.com/eu-north-1/data-portal/m2m`) for one
Polestar 2, captured on 2026-09-18 by Nicolas Kheirallah and published, sanitised (VIN,
coordinates and identifiers replaced), in
[polestar-mcp](https://github.com/NicolasKheirallah/polestar-mcp) (`test/fixtures/dump/`,
commit `b508d47`). They are the fixtures of the simulator's Polestar API and of the client,
as `../volvo-demo-car/` holds Volvo's.

- `token.json`: `POST /token`.
- `vehicles.json`: `GET /v1/vehicles`.
- `telemetry/*.json`, `charging/*.json`: `GET /v1/vehicles/{vin}/telemetry/<domain>` and
  `.../charging/<domain>`.

The car was parked and unplugged: no charging power, current or voltage, no drive.
`charging/is-at-charge-location.json` is the 404 body the API returned.

## Licence

These files come from polestar-mcp, under the MIT licence:

> MIT License
>
> Copyright (c) 2026 Nicolas Kheirallah
>
> Permission is hereby granted, free of charge, to any person obtaining a copy of this
> software and associated documentation files (the "Software"), to deal in the Software
> without restriction, including without limitation the rights to use, copy, modify, merge,
> publish, distribute, sublicense, and/or sell copies of the Software, and to permit persons
> to whom the Software is furnished to do so, subject to the following conditions:
>
> The above copyright notice and this permission notice shall be included in all copies or
> substantial portions of the Software.
>
> THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR IMPLIED,
> INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY, FITNESS FOR A PARTICULAR
> PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE AUTHORS OR COPYRIGHT HOLDERS BE LIABLE
> FOR ANY CLAIM, DAMAGES OR OTHER LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR
> OTHERWISE, ARISING FROM, OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER
> DEALINGS IN THE SOFTWARE.
