# ADR 0005: every service credential is a bearer token

## Status

Accepted — 2026-10-09. Amends ADR 0002 on how an application presents its
key.

## Context

ADR 0002 gave an application one key, presented as-is on `X-Vvaves-Key` for
its own calls and used as the root of its browser-bound signatures. Since
then the suite moved service credentials to urbangate (urbangate ADR 0004):
a product's customers hold a `vvaves_key_…` key urbangate issued, verified
through `packages/go/appkeys`, and a product of the suite holds a
`client_credentials` token from the identity provider. Both travel as
`Authorization: Bearer`, and both carry scopes a route can ask for
(`vvaves:speak`, `vvaves:transcribe`).

Every consumer in production — lalter, synthiz, partage, messag — now holds
an urbangate key. Nothing presents a registry key on `X-Vvaves-Key` any
more, yet the guard still accepted one, and that path answered every service
route with no scope check at all: a registry key granted transcription the
day `/transcribe` appeared, which is exactly what ADR 0004 said a key must
not get for free.

## Decision

The `X-Vvaves-Key` header is gone, from the server, the Go client and the
contract. A service authenticates with a bearer token only: an urbangate
customer key for vvaves, or an identity-provider token with `aud: vvaves`.
The client sends whatever key it was given as that bearer, or what
`WithAuthorize` attaches.

The registry keeps its job: a registry key, or a `SPEAK_KEYS` entry, is the
root a browser-bound `/speak` signature derives from, and nothing else. The
registry no longer answers "which application holds this key".

## Consequences

- Every service route is scope-checked: there is no credential left that
  bypasses `ScopeSpeak` or `ScopeTranscribe`.
- A key the registry minted no longer authenticates a server-to-server call.
  An application that still had one configured for that purpose must take an
  urbangate key instead; none did in production at the time of writing.
- `SPEAK_KEYS` and the registry are signing material only; their
  documentation says so.
