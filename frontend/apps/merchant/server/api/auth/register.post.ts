import type { RegisterRequest, Session, Tenant, User } from '@mscs/shared'

// POST /api/auth/register: create the account and first shop, then log in (cookie).
export default defineEventHandler(async (event) => {
  const body = await readBody<RegisterRequest>(event)
  const res = await callApi<{ user: User; tenant: Tenant; session: Session }>(event, '/auth/register', {
    method: 'POST',
    body,
  })

  if (res.status !== 201 || !res.data || !('session' in res.data)) {
    return relay(event, res)
  }
  setSessionCookie(event, res.data.session)
  setResponseStatus(event, 201)
  return { user: res.data.user, tenant: res.data.tenant }
})
