import { createUrbangateAuth } from '@lalternative/auth/urbangate'

/**
 * Nobody has an account here: a person signs up and signs in on vvaves's
 * screens, Kratos holds the identity, and the core trusts the token
 * urbangate issues for them (urbangate ADR 0009). Built on first use, so the
 * public landing page needs no secret.
 */
function build() {
  const required = (name: string) => {
    const value = process.env[name]
    if (!value) throw new Error(`${name} environment variable is required`)
    return value
  }
  const issuerUrl =
    process.env.URBANGATE_ISSUER_URL ?? 'https://id.urbangate.dev'
  return createUrbangateAuth({
    product: 'vvaves',
    kratosUrl: process.env.KRATOS_PUBLIC_URL ?? issuerUrl,
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
