import type { ChatMessageType, Conversation, OrderChannel, OrderStatus, ProductStatus, Role } from '@mscs/shared'

// Thai labels and badge colors for API enum values (auto-imported in app/).
// Record<Union, ...> makes TypeScript fail the build if a new enum value has no label.

type BadgeColor = 'primary' | 'secondary' | 'success' | 'info' | 'warning' | 'error' | 'neutral'

export const ROLE_LABEL: Record<Role, string> = { owner: 'เจ้าของ', admin: 'ผู้ดูแล', staff: 'พนักงาน' }
export const ROLE_COLOR: Record<Role, BadgeColor> = { owner: 'primary', admin: 'info', staff: 'neutral' }

export const PRODUCT_STATUS_LABEL: Record<ProductStatus, string> = { active: 'วางขาย', archived: 'ปิดการขาย' }
export const PRODUCT_STATUS_COLOR: Record<ProductStatus, BadgeColor> = { active: 'success', archived: 'neutral' }

export const ORDER_STATUS_LABEL: Record<OrderStatus, string> = {
  pending: 'รอชำระเงิน',
  paid: 'ชำระแล้ว',
  shipped: 'จัดส่งแล้ว',
  completed: 'สำเร็จ',
  cancelled: 'ยกเลิก',
}
export const ORDER_STATUS_COLOR: Record<OrderStatus, BadgeColor> = {
  pending: 'warning',
  paid: 'info',
  shipped: 'primary',
  completed: 'success',
  cancelled: 'neutral',
}

/** Button text for moving an order INTO each status. */
export const ORDER_ACTION_LABEL: Record<OrderStatus, string> = {
  pending: '',
  paid: 'ยืนยันรับชำระเงิน',
  shipped: 'บันทึกว่าจัดส่งแล้ว',
  completed: 'ปิดออเดอร์ (สำเร็จ)',
  cancelled: 'ยกเลิกออเดอร์',
}

export const CHANNEL_LABEL: Record<OrderChannel, string> = {
  manual: 'บันทึกเอง',
  line: 'LINE',
  facebook: 'Facebook',
  instagram: 'Instagram',
  storefront: 'หน้าร้านออนไลน์',
}

/** Placeholder text for chat messages that are not text (no media viewer yet). */
const CHAT_TYPE_LABEL: Record<string, string> = {
  sticker: 'ส่งสติกเกอร์',
  image: 'ส่งรูปภาพ',
  video: 'ส่งวิดีโอ',
  audio: 'ส่งข้อความเสียง',
  file: 'ส่งไฟล์',
  location: 'ส่งตำแหน่งที่ตั้ง',
}

/** What to show for a message: its text, or a description of a non-text message. */
export function chatSummary(type: ChatMessageType, text: string): string {
  if (type === 'text') return text
  return `[${CHAT_TYPE_LABEL[type] ?? 'ส่งข้อความประเภทอื่น'}]`
}

/** The customer's name, or a stable placeholder until LINE profiles are fetched. */
export function conversationName(c: Conversation): string {
  return c.display_name || `ลูกค้า LINE …${c.customer_ref}`
}
