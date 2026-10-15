<script setup lang="ts">
import type { TableColumn } from '@nuxt/ui'
import type { Order, OrderStatus } from '@mscs/shared'
import { formatDateTime, formatTHB } from '@mscs/shared'

useHead({ title: 'ออเดอร์' })

const shop = useShop()

// Status tabs: the most common question is "what is waiting for payment / shipping?".
const status = ref<OrderStatus | 'all'>('all')
const tabs = [
  { label: 'ทั้งหมด', value: 'all' },
  ...(['pending', 'paid', 'shipped', 'completed', 'cancelled'] as const).map(s => ({ label: ORDER_STATUS_LABEL[s], value: s })),
]

const search = ref('')
const debouncedSearch = useDebounced(search)

const query = computed(() => ({ status: status.value, q: debouncedSearch.value || undefined, limit: 20 }))
const { items, hasMore, loading, loadingMore, error, loadMore } = useCursorList<Order>(
  () => `orders-${shop.value.id}`,
  () => `/api/tenants/${shop.value.id}/orders`,
  query,
)

const columns: TableColumn<Order>[] = [
  { accessorKey: 'order_number', header: 'เลขที่' },
  { accessorKey: 'customer_name', header: 'ลูกค้า' },
  { accessorKey: 'channel', header: 'ช่องทาง' },
  { accessorKey: 'total_satang', header: 'ยอดรวม' },
  { accessorKey: 'status', header: 'สถานะ' },
  { accessorKey: 'created_at', header: 'สั่งเมื่อ' },
]
</script>

<template>
  <div class="space-y-4">
    <div class="flex flex-wrap items-center gap-3">
      <UInput v-model="search" icon="i-lucide-search" placeholder="ชื่อ เบอร์โทร หรือ #เลขที่" class="w-full sm:w-64" />
      <UButton class="ms-auto" icon="i-lucide-plus" label="สร้างออเดอร์" :to="`/shops/${shop.id}/orders/new`" />
    </div>

    <UTabs v-model="status" :items="tabs" :content="false" class="w-full overflow-x-auto" />

    <UAlert v-if="error" color="error" variant="subtle" :title="apiErrorMessage(error)" />

    <UCard :ui="{ body: 'p-0 sm:p-0' }">
      <UTable :data="items" :columns="columns" :loading="loading" empty="ยังไม่มีออเดอร์">
        <template #order_number-cell="{ row }">
          <NuxtLink :to="`/shops/${shop.id}/orders/${row.original.id}`" class="font-medium text-primary hover:underline">
            #{{ row.original.order_number }}
          </NuxtLink>
        </template>

        <template #customer_name-cell="{ row }">
          <p>{{ row.original.customer_name }}</p>
          <p v-if="row.original.customer_phone" class="text-xs text-muted">
            {{ row.original.customer_phone }}
          </p>
        </template>

        <template #channel-cell="{ row }">
          {{ CHANNEL_LABEL[row.original.channel] }}
        </template>

        <template #total_satang-cell="{ row }">
          <span class="font-medium">{{ formatTHB(row.original.total_satang) }}</span>
        </template>

        <template #status-cell="{ row }">
          <UBadge :color="ORDER_STATUS_COLOR[row.original.status]" variant="subtle" :label="ORDER_STATUS_LABEL[row.original.status]" />
        </template>

        <template #created_at-cell="{ row }">
          <span class="text-sm text-muted">{{ formatDateTime(row.original.created_at) }}</span>
        </template>
      </UTable>
    </UCard>

    <div v-if="hasMore" class="flex justify-center">
      <UButton color="neutral" variant="outline" label="โหลดเพิ่ม" :loading="loadingMore" @click="loadMore" />
    </div>
  </div>
</template>
