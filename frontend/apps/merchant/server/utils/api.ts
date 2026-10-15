import type { H3Event } from 'h3'
import type { ApiErrorBody } from '@mscs/shared'

// Server-side helpers (auto-imported in server/ by Nitro).
//
// BFF ("backend for frontend") pattern: the browser never sees the session token.
// It lives in an HttpOnly cookie that JavaScript cannot read; this Nuxt server reads it
// and forwards it to the Go API as "Authorization: Bearer <token>".

export const SESSION_COOKIE = 'mscs_session'

export function setSessionCookie(event: H3Event, session: { token: string; expires_at: string }) {
  setCookie(event, SESSION_COOKIE, session.token, {
    httpOnly: true, // not readable by document.cookie, so an XSS bug cannot steal it
    secure: !import.meta.dev, // HTTPS only in production
    sameSite: 'lax', // not sent on cross-site POSTs (first CSRF defense; see middleware/csrf.ts)
    path: '/',
    expires: new Date(session.expires_at),
  })
}

export function clearSessionCookie(event: H3Event) {
  deleteCookie(event, SESSION_COOKIE, { path: '/' })
}

interface ApiCallOptions {
  method?: string
  body?: unknown
  query?: Record<string, unknown>
  contentType?: string
}

export interface ApiResult<T> {
  status: number
  data: T | ApiErrorBody | undefined
}

/**
 * Calls the Go API with the session token from the cookie (if any).
 * Never throws on 4xx/5xx: the caller passes the status and body through to the browser.
 * Only a network failure (Go API down) becomes a 502.
 */
export async function callApi<T>(event: H3Event, path: string, opts: ApiCallOptions = {}): Promise<ApiResult<T>> {
  const { apiBase } = useRuntimeConfig(event)
  const token = getCookie(event, SESSION_COOKIE)

  const headers: Record<string, string> = { accept: 'application/json' }
  if (token) headers.authorization = `Bearer ${token}`
  if (opts.body !== undefined) headers['content-type'] = opts.contentType ?? 'application/json'

  try {
    const res = await $fetch.raw<T>(`${apiBase}${path}`, {
      method: (opts.method ?? 'GET') as 'GET',
      body: opts.body as BodyInit | Record<string, unknown> | undefined,
      query: opts.query,
      headers,
      ignoreResponseError: true,
    })
    // A 401 means the session is gone (expired or logged out elsewhere): drop the stale cookie.
    if (res.status === 401) clearSessionCookie(event)
    return { status: res.status, data: res._data }
  }
  catch (err) {
    console.error('[api] cannot reach Go API:', err)
    return {
      status: 502,
      data: { error: { code: 'api_unavailable', message: 'the API server is not reachable' } },
    }
  }
}

/** Sends an API result to the browser unchanged (status + JSON body). */
export function relay<T>(event: H3Event, result: ApiResult<T>) {
  setResponseStatus(event, result.status)
  return result.status === 204 ? null : result.data
}
