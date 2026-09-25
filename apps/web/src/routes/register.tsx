import { createFileRoute, useNavigate } from '@tanstack/react-router'
import { RegisterForm } from '@lalternative/auth'
import { AuthScreen, authHead } from '@/components/auth-screen'
import { authClient } from '@/lib/auth-client'

export const Route = createFileRoute('/register')({
  head: () => authHead('Créer un compte'),
  component: RegisterPage,
})

function RegisterPage() {
  const navigate = useNavigate()
  return (
    <AuthScreen subtitle="Créer un compte">
      <RegisterForm
        authClient={authClient}
        collectName={false}
        labels={{
          passwordHint: 'Au moins 12 caractères.',
          passwordTooShort: 'Le mot de passe doit faire au moins 12 caractères',
        }}
        onSuccess={(email) =>
          navigate({ to: '/verify-email', search: { email } })
        }
      />
    </AuthScreen>
  )
}
