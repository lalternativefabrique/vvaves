import type { PlatformSession } from '@lalternative/auth'
import { auth } from './auth'
import { mintCoreToken } from './core-token'

const CORE_URL = process.env.CORE_API_URL ?? 'http://localhost:8080'

const HOP_BY_HOP = new Set([
  'connection',
  'keep-alive',
  'proxy-authenticate',
  'proxy-authorization',
  'te',
  'trailer',
  'transfer-encoding',
  'upgrade',
  'host',
  'content-length',
  'cookie',
])

function forwardHeaders(headers: Headers): Headers {
  const out = new Headers()
  headers.forEach((value, key) => {
    if (!HOP_BY_HOP.has(key.toLowerCase())) out.set(key, value)
  })
  return out
}

function json(status: number, body: unknown): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  })
}

/**
 * Same-origin proxy to the core. The browser never holds a token: the session
 * is exchanged here for the short-lived JWT the core verifies against
 * JWT_SECRET, and the core decides what that person may do.
 */
export async function proxyToCore(
  request: Request,
  options: { adminOnly: boolean },
): Promise<Response> {
  // eslint-disable-next-line @typescript-eslint/no-unnecessary-type-assertion
  const session = (await auth.api.getSession({
    headers: request.headers,
  })) as PlatformSession | null
  if (!session) return json(401, { error: 'unauthorized' })
  if (options.adminOnly && session.user.role !== 'admin') {
    return json(403, { error: 'forbidden' })
  }

  const url = new URL(request.url)
  const headers = forwardHeaders(request.headers)
  headers.set('Authorization', `Bearer ${mintCoreToken(session)}`)

  const upstream = await fetch(`${CORE_URL}${url.pathname}${url.search}`, {
    method: request.method,
    headers,
    body:
      request.method === 'GET' || request.method === 'HEAD'
        ? undefined
        : request.body,
    duplex: 'half',
    redirect: 'manual',
  } as RequestInit)

  return new Response(upstream.body, {
    status: upstream.status,
    headers: forwardHeaders(upstream.headers),
  })
}
