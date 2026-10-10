# eos

OpenPak's Epic Online Services backend for Nintendo Switch titles: the `api.epicgames.dev`
the EOS SDK inside a game dials once the console's DNS points at OpenPak. One repository for
the network, like [`servers/demonware`](../demonware) and [`servers/photon`](../photon), with
titles as records under `docs/`.

## Status: deployed 2026-10-10 (`openpak-eos`), Among Us signs in to EOS

| Title | Status |
| --- | --- |
| Among Us | EOS sign-in served end to end on Harbor (config, client token, Connect login on the NSA id_token, KWS). Next stop is its own account backend `backend.innersloth.com`, which eosd answers but the signed ceiling does not redirect yet ([`docs/among-us.md`](docs/among-us.md)) |
| TMNT: Shredder's Revenge | faults inside its EOS module on Harbor before any request (a Harbor issue); reached the config request on Ryujinx ([`docs/tmnt-shredders-revenge.md`](docs/tmnt-shredders-revenge.md)) |
| Fall Guys | identity layer is this repository's problem now; the game stays not feasible because its rounds run on Epic-hosted servers ([`docs/fall-guys.md`](docs/fall-guys.md)) |

This repository supersedes [`servers/fall-guys`](../fall-guys), whose study and evidence
plan are carried in [`docs/fall-guys.md`](docs/fall-guys.md) and
[`docs/fall-guys-evidence.md`](docs/fall-guys-evidence.md). That study's identity verdict,
"Epic verifies the Nintendo token upstream, so an OpenPak token can never pass", was true
while Epic was the backend. With OpenPak answering `api.epicgames.dev` and OpenPak minting the
NSA id_token, the backend verifies its own token, the same way `servers/demonware` does
through `nx-baas`. Its match-server verdict is untouched.

What is known so far is in [`docs/protocol.md`](docs/protocol.md), with sources in
[`docs/provenance.md`](docs/provenance.md). The short version:

- The SDK resolves one name, `api.epicgames.dev`, and trusts the console CA store. An
  OpenPak-CA certificate on that name is accepted.
- Its first request is `GET /sdk/v1/default?platformId=SWT`, the SDK config document. That
  endpoint is public on the real service; the document is the SDK's service map, so serving
  our own also decides where every later call goes.
- Until that document is served the SDK retries forever and never logs in. Everything after
  it, starting with Connect login on the NSA id_token, is unmeasured.

## What runs

`cmd/eosd`, one Go binary, plain HTTP on 22620 (health 22621) behind a Traefik TCP router that
terminates the console's TLS for `api.epicgames.dev` and `backend.innersloth.com` with an
OpenPak-CA leaf (`klei-http1` ALPN pin), like `servers/klei`.

| Path | What |
| --- | --- |
| `GET /sdk/v1/default`, `/sdk/v1/product/<id>` | the SDK config document, hand-written, every service at `api.epicgames.dev` |
| `POST /auth/v1/oauth/token` | `client_credentials`; `external_auth` with `nintendo_nsa_id_token` / `nintendo_id_token` (verified by nx-baas) or `deviceid_access_token` |
| `POST /auth/v1/accounts/deviceid`, `GET /auth/v1/oauth/jwks` | device ids; the token signing keys |
| `/kws/v1/...` | age gate info and permissions (every player an adult, the title's keys granted) |
| `/telemetry/...` | analytics, dropped |
| host `backend.innersloth.com`, `/api/user/...` | Among Us's account backend: friend code, friends, requests, blocks, lobby invites |
| anything else | `!! unhandled` with method, path (ids tagged), header names and body shape, 404 |

Configuration: `EOS_ID_SECRET` (PUID derivation, must never change), `EOS_SIGNING_KEY_FILE`
(RSA PEM), `NX_INTERNAL_URL` + `NX_INTERNAL_KEY` (nx-baas). No database: PUIDs and friend codes
are derived; the friends graph is in memory.

```sh
go run ./cmd/eosd            # :22620, health :22621
```

## Ground rules

- Clean room. Read [`docs/clean-room-policy.md`](https://github.com/openpak/docs/blob/main/clean-room-policy.md) before writing a
  line. Public protocol documentation, our own captures and observations, and compatibly
  licensed code are the only inputs. The captured SDK config document is a fact source, not
  a file to ship: the served document is written by hand from the fields the SDK is shown to
  read.
- AGPL-3.0-only, English only, Go 1.26, no secrets or production identifiers in the
  repository. Logs carry shapes, never tokens, PUIDs, names or bodies (`logging.go`, checked by
  `TestLogsCarryNoPersonalData`).

This project is not affiliated with or endorsed by Nintendo, Epic Games, Dotemu, Tribute
Games or Mediatonic. It contains no game code, assets, SDK material, binaries, certificates,
keys, credentials or raw captures.
