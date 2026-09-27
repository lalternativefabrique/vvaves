import { createFileRoute, useNavigate, useRouter } from '@tanstack/react-router'
import { AccountSettings } from '@lalternative/auth'
import { authClient } from '@/lib/auth-client'

export const Route = createFileRoute('/app/settings')({
  component: SettingsPage,
})

function SettingsPage() {
  const { email, name } = Route.useRouteContext()
  const router = useRouter()
  const navigate = useNavigate()
  return (
    <AccountSettings
      client={authClient}
      user={{ name, email }}
      setPasswordHref="/forgot-password"
      onUpdated={() => router.invalidate()}
      deleteAccount={{
        onDeleted: () => navigate({ to: '/login', replace: true }),
        labels: {
          warning:
            'La suppression est définitive : tes clés sont révoquées, les applications qui les présentent cessent aussitôt de parler par vvaves, et ton compte vvaves est fermé.',
          steps: {
            billing: 'Résiliation de ton abonnement',
            data: 'Révocation de tes clés',
          },
        },
      }}
    />
  )
}
