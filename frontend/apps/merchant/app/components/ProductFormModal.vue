<script setup lang="ts">
import { z } from 'zod'
import type { FormSubmitEvent } from '@nuxt/ui'
import type { CreateProductRequest, Product } from '@mscs/shared'
import { parseBahtToSatang, satangToBahtInput } from '@mscs/shared'

// One form for both "add product" (product = null) and "edit product".
const props = defineProps<{
  shopId: string
  product: Product | null
}>()
const open = defineModel<boolean>('open', { required: true })
const emit = defineEmits<{ saved: [product: Product] }>()

const schema = z.object({
  name: z.string().trim().min(1, 'กรุณากรอกชื่อสินค้า').max(200),
  sku: z.string().trim().max(64, 'ไม่เกิน 64 ตัวอักษร'),
  description: z.string().max(5000),
  // Typed as text in baht ("259.50") and converted to satang without floating point.
  price: z.string().refine(v => parseBahtToSatang(v) !== null, 'ราคาไม่ถูกต้อง (เช่น 259 หรือ 259.50)'),
  stock: z.number({ error: 'กรุณากรอกจำนวน' }).int().min(0, 'ต้องไม่ติดลบ'),
})
type Schema = z.output<typeof schema>

const state = reactive({ name: '', sku: '', description: '', price: '', stock: 0 })
const form = useTemplateRef('form')
const saving = ref(false)
const errorMessage = ref('')
const isEdit = computed(() => props.product !== null)

// Each time the modal opens, fill the form from the product being edited (or reset it).
watch(open, (isOpen) => {
  if (!isOpen) return
  const p = props.product
  Object.assign(state, {
    name: p?.name ?? '',
    sku: p?.sku ?? '',
    description: p?.description ?? '',
    price: p ? satangToBahtInput(p.price_satang) : '',
    stock: p?.stock ?? 0,
  })
  errorMessage.value = ''
})

async function onSubmit(event: FormSubmitEvent<Schema>) {
  const v = event.data
  const body: CreateProductRequest = {
    name: v.name,
    description: v.description,
    price_satang: parseBahtToSatang(v.price)!,
    stock: v.stock,
    // Empty SKU: leave it out (the API stores no SKU / keeps the existing one).
    ...(v.sku ? { sku: v.sku } : {}),
  }

  saving.value = true
  errorMessage.value = ''
  try {
    const saved = isEdit.value
      ? await $fetch<Product>(`/api/tenants/${props.shopId}/products/${props.product!.id}`, { method: 'PATCH', body })
      : await $fetch<Product>(`/api/tenants/${props.shopId}/products`, { method: 'POST', body })
    emit('saved', saved)
    open.value = false
  }
  catch (e) {
    if (apiError(e)?.code === 'sku_taken') {
      form.value?.setErrors([{ name: 'sku', message: 'รหัสสินค้านี้มีอยู่แล้วในร้าน' }])
    }
    else {
      errorMessage.value = apiErrorMessage(e)
    }
  }
  finally {
    saving.value = false
  }
}
</script>

<template>
  <UModal v-model:open="open" :title="isEdit ? 'แก้ไขสินค้า' : 'เพิ่มสินค้า'">
    <template #body>
      <UForm id="product-form" ref="form" :schema="schema" :state="state" class="space-y-4" @submit="onSubmit">
        <UFormField label="ชื่อสินค้า" name="name" required>
          <UInput v-model="state.name" class="w-full" />
        </UFormField>

        <div class="grid gap-4 sm:grid-cols-2">
          <UFormField label="ราคา (บาท)" name="price" required>
            <UInput v-model="state.price" inputmode="decimal" placeholder="259.00" class="w-full" />
          </UFormField>
          <UFormField label="จำนวนในสต็อก" name="stock" required>
            <UInputNumber v-model="state.stock" :min="0" class="w-full" />
          </UFormField>
        </div>

        <UFormField label="รหัสสินค้า (SKU)" name="sku" help="ไม่บังคับ ใช้ซ้ำกันภายในร้านไม่ได้">
          <UInput v-model="state.sku" placeholder="TEE-01" class="w-full" />
        </UFormField>

        <UFormField label="รายละเอียด" name="description">
          <UTextarea v-model="state.description" :rows="3" autoresize class="w-full" />
        </UFormField>

        <UAlert v-if="errorMessage" color="error" variant="subtle" icon="i-lucide-circle-alert" :title="errorMessage" />
      </UForm>
    </template>

    <template #footer>
      <div class="flex w-full justify-end gap-2">
        <UButton color="neutral" variant="ghost" label="ยกเลิก" :disabled="saving" @click="open = false" />
        <!-- form="product-form": this button lives outside the <form> but still submits it. -->
        <UButton type="submit" form="product-form" :loading="saving" :label="isEdit ? 'บันทึก' : 'เพิ่มสินค้า'" />
      </div>
    </template>
  </UModal>
</template>
