<script setup lang="ts">
useHead({ title: 'ร้านของฉัน' })

// ROLE_LABEL / ROLE_COLOR come from app/utils/labels.ts (auto-imported).
const { user, memberships } = useAuth()
</script>

<template>
  <div class="space-y-6">
    <div>
      <h1 class="text-2xl font-semibold">
        สวีดัด {{ user?.display_name }} 👋
      </h1>
      <p class="text-muted">
        เลือกร้านที่ต้องการจัดการ
      </p>
    </div>

    <div v-if="memberships.length" class="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
      <UCard v-for="m in memberships" :key="m.tenant.id">
        <div class="flex items-start justify-between gap-2">
          <div class="min-w-0">
            <p class="truncate font-semibold">
              {{ m.tenant.name }}
            </p>
            <p class="truncate text-sm text-muted">
              mscs.shop/{{ m.tenant.slug }}
            </p>
          </div>
          <UBadge :color="ROLE_COLOR[m.role]" variant="subtle" :label="ROLE_LABEL[m.role]" />
        </div>
        <UAlert
          v-if="m.tenant.status === 'suspended'"
          class="mt-3"
          color="error"
          variant="subtle"
          icon="i-lucide-ban"
          title="ร้านถูกระงับโดยผู้ดูแลระบบ"
          :description="m.tenant.suspended_reason || undefined"
        />

        <template #footer>
          <UButton v-if="m.tenant.status === 'suspended'" block disabled color="neutral" variant="subtle" label="ใช้งานไม่ได้ระหว่างถูกระงับ" />
          <UButton v-else block :to="`/shops/${m.tenant.id}`" trailing-icon="i-lucide-arrow-right" label="จัดการร้าน" />
        </template>
      </UCard>
    </div>

    <UAlert
      v-else
      color="neutral"
      variant="subtle"
      icon="i-lucide-info"
      title="คุณยังไม่ได้เป็นสมาชิกร้านใด"
      description="ขอให้เจ้าของร้านเพิ่มคุณเป็นสมาชิก"
    />
  </div>
</template>
