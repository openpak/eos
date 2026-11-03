# Deploy: eos

## What runs

`cmd/eosd`, one Go binary, plain HTTP on 22620 (health 22621) behind a Traefik TCP router that
terminates the console's TLS for `api.epicgames.dev` and `backend.innersloth.com` with an
OpenPak-CA leaf (`klei-http1` ALPN pin), like `platforms/switch/games/klei`.

Configuration: `EOS_ID_SECRET` (PUID derivation, must never change), `EOS_SIGNING_KEY_FILE`
(RSA PEM), `NX_INTERNAL_URL` + `NX_INTERNAL_KEY` (nx-baas). PUIDs and friend codes
are derived; the friends graph is in memory.

`EOS_DATABASE_URL` (Postgres, e.g. `postgres://eos:...@openpak-postgres:5432/eos?sslmode=disable`): the
PUID -> BAAS user id and Epic account -> BAAS user id maps (`eos_owners`), written at each login and read
back on a cache miss, so a restart forgets nobody. Pool of 2. Database down: eosd keeps serving from
memory, logs at ERROR level, retries the connection every 30 s and the write at the next login.
Unset: memory only (development).

`EOS_INTERNAL_KEY` (optional): turns on `GET /internal/v1/puid/<puid>` on the health listener (22621,
never routed by Traefik), which answers `{"baas_user_id":"..."}` for a PUID this process issued at a
Connect login (kept in Postgres), 404 otherwise. Impostor (`among-us`) calls it, with the same key, to tell rewards
who is in an Among Us room. Unset: the endpoint answers 401.

## Rewards events

eosd sends none. The lobby websocket is not served yet (`lobby.go`: the store is always empty,
the socket only logs frame shapes), so eosd never sees a lobby join or leave and cannot tell
who is in a session with whom. TMNT: Shredder's Revenge already reports
`game_sessions.tmnt-shredders-revenge` from `switch-nex` (its NEX matchmaking store), so eosd must
not add a second count. Fall Guys' sessions are the `fall-guys` service's. Among Us room
membership is Impostor's (`REWARDS_EVENTS_*` and `EOS_INTERNAL_*` are set on `among-us`).

## Image

`Dockerfile`: builds `./cmd/eosd` with Go 1.26 into a `gcr.io/distroless/static` image that runs as
`nonroot`. Ports: 22620 (HTTP API), 22621 (`/health`).

`.github/workflows/release.yml`: a `v*.*.*` tag builds amd64 and arm64 and publishes
`ghcr.io/openpak/eos` under the version, `:<major>.<minor>` and `:latest`. Hosts running
podman-auto-update pull `:latest`.

## Run

```sh
go run ./cmd/eosd            # :22620, health :22621
```
