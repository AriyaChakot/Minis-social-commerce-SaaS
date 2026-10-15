<script lang="ts">
  import { onMount } from 'svelte'
  import { Cart, setCart } from '#lib/cart.svelte.ts'
  import type { LayoutProps } from './$types'

  let { data, children }: LayoutProps = $props()

  // One cart for this shop, shared with every page below through context.
  // Reading only the initial slug is intended: the root layout re-creates this component
  // (via {#key}) whenever the shop changes, so the slug never changes for this instance.
  // svelte-ignore state_referenced_locally
  const cart = setCart(new Cart(data.shop.slug))
  onMount(() => cart.load())
</script>

<svelte:head>
  <title>{data.shop.name}</title>
</svelte:head>

<div class="flex min-h-dvh flex-col">
  <header class="sticky top-0 z-10 border-b border-stone-200 bg-white/90 backdrop-blur">
    <div class="mx-auto flex h-14 max-w-5xl items-center justify-between gap-4 px-4">
      <a href="/s/{data.shop.slug}" class="truncate text-lg font-semibold">{data.shop.name}</a>
      <a
        href="/s/{data.shop.slug}/checkout"
        class="relative inline-flex items-center gap-2 rounded-full bg-stone-900 px-4 py-2 text-sm font-medium text-white hover:bg-stone-700"
      >
        ตะกร้า
        {#if cart.count > 0}
          <span class="rounded-full bg-amber-400 px-2 text-xs font-semibold text-stone-900">{cart.count}</span>
        {/if}
      </a>
    </div>
  </header>

  <main class="mx-auto w-full max-w-5xl flex-1 px-4 py-6">
    {@render children()}
  </main>

  <footer class="py-6 text-center text-xs text-stone-400">ร้านค้าออนไลน์โดย MSCS</footer>
</div>
