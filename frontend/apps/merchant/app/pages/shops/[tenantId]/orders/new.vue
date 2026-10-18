<script setup lang="ts">
import { z } from 'zod'
import type { FormSubmitEvent } from '@nuxt/ui'
import type { CreateOrderRequest, Order, OrderChannel, Page, Product } from '@mscs/shared'
import { formatTHB, parseBahtToSatang } from '@mscs/shared'

useHead({ title: 'สร้างออเดอร์' })

const shop = useShop()
const toast = useToast()

// Products to pick from. MVP simplification: the first 100 active products are loaded once
// and filtered in the browser. A shop with a bigger catalog would need server-side search here.
const { data: productPage } = await useFetch<Page<Product>>(() => `/api/tenants/${shop.value.id}/products`, {
  query: { status: 'active', limit: 100 },
  key: () => `order-products-${shop.value.id}`,
})
const products = computed(() => productPage.value?.items ?? [])
const productById = computed(() => new Map(products.value.map(p => [p.id, p])))

const productOptions = computed(() => products.value.map(p => ({
  label: p.name,
  value: p.id,
  description: `${formatTHB(p.price_satang)} · เหลือ ${p.stock}`,
  disabled: p.stock === 0,
})))

const channelOptions = (Object.keys(CHANNEL_LABEL) as OrderChannel[])
  .filter(c => c !== 'storefront') // storefront orders come from the online shop, not this form
  .map(c => ({ label: CHANNEL_LABEL[c], value: c }))

// ---- Form -----------------------------------------------------------------------
const schema = z.object({
  customer_name: z.string().trim().min(1, 'กรุณากรอกชื่อลูกค้า').max(200),
  customer_phone: z.string().trim().max(30),
  shipping_address: z.string().max(1000),
  channel: z.enum(['manual', 'line', 'facebook', 'instagram']),
  note: z.string().max(2000),
  shipping_fee: z.string().refine(v => v.trim() === '' || parseBahtToSatang(v) !== null, 'ค่าส่งไม่ถูกต้อง'),
  items: z
    .array(z.object({
      product_id: z.string().min(1, 'กรุณาเลือกสินค้า'),
      quantity: z.number().int().min(1, 'อย่างน้อย 1'),
    }))
    .min(1, 'ต้องมีสินค้าอย่างน้อย 1 รายการ')
    .refine(lines => new Set(lines.map(l => l.product_id)).size === lines.length, 'เลือกสินค้าซ้ำกัน ให้เพิ่มจำนวนแทน'),
})
type Schema = z.output<typeof schema>

const state = reactive({
  customer_name: '',
  customer_phone: '',
  shipping_address: '',
  channel: 'line' as Schema['channel'],
  note: '',
  shipping_fee: '',
  items: [{ product_id: '', quantity: 1 }],
})

// Opened from the inbox ("สร้างออเดอร์จากแชทนี้"): start with that chat's customer and channel.
const route = useRoute()
if (typeof route.query.customer_name === 'string') {
  state.customer_name = route.query.customer_name.slice(0, 200)
}
const fromChannel = channelOptions.find(o => o.value === route.query.channel)
if (fromChannel) state.channel = fromChannel.value as Schema['channel']

function addLine() {
  state.items.push({ product_id: '', quantity: 1 })
}
function removeLine(i: number) {
  state.items.splice(i, 1)
}

// Live preview of the totals. The server recomputes them from its own prices:
// this is only to help the merchant, never trusted.
const lineTotal = (l: { product_id: string; quantity: number }) =>
  (productById.value.get(l.product_id)?.price_satang ?? 0) * (l.quantity || 0)
const subtotal = computed(() => state.items.reduce((sum, l) => sum + lineTotal(l), 0))
const shippingFee = computed(() => parseBahtToSatang(state.shipping_fee || '0') ?? 0)

// ---- Submit ---------------------------------------------------------------------
const saving = ref(false)
const errorMessage = ref('')

async function onSubmit(event: FormSubmitEvent<Schema>) {
  const v = event.data
  const body: CreateOrderRequest = {
    customer_name: v.customer_name,
    customer_phone: v.customer_phone,
    shipping_address: v.shipping_address,
    channel: v.channel,
    note: v.note,
    shipping_fee_satang: shippingFee.value,
    items: v.items,
  }

  saving.value = true
  errorMessage.value = ''
  try {
    const order = await $fetch<Order>(`/api/tenants/${shop.value.id}/orders`, { method: 'POST', body })
    toast.add({ title: `สร้างออเดอร์ #${order.order_number} แล้ว`, color: 'success', icon: 'i-lucide-check' })
    await navigateTo(`/shops/${shop.value.id}/orders/${order.id}`)
  }
  catch (e) {
    errorMessage.value = orderErrorMessage(e)
  }
  finally {
    saving.value = false
  }
}

// Stock problems name the product, so the merchant knows which line to fix.
function orderErrorMessage(e: unknown): string {
  const err = apiError(e)
  const details = err?.details as { product_id?: string; available?: number } | undefined
  const name = details?.product_id ? productById.value.get(details.product_id)?.name ?? 'สินค้า' : 'สินค้า'
  if (err?.code === 'insufficient_stock') return `“${name}” มีไม่พอ (เหลือ ${details?.available ?? 0})`
  if (err?.code === 'product_unavailable') return `“${name}” ปิดการขายแล้ว หรือไม่มีอยู่ในร้าน`
  return apiErrorMessage(e)
}
</script>

<template>
  <UForm :schema="schema" :state="state" class="grid gap-6 lg:grid-cols-3" @submit="onSubmit">
    <div class="space-y-6 lg:col-span-2">
      <UCard>
        <template #header>
          <h2 class="font-semibold">
            สินค้า
          </h2>
        </template>

        <div class="space-y-3">
          <div v-for="(line, i) in state.items" :key="i" class="flex items-start gap-2">
            <UFormField :name="`items.${i}.product_id`" class="min-w-0 flex-1">
              <USelectMenu
                v-model="line.product_id"
                :items="productOptions"
                value-key="value"
                placeholder="เลือกสินค้า"
                :search-input="{ placeholder: 'ค้นหาสินค้า' }"
                class="w-full"
              />
            </UFormField>
            <UFormField :name="`items.${i}.quantity`" class="w-32">
              <UInputNumber v-model="line.quantity" :min="1" class="w-full" />
            </UFormField>
            <p class="w-24 pt-1.5 text-right text-sm tabular-nums">
              {{ formatTHB(lineTotal(line)) }}
            </p>
            <UButton
              icon="i-lucide-x"
              color="neutral"
              variant="ghost"
              aria-label="ลบรายการ"
              :disabled="state.items.length === 1"
              @click="removeLine(i)"
            />
          </div>

          <UFormField name="items">
            <UButton icon="i-lucide-plus" color="neutral" variant="outline" size="sm" label="เพิ่มรายการ" @click="addLine" />
          </UFormField>

          <p v-if="!products.length" class="text-sm text-muted">
            ร้านยังไม่มีสินค้าที่วางขาย
            <NuxtLink :to="`/shops/${shop.id}/products`" class="text-primary">
              ไปเพิ่มสินค้า
            </NuxtLink>
          </p>
        </div>
      </UCard>

      <UCard>
        <template #header>
          <h2 class="font-semibold">
            ลูกค้า
          </h2>
        </template>

        <div class="grid gap-4 sm:grid-cols-2">
          <UFormField label="ชื่อลูกค้า" name="customer_name" required>
            <UInput v-model="state.customer_name" class="w-full" />
          </UFormField>
          <UFormField label="เบอร์โทร" name="customer_phone">
            <UInput v-model="state.customer_phone" type="tel" class="w-full" />
          </UFormField>
          <UFormField label="ที่อยู่จัดส่ง" name="shipping_address" class="sm:col-span-2">
            <UTextarea v-model="state.shipping_address" :rows="2" autoresize class="w-full" />
          </UFormField>
          <UFormField label="ช่องทาง" name="channel">
            <USelect v-model="state.channel" :items="channelOptions" class="w-full" />
          </UFormField>
          <UFormField label="หมายเหตุ" name="note">
            <UInput v-model="state.note" class="w-full" />
          </UFormField>
        </div>
      </UCard>
    </div>

    <div>
      <UCard class="lg:sticky lg:top-6">
        <template #header>
          <h2 class="font-semibold">
            สรุป
          </h2>
        </template>

        <div class="space-y-3 text-sm">
          <div class="flex justify-between">
            <span class="text-muted">ยอดสินค้า</span>
            <span class="tabular-nums">{{ formatTHB(subtotal) }}</span>
          </div>
          <UFormField label="ค่าส่ง (บาท)" name="shipping_fee">
            <UInput v-model="state.shipping_fee" inputmode="decimal" placeholder="0.00" class="w-full" />
          </UFormField>
          <USeparator />
          <div class="flex justify-between text-base font-semibold">
            <span>ยอดรวม</span>
            <span class="tabular-nums">{{ formatTHB(subtotal + shippingFee) }}</span>
          </div>
        </div>

        <template #footer>
          <div class="space-y-3">
            <UAlert v-if="errorMessage" color="error" variant="subtle" icon="i-lucide-circle-alert" :title="errorMessage" />
            <UButton type="submit" block :loading="saving" label="สร้างออเดอร์" />
            <UButton block color="neutral" variant="ghost" label="ยกเลิก" :to="`/shops/${shop.id}/orders`" />
          </div>
        </template>
      </UCard>
    </div>
  </UForm>
</template>
