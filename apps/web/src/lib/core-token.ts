import { createHmac } from 'node:crypto'
import type { PlatformSession } from '@lalternative/auth'

/**
 * Mints the JWT vvaves verifies (apps/core/middleware/jwt.go). Short-lived
 * because it is minted per request from a session already validated, and it
 * never reaches the browser: the proxy attaches it server-side.
 */
const TTL_SECONDS = 300

function base64url(input: Buffer | string): string {
  return Buffer.from(input)
    .toString('base64')
    .replace(/\+/g, '-')
    .replace(/\//g, '_')
    .replace(/=+$/, '')
}

export function mintCoreToken(session: PlatformSession): string {
  const secret = process.env.JWT_SECRET
  if (!secret) {
    throw new Error('JWT_SECRET is not set; vvaves would reject every request')
  }
  const header = base64url(JSON.stringify({ alg: 'HS256', typ: 'JWT' }))
  const payload = base64url(
    JSON.stringify({
      sub: session.user.id,
      email: session.user.email,
      name: session.user.name,
      identityId:
        (session.user as { identityId?: string | null }).identityId ??
        undefined,
      exp: Math.floor(Date.now() / 1000) + TTL_SECONDS,
    }),
  )
  const signature = base64url(
    createHmac('sha256', secret).update(`${header}.${payload}`).digest(),
  )
  return `${header}.${payload}.${signature}`
}
