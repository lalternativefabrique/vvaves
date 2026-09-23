import {
  createFileRoute,
  Link,
  Outlet,
  useNavigate,
} from '@tanstack/react-router'
import { useEffect, useState } from 'react'
import { AdminLayout } from '@lalternative/admin'
import { getProfile } from '@/lib/services/auth'
import { hasAdminFeatures } from '@/lib/hooks/useAdminFeaturesEnabled'

/**
 * Back-office shell. `/admin/login` sits outside it, as `admin_.login.tsx`:
 * login must be reachable without a session.
 *
 * The guard lives in the component rather than beforeLoad: that hook cannot
 * reach /api/me during SSR and is not replayed on hydration, so the whole
 * chrome would be served to anyone who typed the URL. Rendered as nothing
 * until the profile says admin; every admin endpoint re-checks the role
 * server-side anyway.
 */
export const Route = createFileRoute('/admin')({
  component: AdminShell,
})

function AdminShell() {
  const navigate = useNavigate()
  const [allowed, setAllowed] = useState(false)

  useEffect(() => {
    getProfile()
      .then((user) => {
        if (hasAdminFeatures(user)) setAllowed(true)
        else void navigate({ to: '/admin/login', replace: true })
      })
      .catch(() => void navigate({ to: '/admin/login', replace: true }))
  }, [navigate])

  if (!allowed) return null

  const linkClass = 'text-muted-foreground hover:text-foreground'
  const activeClass = 'text-foreground'
  return (
    <AdminLayout
      nav={
        <>
          <Link
            to="/admin"
            activeOptions={{ exact: true }}
            className={linkClass}
            activeProps={{ className: activeClass }}
          >
            Tableau de bord
          </Link>
          <Link
            to="/admin/apps"
            className={linkClass}
            activeProps={{ className: activeClass }}
          >
            Applications
          </Link>
        </>
      }
      app={{ name: 'Vvaves', tone: 'blue' }}
    >
      <Outlet />
    </AdminLayout>
  )
}
