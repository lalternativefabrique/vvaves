import { AuthLayout } from '@lalternative/auth'
import type { ReactNode } from 'react'
import { AuthPanel } from './auth-panel'

export const AUTH_TITLE_CLASS =
  'text-[2.75rem] font-bold leading-[1] tracking-[-0.04em] sm:text-[3.25rem]'

export function AuthScreen({
  subtitle,
  children,
}: {
  subtitle: string
  children: ReactNode
}) {
  return (
    <AuthLayout
      title="vvaves"
      titleClassName={AUTH_TITLE_CLASS}
      subtitle={subtitle}
      panel={<AuthPanel />}
    >
      {children}
    </AuthLayout>
  )
}

export function authHead(title: string) {
  return {
    meta: [
      { title: `${title} — vvaves` },
      { name: 'robots', content: 'noindex, nofollow' },
    ],
  }
}
