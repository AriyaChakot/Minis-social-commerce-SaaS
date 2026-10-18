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

const timeOnly = new Intl.DateTimeFormat('th-TH', { timeStyle: 'short', timeZone: 'Asia/Bangkok' })
const dayMonth = new Intl.DateTimeFormat('th-TH', { day: 'numeric', month: 'short', timeZone: 'Asia/Bangkok' })
const bangkokDay = new Intl.DateTimeFormat('en-CA', { timeZone: 'Asia/Bangkok' }) // "2026-10-15"

/** Short time for lists, like chat apps: "13:18" today, "14 ต.ค." on earlier days (Bangkok time). */
export function formatShortTime(ts: Timestamp, now: Date = new Date()): string {
  const d = new Date(ts)
  return bangkokDay.format(d) === bangkokDay.format(now) ? timeOnly.format(d) : dayMonth.format(d)
}
