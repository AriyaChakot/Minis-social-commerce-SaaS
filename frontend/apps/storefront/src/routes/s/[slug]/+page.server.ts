import { error } from '@sveltejs/kit'
import type { StorefrontProduct } from '@mscs/shared'
import { api } from '#lib/server/api.ts'
import type { PageServerLoad } from './$types'

export const load: PageServerLoad = async ({ params }) => {
  const res = await api<{ items: StorefrontProduct[] }>(`/public/shops/${encodeURIComponent(params.slug)}/products`)
  if (!res.ok) error(res.status === 404 ? 404 : 503, { message: res.status === 404 ? 'ไม่พบร้านค้านี้' : 'ระบบขัดข้องชั่วคราว' })
  return { products: res.data.items }
}
