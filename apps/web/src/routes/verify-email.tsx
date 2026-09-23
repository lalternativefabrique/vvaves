import { createFileRoute, useNavigate } from '@tanstack/react-router'
import { VerifyEmailForm } from '@lalternative/auth'
import { AuthScreen, authHead } from '@/components/auth-screen'
import { authClient } from '@/lib/auth-client'

export const Route = createFileRoute('/verify-email')({
  head: () => authHead("Vérification de l'adresse"),
  validateSearch: (search: Record<string, unknown>): { email: string } => ({
    email: typeof search.email === 'string' ? search.email : '',
  }),
  component: VerifyEmailPage,
})

function VerifyEmailPage() {
  const { email } = Route.useSearch()
  const navigate = useNavigate()
  return (
    <AuthScreen subtitle={VerifyEmailForm.defaults.subtitle}>
      <VerifyEmailForm
        authClient={authClient}
        email={email}
        onSuccess={() => void navigate({ to: '/app/keys' })}
      />
    </AuthScreen>
  )
}
