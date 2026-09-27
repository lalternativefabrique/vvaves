import {
  Link,
  Outlet,
  createFileRoute,
  redirect,
  useNavigate,
} from '@tanstack/react-router'
import { authClient } from '@/lib/auth-client'
import { requireSessionFn } from '@/lib/session'

export const Route = createFileRoute('/app')({
  beforeLoad: async () => {
    const outcome = await requireSessionFn()
    if (outcome.status === 'refused') throw redirect({ to: '/login' })
    return { email: outcome.email, name: outcome.name }
  },
  component: AppShell,
})

function AppShell() {
  const navigate = useNavigate()
  const { email } = Route.useRouteContext()

  const signOut = async () => {
    await authClient.signOut()
    void navigate({ to: '/login', replace: true })
  }

  return (
    <div className="lalt-admin min-h-screen bg-background text-foreground">
      <header className="sticky top-0 z-40 border-b bg-background/95 backdrop-blur supports-[backdrop-filter]:bg-background/75">
        <div className="mx-auto flex h-16 max-w-5xl items-center gap-4 px-6">
          <span className="flex items-center gap-2 text-sm font-semibold tracking-tight">
            <span aria-hidden className="size-2 rounded-full bg-emerald-500" />
            vvaves
          </span>
          <nav className="flex items-center gap-4 text-sm">
            <Link
              to="/app/keys"
              className="text-muted-foreground hover:text-foreground"
              activeProps={{ className: 'text-foreground' }}
            >
              Clés
            </Link>
            <Link
              to="/app/settings"
              className="text-muted-foreground hover:text-foreground"
              activeProps={{ className: 'text-foreground' }}
            >
              Paramètres
            </Link>
          </nav>
          <div className="ml-auto flex items-center gap-3 text-sm">
            <span className="hidden text-muted-foreground sm:inline">
              {email}
            </span>
            <button
              type="button"
              onClick={() => void signOut()}
              className="rounded-md px-2 py-1 text-muted-foreground transition-colors hover:bg-muted hover:text-foreground"
            >
              Se déconnecter
            </button>
          </div>
        </div>
      </header>
      <main className="mx-auto max-w-5xl px-6 py-10">
        <Outlet />
      </main>
    </div>
  )
}
