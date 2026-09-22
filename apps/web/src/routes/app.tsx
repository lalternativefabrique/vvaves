import { createFileRoute, Outlet, useNavigate } from '@tanstack/react-router'
import { useEffect, useState } from 'react'
import { authClient } from '@/lib/auth-client'
import { getProfile } from '@/lib/services/auth'
import type { UserProfile } from '@/lib/types/auth'

// The guard lives in the component, as in the admin shell: beforeLoad cannot
// reach /api/me during SSR and is not replayed on hydration.
export const Route = createFileRoute('/app')({
  component: AppShell,
})

function AppShell() {
  const navigate = useNavigate()
  const [user, setUser] = useState<UserProfile | null>(null)

  useEffect(() => {
    getProfile()
      .then(setUser)
      .catch(() => void navigate({ to: '/login', replace: true }))
  }, [navigate])

  if (!user) return null

  const signOut = async () => {
    await authClient.signOut()
    void navigate({ to: '/login', replace: true })
  }

  return (
    <div className="min-h-screen">
      <header className="border-b border-border">
        <div className="mx-auto flex max-w-4xl items-center justify-between px-6 py-4">
          <span className="font-semibold">vvaves</span>
          <div className="flex items-center gap-4 text-sm">
            <span className="text-muted-foreground">{user.email}</span>
            <button
              type="button"
              onClick={() => void signOut()}
              className="underline hover:text-foreground"
            >
              Se déconnecter
            </button>
          </div>
        </div>
      </header>
      <main className="mx-auto max-w-4xl px-6 py-8">
        <Outlet />
      </main>
    </div>
  )
}
