import type { LoginRequest, Membership, MeResponse, RegisterRequest, User } from '@mscs/shared'

/**
 * Who is logged in, shared by every component (useState = one value per request on the
 * server, one global value in the browser).
 *
 * The browser never holds the session token: it calls our own /api/* routes and the
 * HttpOnly cookie travels with them automatically (see server/utils/api.ts).
 */
export function useAuth() {
  const user = useState<User | null>('auth:user', () => null)
  const memberships = useState<Membership[]>('auth:memberships', () => [])
  const loaded = useState<boolean>('auth:loaded', () => false)

  // Must be captured synchronously, here. During server-side rendering it forwards the
  // incoming request's cookie to /api/me; plain $fetch would call it without the cookie.
  const requestFetch = useRequestFetch()

  async function refresh() {
    try {
      const me = await requestFetch<MeResponse>('/api/me')
      user.value = me.user
      memberships.value = me.memberships
    }
    catch {
      user.value = null
      memberships.value = []
    }
    loaded.value = true
  }

  async function login(body: LoginRequest) {
    await $fetch('/api/auth/login', { method: 'POST', body })
    await refresh()
  }

  async function register(body: RegisterRequest) {
    await $fetch('/api/auth/register', { method: 'POST', body })
    await refresh()
  }

  async function logout() {
    await $fetch('/api/auth/logout', { method: 'POST' })
    user.value = null
    memberships.value = []
    await navigateTo('/login')
  }

  return { user, memberships, loaded, refresh, login, register, logout }
}
