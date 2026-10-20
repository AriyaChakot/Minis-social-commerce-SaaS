<script lang="ts">
  import { enhance } from '$app/forms'
  import { formatTHB } from '@mscs/shared'
  import { getCart } from '#lib/cart.svelte.ts'
  import { initial, orderErrorMessage, swatch } from '#lib/ui.ts'
  import type { PageProps } from './$types'

  let { data, form }: PageProps = $props()
  const cart = getCart()

  const productById = $derived(new Map(data.products.map(p => [p.id, p])))

  // Cart lines joined with today's product data. A product that was removed from sale since
  // it was added to the cart has no match and is shown as unavailable.
  const lines = $derived(cart.lines.map(l => ({ ...l, product: productById.get(l.productId) })))
  const subtotal = $derived(lines.reduce((sum, l) => sum + (l.product ? l.product.price_satang * l.quantity : 0), 0))
  const hasUnavailable = $derived(lines.some(l => !l.product))

  // What the form sends: ids and quantities only. Prices are decided by the server.
  const itemsJson = $derived(JSON.stringify(cart.lines.map(l => ({ product_id: l.productId, quantity: l.quantity }))))

  const errorMessage = $derived(
    form?.error?.code === 'empty_cart'
      ? 'ตะกร้าว่าง'
      : form?.error ? orderErrorMessage(form.error, id => productById.get(id)?.name) : '',
  )

  let submitting = $state(false)
</script>

<svelte:head>
  <title>ตะกร้าและชำระเงิน · {data.shop.name}</title>
</svelte:head>

<h1 class="mb-4 text-xl font-semibold">ตะกร้าสินค้า</h1>

{#if cart.lines.length === 0}
  <div class="rounded-2xl border border-dashed border-stone-300 py-16 text-center">
    <p class="text-stone-500">ยังไม่มีสินค้าในตะกร้า</p>
    <a href="/s/{data.shop.slug}" class="mt-3 inline-block font-medium text-amber-700 hover:underline">เลือกซื้อสินค้า</a>
  </div>
{:else}
  <div class="grid gap-6 lg:grid-cols-5">
    <ul class="divide-y divide-stone-200 self-start rounded-2xl border border-stone-200 bg-white lg:col-span-3">
      {#each lines as line (line.productId)}
        <li class="flex items-center gap-3 p-3">
          {#if line.product?.images[0]}
            <img src={line.product.images[0].thumb_url} alt="" class="size-14 shrink-0 rounded-xl bg-stone-100 object-cover" />
          {:else}
            <div class="flex size-14 shrink-0 items-center justify-center rounded-xl bg-linear-to-br {swatch(line.product?.name ?? '?')} text-xl font-semibold text-white/80">
              {line.product ? initial(line.product.name) : '?'}
            </div>
          {/if}
          <div class="min-w-0 flex-1">
            {#if line.product}
              <p class="truncate font-medium">{line.product.name}</p>
              <p class="text-sm text-stone-500">{formatTHB(line.product.price_satang)}</p>
              {#if line.quantity > line.product.stock}
                <p class="text-xs font-medium text-rose-600">เหลือเพียง {line.product.stock} ชิ้น</p>
              {/if}
            {:else}
              <p class="font-medium text-rose-600">สินค้านี้หยุดขายแล้ว</p>
            {/if}
          </div>
          {#if line.product}
            <div class="flex items-center rounded-full border border-stone-300">
              <button type="button" class="px-3 py-1" aria-label="ลดจำนวน" onclick={() => cart.setQuantity(line.productId, line.quantity - 1)}>−</button>
              <span class="w-6 text-center tabular-nums">{line.quantity}</span>
              <button
                type="button"
                class="px-3 py-1 disabled:text-stone-300"
                aria-label="เพิ่มจำนวน"
                disabled={line.quantity >= line.product.stock}
                onclick={() => cart.setQuantity(line.productId, line.quantity + 1)}
              >+</button>
            </div>
          {/if}
          <button type="button" class="text-sm text-stone-400 hover:text-rose-600" onclick={() => cart.setQuantity(line.productId, 0)}>ลบ</button>
        </li>
      {/each}
    </ul>

    <form
      method="POST"
      class="space-y-4 rounded-2xl border border-stone-200 bg-white p-4 lg:col-span-2"
      use:enhance={() => {
        submitting = true
        return async ({ update }) => {
          // reset: false keeps the typed values if the order fails.
          // refreshAll: true reloads the products, so a stock change made by another customer
          // since this page loaded shows up next to the line that needs fixing.
          await update({ reset: false, refreshAll: true })
          submitting = false
        }
      }}
    >
      <input type="hidden" name="items" value={itemsJson} />

      <h2 class="font-semibold">ข้อมูลจัดส่ง</h2>
      <label class="block">
        <span class="text-sm text-stone-600">ชื่อ-นามสกุล</span>
        <input name="customer_name" required maxlength="200" autocomplete="name" value={form?.values?.customer_name ?? ''}
          class="mt-1 w-full rounded-xl border border-stone-300 px-3 py-2 focus:border-amber-500 focus:outline-none" />
      </label>
      <label class="block">
        <span class="text-sm text-stone-600">เบอร์โทรศัพท์</span>
        <input name="customer_phone" type="tel" required maxlength="30" autocomplete="tel" value={form?.values?.customer_phone ?? ''}
          class="mt-1 w-full rounded-xl border border-stone-300 px-3 py-2 focus:border-amber-500 focus:outline-none" />
      </label>
      <label class="block">
        <span class="text-sm text-stone-600">ที่อยู่จัดส่ง</span>
        <textarea name="shipping_address" required maxlength="1000" rows="3" autocomplete="street-address"
          class="mt-1 w-full rounded-xl border border-stone-300 px-3 py-2 focus:border-amber-500 focus:outline-none">{form?.values?.shipping_address ?? ''}</textarea>
      </label>
      <label class="block">
        <span class="text-sm text-stone-600">หมายเหตุถึงร้าน (ไม่บังคับ)</span>
        <input name="note" maxlength="2000" value={form?.values?.note ?? ''}
          class="mt-1 w-full rounded-xl border border-stone-300 px-3 py-2 focus:border-amber-500 focus:outline-none" />
      </label>

      <dl class="space-y-1 border-t border-stone-200 pt-4 text-sm">
        <div class="flex justify-between"><dt class="text-stone-500">ยอดสินค้า</dt><dd class="tabular-nums">{formatTHB(subtotal)}</dd></div>
        <div class="flex justify-between"><dt class="text-stone-500">ค่าจัดส่ง</dt><dd>ฟรี</dd></div>
        <div class="flex justify-between pt-1 text-base font-semibold"><dt>ยอดรวม</dt><dd class="tabular-nums">{formatTHB(subtotal)}</dd></div>
      </dl>

      {#if errorMessage}
        <p class="rounded-xl bg-rose-50 px-3 py-2 text-sm text-rose-700" role="alert">{errorMessage}</p>
      {/if}
      {#if hasUnavailable}
        <p class="rounded-xl bg-amber-50 px-3 py-2 text-sm text-amber-800">กรุณาลบสินค้าที่หยุดขายออกจากตะกร้าก่อนสั่งซื้อ</p>
      {/if}

      <button
        class="w-full rounded-full bg-amber-400 py-3 font-semibold text-stone-900 hover:bg-amber-300 disabled:cursor-not-allowed disabled:bg-stone-200 disabled:text-stone-400"
        disabled={submitting || hasUnavailable}
      >
        {submitting ? 'กำลังสั่งซื้อ…' : 'ยืนยันการสั่งซื้อ'}
      </button>
      <p class="text-center text-xs text-stone-400">ชำระเงินตามช่องทางที่ร้านแจ้งหลังสั่งซื้อ</p>
    </form>
  </div>
{/if}
