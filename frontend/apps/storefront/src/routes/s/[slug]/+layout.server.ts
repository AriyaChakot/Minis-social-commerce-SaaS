import type { StorefrontShop } from '@mscs/shared'
import { api, loadFailed } from '#lib/server/api.ts'
import type { LayoutServerLoad } from './$types'

// Runs on the server for every page under /s/:slug. Unknown or suspended shops get a 404 here,
// before any child page loads.
export const load: LayoutServerLoad = async ({ params }) => {
  const res = await api<StorefrontShop>(`/public/shops/${encodeURIComponent(params.slug)}`)
  if (!res.ok) loadFailed(res)
  return { shop: res.data }
}
