import type { ApiErrorBody, FieldError } from '@mscs/shared'

// Helpers to turn a failed $fetch into something the UI can show (auto-imported in app/).

/** Thai text for the error codes the API returns. Unknown codes fall back to the API message. */
const MESSAGES: Record<string, string> = {
  api_unavailable: 'เชื่อมต่อเซิร์ฟเวอร์ไม่ได้ กรุณาลองใหม่อีกครั้ง',
  invalid_credentials: 'อีเมลหรือรหัสผ่านไม่ถูกต้อง',
  unauthenticated: 'กรุณาเข้าสู่ระบบ',
  forbidden: 'คุณไม่มีสิทธิ์ทำรายการนี้',
  email_taken: 'อีเมลนี้ถูกใช้สมัครแล้ว',
  slug_taken: 'ลิงก์ร้านนี้มีร้านอื่นใช้แล้ว',
  validation_failed: 'ข้อมูลบางช่องไม่ถูกต้อง',
  internal_error: 'เกิดข้อผิดพลาดในระบบ กรุณาลองใหม่อีกครั้ง',
  rate_limited: 'ทำรายการถี่เกินไป กรุณารอสักครู่แล้วลองใหม่',
  conversation_not_found: 'ไม่พบแชทนี้',
  tenant_suspended: 'ร้านนี้ถูกระงับโดยผู้ดูแลระบบ',
  replies_not_enabled: 'ยังตอบแชทไม่ได้ ให้เจ้าของร้านใส่ Channel access token ในหน้าตั้งค่าก่อน',
  line_token_rejected: 'LINE ไม่ยอมรับ access token ที่บันทึกไว้ (อาจถูกออกใหม่) ให้บันทึก token ใหม่ในหน้าตั้งค่า',
  line_rate_limited: 'ส่งไม่ได้: ใช้โควตาข้อความของ LINE เดือนนี้หมดแล้ว หรือส่งถี่เกินไป',
  line_unavailable: 'ติดต่อ LINE ไม่ได้ชั่วคราว กรุณาลองส่งอีกครั้ง',
}

/** The API's {error: {...}} body inside a $fetch error, or null for other failures. */
export function apiError(e: unknown): ApiErrorBody['error'] | null {
  const data = (e as { data?: unknown } | null)?.data
  if (data && typeof data === 'object' && 'error' in data) {
    return (data as ApiErrorBody).error
  }
  return null
}

export function apiErrorMessage(e: unknown): string {
  const err = apiError(e)
  if (!err) return 'เกิดข้อผิดพลาด กรุณาลองใหม่อีกครั้ง'
  return MESSAGES[err.code] ?? err.message
}

/** Per-field problems of a "validation_failed" error (empty for any other error). */
export function apiFieldErrors(e: unknown): FieldError[] {
  const err = apiError(e)
  return err?.code === 'validation_failed' && Array.isArray(err.details) ? (err.details as FieldError[]) : []
}
