import { getContext, setContext } from 'svelte'

// The shopping cart: product ids and quantities, kept in the browser's localStorage, one cart
// per shop. Prices are NOT stored here: they are always read from the current product list,
// and the server recomputes everything when the order is placed.
//
// `.svelte.ts` lets this plain TypeScript file use runes ($state): fields declared with $state
// are reactive, so any component reading `cart.count` updates when the cart changes.

export interface CartLine {
  productId: string
  quantity: number
}

export class Cart {
  lines = $state<CartLine[]>([])
  #key: string

  constructor(slug: string) {
    this.#key = `mscs-cart:${slug}`
  }

  /** Total number of items (for the cart badge). A getter over $state is reactive too. */
  get count() {
    return this.lines.reduce((n, l) => n + l.quantity, 0)
  }

  quantityOf(productId: string) {
    return this.lines.find(l => l.productId === productId)?.quantity ?? 0
  }

  /**
   * Reads the saved cart. Call it in onMount, never during server rendering: the server has no
   * localStorage, and reading it during the first browser render would make that render differ
   * from the server's HTML (a hydration mismatch).
   */
  load() {
    try {
      const saved = JSON.parse(localStorage.getItem(this.#key) ?? '[]') as unknown
      if (Array.isArray(saved)) {
        this.lines = saved.filter((l): l is CartLine =>
          typeof l?.productId === 'string' && Number.isInteger(l?.quantity) && l.quantity > 0)
      }
    }
    catch {
      this.lines = [] // corrupted or blocked storage: start with an empty cart
    }
  }

  /** Adds one unit, without going above `max` (the stock we know of). */
  add(productId: string, max: number) {
    this.setQuantity(productId, Math.min(this.quantityOf(productId) + 1, max))
  }

  setQuantity(productId: string, quantity: number) {
    if (quantity <= 0) {
      this.lines = this.lines.filter(l => l.productId !== productId)
    }
    else if (this.quantityOf(productId) === 0) {
      this.lines.push({ productId, quantity })
    }
    else {
      this.lines = this.lines.map(l => (l.productId === productId ? { ...l, quantity } : l))
    }
    this.#save()
  }

  clear() {
    this.lines = []
    this.#save()
  }

  #save() {
    try {
      localStorage.setItem(this.#key, JSON.stringify(this.lines))
    }
    catch {
      // storage full or disabled (private mode): the cart still works for this page view
    }
  }
}

// One Cart per shop, created by the shop layout and shared with its pages through context
// (like provide/inject in Vue).
const CART_KEY = Symbol('cart')

export function setCart(cart: Cart) {
  return setContext(CART_KEY, cart)
}

export function getCart(): Cart {
  return getContext<Cart>(CART_KEY)
}
