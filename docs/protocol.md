# Epic Online Services on Switch: protocol notes

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
| Among Us | — | unknown | `api.epicgames.dev` is on its host list; whether login is required is open (`servers/among-us` design §5 item 5) |

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

## 5. Connect login (UNCONFIRMED HYPOTHESIS)

From Epic's public Connect Web API documentation, the SDK's Connect login is an OAuth 2
token request: `POST /auth/v1/oauth/token`, `grant_type=external_auth`, an
`external_auth_type` naming the Nintendo credential (`nintendo_nsa_id_token` or
`nintendo_id_token`, the C SDK's `EOS_EExternalCredentialType`), `external_auth_token`
carrying the id_token, plus `deployment_id` and a `nonce`; HTTP basic auth with the title's
client id and secret. The reply is a bearer token, a product user id, and expiry.

On an OpenPak console the NSA id_token is minted by `nx-baas` (`kid openpak-baas-key-1`), so
the EOS backend verifies it the same way `servers/demonware` does: through
`nx-baas /internal/switch/identity`. That is the fact that reverses the identity verdict in
[`fall-guys.md`](fall-guys.md): Epic verifying the token upstream was the wall only while
Epic was the backend.

To be measured (E-Connect in `next-session.md`): the exact path and field names the Switch
SDK 1.15.3 sends, the `external_auth_type` value, and what it does with the reply.

## 6. What each title uses after login

Unknown for every title. The SDK config names the whole surface; the title decides. TMNT's
online co-op is expected to be Lobbies plus P2P (SPECULATIVE, from the game's cross-play
feature set). Fall Guys is Connect only, then Catapult and Epic-hosted match servers
([`fall-guys.md`](fall-guys.md)).
