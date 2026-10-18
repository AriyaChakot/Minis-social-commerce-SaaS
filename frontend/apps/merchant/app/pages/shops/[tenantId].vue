<script setup lang="ts">
import type { TenantResponse } from '@mscs/shared'

// Parent route for /shops/:tenantId/*. Loads the shop (and checks membership) once,
// then renders the child page (products, orders...) in <NuxtPage />.
const route = useRoute()
const tenantId = computed(() => String(route.params.tenantId))

const { data, error } = await useFetch<TenantResponse>(() => `/api/tenants/${tenantId.value}`, {
  key: () => `shop-${tenantId.value}`,
})

if (error.value || !data.value) {
  // The API answers 404 for shops you are not a member of, so "not found" covers both cases.
  throw createError({ statusCode: 404, statusMessage: 'ไม่พบร้านนี้ หรือคุณไม่ได้เป็นสมาชิกของร้าน', fatal: true })
}

const shop = computed<ShopContext>(() => ({
  id: tenantId.value,
  tenant: data.value!.tenant,
  role: data.value!.role,
  canManage: data.value!.role === 'owner' || data.value!.role === 'admin',
}))
provideShop(shop)

useHead({ titleTemplate: t => (t ? `${t} · ${shop.value.tenant.name}` : shop.value.tenant.name) })

const SECTIONS = [
  { path: 'orders', label: 'ออเดอร์', icon: 'i-lucide-receipt' },
  { path: 'inbox', label: 'แชท', icon: 'i-lucide-message-circle' },
  { path: 'products', label: 'สินค้า', icon: 'i-lucide-package' },
  { path: 'settings', label: 'ตั้งค่า', icon: 'i-lucide-settings', managersOnly: true },
]

// `active` by path prefix, so "ออเดอร์" stays highlighted on /orders/new and /orders/:id too.
const tabs = computed(() => SECTIONS
  .filter(s => !s.managersOnly || shop.value.canManage)
  .map((s) => {
    const to = `/shops/${tenantId.value}/${s.path}`
    return { label: s.label, icon: s.icon, to, active: route.path.startsWith(to) }
  }))
</script>

<template>
  <div class="space-y-6">
    <div class="flex flex-wrap items-center justify-between gap-3">
      <div class="flex items-center gap-3">
        <UButton to="/" icon="i-lucide-arrow-left" color="neutral" variant="ghost" aria-label="กลับไปหน้าร้านของฉัน" />
        <div>
          <h1 class="text-xl font-semibold">
            {{ shop.tenant.name }}
          </h1>
          <p class="text-sm text-muted">
            mscs.shop/{{ shop.tenant.slug }}
          </p>
        </div>
      </div>
      <UBadge :color="ROLE_COLOR[shop.role]" variant="subtle" :label="ROLE_LABEL[shop.role]" />
    </div>

    <UNavigationMenu :items="tabs" class="border-b border-default" />

    <NuxtPage />
  </div>
</template>
