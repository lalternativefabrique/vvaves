import { createFileRoute } from '@tanstack/react-router'
import { proxyToCore } from '@/lib/core-proxy'

// The key page talks to the core, which holds the provisioner credential and
// names the owner from the token's identityId; any signed-in person may.
const keys = ({ request }: { request: Request }) =>
  proxyToCore(request, { adminOnly: false })

export const Route = createFileRoute('/api/keys/')({
  server: {
    handlers: { GET: keys, POST: keys },
  },
})
