import type { ComputedRef, InjectionKey } from 'vue'
import type { Role, Tenant } from '@mscs/shared'

/** The shop being managed, loaded once by pages/shops/[tenantId].vue for all its child pages. */
export interface ShopContext {
  id: string
  tenant: Tenant
  role: Role
  /** owner/admin may edit the catalog and cancel orders (same rule as the Go API). */
  canManage: boolean
}

const SHOP_KEY: InjectionKey<ComputedRef<ShopContext>> = Symbol('shop')

// provide/inject: a parent component hands a value to every descendant without passing
// props through each level (like a request-scoped variable).
export function provideShop(shop: ComputedRef<ShopContext>) {
  provide(SHOP_KEY, shop)
}

export function useShop(): ComputedRef<ShopContext> {
  const shop = inject(SHOP_KEY)
  if (!shop) {
    throw new Error('useShop() must be used inside pages/shops/[tenantId]')
  }
  return shop
}
