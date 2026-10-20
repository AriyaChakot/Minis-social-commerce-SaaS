<script setup lang="ts">
import type { NuxtError } from '#app'

const props = defineProps<{ error: NuxtError }>()
useHead({ title: props.error.statusCode === 404 ? 'ไม่พบหน้านี้' : 'เกิดข้อผิดพลาด' })

// clearError resets the error state, then navigates (a plain link would keep showing this page).
const goHome = () => clearError({ redirect: '/' })
</script>

<template>
  <UApp>
    <div class="min-h-dvh flex flex-col items-center justify-center gap-4 bg-muted px-4 text-center">
      <p class="text-5xl font-bold text-primary">
        {{ error.statusCode }}
      </p>
      <h1 class="text-xl font-semibold">
        {{ error.statusCode === 404 ? 'ไม่พบหน้าที่คุณต้องการ' : error.statusCode === 403 ? 'ไม่สามารถเข้าถึงได้' : 'เกิดข้อผิดพลาด' }}
      </h1>
      <p class="max-w-md text-muted">
        {{ error.statusMessage || error.message }}
      </p>
      <UButton icon="i-lucide-house" label="กลับหน้าแรก" @click="goHome" />
    </div>
  </UApp>
</template>
