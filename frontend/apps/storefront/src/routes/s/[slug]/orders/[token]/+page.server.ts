import { error } from '@sveltejs/kit'
import type { StorefrontOrder } from '@mscs/shared'
import { api } from '#lib/server/api.ts'
import type { PageServerLoad } from './$types'

// Whoever has this link (with its secret token) can see the order: it is the customer's receipt.
export const load: PageServerLoad = async ({ params, setHeaders }) => {
  const res = await api<StorefrontOrder>(
    `/public/shops/${encodeURIComponent(params.slug)}/orders/${encodeURIComponent(params.token)}`,
  )
  if (!res.ok) {
    if (res.status === 404) error(404, { message: 'ไม่พบคำสั่งซื้อนี้' })
    error(503, { message: 'ระบบขัดข้องชั่วคราว' })
  }
  // A secret URL must not be stored by shared caches (CDN, proxies).
  setHeaders({ 'cache-control': 'private, no-store' })
  return { order: res.data }
}
