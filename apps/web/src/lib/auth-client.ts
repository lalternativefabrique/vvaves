import { createUrbangateAuthClient } from '@lalternative/auth/urbangate-client'
import { appUrl } from './app-url'

const baseURL =
  typeof window === 'undefined' ? appUrl() : window.location.origin

export const authClient = createUrbangateAuthClient({ baseURL })
