import { Link, Outlet, createFileRoute, redirect } from '@tanstack/react-router'
import { AdminLayout } from '@lalternative/admin'
import { requireAdminFn } from '@/lib/session'

export const Route = createFileRoute('/admin')({
  beforeLoad: async () => {
    const outcome = await requireAdminFn()
    if (outcome.status === 'refused') throw redirect({ to: '/admin/login' })
  },
  component: AdminShell,
})

function AdminShell() {
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
          <Link
            to="/admin/voices"
            className={linkClass}
            activeProps={{ className: activeClass }}
          >
            Voix
          </Link>
        </>
      }
      app={{ name: 'Vvaves', tone: 'blue' }}
    >
      <Outlet />
    </AdminLayout>
  )
}
