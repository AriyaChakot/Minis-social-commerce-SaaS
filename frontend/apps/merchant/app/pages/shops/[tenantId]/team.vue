<script setup lang="ts">
import { z } from 'zod'
import type { FormSubmitEvent } from '@nuxt/ui'
import type { CreateInvitationResponse, TeamMember, TeamResponse } from '@mscs/shared'
import { formatDateTime } from '@mscs/shared'

useHead({ title: 'ทีมงาน' })

const shop = useShop()
const toast = useToast()

// Staff reach this page only by typing the URL; the API answers 403 for them anyway.
if (!shop.value.canManage) {
  throw createError({ statusCode: 403, statusMessage: 'เฉพาะเจ้าของร้านและผู้ดูแลเท่านั้น' })
}

const base = computed(() => `/api/tenants/${shop.value.id}`)
const { data, refresh } = await useFetch<TeamResponse>(() => `${base.value}/team`, { key: () => `team-${shop.value.id}` })
const members = computed(() => data.value?.members ?? [])
const invitations = computed(() => data.value?.invitations ?? [])

// Same rule as the API: owners manage admins and staff, admins manage staff.
const isOwner = computed(() => shop.value.role === 'owner')
const canManage = (role: TeamMember['role']) => role === 'staff' || (role === 'admin' && isOwner.value)

// ---- Invite -----------------------------------------------------------------------------
const roleOptions = computed(() => [
  { label: 'พนักงาน — จัดการออเดอร์และแชท', value: 'staff' },
  ...(isOwner.value ? [{ label: 'ผู้ดูแล — จัดการสินค้า ทีมงาน และตั้งค่าได้ด้วย', value: 'admin' }] : []),
])
const schema = z.object({
  email: z.email('กรุณากรอกอีเมลให้ถูกต้อง'),
  role: z.enum(['staff', 'admin']),
})
const state = reactive<{ email: string, role: 'staff' | 'admin' }>({ email: '', role: 'staff' })
const inviting = ref(false)
// The link is shown once: the API keeps only a hash of its token.
const newLink = ref<{ email: string, url: string } | null>(null)
const copied = ref(false)

async function onInvite(event: FormSubmitEvent<z.output<typeof schema>>) {
  inviting.value = true
  try {
    const res = await $fetch<CreateInvitationResponse>(`${base.value}/invitations`, { method: 'POST', body: event.data })
    newLink.value = { email: res.invitation.email, url: `${window.location.origin}/invite/${res.token}` }
    copied.value = false
    state.email = ''
    await refresh()
  }
  catch (e) {
    toast.add({ title: apiErrorMessage(e), color: 'error' })
  }
  finally {
    inviting.value = false
  }
}

// Until Vue has hydrated the server-rendered page, clicking the button would submit the <form>
// natively (a full reload that loses the input): keep it disabled until then.
const hydrated = ref(false)
onMounted(() => { hydrated.value = true })

async function copyLink() {
  if (!newLink.value) return
  await navigator.clipboard.writeText(newLink.value.url)
  copied.value = true
}

// ---- Revoke / remove (both confirmed first) -------------------------------------------
const pending = ref<{ title: string, description: string, label: string, run: () => Promise<unknown> } | null>(null)
const confirmOpen = computed({ get: () => pending.value !== null, set: (v) => { if (!v) pending.value = null } })
const working = ref(false)

function askRevoke(id: string, email: string) {
  pending.value = {
    title: 'ยกเลิกคำเชิญนี้?',
    description: `ลิงก์ที่ส่งให้ ${email} จะใช้ไม่ได้อีก (เชิญใหม่ได้ภายหลัง)`,
    label: 'ยกเลิกคำเชิญ',
    run: () => $fetch(`${base.value}/invitations/${id}`, { method: 'DELETE' }),
  }
}
function askRemove(m: TeamMember) {
  pending.value = {
    title: `นำ ${m.display_name} ออกจากร้าน?`,
    description: `${m.email} จะเข้าหลังร้านนี้ไม่ได้ทันที (บัญชีของเขายังอยู่ เชิญกลับมาได้)`,
    label: 'นำออกจากร้าน',
    run: () => $fetch(`${base.value}/members/${m.user_id}`, { method: 'DELETE' }),
  }
}
async function confirm() {
  if (!pending.value) return
  working.value = true
  try {
    await pending.value.run()
    pending.value = null
    await refresh()
  }
  catch (e) {
    toast.add({ title: apiErrorMessage(e), color: 'error' })
  }
  finally {
    working.value = false
  }
}
</script>

<template>
  <div class="max-w-3xl space-y-6">
    <UCard>
      <template #header>
        <h2 class="font-semibold">
          เชิญคนเข้าร่วมร้าน
        </h2>
        <p class="text-sm text-muted">
          ระบบจะสร้างลิงก์ให้ คุณส่งลิงก์นั้นให้เขาทาง LINE หรือแชทได้เลย
        </p>
      </template>

      <UForm :schema="schema" :state="state" class="grid gap-3 sm:grid-cols-[1fr_16rem_auto] sm:items-start" @submit="onInvite">
        <UFormField name="email" label="อีเมลของคนที่จะเชิญ">
          <UInput v-model="state.email" type="email" autocomplete="off" placeholder="name@example.com" class="w-full" />
        </UFormField>
        <UFormField name="role" label="บทบาท">
          <USelect v-model="state.role" :items="roleOptions" class="w-full" />
        </UFormField>
        <UButton type="submit" :loading="inviting" :disabled="!hydrated" icon="i-lucide-user-plus" label="สร้างลิงก์เชิญ" class="sm:mt-6" />
      </UForm>

      <div v-if="newLink" class="mt-4 space-y-2 rounded-lg bg-elevated p-4 text-sm">
        <p class="font-medium">
          ลิงก์เชิญสำหรับ {{ newLink.email }}
        </p>
        <div class="flex gap-2">
          <UInput :model-value="newLink.url" readonly class="flex-1 font-mono" aria-label="ลิงก์เชิญ" />
          <UButton :icon="copied ? 'i-lucide-check' : 'i-lucide-copy'" :label="copied ? 'คัดลอกแล้ว' : 'คัดลอก'" color="neutral" variant="outline" @click="copyLink" />
        </div>
        <p class="text-xs text-muted">
          ใช้ได้ครั้งเดียว หมดอายุใน 7 วัน และ<strong>จะไม่แสดงอีก</strong> ถ้าหาย ให้เชิญอีเมลเดิมใหม่ (ลิงก์เก่าจะใช้ไม่ได้)
        </p>
      </div>
    </UCard>

    <UCard :ui="{ body: 'p-0 sm:p-0' }">
      <template #header>
        <h2 class="font-semibold">
          สมาชิก ({{ members.length }})
        </h2>
      </template>
      <ul class="divide-y divide-default">
        <li v-for="m in members" :key="m.user_id" class="flex items-center gap-3 px-4 py-3">
          <UAvatar :alt="m.display_name" size="md" />
          <div class="min-w-0 flex-1">
            <p class="truncate font-medium">
              {{ m.display_name }} <span v-if="m.user_id === data?.you" class="text-sm font-normal text-muted">(คุณ)</span>
            </p>
            <p class="truncate text-sm text-muted">
              {{ m.email }}
            </p>
          </div>
          <UBadge :label="ROLE_LABEL[m.role]" :color="ROLE_COLOR[m.role]" variant="subtle" />
          <UButton
            v-if="m.user_id !== data?.you && canManage(m.role)"
            icon="i-lucide-user-minus"
            color="neutral"
            variant="ghost"
            :aria-label="`นำ ${m.display_name} ออกจากร้าน`"
            @click="askRemove(m)"
          />
        </li>
      </ul>
    </UCard>

    <UCard v-if="invitations.length" :ui="{ body: 'p-0 sm:p-0' }">
      <template #header>
        <h2 class="font-semibold">
          คำเชิญที่รอการตอบรับ
        </h2>
      </template>
      <ul class="divide-y divide-default">
        <li v-for="i in invitations" :key="i.id" class="flex items-center gap-3 px-4 py-3">
          <UIcon name="i-lucide-mail" class="size-5 shrink-0 text-muted" />
          <div class="min-w-0 flex-1">
            <p class="truncate">
              {{ i.email }}
            </p>
            <p class="text-xs text-muted">
              หมดอายุ {{ formatDateTime(i.expires_at) }}
            </p>
          </div>
          <UBadge :label="ROLE_LABEL[i.role]" :color="ROLE_COLOR[i.role]" variant="subtle" />
          <UButton
            v-if="canManage(i.role)"
            icon="i-lucide-x"
            color="neutral"
            variant="ghost"
            :aria-label="`ยกเลิกคำเชิญของ ${i.email}`"
            @click="askRevoke(i.id, i.email)"
          />
        </li>
      </ul>
    </UCard>

    <ConfirmModal
      v-model:open="confirmOpen"
      :title="pending?.title ?? ''"
      :description="pending?.description"
      :confirm-label="pending?.label"
      danger
      :loading="working"
      @confirm="confirm"
    />
  </div>
</template>
