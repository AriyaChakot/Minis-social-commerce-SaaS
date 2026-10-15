import { error, fail, redirect } from '@sveltejs/kit'
import type { PlaceOrderRequest, PlaceOrderResponse, StorefrontProduct } from '@mscs/shared'
import { api } from '#lib/server/api.ts'
import type { Actions, PageServerLoad } from './$types'

// Current products, to show names and today's prices for the cart lines.
export const load: PageServerLoad = async ({ params }) => {
  const res = await api<{ items: StorefrontProduct[] }>(`/public/shops/${encodeURIComponent(params.slug)}/products`)
  if (!res.ok) error(res.status === 404 ? 404 : 503, { message: res.status === 404 ? 'ไม่พบร้านค้านี้' : 'ระบบขัดข้องชั่วคราว' })
  return { products: res.data.items }
}

// A form action: the <form method="POST"> on the page posts here, on the server.
// It works as a normal HTML form; with JavaScript, use:enhance submits it without a page reload.
// SvelteKit also rejects form posts coming from other origins (built-in CSRF protection).
export const actions: Actions = {
  default: async ({ request, params }) => {
    const form = await request.formData()
    const values = {
      customer_name: String(form.get('customer_name') ?? '').trim(),
      customer_phone: String(form.get('customer_phone') ?? '').trim(),
      shipping_address: String(form.get('shipping_address') ?? '').trim(),
      note: String(form.get('note') ?? '').trim(),
    }

    let items: PlaceOrderRequest['items'] = []
    try {
      items = JSON.parse(String(form.get('items') ?? '[]'))
    }
    catch {
      items = []
    }
    if (!Array.isArray(items) || items.length === 0) {
      return fail(400, { values, error: { code: 'empty_cart', message: 'cart is empty' } })
    }

    const res = await api<PlaceOrderResponse>(`/public/shops/${encodeURIComponent(params.slug)}/orders`, {
      method: 'POST',
      body: { ...values, items } satisfies PlaceOrderRequest,
    })
    if (!res.ok) {
      // fail() keeps the customer on the page with what they typed, plus the error to show.
      return fail(res.status, { values, error: res.error })
    }

    // 303: "see other" — the browser follows with a GET, so refreshing the next page
    // cannot submit the order twice.
    redirect(303, `/s/${params.slug}/orders/${res.data.tracking_token}?placed=1`)
  },
}
