<script setup lang="ts">
import type { Order, OrderStatus } from '@mscs/shared'
import { formatDateTime, formatTHB, nextStatuses } from '@mscs/shared'

const shop = useShop()
const route = useRoute()
const toast = useToast()
const orderId = computed(() => String(route.params.orderId))

const { data: order, error } = await useFetch<Order>(() => `/api/tenants/${shop.value.id}/orders/${orderId.value}`, {
  key: () => `order-${orderId.value}`,
})
if (error.value || !order.value) {
  throw createError({ statusCode: 404, statusMessage: 'ไม่พบออเดอร์นี้', fatal: true })
}

useHead({ title: () => `ออเดอร์ #${order.value?.order_number}` })

// Buttons = the transitions the state machine allows, minus "cancel" for staff
// (the API enforces the same rule; hiding the button is only for convenience).
const actions = computed<OrderStatus[]>(() =>
  nextStatuses(order.value!.status).filter(s => s !== 'cancelled' || shop.value.canManage),
)

const changing = ref<OrderStatus | null>(null)
const cancelOpen = ref(false)

async function changeStatus(to: OrderStatus) {
  changing.value = to
  try {
    order.value = await $fetch<Order>(`/api/tenants/${shop.value.id}/orders/${orderId.value}/status`, {
      method: 'POST',
      body: { status: to },
    })
    toast.add({ title: `เปลี่ยนสถานะเป็น “${ORDER_STATUS_LABEL[to]}” แล้ว`, color: 'success', icon: 'i-lucide-check' })
    cancelOpen.value = false
  }
  catch (e) {
    toast.add({ title: apiErrorMessage(e), color: 'error' })
  }
  finally {
    changing.value = null
  }
}

function onAction(to: OrderStatus) {
  if (to === 'cancelled') cancelOpen.value = true // returns stock and cannot be undone: confirm first
  else changeStatus(to)
}

const timeline = computed(() => {
  const o = order.value!
  return [
    { label: 'สร้างออเดอร์', at: o.created_at },
    { label: 'ชำระเงิน', at: o.paid_at },
    { label: 'จัดส่ง', at: o.shipped_at },
    { label: 'สำเร็จ', at: o.completed_at },
    { label: 'ยกเลิก', at: o.cancelled_at },
  ].filter(step => step.at !== null) as { label: string; at: string }[]
})
</script>

<template>
  <div v-if="order" class="space-y-6">
    <div class="flex flex-wrap items-center justify-between gap-3">
      <div class="flex items-center gap-3">
        <UButton :to="`/shops/${shop.id}/orders`" icon="i-lucide-arrow-left" color="neutral" variant="ghost" aria-label="กลับไปรายการออเดอร์" />
        <h2 class="text-lg font-semibold">
          ออเดอร์ #{{ order.order_number }}
        </h2>
        <UBadge :color="ORDER_STATUS_COLOR[order.status]" variant="subtle" :label="ORDER_STATUS_LABEL[order.status]" />
      </div>

      <div class="flex flex-wrap gap-2">
        <UButton
          v-for="to in actions"
          :key="to"
          :label="ORDER_ACTION_LABEL[to]"
          :color="to === 'cancelled' ? 'error' : 'primary'"
          :variant="to === 'cancelled' ? 'outline' : 'solid'"
          :loading="changing === to"
          :disabled="changing !== null"
          @click="onAction(to)"
        />
      </div>
    </div>

    <div class="grid gap-6 lg:grid-cols-3">
      <UCard class="lg:col-span-2" :ui="{ body: 'p-0 sm:p-0' }">
        <table class="w-full text-sm">
          <thead class="border-b border-default text-left text-muted">
            <tr>
              <th class="px-4 py-3 font-medium">
                สินค้า
              </th>
              <th class="px-4 py-3 text-right font-medium">
                ราคา
              </th>
              <th class="px-4 py-3 text-right font-medium">
                จำนวน
              </th>
              <th class="px-4 py-3 text-right font-medium">
                รวม
              </th>
            </tr>
          </thead>
          <tbody class="divide-y divide-default">
            <tr v-for="item in order.items" :key="item.id">
              <td class="px-4 py-3">
                <p class="font-medium">
                  {{ item.product_name }}
                </p>
                <p v-if="item.sku" class="text-xs text-muted">
                  {{ item.sku }}
                </p>
              </td>
              <td class="px-4 py-3 text-right tabular-nums">
                {{ formatTHB(item.unit_price_satang) }}
              </td>
              <td class="px-4 py-3 text-right tabular-nums">
                {{ item.quantity }}
              </td>
              <td class="px-4 py-3 text-right tabular-nums">
                {{ formatTHB(item.line_total_satang) }}
              </td>
            </tr>
          </tbody>
          <tfoot class="border-t border-default">
            <tr>
              <td colspan="3" class="px-4 pt-3 text-right text-muted">
                ยอดสินค้า
              </td>
              <td class="px-4 pt-3 text-right tabular-nums">
                {{ formatTHB(order.subtotal_satang) }}
              </td>
            </tr>
            <tr>
              <td colspan="3" class="px-4 py-1 text-right text-muted">
                ค่าส่ง
              </td>
              <td class="px-4 py-1 text-right tabular-nums">
                {{ formatTHB(order.shipping_fee_satang) }}
              </td>
            </tr>
            <tr class="text-base font-semibold">
              <td colspan="3" class="px-4 pb-3 text-right">
                ยอดรวม
              </td>
              <td class="px-4 pb-3 text-right tabular-nums">
                {{ formatTHB(order.total_satang) }}
              </td>
            </tr>
          </tfoot>
        </table>
      </UCard>

      <div class="space-y-6">
        <UCard>
          <template #header>
            <h3 class="font-semibold">
              ลูกค้า
            </h3>
          </template>
          <dl class="space-y-2 text-sm">
            <div>
              <dt class="text-muted">
                ชื่อ
              </dt>
              <dd>{{ order.customer_name }}</dd>
            </div>
            <div v-if="order.customer_phone">
              <dt class="text-muted">
                เบอร์โทร
              </dt>
              <dd>{{ order.customer_phone }}</dd>
            </div>
            <div v-if="order.shipping_address">
              <dt class="text-muted">
                ที่อยู่จัดส่ง
              </dt>
              <dd class="whitespace-pre-line">
                {{ order.shipping_address }}
              </dd>
            </div>
            <div>
              <dt class="text-muted">
                ช่องทาง
              </dt>
              <dd>{{ CHANNEL_LABEL[order.channel] }}</dd>
            </div>
            <div v-if="order.note">
              <dt class="text-muted">
                หมายเหตุ
              </dt>
              <dd>{{ order.note }}</dd>
            </div>
          </dl>
        </UCard>

        <UCard>
          <template #header>
            <h3 class="font-semibold">
              ประวัติ
            </h3>
          </template>
          <ol class="space-y-2 text-sm">
            <li v-for="step in timeline" :key="step.label" class="flex justify-between gap-4">
              <span>{{ step.label }}</span>
              <span class="text-muted">{{ formatDateTime(step.at) }}</span>
            </li>
          </ol>
        </UCard>
      </div>
    </div>

    <ConfirmModal
      v-model:open="cancelOpen"
      :title="`ยกเลิกออเดอร์ #${order.order_number}?`"
      description="สินค้าในออเดอร์จะถูกคืนเข้าสต็อก และย้อนกลับไม่ได้"
      confirm-label="ยกเลิกออเดอร์"
      danger
      :loading="changing === 'cancelled'"
      @confirm="changeStatus('cancelled')"
    />
  </div>
</template>
