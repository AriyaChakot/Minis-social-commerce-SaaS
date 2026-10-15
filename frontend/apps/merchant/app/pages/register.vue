<script setup lang="ts">
import { z } from 'zod'
import type { FormSubmitEvent } from '@nuxt/ui'

definePageMeta({ layout: 'auth' })
useHead({ title: 'สมัครเปิดร้าน' })

// Mirrors the Go API rules (handler binding tags + the tenants_slug_format constraint).
const schema = z.object({
  display_name: z.string().trim().min(1, 'กรุณากรอกชื่อของคุณ').max(100),
  email: z.email('กรุณากรอกอีเมลให้ถูกต้อง').max(254),
  password: z.string().min(8, 'รหัสผ่านอย่างน้อย 8 ตัวอักษร').max(128),
  shop_name: z.string().trim().min(1, 'กรุณากรอกชื่อร้าน').max(100),
  shop_slug: z
    .string()
    .min(3, 'อย่างน้อย 3 ตัวอักษร')
    .max(63)
    .regex(/^[a-z0-9]+(-[a-z0-9]+)*$/, 'ใช้ได้เฉพาะ a-z, 0-9 และขีด - คั่นกลาง'),
})
type Schema = z.output<typeof schema>

const state = reactive({ display_name: '', email: '', password: '', shop_name: '', shop_slug: '' })
const loading = ref(false)
const errorMessage = ref('')
const form = useTemplateRef('form')
const { register } = useAuth()

// Until Vue has hydrated the server-rendered HTML, a click would submit the <form> natively
// (a full page reload that loses the input). Keep the button disabled until then.
const hydrated = ref(false)
onMounted(() => { hydrated.value = true })

// Suggest a slug from an English shop name until the user edits the slug themselves.
// (Thai names have no a-z characters, so the user types the slug in that case.)
const slugEdited = ref(false)
watch(() => state.shop_name, (name) => {
  if (slugEdited.value) return
  state.shop_slug = name.toLowerCase().replace(/[^a-z0-9]+/g, '-').replace(/^-+|-+$/g, '').slice(0, 63)
})

// API field names -> where to show the message on this form.
const FIELD_MESSAGES: Record<string, string> = {
  email_taken: 'อีเมลนี้ถูกใช้สมัครแล้ว',
  slug_taken: 'ลิงก์ร้านนี้มีร้านอื่นใช้แล้ว',
}

async function onSubmit(event: FormSubmitEvent<Schema>) {
  loading.value = true
  errorMessage.value = ''
  try {
    await register(event.data)
    await navigateTo('/')
  }
  catch (e) {
    const code = apiError(e)?.code
    if (code === 'email_taken' || code === 'slug_taken') {
      // Show the conflict under the field it concerns, not as a generic banner.
      form.value?.setErrors([{ name: code === 'email_taken' ? 'email' : 'shop_slug', message: FIELD_MESSAGES[code]! }])
    }
    else if (apiFieldErrors(e).length > 0) {
      form.value?.setErrors(apiFieldErrors(e).map(f => ({ name: f.field, message: 'ข้อมูลไม่ถูกต้อง' })))
    }
    else {
      errorMessage.value = apiErrorMessage(e)
    }
  }
  finally {
    loading.value = false
  }
}
</script>

<template>
  <UCard class="w-full max-w-md">
    <template #header>
      <h1 class="text-xl font-semibold">
        สมัครเปิดร้าน
      </h1>
      <p class="text-sm text-muted">
        สร้างบัญชีและร้านแรกของคุณในขั้นตอนเดียว
      </p>
    </template>

    <UForm ref="form" :schema="schema" :state="state" class="space-y-4" @submit="onSubmit">
      <UFormField label="ชื่อของคุณ" name="display_name">
        <UInput v-model="state.display_name" autocomplete="name" class="w-full" />
      </UFormField>

      <UFormField label="อีเมล" name="email">
        <UInput v-model="state.email" type="email" autocomplete="email" class="w-full" />
      </UFormField>

      <UFormField label="รหัสผ่าน" name="password" help="อย่างน้อย 8 ตัวอักษร">
        <UInput v-model="state.password" type="password" autocomplete="new-password" class="w-full" />
      </UFormField>

      <USeparator label="ข้อมูลร้าน" />

      <UFormField label="ชื่อร้าน" name="shop_name">
        <UInput v-model="state.shop_name" placeholder="เช่น ร้านเสื้อผ้าแม่มณี" class="w-full" />
      </UFormField>

      <UFormField label="ลิงก์ร้าน" name="shop_slug" help="ใช้เป็นที่อยู่หน้าร้านออนไลน์ เปลี่ยนภายหลังไม่ได้">
        <UInput
          v-model="state.shop_slug"
          placeholder="mamanee-shop"
          class="w-full"
          :ui="{ base: 'ps-22' }"
          @update:model-value="slugEdited = true"
        >
          <template #leading>
            <span class="text-sm text-muted">mscs.shop/</span>
          </template>
        </UInput>
      </UFormField>

      <UAlert v-if="errorMessage" color="error" variant="subtle" icon="i-lucide-circle-alert" :title="errorMessage" />

      <UButton type="submit" block :loading="loading" :disabled="!hydrated" label="สมัครและเปิดร้าน" />
    </UForm>

    <template #footer>
      <p class="text-center text-sm text-muted">
        มีบัญชีอยู่แล้ว?
        <NuxtLink to="/login" class="font-medium text-primary">
          เข้าสู่ระบบ
        </NuxtLink>
      </p>
    </template>
  </UCard>
</template>
