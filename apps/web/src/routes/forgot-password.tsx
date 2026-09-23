import { createFileRoute, useNavigate } from '@tanstack/react-router'
import { ForgotPasswordForm } from '@lalternative/auth'
import { AuthScreen, authHead } from '@/components/auth-screen'
import { authClient } from '@/lib/auth-client'

export const Route = createFileRoute('/forgot-password')({
  head: () => authHead('Mot de passe oublié'),
  component: ForgotPasswordPage,
})

function ForgotPasswordPage() {
  const navigate = useNavigate()
  return (
    <AuthScreen subtitle="Réinitialiser ton mot de passe">
      <ForgotPasswordForm
        authClient={authClient}
        onSuccess={(email) =>
          void navigate({ to: '/reset-password', search: { email } })
        }
      />
    </AuthScreen>
  )
}
