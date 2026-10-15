import type { Timestamp } from './api'

// Fixed time zone: the server render and the browser render then produce the same text
// (no hydration mismatch), whatever time zone the server machine runs in.
const dateTime = new Intl.DateTimeFormat('th-TH', {
  dateStyle: 'medium',
  timeStyle: 'short',
  timeZone: 'Asia/Bangkok',
})

/** "2026-10-15T06:18:45Z" -> "15 ต.ค. 2569 13:18" (Thai Buddhist-era year, Bangkok time) */
export function formatDateTime(ts: Timestamp): string {
  return dateTime.format(new Date(ts))
}
