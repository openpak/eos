# Notes: eos

> **Current Switch client (2026-10-13): Harbor**, at
> `~/REPOS/Openpak/emulators/harbor`. Harbor is an independent
> OpenPak project based on Eden. Citron, Ryujinx (desktop and Android), and
> the former OpenPak Eden checkouts have been removed. References to those
> forks below record historical evidence only: do not run their commands,
> reinstall them, or select them for current work. Use Harbor's README and
> current handoff for builds and testing; old results do not establish Harbor parity.

OpenPak's Epic Online Services backend for Nintendo Switch titles: the `api.epicgames.dev`
the EOS SDK inside a game dials once the console's DNS points at OpenPak. One repository for
the network, like [`servers/demonware`](../../demonware) and [`servers/photon`](../../photon), with
titles as records under `docs/`.

## Status: deployed 2026-10-10 (`openpak-eos`), Among Us signs in to EOS

| Title | Status |
| --- | --- |
| Among Us | EOS sign-in served end to end on Harbor (config, client token, Connect login on the NSA id_token, KWS). Next stop is its own account backend `backend.innersloth.com`, which eosd answers but the signed ceiling does not redirect yet ([`among-us.md`](among-us.md)) |
| TMNT: Shredder's Revenge | faults inside its EOS module on Harbor before any request (a Harbor issue); reached the config request on Ryujinx ([`tmnt-shredders-revenge.md`](tmnt-shredders-revenge.md)) |
| Fall Guys | identity layer is this repository's problem now; the game stays not feasible because its rounds run on Epic-hosted servers ([`fall-guys.md`](fall-guys.md)) |

This repository supersedes [`servers/fall-guys`](../../fall-guys), whose study and evidence
plan are carried in [`fall-guys.md`](fall-guys.md) and
[`fall-guys-evidence.md`](fall-guys-evidence.md). That study's identity verdict,
"Epic verifies the Nintendo token upstream, so an OpenPak token can never pass", was true
while Epic was the backend. With OpenPak answering `api.epicgames.dev` and OpenPak minting the
NSA id_token, the backend verifies its own token, the same way `servers/demonware` does
through `nx-baas`. Its match-server verdict is untouched.

## Ground rules

- Clean room. Read [clean-room policy](https://docs.openpak.org/contributors/clean-room/) before writing a
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
