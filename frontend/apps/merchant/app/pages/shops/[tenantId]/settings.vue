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

const endpoint = computed(() => `/api/tenants/${shop.value.id}/channels/line`)
const { data } = await useFetch<{ line: LineChannel | null }>(endpoint, {
  key: () => `line-channel-${shop.value.id}`,
})
const line = computed(() => data.value?.line ?? null)
const webhookUrl = computed(() => (line.value ? `${webhookBaseUrl}${line.value.webhook_path}` : ''))

// Both credentials are saved with the same PUT, one field at a time; neither is ever shown again.
async function save(body: { channel_secret: string } | { access_token: string }) {
  data.value = await $fetch<{ line: LineChannel }>(endpoint.value, { method: 'PUT', body })
}

// --- Channel secret: receive messages (same rule as the API: 32 hex characters) ---
const secretSchema = z.object({
  channel_secret: z.string().trim().regex(/^[0-9a-fA-F]{32}$/, 'Channel secret ต้องเป็นตัวอักษร 0-9 และ a-f จำนวน 32 ตัว'),
})
const secretState = reactive({ channel_secret: '' })
const savingSecret = ref(false)

async function onSaveSecret(event: FormSubmitEvent<z.output<typeof secretSchema>>) {
  savingSecret.value = true
  try {
    await save(event.data)
    secretState.channel_secret = ''
    toast.add({ title: 'บันทึกการเชื่อมต่อ LINE แล้ว', color: 'success', icon: 'i-lucide-check' })
  }
  catch (e) {
    toast.add({ title: apiErrorMessage(e), color: 'error' })
  }
  finally {
    savingSecret.value = false
  }
}

// --- Channel access token: reply and see names. The API checks it with LINE before saving. ---
const tokenSchema = z.object({
  access_token: z.string().trim().min(20, 'วาง Channel access token ที่ได้จาก LINE Developers').max(1000),
})
const tokenState = reactive({ access_token: '' })
const savingToken = ref(false)
const tokenForm = useTemplateRef('tokenForm')

async function onSaveToken(event: FormSubmitEvent<z.output<typeof tokenSchema>>) {
  savingToken.value = true
  try {
    await save(event.data)
    tokenState.access_token = ''
    toast.add({ title: `ตอบแชทในนาม ${line.value?.bot_name} ได้แล้ว`, color: 'success', icon: 'i-lucide-check' })
  }
  catch (e) {
    // "LINE did not accept this token": show it under the field, where the merchant is looking.
    if (apiFieldErrors(e).some(f => f.field === 'access_token')) {
      tokenForm.value?.setErrors([{ name: 'access_token', message: 'LINE ไม่ยอมรับ token นี้ ตรวจว่าคัดลอกมาครบ หรือกด Issue ใหม่' }])
    }
    else {
      toast.add({ title: apiErrorMessage(e), color: 'error' })
    }
  }
  finally {
    savingToken.value = false
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

        <UForm :schema="secretSchema" :state="secretState" class="space-y-3" @submit="onSaveSecret">
          <UFormField :label="line ? 'เปลี่ยน Channel secret' : 'Channel secret'" name="channel_secret" help="ใช้ตรวจว่าข้อความมาจาก LINE จริง เราเก็บแบบเข้ารหัสและจะไม่แสดงค่านี้อีก">
            <UInput v-model="secretState.channel_secret" type="password" autocomplete="off" class="w-full font-mono" />
          </UFormField>
          <UButton type="submit" :loading="savingSecret" :label="line ? 'บันทึก secret ใหม่' : 'เชื่อมต่อ LINE'" />
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

    <!-- Step 2: replies. Only after the secret, since the token belongs to the same channel. -->
    <UCard v-if="line">
      <template #header>
        <div class="flex items-center justify-between gap-3">
          <div>
            <h2 class="font-semibold">
              ตอบแชทและดูชื่อลูกค้า
            </h2>
            <p class="text-sm text-muted">
              ใส่ Channel access token เพื่อพิมพ์ตอบลูกค้าจากหน้า “แชท” และเห็นชื่อกับรูปโปรไฟล์ LINE
            </p>
          </div>
          <UBadge :label="line.can_reply ? 'ตอบแชทได้' : 'ยังตอบไม่ได้'" :color="line.can_reply ? 'success' : 'neutral'" variant="subtle" class="shrink-0" />
        </div>
      </template>

      <div class="space-y-5 text-sm">
        <p v-if="line.can_reply" class="rounded-lg bg-elevated p-4 text-muted">
          ตอบในนามบัญชี <span class="font-medium text-default">{{ line.bot_name }}</span>
          <span v-if="line.bot_basic_id" class="font-mono"> ({{ line.bot_basic_id }})</span>
        </p>
        <p v-else class="text-muted">
          LINE Developers Console → แท็บ <b>Messaging API</b> → ล่างสุด <b>Channel access token (long-lived)</b> → กด <b>Issue</b> แล้วคัดลอกมาวาง
        </p>

        <UForm ref="tokenForm" :schema="tokenSchema" :state="tokenState" class="space-y-3" @submit="onSaveToken">
          <UFormField :label="line.can_reply ? 'เปลี่ยน Channel access token' : 'Channel access token'" name="access_token" help="ระบบจะทดสอบกับ LINE ก่อนบันทึก และเก็บแบบเข้ารหัส">
            <UInput v-model="tokenState.access_token" type="password" autocomplete="off" class="w-full font-mono" />
          </UFormField>
          <UButton type="submit" :loading="savingToken" :label="line.can_reply ? 'บันทึก token ใหม่' : 'บันทึก token'" />
        </UForm>

        <p class="text-xs text-muted">
          ข้อความที่ตอบจากระบบนับเป็นข้อความ push ของ LINE OA ซึ่งแพ็กเกจฟรีส่งได้จำนวนจำกัดต่อเดือน
        </p>
      </div>
    </UCard>
  </div>
</template>
