# Provenance

> **Current Switch client (2026-10-13): Harbor**, at
> `~/REPOS/Openpak/emulators/harbor`. Harbor is an independent
> OpenPak project based on Eden. Citron, Ryujinx (desktop and Android), and
> the former OpenPak Eden checkouts have been removed. References to those
> forks below record historical evidence only: do not run their commands,
> reinstall them, or select them for current work. Use Harbor's README and
> current handoff for builds and testing; old results do not establish Harbor parity.

Where each non-obvious fact in [`protocol.md`](protocol.md) came from, per the
[clean-room policy](https://docs.openpak.org/contributors/clean-room/). Raw material stays in the workspace scratch
folder (`~/REPOS/Openpak/scratch/`), never in this repository.

| Fact | Source |
| --- | --- |
| TMNT title id, version, DLC ids, SDK version, statically linked SDK, User-Agent and header shapes | OpenPak's own Ryujinx logs, 2026-09-24 22:09 and 22:21 sessions (`~/.config/Ryujinx/Logs/`); the game's own guest log lines (`ServiceLm`) |
| Only `api.epicgames.dev` resolved; SNI, verify options, ALPN, session cache; four parallel connections | Same logs (`Sfdnsres`, `ServiceSsl`, `ServiceBsd` lines) and `ss -tnp` on the host |
| `nifm` answered but no socket opened on the first run; `network disabled` from the SDK; the crash at teardown | Same 22:09 log, `coredumpctl` and the systemd journal; written up in [`tmnt-shredders-revenge.md`](tmnt-shredders-revenge.md) |
| Request path, query, `Authorization` constant, retry cadence, telemetry posts, `404 {}` behaviour | `openpak-nx-baas` container log on prod (2026-09-24 21:22 UTC onwards), which received the traffic as an unrouted SNI and answered from its catch-all |
| The SDK config endpoint is public and its document shape and base URLs | Our own `curl` of `https://api.epicgames.dev/sdk/v1/default?platformId=SWT`, 2026-09-24, no credentials, HTTP 200. Copy: `scratch/eos-tmnt/sdk-v1-default-SWT-2026-09-24.json` (not committed) |
| Same first call on every platform; a blocked SDK config fails EOS init | Public reports: a Satisfactory phone-home analysis on the Steam forums (`platformId=WIN`, same path) and an Abiotic Factor dedicated-server issue (`SDK Config Platform Update Request Failed`) |
| Connect Web API token request shape, `external_auth_type` values | Epic's public documentation: Connect Web APIs (`dev.epicgames.com/docs/web-api-ref/connect-web-api`) and `EOS_EExternalCredentialType` in the C SDK reference |
| REST surface of Auth, Connect, Achievements, Stats, Leaderboards, Player and Title Storage, Sanctions, Ecom | Third-party OpenAPI profiles, `github.com/api-evangelist/epic-games` (documentation only, not code) |
| Device-auth OAuth endpoints (`/epic/oauth/v2/deviceAuthorization`, `/epic/oauth/v2/token`) | OpenPak's own Fall Guys research harness in scratch (`scratch/home/fallguys-research/tagnx`), which hand-declares the C SDK structs; kept as a pointer, nothing copied |
| Fall Guys layers, verdict and sources | `servers/fall-guys/docs/design.md`, carried into [`fall-guys.md`](fall-guys.md) with its own provenance section |
| Prior art: the only EOS emulator (client-side, PC) | `gitlab.com/Nemirtingas/nemirtingas_epic_emu` (source withdrawn; mirrors exist). Not read for code; noted so nobody looks for a server there |
| `.epicgames.dev` family in the netprofile; `nx-baas` on-demand certificates for unrouted SNI | `deploy/netprofile-platforms.yml` and the nx-baas log on prod |
| SDK 1.16 boot sequence (§5): paths, grant types, form field names, KWS calls, the age gate on a KWS 404 | `openpak-eos` log on prod (shapes only) and Harbor runs `eos-host`, 2026-10-10 |
| KWS JSON field names (`country`, `consentAgeForCountry`, `kwsUserId`, `dateOfBirth`, `isMinor`, `parentEmail`), status words `GRANTED`/`PENDING`/`REJECTED` | strings in Among Us's own `EOSSDK-Switch-Shipping.nro` (extracted with `scratch/nspx`, kept in `scratch/eos/among-us/`) |
| Among Us permission keys, `backend.innersloth.com` paths and JSON names, `ResponseFriendCode` fields | Among Us's IL2CPP `global-metadata.dat` (string literals, `[JsonProperty]` arguments, type and field names via `scratch/eos/tools/meta_types.py`) |
| backend.innersloth.com resolved to the real Innersloth; FriendCodeFail; ONLINE greyed | Harbor `eos-host` log (`sfdnsres`) and screenshots, 2026-10-10 |
| TMNT fault inside the EOS `.nrs` on Harbor | Harbor `eos-host` log and thread dump, 2026-10-10, twice |
| TMNT lobby search request and Connect login on a retail console | `openpak-eos` log on prod with `EOS_TRACE=1`, 2026-10-24 11:19-11:27 UTC (the owner's own console) |
| Lobby search reply shape (`sessions`, `count`, session and settings field names, `rejoinAfterKick` values) | TMNT's own `EOSSDK-Switch-Shipping.nro`, decompiled in scratch (`scratch/re/tmnt/`): the search-reply and lobby-object parsers; names only, no code |
