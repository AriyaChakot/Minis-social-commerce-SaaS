import type { OrderStatus } from './api'

/**
 * Mirror of the Go order state machine (backend/internal/order/order.go).
 * The server is the authority; frontends use this only to decide which buttons to show.
 *
 *   pending ──► paid ──► shipped ──► completed
 *      │          │
 *      └──────────┴──► cancelled
 */
export const ORDER_TRANSITIONS: Readonly<Record<OrderStatus, readonly OrderStatus[]>> = {
  pending: ['paid', 'cancelled'],
  paid: ['shipped', 'cancelled'],
  shipped: ['completed'],
  completed: [],
  cancelled: [],
}

export function nextStatuses(status: OrderStatus): readonly OrderStatus[] {
  return ORDER_TRANSITIONS[status]
}
