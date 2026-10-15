// POST /api/auth/logout: end the session in the Go API and delete the cookie.
// The cookie is deleted even if the API call fails, so the browser is logged out either way.
export default defineEventHandler(async (event) => {
  if (getCookie(event, SESSION_COOKIE)) {
    await callApi(event, '/auth/logout', { method: 'POST' })
  }
  clearSessionCookie(event)
  setResponseStatus(event, 204)
  return null
})
