import { auth } from './auth'

const CORE_URL = process.env.CORE_API_URL ?? 'http://localhost:8080'

/**
 * Same-origin proxy to the core. The browser never holds a token: the
 * person's urbangate access token is forwarded from their session, and
 * exchanged again when it nears its end.
 */
export function proxyToCore(
  request: Request,
  options: { adminOnly: boolean },
): Promise<Response> {
  return auth.coreProxy({ coreUrl: CORE_URL, adminOnly: options.adminOnly })(
    request,
  )
}
