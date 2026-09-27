import { createFileRoute } from '@tanstack/react-router'
import { auth } from '@/lib/auth'

const admin = ({ request }: { request: Request }) =>
  auth.coreProxy({ adminOnly: true })(request)

export const Route = createFileRoute('/api/v1/$')({
  server: {
    handlers: { GET: admin, POST: admin, DELETE: admin },
  },
})
