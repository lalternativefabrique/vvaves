import { createFileRoute, useNavigate } from '@tanstack/react-router'
import { ResetPasswordForm } from '@lalternative/auth'
import { AuthScreen, authHead } from '@/components/auth-screen'
import { authClient } from '@/lib/auth-client'

export const Route = createFileRoute('/reset-password')({
  head: () => authHead('Nouveau mot de passe'),
  validateSearch: (search: Record<string, unknown>): { email: string } => ({
    email: typeof search.email === 'string' ? search.email : '',
  }),
  component: ResetPasswordPage,
})

function ResetPasswordPage() {
  const { email } = Route.useSearch()
  const navigate = useNavigate()
  return (
    <AuthScreen subtitle="Nouveau mot de passe">
      <ResetPasswordForm
        authClient={authClient}
        email={email}
        onSuccess={() => void navigate({ to: '/login' })}
      />
    </AuthScreen>
  )
}
