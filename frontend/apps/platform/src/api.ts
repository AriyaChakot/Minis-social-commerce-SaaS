// The console talks to /api/v1/platform only. The session is an HttpOnly cookie set by the Go
// API: this code never sees the token, the browser attaches it on its own (same origin).

export class ApiError extends Error {
  constructor(
    readonly status: number,
    readonly code: string,
    message: string,
    readonly details?: unknown,
  ) {
    super(message)
  }
}

export async function api<T>(path: string, init: { method?: string, body?: unknown } = {}): Promise<T> {
  let res: Response
  try {
    res = await fetch(`/api/v1/platform${path}`, {
      method: init.method ?? 'GET',
      credentials: 'same-origin',
      headers: {
        // Required by the API on every state-changing request (CSRF defense: another site
        // cannot add a custom header without a CORS preflight, which the API never approves).
        'X-MSCS-Platform': '1',
        ...(init.body !== undefined ? { 'Content-Type': 'application/json' } : {}),
      },
      body: init.body !== undefined ? JSON.stringify(init.body) : undefined,
    })
  }
  catch {
    throw new ApiError(0, 'api_unavailable', 'network error')
  }

  if (res.status === 204) return undefined as T
  const data = await res.json().catch(() => null) as { error?: { code: string, message: string, details?: unknown } } | null
  if (!res.ok) {
    // No JSON error body: the API is down and the dev proxy answered for it.
    const err = data?.error
    throw new ApiError(res.status, err?.code ?? 'api_unavailable', err?.message ?? res.statusText, err?.details)
  }
  return data as T
}

/** Thai text for the error codes the console can meet. */
const MESSAGES: Record<string, string> = {
  api_unavailable: 'เชื่อมต่อเซิร์ฟเวอร์ไม่ได้ กรุณาลองใหม่อีกครั้ง',
  invalid_credentials: 'อีเมลหรือรหัสผ่านไม่ถูกต้อง',
  not_platform_admin: 'บัญชีนี้ไม่มีสิทธิ์เข้าใช้งานระบบผู้ดูแลแพลตฟอร์ม',
  rate_limited: 'ลองเข้าสู่ระบบถี่เกินไป กรุณารอสักครู่',
  unauthenticated: 'กรุณาเข้าสู่ระบบ',
  forbidden: 'บัญชีนี้ไม่มีสิทธิ์ผู้ดูแลแพลตฟอร์มแล้ว',
  validation_failed: 'ข้อมูลไม่ถูกต้อง',
  already_suspended: 'ร้านนี้ถูกระงับอยู่แล้ว (อาจมีผู้ดูแลคนอื่นทำไปก่อน)',
  not_suspended: 'ร้านนี้เปิดใช้งานอยู่แล้ว (อาจมีผู้ดูแลคนอื่นทำไปก่อน)',
  tenant_not_found: 'ไม่พบร้านนี้',
  internal_error: 'เกิดข้อผิดพลาดในระบบ กรุณาลองใหม่อีกครั้ง',
}

export function errorMessage(e: unknown): string {
  if (e instanceof ApiError) return MESSAGES[e.code] ?? e.message
  return 'เกิดข้อผิดพลาด กรุณาลองใหม่อีกครั้ง'
}
