import { error } from '@sveltejs/kit'
import type { StorefrontShop } from '@mscs/shared'
import { api } from '#lib/server/api.ts'
import type { LayoutServerLoad } from './$types'

// Runs on the server for every page under /s/:slug. Unknown or suspended shops get a 404 here,
// before any child page loads.
export const load: LayoutServerLoad = async ({ params }) => {
  const res = await api<StorefrontShop>(`/public/shops/${encodeURIComponent(params.slug)}`)
  if (!res.ok) {
    if (res.status === 404) error(404, { message: 'ไม่พบร้านค้านี้' })
    error(503, { message: 'ระบบขัดข้องชั่วคราว' })
  }
  return { shop: res.data }
}
