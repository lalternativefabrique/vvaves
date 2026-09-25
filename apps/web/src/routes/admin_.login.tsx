import { createFileRoute, useNavigate } from '@tanstack/react-router'
import { AdminLoginForm } from '@lalternative/admin'
import { authClient } from '@/lib/auth-client'
import { getProfile } from '@/lib/services/auth'

/**
 * Admin sign-in. The underscore is on the `admin_` segment, which excludes
 * this route from the `/admin` layout guard; `admin/login_.tsx` would stay
 * nested under it, and the guard renders nothing without an admin session,
 * so the page that logs you in would never appear.
 */
export const Route = createFileRoute('/admin_/login')({
  component: AdminLoginPage,
})

function AdminLoginPage() {
  const navigate = useNavigate()
  return (
    <AdminLoginForm
      authClient={authClient}
      getProfile={getProfile}
      onSuccess={() => navigate({ to: '/admin' })}
    />
  )
}
