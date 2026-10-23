import type { InvitationPreview, Session, User } from '@mscs/shared'

// POST /api/invitations/:token/register: create the invited account, join the shop, log in.
// Like /api/auth/register, the session token goes into the HttpOnly cookie, never to the page.
export default defineEventHandler(async (event) => {
  const token = encodeURIComponent(getRouterParam(event, 'token') ?? '')
  const res = await callApi<{ user: User, invitation: InvitationPreview, session: Session }>(event, `/invitations/${token}/register`, {
    method: 'POST',
    body: await readBody(event),
  })

  if (res.status !== 201 || !res.data || !('session' in res.data)) {
    return relay(event, res)
  }
  setSessionCookie(event, res.data.session)
  setResponseStatus(event, 201)
  return { user: res.data.user, invitation: res.data.invitation }
})
