import { createUrbangateAuth } from '@lalternative/auth/urbangate'
import { revokeEveryKey } from './account-deletion'
import { appUrl } from './app-url'

/**
 * Nobody has an account here: a person signs up and signs in on vvaves's
 * screens, Kratos holds the identity, and the core trusts the token
 * urbangate issues for them (urbangate ADR 0009). Built on first use, so a
 * route that never signs anyone in needs no secret.
 */
function build() {
  const required = (name: string) => {
    const value = process.env[name]
    if (!value) throw new Error(`${name} environment variable is required`)
    return value
  }
  const issuerUrl =
    process.env.URBANGATE_ISSUER_URL ?? 'https://id.urbangate.dev'
  const coreUrl = process.env.CORE_API_URL ?? 'http://localhost:8080'
  return createUrbangateAuth({
    product: 'vvaves',
    kratosUrl: process.env.KRATOS_PUBLIC_URL ?? issuerUrl,
    coreUrl,
    accountDeletion: ({ accessToken }) => ({
      deleteData: () => revokeEveryKey(coreUrl, accessToken),
    }),
    urbangate: {
      issuerUrl,
      provisioner: {
        clientId:
          process.env.URBANGATE_PROVISIONER_CLIENT_ID ?? 'vvaves-provisioner',
        clientSecret: required('URBANGATE_PROVISIONER_CLIENT_SECRET'),
      },
      admin: {
        clientId: process.env.URBANGATE_CLIENT_ID ?? 'vvaves-admin',
        clientSecret: required('URBANGATE_CLIENT_SECRET'),
      },
    },
    sso: { appUrl: appUrl() },
  })
}

type Auth = ReturnType<typeof build>

let built: Auth | undefined

export function getAuth(): Auth {
  built ??= build()
  return built
}

export const auth: Auth = new Proxy({} as Auth, {
  get: (_, prop: string | symbol) => Reflect.get(getAuth(), prop) as unknown,
})
