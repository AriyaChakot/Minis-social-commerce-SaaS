import type { LoginRequest, Session, User } from '@mscs/shared'

// POST /api/auth/login: log in through the Go API, keep the token in an HttpOnly cookie,
// and return only the user to the browser.
export default defineEventHandler(async (event) => {
  const body = await readBody<LoginRequest>(event)
  const res = await callApi<{ user: User; session: Session }>(event, '/auth/login', { method: 'POST', body })

  if (res.status !== 200 || !res.data || !('session' in res.data)) {
    return relay(event, res)
  }
  setSessionCookie(event, res.data.session)
  return { user: res.data.user }
})
