import type { Satang, ShippingRates } from './api'

const thb = new Intl.NumberFormat('th-TH', { style: 'currency', currency: 'THB' })

/** 25900 -> "฿259.00" */
export function formatTHB(amount: Satang): string {
  return thb.format(amount / 100)
}

/**
 * Converts a baht amount typed by a person ("259", "259.5", "1,299.00") to satang.
 * Returns null when the text is not a valid non-negative amount with at most 2 decimals.
 * Works on the digits as text, so no floating-point rounding can sneak in.
 */
export function parseBahtToSatang(input: string): Satang | null {
  const text = input.replaceAll(',', '').trim()
  const match = /^(\d+)(?:\.(\d{1,2}))?$/.exec(text)
  if (!match) return null
  const baht = Number(match[1])
  const satang = Number((match[2] ?? '').padEnd(2, '0'))
  return baht * 100 + satang
}

/** 25900 -> "259.00" (for pre-filling an input field). */
export function satangToBahtInput(amount: Satang): string {
  return (amount / 100).toFixed(2)
}

/**
 * The shipping fee of an order with this subtotal. Same rule as the Go API
 * (tenant.ShippingRates.FeeFor): shown in carts and previews, but only the API's result is charged.
 */
export function shippingFeeFor(rates: ShippingRates, subtotal: Satang): Satang {
  return rates.free_from_satang !== null && subtotal >= rates.free_from_satang ? 0 : rates.fee_satang
}

/** One line for customers and merchants: "ค่าส่ง ฿40 · ส่งฟรีเมื่อซื้อครบ ฿600". */
export function describeShipping(rates: ShippingRates): string {
  if (rates.fee_satang === 0) return 'ส่งฟรีทุกออเดอร์'
  const fee = `ค่าส่ง ${formatTHB(rates.fee_satang)}`
  return rates.free_from_satang === null ? `${fee} ต่อออเดอร์` : `${fee} · ส่งฟรีเมื่อซื้อครบ ${formatTHB(rates.free_from_satang)}`
}
