import { createFileRoute } from '@tanstack/react-router'
import { auth } from '@/lib/auth'

export const Route = createFileRoute('/api/me')({
  server: {
    handlers: {
      GET: async ({ request }: { request: Request }) => {
        const session = await auth.getSession(request.headers)
        if (!session) {
          return Response.json({ error: 'Unauthorized' }, { status: 401 })
        }
        return Response.json({
          user_id: session.user.id,
          email: session.user.email,
          name: session.user.name,
          avatar_url: '',
          roles: [session.user.role],
        })
      },
    },
  },
})
