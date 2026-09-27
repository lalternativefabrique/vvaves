import { createFileRoute } from '@tanstack/react-router'

const CONSOLE_URL = 'https://id.urbangate.dev/admin/users'

export const Route = createFileRoute('/admin/')({
  component: () => (
    <section className="space-y-3">
      <h1 className="text-xl font-semibold">Tableau de bord</h1>
      <p className="text-sm text-muted-foreground">
        Les personnes et leurs droits se gèrent dans la console de
        L&apos;Alternative, pour tous les produits à la fois.
      </p>
      <a
        href={CONSOLE_URL}
        className="inline-block rounded-md bg-primary px-4 py-2 text-sm font-medium text-primary-foreground"
      >
        Ouvrir la console des personnes
      </a>
    </section>
  ),
})
