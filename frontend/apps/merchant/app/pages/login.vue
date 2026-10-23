<script setup lang="ts">
import { z } from 'zod'
import type { FormSubmitEvent } from '@nuxt/ui'

definePageMeta({ layout: 'auth' })
useHead({ title: 'เข้าสู่ระบบ' })

// The same schema validates in the browser (instant feedback) and types the submitted data.
// The Go API still validates again: the browser can always be bypassed.
const schema = z.object({
  email: z.email('กรุณากรอกอีเมลให้ถูกต้อง'),
  password: z.string().min(1, 'กรุณากรอกรหัสผ่าน'),
})
type Schema = z.output<typeof schema>

const state = reactive({ email: '', password: '' })
const loading = ref(false)
const errorMessage = ref('')
const { login } = useAuth()
const route = useRoute()

// Until Vue has hydrated the server-rendered HTML, a click would submit the <form> natively
// (a full page reload that loses the input). Keep the button disabled until then.
const hydrated = ref(false)
onMounted(() => { hydrated.value = true })

async function onSubmit(event: FormSubmitEvent<Schema>) {
  loading.value = true
  errorMessage.value = ''
  try {
    await login(event.data)
    // Back to where they were going (e.g. an invitation link), or the dashboard.
    await navigateTo(safeRedirect(route.query.redirect))
  }
  catch (e) {
    errorMessage.value = apiErrorMessage(e)
  }
  finally {
    loading.value = false
  }
}
</script>

<template>
  <UCard class="w-full max-w-sm">
    <template #header>
      <h1 class="text-xl font-semibold">
        เข้าสู่ระบบ
      </h1>
      <p class="text-sm text-muted">
        จัดการสินค้าและออเดอร์ของร้านคุณ
      </p>
    </template>

    <UForm :schema="schema" :state="state" class="space-y-4" @submit="onSubmit">
      <UFormField label="อีเมล" name="email">
        <UInput v-model="state.email" type="email" autocomplete="email" class="w-full" />
      </UFormField>

      <UFormField label="รหัสผ่าน" name="password">
        <UInput v-model="state.password" type="password" autocomplete="current-password" class="w-full" />
      </UFormField>

      <UAlert v-if="errorMessage" color="error" variant="subtle" icon="i-lucide-circle-alert" :title="errorMessage" />

      <UButton type="submit" block :loading="loading" :disabled="!hydrated" label="เข้าสู่ระบบ" />
    </UForm>

    <template #footer>
      <p class="text-center text-sm text-muted">
        ยังไม่มีบัญชี?
        <NuxtLink to="/register" class="font-medium text-primary">
          สมัครเปิดร้าน
        </NuxtLink>
      </p>
    </template>
  </UCard>
</template>
