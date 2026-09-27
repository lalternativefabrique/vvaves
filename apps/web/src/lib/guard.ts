import type { Guarded } from '@lalternative/auth/urbangate'

export type GuardOutcome =
  { status: 'allowed'; email: string; name: string } | { status: 'refused' }

export class IdentityProviderUnavailableError extends Error {
  constructor(status: number) {
    super(`identity provider unavailable (${status})`)
    this.name = 'IdentityProviderUnavailableError'
  }
}

export function guardOutcome(guarded: Guarded): GuardOutcome {
  if ('session' in guarded) {
    const { email, name } = guarded.session.user
    return { status: 'allowed', email, name }
  }
  const { status } = guarded.response
  if (status === 401 || status === 403) return { status: 'refused' }
  throw new IdentityProviderUnavailableError(status)
}
