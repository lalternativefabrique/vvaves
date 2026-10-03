/** Where a reading may be fetched, and what to ask for once there. */
export type VoiceSource = {
  url: string
  body: unknown
}

/** The reading an application's server signed a URL for. */
export type SignedReading = {
  /** The signed URL onto vvaves's /speak, as handed out by the application. */
  url: string
  /** The exact text the signature covers: what the server read aloud, not the raw content. */
  text: string
  scope: string
  id: string
  /** The listener's language as a BCP 47 tag; picks the voice. Defaults to the browser's Accept-Language. */
  lang?: string
}

/**
 * Builds the request the browser sends vvaves for a signed reading. Always
 * streamed: an opening the application primed is only served on the
 * streaming path, and only a stream starts playing before the end is read.
 */
export function speakSource(reading: SignedReading): VoiceSource {
  return {
    url: reading.url,
    body: { text: reading.text, scope: reading.scope, id: reading.id, lang: reading.lang, stream: true },
  }
}
