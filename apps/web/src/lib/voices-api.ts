export interface CatalogueVoice {
  id: string
  name: string
  languages: string[]
  gender?: string
}

async function call<T>(init?: RequestInit): Promise<T> {
  const res = await fetch('/api/v1/admin/voices', {
    ...init,
    credentials: 'include',
    headers: { 'Content-Type': 'application/json' },
  })
  const body = (await res.json().catch(() => ({}))) as T & { error?: string }
  if (!res.ok) throw new Error(body.error ?? `vvaves answered ${res.status}`)
  return body
}

export const voicesApi = {
  list: () => call<{ current: string; voices: CatalogueVoice[] }>(),
  choose: (voiceId: string) =>
    call<{ current: string }>({
      method: 'PUT',
      body: JSON.stringify({ voice_id: voiceId }),
    }),
}
