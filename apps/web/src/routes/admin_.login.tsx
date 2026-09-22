import { createFileRoute, useNavigate } from '@tanstack/react-router'
import { createServerFn } from '@tanstack/react-start'
import { AdminLoginForm } from '@lalternative/admin'
import { authClient } from '@/lib/auth-client'
import { getProfile } from '@/lib/services/auth'

/**
 * Admin sign-in. The underscore is on the `admin_` segment, which excludes
 * this route from the `/admin` layout guard; `admin/login_.tsx` would stay
 * nested under it, and the guard renders nothing without an admin session,
 * so the page that logs you in would never appear.
 */
const ssoStatus = createServerFn({ method: 'GET' }).handler(
  async (): Promise<{ enabled: boolean; providerId: string }> => {
    const { ssoEnabled, SSO_PROVIDER_ID } = await import('@/lib/auth')
    return { enabled: ssoEnabled(), providerId: SSO_PROVIDER_ID }
  },
)

export const Route = createFileRoute('/admin_/login')({
  loader: () => ssoStatus(),
  component: AdminLoginPage,
})

function AdminLoginPage() {
  const navigate = useNavigate()
  const sso = Route.useLoaderData()
  return (
    <AdminLoginForm
      authClient={authClient}
      getProfile={getProfile}
      onSuccess={() => navigate({ to: '/admin' })}
      sso={
        sso.enabled
          ? {
              only: true,
              signIn: async () => {
                await authClient.signIn.social({
                  provider: sso.providerId,
                  callbackURL: '/admin',
                })
              },
            }
          : undefined
      }
    />
  )
}
