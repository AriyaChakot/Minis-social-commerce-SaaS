import type { StorefrontProduct } from '@mscs/shared'
import { api, loadFailed } from '#lib/server/api.ts'
import type { PageServerLoad } from './$types'

export const load: PageServerLoad = async ({ params }) => {
  const res = await api<{ items: StorefrontProduct[] }>(`/public/shops/${encodeURIComponent(params.slug)}/products`)
  if (!res.ok) loadFailed(res)
  return { products: res.data.items }
}
