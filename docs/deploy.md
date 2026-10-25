# Deploy: eos

## What runs

`cmd/eosd`, one Go binary, plain HTTP on 22620 (health 22621) behind a Traefik TCP router that
terminates the console's TLS for `api.epicgames.dev` and `backend.innersloth.com` with an
OpenPak-CA leaf (`klei-http1` ALPN pin), like `platforms/switch/games/klei`.

Configuration: `EOS_ID_SECRET` (PUID derivation, must never change), `EOS_SIGNING_KEY_FILE`
(RSA PEM), `NX_INTERNAL_URL` + `NX_INTERNAL_KEY` (nx-baas). No database: PUIDs and friend codes
are derived; the friends graph is in memory.

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
