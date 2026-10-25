# Epic Online Services on Switch: protocol notes

> **Current Switch client (2026-10-13): Harbor**, at
> `~/REPOS/Openpak/emulators/harbor`. Harbor is an independent
> OpenPak project based on Eden. Citron, Ryujinx (desktop and Android), and
> the former OpenPak Eden checkouts have been removed. References to those
> forks below record historical evidence only: do not run their commands,
> reinstall them, or select them for current work. Use Harbor's README and
> current handoff for builds and testing; old results do not establish Harbor parity.

The facts an OpenPak EOS backend is built from, written down before any code. Every entry
says what the shipped EOS SDK sends or expects; [`provenance.md`](provenance.md) says where
each fact came from. Nothing here describes another implementation's code.

Confidence labels follow the workspace conventions: CONFIRMED (measured on our own run),
HIGH, MEDIUM, LOW, SPECULATIVE.

## 1. Titles and the SDK

| Title | Switch title id | EOS SDK | Status |
| --- | --- | --- | --- |
| Teenage Mutant Ninja Turtles: Shredder's Revenge | `0100FE701475A000` (v1.0.9) | 1.15.3, statically linked | research active, see [`tmnt-shredders-revenge.md`](tmnt-shredders-revenge.md) |
| Fall Guys | `0100C3C015738800` | runtime-loaded `EOSSDK-Switch-Shipping.nrs` | identity layer only; the game itself stays not feasible, see [`fall-guys.md`](fall-guys.md) |
| Among Us | `0100B0C013912000` (2026.18.0, v3997696) | 1.16.4, runtime-loaded `Data/Plugins/EOSSDK-Switch-Shipping.nro` | EOS sign-in served end to end (2026-10-10); blocked next on `backend.innersloth.com`, see [`among-us.md`](among-us.md) |

The SDK identifies itself on every request (CONFIRMED):

```
User-Agent: EOS-SDK/<sdk version>+Switch_<firmware> (Switch/<firmware>) <product>/<product version>
X-Eos-Version: <sdk version>+Switch_<firmware>
X-Epic-Correlation-Id: EOS-<base64 fields>
```

## 2. Names, transport, trust

- One bootstrap host: `api.epicgames.dev`, HTTPS on 443 (CONFIRMED). It is the only name the
  TMNT client resolved in a 20-minute session; every EOS service the SDK config names lives
  under it, except the ones in §4.
- DNS goes through the console resolver (`sfdnsres`), so the family redirect in the
  netprofile (`.epicgames.dev`) is enough to bring the SDK to OpenPak (CONFIRMED).
- TLS is the console's own `ssl` service, not a CA store inside the SDK: SNI
  `api.epicgames.dev`, `PeerCa | HostName` verification, ALPN offered, session cache on
  (CONFIRMED on Ryujinx, where the OpenPak CA is supplied as a system certificate; HIGH for a
  console with the CA patch, same service). An OpenPak-CA leaf for `api.epicgames.dev` is
  accepted; no pinning was hit.
- The SDK opens several TLS connections in parallel and keeps them open (four were
  established at once).

## 3. Boot sequence

Observed order on TMNT (CONFIRMED), all against `api.epicgames.dev`:

1. `GET /sdk/v1/default?platformId=SWT` with an `Authorization: <32 hex>` header (a
   client-side constant; its meaning is unknown, LOW that the real service checks it: the
   endpoint answers the same document with no header at all).
2. Retries of 1 with exponential backoff (2 s, 5 s, 9 s, 18 s, 33 s, then reset) for as long
   as the answer is not usable. A `404 {}` is not usable: the SDK logs
   `SDK Config Platform Update Request Failed, Result Code: EOS_UnexpectedError` and never
   proceeds to Connect. Analytics reports `Unable to get the client client auth token`.
3. `POST /telemetry/data/datarouter/api/v1/public/data` in the background regardless of 1
   (CONFIRMED, 72 posts in 20 minutes). Answering it is optional; nothing waits on it.
4. Connect login with the Nintendo NSA id_token, then whatever the title uses (Lobbies, P2P,
   Sessions, Presence...). **Not reached yet**: blocked on 1.

Before any of this the game itself has called `nifm` (request, submit, state, result) and
told the SDK the network is up. If the game's own networking layer is not initialised
(the first TMNT run, where `nifm` was answered but no socket was ever opened), the SDK
reports `ConnectClientAuthTask Failed due to network disabled` and never dials out
(CONFIRMED).

## 4. The SDK config document

`GET /sdk/v1/default?platformId=<id>` is public: the real service answers it without
credentials, for any platform id, with a JSON document of about 20 KB (CONFIRMED,
2026-09-24; a copy is in scratch, see provenance). Its shape:

- Three top-level keys: `client`, `services`, `watermark`.
- `client` holds about a hundred sections. Most are `RateLimiter.<Client>[.Operations]`
  (`MessageCount`, `TimeIntervalInSeconds`, optional `Operation` list); the rest are
  per-interface settings: `AuthClient`, `ConnectClient`, `LobbyClient`, `SessionsClient`,
  `P2PClient`, `PresenceClient`, `PartiesClient`, `MetricsClient`, `DataStorageClient`,
  `TitleStorageClient`, `StatsClient`, `LeaderboardsClient`, `EcomClient`,
  `InventoryClient`, `AntiCheat.GameplayData`, `Messaging.EpicConnect`, `Messaging.Stomp`,
  `Messaging.XMPP`, `WebSockets.LibWebSockets`, `Overlay`, `UIClient`, `HTTP`, `LogFiles`,
  `Core.Log`, `BaseService` (`HttpRetryLimit`, `HttpRetryResponseCodes`).
- It is the SDK's **service map**: each client section carries a `BaseUrl`. Nearly all are
  paths under `https://api.epicgames.dev/` (`/lobby`, `/matchmaking`, `/epic/presence`,
  `/epic/friends`, `/rtc`, `/datastorage`, `/titlestorage`, `/stats`, `/leaderboards`,
  `/notifications`, `/anticheat`, `/sanctions`, `/telemetry/data/`, ...). The exceptions,
  which the netprofile does **not** claim today:
  - `wss://connect.epicgames.dev` and `/v2` (Epic Connect messaging)
  - `wss://xmpp-service-prod.ol.epicgames.com` (XMPP)
  - `https://egp-idsoc-proxy-prod.ol.epicgames.com/{account,friends}`
  - `*.ol.epicgames.com` catalog, ecommerce, price engine, EULA tracking
  - `https://global-postparty.game-social.epicgames.com`,
    `https://gallery-service-prod.social.live.on.epicgames.com`
  - websocket templates `wss://api.epicgames.dev/lobby/v1/<deploymentId>/lobbies/connect`
    and `wss://api.epicgames.dev/notifications/v1/<deploymentId>/connect`

  Serving our own document lets us point every one of those at OpenPak without touching
  DNS for the extra names (HIGH: the SDK reads `BaseUrl`; whether *every* client honours it
  is untested).
- Which subset of the document the SDK needs to proceed is unknown (open question 1 in
  `next-session.md`). The clean-room answer is a hand-written document with the fields the
  SDK is shown to read, not the captured one.

## 5. Boot sequence of SDK 1.16 (CONFIRMED, Among Us on Harbor, 2026-10-10)

Every request below is answered by `eosd` (`cmd/eosd`); the order is the one Among Us sends.

1. `GET /sdk/v1/default?platformId=SWT`: the config document (§4). A served document ends the
   retry loop at once.
2. `POST /auth/v1/oauth/token`, HTTP basic with the title's client id and secret, form
   `grant_type=client_credentials&deployment_id=<32 hex>`. Answered with a bearer JWT,
   `token_type`, `expires_at`, `expires_in`, `features`, `deployment_id` (organization, product
   and sandbox ids empty: the SDK did not mind).
3. `GET /sdk/v1/product/<productId>?deploymentId=<id>&platformId=SWT`: the per-product config
   document, with the client token. Same document as 1.
4. `GET /kws/v1/country/ageGateInfo` (client token). Answered `{"country","consentAgeForCountry"}`
   (field names from the SDK's own strings).
5. `POST /auth/v1/oauth/token`, form `grant_type=external_auth`,
   `external_auth_type=nintendo_nsa_id_token`, `external_auth_token=<the console's NSA id_token,
   ~1260 bytes>`, `deployment_id`, `nonce`, `display_name` (the Nintendo nickname). This is
   Connect login. `eosd` verifies the token through nx-baas `/internal/switch/identity` (it is
   a BAAS id_token nx-baas minted) and answers `access_token`, `id_token` (both RS256 JWTs,
   `sub` = PUID, keys at `/auth/v1/oauth/jwks`), `product_user_id`, `organization_user_id`,
   `product_user_id_created:false`, `nonce`, `expires_at`, `expires_in`, `deployment_id`. The SDK
   accepted the login on the first try; no `CreateUser`/continuance round was needed.
6. `GET /kws/v1/users/<puid>/permissions` (user token). Answered as an adult with the title's
   permission keys granted: `{"kwsUserId","dateOfBirth","isMinor":false,"parentEmail",
   "permissions":{"<key>":"GRANTED"}}`. A 404 here sends Among Us to its own date-of-birth age
   gate; the age gate itself is the game's, shown once per save.
7. `POST /telemetry/data/datarouter/api/v1/public/data` in the background (answered 204).

The PUID is not stored: it is HMAC(EOS_ID_SECRET, deployment, BAAS user id), 32 hex, stable for
a player in a title across logins and restarts and different between titles.

## 6. What each title uses after login

- **Among Us**: KWS (above), then its own account backend `backend.innersloth.com`, not an EOS
  service; [`among-us.md`](among-us.md). Its matchmaker and game server are Impostor
  (`platforms/switch/games/among-us`).
- **TMNT**: Connect login on the NSA id_token (real console, 2026-10-24), then the co-op search
  `POST /lobby/v1/{deployment}/lobbies/filter` with
  `{"criteria":[{"key","op","value"}...],"maxResults":5}`: keys `bucket` and
  `attributes.<name>_<type>` (`VERSION_l`, `ONLINEGAMEPRESENCE_l`, `GAMETYPE_l`, `SLOTS_l`), ops
  `EQUAL`, `NOT_EQUAL`, `GREATER_THAN_OR_EQUAL`, `CONTAINS`, about every 3 s. The reply is
  `{"sessions":[...],"count":n}`, each session
  `{deployment, id, bucket, settings{maxPublicPlayers, allowInvites, shouldAdvertise, allowReadById,
  allowJoinViaPresence, allowJoinInProgress, allowConferenceRoom, allowMigration,
  rejoinAfterKick: "open"|"invite_only"}, totalPlayers, openPublicPlayers, publicPlayers, started,
  lastUpdated, attributes, owner}` (HIGH, from the SDK's parser; one malformed session drops the
  whole reply). eosd answers it from an in-memory store (`lobby.go`), so far always empty.
  Creating a party sent nothing at all: lobbies are created over the lobby websocket
  (`{"name","requestId","payload"}` messages), which the console never opened. Leaderboards are
  NEX (`switch-nex`, game server `20A95200`)
  ([`tmnt-shredders-revenge.md`](tmnt-shredders-revenge.md)).
- **Fall Guys**: EOS Auth as well as Connect. Its Catapult login carries an Epic account user
  token (`EOS_Auth_CopyUserAuthToken`), so the SDK asks for an Epic account first
  (HIGH, from the client's code; the request itself is not measured yet). The SDK names
  `/epic/oauth/v2/token`, `/epic/oauth/v2/tokenInfo` and `/epic/oauth/v2/.well-known/jwks.json`
  under the `EOSAuthService` base, and the external types `nintendo_nsa_id_token` and
  `nintendo_id_token`. eosd answers the token grant with an account straight away (a
  "headless" account, no continuation). Connect accepts that account's access or id token as
  `epicgames_access_token` / `epicgames_id_token`. Then Catapult and the match servers
  (`platforms/switch/games/fall-guys`).

## From the previous README

### Endpoints

| Path | What |
| --- | --- |
| `GET /sdk/v1/default`, `/sdk/v1/product/<id>` | the SDK config document, hand-written, every service at `api.epicgames.dev` |
| `POST /auth/v1/oauth/token` | `client_credentials`; `external_auth` with `nintendo_nsa_id_token` / `nintendo_id_token` (verified by nx-baas) or `deviceid_access_token` |
| `POST /auth/v1/accounts/deviceid`, `GET /auth/v1/oauth/jwks` | device ids; the token signing keys |
| `/kws/v1/...` | age gate info and permissions (every player an adult, the title's keys granted) |
| `/telemetry/...` | analytics, dropped |
| host `backend.innersloth.com`, `/api/user/...` | Among Us's account backend: friend code, friends, requests, blocks, lobby invites |
| anything else | `!! unhandled` with method, path (ids tagged), header names and body shape, 404 |
