import {
  createPlatformAuth,
  kratosPasswordsFromEnv,
  ssoFromEnv,
} from '@lalternative/auth/server'
import { tanstackStartCookies } from 'better-auth/tanstack-start'
import { pool } from './db'

// The Go core does not sign tokens; it verifies the JWT minted from this
// session (apps/core/middleware/jwt.go), so BETTER_AUTH_SECRET and the core's
// JWT_SECRET must match.
const authSecret = process.env.BETTER_AUTH_SECRET
if (!authSecret) {
  throw new Error('BETTER_AUTH_SECRET environment variable is required')
}

export const SSO_PROVIDER_ID = 'urbangate'

function ssoEnv() {
  return {
    URBANGATE_ISSUER_URL: process.env.URBANGATE_ISSUER_URL,
    URBANGATE_CLIENT_ID: process.env.URBANGATE_CLIENT_ID,
    URBANGATE_CLIENT_SECRET: process.env.URBANGATE_CLIENT_SECRET,
  }
}

export function ssoEnabled(): boolean {
  return ssoFromEnv('vvaves', ssoEnv()) !== undefined
}

export const auth = createPlatformAuth({
  database: pool,
  baseURL: process.env.BETTER_AUTH_URL ?? 'http://localhost:5273',
  secret: authSecret,
  appName: 'vvaves',
  betaMode: true,
  isInvited: async () => false,
  google: process.env.GOOGLE_CLIENT_ID
    ? {
        clientId: process.env.GOOGLE_CLIENT_ID,
        clientSecret: process.env.GOOGLE_CLIENT_SECRET!,
      }
    : undefined,
  sso: ssoFromEnv('vvaves', ssoEnv()),
  kratosPasswords: kratosPasswordsFromEnv('vvaves', {
    URBANGATE_ISSUER_URL: process.env.URBANGATE_ISSUER_URL,
    URBANGATE_PUBLIC_URL: process.env.URBANGATE_PUBLIC_URL,
    URBANGATE_PROVISIONER_CLIENT_ID:
      process.env.URBANGATE_PROVISIONER_CLIENT_ID,
    URBANGATE_PROVISIONER_CLIENT_SECRET:
      process.env.URBANGATE_PROVISIONER_CLIENT_SECRET,
  }),
  plugins: [tanstackStartCookies()],
})

export type Auth = typeof auth
