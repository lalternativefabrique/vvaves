import type { PlatformSession } from '@lalternative/auth'
import { pool } from './db'

const ISSUER = process.env.URBANGATE_ISSUER_URL ?? 'https://id.urbangate.dev'
const API = process.env.URBANGATE_API_URL ?? ISSUER
const CLIENT_ID =
  process.env.URBANGATE_PROVISIONER_CLIENT_ID ?? 'vvaves-provisioner'
const CLIENT_SECRET = process.env.URBANGATE_PROVISIONER_CLIENT_SECRET

const SCOPE = 'urbangate:keys:issue'
const AUDIENCE = 'vvaves'

export interface AppKey {
  id: string
  label: string
  audience: Array<string>
  scopes: Array<string>
  createdAt: string | null
  expiresAt: string | null
}

export interface CreatedAppKey extends AppKey {
  secret: string
}

export type KeysFailure =
  | { kind: 'unauthorized' }
  | { kind: 'forbidden' }
  | { kind: 'invalid'; field: string }
  | { kind: 'unavailable'; reason: string }

export class KeysError extends Error {
  constructor(readonly failure: KeysFailure) {
    super(failure.kind)
  }
}

// better-auth writes identityId on every SSO sign-in and at enrolment; an
// account that has neither cannot be given a key.
export async function identityOf(
  session: PlatformSession,
): Promise<string | null> {
  const { rows } = await pool.query<{ identityId: string | null }>(
    `SELECT "identityId" FROM "user" WHERE id = $1`,
    [session.user.id],
  )
  return rows[0]?.identityId ?? null
}

let cached: { token: string; expiresAt: number } | undefined

async function machineToken(): Promise<string> {
  if (!CLIENT_SECRET) {
    throw new KeysError({ kind: 'unavailable', reason: 'no_credential' })
  }
  const now = Date.now()
  if (cached && cached.expiresAt > now + 30_000) return cached.token

  const res = await fetch(`${ISSUER}/oauth2/token`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/x-www-form-urlencoded' },
    body: new URLSearchParams({
      grant_type: 'client_credentials',
      client_id: CLIENT_ID,
      client_secret: CLIENT_SECRET,
      scope: SCOPE,
    }),
  })
  if (!res.ok) {
    throw new KeysError({ kind: 'unavailable', reason: 'token_refused' })
  }
  const body = (await res.json()) as {
    access_token?: string
    expires_in?: number
  }
  if (!body.access_token) {
    throw new KeysError({ kind: 'unavailable', reason: 'token_malformed' })
  }
  cached = {
    token: body.access_token,
    expiresAt: now + (body.expires_in ?? 900) * 1000,
  }
  return cached.token
}

async function call(
  path: string,
  init: RequestInit & { method: string },
): Promise<Response> {
  let res: Response
  try {
    res = await fetch(`${API}${path}`, {
      ...init,
      headers: {
        ...init.headers,
        Authorization: `Bearer ${await machineToken()}`,
      },
    })
  } catch {
    throw new KeysError({ kind: 'unavailable', reason: 'unreachable' })
  }
  if (res.ok) return res

  const reason = await res
    .json()
    .then((b: { error?: string }) => b.error ?? 'unknown')
    .catch(() => 'unknown')

  if (res.status === 401) throw new KeysError({ kind: 'unauthorized' })
  if (res.status === 403) throw new KeysError({ kind: 'forbidden' })
  if (res.status === 422)
    throw new KeysError({ kind: 'invalid', field: reason })
  throw new KeysError({ kind: 'unavailable', reason })
}

export async function listKeys(owner: string): Promise<Array<AppKey>> {
  const res = await call(
    `/api/machine/keys?owner=${encodeURIComponent(owner)}`,
    { method: 'GET' },
  )
  const body = (await res.json()) as { keys?: Array<AppKey> }
  return body.keys ?? []
}

export async function createKey(
  owner: string,
  label: string,
  scopes: Array<string>,
): Promise<CreatedAppKey> {
  const res = await call('/api/machine/keys', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ owner, label, audience: [AUDIENCE], scopes }),
  })
  // urbangate names the secret `key`; the page is handed a `secret`.
  const body = (await res.json()) as { client_id: string; key: string }
  return {
    id: body.client_id,
    label,
    audience: [AUDIENCE],
    scopes,
    createdAt: new Date().toISOString(),
    expiresAt: null,
    secret: body.key,
  }
}

// urbangate records the revocation before dropping the client, so a refusal
// here leaves the key working and must never read as revoked. Retrying is
// safe: the record is idempotent and a client already gone answers 204.
export async function revokeKey(owner: string, id: string): Promise<void> {
  const keys = await listKeys(owner)
  if (!keys.some((k) => k.id === id)) {
    throw new KeysError({ kind: 'forbidden' })
  }
  await call(`/api/machine/keys/${encodeURIComponent(id)}`, {
    method: 'DELETE',
  })
}
