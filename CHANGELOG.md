# Changelog — eos

`git log` stays the source of truth; this file is the readable summary.

## v0.4.0 — 2026-11-03

- The PUID -> BAAS and Epic account -> BAAS maps live in Postgres (`EOS_DATABASE_URL`, table
  `eos_owners`), memory is only a cache; restart test against a real Postgres
  (`EOS_TEST_DATABASE_URL`).

## v0.3.0 — 2026-11-03

- Connect login remembers PUID -> BAAS user id (memory), served to Impostor on
  `GET /internal/v1/puid/<puid>` (health listener, `EOS_INTERNAL_KEY`) so Among Us rooms can
  report `game_sessions.among-us` to rewards. eosd itself sends no rewards events: lobbies are
  not served yet, and TMNT sessions are already counted by switch-nex (docs/deploy.md).

## v0.2.0 — 2026-11-02

- Release from main; replaces the hand-built production image.

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
