<script setup lang="ts">
// A yes/no dialog for actions that are hard to undo (archive a product, cancel an order).
// Usage: <ConfirmModal v-model:open="open" title="..." :loading="busy" @confirm="doIt" />
withDefaults(defineProps<{
  title: string
  description?: string
  confirmLabel?: string
  danger?: boolean
  loading?: boolean
}>(), {
  description: undefined,
  confirmLabel: 'ยืนยัน',
  danger: false,
  loading: false,
})

const open = defineModel<boolean>('open', { required: true })
const emit = defineEmits<{ confirm: [] }>()
</script>

<template>
  <UModal v-model:open="open" :title="title" :description="description">
    <template #footer>
      <div class="flex w-full justify-end gap-2">
        <UButton color="neutral" variant="ghost" label="ไม่ใช่" :disabled="loading" @click="open = false" />
        <UButton :color="danger ? 'error' : 'primary'" :label="confirmLabel" :loading="loading" @click="emit('confirm')" />
      </div>
    </template>
  </UModal>
</template>
