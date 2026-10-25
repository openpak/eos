# Evidence: eos

What is known so far is in [`protocol.md`](protocol.md), with sources in
[`provenance.md`](provenance.md). The short version:

- The SDK resolves one name, `api.epicgames.dev`, and trusts the console CA store. An
  OpenPak-CA certificate on that name is accepted.
- Its first request is `GET /sdk/v1/default?platformId=SWT`, the SDK config document. That
  endpoint is public on the real service; the document is the SDK's service map, so serving
  our own also decides where every later call goes.
- Until that document is served the SDK retries forever and never logs in. Everything after
  it, starting with Connect login on the NSA id_token, is unmeasured.
