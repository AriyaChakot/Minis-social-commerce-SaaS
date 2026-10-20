<script setup lang="ts">
import type { Product } from '@mscs/shared'

// Photos of one product: upload, make cover, delete. Every change returns the updated product.
const props = defineProps<{ shopId: string, product: Product }>()
const emit = defineEmits<{ changed: [product: Product] }>()
const toast = useToast()

const MAX_IMAGES = 5
const MAX_BYTES = 15 * 1024 * 1024
const ACCEPT = ['image/jpeg', 'image/png', 'image/webp']

const base = computed(() => `/api/tenants/${props.shopId}/products/${props.product.id}/images`)
const busy = ref(false)
const uploading = ref(0) // photos still to upload, for the progress text
const input = useTemplateRef('input')
const room = computed(() => MAX_IMAGES - props.product.images.length)

// Quick checks in the browser save a slow upload that would be refused anyway. The API checks
// again (and decodes the image): the browser's checks are a convenience, never the protection.
function precheck(file: File): string | null {
  if (!ACCEPT.includes(file.type)) return `“${file.name}” ไม่ใช่รูป JPEG, PNG หรือ WebP`
  if (file.size > MAX_BYTES) return `“${file.name}” ใหญ่เกิน 15 MB`
  return null
}

async function onPick(event: Event) {
  const files = [...((event.target as HTMLInputElement).files ?? [])]
  ;(event.target as HTMLInputElement).value = '' // picking the same file again fires change again
  if (files.length > room.value) {
    toast.add({ title: `เพิ่มได้อีก ${room.value} รูป (สูงสุด ${MAX_IMAGES} รูปต่อสินค้า)`, color: 'warning' })
    files.length = room.value
  }

  busy.value = true
  uploading.value = files.length
  try {
    // One at a time: the order the merchant picked is the gallery order.
    for (const file of files) {
      const problem = precheck(file)
      if (problem) {
        toast.add({ title: problem, color: 'error' })
      }
      else {
        const form = new FormData()
        form.append('file', file)
        try {
          emit('changed', await $fetch<Product>(base.value, { method: 'POST', body: form }))
        }
        catch (e) {
          toast.add({ title: `อัปโหลด “${file.name}” ไม่สำเร็จ: ${apiErrorMessage(e)}`, color: 'error' })
        }
      }
      uploading.value--
    }
  }
  finally {
    busy.value = false
  }
}

async function run(path: string, method: 'POST' | 'DELETE') {
  busy.value = true
  try {
    emit('changed', await $fetch<Product>(`${base.value}/${path}`, { method }))
  }
  catch (e) {
    toast.add({ title: apiErrorMessage(e), color: 'error' })
  }
  finally {
    busy.value = false
  }
}
</script>

<template>
  <div class="space-y-2">
    <div class="flex items-baseline justify-between">
      <p class="text-sm font-medium">
        รูปสินค้า <span class="font-normal text-muted">({{ product.images.length }}/{{ MAX_IMAGES }})</span>
      </p>
      <p class="text-xs text-muted">
        รูปแรกคือรูปปก
      </p>
    </div>

    <ul class="grid grid-cols-3 gap-2 sm:grid-cols-5">
      <li v-for="(img, i) in product.images" :key="img.id" class="group relative aspect-square overflow-hidden rounded-md ring ring-default">
        <img :src="img.thumb_url" :alt="`${product.name} รูปที่ ${i + 1}`" class="size-full object-cover" loading="lazy">
        <UBadge v-if="i === 0" label="ปก" size="sm" class="absolute left-1 top-1" />
        <!-- Always visible on touch screens; on hover elsewhere. -->
        <div class="absolute inset-x-0 bottom-0 flex justify-end gap-1 bg-black/40 p-1 sm:opacity-0 sm:transition-opacity sm:group-hover:opacity-100 sm:group-focus-within:opacity-100">
          <UButton
            v-if="i > 0"
            icon="i-lucide-star"
            size="xs"
            color="neutral"
            :disabled="busy"
            aria-label="ตั้งเป็นรูปปก"
            @click="run(`${img.id}/cover`, 'POST')"
          />
          <UButton
            icon="i-lucide-trash-2"
            size="xs"
            color="error"
            :disabled="busy"
            aria-label="ลบรูปนี้"
            @click="run(img.id, 'DELETE')"
          />
        </div>
      </li>

      <li v-if="room > 0" class="aspect-square">
        <button
          type="button"
          :disabled="busy"
          class="flex size-full flex-col items-center justify-center gap-1 rounded-md border-2 border-dashed border-default text-xs text-muted hover:border-primary hover:text-primary disabled:opacity-60"
          @click="input?.click()"
        >
          <UIcon :name="uploading ? 'i-lucide-loader-circle' : 'i-lucide-image-plus'" class="size-6" :class="uploading ? 'animate-spin' : ''" />
          {{ uploading ? `กำลังอัปโหลด (${uploading})` : 'เพิ่มรูป' }}
        </button>
      </li>
    </ul>
    <input ref="input" type="file" :accept="ACCEPT.join(',')" multiple class="hidden" @change="onPick">
    <p class="text-xs text-muted">
      JPEG, PNG หรือ WebP ไม่เกิน 15 MB ระบบย่อรูปและลบข้อมูลตำแหน่ง (GPS) ในรูปให้อัตโนมัติ
    </p>
  </div>
</template>
