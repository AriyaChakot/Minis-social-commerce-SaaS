<template>
  <!-- Centered card layout for the login and register pages. -->
  <div class="min-h-dvh flex flex-col items-center justify-center gap-6 bg-muted px-4 py-10">
    <div class="flex items-center gap-2 text-lg font-semibold">
      <UIcon name="i-lucide-store" class="size-6 text-primary" />
      MSCS ร้านค้า
    </div>
    <slot />
  </div>
</template>
