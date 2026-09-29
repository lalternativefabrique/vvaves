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

// The membership is erased last: urbangate ADR 0013. 403 is a member already
// erased, which a retried deletion reaches.
export async function eraseMembership(
  coreUrl: string,
  accessToken: string,
  fetchCore: FetchCore = fetch,
): Promise<void> {
  const base = coreUrl.replace(/\/$/, '')
  const res = await fetchCore(`${base}/api/v1/me`, {
    method: 'DELETE',
    headers: { Authorization: `Bearer ${accessToken}` },
  })
  if (!res.ok && res.status !== 403)
    throw new Error(`erasing the membership answered ${res.status}`)
}
