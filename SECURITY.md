# Security policy

## Reporting a vulnerability

Do not open a public issue for a vulnerability. Report it privately through GitHub's
[Report a vulnerability](https://github.com/runsten-app/runsten/security/advisories/new) form.

Say what an attacker can do, against which version (`RUNSTEN_VERSION` or the image tag), and
how to reproduce it. Leave out real VINs, tokens and API keys: the simulator reproduces most of
what the Volvo API does (`DEVELOPMENT.md`).

You will get an answer within a week. We will tell you when a fix is released, and credit you in
the advisory unless you would rather not be named. Please give us a reasonable time to fix it
before you disclose it.

## Supported versions

Runsten is young: only the latest release gets security fixes. Update with the steps of
[Updating](documentation/self-hosting/updating.md).

## Scope

The code of this repository: the collector, the API, the web interface and the Compose stack.
A self-hosted instance is its owner's to secure: keep it behind an https reverse proxy, as
[Reverse proxy](documentation/self-hosting/reverse-proxy.md) describes, and its debug page on
the loopback. The simulator is a development tool, not meant to be exposed.
