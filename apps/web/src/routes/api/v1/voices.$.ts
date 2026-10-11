import { createFileRoute } from '@tanstack/react-router'
import { auth } from '@/lib/auth'

const voices = ({ request }: { request: Request }) => auth.coreProxy()(request)

export const Route = createFileRoute('/api/v1/voices/$')({
  server: {
    handlers: { GET: voices, PUT: voices, DELETE: voices },
  },
})
