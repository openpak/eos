# Changelog — eos

`git log` stays the source of truth; this file is the readable summary.

## v0.1.0 (2026-10-10)

- eosd: first code. SDK config document (default and per-product), OAuth token endpoint
  (client credentials; Connect `external_auth` on the Nintendo NSA id_token through nx-baas,
  device ids), JWKS, KWS age gate and permissions, telemetry sink, shape-only logging of
  everything else.
- eosd: Among Us's account backend `backend.innersloth.com` (friend codes derived from the
  PUID, in-memory friends, requests, blocks, lobby invites).
- Deployed as `openpak-eos` behind Traefik (`api.epicgames.dev`, `backend.innersloth.com`).
- docs: SDK 1.16 boot sequence measured on Among Us (protocol.md §5); `docs/among-us.md`; TMNT
  Harbor fault.

## Before v0.1.0

- ports: block 22620-22629 registered in the workspace `ports.md` (22620 TLS listener, 22621 health).

- seed: `servers/eos`, AGPL-3.0-only, clean-room gate. Supersedes `servers/fall-guys`;
  its study and evidence plan carried as `docs/fall-guys.md` and
  `docs/fall-guys-evidence.md`.
- docs: EOS on Switch protocol notes and provenance; TMNT: Shredder's Revenge record
  (2026-09-24: SDK reaches the backend, blocked on the SDK config document).
