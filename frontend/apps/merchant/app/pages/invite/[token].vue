<script setup lang="ts">
import { z } from 'zod'
import type { FormSubmitEvent } from '@nuxt/ui'
import type { InvitationPreview } from '@mscs/shared'
import { formatDateTime } from '@mscs/shared'

// The page an invitation link opens. Open to everyone (see middleware/auth.global.ts):
//   - logged out: create an account for the invited email, or log in and come back;
//   - logged in with the invited email: one click to join;
//   - logged in with another account: say so, and offer to log out.
definePageMeta({ layout: 'auth' })
useHead({ title: 'คำเชิญเข้าร่วมร้าน' })

const route = useRoute()
const token = computed(() => String(route.params.token))
const { user, refresh: refreshAuth, logout } = useAuth()

const { data, error } = await useFetch<{ invitation: InvitationPreview }>(() => `/api/invitations/${encodeURIComponent(token.value)}`)
const invitation = computed(() => data.value?.invitation ?? null)
const sameAccount = computed(() => !!user.value && user.value.email === invitation.value?.email)
const loginLink = computed(() => ({ path: '/login', query: { redirect: route.fullPath } }))

const joining = ref(false)
const errorMessage = ref('')

async function goToShop() {
  await refreshAuth() // the new membership appears in the shop list
  await navigateTo(`/shops/${invitation.value!.tenant_id}`)
}

async function accept() {
  joining.value = true
  errorMessage.value = ''
  try {
    await $fetch(`/api/invitations/${encodeURIComponent(token.value)}/accept`, { method: 'POST' })
    await goToShop()
  }
  catch (e) {
    errorMessage.value = apiErrorMessage(e)
  }
  finally {
    joining.value = false
  }
}

// New account: the email is the invitation's (shown, not editable).
const schema = z.object({
  display_name: z.string().trim().min(1, 'กรุณากรอกชื่อ').max(100),
  password: z.string().min(8, 'อย่างน้อย 8 ตัวอักษร').max(128),
})
const state = reactive({ display_name: '', password: '' })

async function onRegister(event: FormSubmitEvent<z.output<typeof schema>>) {
  joining.value = true
  errorMessage.value = ''
  try {
    await $fetch(`/api/invitations/${encodeURIComponent(token.value)}/register`, { method: 'POST', body: event.data })
    await goToShop()
  }
  catch (e) {
    errorMessage.value = apiErrorMessage(e)
  }
  finally {
    joining.value = false
  }
}

// Same as the login page: no native form submit before hydration.
const hydrated = ref(false)
onMounted(() => { hydrated.value = true })
</script>

<template>
  <UCard class="w-full max-w-md">
    <!-- Invalid, expired, used, or the shop is suspended -->
    <div v-if="error || !invitation" class="space-y-4 text-center">
      <UIcon name="i-lucide-link-2-off" class="mx-auto size-10 text-muted" />
      <p class="font-semibold">
        {{ apiErrorMessage(error) }}
      </p>
      <UButton to="/" label="ไปหน้าแรก" color="neutral" variant="outline" />
    </div>

    <div v-else class="space-y-5">
      <div class="text-center">
        <p class="text-sm text-muted">
          คุณได้รับเชิญให้เข้าร่วมร้าน
        </p>
        <h1 class="text-xl font-semibold">
          {{ invitation.tenant_name }}
        </h1>
        <p class="mt-1 text-sm">
          ในบทบาท <UBadge :label="ROLE_LABEL[invitation.role]" :color="ROLE_COLOR[invitation.role]" variant="subtle" />
        </p>
        <p class="mt-1 text-xs text-muted">
          สำหรับ {{ invitation.email }} · ใช้ได้ถึง {{ formatDateTime(invitation.expires_at) }}
        </p>
      </div>

      <UAlert v-if="errorMessage" color="error" variant="subtle" icon="i-lucide-circle-alert" :title="errorMessage" />

      <!-- Logged in with the invited account -->
      <UButton v-if="sameAccount" block size="lg" :loading="joining" :disabled="!hydrated" label="เข้าร่วมร้าน" @click="accept" />

      <!-- Logged in with another account -->
      <div v-else-if="user" class="space-y-3 text-sm">
        <UAlert
          color="warning"
          variant="subtle"
          icon="i-lucide-user-x"
          title="คำเชิญนี้ส่งถึงอีเมลอื่น"
          :description="`คุณเข้าสู่ระบบเป็น ${user.email} ออกจากระบบแล้วเข้าด้วย ${invitation.email} เพื่อรับคำเชิญ`"
        />
        <UButton block color="neutral" variant="outline" label="ออกจากระบบ" @click="logout" />
      </div>

      <!-- Logged out: new account, or log in and come back here -->
      <template v-else>
        <UForm :schema="schema" :state="state" class="space-y-4" @submit="onRegister">
          <UFormField label="อีเมล">
            <UInput :model-value="invitation.email" disabled class="w-full" />
          </UFormField>
          <UFormField label="ชื่อที่จะแสดง" name="display_name">
            <UInput v-model="state.display_name" autocomplete="name" class="w-full" />
          </UFormField>
          <UFormField label="ตั้งรหัสผ่าน" name="password">
            <UInput v-model="state.password" type="password" autocomplete="new-password" class="w-full" />
          </UFormField>
          <UButton type="submit" block :loading="joining" :disabled="!hydrated" label="สร้างบัญชีและเข้าร่วมร้าน" />
        </UForm>
        <p class="text-center text-sm text-muted">
          มีบัญชีอยู่แล้ว?
          <ULink :to="loginLink" class="text-primary">
            เข้าสู่ระบบเพื่อรับคำเชิญ
          </ULink>
        </p>
      </template>
    </div>
  </UCard>
</template>
