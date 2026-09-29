import { describe, expect, it } from 'vitest'
import { eraseMembership, revokeEveryKey } from './account-deletion'

type Call = { method: string; url: string; auth: string | null }

function core(routes: Record<string, Response>) {
  const calls: Array<Call> = []
  const fetchCore = (input: string | URL, init?: RequestInit) => {
    const method = init?.method ?? 'GET'
    const url = String(input)
    calls.push({
      method,
      url,
      auth: new Headers(init?.headers).get('authorization'),
    })
    const answer = routes[`${method} ${url}`] as Response | undefined
    return Promise.resolve(answer ?? new Response(null, { status: 404 }))
  }
  return { calls, fetchCore }
}

const json = (status: number, body: unknown) =>
  new Response(JSON.stringify(body), { status })

describe('revokeEveryKey', () => {
  it("revokes each of the person's keys with their own token", async () => {
    const { calls, fetchCore } = core({
      'GET http://core/api/keys': json(200, {
        keys: [{ id: 'k1' }, { id: 'k2' }],
      }),
      'DELETE http://core/api/keys/k1': new Response(null, { status: 204 }),
      'DELETE http://core/api/keys/k2': new Response(null, { status: 204 }),
    })
    await revokeEveryKey('http://core/', 'tok', fetchCore)
    expect(calls.map((c) => `${c.method} ${c.url}`)).toEqual([
      'GET http://core/api/keys',
      'DELETE http://core/api/keys/k1',
      'DELETE http://core/api/keys/k2',
    ])
    expect(calls.every((c) => c.auth === 'Bearer tok')).toBe(true)
  })

  it('has nothing to revoke on a core that never issued a key', async () => {
    const { calls, fetchCore } = core({
      'GET http://core/api/keys': json(503, { error: 'no_credential' }),
    })
    await revokeEveryKey('http://core', 'tok', fetchCore)
    expect(calls).toHaveLength(1)
  })

  it('fails the step when the keys cannot be listed', async () => {
    const { fetchCore } = core({
      'GET http://core/api/keys': json(503, { error: 'unavailable' }),
    })
    await expect(
      revokeEveryKey('http://core', 'tok', fetchCore),
    ).rejects.toThrow(/503/)
  })

  it('fails the step when a key survives its revocation', async () => {
    const { fetchCore } = core({
      'GET http://core/api/keys': json(200, { keys: [{ id: 'k1' }] }),
      'DELETE http://core/api/keys/k1': json(502, { error: 'unavailable' }),
    })
    await expect(
      revokeEveryKey('http://core', 'tok', fetchCore),
    ).rejects.toThrow(/k1/)
  })
})

describe('eraseMembership', () => {
  it('asks the core to erase the member with the person\'s token', async () => {
    const { calls, fetchCore } = core({
      'DELETE http://core/api/v1/me': new Response(null, { status: 202 }),
    })
    await eraseMembership('http://core/', 'tok', fetchCore)
    expect(calls).toEqual([
      { method: 'DELETE', url: 'http://core/api/v1/me', auth: 'Bearer tok' },
    ])
  })

  it('treats a member already erased as done, and any other refusal as a failure', async () => {
    const gone = core({
      'DELETE http://core/api/v1/me': json(403, { error: 'account_erased' }),
    })
    await expect(eraseMembership('http://core', 'tok', gone.fetchCore)).resolves.toBeUndefined()
    const down = core({
      'DELETE http://core/api/v1/me': new Response(null, { status: 503 }),
    })
    await expect(eraseMembership('http://core', 'tok', down.fetchCore)).rejects.toThrow('503')
  })
})
