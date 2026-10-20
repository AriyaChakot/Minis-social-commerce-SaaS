<script lang="ts">
  import { formatTHB } from '@mscs/shared'
  import { getCart } from '#lib/cart.svelte.ts'
  import { initial, swatch } from '#lib/ui.ts'
  import type { PageProps } from './$types'

  let { data }: PageProps = $props()
  const cart = getCart()
</script>

{#if data.products.length === 0}
  <p class="py-20 text-center text-stone-500">ร้านยังไม่มีสินค้าวางขายในขณะนี้</p>
{:else}
  <!-- pb-20 on phones: room for the floating "ดูตะกร้า" button so it never hides the last product. -->
  <ul class="grid grid-cols-2 gap-4 pb-20 sm:grid-cols-3 sm:pb-0 lg:grid-cols-4">
    {#each data.products as product (product.id)}
      {@const inCart = cart.quantityOf(product.id)}
      {@const soldOut = product.stock === 0}
      <li class="flex flex-col overflow-hidden rounded-2xl border border-stone-200 bg-white">
        {#if product.images[0]}
          <!-- The thumbnail (400 px) is plenty for a grid tile; width/height reserve the space
               before it loads, so the page does not jump. -->
          <img
            src={product.images[0].thumb_url}
            alt={product.name}
            width={product.images[0].width}
            height={product.images[0].height}
            loading="lazy"
            class="aspect-4/3 w-full bg-stone-100 object-cover"
          />
        {:else}
          <!-- No photo: a colored tile with the first letter stands in for one. -->
          <div class="flex aspect-4/3 items-center justify-center bg-linear-to-br {swatch(product.name)} text-5xl font-semibold text-white/80">
            {initial(product.name)}
          </div>
        {/if}

        <div class="flex flex-1 flex-col gap-1 p-3">
          <h2 class="line-clamp-2 font-medium leading-snug">{product.name}</h2>
          {#if product.description}
            <p class="line-clamp-2 text-sm text-stone-500">{product.description}</p>
          {/if}
          <p class="mt-auto pt-2 text-lg font-semibold">{formatTHB(product.price_satang)}</p>
          <p class="text-xs {soldOut ? 'text-rose-600' : product.stock <= 5 ? 'text-amber-600' : 'text-stone-400'}">
            {soldOut ? 'สินค้าหมด' : product.stock <= 5 ? `เหลือ ${product.stock} ชิ้น` : 'มีสินค้า'}
          </p>

          <button
            type="button"
            class="mt-2 rounded-full bg-stone-900 px-3 py-2 text-sm font-medium text-white hover:bg-stone-700 disabled:cursor-not-allowed disabled:bg-stone-300"
            disabled={soldOut || inCart >= product.stock}
            onclick={() => cart.add(product.id, product.stock)}
          >
            {#if soldOut}
              สินค้าหมด
            {:else if inCart > 0}
              ในตะกร้า {inCart} · เพิ่มอีก
            {:else}
              ใส่ตะกร้า
            {/if}
          </button>
        </div>
      </li>
    {/each}
  </ul>

  {#if cart.count > 0}
    <!-- Mobile shortcut to the checkout, always within thumb reach. -->
    <a
      href="/s/{data.shop.slug}/checkout"
      class="fixed inset-x-4 bottom-4 rounded-full bg-amber-400 py-3 text-center font-semibold text-stone-900 shadow-lg sm:hidden"
    >
      ดูตะกร้า ({cart.count} ชิ้น)
    </a>
  {/if}
{/if}
