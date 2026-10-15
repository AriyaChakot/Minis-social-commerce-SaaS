// CSRF defense for cookie-based auth.
//
// Because the browser sends our cookie automatically, a malicious site could try to make a
// visitor's browser POST to /api/... (Cross-Site Request Forgery). SameSite=Lax already blocks
// most of that; this check adds a second layer: a state-changing request must come from our
// own origin.
const SAFE_METHODS = new Set(['GET', 'HEAD', 'OPTIONS'])

export default defineEventHandler((event) => {
  if (SAFE_METHODS.has(event.method) || !event.path.startsWith('/api/')) return

  // Modern browsers label every request: same-origin, same-site, cross-site or none (typed URL).
  const fetchSite = getRequestHeader(event, 'sec-fetch-site')
  if (fetchSite === 'cross-site' || fetchSite === 'same-site') {
    throw createError({ statusCode: 403, statusMessage: 'cross-site request blocked' })
  }

  // Fallback for clients without Sec-Fetch-Site: the Origin header must match our host.
  const origin = getRequestHeader(event, 'origin')
  if (origin && new URL(origin).host !== getRequestHost(event)) {
    throw createError({ statusCode: 403, statusMessage: 'cross-origin request blocked' })
  }
})
