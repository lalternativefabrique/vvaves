# vvaves

The HTTP facade over this platform's search, page extraction, JavaScript
rendering and speech backends. One service, one image: SearXNG, Brave and
the speech server sit behind it, and callers only ever address vvaves.

```
POST /search   {"q": "gramsci"}                     -> ranked results
POST /fetch    {"url": "https://…"}                 -> the page's main text
POST /render   {"url": "https://…"}                 -> rendered HTML
POST /map      {"url": "https://…"}                 -> the site's URLs
POST /crawl    {"url": "https://…"}                 -> 202, a crawl id
GET  /crawl/{id}                                    -> its progress and pages
POST /speak    {"text": "…"}                        -> audio
POST /speak/prime       {"text": "…", "id": "…"}    -> 202, opening read ahead
POST /speak/pregenerate {"text": "…", "id": "…"}    -> 202, whole reading cached
POST /speak/exists      {"text": "…", "id": "…"}    -> {"ready": true}
GET  /healthz                                       -> {"ok": true}
GET  /openapi.json                                  -> this contract, OpenAPI 3
```

It is the HTTP layer over
[`packages/go/search`](https://github.com/lalternativefabrique/packages/tree/main/go/search)
and [`packages/go/tts`](https://github.com/lalternativefabrique/packages/tree/main/go/tts),
plus the one piece that cannot live in a library: a browser.

## Endpoints

### `POST /search`

```json
{"q": "gramsci", "categories": ["general", "academic"],
 "limit": 10, "language": "fr", "deadline_ms": 4000}
```

One category is a plain query. Several are fused by reciprocal rank rather
than raw score — SearXNG's web category peaks around 4.0 where its academic
engines cap at 1.0, so sorting a concatenation buries whichever list scores
lowest. `deadline_ms` bounds the wait on the slowest category; one that misses
it is dropped and `partial` is set, rather than holding the response hostage.

With `BRAVE_API_KEY` set, Brave backs up the general category — it runs its
own index rather than reselling Google or Bing, which covers the gap a
self-hosted SearXNG actually has: an upstream engine rate-limited or blocked.

### `POST /fetch`

```json
{"url": "https://…", "max_runes": 6000, "render": true, "paginate": 4000}
```

Extracts the page's main text with readability. A page whose content is
rendered client-side yields almost nothing statically, so the fetch falls back
to rendering it in-process — no HTTP round trip, the renderer is a function
call. Pass `"render": false` to skip that. `paginate` returns fixed-size pages
instead of a truncation, so a long article can be walked rather than lost.

Pages are cached by URL, holding the full untruncated text, so a later call
with a different `max_runes` still sees the whole page.

### `POST /render`

```json
{"url": "https://…", "timeout_ms": 8000}
```
```json
{"html": "<html>…</html>", "final_url": "https://…/"}
```

`timeout_ms` defaults to 8s and is clamped to 20s regardless of what is asked:
an unbounded render is a request that never finishes, and on a shared browser
it starves everything queued behind it. A page that refuses the connection or
never settles answers `502` — routine on the open web, and callers treat it as
"fall back to the static result" rather than as fatal.

### `POST /map`

```json
{"url": "https://…", "max_depth": 3, "limit": 100,
 "include_paths": ["/docs"], "exclude_paths": ["/blog"],
 "seed": "sitemap", "sitemap": true, "deadline_ms": 20000}
```
```json
{"url": "https://…", "links": ["https://…/a", "https://…/b"]}
```

Every URL a site declares or links to, within a scope. Synchronous and
bounded: `limit` defaults to 100 and is clamped to 1000, the deadline to 50s.
Sitemaps are read before links are walked and are on by default — the cheapest
and most complete source a site offers.

### `POST /crawl`

```json
{"url": "https://…", "max_depth": 3, "max_pages": 50,
 "include_paths": ["/docs"], "exclude_paths": ["/blog"]}
```

Starts a crawl and answers `202` with a job id: the pages are read behind the
request rather than under it, since a hundred of them is minutes of work. At
most 100 pages. Answers `503` where no crawl store is configured — it needs
NATS, which `/search` and `/fetch` do not.

A crawl reads each page exactly as `/fetch` does: the same address check, the
same cache, the same renderer. It can reach nothing a single fetch could not.

### `GET /crawl/{id}`

```json
{"id": "…", "status": "running", "pages": 12, "failed": 1,
 "results": [{"url": "https://…", "depth": 1, "title": "…", "markdown": "…"}],
 "next": 20}
```

The job's progress and the pages read so far, 20 at a time — `offset` and
`limit` walk them, and `next` is the offset of the following page when there
is one.

### `POST /speak`

```json
{"text": "…", "scope": "chat", "id": "msg-42", "stream": false}
```

Reads text through the speech server in `apps/tts` over the OpenAI
`/v1/audio/speech` protocol. That server runs Kyutai's Pocket TTS on CPU and
advances every listener's reading by one frame per step, so a new listener
hears their first seconds at once instead of waiting for someone else's
three minutes to finish; when its slots are full it answers `503` with a
`Retry-After` rather than queueing.

`scope` and `id` name where the reading is kept. Both are optional: with an
`id` a caller can have a reading made before anyone asks for it, since reading
ahead means naming ahead of time what will be listened to. Without one the
reading is still cached under the hash of its own text, so a second listen of
the same words finds it with nothing to opt into.

The key holds a hash of the text, so an edited text misses and is read again
rather than being served the audio of words that are no longer there. Nothing
has to be invalidated: a draft that changes on every keystroke simply writes
under a new key, and a bucket lifecycle rule collects the ones nobody came
back to.

`stream: false` returns the finished audio with a `Content-Length`, which a
plain `<audio src>` needs — without one, browsers infer the duration from the
first frame header and stop at the first seam, playing only the opening
seconds of a long text with nothing reported as wrong. A reading already in
the store is always served this way, ranges included, so a second listen
starts at once and can be seeked.

`stream: true` emits each piece as it is ready, so listening starts on the
first one instead of after the last. Pieces arrive length-prefixed — a
big-endian `uint32` byte count then that many bytes — under
`application/x-lalter-audio-frames`, because concatenated mp3 frames carry no
boundary a player could find on its own. Once a piece has been sent the `200`
is committed, so a later failure cuts the response short instead of reporting
an error — which is why it is not the default.

### `POST /speak/prime`

```json
{"text": "…", "scope": "chat", "id": "msg-42"}
```

Reads the **opening** of a text — one piece, `AUDIO_OPENING_CHARS` — and
stores it, so the first listen starts on audio that already exists while the
rest is read behind it. Answers `202` without waiting: nobody is listening
yet, and a failure only means the first listener waits as they used to.

The cut is the one the reading itself would make, so the two halves meet
exactly where a seam would have fallen anyway — no word is read twice, none is
skipped.

This is what to use for a text that will be heard once or not at all — a reply
to a prompt. It buys the seconds that matter for the price of one piece,
instead of paying for a whole reading on the chance that someone might listen.
`id` is required: a reading nobody can name again cannot be found later.

### `POST /speak/pregenerate`

Reads a text **in full** and caches it, so every listen after it is served
from the store. For a text that will be heard more than once — a published
page — where paying the whole synthesis up front is amortised, and where the
first visitor should not be the one who triggers it.

The speech server serves a bounded number of readings at once, so a reading
nobody asked for holds a slot someone who pressed play may be refused. Reach
for `/speak/prime` unless the reading is genuinely expected to be heard more
than once.

### `POST /speak/exists`

Reports whether a reading is already stored, without reading its bytes — for a
caller deciding whether to offer a play button, which would otherwise download
the whole file to answer yes or no.

## Configuration

| | |
|---|---|
| `SPEAK_KEYS` | `issuer:key` pairs, an issuer repeatable: the key a service presents on `X-Vvaves-Key` and signs its browser URLs with; the registry supersedes them per issuer |
| `SPEAK_UNGUARDED` | `true` lets the speak routes answer with no key at all: a cluster-internal vvaves or a laptop, never one behind a public name. Without it and without keys, `/speak` refuses everyone |
| `FETCH_ALLOW_PRIVATE` | `true` lets `/fetch` and `/render` reach an address this deployment holds privately. Unset refuses them: these routes report what came back from a URL their caller picked, so without the check they read the internal network one request at a time |
| `SEARXNG_URL` | required by `/search`, else `503` |
| `BRAVE_API_KEY` | optional; enables the general-category fallback |
| `FETCH_PROXY` | residential endpoint `/fetch` and its render fallback read through; unset goes direct, unparseable is fatal |
| `PIPER_URL` | required by `/speak`, else `503` |
| `TTS_MODEL`, `TTS_VOICE`, `TTS_FORMAT` | voice selection; format must be frame-based (`mp3`, `opus`, `aac`, `flac`) |
| `TTS_MAX_CHARS` | characters per request; the default, -1, sends each text whole |
| `AUDIO_OPENING_CHARS` | how much of a text counts as its opening, default 800 |
| `S3_ENDPOINT`, `S3_BUCKET`, `S3_ACCESS_KEY`, `S3_SECRET_KEY`, `S3_REGION` | where readings are kept; unset disables the cache, half-set is fatal |
| `TTS_CONCURRENCY` | pieces read at once, default 1 |
| `SEARCH_DEADLINE_MS` | cap on a merged search, default 4000 |
| `FETCH_CACHE_TTL_MS` | default 15 minutes |
| `RENDER_MAX_TIMEOUT_MS` | hard ceiling, default 20000 |
| `CHROMIUM_PATH` | browser binary; set in the image |
| `LISTEN_ADDR` | default `:8080` |

A page fetch goes out through `FETCH_PROXY` when one is set. Publishers behind
bot management refuse a datacenter address whatever headers the request
carries — a browser User-Agent from a cloud network is refused exactly like an
honest one — so the exit IP, not the headers, is what decides whether a page
can be read at all. Unset fetches direct and is reported at startup;
unparseable is fatal, since a typo there would otherwise look like a working
service reading a fraction of what it is asked for.

The IP is not the whole story: a residential exit still presents Go's TLS
handshake, which matches no browser, and bot management compares the two. So
`/fetch`'s **render fallback** — the browser it falls back to when a static
fetch fails or comes back near-empty — goes through the proxy too, which is
the one path that carries both a residential address and a real browser's
fingerprint. A challenge page meant to be solved by a browser is solved there;
a flat refusal usually stays refused, and is reported as such.

`/render` itself does not use the proxy. It is a JavaScript renderer, not a
way around a block, and residential bandwidth is metered by the gigabyte
while a rendered page pulls megabytes of images and scripts — so the cost is
spent on retries, not on every render. The two run as separate Chromium
processes, since `--proxy-server` is process-wide.

An unconfigured backend disables its endpoint rather than degrading silently.

`AUDIO_OPENING_CHARS` is not `TTS_MAX_CHARS`, though both count characters.
The latter is how much text goes to the speech server at once; the former is
how much of a text counts as its opening. They must not be conflated: the
primer and the reader each split the text themselves, and two different sizes
have the halves meet somewhere other than the same cut.

With no `S3_*` set, vvaves still reads text aloud — every reading is simply
paid for again, which is what it did before there was a bucket. A half-set
configuration is fatal instead: someone meant to have a cache, and starting
without one would hide that behind a bill nobody notices until it arrives.

`TTS_MAX_CHARS` used to be the latency lever: Piper answered a request only
once it had read all of it, so a short piece was the only way to hear anything
early, and 120 characters was the measured optimum. The server in `apps/tts`
streams each sentence as it is read and, asked with
`Accept: application/x-lalter-audio-frames`, delimits them the way vvaves
delimits them for the browser — so each text goes as one request whatever
its length, the first sentence is heard after about two seconds, and there is
no cut on this side to add prompt prefills and seams. Measured on a
1200-character text, one request against twelve of 100 characters: first
sentence at 2.0s against 2.5s, the same total. `TTS_CONCURRENCY` stays at 1:
one request covers a reading, and the server takes one slot per request.

## Who may speak

`/speak*` answers two callers, and nothing else.

A **service** on the cluster's own network sends its key on `X-Vvaves-Key`,
or, with `OIDC_ISSUER_URL` set, a bearer token from the suite's identity
provider carrying `aud: vvaves` and the `vvaves:speak` scope. The Go client
sends it through `client.Config.Authorize`, typically
`svcauth.ClientCredentials.Authorize` from `packages/go/svcauth`.
Each application has one key, so it can be rotated or revoked without
touching another's, and a log line can name who called.
A **browser** cannot hold a key, so it carries a signature instead: the
application that knows who is listening signs `scope`, `id`, a hash of the
text and an expiry with a MAC key derived from that same key, and hands the
listener a URL good for
that one reading. Vvaves recomputes the MAC and compares. It still knows
nothing about users — a signature over what was asked for is the whole of its
notion of identity, the same shape as an S3 presigned URL and for the same
reason: the service holding the bytes serves them, the service holding the
rights only authorises.

The signature covers the text because it is what stops a link from being spent
on anything else: without it, an authorisation for one reading is an
authorisation to have any text at all synthesized on that account.

With neither variable set both checks are skipped, which is what a deployment
reachable only from inside the cluster wants — the network is its boundary,
and a credential invented to talk to itself is one that exists only to be
checked.

A signature is good for `/speak` and nothing else. `/speak/prime` and
`/speak/pregenerate` start a synthesis nobody is waiting for, on a voice that
reads one utterance at a time, so they take an app key: a link handed to a
listener must not also buy a whole article being read ahead of them. That is
enforced in the handler rather than left to routing, because a front door that
matches paths by prefix sends `/speak/prime` the same URL as `/speak`.

The browser calls vvaves directly rather than through the application that
authorised it. A reading is tens of seconds of bytes: relaying it would have
that application's own server stream media for the whole of it, one goroutine
per listener, and buffering anywhere along the way would undo what
`/speak/prime` buys. In production only the speak routes are public —
`/render` and `/fetch` execute third-party JavaScript against a URL the caller
picks, which on the open internet is an SSRF offered to anyone.

## Admin

Which applications may speak through vvaves is a registry vvaves owns, not
a pair of environment variables. `apps/web` is the back-office: an operator
signs in, registers an application by its issuer name, and is shown its key
once. That one key is what the application's server presents on its own
calls and what it signs browser URLs with; a second one would protect
nothing, since the key sent on the wire already buys everything a signature
can (see `docs/adr/0002-one-key-per-application.md`). Rotating mints a new
key and keeps the old one working for a day; revoking ends both at once. The
speak guard reads the registry at request time, so none of it needs a
restart.

The team signs in through the suite's identity provider, urbangate
(`URBANGATE_ISSUER_URL`, `URBANGATE_CLIENT_ID`, `URBANGATE_CLIENT_SECRET`): a
person whose roles claim carries `vvaves:admin` is admin here, nobody else is,
and there is no first-admin setup any more. The registry lives in Postgres beside the admin's own accounts
(`DATABASE_URL`), the keys sealed with `REGISTRY_ENCRYPTION_KEY` the way the
platform's other credentials are; the list shows their last four characters.
Without one, vvaves runs as before on `SPEAK_KEYS`; with one, those pairs
still count for the issuers the registry does not name. The admin API (`/api/v1/admin/apps`) sits behind the JWT the web app
mints from its Better Auth session with `JWT_SECRET`; the browser only ever
reaches it through the web app's own proxy.

## The contract

`GET /openapi.json` serves this API's OpenAPI 3 description, generated from the
handlers themselves by `sklp run generate` and committed beside them. CI
regenerates it and fails on a difference, so a handler cannot change without
the contract following.

It is served rather than copied because a copy nobody is forced to refresh goes
stale silently. Both drifts this replaces were exactly that: the README
documented eight routes out of eleven for as long as `/map` and `/crawl`
existed, and `packages/go/search`'s tornade client — written by hand against
`/search` alone — never learned the other two exist. A consumer generating from
this document gets a compile error where it used to get a 404.

## Clients

This module ships both halves of that arrangement, so an application does not
rewrite the contract:

- `client` (Go) is a `tts.Voice` that speaks through vvaves. `client.New`
  takes the `Key` for server-to-server calls; `PrimeOpening`,
  `Pregenerate`, `Exists` and the `*Named` variants map onto the routes above.
- `signed` (Go) holds the signature scheme. `signed.NewSigner` mints the URL
  the application hands its browser; vvaves verifies with the same package.
- `sdk-react` (npm, `@lalternative/vvaves-sdk-react`) plays a signed reading
  in the browser: `speakSource` builds the request, `useVoicePlayback`
  streams and decodes it.

## Running it

```bash
go test ./...
SEARXNG_URL=… PIPER_URL=… go run ./cmd/vvaves
```

`/render` and `/fetch`'s fallback need a Chromium on `PATH`, or `CHROMIUM_PATH`
pointing at one. The image carries its own:

```bash
docker build -t vvaves .
docker run -p 8080:8080 -e SEARXNG_URL=… -e PIPER_URL=… vvaves
```

## Deployment

Two paths, and each needs the same two values supplied at deploy time —
`SPEAK_KEYS` (`<issuer>:<key>` pairs, one per application not held in the
registry) and, on the sklp path, `VVAVES_AUDIO_HOST` (the public name the
speak route answers on). Neither is committed: unset, vvaves simply accepts
nothing it did not already accept on the internal network.

The public name must resolve before the certificate can be issued — the
challenge is served on that host — so point the DNS at the front door first
and let the issuer follow.

It runs on the OVH cluster in its own `vvaves-prod` namespace, reaching
searxng and piper across the `ai` namespace by their cluster DNS names.
Manifests live in `infra/k8s/base`; ArgoCD syncs them from `main` through the
Application in `infra/k8s/argocd`, which the cluster's `vvaves-root`
app-of-apps discovers (declared in kube-infra's `app-v1` stack).

The `ai` namespace is declared here too, in `infra/k8s/base-ai` behind the
`production-ai` overlay. It holds searxng, piper and the redis searxng caches
into — the backends this service exists to put one HTTP contract in front of.
They used to be declared in synthiz, which reaches them the same way vvaves
does; the namespace kept its name through the move, so every caller's DNS
still resolves.

It stays a namespace of its own rather than folding into `vvaves-prod`: its
ResourceQuota covers workloads shared by more than one product, and that
budget is easier to reason about next to them than mixed into a single
service's.

Rolling a version means publishing an image: argocd-image-updater watches the
registry for immutable date-sha tags and writes the new one back to `main`
itself. The tag committed in `infra/k8s/base/kustomization.yaml` is only the
version a fresh cluster starts from. Nothing reads `latest`.

## Design notes

**One Chromium, many tabs.** The browser process is shared and started
lazily on the first render; a tab is opened and closed per request. Launching
a browser per request would pay Chromium's 1-2s startup every time — the tab
is the unit of isolation that is actually cheap. Each tab inherits its
request's deadline, so an abandoned render tears down its own tab without
touching the browser others are using.

**Waiting for the network, not for `load`.** A JS-rendered page's content
arrives through a fetch/XHR fired after the load event, so waiting on `load`
returns the same empty shell a plain HTTP fetch already sees — the entire
reason rendering exists here. Vvaves watches CDP network events and settles
once nothing has been in flight for 500ms, then keeps watching a further two
seconds: a script that fires its request on a timer leaves the network idle in
the meantime, and calling that lull "settled" returns the shell.

**The opening, not the whole reading.** A text of a few thousand characters
takes Piper tens of seconds to read, so a listener who presses play waits out
a spinner. Streaming cuts that to the first piece — two seconds instead of
thirty — and reading that first piece before anyone asks removes even those:
the start comes out of the store at once, and the rest is read while it plays.
The listener hears one recording.

Only the opening, because most readings never happen. Reading a whole text
ahead of time pays for all of them on the chance that someone listens to one,
and it occupies a voice that synthesizes one utterance at a time — a burst of
texts nobody opened would put someone who did press play at the back of a
queue. The opening is a single request, and it buys the seconds that actually
show.

Reading everything ahead is still the right answer where a text will be heard
more than once, or where the first listener must not be the one who pays:
that is `/speak/pregenerate`, and the choice between the two is a question
about how many listens are expected, not about which caller is asking.

**Non-root, without Chromium's inner sandbox.** That sandbox needs
unprivileged user namespaces, which a container's default seccomp profile
blocks; Playwright passed `--no-sandbox` by default, so this is the isolation
the service has always run with. The container is the boundary around the
third-party JavaScript this service exists to execute — hence the non-root
user, which is what actually contains a compromised render.
