# Contributing to Runsten

Thank you for your interest in Runsten. Bug reports, vehicle data, translations, documentation
and code are all welcome. This guide says how to propose them; [`DEVELOPMENT.md`](DEVELOPMENT.md)
says how to set up and test, and [`ARCHITECTURE.md`](ARCHITECTURE.md) how the code is organized.

## Licence, CLA and the hosted offer

Runsten is free software under the [AGPL-3.0-or-later](LICENSE). It is also open core: the
maintainer runs a hosted offer built from this code plus private modules, which add what only a
hosted service needs (sign-in through an identity provider, subscriptions and billing). The
public build stays complete and useful on its own: the hosted modules add, they never take
away from self-hosting.

So that the hosted build may include contributed code, every contributor signs a Contributor
License Agreement once, before their first pull request is merged: it grants the maintainer a
broad licence on the contribution, with the right to sublicense, after the Apache Individual
CLA. You keep the copyright of your work. The CLA bot asks for it on your first pull request.

## Ways to contribute

Issues and discussions of the project live in this repository's
[issues](https://github.com/runsten-app/runsten/issues).

- **Report a bug**: open an issue with what you did, what you expected, what happened, your
  version (`RUNSTEN_VERSION` or the image tag) and the relevant logs. Remove VINs, tokens,
  addresses and positions first.
- **Report what your car's API says**: the Volvo API is little documented, and every real
  response helps. Which model and year, which endpoint, what it returned, with the VIN and
  positions removed.
- **Add or correct a vehicle variant**: a pull request on the catalog, see
  [Contributing a variant](documentation/contributing/variants.md).
- **Translate**: the interface is in English, French and Swedish (`web/src/shared/i18n/locales/`).
  Corrections by native speakers are especially welcome.
- **Improve the documentation**: `documentation/` is the user guide published on the site.
- **Write code**: fix a bug, or take an issue. For anything beyond a small fix, open an issue
  first, or comment on one, so that we agree on the approach before you spend time on it.

## Pull requests

1. Fork the repository and create a branch from `main`.
2. Make your change, with its tests. See [Common changes](DEVELOPMENT.md#common-changes).
3. Run `task unit` (and `task integration` if you touched the store or a migration). The CI runs
   everything on the pull request.
4. Open the pull request: say what it changes and why, how you tested it, and link the issue
   (`Closes #N`). Screenshots for a change of the interface, light and dark.

What we look for in review:

- **Small and focused.** One subject per pull request; unrelated clean-ups go in their own.
- **Atomic commits** whose messages are a sentence in the imperative, in English, that says
  what the commit does: `Group the period's charges by the place their cost takes`. The body,
  if any, says why. No issue number in a commit message: the pull request carries the link.
- **Tests** for what changes, deterministic (no real clock, no network). Coverage must not drop.
- **The rules of [`ARCHITECTURE.md`](ARCHITECTURE.md#rules-that-hold-it-together) hold**:
  dependencies point inward, time is injected, every table is isolated, the API's diff is
  reviewed as a contract.
- **Accessible and translated** interface: axe passes, every text in the three languages.
- **No speculative abstraction**, and no new dependency without a reason given.
- **Nothing private**: no real VIN, token, API key or position, anywhere, fixtures included.

A pull request needs the CI to pass and a maintainer's approval. It is merged by the maintainer.

### AI-assisted contributions

Welcome, under the same rules: you are the author, you have read and understood every line, and
you have run the tests. [`AGENTS.md`](AGENTS.md) holds the detailed rules of the project in a
form an assistant can follow; point yours to it.

## What does not belong here

Some things the core will not take, by design:

- sign-in through a third-party identity provider, self-service sign-up or password recovery by
  email, payments or subscriptions: they serve a hosted offer, not a self-hosted instance;
- behavior of the Volvo API that nobody observed: an assumption is named as one;
- features that send a user's data elsewhere by default: an instance keeps its data unless its
  owner turns something on (a geocoder, map tiles, an MQTT broker).

If unsure, ask in an issue first.

## Security

Do not open a public issue for a vulnerability. Report it privately through GitHub's
[Report a vulnerability](https://github.com/runsten-app/runsten/security/advisories/new) form.
You will get an answer within a week; please give us a reasonable time to fix it before
disclosing it.

## Conduct

Be kind and assume good faith. Criticize code, not people. Harassment or abuse of any kind gets
you removed from the project's spaces.
