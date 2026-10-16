import type { StorefrontOrder } from '@mscs/shared'
import { api, loadFailed } from '#lib/server/api.ts'
import type { PageServerLoad } from './$types'

// Whoever has this link (with its secret token) can see the order: it is the customer's receipt.
export const load: PageServerLoad = async ({ params, setHeaders }) => {
  const res = await api<StorefrontOrder>(
    `/public/shops/${encodeURIComponent(params.slug)}/orders/${encodeURIComponent(params.token)}`,
  )
  if (!res.ok) loadFailed(res, 'ไม่พบคำสั่งซื้อนี้')
  // A secret URL must not be stored by shared caches (CDN, proxies).
  setHeaders({ 'cache-control': 'private, no-store' })
  return { order: res.data }
}
