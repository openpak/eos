# Teenage Mutant Ninja Turtles: Shredder's Revenge

Title `0100FE701475A000`, v1.0.9, with the Dimension Shellshock and Radical Reptiles DLC.
Dotemu/Tribute; EOS SDK 1.15.3 statically linked. The first EOS title to reach an
OpenPak-answered `api.epicgames.dev`. Runs recorded 2026-09-24 on Ryujinx
(`emulators/ryujinx`, OpenPak build), host `n5air`, prod backend.

## Run 1, 22:09 IST: crash at EOS teardown

- Booted, DLC found, PPTC and shader caches empty (first boot of this title on this
  build).
- Game: `Initializing curl / socket / server services / NIFM`, four `nifm` calls (all
  stubs in Ryujinx), `Network is ready`, NSA id_token obtained from nx-baas.
- SDK: boots, then `ConnectClientAuthTask Failed due to network disabled` twice. **No DNS
  lookup, no socket**: the only Bsd activity was libwebsockets writing one byte to fd 0
  every 33 ms (its wake-up pipe, never opened here, EBADF each time, 346 times).
- 36 s in: `Lws(Error): Cannot write to dummy pipe`, `ZERO RANDOM FD`, then the game's
  `Uninitializing server services`, and within 100 ms Ryujinx died with SIGABRT on the host
  thread running the guest main thread.
- Not a managed exception (no error line, crash-report directory empty), not memory
  (5.6 GB peak, 15 GB free). The core was truncated and unsymbolised. The likely mechanism
  is a guest memory fault during libwebsockets teardown surfacing as a fatal runtime abort
  under `HostMappedUnsafe`; **inferred, not proven**.
- Distinct from the seven earlier Ryujinx aborts since 2026-09-21, which were all on the
  GUI render thread.

## Run 2, 22:21 IST: the SDK reaches the backend

- Same build, same title. This time the game's networking initialised, the SDK resolved
  `api.epicgames.dev` to the OpenPak address through the family redirect, opened TLS with
  SNI and system-CA verification, and requested `GET /sdk/v1/default?platformId=SWT`.
- nx-baas received it as an unrouted SNI (on-demand certificate) and answered `404 {}` from
  its catch-all. The SDK retried with backoff for the whole session (60 attempts in 20
  minutes) and posted telemetry in the background (72 posts). Connect login was never
  attempted.
- The game kept running for the whole session; no crash. So run 1 is **not
  deterministic**, and the difference between the runs is whether the game's socket layer
  came up before the SDK gave up.

## What this settles

| Question (from the Fall Guys study) | Answer |
| --- | --- |
| E3: does the client send anything, or abort locally? | It sends. The first request goes out and the SDK waits on the answer. Fall Guys' local abort (2026-08-31) is either title-specific or the same "network disabled" state seen in run 1. |
| E4: trust material | System `ssl` service with the console CA store; an OpenPak-CA leaf passes. No pin. |
| First thing a backend must serve | The SDK config document (`protocol.md` §4). Nothing else is reached before it. |

## Open

- Serve a config document and see what the SDK asks for next (expected: Connect login).
- Which fields of the config document the SDK actually needs.
- Reproduce run 1 with `HostMapped` memory mode to turn the abort into a logged guest fault.
