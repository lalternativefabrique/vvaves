import { createFileRoute } from '@tanstack/react-router'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { voicesApi } from '@/lib/voices-api'

export const Route = createFileRoute('/admin/voices')({ component: VoicesPage })

const voicesKey = ['admin', 'voices']

function VoicesPage() {
  const queryClient = useQueryClient()
  const voices = useQuery({ queryKey: voicesKey, queryFn: voicesApi.list })
  const [language, setLanguage] = useState('fr')
  const [error, setError] = useState<string | null>(null)

  const choose = useMutation({
    mutationFn: voicesApi.choose,
    onSuccess: () => {
      setError(null)
      void queryClient.invalidateQueries({ queryKey: voicesKey })
    },
    onError: (e: Error) => setError(e.message),
  })

  const current = voices.data?.current ?? ''
  const all = voices.data?.voices ?? []
  const languages = [...new Set(all.flatMap((v) => v.languages))].sort()
  const shown = language ? all.filter((v) => v.languages.includes(language)) : all

  return (
    <div className="flex flex-col gap-8">
      <header>
        <h1 className="text-2xl font-semibold">Voix</h1>
        <p className="mt-1 text-sm text-muted-foreground">
          La voix choisie lit toutes les demandes, tout de suite et sans
          redéploiement. Changer de voix fait régénérer l'audio déjà enregistré
          à sa prochaine écoute.
        </p>
      </header>

      {voices.isLoading && <p className="text-sm text-muted-foreground">Chargement…</p>}
      {voices.isError && (
        <p className="text-sm text-destructive">{(voices.error as Error).message}</p>
      )}
      {error && <p className="text-sm text-destructive">{error}</p>}

      {voices.data && (
        <>
          <label className="flex w-48 flex-col gap-1 text-sm">
            <span className="text-muted-foreground">Langue</span>
            <select
              className="rounded-md border border-border bg-background px-3 py-2 text-sm"
              value={language}
              onChange={(e) => setLanguage(e.target.value)}
            >
              <option value="">Toutes</option>
              {languages.map((l) => (
                <option key={l} value={l}>
                  {l}
                </option>
              ))}
            </select>
          </label>

          <ul className="flex flex-col divide-y divide-border rounded-md border border-border">
            {shown.map((v) => (
              <li key={v.id} className="flex items-center justify-between gap-4 px-4 py-3">
                <div className="min-w-0">
                  <p className="font-medium">{v.name}</p>
                  <p className="truncate font-mono text-xs text-muted-foreground">
                    {v.id} · {v.languages.join(', ')}
                    {v.gender ? ` · ${v.gender}` : ''}
                  </p>
                </div>
                {v.id === current ? (
                  <span className="rounded-md bg-primary/10 px-3 py-1.5 text-sm font-medium text-primary">
                    Par défaut
                  </span>
                ) : (
                  <button
                    type="button"
                    disabled={choose.isPending}
                    onClick={() => choose.mutate(v.id)}
                    className="rounded-md border border-border px-3 py-1.5 text-sm hover:bg-muted disabled:opacity-50"
                  >
                    Choisir
                  </button>
                )}
              </li>
            ))}
            {shown.length === 0 && (
              <li className="px-4 py-3 text-sm text-muted-foreground">Aucune voix.</li>
            )}
          </ul>
        </>
      )}
    </div>
  )
}
