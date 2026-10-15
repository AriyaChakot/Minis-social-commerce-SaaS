<script lang="ts">
  import { page } from '$app/state'
</script>

<svelte:head>
  <title>{page.status === 404 ? 'ไม่พบหน้านี้' : 'เกิดข้อผิดพลาด'}</title>
</svelte:head>

<main class="mx-auto flex min-h-dvh max-w-md flex-col items-center justify-center gap-3 px-6 text-center">
  <p class="text-5xl font-bold text-amber-600">{page.status}</p>
  <h1 class="text-xl font-semibold">{page.error?.message ?? 'เกิดข้อผิดพลาด'}</h1>
  <p class="text-stone-500">ตรวจสอบลิงก์อีกครั้ง หรือติดต่อร้านค้าทางแชท</p>
</main>
