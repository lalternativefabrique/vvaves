type FetchCore = (input: string | URL, init?: RequestInit) => Promise<Response>

// A person's keys are all vvaves holds for them. They are signed by urbangate
// and outlive the vvaves:user role, so they are revoked before it is dropped.
export async function revokeEveryKey(
  coreUrl: string,
  accessToken: string,
  fetchCore: FetchCore = fetch,
): Promise<void> {
  const base = coreUrl.replace(/\/$/, '')
  const headers = { Authorization: `Bearer ${accessToken}` }

  const listed = await fetchCore(`${base}/api/keys`, { headers })
  if (listed.status === 503) {
    const body = (await listed.json().catch(() => ({}))) as { error?: string }
    if (body.error === 'no_credential') return
  }
  if (!listed.ok) throw new Error(`listing the keys answered ${listed.status}`)
  const { keys } = (await listed.json()) as { keys: Array<{ id: string }> }

  for (const { id } of keys) {
    const revoked = await fetchCore(
      `${base}/api/keys/${encodeURIComponent(id)}`,
      { method: 'DELETE', headers },
    )
    if (!revoked.ok)
      throw new Error(`revoking key ${id} answered ${revoked.status}`)
  }
}
