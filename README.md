# eos

OpenPak's Epic Online Services backend for Nintendo Switch titles: the `api.epicgames.dev`
the EOS SDK inside a game dials once the console's DNS points at OpenPak. One repository for
the network, like [`servers/demonware`](../demonware) and [`servers/photon`](../photon), with
titles as records under `docs/`.

## Status: research. Documentation only, code next.

| Title | Status |
| --- | --- |
| TMNT: Shredder's Revenge | reaches the backend; blocked on the SDK config document ([`docs/tmnt-shredders-revenge.md`](docs/tmnt-shredders-revenge.md)) |
| Fall Guys | identity layer is this repository's problem now; the game stays not feasible because its rounds run on Epic-hosted servers ([`docs/fall-guys.md`](docs/fall-guys.md)) |
| Among Us | on the EOS host list, login requirement unknown (`servers/among-us`) |

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

## What lives where today

- Prod: the `.epicgames.dev` family is already claimed in the netprofile, and `nx-baas`
  terminates the TLS for it as an unrouted SNI and answers `404` from its catch-all. That is
  where the SDK stops.
- Ports: block 22620-22629 in [`ports.md`](../../ports.md), registered 2026-09-24: 22620 the
  `api.epicgames.dev` listener (TLS, Traefik `HostSNI` passthrough from 443), 22621 `/health`,
  the rest reserved.
- Raw material (logs, the captured config document, the Fall Guys harness) is in the
  workspace scratch folder, referenced from `docs/provenance.md`, never committed.

## Ground rules

- Clean room. Read [`docs/clean-room-policy.md`](https://github.com/openpak/docs/blob/main/clean-room-policy.md) before writing a
  line. Public protocol documentation, our own captures and observations, and compatibly
  licensed code are the only inputs. The captured SDK config document is a fact source, not
  a file to ship: the served document is written by hand from the fields the SDK is shown to
  read.
- AGPL-3.0-only, English only, Go 1.25 when code arrives, no secrets or production
  identifiers in the repository.

This project is not affiliated with or endorsed by Nintendo, Epic Games, Dotemu, Tribute
Games or Mediatonic. It contains no game code, assets, SDK material, binaries, certificates,
keys, credentials or raw captures.
