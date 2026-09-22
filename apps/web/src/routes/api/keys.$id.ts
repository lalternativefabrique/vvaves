import { createFileRoute } from '@tanstack/react-router'
import type { PlatformSession } from '@lalternative/auth'
import { auth } from '@/lib/auth'
import { KeysError, identityOf, revokeKey } from '@/lib/urbangate'

function json(status: number, body: unknown): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  })
}

export const Route = createFileRoute('/api/keys/$id')({
  server: {
    handlers: {
      DELETE: async ({
        request,
        params,
      }: {
        request: Request
        params: { id: string }
      }) => {
        // eslint-disable-next-line @typescript-eslint/no-unnecessary-type-assertion
        const session = (await auth.api.getSession({
          headers: request.headers,
        })) as PlatformSession | null
        if (!session) return json(401, { error: 'unauthorized' })

        const identity = await identityOf(session)
        if (!identity) return json(403, { error: 'no_identity' })

        try {
          await revokeKey(identity, params.id)
          return new Response(null, { status: 204 })
        } catch (error) {
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
      },
    },
  },
})
