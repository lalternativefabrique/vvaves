import { Link, createFileRoute, useNavigate } from '@tanstack/react-router'
import { AuthField, AuthSubmit } from '@lalternative/auth'
import { useState } from 'react'
import type { FormEvent } from 'react'
import { AuthScreen, authHead } from '@/components/auth-screen'
import { authClient } from '@/lib/auth-client'

export const Route = createFileRoute('/login_/code')({
  head: () => authHead('Code de connexion'),
  component: CodeSignInPage,
})

type Step = 'email' | 'code'

function CodeSignInPage() {
  const navigate = useNavigate()
  const [step, setStep] = useState<Step>('email')
  const [email, setEmail] = useState('')
  const [code, setCode] = useState('')
  const [pending, setPending] = useState(false)
  const [error, setError] = useState<string | undefined>()

  const sendCode = async () => {
    const { error: failed } = await authClient.emailOtp.sendVerificationOtp({
      email: email.trim(),
      type: 'sign-in',
    })
    if (failed) {
      setError("Ce code n'a pas pu être envoyé. Vérifie l'adresse.")
      return
    }
    setStep('code')
  }

  const verifyCode = async () => {
    const { error: failed } = await authClient.signIn.emailOtp({
      email: email.trim(),
      otp: code.trim(),
    })
    if (failed) {
      setError('Code incorrect ou expiré.')
      return
    }
    await navigate({ to: '/app/keys' })
  }

  const handleSubmit = async (e: FormEvent) => {
    e.preventDefault()
    setError(undefined)
    setPending(true)
    try {
      await (step === 'email' ? sendCode() : verifyCode())
    } finally {
      setPending(false)
    }
  }

  return (
    <AuthScreen
      subtitle={
        step === 'email' ? 'Recevoir un code par e-mail' : 'Entre le code reçu'
      }
    >
      <div className="space-y-7">
        {error && (
          <p role="alert" className="text-sm text-destructive">
            {error}
          </p>
        )}
        <form onSubmit={handleSubmit} className="space-y-[1.125rem]" noValidate>
          <AuthField
            label="Adresse e-mail"
            type="email"
            inputMode="email"
            value={email}
            onChange={(e) => setEmail(e.target.value)}
            required
            disabled={pending || step === 'code'}
            autoComplete="email"
            autoCapitalize="none"
            spellCheck={false}
            invalid={!!error}
          />
          {step === 'code' && (
            <AuthField
              label="Code reçu par e-mail"
              type="text"
              inputMode="numeric"
              value={code}
              onChange={(e) => setCode(e.target.value)}
              required
              autoFocus
              disabled={pending}
              autoComplete="one-time-code"
              invalid={!!error}
            />
          )}
          <AuthSubmit
            spacedAbove
            pending={pending}
            disabled={step === 'email' ? !email.trim() : !code.trim()}
            pendingLabel="Un instant…"
          >
            {step === 'email' ? 'Recevoir un code' : 'Se connecter'}
          </AuthSubmit>
        </form>
        <p className="text-center text-sm text-muted-foreground">
          {step === 'code' ? (
            <button
              type="button"
              onClick={() => {
                setStep('email')
                setCode('')
                setError(undefined)
              }}
              className="underline underline-offset-4 hover:text-foreground"
            >
              Changer d'adresse
            </button>
          ) : (
            <Link
              to="/login"
              className="underline underline-offset-4 hover:text-foreground"
            >
              Se connecter avec un mot de passe
            </Link>
          )}
        </p>
      </div>
    </AuthScreen>
  )
}
