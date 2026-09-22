import { createFileRoute } from '@tanstack/react-router'
import { SSO_PROVIDER_ID, ssoEnabled } from '@/lib/auth'

export const Route = createFileRoute('/api/auth/sso')({
  server: {
    handlers: {
      GET: async () =>
        new Response(
          JSON.stringify({
            enabled: ssoEnabled(),
            providerId: SSO_PROVIDER_ID,
          }),
          {
            headers: { 'Content-Type': 'application/json' },
          },
        ),
    },
  },
})
