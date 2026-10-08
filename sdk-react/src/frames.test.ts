import { test } from 'node:test'
import assert from 'node:assert/strict'
import { FrameTooLargeError, MAX_FRAME_BYTES, readFrames } from './frames.ts'

function frame(payload: string): Uint8Array {
  const body = new TextEncoder().encode(payload)
  const out = new Uint8Array(4 + body.length)
  new DataView(out.buffer).setUint32(0, body.length)
  out.set(body, 4)
  return out
}

function streamOf(chunks: Uint8Array[]): ReadableStreamDefaultReader<Uint8Array> {
  return new ReadableStream<Uint8Array>({
    start(controller) {
      for (const c of chunks) controller.enqueue(c)
      controller.close()
    },
  }).getReader()
}

async function collect(chunks: Uint8Array[]): Promise<string[]> {
  const got: string[] = []
  await readFrames(streamOf(chunks), (f) => got.push(new TextDecoder().decode(f)), new AbortController().signal)
  return got
}

test('one chunk can hold several frames', async () => {
  const both = new Uint8Array([...frame('one'), ...frame('two')])
  assert.deepEqual(await collect([both]), ['one', 'two'])
})

test('a frame can straddle several chunks', async () => {
  const whole = frame('opening piece')
  const chunks = [whole.subarray(0, 2), whole.subarray(2, 9), whole.subarray(9)]
  assert.deepEqual(await collect(chunks), ['opening piece'])
})

test('an empty frame is handed over as empty', async () => {
  assert.deepEqual(await collect([frame(''), frame('rest')]), ['', 'rest'])
})

test('a truncated tail is dropped rather than emitted', async () => {
  const cut = frame('never finished').subarray(0, 8)
  assert.deepEqual(await collect([frame('ok'), cut]), ['ok'])
})

test('aborting stops before the next read', async () => {
  const controller = new AbortController()
  controller.abort()
  const got: string[] = []
  await readFrames(streamOf([frame('one')]), (f) => got.push(new TextDecoder().decode(f)), controller.signal)
  assert.deepEqual(got, [])
})

test('a length prefix past the ceiling stops the read instead of buffering it', async () => {
  const header = new Uint8Array(4)
  new DataView(header.buffer).setUint32(0, MAX_FRAME_BYTES + 1)
  await assert.rejects(collect([header]), FrameTooLargeError)
})
