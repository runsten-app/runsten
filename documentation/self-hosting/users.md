---
description: "Create the user of a self-hosted Runsten, change its password, and how sessions and sign-in limits work."
---

# Users

An instance has one user, created from the command line: there is no default account, and the password never goes through an argument, a variable or the logs.

```sh
docker compose run --rm api user create <name>     # asks for the password twice, without echo
docker compose run --rm api user password <name>   # a new password; signs out every session
printf '%s\n' "$password" | docker compose run --rm -T api user create <name>   # from a script
```

- Passwords have at least 15 characters: a passphrase, or one from a password manager. They are stored hashed with argon2id.
- Signing in opens a session for 30 days, ended sooner by 7 days without use or by signing out.
- After 10 failed attempts within 15 minutes, for a username or from an address, attempts are refused until the 15 minutes are over. Restarting `api` clears this.
- A forgotten password is replaced with `user password`.
