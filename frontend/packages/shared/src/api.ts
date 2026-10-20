// TypeScript mirror of the Go API's JSON (backend/internal/*). Keep the two in sync:
// a field renamed in Go must be renamed here, or the compiler will not catch the mismatch.

/** UUID string, e.g. "01a13e36-9147-713b-8d34-73eb49a30128". */
export type ID = string
/** RFC 3339 timestamp string, e.g. "2026-10-15T06:18:45.943158Z". */
export type Timestamp = string
/** Money as an integer number of satang (1 THB = 100 satang). Never a float. */
export type Satang = number

// ---- Errors -------------------------------------------------------------------

/** Every API error has this shape: {"error": {...}}. */
export interface ApiErrorBody {
  error: {
    code: string
    message: string
    details?: unknown
  }
}

/** Details of a "validation_failed" error. */
export interface FieldError {
  field: string
  rule: string
}

// ---- Auth / tenants -----------------------------------------------------------

export type Role = 'owner' | 'admin' | 'staff'

export interface User {
  id: ID
  email: string
  display_name: string
  created_at: Timestamp
  updated_at: Timestamp
}

export interface Tenant {
  id: ID
  slug: string
  name: string
  status: 'active' | 'suspended'
  /** Why the platform suspended the shop; empty while active. */
  suspended_reason: string
  created_at: Timestamp
  updated_at: Timestamp
}

export interface Membership {
  tenant: Tenant
  role: Role
}

/** Session as returned by the Go API. Only the merchant app's server sees it; browsers never do. */
export interface Session {
  token: string
  expires_at: Timestamp
}

export interface MeResponse {
  user: User
  memberships: Membership[]
}

export interface LoginRequest {
  email: string
  password: string
}

export interface RegisterRequest {
  email: string
  password: string
  display_name: string
  shop_name: string
  shop_slug: string
}

export interface TenantResponse {
  tenant: Tenant
  role: Role
}

// ---- Products -----------------------------------------------------------------

export type ProductStatus = 'active' | 'archived'

export interface Product {
  id: ID
  tenant_id: ID
  sku: string | null
  name: string
  description: string
  price_satang: Satang
  stock: number
  status: ProductStatus
  created_at: Timestamp
  updated_at: Timestamp
  /** Photos in display order; the first is the cover. */
  images: ProductImage[]
}

/** A product photo. Files never change: a new photo gets a new URL (cache-friendly). */
export interface ProductImage {
  id: ID
  /** Up to 1200 px. */
  url: string
  /** Up to 400 px, for lists and grids. */
  thumb_url: string
  width: number
  height: number
}

export interface CreateProductRequest {
  sku?: string
  name: string
  description?: string
  price_satang: Satang
  stock?: number
}

export type UpdateProductRequest = Partial<CreateProductRequest> & { status?: ProductStatus }

// ---- Orders -------------------------------------------------------------------

export type OrderStatus = 'pending' | 'paid' | 'shipped' | 'completed' | 'cancelled'
export type OrderChannel = 'manual' | 'line' | 'facebook' | 'instagram' | 'storefront'
/** Who or what cancelled an order: a shop member, or the worker (unpaid storefront order). */
export type CancelReason = 'merchant' | 'payment_timeout'

export interface OrderItem {
  id: ID
  product_id: ID
  product_name: string
  sku: string | null
  unit_price_satang: Satang
  quantity: number
  line_total_satang: Satang
}

export interface Order {
  id: ID
  tenant_id: ID
  order_number: number
  customer_name: string
  customer_phone: string
  shipping_address: string
  channel: OrderChannel
  note: string
  status: OrderStatus
  subtotal_satang: Satang
  shipping_fee_satang: Satang
  total_satang: Satang
  created_by: ID | null
  paid_at: Timestamp | null
  shipped_at: Timestamp | null
  completed_at: Timestamp | null
  cancelled_at: Timestamp | null
  /** Set exactly when status is 'cancelled'. */
  cancel_reason: CancelReason | null
  created_at: Timestamp
  updated_at: Timestamp
  /** Present on a single order (GET /orders/:id, create, status change); absent in lists. */
  items?: OrderItem[]
}

export interface CreateOrderRequest {
  customer_name: string
  customer_phone?: string
  shipping_address?: string
  channel?: OrderChannel
  note?: string
  shipping_fee_satang?: Satang
  items: { product_id: ID; quantity: number }[]
}

// ---- Storefront (public, no login) ----------------------------------------------
// Public views returned by /api/v1/public/shops/:slug/...: deliberately smaller than the
// merchant types (no SKU, phone, address or internal ids).

export interface StorefrontShop {
  slug: string
  name: string
}

export interface StorefrontProduct {
  id: ID
  name: string
  description: string
  price_satang: Satang
  stock: number
  images: ProductImage[]
}

export interface StorefrontOrderLine {
  product_name: string
  unit_price_satang: Satang
  quantity: number
  line_total_satang: Satang
}

export interface StorefrontOrder {
  order_number: number
  status: OrderStatus
  customer_name: string
  items: StorefrontOrderLine[]
  subtotal_satang: Satang
  shipping_fee_satang: Satang
  total_satang: Satang
  created_at: Timestamp
  paid_at: Timestamp | null
  shipped_at: Timestamp | null
  completed_at: Timestamp | null
  cancelled_at: Timestamp | null
  /** Payment deadline while pending; after it the order is cancelled automatically. */
  pay_by: Timestamp | null
  cancel_reason: CancelReason | null
}

export interface PlaceOrderRequest {
  customer_name: string
  customer_phone: string
  shipping_address: string
  note?: string
  items: { product_id: ID; quantity: number }[]
}

export interface PlaceOrderResponse {
  order: StorefrontOrder
  /** Shown once: the customer's only key to view the order later. */
  tracking_token: string
}

// ---- Pagination ---------------------------------------------------------------

/** Keyset-paginated list: pass next_cursor back as ?cursor= to get the next page. */
export interface Page<T> {
  items: T[]
  next_cursor: ID | null
}

// ---- Chat inbox ---------------------------------------------------------------------
// Messages customers send to the shop's LINE Official Account (stored in MongoDB).

/** LINE message types; other providers may add more, so any string is accepted. */
export type ChatMessageType = 'text' | 'sticker' | 'image' | 'video' | 'audio' | 'file' | 'location' | (string & {})

export interface Conversation {
  id: string
  channel: 'line'
  /** From the customer's LINE profile; empty until it can be fetched (needs the access token). */
  display_name: string
  /** Profile picture URL on LINE's servers; empty when unknown. */
  picture_url: string
  /** Last characters of the customer's LINE id, to tell nameless customers apart. */
  customer_ref: string
  last_message_at: Timestamp
  last_message_type: ChatMessageType
  last_message_text: string
  unread_count: number
  created_at: Timestamp
}

export interface ChatMessage {
  id: string
  conversation_id: string
  direction: 'in' | 'out'
  type: ChatMessageType
  /** Empty for non-text messages. */
  text: string
  sent_at: Timestamp
}

export interface ConversationThread {
  conversation: Conversation
  messages: ChatMessage[]
}

/** A shop's LINE connection. The channel secret itself is never returned. */
export interface LineChannel {
  id: string
  /** Append to the API's public base URL and paste into the LINE Developers console. */
  webhook_path: string
  secret_hint: string
  /** An access token is saved: the inbox can reply and show customers' names. */
  can_reply: boolean
  /** The Official Account the token belongs to. */
  bot_name: string
  bot_basic_id: string
  created_at: Timestamp
  updated_at: Timestamp
}

// --- Platform console (/api/v1/platform, platform admins only) ---

/** A shop as the platform sees it: counts and sums only, no customers or orders. */
export interface PlatformTenant {
  id: ID
  slug: string
  name: string
  status: 'active' | 'suspended'
  suspended_reason: string
  suspended_at: Timestamp | null
  created_at: Timestamp
  owner_email: string
  member_count: number
  /** Products on sale. */
  product_count: number
  order_count: number
  /** Paid, shipped and completed orders. */
  revenue_satang: number
  last_order_at: Timestamp | null
}

/** One Thai calendar day; days without orders are included with zeros. */
export interface PlatformDailyPoint {
  /** YYYY-MM-DD */
  day: string
  orders: number
  revenue_satang: number
}

export interface PlatformOverview {
  tenants: { total: number, active: number, suspended: number }
  users: number
  /** Totals of `daily` (the last 30 days). */
  orders: number
  revenue_satang: number
  daily: PlatformDailyPoint[]
}

export interface PlatformAuditEntry {
  id: ID
  action: 'tenant.suspend' | 'tenant.reactivate'
  reason: string
  created_at: Timestamp
  actor_email: string
  tenant_id: ID
  tenant_name: string
}
