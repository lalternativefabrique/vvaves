import { createFileRoute } from '@tanstack/react-router'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { voicesApi } from '@/lib/voices-api'
import type { CatalogueVoice } from '@/lib/voices-api'

export const Route = createFileRoute('/app/voices')({ component: VoicesPage })

const chosenKey = ['app', 'voices', 'chosen']

function VoicesPage() {
  const queryClient = useQueryClient()
  const catalogue = useQuery({
    queryKey: ['app', 'voices', 'catalogue'],
    queryFn: voicesApi.catalogue,
    staleTime: 5 * 60_000,
  })
  const keys = useQuery({ queryKey: ['app', 'keys'], queryFn: voicesApi.keys })
  const chosen = useQuery({ queryKey: chosenKey, queryFn: voicesApi.chosen })
  const [language, setLanguage] = useState('fr')
  const [error, setError] = useState<string | null>(null)

  const save = useMutation({
    mutationFn: ({ keyId, voiceId }: { keyId: string; voiceId: string }) =>
      voiceId ? voicesApi.choose(keyId, voiceId) : voicesApi.clear(keyId),
    onSuccess: () => {
      setError(null)
      void queryClient.invalidateQueries({ queryKey: chosenKey })
    },
    onError: (e: Error) => setError(e.message),
  })

  const all = catalogue.data?.voices ?? []
  const languages = [...new Set(all.flatMap((v) => v.languages))].sort()
  const shown = language
    ? all.filter((v) => v.languages.includes(language))
    : all
  const failure = [catalogue, keys, chosen].find((q) => q.isError)?.error
  const loading = catalogue.isLoading || keys.isLoading || chosen.isLoading

  return (
    <div className="flex flex-col gap-8">
      <header>
        <h1 className="text-2xl font-semibold">Voix</h1>
        <p className="mt-1 text-sm text-muted-foreground">
          La voix qui lit les demandes de chaque application. Le changement
          s'applique en moins d'une minute, sans toucher à l'application.
        </p>
      </header>

      {loading && <p className="text-sm text-muted-foreground">Chargement…</p>}
      {failure && <p className="text-sm text-destructive">{failure.message}</p>}
      {error && <p className="text-sm text-destructive">{error}</p>}

      {catalogue.data && keys.data && chosen.data && (
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
            {keys.data.keys.map((k) => (
              <KeyVoiceRow
                key={k.id}
                label={k.label}
                keyId={k.id}
                current={chosen.data.voices[k.id] ?? ''}
                voices={shown}
                all={all}
                disabled={save.isPending}
                onChange={(voiceId) => save.mutate({ keyId: k.id, voiceId })}
              />
            ))}
            {keys.data.keys.length === 0 && (
              <li className="px-4 py-3 text-sm text-muted-foreground">
                Aucune clé. Crée-en une dans l'onglet Clés.
              </li>
            )}
          </ul>
        </>
      )}
    </div>
  )
}

function KeyVoiceRow(props: {
  label: string
  keyId: string
  current: string
  voices: CatalogueVoice[]
  all: CatalogueVoice[]
  disabled: boolean
  onChange: (voiceId: string) => void
}) {
  const currentVoice = props.all.find((v) => v.id === props.current)
  const options =
    currentVoice && !props.voices.includes(currentVoice)
      ? [currentVoice, ...props.voices]
      : props.voices
  return (
    <li className="flex flex-col gap-3 px-4 py-3 sm:flex-row sm:items-center sm:justify-between">
      <div className="min-w-0">
        <p className="font-medium">{props.label}</p>
        <p className="truncate font-mono text-xs text-muted-foreground">
          {props.keyId}
        </p>
      </div>
      <select
        className="w-full rounded-md border border-border bg-background px-3 py-2 text-sm sm:w-72"
        value={props.current}
        disabled={props.disabled}
        onChange={(e) => props.onChange(e.target.value)}
      >
        <option value="">Voix par défaut</option>
        {options.map((v) => (
          <option key={v.id} value={v.id}>
            {v.name} · {v.languages.join(', ')}
            {v.gender ? ` · ${v.gender}` : ''}
          </option>
        ))}
      </select>
    </li>
  )
}
