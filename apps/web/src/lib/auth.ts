import { createPlatformAuth, ssoFromEnv } from '@lalternative/auth/server'
import { tanstackStartCookies } from 'better-auth/tanstack-start'
import { pool } from './db'

/**
 * Better Auth for the web app. The Go core does not sign tokens — it only
 * verifies the JWT minted from this session (see apps/core/middleware/jwt.go),
 * so BETTER_AUTH_SECRET here and JWT_SECRET in the core must be kept in sync
 * per your minting setup.
 *
 * Nobody signs up here with a password: the team signs in through the suite's
 * identity provider (urbangate), and the admin role comes from its roles claim.
 */
const authSecret = process.env.BETTER_AUTH_SECRET
if (!authSecret) {
  throw new Error('BETTER_AUTH_SECRET environment variable is required')
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
  sso: ssoFromEnv('vvaves', {
    URBANGATE_ISSUER_URL: process.env.URBANGATE_ISSUER_URL,
    URBANGATE_CLIENT_ID: process.env.URBANGATE_CLIENT_ID,
    URBANGATE_CLIENT_SECRET: process.env.URBANGATE_CLIENT_SECRET,
  }),
  plugins: [tanstackStartCookies()],
})

export const ssoEnabled = ssoFromEnv('vvaves', {
  URBANGATE_ISSUER_URL: process.env.URBANGATE_ISSUER_URL,
  URBANGATE_CLIENT_ID: process.env.URBANGATE_CLIENT_ID,
  URBANGATE_CLIENT_SECRET: process.env.URBANGATE_CLIENT_SECRET,
}) !== undefined

export type Auth = typeof auth
