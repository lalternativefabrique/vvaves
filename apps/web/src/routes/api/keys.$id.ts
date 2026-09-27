import { createFileRoute } from '@tanstack/react-router'
import { auth } from '@/lib/auth'

export const Route = createFileRoute('/api/keys/$id')({
  server: {
    handlers: {
      DELETE: ({ request }: { request: Request }) => auth.coreProxy()(request),
    },
  },
})
