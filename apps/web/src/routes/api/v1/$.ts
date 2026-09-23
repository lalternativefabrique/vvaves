import { createFileRoute } from '@tanstack/react-router'
import { proxyToCore } from '@/lib/core-proxy'

const admin = ({ request }: { request: Request }) =>
  proxyToCore(request, { adminOnly: true })

export const Route = createFileRoute('/api/v1/$')({
  server: {
    handlers: { GET: admin, POST: admin, DELETE: admin },
  },
})
