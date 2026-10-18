<script setup lang="ts">
import type { Conversation, ConversationThread, LineChannel } from '@mscs/shared'
import { formatDateTime, formatShortTime } from '@mscs/shared'

useHead({ title: 'แชท' })

const shop = useShop()
const route = useRoute()
const router = useRouter()

// The open conversation lives in the URL (?c=<id>): reload, back button and links all work.
const selectedId = computed(() => (typeof route.query.c === 'string' ? route.query.c : null))
function open(id: string | null) {
  router.replace({ query: id ? { c: id } : {} })
}

const base = computed(() => `/api/tenants/${shop.value.id}`)

const { data: list, refresh: refreshList } = await useFetch<{ items: Conversation[] }>(() => `${base.value}/conversations`, {
  key: () => `conversations-${shop.value.id}`,
})
const conversations = computed(() => list.value?.items ?? [])

// Only to explain an empty inbox: "connect LINE first" vs "no messages yet".
const { data: channel } = await useFetch<{ line: LineChannel | null }>(() => `${base.value}/channels/line`, {
  key: () => `line-channel-${shop.value.id}`,
})

// useRequestFetch, not plain $fetch: during server rendering (opening a ?c= link or reloading)
// it forwards the browser's session cookie to our /api proxy; plain $fetch would call it
// without the cookie and get a 401. (useFetch does this by itself.)
const requestFetch = useRequestFetch()
const { data: thread, refresh: refreshThread, error: threadError } = await useAsyncData(
  () => `thread-${shop.value.id}-${selectedId.value}`,
  () => (selectedId.value
    ? requestFetch<ConversationThread>(`${base.value}/conversations/${selectedId.value}/messages`)
    : Promise.resolve(null)),
  { watch: [selectedId] },
)

// New messages arrive by polling every 5 seconds (both the list and the open conversation).
usePolling(() => Promise.all([refreshList(), selectedId.value ? refreshThread() : null]), 5000)

// Opening a conversation (or new messages arriving in the open one) marks it as read.
watch(() => thread.value?.conversation, async (conv) => {
  if (!conv || conv.unread_count === 0) return
  await $fetch(`${base.value}/conversations/${conv.id}/read`, { method: 'POST' }).catch(() => {})
  refreshList()
}, { immediate: true })

// Keep the newest message in view when messages are added.
const scroller = ref<HTMLElement | null>(null)
watch(() => thread.value?.messages.length, async () => {
  await nextTick()
  scroller.value?.scrollTo({ top: scroller.value.scrollHeight })
}, { immediate: true })

// "Create order from this chat": the order form opens with the customer and channel filled in.
const orderLink = computed(() => {
  const conv = thread.value?.conversation
  if (!conv) return ''
  const query = new URLSearchParams({ channel: 'line', customer_name: conversationName(conv) })
  return `/shops/${shop.value.id}/orders/new?${query}`
})
</script>

<template>
  <div class="grid gap-4 md:grid-cols-[20rem_1fr]">
    <!-- Conversation list (on phones: hidden while a conversation is open) -->
    <UCard :class="selectedId ? 'hidden md:block' : ''" :ui="{ body: 'p-0 sm:p-0' }">
      <div v-if="conversations.length === 0" class="space-y-3 p-6 text-center text-sm text-muted">
        <UIcon name="i-lucide-message-circle" class="mx-auto size-8" />
        <template v-if="channel && !channel.line">
          <p>ยังไม่ได้เชื่อมต่อ LINE Official Account</p>
          <UButton v-if="shop.canManage" :to="`/shops/${shop.id}/settings`" label="ไปที่หน้าตั้งค่า" size="sm" />
          <p v-else>
            ให้เจ้าของร้านหรือผู้ดูแลเชื่อมต่อที่หน้าตั้งค่า
          </p>
        </template>
        <p v-else>
          ยังไม่มีข้อความจากลูกค้า
        </p>
      </div>

      <ul v-else class="max-h-[70vh] divide-y divide-default overflow-y-auto">
        <li v-for="c in conversations" :key="c.id">
          <button
            type="button"
            class="flex w-full items-start gap-3 px-4 py-3 text-left hover:bg-elevated"
            :class="c.id === selectedId ? 'bg-elevated' : ''"
            @click="open(c.id)"
          >
            <!-- An icon, not initials: placeholder names ("ลูกค้า LINE …") would all look alike. -->
            <UAvatar icon="i-lucide-user" size="md" class="shrink-0 bg-green-500/15 text-green-700" />
            <div class="min-w-0 flex-1">
              <div class="flex items-baseline justify-between gap-2">
                <p class="truncate" :class="c.unread_count ? 'font-semibold' : 'font-medium'">
                  {{ conversationName(c) }}
                </p>
                <span class="shrink-0 text-xs text-muted">{{ formatShortTime(c.last_message_at) }}</span>
              </div>
              <div class="flex items-center justify-between gap-2">
                <p class="truncate text-sm" :class="c.unread_count ? 'text-default' : 'text-muted'">
                  {{ chatSummary(c.last_message_type, c.last_message_text) }}
                </p>
                <UBadge v-if="c.unread_count" :label="String(c.unread_count)" size="sm" color="success" class="shrink-0 rounded-full" />
              </div>
            </div>
          </button>
        </li>
      </ul>
    </UCard>

    <!-- Open conversation -->
    <UCard v-if="selectedId" :ui="{ header: 'p-3 sm:px-4', body: 'p-0 sm:p-0' }">
      <template #header>
        <div class="flex items-center justify-between gap-2">
          <div class="flex min-w-0 items-center gap-2">
            <UButton icon="i-lucide-arrow-left" color="neutral" variant="ghost" class="md:hidden" aria-label="กลับไปรายการแชท" @click="open(null)" />
            <p class="truncate font-semibold">
              {{ thread ? conversationName(thread.conversation) : '…' }}
            </p>
            <UBadge label="LINE" color="success" variant="subtle" size="sm" />
          </div>
          <UButton v-if="thread" :to="orderLink" icon="i-lucide-receipt" size="sm" class="shrink-0">
            <!-- Short label on phones, so the customer's name keeps the room. -->
            <span class="sm:hidden">สร้างออเดอร์</span>
            <span class="hidden sm:inline">สร้างออเดอร์จากแชทนี้</span>
          </UButton>
        </div>
      </template>

      <p v-if="threadError" class="p-6 text-center text-sm text-muted">
        ไม่พบแชทนี้
      </p>
      <div v-else ref="scroller" class="max-h-[60vh] min-h-64 space-y-2 overflow-y-auto bg-muted/40 p-4">
        <div
          v-for="m in thread?.messages ?? []"
          :key="m.id"
          class="flex"
          :class="m.direction === 'out' ? 'justify-end' : 'justify-start'"
        >
          <div
            class="max-w-[80%] rounded-2xl px-3 py-2 text-sm"
            :class="m.direction === 'out' ? 'bg-primary text-inverted' : 'bg-default ring ring-default'"
          >
            <p class="whitespace-pre-line break-words" :class="m.type === 'text' ? '' : 'italic text-muted'">
              {{ chatSummary(m.type, m.text) }}
            </p>
            <p class="mt-0.5 text-right text-[11px] text-muted">
              {{ formatDateTime(m.sent_at) }}
            </p>
          </div>
        </div>
      </div>
      <p class="border-t border-default px-4 py-2 text-xs text-muted">
        ตอบกลับลูกค้าในแอป LINE Official Account ได้ตามปกติ (การตอบจากหน้านี้จะตามมาในเวอร์ชันถัดไป)
      </p>
    </UCard>

    <UCard v-else class="hidden md:flex md:items-center md:justify-center">
      <p class="text-sm text-muted">
        เลือกแชททางซ้ายเพื่ออ่านข้อความ
      </p>
    </UCard>
  </div>
</template>
