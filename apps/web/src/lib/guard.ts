import type { Guarded } from '@lalternative/auth/urbangate'

export type GuardOutcome =
  { status: 'allowed'; email: string } | { status: 'refused' }

export class IdentityProviderUnavailableError extends Error {
  constructor(status: number) {
    super(`identity provider unavailable (${status})`)
    this.name = 'IdentityProviderUnavailableError'
  }
}

export function guardOutcome(guarded: Guarded): GuardOutcome {
  if ('session' in guarded)
    return { status: 'allowed', email: guarded.session.user.email }
  const { status } = guarded.response
  if (status === 401 || status === 403) return { status: 'refused' }
  throw new IdentityProviderUnavailableError(status)
}
