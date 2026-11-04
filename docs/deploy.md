# Deploy: eos

## What runs

`cmd/eosd`, one Go binary, plain HTTP on 22620 (health 22621) behind a Traefik TCP router that
terminates the console's TLS for `api.epicgames.dev` and `backend.innersloth.com` with an
OpenPak-CA leaf (`klei-http1` ALPN pin), like `platforms/switch/games/klei`.

Configuration: `EOS_ID_SECRET` (PUID derivation, must never change), `EOS_SIGNING_KEY_FILE`
(RSA PEM), `NX_INTERNAL_URL` + `NX_INTERNAL_KEY` (nx-baas). PUIDs and friend codes
are derived. Friends, friend requests and blocks are not stored here: they are the account core's Social
service (`ACCOUNT_GRPC_ADDR`, `ACCOUNT_INTERNAL_KEY`; without them the friends list is empty and requests
answer 503). A player needs a Switch link in the core (their BAAS user id) to have friends.
`blocked_by` is always empty, because the core never says who blocked you.

`EOS_DATABASE_URL` (Postgres, e.g. `postgres://eos:...@openpak-postgres:5432/eos?sslmode=disable`): the
PUID -> BAAS user id and Epic account -> BAAS user id maps (`eos_owners`), written at each login and read
back on a cache miss, so a restart forgets nobody; also `eos_accounts` (PUID <-> core account, the
derived friend code, the pending-deletion flag) and `eos_invites` (Among Us lobby invites, kept an hour;
the core's Invitations need a registered game session per recipient, which eosd does not open). Pool of 2. Database down: eosd keeps serving from
memory, logs at ERROR level, retries the connection every 30 s and the write at the next login.
Unset: memory only (development).

`EOS_INTERNAL_KEY` (optional): turns on `GET /internal/v1/puid/<puid>` on the health listener (22621,
never routed by Traefik), which answers `{"baas_user_id":"..."}` (also `/internal/v1/owner/<id>`, where id may be a PUID or an Epic account id, for Fall Guys) for a PUID this process issued at a
Connect login (kept in Postgres), 404 otherwise. Impostor (`among-us`) calls it, with the same key, to tell rewards
who is in an Among Us room. Unset: the endpoint answers 401.

## Account erasure

`DELETE /internal/accounts/{account_id}/data` on the health listener (`http://openpak-eos:22621`, never routed
by Traefik), header `Authorization: Bearer <ACCOUNT_ERASE_KEY>`; key unset answers 503. Answers
`{"service":"eos","rows":N}`. Erases the account's `eos_accounts` row, every `eos_invites` row it sent or
received, the `eos_owners` rows (PUID and Epic id -> BAAS id) and the in-memory caches. It resolves the ids
from `eos_accounts` and, when `ACCOUNT_GRPC_ADDR` is set, from the core's switch links. Friends, requests and
blocks live in the core and go with it. Nothing is kept: eosd stores no authored content.

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
