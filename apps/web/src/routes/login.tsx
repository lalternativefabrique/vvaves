import { Link, createFileRoute, useNavigate } from '@tanstack/react-router'
import { LoginForm } from '@lalternative/auth'
import { AuthScreen, authHead } from '@/components/auth-screen'
import { authClient } from '@/lib/auth-client'

export const Route = createFileRoute('/login')({
  head: () => authHead('Connexion'),
  component: LoginPage,
})

function LoginPage() {
  const navigate = useNavigate()
  return (
    <AuthScreen subtitle="Se connecter">
      <LoginForm
        authClient={authClient}
        coreTokenUrl={null}
        onSuccess={() => void navigate({ to: '/app/keys' })}
      />
      <p className="mt-4 text-center text-sm text-muted-foreground">
        <Link
          to="/login/code"
          className="underline underline-offset-4 hover:text-foreground"
        >
          Recevoir un code de connexion par e-mail
        </Link>
      </p>
    </AuthScreen>
  )
}
