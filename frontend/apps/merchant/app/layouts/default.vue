<script setup lang="ts">
const { user, logout } = useAuth()
const loggingOut = ref(false)

async function onLogout() {
  loggingOut.value = true
  try {
    await logout()
  }
  finally {
    loggingOut.value = false
  }
}
</script>

<template>
  <div class="min-h-dvh flex flex-col">
    <header class="border-b border-default bg-default">
      <UContainer class="flex h-14 items-center justify-between gap-4">
        <NuxtLink to="/" class="flex items-center gap-2 font-semibold">
          <UIcon name="i-lucide-store" class="size-5 text-primary" />
          MSCS ร้านค้า
        </NuxtLink>

        <div v-if="user" class="flex items-center gap-3">
          <div class="hidden text-right text-sm sm:block">
            <p class="font-medium leading-tight">{{ user.display_name }}</p>
            <p class="text-muted leading-tight">{{ user.email }}</p>
          </div>
          <UButton
            icon="i-lucide-log-out"
            color="neutral"
            variant="ghost"
            label="ออกจากระบบ"
            :loading="loggingOut"
            @click="onLogout"
          />
        </div>
      </UContainer>
    </header>

    <main class="flex-1 bg-muted/40">
      <UContainer class="py-8">
        <slot />
      </UContainer>
    </main>
  </div>
</template>
