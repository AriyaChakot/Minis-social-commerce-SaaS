import { error } from '@sveltejs/kit'
import { API_BASE } from '$app/env/private'
import { getRequestEvent } from '$app/server'
import type { ApiErrorBody } from '@mscs/shared'

// Server-only module (src/lib/server/*: SvelteKit refuses to bundle it into browser code).
// The storefront calls the Go API from its own server (load functions and form actions),
// so the browser never talks to the Go API directly: no CORS, and the API URL stays private.

type ApiError = ApiErrorBody['error']
export type ApiResult<T> = { ok: true; status: number; data: T } | { ok: false; status: number; error: ApiError }

export async function api<T>(path: string, init: { method?: string; body?: unknown } = {}): Promise<ApiResult<T>> {
  let res: Response
  try {
    res = await fetch(`${API_BASE}${path}`, {
      method: init.method ?? 'GET',
      headers: {
        ...(init.body === undefined ? { accept: 'application/json' } : { 'content-type': 'application/json' }),
        // The Go API sees this server as the caller; tell it who the customer really is, so its
        // rate limits count per customer instead of lumping everyone together. Set, never
        // passed through: a browser cannot inject its own value.
        'x-forwarded-for': getRequestEvent().getClientAddress(),
      },
      body: init.body === undefined ? undefined : JSON.stringify(init.body),
    })
  }
  catch (err) {
    console.error('[api] cannot reach Go API:', err)
    return { ok: false, status: 502, error: { code: 'api_unavailable', message: 'the API server is not reachable' } }
  }

  const body = res.status === 204 ? null : await res.json().catch(() => null)
  if (res.ok) return { ok: true, status: res.status, data: body as T }

  const error = (body as ApiErrorBody | null)?.error ?? { code: 'unknown', message: res.statusText }
  return { ok: false, status: res.status, error }
}

/** Ends a load function whose API call failed, with the right error page. */
export function loadFailed(res: { status: number }, notFoundMessage = 'ไม่พบร้านค้านี้'): never {
  if (res.status === 404) error(404, { message: notFoundMessage })
  if (res.status === 429) error(429, { message: 'มีการเข้าชมถี่เกินไป กรุณารอสักครู่แล้วลองใหม่' })
  error(503, { message: 'ระบบขัดข้องชั่วคราว' })
}
