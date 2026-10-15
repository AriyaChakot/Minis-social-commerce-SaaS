<script lang="ts">
  import { onMount } from 'svelte'
  import { goto } from '$app/navigation'
  import { page } from '$app/state'
  import { formatDateTime, formatTHB } from '@mscs/shared'
  import type { OrderStatus } from '@mscs/shared'
  import { getCart } from '#lib/cart.svelte.ts'
  import { STATUS_LABEL } from '#lib/ui.ts'
  import type { PageProps } from './$types'

  let { data }: PageProps = $props()
  const cart = getCart()

  const justPlaced = page.url.searchParams.has('placed')
  let copied = $state(false)

  onMount(() => {
    if (justPlaced) {
      cart.clear() // the order went through: empty the cart
      // Drop "?placed=1" so a refresh or a shared link does not show the thank-you note again.
      // shallow: change the URL only, without reloading the page's data.
      goto(page.url.pathname, { replace: true, shallow: true, state: page.state })
    }
  })

  // The happy path as steps; a cancelled order shows a banner instead.
  const steps: { status: OrderStatus; at: (o: typeof data.order) => string | null }[] = [
    { status: 'pending', at: o => o.created_at },
    { status: 'paid', at: o => o.paid_at },
    { status: 'shipped', at: o => o.shipped_at },
    { status: 'completed', at: o => o.completed_at },
  ]

  async function copyLink() {
    await navigator.clipboard.writeText(location.origin + page.url.pathname)
    copied = true
    setTimeout(() => (copied = false), 2000)
  }
</script>

<svelte:head>
  <title>คำสั่งซื้อ #{data.order.order_number} · {data.shop.name}</title>
  <!-- The URL contains a secret token: do not send it to other sites in the Referer header. -->
  <meta name="referrer" content="no-referrer" />
</svelte:head>

<div class="mx-auto max-w-xl space-y-5">
  {#if justPlaced}
    <div class="rounded-2xl bg-emerald-50 p-4 text-emerald-900">
      <p class="text-lg font-semibold">สั่งซื้อเรียบร้อย ขอบคุณค่ะ 🎉</p>
      <p class="text-sm">ร้านจะติดต่อกลับเพื่อแจ้งช่องทางชำระเงิน</p>
    </div>
  {/if}

  <div class="rounded-2xl border border-stone-200 bg-white p-4">
    <div class="flex items-start justify-between gap-3">
      <div>
        <p class="text-sm text-stone-500">คำสั่งซื้อ</p>
        <h1 class="text-2xl font-semibold">#{data.order.order_number}</h1>
        <p class="text-sm text-stone-500">{formatDateTime(data.order.created_at)}</p>
      </div>
      <span class="rounded-full px-3 py-1 text-sm font-medium {data.order.status === 'cancelled' ? 'bg-stone-100 text-stone-600' : 'bg-amber-100 text-amber-800'}">
        {STATUS_LABEL[data.order.status]}
      </span>
    </div>

    {#if data.order.status === 'cancelled'}
      <p class="mt-4 rounded-xl bg-stone-100 px-3 py-2 text-sm text-stone-600">
        คำสั่งซื้อนี้ถูกยกเลิกเมื่อ {formatDateTime(data.order.cancelled_at!)}
      </p>
    {:else}
      <ol class="mt-5 grid grid-cols-4 gap-1 text-center text-xs">
        {#each steps as step (step.status)}
          {@const at = step.at(data.order)}
          <li>
            <div class="mb-1 h-1.5 rounded-full {at ? 'bg-amber-400' : 'bg-stone-200'}"></div>
            <p class={at ? 'font-medium' : 'text-stone-400'}>{STATUS_LABEL[step.status]}</p>
          </li>
        {/each}
      </ol>
    {/if}
  </div>

  <div class="rounded-2xl border border-stone-200 bg-white">
    <ul class="divide-y divide-stone-200">
      {#each data.order.items as item, i (i)}
        <li class="flex justify-between gap-3 p-4 text-sm">
          <span>{item.product_name} <span class="text-stone-400">× {item.quantity}</span></span>
          <span class="tabular-nums">{formatTHB(item.line_total_satang)}</span>
        </li>
      {/each}
    </ul>
    <dl class="space-y-1 border-t border-stone-200 p-4 text-sm">
      <div class="flex justify-between"><dt class="text-stone-500">ค่าจัดส่ง</dt><dd>{data.order.shipping_fee_satang === 0 ? 'ฟรี' : formatTHB(data.order.shipping_fee_satang)}</dd></div>
      <div class="flex justify-between text-base font-semibold"><dt>ยอดรวม</dt><dd class="tabular-nums">{formatTHB(data.order.total_satang)}</dd></div>
    </dl>
  </div>

  <div class="rounded-2xl border border-amber-200 bg-amber-50 p-4 text-sm text-amber-900">
    <p class="font-medium">เก็บลิงก์หน้านี้ไว้เพื่อติดตามสถานะคำสั่งซื้อ</p>
    <p class="text-amber-800/80">ลิงก์นี้เป็นของคุณคนเดียว ไม่ควรแชร์ในที่สาธารณะ</p>
    <button type="button" onclick={copyLink} class="mt-2 rounded-full bg-amber-400 px-4 py-1.5 font-medium text-stone-900 hover:bg-amber-300">
      {copied ? 'คัดลอกแล้ว ✓' : 'คัดลอกลิงก์'}
    </button>
  </div>

  <p class="text-center">
    <a href="/s/{data.shop.slug}" class="text-sm font-medium text-amber-700 hover:underline">← กลับไปหน้าร้าน</a>
  </p>
</div>
