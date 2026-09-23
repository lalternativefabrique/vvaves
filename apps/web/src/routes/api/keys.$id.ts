import { createFileRoute } from '@tanstack/react-router'
import { proxyToCore } from '@/lib/core-proxy'

export const Route = createFileRoute('/api/keys/$id')({
  server: {
    handlers: {
      DELETE: ({ request }: { request: Request }) =>
        proxyToCore(request, { adminOnly: false }),
    },
  },
})
