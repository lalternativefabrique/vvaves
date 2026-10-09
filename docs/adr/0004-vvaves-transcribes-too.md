# ADR 0004: vvaves transcribes too

## Status

Accepted

## Context

Three products relay recordings to the same Whisper endpoint on OVH AI
Endpoints, each through its own copy of the same proxy: lalter
(`ego/voice`), synthiz (`apps/core/voice`) and messag (dictation in the
composer). Each holds its own provider key, its own size limit and its own
error handling. vvaves is already the suite's voice: every product reaches
it with one key per application (ADR 0002).

## Decision

vvaves serves `POST /transcribe`: a multipart recording in, `{text}` out,
through an OpenAI-compatible `/audio/transcriptions` endpoint set by
`STT_URL`, `STT_API_KEY` and `STT_MODEL` (Whisper on Scaleway Generative APIs by default).

It accepts the service credentials `/speak/prime` accepts, under a scope of
its own, `vvaves:transcribe`, so a token or customer key granted reading is
not thereby granted transcription. A signed URL is refused: a signature buys
one listen of a named text, and a recording has no text to name in advance.
A browser records; its application's server relays the audio with
`client.Voice.Transcribe`.

Nothing is stored: the recording goes to the provider and the text back to
the caller.

## Consequences

- A product dictates with the key it already holds for `/speak`; the
  provider key lives in one place.
- lalter and synthiz can drop their own proxies by switching to the client.
- A recording crosses one more hop. At dictation sizes, seconds of audio, the
  hop is small next to the transcription itself.
- vvaves becomes a processor of recorded speech for every product that uses
  it, which each product's privacy notice has to name.
