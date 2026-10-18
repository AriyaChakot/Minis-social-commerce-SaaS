<script setup lang="ts">
import { z } from 'zod'
import type { FormSubmitEvent } from '@nuxt/ui'
import type { LineChannel } from '@mscs/shared'
import { formatDateTime } from '@mscs/shared'

useHead({ title: 'ตั้งค่า' })

const shop = useShop()
const toast = useToast()
const { public: { webhookBaseUrl } } = useRuntimeConfig()

// Staff reach this page only by typing the URL; the API refuses their changes anyway (403).
if (!shop.value.canManage) {
  throw createError({ statusCode: 403, statusMessage: 'เฉพาะเจ้าของร้านและผู้ดูแลเท่านั้น' })
}

const { data } = await useFetch<{ line: LineChannel | null }>(() => `/api/tenants/${shop.value.id}/channels/line`, {
  key: () => `line-channel-${shop.value.id}`,
})
const line = computed(() => data.value?.line ?? null)
const webhookUrl = computed(() => (line.value ? `${webhookBaseUrl}${line.value.webhook_path}` : ''))

// Same rule as the API and the database: 32 hex characters.
const schema = z.object({
  channel_secret: z.string().trim().regex(/^[0-9a-fA-F]{32}$/, 'Channel secret ต้องเป็นตัวอักษร 0-9 และ a-f จำนวน 32 ตัว'),
})
const state = reactive({ channel_secret: '' })
const saving = ref(false)

async function onSubmit(event: FormSubmitEvent<z.output<typeof schema>>) {
  saving.value = true
  try {
    data.value = await $fetch<{ line: LineChannel }>(`/api/tenants/${shop.value.id}/channels/line`, {
      method: 'PUT',
      body: event.data,
    })
    state.channel_secret = ''
    toast.add({ title: 'บันทึกการเชื่อมต่อ LINE แล้ว', color: 'success', icon: 'i-lucide-check' })
  }
  catch (e) {
    toast.add({ title: apiErrorMessage(e), color: 'error' })
  }
  finally {
    saving.value = false
  }
}

const copied = ref(false)
async function copyWebhook() {
  await navigator.clipboard.writeText(webhookUrl.value)
  copied.value = true
  setTimeout(() => (copied.value = false), 2000)
}

// Dev-only: how to send test messages without LINE (LINE cannot reach localhost).
const isDev = import.meta.dev
</script>

<template>
  <div class="max-w-2xl space-y-6">
    <UCard>
      <template #header>
        <div class="flex items-center justify-between gap-3">
          <div>
            <h2 class="font-semibold">
              LINE Official Account
            </h2>
            <p class="text-sm text-muted">
              รับข้อความจากลูกค้าที่ทักทาง LINE มาไว้ในหน้า “แชท”
            </p>
          </div>
          <UBadge :label="line ? 'เชื่อมต่อแล้ว' : 'ยังไม่เชื่อมต่อ'" :color="line ? 'success' : 'neutral'" variant="subtle" />
        </div>
      </template>

      <div class="space-y-5 text-sm">
        <ol class="list-decimal space-y-1 pl-5 text-muted">
          <li>เปิด LINE Developers Console → เลือก channel แบบ Messaging API ของร้าน</li>
          <li>แท็บ <b>Basic settings</b> → คัดลอก <b>Channel secret</b> มาวางด้านล่าง แล้วกดบันทึก</li>
          <li>แท็บ <b>Messaging API</b> → วาง <b>Webhook URL</b> ที่ได้ → เปิด <b>Use webhook</b> → กด <b>Verify</b></li>
        </ol>

        <div v-if="line" class="space-y-3 rounded-lg bg-elevated p-4">
          <div>
            <p class="text-muted">
              Webhook URL
            </p>
            <div class="mt-1 flex gap-2">
              <UInput :model-value="webhookUrl" readonly class="flex-1 font-mono" />
              <UButton :icon="copied ? 'i-lucide-check' : 'i-lucide-copy'" :label="copied ? 'คัดลอกแล้ว' : 'คัดลอก'" color="neutral" variant="outline" @click="copyWebhook" />
            </div>
          </div>
          <p class="text-muted">
            Channel secret ที่บันทึกไว้ลงท้ายด้วย <span class="font-mono text-default">{{ line.secret_hint }}</span>
            · แก้ไขล่าสุด {{ formatDateTime(line.updated_at) }}
          </p>
        </div>

        <UForm :schema="schema" :state="state" class="space-y-3" @submit="onSubmit">
          <UFormField :label="line ? 'เปลี่ยน Channel secret' : 'Channel secret'" name="channel_secret" help="เราเก็บไว้เพื่อตรวจว่าข้อความมาจาก LINE จริง และจะไม่แสดงค่านี้อีก">
            <UInput v-model="state.channel_secret" type="password" autocomplete="off" class="w-full font-mono" />
          </UFormField>
          <UButton type="submit" :loading="saving" :label="line ? 'บันทึก secret ใหม่' : 'เชื่อมต่อ LINE'" />
        </UForm>

        <div v-if="isDev && line" class="rounded-lg border border-dashed border-default p-4 text-muted">
          <p class="font-medium text-default">
            ทดสอบในเครื่อง (เห็นเฉพาะตอน dev)
          </p>
          <p>LINE ส่งข้อความเข้า localhost ไม่ได้ ใช้ตัวจำลองส่งข้อความตัวอย่างแทน:</p>
          <pre class="mt-2 overflow-x-auto rounded bg-elevated p-2 font-mono text-xs">.\scripts\go.ps1 --db run ./cmd/linesim -channel {{ line.id }} -secret &lt;channel secret&gt;</pre>
        </div>
      </div>
    </UCard>
  </div>
</template>
