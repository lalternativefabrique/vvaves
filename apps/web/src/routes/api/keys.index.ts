import { createFileRoute } from '@tanstack/react-router'
import type { PlatformSession } from '@lalternative/auth'
import { auth } from '@/lib/auth'
import { ScopeSpeak } from '@/lib/scopes'
import { KeysError, createKey, identityOf, listKeys } from '@/lib/urbangate'

function json(status: number, body: unknown): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  })
}

function failed(error: unknown): Response {
  if (!(error instanceof KeysError)) {
    return json(503, { error: 'unavailable' })
  }
  switch (error.failure.kind) {
    case 'unauthorized':
      return json(401, { error: 'unauthorized' })
    case 'forbidden':
      return json(403, { error: 'forbidden' })
    case 'invalid':
      return json(422, { error: error.failure.field })
    case 'unavailable':
      console.error(`urbangate unavailable: ${error.failure.reason}`)
      return json(503, { error: 'unavailable' })
  }
}

// The owner comes from the session, never from the request.
async function owner(request: Request): Promise<string | Response> {
  // eslint-disable-next-line @typescript-eslint/no-unnecessary-type-assertion
  const session = (await auth.api.getSession({
    headers: request.headers,
  })) as PlatformSession | null
  if (!session) return json(401, { error: 'unauthorized' })

  const identity = await identityOf(session)
  if (!identity) return json(403, { error: 'no_identity' })
  return identity
}

export const Route = createFileRoute('/api/keys/')({
  server: {
    handlers: {
      GET: async ({ request }: { request: Request }) => {
        const who = await owner(request)
        if (who instanceof Response) return who
        try {
          return json(200, { keys: await listKeys(who) })
        } catch (error) {
          return failed(error)
        }
      },
      POST: async ({ request }: { request: Request }) => {
        const who = await owner(request)
        if (who instanceof Response) return who

        const body = (await request.json().catch(() => ({}))) as {
          label?: string
        }
        const label = body.label?.trim()
        if (!label) return json(422, { error: 'label' })

        try {
          return json(201, await createKey(who, label, [ScopeSpeak]))
        } catch (error) {
          return failed(error)
        }
      },
    },
  },
})
