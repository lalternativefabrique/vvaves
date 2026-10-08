# ADR 0003: vvaves signs browser URLs for customer keys

## Context

Browser playback uses signed `/speak` URLs whose MAC key is derived from the
application's key (ADR 0002), checked against the keys the registry stores.
Applications now hold urbangate customer keys (`vvaves_key_<JWT>`). vvaves
verifies those statelessly and never stores them, so a signature derived
from one cannot be checked: every such URL is refused with 403.

## Decision

vvaves signs on the application's behalf. `POST /speak/sign` takes the same
body as `/speak` and a server credential (customer key, service token or
`X-Vvaves-Key`, never a signature) and answers
`{"url", "expires_at"}`: a `/speak` URL signed under the reserved issuer
`vvaves` with a vvaves-owned secret, valid 30 minutes.

- The secret is `SPEAK_SIGNING_SECRET`, or else
  `HMAC-SHA256(REGISTRY_ENCRYPTION_KEY, "vvaves/speak-signing/v1")`, so a
  deployment needs no new secret.
- The verifier answers the `vvaves` issuer with that secret before asking the
  registry; an application named `vvaves` cannot sign under its own name.
- The URL's origin is `SPEAK_PUBLIC_URL`, or else the request's
  `X-Forwarded-Proto`/`X-Forwarded-Host`/`Host`.
- In the SDK, `(*signed.Signer).Sign` calls the route for a customer key and
  signs locally for any other key; `URL` keeps signing locally.

## Consequences

Playing a reading for a customer-key application costs one round trip to
vvaves. Rotating the signing secret (or the registry key it falls back on)
invalidates outstanding URLs, which expire within 30 minutes anyway.
