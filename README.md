# Epic Online Services

OpenPak's Epic Online Services Server.

| | |
| --- | --- |
| Networks | Nintendo Switch |
| Protocol | Epic Online Services SDK: HTTP REST, lobby and notification websockets |
| Licence | AGPL-3.0-only |

## Titles

| Game | Network | Title ID | Module |
| --- | --- | --- | --- |
| [Among Us](https://openpak.org/titles#t-19) | Nintendo Switch | `0100B0C013912000` | EOS sign-in, Innersloth account backend |
| [Teenage Mutant Ninja Turtles: Shredder's Revenge](https://openpak.org/titles#t-127) | Nintendo Switch | `0100FE701475A000` | EOS Connect, lobbies |
| [Fall Guys](https://openpak.org/titles#t-21) | Nintendo Switch | `0100C3C015738800` | EOS Auth and Connect |

## Networks

| Network | Reached through |
| --- | --- |
| Nintendo Switch | `api.epicgames.dev` and `backend.innersloth.com`, through the console's DNS; Traefik TCP router terminates TLS |

## What it serves

| Listener | What | Port |
| --- | --- | --- |
| HTTP API (`eosd`) | EOS SDK services on `api.epicgames.dev`, Among Us account backend on `backend.innersloth.com` | TCP 22620 |
| Health | `/health` | TCP 22621 |

## Run

```sh
go run ./cmd/eosd
```

## Docs

- [protocol.md](docs/protocol.md): endpoints, wire formats and the SDK boot sequence
- [evidence.md](docs/evidence.md): what is confirmed, what is assumed, evidence still needed
- [deploy.md](docs/deploy.md): ports, configuration, image and run detail
- [notes.md](docs/notes.md): design rationale, history, status and ground rules
- [among-us.md](docs/among-us.md): Among Us
- [tmnt-shredders-revenge.md](docs/tmnt-shredders-revenge.md): Teenage Mutant Ninja Turtles: Shredder's Revenge
- [fall-guys.md](docs/fall-guys.md): Fall Guys
- [fall-guys-evidence.md](docs/fall-guys-evidence.md): Fall Guys evidence plan
- [provenance.md](docs/provenance.md): where each non-obvious fact came from

## Licence

AGPL-3.0-only. See [LICENSE](LICENSE).
