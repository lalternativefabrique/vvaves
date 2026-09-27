import { createFileRoute, useNavigate } from '@tanstack/react-router'
import { EmailCodeSignInForm } from '@lalternative/auth'
import { AuthScreen, authHead } from '@/components/auth-screen'
import { authClient } from '@/lib/auth-client'

export const Route = createFileRoute('/login_/code')({
  head: () => authHead('Code de connexion'),
  component: CodeSignInPage,
})

function CodeSignInPage() {
  const navigate = useNavigate()
  return (
    <AuthScreen subtitle="Recevoir un code par e-mail">
      <EmailCodeSignInForm
        authClient={authClient}
        coreTokenUrl={null}
        onSuccess={() => void navigate({ to: '/app/keys' })}
      />
    </AuthScreen>
  )
}
