/**
 * A streamed reading is a sequence of independently decodable mp3 pieces,
 * each length-prefixed in the response body: a big-endian uint32 byte count
 * followed by that many bytes. Mirrors go/audioreader's FramesContentType.
 */
export const FRAMES_CONTENT_TYPE = 'application/x-lalter-audio-frames'

/** Bounds one piece: the length prefix is four bytes the server chose. */
export const MAX_FRAME_BYTES = 8 << 20

export class FrameTooLargeError extends Error {
  readonly frameLength: number

  constructor(frameLength: number) {
    super(`streamed frame of ${frameLength} bytes exceeds ${MAX_FRAME_BYTES}`)
    this.name = 'FrameTooLargeError'
    this.frameLength = frameLength
  }
}

/**
 * Reads length-prefixed frames off a byte stream as they arrive, handing
 * each complete frame to onFrame as soon as it is fully buffered. A frame can
 * straddle several network chunks, or a chunk can hold several frames.
 */
export async function readFrames(
  reader: ReadableStreamDefaultReader<Uint8Array>,
  onFrame: (frame: Uint8Array) => void,
  signal: AbortSignal,
): Promise<void> {
  let buffer = new Uint8Array(0)

  const append = (chunk: Uint8Array) => {
    const next = new Uint8Array(buffer.length + chunk.length)
    next.set(buffer)
    next.set(chunk, buffer.length)
    buffer = next
  }

  for (;;) {
    if (signal.aborted) return
    const { done, value } = await reader.read()
    if (value) append(value)
    if (done) return

    for (;;) {
      if (buffer.length < 4) break
      const view = new DataView(buffer.buffer, buffer.byteOffset, buffer.length)
      const frameLength = view.getUint32(0)
      if (frameLength > MAX_FRAME_BYTES) {
        await reader.cancel()
        throw new FrameTooLargeError(frameLength)
      }
      const total = 4 + frameLength
      if (buffer.length < total) break
      onFrame(buffer.subarray(4, total))
      buffer = buffer.subarray(total)
    }
  }
}
