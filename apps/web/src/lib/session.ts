import { createServerFn } from '@tanstack/react-start'
import {
  getRequestHeaders,
  setResponseHeader,
} from '@tanstack/react-start/server'
import { auth } from './auth'
import { guardOutcome } from './guard'

async function guardRequest(admin: boolean) {
  const headers = new Headers(getRequestHeaders())
  const guarded = admin
    ? await auth.requireAdmin(headers)
    : await auth.requireSession(headers)
  if ('session' in guarded && guarded.setCookies.length > 0)
    setResponseHeader('set-cookie', guarded.setCookies)
  return guardOutcome(guarded)
}

export const requireSessionFn = createServerFn().handler(() =>
  guardRequest(false),
)

export const requireAdminFn = createServerFn().handler(() => guardRequest(true))
