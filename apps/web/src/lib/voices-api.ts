export interface CatalogueVoice {
  id: string
  name: string
  languages: string[]
  gender?: string
}

export interface AppKey {
  id: string
  label: string
}

async function call<T>(path: string, init?: RequestInit): Promise<T> {
  const res = await fetch(path, {
    ...init,
    credentials: 'include',
    headers: { 'Content-Type': 'application/json' },
  })
  if (res.status === 204) return undefined as T
  const body = (await res.json().catch(() => ({}))) as T & { error?: string }
  if (!res.ok) throw new Error(body.error ?? `vvaves answered ${res.status}`)
  return body
}

export const voicesApi = {
  catalogue: () =>
    call<{ voices: CatalogueVoice[] }>('/api/v1/voices/catalogue'),
  keys: () => call<{ keys: AppKey[] }>('/api/keys'),
  chosen: () => call<{ voices: Record<string, string> }>('/api/v1/voices/keys'),
  choose: (keyId: string, voiceId: string) =>
    call<unknown>(`/api/v1/voices/keys/${encodeURIComponent(keyId)}`, {
      method: 'PUT',
      body: JSON.stringify({ voice_id: voiceId }),
    }),
  clear: (keyId: string) =>
    call<unknown>(`/api/v1/voices/keys/${encodeURIComponent(keyId)}`, {
      method: 'DELETE',
    }),
}
