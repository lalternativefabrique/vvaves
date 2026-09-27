import { createFileRoute } from '@tanstack/react-router'
import { auth } from '@/lib/auth'

// The key page talks to the core, which holds the provisioner credential and
// names the owner from the token's identityId; any signed-in person may.
const keys = ({ request }: { request: Request }) => auth.coreProxy()(request)

export const Route = createFileRoute('/api/keys/')({
  server: {
    handlers: { GET: keys, POST: keys },
  },
})
