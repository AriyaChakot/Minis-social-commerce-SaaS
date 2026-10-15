<script setup lang="ts">
import type { Role } from '@mscs/shared'

useHead({ title: 'ร้านของฉัน' })

const { user, memberships } = useAuth()

const ROLE_LABEL: Record<Role, string> = { owner: 'เจ้าของ', admin: 'ผู้ดูแล', staff: 'พนักงาน' }
const ROLE_COLOR: Record<Role, 'primary' | 'info' | 'neutral'> = { owner: 'primary', admin: 'info', staff: 'neutral' }
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

        <template #footer>
          <!-- Product and order pages for a shop arrive in step 9b. -->
          <UButton block disabled color="neutral" variant="soft" icon="i-lucide-arrow-right" label="จัดการร้าน (เร็ว ๆ นี้)" />
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
