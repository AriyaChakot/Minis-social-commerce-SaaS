<script setup lang="ts">
import type { TableColumn } from '@nuxt/ui'
import type { Product } from '@mscs/shared'
import { formatTHB } from '@mscs/shared'

useHead({ title: 'สินค้า' })

const shop = useShop()
const toast = useToast()

// ---- Filters -> query -> list ---------------------------------------------------
const search = ref('')
const debouncedSearch = useDebounced(search)
const status = ref<'active' | 'archived' | 'all'>('active')
const statusItems = [
  { label: 'วางขาย', value: 'active' },
  { label: 'ปิดการขาย', value: 'archived' },
  { label: 'ทั้งหมด', value: 'all' },
]

const query = computed(() => ({ status: status.value, q: debouncedSearch.value || undefined, limit: 20 }))
const { items, hasMore, loading, loadingMore, error, loadMore, refresh } = useCursorList<Product>(
  () => `products-${shop.value.id}`,
  () => `/api/tenants/${shop.value.id}/products`,
  query,
)

// ---- Table ----------------------------------------------------------------------
// Columns only declare structure; how each cell looks is in the #<id>-cell slots below.
const columns = computed<TableColumn<Product>[]>(() => [
  { accessorKey: 'name', header: 'สินค้า' },
  { accessorKey: 'price_satang', header: 'ราคา' },
  { accessorKey: 'stock', header: 'คงเหลือ' },
  { accessorKey: 'status', header: 'สถานะ' },
  // Never `header: ''`: an empty string becomes an empty text node, which the server-rendered
  // HTML cannot contain, so hydration reports a mismatch. A screen-reader-only label avoids it
  // (and tells screen-reader users what the column is).
  ...(shop.value.canManage ? [{ id: 'actions', header: () => h('span', { class: 'sr-only' }, 'การจัดการ') }] : []),
])

// ---- Add / edit -----------------------------------------------------------------
const formOpen = ref(false)
const editing = ref<Product | null>(null)

function openCreate() {
  editing.value = null
  formOpen.value = true
}
function openEdit(p: Product) {
  editing.value = p
  formOpen.value = true
}
async function onSaved(p: Product) {
  toast.add({ title: editing.value ? 'บันทึกการแก้ไขแล้ว' : `เพิ่ม “${p.name}” แล้ว`, color: 'success', icon: 'i-lucide-check' })
  await refresh()
}

// ---- Archive / restore ----------------------------------------------------------
const archiveTarget = ref<Product | null>(null)
const archiveOpen = computed({
  get: () => archiveTarget.value !== null,
  set: (v) => { if (!v) archiveTarget.value = null },
})
const archiving = ref(false)

async function confirmArchive() {
  if (!archiveTarget.value) return
  archiving.value = true
  try {
    await $fetch(`/api/tenants/${shop.value.id}/products/${archiveTarget.value.id}`, { method: 'DELETE' })
    toast.add({ title: 'ปิดการขายแล้ว', color: 'success', icon: 'i-lucide-archive' })
    archiveTarget.value = null
    await refresh()
  }
  catch (e) {
    toast.add({ title: apiErrorMessage(e), color: 'error' })
  }
  finally {
    archiving.value = false
  }
}

async function restore(p: Product) {
  try {
    await $fetch(`/api/tenants/${shop.value.id}/products/${p.id}`, { method: 'PATCH', body: { status: 'active' } })
    toast.add({ title: `เปิดขาย “${p.name}” อีกครั้งแล้ว`, color: 'success' })
    await refresh()
  }
  catch (e) {
    toast.add({ title: apiErrorMessage(e), color: 'error' })
  }
}
</script>

<template>
  <div class="space-y-4">
    <div class="flex flex-wrap items-center gap-3">
      <UInput v-model="search" icon="i-lucide-search" placeholder="ค้นหาชื่อสินค้า" class="w-full sm:w-64" />
      <!-- Same status-tabs pattern as the orders page. -->
      <UTabs v-model="status" :items="statusItems" :content="false" size="sm" />
      <UButton v-if="shop.canManage" class="ms-auto" icon="i-lucide-plus" label="เพิ่มสินค้า" @click="openCreate" />
    </div>

    <UAlert v-if="error" color="error" variant="subtle" :title="apiErrorMessage(error)" />

    <UCard :ui="{ body: 'p-0 sm:p-0' }">
      <UTable :data="items" :columns="columns" :loading="loading" empty="ยังไม่มีสินค้า">
        <template #name-cell="{ row }">
          <div class="flex items-center gap-3">
            <img
              v-if="row.original.images[0]"
              :src="row.original.images[0].thumb_url"
              alt=""
              class="size-10 shrink-0 rounded object-cover ring ring-default"
              loading="lazy"
            >
            <div v-else class="flex size-10 shrink-0 items-center justify-center rounded bg-elevated text-muted">
              <UIcon name="i-lucide-image" />
            </div>
            <div class="min-w-0">
              <p class="font-medium">
                {{ row.original.name }}
              </p>
              <p v-if="row.original.sku" class="text-xs text-muted">
                {{ row.original.sku }}
              </p>
            </div>
          </div>
        </template>

        <template #price_satang-cell="{ row }">
          {{ formatTHB(row.original.price_satang) }}
        </template>

        <template #stock-cell="{ row }">
          <UBadge v-if="row.original.stock === 0" color="error" variant="subtle" label="หมด" />
          <span v-else>{{ row.original.stock.toLocaleString('th-TH') }}</span>
        </template>

        <template #status-cell="{ row }">
          <UBadge :color="PRODUCT_STATUS_COLOR[row.original.status]" variant="subtle" :label="PRODUCT_STATUS_LABEL[row.original.status]" />
        </template>

        <template #actions-cell="{ row }">
          <div class="flex justify-end gap-1">
            <UButton icon="i-lucide-pencil" color="neutral" variant="ghost" size="sm" aria-label="แก้ไข" @click="openEdit(row.original)" />
            <UButton
              v-if="row.original.status === 'active'"
              icon="i-lucide-archive"
              color="neutral"
              variant="ghost"
              size="sm"
              aria-label="ปิดการขาย"
              @click="archiveTarget = row.original"
            />
            <UButton v-else icon="i-lucide-archive-restore" color="neutral" variant="ghost" size="sm" aria-label="เปิดขายอีกครั้ง" @click="restore(row.original)" />
          </div>
        </template>
      </UTable>
    </UCard>

    <div v-if="hasMore" class="flex justify-center">
      <UButton color="neutral" variant="outline" label="โหลดเพิ่ม" :loading="loadingMore" @click="loadMore" />
    </div>

    <ProductFormModal v-model:open="formOpen" :shop-id="shop.id" :product="editing" @saved="onSaved" @images-changed="refresh()" />

    <ConfirmModal
      v-model:open="archiveOpen"
      title="ปิดการขายสินค้านี้?"
      :description="`“${archiveTarget?.name}” จะไม่แสดงในรายการขายและสั่งซื้อไม่ได้ ออเดอร์เก่ายังอยู่ครบ และเปิดขายอีกครั้งได้ภายหลัง`"
      confirm-label="ปิดการขาย"
      :loading="archiving"
      @confirm="confirmArchive"
    />
  </div>
</template>
