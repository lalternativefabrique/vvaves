import { createFileRoute, useNavigate } from '@tanstack/react-router'
import { createServerFn } from '@tanstack/react-start'
import { useEffect, useState } from 'react'
import { authClient } from '@/lib/auth-client'
import { getProfile } from '@/lib/services/auth'

// Outside the /app layout: its guard renders nothing without a session.
// betaMode only gates /sign-up/email; SSO and the email code walk past it.
const ssoStatus = createServerFn({ method: 'GET' }).handler(
  async (): Promise<{ enabled: boolean; providerId: string }> => {
    const { ssoEnabled, SSO_PROVIDER_ID } = await import('@/lib/auth')
    return { enabled: ssoEnabled(), providerId: SSO_PROVIDER_ID }
  },
)

export const Route = createFileRoute('/login')({
  loader: () => ssoStatus(),
  component: LoginPage,
})

type Step = 'email' | 'code'

function LoginPage() {
  const navigate = useNavigate()
  const sso = Route.useLoaderData()
  const [step, setStep] = useState<Step>('email')
  const [email, setEmail] = useState('')
  const [code, setCode] = useState('')
  const [pending, setPending] = useState(false)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    getProfile()
      .then(() => void navigate({ to: '/app/keys', replace: true }))
      .catch(() => undefined)
  }, [navigate])

  const sendCode = async () => {
    setPending(true)
    setError(null)
    const { error: failed } = await authClient.emailOtp.sendVerificationOtp({
      email,
      type: 'sign-in',
    })
    setPending(false)
    if (failed) {
      setError("Ce code n'a pas pu être envoyé.")
      return
    }
    setStep('code')
  }

  const verifyCode = async () => {
    setPending(true)
    setError(null)
    const { error: failed } = await authClient.signIn.emailOtp({
      email,
      otp: code,
    })
    setPending(false)
    if (failed) {
      setError('Code incorrect ou expiré.')
      return
    }
    void navigate({ to: '/app/keys' })
  }

  return (
    <main className="mx-auto flex min-h-screen max-w-sm flex-col justify-center gap-6 px-6">
      <header>
        <h1 className="text-2xl font-semibold">Connexion</h1>
        <p className="mt-1 text-sm text-muted-foreground">
          Pour gérer les clés de tes applications.
        </p>
      </header>

      {sso.enabled && (
        <>
          <button
            type="button"
            onClick={() =>
              void authClient.signIn.social({
                provider: sso.providerId,
                callbackURL: '/app/keys',
              })
            }
            className="rounded-md bg-primary px-4 py-2 text-sm font-medium text-primary-foreground"
          >
            Continuer avec L'Alternative
          </button>
          <div className="flex items-center gap-3 text-xs text-muted-foreground">
            <span className="h-px flex-1 bg-border" />
            ou
            <span className="h-px flex-1 bg-border" />
          </div>
        </>
      )}

      <form
        className="flex flex-col gap-3"
        onSubmit={(e) => {
          e.preventDefault()
          void (step === 'email' ? sendCode() : verifyCode())
        }}
      >
        <label className="flex flex-col gap-1 text-sm">
          <span className="text-muted-foreground">Adresse e-mail</span>
          <input
            type="email"
            autoComplete="email"
            required
            readOnly={step === 'code'}
            value={email}
            onChange={(e) => setEmail(e.target.value)}
            className="rounded-md border border-border bg-background px-3 py-2 text-sm read-only:text-muted-foreground"
          />
        </label>

        {step === 'code' && (
          <label className="flex flex-col gap-1 text-sm">
            <span className="text-muted-foreground">Code reçu par e-mail</span>
            <input
              inputMode="numeric"
              autoComplete="one-time-code"
              required
              autoFocus
              value={code}
              onChange={(e) => setCode(e.target.value)}
              className="rounded-md border border-border bg-background px-3 py-2 font-mono text-sm tracking-widest"
            />
          </label>
        )}

        {error && <p className="text-sm text-destructive">{error}</p>}

        <button
          type="submit"
          disabled={pending}
          className="rounded-md bg-primary px-4 py-2 text-sm font-medium text-primary-foreground disabled:opacity-50"
        >
          {pending
            ? 'Un instant…'
            : step === 'email'
              ? 'Recevoir un code'
              : 'Se connecter'}
        </button>

        {step === 'code' && (
          <button
            type="button"
            onClick={() => {
              setStep('email')
              setCode('')
              setError(null)
            }}
            className="text-xs text-muted-foreground underline"
          >
            Changer d'adresse
          </button>
        )}
      </form>
    </main>
  )
}
