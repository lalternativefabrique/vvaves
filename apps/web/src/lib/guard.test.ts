import { describe, expect, it } from 'vitest'
import type { Guarded } from '@lalternative/auth/urbangate'
import { IdentityProviderUnavailableError, guardOutcome } from './guard'

const refusal = (status: number): Guarded => ({
  response: new Response(null, { status }),
})

describe('guardOutcome', () => {
  it('lets a session in with its address', () => {
    const guarded: Guarded = {
      session: {
        user: {
          id: 'u1',
          identityId: 'i1',
          email: 'ada@example.com',
          emailVerified: true,
          name: 'Ada',
          role: 'user',
        },
        session: { expiresAt: '2026-10-01T00:00:00Z' },
      },
      setCookies: [],
    }
    expect(guardOutcome(guarded)).toEqual({
      status: 'allowed',
      email: 'ada@example.com',
    })
  })

  it.each([401, 403])(
    'refuses on %i, which the route turns into a redirect',
    (status) => {
      expect(guardOutcome(refusal(status))).toEqual({ status: 'refused' })
    },
  )

  it('throws on 503 rather than sending the person to sign in again', () => {
    expect(() => guardOutcome(refusal(503))).toThrow(
      IdentityProviderUnavailableError,
    )
  })
})
