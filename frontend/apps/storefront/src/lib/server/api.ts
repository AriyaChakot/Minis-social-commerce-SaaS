import { API_BASE } from '$app/env/private'
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
      headers: init.body === undefined ? { accept: 'application/json' } : { 'content-type': 'application/json' },
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
