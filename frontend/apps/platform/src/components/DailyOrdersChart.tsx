import { useState } from 'react'
import { formatTHB, type PlatformDailyPoint } from '@mscs/shared'

// "2026-10-20" is a calendar day, not an instant: format it in UTC so no time zone shifts it.
const dayLabel = new Intl.DateTimeFormat('th-TH', { day: 'numeric', month: 'short', timeZone: 'UTC' })
const fullDay = new Intl.DateTimeFormat('th-TH', { weekday: 'short', day: 'numeric', month: 'short', timeZone: 'UTC' })
const label = (day: string) => dayLabel.format(new Date(`${day}T00:00:00Z`))
const longLabel = (day: string) => fullDay.format(new Date(`${day}T00:00:00Z`))

/** A round axis maximum (1, 2, 5 × 10^n) at or above the largest value, so gridlines read easily. */
function niceMax(max: number): number {
  if (max <= 4) return 4
  const pow = 10 ** Math.floor(Math.log10(max))
  return ([1, 2, 5, 10].map(m => m * pow).find(v => v >= max)) ?? max
}

/**
 * Orders per day across all shops. One series, so no legend: the title names it.
 * Bars are anchored to the baseline with rounded tops; every column (not just the bar) is the
 * hover/focus target, so short bars and empty days are easy to point at.
 */
export function DailyOrdersChart({ points }: { points: PlatformDailyPoint[] }) {
  const [active, setActive] = useState<number | null>(null)
  const [asTable, setAsTable] = useState(false)
  const top = niceMax(Math.max(0, ...points.map(p => p.orders)))
  const ticks = [top, top / 2, 0]

  return (
    <section className="rounded-xl border border-line bg-surface p-4">
      <div className="mb-3 flex items-baseline justify-between gap-3">
        <h2 className="font-semibold">ออเดอร์รายวัน (ทุกร้าน, 30 วันล่าสุด)</h2>
        <button type="button" className="shrink-0 text-sm text-ink-muted underline" onClick={() => setAsTable(t => !t)}>
          {asTable ? 'ดูเป็นกราฟ' : 'ดูเป็นตาราง'}
        </button>
      </div>

      {asTable
        ? (
            <div className="max-h-72 overflow-y-auto">
              <table className="w-full text-sm">
                <thead className="sticky top-0 bg-surface text-left text-ink-muted">
                  <tr><th className="py-1 font-normal">วัน</th><th className="py-1 text-right font-normal">ออเดอร์</th><th className="py-1 text-right font-normal">ยอดขายที่ชำระแล้ว</th></tr>
                </thead>
                <tbody>
                  {[...points].reverse().map(p => (
                    <tr key={p.day} className="border-t border-line">
                      <td className="py-1">{longLabel(p.day)}</td>
                      <td className="py-1 text-right tabular-nums">{p.orders}</td>
                      <td className="py-1 text-right tabular-nums">{formatTHB(p.revenue_satang)}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )
        : (
            <div className="flex gap-2">
              {/* Y axis: three recessive gridline labels. */}
              <div className="flex h-44 flex-col justify-between text-right text-xs tabular-nums text-ink-muted">
                {ticks.map(t => <span key={t} className="-translate-y-1/2 first:translate-y-0 last:translate-y-0">{t}</span>)}
              </div>
              <div className="min-w-0 flex-1">
                <div className="relative h-44">
                  {ticks.map((t, i) => (
                    <div key={t} className="absolute inset-x-0 border-t border-line" style={{ top: `${(i / (ticks.length - 1)) * 100}%` }} />
                  ))}
                  <div className="absolute inset-0 flex items-end gap-0.5" onMouseLeave={() => setActive(null)}>
                    {points.map((p, i) => (
                      <button
                        key={p.day}
                        type="button"
                        aria-label={`${longLabel(p.day)}: ${p.orders} ออเดอร์, ยอดขาย ${formatTHB(p.revenue_satang)}`}
                        className="relative flex h-full flex-1 items-end outline-none"
                        onMouseEnter={() => setActive(i)}
                        onFocus={() => setActive(i)}
                        onBlur={() => setActive(null)}
                      >
                        <span
                          className={`w-full rounded-t bg-accent transition-opacity ${active !== null && active !== i ? 'opacity-50' : ''}`}
                          style={{ height: `${(p.orders / top) * 100}%`, minHeight: p.orders > 0 ? 2 : 0 }}
                        />
                        {active === i && (
                          <span
                            role="tooltip"
                            className={`pointer-events-none absolute bottom-full z-10 mb-1 whitespace-nowrap rounded-md border border-line bg-surface px-2 py-1 text-left text-xs shadow-md ${
                              i < 4 ? 'left-0' : i > points.length - 5 ? 'right-0' : 'left-1/2 -translate-x-1/2'}`}
                          >
                            <span className="block font-medium">{longLabel(p.day)}</span>
                            <span className="block tabular-nums">{p.orders} ออเดอร์</span>
                            <span className="block tabular-nums text-ink-muted">ชำระแล้ว {formatTHB(p.revenue_satang)}</span>
                          </span>
                        )}
                      </button>
                    ))}
                  </div>
                </div>
                {/* X axis: first, middle and last day only, so labels never collide. */}
                <div className="mt-1 flex justify-between text-xs text-ink-muted">
                  <span>{points[0] && label(points[0].day)}</span>
                  <span>{points[Math.floor(points.length / 2)] && label(points[Math.floor(points.length / 2)]!.day)}</span>
                  <span>{points.at(-1) && 'วันนี้'}</span>
                </div>
              </div>
            </div>
          )}
    </section>
  )
}
