# Among Us

Title `0100B0C013912000`, update 2026.18.0 (v3997696). Unity IL2CPP; EOS SDK 1.16.4 as
`Data/Plugins/EOSSDK-Switch-Shipping.nro`, loaded at runtime. Its matchmaker and game server are
Impostor (`servers/among-us`). Runs recorded 2026-10-10 on Harbor (desktop, console mode,
firmware 23.0.1, build `64949c8e1d`), run directory `eos-host`, test account email3.

## What the game needs, in order

1. EOS sign-in: the whole sequence in [`protocol.md`](protocol.md) §5. **Served and working**:
   config document, client token, per-product config, KWS age gate info, Connect login on the
   NSA id_token, KWS permissions.
2. Its own date-of-birth age gate, once per save (the game's screen, not a server call).
3. `https://backend.innersloth.com/api/user/...`: Innersloth's account backend. First call
   after the age gate; unanswered, the game says "Could not connect to your Among Us Account.
   (Error FriendCodeFail)" and greys out ONLINE. **Served by eosd, not yet reachable**: the
   name is outside the signed redirect ceiling, so Harbor resolves it to the real Innersloth
   (`104.20.x.x`), which refuses an OpenPak EOS token. Waiting on the owner's ceiling signature
   (`.innersloth.com`).
4. `matchmaker.among.us /api/user`, `/api/games`, then UDP to Impostor: `servers/among-us`,
   live since 2026-09-11, never reached yet.

## The Innersloth backend as eosd answers it

Paths and JSON names are the game's own, read from its IL2CPP metadata (string literals and
`[JsonProperty]` arguments): `user/username`, `user/friends`, `user/friends/received-requests`,
`user/friends/request`, `user/friends/request-by-friendcode`, `user/blocks`,
`user/lobby/invites`, plus `user/report-player`, `user/accounts/deletion`,
`user/redeem-currency`, `user/validate-receipt[-batch]` (not answered yet; logged). Bodies are
JSON:API shaped, `{"data":{"type":…,"attributes":{…}}}`; a friend code is
`{"username","discriminator"}`. Authorization is the EOS token eosd minted (checked with eosd's
own key).

- The friend code is derived from the PUID (two short words and four digits), so it never
  changes and needs no storage. Renaming is accepted and ignored.
- Friends, requests, blocks and lobby invites live in memory; a restart loses them, never a
  sign-in.
- Which method each path takes (GET friend code, POST/PUT/DELETE on requests) is inferred from
  the field names; the first run past the ceiling settles it. Every request outside the table
  is logged by shape (`[innersloth] !! unhandled`).

## Open

- Ceiling signature for `.innersloth.com` (`scratch/eos/SIGN-ME.md`), then: friend code shows,
  ONLINE enables, `/api/user` reaches Impostor, two consoles in one lobby.
- KWS answers every player as an adult; a minor's restrictions come from the console's parental
  controls (nx-pctl), which the SDK checks itself before KWS.
