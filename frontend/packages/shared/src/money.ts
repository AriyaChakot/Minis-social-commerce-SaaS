import type { Satang } from './api'

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
