import type { ApiErrorBody, OrderStatus } from '@mscs/shared'
import { formatTHB } from '@mscs/shared'

// Small display helpers shared by the storefront pages.

/** Products have no photos yet: give each one a stable, pleasant color from its name instead. */
const SWATCHES = [
  'from-amber-200 to-orange-300',
  'from-sky-200 to-indigo-300',
  'from-emerald-200 to-teal-300',
  'from-rose-200 to-pink-300',
  'from-lime-200 to-green-300',
  'from-violet-200 to-purple-300',
]

/**
 * The letter shown on a product's tile. Thai leading vowels (เ แ โ ใ ไ) are written before the
 * consonant they belong to, so "เสื้อ" should show "ส", not "เ".
 */
export function initial(name: string): string {
  const trimmed = name.trim().replace(/^[เแโใไ]+/u, '')
  return [...trimmed][0] ?? '?'
}

export function swatch(name: string): string {
  let h = 0
  for (const ch of name) h = (h * 31 + ch.codePointAt(0)!) >>> 0
  return SWATCHES[h % SWATCHES.length]!
}

export const STATUS_LABEL: Record<OrderStatus, string> = {
  pending: 'รอชำระเงิน',
  paid: 'ชำระเงินแล้ว',
  shipped: 'จัดส่งแล้ว',
  completed: 'ได้รับสินค้าแล้ว',
  cancelled: 'ยกเลิกแล้ว',
}

/** Thai text for an API error, naming the product when the error is about one. */
export function orderErrorMessage(error: ApiErrorBody['error'] | undefined, productName: (id: string) => string | undefined): string {
  if (!error) return 'เกิดข้อผิดพลาด กรุณาลองใหม่อีกครั้ง'
  const details = error.details as { product_id?: string; available?: number; total_satang?: number } | undefined
  const name = (details?.product_id && productName(details.product_id)) || 'สินค้าบางรายการ'
  switch (error.code) {
    case 'insufficient_stock':
      return `“${name}” เหลือเพียง ${details?.available ?? 0} ชิ้น กรุณาปรับจำนวน`
    case 'product_unavailable':
      return `“${name}” หยุดขายแล้ว กรุณานำออกจากตะกร้า`
    case 'total_changed':
      return `ราคาหรือค่าส่งเพิ่งเปลี่ยน ยอดรวมใหม่คือ ${formatTHB(details?.total_satang ?? 0)} กรุณาตรวจสอบแล้วกดยืนยันอีกครั้ง`
    case 'validation_failed':
      return 'กรุณากรอกข้อมูลให้ครบถ้วน'
    case 'api_unavailable':
      return 'ระบบขัดข้องชั่วคราว กรุณาลองใหม่อีกครั้ง'
    case 'rate_limited':
      return 'มีการสั่งซื้อถี่เกินไป กรุณารอสักครู่แล้วลองใหม่'
    default:
      return 'เกิดข้อผิดพลาด กรุณาลองใหม่อีกครั้ง'
  }
}
